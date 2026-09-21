package controller

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"reflect"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	platformv1alpha1 "github.com/miku-wwl/kube-platform-control-plane/api/v1alpha1"
	"github.com/miku-wwl/kube-platform-control-plane/internal/artifacts"
	targetresolver "github.com/miku-wwl/kube-platform-control-plane/internal/target"
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
	Scheme           *runtime.Scheme
	ArtifactEndpoint string
	ArtifactRegion   string
	ArtifactBucket   string
	TargetVerifier   targetresolver.TargetVerifier
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
		if !object.Spec.MutationFence {
			object.Spec.MutationFence = true
			if err := r.Update(ctx, &object); err != nil {
				return ctrl.Result{}, err
			}
			return ctrl.Result{RequeueAfter: lifecycleRequeue}, nil
		}
		return r.reconcileDestroy(ctx, &object)
	}
	if object.Spec.MutationFence {
		return r.setStackCondition(ctx, &object, lifecycleCondition(object.Generation, metav1.ConditionFalse, "MutationFence", "Infrastructure mutation is closed by the durable deletion fence."), false)
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
		return r.setStackCondition(ctx, object, *condition, true)
	}
	var runs platformv1alpha1.TerraformRunList
	if err := r.List(ctx, &runs, client.InNamespace(object.Namespace)); err != nil {
		return ctrl.Result{}, err
	}
	for index := range runs.Items {
		run := &runs.Items[index]
		if run.Spec.StackRef.Name != object.Name || run.Spec.Operation != "Apply" {
			continue
		}
		if run.Status.ExecutionOutcome == "Indeterminate" || (run.Status.ExecutionOutcome == "Failed" && run.Status.MutationMayHaveOccurred) {
			return r.setStackCondition(ctx, object, lifecycleCondition(object.Generation, metav1.ConditionUnknown, "RecoveryRequired", "An active Apply is indeterminate; destroy is blocked until fresh observation and Plan."), false)
		}
		if run.Status.ExecutionOutcome != "Succeeded" && run.Status.ExecutionOutcome != "Failed" && run.Status.ExecutionOutcome != "Rejected" {
			return r.setStackCondition(ctx, object, lifecycleCondition(object.Generation, metav1.ConditionFalse, "DeletionHeld", "Destroy is waiting for the active Apply to reach a terminal classification."), false)
		}
		if run.Status.ExecutionOutcome == "Failed" && run.Status.MutationClassification != "FailedPreMutation" {
			return r.setStackCondition(ctx, object, lifecycleCondition(object.Generation, metav1.ConditionUnknown, "RecoveryRequired", "A failed Apply may have changed infrastructure; refusing blind Destroy."), false)
		}
	}
	var applied platformv1alpha1.TerraformRun
	if object.Status.LastConverged == nil {
		if object.Status.LastAppliedRunRef == nil || object.Status.LastAppliedRunRef.Name == "" {
			for index := range runs.Items {
				run := &runs.Items[index]
				if run.Spec.StackRef.Name == object.Name && run.Spec.Operation == "Apply" {
					return r.setStackCondition(ctx, object, lifecycleCondition(object.Generation, metav1.ConditionFalse, "RecoveryRequired", "Destroy requires evidence for an Apply that may have changed infrastructure."), false)
				}
			}
			if condition := findCondition(object.Status.Conditions, ConditionReady); condition == nil || condition.Reason != "InfrastructureRemoved" {
				if _, err := r.setStackCondition(ctx, object, lifecycleCondition(object.Generation, metav1.ConditionTrue, "InfrastructureRemoved", "No successful Apply exists; Terraform infrastructure was never admitted."), true); err != nil {
					return ctrl.Result{}, err
				}
			}
			return ctrl.Result{}, nil
		}
		return r.setStackCondition(ctx, object, lifecycleCondition(object.Generation, metav1.ConditionFalse, "RecoveryRequired", "The last successful convergence closure is unavailable; refusing to guess a Destroy source."), false)
	}
	applied = *retainedRunFromClosure(object, object.Status.LastConverged)

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
		return r.setStackCondition(ctx, object, *condition, true)
	}
	return ctrl.Result{}, nil
}

