package controller

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"

	platformv1alpha1 "github.com/miku-wwl/kube-platform-control-plane/api/v1alpha1"
)

const (
	platformEnvironmentFinalizer = "platform.example.io/platformenvironment-finalizer"
	environmentRequeue           = time.Second
)

// +kubebuilder:rbac:groups=platform.example.io,resources=platformenvironments,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=platform.example.io,resources=platformenvironments/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=platform.example.io,resources=platformenvironments/finalizers,verbs=update
// +kubebuilder:rbac:groups=platform.example.io,resources=resourcesets,verbs=get;list;watch;create;delete;update;patch
// +kubebuilder:rbac:groups=platform.example.io,resources=infrastacks,verbs=get;list;watch;update;patch;delete
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
	if !object.DeletionTimestamp.IsZero() {
		return r.reconcileDelete(ctx, &object)
	}
	if !containsString(object.Finalizers, platformEnvironmentFinalizer) {
		object.Finalizers = append(object.Finalizers, platformEnvironmentFinalizer)
		if err := r.Update(ctx, &object); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{Requeue: true}, nil
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
		return ctrl.Result{RequeueAfter: environmentRequeue}, nil
	} else if getErr != nil {
		return ctrl.Result{}, getErr
	}

	if !reflect.DeepEqual(child.Spec.Target, object.Spec.Target) {
		child.Spec.Target = object.Spec.Target
		if err := r.Update(ctx, &child); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{RequeueAfter: environmentRequeue}, nil
	}

	var stack platformv1alpha1.InfraStack
	stackFound := object.Spec.InfraStackRef.Name != ""
	if stackFound {
		getErr = r.Get(ctx, client.ObjectKey{Name: object.Spec.InfraStackRef.Name, Namespace: object.Namespace}, &stack)
		if apierrors.IsNotFound(getErr) {
			stackFound = false
		} else if getErr != nil {
			return ctrl.Result{}, getErr
		}
	}

	status := object.Status
	status.ObservedGeneration = object.Generation
	status.InfraStackRef = nil
	if object.Spec.InfraStackRef.Name != "" {
		status.InfraStackRef = &corev1.LocalObjectReference{Name: object.Spec.InfraStackRef.Name}
	}
	status.ResourceSetRef = &corev1.LocalObjectReference{Name: child.Name}

	condition := lifecycleCondition(object.Generation, metav1.ConditionFalse, "InfrastructurePending", "InfraStack is not available for the current PlatformEnvironment generation.")
	if !stackFound {
		condition = lifecycleCondition(object.Generation, metav1.ConditionFalse, "InfraStackNotFound", "The referenced InfraStack has not been created yet.")
	} else if !conditionReadyForGeneration(stack.Status.Conditions, ConditionReady, stack.Generation) {
		condition = lifecycleCondition(object.Generation, metav1.ConditionFalse, "InfrastructureNotReady", infrastructureMessage(&stack))
	} else if !conditionReadyForGeneration(child.Status.Conditions, ConditionReady, child.Generation) {
		condition = lifecycleCondition(object.Generation, metav1.ConditionFalse, "RuntimeNotReady", runtimeMessage(&child))
	} else {
		condition = lifecycleCondition(object.Generation, metav1.ConditionTrue, "EnvironmentReady", "Infrastructure, runtime, and required domain readiness are all current.")
	}
	status.Conditions = []metav1.Condition{stableCondition(findCondition(object.Status.Conditions, ConditionReady), object.Generation, condition.Status, condition.Reason, condition.Message)}
	if !reflect.DeepEqual(object.Status, status) {
		object.Status = status
		if err := r.Status().Update(ctx, &object); err != nil {
			return ctrl.Result{}, err
		}
	}
	if condition.Status != metav1.ConditionTrue {
		return ctrl.Result{RequeueAfter: environmentRequeue}, nil
	}
	return ctrl.Result{}, nil
}

