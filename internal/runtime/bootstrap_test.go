package runtime

import (
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func TestBootstrapObjectsAreOrderedByWave(t *testing.T) {
	objects := []BootstrapObject{
		{Wave: 2, GVR: schema.GroupVersionResource{Version: "v1", Resource: "configmaps"}, Object: &unstructured.Unstructured{Object: map[string]interface{}{"kind": "ConfigMap", "metadata": map[string]interface{}{"name": "late"}}}},
		{Wave: 1, GVR: schema.GroupVersionResource{Version: "v1", Resource: "serviceaccounts"}, Object: &unstructured.Unstructured{Object: map[string]interface{}{"kind": "ServiceAccount", "metadata": map[string]interface{}{"name": "early"}}}},
	}
	ordered := orderBootstrapObjects(objects)
	if ordered[0].Object.GetName() != "early" || ordered[1].Object.GetName() != "late" {
		t.Fatalf("bootstrap order = %s, %s", ordered[0].Object.GetName(), ordered[1].Object.GetName())
	}
}