func (r *InfraStackReconciler) ensurePlanRun(ctx context.Context, stack *platformv1alpha1.InfraStack, destroy bool, applied ...*platformv1alpha1.TerraformRun) (*platformv1alpha1.TerraformRun, error) {
	var runs platformv1alpha1.TerraformRunList
	if err := r.List(ctx, &runs, client.InNamespace(stack.Namespace)); err != nil {
		return nil, err
	}
	planMode := "Reconcile"
	if destroy {
		planMode = "Destroy"
	}
	var candidate *platformv1alpha1.TerraformRun
	for index := range runs.Items {
		item := &runs.Items[index]
		if item.Spec.StackRef.Name != stack.Name || item.Spec.Operation != "Plan" || item.Spec.PlanMode != planMode || item.Spec.InfraStackGeneration != stack.Generation {
			continue
		}
		if hasForeignController(item, stack, "InfraStack") {
			return nil, fmt.Errorf("TerraformRun %q is owned by another InfraStack", item.Name)
		}
		if candidate == nil || item.CreationTimestamp.After(candidate.CreationTimestamp.Time) {
			candidate = item
		}
	}
	if candidate != nil && !planAttemptStale(candidate) {
		if stack.UID != "" && !ownedBy(candidate, stack, "InfraStack") {
			if err := ctrl.SetControllerReference(stack, candidate, r.Scheme); err != nil {
				return nil, err
			}
			if err := r.Update(ctx, candidate); err != nil {
				return nil, err
			}
		}
		return candidate, nil
	}
	if candidate != nil {
		candidate.Status.ExecutionOutcome = "Superseded"
		candidate.Status.ObservedGeneration = candidate.Generation
		candidate.Status.Conditions = []metav1.Condition{executionCondition(candidate.Generation, metav1.ConditionFalse, "Superseded", "A stale or failed Plan attempt was superseded by a fresh immutable attempt.")}
		if err := r.Status().Update(ctx, candidate); err != nil {
			return nil, err
		}
	}
	if destroy && len(applied) != 1 {
		return nil, fmt.Errorf("destroy PlanRun requires the last successful convergence closure")
	}
	name := planRunName(stack.Name, stack.Generation)
	if destroy {
		name = destroyPlanRunName(stack.Name, stack.Generation)
	}
	if candidate != nil {
		name = planAttemptName(stack.Name, stack.Generation, destroy)
	}
	var child platformv1alpha1.TerraformRun
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
	return &child, nil
}

func planAttemptStale(run *platformv1alpha1.TerraformRun) bool {
	if run == nil {
		return true
	}
	switch run.Status.ExecutionOutcome {
	case "Failed", "Indeterminate", "Rejected", "Expired", "Superseded":
		return true
	}
	return run.Status.PlanExpiresAt != nil && !time.Now().Before(run.Status.PlanExpiresAt.Time)
}

func planAttemptName(stackName string, generation int64, destroy bool) string {
	suffix := "plan"
	if destroy {
		suffix = "destroy-plan"
	}
	return boundedResourceName(fmt.Sprintf("%s-%s-%d-attempt-%d", stackName, suffix, generation, time.Now().UnixNano()))
}

