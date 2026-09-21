package controller

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	platformv1alpha1 "github.com/miku-wwl/kube-platform-control-plane/api/v1alpha1"
)

func TestRunnerServiceAccountWiresAWSManagementRoleOnly(t *testing.T) {
	class := &platformv1alpha1.EnvironmentClass{ObjectMeta: metav1.ObjectMeta{Name: "aws"}, Spec: platformv1alpha1.EnvironmentClassSpec{
		RunnerProfile: platformv1alpha1.RunnerProfileSpec{ManagementRoleARN: "arn:aws:iam::123456789012:role/management"},
		Target:        platformv1alpha1.TargetReference{Provider: "aws", ExecutionRoleARN: "arn:aws:iam::123456789012:role/infra"},
	}}
	annotations := runnerServiceAccountAnnotations(class)
	if annotations["eks.amazonaws.com/role-arn"] != class.Spec.RunnerProfile.ManagementRoleARN {
		t.Fatalf("AWS runner annotations = %#v", annotations)
	}
	if annotations["eks.amazonaws.com/role-arn"] == class.Spec.Target.ExecutionRoleARN {
		t.Fatalf("AWS runner annotation collapsed into execution role = %#v", annotations)
	}
	withoutManagementRole := class.DeepCopy()
	withoutManagementRole.Spec.RunnerProfile.ManagementRoleARN = ""
	if got := runnerServiceAccountAnnotations(withoutManagementRole); got != nil {
		t.Fatalf("AWS runner unexpectedly received an implicit role = %#v", got)
	}
	kind := &platformv1alpha1.EnvironmentClass{Spec: platformv1alpha1.EnvironmentClassSpec{Target: platformv1alpha1.TargetReference{Provider: "kind", ExecutionRoleARN: "must-not-leak"}}}
	if got := runnerServiceAccountAnnotations(kind); got != nil {
		t.Fatalf("Kind runner unexpectedly received AWS annotations = %#v", got)
	}
}
