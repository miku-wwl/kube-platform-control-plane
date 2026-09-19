package runtime

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
)

const managedByLabel = "platform.example.io/managed-by"
const ownershipIDLabel = "platform.example.io/ownership-id"

type TargetIdentity struct {
	Provider      string `json:"provider"`
	Account       string `json:"account,omitempty"`
	Region        string `json:"region,omitempty"`
	ClusterName   string `json:"clusterName"`
	ClusterID     string `json:"clusterID,omitempty"`
	ClusterARN    string `json:"clusterARN,omitempty"`
	IncarnationID string `json:"incarnationID,omitempty"`
}

type InventoryItem struct {
	TargetIdentityDigest string `json:"targetIdentityDigest"`
	Group                string `json:"group"`
	Version              string `json:"version"`
	Resource             string `json:"resource"`
	Kind                 string `json:"kind"`
	Namespace            string `json:"namespace,omitempty"`
	Name                 string `json:"name"`
	UID                  string `json:"uid"`
}

func (i TargetIdentity) Digest() (string, error) {
	encoded, err := json.Marshal(i)
	if err != nil {
		return "", err
	}
	hash := sha256.Sum256(encoded)
	return "sha256:" + hex.EncodeToString(hash[:]), nil
}

func Apply(ctx context.Context, client dynamic.Interface, gvr schema.GroupVersionResource, object *unstructured.Unstructured, fieldManager string) (*unstructured.Unstructured, error) {
	if client == nil {
		return nil, fmt.Errorf("target dynamic client is required")
	}
	if err := ValidateRuntimeObject(object); err != nil {
		return nil, err
	}
	if fieldManager == "" {
		return nil, fmt.Errorf("field manager is required")
	}
	labels := object.GetLabels()
	if labels == nil {
		labels = map[string]string{}
	}
	labels[managedByLabel] = "platform-control-plane"
	if ownershipID := labels[ownershipIDLabel]; ownershipID == "" {
		labels[ownershipIDLabel] = fieldManager
	}
	object.SetLabels(labels)
	encoded, err := json.Marshal(object.Object)
	if err != nil {
		return nil, err
	}
	force := false
	var resource dynamic.ResourceInterface
	if object.GetNamespace() != "" {
		resource = client.Resource(gvr).Namespace(object.GetNamespace())
	} else {
		resource = client.Resource(gvr)
	}
	return resource.Patch(ctx, object.GetName(), types.ApplyPatchType, encoded, metav1.PatchOptions{FieldManager: fieldManager, Force: &force})
}

func ValidateRuntimeObject(object *unstructured.Unstructured) error {
	if object == nil || object.GetName() == "" || object.GroupVersionKind().Kind == "" {
		return fmt.Errorf("runtime object kind and name are required")
	}
	if object.GetKind() == "Secret" {
		if _, found, _ := unstructured.NestedFieldNoCopy(object.Object, "data"); found {
			return fmt.Errorf("inline Secret data is forbidden")
		}
		if _, found, _ := unstructured.NestedFieldNoCopy(object.Object, "stringData"); found {
			return fmt.Errorf("inline Secret stringData is forbidden")
		}
	}
	return nil
}

func InventoryFor(object *unstructured.Unstructured, target TargetIdentity) (InventoryItem, error) {
	if object == nil || object.GetName() == "" || object.GetUID() == "" {
		return InventoryItem{}, fmt.Errorf("inventory requires object name and UID")
	}
	targetDigest, err := target.Digest()
	if err != nil {
		return InventoryItem{}, err
	}
	gvk := object.GroupVersionKind()
	return InventoryItem{TargetIdentityDigest: targetDigest, Group: gvk.Group, Version: gvk.Version, Kind: gvk.Kind, Namespace: object.GetNamespace(), Name: object.GetName(), UID: string(object.GetUID())}, nil
}

func ValidatePrune(item InventoryItem, target TargetIdentity, object *unstructured.Unstructured) error {
	targetDigest, err := target.Digest()
	if err != nil {
		return err
	}
	if item.TargetIdentityDigest != targetDigest {
		return fmt.Errorf("prune target identity mismatch")
	}
	if object == nil || string(object.GetUID()) != item.UID {
		return fmt.Errorf("prune UID mismatch")
	}
	if object.GetLabels()[managedByLabel] != "platform-control-plane" {
		return fmt.Errorf("prune ownership label missing")
	}
	if IsProtected(item) {
		return fmt.Errorf("protected resource cannot be pruned")
	}
	return nil
}

func IsProtected(item InventoryItem) bool {
	if item.Kind == "Namespace" || item.Kind == "PersistentVolumeClaim" || item.Kind == "CustomResourceDefinition" {
		return true
	}
	return strings.EqualFold(item.Namespace, "kube-system")
}