func (r *InfraStackReconciler) reconcilePlanResult(ctx context.Context, stack *platformv1alpha1.InfraStack, plan *platformv1alpha1.TerraformRun, destroy bool) (ctrl.Result, error) {
	status := stack.Status
	status.ObservedGeneration = stack.Generation
	if stack.Status.ObservedGeneration != stack.Generation {
		status.DiscoveredRuntimeTargetIdentity = nil
		status.DiscoveredTargetConnectionProfile = nil
		status.TargetDiscoveryRef = ""
		status.TargetDiscoveryDigest = ""
	}
	status.LatestPlanRunRef = &corev1.LocalObjectReference{Name: plan.Name}

	condition := lifecycleCondition(stack.Generation, metav1.ConditionFalse, "PlanPending", "Terraform PlanRun is pending.")
	switch plan.Status.ExecutionOutcome {
	case "NoChange":
		if destroy {
			if !runEvidenceReady(plan) {
				condition = lifecycleCondition(stack.Generation, metav1.ConditionFalse, "DestroyEvidencePending", "Terraform Destroy Plan is terminal, but durable evidence is incomplete.")
			} else {
				condition = lifecycleCondition(stack.Generation, metav1.ConditionTrue, "InfrastructureRemoved", "Terraform Destroy plan has no changes; infrastructure is already absent.")
			}
		} else {
			discovery, discoveryErr := r.verifyTargetDiscovery(ctx, stack, plan)
			closure, ok := convergenceClosure(stack, plan, plan)
			if discoveryErr != nil {
				ok = false
				condition = lifecycleCondition(stack.Generation, metav1.ConditionFalse, "TargetDiscoveryRejected", discoveryErr.Error())
			}
			if !ok {
				if discoveryErr == nil {
					condition = lifecycleCondition(stack.Generation, metav1.ConditionFalse, "ConvergenceEvidencePending", "Terraform reported NoChange, but durable source/backend/terminal evidence is incomplete.")
				}
			} else {
				status.LastConverged = closure
				status.ConvergenceEvidenceRef = plan.Status.TerminalResultRef
				status.ConvergenceEvidenceDigest = plan.Status.TerminalResultDigest
				status.TargetDiscoveryRef = plan.Status.TargetDiscoveryRef
				status.TargetDiscoveryDigest = plan.Status.TargetDiscoveryDigest
				setDiscoveredTarget(&status, discovery)
				condition = lifecycleCondition(stack.Generation, metav1.ConditionTrue, "InfrastructureReady", "Terraform plan has no changes and durable convergence evidence is present.")
			}
		}
	case "ChangesPresent":
		condition = lifecycleCondition(stack.Generation, metav1.ConditionFalse, "WaitingApproval", "Terraform plan contains changes and is waiting for a valid ChangeApproval.")
		approval, found, err := r.validApprovalForPlan(ctx, plan)
		if err != nil {
			return ctrl.Result{}, err
		}
		if found {
			if plan.Status.PlanDigest == "" || plan.Status.SourceBundleRef == "" || plan.Status.SourceBundleDigest == "" || !planEvidenceReady(plan) {
				condition = lifecycleCondition(stack.Generation, metav1.ConditionFalse, "PlanArtifactsPending", "A valid approval exists, but immutable plan/source artifacts are not ready.")
			} else {
				apply, applyErr := r.ensureApplyRun(ctx, stack, plan, approval)
				if applyErr != nil {
					condition = lifecycleCondition(stack.Generation, metav1.ConditionFalse, "ApplyBlocked", applyErr.Error())
				} else {
					if err := validateApplyAdmission(stack, plan, approval); err != nil {
						condition = lifecycleCondition(stack.Generation, metav1.ConditionFalse, "ApplyBlocked", err.Error())
						break
					}
					if stack.Spec.MutationFence && !destroy {
						condition = lifecycleCondition(stack.Generation, metav1.ConditionFalse, "MutationFence", "Apply admission is closed by the durable mutation fence.")
						break
					}
					condition = applyCondition(stack.Generation, apply, destroy)
					if !destroy && apply.Status.ExecutionOutcome == "Succeeded" {
						status.LastAppliedRunRef = &corev1.LocalObjectReference{Name: apply.Name}
						status.LastAppliedSourceBundleRef = apply.Status.SourceBundleRef
						status.LastAppliedSourceBundleDigest = apply.Status.SourceBundleDigest
						if status.LastAppliedSourceBundleRef == "" && apply.Spec.Source.Type == terraformexec.SourceRetainedBundle {
							status.LastAppliedSourceBundleRef = apply.Spec.Source.Ref
							status.LastAppliedSourceBundleDigest = apply.Spec.Source.Digest
						}
						closure, ok := convergenceClosure(stack, plan, apply)
						discovery, discoveryErr := r.verifyTargetDiscovery(ctx, stack, apply)
						if discoveryErr != nil {
							ok = false
							condition = lifecycleCondition(stack.Generation, metav1.ConditionFalse, "TargetDiscoveryRejected", discoveryErr.Error())
						}
						if !ok {
							if condition.Reason != "TargetDiscoveryRejected" {
								condition = lifecycleCondition(stack.Generation, metav1.ConditionFalse, "ConvergenceEvidencePending", "Terraform Apply succeeded, but durable convergence evidence is incomplete.")
							}
						} else {
							status.LastConverged = closure
							status.ConvergenceEvidenceRef = apply.Status.TerminalResultRef
							status.ConvergenceEvidenceDigest = apply.Status.TerminalResultDigest
							status.TargetDiscoveryRef = apply.Status.TargetDiscoveryRef
							status.TargetDiscoveryDigest = apply.Status.TargetDiscoveryDigest
							setDiscoveredTarget(&status, discovery)
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
		if approval.Spec.PlanDigest != plan.Status.PlanDigest || approval.Spec.ExecutionContextDigest != plan.Spec.ExecutionContextDigest || approval.Spec.EffectivePlanInputDigest != plan.Status.EffectivePlanInputDigest || approval.Spec.PlanReportRef != plan.Status.PlanReportRef || approval.Spec.PlanReportDigest != plan.Status.PlanReportDigest {
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
	if err := validateApplyAdmission(stack, plan, approval); err != nil {
		return nil, err
	}
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

func validateApplyAdmission(stack *platformv1alpha1.InfraStack, plan *platformv1alpha1.TerraformRun, approval *platformv1alpha1.ChangeApproval) error {
	if stack == nil || plan == nil || approval == nil {
		return fmt.Errorf("stack, PlanRun, and ChangeApproval are required")
	}
	if stack.Spec.MutationFence && plan.Spec.PlanMode != "Destroy" {
		return fmt.Errorf("mutation fence is set")
	}
	if plan.UID == "" || approval.UID == "" || approval.Spec.PlanRunUID != string(plan.UID) {
		return fmt.Errorf("PlanRun/approval UID binding mismatch")
	}
	if plan.Status.PlanDigest == "" || approval.Spec.PlanDigest != plan.Status.PlanDigest {
		return fmt.Errorf("plan digest binding mismatch")
	}
	if approval.Spec.ExecutionContextDigest != plan.Spec.ExecutionContextDigest {
		return fmt.Errorf("execution context binding mismatch")
	}
	if plan.Status.PlanReportRef == "" || plan.Status.PlanReportDigest == "" || approval.Spec.PlanReportRef != plan.Status.PlanReportRef || approval.Spec.PlanReportDigest != plan.Status.PlanReportDigest {
		return fmt.Errorf("plan report binding mismatch")
	}
	if plan.Status.PlanExpiresAt == nil || !time.Now().Before(plan.Status.PlanExpiresAt.Time) {
		return fmt.Errorf("saved PlanRun is expired or has no trusted expiry")
	}
	if plan.Status.EffectivePlanInputDigest == "" || approval.Spec.EffectivePlanInputDigest != plan.Status.EffectivePlanInputDigest || plan.Spec.RuntimeTargetIdentityDigest == "" || plan.Spec.InfrastructureExecutionIdentityDigest == "" {
		return fmt.Errorf("PlanRun execution snapshot is incomplete")
	}
	return nil
}

func (r *InfraStackReconciler) verifyTargetDiscovery(ctx context.Context, stack *platformv1alpha1.InfraStack, run *platformv1alpha1.TerraformRun) (targetresolver.DiscoveryResult, error) {
	if run == nil || run.Status.TargetDiscoveryRef == "" || run.Status.TargetDiscoveryDigest == "" {
		return targetresolver.DiscoveryResult{}, fmt.Errorf("target discovery artifact is missing")
	}
	if r.ArtifactRegion == "" || r.ArtifactBucket == "" {
		return targetresolver.DiscoveryResult{}, fmt.Errorf("target discovery artifact store region and bucket are not configured")
	}
	store, err := artifacts.NewS3Store(r.ArtifactEndpoint, r.ArtifactRegion, r.ArtifactBucket)
	if err != nil {
		return targetresolver.DiscoveryResult{}, err
	}
	content, err := store.GetVerified(ctx, artifacts.Ref{Key: run.Status.TargetDiscoveryRef, Digest: run.Status.TargetDiscoveryDigest})
	if err != nil {
		return targetresolver.DiscoveryResult{}, fmt.Errorf("read target discovery artifact: %w", err)
	}
	expectedIdentity := targetresolver.RuntimeTargetIdentity{
		Provider: stack.Spec.RuntimeTargetIdentity.Provider, AccountID: stack.Spec.RuntimeTargetIdentity.AccountID,
		Region: stack.Spec.RuntimeTargetIdentity.Region, ClusterARN: stack.Spec.RuntimeTargetIdentity.ClusterARN,
		ClusterName: stack.Spec.RuntimeTargetIdentity.ClusterName, IncarnationID: stack.Spec.RuntimeTargetIdentity.IncarnationID,
	}
	expectedProfile := targetresolver.TargetConnectionProfile{
		Endpoint: stack.Spec.TargetConnectionProfile.Endpoint, CACertificateData: stack.Spec.TargetConnectionProfile.CACertificateData, CACertificateDigest: stack.Spec.TargetConnectionProfile.CACertificateDigest,
		AuthMode: stack.Spec.TargetConnectionProfile.AuthMode, NetworkRouteProfile: stack.Spec.TargetConnectionProfile.NetworkRouteProfile,
		KubeContext: stack.Spec.TargetConnectionProfile.KubeContext, RoleARN: stack.Spec.TargetConnectionProfile.RoleARN,
	}
	if expectedIdentity.AccountID == "" {
		if expectedIdentity.Provider == targetresolver.ProviderAWS {
			return targetresolver.DiscoveryResult{}, fmt.Errorf("AWS target account is required before discovery")
		}
		expectedIdentity.AccountID = "local"
	}
	if expectedIdentity.Region == "" {
		if expectedIdentity.Provider == targetresolver.ProviderAWS {
			return targetresolver.DiscoveryResult{}, fmt.Errorf("AWS target region is required before discovery")
		}
		expectedIdentity.Region = "local"
	}
	input := targetresolver.DiscoveryInput{SourceClosureDigest: run.Status.SourceBundleDigest, BackendSnapshotDigest: run.Status.ResolvedBackendConfigDigest, EffectivePlanInputDigest: run.Status.EffectivePlanInputDigest, ExpectedTarget: expectedIdentity, ExpectedConnection: expectedProfile}
	verifier := r.TargetVerifier
	if expectedIdentity.Provider == targetresolver.ProviderKind {
		verifier = targetresolver.VerifyLocalKindTarget(expectedProfile.KubeContext)
	}
	if verifier == nil {
		return targetresolver.DiscoveryResult{}, fmt.Errorf("trusted target verifier is not configured for provider %q", expectedIdentity.Provider)
	}
	result, err := targetresolver.DiscoverFromTerraformOutputContext(ctx, content, input, targetresolver.TargetVerifierFunc(func(ctx context.Context, identity targetresolver.RuntimeTargetIdentity, profile targetresolver.TargetConnectionProfile) error {
		if identity.Provider != expectedIdentity.Provider || identity.AccountID != expectedIdentity.AccountID || identity.Region != expectedIdentity.Region || (expectedIdentity.ClusterName != "" && identity.ClusterName != expectedIdentity.ClusterName) || (expectedIdentity.ClusterARN != "" && identity.ClusterARN != expectedIdentity.ClusterARN) {
			return fmt.Errorf("discovered runtime target does not match the InfraStack identity")
		}
		if profile.AuthMode != expectedProfile.AuthMode || (expectedProfile.KubeContext != "" && profile.KubeContext != expectedProfile.KubeContext) {
			return fmt.Errorf("discovered target connection profile does not match the frozen profile")
		}
		if err := targetresolver.ValidateBinding(identity, profile); err != nil {
			return err
		}
		return verifier.Verify(ctx, identity, profile)
	}))
	return result, err
}

func setDiscoveredTarget(status *platformv1alpha1.InfraStackStatus, discovery targetresolver.DiscoveryResult) {
	status.DiscoveredRuntimeTargetIdentity = &platformv1alpha1.RuntimeTargetIdentity{
		Provider: discovery.RuntimeTargetIdentity.Provider, AccountID: discovery.RuntimeTargetIdentity.AccountID,
		Region: discovery.RuntimeTargetIdentity.Region, ClusterARN: discovery.RuntimeTargetIdentity.ClusterARN,
		ClusterName: discovery.RuntimeTargetIdentity.ClusterName, IncarnationID: discovery.RuntimeTargetIdentity.IncarnationID,
	}
	status.DiscoveredTargetConnectionProfile = &platformv1alpha1.TargetConnectionProfile{
		Endpoint: discovery.TargetConnection.Endpoint, CACertificateData: discovery.TargetConnection.CACertificateData,
		CACertificateDigest: discovery.TargetConnection.CACertificateDigest, AuthMode: discovery.TargetConnection.AuthMode,
		NetworkRouteProfile: discovery.TargetConnection.NetworkRouteProfile, KubeContext: discovery.TargetConnection.KubeContext,
		RoleARN: discovery.TargetConnection.RoleARN,
	}
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
		status.CleanupEvidenceRef = fmt.Sprintf("cleanup/infrastack/%s/%d", object.Name, object.Generation)
		status.CleanupEvidenceDigest = digestIdentity(struct {
			Name       string
			Generation int64
			Condition  string
		}{object.Name, object.Generation, condition.Reason})
		if err := r.updateStackStatus(ctx, object, status); err != nil {
			return ctrl.Result{}, err
		}
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
			if !destroyEvidenceReady(apply) {
				return lifecycleCondition(generation, metav1.ConditionFalse, "DestroyEvidencePending", "Terraform Destroy Apply succeeded, but durable terminal or retained target-discovery evidence is incomplete.")
			}
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

func destroyEvidenceReady(run *platformv1alpha1.TerraformRun) bool {
	return run != nil && run.Status.EvidenceCaptured && run.Status.ArtifactsReady && run.Status.TerminalResultRef != "" && run.Status.TerminalResultDigest != "" && run.Status.TargetDiscoveryRef != "" && run.Status.TargetDiscoveryDigest != ""
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
			ResolvedBackendConfigRef:              stack.Spec.Backend.ConfigRef.Name,
			LockTimeout:                           stack.Spec.Backend.LockTimeout,
			VariableSecretRefs:                    stack.Spec.Variables.SecretRefs,
			VariableSecretVariables:               stack.Spec.Variables.SecretVariables,
			RunnerServiceAccountName:              stack.Spec.RunnerServiceAccountName,
			Workspace:                             stack.Spec.Workspace,
			Executor:                              stack.Spec.Executor,
			ExecutionContextDigest:                executionContextDigest(stack.Spec),
			EffectivePlanInputDigest:              effectivePlanInputDigest(stack.Spec),
			InfrastructureExecutionIdentityDigest: digestIdentity(stack.Spec.InfrastructureExecutionIdentity),
			RuntimeTargetIdentityDigest:           digestIdentity(stack.Spec.RuntimeTargetIdentity),
			TargetConnectionProfileDigest:         digestIdentity(stack.Spec.TargetConnectionProfile),
			RunnerImageDigest:                     stack.Spec.Executor.Image,
			ExpectedTerraformVersion:              stack.Spec.Executor.TerraformVersion,
			MutationFence:                         stack.Spec.MutationFence,
			ConcurrencyGroup:                      stack.Spec.ConcurrencyGroup,
			MaxConcurrentPlans:                    stack.Spec.MaxConcurrentPlans,
			MaxConcurrentApplies:                  stack.Spec.MaxConcurrentApplies,
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
			StackRef:                              corev1.LocalObjectReference{Name: stack.Name},
			InfraStackGeneration:                  stack.Generation,
			Operation:                             "Plan",
			PlanMode:                              "Destroy",
			Source:                                platformv1alpha1.PlanRunSourceSpec{Type: terraformexec.SourceRetainedBundle, Path: applied.Spec.Source.Path, Ref: sourceRef, Digest: sourceDigest},
			ResolvedBackendConfigRef:              applied.Spec.ResolvedBackendConfigRef,
			BackendConfigArtifactRef:              backendRef,
			BackendConfigArtifactDigest:           backendDigest,
			TargetDiscoveryRef:                    applied.Status.TargetDiscoveryRef,
			TargetDiscoveryDigest:                 applied.Status.TargetDiscoveryDigest,
			VariableSecretRefs:                    applied.Spec.VariableSecretRefs,
			VariableSecretVariables:               applied.Spec.VariableSecretVariables,
			RunnerServiceAccountName:              applied.Spec.RunnerServiceAccountName,
			LockTimeout:                           applied.Spec.LockTimeout,
			Workspace:                             applied.Spec.Workspace,
			Executor:                              applied.Spec.Executor,
			ExecutionContextDigest:                applied.Spec.ExecutionContextDigest,
			ExecutionTargetIdentityDigest:         applied.Spec.ExecutionTargetIdentityDigest,
			ExecutionPlatformIdentityDigest:       applied.Spec.ExecutionPlatformIdentityDigest,
			EffectivePlanInputDigest:              applied.Spec.EffectivePlanInputDigest,
			SourceClosureDigest:                   applied.Status.SourceBundleDigest,
			VariablesSnapshotDigest:               applied.Spec.VariablesSnapshotDigest,
			SecretVariableIdentityDigest:          applied.Spec.SecretVariableIdentityDigest,
			InfrastructureExecutionIdentityDigest: applied.Spec.InfrastructureExecutionIdentityDigest,
			RuntimeTargetIdentityDigest:           applied.Spec.RuntimeTargetIdentityDigest,
			TargetConnectionProfileDigest:         applied.Spec.TargetConnectionProfileDigest,
			RunnerServiceAccountIdentityDigest:    applied.Spec.RunnerServiceAccountIdentityDigest,
			TerraformLockfileDigest:               applied.Spec.TerraformLockfileDigest,
			ExpectedTerraformVersion:              applied.Spec.ExpectedTerraformVersion,
			RunnerImageDigest:                     applied.Spec.RunnerImageDigest,
			MutationFence:                         stack.Spec.MutationFence,
			ConcurrencyGroup:                      applied.Spec.ConcurrencyGroup,
			MaxConcurrentPlans:                    applied.Spec.MaxConcurrentPlans,
			MaxConcurrentApplies:                  applied.Spec.MaxConcurrentApplies,
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

func retainedRunFromClosure(stack *platformv1alpha1.InfraStack, closure *platformv1alpha1.LastConvergedSourceClosure) *platformv1alpha1.TerraformRun {
	return &platformv1alpha1.TerraformRun{
		ObjectMeta: metav1.ObjectMeta{UID: types.UID(closure.TerraformRunUID)},
		Spec: platformv1alpha1.TerraformRunSpec{
			StackRef:                 corev1.LocalObjectReference{Name: stack.Name},
			Source:                   platformv1alpha1.PlanRunSourceSpec{Type: terraformexec.SourceRetainedBundle, Path: stack.Spec.Source.Path, Ref: closure.SourceClosureRef, Digest: closure.SourceClosureDigest},
			BackendConfigArtifactRef: closure.BackendSnapshotRef, BackendConfigArtifactDigest: closure.BackendSnapshotDigest,
			ResolvedBackendConfigRef: closure.BackendSnapshotRef,
			Workspace:                stack.Spec.Workspace, Executor: stack.Spec.Executor, LockTimeout: stack.Spec.Backend.LockTimeout,
			ExecutionContextDigest:                executionContextDigest(stack.Spec),
			EffectivePlanInputDigest:              closure.EffectivePlanInputDigest,
			InfrastructureExecutionIdentityDigest: closure.InfrastructureExecutionIdentityDigest,
			RuntimeTargetIdentityDigest:           digestIdentity(stack.Spec.RuntimeTargetIdentity),
			TargetConnectionProfileDigest:         digestIdentity(stack.Spec.TargetConnectionProfile),
			RunnerServiceAccountName:              stack.Spec.RunnerServiceAccountName,
			RunnerServiceAccountIdentityDigest:    closure.RunnerServiceAccountIdentityDigest,
			ExpectedTerraformVersion:              stack.Spec.Executor.TerraformVersion,
			RunnerImageDigest:                     stack.Spec.Executor.Image,
		},
		Status: platformv1alpha1.TerraformRunStatus{SourceBundleRef: closure.SourceClosureRef, SourceBundleDigest: closure.SourceClosureDigest, ResolvedBackendConfigRef: closure.BackendSnapshotRef, ResolvedBackendConfigDigest: closure.BackendSnapshotDigest, EffectivePlanInputDigest: closure.EffectivePlanInputDigest, TargetDiscoveryRef: closure.TargetDiscoveryRef, TargetDiscoveryDigest: closure.TargetDiscoveryDigest},
	}
}

func runEvidenceReady(run *platformv1alpha1.TerraformRun) bool {
	return planEvidenceReady(run) && run.Status.TargetDiscoveryRef != "" && run.Status.TargetDiscoveryDigest != ""
}

func planEvidenceReady(run *platformv1alpha1.TerraformRun) bool {
	return run != nil && run.Status.EvidenceCaptured && run.Status.ArtifactsReady && run.Status.TerminalResultRef != "" && run.Status.TerminalResultDigest != "" && run.Status.PlanReportRef != "" && run.Status.PlanReportDigest != ""
}

func convergenceClosure(stack *platformv1alpha1.InfraStack, sourceRun, terminalRun *platformv1alpha1.TerraformRun) (*platformv1alpha1.LastConvergedSourceClosure, bool) {
	if stack == nil || sourceRun == nil || terminalRun == nil || !planEvidenceReady(sourceRun) || terminalRun.Status.TerminalResultRef == "" || terminalRun.Status.TerminalResultDigest == "" || terminalRun.Status.TargetDiscoveryRef == "" || terminalRun.Status.TargetDiscoveryDigest == "" {
		return nil, false
	}
	sourceRef, sourceDigest := retainedBundle(sourceRun)
	backendRef := sourceRun.Status.ResolvedBackendConfigRef
	backendDigest := sourceRun.Status.ResolvedBackendConfigDigest
	if backendRef == "" {
		backendRef = sourceRun.Spec.BackendConfigArtifactRef
		backendDigest = sourceRun.Spec.BackendConfigArtifactDigest
	}
	effective := terminalRun.Status.EffectivePlanInputDigest
	if effective == "" {
		effective = sourceRun.Status.EffectivePlanInputDigest
	}
	if effective == "" {
		effective = sourceRun.Spec.EffectivePlanInputDigest
	}
	if sourceRef == "" || sourceDigest == "" || backendRef == "" || backendDigest == "" || effective == "" || stack.Spec.InfrastructureExecutionIdentity.Provider == "" || stack.Spec.RuntimeTargetIdentity.Provider == "" {
		return nil, false
	}
	return &platformv1alpha1.LastConvergedSourceClosure{
		SourceClosureRef:                      sourceRef,
		SourceClosureDigest:                   sourceDigest,
		BackendSnapshotRef:                    backendRef,
		BackendSnapshotDigest:                 backendDigest,
		TargetDiscoveryRef:                    terminalRun.Status.TargetDiscoveryRef,
		TargetDiscoveryDigest:                 terminalRun.Status.TargetDiscoveryDigest,
		EffectivePlanInputDigest:              effective,
		InfrastructureExecutionIdentityDigest: digestIdentity(stack.Spec.InfrastructureExecutionIdentity),
		ExecutionPlatformIdentityDigest:       digestIdentity(stack.Spec.TargetConnectionProfile),
		RunnerServiceAccountIdentityDigest:    terminalRun.Spec.RunnerServiceAccountIdentityDigest,
		TerraformRunUID:                       string(terminalRun.UID),
		Generation:                            stack.Generation,
	}, true
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
			StackRef:                              corev1.LocalObjectReference{Name: stack.Name},
			InfraStackGeneration:                  stack.Generation,
			Operation:                             "Apply",
			PlanMode:                              plan.Spec.PlanMode,
			Source:                                platformv1alpha1.PlanRunSourceSpec{Type: terraformexec.SourceRetainedBundle, Path: plan.Spec.Source.Path, Ref: sourceRef, Digest: sourceDigest},
			ResolvedBackendConfigRef:              plan.Spec.ResolvedBackendConfigRef,
			BackendConfigArtifactRef:              backendRef,
			BackendConfigArtifactDigest:           backendDigest,
			VariableSecretRefs:                    plan.Spec.VariableSecretRefs,
			VariableSecretVariables:               plan.Spec.VariableSecretVariables,
			RunnerServiceAccountName:              plan.Spec.RunnerServiceAccountName,
			LockTimeout:                           plan.Spec.LockTimeout,
			Workspace:                             plan.Spec.Workspace,
			Executor:                              plan.Spec.Executor,
			ExecutionContextDigest:                plan.Spec.ExecutionContextDigest,
			ExecutionTargetIdentityDigest:         plan.Spec.ExecutionTargetIdentityDigest,
			ExecutionPlatformIdentityDigest:       plan.Spec.ExecutionPlatformIdentityDigest,
			EffectivePlanInputDigest:              plan.Status.EffectivePlanInputDigest,
			SourceClosureDigest:                   plan.Status.SourceBundleDigest,
			VariablesSnapshotDigest:               plan.Spec.VariablesSnapshotDigest,
			SecretVariableIdentityDigest:          plan.Spec.SecretVariableIdentityDigest,
			InfrastructureExecutionIdentityDigest: plan.Spec.InfrastructureExecutionIdentityDigest,
			RuntimeTargetIdentityDigest:           plan.Spec.RuntimeTargetIdentityDigest,
			TargetConnectionProfileDigest:         plan.Spec.TargetConnectionProfileDigest,
			RunnerServiceAccountIdentityDigest:    plan.Spec.RunnerServiceAccountIdentityDigest,
			TerraformLockfileDigest:               plan.Spec.TerraformLockfileDigest,
			ExpectedTerraformVersion:              plan.Spec.ExpectedTerraformVersion,
			RunnerImageDigest:                     plan.Spec.RunnerImageDigest,
			MutationFence:                         stack.Spec.MutationFence,
			PlanRunUID:                            string(plan.UID),
			PlanRef:                               plan.Status.PlanRef,
			PlanDigest:                            plan.Status.PlanDigest,
			PlanReportRef:                         plan.Status.PlanReportRef,
			PlanReportDigest:                      plan.Status.PlanReportDigest,
			TargetDiscoveryRef:                    plan.Spec.TargetDiscoveryRef,
			TargetDiscoveryDigest:                 plan.Spec.TargetDiscoveryDigest,
			ApprovalRef:                           &corev1.LocalObjectReference{Name: approval.Name},
			ApprovalUID:                           string(approval.UID),
			ConcurrencyGroup:                      plan.Spec.ConcurrencyGroup,
			MaxConcurrentPlans:                    plan.Spec.MaxConcurrentPlans,
			MaxConcurrentApplies:                  plan.Spec.MaxConcurrentApplies,
		},
	}
}

func executionContextDigest(spec platformv1alpha1.InfraStackSpec) string {
	encoded, _ := json.Marshal(struct {
		Source    platformv1alpha1.SourceSpec    `json:"source"`
		Backend   platformv1alpha1.BackendSpec   `json:"backend"`
		Workspace string                         `json:"workspace"`
		Capacity  platformv1alpha1.CapacitySpec  `json:"capacity"`
		Variables platformv1alpha1.VariablesSpec `json:"variables"`
		Executor  platformv1alpha1.ExecutorSpec  `json:"executor"`
	}{Source: spec.Source, Backend: spec.Backend, Workspace: spec.Workspace, Capacity: spec.Capacity, Variables: spec.Variables, Executor: spec.Executor})
	hash := sha256.Sum256(encoded)
	return "sha256:" + hex.EncodeToString(hash[:])
}

func effectivePlanInputDigest(spec platformv1alpha1.InfraStackSpec) string {
	digest, err := terraformexec.Digest(struct {
		Source                 platformv1alpha1.SourceSpec
		Backend                platformv1alpha1.BackendSpec
		Workspace              string
		Capacity               platformv1alpha1.CapacitySpec
		Variables              platformv1alpha1.VariablesSpec
		Executor               platformv1alpha1.ExecutorSpec
		InfrastructureIdentity platformv1alpha1.InfrastructureExecutionIdentity
		TargetIdentity         platformv1alpha1.RuntimeTargetIdentity
		ConnectionProfile      platformv1alpha1.TargetConnectionProfile
	}{Source: spec.Source, Backend: spec.Backend, Workspace: spec.Workspace, Capacity: spec.Capacity, Variables: spec.Variables, Executor: spec.Executor, InfrastructureIdentity: spec.InfrastructureExecutionIdentity, TargetIdentity: spec.RuntimeTargetIdentity, ConnectionProfile: spec.TargetConnectionProfile})
	if err != nil {
		return ""
	}
	return digest
}

func digestIdentity(value any) string {
	digest, err := terraformexec.Digest(value)
	if err != nil {
		return ""
	}
	return digest
}

func planRunName(stackName string, generation int64) string {
	return boundedResourceName(fmt.Sprintf("%s-plan-%d", stackName, generation))
}

func destroyPlanRunName(stackName string, generation int64) string {
	return boundedResourceName(fmt.Sprintf("%s-destroy-plan-%d", stackName, generation))
}

func applyRunName(planName, approvalUID string) string {
	hash := sha256.Sum256([]byte(approvalUID))
	return boundedResourceName(fmt.Sprintf("%s-apply-%s", planName, hex.EncodeToString(hash[:])[:10]))
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
