package artifacts

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/sts"
)

// Opt-in contract test against a supplied emulator. It owns one unique bucket;
// no pre-existing bucket, container or cluster is reset or removed.
func TestMiniStackIntegration(t *testing.T) {
	endpoint := os.Getenv("KPCP_TEST_MINISTACK_ENDPOINT")
	if endpoint == "" {
		t.Skip("set KPCP_TEST_MINISTACK_ENDPOINT for live MiniStack validation")
	}
	t.Setenv("AWS_ACCESS_KEY_ID", "000000000000")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "test")
	t.Setenv("AWS_SESSION_TOKEN", "")
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	bucket := fmt.Sprintf("kpcp-contract-%d", time.Now().UnixNano())
	store, err := NewS3StoreWithKMS(endpoint, "us-east-1", bucket, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.client.CreateBucket(ctx, &s3.CreateBucketInput{Bucket: aws.String(bucket)}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanup, done := context.WithTimeout(context.Background(), 15*time.Second)
		defer done()
		objects, listErr := store.client.ListObjectsV2(cleanup, &s3.ListObjectsV2Input{Bucket: aws.String(bucket)})
		if listErr != nil {
			t.Errorf("list owned bucket for cleanup: %v", listErr)
			return
		}
		for _, object := range objects.Contents {
			if _, err := store.client.DeleteObject(cleanup, &s3.DeleteObjectInput{Bucket: aws.String(bucket), Key: object.Key}); err != nil {
				t.Error(err)
			}
		}
		if _, err := store.client.DeleteBucket(cleanup, &s3.DeleteBucketInput{Bucket: aws.String(bucket)}); err != nil {
			t.Error(err)
		}
	})
	t.Run("STSIdentity", func(t *testing.T) {
		client := sts.NewFromConfig(aws.Config{Region: "us-east-1", Credentials: credentials.NewStaticCredentialsProvider("000000000000", "test", "")}, func(o *sts.Options) { o.BaseEndpoint = aws.String(endpoint) })
		identity, err := client.GetCallerIdentity(ctx, &sts.GetCallerIdentityInput{})
		if err != nil {
			t.Fatal(err)
		}
		if aws.ToString(identity.Account) != "000000000000" {
			t.Fatalf("unexpected dummy account: %q", aws.ToString(identity.Account))
		}
	})
	t.Run("ImmutableArtifacts", func(t *testing.T) {
		ref, err := store.PutImmutable(ctx, "evidence/plan", []byte("saved-plan"))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := store.GetVerified(ctx, ref); err != nil {
			t.Fatal(err)
		}
		if _, err := store.PutImmutable(ctx, ref.Key, []byte("saved-plan")); err != nil {
			t.Fatalf("same-content retry: %v", err)
		}
		if _, err := store.PutImmutable(ctx, ref.Key, []byte("changed-plan")); err == nil {
			t.Fatal("conflicting immutable write was accepted")
		}
		// Simulate storage corruption outside the application's immutable writer.
		_, err = store.client.PutObject(ctx, &s3.PutObjectInput{Bucket: aws.String(bucket), Key: aws.String(ref.Key), Body: bytes.NewReader([]byte("tampered")), Metadata: map[string]string{"sha256": ref.Digest}})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := store.GetVerified(ctx, ref); err == nil {
			t.Fatal("tampered bytes were accepted")
		}
		if _, err := store.PutImmutable(ctx, ref.Key, []byte("saved-plan")); err == nil {
			t.Fatal("metadata alone authorized corrupt content")
		}
		if _, _, err := store.GetVerifiedByKey(ctx, "missing"); !IsNotFound(err) {
			t.Fatalf("missing artifact must return identifiable 404: %v", err)
		}
	})
	t.Run("BackendLockContention", func(t *testing.T) {
		input := &s3.PutObjectInput{Bucket: aws.String(bucket), Key: aws.String("state.tflock"), Body: bytes.NewReader([]byte("owner-a")), IfNoneMatch: aws.String("*")}
		if _, err := store.client.PutObject(ctx, input); err != nil {
			t.Fatal(err)
		}
		input.Body = bytes.NewReader([]byte("owner-b"))
		if _, err := store.client.PutObject(ctx, input); err == nil || !isPreconditionFailed(err) {
			t.Fatalf("S3 backend lock requires 412 on contention: %v", err)
		}
	})
}
