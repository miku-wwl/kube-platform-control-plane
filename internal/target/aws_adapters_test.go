package target

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"math/big"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/aws/signer/v4"
	"github.com/aws/aws-sdk-go-v2/service/eks"
	ekstypes "github.com/aws/aws-sdk-go-v2/service/eks/types"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	"github.com/aws/aws-sdk-go-v2/service/sts/types"
)

type fakeSTS struct {
	output *sts.AssumeRoleOutput
	err    error
}

func (f fakeSTS) AssumeRole(context.Context, *sts.AssumeRoleInput, ...func(*sts.Options)) (*sts.AssumeRoleOutput, error) {
	return f.output, f.err
}

func TestRealAWSAssumeRoleValidatesAccountRoleAndExpiry(t *testing.T) {
	expires := time.Now().Add(time.Hour)
	adapter := RealAWSAssumeRole{Client: fakeSTS{output: &sts.AssumeRoleOutput{
		AssumedRoleUser: &types.AssumedRoleUser{Arn: aws.String("arn:aws:sts::123456789012:assumed-role/platform-provisioner/session")},
		Credentials:     &types.Credentials{AccessKeyId: aws.String("ASIA123"), SecretAccessKey: aws.String("secret"), SessionToken: aws.String("session-token"), Expiration: &expires},
	}}, Now: time.Now}
	session, err := adapter.AssumeRole(AssumeRoleRequest{RoleARN: "arn:aws:iam::123456789012:role/platform-provisioner", SessionName: "session", ExpectedAccount: "123456789012", Region: "us-east-1"})
	if err != nil {
		t.Fatalf("AssumeRole() error = %v", err)
	}
	if session.AccountID != "123456789012" || session.TokenID != "ASIA123" {
		t.Fatalf("session = %+v", session)
	}

	adapter.Client = fakeSTS{output: &sts.AssumeRoleOutput{
		AssumedRoleUser: &types.AssumedRoleUser{Arn: aws.String("arn:aws:sts::999999999999:assumed-role/platform-provisioner/session")},
		Credentials:     &types.Credentials{AccessKeyId: aws.String("ASIA123"), SecretAccessKey: aws.String("secret"), SessionToken: aws.String("session-token"), Expiration: &expires},
	}}
	if _, err := adapter.AssumeRole(AssumeRoleRequest{RoleARN: "arn:aws:iam::123456789012:role/platform-provisioner", SessionName: "session", ExpectedAccount: "123456789012", Region: "us-east-1"}); err == nil {
		t.Fatal("wrong account was accepted")
	}

	adapter.Client = fakeSTS{output: &sts.AssumeRoleOutput{
		AssumedRoleUser: &types.AssumedRoleUser{Arn: aws.String("arn:aws:sts::123456789012:assumed-role/other-role/session")},
		Credentials:     &types.Credentials{AccessKeyId: aws.String("ASIA123"), SecretAccessKey: aws.String("secret"), SessionToken: aws.String("session-token"), Expiration: &expires},
	}}
	if _, err := adapter.AssumeRole(AssumeRoleRequest{RoleARN: "arn:aws:iam::123456789012:role/platform-provisioner", SessionName: "session", ExpectedAccount: "123456789012", Region: "us-east-1"}); err == nil {
		t.Fatal("role mismatch was accepted")
	}

	expired := time.Now().Add(-time.Minute)
	adapter.Client = fakeSTS{output: &sts.AssumeRoleOutput{
		AssumedRoleUser: &types.AssumedRoleUser{Arn: aws.String("arn:aws:sts::123456789012:assumed-role/platform-provisioner/session")},
		Credentials:     &types.Credentials{AccessKeyId: aws.String("ASIA123"), SecretAccessKey: aws.String("secret"), SessionToken: aws.String("session-token"), Expiration: &expired},
	}}
	if _, err := adapter.AssumeRole(AssumeRoleRequest{RoleARN: "arn:aws:iam::123456789012:role/platform-provisioner", SessionName: "session", ExpectedAccount: "123456789012", Region: "us-east-1"}); err == nil {
		t.Fatal("expired credentials were accepted")
	}
	adapter.Client = fakeSTS{err: errors.New("AccessDenied")}
	if _, err := adapter.AssumeRole(AssumeRoleRequest{RoleARN: "arn:aws:iam::123456789012:role/platform-provisioner", SessionName: "session", ExpectedAccount: "123456789012", Region: "us-east-1"}); err == nil {
		t.Fatal("AssumeRole denial was swallowed")
	}
}

type fakeEKS struct {
	output *eks.DescribeClusterOutput
	err    error
}

func (f fakeEKS) DescribeCluster(context.Context, *eks.DescribeClusterInput, ...func(*eks.Options)) (*eks.DescribeClusterOutput, error) {
	return f.output, f.err
}

