package controller

import (
	"context"
	"fmt"
	"regexp"

	platform "github.com/miku-wwl/kube-platform-control-plane/api/v1alpha1"
	"github.com/miku-wwl/kube-platform-control-plane/internal/cloud"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var immutableImage = regexp.MustCompile(`@sha256:[a-f0-9]{64}$`)

func resolveExecutionProfile(ctx context.Context, reader client.Reader, namespace string, input *platform.ExecutionProfile) (*platform.ExecutionProfile, error) {
	if err := cloud.ValidateExecutionProfile(input); err != nil {
		return nil, err
	}
	p := input.DeepCopy()
	var secret corev1.Secret
	if err := reader.Get(ctx, client.ObjectKey{Namespace: namespace, Name: p.CredentialsSecretRef.Name}, &secret); err != nil {
		return nil, fmt.Errorf("read emulator credential Secret: %w", err)
	}
	account, key, token := string(secret.Data["AWS_ACCESS_KEY_ID"]), string(secret.Data["AWS_SECRET_ACCESS_KEY"]), string(secret.Data["AWS_SESSION_TOKEN"])
	if err := cloud.EmulatorCredentials(account, key, token); err != nil {
		return nil, err
	}
	if account == "test" {
		account = "000000000000"
	}
	if account != p.AccountID || secret.UID == "" || secret.ResourceVersion == "" {
		return nil, fmt.Errorf("emulator credential account/identity mismatch")
	}
	identity := digestIdentity([]string{namespace, secret.Name, string(secret.UID), secret.ResourceVersion})
	if p.CredentialsIdentity != "" && p.CredentialsIdentity != identity {
		return nil, fmt.Errorf("emulator credential Secret changed; a new Plan and approval are required")
	}
	p.CredentialsIdentity = identity
	return p, nil
}
