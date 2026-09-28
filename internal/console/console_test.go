package console

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	platformv1alpha1 "github.com/miku-wwl/kube-platform-control-plane/api/v1alpha1"
)

func TestApprovePlanCreatesExactImmutableBinding(t *testing.T) {
	server, kubeClient, run := approvalFixture(t)
	request := ApprovalRequest{PlanRunUID: string(run.UID), PlanDigest: run.Status.PlanDigest, ExecutionContextDigest: run.Spec.ExecutionContextDigest, EffectivePlanInputDigest: run.Status.EffectivePlanInputDigest, PlanReportRef: run.Status.PlanReportRef, PlanReportDigest: run.Status.PlanReportDigest}
	body, _ := json.Marshal(request)
	httpRequest := httptest.NewRequest(http.MethodPost, "/api/terraform-runs/team-a/plan-a/approve", bytes.NewReader(body))
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, httpRequest)
	if response.Code != http.StatusCreated {
		t.Fatalf("status %d: %s", response.Code, response.Body.String())
	}
	var approvals platformv1alpha1.ChangeApprovalList
	if err := kubeClient.List(context.Background(), &approvals, client.InNamespace(run.Namespace)); err != nil {
		t.Fatal(err)
	}
	if len(approvals.Items) != 1 {
		t.Fatalf("expected one ChangeApproval, got %d", len(approvals.Items))
	}
	actual := approvals.Items[0].Spec
	if actual.PlanRunRef.Name != run.Name || actual.PlanRunUID != string(run.UID) || actual.PlanDigest != run.Status.PlanDigest || actual.ExecutionContextDigest != run.Spec.ExecutionContextDigest || actual.EffectivePlanInputDigest != run.Status.EffectivePlanInputDigest || actual.PlanReportRef != run.Status.PlanReportRef || actual.PlanReportDigest != run.Status.PlanReportDigest {
		t.Fatalf("approval did not preserve all exact plan bindings: %+v", actual)
	}
	var applyRuns platformv1alpha1.TerraformRunList
	if err := kubeClient.List(context.Background(), &applyRuns, client.InNamespace(run.Namespace)); err != nil {
		t.Fatal(err)
	}
	if len(applyRuns.Items) != 1 {
		t.Fatalf("platform API created or altered TerraformRuns: got %d", len(applyRuns.Items))
	}
}

func TestApprovePlanRejectsStaleBinding(t *testing.T) {
	server, kubeClient, run := approvalFixture(t)
	body, _ := json.Marshal(ApprovalRequest{PlanRunUID: string(run.UID), PlanDigest: "sha256:stale", ExecutionContextDigest: run.Spec.ExecutionContextDigest, EffectivePlanInputDigest: run.Status.EffectivePlanInputDigest, PlanReportRef: run.Status.PlanReportRef, PlanReportDigest: run.Status.PlanReportDigest})
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/terraform-runs/team-a/plan-a/approve", bytes.NewReader(body)))
	if response.Code != http.StatusConflict {
		t.Fatalf("expected stale binding conflict, got %d: %s", response.Code, response.Body.String())
	}
	var approvals platformv1alpha1.ChangeApprovalList
	if err := kubeClient.List(context.Background(), &approvals, client.InNamespace(run.Namespace)); err != nil {
		t.Fatal(err)
	}
	if len(approvals.Items) != 0 {
		t.Fatalf("stale plan created an approval: %+v", approvals.Items)
	}
}

func TestApproveDestroyPlanBehindMutationFence(t *testing.T) {
	server, kubeClient, run := approvalFixture(t)
	run.Spec.PlanMode = "Destroy"
	if err := kubeClient.Update(context.Background(), run); err != nil {
		t.Fatal(err)
	}
	var stack platformv1alpha1.InfraStack
	if err := kubeClient.Get(context.Background(), client.ObjectKey{Namespace: run.Namespace, Name: "app-infra"}, &stack); err != nil {
		t.Fatal(err)
	}
	stack.Spec.MutationFence = true
	if err := kubeClient.Update(context.Background(), &stack); err != nil {
		t.Fatal(err)
	}
	request := ApprovalRequest{PlanRunUID: string(run.UID), PlanDigest: run.Status.PlanDigest, ExecutionContextDigest: run.Spec.ExecutionContextDigest, EffectivePlanInputDigest: run.Status.EffectivePlanInputDigest, PlanReportRef: run.Status.PlanReportRef, PlanReportDigest: run.Status.PlanReportDigest}
	body, _ := json.Marshal(request)
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/terraform-runs/team-a/plan-a/approve", bytes.NewReader(body)))
	if response.Code != http.StatusCreated {
		t.Fatalf("destroy approval behind the durable fence should be allowed, got %d: %s", response.Code, response.Body.String())
	}
}

