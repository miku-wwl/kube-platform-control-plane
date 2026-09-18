package terraform

import "testing"

func TestDigestIsStableAndChangesWithIdentity(t *testing.T) {
	first, err := Digest(TargetIdentity{Provider: "aws", Mode: "custom-endpoint", AccountID: "000000000000", Region: "us-east-1", EndpointProfileDigest: "sha256:a"})
	if err != nil {
		t.Fatalf("Digest() error = %v", err)
	}
	second, err := Digest(TargetIdentity{Provider: "aws", Mode: "custom-endpoint", AccountID: "000000000000", Region: "us-east-1", EndpointProfileDigest: "sha256:a"})
	if err != nil {
		t.Fatalf("Digest() error = %v", err)
	}
	if first != second || len(first) != len("sha256:")+64 {
		t.Fatalf("stable digest = %q and %q", first, second)
	}
	changed, err := Digest(TargetIdentity{Provider: "aws", Mode: "custom-endpoint", AccountID: "000000000000", Region: "ap-southeast-2", EndpointProfileDigest: "sha256:a"})
	if err != nil {
		t.Fatalf("Digest() error = %v", err)
	}
	if changed == first {
		t.Fatal("region change did not change digest")
	}
}

func TestBackendSnapshotRejectsCredentialsAndRequiresLockfile(t *testing.T) {
	snapshot := BackendSnapshot{Type: "s3", Bucket: "state", Key: "workspace.tfstate", Region: "us-east-1", UseLockfile: true, EndpointSettings: map[string]string{"secret_key": "must-not-persist"}}
	if err := snapshot.Validate(); err == nil {
		t.Fatal("Validate() accepted a credential field")
	}
	snapshot.EndpointSettings = nil
	snapshot.UseLockfile = false
	if err := snapshot.Validate(); err == nil {
		t.Fatal("Validate() accepted use_lockfile=false")
	}
}

func TestEndpointProfileDigestCoversProviderAndBackendRouting(t *testing.T) {
	profile := EndpointProfile{
		GatewayEndpoint: "http://localhost:4566",
		ProviderEndpoints: map[string]string{
			"s3":  "http://localhost:4566",
			"sts": "http://localhost:4566",
		},
		BackendEndpoints: map[string]string{
			"s3": "http://localhost:4566",
		},
		S3AddressingMode: "path",
		AccountID:        "000000000000",
		Region:           "us-east-1",
	}
	first, err := profile.Digest()
	if err != nil {
		t.Fatalf("Digest() error = %v", err)
	}
	profile.BackendEndpoints["s3"] = "http://different-endpoint:4566"
	second, err := profile.Digest()
	if err != nil {
		t.Fatalf("Digest() error = %v", err)
	}
	if first == second {
		t.Fatal("backend routing change did not change endpoint profile digest")
	}
}
