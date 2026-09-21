package target

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"
)

const (
	ProviderKind = "kind"
	ProviderAWS  = "aws"

	ModeLocal     = "local"
	ModeAWSNative = "aws-native"

	AuthKindContext = "kind-context"
	AuthAWSEKS      = "aws-eks"
)

// InfrastructureExecutionIdentity is the identity allowed to mutate the
// Terraform backend and infrastructure provider. It must never be used as a
// runtime Kubernetes credential.
type InfrastructureExecutionIdentity struct {
	Provider       string `json:"provider"`
	Mode           string `json:"mode"`
	AccountID      string `json:"accountId"`
	Region         string `json:"region"`
	RoleARN        string `json:"roleArn,omitempty"`
	SessionProfile string `json:"sessionProfile,omitempty"`
}

// RuntimeTargetIdentity is the immutable identity of one target incarnation.
// ClusterName alone is deliberately not sufficient for isolation.
type RuntimeTargetIdentity struct {
	Provider      string `json:"provider"`
	AccountID     string `json:"accountId"`
	Region        string `json:"region"`
	ClusterARN    string `json:"clusterArn,omitempty"`
	ClusterName   string `json:"clusterName,omitempty"`
	IncarnationID string `json:"incarnationId"`
}

type TargetConnectionProfile struct {
	Endpoint            string `json:"endpoint"`
	CACertificateData   string `json:"caCertificateData,omitempty"`
	CACertificateDigest string `json:"caCertificateDigest,omitempty"`
	AuthMode            string `json:"authMode"`
	NetworkRouteProfile string `json:"networkRouteProfile,omitempty"`
	KubeContext         string `json:"kubeContext,omitempty"`
	RoleARN             string `json:"roleArn,omitempty"`
}

// TargetExpectation is the provider-neutral desired target input used by the
// golden-path materializer. It deliberately contains no Kubernetes client or
// controller concerns.
type TargetExpectation struct {
	Provider          string
	AccountID         string
	Region            string
	ClusterARN        string
	ClusterName       string
	ClusterID         string
	IncarnationID     string
	ConnectionProfile string
	ExecutionRoleARN  string
	RuntimeRoleARN    string
}

type MaterializedTarget struct {
	InfrastructureExecutionIdentity InfrastructureExecutionIdentity
	RuntimeTargetIdentity           RuntimeTargetIdentity
	TargetConnectionProfile         TargetConnectionProfile
}

// MaterializeTarget is the single provider-aware normalization boundary for
// Class Mode. AWS must never inherit Kind defaults or a synthetic local
// account/region.
func MaterializeTarget(input TargetExpectation) (MaterializedTarget, error) {
	provider := strings.ToLower(strings.TrimSpace(input.Provider))
	if provider == "" {
		return MaterializedTarget{}, fmt.Errorf("target provider is required")
	}
	switch provider {
	case ProviderKind:
		if input.ClusterName == "" {
			return MaterializedTarget{}, fmt.Errorf("kind target cluster name is required")
		}
		account := input.AccountID
		if account == "" {
			account = "local"
		}
		region := input.Region
		if region == "" {
			region = "local"
		}
		incarnation := input.IncarnationID
		if incarnation == "" {
			incarnation = input.ClusterID
		}
		if incarnation == "" {
			incarnation = input.ClusterName
		}
		endpoint := input.ConnectionProfile
		if endpoint == "" {
			endpoint = "kubeconfig:" + input.ClusterName
		}
		materialized := MaterializedTarget{
			InfrastructureExecutionIdentity: InfrastructureExecutionIdentity{Provider: ProviderKind, Mode: ModeLocal, AccountID: account, Region: region},
			RuntimeTargetIdentity:           RuntimeTargetIdentity{Provider: ProviderKind, AccountID: account, Region: region, ClusterARN: input.ClusterARN, ClusterName: input.ClusterName, IncarnationID: incarnation},
			TargetConnectionProfile:         TargetConnectionProfile{Endpoint: endpoint, AuthMode: AuthKindContext, KubeContext: input.ClusterName, NetworkRouteProfile: "local-kind"},
		}
		if err := materialized.InfrastructureExecutionIdentity.Validate(); err != nil {
			return MaterializedTarget{}, err
		}
		return materialized, nil
	case ProviderAWS:
		if input.AccountID == "" || input.Region == "" {
			return MaterializedTarget{}, fmt.Errorf("AWS target account ID and region are required")
		}
		if input.ClusterName == "" && input.ClusterARN == "" {
			return MaterializedTarget{}, fmt.Errorf("AWS target cluster name or ARN is required")
		}
		incarnation := input.IncarnationID
		if incarnation == "" {
			incarnation = input.ClusterARN
		}
		if incarnation == "" {
			incarnation = input.ClusterName
		}
		materialized := MaterializedTarget{
			InfrastructureExecutionIdentity: InfrastructureExecutionIdentity{Provider: ProviderAWS, Mode: ModeAWSNative, AccountID: input.AccountID, Region: input.Region, RoleARN: input.ExecutionRoleARN},
			RuntimeTargetIdentity:           RuntimeTargetIdentity{Provider: ProviderAWS, AccountID: input.AccountID, Region: input.Region, ClusterARN: input.ClusterARN, ClusterName: input.ClusterName, IncarnationID: incarnation},
			TargetConnectionProfile:         TargetConnectionProfile{Endpoint: input.ConnectionProfile, AuthMode: AuthAWSEKS, NetworkRouteProfile: "aws-eks", RoleARN: input.RuntimeRoleARN},
		}
		if err := materialized.InfrastructureExecutionIdentity.Validate(); err != nil {
			return MaterializedTarget{}, err
		}
		return materialized, nil
	default:
		return MaterializedTarget{}, fmt.Errorf("unsupported target provider %q", input.Provider)
	}
}

