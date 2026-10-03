package console

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	platform "github.com/miku-wwl/kube-platform-control-plane/api/v1alpha1"
)

func validClassRequest() CreateClassRequest {
	return CreateClassRequest{Name: "local-dev-v1", Spec: platform.EnvironmentClassSpec{
		Version: "v1", Source: platform.SourceSpec{URL: "git://local-source/templates.git", Revision: strings.Repeat("a", 40), Path: "."},
		Backend:       platform.BackendSpec{Type: "s3", ConfigRef: corev1.LocalObjectReference{Name: "local-backend"}, AuthRef: platform.ServiceAccountReference{ServiceAccountName: "runner"}, LockTimeout: metav1.Duration{Duration: 2 * time.Minute}},
		Executor:      platform.ExecutorSpec{TerraformVersion: "1.14.0", Image: "runner:local", WorkDir: "/workspace/terraform", ExecutionTimeout: metav1.Duration{Duration: 12 * time.Minute}},
		RunnerProfile: platform.RunnerProfileSpec{ServiceAccountName: "runner", Image: "runner:local", ImageDigest: "local-image", TerraformVersion: "1.14.0"},
		Target:        platform.TargetReference{Provider: "kind", Account: "local", Region: "local", ClusterName: "kind-pcp-dev"}, AllowedRegions: []string{"local"},
		CapacityBounds: platform.CapacityBounds{MinNodeCount: 1, MaxNodeCount: 5}, ApprovalPolicy: platform.ApprovalPolicyManual,
	}}
}

func classHTTP(t *testing.T, server *Server, method, url string, body any) *httptest.ResponseRecorder {
	t.Helper()
	encoded, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, httptest.NewRequest(method, url, bytes.NewReader(encoded)))
	return response
}

func TestClassValidationCreateAndCopy(t *testing.T) {
	server, kube, err := testServer(t)
	if err != nil {
		t.Fatal(err)
	}
	request := validClassRequest()
	check := classHTTP(t, server, "POST", "/api/classes/validate", request)
	if check.Code != http.StatusOK || !strings.Contains(check.Body.String(), `"valid":true`) {
		t.Fatalf("validate: %d %s", check.Code, check.Body.String())
	}
	var classes platform.EnvironmentClassList
	if err := kube.List(context.Background(), &classes); err != nil || len(classes.Items) != 0 {
		t.Fatalf("dry-run persisted a class: %+v %v", classes.Items, err)
	}
	created := classHTTP(t, server, "POST", "/api/classes", request)
	if created.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", created.Code, created.Body.String())
	}
	var detail ClassDetail
	if err := json.Unmarshal(created.Body.Bytes(), &detail); err != nil {
		t.Fatal(err)
	}
	if detail.Ready || len(detail.Conditions) != 0 || !reflect.DeepEqual(detail.Spec, request.Spec) {
		t.Fatalf("creation forged readiness or changed spec: %+v", detail)
	}
	duplicate := classHTTP(t, server, "POST", "/api/classes", request)
	if duplicate.Code != http.StatusConflict {
		t.Fatalf("duplicate did not fail closed: %d", duplicate.Code)
	}
	copyRequest := request
	copyRequest.Name = "local-dev-v2"
	copyRequest.Spec.Version = "v2"
	if response := classHTTP(t, server, "POST", "/api/classes", copyRequest); response.Code != http.StatusCreated {
		t.Fatalf("copy: %s", response.Body.String())
	}
	var original platform.EnvironmentClass
	if err := kube.Get(context.Background(), client.ObjectKey{Name: request.Name}, &original); err != nil || original.Spec.Version != "v1" {
		t.Fatalf("copy changed original: %v %+v", err, original.Spec)
	}
	var environments platform.PlatformEnvironmentList
	_ = kube.List(context.Background(), &environments)
	var runs platform.TerraformRunList
	_ = kube.List(context.Background(), &runs)
	if len(environments.Items) != 0 || len(runs.Items) != 0 {
		t.Fatal("class management created execution resources")
	}
	if response := classHTTP(t, server, "PUT", "/api/classes/"+request.Name, request); response.Code != http.StatusMethodNotAllowed {
		t.Fatal("immutable class has an update endpoint")
	}
}