func (r *PlatformEnvironmentReconciler) reconcileDelete(ctx context.Context, object *platformv1alpha1.PlatformEnvironment) (ctrl.Result, error) {
	status := object.Status
	status.ObservedGeneration = object.Generation
	status.Conditions = []metav1.Condition{stableCondition(findCondition(object.Status.Conditions, ConditionReady), object.Generation, metav1.ConditionFalse, "Deleting", "PlatformEnvironment deletion is waiting for runtime prune and infrastructure removal.")}
	if !reflect.DeepEqual(object.Status, status) {
		object.Status = status
		if err := r.Status().Update(ctx, object); err != nil {
			return ctrl.Result{}, err
		}
	}
	var child platformv1alpha1.ResourceSet
	childErr := r.Get(ctx, client.ObjectKey{Name: resourceSetName(object.Name), Namespace: object.Namespace}, &child)
	if childErr == nil {
		if child.DeletionTimestamp.IsZero() {
			if err := r.Delete(ctx, &child); err != nil && !apierrors.IsNotFound(err) {
				return ctrl.Result{}, err
			}
		}
		return ctrl.Result{RequeueAfter: environmentRequeue}, nil
	}
	if !apierrors.IsNotFound(childErr) {
		return ctrl.Result{}, childErr
	}

	if object.Spec.InfraStackRef.Name != "" {
		var stack platformv1alpha1.InfraStack
		stackErr := r.Get(ctx, client.ObjectKey{Name: object.Spec.InfraStackRef.Name, Namespace: object.Namespace}, &stack)
		if stackErr == nil {
			if stack.DeletionTimestamp.IsZero() && stack.Spec.DesiredState != platformv1alpha1.DesiredStateDestroy {
				stack.Spec.DesiredState = platformv1alpha1.DesiredStateDestroy
				if err := r.Update(ctx, &stack); err != nil {
					return ctrl.Result{}, err
				}
				return ctrl.Result{RequeueAfter: environmentRequeue}, nil
			}
			if !conditionReadyForGeneration(stack.Status.Conditions, ConditionReady, stack.Generation) || findCondition(stack.Status.Conditions, ConditionReady).Reason != "InfrastructureRemoved" {
				return ctrl.Result{RequeueAfter: environmentRequeue}, nil
			}
			if stack.DeletionTimestamp.IsZero() {
				if err := r.Delete(ctx, &stack); err != nil && !apierrors.IsNotFound(err) {
					return ctrl.Result{}, err
				}
				return ctrl.Result{RequeueAfter: environmentRequeue}, nil
			}
			return ctrl.Result{RequeueAfter: environmentRequeue}, nil
		}
		if !apierrors.IsNotFound(stackErr) {
			return ctrl.Result{}, stackErr
		}
	}

	if containsString(object.Finalizers, platformEnvironmentFinalizer) {
		object.Finalizers = removeString(object.Finalizers, platformEnvironmentFinalizer)
		if err := r.Update(ctx, object); err != nil && !apierrors.IsNotFound(err) {
			return ctrl.Result{}, err
		}
	}
	return ctrl.Result{}, nil
}

func (r *PlatformEnvironmentReconciler) SetupWithManager(manager ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(manager).
		For(&platformv1alpha1.PlatformEnvironment{}).
		Owns(&platformv1alpha1.ResourceSet{}).
		Watches(&platformv1alpha1.InfraStack{}, handler.EnqueueRequestsFromMapFunc(r.mapInfraStack)).
		Complete(r)
}

func (r *PlatformEnvironmentReconciler) mapInfraStack(ctx context.Context, object client.Object) []ctrl.Request {
	var environments platformv1alpha1.PlatformEnvironmentList
	if err := r.List(ctx, &environments, client.InNamespace(object.GetNamespace())); err != nil {
		return nil
	}
	requests := make([]ctrl.Request, 0, len(environments.Items))
	for index := range environments.Items {
		environment := &environments.Items[index]
		if environment.Spec.InfraStackRef.Name == object.GetName() {
			requests = append(requests, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(environment)})
		}
	}
	return requests
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

func conditionReadyForGeneration(conditions []metav1.Condition, conditionType string, generation int64) bool {
	condition := findCondition(conditions, conditionType)
	return condition != nil && condition.Status == metav1.ConditionTrue && condition.ObservedGeneration == generation
}

func infrastructureMessage(stack *platformv1alpha1.InfraStack) string {
	if condition := findCondition(stack.Status.Conditions, ConditionReady); condition != nil {
		return fmt.Sprintf("InfraStack generation %d is %s: %s", stack.Generation, condition.Reason, condition.Message)
	}
	return "InfraStack has not reported a current Ready condition."
}

func runtimeMessage(resourceSet *platformv1alpha1.ResourceSet) string {
	if condition := findCondition(resourceSet.Status.Conditions, ConditionReady); condition != nil {
		return fmt.Sprintf("ResourceSet generation %d is %s: %s", resourceSet.Generation, condition.Reason, condition.Message)
	}
	return "ResourceSet has not reported a current Ready condition."
}

func resourceSetName(environmentName string) string {
	name := fmt.Sprintf("%s-resources", environmentName)
	if len(name) > 63 {
		name = name[:63]
	}
	return strings.TrimRight(name, "-")
}
