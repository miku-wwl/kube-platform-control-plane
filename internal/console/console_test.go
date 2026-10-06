package console

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
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
	var view ApprovalView
	if err := json.Unmarshal(response.Body.Bytes(), &view); err != nil {
		t.Fatal(err)
	}
	if view.State != "Recorded" || view.ApprovedAt != nil {
		t.Fatalf("immutable approval should be recorded without invented controller status: %+v", view)
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

func TestReadModelIgnoresMismatchedApprovalBinding(t *testing.T) {
	_, _, run := approvalFixture(t)
	approval := platformv1alpha1.ChangeApproval{ObjectMeta: metav1.ObjectMeta{Name: "decision", Namespace: run.Namespace}, Spec: platformv1alpha1.ChangeApprovalSpec{
		PlanRunRef: corev1.LocalObjectReference{Name: run.Name}, PlanRunUID: string(run.UID), PlanDigest: run.Status.PlanDigest,
		ExecutionContextDigest: run.Spec.ExecutionContextDigest, EffectivePlanInputDigest: run.Status.EffectivePlanInputDigest,
		PlanReportRef: run.Status.PlanReportRef, PlanReportDigest: run.Status.PlanReportDigest,
	}}
	if related := matchingApprovals([]platformv1alpha1.ChangeApproval{approval}, run); len(related) != 1 || runView(run, related).Approval.State != "Recorded" {
		t.Fatal("valid immutable approval was not displayed as recorded")
	}
	approval.Spec.PlanReportDigest = "sha256:stale"
	if related := matchingApprovals([]platformv1alpha1.ChangeApproval{approval}, run); len(related) != 0 || runView(run, related).Approval != nil {
		t.Fatal("mismatched approval was displayed as approval of the current plan")
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

func TestOllamaDraftGeneratorContract(t *testing.T) {
	const intentJSON = `{"name":"demo-dev","region":"us-east-1","nodeCount":2,"valkeyEnabled":true,"valkeyShards":1,"valkeyReplicas":1}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/chat" {
			t.Errorf("unexpected Ollama request: %s %s", r.Method, r.URL.Path)
		}
		var request struct {
			Model   string         `json:"model"`
			Stream  bool           `json:"stream"`
			Format  map[string]any `json:"format"`
			Options map[string]any `json:"options"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Errorf("decode chat request: %v", err)
		}
		if request.Model != "phi4-mini:latest" || request.Stream || request.Format["type"] != "object" || request.Format["additionalProperties"] != false || request.Options["temperature"] != float64(0) || request.Options["num_predict"] != float64(256) || request.Options["num_ctx"] != float64(4096) {
			t.Errorf("unexpected model or response format: %+v", request)
		}
		if required, ok := request.Format["required"].([]any); !ok || len(required) != 6 {
			t.Errorf("schema must require all six intent fields: %+v", request.Format)
		}
		properties, ok := request.Format["properties"].(map[string]any)
		if !ok {
			t.Fatal("intent properties missing from schema")
		}
		for key, expected := range map[string]any{"name": "demo-dev", "region": "us-east-1", "nodeCount": float64(2)} {
			property, ok := properties[key].(map[string]any)
			if !ok || property["const"] != expected {
				t.Errorf("explicit %s is not constrained in schema: %+v", key, property)
			}
		}
		_, _ = io.WriteString(w, `{"message":{"role":"assistant","content":`+mustJSONString(t, intentJSON)+`,"tool_calls":[]},"done":true,"done_reason":"stop"}`)
	}))
	defer server.Close()

	generator, err := newOllamaDraftGenerator(server.URL, "phi4-mini:latest", server.Client())
	if err != nil {
		t.Fatal(err)
	}
	intent, err := generator.Generate(context.Background(), DraftRequest{Description: "Create an environment with cache and 2 nodes", Name: "demo-dev", Region: "us-east-1", NodeCount: 2, Namespace: "default", ClassRef: "dev-small"})
	if err != nil {
		t.Fatal(err)
	}
	if intent.Name != "demo-dev" || intent.Namespace != "default" || intent.ClassRef != "dev-small" || intent.NodeCount != 2 || intent.Region != "us-east-1" || !intent.ValkeyEnabled || intent.ValkeyShards != 1 || intent.ValkeyReplicas != 1 {
		t.Fatalf("unexpected typed intent: %+v", intent)
	}
}

