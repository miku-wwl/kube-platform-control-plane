package e2e

import (
	"context"
	"crypto/sha256"
	"fmt"
	"strings"
	"testing"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

// Record fingerprints only. Never serialize a rest.Config or credential bytes.
func assertIndependentTargetCredentials(t *testing.T, ctx context.Context, run string, a, b *rest.Config) {
	t.Helper()
	fingerprint := func(data []byte) string { return fmt.Sprintf("sha256:%x", sha256.Sum256(data)) }
	if len(a.CAData) == 0 || len(b.CAData) == 0 || len(a.CertData) == 0 || len(b.CertData) == 0 ||
		fingerprint(a.CAData) == fingerprint(b.CAData) || fingerprint(a.CertData) == fingerprint(b.CertData) {
		t.Fatal("independent target lane requires distinct inline cluster CAs and client certificates")
	}
	results := []map[string]any{}
	for _, pair := range []struct {
		name            string
		target, foreign *rest.Config
	}{{"A credential against B", b, a}, {"B credential against A", a, b}} {
		swapped := rest.CopyConfig(pair.target)
		swapped.CertData, swapped.KeyData = pair.foreign.CertData, pair.foreign.KeyData
		swapped.CertFile, swapped.KeyFile = "", ""
		client, err := kubernetes.NewForConfig(swapped)
		if err != nil {
			t.Fatal(err)
		}
		_, err = client.CoreV1().Namespaces().Get(ctx, "kube-system", metav1.GetOptions{})
		if err == nil {
			t.Fatalf("foreign credential could read target: %s", pair.name)
		}
		message := err.Error()
		if !apierrors.IsUnauthorized(err) && !apierrors.IsForbidden(err) &&
			!strings.Contains(message, "remote error: tls: unknown certificate authority") &&
			!strings.Contains(message, "remote error: tls: bad certificate") {
			t.Fatalf("cross-credential rejection was not authentication evidence: %s: %v", pair.name, err)
		}
		results = append(results, map[string]any{"attempt": pair.name, "rejected": true, "error": message})
	}
	recordGateEvidence(t, run, "target-credential-isolation", map[string]any{
		"clusterACAFingerprint": fingerprint(a.CAData), "clusterBCAFingerprint": fingerprint(b.CAData),
		"clientACertificateFingerprint": fingerprint(a.CertData), "clientBCertificateFingerprint": fingerprint(b.CertData),
		"crossCredentialReads": results,
	})
}
