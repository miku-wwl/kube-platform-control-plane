package controller

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	platformv1alpha1 "github.com/miku-wwl/kube-platform-control-plane/api/v1alpha1"
	terraformexec "github.com/miku-wwl/kube-platform-control-plane/internal/terraform"
)

const (
	infraStackFinalizer = "platform.example.io/infrastack-finalizer"
	planValidity        = time.Hour
	lifecycleRequeue    = time.Second
)

// +kubebuilder:rbac:groups=platform.example.io,resources=infrastacks,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=platform.example.io,resources=infrastacks/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=platform.example.io,resources=infrastacks/finalizers,verbs=update
// +kubebuilder:rbac:groups=platform.example.io,resources=terraformruns,verbs=get;list;watch;create;delete;update;patch
// +kubebuilder:rbac:groups=platform.example.io,resources=changeapprovals,verbs=get;list;watch
type InfraStackReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

func (r *InfraStackReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var object platformv1alpha1.InfraStack
	if err := r.Get(ctx, req.NamespacedName, &object); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	if object.DeletionTimestamp.IsZero() && !containsString(object.Finalizers, infraStackFinalizer) {
		object.Finalizers = append(object.Finalizers, infraStackFinalizer)
		if err := r.Update(ctx, &object); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{Requeue: true}, nil
	}

	destroy := object.Spec.DesiredState == platformv1alpha1.DesiredStateDestroy || !object.DeletionTimestamp.IsZero()
	if destroy {
		return r.reconcileDestroy(ctx, &object)
	}
	return r.reconcilePresent(ctx, &object)
}

func (r *InfraStackReconciler) reconcilePresent(ctx context.Context, object *platformv1alpha1.InfraStack) (ctrl.Result, error) {
	child, err := r.ensurePlanRun(ctx, object, false)
	if err != nil {
		return ctrl.Result{}, err
	}
	return r.reconcilePlanResult(ctx, object, child, false)
}

func (r *InfraStackReconciler) reconcileDestroy(ctx context.Context, object *platformv1alpha1.InfraStack) (ctrl.Result, error) {
	if condition := findCondition(object.Status.Conditions, ConditionReady); condition != nil && condition.Status == metav1.ConditionTrue && condition.Reason == "InfrastructureRemoved" {
		if !object.DeletionTimestamp.IsZero() && containsString(object.Finalizers, infraStackFinalizer) {
			object.Finalizers = removeString(object.Finalizers, infraStackFinalizer)
			if err := r.Update(ctx, object); err != nil && !apierrors.IsNotFound(err) {
				return ctrl.Result{}, err
			}
		}
		return ctrl.Result{}, nil
	}
	var applied platformv1alpha1.TerraformRun
	if object.Status.LastAppliedRunRef == nil || object.Status.LastAppliedRunRef.Name == "" {
		return r.setStackCondition(ctx, object, lifecycleCondition(object.Generation, metav1.ConditionFalse, "RecoveryRequired", "Destroy requires the retained source bundle from the last successful Apply."), false)
	}
	if err := r.Get(ctx, types.NamespacedName{Name: object.Status.LastAppliedRunRef.Name, Namespace: object.Namespace}, &applied); err != nil {
		if apierrors.IsNotFound(err) {
			return r.setStackCondition(ctx, object, lifecycleCondition(object.Generation, metav1.ConditionFalse, "RecoveryRequired", "The last successful Apply record is unavailable; refusing to guess a Destroy source."), false)
		}
		return ctrl.Result{}, err
	}
	if sourceRef, sourceDigest := retainedBundle(&applied); sourceRef == "" || sourceDigest == "" {
		return r.setStackCondition(ctx, object, lifecycleCondition(object.Generation, metav1.ConditionFalse, "RecoveryRequired", "The last successful Apply has no retained source bundle; refusing to guess a Destroy source."), false)
	}

	child, err := r.ensurePlanRun(ctx, object, true, &applied)
	if err != nil {
		return ctrl.Result{}, err
	}
	result, err := r.reconcilePlanResult(ctx, object, child, true)
	if err != nil || result.Requeue || result.RequeueAfter > 0 {
		return result, err
	}
	condition := findCondition(object.Status.Conditions, ConditionReady)
	if condition == nil || condition.Status != metav1.ConditionTrue || condition.Reason != "InfrastructureRemoved" {
		return ctrl.Result{RequeueAfter: lifecycleRequeue}, nil
	}
	if !object.DeletionTimestamp.IsZero() && containsString(object.Finalizers, infraStackFinalizer) {
		object.Finalizers = removeString(object.Finalizers, infraStackFinalizer)
		if err := r.Update(ctx, object); err != nil && !apierrors.IsNotFound(err) {
			return ctrl.Result{}, err
		}
	}
	return ctrl.Result{}, nil
}

