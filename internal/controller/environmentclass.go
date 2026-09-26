package controller

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	platformv1alpha1 "github.com/miku-wwl/kube-platform-control-plane/api/v1alpha1"
	targetresolver "github.com/miku-wwl/kube-platform-control-plane/internal/target"
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
	if authName := class.Spec.Backend.AuthRef.ServiceAccountName; authName != "" && class.Spec.RunnerProfile.ServiceAccountName != "" && authName != class.Spec.RunnerProfile.ServiceAccountName {
		return fmt.Errorf("backend authRef service account %q must match runnerProfile service account %q", authName, class.Spec.RunnerProfile.ServiceAccountName)
	}
	if runnerServiceAccountName(class) == "" {
		return fmt.Errorf("a management ServiceAccount is required through backend.authRef or runnerProfile")
	}
	if bounds := class.Spec.CapacityBounds; bounds.MinNodeCount > 0 && environment.Spec.Capacity.NodeCount < bounds.MinNodeCount {
		return fmt.Errorf("nodeCount %d is below EnvironmentClass minimum %d", environment.Spec.Capacity.NodeCount, bounds.MinNodeCount)
	} else if bounds.MaxNodeCount > 0 && environment.Spec.Capacity.NodeCount > bounds.MaxNodeCount {
		return fmt.Errorf("nodeCount %d exceeds EnvironmentClass maximum %d", environment.Spec.Capacity.NodeCount, bounds.MaxNodeCount)
	}
	if _, err := materializeTarget(classTarget(environment, class)); err != nil {
		return fmt.Errorf("target materialization rejected: %w", err)
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

func runnerServiceAccountName(class *platformv1alpha1.EnvironmentClass) string {
	if class == nil {
		return ""
	}
	if name := class.Spec.Backend.AuthRef.ServiceAccountName; name != "" {
		return name
	}
	return class.Spec.RunnerProfile.ServiceAccountName
}

func runnerServiceAccountIdentityDigest(name, managementRoleARN string) string {
	return digestIdentity(struct {
		ServiceAccountName string `json:"serviceAccountName"`
		ManagementRoleARN  string `json:"managementRoleARN,omitempty"`
	}{ServiceAccountName: name, ManagementRoleARN: managementRoleARN})
}

func buildClassInfraStack(environment *platformv1alpha1.PlatformEnvironment, class *platformv1alpha1.EnvironmentClass, name string, target platformv1alpha1.TargetReference) (*platformv1alpha1.InfraStack, error) {
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
	materialized, err := materializeTarget(target)
	if err != nil {
		return nil, err
	}
	return &platformv1alpha1.InfraStack{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: environment.Namespace},
		Spec: platformv1alpha1.InfraStackSpec{
			Source: class.Spec.Source, Backend: class.Spec.Backend, Workspace: environment.Name, Capacity: environment.Spec.Capacity,
			Executor: runner, DesiredState: platformv1alpha1.DesiredStatePresent, ApprovalPolicy: approval,
			OwnerEnvironmentUID:                string(environment.UID),
			InfrastructureExecutionIdentity:    materialized.InfrastructureExecutionIdentity,
			RunnerServiceAccountName:           runnerServiceAccountName(class),
			RunnerManagementRoleARN:            class.Spec.RunnerProfile.ManagementRoleARN,
			RunnerServiceAccountIdentityDigest: runnerServiceAccountIdentityDigest(runnerServiceAccountName(class), class.Spec.RunnerProfile.ManagementRoleARN),
			RuntimeTargetIdentity:              materialized.RuntimeTargetIdentity,
			TargetConnectionProfile:            materialized.TargetConnectionProfile,
			ConcurrencyGroup:                   "environmentclass:" + class.Name,
			MaxConcurrentPlans:                 class.Spec.CapacityBounds.MaxConcurrentPlans,
			MaxConcurrentApplies:               class.Spec.CapacityBounds.MaxConcurrentApplies,
		},
	}, nil
}

func classRuntimeObjects(class *platformv1alpha1.EnvironmentClass) []platformv1alpha1.RuntimeObject {
	if class == nil || class.Spec.RuntimeProfile.RuntimeObjects == nil {
		return nil
	}
	return append([]platformv1alpha1.RuntimeObject(nil), class.Spec.RuntimeProfile.RuntimeObjects...)
}

func targetRuntimeIdentity(target platformv1alpha1.TargetReference) platformv1alpha1.RuntimeTargetIdentity {
	materialized, _ := materializeTarget(target)
	return materialized.RuntimeTargetIdentity
}

func environmentTargetConnection(target platformv1alpha1.TargetReference) platformv1alpha1.TargetConnectionProfile {
	materialized, _ := materializeTarget(target)
	return materialized.TargetConnectionProfile
}

func materializeTarget(target platformv1alpha1.TargetReference) (platformv1alpha1.InfraStackSpec, error) {
	materialized, err := targetresolver.MaterializeTarget(targetresolver.TargetExpectation{
		Provider:          target.Provider,
		AccountID:         target.Account,
		Region:            target.Region,
		ClusterARN:        target.ClusterARN,
		ClusterName:       target.ClusterName,
		ClusterID:         target.ClusterID,
		IncarnationID:     target.IncarnationID,
		ConnectionProfile: target.ConnectionProfileRef,
		ExecutionRoleARN:  target.ExecutionRoleARN,
		RuntimeRoleARN:    target.RuntimeRoleARN,
	})
	if err != nil {
		return platformv1alpha1.InfraStackSpec{}, err
	}
	return platformv1alpha1.InfraStackSpec{
		InfrastructureExecutionIdentity: platformv1alpha1.InfrastructureExecutionIdentity{
			Provider:       materialized.InfrastructureExecutionIdentity.Provider,
			Mode:           materialized.InfrastructureExecutionIdentity.Mode,
			AccountID:      materialized.InfrastructureExecutionIdentity.AccountID,
			Region:         materialized.InfrastructureExecutionIdentity.Region,
			RoleARN:        materialized.InfrastructureExecutionIdentity.RoleARN,
			SessionProfile: materialized.InfrastructureExecutionIdentity.SessionProfile,
		},
		RuntimeTargetIdentity: platformv1alpha1.RuntimeTargetIdentity{
			Provider:      materialized.RuntimeTargetIdentity.Provider,
			AccountID:     materialized.RuntimeTargetIdentity.AccountID,
			Region:        materialized.RuntimeTargetIdentity.Region,
			ClusterARN:    materialized.RuntimeTargetIdentity.ClusterARN,
			ClusterName:   materialized.RuntimeTargetIdentity.ClusterName,
			IncarnationID: materialized.RuntimeTargetIdentity.IncarnationID,
		},
		TargetConnectionProfile: platformv1alpha1.TargetConnectionProfile{
			Endpoint:            materialized.TargetConnectionProfile.Endpoint,
			CACertificateData:   materialized.TargetConnectionProfile.CACertificateData,
			CACertificateDigest: materialized.TargetConnectionProfile.CACertificateDigest,
			AuthMode:            materialized.TargetConnectionProfile.AuthMode,
			NetworkRouteProfile: materialized.TargetConnectionProfile.NetworkRouteProfile,
			KubeContext:         materialized.TargetConnectionProfile.KubeContext,
			RoleARN:             materialized.TargetConnectionProfile.RoleARN,
		},
	}, nil
}
