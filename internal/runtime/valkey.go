package runtime

import (
	"fmt"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

var (
	ValkeyGVR      = schema.GroupVersionResource{Group: "platform.example.io", Version: "v1alpha1", Resource: "valkeyclusters"}
	StatefulSetGVR = schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "statefulsets"}
	ServiceGVR     = schema.GroupVersionResource{Version: "v1", Resource: "services"}
)

type ValkeySpec struct {
	Name      string
	Namespace string
	Replicas  int64
	Image     string
}

func BuildValkeyObjects(spec ValkeySpec) ([]BootstrapObject, error) {
	if spec.Name == "" || spec.Namespace == "" || spec.Replicas < 1 || spec.Image == "" {
		return nil, fmt.Errorf("Valkey name, namespace, positive replicas, and image are required")
	}
	labels := map[string]interface{}{"app.kubernetes.io/name": spec.Name, managedByLabel: "platform-control-plane"}
	selector := map[string]interface{}{"matchLabels": map[string]interface{}{"app.kubernetes.io/name": spec.Name}}
	cluster := &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "platform.example.io/v1alpha1",
		"kind":       "ValkeyCluster",
		"metadata":   map[string]interface{}{"name": spec.Name, "namespace": spec.Namespace, "labels": labels},
		"spec":       map[string]interface{}{"replicas": spec.Replicas, "image": spec.Image},
	}}
	cluster.SetGroupVersionKind(schema.GroupVersionKind{Group: "platform.example.io", Version: "v1alpha1", Kind: "ValkeyCluster"})
	service := &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "v1",
		"kind":       "Service",
		"metadata":   map[string]interface{}{"name": spec.Name, "namespace": spec.Namespace, "labels": labels},
		"spec": map[string]interface{}{
			"clusterIP": "None",
			"selector":  map[string]interface{}{"app.kubernetes.io/name": spec.Name},
			"ports":     []interface{}{map[string]interface{}{"name": "valkey", "port": int64(6379), "targetPort": int64(6379)}},
		},
	}}
	service.SetGroupVersionKind(schema.GroupVersionKind{Version: "v1", Kind: "Service"})
	statefulSet := &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "apps/v1",
		"kind":       "StatefulSet",
		"metadata":   map[string]interface{}{"name": spec.Name, "namespace": spec.Namespace, "labels": labels},
		"spec": map[string]interface{}{
			"serviceName": spec.Name,
			"replicas":    spec.Replicas,
			"selector":    selector,
			"template": map[string]interface{}{
				"metadata": map[string]interface{}{"labels": map[string]interface{}{"app.kubernetes.io/name": spec.Name}},
				"spec": map[string]interface{}{"containers": []interface{}{map[string]interface{}{
					"name":  "valkey",
					"image": spec.Image,
					"ports": []interface{}{map[string]interface{}{"name": "valkey", "containerPort": int64(6379)}},
				}}},
			},
		},
	}}
	statefulSet.SetGroupVersionKind(schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "StatefulSet"})
	return []BootstrapObject{
		{GVR: ValkeyGVR, Object: cluster, Wave: 0},
		{GVR: ServiceGVR, Object: service, Wave: 1},
		{GVR: StatefulSetGVR, Object: statefulSet, Wave: 2},
	}, nil
}

func ValkeyReadyFromStatefulSet(statefulSet *unstructured.Unstructured) (bool, string, error) {
	return Ready(statefulSet)
}
