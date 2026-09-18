package controller

import (
	"context"
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	platformv1alpha1 "github.com/miku-wwl/kube-platform-control-plane/api/v1alpha1"
)

// +kubebuilder:rbac:groups=platform.example.io,resources=platformenvironments,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=platform.example.io,resources=platformenvironments/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=platform.example.io,resources=platformenvironments/finalizers,verbs=update
// +kubebuilder:rbac:groups=platform.example.io,resources=resourcesets,verbs=get;list;watch;create;delete;update;patch
type PlatformEnvironmentReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

func (r *PlatformEnvironmentReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var object platformv1alpha1.PlatformEnvironment
	if err := r.Get(ctx, req.NamespacedName, &object); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	childName := resourceSetName(object.Name)
	var child platformv1alpha1.ResourceSet
	getErr := r.Get(ctx, client.ObjectKey{Name: childName, Namespace: object.Namespace}, &child)
	if apierrors.IsNotFound(getErr) {
		child = *buildResourceSet(&object, childName)
		if err := ctrl.SetControllerReference(&object, &child, r.Scheme); err != nil {
			return ctrl.Result{}, err
		}
		if err := r.Create(ctx, &child); err != nil {
			return ctrl.Result{}, err
		}
	} else if getErr != nil {
		return ctrl.Result{}, getErr
	}

	condition := lifecycleCondition(object.Generation, metav1.ConditionFalse, "ResourceSetPending", "ResourceSet runtime reconciliation is pending.")
	if childCondition := findCondition(child.Status.Conditions, ConditionReady); childCondition != nil {
		condition = *childCondition
	}
	if !sameCondition(findCondition(object.Status.Conditions, ConditionReady), &condition) || object.Status.ObservedGeneration != object.Generation || object.Status.ResourceSetRef == nil || object.Status.ResourceSetRef.Name != child.Name {
		object.Status.ObservedGeneration = object.Generation
		object.Status.ResourceSetRef = &corev1.LocalObjectReference{Name: child.Name}
		object.Status.Conditions = []metav1.Condition{condition}
		if err := r.Status().Update(ctx, &object); err != nil {
			return ctrl.Result{}, err
		}
	}
	return ctrl.Result{}, nil
}

func (r *PlatformEnvironmentReconciler) SetupWithManager(manager ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(manager).For(&platformv1alpha1.PlatformEnvironment{}).Owns(&platformv1alpha1.ResourceSet{}).Complete(r)
}

func buildResourceSet(environment *platformv1alpha1.PlatformEnvironment, name string) *platformv1alpha1.ResourceSet {
	return &platformv1alpha1.ResourceSet{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: environment.Namespace},
		Spec: platformv1alpha1.ResourceSetSpec{
			Target:            environment.Spec.Target,
			MaxInventoryItems: 500,
		},
	}
}

func resourceSetName(environmentName string) string {
	name := fmt.Sprintf("%s-resources", environmentName)
	if len(name) > 63 {
		name = name[:63]
	}
	return strings.TrimRight(name, "-")
}
