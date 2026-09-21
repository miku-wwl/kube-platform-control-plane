package controller

import (
	"context"
	"encoding/json"
	"fmt"
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
	"github.com/miku-wwl/kube-platform-control-plane/internal/artifacts"
	"github.com/miku-wwl/kube-platform-control-plane/internal/observability"
	"github.com/miku-wwl/kube-platform-control-plane/internal/reliability"
	terraformexec "github.com/miku-wwl/kube-platform-control-plane/internal/terraform"
)

// +kubebuilder:rbac:groups=platform.example.io,resources=terraformruns,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=platform.example.io,resources=terraformruns/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=platform.example.io,resources=terraformruns/finalizers,verbs=update
// +kubebuilder:rbac:groups=batch,resources=jobs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch
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
	ArtifactKMSKeyID string
	KubeClient       kubernetes.Interface
	Gate             *reliability.Gate
}

type runnerTerminalResult struct {
	RunUID                   string    `json:"terraformRunUID"`
	JobUID                   string    `json:"jobUID,omitempty"`
	StartedAt                time.Time `json:"startedAt"`
	FinishedAt               time.Time `json:"finishedAt"`
	Operation                string    `json:"operation"`
	TerraformExitCode        int       `json:"terraformExitCode"`
	ExecutionOutcome         string    `json:"executionOutcome"`
	ArtifactsReady           bool      `json:"artifactsReady"`
	HasChanges               bool      `json:"hasChanges"`
	Error                    string    `json:"error,omitempty"`
	PlanRef                  string    `json:"planRef,omitempty"`
	PlanDigest               string    `json:"planDigest,omitempty"`
	SourceBundleRef          string    `json:"sourceBundleRef,omitempty"`
	SourceBundleDigest       string    `json:"sourceBundleDigest,omitempty"`
	BackendConfigRef         string    `json:"backendConfigRef,omitempty"`
	BackendConfigDigest      string    `json:"backendConfigDigest,omitempty"`
	PlanReportRef            string    `json:"planReportRef,omitempty"`
	PlanReportDigest         string    `json:"planReportDigest,omitempty"`
	TerminalResultRef        string    `json:"terminalResultRef,omitempty"`
	TerminalResultDigest     string    `json:"terminalResultDigest,omitempty"`
	EffectivePlanInputDigest string    `json:"effectivePlanInputDigest,omitempty"`
	TargetDiscoveryRef       string    `json:"targetDiscoveryRef,omitempty"`
	TargetDiscoveryDigest    string    `json:"targetDiscoveryDigest,omitempty"`
	MutationClassification   string    `json:"mutationClassification,omitempty"`
	MutationMayHaveOccurred  bool      `json:"mutationMayHaveOccurred"`
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
	if object.Spec.Operation == "Apply" && object.Spec.MutationFence && object.Spec.PlanMode != "Destroy" {
		return r.updateExecutionStatus(ctx, &object, "Rejected", metav1.ConditionFalse, "MutationFence", "Apply admission is closed by the durable mutation fence.")
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
		if err := validateTerraformJobIdentity(&object, &existing); err != nil {
			return r.updateExecutionStatus(ctx, &object, "Indeterminate", metav1.ConditionUnknown, "TerraformJobIdentityMismatch", err.Error())
		}
		object.Status.JobRef = &corev1.LocalObjectReference{Name: existing.Name}
		if existing.UID != "" {
			object.Status.JobUID = string(existing.UID)
		}
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
	if err := validateTerraformJobIdentity(&object, &job); err != nil {
		return r.updateExecutionStatus(ctx, &object, "Indeterminate", metav1.ConditionUnknown, "TerraformJobIdentityMismatch", err.Error())
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
	return reliability.WorkItem{ID: id, Kind: kind, Stack: object.Spec.StackRef.Name, ConcurrencyGroup: object.Spec.ConcurrencyGroup, MaxConcurrentPlans: int(object.Spec.MaxConcurrentPlans), MaxConcurrentApplies: int(object.Spec.MaxConcurrentApplies)}
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
	if approval.Spec.PlanDigest != apply.Spec.PlanDigest || approval.Spec.ExecutionContextDigest != apply.Spec.ExecutionContextDigest || approval.Spec.EffectivePlanInputDigest != apply.Spec.EffectivePlanInputDigest || approval.Spec.PlanReportRef != apply.Spec.PlanReportRef || approval.Spec.PlanReportDigest != apply.Spec.PlanReportDigest {
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
	if plan.Status.PlanReportRef == "" || plan.Status.PlanReportDigest == "" || apply.Spec.PlanReportRef != plan.Status.PlanReportRef || apply.Spec.PlanReportDigest != plan.Status.PlanReportDigest {
		return fmt.Errorf("PlanRun report binding mismatch")
	}
	if plan.Spec.ExecutionContextDigest != "" && plan.Spec.ExecutionContextDigest != apply.Spec.ExecutionContextDigest {
		return fmt.Errorf("PlanRun execution context mismatch")
	}
	if plan.Status.PlanExpiresAt == nil || !time.Now().Before(plan.Status.PlanExpiresAt.Time) {
		return fmt.Errorf("saved PlanRun is expired or has no trusted expiry")
	}
	if apply.Spec.EffectivePlanInputDigest == "" || plan.Status.EffectivePlanInputDigest != apply.Spec.EffectivePlanInputDigest {
		return fmt.Errorf("effective plan input digest mismatch")
	}
	if apply.Spec.SourceClosureDigest != "" && plan.Status.SourceBundleDigest != "" && apply.Spec.SourceClosureDigest != plan.Status.SourceBundleDigest {
		return fmt.Errorf("source closure digest mismatch")
	}
	if apply.Spec.RuntimeTargetIdentityDigest != "" && plan.Spec.RuntimeTargetIdentityDigest != apply.Spec.RuntimeTargetIdentityDigest {
		return fmt.Errorf("runtime target identity digest mismatch")
	}
	if apply.Spec.InfrastructureExecutionIdentityDigest != "" && plan.Spec.InfrastructureExecutionIdentityDigest != apply.Spec.InfrastructureExecutionIdentityDigest {
		return fmt.Errorf("infrastructure execution identity digest mismatch")
	}
	return nil
}

func validateTerraformJobIdentity(run *platformv1alpha1.TerraformRun, job *batchv1.Job) error {
	if run == nil || job == nil {
		return fmt.Errorf("TerraformRun and Job are required")
	}
	if run.Status.JobUID != "" && job.UID != "" && run.Status.JobUID != string(job.UID) {
		return fmt.Errorf("Job UID changed from %q to %q", run.Status.JobUID, job.UID)
	}
	if run.UID != "" && job.Labels["platform.example.io/terraform-run-uid"] != string(run.UID) {
		return fmt.Errorf("Job terraform-run-uid label mismatch")
	}
	ownerFound := false
	for _, owner := range job.OwnerReferences {
		if owner.Controller != nil && *owner.Controller && owner.Kind == "TerraformRun" {
			ownerFound = true
			if owner.UID != run.UID {
				return fmt.Errorf("Job owner UID mismatch")
			}
		}
	}
	if run.UID != "" && !ownerFound {
		return fmt.Errorf("Job is not controller-owned by TerraformRun")
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
	backendConfigMap := ""
	if object.Spec.Operation == "Plan" && object.Spec.Source.Type == terraformexec.SourceGitCommit {
		backendConfigMap = object.Spec.ResolvedBackendConfigRef
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
		BackendConfigMap:            backendConfigMap,
		BackendConfigPath:           "/workspace/backend/backend.hcl",
		PlanPath:                    "/workspace/terraform/plan.binary",
		LockTimeout:                 lockTimeout,
		ExecutionTimeout:            executionTimeout,
		Parallelism:                 object.Spec.Executor.Parallelism,
		GitImage:                    gitImage,
		ArtifactEndpoint:            r.ArtifactEndpoint,
		ArtifactRegion:              r.ArtifactRegion,
		ArtifactBucket:              r.ArtifactBucket,
		ArtifactKMSKeyID:            r.ArtifactKMSKeyID,
		ArtifactPrefix:              path.Join("runs", string(object.UID)),
		BackendConfigArtifactRef:    object.Spec.BackendConfigArtifactRef,
		BackendConfigArtifactDigest: object.Spec.BackendConfigArtifactDigest,
		VariableSecretRefs:          object.Spec.VariableSecretRefs,
		VariableSecretVariables:     terraformSecretVariables(object.Spec.VariableSecretVariables),
		ServiceAccountName:          object.Spec.RunnerServiceAccountName,
		ExpectedTerraformVersion:    object.Spec.ExpectedTerraformVersion,
		PlanRef:                     object.Spec.PlanRef,
		PlanDigest:                  object.Spec.PlanDigest,
		TargetDiscoveryRef:          object.Spec.TargetDiscoveryRef,
		TargetDiscoveryDigest:       object.Spec.TargetDiscoveryDigest,
	}
	if object.Spec.Source.Type == terraformexec.SourceRetainedBundle {
		request.SourceBundleRef = object.Spec.Source.Ref
		request.SourceBundleDigest = object.Spec.Source.Digest
	}
	return terraformexec.BuildJob(request)
}

func sourceRootForPlan(object *platformv1alpha1.TerraformRun, workingDir string) string {
	if object.Spec.Source.Type == terraformexec.SourceGitCommit || object.Spec.Source.Type == terraformexec.SourceRetainedBundle {
		return "/workspace/terraform"
	}
	return ""
}

func (r *TerraformRunReconciler) reconcileJobStatus(ctx context.Context, object *platformv1alpha1.TerraformRun, job *batchv1.Job) (ctrl.Result, error) {
	if job.Status.Succeeded > 0 || job.Status.Failed > 0 {
		terminal, err := r.captureTerminalResult(ctx, object, job)
		if err == nil {
			return r.applyTerminalResult(ctx, object, terminal, job)
		}
		return r.applyMissingTerminalResult(ctx, object, err)
	}
	if job.Status.Active > 0 {
		return r.updateExecutionStatus(ctx, object, "Running", metav1.ConditionFalse, "TerraformJobRunning", "Terraform Job is running.")
	}
	return r.updateExecutionStatus(ctx, object, "Pending", metav1.ConditionFalse, "TerraformJobPending", "Terraform Job is pending.")
}

func (r *TerraformRunReconciler) captureTerminalResult(ctx context.Context, object *platformv1alpha1.TerraformRun, job *batchv1.Job) (runnerTerminalResult, error) {
	if r.ArtifactRegion == "" || r.ArtifactBucket == "" {
		return runnerTerminalResult{}, fmt.Errorf("durable terminal artifact store region and bucket are not configured")
	}
	store, err := artifacts.NewS3StoreWithKMS(r.ArtifactEndpoint, r.ArtifactRegion, r.ArtifactBucket, r.ArtifactKMSKeyID)
	if err != nil {
		return runnerTerminalResult{}, err
	}
	key := path.Join("runs", string(object.UID), "terminal-result.json")
	ref, content, err := store.GetVerifiedByKey(ctx, key)
	if err != nil {
		return runnerTerminalResult{}, fmt.Errorf("read durable terminal result %q: %w", key, err)
	}
	if ref.Digest == "" {
		return runnerTerminalResult{}, fmt.Errorf("durable terminal result %q has no store digest metadata", key)
	}
	var result runnerTerminalResult
	if err := json.Unmarshal(content, &result); err != nil {
		return runnerTerminalResult{}, fmt.Errorf("decode durable terminal result: %w", err)
	}
	if result.Operation == "" || result.ExecutionOutcome == "" {
		return runnerTerminalResult{}, fmt.Errorf("durable terminal result is incomplete")
	}
	if result.RunUID != "" && object.UID != "" && result.RunUID != string(object.UID) {
		return runnerTerminalResult{}, fmt.Errorf("durable terminal result TerraformRun UID mismatch")
	}
	if result.JobUID != "" && job != nil && job.UID != "" && result.JobUID != string(job.UID) {
		return runnerTerminalResult{}, fmt.Errorf("durable terminal result Job UID mismatch")
	}
	result.TerminalResultRef = ref.Key
	result.TerminalResultDigest = ref.Digest
	return result, nil
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

func (r *TerraformRunReconciler) applyTerminalResult(ctx context.Context, object *platformv1alpha1.TerraformRun, result runnerTerminalResult, job *batchv1.Job) (ctrl.Result, error) {
	object.Status.EvidenceCaptured = true
	object.Status.ArtifactsReady = result.ArtifactsReady
	object.Status.HasChanges = result.HasChanges
	object.Status.TerminalResultRef = result.TerminalResultRef
	object.Status.TerminalResultDigest = result.TerminalResultDigest
	object.Status.MutationClassification = result.MutationClassification
	object.Status.MutationMayHaveOccurred = result.MutationMayHaveOccurred
	if result.PlanReportRef != "" {
		object.Status.PlanReportRef = result.PlanReportRef
		object.Status.PlanReportDigest = result.PlanReportDigest
	}
	if result.EffectivePlanInputDigest != "" {
		object.Status.EffectivePlanInputDigest = result.EffectivePlanInputDigest
	} else {
		object.Status.EffectivePlanInputDigest = effectiveRunInputDigest(object, result)
	}
	object.Status.TargetDiscoveryRef = result.TargetDiscoveryRef
	object.Status.TargetDiscoveryDigest = result.TargetDiscoveryDigest
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
	if !result.StartedAt.IsZero() {
		started := metav1.NewTime(result.StartedAt)
		object.Status.TerminalStartedAt = &started
	}
	if !result.FinishedAt.IsZero() {
		finished := metav1.NewTime(result.FinishedAt)
		object.Status.TerminalFinishedAt = &finished
	} else if job != nil && job.Status.CompletionTime != nil {
		finished := *job.Status.CompletionTime
		object.Status.TerminalFinishedAt = &finished
	}
	if object.Spec.Operation == "Plan" && (result.ExecutionOutcome == "NoChange" || result.ExecutionOutcome == "ChangesPresent") && object.Status.PlanCreatedAt == nil {
		if object.Status.TerminalFinishedAt == nil {
			return r.applyMissingTerminalResult(ctx, object, fmt.Errorf("trusted terminal finishedAt is required for Plan expiry"))
		}
		created := *object.Status.TerminalFinishedAt
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
	observability.TerraformExecutions.WithLabelValues(object.Spec.Operation, outcome).Inc()
	if outcome == "Indeterminate" {
		observability.RecoveryEvents.WithLabelValues(reason).Inc()
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

func terraformSecretVariables(values []platformv1alpha1.SecretVariableReference) []terraformexec.VariableSecretReference {
	result := make([]terraformexec.VariableSecretReference, 0, len(values))
	for _, value := range values {
		result = append(result, terraformexec.VariableSecretReference{Variable: value.Variable, Name: value.SecretKeyRef.Name, Key: value.SecretKeyRef.Key})
	}
	return result
}

func effectiveRunInputDigest(object *platformv1alpha1.TerraformRun, result runnerTerminalResult) string {
	if object == nil {
		return ""
	}
	digest, err := terraformexec.Digest(struct {
		SpecDigest         string
		SourceBundleDigest string
		BackendDigest      string
		PlanDigest         string
		PlanReportDigest   string
		TerraformVersion   string
		RunnerImageDigest  string
		TargetIdentity     string
		PlatformIdentity   string
		RunnerIdentity     string
	}{
		SpecDigest:         object.Spec.EffectivePlanInputDigest,
		SourceBundleDigest: result.SourceBundleDigest,
		BackendDigest:      result.BackendConfigDigest,
		PlanDigest:         result.PlanDigest,
		PlanReportDigest:   result.PlanReportDigest,
		TerraformVersion:   object.Spec.ExpectedTerraformVersion,
		RunnerImageDigest:  object.Spec.RunnerImageDigest,
		TargetIdentity:     object.Spec.RuntimeTargetIdentityDigest,
		PlatformIdentity:   object.Spec.ExecutionPlatformIdentityDigest,
		RunnerIdentity:     object.Spec.RunnerServiceAccountIdentityDigest,
	})
	if err != nil {
		return ""
	}
	return digest
}
