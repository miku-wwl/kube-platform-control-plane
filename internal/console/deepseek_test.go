package console

import (
	"bytes"
	"context"
	"crypto/x509"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	platformv1alpha1 "github.com/miku-wwl/kube-platform-control-plane/api/v1alpha1"
)

type deepSeekRoundTripper func(*http.Request) (*http.Response, error)

func (f deepSeekRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

const validDeepSeekIntent = `{"name":"demo-development","region":"us-east-1","nodeCount":1,"valkeyEnabled":false,"valkeyShards":0,"valkeyReplicas":0}`

func deepSeekHTTPResponse(request *http.Request, status int, body string, headers http.Header) *http.Response {
	if headers == nil {
		headers = make(http.Header)
	}
	return &http.Response{StatusCode: status, Header: headers, Body: io.NopCloser(strings.NewReader(body)), Request: request}
}

func deepSeekCompletionBody(t *testing.T, content any, finishReason, role string, count int, messageFields map[string]any) string {
	t.Helper()
	choices := make([]any, 0, count)
	for range count {
		message := map[string]any{"role": role, "content": content}
		for key, value := range messageFields {
			message[key] = value
		}
		choices = append(choices, map[string]any{"finish_reason": finishReason, "message": message})
	}
	encoded, err := json.Marshal(map[string]any{
		"id":      "chatcmpl-test",
		"model":   "deepseek-flash",
		"choices": choices,
		"usage":   map[string]int{"prompt_tokens": 17, "completion_tokens": 8, "total_tokens": 25},
	})
	if err != nil {
		t.Fatal(err)
	}
	return string(encoded)
}

func deepSeekTestGenerator(t *testing.T, status int, body string, headers http.Header) *DeepSeekDraftGenerator {
	t.Helper()
	client := &http.Client{Transport: deepSeekRoundTripper(func(request *http.Request) (*http.Response, error) {
		return deepSeekHTTPResponse(request, status, body, headers), nil
	})}
	generator, err := newDeepSeekDraftGenerator(defaultDeepSeekURL, defaultDeepSeekModel, "test-key-never-log", client)
	if err != nil {
		t.Fatal(err)
	}
	return generator
}

func assertDeepSeekError(t *testing.T, err error, code string) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected %s, got nil", code)
	}
	var providerErr *DraftProviderError
	if !errors.As(err, &providerErr) {
		t.Fatalf("expected DraftProviderError, got %T: %v", err, err)
	}
	if providerErr.Code != code {
		t.Fatalf("expected code %s, got %+v", code, providerErr)
	}
}

