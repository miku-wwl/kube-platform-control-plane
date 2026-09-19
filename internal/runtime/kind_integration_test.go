package runtime

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/tools/clientcmd"
)

func TestKindTargetSSAInventoryAndReadiness(t *testing.T) {
	contextName := os.Getenv("PCP_TARGET_CONTEXT")
	if contextName == "" {
		t.Skip("set PCP_TARGET_CONTEXT for Kind target integration")
	}
	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	if explicitPath := os.Getenv("PCP_KUBECONFIG"); explicitPath != "" {
		loadingRules.ExplicitPath = explicitPath
	}
	config, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, &clientcmd.ConfigOverrides{CurrentContext: contextName}).ClientConfig()
	if err != nil {
		t.Fatalf("build kubeconfig: %v", err)
	}
	client, err := dynamic.NewForConfig(config)
	if err != nil {
		t.Fatalf("build dynamic client: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	identity := TargetIdentity{Provider: "kind", ClusterName: contextName}

	namespaceName := fmt.Sprintf("pcp-runtime-integration-%d", time.Now().UnixNano())
	namespace := &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "v1",
		"kind":       "Namespace",
		"metadata":   map[string]interface{}{"name": namespaceName},
	}}
	namespace.SetGroupVersionKind(schema.GroupVersionKind{Version: "v1", Kind: "Namespace"})
	if _, err := ApplyBootstrap(ctx, client, []BootstrapObject{{GVR: schema.GroupVersionResource{Version: "v1", Resource: "namespaces"}, Object: namespace, Wave: 0}}, "pcp-rs-integration", identity); err != nil {
		t.Fatalf("apply namespace: %v", err)
	}
	deployment := &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "apps/v1",
		"kind":       "Deployment",
		"metadata": map[string]interface{}{
			"name":      "pcp-runtime",
			"namespace": namespaceName,
			"labels":    map[string]interface{}{managedByLabel: "platform-control-plane"},
		},
		"spec": map[string]interface{}{
			"replicas": int64(1),
			"selector": map[string]interface{}{"matchLabels": map[string]interface{}{"app": "pcp-runtime"}},
			"template": map[string]interface{}{
				"metadata": map[string]interface{}{"labels": map[string]interface{}{"app": "pcp-runtime"}},
				"spec":     map[string]interface{}{"containers": []interface{}{map[string]interface{}{"name": "pause", "image": "busybox:1.36", "command": []interface{}{"sh", "-c", "sleep 3600"}}}},
			},
		},
	}}
	deployment.SetGroupVersionKind(schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "Deployment"})
	resource := schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"}
	if _, err := ApplyBootstrap(ctx, client, []BootstrapObject{{GVR: resource, Object: deployment, Wave: 1}}, "pcp-rs-integration", identity); err != nil {
		t.Fatalf("apply deployment: %v", err)
	}
	defer func() {
		_ = client.Resource(resource).Namespace(deployment.GetNamespace()).Delete(context.Background(), deployment.GetName(), metav1.DeleteOptions{})
		_ = client.Resource(schema.GroupVersionResource{Version: "v1", Resource: "namespaces"}).Delete(context.Background(), namespace.GetName(), metav1.DeleteOptions{})
	}()

	var observed *unstructured.Unstructured
	for ctx.Err() == nil {
		observed, err = client.Resource(resource).Namespace(deployment.GetNamespace()).Get(ctx, deployment.GetName(), metav1.GetOptions{})
		if err == nil {
			ready, _, readinessErr := Ready(observed)
			if readinessErr != nil {
				t.Fatalf("readiness: %v", readinessErr)
			}
			if ready {
				break
			}
		}
		time.Sleep(500 * time.Millisecond)
	}
	if observed == nil || ctx.Err() != nil {
		t.Fatalf("deployment did not become ready: %v", ctx.Err())
	}
	item, err := InventoryFor(observed, identity)
	if err != nil {
		t.Fatalf("inventory: %v", err)
	}
	if err := ValidatePrune(item, identity, observed); err != nil {
		t.Fatalf("prune validation: %v", err)
	}
}

func TestKindValkeyBootstrapReadiness(t *testing.T) {
	contextName := os.Getenv("PCP_TARGET_CONTEXT")
	if contextName == "" {
		t.Skip("set PCP_TARGET_CONTEXT for Kind target integration")
	}
	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	if explicitPath := os.Getenv("PCP_KUBECONFIG"); explicitPath != "" {
		loadingRules.ExplicitPath = explicitPath
	}
	config, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, &clientcmd.ConfigOverrides{CurrentContext: contextName}).ClientConfig()
	if err != nil {
		t.Fatalf("build kubeconfig: %v", err)
	}
	client, err := dynamic.NewForConfig(config)
	if err != nil {
		t.Fatalf("build dynamic client: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	target := TargetIdentity{Provider: "kind", ClusterName: contextName}
	namespaceName := fmt.Sprintf("pcp-valkey-integration-%d", time.Now().UnixNano())
	namespace := &unstructured.Unstructured{Object: map[string]interface{}{"apiVersion": "v1", "kind": "Namespace", "metadata": map[string]interface{}{"name": namespaceName}}}
	namespace.SetGroupVersionKind(schema.GroupVersionKind{Version: "v1", Kind: "Namespace"})
	if _, err := ApplyBootstrap(ctx, client, []BootstrapObject{{GVR: schema.GroupVersionResource{Version: "v1", Resource: "namespaces"}, Object: namespace, Wave: 0}}, "pcp-valkey-integration", target); err != nil {
		t.Fatalf("apply Valkey namespace: %v", err)
	}
	objects, err := BuildValkeyObjects(ValkeySpec{Name: "cache", Namespace: namespaceName, Replicas: 1, Image: "valkey/valkey:8"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ApplyBootstrap(ctx, client, objects, "pcp-valkey-integration", target); err != nil {
		t.Fatalf("apply Valkey objects: %v", err)
	}
	defer func() {
		_ = client.Resource(StatefulSetGVR).Namespace(namespaceName).Delete(context.Background(), "cache", metav1.DeleteOptions{})
		_ = client.Resource(ServiceGVR).Namespace(namespaceName).Delete(context.Background(), "cache", metav1.DeleteOptions{})
		_ = client.Resource(ValkeyGVR).Namespace(namespaceName).Delete(context.Background(), "cache", metav1.DeleteOptions{})
		_ = client.Resource(schema.GroupVersionResource{Version: "v1", Resource: "namespaces"}).Delete(context.Background(), namespaceName, metav1.DeleteOptions{})
	}()

	for ctx.Err() == nil {
		statefulSet, getErr := client.Resource(StatefulSetGVR).Namespace(namespaceName).Get(ctx, "cache", metav1.GetOptions{})
		if getErr == nil {
			ready, _, readinessErr := ValkeyReadyFromStatefulSet(statefulSet)
			if readinessErr != nil {
				t.Fatalf("Valkey readiness: %v", readinessErr)
			}
			if ready {
				return
			}
		}
		time.Sleep(500 * time.Millisecond)
	}
	t.Fatalf("Valkey StatefulSet did not become ready: %v", ctx.Err())
}
