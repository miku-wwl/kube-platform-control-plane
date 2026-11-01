package terraform

import (
	"fmt"
	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/gohcl"
	"github.com/hashicorp/hcl/v2/hclparse"
	platform "github.com/miku-wwl/kube-platform-control-plane/api/v1alpha1"
)

func ValidateBackendProfile(content []byte, p *platform.ExecutionProfile) error {
	if err := ValidateBackendConfigContent(content); err != nil {
		return err
	}
	if p == nil {
		return fmt.Errorf("backend requires an explicit emulator profile")
	}
	f, diag := hclparse.NewParser().ParseHCL(content, "backend.hcl")
	if diag.HasErrors() {
		return fmt.Errorf("invalid backend HCL")
	}
	var config struct {
		Bucket       string            `hcl:"bucket"`
		Key          string            `hcl:"key"`
		Region       string            `hcl:"region"`
		UseLockfile  bool              `hcl:"use_lockfile"`
		UsePathStyle bool              `hcl:"use_path_style"`
		Endpoints    map[string]string `hcl:"endpoints"`
		Remain       hcl.Body          `hcl:",remain"`
	}
	if diag := gohcl.DecodeBody(f.Body, nil, &config); diag.HasErrors() {
		return fmt.Errorf("backend requires explicit bucket, key, region, locking and endpoint configuration")
	}
	if config.Bucket == "" || config.Key == "" || config.Region != p.Region || !config.UseLockfile || !config.UsePathStyle || config.Endpoints["s3"] != p.S3Endpoint || config.Endpoints["sts"] != p.STSEndpoint {
		return fmt.Errorf("backend routing, region or locking does not match the execution profile")
	}
	return nil
}