func (r *InfraStackReconciler) ensurePlanRun(ctx context.Context, stack *platformv1alpha1.InfraStack, destroy bool, applied ...*platformv1alpha1.TerraformRun) (*platformv1alpha1.TerraformRun, error) {
	name := planRunName(stack.Name, stack.Generation)
	if destroy {
		name = destroyPlanRunName(stack.Name, stack.Generation)
	}
	var child platformv1alpha1.TerraformRun
	err := r.Get(ctx, client.ObjectKey{Name: name, Namespace: stack.Namespace}, &child)
	if apierrors.IsNotFound(err) {
		if destroy && len(applied) != 1 {
			return nil, fmt.Errorf("destroy PlanRun requires the last successful Apply")
		}
		if destroy {
			child = *buildDestroyPlanRun(stack, applied[0], name)
		} else {
			child = *buildPlanRun(stack, name)
		}
		if err := ctrl.SetControllerReference(stack, &child, r.Scheme); err != nil {
			return nil, err
		}
		if err := r.Create(ctx, &child); err != nil {
			return nil, err
		}
	} else if err != nil {
		return nil, err
	}
	return &child, nil
}

func (r *InfraStackReconciler) reconcilePlanResult(ctx context.Context, stack *platformv1alpha1.InfraStack, plan *platformv1alpha1.TerraformRun, destroy bool) (ctrl.Result, error) {
	status := stack.Status
	status.ObservedGeneration = stack.Generation
	status.LatestPlanRunRef = &corev1.LocalObjectReference{Name: plan.Name}

	condition := lifecycleCondition(stack.Generation, metav1.ConditionFalse, "PlanPending", "Terraform PlanRun is pending.")
	switch plan.Status.ExecutionOutcome {
	case "NoChange":
		if destroy {
			condition = lifecycleCondition(stack.Generation, metav1.ConditionTrue, "InfrastructureRemoved", "Terraform Destroy plan has no changes; infrastructure is already absent.")
		} else {
			condition = lifecycleCondition(stack.Generation, metav1.ConditionTrue, "InfrastructureReady", "Terraform plan has no changes; infrastructure is ready.")
		}
	case "ChangesPresent":
		condition = lifecycleCondition(stack.Generation, metav1.ConditionFalse, "WaitingApproval", "Terraform plan contains changes and is waiting for a valid ChangeApproval.")
		approval, found, err := r.validApprovalForPlan(ctx, plan)
		if err != nil {
			return ctrl.Result{}, err
		}
		if found {
			if plan.Status.PlanDigest == "" || plan.Status.SourceBundleRef == "" || plan.Status.SourceBundleDigest == "" {
				condition = lifecycleCondition(stack.Generation, metav1.ConditionFalse, "PlanArtifactsPending", "A valid approval exists, but immutable plan/source artifacts are not ready.")
			} else {
				apply, applyErr := r.ensureApplyRun(ctx, stack, plan, approval)
				if applyErr != nil {
					condition = lifecycleCondition(stack.Generation, metav1.ConditionFalse, "ApplyBlocked", applyErr.Error())
				} else {
					condition = applyCondition(stack.Generation, apply, destroy)
					if !destroy && apply.Status.ExecutionOutcome == "Succeeded" {
						status.LastAppliedRunRef = &corev1.LocalObjectReference{Name: apply.Name}
						status.LastAppliedSourceBundleRef = apply.Status.SourceBundleRef
						status.LastAppliedSourceBundleDigest = apply.Status.SourceBundleDigest
						if status.LastAppliedSourceBundleRef == "" && apply.Spec.Source.Type == terraformexec.SourceRetainedBundle {
							status.LastAppliedSourceBundleRef = apply.Spec.Source.Ref
							status.LastAppliedSourceBundleDigest = apply.Spec.Source.Digest
						}
					}
				}
			}
		}
	case "Failed":
		condition = lifecycleCondition(stack.Generation, metav1.ConditionFalse, "PlanFailed", "Terraform PlanRun failed.")
	case "Indeterminate":
		condition = lifecycleCondition(stack.Generation, metav1.ConditionUnknown, "PlanIndeterminate", "Terraform PlanRun ended without a trusted terminal result.")
	default:
		if plan.Status.JobRef != nil {
			condition = lifecycleCondition(stack.Generation, metav1.ConditionFalse, "PlanRunning", "Terraform PlanRun is executing.")
		}
	}

	status.Conditions = []metav1.Condition{stableCondition(findCondition(stack.Status.Conditions, ConditionReady), stack.Generation, condition.Status, condition.Reason, condition.Message)}
	if err := r.updateStackStatus(ctx, stack, status); err != nil {
		return ctrl.Result{}, err
	}
	if condition.Status != metav1.ConditionTrue {
		return ctrl.Result{RequeueAfter: lifecycleRequeue}, nil
	}
	return ctrl.Result{}, nil
}

