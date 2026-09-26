package artifacts

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"path"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/smithy-go/transport/http"
)

var ErrArtifactExists = errors.New("artifact exists with a different digest")

type Ref struct {
	Key       string
	Digest    string
	Size      int64
	VersionID string
}

type Store struct {
	client   *s3.Client
	bucket   string
	kmsKeyID string
}

// NewS3StoreWithKMS keeps the local profile unchanged while allowing an AWS
// deployment to opt into SSE-KMS through configuration rather than code.
func NewS3StoreWithKMS(endpoint, region, bucket, kmsKeyID string) (*Store, error) {
	if region == "" || bucket == "" {
		return nil, fmt.Errorf("region and bucket are required")
	}
	loadOptions := []func(*awsconfig.LoadOptions) error{awsconfig.WithRegion(region)}
	customEndpoint := endpoint != ""
	if isLocalEndpoint(endpoint) {
		loadOptions = append(loadOptions, awsconfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider("test", "test", "")))
	}
	cfg, err := awsconfig.LoadDefaultConfig(context.Background(), loadOptions...)
	if err != nil {
		return nil, err
	}
	client := s3.NewFromConfig(cfg, func(options *s3.Options) {
		if customEndpoint {
			options.BaseEndpoint = aws.String(endpoint)
			// LocalStack's gateway requires path-style addressing. Native AWS
			// uses the SDK's standard endpoint and addressing resolution.
			options.UsePathStyle = isLocalEndpoint(endpoint)
		}
	})
	return &Store{client: client, bucket: bucket, kmsKeyID: kmsKeyID}, nil
}

func (s *Store) PutImmutable(ctx context.Context, key string, content []byte) (Ref, error) {
	if err := validateKey(key); err != nil {
		return Ref{}, err
	}
	digestValue := digest(content)
	putInput := &s3.PutObjectInput{
		Bucket:               aws.String(s.bucket),
		Key:                  aws.String(key),
		Body:                 bytes.NewReader(content),
		ContentType:          aws.String("application/octet-stream"),
		ServerSideEncryption: "AES256",
		Metadata:             map[string]string{"sha256": digestValue},
		IfNoneMatch:          aws.String("*"),
	}
	if s.kmsKeyID != "" {
		putInput.ServerSideEncryption = "aws:kms"
		putInput.SSEKMSKeyId = aws.String(s.kmsKeyID)
	}
	output, err := s.client.PutObject(ctx, putInput)
	if err == nil {
		versionID := ""
		if output.VersionId != nil {
			versionID = *output.VersionId
		}
		return Ref{Key: key, Digest: digestValue, Size: int64(len(content)), VersionID: versionID}, nil
	}
	if !isPreconditionFailed(err) {
		return Ref{}, err
	}
	existing, headErr := s.head(ctx, key)
	if headErr != nil {
		return Ref{}, headErr
	}
	if existing.Digest == digestValue {
		return existing, nil
	}
	return Ref{}, ErrArtifactExists
}

func (s *Store) GetVerified(ctx context.Context, ref Ref) ([]byte, error) {
	if err := validateKey(ref.Key); err != nil {
		return nil, err
	}
	result, err := s.client.GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(ref.Key)})
	if err != nil {
		return nil, err
	}
	defer result.Body.Close()
	content, err := io.ReadAll(result.Body)
	if err != nil {
		return nil, err
	}
	actual := digest(content)
	if actual != ref.Digest {
		return nil, fmt.Errorf("artifact digest mismatch: got %s, want %s", actual, ref.Digest)
	}
	return content, nil
}

// GetVerifiedByKey is used for deterministic terminal evidence whose digest is
// recorded by the object store metadata after the runner uploads it.
func (s *Store) GetVerifiedByKey(ctx context.Context, key string) (Ref, []byte, error) {
	ref, err := s.head(ctx, key)
	if err != nil {
		return Ref{}, nil, err
	}
	content, err := s.GetVerified(ctx, ref)
	return ref, content, err
}

func (s *Store) Delete(ctx context.Context, key string) error {
	if err := validateKey(key); err != nil {
		return err
	}
	_, err := s.client.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(key)})
	return err
}

func (s *Store) head(ctx context.Context, key string) (Ref, error) {
	result, err := s.client.HeadObject(ctx, &s3.HeadObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(key)})
	if err != nil {
		return Ref{}, err
	}
	digestValue := ""
	for metadataKey, value := range result.Metadata {
		if strings.EqualFold(metadataKey, "sha256") {
			digestValue = value
			break
		}
	}
	versionID := ""
	if result.VersionId != nil {
		versionID = *result.VersionId
	}
	return Ref{Key: key, Digest: digestValue, Size: aws.ToInt64(result.ContentLength), VersionID: versionID}, nil
}

func validateKey(key string) error {
	if key == "" || strings.HasPrefix(key, "/") || path.Clean(key) != key || strings.Contains(key, "\\") {
		return fmt.Errorf("invalid artifact key %q", key)
	}
	return nil
}

func digest(content []byte) string {
	hash := sha256.Sum256(content)
	return "sha256:" + hex.EncodeToString(hash[:])
}

func isPreconditionFailed(err error) bool {
	var responseError *http.ResponseError
	if errors.As(err, &responseError) {
		return responseError.HTTPStatusCode() == 412
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "preconditionfailed") || strings.Contains(message, "precondition failed")
}

func isLocalEndpoint(endpoint string) bool {
	return strings.Contains(endpoint, "localhost") || strings.Contains(endpoint, "127.0.0.1") || strings.Contains(endpoint, "host.docker.internal") || strings.Contains(endpoint, "localstack")
}
