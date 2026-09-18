package controller

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"path"
	"strings"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	platformv1alpha1 "github.com/miku-wwl/kube-platform-control-plane/api/v1alpha1"
	"github.com/miku-wwl/kube-platform-control-plane/internal/reliability"
	terraformexec "github.com/miku-wwl/kube-platform-control-plane/internal/terraform"
)

// +kubebuilder:rbac:groups=platform.example.io,resources=terraformruns,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=platform.example.io,resources=terraformruns/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=platform.example.io,resources=terraformruns/finalizers,verbs=update
// +kubebuilder:rbac:groups=batch,resources=jobs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get
// +kubebuilder:rbac:groups="",resources=pods,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=pods/log,verbs=get
type TerraformRunReconciler struct {
	client.Client
	Scheme           *runtime.Scheme
	ExecutionEnabled bool
	GitImage         string
	ArtifactEndpoint string
	ArtifactRegion   string
	ArtifactBucket   string
	KubeClient       kubernetes.Interface
	Gate             *reliability.Gate
}

type runnerTerminalResult struct {
	Operation           string `json:"operation"`
	TerraformExitCode   int    `json:"terraformExitCode"`
	ExecutionOutcome    string `json:"executionOutcome"`
	ArtifactsReady      bool   `json:"artifactsReady"`
	HasChanges          bool   `json:"hasChanges"`
	Error               string `json:"error,omitempty"`
	PlanRef             string `json:"planRef,omitempty"`
	PlanDigest          string `json:"planDigest,omitempty"`
	SourceBundleRef     string `json:"sourceBundleRef,omitempty"`
	SourceBundleDigest  string `json:"sourceBundleDigest,omitempty"`
	BackendConfigRef    string `json:"backendConfigRef,omitempty"`
	BackendConfigDigest string `json:"backendConfigDigest,omitempty"`
}

func (r *TerraformRunReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var object platformv1alpha1.TerraformRun
	if err := r.Get(ctx, req.NamespacedName, &object); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	if !r.ExecutionEnabled {
		if object.Status.ObservedGeneration != object.Generation || !skeletonConditionCurrent(object.Status.Conditions, object.Generation) {
			object.Status.ObservedGeneration = object.Generation
			object.Status.Conditions = []metav1.Condition{skeletonCondition(object.Generation)}
			if err := r.Status().Update(ctx, &object); err != nil {
				return ctrl.Result{}, err
			}
		}
		return ctrl.Result{}, nil
	}

	if object.Status.JobRef == nil {
		if !r.ensureExecutionSlot(&object, false) {
			result, err := r.updateExecutionStatus(ctx, &object, "Pending", metav1.ConditionFalse, "ExecutionSlotUnavailable", "Terraform execution is waiting for an available concurrency slot.")
			result.RequeueAfter = time.Second
			return result, err
		}
		if object.Spec.Operation == "Apply" {
			if err := r.validateApplyApproval(ctx, &object); err != nil {
				r.releaseExecutionSlot(&object)
				return r.updateExecutionStatus(ctx, &object, "Rejected", metav1.ConditionFalse, "ApplyBindingRejected", err.Error())
			}
		}
		job, err := r.buildJob(&object)
		if err != nil {
			r.releaseExecutionSlot(&object)
			return r.updateExecutionStatus(ctx, &object, "Rejected", metav1.ConditionFalse, "TerraformJobRejected", err.Error())
		}
		var existing batchv1.Job
		getErr := r.Get(ctx, types.NamespacedName{Name: job.Name, Namespace: job.Namespace}, &existing)
		if apierrors.IsNotFound(getErr) {
			if err := ctrl.SetControllerReference(&object, job, r.Scheme); err != nil {
				return ctrl.Result{}, err
			}
			if err := r.Create(ctx, job); err != nil {
				return ctrl.Result{}, err
			}
			existing = *job
		} else if getErr != nil {
			r.releaseExecutionSlot(&object)
			return ctrl.Result{}, getErr
		}
		object.Status.JobRef = &corev1.LocalObjectReference{Name: existing.Name}
		object.Status.ObservedGeneration = object.Generation
		object.Status.Conditions = []metav1.Condition{executionCondition(object.Generation, metav1.ConditionFalse, "TerraformJobPending", "Terraform Job created; terminal evidence is not captured yet.")}
		if err := r.Status().Update(ctx, &object); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{RequeueAfter: time.Second}, nil
	}

	var job batchv1.Job
	if err := r.Get(ctx, types.NamespacedName{Name: object.Status.JobRef.Name, Namespace: object.Namespace}, &job); err != nil {
		if apierrors.IsNotFound(err) {
			return r.updateExecutionStatus(ctx, &object, "Indeterminate", metav1.ConditionUnknown, "TerraformJobMissing", "Terraform Job disappeared before terminal evidence was captured.")
		}
		return ctrl.Result{}, err
	}
	if job.Status.Succeeded == 0 && job.Status.Failed == 0 && !r.ensureExecutionSlot(&object, true) {
		result, err := r.updateExecutionStatus(ctx, &object, "Pending", metav1.ConditionFalse, "ExecutionSlotUnavailable", "Active Terraform Job is waiting for a reconstructed concurrency slot.")
		result.RequeueAfter = time.Second
		return result, err
	}
	result, err := r.reconcileJobStatus(ctx, &object, &job)
	if err == nil && (job.Status.Succeeded > 0 || job.Status.Failed > 0) {
		r.releaseExecutionSlot(&object)
	}
	return result, err
}