func TestAWSEKSVerifierBindsEndpointCAAccountAndIncarnation(t *testing.T) {
	ca := "Y2VydGlmaWNhdGU="
	identity := RuntimeTargetIdentity{Provider: ProviderAWS, AccountID: "123456789012", Region: "us-east-1", ClusterARN: "arn:aws:eks:us-east-1:123456789012:cluster/platform", ClusterName: "platform", IncarnationID: "arn:aws:eks:us-east-1:123456789012:cluster/platform"}
	profile := TargetConnectionProfile{Endpoint: "https://platform.eks.example", CACertificateData: ca, CACertificateDigest: certificateDigest(ca), AuthMode: AuthAWSEKS, NetworkRouteProfile: "aws-eks"}
	verifier := &AWSEKSVerifier{ClientFactory: func(context.Context, string) (EKSDescribeAPI, error) {
		return fakeEKS{output: &eks.DescribeClusterOutput{Cluster: &ekstypes.Cluster{
			Arn: aws.String(identity.ClusterARN), Name: aws.String(identity.ClusterName), Endpoint: aws.String(profile.Endpoint),
			CertificateAuthority: &ekstypes.Certificate{Data: aws.String(ca)},
		}}}, nil
	}}
	if err := verifier.Verify(context.Background(), identity, profile); err != nil {
		t.Fatalf("Verify() error = %v", err)
	}

	wrong := identity
	wrong.AccountID = "999999999999"
	if err := verifier.Verify(context.Background(), wrong, profile); err == nil {
		t.Fatal("wrong account was accepted")
	}
	if err := (&AWSEKSVerifier{ClientFactory: func(context.Context, string) (EKSDescribeAPI, error) {
		return nil, errors.New("DescribeCluster denied")
	}}).Verify(context.Background(), identity, profile); err == nil {
		t.Fatal("DescribeCluster error was swallowed")
	}
}

type fakePresigner struct{}

func (fakePresigner) PresignGetCallerIdentity(context.Context, *sts.GetCallerIdentityInput, ...func(*sts.PresignOptions)) (*v4.PresignedHTTPRequest, error) {
	return &v4.PresignedHTTPRequest{URL: "https://sts.amazonaws.com/?Action=GetCallerIdentity&X-Amz-SignedHeaders=host%3Bx-k8s-aws-id"}, nil
}

type fakeTokenProvider struct{ token string }

func (f fakeTokenProvider) Token(context.Context, RuntimeTargetIdentity, TargetConnectionProfile) (string, error) {
	return f.token, nil
}

func TestEKSTokenAndResolverUseTrustedTargetBinding(t *testing.T) {
	provider := AWSSTSTokenProvider{
		ConfigLoader:     func(context.Context, string) (aws.Config, error) { return aws.Config{}, nil },
		PresignerFactory: func(context.Context, aws.Config, string) (STSPresigner, error) { return fakePresigner{}, nil },
	}
	identity := RuntimeTargetIdentity{Provider: ProviderAWS, AccountID: "123456789012", Region: "us-east-1", ClusterName: "platform", IncarnationID: "cluster-incarnation"}
	token, err := provider.Token(context.Background(), identity, TargetConnectionProfile{})
	if err != nil || len(token) < len("k8s-aws-v1.") || token[:len("k8s-aws-v1.")] != "k8s-aws-v1." {
		t.Fatalf("Token() = %q, error = %v", token, err)
	}

	resolver := EKSTargetResolver{TokenProvider: fakeTokenProvider{token: "k8s-aws-v1.fake"}}
	profile := TargetConnectionProfile{Endpoint: "https://platform.eks.example", CACertificateData: validCAData(t), AuthMode: AuthAWSEKS, NetworkRouteProfile: "aws-eks"}
	if _, err := resolver.Client(context.Background(), identity, profile); err != nil {
		t.Fatalf("Client() error = %v", err)
	}
	wrong := identity
	wrong.Provider = ProviderKind
	if _, err := resolver.Client(context.Background(), wrong, profile); err == nil {
		t.Fatal("wrong provider was accepted")
	}
}

type fakeAssumeRole struct {
	request AssumeRoleRequest
	session AssumedSession
	err     error
}

func (f *fakeAssumeRole) AssumeRole(request AssumeRoleRequest) (AssumedSession, error) {
	f.request = request
	return f.session, f.err
}

