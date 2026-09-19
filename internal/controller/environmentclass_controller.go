package controller

import (
	"context"
	"reflect"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	platformv1alpha1 "github.com/miku-wwl/kube-platform-control-plane/api/v1alpha1"
)

const environmentClassFinalizer = "platform.example.io/environmentclass-finalizer"

// +kubebuilder:rbac:groups=platform.example.io,resources=environmentclasses,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=platform.example.io,resources=environmentclasses/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=platform.example.io,resources=environmentclasses/finalizers,verbs=update
// +kubebuilder:rbac:groups=platform.example.io,resources=platformenvironments,verbs=get;list;watch
type EnvironmentClassReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

func (r *EnvironmentClassReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var class platformv1alpha1.EnvironmentClass
	if err := r.Get(ctx, req.NamespacedName, &class); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}
	var environments platformv1alpha1.PlatformEnvironmentList
	if err := r.List(ctx, &environments); err != nil {
		return ctrl.Result{}, err
	}
	usage := int32(0)
	for index := range environments.Items {
		if environments.Items[index].Spec.ClassRef != nil && environments.Items[index].Spec.ClassRef.Name == class.Name {
			usage++
		}
	}
	if !class.DeletionTimestamp.IsZero() {
		if usage > 0 {
			return r.updateClassStatus(ctx, &class, usage, metav1.ConditionFalse, "ClassInUse", "EnvironmentClass is in use and cannot be destructively deleted")
		}
		if containsString(class.Finalizers, environmentClassFinalizer) {
			class.Finalizers = removeString(class.Finalizers, environmentClassFinalizer)
			if err := r.Update(ctx, &class); err != nil && !apierrors.IsNotFound(err) {
				return ctrl.Result{}, err
			}
		}
		return ctrl.Result{}, nil
	}
	specDigest := environmentClassSpecDigest(&class)
	if class.Status.SpecDigest != "" && class.Status.SpecDigest != specDigest {
		return r.updateClassStatus(ctx, &class, usage, metav1.ConditionFalse, "ClassSpecChanged", "EnvironmentClass spec changed after admission; create a new versioned class")
	}
	if !containsString(class.Finalizers, environmentClassFinalizer) {
		class.Finalizers = append(class.Finalizers, environmentClassFinalizer)
		if err := r.Update(ctx, &class); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{Requeue: true}, nil
	}
	return r.updateClassStatus(ctx, &class, usage, metav1.ConditionTrue, "ClassValidated", "EnvironmentClass is immutable and available for typed PlatformEnvironment use")
}

func (r *EnvironmentClassReconciler) updateClassStatus(ctx context.Context, class *platformv1alpha1.EnvironmentClass, usage int32, status metav1.ConditionStatus, reason, message string) (ctrl.Result, error) {
	updated := class.Status
	updated.ObservedGeneration = class.Generation
	updated.UsageCount = usage
	if updated.SpecDigest == "" {
		updated.SpecDigest = environmentClassSpecDigest(class)
	}
	updated.Conditions = []metav1.Condition{stableCondition(findCondition(class.Status.Conditions, ConditionReady), class.Generation, status, reason, message)}
	if reflect.DeepEqual(class.Status, updated) {
		return ctrl.Result{}, nil
	}
	class.Status = updated
	return ctrl.Result{}, r.Status().Update(ctx, class)
}

func (r *EnvironmentClassReconciler) SetupWithManager(manager ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(manager).For(&platformv1alpha1.EnvironmentClass{}).Complete(r)
}
