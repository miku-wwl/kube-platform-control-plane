package terraform

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"
)

type fakeRunner struct {
	commands []string
	results  []CommandResult
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
		"init -input=false -no-color -lock-timeout 30s",
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

func TestVerifyTerraformVersionUsesMachineReadableOutput(t *testing.T) {
	runner := &fakeRunner{results: []CommandResult{{ExitCode: 0, Stdout: `{"terraform_version":"1.14.0"}`}}}
	if err := (Executor{Runner: runner}).VerifyTerraformVersion(context.Background(), "v1.14.0", "/workspace/terraform"); err != nil {
		t.Fatalf("VerifyTerraformVersion() error = %v", err)
	}
	if got := runner.commands[0]; got != "version -json" {
		t.Fatalf("command = %q, want version -json", got)
	}
}
