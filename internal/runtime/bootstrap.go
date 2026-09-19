package runtime

import (
	"context"
	"fmt"
	"sort"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
)

type BootstrapObject struct {
	GVR             schema.GroupVersionResource
	Object          *unstructured.Unstructured
	Wave            int
	ReadinessPolicy string
	DeletionPolicy  string
	OwnershipID     string
}

func ApplyBootstrap(ctx context.Context, client dynamic.Interface, objects []BootstrapObject, fieldManager string, target TargetIdentity) ([]InventoryItem, error) {
	if len(objects) == 0 {
		return nil, fmt.Errorf("bootstrap requires at least one object")
	}
	ordered := orderBootstrapObjects(objects)
	inventory := make([]InventoryItem, 0, len(ordered))
	for start := 0; start < len(ordered); {
		end := start + 1
		for end < len(ordered) && ordered[end].Wave == ordered[start].Wave {
			end++
		}
		for _, item := range ordered[start:end] {
			labels := item.Object.GetLabels()
			if labels == nil {
				labels = map[string]string{}
			}
			labels[ownershipIDLabel] = item.OwnershipID
			item.Object.SetLabels(labels)
			applied, err := Apply(ctx, client, item.GVR, item.Object, fieldManager)
			if err != nil {
				return nil, fmt.Errorf("apply %s/%s: %w", item.Object.GetKind(), item.Object.GetName(), err)
			}
			entry, err := InventoryFor(applied, target)
			if err != nil {
				return nil, err
			}
			entry.Resource = item.GVR.Resource
			entry.OwnershipID = item.OwnershipID
			entry.ReadinessPolicy = item.ReadinessPolicy
			entry.DeletionPolicy = item.DeletionPolicy
			inventory = append(inventory, entry)
		}
		for _, item := range ordered[start:end] {
			if item.ReadinessPolicy != "RequireReady" {
				continue
			}
			var resource dynamic.ResourceInterface
			if item.Object.GetNamespace() != "" {
				resource = client.Resource(item.GVR).Namespace(item.Object.GetNamespace())
			} else {
				resource = client.Resource(item.GVR)
			}
			observed, err := resource.Get(ctx, item.Object.GetName(), metav1.GetOptions{})
			if err != nil {
				return nil, fmt.Errorf("readiness read %s/%s: %w", item.Object.GetKind(), item.Object.GetName(), err)
			}
			ready, reason, err := Ready(observed)
			if err != nil {
				return nil, err
			}
			if !ready {
				return nil, fmt.Errorf("wave %d object %s/%s is not ready: %s", item.Wave, item.Object.GetKind(), item.Object.GetName(), reason)
			}
		}
		start = end
	}
	return inventory, nil
}

func orderBootstrapObjects(objects []BootstrapObject) []BootstrapObject {
	ordered := append([]BootstrapObject(nil), objects...)
	sort.SliceStable(ordered, func(i, j int) bool { return ordered[i].Wave < ordered[j].Wave })
	return ordered
}
