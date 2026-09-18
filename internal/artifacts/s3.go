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

	"github.com/aws/aws-sdk-go/aws"
	awserr "github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/aws/request"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/aws/aws-sdk-go/service/s3/s3iface"
)

var ErrArtifactExists = errors.New("artifact exists with a different digest")

type Ref struct {
	Key       string
	Digest    string
	Size      int64
	VersionID string
}

type Store struct {
	client s3iface.S3API
	bucket string
}

func NewS3Store(endpoint, region, bucket string) (*Store, error) {
	if endpoint == "" || region == "" || bucket == "" {
		return nil, fmt.Errorf("endpoint, region, and bucket are required")
	}
	sess, err := session.NewSession(&aws.Config{
		Endpoint:         aws.String(endpoint),
		Region:           aws.String(region),
		S3ForcePathStyle: aws.Bool(true),
	})
	if err != nil {
		return nil, err
	}
	return &Store{client: s3.New(sess), bucket: bucket}, nil
}

func NewStore(client s3iface.S3API, bucket string) *Store {
	return &Store{client: client, bucket: bucket}
}

func (s *Store) PutImmutable(ctx context.Context, key string, content []byte) (Ref, error) {
	if err := validateKey(key); err != nil {
		return Ref{}, err
	}
	digest := digest(content)
	input := &s3.PutObjectInput{
		Bucket:               aws.String(s.bucket),
		Key:                  aws.String(key),
		Body:                 bytes.NewReader(content),
		ContentType:          aws.String("application/octet-stream"),
		ServerSideEncryption: aws.String("AES256"),
		Metadata:             map[string]*string{"sha256": aws.String(digest)},
	}
	req, output := s.client.PutObjectRequest(input)
	req.SetContext(ctx)
	req.Handlers.Build.PushBack(func(r *request.Request) {
		r.HTTPRequest.Header.Set("If-None-Match", "*")
	})
	err := req.Send()
	if err == nil {
		versionID := ""
		if output.VersionId != nil {
			versionID = *output.VersionId
		}
		return Ref{Key: key, Digest: digest, Size: int64(len(content)), VersionID: versionID}, nil
	}
	if !isPreconditionFailed(err) {
		return Ref{}, err
	}
	existing, headErr := s.head(ctx, key)
	if headErr != nil {
		return Ref{}, headErr
	}
	if existing.Digest == digest {
		return existing, nil
	}
	return Ref{}, ErrArtifactExists
}

func (s *Store) GetVerified(ctx context.Context, ref Ref) ([]byte, error) {
	if err := validateKey(ref.Key); err != nil {
		return nil, err
	}
	result, err := s.client.GetObjectWithContext(ctx, &s3.GetObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(ref.Key)})
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

func (s *Store) Delete(ctx context.Context, key string) error {
	if err := validateKey(key); err != nil {
		return err
	}
	_, err := s.client.DeleteObjectWithContext(ctx, &s3.DeleteObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(key)})
	return err
}

func (s *Store) head(ctx context.Context, key string) (Ref, error) {
	result, err := s.client.HeadObjectWithContext(ctx, &s3.HeadObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(key)})
	if err != nil {
		return Ref{}, err
	}
	digestValue := ""
	for key, value := range result.Metadata {
		if strings.EqualFold(key, "sha256") && value != nil {
			digestValue = *value
			break
		}
	}
	versionID := ""
	if result.VersionId != nil {
		versionID = *result.VersionId
	}
	return Ref{Key: key, Digest: digestValue, Size: aws.Int64Value(result.ContentLength), VersionID: versionID}, nil
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
	if requestFailure, ok := err.(awserr.RequestFailure); ok {
		return requestFailure.StatusCode() == 412 || requestFailure.Code() == "PreconditionFailed"
	}
	return false
}
