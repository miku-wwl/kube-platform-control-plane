package cloud

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"

	platform "github.com/miku-wwl/kube-platform-control-plane/api/v1alpha1"
)

var emulatorAccount = regexp.MustCompile(`^[0-9]{12}$`)

func ValidateEmulatorEndpoint(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return fmt.Errorf("explicit HTTP(S) emulator endpoint without credentials is required")
	}
	host := strings.ToLower(u.Hostname())
	if host == "amazonaws.com" || strings.HasSuffix(host, ".amazonaws.com") || host == "amazonaws.com.cn" || strings.HasSuffix(host, ".amazonaws.com.cn") {
		return fmt.Errorf("AWS endpoints are forbidden in emulator mode")
	}
	return nil
}

func ValidateExecutionProfile(p *platform.ExecutionProfile) error {
	if p == nil || p.Mode != "emulator" || !emulatorAccount.MatchString(p.AccountID) || p.Region == "" || p.ArtifactBucket == "" || p.CredentialsSecretRef.Name == "" {
		return fmt.Errorf("emulator profile requires mode, 12-digit account, region, artifact bucket and credential Secret reference")
	}
	if err := ValidateEmulatorEndpoint(p.S3Endpoint); err != nil {
		return err
	}
	return ValidateEmulatorEndpoint(p.STSEndpoint)
}

// EmulatorCredentials accepts explicit dummy credentials only; no ambient AWS,
// shared-profile, metadata, web-identity or real STS fallback is permitted.
func EmulatorCredentials(account, secret, token string) error {
	if !(emulatorAccount.MatchString(account) || account == "test") || secret != "test" || token != "" {
		return fmt.Errorf("emulator requires explicit dummy credentials and no session token")
	}
	return nil
}
