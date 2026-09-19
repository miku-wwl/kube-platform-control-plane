package controller

import (
	"context"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	clientfake "sigs.k8s.io/controller-runtime/pkg/client/fake"

	platformv1alpha1 "github.com/miku-wwl/kube-platform-control-plane/api/v1alpha1"
	terraformexec "github.com/miku-wwl/kube-platform-control-plane/internal/terraform"
)

func phase61Scheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := platformv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme() error = %v", err)
	}
	return scheme
}

func TestPlatformEnvironmentGatesReadyOnCurrentInfrastructureGeneration(t *testing.T) {
	scheme := phase61Scheme(t)
	stack := &platformv1alpha1.InfraStack{
		ObjectMeta: metav1.ObjectMeta{Name: "stack", Namespace: "platform-system", Generation: 3},
		Status: platformv1alpha1.InfraStackStatus{Conditions: []metav1.Condition{
			lifecycleCondition(3, metav1.ConditionFalse, "Applying", "apply is running"),
		}},
	}
	resourceSet := &platformv1alpha1.ResourceSet{
		ObjectMeta: metav1.ObjectMeta{Name: "environment-resources", Namespace: "platform-system", Generation: 4},
		Spec:       platformv1alpha1.ResourceSetSpec{Target: platformv1alpha1.TargetReference{Provider: "kind", ClusterName: "target"}},
		Status: platformv1alpha1.ResourceSetStatus{Conditions: []metav1.Condition{
			lifecycleCondition(4, metav1.ConditionTrue, "RuntimeReady", "runtime is ready"),
		}},
	}
	environment := &platformv1alpha1.PlatformEnvironment{
		ObjectMeta: metav1.ObjectMeta{Name: "environment", Namespace: "platform-system", Generation: 2, Finalizers: []string{platformEnvironmentFinalizer}},
		Spec: platformv1alpha1.PlatformEnvironmentSpec{
			InfraStackRef: corev1.LocalObjectReference{Name: stack.Name},
			Target:        resourceSet.Spec.Target,
		},
		Status: platformv1alpha1.PlatformEnvironmentStatus{Conditions: []metav1.Condition{
			lifecycleCondition(2, metav1.ConditionTrue, "RuntimeEmpty", "stale ready state"),
		}},
	}
	client := clientfake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(environment, stack, resourceSet).WithObjects(environment, stack, resourceSet).Build()
	reconciler := &PlatformEnvironmentReconciler{Client: client, Scheme: scheme}
	request := ctrl.Request{NamespacedName: types.NamespacedName{Name: environment.Name, Namespace: environment.Namespace}}

	if _, err := reconciler.Reconcile(context.Background(), request); err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}
	var observed platformv1alpha1.PlatformEnvironment
	if err := client.Get(context.Background(), request.NamespacedName, &observed); err != nil {
		t.Fatalf("read environment: %v", err)
	}
	condition := findCondition(observed.Status.Conditions, ConditionReady)
	if condition == nil || condition.Status != metav1.ConditionFalse || condition.Reason != "InfrastructureNotReady" {
		t.Fatalf("environment condition = %+v", condition)
	}

	stack.Status.Conditions = []metav1.Condition{lifecycleCondition(stack.Generation, metav1.ConditionTrue, "InfrastructureReady", "infrastructure is ready")}
	if err := client.Status().Update(context.Background(), stack); err != nil {
		t.Fatalf("update stack status: %v", err)
	}
	if _, err := reconciler.Reconcile(context.Background(), request); err != nil {
		t.Fatalf("second Reconcile() error = %v", err)
	}
	if err := client.Get(context.Background(), request.NamespacedName, &observed); err != nil {
		t.Fatalf("read environment after stack readiness: %v", err)
	}
	condition = findCondition(observed.Status.Conditions, ConditionReady)
	if condition == nil || condition.Status != metav1.ConditionTrue || condition.Reason != "EnvironmentReady" {
		t.Fatalf("environment did not become ready after both children: %+v", condition)
	}
}