func (r *InfraStackReconciler) validApprovalForPlan(ctx context.Context, plan *platformv1alpha1.TerraformRun) (*platformv1alpha1.ChangeApproval, bool, error) {
	var approvals platformv1alpha1.ChangeApprovalList
	if err := r.List(ctx, &approvals, client.InNamespace(plan.Namespace)); err != nil {
		return nil, false, err
	}
	now := time.Now()
	for index := range approvals.Items {
		approval := &approvals.Items[index]
		if approval.Spec.PlanRunRef.Name != plan.Name || approval.Spec.PlanRunUID != string(plan.UID) {
			continue
		}
		if approval.Spec.PlanDigest != plan.Status.PlanDigest || approval.Spec.ExecutionContextDigest != plan.Spec.ExecutionContextDigest {
			continue
		}
		if plan.Status.PlanExpiresAt == nil || !now.Before(plan.Status.PlanExpiresAt.Time) {
			continue
		}
		return approval, true, nil
	}
	return nil, false, nil
}

func (r *InfraStackReconciler) ensureApplyRun(ctx context.Context, stack *platformv1alpha1.InfraStack, plan *platformv1alpha1.TerraformRun, approval *platformv1alpha1.ChangeApproval) (*platformv1alpha1.TerraformRun, error) {
	var runs platformv1alpha1.TerraformRunList
	if err := r.List(ctx, &runs, client.InNamespace(stack.Namespace)); err != nil {
		return nil, err
	}
	for index := range runs.Items {
		candidate := &runs.Items[index]
		if candidate.Spec.Operation == "Apply" && candidate.Spec.PlanRunUID == string(plan.UID) && candidate.Spec.ApprovalUID == string(approval.UID) {
			return candidate, nil
		}
	}
	apply := buildApplyRun(stack, plan, approval, applyRunName(plan.Name, string(approval.UID)))
	if err := ctrl.SetControllerReference(stack, apply, r.Scheme); err != nil {
		return nil, err
	}
	if err := r.Create(ctx, apply); err != nil {
		if apierrors.IsAlreadyExists(err) {
			var existing platformv1alpha1.TerraformRun
			if getErr := r.Get(ctx, client.ObjectKey{Name: apply.Name, Namespace: apply.Namespace}, &existing); getErr == nil {
				return &existing, nil
			}
		}
		return nil, err
	}
	return apply, nil
}

func (r *InfraStackReconciler) SetupWithManager(manager ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(manager).For(&platformv1alpha1.InfraStack{}).Owns(&platformv1alpha1.TerraformRun{}).Complete(r)
}

func (r *InfraStackReconciler) updateStackStatus(ctx context.Context, object *platformv1alpha1.InfraStack, status platformv1alpha1.InfraStackStatus) error {
	if reflect.DeepEqual(object.Status, status) {
		return nil
	}
	object.Status = status
	return r.Status().Update(ctx, object)
}

func (r *InfraStackReconciler) setStackCondition(ctx context.Context, object *platformv1alpha1.InfraStack, condition metav1.Condition, removeFinalizer bool) (ctrl.Result, error) {
	status := object.Status
	status.ObservedGeneration = object.Generation
	status.Conditions = []metav1.Condition{stableCondition(findCondition(object.Status.Conditions, ConditionReady), object.Generation, condition.Status, condition.Reason, condition.Message)}
	if err := r.updateStackStatus(ctx, object, status); err != nil {
		return ctrl.Result{}, err
	}
	if removeFinalizer && condition.Status == metav1.ConditionTrue && containsString(object.Finalizers, infraStackFinalizer) {
		object.Finalizers = removeString(object.Finalizers, infraStackFinalizer)
		if err := r.Update(ctx, object); err != nil && !apierrors.IsNotFound(err) {
			return ctrl.Result{}, err
		}
	}
	return ctrl.Result{}, nil
}