func Digest(value any) (string, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(encoded)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

func (i InfrastructureExecutionIdentity) Validate() error {
	if i.Provider == "" || i.Mode == "" || i.AccountID == "" || i.Region == "" {
		return fmt.Errorf("infrastructure execution identity requires provider, mode, account ID, and region")
	}
	switch i.Provider {
	case ProviderKind:
		if i.Mode != ModeLocal {
			return fmt.Errorf("Kind infrastructure identity must use mode %q", ModeLocal)
		}
	case ProviderAWS:
		if i.Mode != ModeAWSNative {
			return fmt.Errorf("AWS infrastructure identity must use mode %q", ModeAWSNative)
		}
		if i.RoleARN != "" {
			account, err := accountFromARN(i.RoleARN)
			if err != nil || roleNameFromARN(i.RoleARN) == "" {
				return fmt.Errorf("AWS infrastructure execution role ARN is invalid")
			}
			if account != i.AccountID {
				return fmt.Errorf("AWS infrastructure execution role account mismatch: got %q, want %q", account, i.AccountID)
			}
		}
	default:
		return fmt.Errorf("unsupported infrastructure identity provider %q", i.Provider)
	}
	return nil
}

func (i RuntimeTargetIdentity) Validate() error {
	if i.Provider == "" || i.AccountID == "" || i.Region == "" || i.IncarnationID == "" {
		return fmt.Errorf("runtime target identity requires provider, account ID, region, and incarnation ID")
	}
	if i.ClusterARN == "" && i.ClusterName == "" {
		return fmt.Errorf("runtime target identity requires cluster ARN or cluster name")
	}
	return nil
}

func (p TargetConnectionProfile) Validate() error {
	if p.AuthMode == "" {
		return fmt.Errorf("target connection profile requires auth mode")
	}
	switch p.AuthMode {
	case AuthKindContext:
		if p.Endpoint == "" || p.KubeContext == "" {
			return fmt.Errorf("kind-context target connection requires endpoint and kube context")
		}
	case AuthAWSEKS:
		if p.Endpoint == "" || p.CACertificateData == "" {
			return fmt.Errorf("aws-eks target connection requires endpoint and CA certificate data")
		}
		if parsed, err := url.Parse(p.Endpoint); err != nil || parsed.Scheme != "https" || parsed.Host == "" {
			return fmt.Errorf("aws-eks target endpoint must be an HTTPS URL")
		}
	default:
		return fmt.Errorf("unsupported target authentication mode %q", p.AuthMode)
	}
	return nil
}

func ValidateBinding(identity RuntimeTargetIdentity, profile TargetConnectionProfile) error {
	if err := identity.Validate(); err != nil {
		return err
	}
	if err := profile.Validate(); err != nil {
		return err
	}
	if profile.AuthMode == AuthAWSEKS && identity.Provider != ProviderAWS {
		return fmt.Errorf("AWS EKS authentication cannot be used for provider %q", identity.Provider)
	}
	if profile.AuthMode == AuthKindContext && identity.Provider != ProviderKind {
		return fmt.Errorf("kind-context authentication cannot be used for provider %q", identity.Provider)
	}
	if profile.AuthMode == AuthKindContext && profile.KubeContext == "" {
		return fmt.Errorf("kind-context authentication requires kube context")
	}
	return nil
}

type AssumeRoleRequest struct {
	RoleARN         string
	SessionName     string
	ExternalID      string
	ExpectedAccount string
	Region          string
}

type AssumedSession struct {
	AccountID       string
	RoleARN         string
	Region          string
	AccessKeyID     string
	SecretAccessKey string
	SessionToken    string
	TokenID         string
	ExpiresAt       time.Time
}

// AssumeRole is the integration boundary for real STS. Local validation uses
// a deterministic implementation and never calls AWS.
type AssumeRole interface {
	AssumeRole(AssumeRoleRequest) (AssumedSession, error)
}

type LocalAssumeRole struct{}

func (LocalAssumeRole) AssumeRole(request AssumeRoleRequest) (AssumedSession, error) {
	if request.ExpectedAccount == "" || request.Region == "" {
		return AssumedSession{}, fmt.Errorf("local AssumeRole requires expected account and region")
	}
	return AssumedSession{AccountID: request.ExpectedAccount, RoleARN: request.RoleARN, Region: request.Region, TokenID: "localstack-session"}, nil
}
