package controller

import (
	"context"
	"testing"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	clientfake "sigs.k8s.io/controller-runtime/pkg/client/fake"

	platformv1alpha1 "github.com/miku-wwl/kube-platform-control-plane/api/v1alpha1"
)

func TestResourceSetWithoutTargetClientFailsClosed(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := platformv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme() error = %v", err)
	}
	object := &platformv1alpha1.ResourceSet{
		ObjectMeta: metav1.ObjectMeta{Name: "runtime", Namespace: "platform-system"},
		Spec:       platformv1alpha1.ResourceSetSpec{Target: platformv1alpha1.TargetReference{Provider: "kind", ClusterName: "target"}},
	}
	client := clientfake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(object).WithObjects(object).Build()
	reconciler := &ResourceSetReconciler{Client: client, Scheme: scheme}
	request := ctrl.Request{NamespacedName: types.NamespacedName{Name: object.Name, Namespace: object.Namespace}}
	if _, err := reconciler.Reconcile(context.Background(), request); err != nil {
		t.Fatalf("first Reconcile() error = %v", err)
	}
	if _, err := reconciler.Reconcile(context.Background(), request); err != nil {
		t.Fatalf("second Reconcile() error = %v", err)
	}
	var observed platformv1alpha1.ResourceSet
	if err := client.Get(context.Background(), request.NamespacedName, &observed); err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if !containsString(observed.Finalizers, resourceSetFinalizer) {
		t.Fatal("ResourceSet finalizer was not installed")
	}
	condition := findCondition(observed.Status.Conditions, ConditionReady)
	if condition == nil || condition.Reason != "RuntimeClientUnavailable" || condition.Status != metav1.ConditionUnknown {
		t.Fatalf("runtime unavailable condition = %+v", condition)
	}
}

func TestBootstrapObjectsRejectsInlineSecretData(t *testing.T) {
	_, err := bootstrapObjects([]platformv1alpha1.RuntimeObject{{
		Version:  "v1",
		Resource: "secrets",
		Object:   apiextensionsv1.JSON{Raw: []byte(`{"apiVersion":"v1","kind":"Secret","metadata":{"name":"inline"},"data":{"token":"c2VjcmV0"}}`)},
	}})
	if err == nil {
		t.Fatal("inline Secret data must be rejected")
	}
}
