package controller

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	platformv1alpha1 "github.com/miku-wwl/kube-platform-control-plane/api/v1alpha1"
	runtimer "github.com/miku-wwl/kube-platform-control-plane/internal/runtime"
)

const (
	resourceSetFinalizer     = "platform.example.io/resourceset-finalizer"
	defaultMaxInventoryItems = int32(500)
	maxInventoryStatusBytes  = 700 * 1024
	defaultCleanupTimeout    = 5 * time.Second
)

// +kubebuilder:rbac:groups=platform.example.io,resources=resourcesets,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=platform.example.io,resources=resourcesets/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=platform.example.io,resources=resourcesets/finalizers,verbs=update
type ResourceSetReconciler struct {
	client.Client
	Scheme       *runtime.Scheme
	TargetClient dynamic.Interface
}

func (r *ResourceSetReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var object platformv1alpha1.ResourceSet
	if err := r.Get(ctx, req.NamespacedName, &object); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	if object.DeletionTimestamp.IsZero() {
		if !containsString(object.Finalizers, resourceSetFinalizer) {
			object.Finalizers = append(object.Finalizers, resourceSetFinalizer)
			if err := r.Update(ctx, &object); err != nil {
				return ctrl.Result{}, err
			}
			return ctrl.Result{Requeue: true}, nil
		}
		return r.reconcilePresent(ctx, &object)
	}
	return r.reconcileDelete(ctx, &object)
}

func (r *ResourceSetReconciler) reconcilePresent(ctx context.Context, object *platformv1alpha1.ResourceSet) (ctrl.Result, error) {
	maxItems := object.Spec.MaxInventoryItems
	if maxItems == 0 {
		maxItems = defaultMaxInventoryItems
	}
	if maxItems < 0 {
		return r.setRuntimeStatus(ctx, object, nil, false, metav1.ConditionFalse, "InvalidInventoryLimit", "maxInventoryItems cannot be negative")
	}
	if int32(len(object.Spec.Resources)) > maxItems {
		return r.setRuntimeStatus(ctx, object, nil, true, metav1.ConditionFalse, "InventoryLimitExceeded", fmt.Sprintf("ResourceSet declares %d resources, limit is %d", len(object.Spec.Resources), maxItems))
	}
	if r.TargetClient == nil {
		return r.setRuntimeStatus(ctx, object, nil, false, metav1.ConditionUnknown, "RuntimeClientUnavailable", "target Kubernetes client is not configured; runtime mutation is disabled")
	}

	target := targetIdentity(object.Spec.Target)
	objects, err := bootstrapObjects(object.Spec.Resources)
	if err != nil {
		return r.setRuntimeStatus(ctx, object, nil, false, metav1.ConditionFalse, "RuntimeObjectRejected", err.Error())
	}
	if len(objects) == 0 {
		inventory := []runtimer.InventoryItem{}
		if err := r.pruneRemoved(ctx, object, inventory, target); err != nil {
			return r.setRuntimeStatus(ctx, object, nil, false, metav1.ConditionFalse, "RuntimePruneRejected", err.Error())
		}
		return r.setRuntimeStatus(ctx, object, inventory, false, metav1.ConditionTrue, "RuntimeEmpty", "ResourceSet has no runtime objects")
	}
	inventory, err := runtimer.ApplyBootstrap(ctx, r.TargetClient, objects, fieldManager(object), target)
	if err != nil {
		return r.setRuntimeStatus(ctx, object, nil, false, metav1.ConditionFalse, "RuntimeApplyFailed", err.Error())
	}
	if err := r.pruneRemoved(ctx, object, inventory, target); err != nil {
		return r.setRuntimeStatus(ctx, object, nil, false, metav1.ConditionFalse, "RuntimePruneRejected", err.Error())
	}
	if err := validateInventorySize(inventory); err != nil {
		return r.setRuntimeStatus(ctx, object, nil, false, metav1.ConditionFalse, "InventoryStatusTooLarge", err.Error())
	}

	message, reason, conditionStatus := r.runtimeReadiness(ctx, inventory)
	return r.setRuntimeStatus(ctx, object, inventory, false, conditionStatus, reason, message)
}

func (r *ResourceSetReconciler) reconcileDelete(ctx context.Context, object *platformv1alpha1.ResourceSet) (ctrl.Result, error) {
	if !containsString(object.Finalizers, resourceSetFinalizer) {
		return ctrl.Result{}, nil
	}
	if r.TargetClient == nil {
		return r.finishDelete(ctx, object, true, "target Kubernetes client unavailable; runtime cleanup was skipped")
	}
	cleanupCtx, cancel := context.WithTimeout(ctx, defaultCleanupTimeout)
	defer cancel()
	target := targetIdentity(object.Spec.Target)
	if err := r.pruneInventory(cleanupCtx, object.Status.Inventory, target); err != nil {
		return r.finishDelete(ctx, object, true, fmt.Sprintf("runtime cleanup skipped after bounded attempt: %v", err))
	}
	return r.finishDelete(ctx, object, false, "runtime inventory cleaned")
}

