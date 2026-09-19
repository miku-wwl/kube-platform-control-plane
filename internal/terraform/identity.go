package terraform

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

type TargetIdentity struct {
	Provider              string `json:"provider"`
	Mode                  string `json:"mode"`
	AccountID             string `json:"accountId"`
	Region                string `json:"region"`
	EndpointProfileDigest string `json:"endpointProfileDigest,omitempty"`
}

type EndpointProfile struct {
	GatewayEndpoint        string            `json:"gatewayEndpoint"`
	ProviderEndpoints      map[string]string `json:"providerEndpoints"`
	BackendEndpoints       map[string]string `json:"backendEndpoints"`
	S3AddressingMode       string            `json:"s3AddressingMode"`
	AccountID              string            `json:"accountId"`
	Region                 string            `json:"region"`
	EKSEndpointTranslation string            `json:"eksEndpointTranslationPolicy,omitempty"`
}

func (p EndpointProfile) Validate() error {
	if p.GatewayEndpoint == "" || p.AccountID == "" || p.Region == "" {
		return fmt.Errorf("endpoint profile gateway, account ID, and region are required")
	}
	if p.S3AddressingMode == "" {
		return fmt.Errorf("endpoint profile S3 addressing mode is required")
	}
	if p.ProviderEndpoints["s3"] == "" || p.BackendEndpoints["s3"] == "" {
		return fmt.Errorf("endpoint profile must route provider and backend S3")
	}
	return nil
}

func (p EndpointProfile) Digest() (string, error) {
	if err := p.Validate(); err != nil {
		return "", err
	}
	return Digest(p)
}

type PlatformIdentity struct {
	RuntimeOS         string `json:"runtimeOS"`
	RuntimeArch       string `json:"runtimeArch"`
	AbsoluteWorkDir   string `json:"absoluteWorkDir"`
	TerraformVersion  string `json:"terraformVersion"`
	RunnerImageDigest string `json:"runnerImageDigest"`
}

type ExecutionContext struct {
	SourceBundleDigest              string `json:"sourceBundleDigest"`
	ResolvedBackendConfigDigest     string `json:"resolvedBackendConfigDigest"`
	TerraformVersion                string `json:"terraformVersion"`
	TerraformLockfileDigest         string `json:"terraformLockfileDigest"`
	VariablesIdentityDigest         string `json:"variablesIdentityDigest"`
	BackendConfigIdentityDigest     string `json:"backendConfigIdentityDigest"`
	Workspace                       string `json:"workspace"`
	RunnerImageDigest               string `json:"runnerImageDigest"`
	ExecutionTargetIdentityDigest   string `json:"executionTargetIdentityDigest"`
	ExecutionPlatformIdentityDigest string `json:"executionPlatformIdentityDigest"`
	TerraformParallelism            *int32 `json:"terraformParallelism,omitempty"`
}

type BackendSnapshot struct {
	Type             string            `json:"type"`
	Bucket           string            `json:"bucket"`
	Key              string            `json:"key"`
	Region           string            `json:"region"`
	UseLockfile      bool              `json:"use_lockfile"`
	EndpointSettings map[string]string `json:"endpointSettings,omitempty"`
}

func Digest(value any) (string, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	hash := sha256.Sum256(encoded)
	return "sha256:" + hex.EncodeToString(hash[:]), nil
}

func (s BackendSnapshot) Validate() error {
	if s.Type != "s3" {
		return fmt.Errorf("unsupported backend type %q", s.Type)
	}
	if s.Bucket == "" || s.Key == "" || s.Region == "" {
		return fmt.Errorf("backend bucket, key, and region are required")
	}
	if !s.UseLockfile {
		return fmt.Errorf("s3 backend must enable use_lockfile")
	}
	for key := range s.EndpointSettings {
		lower := strings.ToLower(key)
		if strings.Contains(lower, "access_key") || strings.Contains(lower, "secret_key") || strings.Contains(lower, "session_token") {
			return fmt.Errorf("backend snapshot cannot contain credential field %q", key)
		}
	}
	return nil
}

var backendCredentialField = regexp.MustCompile(`(?i)\b(access_key|secret_key|session_token|token)\b`)

func ValidateBackendConfigContent(content []byte) error {
	if len(content) == 0 {
		return fmt.Errorf("backend snapshot is empty")
	}
	if backendCredentialField.Match(content) {
		return fmt.Errorf("backend snapshot contains credential material")
	}
	return nil
}
