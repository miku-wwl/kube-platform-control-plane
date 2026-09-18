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
	terraformexec "github.com/miku-wwl/kube-platform-control-plane/internal/terraform"
)

// +kubebuilder:rbac:groups=platform.example.io,resources=infrastacks,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=platform.example.io,resources=infrastacks/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=platform.example.io,resources=infrastacks/finalizers,verbs=update
// +kubebuilder:rbac:groups=platform.example.io,resources=terraformruns,verbs=get;list;watch;create;delete;update;patch
type InfraStackReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

func (r *InfraStackReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var object platformv1alpha1.InfraStack
	if err := r.Get(ctx, req.NamespacedName, &object); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	desiredState := object.Spec.DesiredState
	if desiredState == platformv1alpha1.DesiredStateDestroy {
		condition := lifecycleCondition(object.Generation, metav1.ConditionFalse, "RecoveryRequired", "Destroy is blocked until a retained source bundle from the last successful Apply is available.")
		if object.Status.ObservedGeneration != object.Generation || object.Status.LatestPlanRunRef != nil || !sameCondition(findCondition(object.Status.Conditions, ConditionReady), &condition) {
			object.Status.ObservedGeneration = object.Generation
			object.Status.LatestPlanRunRef = nil
			object.Status.Conditions = []metav1.Condition{condition}
			if err := r.Status().Update(ctx, &object); err != nil {
				return ctrl.Result{}, err
			}
		}
		return ctrl.Result{}, nil
	}

	childName := planRunName(object.Name, object.Generation)
	var child platformv1alpha1.TerraformRun
	getErr := r.Get(ctx, client.ObjectKey{Name: childName, Namespace: object.Namespace}, &child)
	if apierrors.IsNotFound(getErr) {
		child = *buildPlanRun(&object, childName)
		if err := ctrl.SetControllerReference(&object, &child, r.Scheme); err != nil {
			return ctrl.Result{}, err
		}
		if err := r.Create(ctx, &child); err != nil {
			return ctrl.Result{}, err
		}
	} else if getErr != nil {
		return ctrl.Result{}, getErr
	}

	condition := lifecycleCondition(object.Generation, metav1.ConditionFalse, "PlanPending", "Terraform PlanRun is pending.")
	if childCondition := findCondition(child.Status.Conditions, ConditionReady); childCondition != nil {
		condition = *childCondition
	}
	if object.Status.ObservedGeneration != object.Generation || object.Status.LatestPlanRunRef == nil || object.Status.LatestPlanRunRef.Name != child.Name || !sameCondition(findCondition(object.Status.Conditions, ConditionReady), &condition) {
		object.Status.ObservedGeneration = object.Generation
		object.Status.LatestPlanRunRef = &corev1.LocalObjectReference{Name: child.Name}
		object.Status.Conditions = []metav1.Condition{condition}
		if err := r.Status().Update(ctx, &object); err != nil {
			return ctrl.Result{}, err
		}
	}
	return ctrl.Result{}, nil
}

func (r *InfraStackReconciler) SetupWithManager(manager ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(manager).For(&platformv1alpha1.InfraStack{}).Owns(&platformv1alpha1.TerraformRun{}).Complete(r)
}

func buildPlanRun(stack *platformv1alpha1.InfraStack, name string) *platformv1alpha1.TerraformRun {
	return &platformv1alpha1.TerraformRun{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: stack.Namespace},
		Spec: platformv1alpha1.TerraformRunSpec{
			StackRef:             corev1.LocalObjectReference{Name: stack.Name},
			InfraStackGeneration: stack.Generation,
			Operation:            "Plan",
			PlanMode:             "Reconcile",
			Source: platformv1alpha1.PlanRunSourceSpec{
				Type:     terraformexec.SourceGitCommit,
				URL:      stack.Spec.Source.URL,
				Revision: stack.Spec.Source.Revision,
				Path:     stack.Spec.Source.Path,
			},
			ResolvedBackendConfigRef: stack.Spec.Backend.ConfigRef.Name,
			LockTimeout:              stack.Spec.Backend.LockTimeout,
			VariableSecretRefs:       stack.Spec.Variables.SecretRefs,
			Workspace:                stack.Spec.Workspace,
			Executor:                 stack.Spec.Executor,
		},
	}
}

func planRunName(stackName string, generation int64) string {
	name := fmt.Sprintf("%s-plan-%d", stackName, generation)
	if len(name) > 63 {
		name = name[:63]
	}
	return strings.TrimRight(name, "-")
}

func lifecycleCondition(generation int64, status metav1.ConditionStatus, reason, message string) metav1.Condition {
	return metav1.Condition{Type: ConditionReady, Status: status, ObservedGeneration: generation, Reason: reason, Message: message, LastTransitionTime: metav1.Now()}
}

func sameCondition(left, right *metav1.Condition) bool {
	if left == nil || right == nil {
		return left == right
	}
	return left.Type == right.Type && left.Status == right.Status && left.ObservedGeneration == right.ObservedGeneration && left.Reason == right.Reason && left.Message == right.Message
}
