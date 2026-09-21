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
		Credentials:     &types.Credentials{AccessKeyId: aws.String("ASIA123"), Expiration: &expires},
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
		Credentials:     &types.Credentials{AccessKeyId: aws.String("ASIA123"), Expiration: &expires},
	}}
	if _, err := adapter.AssumeRole(AssumeRoleRequest{RoleARN: "arn:aws:iam::123456789012:role/platform-provisioner", SessionName: "session", ExpectedAccount: "123456789012", Region: "us-east-1"}); err == nil {
		t.Fatal("wrong account was accepted")
	}

	adapter.Client = fakeSTS{output: &sts.AssumeRoleOutput{
		AssumedRoleUser: &types.AssumedRoleUser{Arn: aws.String("arn:aws:sts::123456789012:assumed-role/other-role/session")},
		Credentials:     &types.Credentials{AccessKeyId: aws.String("ASIA123"), Expiration: &expires},
	}}
	if _, err := adapter.AssumeRole(AssumeRoleRequest{RoleARN: "arn:aws:iam::123456789012:role/platform-provisioner", SessionName: "session", ExpectedAccount: "123456789012", Region: "us-east-1"}); err == nil {
		t.Fatal("role mismatch was accepted")
	}

	expired := time.Now().Add(-time.Minute)
	adapter.Client = fakeSTS{output: &sts.AssumeRoleOutput{
		AssumedRoleUser: &types.AssumedRoleUser{Arn: aws.String("arn:aws:sts::123456789012:assumed-role/platform-provisioner/session")},
		Credentials:     &types.Credentials{AccessKeyId: aws.String("ASIA123"), Expiration: &expired},
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

func (f fakeTokenProvider) Token(context.Context, RuntimeTargetIdentity) (string, error) {
	return f.token, nil
}

func TestEKSTokenAndResolverUseTrustedTargetBinding(t *testing.T) {
	provider := AWSSTSTokenProvider{PresignerFactory: func(context.Context, string, string) (STSPresigner, error) { return fakePresigner{}, nil }}
	identity := RuntimeTargetIdentity{Provider: ProviderAWS, AccountID: "123456789012", Region: "us-east-1", ClusterName: "platform", IncarnationID: "cluster-incarnation"}
	token, err := provider.Token(context.Background(), identity)
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
