package target

import (
	"context"
	"fmt"
	"testing"

	"k8s.io/apimachinery/pkg/runtime"
	dynamicfake "k8s.io/client-go/dynamic/fake"
)

func TestMaterializeTargetKeepsAWSNativeIdentitySeparateFromKind(t *testing.T) {
	materialized, err := MaterializeTarget(TargetExpectation{
		Provider:         ProviderAWS,
		AccountID:        "123456789012",
		Region:           "us-east-1",
		ClusterName:      "platform",
		ClusterARN:       "arn:aws:eks:us-east-1:123456789012:cluster/platform",
		ExecutionRoleARN: "arn:aws:iam::123456789012:role/infra",
		RuntimeRoleARN:   "arn:aws:iam::123456789012:role/runtime",
	})
	if err != nil {
		t.Fatalf("MaterializeTarget() error = %v", err)
	}
	if materialized.InfrastructureExecutionIdentity.Mode != ModeAWSNative || materialized.InfrastructureExecutionIdentity.AccountID != "123456789012" {
		t.Fatalf("AWS infrastructure identity = %+v", materialized.InfrastructureExecutionIdentity)
	}
	if materialized.InfrastructureExecutionIdentity.RoleARN == materialized.TargetConnectionProfile.RoleARN || materialized.InfrastructureExecutionIdentity.RoleARN == "" || materialized.TargetConnectionProfile.RoleARN == "" {
		t.Fatalf("execution/runtime role separation = infra=%q runtime=%q", materialized.InfrastructureExecutionIdentity.RoleARN, materialized.TargetConnectionProfile.RoleARN)
	}
	if materialized.TargetConnectionProfile.AuthMode != AuthAWSEKS || materialized.TargetConnectionProfile.NetworkRouteProfile == "local-kind" {
		t.Fatalf("AWS target connection = %+v", materialized.TargetConnectionProfile)
	}

	kindTarget, err := MaterializeTarget(TargetExpectation{Provider: ProviderKind, ClusterName: "kind-target"})
	if err != nil {
		t.Fatalf("MaterializeTarget(kind) error = %v", err)
	}
	if kindTarget.InfrastructureExecutionIdentity.AccountID != "local" || kindTarget.TargetConnectionProfile.AuthMode != AuthKindContext {
		t.Fatalf("Kind target = %+v", kindTarget)
	}
}

func TestMaterializeTargetRejectsAWSLocalFallback(t *testing.T) {
	if _, err := MaterializeTarget(TargetExpectation{Provider: ProviderAWS, ClusterName: "platform"}); err == nil {
		t.Fatal("AWS target without account and region was accepted")
	}
}

func TestMaterializeTargetRejectsExecutionRoleFromWrongAccount(t *testing.T) {
	if _, err := MaterializeTarget(TargetExpectation{
		Provider:         ProviderAWS,
		AccountID:        "123456789012",
		Region:           "us-east-1",
		ClusterName:      "platform",
		ExecutionRoleARN: "arn:aws:iam::999999999999:role/platform/terraform-runner",
	}); err == nil {
		t.Fatal("execution role from the wrong account was accepted")
	}
}

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
	result, err := DiscoverFromTerraformOutput([]byte(`{"provider":"kind","accountId":"local","region":"local","clusterName":"kind-a","incarnationId":"kind-a","endpoint":"kubeconfig:kind-a","authMode":"kind-context","kubeContext":"kind-a","sourceClosureDigest":"sha256:source","backendSnapshotDigest":"sha256:backend","effectivePlanInputDigest":"sha256:input","secret":"must-be-ignored"}`), input, VerifyLocalKindTarget("kind-a"))
	if err != nil {
		t.Fatalf("DiscoverFromTerraformOutput() error = %v", err)
	}
	if result.SourceClosureDigest != input.SourceClosureDigest || result.RuntimeTargetIdentity.ClusterName != "kind-a" {
		t.Fatalf("discovery result = %+v", result)
	}
}

