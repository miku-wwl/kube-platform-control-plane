package target

import (
	"testing"

	"k8s.io/apimachinery/pkg/runtime"
	dynamicfake "k8s.io/client-go/dynamic/fake"
)

func TestTargetClientFactoryRejectsWrongTargetProfile(t *testing.T) {
	factory := NewTargetClientFactory()
	client := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme())
	identity := RuntimeTargetIdentity{Provider: "kind", AccountID: "local", Region: "local", ClusterName: "kind-a", IncarnationID: "kind-a"}
	profile := TargetConnectionProfile{Endpoint: "kubeconfig:kind-a", AuthMode: "kind-context", KubeContext: "kind-a"}
	if err := factory.Register(identity, profile, client); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	wrong := profile
	wrong.KubeContext = "kind-b"
	if _, err := factory.Client(nil, identity, wrong); err == nil {
		t.Fatal("wrong target profile was accepted")
	}
}

func TestDiscoveryUsesFrozenEvidenceAndAllowlistedOutput(t *testing.T) {
	input := DiscoveryInput{
		SourceClosureDigest:      "sha256:source",
		BackendSnapshotDigest:    "sha256:backend",
		EffectivePlanInputDigest: "sha256:input",
		ExpectedTarget:           RuntimeTargetIdentity{Provider: "kind", AccountID: "local", Region: "local", ClusterName: "kind-a", IncarnationID: "kind-a"},
		ExpectedConnection:       TargetConnectionProfile{Endpoint: "kubeconfig:kind-a", AuthMode: "kind-context", KubeContext: "kind-a"},
	}
	result, err := DiscoverFromTerraformOutput([]byte(`{"provider":"kind","accountId":"local","region":"local","clusterName":"kind-a","incarnationId":"kind-a","endpoint":"kubeconfig:kind-a","authMode":"kind-context","kubeContext":"kind-a","secret":"must-be-ignored"}`), input, VerifyLocalKindTarget("kind-a"))
	if err != nil {
		t.Fatalf("DiscoverFromTerraformOutput() error = %v", err)
	}
	if result.SourceClosureDigest != input.SourceClosureDigest || result.RuntimeTargetIdentity.ClusterName != "kind-a" {
		t.Fatalf("discovery result = %+v", result)
	}
}
