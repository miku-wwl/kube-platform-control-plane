package terraform

import (
	"strings"
	"testing"
	"time"
)

func TestJobDoesNotInferCloudCredentialsFromEndpoint(t *testing.T) {
	for _, endpoint := range []string{"", "http://localhost:4566", "http://ministack.example.invalid:4566", "https://s3.example.invalid"} {
		job, err := BuildJob(JobRequest{
			Name: "plan", Namespace: "test", RunUID: "run-uid", RunnerImage: "runner@sha256:fixture",
			SourceType: SourceGitCommit, SourceURL: "https://modules.example.invalid/fixed.git", SourceRevision: strings.Repeat("a", 40),
			GitImage: DefaultGitImage, Operation: "Plan", WorkingDir: "/workspace/terraform", Workspace: "owned",
			PlanPath: "/workspace/terraform/plan.binary", ExecutionTimeout: time.Minute,
			ArtifactEndpoint: endpoint, ArtifactRegion: "us-east-1", ArtifactBucket: "evidence",
		})
		if err != nil {
			t.Fatal(err)
		}
		runner := job.Spec.Template.Spec.Containers[0]
		for _, env := range runner.Env {
			if strings.HasPrefix(env.Name, "AWS_") {
				t.Fatal("endpoint implicitly injected AWS configuration", env.Name)
			}
		}
	}
}