func (r *TerraformRunReconciler) ensureExecutionSlot(object *platformv1alpha1.TerraformRun, existingJob bool) bool {
	if r.Gate == nil {
		return true
	}
	item := r.executionWorkItem(object)
	if existingJob {
		return r.Gate.EnsureActive(item)
	}
	return r.Gate.TryAcquire(item)
}

func (r *TerraformRunReconciler) releaseExecutionSlot(object *platformv1alpha1.TerraformRun) {
	if r.Gate != nil {
		r.Gate.Release(r.executionWorkItem(object))
	}
}

func (r *TerraformRunReconciler) executionWorkItem(object *platformv1alpha1.TerraformRun) reliability.WorkItem {
	kind := reliability.KindPlan
	if object.Spec.Operation == "Apply" {
		kind = reliability.KindApply
	}
	id := string(object.UID)
	if id == "" {
		id = object.Namespace + "/" + object.Name
	}
	return reliability.WorkItem{ID: id, Kind: kind, Stack: object.Spec.StackRef.Name}
}

func (r *TerraformRunReconciler) validateApplyApproval(ctx context.Context, apply *platformv1alpha1.TerraformRun) error {
	if apply.Spec.ApprovalRef == nil || apply.Spec.ApprovalRef.Name == "" || apply.Spec.ApprovalUID == "" || apply.Spec.PlanRunUID == "" {
		return fmt.Errorf("Apply requires approvalRef, approvalUID, and planRunUID")
	}
	var approval platformv1alpha1.ChangeApproval
	if err := r.Get(ctx, types.NamespacedName{Name: apply.Spec.ApprovalRef.Name, Namespace: apply.Namespace}, &approval); err != nil {
		return fmt.Errorf("read ChangeApproval: %w", err)
	}
	if string(approval.UID) != apply.Spec.ApprovalUID {
		return fmt.Errorf("approval UID mismatch")
	}
	if approval.Spec.PlanDigest != apply.Spec.PlanDigest || approval.Spec.ExecutionContextDigest != apply.Spec.ExecutionContextDigest {
		return fmt.Errorf("approval digest binding mismatch")
	}
	var plan platformv1alpha1.TerraformRun
	if err := r.Get(ctx, types.NamespacedName{Name: approval.Spec.PlanRunRef.Name, Namespace: apply.Namespace}, &plan); err != nil {
		return fmt.Errorf("read PlanRun: %w", err)
	}
	if string(plan.UID) != apply.Spec.PlanRunUID {
		return fmt.Errorf("PlanRun UID mismatch")
	}
	if plan.Status.PlanDigest != "" && plan.Status.PlanDigest != apply.Spec.PlanDigest {
		return fmt.Errorf("PlanRun digest mismatch")
	}
	if plan.Spec.ExecutionContextDigest != "" && plan.Spec.ExecutionContextDigest != apply.Spec.ExecutionContextDigest {
		return fmt.Errorf("PlanRun execution context mismatch")
	}
	return nil
}

func (r *TerraformRunReconciler) SetupWithManager(manager ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(manager).For(&platformv1alpha1.TerraformRun{}).Owns(&batchv1.Job{}).Complete(r)
}

