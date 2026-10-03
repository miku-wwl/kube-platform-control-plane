package controller

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	platform "github.com/miku-wwl/kube-platform-control-plane/api/v1alpha1"
)

func TestClassFinalizerProtectsUsageAndReleasesAfterEnvironmentRemoval(t *testing.T) {
	ctx := context.Background()
	scheme := runtime.NewScheme()
	_ = platform.AddToScheme(scheme)
	class := &platform.EnvironmentClass{ObjectMeta: metav1.ObjectMeta{Name: "dev-class", Generation: 1}}
	environment := &platform.PlatformEnvironment{ObjectMeta: metav1.ObjectMeta{Name: "dev", Namespace: "default"}, Spec: platform.PlatformEnvironmentSpec{ClassRef: &corev1.LocalObjectReference{Name: class.Name}}}
	kube := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(class).WithObjects(class, environment).Build()
	reconciler := &EnvironmentClassReconciler{Client: kube, Scheme: scheme}
	request := ctrl.Request{NamespacedName: types.NamespacedName{Name: class.Name}}
	for i := 0; i < 2; i++ {
		if _, err := reconciler.Reconcile(ctx, request); err != nil {
			t.Fatal(err)
		}
	}
	if err := kube.Get(ctx, client.ObjectKey{Name: class.Name}, class); err != nil {
		t.Fatal(err)
	}
	if class.Status.UsageCount != 1 || !containsString(class.Finalizers, environmentClassFinalizer) {
		t.Fatal("class did not establish deletion guard and usage")
	}
	if err := kube.Delete(ctx, class); err != nil {
		t.Fatal(err)
	}
	if _, err := reconciler.Reconcile(ctx, request); err != nil {
		t.Fatal(err)
	}
	if err := kube.Get(ctx, client.ObjectKey{Name: class.Name}, class); err != nil {
		t.Fatalf("used class was deleted: %v", err)
	}
	condition := meta.FindStatusCondition(class.Status.Conditions, "Ready")
	if condition == nil || condition.Reason != "ClassInUse" {
		t.Fatal("used class did not report deletion protection")
	}
	if err := kube.Delete(ctx, environment); err != nil {
		t.Fatal(err)
	}
	if _, err := reconciler.Reconcile(ctx, request); err != nil {
		t.Fatal(err)
	}
	if err := kube.Get(ctx, client.ObjectKey{Name: class.Name}, class); !apierrors.IsNotFound(err) {
		t.Fatalf("unused class finalizer did not release: %v", err)
	}
}
