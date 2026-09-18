package runtime

import (
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func TestValidateRuntimeObjectRejectsInlineSecret(t *testing.T) {
	secret := &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "v1",
		"kind":       "Secret",
		"metadata":   map[string]interface{}{"name": "runtime-secret"},
		"data":       map[string]interface{}{"password": "forbidden"},
	}}
	if err := ValidateRuntimeObject(secret); err == nil {
		t.Fatal("ValidateRuntimeObject accepted inline Secret data")
	}
}

func TestValidatePruneRequiresTargetIdentityOwnershipAndUID(t *testing.T) {
	target := TargetIdentity{Provider: "kind", ClusterName: "target-a"}
	digest, err := target.Digest()
	if err != nil {
		t.Fatal(err)
	}
	object := &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "apps/v1",
		"kind":       "Deployment",
		"metadata": map[string]interface{}{
			"name":      "runtime",
			"namespace": "default",
			"uid":       "uid-1",
			"labels":    map[string]interface{}{managedByLabel: "platform-control-plane"},
		},
	}}
	item := InventoryItem{TargetIdentityDigest: digest, Kind: "Deployment", Namespace: "default", Name: "runtime", UID: "uid-1"}
	if err := ValidatePrune(item, target, object); err != nil {
		t.Fatalf("ValidatePrune() error = %v", err)
	}
	item.TargetIdentityDigest = "sha256:wrong"
	if err := ValidatePrune(item, target, object); err == nil {
		t.Fatal("ValidatePrune accepted target mismatch")
	}
}

func TestReadyDeploymentAndStatefulSet(t *testing.T) {
	deployment := &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "apps/v1", "kind": "Deployment",
		"metadata": map[string]interface{}{"name": "runtime", "generation": int64(2)},
		"status":   map[string]interface{}{"observedGeneration": int64(2), "conditions": []interface{}{map[string]interface{}{"type": "Available", "status": "True"}}},
	}}
	ready, _, err := Ready(deployment)
	if err != nil || !ready {
		t.Fatalf("deployment readiness = %v, %v", ready, err)
	}
	statefulSet := &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "apps/v1", "kind": "StatefulSet",
		"metadata": map[string]interface{}{"name": "valkey", "generation": int64(1)},
		"spec":     map[string]interface{}{"replicas": int64(1)},
		"status":   map[string]interface{}{"observedGeneration": int64(1), "readyReplicas": int64(1), "currentRevision": "rev-1", "updateRevision": "rev-1"},
	}}
	ready, _, err = Ready(statefulSet)
	if err != nil || !ready {
		t.Fatalf("statefulset readiness = %v, %v", ready, err)
	}
}

func TestInventoryUsesGVKAndTargetIdentity(t *testing.T) {
	object := &unstructured.Unstructured{Object: map[string]interface{}{"apiVersion": "apps/v1", "kind": "Deployment", "metadata": map[string]interface{}{"name": "runtime", "uid": "uid-1"}}}
	object.SetGroupVersionKind(schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "Deployment"})
	item, err := InventoryFor(object, TargetIdentity{Provider: "kind", ClusterName: "target-a"})
	if err != nil || item.Kind != "Deployment" || item.UID != "uid-1" {
		t.Fatalf("inventory = %+v err=%v", item, err)
	}
}
