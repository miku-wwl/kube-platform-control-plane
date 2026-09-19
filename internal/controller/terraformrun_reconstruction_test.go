package controller

import (
	"context"
	"testing"

	batchv1 "k8s.io/api/batch/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	corefake "k8s.io/client-go/kubernetes/fake"
	clientfake "sigs.k8s.io/controller-runtime/pkg/client/fake"

	platformv1alpha1 "github.com/miku-wwl/kube-platform-control-plane/api/v1alpha1"
	"github.com/miku-wwl/kube-platform-control-plane/internal/reliability"
	corev1 "k8s.io/api/core/v1"
)

func TestReconstructActiveTerraformRunsUsesJobsAsAuthority(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := platformv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("add platform scheme: %v", err)
	}
	run := &platformv1alpha1.TerraformRun{
		ObjectMeta: metav1.ObjectMeta{Namespace: "platform-system", Name: "plan", UID: types.UID("run-1")},
		Spec:       platformv1alpha1.TerraformRunSpec{Operation: "Plan", StackRef: corev1.LocalObjectReference{Name: "stack"}},
	}
	reader := clientfake.NewClientBuilder().WithScheme(scheme).WithObjects(run).Build()
	job := &batchv1.Job{ObjectMeta: metav1.ObjectMeta{Namespace: "platform-system", Name: "plan-job", Labels: map[string]string{"platform.example.io/terraform-run-uid": "run-1"}}, Status: batchv1.JobStatus{Active: 1}}
	result, err := ReconstructActiveTerraformRuns(context.Background(), reader, corefake.NewSimpleClientset(job), reliability.NewGate(1, 1))
	if err != nil {
		t.Fatalf("reconstruct: %v", err)
	}
	if !result.GateReady || result.SafetyViolation || len(result.Active) != 1 || result.Active[0].ID != "run-1" {
		t.Fatalf("reconstruction result = %+v", result)
	}
}

func TestReconstructActiveTerraformRunsBlocksOrphanJob(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := platformv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("add platform scheme: %v", err)
	}
	reader := clientfake.NewClientBuilder().WithScheme(scheme).Build()
	job := &batchv1.Job{ObjectMeta: metav1.ObjectMeta{Namespace: "platform-system", Name: "orphan", Labels: map[string]string{"platform.example.io/terraform-run-uid": "missing"}}, Status: batchv1.JobStatus{Active: 1}}
	result, err := ReconstructActiveTerraformRuns(context.Background(), reader, corefake.NewSimpleClientset(job), reliability.NewGate(1, 1))
	if err != nil {
		t.Fatalf("reconstruct: %v", err)
	}
	if !result.GateReady || !result.SafetyViolation {
		t.Fatalf("orphan reconstruction result = %+v", result)
	}
}
