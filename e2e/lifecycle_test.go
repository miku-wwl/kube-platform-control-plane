package e2e

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	platform "github.com/miku-wwl/kube-platform-control-plane/api/v1alpha1"
	"github.com/miku-wwl/kube-platform-control-plane/internal/artifacts"
	"github.com/miku-wwl/kube-platform-control-plane/internal/cloud"
	"github.com/miku-wwl/kube-platform-control-plane/internal/console"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type configuration struct {
	TimeoutSeconds                                       int    // optional bounded integration context; Go test timeout must allow cleanup
	ControllerImage, ControllerKubeconfig                string // optional isolated two-replica Kubernetes controller lane
	TransportScopeFile                                   string // optional new test-owned path restricting an external transport to this run
	Kubeconfig, Context, ControllerBinary, RunnerImage   string
	HostEndpoint, PodEndpoint, SourceURL, SourceRevision string
	FaultFile                                            string // optional test-owned transport fault switch
	SecondContext                                        string // optional independent target in the supplied kubeconfig
	FinalSafetyFaultFile, FinalSafetySourceRevision      string // optional owned live-fault transport and alternate fixed fixture revision
	FinalSafetyRunnerImage                               string // optional second owned digest with the same runner executable
	FinalSafetyGitNamespace, FinalSafetyOwnerID          string
	FinalSafetyPartialRevision                           string
}

