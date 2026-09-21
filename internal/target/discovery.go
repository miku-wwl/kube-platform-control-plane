package target

import (
	"context"
	"encoding/json"
	"fmt"
)

type TargetVerifier interface {
	Verify(context.Context, RuntimeTargetIdentity, TargetConnectionProfile) error
}

type TargetVerifierFunc func(context.Context, RuntimeTargetIdentity, TargetConnectionProfile) error

func (f TargetVerifierFunc) Verify(ctx context.Context, identity RuntimeTargetIdentity, profile TargetConnectionProfile) error {
	return f(ctx, identity, profile)
}

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
func DiscoverFromTerraformOutput(output []byte, input DiscoveryInput, verifier TargetVerifier) (DiscoveryResult, error) {
	return DiscoverFromTerraformOutputContext(context.Background(), output, input, verifier)
}

func DiscoverFromTerraformOutputContext(ctx context.Context, output []byte, input DiscoveryInput, verifier TargetVerifier) (DiscoveryResult, error) {
	if input.SourceClosureDigest == "" || input.BackendSnapshotDigest == "" || input.EffectivePlanInputDigest == "" {
		return DiscoveryResult{}, fmt.Errorf("discovery requires convergence evidence digests")
	}
	var values struct {
		Provider                 string `json:"provider"`
		AccountID                string `json:"accountId"`
		Region                   string `json:"region"`
		ClusterARN               string `json:"clusterArn"`
		ClusterName              string `json:"clusterName"`
		IncarnationID            string `json:"incarnationId"`
		Endpoint                 string `json:"endpoint"`
		CADataDigest             string `json:"caCertificateDigest"`
		CAData                   string `json:"caCertificateData"`
		AuthMode                 string `json:"authMode"`
		KubeContext              string `json:"kubeContext"`
		SourceClosureDigest      string `json:"sourceClosureDigest"`
		BackendSnapshotDigest    string `json:"backendSnapshotDigest"`
		EffectivePlanInputDigest string `json:"effectivePlanInputDigest"`
	}
	if err := json.Unmarshal(output, &values); err != nil {
		return DiscoveryResult{}, fmt.Errorf("decode target-discovery output: %w", err)
	}
	if values.SourceClosureDigest != input.SourceClosureDigest || values.BackendSnapshotDigest != input.BackendSnapshotDigest || values.EffectivePlanInputDigest != input.EffectivePlanInputDigest {
		return DiscoveryResult{}, fmt.Errorf("target discovery evidence does not match the current convergence")
	}
	identity := input.ExpectedTarget
	profile := input.ExpectedConnection
	if values.Provider != "" {
		identity.Provider = values.Provider
	}
	if values.AccountID != "" {
		identity.AccountID = values.AccountID
	}
	// Kind's runtime identity uses the provider-local "local" region while
	// Terraform may legitimately use an AWS-compatible emulation region such
	// as us-east-1 for LocalStack. AWS discovery remains strictly bound to the
	// discovered region below.
	if values.Region != "" && identity.Provider != ProviderKind {
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
	if identity.Provider == ProviderAWS && identity.IncarnationID == "" {
		identity.IncarnationID = identity.ClusterARN
	}
	if values.Endpoint != "" {
		profile.Endpoint = values.Endpoint
	}
	if values.CAData != "" {
		profile.CACertificateData = values.CAData
		if values.CADataDigest == "" {
			profile.CACertificateDigest = certificateDigest(values.CAData)
		}
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
	if err := identity.Validate(); err != nil {
		return DiscoveryResult{}, fmt.Errorf("discovered target identity is invalid: %w", err)
	}
	if err := verifier.Verify(ctx, identity, profile); err != nil {
		return DiscoveryResult{}, fmt.Errorf("target discovery verification failed: %w", err)
	}
	return DiscoveryResult{RuntimeTargetIdentity: identity, TargetConnection: profile, SourceClosureDigest: input.SourceClosureDigest, BackendSnapshotDigest: input.BackendSnapshotDigest, EffectivePlanInputDigest: input.EffectivePlanInputDigest}, nil
}

func VerifyLocalKindTarget(expectedContext string) TargetVerifierFunc {
	return func(_ context.Context, identity RuntimeTargetIdentity, profile TargetConnectionProfile) error {
		if identity.Provider != ProviderKind || profile.AuthMode != AuthKindContext || profile.KubeContext != expectedContext {
			return fmt.Errorf("local target mismatch: provider=%q auth=%q context=%q", identity.Provider, profile.AuthMode, profile.KubeContext)
		}
		if identity.ClusterName != expectedContext && identity.ClusterName != "" {
			return fmt.Errorf("local target cluster mismatch: got %q want %q", identity.ClusterName, expectedContext)
		}
		return ValidateBinding(identity, profile)
	}
}
