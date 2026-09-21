package target

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/eks"
	"github.com/aws/aws-sdk-go-v2/service/sts"
)

type STSAssumeRoleAPI interface {
	AssumeRole(context.Context, *sts.AssumeRoleInput, ...func(*sts.Options)) (*sts.AssumeRoleOutput, error)
}

// RealAWSAssumeRole is production-capable but injectable. It uses the AWS SDK
// credential chain supplied by the caller; it never stores long-lived keys.
type RealAWSAssumeRole struct {
	Client STSAssumeRoleAPI
	Now    func() time.Time
}

func NewRealAWSAssumeRole(ctx context.Context, region string) (*RealAWSAssumeRole, error) {
	cfg, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion(region))
	if err != nil {
		return nil, err
	}
	return &RealAWSAssumeRole{Client: sts.NewFromConfig(cfg), Now: time.Now}, nil
}

func (r RealAWSAssumeRole) AssumeRole(request AssumeRoleRequest) (AssumedSession, error) {
	if r.Client == nil {
		return AssumedSession{}, fmt.Errorf("AWS STS client is not configured")
	}
	if request.RoleARN == "" || request.SessionName == "" || request.ExpectedAccount == "" || request.Region == "" {
		return AssumedSession{}, fmt.Errorf("AWS AssumeRole requires role ARN, session name, expected account, and region")
	}
	expectedRoleName := roleNameFromARN(request.RoleARN)
	if expectedRoleName == "" {
		return AssumedSession{}, fmt.Errorf("AWS AssumeRole requires a valid IAM role ARN")
	}
	output, err := r.Client.AssumeRole(context.Background(), &sts.AssumeRoleInput{
		RoleArn:         aws.String(request.RoleARN),
		RoleSessionName: aws.String(request.SessionName),
		ExternalId:      optionalString(request.ExternalID),
	})
	if err != nil {
		return AssumedSession{}, fmt.Errorf("AWS AssumeRole: %w", err)
	}
	if output == nil || output.Credentials == nil || output.AssumedRoleUser == nil || output.AssumedRoleUser.Arn == nil {
		return AssumedSession{}, fmt.Errorf("AWS AssumeRole returned incomplete session")
	}
	roleARN := aws.ToString(output.AssumedRoleUser.Arn)
	actualRoleName := roleNameFromARN(roleARN)
	if actualRoleName == "" {
		return AssumedSession{}, fmt.Errorf("AWS AssumeRole returned an invalid assumed role ARN")
	}
	accountID, err := accountFromARN(roleARN)
	if err != nil {
		return AssumedSession{}, err
	}
	if accountID != request.ExpectedAccount {
		return AssumedSession{}, fmt.Errorf("AWS AssumeRole account mismatch: got %q, want %q", accountID, request.ExpectedAccount)
	}
	if actualRoleName != expectedRoleName {
		return AssumedSession{}, fmt.Errorf("AWS AssumeRole role mismatch: got %q, want %q", actualRoleName, expectedRoleName)
	}
	if aws.ToString(output.Credentials.AccessKeyId) == "" || aws.ToString(output.Credentials.SecretAccessKey) == "" || aws.ToString(output.Credentials.SessionToken) == "" || output.Credentials.Expiration == nil {
		return AssumedSession{}, fmt.Errorf("AWS AssumeRole returned incomplete credentials")
	}
	now := time.Now
	if r.Now != nil {
		now = r.Now
	}
	if !output.Credentials.Expiration.After(now()) {
		return AssumedSession{}, fmt.Errorf("AWS AssumeRole returned expired credentials")
	}
	return AssumedSession{
		AccountID:       accountID,
		RoleARN:         roleARN,
		Region:          request.Region,
		AccessKeyID:     aws.ToString(output.Credentials.AccessKeyId),
		SecretAccessKey: aws.ToString(output.Credentials.SecretAccessKey),
		SessionToken:    aws.ToString(output.Credentials.SessionToken),
		TokenID:         aws.ToString(output.Credentials.AccessKeyId),
		ExpiresAt:       aws.ToTime(output.Credentials.Expiration),
	}, nil
}

func optionalString(value string) *string {
	if value == "" {
		return nil
	}
	return aws.String(value)
}

func accountFromARN(value string) (string, error) {
	parts, err := splitARN(value)
	if err != nil {
		return "", fmt.Errorf("invalid AWS identity ARN %q", value)
	}
	return parts[4], nil
}

func roleNameFromARN(value string) string {
	parts, err := splitARN(value)
	if err != nil {
		return ""
	}
	resourceParts := strings.Split(parts[5], "/")
	if len(resourceParts) < 2 {
		return ""
	}
	switch resourceParts[0] {
	case "role":
		return resourceParts[len(resourceParts)-1]
	case "assumed-role":
		if len(resourceParts) < 3 {
			return resourceParts[1]
		}
		return resourceParts[len(resourceParts)-2]
	}
	return ""
}

