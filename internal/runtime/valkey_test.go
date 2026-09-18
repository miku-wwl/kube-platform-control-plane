package runtime

import "testing"

func TestBuildValkeyObjectsUsesCRDServiceAndStatefulSetWaves(t *testing.T) {
	objects, err := BuildValkeyObjects(ValkeySpec{Name: "cache", Namespace: "runtime", Replicas: 1, Image: "valkey/valkey:8"})
	if err != nil {
		t.Fatalf("BuildValkeyObjects() error = %v", err)
	}
	if len(objects) != 3 || objects[0].Object.GetKind() != "ValkeyCluster" || objects[1].Object.GetKind() != "Service" || objects[2].Object.GetKind() != "StatefulSet" {
		t.Fatalf("objects = %#v", objects)
	}
}
