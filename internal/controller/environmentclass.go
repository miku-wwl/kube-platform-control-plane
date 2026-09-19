package controller

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	platformv1alpha1 "github.com/miku-wwl/kube-platform-control-plane/api/v1alpha1"
)

func environmentClassSpecDigest(class *platformv1alpha1.EnvironmentClass) string {
	if class == nil {
		return ""
	}
	encoded, err := json.Marshal(class.Spec)
	if err != nil {
		return ""
	}
	digest := sha256.Sum256(encoded)
	return "sha256:" + hex.EncodeToString(digest[:])
}

func validateEnvironmentClass(environment *platformv1alpha1.PlatformEnvironment, class *platformv1alpha1.EnvironmentClass) error {
	if environment == nil || class == nil {
		return fmt.Errorf("environment and EnvironmentClass are required")
	}
	if class.Status.SpecDigest != "" && class.Status.SpecDigest != environmentClassSpecDigest(class) {
		return fmt.Errorf("EnvironmentClass %q spec digest changed; create a new versioned class", class.Name)
	}
	if environment.Spec.InfraStackRef.Name != "" || environment.Spec.Target.Provider != "" || environment.Spec.Target.ClusterName != "" {
		return fmt.Errorf("Class Mode cannot set legacy infraStackRef or target")
	}
	region := environment.Spec.Region
	if region == "" {
		region = class.Spec.Target.Region
	}
	if len(class.Spec.AllowedRegions) > 0 && !containsString(class.Spec.AllowedRegions, region) {
		return fmt.Errorf("region %q is not allowed by EnvironmentClass %q", region, class.Name)
	}
	if bounds := class.Spec.CapacityBounds; bounds.MinNodeCount > 0 && environment.Spec.Capacity.NodeCount < bounds.MinNodeCount {
		return fmt.Errorf("nodeCount %d is below EnvironmentClass minimum %d", environment.Spec.Capacity.NodeCount, bounds.MinNodeCount)
	} else if bounds.MaxNodeCount > 0 && environment.Spec.Capacity.NodeCount > bounds.MaxNodeCount {
		return fmt.Errorf("nodeCount %d exceeds EnvironmentClass maximum %d", environment.Spec.Capacity.NodeCount, bounds.MaxNodeCount)
	}
	return nil
}

func classStackName(environmentName string) string {
	return boundedResourceName(environmentName + "-infra")
}

func boundedResourceName(logical string) string {
	logical = strings.Trim(strings.ToLower(logical), "-")
	if len(logical) <= 63 {
		return logical
	}
	hash := sha256.Sum256([]byte(logical))
	suffix := "-" + hex.EncodeToString(hash[:])[:10]
	prefix := strings.TrimRight(logical[:63-len(suffix)], "-")
	return prefix + suffix
}

func classTarget(environment *platformv1alpha1.PlatformEnvironment, class *platformv1alpha1.EnvironmentClass) platformv1alpha1.TargetReference {
	target := class.Spec.Target
	if environment.Spec.Region != "" {
		target.Region = environment.Spec.Region
	}
	if target.ClusterID == "" {
		target.ClusterID = target.ClusterName
	}
	if target.IncarnationID == "" {
		target.IncarnationID = target.ClusterID
	}
	return target
}

func buildClassInfraStack(environment *platformv1alpha1.PlatformEnvironment, class *platformv1alpha1.EnvironmentClass, name string, target platformv1alpha1.TargetReference) *platformv1alpha1.InfraStack {
	approval := class.Spec.ApprovalPolicy
	if approval == "" {
		approval = platformv1alpha1.ApprovalPolicyManual
	}
	runner := class.Spec.Executor
	if runner.Image == "" {
		runner.Image = class.Spec.RunnerProfile.Image
	}
	if runner.TerraformVersion == "" {
		runner.TerraformVersion = class.Spec.RunnerProfile.TerraformVersion
	}
	identity := platformv1alpha1.InfrastructureExecutionIdentity{
		Provider:  target.Provider,
		Mode:      "local",
		AccountID: target.Account,
		Region:    target.Region,
	}
	if identity.AccountID == "" {
		identity.AccountID = "local"
	}
	runtimeIdentity := platformv1alpha1.RuntimeTargetIdentity{
		Provider: target.Provider, AccountID: identity.AccountID, Region: target.Region,
		ClusterARN: target.ClusterARN, ClusterName: target.ClusterName, IncarnationID: target.IncarnationID,
	}
	return &platformv1alpha1.InfraStack{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: environment.Namespace},
		Spec: platformv1alpha1.InfraStackSpec{
			Source: class.Spec.Source, Backend: class.Spec.Backend, Workspace: environment.Name, Capacity: environment.Spec.Capacity,
			Executor: runner, DesiredState: platformv1alpha1.DesiredStatePresent, ApprovalPolicy: approval,
			OwnerEnvironmentUID: string(environment.UID), InfrastructureExecutionIdentity: identity,
			RunnerServiceAccountName: class.Spec.RunnerProfile.ServiceAccountName,
			RuntimeTargetIdentity:    runtimeIdentity,
			TargetConnectionProfile:  platformv1alpha1.TargetConnectionProfile{Endpoint: target.ConnectionProfileRef, AuthMode: "kind-context", KubeContext: target.ClusterName, NetworkRouteProfile: "local-kind"},
			ConcurrencyGroup:         "environmentclass:" + class.Name,
			MaxConcurrentPlans:       class.Spec.CapacityBounds.MaxConcurrentPlans,
			MaxConcurrentApplies:     class.Spec.CapacityBounds.MaxConcurrentApplies,
		},
	}
}

func classRuntimeObjects(class *platformv1alpha1.EnvironmentClass) []platformv1alpha1.RuntimeObject {
	if class == nil || class.Spec.RuntimeProfile.RuntimeObjects == nil {
		return nil
	}
	return append([]platformv1alpha1.RuntimeObject(nil), class.Spec.RuntimeProfile.RuntimeObjects...)
}

func targetRuntimeIdentity(target platformv1alpha1.TargetReference) platformv1alpha1.RuntimeTargetIdentity {
	incarnation := target.IncarnationID
	if incarnation == "" {
		incarnation = target.ClusterID
	}
	if incarnation == "" {
		incarnation = target.ClusterName
	}
	account := target.Account
	if account == "" {
		account = "local"
	}
	return platformv1alpha1.RuntimeTargetIdentity{Provider: target.Provider, AccountID: account, Region: target.Region, ClusterARN: target.ClusterARN, ClusterName: target.ClusterName, IncarnationID: incarnation}
}

func environmentTargetConnection(target platformv1alpha1.TargetReference) platformv1alpha1.TargetConnectionProfile {
	authMode := "kind-context"
	if target.Provider == "aws" {
		authMode = "aws-eks"
	}
	return platformv1alpha1.TargetConnectionProfile{Endpoint: target.ConnectionProfileRef, AuthMode: authMode, KubeContext: target.ClusterName, NetworkRouteProfile: "local-kind"}
}

func ownerUID(environment *platformv1alpha1.PlatformEnvironment) types.UID {
	if environment == nil {
		return ""
	}
	return environment.UID
}