func splitARN(value string) ([]string, error) {
	parts := strings.Split(value, ":")
	if len(parts) != 6 || parts[0] != "arn" || parts[1] == "" || parts[2] == "" || parts[4] == "" || parts[5] == "" {
		return nil, fmt.Errorf("invalid ARN")
	}
	return parts, nil
}

type EKSDescribeAPI interface {
	DescribeCluster(context.Context, *eks.DescribeClusterInput, ...func(*eks.Options)) (*eks.DescribeClusterOutput, error)
}

type EKSClientFactory func(context.Context, string) (EKSDescribeAPI, error)

// AWSEKSVerifier verifies Terraform discovery against DescribeCluster before
// any ResourceSet mutation is admitted.
type AWSEKSVerifier struct {
	ClientFactory EKSClientFactory
}

func NewAWSEKSVerifier() *AWSEKSVerifier {
	return &AWSEKSVerifier{ClientFactory: func(ctx context.Context, region string) (EKSDescribeAPI, error) {
		cfg, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion(region))
		if err != nil {
			return nil, err
		}
		return eks.NewFromConfig(cfg), nil
	}}
}

func (v *AWSEKSVerifier) Verify(ctx context.Context, identity RuntimeTargetIdentity, profile TargetConnectionProfile) error {
	if identity.Provider != ProviderAWS {
		return fmt.Errorf("AWS EKS verifier requires AWS provider")
	}
	if err := profile.Validate(); err != nil {
		return err
	}
	if v == nil || v.ClientFactory == nil {
		return fmt.Errorf("AWS EKS verifier client factory is not configured")
	}
	name := identity.ClusterName
	if name == "" {
		name = clusterNameFromARN(identity.ClusterARN)
	}
	if name == "" {
		return fmt.Errorf("AWS EKS verification requires cluster name or ARN")
	}
	client, err := v.ClientFactory(ctx, identity.Region)
	if err != nil {
		return fmt.Errorf("create AWS EKS client: %w", err)
	}
	output, err := client.DescribeCluster(ctx, &eks.DescribeClusterInput{Name: aws.String(name)})
	if err != nil {
		return fmt.Errorf("DescribeCluster %q: %w", name, err)
	}
	if output == nil || output.Cluster == nil {
		return fmt.Errorf("DescribeCluster %q returned no cluster", name)
	}
	cluster := output.Cluster
	if aws.ToString(cluster.Name) != name {
		return fmt.Errorf("EKS cluster name mismatch: got %q, want %q", aws.ToString(cluster.Name), name)
	}
	if identity.ClusterARN != "" && aws.ToString(cluster.Arn) != identity.ClusterARN {
		return fmt.Errorf("EKS cluster ARN mismatch: got %q, want %q", aws.ToString(cluster.Arn), identity.ClusterARN)
	}
	if account, accountErr := accountFromARN(aws.ToString(cluster.Arn)); accountErr != nil || account != identity.AccountID {
		return fmt.Errorf("EKS cluster account mismatch: got %q, want %q", account, identity.AccountID)
	}
	if aws.ToString(cluster.Endpoint) != profile.Endpoint {
		return fmt.Errorf("EKS endpoint mismatch: got %q, want %q", aws.ToString(cluster.Endpoint), profile.Endpoint)
	}
	if cluster.CertificateAuthority == nil || aws.ToString(cluster.CertificateAuthority.Data) != profile.CACertificateData {
		return fmt.Errorf("EKS certificate authority mismatch")
	}
	if profile.CACertificateDigest != "" && profile.CACertificateDigest != certificateDigest(profile.CACertificateData) {
		return fmt.Errorf("EKS certificate authority digest mismatch")
	}
	if identity.IncarnationID != "" && identity.IncarnationID != aws.ToString(cluster.Arn) {
		return fmt.Errorf("EKS cluster incarnation mismatch: got %q, want %q", aws.ToString(cluster.Arn), identity.IncarnationID)
	}
	return nil
}

func clusterNameFromARN(value string) string {
	parts := strings.Split(value, "/")
	if len(parts) == 0 {
		return ""
	}
	return parts[len(parts)-1]
}

func certificateDigest(data string) string {
	sum := sha256.Sum256([]byte(data))
	return "sha256:" + hex.EncodeToString(sum[:])
}

var _ EKSDescribeAPI = (*eks.Client)(nil)

// DecodeEKSCA is shared by the EKS client resolver and tests. EKS returns
// certificate-authority data as base64; accepting raw bytes keeps the fake
// adapter convenient without changing production semantics.
func DecodeEKSCA(data string) ([]byte, error) {
	decoded, err := base64.StdEncoding.DecodeString(data)
	if err == nil {
		return decoded, nil
	}
	return []byte(data), nil
}
