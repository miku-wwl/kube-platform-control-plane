package controller

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"

	platformv1alpha1 "github.com/miku-wwl/kube-platform-control-plane/api/v1alpha1"
	targetresolver "github.com/miku-wwl/kube-platform-control-plane/internal/target"
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
// +kubebuilder:rbac:groups="",resources=serviceaccounts,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=roles,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=rolebindings,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=namespaces,verbs=get
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
	if err := validatePlatformEnvironmentSpec(&object); err != nil {
		return r.setEnvironmentCondition(ctx, &object, "InvalidSpec", err.Error(), metav1.ConditionFalse)
	}

	stackRefName := object.Spec.InfraStackRef.Name
	target := object.Spec.Target
	var class *platformv1alpha1.EnvironmentClass
	var classObjects []platformv1alpha1.RuntimeObject
	if object.Spec.ClassRef != nil && object.Spec.ClassRef.Name != "" {
		var classObject platformv1alpha1.EnvironmentClass
		if err := r.Get(ctx, client.ObjectKey{Name: object.Spec.ClassRef.Name}, &classObject); err != nil {
			if apierrors.IsNotFound(err) {
				return r.setEnvironmentCondition(ctx, &object, "ClassNotFound", fmt.Sprintf("EnvironmentClass %q was not found", object.Spec.ClassRef.Name), metav1.ConditionFalse)
			}
			return ctrl.Result{}, err
		}
		class = &classObject
		if err := validateEnvironmentClass(&object, class); err != nil {
			return r.setEnvironmentCondition(ctx, &object, "ClassRejected", err.Error(), metav1.ConditionFalse)
		}
		if err := r.validateTenantPolicy(ctx, &object, class); err != nil {
			return r.setEnvironmentCondition(ctx, &object, "QuotaRejected", err.Error(), metav1.ConditionFalse)
		}
		if err := r.ensureRunnerIdentity(ctx, &object, class); err != nil {
			return ctrl.Result{}, err
		}
		stackRefName = classStackName(object.Name)
		target = classTarget(&object, class)
		classObjects = classRuntimeObjects(class)
		var classStack platformv1alpha1.InfraStack
		classStackErr := r.Get(ctx, client.ObjectKey{Name: stackRefName, Namespace: object.Namespace}, &classStack)
		if apierrors.IsNotFound(classStackErr) {
			desiredStack, buildErr := buildClassInfraStack(&object, class, stackRefName, target)
			if buildErr != nil {
				return r.setEnvironmentCondition(ctx, &object, "ClassRejected", buildErr.Error(), metav1.ConditionFalse)
			}
			classStack = *desiredStack
			if err := ctrl.SetControllerReference(&object, &classStack, r.Scheme); err != nil {
				return ctrl.Result{}, err
			}
			if err := r.Create(ctx, &classStack); err != nil {
				return ctrl.Result{}, err
			}
		} else if classStackErr != nil {
			return ctrl.Result{}, classStackErr
		} else {
			if hasForeignController(&classStack, &object, "PlatformEnvironment") {
				return r.setEnvironmentCondition(ctx, &object, "ChildOwnershipConflict", fmt.Sprintf("InfraStack %q exists but is owned by another PlatformEnvironment", classStack.Name), metav1.ConditionFalse)
			}
			if object.UID != "" && !ownedBy(&classStack, &object, "PlatformEnvironment") {
				if err := ctrl.SetControllerReference(&object, &classStack, r.Scheme); err != nil {
					return ctrl.Result{}, err
				}
				if err := r.Update(ctx, &classStack); err != nil {
					return ctrl.Result{}, err
				}
				return ctrl.Result{RequeueAfter: environmentRequeue}, nil
			}
			desiredStack, buildErr := buildClassInfraStack(&object, class, stackRefName, target)
			if buildErr != nil {
				return r.setEnvironmentCondition(ctx, &object, "ClassRejected", buildErr.Error(), metav1.ConditionFalse)
			}
			if !reflect.DeepEqual(classStack.Spec, desiredStack.Spec) {
				classStack.Spec = desiredStack.Spec
				if err := r.Update(ctx, &classStack); err != nil {
					return ctrl.Result{}, err
				}
				return ctrl.Result{RequeueAfter: environmentRequeue}, nil
			}
		}
	}
	childName := resourceSetName(object.Name)
	var child platformv1alpha1.ResourceSet
	getErr := r.Get(ctx, client.ObjectKey{Name: childName, Namespace: object.Namespace}, &child)
	if apierrors.IsNotFound(getErr) {
		child = *buildResourceSetForTarget(&object, childName, target, classObjects)
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
	if hasForeignController(&child, &object, "PlatformEnvironment") {
		return r.setEnvironmentCondition(ctx, &object, "ChildOwnershipConflict", fmt.Sprintf("ResourceSet %q exists but is owned by another PlatformEnvironment", child.Name), metav1.ConditionFalse)
	}
	if object.UID != "" && !ownedBy(&child, &object, "PlatformEnvironment") {
		if err := ctrl.SetControllerReference(&object, &child, r.Scheme); err != nil {
			return ctrl.Result{}, err
		}
		if err := r.Update(ctx, &child); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{RequeueAfter: environmentRequeue}, nil
	}

	stackReady := false
	var stack platformv1alpha1.InfraStack
	if stackRefName != "" {
		getErr = r.Get(ctx, client.ObjectKey{Name: stackRefName, Namespace: object.Namespace}, &stack)
		if apierrors.IsNotFound(getErr) {
			getErr = nil
		} else if getErr != nil {
			return ctrl.Result{}, getErr
		} else {
			stackReady = conditionReadyForGeneration(stack.Status.Conditions, ConditionReady, stack.Generation) && stack.Spec.DesiredState == platformv1alpha1.DesiredStatePresent && findCondition(stack.Status.Conditions, ConditionReady).Reason == "InfrastructureReady"
		}
	}
	targetDigest, _ := targetRuntimeIdentityDigest(target)
	childSpec := child.Spec
	childSpec.Target = target
	childSpec.Resources = classObjects
	childSpec.RuntimeTargetIdentityDigest = targetDigest
	childSpec.TrustedRuntimeTargetIdentity = nil
	childSpec.TrustedTargetConnectionProfile = nil
	childSpec.TargetDiscoveryRef = ""
	childSpec.TargetDiscoveryDigest = ""
	if stack.Status.DiscoveredRuntimeTargetIdentity != nil && stack.Status.DiscoveredTargetConnectionProfile != nil {
		childSpec.TrustedRuntimeTargetIdentity = stack.Status.DiscoveredRuntimeTargetIdentity.DeepCopy()
		childSpec.TrustedTargetConnectionProfile = stack.Status.DiscoveredTargetConnectionProfile.DeepCopy()
		childSpec.TargetDiscoveryRef = stack.Status.TargetDiscoveryRef
		childSpec.TargetDiscoveryDigest = stack.Status.TargetDiscoveryDigest
		if trustedDigest, digestErr := targetresolver.Digest(stack.Status.DiscoveredRuntimeTargetIdentity); digestErr == nil {
			targetDigest = trustedDigest
			childSpec.RuntimeTargetIdentityDigest = trustedDigest
		}
	}
	trustedDiscovery := childSpec.TrustedRuntimeTargetIdentity != nil && childSpec.TrustedTargetConnectionProfile != nil && childSpec.TargetDiscoveryRef != "" && childSpec.TargetDiscoveryDigest != ""
	if target.Provider == "aws" && !trustedDiscovery {
		stackReady = false
	}
	childSpec.RuntimeMutationAllowed = stackReady && !stack.Spec.MutationFence
	childSpec.MutationFence = stack.Spec.MutationFence || !stackReady
	if !reflect.DeepEqual(child.Spec, childSpec) {
		child.Spec = childSpec
		if err := r.Update(ctx, &child); err != nil {
			return ctrl.Result{}, err
		}
	}

	stackFound := stackRefName != ""
	if stackFound {
		getErr = r.Get(ctx, client.ObjectKey{Name: stackRefName, Namespace: object.Namespace}, &stack)
		if apierrors.IsNotFound(getErr) {
			stackFound = false
		} else if getErr != nil {
			return ctrl.Result{}, getErr
		}
	}

	status := object.Status
	status.ObservedGeneration = object.Generation
	status.InfraStackRef = nil
	if stackRefName != "" {
		status.InfraStackRef = &corev1.LocalObjectReference{Name: stackRefName}
	}
	identity := targetRuntimeIdentity(target)
	connection := environmentTargetConnection(target)
	if stack.Status.DiscoveredRuntimeTargetIdentity != nil && stack.Status.DiscoveredTargetConnectionProfile != nil {
		identity = *stack.Status.DiscoveredRuntimeTargetIdentity
		connection = *stack.Status.DiscoveredTargetConnectionProfile
	}
	status.TargetIdentity = &identity
	status.TargetConnection = &connection
	status.ResourceSetRef = &corev1.LocalObjectReference{Name: child.Name}

	condition := lifecycleCondition(object.Generation, metav1.ConditionFalse, "InfrastructurePending", "InfraStack is not available for the current PlatformEnvironment generation.")
	if !stackFound {
		condition = lifecycleCondition(object.Generation, metav1.ConditionFalse, "InfraStackNotFound", "The referenced InfraStack has not been created yet.")
	} else if !conditionReadyForGeneration(stack.Status.Conditions, ConditionReady, stack.Generation) || findCondition(stack.Status.Conditions, ConditionReady).Reason != "InfrastructureReady" {
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

func (r *PlatformEnvironmentReconciler) validateTenantPolicy(ctx context.Context, environment *platformv1alpha1.PlatformEnvironment, class *platformv1alpha1.EnvironmentClass) error {
	var namespace corev1.Namespace
	if err := r.Get(ctx, client.ObjectKey{Name: environment.Namespace}, &namespace); err != nil {
		return fmt.Errorf("read tenant namespace: %w", err)
	}
	if allowed := splitAnnotation(namespace.Annotations["platform.example.io/allowed-environment-classes"]); len(allowed) > 0 && !containsString(allowed, class.Name) {
		return fmt.Errorf("EnvironmentClass %q is not allowed in tenant namespace %q", class.Name, environment.Namespace)
	}
	maxEnvironments := class.Spec.CapacityBounds.MaxEnvironments
	if raw := namespace.Annotations["platform.example.io/max-platform-environments"]; raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 {
			return fmt.Errorf("tenant namespace max-platform-environments annotation must be a positive integer")
		}
		maxEnvironments = int32(parsed)
	}
	if maxEnvironments <= 0 {
		return nil
	}
	var environments platformv1alpha1.PlatformEnvironmentList
	if err := r.List(ctx, &environments, client.InNamespace(environment.Namespace)); err != nil {
		return fmt.Errorf("list tenant environments: %w", err)
	}
	if environmentAlreadyAdmitted(environment) {
		return nil
	}
	admitted := int32(0)
	pendingBefore := int32(0)
	for index := range environments.Items {
		candidate := &environments.Items[index]
		if candidate.Name == environment.Name && candidate.Namespace == environment.Namespace {
			continue
		}
		if candidate.DeletionTimestamp.IsZero() {
			if environmentAlreadyAdmitted(candidate) {
				admitted++
			} else if candidate.CreationTimestamp.Before(&environment.CreationTimestamp) || (candidate.CreationTimestamp.Equal(&environment.CreationTimestamp) && candidate.Name < environment.Name) {
				pendingBefore++
			}
		}
	}
	if admitted+pendingBefore >= maxEnvironments {
		return fmt.Errorf("tenant namespace %q has reached maximum admitted PlatformEnvironments (%d)", environment.Namespace, maxEnvironments)
	}
	return nil
}

func environmentAlreadyAdmitted(environment *platformv1alpha1.PlatformEnvironment) bool {
	if environment == nil {
		return false
	}
	if environment.Status.InfraStackRef != nil && environment.Status.InfraStackRef.Name != "" {
		return true
	}
	condition := findCondition(environment.Status.Conditions, ConditionReady)
	return condition != nil && condition.Reason != "QuotaRejected" && condition.Reason != "ClassRejected" && condition.Reason != "ClassNotFound"
}

func splitAnnotation(value string) []string {
	values := strings.Split(value, ",")
	result := make([]string, 0, len(values))
	for _, item := range values {
		if trimmed := strings.TrimSpace(item); trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}

func (r *PlatformEnvironmentReconciler) ensureRunnerIdentity(ctx context.Context, environment *platformv1alpha1.PlatformEnvironment, class *platformv1alpha1.EnvironmentClass) error {
	name := runnerServiceAccountName(class)
	if name == "" {
		return nil
	}
	annotations := runnerServiceAccountAnnotations(class)
	serviceAccount := &corev1.ServiceAccount{}
	key := client.ObjectKey{Name: name, Namespace: environment.Namespace}
	if err := r.Get(ctx, key, serviceAccount); apierrors.IsNotFound(err) {
		serviceAccount = &corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: environment.Namespace, Labels: map[string]string{"platform.example.io/managed-by": "platform-control-plane"}, Annotations: annotations}}
		if err := ctrl.SetControllerReference(environment, serviceAccount, r.Scheme); err != nil {
			return err
		}
		if err := r.Create(ctx, serviceAccount); err != nil && !apierrors.IsAlreadyExists(err) {
			return err
		}
	} else if err != nil {
		return err
	} else if len(annotations) > 0 || serviceAccount.Annotations != nil {
		if serviceAccount.Labels["platform.example.io/managed-by"] != "platform-control-plane" {
			return fmt.Errorf("runner management identity requires controller-managed ServiceAccount %q", name)
		}
		desiredRole := annotations["eks.amazonaws.com/role-arn"]
		if serviceAccount.Annotations["eks.amazonaws.com/role-arn"] != desiredRole {
			if serviceAccount.Annotations == nil {
				serviceAccount.Annotations = map[string]string{}
			}
			if desiredRole == "" {
				delete(serviceAccount.Annotations, "eks.amazonaws.com/role-arn")
			} else {
				serviceAccount.Annotations["eks.amazonaws.com/role-arn"] = desiredRole
			}
			if err := r.Update(ctx, serviceAccount); err != nil {
				return err
			}
		}
	}
	role := &rbacv1.Role{}
	if err := r.Get(ctx, key, role); apierrors.IsNotFound(err) {
		role = &rbacv1.Role{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: environment.Namespace, Labels: map[string]string{"platform.example.io/managed-by": "platform-control-plane"}}, Rules: []rbacv1.PolicyRule{{APIGroups: []string{""}, Resources: []string{"configmaps", "pods"}, Verbs: []string{"get"}}, {APIGroups: []string{"batch"}, Resources: []string{"jobs"}, Verbs: []string{"get"}}}}
		if err := ctrl.SetControllerReference(environment, role, r.Scheme); err != nil {
			return err
		}
		if err := r.Create(ctx, role); err != nil && !apierrors.IsAlreadyExists(err) {
			return err
		}
	} else if err != nil {
		return err
	}
	binding := &rbacv1.RoleBinding{}
	if err := r.Get(ctx, key, binding); apierrors.IsNotFound(err) {
		binding = &rbacv1.RoleBinding{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: environment.Namespace, Labels: map[string]string{"platform.example.io/managed-by": "platform-control-plane"}}, RoleRef: rbacv1.RoleRef{APIGroup: rbacv1.GroupName, Kind: "Role", Name: name}, Subjects: []rbacv1.Subject{{Kind: "ServiceAccount", Name: name, Namespace: environment.Namespace}}}
		if err := ctrl.SetControllerReference(environment, binding, r.Scheme); err != nil {
			return err
		}
		if err := r.Create(ctx, binding); err != nil && !apierrors.IsAlreadyExists(err) {
			return err
		}
	} else if err != nil {
		return err
	}
	return nil
}