func TestApproveReconcilePlanRejectedBehindMutationFence(t *testing.T) {
	server, kubeClient, run := approvalFixture(t)
	var stack platformv1alpha1.InfraStack
	if err := kubeClient.Get(context.Background(), client.ObjectKey{Namespace: run.Namespace, Name: "app-infra"}, &stack); err != nil {
		t.Fatal(err)
	}
	stack.Spec.MutationFence = true
	if err := kubeClient.Update(context.Background(), &stack); err != nil {
		t.Fatal(err)
	}
	request := ApprovalRequest{PlanRunUID: string(run.UID), PlanDigest: run.Status.PlanDigest, ExecutionContextDigest: run.Spec.ExecutionContextDigest, EffectivePlanInputDigest: run.Status.EffectivePlanInputDigest, PlanReportRef: run.Status.PlanReportRef, PlanReportDigest: run.Status.PlanReportDigest}
	body, _ := json.Marshal(request)
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/terraform-runs/team-a/plan-a/approve", bytes.NewReader(body)))
	if response.Code != http.StatusConflict || !strings.Contains(response.Body.String(), "MutationFenced") {
		t.Fatalf("reconcile approval should remain blocked by the durable fence, got %d: %s", response.Code, response.Body.String())
	}
}

func TestEnvironmentSummaryRequiresApprovalForChangesPresentPlan(t *testing.T) {
	environment := &platformv1alpha1.PlatformEnvironment{ObjectMeta: metav1.ObjectMeta{Name: "app", Namespace: "team-a", Generation: 1}}
	run := &platformv1alpha1.TerraformRun{Spec: platformv1alpha1.TerraformRunSpec{Operation: "Plan"}, Status: platformv1alpha1.TerraformRunStatus{ExecutionOutcome: "ChangesPresent", HasChanges: true}}
	summary := environmentSummary(environment, nil, nil, nil, []*platformv1alpha1.TerraformRun{run}, nil)
	if summary.LatestApproval != "Required" {
		t.Fatalf("expected approval to be required for ChangesPresent plan, got %q", summary.LatestApproval)
	}
}

func TestDraftValidationUsesReadyClassAndPolicyBounds(t *testing.T) {
	class := &platformv1alpha1.EnvironmentClass{ObjectMeta: metav1.ObjectMeta{Name: "dev-small", Generation: 2}, Spec: platformv1alpha1.EnvironmentClassSpec{Target: platformv1alpha1.TargetReference{Provider: "Kind", ClusterName: "kind-local", Region: "us-east-1"}, AllowedRegions: []string{"us-east-1"}, CapacityBounds: platformv1alpha1.CapacityBounds{MinNodeCount: 1, MaxNodeCount: 2, MaxEnvironments: 3}}, Status: platformv1alpha1.EnvironmentClassStatus{ObservedGeneration: 2, Conditions: []metav1.Condition{{Type: "Ready", Status: metav1.ConditionTrue, Reason: "ClassValidated"}}}}
	namespace := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default"}}
	server, _, err := testServer(t, class, namespace)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := server.validateEnvironmentRequest(context.Background(), CreateEnvironmentRequest{Name: "too-large", Namespace: "default", ClassRef: "dev-small", NodeCount: 3}); err == nil {
		t.Fatal("expected capacity policy rejection")
	}
	environment, _, err := server.validateEnvironmentRequest(context.Background(), CreateEnvironmentRequest{Name: "valid-env", Namespace: "default", ClassRef: "dev-small", NodeCount: 2})
	if err != nil {
		t.Fatal(err)
	}
	if environment.Spec.ClassRef == nil || environment.Spec.ClassRef.Name != "dev-small" || environment.Spec.DesiredState != platformv1alpha1.DesiredStatePresent || environment.Spec.Target.Provider != "" {
		t.Fatalf("unexpected or untyped draft: %+v", environment.Spec)
	}
	intent, err := (DeterministicDraftGenerator{}).Generate(context.Background(), DraftRequest{Description: "Create a small development environment with redis and 3 nodes", ClassRef: "dev-small", Namespace: "default"})
	if err != nil {
		t.Fatal(err)
	}
	if intent.NodeCount != 3 || !intent.ValkeyEnabled || intent.ClassRef != "dev-small" || len(intent.Name) == 0 {
		t.Fatalf("unexpected deterministic intent: %+v", intent)
	}
}