func TestClassCreationRejectsInvalidPoliciesAndForgedStatus(t *testing.T) {
	cases := []struct {
		name   string
		change func(*CreateClassRequest)
	}{
		{"unpinned source", func(r *CreateClassRequest) { r.Spec.Source.Revision = "main" }},
		{"source traversal", func(r *CreateClassRequest) { r.Spec.Source.Path = "../outside" }},
		{"inverted capacity", func(r *CreateClassRequest) { r.Spec.CapacityBounds.MinNodeCount = 9 }},
		{"default outside allowed regions", func(r *CreateClassRequest) { r.Spec.AllowedRegions = []string{"another"} }},
		{"mismatched identity", func(r *CreateClassRequest) { r.Spec.Backend.AuthRef.ServiceAccountName = "different" }},
		{"missing runner", func(r *CreateClassRequest) { r.Spec.RunnerProfile.Image = "" }},
		{"unsupported target", func(r *CreateClassRequest) { r.Spec.Target.Provider = "unknown" }},
		{"inline password", func(r *CreateClassRequest) { r.Spec.Source.URL = "https://user:private-password@example.test/repo" }},
		{"inline secret resource", func(r *CreateClassRequest) {
			r.Spec.RuntimeProfile.RuntimeObjects = []platform.RuntimeObject{{Version: "v1", Resource: "secrets", Object: apiextensionsv1.JSON{Raw: []byte(`{"apiVersion":"v1","kind":"Secret","data":{"password":"private"}}`)}}}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server, kube, _ := testServer(t)
			request := validClassRequest()
			tc.change(&request)
			response := classHTTP(t, server, "POST", "/api/classes", request)
			if response.Code != http.StatusUnprocessableEntity {
				t.Fatalf("accepted invalid class: %d %s", response.Code, response.Body.String())
			}
			var classes platform.EnvironmentClassList
			_ = kube.List(context.Background(), &classes)
			if len(classes.Items) != 0 {
				t.Fatal("invalid request persisted")
			}
		})
	}
	server, _, _ := testServer(t)
	request := validClassRequest()
	response := classHTTP(t, server, "POST", "/api/classes", map[string]any{"name": request.Name, "spec": request.Spec, "status": map[string]bool{"ready": true}})
	if response.Code != http.StatusBadRequest {
		t.Fatal("client could submit status")
	}
}

func TestClassDeletionChecksFreshUsageAndUID(t *testing.T) {
	class := classObject(validClassRequest())
	class.UID = types.UID("class-uid")
	// Status intentionally reports zero: API must count current references.
	environment := &platform.PlatformEnvironment{ObjectMeta: metav1.ObjectMeta{Name: "in-use", Namespace: "default"}, Spec: platform.PlatformEnvironmentSpec{ClassRef: &corev1.LocalObjectReference{Name: class.Name}}}
	server, kube, _ := testServer(t, class, environment)
	confirmation := map[string]string{"uid": "class-uid", "confirmName": class.Name}
	if response := classHTTP(t, server, "DELETE", "/api/classes/"+class.Name, confirmation); response.Code != http.StatusConflict {
		t.Fatalf("in-use delete: %d %s", response.Code, response.Body.String())
	}
	if response := classHTTP(t, server, "GET", "/api/classes", nil); !strings.Contains(response.Body.String(), `"usageCount":1`) {
		t.Fatal("catalog used stale usage")
	}
	_ = kube.Delete(context.Background(), environment)
	confirmation["uid"] = "wrong"
	if response := classHTTP(t, server, "DELETE", "/api/classes/"+class.Name, confirmation); response.Code != http.StatusConflict {
		t.Fatal("stale identity allowed deletion")
	}
	confirmation["uid"] = "class-uid"
	confirmation["confirmName"] = "wrong"
	if response := classHTTP(t, server, "DELETE", "/api/classes/"+class.Name, confirmation); response.Code != http.StatusBadRequest {
		t.Fatal("wrong confirmation allowed deletion")
	}
	confirmation["confirmName"] = class.Name
	if response := classHTTP(t, server, "DELETE", "/api/classes/"+class.Name, confirmation); response.Code != http.StatusAccepted {
		t.Fatalf("unused delete: %s", response.Body.String())
	}
	if err := kube.Get(context.Background(), client.ObjectKey{Name: class.Name}, &platform.EnvironmentClass{}); !apierrors.IsNotFound(err) {
		t.Fatalf("unused class retained: %v", err)
	}
}