func applyCondition(generation int64, apply *platformv1alpha1.TerraformRun, destroy bool) metav1.Condition {
	switch apply.Status.ExecutionOutcome {
	case "Succeeded":
		if destroy {
			return lifecycleCondition(generation, metav1.ConditionTrue, "InfrastructureRemoved", "Terraform Destroy Apply completed successfully.")
		}
		return lifecycleCondition(generation, metav1.ConditionTrue, "InfrastructureReady", "Terraform Apply completed successfully.")
	case "Indeterminate":
		return lifecycleCondition(generation, metav1.ConditionUnknown, "ApplyIndeterminate", "Terraform Apply ended without a trusted terminal result.")
	case "Failed", "Rejected":
		return lifecycleCondition(generation, metav1.ConditionFalse, "ApplyFailed", "Terraform Apply failed.")
	default:
		return lifecycleCondition(generation, metav1.ConditionFalse, "Applying", "Terraform ApplyRun is executing.")
	}
}

func buildPlanRun(stack *platformv1alpha1.InfraStack, name string) *platformv1alpha1.TerraformRun {
	return &platformv1alpha1.TerraformRun{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: stack.Namespace},
		Spec: platformv1alpha1.TerraformRunSpec{
			StackRef:             corev1.LocalObjectReference{Name: stack.Name},
			InfraStackGeneration: stack.Generation,
			Operation:            "Plan",
			PlanMode:             "Reconcile",
			Source: platformv1alpha1.PlanRunSourceSpec{
				Type:     terraformexec.SourceGitCommit,
				URL:      stack.Spec.Source.URL,
				Revision: stack.Spec.Source.Revision,
				Path:     stack.Spec.Source.Path,
			},
			ResolvedBackendConfigRef: stack.Spec.Backend.ConfigRef.Name,
			LockTimeout:              stack.Spec.Backend.LockTimeout,
			VariableSecretRefs:       stack.Spec.Variables.SecretRefs,
			Workspace:                stack.Spec.Workspace,
			Executor:                 stack.Spec.Executor,
			ExecutionContextDigest:   executionContextDigest(stack.Spec),
		},
	}
}

func buildDestroyPlanRun(stack *platformv1alpha1.InfraStack, applied *platformv1alpha1.TerraformRun, name string) *platformv1alpha1.TerraformRun {
	sourceRef, sourceDigest := retainedBundle(applied)
	backendRef := applied.Status.ResolvedBackendConfigRef
	backendDigest := applied.Status.ResolvedBackendConfigDigest
	if backendRef == "" {
		backendRef = applied.Spec.BackendConfigArtifactRef
		backendDigest = applied.Spec.BackendConfigArtifactDigest
	}
	return &platformv1alpha1.TerraformRun{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: stack.Namespace},
		Spec: platformv1alpha1.TerraformRunSpec{
			StackRef:                        corev1.LocalObjectReference{Name: stack.Name},
			InfraStackGeneration:            stack.Generation,
			Operation:                       "Plan",
			PlanMode:                        "Destroy",
			Source:                          platformv1alpha1.PlanRunSourceSpec{Type: terraformexec.SourceRetainedBundle, Ref: sourceRef, Digest: sourceDigest},
			ResolvedBackendConfigRef:        applied.Spec.ResolvedBackendConfigRef,
			BackendConfigArtifactRef:        backendRef,
			BackendConfigArtifactDigest:     backendDigest,
			VariableSecretRefs:              applied.Spec.VariableSecretRefs,
			LockTimeout:                     applied.Spec.LockTimeout,
			Workspace:                       applied.Spec.Workspace,
			Executor:                        applied.Spec.Executor,
			ExecutionContextDigest:          applied.Spec.ExecutionContextDigest,
			ExecutionTargetIdentityDigest:   applied.Spec.ExecutionTargetIdentityDigest,
			ExecutionPlatformIdentityDigest: applied.Spec.ExecutionPlatformIdentityDigest,
		},
	}
}

func retainedBundle(applied *platformv1alpha1.TerraformRun) (string, string) {
	if applied == nil {
		return "", ""
	}
	if applied.Status.SourceBundleRef != "" && applied.Status.SourceBundleDigest != "" {
		return applied.Status.SourceBundleRef, applied.Status.SourceBundleDigest
	}
	if applied.Spec.Source.Type == terraformexec.SourceRetainedBundle {
		return applied.Spec.Source.Ref, applied.Spec.Source.Digest
	}
	return "", ""
}

