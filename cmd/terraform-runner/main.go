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

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	"github.com/miku-wwl/kube-platform-control-plane/internal/artifacts"
	"github.com/miku-wwl/kube-platform-control-plane/internal/terraform"
)

type terminalResult struct {
	RunUID                   string    `json:"terraformRunUID"`
	JobUID                   string    `json:"jobUID,omitempty"`
	StartedAt                time.Time `json:"startedAt"`
	FinishedAt               time.Time `json:"finishedAt"`
	Operation                string    `json:"operation"`
	TerraformExitCode        int       `json:"terraformExitCode"`
	ExecutionOutcome         string    `json:"executionOutcome"`
	MutationClassification   string    `json:"mutationClassification,omitempty"`
	MutationMayHaveOccurred  bool      `json:"mutationMayHaveOccurred"`
	ArtifactsReady           bool      `json:"artifactsReady"`
	HasChanges               bool      `json:"hasChanges"`
	Error                    string    `json:"error,omitempty"`
	PlanRef                  string    `json:"planRef,omitempty"`
	PlanDigest               string    `json:"planDigest,omitempty"`
	SourceBundleRef          string    `json:"sourceBundleRef,omitempty"`
	SourceBundleDigest       string    `json:"sourceBundleDigest,omitempty"`
	BackendConfigRef         string    `json:"backendConfigRef,omitempty"`
	BackendConfigDigest      string    `json:"backendConfigDigest,omitempty"`
	PlanReportRef            string    `json:"planReportRef,omitempty"`
	PlanReportDigest         string    `json:"planReportDigest,omitempty"`
	TerminalResultRef        string    `json:"terminalResultRef,omitempty"`
	EffectivePlanInputDigest string    `json:"effectivePlanInputDigest,omitempty"`
	TargetDiscoveryRef       string    `json:"targetDiscoveryRef,omitempty"`
	TargetDiscoveryDigest    string    `json:"targetDiscoveryDigest,omitempty"`
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
	var runUID string
	var expectedVersion string

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
	flag.StringVar(&runUID, "run-uid", "", "immutable TerraformRun UID")
	flag.StringVar(&expectedVersion, "expected-terraform-version", "", "expected Terraform version")
	flag.Parse()
	startedAt := time.Now().UTC()

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
	jobUID, err := resolveJobUID(ctx)
	if err != nil {
		emit(terminalResult{RunUID: runUID, Operation: operation, StartedAt: startedAt, FinishedAt: time.Now().UTC(), ExecutionOutcome: "Failed", MutationClassification: "FailedPreMutation", Error: err.Error()})
		os.Exit(2)
	}
	executor := terraform.Executor{Runner: terraform.OSCommandRunner{Binary: terraformBinary}}
	store, storeErr := buildArtifactStore(artifactEndpoint, artifactRegion, artifactBucket)
	if storeErr != nil {
		emit(terminalResult{RunUID: runUID, JobUID: jobUID, Operation: operation, StartedAt: startedAt, FinishedAt: time.Now().UTC(), ExecutionOutcome: "Failed", MutationClassification: "FailedPreMutation", Error: storeErr.Error()})
		os.Exit(2)
	}
	if err := prepareArtifacts(ctx, store, request, sourceRoot, sourceBundleRef, sourceBundleDigest, planRef, planDigest, backendConfigRef, backendConfigDigest); err != nil {
		terminal := terminalResult{RunUID: runUID, JobUID: jobUID, Operation: operation, StartedAt: startedAt, FinishedAt: time.Now().UTC(), ExecutionOutcome: "Failed", MutationClassification: "FailedPreMutation", Error: err.Error()}
		_ = persistTerminalResult(ctx, store, artifactPrefix, &terminal)
		emit(terminal)
		os.Exit(2)
	}
	if err := executor.VerifyTerraformVersion(ctx, expectedVersion, request.WorkingDir); err != nil {
		terminal := terminalResult{RunUID: runUID, JobUID: jobUID, Operation: operation, StartedAt: startedAt, FinishedAt: time.Now().UTC(), ExecutionOutcome: "Failed", MutationClassification: "FailedPreMutation", Error: err.Error()}
		_ = persistTerminalResult(ctx, store, artifactPrefix, &terminal)
		emit(terminal)
		os.Exit(2)
	}
	switch operation {
	case "Plan":
		result, err := executor.Plan(ctx, request)
		terminal := terminalResult{
			RunUID:            runUID,
			JobUID:            jobUID,
			StartedAt:         startedAt,
			FinishedAt:        time.Now().UTC(),
			Operation:         operation,
			TerraformExitCode: result.ExitCode,
			ExecutionOutcome:  string(result.Outcome),
			HasChanges:        result.HasChanges,
			ArtifactsReady:    result.ExitCode == 0 || result.ExitCode == 2,
		}
		if err != nil {
			terminal.Error = err.Error()
		}
		terminal.MutationClassification = "NoMutation"
		if err == nil && store != nil {
			if uploadErr := uploadPlanArtifacts(ctx, store, artifactPrefix, request, sourceRoot, result, &terminal); uploadErr != nil {
				terminal.ArtifactsReady = false
				terminal.Error = uploadErr.Error()
			}
			if result.Outcome == terraform.PlanNoChange && terminal.ArtifactsReady {
				if uploadErr := uploadTargetDiscovery(ctx, store, artifactPrefix, request); uploadErr != nil {
					terminal.ArtifactsReady = false
					terminal.Error = uploadErr.Error()
				} else {
					terminal.TargetDiscoveryRef = artifactKey(artifactPrefix, "target-discovery.json")
					if ref, _, headErr := store.GetVerifiedByKey(ctx, terminal.TargetDiscoveryRef); headErr == nil {
						terminal.TargetDiscoveryDigest = ref.Digest
					}
				}
			}
		}
		terminal.EffectivePlanInputDigest = effectiveInputDigest(request, expectedVersion, terminal.SourceBundleDigest, terminal.BackendConfigDigest, terminal.PlanDigest)
		if result.PlanPath != "" {
			if _, statErr := os.Stat(result.PlanPath); statErr != nil {
				terminal.ArtifactsReady = false
			}
		}
		_ = persistTerminalResult(ctx, store, artifactPrefix, &terminal)
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
		terminal := terminalResult{
			RunUID: runUID, JobUID: jobUID, StartedAt: startedAt, FinishedAt: time.Now().UTC(),
			Operation:              operation,
			TerraformExitCode:      result.ExitCode,
			ExecutionOutcome:       outcome,
			ArtifactsReady:         result.Succeeded,
			Error:                  errorText(err),
			MutationClassification: "SucceededPostMutation",
			PlanRef:                planRef,
			PlanDigest:             planDigest,
			SourceBundleRef:        sourceBundleRef,
			SourceBundleDigest:     sourceBundleDigest,
			BackendConfigRef:       backendConfigRef,
			BackendConfigDigest:    backendConfigDigest,
		}
		if err != nil {
			terminal.MutationMayHaveOccurred = true
			terminal.MutationClassification = "FailedPostMutationPossible"
			if errors.Is(err, context.DeadlineExceeded) {
				terminal.MutationClassification = "Indeterminate"
			}
		}
		if result.Succeeded && store != nil {
			if uploadErr := uploadTargetDiscovery(ctx, store, artifactPrefix, request); uploadErr != nil {
				terminal.ArtifactsReady = false
				terminal.Error = uploadErr.Error()
			} else {
				terminal.TargetDiscoveryRef = artifactKey(artifactPrefix, "target-discovery.json")
				if ref, _, headErr := store.GetVerifiedByKey(ctx, terminal.TargetDiscoveryRef); headErr == nil {
					terminal.TargetDiscoveryDigest = ref.Digest
				}
			}
		}
		terminal.EffectivePlanInputDigest = effectiveInputDigest(request, expectedVersion, sourceBundleDigest, backendConfigDigest, planDigest)
		_ = persistTerminalResult(ctx, store, artifactPrefix, &terminal)
		emit(terminal)
		if exitCode != 0 {
			os.Exit(exitCode)
		}
	default:
		emit(terminalResult{Operation: operation, ExecutionOutcome: "Failed"})
		fmt.Fprintln(os.Stderr, "operation must be Plan or Apply")
		os.Exit(2)
	}
}