func runnerServiceAccountAnnotations(class *platformv1alpha1.EnvironmentClass) map[string]string {
	if class == nil || class.Spec.Target.Provider != targetresolver.ProviderAWS || class.Spec.RunnerProfile.ManagementRoleARN == "" {
		return nil
	}
	return map[string]string{"eks.amazonaws.com/role-arn": class.Spec.RunnerProfile.ManagementRoleARN}
}

func validatePlatformEnvironmentSpec(object *platformv1alpha1.PlatformEnvironment) error {
	classMode := object.Spec.ClassRef != nil && object.Spec.ClassRef.Name != ""
	legacyMode := object.Spec.InfraStackRef.Name != "" && object.Spec.Target.Provider != "" && object.Spec.Target.ClusterName != ""
	if classMode == legacyMode {
		return fmt.Errorf("use exactly one of Class Mode or legacy InfraStack/Target mode")
	}
	return nil
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
	stackName := ""
	if object.Status.InfraStackRef != nil {
		stackName = object.Status.InfraStackRef.Name
	}
	if stackName == "" {
		stackName = object.Spec.InfraStackRef.Name
	}
	if stackName == "" && object.Spec.ClassRef != nil && object.Spec.ClassRef.Name != "" {
		stackName = classStackName(object.Name)
	}
	var stack platformv1alpha1.InfraStack
	stackFound := false
	if stackName != "" {
		err := r.Get(ctx, client.ObjectKey{Name: stackName, Namespace: object.Namespace}, &stack)
		if err == nil {
			stackFound = true
			if stack.Spec.OwnerEnvironmentUID != "" && stack.Spec.OwnerEnvironmentUID != string(object.UID) {
				return r.setEnvironmentCondition(ctx, object, "ChildOwnershipConflict", fmt.Sprintf("InfraStack %q is owned by another PlatformEnvironment", stack.Name), metav1.ConditionFalse)
			}
			if !stack.Spec.MutationFence {
				stack.Spec.MutationFence = true
				if err := r.Update(ctx, &stack); err != nil {
					return ctrl.Result{}, err
				}
				return ctrl.Result{RequeueAfter: environmentRequeue}, nil
			}
		} else if !apierrors.IsNotFound(err) {
			return ctrl.Result{}, err
		}
	}
	var child platformv1alpha1.ResourceSet
	childErr := r.Get(ctx, client.ObjectKey{Name: resourceSetName(object.Name), Namespace: object.Namespace}, &child)
	if childErr == nil {
		if !child.Spec.MutationFence || child.Spec.RuntimeMutationAllowed {
			child.Spec.MutationFence = true
			child.Spec.RuntimeMutationAllowed = false
			if err := r.Update(ctx, &child); err != nil {
				return ctrl.Result{}, err
			}
			return ctrl.Result{RequeueAfter: environmentRequeue}, nil
		}
		if stackFound && hasActiveMutatingApply(ctx, r, &stack) {
			return ctrl.Result{RequeueAfter: environmentRequeue}, nil
		}
		if stackFound && stackConditionReason(&stack) == "RecoveryRequired" {
			return r.setEnvironmentCondition(ctx, object, "RecoveryRequired", "Destroy is blocked until the Apply mutation outcome is observed and reconciled.", metav1.ConditionUnknown)
		}
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
	if stackFound {
		if stack.Spec.DesiredState != platformv1alpha1.DesiredStateDestroy && stack.DeletionTimestamp.IsZero() {
			stack.Spec.DesiredState = platformv1alpha1.DesiredStateDestroy
			if err := r.Update(ctx, &stack); err != nil {
				return ctrl.Result{}, err
			}
			return ctrl.Result{RequeueAfter: environmentRequeue}, nil
		}
		condition := findCondition(stack.Status.Conditions, ConditionReady)
		if condition == nil || condition.Status != metav1.ConditionTrue || condition.Reason != "InfrastructureRemoved" {
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

	if containsString(object.Finalizers, platformEnvironmentFinalizer) {
		object.Finalizers = removeString(object.Finalizers, platformEnvironmentFinalizer)
		if err := r.Update(ctx, object); err != nil && !apierrors.IsNotFound(err) {
			return ctrl.Result{}, err
		}
	}
	return ctrl.Result{}, nil
}

func hasActiveMutatingApply(ctx context.Context, reconciler *PlatformEnvironmentReconciler, stack *platformv1alpha1.InfraStack) bool {
	if reconciler == nil || stack == nil {
		return false
	}
	var runs platformv1alpha1.TerraformRunList
	if err := reconciler.List(ctx, &runs, client.InNamespace(stack.Namespace)); err != nil {
		return true
	}
	for index := range runs.Items {
		run := &runs.Items[index]
		if run.Spec.StackRef.Name != stack.Name || run.Spec.Operation != "Apply" {
			continue
		}
		if run.Status.ExecutionOutcome == "" || run.Status.ExecutionOutcome == "Pending" || run.Status.ExecutionOutcome == "Running" {
			return true
		}
	}
	return false
}

func stackConditionReason(stack *platformv1alpha1.InfraStack) string {
	if stack == nil {
		return ""
	}
	if condition := findCondition(stack.Status.Conditions, ConditionReady); condition != nil {
		return condition.Reason
	}
	return ""
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
		if environment.Spec.InfraStackRef.Name == object.GetName() || (environment.Status.InfraStackRef != nil && environment.Status.InfraStackRef.Name == object.GetName()) {
			requests = append(requests, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(environment)})
		}
	}
	return requests
}

func buildResourceSet(environment *platformv1alpha1.PlatformEnvironment, name string) *platformv1alpha1.ResourceSet {
	return buildResourceSetForTarget(environment, name, environment.Spec.Target, nil)
}

func buildResourceSetForTarget(environment *platformv1alpha1.PlatformEnvironment, name string, target platformv1alpha1.TargetReference, resources []platformv1alpha1.RuntimeObject) *platformv1alpha1.ResourceSet {
	ownershipID := string(environment.UID)
	if ownershipID == "" {
		ownershipID = environment.Namespace + "/" + environment.Name
	}
	return &platformv1alpha1.ResourceSet{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: environment.Namespace},
		Spec: platformv1alpha1.ResourceSetSpec{
			Target:                 target,
			OwnershipID:            ownershipID,
			MaxInventoryItems:      500,
			Resources:              resources,
			RuntimeMutationAllowed: false,
			MutationFence:          true,
		},
	}
}

