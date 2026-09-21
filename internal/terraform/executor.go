package terraform

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type CommandResult struct {
	ExitCode int
	Stdout   string
	Stderr   string
}

type CommandRunner interface {
	Run(ctx context.Context, workingDir string, args ...string) (CommandResult, error)
}

type OSCommandRunner struct {
	Binary string
}

func (r OSCommandRunner) Run(ctx context.Context, workingDir string, args ...string) (CommandResult, error) {
	command := exec.CommandContext(ctx, r.Binary, args...)
	command.Dir = workingDir
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	if err := command.Run(); err != nil {
		if ctx.Err() != nil {
			return CommandResult{}, ctx.Err()
		}
		if exitError, ok := err.(*exec.ExitError); ok {
			return CommandResult{ExitCode: exitError.ExitCode(), Stdout: stdout.String(), Stderr: stderr.String()}, nil
		}
		return CommandResult{}, err
	}
	return CommandResult{ExitCode: 0, Stdout: stdout.String(), Stderr: stderr.String()}, nil
}

type Request struct {
	WorkingDir                     string
	Workspace                      string
	BackendConfigPath              string
	PlanPath                       string
	Destroy                        bool
	LockTimeout                    time.Duration
	Parallelism                    *int32
	InfrastructureExecutionRoleARN string
}

type PlanOutcome string

const (
	PlanNoChange PlanOutcome = "NoChange"
	PlanChanges  PlanOutcome = "ChangesPresent"
	PlanFailed   PlanOutcome = "Failed"
)

type PlanResult struct {
	Outcome    PlanOutcome
	ExitCode   int
	Stdout     string
	Stderr     string
	PlanPath   string
	HasChanges bool
}

type ApplyResult struct {
	Succeeded bool
	ExitCode  int
	Stdout    string
	Stderr    string
}

type Executor struct {
	Runner CommandRunner
}

// VerifyTerraformVersion makes the executor fail closed before Plan or Apply
// when the runner image does not contain the version selected by the frozen
// EnvironmentClass.
func (e Executor) VerifyTerraformVersion(ctx context.Context, expected string, workingDir string) error {
	if expected == "" {
		return nil
	}
	result, err := e.run(ctx, workingDir, "version", "-json")
	if err != nil {
		return err
	}
	if result.ExitCode != 0 {
		return fmt.Errorf("terraform version verification failed with exit code %d: %s", result.ExitCode, strings.TrimSpace(result.Stderr))
	}
	var payload struct {
		TerraformVersion string `json:"terraform_version"`
	}
	if err := json.Unmarshal([]byte(result.Stdout), &payload); err != nil {
		return fmt.Errorf("parse terraform version -json: %w", err)
	}
	actual := strings.TrimPrefix(strings.TrimSpace(payload.TerraformVersion), "v")
	want := strings.TrimPrefix(strings.TrimSpace(expected), "v")
	if actual == "" || actual != want {
		return fmt.Errorf("terraform version mismatch: got %q, want %q", actual, want)
	}
	return nil
}

func (e Executor) Plan(ctx context.Context, request Request) (PlanResult, error) {
	if err := request.validate(true); err != nil {
		return PlanResult{}, err
	}
	cleanup, err := prepareExecutionProviderOverride(request.WorkingDir, request.InfrastructureExecutionRoleARN)
	if err != nil {
		return PlanResult{}, err
	}
	defer cleanup()
	if err := e.init(ctx, request, false); err != nil {
		return PlanResult{}, err
	}
	if err := e.selectWorkspace(ctx, request, true); err != nil {
		return PlanResult{}, err
	}

	args := []string{"plan", "-input=false", "-no-color", "-detailed-exitcode", "-out", request.PlanPath}
	if request.Destroy {
		args = append(args, "-destroy")
	}
	args = appendLockTimeout(args, request.LockTimeout)
	args = appendParallelism(args, request.Parallelism)
	result, err := e.run(ctx, request.WorkingDir, args...)
	if err != nil {
		return PlanResult{}, err
	}
	outcome := PlanFailed
	switch result.ExitCode {
	case 0:
		outcome = PlanNoChange
	case 2:
		outcome = PlanChanges
	default:
		return PlanResult{Outcome: outcome, ExitCode: result.ExitCode, Stdout: result.Stdout, Stderr: result.Stderr, PlanPath: request.PlanPath}, fmt.Errorf("terraform plan failed with exit code %d", result.ExitCode)
	}
	return PlanResult{
		Outcome:    outcome,
		ExitCode:   result.ExitCode,
		Stdout:     result.Stdout,
		Stderr:     result.Stderr,
		PlanPath:   request.PlanPath,
		HasChanges: result.ExitCode == 2,
	}, nil
}