func (r *ResourceSetReconciler) pruneRemoved(ctx context.Context, object *platformv1alpha1.ResourceSet, current []runtimer.InventoryItem, target runtimer.TargetIdentity) error {
	currentKeys := make(map[string]struct{}, len(current))
	for _, item := range current {
		currentKeys[inventoryKey(item.Group, item.Version, item.Resource, item.Namespace, item.Name)] = struct{}{}
	}
	for _, previous := range object.Status.Inventory {
		key := inventoryKey(previous.Group, previous.Version, previous.Resource, previous.Namespace, previous.Name)
		if _, found := currentKeys[key]; found {
			continue
		}
		if err := r.pruneItem(ctx, inventoryFromStatus(previous), target); err != nil {
			return err
		}
	}
	return nil
}

func (r *ResourceSetReconciler) pruneInventory(ctx context.Context, inventory []platformv1alpha1.InventoryItemStatus, target runtimer.TargetIdentity) error {
	for _, statusItem := range inventory {
		if err := r.pruneItem(ctx, inventoryFromStatus(statusItem), target); err != nil {
			if apierrors.IsNotFound(err) {
				continue
			}
			return err
		}
	}
	return nil
}

func (r *ResourceSetReconciler) pruneItem(ctx context.Context, item runtimer.InventoryItem, target runtimer.TargetIdentity) error {
	if item.Resource == "" {
		return fmt.Errorf("inventory item %s/%s has no resource name", item.Namespace, item.Name)
	}
	gvr := schema.GroupVersionResource{Group: item.Group, Version: item.Version, Resource: item.Resource}
	var resource dynamic.ResourceInterface
	if item.Namespace != "" {
		resource = r.TargetClient.Resource(gvr).Namespace(item.Namespace)
	} else {
		resource = r.TargetClient.Resource(gvr)
	}
	object, err := resource.Get(ctx, item.Name, metav1.GetOptions{})
	if err != nil {
		return err
	}
	if err := runtimer.ValidatePrune(item, target, object); err != nil {
		return err
	}
	return resource.Delete(ctx, item.Name, metav1.DeleteOptions{})
}

func (r *ResourceSetReconciler) runtimeReadiness(ctx context.Context, inventory []runtimer.InventoryItem) (string, string, metav1.ConditionStatus) {
	unknown := 0
	notReady := 0
	for _, item := range inventory {
		gvr := schema.GroupVersionResource{Group: item.Group, Version: item.Version, Resource: item.Resource}
		var resource dynamic.ResourceInterface
		if item.Namespace != "" {
			resource = r.TargetClient.Resource(gvr).Namespace(item.Namespace)
		} else {
			resource = r.TargetClient.Resource(gvr)
		}
		object, err := resource.Get(ctx, item.Name, metav1.GetOptions{})
		if err != nil {
			return fmt.Sprintf("runtime readiness read failed: %v", err), "RuntimeReadinessFailed", metav1.ConditionFalse
		}
		ready, reason, err := runtimer.Ready(object)
		if err != nil {
			return fmt.Sprintf("runtime readiness invalid: %v", err), "RuntimeReadinessFailed", metav1.ConditionFalse
		}
		if ready {
			continue
		}
		if reason == "readiness adapter unavailable" || reason == "condition not reported" || reason == "StatefulSet replicas or revision is not ready" {
			unknown++
		} else {
			notReady++
		}
	}
	if notReady > 0 {
		return fmt.Sprintf("%d runtime objects are not ready", notReady), "RuntimeNotReady", metav1.ConditionFalse
	}
	if unknown > 0 {
		return fmt.Sprintf("%d runtime objects have no ready adapter or are still reporting readiness", unknown), "RuntimeReadinessUnknown", metav1.ConditionUnknown
	}
	return fmt.Sprintf("%d runtime objects are ready", len(inventory)), "RuntimeReady", metav1.ConditionTrue
}

func (r *ResourceSetReconciler) setRuntimeStatus(ctx context.Context, object *platformv1alpha1.ResourceSet, inventory []runtimer.InventoryItem, limitExceeded bool, conditionStatus metav1.ConditionStatus, reason, message string) (ctrl.Result, error) {
	status := object.Status
	status.ObservedGeneration = object.Generation
	digest, err := targetIdentity(object.Spec.Target).Digest()
	if err != nil {
		return ctrl.Result{}, err
	}
	status.TargetIdentityDigest = digest
	status.InventoryLimitExceeded = limitExceeded
	if inventory != nil {
		status.Inventory = inventoryStatus(inventory)
		status.InventoryItems = int32(len(inventory))
	}
	status.Conditions = []metav1.Condition{stableCondition(findCondition(object.Status.Conditions, ConditionReady), object.Generation, conditionStatus, reason, message)}
	if reflect.DeepEqual(object.Status, status) {
		return ctrl.Result{}, nil
	}
	object.Status = status
	return ctrl.Result{}, r.Status().Update(ctx, object)
}