func (r *TerraformRunReconciler) buildJob(object *platformv1alpha1.TerraformRun) (*batchv1.Job, error) {
	lockTimeout := object.Spec.LockTimeout.Duration
	if lockTimeout == 0 {
		lockTimeout = 5 * time.Minute
	}
	executionTimeout := object.Spec.Executor.ExecutionTimeout.Duration
	if executionTimeout == 0 {
		executionTimeout = time.Hour
	}
	workingDir := object.Spec.Executor.WorkDir
	if workingDir == "" {
		workingDir = "/workspace/terraform"
	}
	if object.Spec.Source.Path != "" {
		workingDir = path.Join(workingDir, object.Spec.Source.Path)
	}
	gitImage := r.GitImage
	if gitImage == "" {
		gitImage = terraformexec.DefaultGitImage
	}
	request := terraformexec.JobRequest{
		Name:                        object.Name,
		Namespace:                   object.Namespace,
		RunUID:                      string(object.UID),
		RunnerImage:                 object.Spec.Executor.Image,
		SourceType:                  object.Spec.Source.Type,
		SourceURL:                   object.Spec.Source.URL,
		SourceRevision:              object.Spec.Source.Revision,
		SourcePath:                  object.Spec.Source.Path,
		SourceRoot:                  sourceRootForPlan(object, workingDir),
		Operation:                   object.Spec.Operation,
		Destroy:                     object.Spec.PlanMode == "Destroy",
		WorkingDir:                  workingDir,
		Workspace:                   object.Spec.Workspace,
		BackendConfigMap:            object.Spec.ResolvedBackendConfigRef,
		BackendConfigPath:           "/workspace/backend/backend.hcl",
		PlanPath:                    "/workspace/terraform/plan.binary",
		LockTimeout:                 lockTimeout,
		ExecutionTimeout:            executionTimeout,
		Parallelism:                 object.Spec.Executor.Parallelism,
		GitImage:                    gitImage,
		ArtifactEndpoint:            r.ArtifactEndpoint,
		ArtifactRegion:              r.ArtifactRegion,
		ArtifactBucket:              r.ArtifactBucket,
		ArtifactPrefix:              path.Join("runs", string(object.UID)),
		BackendConfigArtifactRef:    object.Spec.BackendConfigArtifactRef,
		BackendConfigArtifactDigest: object.Spec.BackendConfigArtifactDigest,
		VariableSecretRefs:          object.Spec.VariableSecretRefs,
		PlanRef:                     object.Spec.PlanRef,
		PlanDigest:                  object.Spec.PlanDigest,
	}
	if object.Spec.Source.Type == terraformexec.SourceRetainedBundle {
		request.SourceBundleRef = object.Spec.Source.Ref
		request.SourceBundleDigest = object.Spec.Source.Digest
	}
	return terraformexec.BuildJob(request)
}

func sourceRootForPlan(object *platformv1alpha1.TerraformRun, workingDir string) string {
	if object.Spec.Operation == "Plan" && object.Spec.Source.Type == terraformexec.SourceGitCommit {
		return workingDir
	}
	return ""
}

func (r *TerraformRunReconciler) reconcileJobStatus(ctx context.Context, object *platformv1alpha1.TerraformRun, job *batchv1.Job) (ctrl.Result, error) {
	if job.Status.Succeeded > 0 || job.Status.Failed > 0 {
		terminal, err := r.captureTerminalResult(ctx, object, job)
		if err == nil {
			return r.applyTerminalResult(ctx, object, terminal)
		}
		return r.applyMissingTerminalResult(ctx, object, err)
	}
	if job.Status.Active > 0 {
		return r.updateExecutionStatus(ctx, object, "Running", metav1.ConditionFalse, "TerraformJobRunning", "Terraform Job is running.")
	}
	return r.updateExecutionStatus(ctx, object, "Pending", metav1.ConditionFalse, "TerraformJobPending", "Terraform Job is pending.")
}

func (r *TerraformRunReconciler) captureTerminalResult(ctx context.Context, object *platformv1alpha1.TerraformRun, job *batchv1.Job) (runnerTerminalResult, error) {
	if r.KubeClient == nil {
		return runnerTerminalResult{}, fmt.Errorf("kubernetes client is required for terminal evidence capture")
	}
	selector := "platform.example.io/terraform-run=" + job.Name
	if uid := job.Labels["platform.example.io/terraform-run-uid"]; uid != "" {
		selector += ",platform.example.io/terraform-run-uid=" + uid
	}
	pods, err := r.KubeClient.CoreV1().Pods(object.Namespace).List(ctx, metav1.ListOptions{LabelSelector: selector})
	if err != nil {
		return runnerTerminalResult{}, err
	}
	if len(pods.Items) == 0 {
		return runnerTerminalResult{}, fmt.Errorf("terminal Job %q has no runner Pod", job.Name)
	}
	logReader, err := r.KubeClient.CoreV1().Pods(object.Namespace).GetLogs(pods.Items[0].Name, &corev1.PodLogOptions{Container: "terraform-runner"}).Stream(ctx)
	if err != nil {
		return runnerTerminalResult{}, err
	}
	defer logReader.Close()
	logs, err := io.ReadAll(logReader)
	if err != nil {
		return runnerTerminalResult{}, err
	}
	return parseRunnerTerminalResult(string(logs))
}

