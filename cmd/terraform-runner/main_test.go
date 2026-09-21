package main

import (
	"encoding/json"
	"testing"
)

func TestSanitizeTargetDiscoveryAllowlist(t *testing.T) {
	content, err := sanitizeTargetDiscovery([]byte(`{"target_discovery":{"value":{"provider":"kind","accountId":"local","region":"local","clusterName":"kind-target","incarnationId":"kind-target","endpoint":"kubeconfig:kind-target","authMode":"kind-context","kubeContext":"kind-target","caCertificateData":"must-be-preserved-for-aws","sourceClosureDigest":"sha256:source","backendSnapshotDigest":"sha256:backend","effectivePlanInputDigest":"sha256:input","secret":"must-not-survive"}}}`))
	if err != nil {
		t.Fatalf("sanitizeTargetDiscovery() error = %v", err)
	}
	var values map[string]json.RawMessage
	if err := json.Unmarshal(content, &values); err != nil {
		t.Fatalf("decode sanitized output: %v", err)
	}
	if _, ok := values["secret"]; ok {
		t.Fatal("sanitized discovery retained a non-allowlisted field")
	}
	if string(values["provider"]) != `"kind"` {
		t.Fatalf("provider = %s, want kind", values["provider"])
	}
	if string(values["caCertificateData"]) != `"must-be-preserved-for-aws"` {
		t.Fatalf("caCertificateData = %s, want preserved value", values["caCertificateData"])
	}
}

func TestSanitizeTargetDiscoveryRejectsEmptyOutput(t *testing.T) {
	if _, err := sanitizeTargetDiscovery([]byte(`{"bucket_name":{"value":"not-a-target"}}`)); err == nil {
		t.Fatal("sanitizeTargetDiscovery accepted output without target fields")
	}
}
