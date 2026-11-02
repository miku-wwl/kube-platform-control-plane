package e2e

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

// A real controller binary must reject an invalid target registry before its
// manager starts. No fixture Kubernetes resources or infrastructure are created.
func TestTargetStartupRejectsInvalidBinding(t *testing.T) {
	file := os.Getenv("KPCP_E2E_CONFIG")
	if file == "" {
		t.Skip("requires explicit real integration configuration")
	}
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	var cfg configuration
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatal(err)
	}
	rules := clientcmd.NewDefaultClientConfigLoadingRules()
	rules.ExplicitPath = cfg.Kubeconfig
	rest, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(rules, &clientcmd.ConfigOverrides{CurrentContext: cfg.Context}).ClientConfig()
	if err != nil {
		t.Fatal(err)
	}
	rest.Timeout = 10 * time.Second
	kube, err := kubernetes.NewForConfig(rest)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	namespace, err := kube.CoreV1().Namespaces().Get(ctx, "kube-system", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	before, err := kube.BatchV1().Jobs("").List(ctx, metav1.ListOptions{})
	if err != nil {
		t.Fatal(err)
	}
	rows := []map[string]any{}
	for _, tc := range []struct{ name, want string }{
		{"incarnation", "configured target incarnation does not match kube-system UID"},
		{"unknown-context", "does not exist"},
		{"endpoint", "matching context endpoint are required"},
		{"unknown-field", "unknown field"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			identity := map[string]string{"provider": "kubernetes", "accountId": "local", "region": "local", "clusterName": cfg.Context, "incarnationId": string(namespace.UID)}
			connection := map[string]string{"endpoint": "kubeconfig:" + cfg.Context, "authMode": "kubeconfig-context", "kubeContext": cfg.Context, "networkRouteProfile": "kubeconfig"}
			entry := map[string]any{"identity": identity, "connection": connection}
			switch tc.name {
			case "incarnation":
				identity["incarnationId"] = "not-the-live-cluster-uid"
			case "unknown-context":
				identity["clusterName"] = "qa-unregistered-context"
				connection["kubeContext"] = "qa-unregistered-context"
				connection["endpoint"] = "kubeconfig:qa-unregistered-context"
			case "endpoint":
				connection["endpoint"] = "kubeconfig:wrong-profile"
			case "unknown-field":
				entry["allowUnverifiedTarget"] = true
			}
			profile, err := json.Marshal([]any{entry})
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(t.TempDir(), "targets.json")
			if err := os.WriteFile(path, profile, 0600); err != nil {
				t.Fatal(err)
			}
			runCtx, stop := context.WithTimeout(ctx, 25*time.Second)
			defer stop()
			command := exec.CommandContext(runCtx, cfg.ControllerBinary, "--leader-elect=true", "--health-probe-bind-address=0", "--metrics-bind-address=0")
			command.Env = append(os.Environ(), "KUBECONFIG="+cfg.Kubeconfig, "PCP_KUBECONFIG="+cfg.Kubeconfig, "PCP_TARGETS_FILE="+path, "PCP_TARGET_CONTEXTS=", "PCP_TARGET_CONTEXT=", "PCP_ENABLE_AWS_EKS=false", "PCP_ENABLE_TERRAFORM_EXECUTION=true")
			output, err := command.CombinedOutput()
			if err == nil || runCtx.Err() != nil || !strings.Contains(string(output), tc.want) || strings.Contains(string(output), "starting manager") {
				t.Fatalf("startup did not fail closed as expected: %v %s", err, output)
			}
			rows = append(rows, map[string]any{"case": tc.name, "expectedError": tc.want, "actualOutput": string(output), "exitCode": command.ProcessState.ExitCode(), "managerStarted": false})
		})
	}
	after, err := kube.BatchV1().Jobs("").List(ctx, metav1.ListOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(before.Items) != len(after.Items) {
		t.Fatal("Jobs changed during rejected startup")
	}
	seen := map[string]bool{}
	for _, j := range before.Items {
		seen[string(j.UID)] = true
	}
	for _, j := range after.Items {
		if !seen[string(j.UID)] {
			t.Fatal("new Job appeared during rejected startup")
		}
	}
	recordGateEvidence(t, "target-startup", "rejections", map[string]any{"cases": rows, "clusterUID": namespace.UID, "jobUIDsUnchanged": true})
}