func parseRunnerTerminalResult(logs string) (runnerTerminalResult, error) {
	lines := strings.Split(logs, "\n")
	for index := len(lines) - 1; index >= 0; index-- {
		line := strings.TrimSpace(lines[index])
		if line == "" {
			continue
		}
		var result runnerTerminalResult
		if err := json.Unmarshal([]byte(line), &result); err == nil && result.Operation != "" && result.ExecutionOutcome != "" {
			return result, nil
		}
	}
	return runnerTerminalResult{}, fmt.Errorf("runner terminal result was not found in Pod logs")
}

func (r *TerraformRunReconciler) applyTerminalResult(ctx context.Context, object *platformv1alpha1.TerraformRun, result runnerTerminalResult) (ctrl.Result, error) {
	object.Status.EvidenceCaptured = true
	object.Status.ArtifactsReady = result.ArtifactsReady
	object.Status.HasChanges = result.HasChanges
	if result.PlanRef != "" {
		object.Status.PlanRef = result.PlanRef
		object.Status.PlanDigest = result.PlanDigest
	}
	if result.SourceBundleRef != "" {
		object.Status.SourceBundleRef = result.SourceBundleRef
		object.Status.SourceBundleDigest = result.SourceBundleDigest
	} else if object.Status.SourceBundleRef == "" && object.Spec.Source.Type == terraformexec.SourceRetainedBundle {
		object.Status.SourceBundleRef = object.Spec.Source.Ref
		object.Status.SourceBundleDigest = object.Spec.Source.Digest
	}
	if result.BackendConfigRef != "" {
		object.Status.ResolvedBackendConfigRef = result.BackendConfigRef
		object.Status.ResolvedBackendConfigDigest = result.BackendConfigDigest
	} else if object.Status.ResolvedBackendConfigRef == "" {
		object.Status.ResolvedBackendConfigRef = object.Spec.BackendConfigArtifactRef
		object.Status.ResolvedBackendConfigDigest = object.Spec.BackendConfigArtifactDigest
	}
	if object.Spec.Operation == "Plan" && (result.ExecutionOutcome == "NoChange" || result.ExecutionOutcome == "ChangesPresent") && object.Status.PlanCreatedAt == nil {
		created := metav1.Now()
		expires := metav1.NewTime(created.Add(time.Hour))
		object.Status.PlanCreatedAt = &created
		object.Status.PlanExpiresAt = &expires
	}
	outcome := result.ExecutionOutcome
	status := metav1.ConditionFalse
	reason := "TerraformJobFailed"
	message := result.Error
	if message == "" {
		message = "Terraform runner terminal result captured."
	}
	switch outcome {
	case "Succeeded", "NoChange", "ChangesPresent":
		status = metav1.ConditionTrue
		reason = "TerraformSucceeded"
	case "Indeterminate":
		status = metav1.ConditionUnknown
		reason = "TerraformIndeterminate"
	}
	return r.updateExecutionStatus(ctx, object, outcome, status, reason, message)
}

func (r *TerraformRunReconciler) applyMissingTerminalResult(ctx context.Context, object *platformv1alpha1.TerraformRun, captureErr error) (ctrl.Result, error) {
	object.Status.EvidenceCaptured = false
	object.Status.ArtifactsReady = false
	if object.Spec.Operation == "Apply" {
		return r.updateExecutionStatus(ctx, object, "Indeterminate", metav1.ConditionUnknown, "TerraformTerminalResultMissing", captureErr.Error())
	}
	return r.updateExecutionStatus(ctx, object, "Failed", metav1.ConditionFalse, "TerraformTerminalResultMissing", captureErr.Error())
}

func (r *TerraformRunReconciler) updateExecutionStatus(ctx context.Context, object *platformv1alpha1.TerraformRun, outcome string, status metav1.ConditionStatus, reason, message string) (ctrl.Result, error) {
	object.Status.ObservedGeneration = object.Generation
	object.Status.ExecutionOutcome = outcome
	condition := executionCondition(object.Generation, status, reason, message)
	current := findCondition(object.Status.Conditions, ConditionReady)
	if current != nil && current.Status == condition.Status && current.Reason == condition.Reason && current.Message == condition.Message && object.Status.ObservedGeneration == object.Generation && object.Status.ExecutionOutcome == outcome {
		return ctrl.Result{}, nil
	}
	object.Status.Conditions = []metav1.Condition{condition}
	return ctrl.Result{}, r.Status().Update(ctx, object)
}

func executionCondition(generation int64, status metav1.ConditionStatus, reason, message string) metav1.Condition {
	return metav1.Condition{Type: ConditionReady, Status: status, ObservedGeneration: generation, Reason: reason, Message: message, LastTransitionTime: metav1.Now()}
}

func findCondition(conditions []metav1.Condition, conditionType string) *metav1.Condition {
	for index := range conditions {
		if conditions[index].Type == conditionType {
			return &conditions[index]
		}
	}
	return nil
}