// The caller supplies Kubernetes, the emulator, a built controller and immutable
// runner/source identities. This suite creates no cluster or engine. The child
// controller is a real process; killing it tests durable recovery at the API.
func TestLifecycle(t *testing.T) {
	file := os.Getenv("KPCP_E2E_CONFIG")
	if file == "" {
		t.Skip("set KPCP_E2E_CONFIG to an explicit local integration configuration")
	}
	content, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	var cfg configuration
	if err := json.Unmarshal(content, &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.Context == "" || cfg.Kubeconfig == "" || (cfg.ControllerBinary == "" && cfg.ControllerImage == "") || !strings.Contains(cfg.RunnerImage, "@sha256:") || cfg.SourceURL == "" || len(cfg.SourceRevision) != 40 {
		t.Fatal("explicit contexts, binary and immutable source/image are required")
	}
	for _, endpoint := range []string{cfg.HostEndpoint, cfg.PodEndpoint} {
		if err := cloud.ValidateEmulatorEndpoint(endpoint); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("AWS_ACCESS_KEY_ID", "000000000000")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "test")
	t.Setenv("AWS_SESSION_TOKEN", "")
	timeout := 35 * time.Minute
	if cfg.TimeoutSeconds > 0 {
		timeout = time.Duration(cfg.TimeoutSeconds) * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	rules := clientcmd.NewDefaultClientConfigLoadingRules()
	rules.ExplicitPath = cfg.Kubeconfig
	rest, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(rules, &clientcmd.ConfigOverrides{CurrentContext: cfg.Context}).ClientConfig()
	if err != nil {
		t.Fatal(err)
	}
	rest.Timeout = 15 * time.Second
	scheme := runtime.NewScheme()
	_ = platform.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)
	_ = batchv1.AddToScheme(scheme)
	kube, err := client.New(rest, client.Options{Scheme: scheme})
	if err != nil {
		t.Fatal(err)
	}
	var existing platform.TerraformRunList
	if err := kube.List(ctx, &existing); err != nil {
		t.Fatal("install the supplied CRDs before the integration suite: ", err)
	}
	if len(existing.Items) != 0 {
		t.Fatal("use a dedicated management API without existing TerraformRuns for process-restart testing")
	}
	for _, list := range []client.ObjectList{&platform.PlatformEnvironmentList{}, &platform.InfraStackList{}, &platform.ResourceSetList{}, &platform.EnvironmentClassList{}} {
		if err := kube.List(ctx, list); err != nil {
			t.Fatal(err)
		}
		items, err := meta.ExtractList(list)
		if err != nil || len(items) != 0 {
			t.Fatal("use a dedicated management API without existing platform resources")
		}
	}
	prefix := fmt.Sprintf("kpcp-%d", time.Now().UnixNano())
	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: prefix, Labels: map[string]string{"platform.example.io/e2e": prefix}}}
	if err := kube.Create(ctx, ns); err != nil {
		t.Fatal(err)
	}
	t.Logf("owned namespace=%s uid=%s source=%s image=%s", ns.Name, ns.UID, cfg.SourceRevision, cfg.RunnerImage)
	if cfg.TransportScopeFile != "" {
		if !filepath.IsAbs(cfg.TransportScopeFile) {
			t.Fatal("TransportScopeFile must be a new absolute test-owned path")
		}
		file, err := os.OpenFile(cfg.TransportScopeFile, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := file.WriteString(prefix); err != nil {
			_ = file.Close()
			t.Fatal(err)
		}
		if err := file.Close(); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Remove(cfg.TransportScopeFile) })
	}
	var target corev1.Namespace
	if err := kube.Get(ctx, client.ObjectKey{Name: "kube-system"}, &target); err != nil {
		t.Fatal(err)
	}
	var secondClient client.Client
	var secondIdentity corev1.Namespace
	if cfg.SecondContext != "" {
		secondREST, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(rules, &clientcmd.ConfigOverrides{CurrentContext: cfg.SecondContext}).ClientConfig()
		if err != nil {
			t.Fatal(err)
		}
		secondREST.Timeout = 15 * time.Second
		secondClient, err = client.New(secondREST, client.Options{Scheme: scheme})
		if err != nil {
			t.Fatal(err)
		}
		if err := secondClient.Get(ctx, client.ObjectKey{Name: "kube-system"}, &secondIdentity); err != nil {
			t.Fatal(err)
		}
		if secondIdentity.UID == target.UID {
			t.Fatal("SecondContext must identify an independent cluster")
		}
		assertIndependentTargetCredentials(t, ctx, prefix, rest, secondREST)
	}
	storage := s3.NewFromConfig(aws.Config{Region: "us-east-1", Credentials: credentials.NewStaticCredentialsProvider("000000000000", "test", "")}, func(o *s3.Options) { o.BaseEndpoint = aws.String(cfg.HostEndpoint); o.UsePathStyle = true })
	if _, err := storage.CreateBucket(ctx, &s3.CreateBucketInput{Bucket: aws.String(prefix)}); err != nil {
		t.Fatal(err)
	}
	store, err := artifacts.NewS3StoreWithKMS(cfg.HostEndpoint, "us-east-1", prefix, "")
	if err != nil {
		t.Fatal(err)
	}
	api, err := console.NewServer(kube, store, "deterministic", console.DeterministicDraftGenerator{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	apiServer := httptest.NewServer(api.Handler())
	defer apiServer.Close()
	apiRequest := func(t *testing.T, method, path string, payload any, expected int) []byte {
		t.Helper()
		body, err := json.Marshal(payload)
		if err != nil {
			t.Fatal(err)
		}
		request, err := http.NewRequestWithContext(ctx, method, apiServer.URL+path, bytes.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		request.Header.Set("Content-Type", "application/json")
		response, err := apiServer.Client().Do(request)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		content, err := io.ReadAll(io.LimitReader(response.Body, 2<<20))
		if err != nil {
			t.Fatal(err)
		}
		if response.StatusCode != expected {
			t.Fatalf("%s %s: status=%d want=%d body=%s", method, path, response.StatusCode, expected, content)
		}
		return content
	}
	dir := t.TempDir()
	managementConfig, err := rules.Load()
	if err != nil {
		t.Fatal(err)
	}
	managementConfig.CurrentContext = cfg.Context
	managementFile := filepath.Join(dir, "management.kubeconfig")
	if err := clientcmd.WriteToFile(*managementConfig, managementFile); err != nil {
		t.Fatal(err)
	}
	profiles := []map[string]any{{"identity": map[string]string{"provider": "kubernetes", "accountId": "local", "region": "local", "clusterName": cfg.Context, "incarnationId": string(target.UID)}, "connection": map[string]string{"endpoint": "kubeconfig:" + cfg.Context, "authMode": "kubeconfig-context", "kubeContext": cfg.Context, "networkRouteProfile": "kubeconfig"}}}
	if secondClient != nil {
		profiles = append(profiles, map[string]any{"identity": map[string]string{"provider": "kubernetes", "accountId": "local", "region": "local", "clusterName": cfg.SecondContext, "incarnationId": string(secondIdentity.UID)}, "connection": map[string]string{"endpoint": "kubeconfig:" + cfg.SecondContext, "authMode": "kubeconfig-context", "kubeContext": cfg.SecondContext, "networkRouteProfile": "kubeconfig"}})
	}
	encoded, _ := json.Marshal(profiles)
	targetsFile := filepath.Join(dir, "targets.json")
	if err := os.WriteFile(targetsFile, encoded, 0600); err != nil {
		t.Fatal(err)
	}
	var process *exec.Cmd
	var deployment *controllerDeployment
	complete := false
	log, err := os.Create(filepath.Join(dir, "controller.log"))
	if err != nil {
		t.Fatal(err)
	}
	defer log.Close()
	stop := func() {
		if process != nil {
			_ = process.Process.Kill()
			_ = process.Wait()
			process = nil
		}
	}
	start := func() {
		if cfg.ControllerImage != "" {
			if deployment == nil {
				deployment = startControllerDeployment(t, ctx, kube, scheme, rest, cfg, prefix, targetsFile, &complete)
			}
			return
		}
		process = exec.Command(cfg.ControllerBinary, "--leader-elect=true", "--health-probe-bind-address=0", "--metrics-bind-address=0")
		process.Env = append(os.Environ(), "KUBECONFIG="+managementFile, "PCP_KUBECONFIG="+cfg.Kubeconfig, "PCP_TARGET_CONTEXTS=", "PCP_TARGETS_FILE="+targetsFile, "PCP_ENABLE_AWS_EKS=false", "PCP_ENABLE_TERRAFORM_EXECUTION=true", "PCP_ARTIFACT_ENDPOINT="+cfg.HostEndpoint, "PCP_ARTIFACT_BUCKET="+prefix, "PCP_ARTIFACT_REGION=us-east-1", "PCP_LEADER_ELECTION_NAMESPACE="+prefix, "PCP_MAX_CONCURRENT_APPLIES=1")
		process.Stdout, process.Stderr = log, log
		if err := process.Start(); err != nil {
			t.Fatal(err)
		}
	}
	restart := func(phase string) {
		if deployment != nil {
			deployment.failover(t, ctx, phase)
			return
		}
		stop()
		start()
		t.Logf("dedicated controller process restarted phase=%s", phase)
	}
	defer stop()
	t.Cleanup(func() {
		if output := os.Getenv("KPCP_E2E_EVIDENCE_DIR"); output != "" {
			if err := os.MkdirAll(output, 0700); err != nil {
				t.Error(err)
			} else if data, err := os.ReadFile(log.Name()); err == nil {
				if err := os.WriteFile(filepath.Join(output, prefix+"-controller.log"), data, 0600); err != nil {
					t.Error(err)
				}
			}
		}
		if !complete {
			data, _ := os.ReadFile(log.Name())
			t.Logf("controller diagnostics (tail): %s", string(data[max(0, len(data)-12000):]))
			t.Logf("retained owned namespace/bucket %s; finalizers and evidence preserved", prefix)
			return
		}
		cleanup, done := context.WithTimeout(context.Background(), 30*time.Second)
		defer done()
		objects, err := storage.ListObjectsV2(cleanup, &s3.ListObjectsV2Input{Bucket: aws.String(prefix)})
		if err != nil {
			t.Error(err)
			return
		}
		for _, object := range objects.Contents {
			if _, err := storage.DeleteObject(cleanup, &s3.DeleteObjectInput{Bucket: aws.String(prefix), Key: object.Key}); err != nil {
				t.Error(err)
			}
		}
		if _, err := storage.DeleteBucket(cleanup, &s3.DeleteBucketInput{Bucket: aws.String(prefix)}); err != nil {
			t.Error(err)
		}
		if err := kube.Delete(cleanup, ns, client.Preconditions{UID: &ns.UID}); err != nil {
			t.Error(err)
		}
	})
	backend := fmt.Sprintf("bucket = %q\nkey = %q\nregion = \"us-east-1\"\nuse_lockfile = true\nuse_path_style = true\nskip_credentials_validation = true\nskip_region_validation = true\nskip_requesting_account_id = true\nskip_metadata_api_check = true\nendpoints = { s3 = %q, sts = %q }\n", prefix, prefix+"/state.tfstate", cfg.PodEndpoint, cfg.PodEndpoint)
	for _, object := range []client.Object{
		&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "backend", Namespace: prefix}, Data: map[string]string{"backend.hcl": backend}},
		&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "emulator", Namespace: prefix}, Data: map[string][]byte{"AWS_ACCESS_KEY_ID": []byte("000000000000"), "AWS_SECRET_ACCESS_KEY": []byte("test")}},
		&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "unrelated", Namespace: prefix}, Data: map[string]string{"sentinel": prefix}},
	} {
		if err := kube.Create(ctx, object); err != nil {
			t.Fatal(err)
		}
	}
	class := &platform.EnvironmentClass{ObjectMeta: metav1.ObjectMeta{Name: prefix}, Spec: platform.EnvironmentClassSpec{
		Version:          "phase25b-v1",
		ExecutionProfile: &platform.ExecutionProfile{Mode: "emulator", S3Endpoint: cfg.PodEndpoint, STSEndpoint: cfg.PodEndpoint, Region: "us-east-1", AccountID: "000000000000", ArtifactBucket: prefix, CredentialsSecretRef: corev1.LocalObjectReference{Name: "emulator"}},
		Inputs:           map[string]string{"bucket_name": prefix + "-cloud", "target_cluster_name": cfg.Context, "target_endpoint": "kubeconfig:" + cfg.Context, "target_incarnation_id": string(target.UID)},
		Source:           platform.SourceSpec{URL: cfg.SourceURL, Revision: cfg.SourceRevision, Path: "."},
		Backend:          platform.BackendSpec{Type: "s3", ConfigRef: corev1.LocalObjectReference{Name: "backend"}, AuthRef: platform.ServiceAccountReference{ServiceAccountName: "runner"}, LockTimeout: metav1.Duration{Duration: 30 * time.Second}},
		Executor:         platform.ExecutorSpec{TerraformVersion: "1.14.0", Image: cfg.RunnerImage, WorkDir: "/workspace/terraform", ExecutionTimeout: metav1.Duration{Duration: 8 * time.Minute}},
		RunnerProfile:    platform.RunnerProfileSpec{ServiceAccountName: "runner", Image: cfg.RunnerImage, ImageDigest: cfg.RunnerImage, TerraformVersion: "1.14.0"},
		RuntimeProfile:   platform.RuntimeProfileSpec{RuntimeObjects: []platform.RuntimeObject{{Version: "v1", Resource: "configmaps", Object: apiextensionsv1.JSON{Raw: []byte(fmt.Sprintf(`{"apiVersion":"v1","kind":"ConfigMap","metadata":{"name":"runtime","namespace":%q},"data":{"owner":%q}}`, prefix, prefix))}}}},
		Target:           platform.TargetReference{Provider: "kubernetes", Region: "local", ClusterName: cfg.Context, IncarnationID: string(target.UID), ConnectionProfileRef: "kubeconfig:" + cfg.Context}, ApprovalPolicy: platform.ApprovalPolicyManual,
	}}
	start()
	wait := func(t *testing.T, description string, check func() (bool, error)) {
		t.Helper()
		for {
			ready, err := check()
			if err != nil {
				t.Fatalf("%s: %v", description, err)
			}
			if ready {
				return
			}
			select {
			case <-ctx.Done():
				t.Fatalf("%s: %v", description, ctx.Err())
			case <-time.After(300 * time.Millisecond):
			}
		}
	}
	classRequest := console.CreateClassRequest{Name: class.Name, Spec: class.Spec}
	var validation console.ClassValidation
	if err := json.Unmarshal(apiRequest(t, "POST", "/api/classes/validate", classRequest, http.StatusOK), &validation); err != nil || !validation.Valid {
		t.Fatalf("real class admission: %+v %v", validation, err)
	}
	apiRequest(t, "GET", "/api/classes/"+class.Name, nil, http.StatusNotFound)
	apiRequest(t, "POST", "/api/classes", classRequest, http.StatusCreated)
	apiRequest(t, "POST", "/api/classes", classRequest, http.StatusConflict)
	wait(t, "class API readiness", func() (bool, error) {
		if err := kube.Get(ctx, client.ObjectKeyFromObject(class), class); err != nil {
			return false, err
		}
		return meta.IsStatusConditionTrue(class.Status.Conditions, "Ready"), nil
	})
	if !t.Run("PlatformAPI_DraftAndTemplate", func(t *testing.T) {
		template := console.ClassTemplateRequest{Name: prefix, Title: "Owned integration template", Spec: class.Spec}
		var saved console.ClassTemplate
		if err := json.Unmarshal(apiRequest(t, "POST", "/api/class-templates", template, http.StatusCreated), &saved); err != nil || saved.UID == "" {
			t.Fatal("template identity missing: ", err)
		}
		ownedTemplate := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "pcp-class-template-" + prefix, Namespace: "default", UID: types.UID(saved.UID)}}
		t.Cleanup(func() {
			_ = client.IgnoreNotFound(kube.Delete(context.Background(), ownedTemplate, client.Preconditions{UID: &ownedTemplate.UID}))
		})
		apiRequest(t, "POST", "/api/class-templates", template, http.StatusConflict)
		if !bytes.Contains(apiRequest(t, "GET", "/api/class-templates", nil, http.StatusOK), []byte(saved.UID)) {
			t.Fatal("template not persisted")
		}
		apiRequest(t, "DELETE", "/api/class-templates/"+prefix, map[string]string{"uid": "stale", "confirmName": prefix}, http.StatusConflict)
		apiRequest(t, "DELETE", "/api/class-templates/"+prefix, map[string]string{"uid": saved.UID, "confirmName": prefix}, http.StatusOK)
		var draft console.DraftResponse
		if err := json.Unmarshal(apiRequest(t, "POST", "/api/drafts", console.DraftRequest{Description: "Create an owned test environment", Name: "environment", Namespace: prefix, ClassRef: class.Name, Region: "local", NodeCount: 1}, http.StatusOK), &draft); err != nil || !draft.Valid || draft.YAML == "" {
			t.Fatalf("draft: %+v %v", draft, err)
		}
		var envs platform.PlatformEnvironmentList
		var runs platform.TerraformRunList
		if err := kube.List(ctx, &envs, client.InNamespace(prefix)); err != nil {
			t.Fatal(err)
		}
		if err := kube.List(ctx, &runs, client.InNamespace(prefix)); err != nil {
			t.Fatal(err)
		}
		if len(envs.Items) != 0 || len(runs.Items) != 0 {
			t.Fatal("draft/template caused execution without explicit submit")
		}
	}) {
		return
	}
	apiRequest(t, "POST", "/api/environments", console.CreateEnvironmentRequest{Name: "environment", Namespace: prefix, ClassRef: class.Name, Region: "local", NodeCount: 1}, http.StatusCreated)
	environment := &platform.PlatformEnvironment{ObjectMeta: metav1.ObjectMeta{Name: "environment", Namespace: prefix}}
	if err := kube.Get(ctx, client.ObjectKeyFromObject(environment), environment); err != nil {
		t.Fatal(err)
	}
	apiRequest(t, "DELETE", "/api/classes/"+class.Name, map[string]string{"uid": string(class.UID), "confirmName": class.Name}, http.StatusConflict)
	var plan platform.TerraformRun
	getPlan := func(mode string) (bool, error) {
		var runs platform.TerraformRunList
		if err := kube.List(ctx, &runs, client.InNamespace(prefix)); err != nil {
			return false, err
		}
		for _, run := range runs.Items {
			if run.Spec.StackRef.Name == "environment-infra" && run.Spec.Operation == "Plan" && run.Spec.PlanMode == mode {
				plan = run
				if run.Status.ExecutionOutcome == "Failed" || run.Status.ExecutionOutcome == "Rejected" || run.Status.ExecutionOutcome == "Superseded" || run.Status.ExecutionOutcome == "Indeterminate" {
					return false, fmt.Errorf("plan %s: %+v", run.Name, run.Status.Conditions)
				}
				return run.Status.ExecutionOutcome == "ChangesPresent", nil
			}
		}
		return false, nil
	}
	countApply := func() int {
		var runs platform.TerraformRunList
		if err := kube.List(ctx, &runs, client.InNamespace(prefix)); err != nil {
			t.Fatal(err)
		}
		n := 0
		for _, run := range runs.Items {
			if run.Spec.StackRef.Name == "environment-infra" && run.Spec.Operation == "Apply" {
				n++
			}
		}
		return n
	}
	approve := func(name string, current platform.TerraformRun) *platform.ChangeApproval {
		return &platform.ChangeApproval{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: prefix}, Spec: platform.ChangeApprovalSpec{PlanRunRef: corev1.LocalObjectReference{Name: current.Name}, PlanRunUID: string(current.UID), PlanDigest: current.Status.PlanDigest, ExecutionContextDigest: current.Spec.ExecutionContextDigest, EffectivePlanInputDigest: current.Status.EffectivePlanInputDigest, PlanReportRef: current.Status.PlanReportRef, PlanReportDigest: current.Status.PlanReportDigest}}
	}
	// Kill a genuinely active Plan process, retaining its Job/UID.
	wait(t, "active Plan Job", func() (bool, error) {
		_, err := getPlan("Reconcile")
		if err != nil || plan.Status.JobRef == nil || plan.Status.JobUID == "" {
			return false, err
		}
		var job batchv1.Job
		if err := kube.Get(ctx, client.ObjectKey{Namespace: prefix, Name: plan.Status.JobRef.Name}, &job); err != nil {
			return false, client.IgnoreNotFound(err)
		}
		if string(job.UID) != plan.Status.JobUID || !hasOwnerUID(job.OwnerReferences, plan.UID) {
			return false, fmt.Errorf("active Plan Job identity is not durable")
		}
		if job.Status.Succeeded > 0 || job.Status.Failed > 0 {
			return false, fmt.Errorf("Plan Job is no longer active before restart")
		}
		recordGateEvidence(t, prefix, "plan-active-before-restart", map[string]any{"run": plan, "job": job})
		return true, nil
	})
	planUID := plan.UID
	restart("active-plan")
	wait(t, "saved Plan", func() (bool, error) { return getPlan("Reconcile") })
	if plan.UID != planUID {
		t.Fatal("restart replaced Plan UID")
	}
	restart("pending-approval")
	wait(t, "Reconcile approval admission after recovery", func() (bool, error) {
		var stack platform.InfraStack
		if err := kube.Get(ctx, client.ObjectKey{Namespace: prefix, Name: "environment-infra"}, &stack); err != nil {
			return false, err
		}
		condition := meta.FindStatusCondition(stack.Status.Conditions, "Ready")
		return stack.Status.LatestPlanRunRef != nil && stack.Status.LatestPlanRunRef.Name == plan.Name && condition != nil && condition.Reason == "WaitingApproval", nil
	})
	if !t.Run("A_ExactApproval", func(t *testing.T) {
		if countApply() != 0 {
			t.Fatal("Apply before approval")
		}
		for _, field := range []string{"digest", "uid", "input", "report", "report-ref", "context", "plan-ref"} {
			bad := approve("wrong-"+field, plan)
			switch field {
			case "digest":
				bad.Spec.PlanDigest = "sha256:wrong"
			case "uid":
				bad.Spec.PlanRunUID = "wrong"
			case "input":
				bad.Spec.EffectivePlanInputDigest = "wrong"
			case "report":
				bad.Spec.PlanReportDigest = "wrong"
			case "report-ref":
				bad.Spec.PlanReportRef = "runs/foreign/report.json"
			case "plan-ref":
				bad.Spec.PlanRunRef.Name = "not-this-plan"
			case "context":
				bad.Spec.ExecutionContextDigest = "wrong"
			}
			if err := kube.Create(ctx, bad); err != nil {
				t.Fatal(err)
			}
			recordGateEvidence(t, prefix, "approval-"+field, bad)
		}
		time.Sleep(2 * time.Second)
		if countApply() != 0 {
			t.Fatal("mismatched approval admitted Apply")
		}
		var jobs batchv1.JobList
		if err := kube.List(ctx, &jobs, client.InNamespace(prefix)); err != nil || len(jobs.Items) != 1 || !hasOwnerUID(jobs.Items[0].OwnerReferences, plan.UID) {
			t.Fatalf("unapproved execution created an unexpected Job: %v %+v", err, jobs.Items)
		}
		if _, err := storage.HeadBucket(ctx, &s3.HeadBucketInput{Bucket: aws.String(prefix + "-cloud")}); !artifacts.IsNotFound(err) {
			t.Fatal("unapproved execution changed cloud resources: ", err)
		}
		recordGateEvidence(t, prefix, "approval-blocked", map[string]any{"plan": plan, "jobs": jobs, "applyCount": countApply()})
	}) {
		return
	}
	if !t.Run("B_ImmutableEvidence", func(t *testing.T) {
		for _, ref := range []artifacts.Ref{{Key: plan.Status.PlanRef, Digest: plan.Status.PlanDigest}, {Key: plan.Status.SourceBundleRef, Digest: plan.Status.SourceBundleDigest}, {Key: plan.Status.ResolvedBackendConfigRef, Digest: plan.Status.ResolvedBackendConfigDigest}, {Key: plan.Status.PlanReportRef, Digest: plan.Status.PlanReportDigest}, {Key: plan.Status.TerminalResultRef, Digest: plan.Status.TerminalResultDigest}} {
			if _, err := store.GetVerified(ctx, ref); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := store.PutImmutable(ctx, plan.Status.PlanRef, []byte("tampered")); err == nil {
			t.Fatal("saved plan overwrite accepted")
		}
		for _, damage := range []string{"tamper", "missing", "source-tamper", "backend-tamper", "report-missing", "report-tamper", "plan-missing", "terminal-tamper", "credential-change", "expired"} {
			victimClass := class.DeepCopy()
			victimClass.ObjectMeta = metav1.ObjectMeta{Name: prefix + "-" + damage}
			victimClass.Status = platform.EnvironmentClassStatus{}
			victimClass.Spec.Inputs["bucket_name"] = prefix + "-" + damage + "-cloud"
			victimClass.Spec.RunnerProfile.ServiceAccountName = "runner-" + damage
			victimClass.Spec.Backend.AuthRef.ServiceAccountName = "runner-" + damage
			var victimSecret *corev1.Secret
			if damage == "credential-change" {
				victimSecret = &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "emulator-" + damage, Namespace: prefix}, Data: map[string][]byte{"AWS_ACCESS_KEY_ID": []byte("000000000000"), "AWS_SECRET_ACCESS_KEY": []byte("test")}}
				if err := kube.Create(ctx, victimSecret); err != nil {
					t.Fatal(err)
				}
				victimClass.Spec.ExecutionProfile.CredentialsSecretRef.Name = victimSecret.Name
			}
			if err := kube.Create(ctx, victimClass); err != nil {
				t.Fatal(err)
			}
			victim := &platform.PlatformEnvironment{ObjectMeta: metav1.ObjectMeta{Name: damage, Namespace: prefix}, Spec: platform.PlatformEnvironmentSpec{ClassRef: &corev1.LocalObjectReference{Name: victimClass.Name}}}
			if err := kube.Create(ctx, victim); err != nil {
				t.Fatal(err)
			}
			var saved platform.TerraformRun
			wait(t, damage+" fixture Plan", func() (bool, error) {
				var runs platform.TerraformRunList
				if err := kube.List(ctx, &runs, client.InNamespace(prefix)); err != nil {
					return false, err
				}
				for _, run := range runs.Items {
					if run.Spec.StackRef.Name == damage+"-infra" && run.Status.ExecutionOutcome == "ChangesPresent" {
						saved = run
						return true, nil
					}
					if run.Spec.StackRef.Name == damage+"-infra" && (run.Status.ExecutionOutcome == "Failed" || run.Status.ExecutionOutcome == "Rejected") {
						return false, fmt.Errorf("fixture Plan failed: %+v", run.Status.Conditions)
					}
				}
				return false, nil
			})
			key, digest := saved.Status.PlanRef, saved.Status.PlanDigest
			switch damage {
			case "missing", "source-tamper":
				key, digest = saved.Status.SourceBundleRef, saved.Status.SourceBundleDigest
			case "backend-tamper":
				key, digest = saved.Status.ResolvedBackendConfigRef, saved.Status.ResolvedBackendConfigDigest
			case "report-missing", "report-tamper":
				key, digest = saved.Status.PlanReportRef, saved.Status.PlanReportDigest
			case "terminal-tamper":
				key, digest = saved.Status.TerminalResultRef, saved.Status.TerminalResultDigest
			}
			if damage == "credential-change" {
				victimSecret.Annotations = map[string]string{"qa.example.io/credential-rotation": "changed-after-plan"}
				if err := kube.Update(ctx, victimSecret); err != nil {
					t.Fatal(err)
				}
				t.Logf("credential drift secret=%s uid=%s version=%s planIdentity=%s", victimSecret.Name, victimSecret.UID, victimSecret.ResourceVersion, saved.Spec.ExecutionProfile.CredentialsIdentity)
			} else if damage == "expired" {
				saved.Status.PlanExpiresAt = &metav1.Time{Time: time.Now().Add(-time.Minute)}
				if err := kube.Status().Update(ctx, &saved); err != nil {
					t.Fatal(err)
				}
			} else if strings.HasSuffix(damage, "missing") || damage == "missing" {
				if _, err := storage.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: aws.String(prefix), Key: aws.String(key)}); err != nil {
					t.Fatal(err)
				}
				t.Logf("damage=%s planUID=%s key=%s originalDigest=%s actual=missing", damage, saved.UID, key, digest)
			} else {
				corrupt := []byte("test-owned corrupted " + damage)
				if _, err := storage.PutObject(ctx, &s3.PutObjectInput{Bucket: aws.String(prefix), Key: aws.String(key), Body: bytes.NewReader(corrupt), Metadata: map[string]string{"sha256": digest}}); err != nil {
					t.Fatal(err)
				}
				t.Logf("damage=%s planUID=%s key=%s originalDigest=%s actualDigest=sha256:%x", damage, saved.UID, key, digest, sha256.Sum256(corrupt))
			}
			if err := kube.Create(ctx, approve(damage+"-approval", saved)); err != nil {
				t.Fatal(err)
			}
			wait(t, damage+" blocks Apply", func() (bool, error) {
				var runs platform.TerraformRunList
				if err := kube.List(ctx, &runs, client.InNamespace(prefix)); err != nil {
					return false, err
				}
				for _, run := range runs.Items {
					if damage == "expired" && run.UID == saved.UID && run.Status.ExecutionOutcome == "Superseded" {
						for _, candidate := range runs.Items {
							if candidate.Spec.StackRef.Name == damage+"-infra" && candidate.Spec.Operation == "Apply" {
								return false, fmt.Errorf("expired/superseded approval admitted Apply")
							}
						}
						recordGateEvidence(t, prefix, "expired-superseded-blocked", runs)
						return true, nil
					}
					if run.Spec.StackRef.Name == damage+"-infra" && run.Spec.Operation == "Apply" {
						if run.Status.JobRef != nil {
							return false, fmt.Errorf("damaged evidence started an Apply Job")
						}
						if run.Status.ExecutionOutcome == "Rejected" {
							recordGateEvidence(t, prefix, damage+"-rejected", map[string]any{"savedPlan": saved, "rejectedApply": run})
							return true, nil
						}
					}
				}
				return false, nil
			})
			var after platform.TerraformRunList
			if err := kube.List(ctx, &after, client.InNamespace(prefix)); err != nil {
				t.Fatal(err)
			}
			plans := 0
			for _, run := range after.Items {
				if run.Spec.StackRef.Name == damage+"-infra" && run.Spec.Operation == "Plan" {
					plans++
				}
			}
			if damage != "expired" && plans != 1 {
				t.Fatal("artifact/credential failure silently regenerated a Plan")
			}
			if _, err := storage.HeadBucket(ctx, &s3.HeadBucketInput{Bucket: aws.String(prefix + "-" + damage + "-cloud")}); !artifacts.IsNotFound(err) {
				t.Fatal("rejected execution changed cloud resources: ", err)
			}
			if err := kube.Delete(ctx, victim, client.Preconditions{UID: &victim.UID}); err != nil {
				t.Fatal(err)
			}
			wait(t, damage+" cleanup", func() (bool, error) {
				err := kube.Get(ctx, client.ObjectKeyFromObject(victim), victim)
				return apierrors.IsNotFound(err), client.IgnoreNotFound(err)
			})
			if err := kube.Delete(ctx, victimClass, client.Preconditions{UID: &victimClass.UID}); err != nil {
				t.Fatal(err)
			}
			wait(t, damage+" class cleanup", func() (bool, error) {
				err := kube.Get(ctx, client.ObjectKeyFromObject(victimClass), victimClass)
				return apierrors.IsNotFound(err), client.IgnoreNotFound(err)
			})
			if victimSecret != nil {
				if err := kube.Delete(ctx, victimSecret, client.Preconditions{UID: &victimSecret.UID}); err != nil {
					t.Fatal(err)
				}
			}
		}
	}) {
		return
	}
	approvalPayload := func(p platform.TerraformRun) console.ApprovalRequest {
		return console.ApprovalRequest{PlanRunUID: string(p.UID), PlanDigest: p.Status.PlanDigest, ExecutionContextDigest: p.Spec.ExecutionContextDigest, EffectivePlanInputDigest: p.Status.EffectivePlanInputDigest, PlanReportRef: p.Status.PlanReportRef, PlanReportDigest: p.Status.PlanReportDigest}
	}
	if !t.Run("PlatformAPI_ExactApproval", func(t *testing.T) {
		var graph console.PlanView
		if err := json.Unmarshal(apiRequest(t, "GET", "/api/terraform-runs/"+prefix+"/"+plan.Name+"/plan", nil, http.StatusOK), &graph); err != nil || !graph.Available || graph.Graph == nil || graph.Graph.RunUID != string(plan.UID) || graph.Graph.PlanDigest != plan.Status.PlanDigest {
			t.Fatalf("live plan graph identity: %+v %v", graph, err)
		}
		stale := approvalPayload(plan)
		stale.PlanReportDigest = "sha256:stale"
		apiRequest(t, "POST", "/api/terraform-runs/"+prefix+"/"+plan.Name+"/approve", stale, http.StatusConflict)
		if countApply() != 0 {
			t.Fatal("stale API approval admitted Apply")
		}
	}) {
		return
	}
	var recorded console.ApprovalView
	if err := json.Unmarshal(apiRequest(t, "POST", "/api/terraform-runs/"+prefix+"/"+plan.Name+"/approve", approvalPayload(plan), http.StatusCreated), &recorded); err != nil || recorded.State != "Recorded" || recorded.PlanRunUID != string(plan.UID) {
		t.Fatalf("live approval: %+v %v", recorded, err)
	}
	var duplicate console.ApprovalView
	var approvalBefore, approvalAfter platform.ChangeApproval
	if err := kube.Get(ctx, client.ObjectKey{Namespace: prefix, Name: recorded.Name}, &approvalBefore); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(apiRequest(t, "POST", "/api/terraform-runs/"+prefix+"/"+plan.Name+"/approve", approvalPayload(plan), http.StatusOK), &duplicate); err != nil || duplicate.Name != recorded.Name {
		t.Fatalf("exact retry did not preserve approval identity: %+v %v", duplicate, err)
	}
	if err := kube.Get(ctx, client.ObjectKeyFromObject(&approvalBefore), &approvalAfter); err != nil || approvalBefore.UID != approvalAfter.UID || approvalBefore.Spec != approvalAfter.Spec {
		t.Fatal("exact retry changed the durable approval: ", err)
	}
	recordGateEvidence(t, prefix, "exact-approval-idempotent", map[string]any{"first": approvalBefore, "retry": approvalAfter, "plan": plan})
	wait(t, "active Apply Job", func() (bool, error) {
		var runs platform.TerraformRunList
		if err := kube.List(ctx, &runs, client.InNamespace(prefix)); err != nil {
			return false, err
		}
		for _, run := range runs.Items {
			if run.Spec.StackRef.Name == "environment-infra" && run.Spec.Operation == "Apply" && run.Status.JobRef != nil {
				return true, nil
			}
		}
		return false, nil
	})
	restart("active-apply")
	wait(t, "environment Ready", func() (bool, error) {
		if err := kube.Get(ctx, client.ObjectKeyFromObject(environment), environment); err != nil {
			return false, err
		}
		return meta.IsStatusConditionTrue(environment.Status.Conditions, "Ready"), nil
	})
	restart("ready")
	time.Sleep(2 * time.Second)
	var runtimeObject corev1.ConfigMap
	if err := kube.Get(ctx, client.ObjectKey{Namespace: prefix, Name: "runtime"}, &runtimeObject); err != nil {
		t.Fatal("runtime was not reconciled: ", err)
	}
	if runtimeObject.Data["owner"] != prefix {
		t.Fatal("runtime ownership content mismatch")
	}
	for _, suffix := range []string{"", "/timeline", "/terraform-runs", "/architecture"} {
		apiRequest(t, "GET", "/api/environments/"+prefix+"/environment"+suffix, nil, http.StatusOK)
	}
	if !bytes.Contains(apiRequest(t, "GET", "/api/environments", nil, http.StatusOK), []byte(string(environment.UID))) {
		t.Fatal("environment read model omitted the live UID")
	}
	if !t.Run("C_RestartRecovery", func(t *testing.T) {
		if countApply() != 1 {
			t.Fatal("duplicate Apply after four controller recoveries")
		}
		var jobs batchv1.JobList
		if err := kube.List(ctx, &jobs, client.InNamespace(prefix)); err != nil {
			t.Fatal(err)
		}
		mainJobs := 0
		for _, job := range jobs.Items {
			if strings.HasPrefix(job.Name, "environment-infra-") {
				mainJobs++
			}
		}
		if mainJobs != 2 {
			t.Fatalf("expected exactly Plan+Apply Jobs, got %d", mainJobs)
		}
		t.Logf("Plan UID=%s digest=%s input=%s source=%s", plan.UID, plan.Status.PlanDigest, plan.Status.EffectivePlanInputDigest, plan.Status.SourceBundleDigest)
		recordGateEvidence(t, prefix, "controller-recovery-result", map[string]any{"deploymentLane": deployment != nil, "plan": plan, "applyCount": countApply(), "jobs": jobs})
	}) {
		return
	}
	if !t.Run("NoChange", func(t *testing.T) {
		previous := plan.UID
		update := console.UpdateEnvironmentRequest{UID: string(environment.UID), Generation: environment.Generation, NodeCount: environment.Spec.Capacity.NodeCount + 1}
		stale := update
		stale.Generation--
		apiRequest(t, "PUT", "/api/environments/"+prefix+"/environment", stale, http.StatusConflict)
		apiRequest(t, "PUT", "/api/environments/"+prefix+"/environment", update, http.StatusOK)
		wait(t, "new NoChange Plan", func() (bool, error) {
			var runs platform.TerraformRunList
			if err := kube.List(ctx, &runs, client.InNamespace(prefix)); err != nil {
				return false, err
			}
			for _, run := range runs.Items {
				if run.Spec.StackRef.Name == "environment-infra" && run.Spec.Operation == "Plan" && run.Spec.PlanMode == "Reconcile" && run.UID != previous && run.Status.ExecutionOutcome == "NoChange" {
					plan = run
					return true, nil
				}
			}
			return false, nil
		})
		if err := kube.Create(ctx, approve("nochange", plan)); err != nil {
			t.Fatal(err)
		}
		apiRequest(t, "POST", "/api/terraform-runs/"+prefix+"/"+plan.Name+"/approve", approvalPayload(plan), http.StatusConflict)
		wait(t, "NoChange convergence", func() (bool, error) {
			if err := kube.Get(ctx, client.ObjectKeyFromObject(environment), environment); err != nil {
				return false, err
			}
			return environment.Status.ObservedGeneration == environment.Generation && meta.IsStatusConditionTrue(environment.Status.Conditions, "Ready"), nil
		})
		if countApply() != 1 {
			t.Fatal("NoChange triggered an Apply")
		}
	}) {
		return
	}
	isolationLane := func(t *testing.T, multiCluster bool) {
		isolatedClient, isolatedContext, isolatedUID := kube, cfg.Context, string(target.UID)
		isolatedNamespace := prefix + "-secondary"
		if multiCluster {
			isolatedClient, isolatedContext, isolatedUID = secondClient, cfg.SecondContext, string(secondIdentity.UID)
			isolatedNamespace = prefix
		}
		secondNamespace := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: isolatedNamespace, Labels: map[string]string{"platform.example.io/e2e": prefix}}}
		if err := isolatedClient.Create(ctx, secondNamespace); err != nil {
			t.Fatal(err)
		}
		t.Logf("owned isolation namespace=%s uid=%s clusterUID=%s multiCluster=%t", secondNamespace.Name, secondNamespace.UID, isolatedUID, multiCluster)
		envs := map[string]*platform.PlatformEnvironment{}
		classes := map[string]*platform.EnvironmentClass{}
		plans := map[string]platform.TerraformRun{}
		for _, label := range []string{"second", "concurrent"} {
			contextName, uid := cfg.Context, string(target.UID)
			runtimeNamespace := prefix
			if label == "second" {
				contextName, uid = isolatedContext, isolatedUID
				runtimeNamespace = secondNamespace.Name
			}
			c := class.DeepCopy()
			c.ObjectMeta = metav1.ObjectMeta{Name: prefix + "-" + label}
			c.Status = platform.EnvironmentClassStatus{}
			c.Spec.Inputs["bucket_name"] = prefix + "-" + label + "-cloud"
			c.Spec.Inputs["target_cluster_name"] = contextName
			c.Spec.Inputs["target_endpoint"] = "kubeconfig:" + contextName
			c.Spec.Inputs["target_incarnation_id"] = uid
			c.Spec.Target = platform.TargetReference{Provider: "kubernetes", Region: "local", ClusterName: contextName, IncarnationID: uid, ConnectionProfileRef: "kubeconfig:" + contextName}
			c.Spec.RunnerProfile.ServiceAccountName = "runner-" + label
			c.Spec.Backend.AuthRef.ServiceAccountName = "runner-" + label
			c.Spec.RuntimeProfile.RuntimeObjects[0].Object.Raw = []byte(fmt.Sprintf(`{"apiVersion":"v1","kind":"ConfigMap","metadata":{"name":%q,"namespace":%q},"data":{"owner":%q}}`, label+"-runtime", runtimeNamespace, prefix))
			if err := kube.Create(ctx, c); err != nil {
				t.Fatal(err)
			}
			classes[label] = c
			e := &platform.PlatformEnvironment{ObjectMeta: metav1.ObjectMeta{Name: label, Namespace: prefix}, Spec: platform.PlatformEnvironmentSpec{ClassRef: &corev1.LocalObjectReference{Name: c.Name}}}
			if err := kube.Create(ctx, e); err != nil {
				t.Fatal(err)
			}
			envs[label] = e
		}
		collect := func(mode string) (bool, error) {
			var runs platform.TerraformRunList
			if err := kube.List(ctx, &runs, client.InNamespace(prefix)); err != nil {
				return false, err
			}
			for _, run := range runs.Items {
				for label := range envs {
					if run.Spec.StackRef.Name == label+"-infra" && run.Spec.Operation == "Plan" && run.Spec.PlanMode == mode {
						if run.Status.ExecutionOutcome == "Failed" || run.Status.ExecutionOutcome == "Rejected" {
							return false, fmt.Errorf("%s plan failed", label)
						}
						if run.Status.ExecutionOutcome == "ChangesPresent" {
							plans[label] = run
						}
					}
				}
			}
			return len(plans) == 2, nil
		}
		wait(t, "two independent Plans", func() (bool, error) { return collect("Reconcile") })
		recordGateEvidence(t, prefix, "isolation-plans", map[string]any{"multiCluster": multiCluster, "clusterAUID": target.UID, "clusterBUID": isolatedUID, "targetNamespace": secondNamespace, "plans": plans})
		if plans["second"].Spec.Workspace == plans["concurrent"].Spec.Workspace || plans["second"].Spec.RunnerServiceAccountName == plans["concurrent"].Spec.RunnerServiceAccountName {
			t.Fatal("state/runner identity shared across environments")
		}
		sameTarget := plans["second"].Spec.RuntimeTargetIdentityDigest == plans["concurrent"].Spec.RuntimeTargetIdentityDigest
		if sameTarget == multiCluster {
			t.Fatal("unexpected cluster identity binding")
		}
		for label, p := range plans {
			if err := kube.Create(ctx, approve("isolation-"+label, p)); err != nil {
				t.Fatal(err)
			}
		}
		contended, recovered := false, false
		retainedJobs := map[string]string{}
		wait(t, "isolated concurrent Applies", func() (bool, error) {
			var runs platform.TerraformRunList
			if err := kube.List(ctx, &runs, client.InNamespace(prefix)); err != nil {
				return false, err
			}
			active, waiting := 0, 0
			for _, run := range runs.Items {
				if run.Spec.Operation != "Apply" || run.Spec.PlanMode != "Reconcile" {
					continue
				}
				if run.Spec.StackRef.Name != "second-infra" && run.Spec.StackRef.Name != "concurrent-infra" {
					continue
				}
				if run.Status.ExecutionOutcome == "Rejected" || run.Status.ExecutionOutcome == "Failed" || run.Status.ExecutionOutcome == "Indeterminate" {
					return false, fmt.Errorf("isolation Apply failed")
				}
				if run.Status.JobRef == nil {
					waiting++
					continue
				}
				var job batchv1.Job
				if err := kube.Get(ctx, client.ObjectKey{Namespace: prefix, Name: run.Status.JobRef.Name}, &job); err != nil {
					return false, err
				}
				if job.Status.Succeeded == 0 && job.Status.Failed == 0 {
					active++
				}
				if previous, ok := retainedJobs[run.Name]; ok && previous != string(job.UID) {
					return false, fmt.Errorf("Apply Job identity changed across recovery")
				}
				retainedJobs[run.Name] = string(job.UID)
			}
			if active > 1 {
				return false, fmt.Errorf("global Apply limit of one exceeded")
			}
			if active == 1 && waiting > 0 {
				contended = true
				if multiCluster && !recovered {
					recordGateEvidence(t, prefix, "isolation-contention-before-restart", map[string]any{"runs": runs, "active": active, "waiting": waiting, "jobUIDs": retainedJobs})
					restart("cross-target-apply-contention")
					recovered = true
					return false, nil
				}
			}
			for _, e := range envs {
				if err := kube.Get(ctx, client.ObjectKeyFromObject(e), e); err != nil {
					return false, err
				}
				if !meta.IsStatusConditionTrue(e.Status.Conditions, "Ready") {
					return false, nil
				}
			}
			return true, nil
		})
		if !contended {
			t.Fatal("no actual Apply contention was observed")
		}
		if multiCluster && !recovered {
			t.Fatal("no cross-target recovery was exercised")
		}
		var isolationRuns platform.TerraformRunList
		var isolationJobs batchv1.JobList
		if err := kube.List(ctx, &isolationRuns, client.InNamespace(prefix)); err != nil {
			t.Fatal(err)
		}
		if err := kube.List(ctx, &isolationJobs, client.InNamespace(prefix)); err != nil {
			t.Fatal(err)
		}
		for label := range envs {
			count, jobs := 0, 0
			for _, r := range isolationRuns.Items {
				if r.Spec.StackRef.Name == label+"-infra" && r.Spec.Operation == "Apply" && r.Spec.PlanMode == "Reconcile" {
					count++
					for _, j := range isolationJobs.Items {
						if hasOwnerUID(j.OwnerReferences, r.UID) {
							jobs++
						}
					}
				}
			}
			if count != 1 || jobs != 1 {
				t.Fatalf("isolation target %s: Apply runs=%d Jobs=%d", label, count, jobs)
			}
		}
		recordGateEvidence(t, prefix, "isolation-recovered", map[string]any{"multiCluster": multiCluster, "controllerRestartDuringContention": recovered, "runs": isolationRuns, "jobs": isolationJobs, "retainedJobUIDs": retainedJobs})
		for _, pair := range []struct {
			client                     client.Client
			namespace, present, absent string
		}{{kube, prefix, "concurrent-runtime", "second-runtime"}, {isolatedClient, secondNamespace.Name, "second-runtime", "concurrent-runtime"}} {
			var cm corev1.ConfigMap
			if err := pair.client.Get(ctx, client.ObjectKey{Namespace: pair.namespace, Name: pair.present}, &cm); err != nil || cm.Data["owner"] != prefix {
				t.Fatal("expected target runtime missing: ", err)
			}
			recordGateEvidence(t, prefix, "isolation-runtime-"+pair.present, cm)
			if err := pair.client.Get(ctx, client.ObjectKey{Namespace: pair.namespace, Name: pair.absent}, &cm); !apierrors.IsNotFound(err) {
				t.Fatal("runtime written outside its intended namespace/target")
			}
		}
		objects, err := storage.ListObjectsV2(ctx, &s3.ListObjectsV2Input{Bucket: aws.String(prefix)})
		if err != nil {
			t.Fatal(err)
		}
		states := map[string]bool{}
		for _, object := range objects.Contents {
			for label := range envs {
				if strings.Contains(aws.ToString(object.Key), "/"+label+"/") && strings.HasSuffix(aws.ToString(object.Key), "/state.tfstate") {
					states[label] = true
				}
			}
		}
		if len(states) != 2 {
			t.Fatal("independent durable backend states were not observed")
		}
		recordGateEvidence(t, prefix, "isolation-states", objects)
		for _, e := range envs {
			if err := kube.Delete(ctx, e, client.Preconditions{UID: &e.UID}); err != nil {
				t.Fatal(err)
			}
		}
		plans = map[string]platform.TerraformRun{}
		wait(t, "two isolated Destroy Plans", func() (bool, error) { return collect("Destroy") })
		for label, p := range plans {
			if err := kube.Create(ctx, approve("isolation-destroy-"+label, p)); err != nil {
				t.Fatal(err)
			}
		}
		wait(t, "isolated environment cleanup", func() (bool, error) {
			for _, e := range envs {
				err := kube.Get(ctx, client.ObjectKeyFromObject(e), e)
				if err == nil {
					return false, nil
				}
				if !apierrors.IsNotFound(err) {
					return false, err
				}
			}
			return true, nil
		})
		for label, c := range classes {
			if _, err := storage.HeadBucket(ctx, &s3.HeadBucketInput{Bucket: aws.String(prefix + "-" + label + "-cloud")}); !artifacts.IsNotFound(err) {
				t.Fatal("isolated cloud cleanup unverified: ", err)
			}
			if err := kube.Delete(ctx, c, client.Preconditions{UID: &c.UID}); err != nil {
				t.Fatal(err)
			}
			wait(t, "isolated class cleanup", func() (bool, error) {
				err := kube.Get(ctx, client.ObjectKeyFromObject(c), c)
				return apierrors.IsNotFound(err), client.IgnoreNotFound(err)
			})
		}
		if err := isolatedClient.Delete(ctx, secondNamespace, client.Preconditions{UID: &secondNamespace.UID}); err != nil {
			t.Fatal(err)
		}
		wait(t, "isolation namespace cleanup", func() (bool, error) {
			err := isolatedClient.Get(ctx, client.ObjectKeyFromObject(secondNamespace), secondNamespace)
			return apierrors.IsNotFound(err), client.IgnoreNotFound(err)
		})
	}
	if !t.Run("D_MultiTarget", func(t *testing.T) {
		if secondClient == nil {
			t.Skip("one actual cluster supplied; cross-cluster isolation is NOT RUN")
		}
		isolationLane(t, true)
	}) {
		return
	}
	if secondClient == nil && !t.Run("SingleClusterConcurrency", func(t *testing.T) { isolationLane(t, false) }) {
		return
	}
	if !t.Run("E_FailClosed", func(t *testing.T) {
		if countApply() != 1 {
			t.Fatal("unexpected mutation")
		}
		for _, failure := range []string{"source", "endpoint"} {
			badClass := class.DeepCopy()
			badClass.ObjectMeta = metav1.ObjectMeta{Name: prefix + "-bad-" + failure}
			badClass.Status = platform.EnvironmentClassStatus{}
			badClass.Spec.Inputs["bucket_name"] = prefix + "-bad-" + failure + "-cloud"
			badClass.Spec.RunnerProfile.ServiceAccountName = "runner-" + failure
			badClass.Spec.Backend.AuthRef.ServiceAccountName = "runner-" + failure
			if failure == "source" {
				badClass.Spec.Source.Revision = strings.Repeat("0", 40)
			} else {
				badClass.Spec.ExecutionProfile.S3Endpoint = "http://127.0.0.1:9"
				badClass.Spec.ExecutionProfile.STSEndpoint = "http://127.0.0.1:9"
				cm := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "bad-backend", Namespace: prefix}, Data: map[string]string{"backend.hcl": strings.ReplaceAll(backend, cfg.PodEndpoint, "http://127.0.0.1:9")}}
				if err := kube.Create(ctx, cm); err != nil {
					t.Fatal(err)
				}
				badClass.Spec.Backend.ConfigRef.Name = cm.Name
			}
			if err := kube.Create(ctx, badClass); err != nil {
				t.Fatal(err)
			}
			bad := &platform.PlatformEnvironment{ObjectMeta: metav1.ObjectMeta{Name: "bad-" + failure, Namespace: prefix}, Spec: platform.PlatformEnvironmentSpec{ClassRef: &corev1.LocalObjectReference{Name: badClass.Name}}}
			if err := kube.Create(ctx, bad); err != nil {
				t.Fatal(err)
			}
			wait(t, failure+" Job fails closed", func() (bool, error) {
				var jobs batchv1.JobList
				if err := kube.List(ctx, &jobs, client.InNamespace(prefix)); err != nil {
					return false, err
				}
				for _, job := range jobs.Items {
					if strings.HasPrefix(job.Name, bad.Name+"-infra-") && job.Status.Failed > 0 {
						return true, nil
					}
				}
				return false, nil
			})
			var runs platform.TerraformRunList
			if err := kube.List(ctx, &runs, client.InNamespace(prefix)); err != nil {
				t.Fatal(err)
			}
			for _, run := range runs.Items {
				if run.Spec.StackRef.Name == bad.Name+"-infra" && run.Spec.Operation == "Apply" {
					t.Fatal("failed prerequisite admitted Apply")
				}
			}
			if err := kube.Delete(ctx, bad, client.Preconditions{UID: &bad.UID}); err != nil {
				t.Fatal(err)
			}
			wait(t, failure+" cleanup", func() (bool, error) {
				err := kube.Get(ctx, client.ObjectKeyFromObject(bad), bad)
				return apierrors.IsNotFound(err), client.IgnoreNotFound(err)
			})
			if err := kube.Delete(ctx, badClass, client.Preconditions{UID: &badClass.UID}); err != nil {
				t.Fatal(err)
			}
			wait(t, failure+" class cleanup", func() (bool, error) {
				err := kube.Get(ctx, client.ObjectKeyFromObject(badClass), badClass)
				return apierrors.IsNotFound(err), client.IgnoreNotFound(err)
			})
		}
	}) {
		return
	}
	safetyClean := true
	t.Run("FinalSafety", func(t *testing.T) {
		if os.Getenv("KPCP_FINAL_SAFETY") != "1" {
			t.Skip("set KPCP_FINAL_SAFETY=1 after the complete lifecycle baseline passes")
		}
		var template platform.InfraStack
		if err := kube.Get(ctx, client.ObjectKey{Namespace: prefix, Name: "environment-infra"}, &template); err != nil {
			t.Fatal(err)
		}
		safetyClean = runFinalSafety(t, ctx, kube, storage, store, cfg, prefix, template, log.Name(), stop, start)
	})
	if !t.Run("F_DestroyFinalizer", func(t *testing.T) {
		if cfg.FaultFile != "" {
			if !filepath.IsAbs(cfg.FaultFile) {
				t.Fatal("FaultFile must be an absolute test-owned path")
			}
			fault, err := os.OpenFile(cfg.FaultFile, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
			if err != nil {
				t.Fatal(err)
			}
			defer os.Remove(cfg.FaultFile)
			if _, err := fault.Write([]byte(prefix)); err != nil {
				_ = fault.Close()
				t.Fatal(err)
			}
			if err := fault.Close(); err != nil {
				t.Fatal(err)
			}
		}
		apiRequest(t, "DELETE", "/api/environments/"+prefix+"/environment", console.DeleteEnvironmentRequest{UID: "stale"}, http.StatusConflict)
		apiRequest(t, "DELETE", "/api/environments/"+prefix+"/environment", console.DeleteEnvironmentRequest{UID: string(environment.UID)}, http.StatusAccepted)
		if cfg.FaultFile != "" {
			wait(t, "transient Destroy Plan failure", func() (bool, error) {
				var jobs batchv1.JobList
				if err := kube.List(ctx, &jobs, client.InNamespace(prefix)); err != nil {
					return false, err
				}
				for _, job := range jobs.Items {
					if strings.HasPrefix(job.Name, "environment-infra-destroy-") && job.Status.Failed > 0 {
						t.Logf("transient cloud failure Job UID=%s", job.UID)
						return true, nil
					}
				}
				return false, nil
			})
			if err := kube.Get(ctx, client.ObjectKeyFromObject(environment), environment); err != nil || len(environment.Finalizers) == 0 {
				t.Fatal("transient cloud error released deletion finalizer: ", err)
			}
			if _, err := storage.HeadBucket(ctx, &s3.HeadBucketInput{Bucket: aws.String(prefix + "-cloud")}); err != nil {
				t.Fatal("cloud resource was removed before approved Destroy: ", err)
			}
			if countApply() != 1 {
				t.Fatal("transient failure admitted an unapproved Destroy Apply")
			}
			if err := os.Remove(cfg.FaultFile); err != nil {
				t.Fatal(err)
			}
			wait(t, "recovered Destroy Plan", func() (bool, error) {
				var runs platform.TerraformRunList
				if err := kube.List(ctx, &runs, client.InNamespace(prefix)); err != nil {
					return false, err
				}
				for _, run := range runs.Items {
					if run.Spec.StackRef.Name == "environment-infra" && run.Spec.Operation == "Plan" && run.Spec.PlanMode == "Destroy" && run.Status.ExecutionOutcome == "ChangesPresent" {
						plan = run
						return true, nil
					}
				}
				return false, nil
			})
		} else {
			wait(t, "destroy Plan", func() (bool, error) { return getPlan("Destroy") })
		}
		if err := kube.Get(ctx, client.ObjectKeyFromObject(&runtimeObject), &corev1.ConfigMap{}); !apierrors.IsNotFound(err) {
			t.Fatal("owned runtime must be pruned before Destroy approval: ", err)
		}
		if err := kube.Get(ctx, client.ObjectKeyFromObject(environment), environment); err != nil {
			t.Fatal("finalizer released before destroy approval: ", err)
		}
		if len(environment.Finalizers) == 0 {
			t.Fatal("missing deletion finalizer")
		}
		wait(t, "Destroy approval admission", func() (bool, error) {
			var stack platform.InfraStack
			if err := kube.Get(ctx, client.ObjectKey{Namespace: prefix, Name: "environment-infra"}, &stack); err != nil {
				return false, err
			}
			condition := meta.FindStatusCondition(stack.Status.Conditions, "Ready")
			return stack.Status.LatestPlanRunRef != nil && stack.Status.LatestPlanRunRef.Name == plan.Name && condition != nil && condition.Reason == "WaitingApproval", nil
		})
		badDestroy := approve("wrong-destroy-digest", plan)
		badDestroy.Spec.PlanDigest = "sha256:not-the-destroy-plan"
		if err := kube.Create(ctx, badDestroy); err != nil {
			t.Fatal(err)
		}
		time.Sleep(2 * time.Second)
		if countApply() != 1 {
			t.Fatal("Reconcile approval or mismatched Destroy approval authorized Destroy")
		}
		if _, err := storage.HeadBucket(ctx, &s3.HeadBucketInput{Bucket: aws.String(prefix + "-cloud")}); err != nil {
			t.Fatal("unapproved Destroy changed the cloud resource: ", err)
		}
		recordGateEvidence(t, prefix, "destroy-approval-blocked", map[string]any{"environment": environment, "plan": plan, "badApproval": badDestroy, "existingReconcileApproval": approvalBefore, "applyCount": countApply()})
		restart("destroy-pending-approval")
		apiRequest(t, "POST", "/api/terraform-runs/"+prefix+"/"+plan.Name+"/approve", approvalPayload(plan), http.StatusCreated)
		var destroyApply platform.TerraformRun
		wait(t, "active Destroy Apply Job", func() (bool, error) {
			var runs platform.TerraformRunList
			if err := kube.List(ctx, &runs, client.InNamespace(prefix)); err != nil {
				return false, err
			}
			for _, run := range runs.Items {
				if run.Spec.StackRef.Name == "environment-infra" && run.Spec.Operation == "Apply" && run.Spec.PlanMode == "Destroy" && run.Status.JobRef != nil {
					destroyApply = run
					var job batchv1.Job
					if err := kube.Get(ctx, client.ObjectKey{Namespace: prefix, Name: run.Status.JobRef.Name}, &job); err != nil {
						return false, err
					}
					if job.Status.Succeeded > 0 || job.Status.Failed > 0 {
						return false, fmt.Errorf("Destroy was no longer active before restart")
					}
					recordGateEvidence(t, prefix, "destroy-active-before-restart", map[string]any{"run": run, "job": job})
					return true, nil
				}
			}
			return false, nil
		})
		restart("active-destroy")
		wait(t, "verified environment deletion", func() (bool, error) {
			err := kube.Get(ctx, client.ObjectKeyFromObject(environment), environment)
			return apierrors.IsNotFound(err), client.IgnoreNotFound(err)
		})
		apiRequest(t, "GET", "/api/environments/"+prefix+"/environment", nil, http.StatusNotFound)
		if _, err := storage.HeadBucket(ctx, &s3.HeadBucketInput{Bucket: aws.String(prefix + "-cloud")}); !artifacts.IsNotFound(err) {
			t.Fatal("Terraform resource deletion is not verified: ", err)
		}
		terminalRef, terminalBytes, err := store.GetVerifiedByKey(ctx, "runs/"+string(destroyApply.UID)+"/terminal-result.json")
		if err != nil {
			t.Fatal("Destroy terminal evidence missing after recovery: ", err)
		}
		var terminal struct {
			RunUID                                                   string `json:"terraformRunUID"`
			JobUID, Operation, ExecutionOutcome, PlanRef, PlanDigest string
			TerraformExitCode                                        int
			ArtifactsReady                                           bool
		}
		if err := json.Unmarshal(terminalBytes, &terminal); err != nil || terminal.RunUID != string(destroyApply.UID) || terminal.JobUID != destroyApply.Status.JobUID || terminal.Operation != "Apply" || terminal.ExecutionOutcome != "Succeeded" || terminal.TerraformExitCode != 0 || !terminal.ArtifactsReady || terminal.PlanRef != plan.Status.PlanRef || terminal.PlanDigest != plan.Status.PlanDigest {
			t.Fatalf("Destroy did not retain the exact saved-plan terminal identity: %+v %v", terminal, err)
		}
		recordGateEvidence(t, prefix, "destroy-recovered-terminal", map[string]any{"terminal": terminal, "ref": terminalRef, "environmentNotFound": true, "cloudNotFound": true})
		var sentinel corev1.ConfigMap
		if err := kube.Get(ctx, client.ObjectKey{Namespace: prefix, Name: "unrelated"}, &sentinel); err != nil || sentinel.Data["sentinel"] != prefix {
			t.Fatal("unrelated runtime resource changed: ", err)
		}
	}) {
		return
	}
	apiRequest(t, "DELETE", "/api/classes/"+class.Name, map[string]string{"uid": string(class.UID), "confirmName": class.Name}, http.StatusAccepted)
	wait(t, "class cleanup", func() (bool, error) {
		err := kube.Get(ctx, client.ObjectKeyFromObject(class), class)
		return apierrors.IsNotFound(err), client.IgnoreNotFound(err)
	})
	complete = safetyClean
}