func TestClassReadinessAndSensitiveDetail(t *testing.T) {
	class := readyDraftClass()
	class.Status.ObservedGeneration = 0
	if classView(class).Ready {
		t.Fatal("stale Ready advertised as selectable")
	}
	class.Status.ObservedGeneration = class.Generation
	now := metav1.Now()
	class.DeletionTimestamp = &now
	class.Finalizers = []string{"guard"}
	if classView(class).Ready {
		t.Fatal("deleting class advertised as selectable")
	}
	server, _, _ := testServer(t, class)
	if _, _, err := server.validateEnvironmentRequest(context.Background(), CreateEnvironmentRequest{Name: "dev", Namespace: "default", ClassRef: class.Name, NodeCount: 1}); err == nil || !strings.Contains(err.Error(), "being deleted") {
		t.Fatalf("deleting class accepted: %v", err)
	}
	class = classObject(validClassRequest())
	class.Spec.Source.URL = "https://user:private-password@example.test/repo"
	class.Spec.RuntimeProfile.RuntimeObjects = []platform.RuntimeObject{{Version: "v1", Resource: "secrets", Object: apiextensionsv1.JSON{Raw: []byte(`{"kind":"Secret","stringData":{"token":"private-token"}}`)}}}
	detail := classDetail(class, 0)
	encoded, _ := json.Marshal(detail)
	if !detail.Redacted || strings.Contains(string(encoded), "private-password") || strings.Contains(string(encoded), "private-token") {
		t.Fatal("class detail leaked inline credentials")
	}
	if !strings.Contains(class.Spec.Source.URL, "private-password") {
		t.Fatal("read projection mutated stored source")
	}
}

func TestClassCredentialReferencesAllowedAndInlineValuesRejected(t *testing.T) {
	request := validClassRequest()
	request.Spec.RuntimeProfile.RuntimeObjects = []platform.RuntimeObject{{Version: "v1", Resource: "pods", Object: apiextensionsv1.JSON{Raw: []byte(`{"kind":"Pod","spec":{"automountServiceAccountToken":false,"containers":[{"name":"app","env":[{"name":"DB_PASSWORD","valueFrom":{"secretKeyRef":{"name":"db","key":"password"}}}]}]}}`)}}}
	if result := validateClassRequest(request); !result.Valid {
		t.Fatalf("credential reference rejected: %v", result.Errors)
	}
	request.Spec.RuntimeProfile.RuntimeObjects[0].Object.Raw = []byte(`{"kind":"Pod","spec":{"containers":[{"name":"app","env":[{"name":"DB_PASSWORD","value":"private-password"}]}]}}`)
	if result := validateClassRequest(request); result.Valid {
		t.Fatal("inline environment credential accepted")
	}
	detail := classDetail(classObject(request), 0)
	if !detail.Redacted || strings.Contains(detail.YAML, "private-password") {
		t.Fatal("inline environment credential leaked")
	}
	request.Spec.RuntimeProfile.RuntimeObjects = nil
	request.Spec.Source.URL = "https://example.test/repo?access_token=private-token"
	if result := validateClassRequest(request); result.Valid {
		t.Fatal("URL credential accepted")
	}
}
