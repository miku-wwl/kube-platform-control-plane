package target

import (
	"context"
	"testing"
)

func TestKubeconfigDiscoveryRejectsChangedTarget(t *testing.T) {
	for _, provider := range []string{ProviderKubernetes, ProviderKind} {
		value, err := MaterializeTarget(TargetExpectation{Provider: provider, ClusterName: "supplied-target"})
		if err != nil {
			t.Fatal(err)
		}
		verify := VerifyKubeconfigTarget(value.RuntimeTargetIdentity, value.TargetConnectionProfile)
		if err := verify(context.Background(), value.RuntimeTargetIdentity, value.TargetConnectionProfile); err != nil {
			t.Fatal(err)
		}
		changedIdentity := value.RuntimeTargetIdentity
		changedIdentity.IncarnationID = "replacement-cluster"
		if err := verify(context.Background(), changedIdentity, value.TargetConnectionProfile); err == nil {
			t.Fatal("discovery accepted another cluster incarnation", provider)
		}
		changedProfile := value.TargetConnectionProfile
		changedProfile.Endpoint = "kubeconfig:other-target"
		if err := verify(context.Background(), value.RuntimeTargetIdentity, changedProfile); err == nil {
			t.Fatal("discovery accepted another endpoint", provider)
		}
	}
}
