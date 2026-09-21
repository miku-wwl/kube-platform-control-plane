package artifacts

import (
	"context"
	"os"
	"strings"
	"testing"
)

func TestValidateKeyRejectsTraversal(t *testing.T) {
	for _, key := range []string{"", "/absolute", "runs/../plan.binary", `runs\\plan.binary`} {
		if err := validateKey(key); err == nil {
			t.Errorf("validateKey(%q) accepted invalid key", key)
		}
	}
}

func TestNewS3StoreSupportsAWSNativeEndpointResolution(t *testing.T) {
	store, err := NewS3Store("", "us-east-1", "native-artifacts")
	if err != nil {
		t.Fatalf("NewS3Store() native error = %v", err)
	}
	if store == nil || store.bucket != "native-artifacts" {
		t.Fatalf("native store = %#v", store)
	}
}

func TestNewS3StoreKeepsCustomEndpointForLocalStack(t *testing.T) {
	store, err := NewS3Store("http://localhost:4566", "us-east-1", "local-artifacts")
	if err != nil {
		t.Fatalf("NewS3Store() custom endpoint error = %v", err)
	}
	if store == nil || store.bucket != "local-artifacts" {
		t.Fatalf("custom endpoint store = %#v", store)
	}
}

func TestLocalStackImmutableArtifactRoundTrip(t *testing.T) {
	endpoint := os.Getenv("PCP_LOCALSTACK_ENDPOINT")
	bucket := os.Getenv("PCP_ARTIFACT_BUCKET")
	if endpoint == "" || bucket == "" {
		t.Skip("set PCP_LOCALSTACK_ENDPOINT and PCP_ARTIFACT_BUCKET for LocalStack integration")
	}
	store, err := NewS3Store(endpoint, "us-east-1", bucket)
	if err != nil {
		t.Fatalf("NewS3Store() error = %v", err)
	}
	ctx := context.Background()
	key := "runs/artifact-immutability-test/payload.bin"
	defer func() { _ = store.Delete(ctx, key) }()
	first, err := store.PutImmutable(ctx, key, []byte("first"))
	if err != nil {
		t.Fatalf("first PutImmutable() error = %v", err)
	}
	same, err := store.PutImmutable(ctx, key, []byte("first"))
	if err != nil {
		t.Fatalf("idempotent PutImmutable() error = %v", err)
	}
	if same.Digest != first.Digest || same.Size != first.Size {
		t.Fatalf("idempotent ref = %+v, first = %+v", same, first)
	}
	if _, err := store.PutImmutable(ctx, key, []byte("second")); err != ErrArtifactExists {
		t.Fatalf("overwrite error = %v, want ErrArtifactExists", err)
	}
	content, err := store.GetVerified(ctx, first)
	if err != nil {
		t.Fatalf("GetVerified() error = %v", err)
	}
	if string(content) != "first" || !strings.HasPrefix(first.Digest, "sha256:") {
		t.Fatalf("content=%q ref=%+v", content, first)
	}
}