func (r *ResourceSetReconciler) finishDelete(ctx context.Context, object *platformv1alpha1.ResourceSet, skipped bool, message string) (ctrl.Result, error) {
	if skipped {
		object.Status.RuntimeCleanupSkipped = true
		object.Status.Conditions = []metav1.Condition{stableCondition(findCondition(object.Status.Conditions, ConditionReady), object.Generation, metav1.ConditionFalse, "RuntimeCleanupSkipped", message)}
		if err := r.Status().Update(ctx, object); err != nil && !apierrors.IsNotFound(err) {
			return ctrl.Result{}, err
		}
	}
	object.Finalizers = removeString(object.Finalizers, resourceSetFinalizer)
	if err := r.Update(ctx, object); err != nil && !apierrors.IsNotFound(err) {
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

func (r *ResourceSetReconciler) SetupWithManager(manager ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(manager).For(&platformv1alpha1.ResourceSet{}).Complete(r)
}

func bootstrapObjects(resources []platformv1alpha1.RuntimeObject) ([]runtimer.BootstrapObject, error) {
	objects := make([]runtimer.BootstrapObject, 0, len(resources))
	for index, spec := range resources {
		if spec.Version == "" || spec.Resource == "" || len(spec.Object.Raw) == 0 {
			return nil, fmt.Errorf("runtime object %d requires version, resource, and object", index)
		}
		var raw map[string]interface{}
		if err := json.Unmarshal(spec.Object.Raw, &raw); err != nil {
			return nil, fmt.Errorf("runtime object %d is invalid JSON: %w", index, err)
		}
		object := &unstructured.Unstructured{Object: raw}
		if err := runtimer.ValidateRuntimeObject(object); err != nil {
			return nil, fmt.Errorf("runtime object %d rejected: %w", index, err)
		}
		objects = append(objects, runtimer.BootstrapObject{
			GVR:    schema.GroupVersionResource{Group: spec.Group, Version: spec.Version, Resource: spec.Resource},
			Object: object,
			Wave:   int(spec.Wave),
		})
	}
	return objects, nil
}

func targetIdentity(reference platformv1alpha1.TargetReference) runtimer.TargetIdentity {
	return runtimer.TargetIdentity{Provider: reference.Provider, Account: reference.Account, Region: reference.Region, ClusterName: reference.ClusterName, ClusterID: reference.ClusterID}
}

func fieldManager(object *platformv1alpha1.ResourceSet) string {
	if object.UID != "" {
		return "pcp-rs-" + string(object.UID)
	}
	return "pcp-rs-" + object.Name
}

func inventoryStatus(inventory []runtimer.InventoryItem) []platformv1alpha1.InventoryItemStatus {
	status := make([]platformv1alpha1.InventoryItemStatus, 0, len(inventory))
	for _, item := range inventory {
		status = append(status, platformv1alpha1.InventoryItemStatus{TargetIdentityDigest: item.TargetIdentityDigest, Group: item.Group, Version: item.Version, Resource: item.Resource, Kind: item.Kind, Namespace: item.Namespace, Name: item.Name, UID: item.UID})
	}
	return status
}

func inventoryFromStatus(item platformv1alpha1.InventoryItemStatus) runtimer.InventoryItem {
	return runtimer.InventoryItem{TargetIdentityDigest: item.TargetIdentityDigest, Group: item.Group, Version: item.Version, Resource: item.Resource, Kind: item.Kind, Namespace: item.Namespace, Name: item.Name, UID: item.UID}
}

func inventoryKey(group, version, resource, namespace, name string) string {
	return group + "/" + version + "/" + resource + "/" + namespace + "/" + name
}

func validateInventorySize(inventory []runtimer.InventoryItem) error {
	encoded, err := json.Marshal(inventoryStatus(inventory))
	if err != nil {
		return err
	}
	if len(encoded) > maxInventoryStatusBytes {
		return fmt.Errorf("serialized inventory is %d bytes, limit is %d", len(encoded), maxInventoryStatusBytes)
	}
	return nil
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func removeString(values []string, want string) []string {
	filtered := values[:0]
	for _, value := range values {
		if value != want {
			filtered = append(filtered, value)
		}
	}
	return filtered
}

func stableCondition(previous *metav1.Condition, generation int64, status metav1.ConditionStatus, reason, message string) metav1.Condition {
	if previous != nil && previous.Status == status && previous.ObservedGeneration == generation && previous.Reason == reason && previous.Message == message {
		return *previous
	}
	return metav1.Condition{Type: ConditionReady, Status: status, ObservedGeneration: generation, Reason: reason, Message: message, LastTransitionTime: metav1.Now()}
}
