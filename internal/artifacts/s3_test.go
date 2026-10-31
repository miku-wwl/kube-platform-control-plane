package artifacts

import (
	"context"
	"testing"
)

func TestS3EndpointConfiguration(t *testing.T) {
	t.Setenv("AWS_ACCESS_KEY_ID", "explicit-key")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "explicit-secret")
	t.Setenv("AWS_SESSION_TOKEN", "")
	for _, endpoint := range []string{"http://127.0.0.1:4566", "http://ministack.example.invalid:4566", "https://s3.example.invalid"} {
		store, err := NewS3StoreWithKMS(endpoint, "us-east-1", "evidence", "kms-key")
		if err != nil {
			t.Fatal(err)
		}
		options := store.client.Options()
		credentials, err := options.Credentials.Retrieve(context.Background())
		if err != nil || credentials.AccessKeyID != "explicit-key" || !options.UsePathStyle {
			t.Fatal("endpoint must not select synthetic credentials or hostname-specific addressing", err)
		}
	}
	for _, endpoint := range []string{"file:///tmp/artifacts", "http://user:pass@example.invalid", "http://example.invalid?token=secret", "http://example.invalid#fragment", "missing-host"} {
		if _, err := NewS3StoreWithKMS(endpoint, "us-east-1", "evidence", ""); err == nil {
			t.Fatal("unsafe endpoint accepted", endpoint)
		}
	}
}
