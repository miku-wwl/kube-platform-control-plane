package controller

import (
	"context"
	"testing"
	"time"

	platform "github.com/miku-wwl/kube-platform-control-plane/api/v1alpha1"
	"github.com/miku-wwl/kube-platform-control-plane/internal/reliability"
	runtimer "github.com/miku-wwl/kube-platform-control-plane/internal/runtime"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	kubefake "k8s.io/client-go/kubernetes/fake"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func approvalFixture() (*platform.InfraStack, *platform.TerraformRun, *platform.ChangeApproval) {
	stack := &platform.InfraStack{ObjectMeta: metav1.ObjectMeta{Name: "stack", Namespace: "owned", UID: "stack-uid", Generation: 1}, Spec: platform.InfraStackSpec{RunnerServiceAccountName: "runner", RuntimeTargetIdentity: platform.RuntimeTargetIdentity{Provider: "kubernetes"}, InfrastructureExecutionIdentity: platform.InfrastructureExecutionIdentity{Provider: "kubernetes"}, Variables: platform.VariablesSpec{Values: map[string]string{"bucket_name": "owned"}}}}
	plan := buildPlanRun(stack, "plan")
	plan.UID = "plan-uid"
	plan.Spec.ExecutionContextDigest = planContextDigest(stack.Spec, nil)
	expires := metav1.NewTime(time.Now().Add(time.Hour))
	plan.Status = platform.TerraformRunStatus{ExecutionOutcome: "ChangesPresent", EvidenceCaptured: true, ArtifactsReady: true, TerminalResultRef: "terminal", TerminalResultDigest: "sha256:terminal", PlanDigest: "sha256:plan", PlanReportRef: "report", PlanReportDigest: "sha256:report", EffectivePlanInputDigest: "sha256:inputs", PlanExpiresAt: &expires}
	approval := &platform.ChangeApproval{ObjectMeta: metav1.ObjectMeta{Name: "approval", Namespace: stack.Namespace, UID: "approval-uid"}, Spec: platform.ChangeApprovalSpec{PlanRunRef: corev1.LocalObjectReference{Name: plan.Name}, PlanRunUID: string(plan.UID), PlanDigest: plan.Status.PlanDigest, ExecutionContextDigest: plan.Spec.ExecutionContextDigest, EffectivePlanInputDigest: plan.Status.EffectivePlanInputDigest, PlanReportRef: plan.Status.PlanReportRef, PlanReportDigest: plan.Status.PlanReportDigest}}
	return stack, plan, approval
}

func TestApprovalBindsCompleteSnapshotAndDeduplicatesPlan(t *testing.T) {
	stack, plan, approval := approvalFixture()
	if err := validateApplyAdmission(stack, plan, approval); err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*platform.InfraStack){
		"source":  func(s *platform.InfraStack) { s.Spec.Source.Revision = "changed" },
		"input":   func(s *platform.InfraStack) { s.Spec.Variables.Values["bucket_name"] = "foreign" },
		"target":  func(s *platform.InfraStack) { s.Spec.RuntimeTargetIdentity.IncarnationID = "replaced" },
		"backend": func(s *platform.InfraStack) { s.Spec.Backend.ConfigRef.Name = "other" },
		"runner":  func(s *platform.InfraStack) { s.Spec.Executor.Image = "changed" },
	} {
		t.Run(name, func(t *testing.T) {
			copy := stack.DeepCopy()
			mutate(copy)
			if validateApplyAdmission(copy, plan, approval) == nil {
				t.Fatal("changed snapshot accepted")
			}
		})
	}
	for name, mutate := range map[string]func(*platform.TerraformRun, *platform.ChangeApproval){
		"digest":            func(_ *platform.TerraformRun, a *platform.ChangeApproval) { a.Spec.PlanDigest = "wrong" },
		"uid":               func(_ *platform.TerraformRun, a *platform.ChangeApproval) { a.Spec.PlanRunUID = "wrong" },
		"report reference":  func(_ *platform.TerraformRun, a *platform.ChangeApproval) { a.Spec.PlanReportRef = "wrong" },
		"report digest":     func(_ *platform.TerraformRun, a *platform.ChangeApproval) { a.Spec.PlanReportDigest = "wrong" },
		"execution context": func(_ *platform.TerraformRun, a *platform.ChangeApproval) { a.Spec.ExecutionContextDigest = "wrong" },
		"effective inputs":  func(_ *platform.TerraformRun, a *platform.ChangeApproval) { a.Spec.EffectivePlanInputDigest = "wrong" },
		"wrong operation":   func(p *platform.TerraformRun, _ *platform.ChangeApproval) { p.Spec.Operation = "Apply" },
		"Superseded":        func(p *platform.TerraformRun, _ *platform.ChangeApproval) { p.Status.ExecutionOutcome = "Superseded" },
		"expired":           func(p *platform.TerraformRun, _ *platform.ChangeApproval) { p.Status.PlanExpiresAt = &metav1.Time{} },
		"NoChange":          func(p *platform.TerraformRun, _ *platform.ChangeApproval) { p.Status.ExecutionOutcome = "NoChange" },
		"evidence":          func(p *platform.TerraformRun, _ *platform.ChangeApproval) { p.Status.EvidenceCaptured = false },
	} {
		t.Run(name, func(t *testing.T) {
			p, a := plan.DeepCopy(), approval.DeepCopy()
			mutate(p, a)
			if validateApplyAdmission(stack, p, a) == nil {
				t.Fatal("invalid approval accepted")
			}
		})
	}
	scheme := runtime.NewScheme()
	_ = platform.AddToScheme(scheme)
	kube := fake.NewClientBuilder().WithScheme(scheme).Build()
	r := InfraStackReconciler{Client: kube, Scheme: scheme}
	first, err := r.ensureApplyRun(context.Background(), stack, plan, approval)
	if err != nil {
		t.Fatal(err)
	}
	approval.UID = "second-human-approval"
	second, err := r.ensureApplyRun(context.Background(), stack, plan, approval)
	if err != nil {
		t.Fatal(err)
	}
	var runs platform.TerraformRunList
	_ = kube.List(context.Background(), &runs)
	if first.Name != second.Name || len(runs.Items) != 1 {
		t.Fatal("second approval duplicated Apply for the same Plan")
	}
}

