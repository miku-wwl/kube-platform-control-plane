package controller

import (
	"context"
	"testing"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	clientfake "sigs.k8s.io/controller-runtime/pkg/client/fake"

	platformv1alpha1 "github.com/miku-wwl/kube-platform-control-plane/api/v1alpha1"
	"github.com/miku-wwl/kube-platform-control-plane/internal/reliability"
)

func TestTerraformRunBackpressureKeepsPendingUntilSlotAvailable(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := platformv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme() error = %v", err)
	}
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		t.Fatalf("client scheme error = %v", err)
	}
	first := testPlanRun("plan-one", types.UID("run-one"))
	second := testPlanRun("plan-two", types.UID("run-two"))
	client := clientfake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(first, second).WithObjects(first, second).Build()
	reconciler := &TerraformRunReconciler{
		Client:           client,
		Scheme:           scheme,
		ExecutionEnabled: true,
		ArtifactEndpoint: "http://localstack:4566",
		ArtifactRegion:   "us-east-1",
		ArtifactBucket:   "artifacts",
		Gate:             reliability.NewGate(1, 1),
	}

	if _, err := reconciler.Reconcile(context.Background(), ctrl.Request{NamespacedName: types.NamespacedName{Name: first.Name, Namespace: first.Namespace}}); err != nil {
		t.Fatalf("first Reconcile() error = %v", err)
	}
	var firstAfter platformv1alpha1.TerraformRun
	if err := client.Get(context.Background(), types.NamespacedName{Name: first.Name, Namespace: first.Namespace}, &firstAfter); err != nil {
		t.Fatalf("get first run: %v", err)
	}
	if firstAfter.Status.JobRef == nil {
		var jobs batchv1.JobList
		if err := client.List(context.Background(), &jobs); err != nil {
			t.Fatalf("list jobs: %v", err)
		}
		t.Fatalf("first run did not create a Job: status=%+v jobs=%d", firstAfter.Status, len(jobs.Items))
	}

	result, err := reconciler.Reconcile(context.Background(), ctrl.Request{NamespacedName: types.NamespacedName{Name: second.Name, Namespace: second.Namespace}})
	if err != nil {
		t.Fatalf("second pending Reconcile() error = %v", err)
	}
	if result.RequeueAfter <= 0 {
		t.Fatalf("pending run requeue duration = %s, want positive", result.RequeueAfter)
	}
	var secondAfter platformv1alpha1.TerraformRun
	if err := client.Get(context.Background(), types.NamespacedName{Name: second.Name, Namespace: second.Namespace}, &secondAfter); err != nil {
		t.Fatalf("get second run: %v", err)
	}
	condition := findCondition(secondAfter.Status.Conditions, ConditionReady)
	if condition == nil || condition.Reason != "ExecutionSlotUnavailable" || secondAfter.Status.JobRef != nil {
		t.Fatalf("pending run status = %+v", secondAfter.Status)
	}

	reconciler.Gate.Release(reliability.WorkItem{ID: string(first.UID), Kind: reliability.KindPlan, Stack: first.Spec.StackRef.Name})
	if _, err := reconciler.Reconcile(context.Background(), ctrl.Request{NamespacedName: types.NamespacedName{Name: second.Name, Namespace: second.Namespace}}); err != nil {
		t.Fatalf("drained second Reconcile() error = %v", err)
	}
	if err := client.Get(context.Background(), types.NamespacedName{Name: second.Name, Namespace: second.Namespace}, &secondAfter); err != nil {
		t.Fatalf("get drained second run: %v", err)
	}
	if secondAfter.Status.JobRef == nil {
		t.Fatal("pending run did not drain after slot release")
	}
}

func testPlanRun(name string, uid types.UID) *platformv1alpha1.TerraformRun {
	return &platformv1alpha1.TerraformRun{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "platform-system", UID: uid},
		Spec: platformv1alpha1.TerraformRunSpec{
			StackRef:  coreLocalReference("stack-a"),
			Operation: "Plan",
			PlanMode:  "Reconcile",
			Source: platformv1alpha1.PlanRunSourceSpec{
				Type:     "GitCommit",
				URL:      "https://example.invalid/platform.git",
				Revision: "0123456789012345678901234567890123456789",
			},
			Workspace: "default",
			Executor: platformv1alpha1.ExecutorSpec{
				Image:            "platform-terraform-runner:phase2",
				WorkDir:          "/workspace/terraform",
				ExecutionTimeout: metav1.Duration{Duration: time.Minute},
			},
		},
	}
}

func coreLocalReference(name string) corev1.LocalObjectReference {
	return corev1.LocalObjectReference{Name: name}
}