func buildApplyRun(stack *platformv1alpha1.InfraStack, plan *platformv1alpha1.TerraformRun, approval *platformv1alpha1.ChangeApproval, name string) *platformv1alpha1.TerraformRun {
	sourceRef, sourceDigest := plan.Status.SourceBundleRef, plan.Status.SourceBundleDigest
	if sourceRef == "" || sourceDigest == "" {
		sourceRef, sourceDigest = retainedBundle(plan)
	}
	backendRef := plan.Status.ResolvedBackendConfigRef
	backendDigest := plan.Status.ResolvedBackendConfigDigest
	if backendRef == "" {
		backendRef = plan.Spec.BackendConfigArtifactRef
		backendDigest = plan.Spec.BackendConfigArtifactDigest
	}
	return &platformv1alpha1.TerraformRun{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: stack.Namespace, Labels: map[string]string{
			"platform.example.io/terraform-plan":         plan.Name,
			"platform.example.io/terraform-plan-uid":     string(plan.UID),
			"platform.example.io/terraform-approval-uid": string(approval.UID),
		}},
		Spec: platformv1alpha1.TerraformRunSpec{
			StackRef:                        corev1.LocalObjectReference{Name: stack.Name},
			InfraStackGeneration:            stack.Generation,
			Operation:                       "Apply",
			PlanMode:                        plan.Spec.PlanMode,
			Source:                          platformv1alpha1.PlanRunSourceSpec{Type: terraformexec.SourceRetainedBundle, Ref: sourceRef, Digest: sourceDigest},
			ResolvedBackendConfigRef:        plan.Spec.ResolvedBackendConfigRef,
			BackendConfigArtifactRef:        backendRef,
			BackendConfigArtifactDigest:     backendDigest,
			VariableSecretRefs:              plan.Spec.VariableSecretRefs,
			LockTimeout:                     plan.Spec.LockTimeout,
			Workspace:                       plan.Spec.Workspace,
			Executor:                        plan.Spec.Executor,
			ExecutionContextDigest:          plan.Spec.ExecutionContextDigest,
			ExecutionTargetIdentityDigest:   plan.Spec.ExecutionTargetIdentityDigest,
			ExecutionPlatformIdentityDigest: plan.Spec.ExecutionPlatformIdentityDigest,
			PlanRunUID:                      string(plan.UID),
			PlanRef:                         plan.Status.PlanRef,
			PlanDigest:                      plan.Status.PlanDigest,
			ApprovalRef:                     &corev1.LocalObjectReference{Name: approval.Name},
			ApprovalUID:                     string(approval.UID),
		},
	}
}

func executionContextDigest(spec platformv1alpha1.InfraStackSpec) string {
	encoded, _ := json.Marshal(struct {
		Source    platformv1alpha1.SourceSpec    `json:"source"`
		Backend   platformv1alpha1.BackendSpec   `json:"backend"`
		Workspace string                         `json:"workspace"`
		Variables platformv1alpha1.VariablesSpec `json:"variables"`
		Executor  platformv1alpha1.ExecutorSpec  `json:"executor"`
	}{Source: spec.Source, Backend: spec.Backend, Workspace: spec.Workspace, Variables: spec.Variables, Executor: spec.Executor})
	hash := sha256.Sum256(encoded)
	return "sha256:" + hex.EncodeToString(hash[:])
}

func planRunName(stackName string, generation int64) string {
	name := fmt.Sprintf("%s-plan-%d", stackName, generation)
	if len(name) > 63 {
		name = name[:63]
	}
	return strings.TrimRight(name, "-")
}

func destroyPlanRunName(stackName string, generation int64) string {
	name := fmt.Sprintf("%s-destroy-plan-%d", stackName, generation)
	if len(name) > 63 {
		name = name[:63]
	}
	return strings.TrimRight(name, "-")
}

func applyRunName(planName, approvalUID string) string {
	hash := sha256.Sum256([]byte(approvalUID))
	name := fmt.Sprintf("%s-apply-%s", planName, hex.EncodeToString(hash[:])[:10])
	if len(name) > 63 {
		name = name[:63]
	}
	return strings.TrimRight(name, "-")
}

func lifecycleCondition(generation int64, status metav1.ConditionStatus, reason, message string) metav1.Condition {
	return metav1.Condition{Type: ConditionReady, Status: status, ObservedGeneration: generation, Reason: reason, Message: message, LastTransitionTime: metav1.Now()}
}

func sameCondition(left, right *metav1.Condition) bool {
	if left == nil || right == nil {
		return left == right
	}
	return left.Type == right.Type && left.Status == right.Status && left.ObservedGeneration == right.ObservedGeneration && left.Reason == right.Reason && left.Message == right.Message
}
