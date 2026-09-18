package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path"
	"time"

	"github.com/miku-wwl/kube-platform-control-plane/internal/artifacts"
	"github.com/miku-wwl/kube-platform-control-plane/internal/terraform"
)

type terminalResult struct {
	Operation           string `json:"operation"`
	TerraformExitCode   int    `json:"terraformExitCode"`
	ExecutionOutcome    string `json:"executionOutcome"`
	ArtifactsReady      bool   `json:"artifactsReady"`
	HasChanges          bool   `json:"hasChanges"`
	Error               string `json:"error,omitempty"`
	PlanRef             string `json:"planRef,omitempty"`
	PlanDigest          string `json:"planDigest,omitempty"`
	SourceBundleRef     string `json:"sourceBundleRef,omitempty"`
	SourceBundleDigest  string `json:"sourceBundleDigest,omitempty"`
	BackendConfigRef    string `json:"backendConfigRef,omitempty"`
	BackendConfigDigest string `json:"backendConfigDigest,omitempty"`
}

func main() {
	var operation string
	var workingDir string
	var workspace string
	var backendConfig string
	var planPath string
	var terraformBinary string
	var lockTimeout time.Duration
	var parallelism int
	var destroy bool
	var artifactEndpoint string
	var artifactRegion string
	var artifactBucket string
	var artifactPrefix string
	var sourceRoot string
	var sourceBundleRef string
	var sourceBundleDigest string
	var planRef string
	var planDigest string
	var backendConfigRef string
	var backendConfigDigest string

	flag.StringVar(&operation, "operation", "", "Terraform operation: Plan or Apply")
	flag.StringVar(&workingDir, "working-dir", "/workspace/terraform", "absolute Terraform working directory")
	flag.StringVar(&workspace, "workspace", "default", "Terraform workspace")
	flag.StringVar(&backendConfig, "backend-config", "", "resolved non-secret backend config path")
	flag.StringVar(&planPath, "plan", "plan.binary", "saved plan path")
	flag.StringVar(&terraformBinary, "terraform-binary", "terraform", "Terraform executable")
	flag.DurationVar(&lockTimeout, "lock-timeout", 0, "Terraform backend lock timeout")
	flag.IntVar(&parallelism, "parallelism", 0, "optional Terraform resource parallelism")
	flag.BoolVar(&destroy, "destroy", false, "create a Terraform destroy plan")
	flag.StringVar(&artifactEndpoint, "artifact-endpoint", "", "S3-compatible artifact endpoint")
	flag.StringVar(&artifactRegion, "artifact-region", "us-east-1", "artifact bucket region")
	flag.StringVar(&artifactBucket, "artifact-bucket", "", "artifact bucket")
	flag.StringVar(&artifactPrefix, "artifact-prefix", "", "immutable artifact key prefix")
	flag.StringVar(&sourceRoot, "source-root", "", "pristine source root for source bundle creation")
	flag.StringVar(&sourceBundleRef, "source-bundle-ref", "", "immutable source bundle object key")
	flag.StringVar(&sourceBundleDigest, "source-bundle-digest", "", "source bundle digest")
	flag.StringVar(&planRef, "plan-ref", "", "immutable saved plan object key")
	flag.StringVar(&planDigest, "plan-digest", "", "saved plan digest")
	flag.StringVar(&backendConfigRef, "backend-config-ref", "", "immutable backend snapshot object key")
	flag.StringVar(&backendConfigDigest, "backend-config-digest", "", "backend snapshot digest")
	flag.Parse()

	request := terraform.Request{
		WorkingDir:        workingDir,
		Workspace:         workspace,
		BackendConfigPath: backendConfig,
		PlanPath:          planPath,
		LockTimeout:       lockTimeout,
		Destroy:           destroy,
	}
	if parallelism > 0 {
		value := int32(parallelism)
		request.Parallelism = &value
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	executor := terraform.Executor{Runner: terraform.OSCommandRunner{Binary: terraformBinary}}
	store, storeErr := buildArtifactStore(artifactEndpoint, artifactRegion, artifactBucket)
	if storeErr != nil {
		emit(terminalResult{Operation: operation, ExecutionOutcome: "Failed", Error: storeErr.Error()})
		os.Exit(2)
	}
	if err := prepareArtifacts(ctx, store, request, sourceRoot, sourceBundleRef, sourceBundleDigest, planRef, planDigest, backendConfigRef, backendConfigDigest); err != nil {
		emit(terminalResult{Operation: operation, ExecutionOutcome: "Failed", Error: err.Error()})
		os.Exit(2)
	}
	switch operation {
	case "Plan":
		result, err := executor.Plan(ctx, request)
		terminal := terminalResult{
			Operation:         operation,
			TerraformExitCode: result.ExitCode,
			ExecutionOutcome:  string(result.Outcome),
			HasChanges:        result.HasChanges,
			ArtifactsReady:    result.ExitCode == 0 || result.ExitCode == 2,
		}
		if err != nil {
			terminal.Error = err.Error()
		}
		if err == nil && store != nil && result.HasChanges {
			if uploadErr := uploadPlanArtifacts(ctx, store, artifactPrefix, request, sourceRoot, result, &terminal); uploadErr != nil {
				terminal.ArtifactsReady = false
				terminal.Error = uploadErr.Error()
			}
		}
		if result.PlanPath != "" {
			if _, statErr := os.Stat(result.PlanPath); statErr != nil {
				terminal.ArtifactsReady = false
			}
		}
		emit(terminal)
		if err != nil {
			os.Exit(1)
		}
	case "Apply":
		result, err := executor.Apply(ctx, request)
		outcome := "Succeeded"
		exitCode := 0
		if err != nil {
			exitCode = 1
			outcome = "Failed"
			if errors.Is(err, context.DeadlineExceeded) {
				outcome = "Indeterminate"
			}
		}
		emit(terminalResult{
			Operation:         operation,
			TerraformExitCode: result.ExitCode,
			ExecutionOutcome:  outcome,
			ArtifactsReady:    result.Succeeded,
			Error:             errorText(err),
		})
		if exitCode != 0 {
			os.Exit(exitCode)
		}
	default:
		emit(terminalResult{Operation: operation, ExecutionOutcome: "Failed"})
		fmt.Fprintln(os.Stderr, "operation must be Plan or Apply")
		os.Exit(2)
	}
}

func buildArtifactStore(endpoint, region, bucket string) (*artifacts.Store, error) {
	if endpoint == "" && bucket == "" {
		return nil, nil
	}
	if endpoint == "" || bucket == "" {
		return nil, fmt.Errorf("artifact endpoint and bucket must be provided together")
	}
	return artifacts.NewS3Store(endpoint, region, bucket)
}

func uploadPlanArtifacts(ctx context.Context, store *artifacts.Store, prefix string, request terraform.Request, sourceRoot string, result terraform.PlanResult, terminal *terminalResult) error {
	if _, err := os.Stat(result.PlanPath); err != nil {
		return fmt.Errorf("plan artifact is unavailable: %w", err)
	}
	planContent, err := os.ReadFile(result.PlanPath)
	if err != nil {
		return fmt.Errorf("read plan artifact: %w", err)
	}
	planRef, err := store.PutImmutable(ctx, artifactKey(prefix, "plan.binary"), planContent)
	if err != nil {
		return fmt.Errorf("upload plan artifact: %w", err)
	}
	terminal.PlanRef = planRef.Key
	terminal.PlanDigest = planRef.Digest
	if request.BackendConfigPath != "" {
		backendContent, err := os.ReadFile(request.BackendConfigPath)
		if err != nil {
			return fmt.Errorf("read backend snapshot: %w", err)
		}
		backendRef, err := store.PutImmutable(ctx, artifactKey(prefix, "backend-config.hcl"), backendContent)
		if err != nil {
			return fmt.Errorf("upload backend snapshot: %w", err)
		}
		terminal.BackendConfigRef = backendRef.Key
		terminal.BackendConfigDigest = backendRef.Digest
	}
	if sourceRoot != "" {
		bundle, err := terraform.BuildSourceBundle(sourceRoot)
		if err != nil {
			return fmt.Errorf("build source bundle: %w", err)
		}
		bundleRef, err := store.PutImmutable(ctx, artifactKey(prefix, "source-bundle.tar.zst"), bundle.Content)
		if err != nil {
			return fmt.Errorf("upload source bundle: %w", err)
		}
		terminal.SourceBundleRef = bundleRef.Key
		terminal.SourceBundleDigest = bundleRef.Digest
	}
	return nil
}

func artifactKey(prefix, name string) string {
	return path.Join(prefix, name)
}

func prepareArtifacts(ctx context.Context, store *artifacts.Store, request terraform.Request, sourceRoot, sourceBundleRef, sourceBundleDigest, planRef, planDigest, backendConfigRef, backendConfigDigest string) error {
	if sourceBundleRef != "" || planRef != "" || backendConfigRef != "" {
		if store == nil {
			return fmt.Errorf("artifact store is required to restore immutable artifacts")
		}
	}
	if sourceBundleRef != "" {
		if sourceRoot == "" || sourceBundleDigest == "" {
			return fmt.Errorf("source root and source bundle digest are required")
		}
		content, err := store.GetVerified(ctx, artifacts.Ref{Key: sourceBundleRef, Digest: sourceBundleDigest})
		if err != nil {
			return fmt.Errorf("download source bundle: %w", err)
		}
		if _, err := terraform.RestoreSourceBundle(content, sourceRoot); err != nil {
			return fmt.Errorf("restore source bundle: %w", err)
		}
	}
	if planRef != "" {
		if planDigest == "" {
			return fmt.Errorf("plan digest is required")
		}
		content, err := store.GetVerified(ctx, artifacts.Ref{Key: planRef, Digest: planDigest})
		if err != nil {
			return fmt.Errorf("download saved plan: %w", err)
		}
		if err := writeImmutableFile(request.PlanPath, content); err != nil {
			return fmt.Errorf("restore saved plan: %w", err)
		}
	}
	if backendConfigRef != "" {
		if request.BackendConfigPath == "" || backendConfigDigest == "" {
			return fmt.Errorf("backend config path and digest are required")
		}
		content, err := store.GetVerified(ctx, artifacts.Ref{Key: backendConfigRef, Digest: backendConfigDigest})
		if err != nil {
			return fmt.Errorf("download backend snapshot: %w", err)
		}
		if err := writeImmutableFile(request.BackendConfigPath, content); err != nil {
			return fmt.Errorf("restore backend snapshot: %w", err)
		}
	}
	return nil
}

func writeImmutableFile(filePath string, content []byte) error {
	if existing, err := os.ReadFile(filePath); err == nil {
		actual, digestErr := terraform.Digest(existing)
		if digestErr != nil {
			return digestErr
		}
		expected, digestErr := terraform.Digest(content)
		if digestErr != nil {
			return digestErr
		}
		if actual != expected {
			return fmt.Errorf("refusing to overwrite existing file %q", filePath)
		}
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := os.MkdirAll(path.Dir(filePath), 0o755); err != nil {
		return err
	}
	return os.WriteFile(filePath, content, 0o600)
}

func errorText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func emit(result terminalResult) {
	encoded, err := json.Marshal(result)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return
	}
	fmt.Println(string(encoded))
}
