package target

import (
	"encoding/json"
	"fmt"
)

type DiscoveryInput struct {
	SourceClosureDigest      string
	BackendSnapshotDigest    string
	EffectivePlanInputDigest string
	ExpectedTarget           RuntimeTargetIdentity
	ExpectedConnection       TargetConnectionProfile
}

type DiscoveryResult struct {
	RuntimeTargetIdentity    RuntimeTargetIdentity   `json:"runtimeTargetIdentity"`
	TargetConnection         TargetConnectionProfile `json:"targetConnectionProfile"`
	SourceClosureDigest      string                  `json:"sourceClosureDigest"`
	BackendSnapshotDigest    string                  `json:"backendSnapshotDigest"`
	EffectivePlanInputDigest string                  `json:"effectivePlanInputDigest"`
}

// DiscoverFromTerraformOutput consumes only an allowlisted, non-sensitive
// output object. The verifier represents trusted local Kind discovery or the
// future AWS DescribeCluster boundary.
func DiscoverFromTerraformOutput(output []byte, input DiscoveryInput, verifier func(RuntimeTargetIdentity, TargetConnectionProfile) error) (DiscoveryResult, error) {
	if input.SourceClosureDigest == "" || input.BackendSnapshotDigest == "" || input.EffectivePlanInputDigest == "" {
		return DiscoveryResult{}, fmt.Errorf("discovery requires convergence evidence digests")
	}
	var values struct {
		Provider      string `json:"provider"`
		AccountID     string `json:"accountId"`
		Region        string `json:"region"`
		ClusterARN    string `json:"clusterArn"`
		ClusterName   string `json:"clusterName"`
		IncarnationID string `json:"incarnationId"`
		Endpoint      string `json:"endpoint"`
		CADataDigest  string `json:"caCertificateDigest"`
		AuthMode      string `json:"authMode"`
		KubeContext   string `json:"kubeContext"`
	}
	if err := json.Unmarshal(output, &values); err != nil {
		return DiscoveryResult{}, fmt.Errorf("decode target-discovery output: %w", err)
	}
	identity := input.ExpectedTarget
	profile := input.ExpectedConnection
	if values.Provider != "" {
		identity.Provider = values.Provider
	}
	if values.AccountID != "" {
		identity.AccountID = values.AccountID
	}
	if values.Region != "" {
		identity.Region = values.Region
	}
	if values.ClusterARN != "" {
		identity.ClusterARN = values.ClusterARN
	}
	if values.ClusterName != "" {
		identity.ClusterName = values.ClusterName
	}
	if values.IncarnationID != "" {
		identity.IncarnationID = values.IncarnationID
	}
	if values.Endpoint != "" {
		profile.Endpoint = values.Endpoint
	}
	if values.CADataDigest != "" {
		profile.CACertificateDigest = values.CADataDigest
	}
	if values.AuthMode != "" {
		profile.AuthMode = values.AuthMode
	}
	if values.KubeContext != "" {
		profile.KubeContext = values.KubeContext
	}
	if verifier == nil {
		return DiscoveryResult{}, fmt.Errorf("trusted target verifier is required")
	}
	if err := verifier(identity, profile); err != nil {
		return DiscoveryResult{}, fmt.Errorf("target discovery verification failed: %w", err)
	}
	return DiscoveryResult{RuntimeTargetIdentity: identity, TargetConnection: profile, SourceClosureDigest: input.SourceClosureDigest, BackendSnapshotDigest: input.BackendSnapshotDigest, EffectivePlanInputDigest: input.EffectivePlanInputDigest}, nil
}

func VerifyLocalKindTarget(expectedContext string) func(RuntimeTargetIdentity, TargetConnectionProfile) error {
	return func(identity RuntimeTargetIdentity, profile TargetConnectionProfile) error {
		if identity.Provider != "kind" || profile.AuthMode != "kind-context" || profile.KubeContext != expectedContext {
			return fmt.Errorf("local target mismatch: provider=%q auth=%q context=%q", identity.Provider, profile.AuthMode, profile.KubeContext)
		}
		if identity.ClusterName != expectedContext && identity.ClusterName != "" {
			return fmt.Errorf("local target cluster mismatch: got %q want %q", identity.ClusterName, expectedContext)
		}
		return ValidateBinding(identity, profile)
	}
}
