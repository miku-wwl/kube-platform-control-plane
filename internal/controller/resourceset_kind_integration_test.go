package controller

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/tools/clientcmd"
	ctrl "sigs.k8s.io/controller-runtime"
	clientfake "sigs.k8s.io/controller-runtime/pkg/client/fake"

	platformv1alpha1 "github.com/miku-wwl/kube-platform-control-plane/api/v1alpha1"
)

func TestKindResourceSetControllerSSAInventoryAndPrune(t *testing.T) {
	contextName := os.Getenv("PCP_TARGET_CONTEXT")
	if contextName == "" {
		t.Skip("set PCP_TARGET_CONTEXT for target-cluster controller integration")
	}
	config, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		clientcmd.NewDefaultClientConfigLoadingRules(),
		&clientcmd.ConfigOverrides{CurrentContext: contextName},
	).ClientConfig()
	if err != nil {
		t.Fatalf("target kubeconfig: %v", err)
	}
	targetClient, err := dynamic.NewForConfig(config)
	if err != nil {
		t.Fatalf("target dynamic client: %v", err)
	}

	name := fmt.Sprintf("pcp-rs-controller-%d", time.Now().UnixNano())
	configMapGVR := schema.GroupVersionResource{Version: "v1", Resource: "configmaps"}
	defer func() {
		_ = targetClient.Resource(configMapGVR).Namespace("default").Delete(context.Background(), name, metav1.DeleteOptions{})
	}()

	scheme := runtime.NewScheme()
	if err := platformv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme: %v", err)
	}
	resourceSet := &platformv1alpha1.ResourceSet{
		ObjectMeta: metav1.ObjectMeta{Name: "runtime-controller", Namespace: "platform-system"},
		Spec: platformv1alpha1.ResourceSetSpec{
			Target:                 platformv1alpha1.TargetReference{Provider: "kind", ClusterName: contextName, ClusterID: contextName},
			RuntimeMutationAllowed: true,
			Resources: []platformv1alpha1.RuntimeObject{{
				Version:  "v1",
				Resource: "configmaps",
				Object:   apiextensionsv1.JSON{Raw: []byte(fmt.Sprintf(`{"apiVersion":"v1","kind":"ConfigMap","metadata":{"name":"%s","namespace":"default","labels":{"platform.example.io/managed-by":"platform-control-plane"}},"data":{"phase":"controller"}}`, name))},
			}},
		},
	}
	managementClient := clientfake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(resourceSet).WithObjects(resourceSet).Build()
	reconciler := &ResourceSetReconciler{Client: managementClient, Scheme: scheme, TargetClient: targetClient}
	request := ctrl.Request{NamespacedName: types.NamespacedName{Name: resourceSet.Name, Namespace: resourceSet.Namespace}}

	if _, err := reconciler.Reconcile(context.Background(), request); err != nil {
		t.Fatalf("install finalizer: %v", err)
	}
	if _, err := reconciler.Reconcile(context.Background(), request); err != nil {
		t.Fatalf("apply runtime: %v", err)
	}
	if _, err := targetClient.Resource(configMapGVR).Namespace("default").Get(context.Background(), name, metav1.GetOptions{}); err != nil {
		t.Fatalf("target ConfigMap was not applied: %v", err)
	}

	var observed platformv1alpha1.ResourceSet
	if err := managementClient.Get(context.Background(), request.NamespacedName, &observed); err != nil {
		t.Fatalf("read ResourceSet status: %v", err)
	}
	if observed.Status.InventoryItems != 1 || observed.Status.TargetIdentityDigest == "" {
		t.Fatalf("inventory status = %+v", observed.Status)
	}
	if condition := findCondition(observed.Status.Conditions, ConditionReady); condition == nil || condition.Reason != "RuntimeReadinessUnknown" {
		t.Fatalf("readiness condition = %+v", condition)
	}

	observed.Spec.Resources = nil
	if err := managementClient.Update(context.Background(), &observed); err != nil {
		t.Fatalf("remove desired runtime object: %v", err)
	}
	if _, err := reconciler.Reconcile(context.Background(), request); err != nil {
		t.Fatalf("prune removed object: %v", err)
	}
	if _, err := targetClient.Resource(configMapGVR).Namespace("default").Get(context.Background(), name, metav1.GetOptions{}); err == nil {
		t.Fatal("removed runtime object was not pruned")
	}
}
