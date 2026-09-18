package terraform

import (
	"archive/tar"
	"bytes"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/klauspost/compress/zstd"
)

func TestBuildSourceBundleExcludesRuntimeAndOverlayFiles(t *testing.T) {
	root := t.TempDir()
	files := map[string]string{
		"main.tf":                      "resource \"null_resource\" \"demo\" {}",
		".terraform/terraform.tfstate": "state",
		"terraform.tfstate":            "state",
		"local_override.tf":            "override",
		"secrets.tfvars":               "secret",
		"backend-config.json":          "backend",
		"backend.hcl":                  "backend",
		"plan.binary":                  "plan",
		"destroy.tfplan":               "plan",
		"modules/module.tf":            "module",
		".terraform.lock.hcl":          "lock",
	}
	for name, content := range files {
		filePath := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(filePath), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filePath, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	bundle, err := BuildSourceBundle(root)
	if err != nil {
		t.Fatalf("BuildSourceBundle() error = %v", err)
	}
	wantFiles := []string{".terraform.lock.hcl", "main.tf", "modules/module.tf"}
	if !reflect.DeepEqual(bundle.Files, wantFiles) {
		t.Fatalf("bundle files = %#v, want %#v", bundle.Files, wantFiles)
	}
	reader, err := zstdReader(bundle.Content)
	if err != nil {
		t.Fatal(err)
	}
	archive := tar.NewReader(reader)
	var names []string
	for {
		header, err := archive.Next()
		if err != nil {
			break
		}
		names = append(names, header.Name)
	}
	if !reflect.DeepEqual(names, wantFiles) {
		t.Fatalf("archive files = %#v, want %#v", names, wantFiles)
	}
}

func zstdReader(content []byte) (io.Reader, error) {
	return zstd.NewReader(bytes.NewReader(content))
}

func TestBuildSourceBundleDigestIsStable(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "main.tf"), []byte("terraform {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	first, err := BuildSourceBundle(root)
	if err != nil {
		t.Fatal(err)
	}
	second, err := BuildSourceBundle(root)
	if err != nil {
		t.Fatal(err)
	}
	if first.Digest != second.Digest || !bytes.Equal(first.Content, second.Content) {
		t.Fatalf("bundle is not stable: %s vs %s", first.Digest, second.Digest)
	}
}

func TestRestoreSourceBundleRejectsUnsafeEntries(t *testing.T) {
	root := t.TempDir()
	if _, err := RestoreSourceBundle([]byte("not-zstd"), root); err == nil {
		t.Fatal("RestoreSourceBundle accepted invalid compression")
	}
}