func TestArtifactEndpointRejectsNonLocalHost(t *testing.T) {
	if err := validateArtifactEndpoint("http://localhost:4566"); err != nil {
		t.Fatalf("local endpoint rejected: %v", err)
	}
	for _, endpoint := range []string{"https://localhost:4566", "http://example.com:4566", "http://127.0.0.1.evil.test:4566"} {
		if err := validateArtifactEndpoint(endpoint); err == nil {
			t.Fatalf("non-local endpoint %q accepted", endpoint)
		}
	}
}

func TestDraftRequiresSeparateExplicitEnvironmentSubmit(t *testing.T) {
	class := &platformv1alpha1.EnvironmentClass{ObjectMeta: metav1.ObjectMeta{Name: "dev-small", Generation: 1}, Spec: platformv1alpha1.EnvironmentClassSpec{Target: platformv1alpha1.TargetReference{Provider: "Kind", ClusterName: "kind-local", Region: "us-east-1"}, CapacityBounds: platformv1alpha1.CapacityBounds{MinNodeCount: 1, MaxNodeCount: 3, MaxEnvironments: 4}}, Status: platformv1alpha1.EnvironmentClassStatus{ObservedGeneration: 1, Conditions: []metav1.Condition{{Type: "Ready", Status: metav1.ConditionTrue}}}}
	namespace := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default"}}
	server, kubeClient, err := testServer(t, class, namespace)
	if err != nil {
		t.Fatal(err)
	}
	draftBody, _ := json.Marshal(DraftRequest{Description: "Create a small development environment", Namespace: "default", ClassRef: "dev-small"})
	draftResponse := httptest.NewRecorder()
	server.Handler().ServeHTTP(draftResponse, httptest.NewRequest(http.MethodPost, "/api/drafts", bytes.NewReader(draftBody)))
	if draftResponse.Code != http.StatusOK {
		t.Fatalf("draft status %d: %s", draftResponse.Code, draftResponse.Body.String())
	}
	var draft DraftResponse
	if err := json.Unmarshal(draftResponse.Body.Bytes(), &draft); err != nil {
		t.Fatal(err)
	}
	if !draft.Valid || draft.YAML == "" {
		t.Fatalf("expected valid typed preview, got %+v", draft)
	}
	var environments platformv1alpha1.PlatformEnvironmentList
	if err := kubeClient.List(context.Background(), &environments); err != nil {
		t.Fatal(err)
	}
	if len(environments.Items) != 0 {
		t.Fatal("draft generation created a PlatformEnvironment without explicit submit")
	}
	createBody, _ := json.Marshal(CreateEnvironmentRequest{Name: draft.Intent.Name, Namespace: draft.Intent.Namespace, ClassRef: draft.Intent.ClassRef, Region: draft.Intent.Region, NodeCount: draft.Intent.NodeCount, ValkeyEnabled: draft.Intent.ValkeyEnabled, ValkeyShards: draft.Intent.ValkeyShards, ValkeyReplicas: draft.Intent.ValkeyReplicas})
	createResponse := httptest.NewRecorder()
	server.Handler().ServeHTTP(createResponse, httptest.NewRequest(http.MethodPost, "/api/environments", bytes.NewReader(createBody)))
	if createResponse.Code != http.StatusCreated {
		t.Fatalf("explicit submit status %d: %s", createResponse.Code, createResponse.Body.String())
	}
	if err := kubeClient.List(context.Background(), &environments); err != nil {
		t.Fatal(err)
	}
	if len(environments.Items) != 1 || environments.Items[0].Spec.DesiredState != platformv1alpha1.DesiredStatePresent {
		t.Fatalf("explicit submit did not create exactly one typed environment: %+v", environments.Items)
	}
}