func TestPlatformEnvironmentDeleteWithoutInfraStackReferenceRemovesFinalizer(t *testing.T) {
	scheme := phase61Scheme(t)
	deletedAt := metav1.NewTime(time.Now())
	environment := &platformv1alpha1.PlatformEnvironment{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "quota-rejected",
			Namespace:         "platform-system",
			DeletionTimestamp: &deletedAt,
			Finalizers:        []string{platformEnvironmentFinalizer},
		},
	}
	client := clientfake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(environment).WithObjects(environment).Build()
	reconciler := &PlatformEnvironmentReconciler{Client: client, Scheme: scheme}
	request := ctrl.Request{NamespacedName: types.NamespacedName{Name: environment.Name, Namespace: environment.Namespace}}

	if _, err := reconciler.Reconcile(context.Background(), request); err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}
	var observed platformv1alpha1.PlatformEnvironment
	if err := client.Get(context.Background(), request.NamespacedName, &observed); apierrors.IsNotFound(err) {
		return
	} else if err != nil {
		t.Fatalf("read environment: %v", err)
	}
	if containsString(observed.Finalizers, platformEnvironmentFinalizer) {
		t.Fatalf("finalizer was not removed: %v", observed.Finalizers)
	}
}

func TestInfraStackAutomaticallyCreatesExactlyOneApplyRunAfterApproval(t *testing.T) {
	scheme := phase61Scheme(t)
	planExpires := metav1.NewTime(time.Now().Add(time.Hour))
	plan := &platformv1alpha1.TerraformRun{
		ObjectMeta: metav1.ObjectMeta{Name: "stack-plan-1", Namespace: "platform-system", UID: types.UID("plan-uid")},
		Spec: platformv1alpha1.TerraformRunSpec{
			StackRef: corev1.LocalObjectReference{Name: "stack"}, PlanMode: "Reconcile", ExecutionContextDigest: "sha256:context", EffectivePlanInputDigest: "sha256:input", RuntimeTargetIdentityDigest: "sha256:target", InfrastructureExecutionIdentityDigest: "sha256:infra",
		},
		Status: platformv1alpha1.TerraformRunStatus{
			ExecutionOutcome: "ChangesPresent", PlanDigest: "sha256:plan", SourceBundleRef: "runs/plan/source-bundle.tar.zst", SourceBundleDigest: "sha256:bundle", PlanExpiresAt: &planExpires,
		},
	}
	approval := &platformv1alpha1.ChangeApproval{
		ObjectMeta: metav1.ObjectMeta{Name: "approval", Namespace: "platform-system", UID: types.UID("approval-uid")},
		Spec:       platformv1alpha1.ChangeApprovalSpec{PlanRunRef: corev1.LocalObjectReference{Name: plan.Name}, PlanRunUID: string(plan.UID), PlanDigest: plan.Status.PlanDigest, ExecutionContextDigest: plan.Spec.ExecutionContextDigest},
	}
	stack := phase61Stack()
	client := clientfake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(stack, plan).WithObjects(stack, plan, approval).Build()
	reconciler := &InfraStackReconciler{Client: client, Scheme: scheme}
	request := ctrl.Request{NamespacedName: types.NamespacedName{Name: stack.Name, Namespace: stack.Namespace}}

	if _, err := reconciler.Reconcile(context.Background(), request); err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}
	if _, err := reconciler.Reconcile(context.Background(), request); err != nil {
		t.Fatalf("idempotent Reconcile() error = %v", err)
	}
	var runs platformv1alpha1.TerraformRunList
	if err := client.List(context.Background(), &runs); err != nil {
		t.Fatalf("list TerraformRuns: %v", err)
	}
	applyCount := 0
	for _, run := range runs.Items {
		if run.Spec.Operation == "Apply" {
			applyCount++
			if run.Spec.PlanRunUID != string(plan.UID) || run.Spec.ApprovalUID != string(approval.UID) || run.Spec.Source.Type != terraformexec.SourceRetainedBundle {
				t.Fatalf("ApplyRun binding = %+v", run.Spec)
			}
		}
	}
	if applyCount != 1 {
		t.Fatalf("ApplyRun count = %d, want 1", applyCount)
	}
}