func TestDeepSeekChatCompletionsContractAndExplicitFields(t *testing.T) {
	content := `{"name":"explicit-dev","region":"ap-southeast-2","nodeCount":2,"valkeyEnabled":true,"valkeyShards":1,"valkeyReplicas":1}`
	var observed DeepSeekUsage
	generator := deepSeekTestGenerator(t, http.StatusOK, deepSeekCompletionBody(t, content, "stop", "assistant", 1, nil), http.Header{"X-Request-Id": []string{"req-2026-01"}})
	generator.usageObserver = func(usage DeepSeekUsage) { observed = usage }

	// Replace the fixture transport with one that checks the outgoing HTTPS request.
	clientCopy := *generator.client
	clientCopy.Transport = deepSeekRoundTripper(func(request *http.Request) (*http.Response, error) {
		if request.Method != http.MethodPost || request.URL.String() != defaultDeepSeekURL+"/chat/completions" {
			t.Errorf("unexpected DeepSeek endpoint: %s %s", request.Method, request.URL)
		}
		if request.Header.Get("Authorization") != "Bearer test-key-never-log" || request.Header.Get("Content-Type") != "application/json" {
			t.Errorf("missing required request headers")
		}
		var payload map[string]any
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		if payload["model"] != "deepseek-flash" || payload["stream"] != false || payload["max_tokens"] != float64(512) || payload["temperature"] != float64(0) {
			t.Errorf("unexpected request limits/model: %#v", payload)
		}
		if _, exists := payload["tools"]; exists {
			t.Error("DeepSeek request must not enable tool calls")
		}
		responseFormat, _ := payload["response_format"].(map[string]any)
		thinking, _ := payload["thinking"].(map[string]any)
		if responseFormat["type"] != "json_object" || thinking["type"] != "disabled" {
			t.Errorf("unexpected structured response settings: response_format=%v thinking=%v", responseFormat, thinking)
		}
		messages, _ := payload["messages"].([]any)
		if len(messages) != 2 || !strings.Contains(messages[0].(map[string]any)["content"].(string), "JSON") {
			t.Errorf("system prompt must require JSON and exactly two messages: %#v", messages)
		}
		var userInput map[string]any
		if err := json.Unmarshal([]byte(messages[1].(map[string]any)["content"].(string)), &userInput); err != nil {
			t.Fatalf("decode constrained user input: %v", err)
		}
		if userInput["explicitName"] != "explicit-dev" || userInput["explicitRegion"] != "ap-southeast-2" || userInput["explicitNodeCount"] != float64(2) {
			t.Errorf("explicit fields not supplied to model: %#v", userInput)
		}
		return deepSeekHTTPResponse(request, http.StatusOK, deepSeekCompletionBody(t, content, "stop", "assistant", 1, nil), http.Header{"X-Request-Id": []string{"req-2026-01"}}), nil
	})
	generator.client = &clientCopy

	intent, err := generator.Generate(context.Background(), DraftRequest{Description: "Create a development environment with cache", Name: "explicit-dev", Region: "ap-southeast-2", NodeCount: 2, Namespace: "qa-team", ClassRef: "dev-small"})
	if err != nil {
		t.Fatal(err)
	}
	if intent.Name != "explicit-dev" || intent.Namespace != "qa-team" || intent.ClassRef != "dev-small" || intent.Region != "ap-southeast-2" || intent.NodeCount != 2 || !intent.ValkeyEnabled || intent.ValkeyShards != 1 || intent.ValkeyReplicas != 1 {
		t.Fatalf("unexpected typed intent: %+v", intent)
	}
	if observed.Model != "deepseek-flash" || observed.PromptTokens != 17 || observed.CompletionTokens != 8 || observed.TotalTokens != 25 || observed.RequestID != "req-2026-01" || observed.Duration < 0 {
		t.Fatalf("unexpected usage receipt: %+v", observed)
	}
}