func TestTerminalIdentityRejectsEveryExecutionBinding(t *testing.T) {
	_, run, _ := approvalFixture()
	run.Status.JobUID = "job-uid"
	job := &batchv1.Job{ObjectMeta: metav1.ObjectMeta{UID: "job-uid"}}
	now := time.Now()
	good := runnerTerminalResult{RunUID: string(run.UID), JobUID: "job-uid", Operation: "Plan", ExecutionOutcome: "ChangesPresent", TerraformExitCode: 2, HasChanges: true, StartedAt: now, FinishedAt: now.Add(time.Second)}
	if err := validateTerminalIdentity(run, good, job); err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*runnerTerminalResult){
		"Run UID":              func(r *runnerTerminalResult) { r.RunUID = "foreign" },
		"Job UID":              func(r *runnerTerminalResult) { r.JobUID = "foreign" },
		"operation":            func(r *runnerTerminalResult) { r.Operation = "Apply" },
		"missing start":        func(r *runnerTerminalResult) { r.StartedAt = time.Time{} },
		"reversed timestamps":  func(r *runnerTerminalResult) { r.FinishedAt = now.Add(-time.Second) },
		"inconsistent exit":    func(r *runnerTerminalResult) { r.TerraformExitCode = 0 },
		"inconsistent changes": func(r *runnerTerminalResult) { r.HasChanges = false },
		"unknown outcome":      func(r *runnerTerminalResult) { r.ExecutionOutcome = "invented" },
	} {
		t.Run(name, func(t *testing.T) {
			bad := good
			mutate(&bad)
			if err := validateTerminalIdentity(run, bad, job); err == nil {
				t.Fatal("invalid terminal identity accepted")
			}
		})
	}
}

func TestDeepCopyIncludesInputsCredentialsAndRetainedExecution(t *testing.T) {
	stack, _, _ := approvalFixture()
	stack.Spec.ExecutionProfile = &platform.ExecutionProfile{CredentialsSecretRef: corev1.LocalObjectReference{Name: "original"}}
	stack.Status.LastConverged = &platform.LastConvergedSourceClosure{ExecutionSnapshot: &runtime.RawExtension{Raw: []byte(`{"inputs":{"bucket_name":"original"}}`)}}
	copy := stack.DeepCopy()
	copy.Spec.Variables.Values["bucket_name"] = "changed"
	copy.Spec.ExecutionProfile.CredentialsSecretRef.Name = "changed"
	copy.Status.LastConverged.ExecutionSnapshot.Raw[0] = 'x'
	if stack.Spec.Variables.Values["bucket_name"] != "owned" || stack.Spec.ExecutionProfile.CredentialsSecretRef.Name != "original" || stack.Status.LastConverged.ExecutionSnapshot.Raw[0] != '{' {
		t.Fatal("deep copy aliases execution authority")
	}
}