func TestDiscoveryRejectsStaleConvergenceEvidence(t *testing.T) {
	input := DiscoveryInput{SourceClosureDigest: "sha256:current-source", BackendSnapshotDigest: "sha256:backend", EffectivePlanInputDigest: "sha256:input", ExpectedTarget: RuntimeTargetIdentity{Provider: ProviderKind, AccountID: "local", Region: "local", ClusterName: "kind-a", IncarnationID: "kind-a"}, ExpectedConnection: TargetConnectionProfile{Endpoint: "kubeconfig:kind-a", AuthMode: AuthKindContext, KubeContext: "kind-a"}}
	_, err := DiscoverFromTerraformOutput([]byte(`{"provider":"kind","accountId":"local","region":"local","clusterName":"kind-a","incarnationId":"kind-a","endpoint":"kubeconfig:kind-a","authMode":"kind-context","kubeContext":"kind-a","sourceClosureDigest":"sha256:old-source","backendSnapshotDigest":"sha256:backend","effectivePlanInputDigest":"sha256:input"}`), input, VerifyLocalKindTarget("kind-a"))
	if err == nil {
		t.Fatal("stale discovery evidence was accepted")
	}
}

func TestKindDiscoveryKeepsLocalRuntimeRegionWhenTerraformUsesEmulationRegion(t *testing.T) {
	input := DiscoveryInput{SourceClosureDigest: "sha256:source", BackendSnapshotDigest: "sha256:backend", EffectivePlanInputDigest: "sha256:input", ExpectedTarget: RuntimeTargetIdentity{Provider: ProviderKind, AccountID: "local", Region: "local", ClusterName: "kind-a", IncarnationID: "kind-a"}, ExpectedConnection: TargetConnectionProfile{Endpoint: "kubeconfig:kind-a", AuthMode: AuthKindContext, KubeContext: "kind-a"}}
	result, err := DiscoverFromTerraformOutput([]byte(`{"provider":"kind","accountId":"local","region":"us-east-1","clusterName":"kind-a","incarnationId":"kind-a","endpoint":"kubeconfig:kind-a","authMode":"kind-context","kubeContext":"kind-a","sourceClosureDigest":"sha256:source","backendSnapshotDigest":"sha256:backend","effectivePlanInputDigest":"sha256:input"}`), input, VerifyLocalKindTarget("kind-a"))
	if err != nil {
		t.Fatalf("Kind discovery error = %v", err)
	}
	if result.RuntimeTargetIdentity.Region != "local" {
		t.Fatalf("Kind runtime region = %q, want local", result.RuntimeTargetIdentity.Region)
	}
}

func TestDiscoveryRejectsWrongAWSAccountBeforeRuntimeMutation(t *testing.T) {
	input := DiscoveryInput{
		SourceClosureDigest:      "sha256:source",
		BackendSnapshotDigest:    "sha256:backend",
		EffectivePlanInputDigest: "sha256:input",
		ExpectedTarget:           RuntimeTargetIdentity{Provider: ProviderAWS, AccountID: "123456789012", Region: "us-east-1", ClusterName: "platform", IncarnationID: "cluster-incarnation"},
		ExpectedConnection:       TargetConnectionProfile{Endpoint: "https://platform.eks.example", CACertificateData: "Y2E=", AuthMode: AuthAWSEKS},
	}
	verifier := TargetVerifierFunc(func(_ context.Context, identity RuntimeTargetIdentity, profile TargetConnectionProfile) error {
		if identity.AccountID != "123456789012" || profile.AuthMode != AuthAWSEKS {
			return fmt.Errorf("wrong AWS target binding")
		}
		return nil
	})
	_, err := DiscoverFromTerraformOutput([]byte(`{"provider":"aws","accountId":"999999999999","region":"us-east-1","clusterName":"platform","incarnationId":"cluster-incarnation","endpoint":"https://platform.eks.example","caCertificateData":"Y2E=","authMode":"aws-eks"}`), input, verifier)
	if err == nil {
		t.Fatal("wrong AWS account was accepted")
	}
}
