package terraform

import (
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
)

func TestBuildJobEnforcesTerraformJobContract(t *testing.T) {
	parallelism := int32(10)
	job, err := BuildJob(JobRequest{
		Name:              "plan-run",
		Namespace:         "platform-system",
		RunUID:            "run-uid",
		RunnerImage:       "registry.example/runner@sha256:runner",
		SourceType:        SourceGitCommit,
		SourceURL:         "https://example.invalid/platform.git",
		SourceRevision:    "0123456789012345678901234567890123456789",
		Operation:         "Plan",
		WorkingDir:        "/workspace/terraform",
		Workspace:         "staging",
		BackendConfigMap:  "backend-config",
		BackendConfigPath: "/workspace/backend/backend.hcl",
		PlanPath:          "/workspace/terraform/plan.binary",
		LockTimeout:       5 * time.Minute,
		ExecutionTimeout:  60 * time.Minute,
		Parallelism:       &parallelism,
		GitImage:          DefaultGitImage,
	})
	if err != nil {
		t.Fatalf("BuildJob() error = %v", err)
	}
	if *job.Spec.BackoffLimit != 0 || *job.Spec.ActiveDeadlineSeconds != 3600 {
		t.Fatalf("job timeout contract = backoff %d deadline %d", *job.Spec.BackoffLimit, *job.Spec.ActiveDeadlineSeconds)
	}
	if job.Spec.Template.Spec.RestartPolicy != "Never" {
		t.Fatalf("restart policy = %q", job.Spec.Template.Spec.RestartPolicy)
	}
	if len(job.Spec.Template.Spec.InitContainers) != 2 {
		t.Fatalf("init containers = %d, want 2", len(job.Spec.Template.Spec.InitContainers))
	}
	if got := job.Spec.Template.Spec.InitContainers[1].Args[4]; got != "0123456789012345678901234567890123456789" {
		t.Fatalf("checkout revision = %q", got)
	}
	runner := job.Spec.Template.Spec.Containers[0]
	if runner.Command[0] != "terraform-runner" || runner.Args[0] != "--operation=Plan" {
		t.Fatalf("runner command = %#v args=%#v", runner.Command, runner.Args)
	}
	if runner.Args[len(runner.Args)-1] != "--parallelism=10" {
		t.Fatalf("runner args = %#v", runner.Args)
	}
}

func TestBuildJobBuildsRetainedBundleWithoutGitInitContainers(t *testing.T) {
	job, err := BuildJob(JobRequest{
		Name:               "destroy-run",
		Namespace:          "platform-system",
		RunUID:             "run-uid",
		RunnerImage:        "registry.example/runner@sha256:runner",
		SourceType:         SourceRetainedBundle,
		SourceBundleRef:    "runs/previous/source-bundle.tar.zst",
		SourceBundleDigest: "sha256:bundle",
		ArtifactEndpoint:   "http://localstack:4566",
		ArtifactRegion:     "us-east-1",
		ArtifactBucket:     "artifacts",
		ArtifactPrefix:     "runs/run-uid",
		VariableSecretRefs: []corev1.LocalObjectReference{{Name: "terraform-vars"}},
		Operation:          "Plan",
		WorkingDir:         "/workspace/terraform",
		Workspace:          "default",
		PlanPath:           "/workspace/terraform/plan.binary",
		ExecutionTimeout:   time.Minute,
		GitImage:           DefaultGitImage,
	})
	if err != nil {
		t.Fatalf("BuildJob() error = %v", err)
	}
	if len(job.Spec.Template.Spec.InitContainers) != 0 {
		t.Fatalf("retained bundle init containers = %d, want 0", len(job.Spec.Template.Spec.InitContainers))
	}
	args := job.Spec.Template.Spec.Containers[0].Args
	if !contains(args, "--source-bundle-ref=runs/previous/source-bundle.tar.zst") {
		t.Fatalf("runner args = %#v", args)
	}
	if !contains(args, "--source-root=/workspace/terraform") {
		t.Fatalf("runner source root args = %#v", args)
	}
	if !contains(args, "--artifact-prefix=runs/run-uid") {
		t.Fatalf("runner artifact prefix args = %#v", args)
	}
	if len(job.Spec.Template.Spec.Containers[0].EnvFrom) != 1 || job.Spec.Template.Spec.Containers[0].EnvFrom[0].SecretRef.Name != "terraform-vars" {
		t.Fatalf("runner secret env = %#v", job.Spec.Template.Spec.Containers[0].EnvFrom)
	}
}

func TestBuildJobPassesBackendArtifactRestorePath(t *testing.T) {
	job, err := BuildJob(JobRequest{
		Name:                        "plan-run",
		Namespace:                   "platform-system",
		RunUID:                      "run-uid",
		RunnerImage:                 "registry.example/runner@sha256:runner",
		SourceType:                  SourceRetainedBundle,
		SourceBundleRef:             "runs/source-bundle.tar.zst",
		SourceBundleDigest:          "sha256:bundle",
		ArtifactEndpoint:            "http://localstack:4566",
		ArtifactRegion:              "us-east-1",
		ArtifactBucket:              "artifacts",
		BackendConfigArtifactRef:    "runs/backend-config.hcl",
		BackendConfigArtifactDigest: "sha256:backend",
		BackendConfigPath:           "/workspace/backend/backend.hcl",
		Operation:                   "Plan",
		WorkingDir:                  "/workspace/terraform",
		Workspace:                   "default",
		PlanPath:                    "/workspace/terraform/plan.binary",
		ExecutionTimeout:            time.Minute,
	})
	if err != nil {
		t.Fatalf("BuildJob() error = %v", err)
	}
	args := job.Spec.Template.Spec.Containers[0].Args
	if !contains(args, "--backend-config=/workspace/backend/backend.hcl") {
		t.Fatalf("runner backend path args = %#v", args)
	}
	if !contains(args, "--backend-config-ref=runs/backend-config.hcl") {
		t.Fatalf("runner backend ref args = %#v", args)
	}
}

func TestBuildJobRejectsDestroyFromGitCommit(t *testing.T) {
	_, err := BuildJob(JobRequest{
		Name:             "destroy-run",
		Namespace:        "platform-system",
		RunUID:           "run-uid",
		RunnerImage:      "registry.example/runner@sha256:runner",
		SourceType:       SourceGitCommit,
		SourceURL:        "https://example.invalid/platform.git",
		SourceRevision:   "0123456789012345678901234567890123456789",
		Operation:        "Plan",
		Destroy:          true,
		WorkingDir:       "/workspace/terraform",
		Workspace:        "default",
		PlanPath:         "/workspace/terraform/plan.binary",
		ExecutionTimeout: time.Minute,
		GitImage:         DefaultGitImage,
	})
	if err == nil {
		t.Fatal("Destroy from GitCommit must be rejected")
	}
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