func TestDeepSeekRejectsMalformedOrPrivilegedModelOutput(t *testing.T) {
	valid := validDeepSeekIntent
	cases := []struct {
		name         string
		content      any
		finishReason string
		role         string
		choiceCount  int
		fields       map[string]any
		wantCode     string
	}{
		{name: "malformed JSON", content: `{"name":`, finishReason: "stop", role: "assistant", choiceCount: 1, wantCode: "DeepSeekInvalidIntent"},
		{name: "missing field", content: `{"name":"demo-development","region":"us-east-1","nodeCount":1,"valkeyEnabled":false,"valkeyShards":0}`, finishReason: "stop", role: "assistant", choiceCount: 1, wantCode: "DeepSeekInvalidIntent"},
		{name: "null field", content: `{"name":null,"region":"us-east-1","nodeCount":1,"valkeyEnabled":false,"valkeyShards":0,"valkeyReplicas":0}`, finishReason: "stop", role: "assistant", choiceCount: 1, wantCode: "DeepSeekInvalidIntent"},
		{name: "extra target field", content: strings.TrimSuffix(valid, "}") + `,"target":"other-cluster"}`, finishReason: "stop", role: "assistant", choiceCount: 1, wantCode: "DeepSeekInvalidIntent"},
		{name: "invalid name", content: strings.Replace(valid, "demo-development", "bad name", 1), finishReason: "stop", role: "assistant", choiceCount: 1, wantCode: "DeepSeekInvalidIntent"},
		{name: "duplicate field", content: strings.TrimSuffix(valid, "}") + `,"nodeCount":99}`, finishReason: "stop", role: "assistant", choiceCount: 1, wantCode: "DeepSeekInvalidIntent"},
		{name: "trailing content", content: valid + ` {"apply":true}`, finishReason: "stop", role: "assistant", choiceCount: 1, wantCode: "DeepSeekInvalidIntent"},
		{name: "empty", content: "   ", finishReason: "stop", role: "assistant", choiceCount: 1, wantCode: "DeepSeekEmptyContent"},
		{name: "null content", content: nil, finishReason: "stop", role: "assistant", choiceCount: 1, wantCode: "DeepSeekEmptyContent"},
		{name: "truncated", content: valid, finishReason: "length", role: "assistant", choiceCount: 1, wantCode: "DeepSeekTruncatedResponse"},
		{name: "tool reason", content: valid, finishReason: "tool_calls", role: "assistant", choiceCount: 1, wantCode: "DeepSeekToolCallUnsupported"},
		{name: "tool payload", content: valid, finishReason: "stop", role: "assistant", choiceCount: 1, fields: map[string]any{"tool_calls": []any{map[string]any{"type": "function"}}}, wantCode: "DeepSeekToolCallUnsupported"},
		{name: "function payload", content: valid, finishReason: "stop", role: "assistant", choiceCount: 1, fields: map[string]any{"function_call": map[string]any{"name": "apply"}}, wantCode: "DeepSeekToolCallUnsupported"},
		{name: "incomplete", content: valid, finishReason: "content_filter", role: "assistant", choiceCount: 1, wantCode: "DeepSeekIncompleteResponse"},
		{name: "wrong role", content: valid, finishReason: "stop", role: "user", choiceCount: 1, wantCode: "DeepSeekIncompleteResponse"},
		{name: "no choices", content: valid, finishReason: "stop", role: "assistant", choiceCount: 0, wantCode: "DeepSeekMalformedResponse"},
		{name: "ambiguous choices", content: valid, finishReason: "stop", role: "assistant", choiceCount: 2, wantCode: "DeepSeekMalformedResponse"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			body := deepSeekCompletionBody(t, test.content, test.finishReason, test.role, test.choiceCount, test.fields)
			generator := deepSeekTestGenerator(t, http.StatusOK, body, nil)
			intent, err := generator.Generate(context.Background(), DraftRequest{Description: "Create a small dev environment", Namespace: "qa", ClassRef: "dev-small"})
			assertDeepSeekError(t, err, test.wantCode)
			if intent.Name != "" {
				t.Fatalf("invalid model response returned an intent: %+v", intent)
			}
		})
	}
}

func TestDeepSeekRejectsChangedExplicitFields(t *testing.T) {
	generator := deepSeekTestGenerator(t, http.StatusOK, deepSeekCompletionBody(t, validDeepSeekIntent, "stop", "assistant", 1, nil), nil)
	intent, err := generator.Generate(context.Background(), DraftRequest{Description: "Create a small environment", Name: "requested-name", Region: "ap-southeast-2", NodeCount: 2, Namespace: "qa", ClassRef: "dev-small"})
	assertDeepSeekError(t, err, "DeepSeekExplicitFieldChanged")
	if intent.Name != "" {
		t.Fatalf("changed explicit fields returned an intent: %+v", intent)
	}
}

