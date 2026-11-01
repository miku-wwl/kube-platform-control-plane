package target

import (
	"context"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	"testing"
)

func TestKubeconfigDiscoveryRejectsChangedTarget(t *testing.T) {
	for _, provider := range []string{ProviderKubernetes, ProviderKind} {
		value, err := MaterializeTarget(TargetExpectation{Provider: provider, ClusterName: "supplied-target", IncarnationID: "fixture-cluster-uid"})
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

func TestKubeconfigClientRejectsReplacementAfterRegistration(t *testing.T) {
	ctx := context.Background()
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	kube := dynamicfake.NewSimpleDynamicClient(scheme, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "kube-system", UID: "original"}})
	value, err := MaterializeTarget(TargetExpectation{Provider: ProviderKubernetes, ClusterName: "target", IncarnationID: "original"})
	if err != nil {
		t.Fatal(err)
	}
	factory := NewTargetClientFactory()
	if err := factory.Register(value.RuntimeTargetIdentity, value.TargetConnectionProfile, kube); err != nil {
		t.Fatal(err)
	}
	if _, err := factory.Client(ctx, value.RuntimeTargetIdentity, value.TargetConnectionProfile); err != nil {
		t.Fatal(err)
	}
	namespaces := kube.Resource(schema.GroupVersionResource{Version: "v1", Resource: "namespaces"})
	current, err := namespaces.Get(ctx, "kube-system", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	current.SetUID("replacement")
	if _, err := namespaces.Update(ctx, current, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err := factory.Client(ctx, value.RuntimeTargetIdentity, value.TargetConnectionProfile); err == nil {
		t.Fatal("registered client accepted a replaced cluster")
	}
}
