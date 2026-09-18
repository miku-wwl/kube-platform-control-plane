package runtime

import (
	"fmt"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func Ready(object *unstructured.Unstructured) (bool, string, error) {
	if object == nil {
		return false, "object missing", fmt.Errorf("object is required")
	}
	generation := object.GetGeneration()
	switch object.GetKind() {
	case "Namespace":
		phase, _, err := unstructured.NestedString(object.Object, "status", "phase")
		if err != nil {
			return false, "invalid namespace phase", err
		}
		if object.GetDeletionTimestamp() != nil {
			return false, "namespace is being deleted", nil
		}
		if phase != "Active" {
			return false, "namespace is not Active", nil
		}
		return true, "Namespace Active", nil
	case "Service":
		if object.GetDeletionTimestamp() != nil {
			return false, "service is being deleted", nil
		}
		return true, "Service exists", nil
	case "Deployment":
		observed, _, _ := unstructured.NestedInt64(object.Object, "status", "observedGeneration")
		if observed < generation {
			return false, "observedGeneration is behind", nil
		}
		return conditionTrue(object, "Available")
	case "StatefulSet":
		observed, _, _ := unstructured.NestedInt64(object.Object, "status", "observedGeneration")
		readyReplicas, _, _ := unstructured.NestedInt64(object.Object, "status", "readyReplicas")
		replicas, _, _ := unstructured.NestedInt64(object.Object, "spec", "replicas")
		currentRevision, _, _ := unstructured.NestedString(object.Object, "status", "currentRevision")
		updateRevision, _, _ := unstructured.NestedString(object.Object, "status", "updateRevision")
		if observed < generation || readyReplicas < replicas || currentRevision == "" || currentRevision != updateRevision {
			return false, "StatefulSet replicas or revision is not ready", nil
		}
		return true, "StatefulSet ready", nil
	case "Job":
		return conditionTrue(object, "Complete")
	case "CustomResourceDefinition":
		return conditionTrue(object, "Established")
	case "ValkeyCluster":
		if ready, reason, err := conditionTrue(object, "Available"); ready || err != nil {
			return ready, reason, err
		}
		if degraded, reason, err := conditionTrue(object, "Degraded"); err != nil {
			return false, reason, err
		} else if degraded {
			return false, "ValkeyCluster is Degraded", nil
		}
		return false, "Available condition not reported", nil
	default:
		return false, "readiness adapter unavailable", nil
	}
}

func conditionTrue(object *unstructured.Unstructured, conditionType string) (bool, string, error) {
	conditions, found, err := unstructured.NestedSlice(object.Object, "status", "conditions")
	if err != nil {
		return false, "invalid conditions", err
	}
	if !found {
		return false, "condition not reported", nil
	}
	for _, raw := range conditions {
		condition, ok := raw.(map[string]interface{})
		if !ok || condition["type"] != conditionType {
			continue
		}
		if condition["status"] == "True" {
			return true, conditionType + "=True", nil
		}
		return false, conditionType + " is not True", nil
	}
	return false, conditionType + " condition not found", nil
}