func TestDeepSeekStatusMappingDoesNotExposeResponseBodyOrKey(t *testing.T) {
	cases := []struct {
		status int
		code   string
	}{
		{http.StatusUnauthorized, "DeepSeekInvalidAPIKey"},
		{http.StatusPaymentRequired, "DeepSeekInsufficientBalance"},
		{http.StatusForbidden, "DeepSeekAuthorizationFailed"},
		{http.StatusTooManyRequests, "DeepSeekRateLimited"},
		{http.StatusBadRequest, "DeepSeekInvalidRequest"},
		{http.StatusUnprocessableEntity, "DeepSeekInvalidRequest"},
		{http.StatusServiceUnavailable, "DeepSeekServiceUnavailable"},
		{http.StatusBadGateway, "DeepSeekServiceUnavailable"},
	}
	for _, test := range cases {
		t.Run(http.StatusText(test.status), func(t *testing.T) {
			generator := deepSeekTestGenerator(t, test.status, `{"error":"private upstream detail test-secret-never-log"}`, nil)
			_, err := generator.Generate(context.Background(), DraftRequest{Description: "Create a small dev environment"})
			assertDeepSeekError(t, err, test.code)
			if strings.Contains(err.Error(), "private upstream detail") || strings.Contains(err.Error(), "test-secret-never-log") {
				t.Fatal("upstream response text or API key escaped into the error")
			}
		})
	}
}

func TestDeepSeekRefusesRedirectAndDoesNotRetry(t *testing.T) {
	calls := 0
	client := &http.Client{Transport: deepSeekRoundTripper(func(request *http.Request) (*http.Response, error) {
		calls++
		return deepSeekHTTPResponse(request, http.StatusFound, "", http.Header{"Location": []string{"https://attacker.invalid/collect"}}), nil
	})}
	generator, err := newDeepSeekDraftGenerator(defaultDeepSeekURL, defaultDeepSeekModel, "test-key-never-log", client)
	if err != nil {
		t.Fatal(err)
	}
	_, err = generator.Generate(context.Background(), DraftRequest{Description: "Create a small environment"})
	assertDeepSeekError(t, err, "DeepSeekRedirectRejected")
	if calls != 1 {
		t.Fatalf("redirect was followed or request retried: calls=%d", calls)
	}
}

func TestDeepSeekTimeoutAndCancellationAreClassified(t *testing.T) {
	t.Run("timeout", func(t *testing.T) {
		client := &http.Client{Timeout: 10 * time.Millisecond, Transport: deepSeekRoundTripper(func(request *http.Request) (*http.Response, error) {
			<-request.Context().Done()
			return nil, request.Context().Err()
		})}
		generator, err := newDeepSeekDraftGenerator(defaultDeepSeekURL, defaultDeepSeekModel, "test-key-never-log", client)
		if err != nil {
			t.Fatal(err)
		}
		_, err = generator.Generate(context.Background(), DraftRequest{Description: "Create a small environment"})
		assertDeepSeekError(t, err, "DeepSeekTimeout")
	})
	t.Run("cancelled context", func(t *testing.T) {
		client := &http.Client{Transport: deepSeekRoundTripper(func(request *http.Request) (*http.Response, error) {
			return nil, request.Context().Err()
		})}
		generator, err := newDeepSeekDraftGenerator(defaultDeepSeekURL, defaultDeepSeekModel, "test-key-never-log", client)
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		_, err = generator.Generate(ctx, DraftRequest{Description: "Create a small environment"})
		assertDeepSeekError(t, err, "DeepSeekRequestCancelled")
	})
}

func TestDeepSeekNetworkFailureCategories(t *testing.T) {
	cases := []struct {
		name string
		err  error
		code string
	}{
		{name: "dns", err: &net.DNSError{Err: "no such host", Name: "api.deepseek.com"}, code: "DeepSeekDNSFailure"},
		{name: "tls trust", err: x509.UnknownAuthorityError{}, code: "DeepSeekTLSFailure"},
		{name: "network", err: errors.New("connection refused"), code: "DeepSeekNetworkFailure"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			code, diagnostic := classifyDeepSeekNetworkError(context.Background(), test.err)
			if code != test.code || diagnostic == "" {
				t.Fatalf("unexpected network classification: code=%s diagnostic=%q", code, diagnostic)
			}
		})
	}
}