func resolveJobUID(ctx context.Context) (string, error) {
	config, err := rest.InClusterConfig()
	if err != nil {
		return "", fmt.Errorf("load in-cluster config for Job identity: %w", err)
	}
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return "", fmt.Errorf("create Kubernetes client for Job identity: %w", err)
	}
	podName := os.Getenv("PCP_POD_NAME")
	namespace := os.Getenv("PCP_POD_NAMESPACE")
	if podName == "" || namespace == "" {
		return "", fmt.Errorf("pod name and namespace are required for Job identity")
	}
	pod, err := clientset.CoreV1().Pods(namespace).Get(ctx, podName, metav1.GetOptions{})
	if err != nil {
		return "", fmt.Errorf("read runner Pod for Job identity: %w", err)
	}
	for _, owner := range pod.OwnerReferences {
		if owner.Controller != nil && *owner.Controller && owner.Kind == "Job" && owner.UID != "" {
			return string(owner.UID), nil
		}
	}
	return "", fmt.Errorf("runner Pod has no controller-owned Job identity")
}

func effectiveInputDigest(request terraform.Request, expectedVersion, sourceBundleDigest, backendDigest, planDigest string) string {
	digest, err := terraform.Digest(struct {
		SourceBundleDigest string
		BackendDigest      string
		PlanDigest         string
		TerraformVersion   string
		WorkingDir         string
		Workspace          string
		Destroy            bool
	}{sourceBundleDigest, backendDigest, planDigest, expectedVersion, request.WorkingDir, request.Workspace, request.Destroy})
	if err != nil {
		return ""
	}
	return digest
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
	report, err := buildPlanReport(ctx, request, result, planRef.Digest)
	if err != nil {
		return err
	}
	reportContent, err := json.Marshal(report)
	if err != nil {
		return fmt.Errorf("marshal plan report: %w", err)
	}
	reportRef, err := store.PutImmutable(ctx, artifactKey(prefix, "plan-report.json"), reportContent)
	if err != nil {
		return fmt.Errorf("upload plan report: %w", err)
	}
	terminal.PlanReportRef = reportRef.Key
	terminal.PlanReportDigest = reportRef.Digest
	if request.BackendConfigPath != "" {
		backendContent, err := os.ReadFile(request.BackendConfigPath)
		if err != nil {
			return fmt.Errorf("read backend snapshot: %w", err)
		}
		if err := terraform.ValidateBackendConfigContent(backendContent); err != nil {
			return fmt.Errorf("validate backend snapshot: %w", err)
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

func uploadTargetDiscovery(ctx context.Context, store *artifacts.Store, prefix string, request terraform.Request) error {
	if store == nil {
		return fmt.Errorf("target discovery requires artifact store")
	}
	command, err := (terraform.OSCommandRunner{Binary: "terraform"}).Run(ctx, request.WorkingDir, "output", "-json")
	if err != nil {
		return fmt.Errorf("terraform output discovery: %w", err)
	}
	if command.ExitCode != 0 {
		return fmt.Errorf("terraform output discovery failed: %s", command.Stderr)
	}
	sanitized, err := sanitizeTargetDiscovery([]byte(command.Stdout))
	if err != nil {
		return err
	}
	if _, err := store.PutImmutable(ctx, artifactKey(prefix, "target-discovery.json"), sanitized); err != nil {
		return fmt.Errorf("upload target discovery: %w", err)
	}
	return nil
}

func sanitizeTargetDiscovery(content []byte) ([]byte, error) {
	var values map[string]json.RawMessage
	if err := json.Unmarshal(content, &values); err != nil {
		return nil, fmt.Errorf("decode terraform target discovery output: %w", err)
	}
	allowed := map[string]struct{}{"provider": {}, "accountId": {}, "region": {}, "clusterArn": {}, "clusterName": {}, "incarnationId": {}, "endpoint": {}, "caCertificateDigest": {}, "authMode": {}, "kubeContext": {}}
	result := map[string]json.RawMessage{}
	for key, value := range values {
		if _, ok := allowed[key]; ok {
			result[key] = value
			continue
		}
		var wrapper struct {
			Value json.RawMessage `json:"value"`
		}
		if json.Unmarshal(value, &wrapper) == nil && len(wrapper.Value) > 0 {
			var nested map[string]json.RawMessage
			if json.Unmarshal(wrapper.Value, &nested) == nil {
				for nestedKey, nestedValue := range nested {
					if _, ok := allowed[nestedKey]; ok {
						result[nestedKey] = nestedValue
					}
				}
			}
		}
	}
	if len(result) == 0 {
		return nil, fmt.Errorf("terraform output does not contain an allowlisted target discovery object")
	}
	return json.Marshal(result)
}

type planReport struct {
	PlanDigest      string         `json:"planDigest"`
	Outcome         string         `json:"outcome"`
	HasChanges      bool           `json:"hasChanges"`
	ResourceActions map[string]int `json:"resourceActions,omitempty"`
	GeneratedAt     time.Time      `json:"generatedAt"`
}

func buildPlanReport(ctx context.Context, request terraform.Request, result terraform.PlanResult, planDigest string) (planReport, error) {
	show := terraform.OSCommandRunner{Binary: "terraform"}
	command, err := show.Run(ctx, request.WorkingDir, "show", "-json", request.PlanPath)
	if err != nil {
		return planReport{}, fmt.Errorf("terraform show plan report: %w", err)
	}
	if command.ExitCode != 0 {
		return planReport{}, fmt.Errorf("terraform show plan report failed: %s", command.Stderr)
	}
	var payload struct {
		ResourceChanges []struct {
			Change struct {
				Actions []string `json:"actions"`
			} `json:"change"`
		} `json:"resource_changes"`
	}
	if err := json.Unmarshal([]byte(command.Stdout), &payload); err != nil {
		return planReport{}, fmt.Errorf("parse terraform show plan report: %w", err)
	}
	actions := map[string]int{}
	for _, resource := range payload.ResourceChanges {
		for _, action := range resource.Change.Actions {
			actions[action]++
		}
	}
	return planReport{PlanDigest: planDigest, Outcome: string(result.Outcome), HasChanges: result.HasChanges, ResourceActions: actions, GeneratedAt: time.Now().UTC()}, nil
}

func persistTerminalResult(ctx context.Context, store *artifacts.Store, prefix string, result *terminalResult) error {
	if store == nil || result == nil {
		return nil
	}
	result.TerminalResultRef = artifactKey(prefix, "terminal-result.json")
	content, err := json.Marshal(result)
	if err != nil {
		return err
	}
	_, err = store.PutImmutable(ctx, result.TerminalResultRef, content)
	return err
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
		if err := os.MkdirAll(sourceRoot, 0o755); err != nil {
			return fmt.Errorf("create source root: %w", err)
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