func TestOllamaStructuredOutputFailsClosed(t *testing.T) {
	valid := `{"name":"demo-dev","region":"us-east-1","nodeCount":1,"valkeyEnabled":false,"valkeyShards":0,"valkeyReplicas":0}`
	cases := map[string]string{
		"prose":            "Here is the JSON: " + valid,
		"malformed":        `{"name":`,
		"unknown field":    strings.TrimSuffix(valid, "}") + `,"apiVersion":"v1"}`,
		"missing field":    `{"name":"demo-dev","region":"us-east-1","nodeCount":1,"valkeyEnabled":false,"valkeyShards":0}`,
		"null field":       `{"name":null,"region":"us-east-1","nodeCount":1,"valkeyEnabled":false,"valkeyShards":0,"valkeyReplicas":0}`,
		"wrong type":       `{"name":"demo-dev","region":"us-east-1","nodeCount":"1","valkeyEnabled":false,"valkeyShards":0,"valkeyReplicas":0}`,
		"invalid name":     `{"name":"Invalid Name","region":"us-east-1","nodeCount":1,"valkeyEnabled":false,"valkeyShards":0,"valkeyReplicas":0}`,
		"invalid capacity": `{"name":"demo-dev","region":"us-east-1","nodeCount":0,"valkeyEnabled":false,"valkeyShards":0,"valkeyReplicas":0}`,
		"unexpected cache": `{"name":"demo-dev","region":"us-east-1","nodeCount":1,"valkeyEnabled":false,"valkeyShards":1,"valkeyReplicas":0}`,
	}
	for name, content := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := decodeOllamaIntent(content); err == nil {
				t.Fatal("invalid structured output was accepted")
			}
		})
	}
	for _, endpoint := range []string{"https://example.com:443", "http://example.com:11434", "http://127.0.0.1", "http://127.0.0.1:11434/api", "http://user:password@127.0.0.1:11434", "http://127.0.0.1:11434?key=value"} {
		if _, err := newOllamaDraftGenerator(endpoint, defaultOllamaModel, nil); err == nil {
			t.Errorf("unsafe inference endpoint was accepted: %s", endpoint)
		}
	}
}

func TestOllamaDraftAppliesExplicitCacheIntentDeterministically(t *testing.T) {
	cases := []struct {
		name        string
		description string
		wantEnabled bool
	}{
		{name: "explicit enable", description: "Create a development environment with cache enabled", wantEnabled: true},
		{name: "explicit disable", description: "Create a development environment without cache", wantEnabled: false},
		{name: "ambiguous request", description: "Create a small development environment", wantEnabled: false},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			intent := applyExplicitValkeyIntent(test.description, DraftIntent{ValkeyEnabled: !test.wantEnabled})
			if intent.ValkeyEnabled != test.wantEnabled {
				t.Fatalf("unexpected Valkey choice for %q: %+v", test.description, intent)
			}
			if test.wantEnabled && (intent.ValkeyShards != 1 || intent.ValkeyReplicas != 1) {
				t.Fatalf("enabled Valkey did not receive supported default topology: %+v", intent)
			}
			if !test.wantEnabled && (intent.ValkeyShards != 0 || intent.ValkeyReplicas != 0) {
				t.Fatalf("disabled Valkey retained topology: %+v", intent)
			}
		})
	}
}

