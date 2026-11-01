package terraform

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	platform "github.com/miku-wwl/kube-platform-control-plane/api/v1alpha1"
	"github.com/miku-wwl/kube-platform-control-plane/internal/cloud"
)

var immutableRevision = regexp.MustCompile(`^[a-fA-F0-9]{40,64}$`)
var inputName = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

// ValidateModule reads a contract shipped inside the pinned source. The runner
// passes only declared string inputs; it never writes Terraform source code.
func ValidateModule(dir, version string, inputs map[string]string) error {
	content, err := os.ReadFile(filepath.Join(dir, "kpcp-module.json"))
	if err != nil {
		return fmt.Errorf("fixed module contract: %w", err)
	}
	var contract struct {
		ID               string          `json:"id"`
		TerraformVersion string          `json:"terraformVersion"`
		Inputs           map[string]bool `json:"inputs"` // true means required
	}
	if err := json.Unmarshal(content, &contract); err != nil {
		return err
	}
	if contract.ID != "aws-s3-lifecycle-v1" || contract.TerraformVersion != version || version != "1.14.0" || len(contract.Inputs) == 0 {
		return fmt.Errorf("module identity, exact Terraform version and input contract are required")
	}
	if lock, err := os.ReadFile(filepath.Join(dir, ".terraform.lock.hcl")); err != nil || len(lock) == 0 {
		return fmt.Errorf("fixed module requires a provider lockfile")
	}
	for key, required := range contract.Inputs {
		if required && inputs[key] == "" {
			return fmt.Errorf("required module input %q is missing", key)
		}
	}
	for key := range inputs {
		if _, ok := contract.Inputs[key]; !ok || !inputName.MatchString(key) {
			return fmt.Errorf("module input %q is not allowed", key)
		}
		lower := strings.ToLower(key)
		if strings.Contains(lower, "password") || strings.Contains(lower, "secret") || strings.Contains(lower, "access_key") || strings.Contains(lower, "token") {
			return fmt.Errorf("inline credential input %q is forbidden", key)
		}
	}
	return nil
}

// BindModuleInputs fixes endpoint/role authority independently of user inputs.
func BindModuleInputs(inputs map[string]string, profile *platform.ExecutionProfile, role string) (map[string]string, error) {
	if profile == nil {
		return nil, fmt.Errorf("explicit emulator executionProfile is required")
	}
	if err := cloud.ValidateExecutionProfile(profile); err != nil {
		return nil, err
	}
	result := make(map[string]string, len(inputs)+4)
	for key, value := range inputs {
		result[key] = value
	}
	for key, value := range map[string]string{"s3_endpoint": profile.S3Endpoint, "sts_endpoint": profile.STSEndpoint, "region": profile.Region, "execution_role_arn": role} {
		if supplied, ok := result[key]; ok && supplied != value {
			return nil, fmt.Errorf("input %q conflicts with execution identity", key)
		}
		result[key] = value
	}
	return result, nil
}
