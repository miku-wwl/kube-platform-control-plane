package target

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
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
	CACertificateDigest string `json:"caCertificateDigest,omitempty"`
	AuthMode            string `json:"authMode"`
	NetworkRouteProfile string `json:"networkRouteProfile,omitempty"`
	KubeContext         string `json:"kubeContext,omitempty"`
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
	if p.Endpoint == "" || p.AuthMode == "" {
		return fmt.Errorf("target connection profile requires endpoint and auth mode")
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
	if profile.AuthMode == "aws-eks" && identity.Provider != "aws" {
		return fmt.Errorf("AWS EKS authentication cannot be used for provider %q", identity.Provider)
	}
	if profile.AuthMode == "kind-context" && profile.KubeContext == "" {
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
	AccountID string
	RoleARN   string
	Region    string
	TokenID   string
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

// RealAWSAssumeRole is intentionally a boundary marker. It is not wired into
// local controllers; Phase 13 owns the AWS STS implementation and evidence.
type RealAWSAssumeRole struct{}

func (RealAWSAssumeRole) AssumeRole(AssumeRoleRequest) (AssumedSession, error) {
	return AssumedSession{}, fmt.Errorf("real AWS AssumeRole is deferred to Phase 13")
}