func TestBedrockProviderRejectsRemoteEndpoint(t *testing.T) {
	t.Setenv("AI_PROVIDER", "bedrock")
	t.Setenv("AWS_ENDPOINT_URL", "https://bedrock-runtime.us-east-1.amazonaws.com")
	if _, _, err := NewDraftGeneratorFromEnvironment(); err == nil {
		t.Fatal("remote Bedrock endpoint was accepted")
	}
}

func approvalFixture(t *testing.T) (*Server, client.Client, *platformv1alpha1.TerraformRun) {
	t.Helper()
	expires := metav1.NewTime(time.Now().Add(30 * time.Minute))
	now := metav1.Now()
	run := &platformv1alpha1.TerraformRun{ObjectMeta: metav1.ObjectMeta{Name: "plan-a", Namespace: "team-a", UID: types.UID("plan-uid-1"), Generation: 1, CreationTimestamp: now}, Spec: platformv1alpha1.TerraformRunSpec{StackRef: corev1.LocalObjectReference{Name: "app-infra"}, InfraStackGeneration: 4, Operation: "Plan", ExecutionContextDigest: "sha256:context", RuntimeTargetIdentityDigest: "sha256:runtime", InfrastructureExecutionIdentityDigest: "sha256:infra", RunnerServiceAccountName: "runner", RunnerServiceAccountIdentityDigest: "sha256:runner"}, Status: platformv1alpha1.TerraformRunStatus{ExecutionOutcome: "ChangesPresent", HasChanges: true, ArtifactsReady: true, EvidenceCaptured: true, PlanRef: "runs/plan-uid-1/plan.binary", PlanDigest: "sha256:plan", SourceBundleRef: "runs/plan-uid-1/source-bundle.tar.zst", SourceBundleDigest: "sha256:source", TerminalResultRef: "runs/plan-uid-1/terminal-result.json", TerminalResultDigest: "sha256:terminal", EffectivePlanInputDigest: "sha256:input", PlanReportRef: "runs/plan-uid-1/plan-report.json", PlanReportDigest: "sha256:report", PlanExpiresAt: &expires, Conditions: []metav1.Condition{{Type: "Ready", Status: metav1.ConditionTrue, Reason: "PlanComplete"}}}}
	stack := &platformv1alpha1.InfraStack{ObjectMeta: metav1.ObjectMeta{Name: "app-infra", Namespace: "team-a", Generation: 4}, Spec: platformv1alpha1.InfraStackSpec{RunnerServiceAccountName: "runner", RunnerServiceAccountIdentityDigest: "sha256:runner"}, Status: platformv1alpha1.InfraStackStatus{LatestPlanRunRef: &corev1.LocalObjectReference{Name: run.Name}, Conditions: []metav1.Condition{{Type: "Ready", Status: metav1.ConditionFalse, Reason: "WaitingApproval"}}}}
	server, kubeClient, err := testServer(t, run, stack)
	if err != nil {
		t.Fatal(err)
	}
	return server, kubeClient, run
}

func testServer(t *testing.T, objects ...client.Object) (*Server, client.Client, error) {
	t.Helper()
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	_ = platformv1alpha1.AddToScheme(scheme)
	kubeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).Build()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	server, err := NewServer(kubeClient, nil, "deterministic", DeterministicDraftGenerator{}, logger)
	return server, kubeClient, err
}

func TestSlugIsDNSLabel(t *testing.T) {
	name := slugName(strings.Repeat("中文", 8), "dev-small")
	if len(name) == 0 || len(name) > 63 {
		t.Fatalf("invalid slug %q", name)
	}
}
