package runtime

import (
	"context"
	"fmt"
	"sort"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
)

type BootstrapObject struct {
	GVR    schema.GroupVersionResource
	Object *unstructured.Unstructured
	Wave   int
}

func ApplyBootstrap(ctx context.Context, client dynamic.Interface, objects []BootstrapObject, fieldManager string, target TargetIdentity) ([]InventoryItem, error) {
	if len(objects) == 0 {
		return nil, fmt.Errorf("bootstrap requires at least one object")
	}
	ordered := orderBootstrapObjects(objects)
	inventory := make([]InventoryItem, 0, len(ordered))
	for _, item := range ordered {
		applied, err := Apply(ctx, client, item.GVR, item.Object, fieldManager)
		if err != nil {
			return nil, fmt.Errorf("apply %s/%s: %w", item.Object.GetKind(), item.Object.GetName(), err)
		}
		entry, err := InventoryFor(applied, target)
		if err != nil {
			return nil, err
		}
		entry.Resource = item.GVR.Resource
		inventory = append(inventory, entry)
	}
	return inventory, nil
}

func orderBootstrapObjects(objects []BootstrapObject) []BootstrapObject {
	ordered := append([]BootstrapObject(nil), objects...)
	sort.SliceStable(ordered, func(i, j int) bool { return ordered[i].Wave < ordered[j].Wave })
	return ordered
}