func TestInfraStackDestroyPlanUsesRetainedApplyBundle(t *testing.T) {
	scheme := phase61Scheme(t)
	stack := phase61Stack()
	stack.Spec.DesiredState = platformv1alpha1.DesiredStateDestroy
	stack.Status.LastAppliedRunRef = &corev1.LocalObjectReference{Name: "apply-run"}
	apply := &platformv1alpha1.TerraformRun{
		ObjectMeta: metav1.ObjectMeta{Name: "apply-run", Namespace: stack.Namespace},
		Spec:       platformv1alpha1.TerraformRunSpec{StackRef: corev1.LocalObjectReference{Name: stack.Name}, Source: platformv1alpha1.PlanRunSourceSpec{Type: terraformexec.SourceRetainedBundle, Ref: "runs/apply/source-bundle.tar.zst", Digest: "sha256:bundle"}, ExecutionContextDigest: "sha256:context", Executor: stack.Spec.Executor, Workspace: stack.Spec.Workspace},
		Status:     platformv1alpha1.TerraformRunStatus{ExecutionOutcome: "Succeeded", SourceBundleRef: "runs/apply/source-bundle.tar.zst", SourceBundleDigest: "sha256:bundle"},
	}
	client := clientfake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(stack, apply).WithObjects(stack, apply).Build()
	reconciler := &InfraStackReconciler{Client: client, Scheme: scheme}
	request := ctrl.Request{NamespacedName: types.NamespacedName{Name: stack.Name, Namespace: stack.Namespace}}

	if _, err := reconciler.Reconcile(context.Background(), request); err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}
	if _, err := reconciler.Reconcile(context.Background(), request); err != nil {
		t.Fatalf("Reconcile() after durable fence: %v", err)
	}
	var destroyPlan platformv1alpha1.TerraformRun
	if err := client.Get(context.Background(), types.NamespacedName{Name: destroyPlanRunName(stack.Name, stack.Generation), Namespace: stack.Namespace}, &destroyPlan); err != nil {
		t.Fatalf("read destroy plan: %v", err)
	}
	if destroyPlan.Spec.PlanMode != "Destroy" || destroyPlan.Spec.Source.Type != terraformexec.SourceRetainedBundle || destroyPlan.Spec.Source.Ref != "runs/apply/source-bundle.tar.zst" {
		t.Fatalf("destroy source = %+v", destroyPlan.Spec.Source)
	}
}

func TestInfraStackDoesNotReplanAfterInfrastructureRemoval(t *testing.T) {
	scheme := phase61Scheme(t)
	stack := phase61Stack()
	stack.Spec.DesiredState = platformv1alpha1.DesiredStateDestroy
	stack.Status.Conditions = []metav1.Condition{lifecycleCondition(stack.Generation, metav1.ConditionTrue, "InfrastructureRemoved", "destroy completed")}
	client := clientfake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(stack).WithObjects(stack).Build()
	reconciler := &InfraStackReconciler{Client: client, Scheme: scheme}
	request := ctrl.Request{NamespacedName: types.NamespacedName{Name: stack.Name, Namespace: stack.Namespace}}
	if _, err := reconciler.Reconcile(context.Background(), request); err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}
	var plans platformv1alpha1.TerraformRunList
	if err := client.List(context.Background(), &plans); err != nil {
		t.Fatalf("list TerraformRuns: %v", err)
	}
	if len(plans.Items) != 0 {
		t.Fatalf("completed destroy was replanned: %+v", plans.Items)
	}
}

func phase61Stack() *platformv1alpha1.InfraStack {
	return &platformv1alpha1.InfraStack{
		ObjectMeta: metav1.ObjectMeta{Name: "stack", Namespace: "platform-system", Generation: 1, UID: types.UID("stack-uid"), Finalizers: []string{infraStackFinalizer}},
		Spec: platformv1alpha1.InfraStackSpec{
			Source:       platformv1alpha1.SourceSpec{URL: "https://example.invalid/repo", Revision: "0123456789012345678901234567890123456789", Path: "env"},
			Backend:      platformv1alpha1.BackendSpec{Type: "s3", ConfigRef: corev1.LocalObjectReference{Name: "backend"}},
			Workspace:    "stack",
			Executor:     platformv1alpha1.ExecutorSpec{TerraformVersion: "1.9.0", Image: "runner:latest", WorkDir: "/workspace/terraform", ExecutionTimeout: metav1.Duration{Duration: time.Hour}},
			DesiredState: platformv1alpha1.DesiredStatePresent,
		},
	}
}