func targetRuntimeIdentityDigest(target platformv1alpha1.TargetReference) (string, error) {
	identity := targetRuntimeIdentity(target)
	encoded, err := json.Marshal(identity)
	if err != nil {
		return "", err
	}
	hash := sha256.Sum256(encoded)
	return "sha256:" + hex.EncodeToString(hash[:]), nil
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
	return boundedResourceName(fmt.Sprintf("%s-resources", environmentName))
}

func ownedBy(object metav1.Object, parent metav1.Object, kind string) bool {
	if object == nil || parent == nil || parent.GetUID() == "" {
		return false
	}
	for _, owner := range object.GetOwnerReferences() {
		if owner.Controller != nil && *owner.Controller && owner.Kind == kind && owner.UID == parent.GetUID() {
			return true
		}
	}
	return false
}

func hasForeignController(object metav1.Object, parent metav1.Object, kind string) bool {
	if object == nil {
		return false
	}
	parentUID := ""
	if parent != nil {
		parentUID = string(parent.GetUID())
	}
	for _, owner := range object.GetOwnerReferences() {
		if owner.Controller != nil && *owner.Controller && owner.Kind == kind && string(owner.UID) != parentUID {
			return true
		}
	}
	return false
}

func (r *PlatformEnvironmentReconciler) setEnvironmentCondition(ctx context.Context, object *platformv1alpha1.PlatformEnvironment, reason, message string, status metav1.ConditionStatus) (ctrl.Result, error) {
	object.Status.ObservedGeneration = object.Generation
	object.Status.Conditions = []metav1.Condition{stableCondition(findCondition(object.Status.Conditions, ConditionReady), object.Generation, status, reason, message)}
	if err := r.Status().Update(ctx, object); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{RequeueAfter: environmentRequeue}, nil
}