func TestAWSSTSTokenProviderUsesRuntimeRoleAndDoesNotCrossTargetCache(t *testing.T) {
	identityA := RuntimeTargetIdentity{Provider: ProviderAWS, AccountID: "123456789012", Region: "us-east-1", ClusterName: "platform-a", IncarnationID: "a"}
	identityB := RuntimeTargetIdentity{Provider: ProviderAWS, AccountID: "123456789012", Region: "us-west-2", ClusterName: "platform-b", IncarnationID: "b"}
	assumptions := make([]AssumeRoleRequest, 0, 2)
	provider := AWSSTSTokenProvider{
		ConfigLoader: func(context.Context, string) (aws.Config, error) { return aws.Config{}, nil },
		AssumeRoleFactory: func(aws.Config) AssumeRole {
			return fakeAssumeRoleFunc(func(request AssumeRoleRequest) (AssumedSession, error) {
				assumptions = append(assumptions, request)
				return AssumedSession{AccountID: request.ExpectedAccount, RoleARN: request.RoleARN, Region: request.Region, AccessKeyID: "ASIA" + request.Region, SecretAccessKey: "secret", SessionToken: "token", ExpiresAt: time.Now().Add(time.Hour)}, nil
			})
		},
		PresignerFactory: func(_ context.Context, cfg aws.Config, _ string) (STSPresigner, error) {
			if cfg.Credentials == nil {
				return nil, errors.New("assumed credentials were not installed")
			}
			return fakePresigner{}, nil
		},
	}
	if _, err := provider.Token(context.Background(), identityA, TargetConnectionProfile{RoleARN: "arn:aws:iam::123456789012:role/platform/runtime-a"}); err != nil {
		t.Fatalf("runtime role A token = %v", err)
	}
	if _, err := provider.Token(context.Background(), identityB, TargetConnectionProfile{RoleARN: "arn:aws:iam::123456789012:role/platform/runtime-b"}); err != nil {
		t.Fatalf("runtime role B token = %v", err)
	}
	if len(assumptions) != 2 || assumptions[0].RoleARN == assumptions[1].RoleARN || assumptions[0].Region == assumptions[1].Region {
		t.Fatalf("runtime assumptions were reused or not target-bound: %#v", assumptions)
	}
}

type fakeAssumeRoleFunc func(AssumeRoleRequest) (AssumedSession, error)

func (f fakeAssumeRoleFunc) AssumeRole(request AssumeRoleRequest) (AssumedSession, error) {
	return f(request)
}

func TestAWSSTSTokenProviderFailsClosedForRuntimeRoleErrors(t *testing.T) {
	identity := RuntimeTargetIdentity{Provider: ProviderAWS, AccountID: "123456789012", Region: "us-east-1", ClusterName: "platform", IncarnationID: "cluster"}
	base := AWSSTSTokenProvider{ConfigLoader: func(context.Context, string) (aws.Config, error) { return aws.Config{}, nil }, PresignerFactory: func(context.Context, aws.Config, string) (STSPresigner, error) { return fakePresigner{}, nil }}
	for name, test := range map[string]struct {
		session   AssumedSession
		assumeErr error
	}{
		"denied":        {assumeErr: errors.New("AccessDenied")},
		"wrong-account": {session: AssumedSession{AccountID: "999999999999", RoleARN: "arn:aws:sts::999999999999:assumed-role/runtime/session", Region: "us-east-1", AccessKeyID: "ASIA", SecretAccessKey: "secret", SessionToken: "token", ExpiresAt: time.Now().Add(time.Hour)}},
		"wrong-region":  {session: AssumedSession{AccountID: "123456789012", RoleARN: "arn:aws:sts::123456789012:assumed-role/runtime/session", Region: "us-west-2", AccessKeyID: "ASIA", SecretAccessKey: "secret", SessionToken: "token", ExpiresAt: time.Now().Add(time.Hour)}},
		"expired":       {session: AssumedSession{AccountID: "123456789012", RoleARN: "arn:aws:sts::123456789012:assumed-role/runtime/session", Region: "us-east-1", AccessKeyID: "ASIA", SecretAccessKey: "secret", SessionToken: "token", ExpiresAt: time.Now().Add(-time.Minute)}},
	} {
		t.Run(name, func(t *testing.T) {
			provider := base
			provider.AssumeRoleFactory = func(aws.Config) AssumeRole {
				return fakeAssumeRoleFunc(func(AssumeRoleRequest) (AssumedSession, error) { return test.session, test.assumeErr })
			}
			if _, err := provider.Token(context.Background(), identity, TargetConnectionProfile{RoleARN: "arn:aws:iam::123456789012:role/runtime"}); err == nil {
				t.Fatal("invalid runtime identity was accepted")
			}
		})
	}
}

func TestRoleNameFromARNHandlesPathsAndInvalidInput(t *testing.T) {
	tests := []struct {
		name string
		arn  string
		want string
	}{
		{name: "simple", arn: "arn:aws:iam::123456789012:role/terraform-runner", want: "terraform-runner"},
		{name: "one path", arn: "arn:aws:iam::123456789012:role/platform/terraform-runner", want: "terraform-runner"},
		{name: "nested path", arn: "arn:aws:iam::123456789012:role/platform/prod/terraform-runner", want: "terraform-runner"},
		{name: "invalid", arn: "not-an-arn", want: ""},
		{name: "empty", arn: "", want: ""},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := roleNameFromARN(test.arn); got != test.want {
				t.Fatalf("roleNameFromARN(%q) = %q, want %q", test.arn, got, test.want)
			}
		})
	}
}

func validCAData(t *testing.T) string {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 1024)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "test"}, NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(time.Hour), KeyUsage: x509.KeyUsageCertSign}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	return base64.StdEncoding.EncodeToString(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
}
