package target

import (
	"context"
	"fmt"
	"sync"

	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/tools/clientcmd"
)

// TargetClientFactory resolves a client only for a previously registered
// target identity/profile pair. This makes wrong-target and wrong-account
// use fail closed before any runtime mutation.
type TargetClientFactory struct {
	mu       sync.RWMutex
	clients  map[string]dynamic.Interface
	profiles map[string]TargetConnectionProfile
}

type ClientResolver interface {
	Client(context.Context, RuntimeTargetIdentity, TargetConnectionProfile) (dynamic.Interface, error)
}

// ProviderClientResolver keeps runtime reconciliation provider-neutral. The
// ResourceSet only asks for a client; Kind and EKS authentication stay behind
// this adapter boundary.
type ProviderClientResolver struct {
	Kind ClientResolver
	AWS  ClientResolver
}

func (r ProviderClientResolver) Client(ctx context.Context, identity RuntimeTargetIdentity, profile TargetConnectionProfile) (dynamic.Interface, error) {
	switch profile.AuthMode {
	case AuthKindContext:
		if r.Kind == nil {
			return nil, fmt.Errorf("kind target resolver is not configured")
		}
		return r.Kind.Client(ctx, identity, profile)
	case AuthAWSEKS:
		if r.AWS == nil {
			return nil, fmt.Errorf("AWS EKS target resolver is not configured")
		}
		return r.AWS.Client(ctx, identity, profile)
	default:
		return nil, fmt.Errorf("unsupported target auth mode %q", profile.AuthMode)
	}
}

func NewTargetClientFactory() *TargetClientFactory {
	return &TargetClientFactory{clients: map[string]dynamic.Interface{}, profiles: map[string]TargetConnectionProfile{}}
}

func (f *TargetClientFactory) Register(identity RuntimeTargetIdentity, profile TargetConnectionProfile, client dynamic.Interface) error {
	if f == nil || client == nil {
		return fmt.Errorf("target client factory and client are required")
	}
	if err := ValidateBinding(identity, profile); err != nil {
		return err
	}
	digest, err := Digest(identity)
	if err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.clients[digest] = client
	f.profiles[digest] = profile
	return nil
}

func (f *TargetClientFactory) Client(_ context.Context, identity RuntimeTargetIdentity, profile TargetConnectionProfile) (dynamic.Interface, error) {
	if f == nil {
		return nil, fmt.Errorf("target client factory is nil")
	}
	if err := ValidateBinding(identity, profile); err != nil {
		return nil, err
	}
	digest, err := Digest(identity)
	if err != nil {
		return nil, err
	}
	f.mu.RLock()
	defer f.mu.RUnlock()
	registered, ok := f.profiles[digest]
	if !ok {
		return nil, fmt.Errorf("target identity %s is not registered", digest)
	}
	if registered != profile {
		return nil, fmt.Errorf("target connection profile mismatch for identity %s", digest)
	}
	return f.clients[digest], nil
}

// KubeconfigClientFactory is the local Kind adapter. The context is part of
// the frozen connection profile, so a caller cannot silently use the current
// kubectl context for another target.
type KubeconfigClientFactory struct {
	Path string
}

func (f KubeconfigClientFactory) Client(_ context.Context, identity RuntimeTargetIdentity, profile TargetConnectionProfile) (dynamic.Interface, error) {
	if err := ValidateBinding(identity, profile); err != nil {
		return nil, err
	}
	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	if f.Path != "" {
		loadingRules.ExplicitPath = f.Path
	}
	config, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, &clientcmd.ConfigOverrides{CurrentContext: profile.KubeContext}).ClientConfig()
	if err != nil {
		return nil, fmt.Errorf("load target context %q: %w", profile.KubeContext, err)
	}
	client, err := dynamic.NewForConfig(config)
	if err != nil {
		return nil, err
	}
	return client, nil
}