func (e Executor) Apply(ctx context.Context, request Request) (ApplyResult, error) {
	if err := request.validate(false); err != nil {
		return ApplyResult{}, err
	}
	cleanup, err := prepareExecutionProviderOverride(request.WorkingDir, request.InfrastructureExecutionRoleARN)
	if err != nil {
		return ApplyResult{}, err
	}
	defer cleanup()
	if err := e.init(ctx, request, true); err != nil {
		return ApplyResult{}, err
	}
	// The plan Pod's local .terraform workspace marker is intentionally not
	// part of the source bundle. Recreate the named remote workspace when a
	// clean Apply Pod does not see it yet; the saved plan remains the only
	// mutation input and no re-plan is performed.
	if err := e.selectWorkspace(ctx, request, true); err != nil {
		return ApplyResult{}, err
	}

	args := []string{"apply", "-input=false", "-no-color"}
	args = appendLockTimeout(args, request.LockTimeout)
	args = appendParallelism(args, request.Parallelism)
	args = append(args, request.PlanPath)
	result, err := e.run(ctx, request.WorkingDir, args...)
	if err != nil {
		return ApplyResult{}, err
	}
	if result.ExitCode != 0 {
		detail := strings.TrimSpace(result.Stderr)
		if detail == "" {
			detail = strings.TrimSpace(result.Stdout)
		}
		if detail == "" {
			return ApplyResult{ExitCode: result.ExitCode, Stdout: result.Stdout, Stderr: result.Stderr}, fmt.Errorf("terraform apply failed with exit code %d", result.ExitCode)
		}
		return ApplyResult{ExitCode: result.ExitCode, Stdout: result.Stdout, Stderr: result.Stderr}, fmt.Errorf("terraform apply failed with exit code %d: %s", result.ExitCode, detail)
	}
	return ApplyResult{Succeeded: true, ExitCode: 0, Stdout: result.Stdout, Stderr: result.Stderr}, nil
}

const executionProviderOverrideFile = "zz_platform_execution_override.tf"

// prepareExecutionProviderOverride keeps the Runner Pod on its management
// workload identity while making Terraform's AWS provider assume the target
// infrastructure role. The file is ephemeral, excluded by source bundling,
// and contains no credentials.
func prepareExecutionProviderOverride(workingDir, roleARN string) (func(), error) {
	if roleARN == "" {
		return func() {}, nil
	}
	if !strings.HasPrefix(roleARN, "arn:") || strings.ContainsAny(roleARN, "\"\r\n") {
		return nil, fmt.Errorf("infrastructure execution role ARN is invalid")
	}
	file := filepath.Join(workingDir, executionProviderOverrideFile)
	if _, err := os.Stat(file); err == nil {
		return nil, fmt.Errorf("infrastructure execution provider override already exists")
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("check infrastructure execution provider override: %w", err)
	}
	content := fmt.Sprintf("provider \"aws\" {\n  assume_role {\n    role_arn = %q\n  }\n}\n", roleARN)
	if err := os.WriteFile(file, []byte(content), 0o600); err != nil {
		return nil, fmt.Errorf("write infrastructure execution provider override: %w", err)
	}
	return func() { _ = os.Remove(file) }, nil
}

func (e Executor) init(ctx context.Context, request Request, readonlyLockfile bool) error {
	args := []string{"init", "-input=false", "-no-color"}
	if readonlyLockfile {
		args = append(args, "-lockfile", "readonly")
	}
	args = appendLockTimeout(args, request.LockTimeout)
	if request.BackendConfigPath != "" {
		args = append(args, "-backend-config", request.BackendConfigPath)
	}
	result, err := e.run(ctx, request.WorkingDir, args...)
	if err != nil {
		return err
	}
	if result.ExitCode != 0 {
		return fmt.Errorf("terraform init failed with exit code %d: %s", result.ExitCode, result.Stderr)
	}
	return nil
}

func (e Executor) selectWorkspace(ctx context.Context, request Request, allowCreate bool) error {
	result, err := e.run(ctx, request.WorkingDir, "workspace", "select", request.Workspace)
	if err != nil {
		return err
	}
	if result.ExitCode == 0 {
		return nil
	}
	if !allowCreate {
		return fmt.Errorf("terraform workspace select failed with exit code %d: %s", result.ExitCode, result.Stderr)
	}
	result, err = e.run(ctx, request.WorkingDir, "workspace", "new", request.Workspace)
	if err != nil {
		return err
	}
	if result.ExitCode != 0 {
		return fmt.Errorf("terraform workspace new failed with exit code %d: %s", result.ExitCode, result.Stderr)
	}
	return nil
}

func (e Executor) run(ctx context.Context, workingDir string, args ...string) (CommandResult, error) {
	if e.Runner == nil {
		return CommandResult{}, fmt.Errorf("terraform command runner is required")
	}
	return e.Runner.Run(ctx, workingDir, args...)
}

func (r Request) validate(plan bool) error {
	if r.WorkingDir == "" {
		return fmt.Errorf("working directory is required")
	}
	if r.Workspace == "" {
		return fmt.Errorf("workspace is required")
	}
	if plan && r.PlanPath == "" {
		return fmt.Errorf("plan path is required for plan")
	}
	if !plan && r.PlanPath == "" {
		return fmt.Errorf("saved plan path is required for apply")
	}
	if r.LockTimeout < 0 {
		return fmt.Errorf("lock timeout cannot be negative")
	}
	if r.Parallelism != nil && *r.Parallelism < 1 {
		return fmt.Errorf("terraform parallelism must be positive")
	}
	return nil
}

func appendLockTimeout(args []string, timeout time.Duration) []string {
	if timeout <= 0 {
		return args
	}
	return append(args, "-lock-timeout", timeout.String())
}

func appendParallelism(args []string, parallelism *int32) []string {
	if parallelism == nil {
		return args
	}
	return append(args, "-parallelism", strconv.FormatInt(int64(*parallelism), 10))
}
