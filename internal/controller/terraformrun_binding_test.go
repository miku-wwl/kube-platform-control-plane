package controller

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientfake "sigs.k8s.io/controller-runtime/pkg/client/fake"

	platformv1alpha1 "github.com/miku-wwl/kube-platform-control-plane/api/v1alpha1"
)

func TestValidateApplyApprovalBindsPlanAndApprovalUID(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := platformv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme() error = %v", err)
	}
	plan := &platformv1alpha1.TerraformRun{
		ObjectMeta: metav1.ObjectMeta{Name: "plan-run", Namespace: "platform-system", UID: types.UID("plan-uid")},
		Status:     platformv1alpha1.TerraformRunStatus{PlanDigest: "sha256:plan"},
		Spec:       platformv1alpha1.TerraformRunSpec{ExecutionContextDigest: "sha256:context"},
	}
	approval := &platformv1alpha1.ChangeApproval{
		ObjectMeta: metav1.ObjectMeta{Name: "approval", Namespace: "platform-system", UID: types.UID("approval-uid")},
		Spec: platformv1alpha1.ChangeApprovalSpec{
			PlanRunRef:             corev1.LocalObjectReference{Name: plan.Name},
			PlanDigest:             "sha256:plan",
			ExecutionContextDigest: "sha256:context",
		},
	}
	apply := &platformv1alpha1.TerraformRun{
		ObjectMeta: metav1.ObjectMeta{Name: "apply-run", Namespace: "platform-system"},
		Spec: platformv1alpha1.TerraformRunSpec{
			Operation:              "Apply",
			ApprovalRef:            &corev1.LocalObjectReference{Name: approval.Name},
			ApprovalUID:            "approval-uid",
			PlanRunUID:             "plan-uid",
			PlanDigest:             "sha256:plan",
			ExecutionContextDigest: "sha256:context",
		},
	}
	client := clientfake.NewClientBuilder().WithScheme(scheme).WithObjects(plan, approval).Build()
	reconciler := &TerraformRunReconciler{Client: client}
	if err := reconciler.validateApplyApproval(context.Background(), apply); err != nil {
		t.Fatalf("validateApplyApproval() error = %v", err)
	}

	apply.Spec.ApprovalUID = "replacement-approval-uid"
	if err := reconciler.validateApplyApproval(context.Background(), apply); err == nil {
		t.Fatal("approval UID replacement must be rejected")
	}
}
