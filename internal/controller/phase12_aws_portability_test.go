package controller

import (
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	platformv1alpha1 "github.com/miku-wwl/kube-platform-control-plane/api/v1alpha1"
)

func TestAWSNativeMaterializationPathBuildsStackAndTerraformJobOffline(t *testing.T) {
	class := &platformv1alpha1.EnvironmentClass{
		ObjectMeta: metav1.ObjectMeta{Name: "aws-native"},
		Spec: platformv1alpha1.EnvironmentClassSpec{
			Source:        platformv1alpha1.SourceSpec{URL: "https://example.invalid/platform.git", Revision: "0123456789012345678901234567890123456789"},
			Backend:       platformv1alpha1.BackendSpec{Type: "s3", ConfigRef: corev1.LocalObjectReference{Name: "backend"}, AuthRef: platformv1alpha1.ServiceAccountReference{ServiceAccountName: "runner"}},
			Executor:      platformv1alpha1.ExecutorSpec{Image: "registry.invalid/terraform-runner@sha256:runner", TerraformVersion: "1.9.0", WorkDir: "/workspace/terraform", ExecutionTimeout: metav1.Duration{Duration: time.Minute}},
			RunnerProfile: platformv1alpha1.RunnerProfileSpec{ServiceAccountName: "runner", Image: "registry.invalid/terraform-runner@sha256:runner", TerraformVersion: "1.9.0"},
			Target:        platformv1alpha1.TargetReference{Provider: "aws", Account: "123456789012", Region: "us-east-1", ClusterName: "platform", ClusterARN: "arn:aws:eks:us-east-1:123456789012:cluster/platform", ExecutionRoleARN: "arn:aws:iam::123456789012:role/infra", RuntimeRoleARN: "arn:aws:iam::123456789012:role/runtime"},
		},
	}
	environment := &platformv1alpha1.PlatformEnvironment{ObjectMeta: metav1.ObjectMeta{Name: "env", Namespace: "tenant-a"}}
	stack, err := buildClassInfraStack(environment, class, "env-infra", class.Spec.Target)
	if err != nil {
		t.Fatalf("buildClassInfraStack() error = %v", err)
	}
	if stack.Spec.InfrastructureExecutionIdentity.Mode != "aws-native" || stack.Spec.InfrastructureExecutionIdentity.RoleARN != class.Spec.Target.ExecutionRoleARN {
		t.Fatalf("stack infrastructure identity = %+v", stack.Spec.InfrastructureExecutionIdentity)
	}
	if stack.Spec.RuntimeTargetIdentity.AccountID != "123456789012" || stack.Spec.TargetConnectionProfile.AuthMode != "aws-eks" || stack.Spec.TargetConnectionProfile.Endpoint != "" {
		t.Fatalf("stack AWS target materialization = identity=%+v profile=%+v", stack.Spec.RuntimeTargetIdentity, stack.Spec.TargetConnectionProfile)
	}

	run := buildPlanRun(stack, "env-plan-1")
	run.UID = "plan-run-uid"
	job, err := (&TerraformRunReconciler{ArtifactRegion: "us-east-1", ArtifactBucket: "native-artifacts"}).buildJob(run)
	if err != nil {
		t.Fatalf("buildJob() error = %v", err)
	}
	runner := job.Spec.Template.Spec.Containers[0]
	if job.Spec.Template.Spec.ServiceAccountName != "runner" || !containsTerraformArg(runner.Args, "--artifact-region=us-east-1") || !containsTerraformArg(runner.Args, "--artifact-bucket=native-artifacts") {
		t.Fatalf("AWS-native Terraform Job identity/artifact args = serviceAccount=%q args=%#v", job.Spec.Template.Spec.ServiceAccountName, runner.Args)
	}
	approval := &platformv1alpha1.ChangeApproval{ObjectMeta: metav1.ObjectMeta{Name: "approval", UID: "approval-uid"}}
	run.Status.SourceBundleRef = "runs/plan/source-bundle.tar.zst"
	run.Status.SourceBundleDigest = "sha256:bundle"
	run.Status.PlanRef = "runs/plan/plan.binary"
	run.Status.PlanDigest = "sha256:plan"
	apply := buildApplyRun(stack, run, approval, "env-apply")
	if apply.Spec.Source.Path != class.Spec.Source.Path {
		t.Fatalf("retained Apply source path = %q, want %q", apply.Spec.Source.Path, class.Spec.Source.Path)
	}
	destroy := buildDestroyPlanRun(stack, apply, "env-destroy")
	if destroy.Spec.Source.Path != class.Spec.Source.Path {
		t.Fatalf("retained Destroy source path = %q, want %q", destroy.Spec.Source.Path, class.Spec.Source.Path)
	}
	for _, env := range runner.Env {
		if (env.Name == "AWS_ACCESS_KEY_ID" || env.Name == "AWS_SECRET_ACCESS_KEY") && env.Value == "test" {
			t.Fatalf("AWS-native Terraform Job received synthetic credentials: %#v", runner.Env)
		}
	}
}

func containsTerraformArg(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
