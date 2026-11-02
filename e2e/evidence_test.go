package e2e

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

func recordGateEvidence(t *testing.T, run, name string, value any) {
	t.Helper()
	if dir := os.Getenv("KPCP_E2E_EVIDENCE_DIR"); dir != "" {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
		data, err := json.MarshalIndent(map[string]any{"runID": run, "test": t.Name(), "at": time.Now().UTC(), "sourceSHA": os.Getenv("KPCP_SOURCE_SHA"), "evidence": value}, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, run+"-"+name+".json"), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
}

func hasOwnerUID(owners []metav1.OwnerReference, uid types.UID) bool {
	for _, owner := range owners {
		if owner.UID == uid && owner.Controller != nil && *owner.Controller {
			return true
		}
	}
	return false
}
