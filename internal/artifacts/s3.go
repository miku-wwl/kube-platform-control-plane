package artifacts

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/smithy-go/transport/http"
	"github.com/miku-wwl/kube-platform-control-plane/internal/cloud"
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

// NewS3StoreWithKMS accepts an explicit AWS-compatible S3 endpoint. Credentials
// must be explicit dummy environment credentials; no ambient chain is used.
func NewS3StoreWithKMS(endpoint, region, bucket, kmsKeyID string) (*Store, error) {
	if region == "" || bucket == "" {
		return nil, fmt.Errorf("region and bucket are required")
	}
	if err := cloud.ValidateEmulatorEndpoint(endpoint); err != nil {
		return nil, err
	}
	account, secret, token := os.Getenv("AWS_ACCESS_KEY_ID"), os.Getenv("AWS_SECRET_ACCESS_KEY"), os.Getenv("AWS_SESSION_TOKEN")
	if err := cloud.EmulatorCredentials(account, secret, token); err != nil {
		return nil, err
	}
	cfg := aws.Config{Region: region, Credentials: credentials.NewStaticCredentialsProvider(account, secret, "")}
	client := s3.NewFromConfig(cfg, func(options *s3.Options) {
		options.BaseEndpoint = aws.String(endpoint)
		// The explicit emulator endpoint always uses path-style addressing.
		options.UsePathStyle = true
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
		if _, err := s.GetVerified(ctx, existing); err != nil {
			return Ref{}, err
		}
		return existing, nil
	}
	return Ref{}, ErrArtifactExists
}

func (s *Store) GetVerified(ctx context.Context, ref Ref) ([]byte, error) {
	if err := validateKey(ref.Key); err != nil {
		return nil, err
	}
	input := &s3.GetObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(ref.Key)}
	if ref.VersionID != "" {
		input.VersionId = aws.String(ref.VersionID)
	}
	result, err := s.client.GetObject(ctx, input)
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
	if err := validateKey(key); err != nil {
		return Ref{}, nil, err
	}
	ref, err := s.head(ctx, key)
	if err != nil {
		return Ref{}, nil, err
	}
	content, err := s.GetVerified(ctx, ref)
	return ref, content, err
}

func IsNotFound(err error) bool {
	var responseError *http.ResponseError
	return errors.As(err, &responseError) && responseError.HTTPStatusCode() == 404
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
