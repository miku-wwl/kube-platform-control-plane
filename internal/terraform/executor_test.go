package terraform

import (
	"context"
	platform "github.com/miku-wwl/kube-platform-control-plane/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

type fakeRunner struct {
	commands []string
	results  []CommandResult
}

func TestFixedModuleAndBackendFailClosed(t *testing.T) {
	dir := filepath.Join("..", "..", "test", "fixtures", "terraform", "aws-compatible-basic")
	profile := &platform.ExecutionProfile{Mode: "emulator", S3Endpoint: "http://emulator.invalid:4566", STSEndpoint: "http://emulator.invalid:4566", Region: "us-east-1", AccountID: "000000000000", ArtifactBucket: "evidence", CredentialsSecretRef: corev1.LocalObjectReference{Name: "dummy"}}
	inputs, err := BindModuleInputs(map[string]string{"bucket_name": "owned", "target_cluster_name": "target", "target_endpoint": "kubeconfig:target", "target_incarnation_id": "namespace-uid"}, profile, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateModule(dir, "1.14.0", inputs); err != nil {
		t.Fatal(err)
	}
	inputs["password"] = "inline"
	if ValidateModule(dir, "1.14.0", inputs) == nil {
		t.Fatal("undeclared credential input accepted")
	}
	delete(inputs, "password")
	if ValidateModule(dir, "1.15.5", inputs) == nil {
		t.Fatal("unapproved Terraform version accepted")
	}
	inputs["s3_endpoint"] = "https://s3.amazonaws.com"
	if _, err := BindModuleInputs(inputs, profile, ""); err == nil {
		t.Fatal("user input overrode endpoint authority")
	}
	backend := []byte(`bucket = "state"
key = "owned/state"
region = "us-east-1"
use_lockfile = true
use_path_style = true
endpoints = { s3 = "http://emulator.invalid:4566", sts = "http://emulator.invalid:4566" }
`)
	if err := ValidateBackendProfile(backend, profile); err != nil {
		t.Fatal(err)
	}
	for _, unsafe := range []string{strings.Replace(string(backend), "use_lockfile = true", "use_lockfile = false", 1), strings.Replace(string(backend), "http://emulator.invalid:4566", "https://s3.amazonaws.com", 1), string(backend) + "access_key = \"inline\"\n"} {
		if ValidateBackendProfile([]byte(unsafe), profile) == nil {
			t.Fatal("unsafe backend accepted")
		}
	}
}

func TestExecutionRoleUsesFixedInputWithoutGeneratingSource(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "main.tf")
	original := []byte("# fixed versioned source\n")
	if err := os.WriteFile(source, original, 0600); err != nil {
		t.Fatal(err)
	}
	cleanup, err := prepareExecutionIdentity(dir, "arn:aws:iam::123456789012:role/platform/prod/terraform-runner")
	if err != nil {
		t.Fatal(err)
	}
	cleanup()
	entries, _ := os.ReadDir(dir)
	content, _ := os.ReadFile(source)
	if len(entries) != 1 || string(content) != string(original) {
		t.Fatal("execution changed fixed HCL source")
	}
	cleanup, err = prepareExecutionIdentity(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	cleanup()
}

func TestExecutionRoleRejectsUnsafeRoleARN(t *testing.T) {
	if _, err := prepareExecutionIdentity(t.TempDir(), "not-an-arn"); err == nil {
		t.Fatal("invalid execution role ARN was accepted")
	}
}

func (f *fakeRunner) Run(_ context.Context, _ string, args ...string) (CommandResult, error) {
	f.commands = append(f.commands, joinArgs(args))
	result := f.results[0]
	f.results = f.results[1:]
	return result, nil
}

func joinArgs(args []string) string {
	result := ""
	for index, arg := range args {
		if index > 0 {
			result += " "
		}
		result += arg
	}
	return result
}

func TestPlanUsesInitWorkspaceAndDetailedExitCode(t *testing.T) {
	parallelism := int32(4)
	runner := &fakeRunner{results: []CommandResult{{ExitCode: 0}, {ExitCode: 1}, {ExitCode: 0}, {ExitCode: 2, Stdout: "changes"}}}
	executor := Executor{Runner: runner}
	result, err := executor.Plan(context.Background(), Request{
		WorkingDir:  "/workspace/terraform",
		Workspace:   "staging",
		PlanPath:    "/workspace/terraform/plan.binary",
		LockTimeout: 30 * time.Second,
		Parallelism: &parallelism,
	})
	if err != nil {
		t.Fatalf("Plan() error = %v", err)
	}
	if result.Outcome != PlanChanges || !result.HasChanges || result.ExitCode != 2 {
		t.Fatalf("unexpected plan result: %+v", result)
	}
	want := []string{
		"init -input=false -no-color -lockfile readonly -lock-timeout 30s",
		"workspace select staging",
		"workspace new staging",
		"plan -input=false -no-color -detailed-exitcode -out /workspace/terraform/plan.binary -lock-timeout 30s -parallelism 4",
	}
	if !reflect.DeepEqual(runner.commands, want) {
		t.Fatalf("commands = %#v, want %#v", runner.commands, want)
	}
}

func TestPlanNoChangeIsFastPath(t *testing.T) {
	runner := &fakeRunner{results: []CommandResult{{ExitCode: 0}, {ExitCode: 0}, {ExitCode: 0}}}
	result, err := (Executor{Runner: runner}).Plan(context.Background(), Request{
		WorkingDir: "/workspace/terraform",
		Workspace:  "default",
		PlanPath:   "/workspace/terraform/plan.binary",
	})
	if err != nil {
		t.Fatalf("Plan() error = %v", err)
	}
	if result.Outcome != PlanNoChange || result.HasChanges {
		t.Fatalf("unexpected no-change result: %+v", result)
	}
}

func TestDestroyPlanUsesDestroyFlagBeforeExecution(t *testing.T) {
	runner := &fakeRunner{results: []CommandResult{{ExitCode: 0}, {ExitCode: 0}, {ExitCode: 2}}}
	_, err := (Executor{Runner: runner}).Plan(context.Background(), Request{
		WorkingDir: "/workspace/terraform",
		Workspace:  "default",
		PlanPath:   "/workspace/terraform/destroy.binary",
		Destroy:    true,
	})
	if err != nil {
		t.Fatalf("Plan() error = %v", err)
	}
	want := "plan -input=false -no-color -detailed-exitcode -out /workspace/terraform/destroy.binary -destroy"
	if runner.commands[len(runner.commands)-1] != want {
		t.Fatalf("destroy command = %q, want %q", runner.commands[len(runner.commands)-1], want)
	}
}

func TestApplyNeverCreatesWorkspaceOrReplans(t *testing.T) {
	runner := &fakeRunner{results: []CommandResult{{ExitCode: 0}, {ExitCode: 0}, {ExitCode: 0}}}
	result, err := (Executor{Runner: runner}).Apply(context.Background(), Request{
		WorkingDir:        "/workspace/terraform",
		Workspace:         "staging",
		BackendConfigPath: "/workspace/backend-config",
		PlanPath:          "/workspace/plan.binary",
		LockTimeout:       time.Minute,
	})
	if err != nil || !result.Succeeded {
		t.Fatalf("Apply() result=%+v err=%v", result, err)
	}
	want := []string{
		"init -input=false -no-color -lockfile readonly -lock-timeout 1m0s -backend-config /workspace/backend-config",
		"workspace select staging",
		"apply -input=false -no-color -lock-timeout 1m0s /workspace/plan.binary",
	}
	if !reflect.DeepEqual(runner.commands, want) {
		t.Fatalf("commands = %#v, want %#v", runner.commands, want)
	}
}

func TestApplyIncludesTerraformDiagnosticsOnFailure(t *testing.T) {
	runner := &fakeRunner{results: []CommandResult{{ExitCode: 0}, {ExitCode: 0}, {ExitCode: 1, Stderr: "saved plan is stale"}}}
	_, err := (Executor{Runner: runner}).Apply(context.Background(), Request{
		WorkingDir:        "/workspace/terraform",
		Workspace:         "staging",
		BackendConfigPath: "/workspace/backend-config",
		PlanPath:          "/workspace/plan.binary",
	})
	if err == nil || !strings.Contains(err.Error(), "saved plan is stale") {
		t.Fatalf("Apply() error = %v, want Terraform diagnostics", err)
	}
}

func TestApplyCreatesMissingWorkspaceWithoutReplanning(t *testing.T) {
	runner := &fakeRunner{results: []CommandResult{{ExitCode: 0}, {ExitCode: 1}, {ExitCode: 0}, {ExitCode: 0}}}
	result, err := (Executor{Runner: runner}).Apply(context.Background(), Request{
		WorkingDir:        "/workspace/terraform",
		Workspace:         "clean-state",
		BackendConfigPath: "/workspace/backend-config",
		PlanPath:          "/workspace/plan.binary",
	})
	if err != nil || !result.Succeeded {
		t.Fatalf("Apply() result=%+v err=%v", result, err)
	}
	want := []string{
		"init -input=false -no-color -lockfile readonly -backend-config /workspace/backend-config",
		"workspace select clean-state",
		"workspace new clean-state",
		"apply -input=false -no-color /workspace/plan.binary",
	}
	if !reflect.DeepEqual(runner.commands, want) {
		t.Fatalf("commands = %#v, want %#v", runner.commands, want)
	}
}

func TestVerifyTerraformVersionUsesMachineReadableOutput(t *testing.T) {
	runner := &fakeRunner{results: []CommandResult{{ExitCode: 0, Stdout: `{"terraform_version":"1.14.0"}`}}}
	if err := (Executor{Runner: runner}).VerifyTerraformVersion(context.Background(), "v1.14.0", "/workspace/terraform"); err != nil {
		t.Fatalf("VerifyTerraformVersion() error = %v", err)
	}
	if got := runner.commands[0]; got != "version -json" {
		t.Fatalf("command = %q, want version -json", got)
	}
}