func TestMalformedOllamaDraftDoesNotCreateResources(t *testing.T) {
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"message":{"content":"not JSON"},"done":true,"done_reason":"stop"}`)
	}))
	defer model.Close()
	generator, err := newOllamaDraftGenerator(model.URL, "phi4-mini:latest", model.Client())
	if err != nil {
		t.Fatal(err)
	}
	class := readyDraftClass()
	namespace := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default"}}
	_, kubeClient, err := testServer(t, class, namespace)
	if err != nil {
		t.Fatal(err)
	}
	api, err := NewServer(kubeClient, nil, ollamaProvider, generator, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(DraftRequest{Description: "Create a development environment", Namespace: "default", ClassRef: "dev-small"})
	response := httptest.NewRecorder()
	api.Handler().ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/drafts", bytes.NewReader(body)))
	if response.Code != http.StatusBadGateway {
		t.Fatalf("malformed model output returned %d: %s", response.Code, response.Body.String())
	}
	var environments platformv1alpha1.PlatformEnvironmentList
	var approvals platformv1alpha1.ChangeApprovalList
	var runs platformv1alpha1.TerraformRunList
	for _, objectList := range []client.ObjectList{&environments, &approvals, &runs} {
		if err := kubeClient.List(context.Background(), objectList); err != nil {
			t.Fatal(err)
		}
	}
	if len(environments.Items) != 0 || len(approvals.Items) != 0 || len(runs.Items) != 0 {
		t.Fatal("malformed draft caused a platform resource or approval to be created")
	}
}

func TestOllamaProviderConfiguration(t *testing.T) {
	t.Setenv("AI_PROVIDER", "")
	t.Setenv("OLLAMA_ENDPOINT", "")
	t.Setenv("OLLAMA_MODEL", "")
	provider, generator, err := NewDraftGeneratorFromEnvironment()
	if err != nil {
		t.Fatal(err)
	}
	local, ok := generator.(*OllamaDraftGenerator)
	if !ok || provider != "ollama" || local.endpoint != defaultOllamaEndpoint || local.model != defaultOllamaModel {
		t.Fatalf("unexpected default provider: %s, %+v", provider, generator)
	}
	t.Setenv("AI_PROVIDER", "foundry-local")
	if _, _, err := NewDraftGeneratorFromEnvironment(); err == nil {
		t.Fatal("retired inference provider was silently accepted")
	}
	t.Setenv("AI_PROVIDER", "deterministic")
	if provider, _, err := NewDraftGeneratorFromEnvironment(); err != nil || provider != "deterministic" {
		t.Fatalf("explicit offline provider failed: %s, %v", provider, err)
	}
}

func TestOllamaResponseFailsClosed(t *testing.T) {
	const valid = `{"name":"demo-dev","region":"us-east-1","nodeCount":1,"valkeyEnabled":false,"valkeyShards":0,"valkeyReplicas":0}`
	cases := []struct {
		name   string
		body   string
		status int
	}{
		{"unfinished", `{"message":{"content":` + mustJSONString(t, valid) + `},"done":false}`, 200},
		{"truncated", `{"message":{"content":` + mustJSONString(t, valid) + `},"done":true,"done_reason":"length"}`, 200},
		{"tool call", `{"message":{"content":` + mustJSONString(t, valid) + `,"tool_calls":[{}]},"done":true,"done_reason":"stop"}`, 200},
		{"empty", `{"message":{"content":""},"done":true,"done_reason":"stop"}`, 200},
		{"malformed envelope", `not JSON`, 200},
		{"missing model", `{"error":"private server details"}`, 404},
		{"oversized", strings.Repeat("x", maxOllamaResponse+1), 200},
		{"explicit name changed", `{"message":{"content":` + mustJSONString(t, valid) + `},"done":true,"done_reason":"stop"}`, 200},
	}
	for _, scenario := range cases {
		t.Run(scenario.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(scenario.status)
				_, _ = io.WriteString(w, scenario.body)
			}))
			defer server.Close()
			generator, err := newOllamaDraftGenerator(server.URL, defaultOllamaModel, server.Client())
			if err != nil {
				t.Fatal(err)
			}
			_, err = generator.Generate(context.Background(), DraftRequest{Name: "requested-name"})
			if err == nil || strings.Contains(err.Error(), "private server details") {
				t.Fatalf("unsafe response accepted or server details leaked: %v", err)
			}
		})
	}
}

func TestOllamaUnavailableDoesNotFallBack(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	endpoint := server.URL
	server.Close()
	generator, err := newOllamaDraftGenerator(endpoint, defaultOllamaModel, nil)
	if err != nil {
		t.Fatal(err)
	}
	if intent, err := generator.Generate(context.Background(), DraftRequest{Description: "Create a small environment", ClassRef: "dev-small"}); err == nil || intent.Name != "" {
		t.Fatalf("unavailable inference must not generate a fallback draft: %+v, %v", intent, err)
	}
}

func TestOllamaLiveDraftScenarios(t *testing.T) {
	if os.Getenv("PCP_RUN_OLLAMA_INTEGRATION") != "1" {
		t.Skip("set PCP_RUN_OLLAMA_INTEGRATION=1 to run live local inference scenarios")
	}
	provider, generator, err := NewDraftGeneratorFromEnvironment()
	if err != nil {
		t.Fatal(err)
	}
	if provider != ollamaProvider {
		t.Fatalf("live local inference expected provider %q, got %q", ollamaProvider, provider)
	}
	t.Run("missing local model", func(t *testing.T) {
		local := generator.(*OllamaDraftGenerator)
		missing, err := newOllamaDraftGenerator(local.endpoint, "pcp-ollama-integration-missing-model", nil)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := missing.Generate(context.Background(), DraftRequest{Description: "Create a small environment"}); err == nil || !strings.Contains(err.Error(), "HTTP 404") {
			t.Fatalf("missing model must fail closed with HTTP 404: %v", err)
		}
	})
	class := readyDraftClass()
	namespace := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default"}}
	server, kubeClient, err := testServer(t, class, namespace)
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name        string
		description string
		wantNodes   int32
		wantValkey  bool
	}{
		{name: "small development", description: "Create a small development environment", wantNodes: 1},
		{name: "cache and two nodes", description: "Create a development environment with cache enabled and 2 nodes", wantNodes: 2, wantValkey: true},
		{name: "Chinese cache request", description: "创建一个启用缓存、有 2 个节点的开发环境", wantNodes: 2, wantValkey: true},
		{name: "Chinese cache disabled", description: "创建一个有 1 个节点的开发环境，不要缓存", wantNodes: 1},
	}
	for _, scenario := range cases {
		t.Run(scenario.name, func(t *testing.T) {
			intent, err := generator.Generate(context.Background(), DraftRequest{Description: scenario.description, Namespace: "default", ClassRef: "dev-small"})
			if err != nil {
				t.Fatal(err)
			}
			if intent.NodeCount != scenario.wantNodes || intent.ValkeyEnabled != scenario.wantValkey {
				t.Fatalf("live model missed requested intent: %+v", intent)
			}
			candidate, _, err := server.validateEnvironmentRequest(context.Background(), CreateEnvironmentRequest{Name: intent.Name, Namespace: intent.Namespace, ClassRef: intent.ClassRef, Region: intent.Region, NodeCount: intent.NodeCount, ValkeyEnabled: intent.ValkeyEnabled, ValkeyShards: intent.ValkeyShards, ValkeyReplicas: intent.ValkeyReplicas})
			if err != nil {
				t.Fatalf("deterministic validation rejected live draft: %v", err)
			}
			if candidate.Spec.ClassRef == nil || candidate.Spec.ClassRef.Name != "dev-small" {
				t.Fatalf("draft is not a typed class-based PlatformEnvironment: %+v", candidate.Spec)
			}
			preview, err := environmentYAML(candidate)
			if err != nil || preview == "" {
				t.Fatalf("typed draft did not render a preview: %q, %v", preview, err)
			}
		})
	}
	t.Run("explicit fields", func(t *testing.T) {
		intent, err := generator.Generate(context.Background(), DraftRequest{Description: "Create a small development environment without cache", Name: "ollama-explicit", Namespace: "default", ClassRef: "dev-small", Region: "us-east-1", NodeCount: 2})
		if err != nil {
			t.Fatal(err)
		}
		if intent.Name != "ollama-explicit" || intent.Region != "us-east-1" || intent.NodeCount != 2 || intent.ValkeyEnabled {
			t.Fatalf("explicit form fields were not preserved: %+v", intent)
		}
	})
	t.Run("ambiguous request", func(t *testing.T) {
		intent, err := generator.Generate(context.Background(), DraftRequest{Description: "Set up something useful for me", Namespace: "default", ClassRef: "dev-small"})
		if err != nil {
			return // A deterministic fail-closed rejection is safe for an ambiguous request.
		}
		candidate, _, validationErr := server.validateEnvironmentRequest(context.Background(), CreateEnvironmentRequest{Name: intent.Name, Namespace: intent.Namespace, ClassRef: intent.ClassRef, Region: intent.Region, NodeCount: intent.NodeCount, ValkeyEnabled: intent.ValkeyEnabled, ValkeyShards: intent.ValkeyShards, ValkeyReplicas: intent.ValkeyReplicas})
		if validationErr == nil && (candidate.Spec.Capacity.NodeCount != 1 || candidate.Spec.Valkey.Enabled) {
			t.Fatalf("ambiguous request was not handled conservatively: %+v", candidate.Spec)
		}
	})
	var environments platformv1alpha1.PlatformEnvironmentList
	if err := kubeClient.List(context.Background(), &environments); err != nil {
		t.Fatal(err)
	}
	if len(environments.Items) != 0 {
		t.Fatal("live draft generation submitted a PlatformEnvironment without explicit human action")
	}
}

func readyDraftClass() *platformv1alpha1.EnvironmentClass {
	return &platformv1alpha1.EnvironmentClass{ObjectMeta: metav1.ObjectMeta{Name: "dev-small", Generation: 1}, Spec: platformv1alpha1.EnvironmentClassSpec{Target: platformv1alpha1.TargetReference{Provider: "Kind", ClusterName: "kind-local", Region: "us-east-1"}, AllowedRegions: []string{"us-east-1"}, CapacityBounds: platformv1alpha1.CapacityBounds{MinNodeCount: 1, MaxNodeCount: 2, MaxEnvironments: 3}}, Status: platformv1alpha1.EnvironmentClassStatus{ObservedGeneration: 1, Conditions: []metav1.Condition{{Type: "Ready", Status: metav1.ConditionTrue}}}}
}

func mustJSONString(t *testing.T, value string) string {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(encoded)
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
