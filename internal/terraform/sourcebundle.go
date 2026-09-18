package terraform

import (
	"archive/tar"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/klauspost/compress/zstd"
)

type SourceBundle struct {
	Content []byte
	Digest  string
	Files   []string
}

func RestoreSourceBundle(content []byte, destination string) ([]string, error) {
	decoder, err := zstd.NewReader(bytes.NewReader(content))
	if err != nil {
		return nil, err
	}
	defer decoder.Close()
	archive := tar.NewReader(decoder)
	files := make([]string, 0)
	for {
		header, err := archive.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		if header.Typeflag != tar.TypeReg || !bundlePathAllowed(header.Name) {
			return nil, fmt.Errorf("source bundle contains unsupported entry %q", header.Name)
		}
		relative := filepath.FromSlash(filepath.Clean(header.Name))
		if relative == "." || strings.HasPrefix(relative, ".."+string(os.PathSeparator)) || filepath.IsAbs(relative) {
			return nil, fmt.Errorf("source bundle contains unsafe path %q", header.Name)
		}
		filePath := filepath.Join(destination, relative)
		if err := os.MkdirAll(filepath.Dir(filePath), 0o755); err != nil {
			return nil, err
		}
		file, err := os.OpenFile(filePath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
		if err != nil {
			return nil, err
		}
		_, copyErr := io.Copy(file, archive)
		closeErr := file.Close()
		if copyErr != nil {
			return nil, copyErr
		}
		if closeErr != nil {
			return nil, closeErr
		}
		files = append(files, filepath.ToSlash(relative))
	}
	sort.Strings(files)
	return files, nil
}

func BuildSourceBundle(root string) (SourceBundle, error) {
	entries := make([]string, 0)
	if err := filepath.WalkDir(root, func(pathName string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(root, pathName)
		if err != nil {
			return err
		}
		if relative == "." {
			return nil
		}
		relative = filepath.ToSlash(relative)
		if entry.IsDir() {
			if relative == ".terraform" || strings.HasPrefix(relative, ".terraform/") {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return nil
		}
		if entry.Type().IsRegular() && bundlePathAllowed(relative) {
			entries = append(entries, relative)
		}
		return nil
	}); err != nil {
		return SourceBundle{}, err
	}
	sort.Strings(entries)

	var compressed bytes.Buffer
	encoder, err := zstd.NewWriter(&compressed, zstd.WithEncoderCRC(true), zstd.WithZeroFrames(true))
	if err != nil {
		return SourceBundle{}, err
	}
	archive := tar.NewWriter(encoder)
	for _, relative := range entries {
		content, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(relative)))
		if err != nil {
			_ = archive.Close()
			_ = encoder.Close()
			return SourceBundle{}, err
		}
		header := &tar.Header{
			Name: relative,
			Mode: 0o644,
			Size: int64(len(content)),
		}
		if err := archive.WriteHeader(header); err != nil {
			_ = archive.Close()
			_ = encoder.Close()
			return SourceBundle{}, err
		}
		if _, err := archive.Write(content); err != nil {
			_ = archive.Close()
			_ = encoder.Close()
			return SourceBundle{}, err
		}
	}
	if err := archive.Close(); err != nil {
		_ = encoder.Close()
		return SourceBundle{}, err
	}
	if err := encoder.Close(); err != nil {
		return SourceBundle{}, err
	}
	content := compressed.Bytes()
	hash := sha256.Sum256(content)
	return SourceBundle{Content: content, Digest: "sha256:" + hex.EncodeToString(hash[:]), Files: entries}, nil
}

func bundlePathAllowed(relative string) bool {
	clean := filepath.ToSlash(filepath.Clean(relative))
	if clean == "." || strings.HasPrefix(clean, "../") || strings.HasPrefix(clean, "/") {
		return false
	}
	for _, part := range strings.Split(clean, "/") {
		if part == ".terraform" {
			return false
		}
	}
	base := strings.ToLower(filepath.Base(clean))
	switch {
	case strings.HasSuffix(base, ".tfstate"), strings.HasSuffix(base, ".tfstate.backup"):
		return false
	case strings.HasSuffix(base, "_override.tf"), strings.HasSuffix(base, "_override.tf.json"):
		return false
	case strings.HasSuffix(base, ".binary"), strings.HasSuffix(base, ".tfplan"):
		return false
	case base == "backend.hcl", base == "backend-config.json", base == "credentials", base == "credentials.json":
		return false
	case strings.HasSuffix(base, ".tfvars"), strings.HasSuffix(base, ".tfvars.json"):
		return false
	}
	return true
}

func (s SourceBundle) Validate() error {
	if len(s.Content) == 0 || s.Digest == "" {
		return fmt.Errorf("source bundle content and digest are required")
	}
	return nil
}
