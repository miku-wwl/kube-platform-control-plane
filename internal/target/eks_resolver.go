package target

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/aws/signer/v4"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	"github.com/aws/smithy-go/middleware"
	smithyhttp "github.com/aws/smithy-go/transport/http"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
)

type EKSTokenProvider interface {
	Token(context.Context, RuntimeTargetIdentity, TargetConnectionProfile) (string, error)
}

type STSPresigner interface {
	PresignGetCallerIdentity(context.Context, *sts.GetCallerIdentityInput, ...func(*sts.PresignOptions)) (*v4.PresignedHTTPRequest, error)
}

type AWSSTSTokenProvider struct {
	ConfigLoader      func(context.Context, string) (aws.Config, error)
	AssumeRoleFactory func(aws.Config) AssumeRole
	PresignerFactory  func(context.Context, aws.Config, string) (STSPresigner, error)
}

func NewAWSSTSTokenProvider() *AWSSTSTokenProvider {
	return &AWSSTSTokenProvider{
		ConfigLoader: func(ctx context.Context, region string) (aws.Config, error) {
			return awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion(region))
		},
		AssumeRoleFactory: func(cfg aws.Config) AssumeRole {
			return RealAWSAssumeRole{Client: sts.NewFromConfig(cfg)}
		},
		PresignerFactory: func(ctx context.Context, cfg aws.Config, clusterName string) (STSPresigner, error) {
			client := sts.NewFromConfig(cfg)
			return sts.NewPresignClient(client, sts.WithPresignClientFromClientOptions(sts.WithAPIOptions(addEKSClusterHeader(clusterName)))), nil
		},
	}
}

func (p AWSSTSTokenProvider) Token(ctx context.Context, identity RuntimeTargetIdentity, profile TargetConnectionProfile) (string, error) {
	if identity.Provider != ProviderAWS || identity.ClusterName == "" || identity.Region == "" {
		return "", fmt.Errorf("AWS EKS token requires AWS provider, cluster name, and region")
	}
	if p.ConfigLoader == nil {
		return "", fmt.Errorf("AWS EKS token AWS config loader is not configured")
	}
	cfg, err := p.ConfigLoader(ctx, identity.Region)
	if err != nil {
		return "", fmt.Errorf("load AWS runtime identity: %w", err)
	}
	if profile.RoleARN != "" {
		expectedRoleName := roleNameFromARN(profile.RoleARN)
		if expectedRoleName == "" {
			return "", fmt.Errorf("runtime role ARN is not an IAM role ARN")
		}
		roleAccount, err := accountFromARN(profile.RoleARN)
		if err != nil {
			return "", fmt.Errorf("validate runtime role ARN: %w", err)
		}
		if roleAccount != identity.AccountID {
			return "", fmt.Errorf("runtime role account mismatch: got %q, want %q", roleAccount, identity.AccountID)
		}
		if p.AssumeRoleFactory == nil {
			return "", fmt.Errorf("AWS runtime role is configured but AssumeRole is not configured")
		}
		session, err := p.AssumeRoleFactory(cfg).AssumeRole(AssumeRoleRequest{
			RoleARN:         profile.RoleARN,
			SessionName:     "pcp-eks-runtime",
			ExpectedAccount: identity.AccountID,
			Region:          identity.Region,
		})
		if err != nil {
			return "", fmt.Errorf("assume runtime role: %w", err)
		}
		if session.AccountID != identity.AccountID || session.Region != identity.Region {
			return "", fmt.Errorf("assumed runtime identity does not match target")
		}
		if roleAccount, err := accountFromARN(session.RoleARN); err != nil || roleAccount != identity.AccountID {
			return "", fmt.Errorf("assumed runtime role account does not match target")
		}
		if roleNameFromARN(session.RoleARN) != expectedRoleName {
			return "", fmt.Errorf("assumed runtime role does not match configured role")
		}
		if session.AccessKeyID == "" || session.SecretAccessKey == "" || session.SessionToken == "" || session.ExpiresAt.IsZero() {
			return "", fmt.Errorf("assumed runtime role returned incomplete credentials")
		}
		if !session.ExpiresAt.After(time.Now()) {
			return "", fmt.Errorf("assumed runtime role returned expired credentials")
		}
		cfg.Credentials = assumedCredentialsProvider{credentials: aws.Credentials{
			AccessKeyID: session.AccessKeyID, SecretAccessKey: session.SecretAccessKey,
			SessionToken: session.SessionToken, Source: "AssumeRole", CanExpire: true, Expires: session.ExpiresAt,
		}}
	}
	if p.PresignerFactory == nil {
		return "", fmt.Errorf("AWS EKS token presigner is not configured")
	}
	presigner, err := p.PresignerFactory(ctx, cfg, identity.ClusterName)
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

// assumedCredentialsProvider represents short-lived STS credentials only. It
// is intentionally not a long-lived/static access-key provider; a new token
// request rebuilds the AWS config and re-enters the AssumeRole boundary.
type assumedCredentialsProvider struct {
	credentials aws.Credentials
}

func (p assumedCredentialsProvider) Retrieve(context.Context) (aws.Credentials, error) {
	return p.credentials, nil
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
	token, err := r.TokenProvider.Token(ctx, identity, profile)
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