func TestDeepSeekOperatorDiagnosticIsUsefulBoundedAndRedacted(t *testing.T) {
	diagnostic := safeDeepSeekDiagnostic("Post https://api.deepseek.com/chat/completions: tls handshake failed; Authorization: Bearer bearer-secret DEEPSEEK_API_KEY=live-secret-token\nretry later", "live-secret-token")
	if !strings.Contains(diagnostic, "tls handshake failed") || !strings.Contains(diagnostic, "retry later") {
		t.Fatalf("technical diagnostic was lost: %q", diagnostic)
	}
	if strings.Contains(diagnostic, "live-secret-token") || strings.Contains(diagnostic, "bearer-secret") || strings.Contains(diagnostic, "\n") {
		t.Fatalf("diagnostic leaked a credential or line break: %q", diagnostic)
	}
	if len(diagnostic) > 512 {
		t.Fatalf("diagnostic was not bounded: len=%d", len(diagnostic))
	}
	if got := safeDeepSeekDiagnostic(strings.Repeat("x", 600), ""); len(got) != 512 {
		t.Fatalf("unexpected diagnostic limit: len=%d", len(got))
	}
}

func TestDeepSeekOversizedResponseFailsClosed(t *testing.T) {
	generator := deepSeekTestGenerator(t, http.StatusOK, strings.Repeat("x", maxDeepSeekResponse+1), nil)
	intent, err := generator.Generate(context.Background(), DraftRequest{Description: "Create a small environment"})
	assertDeepSeekError(t, err, "DeepSeekResponseTooLarge")
	if intent.Name != "" {
		t.Fatalf("oversized response returned an intent: %+v", intent)
	}
}

func TestDeepSeekConfigurationIsRestrictedAndMissingKeyFailsClosed(t *testing.T) {
	for _, endpoint := range []string{
		"http://api.deepseek.com",
		"https://example.com",
		"https://api.deepseek.com/v1",
		"https://user:secret@api.deepseek.com",
		"https://api.deepseek.com?key=value",
		"https://api.deepseek.com:8443",
	} {
		if _, err := validateDeepSeekBaseURL(endpoint); err == nil {
			t.Errorf("unsafe DeepSeek endpoint was accepted: %s", endpoint)
		}
	}
	if endpoint, err := validateDeepSeekBaseURL("https://api.deepseek.com/"); err != nil || endpoint != defaultDeepSeekURL {
		t.Fatalf("official DeepSeek endpoint rejected: %q, %v", endpoint, err)
	}
	if _, err := newDeepSeekDraftGenerator(defaultDeepSeekURL, "retired-model", "key", nil); err == nil {
		t.Fatal("unsupported model identifier was accepted")
	}
	t.Setenv("AI_PROVIDER", "deepseek")
	t.Setenv("DEEPSEEK_BASE_URL", "")
	t.Setenv("DEEPSEEK_MODEL", "")
	t.Setenv("DEEPSEEK_API_KEY", "")
	provider, generator, err := NewDraftGeneratorFromEnvironment()
	if err != nil || provider != deepSeekProvider {
		t.Fatalf("DeepSeek environment configuration failed: provider=%s err=%v", provider, err)
	}
	_, err = generator.Generate(context.Background(), DraftRequest{Description: "Create a small environment"})
	assertDeepSeekError(t, err, "DeepSeekKeyMissing")
}

