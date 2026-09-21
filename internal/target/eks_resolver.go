package target

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"

	"github.com/aws/aws-sdk-go-v2/aws/signer/v4"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	"github.com/aws/smithy-go/middleware"
	smithyhttp "github.com/aws/smithy-go/transport/http"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
)

type EKSTokenProvider interface {
	Token(context.Context, RuntimeTargetIdentity) (string, error)
}

type STSPresigner interface {
	PresignGetCallerIdentity(context.Context, *sts.GetCallerIdentityInput, ...func(*sts.PresignOptions)) (*v4.PresignedHTTPRequest, error)
}

type AWSSTSTokenProvider struct {
	PresignerFactory func(context.Context, string, string) (STSPresigner, error)
}

func NewAWSSTSTokenProvider() *AWSSTSTokenProvider {
	return &AWSSTSTokenProvider{PresignerFactory: func(ctx context.Context, region, clusterName string) (STSPresigner, error) {
		cfg, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion(region))
		if err != nil {
			return nil, err
		}
		client := sts.NewFromConfig(cfg)
		return sts.NewPresignClient(client, sts.WithPresignClientFromClientOptions(sts.WithAPIOptions(addEKSClusterHeader(clusterName)))), nil
	}}
}

func (p AWSSTSTokenProvider) Token(ctx context.Context, identity RuntimeTargetIdentity) (string, error) {
	if identity.Provider != ProviderAWS || identity.ClusterName == "" || identity.Region == "" {
		return "", fmt.Errorf("AWS EKS token requires AWS provider, cluster name, and region")
	}
	if p.PresignerFactory == nil {
		return "", fmt.Errorf("AWS EKS token presigner is not configured")
	}
	presigner, err := p.PresignerFactory(ctx, identity.Region, identity.ClusterName)
	if err != nil {
		return "", err
	}
	request, err := presigner.PresignGetCallerIdentity(ctx, &sts.GetCallerIdentityInput{})
	if err != nil {
		return "", fmt.Errorf("presign STS GetCallerIdentity: %w", err)
	}
	if request == nil || request.URL == "" {
		return "", fmt.Errorf("presign STS GetCallerIdentity returned no URL")
	}
	return "k8s-aws-v1." + base64.RawURLEncoding.EncodeToString([]byte(request.URL)), nil
}

func addEKSClusterHeader(clusterName string) func(*middleware.Stack) error {
	return func(stack *middleware.Stack) error {
		return stack.Build.Add(middleware.BuildMiddlewareFunc("eksClusterID", func(ctx context.Context, input middleware.BuildInput, next middleware.BuildHandler) (middleware.BuildOutput, middleware.Metadata, error) {
			request, ok := input.Request.(*smithyhttp.Request)
			if !ok {
				return middleware.BuildOutput{}, middleware.Metadata{}, fmt.Errorf("STS request is not an HTTP request")
			}
			request.Header.Set("x-k8s-aws-id", clusterName)
			return next.HandleBuild(ctx, input)
		}), middleware.Before)
	}
}

type EKSTargetResolver struct {
	TokenProvider EKSTokenProvider
	HTTPClient    *http.Client
}

func (r EKSTargetResolver) Client(ctx context.Context, identity RuntimeTargetIdentity, profile TargetConnectionProfile) (dynamic.Interface, error) {
	if identity.Provider != ProviderAWS || profile.AuthMode != AuthAWSEKS {
		return nil, fmt.Errorf("EKS resolver received a non-AWS target")
	}
	if err := ValidateBinding(identity, profile); err != nil {
		return nil, err
	}
	if r.TokenProvider == nil {
		return nil, fmt.Errorf("AWS EKS token provider is not configured")
	}
	caData, err := DecodeEKSCA(profile.CACertificateData)
	if err != nil {
		return nil, fmt.Errorf("decode EKS CA data: %w", err)
	}
	token, err := r.TokenProvider.Token(ctx, identity)
	if err != nil {
		return nil, err
	}
	config := &rest.Config{
		Host:        profile.Endpoint,
		BearerToken: token,
		TLSClientConfig: rest.TLSClientConfig{
			CAData: caData,
		},
	}
	if r.HTTPClient != nil {
		config.Transport = httpTransportFor(r.HTTPClient)
	}
	client, err := dynamic.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("create EKS dynamic client: %w", err)
	}
	return client, nil
}

// httpTransportFor keeps the resolver injectable without allowing a caller to
// replace the endpoint or credentials encoded in rest.Config.
func httpTransportFor(client *http.Client) http.RoundTripper {
	return roundTripperFunc(func(request *http.Request) (*http.Response, error) {
		return client.Do(request)
	})
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}