func TestRecoveryReconstructsConcurrencyAndRejectsForeignJobs(t *testing.T) {
	ctx := context.Background()
	scheme := runtime.NewScheme()
	_ = platform.AddToScheme(scheme)
	run := &platform.TerraformRun{ObjectMeta: metav1.ObjectMeta{Name: "run", Namespace: "owned", UID: "run-uid"}, Spec: platform.TerraformRunSpec{Operation: "Apply", StackRef: corev1.LocalObjectReference{Name: "stack"}, ConcurrencyGroup: "class", MaxConcurrentApplies: 1}}
	controller := true
	job := &batchv1.Job{ObjectMeta: metav1.ObjectMeta{Name: run.Name, Namespace: run.Namespace, UID: "job-uid", Labels: map[string]string{"platform.example.io/terraform-run-uid": string(run.UID)}, OwnerReferences: []metav1.OwnerReference{{Kind: "TerraformRun", UID: run.UID, Controller: &controller}}}}
	reader := fake.NewClientBuilder().WithScheme(scheme).WithObjects(run).Build()
	gate := reliability.NewGate(4, 4)
	gate.CloseAdmission()
	if gate.Ready() {
		t.Fatal("admission opened before reconstruction")
	}
	result, err := ReconstructActiveTerraformRuns(ctx, reader, kubefake.NewSimpleClientset(job), gate)
	if err != nil || result.SafetyViolation {
		t.Fatalf("reconstruct: %+v %v", result, err)
	}
	if gate.TryAcquire(reliability.WorkItem{ID: "other", Kind: reliability.KindApply, Stack: "other", ConcurrencyGroup: "class", MaxConcurrentApplies: 1}) {
		t.Fatal("reconstruction lost class concurrency limit")
	}
	job.OwnerReferences[0].UID = "foreign"
	result, err = ReconstructActiveTerraformRuns(ctx, reader, kubefake.NewSimpleClientset(job), gate)
	if err != nil || !result.SafetyViolation || gate.Ready() {
		t.Fatal("foreign active Job did not close admission")
	}
}

func TestTerminalIdentityAndRuntimeFailureRetainSafety(t *testing.T) {
	_, run, _ := approvalFixture()
	now := time.Now()
	result := runnerTerminalResult{RunUID: string(run.UID), JobUID: "job", Operation: "Plan", ExecutionOutcome: "ChangesPresent", TerraformExitCode: 2, HasChanges: true, StartedAt: now, FinishedAt: now}
	if err := validateTerminalIdentity(run, result, nil); err != nil {
		t.Fatal(err)
	}
	result.Operation = "Apply"
	if validateTerminalIdentity(run, result, nil) == nil {
		t.Fatal("wrong terminal operation accepted")
	}
	scheme := runtime.NewScheme()
	_ = platform.AddToScheme(scheme)
	resource := &platform.ResourceSet{ObjectMeta: metav1.ObjectMeta{Name: "runtime", Namespace: "owned", UID: types.UID("runtime-uid"), Finalizers: []string{resourceSetFinalizer}}, Status: platform.ResourceSetStatus{Inventory: []platform.InventoryItemStatus{{Name: "owned", UID: "object-uid"}}}}
	kube := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(resource).WithObjects(resource).Build()
	r := ResourceSetReconciler{Client: kube, Scheme: scheme}
	if _, err := r.reconcileDelete(context.Background(), resource); err == nil {
		t.Fatal("unavailable target must block cleanup")
	}
	var retained platform.ResourceSet
	if err := kube.Get(context.Background(), client.ObjectKeyFromObject(resource), &retained); err != nil || len(retained.Finalizers) == 0 {
		t.Fatal("transient failure released finalizer")
	}
}

func TestApplyOnlyReadinessPreservesExplicitPolicy(t *testing.T) {
	object := &unstructured.Unstructured{Object: map[string]any{"apiVersion": "v1", "kind": "ConfigMap", "metadata": map[string]any{"name": "owned", "namespace": "owned"}}}
	kube := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme(), object)
	item := runtimer.InventoryItem{Version: "v1", Resource: "configmaps", Kind: "ConfigMap", Namespace: "owned", Name: "owned", ReadinessPolicy: "ApplyOnly"}
	r := ResourceSetReconciler{}
	if _, _, status := r.runtimeReadiness(context.Background(), []runtimer.InventoryItem{item}, kube); status != metav1.ConditionTrue {
		t.Fatal("ApplyOnly resource incorrectly requires a readiness adapter")
	}
	item.ReadinessPolicy = "RequireReady"
	if _, _, status := r.runtimeReadiness(context.Background(), []runtimer.InventoryItem{item}, kube); status != metav1.ConditionUnknown {
		t.Fatal("RequireReady silently downgraded")
	}
}