func TestDeepSeekDraftIsAdvisoryAndDoesNotCreateResources(t *testing.T) {
	body := deepSeekCompletionBody(t, validDeepSeekIntent, "stop", "assistant", 1, nil)
	generator := deepSeekTestGenerator(t, http.StatusOK, body, nil)
	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	if err := batchv1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	if err := platformv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	kubeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(readyDraftClass(), &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default"}}).Build()
	server, err := NewServer(kubeClient, nil, deepSeekProvider, generator, nil)
	if err != nil {
		t.Fatal(err)
	}
	requestBody, err := json.Marshal(DraftRequest{Description: "Create a small development environment with one node and no cache", Namespace: "default", ClassRef: "dev-small", Region: "us-east-1", NodeCount: 1})
	if err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/drafts", bytes.NewReader(requestBody)))
	if response.Code != http.StatusOK {
		t.Fatalf("draft request failed: %d %s", response.Code, response.Body.String())
	}
	var draft DraftResponse
	if err := json.Unmarshal(response.Body.Bytes(), &draft); err != nil {
		t.Fatal(err)
	}
	if draft.Provider != deepSeekProvider || !draft.Valid || draft.Intent.Namespace != "default" || draft.Intent.ClassRef != "dev-small" {
		t.Fatalf("unexpected advisory draft: %+v", draft)
	}
	for _, list := range []client.ObjectList{
		&platformv1alpha1.PlatformEnvironmentList{},
		&platformv1alpha1.InfraStackList{},
		&platformv1alpha1.TerraformRunList{},
		&platformv1alpha1.ChangeApprovalList{},
		&batchv1.JobList{},
	} {
		if err := kubeClient.List(context.Background(), list); err != nil {
			t.Fatal(err)
		}
		switch values := list.(type) {
		case *platformv1alpha1.PlatformEnvironmentList:
			if len(values.Items) != 0 {
				t.Fatal("Draft generated a PlatformEnvironment")
			}
		case *platformv1alpha1.InfraStackList:
			if len(values.Items) != 0 {
				t.Fatal("Draft generated an InfraStack")
			}
		case *platformv1alpha1.TerraformRunList:
			if len(values.Items) != 0 {
				t.Fatal("Draft generated a TerraformRun")
			}
		case *platformv1alpha1.ChangeApprovalList:
			if len(values.Items) != 0 {
				t.Fatal("Draft generated an approval")
			}
		case *batchv1.JobList:
			if len(values.Items) != 0 {
				t.Fatal("Draft generated an execution Job")
			}
		}
	}
}

func TestDeepSeekAPIErrorAndHealthNeverExposeCredentialOrUpstreamBody(t *testing.T) {
	const apiKey = "test-key-never-log"
	const upstreamDetail = "private upstream diagnostic must stay in provider"
	generator, err := newDeepSeekDraftGenerator(defaultDeepSeekURL, defaultDeepSeekModel, apiKey, &http.Client{Transport: deepSeekRoundTripper(func(request *http.Request) (*http.Response, error) {
		return deepSeekHTTPResponse(request, http.StatusUnauthorized, `{"error":"`+upstreamDetail+`"}`, nil), nil
	})})
	if err != nil {
		t.Fatal(err)
	}
	_, kubeClient, err := testServer(t, readyDraftClass(), &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default"}})
	if err != nil {
		t.Fatal(err)
	}
	var logs bytes.Buffer
	server, err := NewServer(kubeClient, nil, deepSeekProvider, generator, slog.New(slog.NewTextHandler(&logs, nil)))
	if err != nil {
		t.Fatal(err)
	}
	requestBody, err := json.Marshal(DraftRequest{Description: "Create a small environment", Namespace: "default", ClassRef: "dev-small", Region: "us-east-1", NodeCount: 1})
	if err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/drafts", bytes.NewReader(requestBody)))
	if response.Code != http.StatusBadGateway {
		t.Fatalf("provider failure status=%d body=%s", response.Code, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), "DeepSeekInvalidAPIKey") || strings.Contains(response.Body.String(), apiKey) || strings.Contains(response.Body.String(), upstreamDetail) {
		t.Fatalf("unsafe or unstable error response: %s", response.Body.String())
	}
	if strings.Contains(logs.String(), apiKey) || strings.Contains(logs.String(), upstreamDetail) || !strings.Contains(logs.String(), "DeepSeekInvalidAPIKey") {
		t.Fatalf("unsafe or missing operator diagnostic: %s", logs.String())
	}
	health := httptest.NewRecorder()
	server.Handler().ServeHTTP(health, httptest.NewRequest(http.MethodGet, "/api/health", nil))
	if strings.Contains(health.Body.String(), apiKey) || strings.Contains(health.Body.String(), "deepseek_api_key") {
		t.Fatalf("health endpoint exposed credential configuration: %s", health.Body.String())
	}
}
