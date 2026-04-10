package s3compat

import (
	"context"
	"testing"

	"media-server-pro/pkg/storage"
)

// ---------------------------------------------------------------------------
// Config validation in New()
// ---------------------------------------------------------------------------

func TestNew_MissingBucket(t *testing.T) {
	_, err := New(context.Background(), Config{
		Endpoint: "s3.example.com",
	})
	if err == nil {
		t.Error("expected error for empty bucket")
	}
}

func TestNew_MissingEndpoint(t *testing.T) {
	_, err := New(context.Background(), Config{
		Bucket: "mybucket",
	})
	if err == nil {
		t.Error("expected error for empty endpoint")
	}
}

func TestNew_Success(t *testing.T) {
	b, err := New(context.Background(), Config{
		Endpoint:        "s3.example.com",
		Region:          "us-east-1",
		AccessKeyID:     "AKID",
		SecretAccessKey: "secret",
		Bucket:          "testbucket",
		Prefix:          "videos",
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	// Prefix should be normalized with trailing slash
	if b.prefix != "videos/" {
		t.Errorf("prefix = %q, want %q", b.prefix, "videos/")
	}
	if b.bucket != "testbucket" {
		t.Errorf("bucket = %q, want %q", b.bucket, "testbucket")
	}
}

func TestNew_PrefixAlreadyHasSlash(t *testing.T) {
	b, err := New(context.Background(), Config{
		Endpoint: "s3.example.com",
		Bucket:   "b",
		Prefix:   "data/",
	})
	if err != nil {
		t.Fatal(err)
	}
	if b.prefix != "data/" {
		t.Errorf("prefix = %q, want %q", b.prefix, "data/")
	}
}

func TestNew_EmptyPrefix(t *testing.T) {
	b, err := New(context.Background(), Config{
		Endpoint: "s3.example.com",
		Bucket:   "b",
	})
	if err != nil {
		t.Fatal(err)
	}
	if b.prefix != "" {
		t.Errorf("prefix = %q, want empty", b.prefix)
	}
}

func TestNew_HTTPEndpoint(t *testing.T) {
	b, err := New(context.Background(), Config{
		Endpoint: "http://localhost:9000",
		Bucket:   "b",
	})
	if err != nil {
		t.Fatal(err)
	}
	if b.IsLocal() {
		t.Error("S3 backend should not be local")
	}
}

func TestNew_HTTPSEndpoint(t *testing.T) {
	b, err := New(context.Background(), Config{
		Endpoint: "https://s3.example.com",
		Bucket:   "b",
	})
	if err != nil {
		t.Fatal(err)
	}
	if b.IsLocal() {
		t.Error("S3 backend should not be local")
	}
}

// ---------------------------------------------------------------------------
// key() path resolution
// ---------------------------------------------------------------------------

func TestKey_Simple(t *testing.T) {
	b := &Backend{prefix: "videos/"}
	k, err := b.key("movie.mp4")
	if err != nil {
		t.Fatal(err)
	}
	if k != "videos/movie.mp4" {
		t.Errorf("key = %q, want %q", k, "videos/movie.mp4")
	}
}

func TestKey_Nested(t *testing.T) {
	b := &Backend{prefix: "media/"}
	k, err := b.key("sub/dir/file.mp4")
	if err != nil {
		t.Fatal(err)
	}
	if k != "media/sub/dir/file.mp4" {
		t.Errorf("key = %q, want %q", k, "media/sub/dir/file.mp4")
	}
}

func TestKey_LeadingSlash(t *testing.T) {
	b := &Backend{prefix: "data/"}
	k, err := b.key("/file.txt")
	if err != nil {
		t.Fatal(err)
	}
	if k != "data/file.txt" {
		t.Errorf("key = %q, want %q", k, "data/file.txt")
	}
}

func TestKey_DotSlash(t *testing.T) {
	b := &Backend{prefix: "data/"}
	k, err := b.key("./file.txt")
	if err != nil {
		t.Fatal(err)
	}
	if k != "data/file.txt" {
		t.Errorf("key = %q, want %q", k, "data/file.txt")
	}
}

func TestKey_EmptyAndDot(t *testing.T) {
	b := &Backend{prefix: "pfx/"}
	for _, rel := range []string{"", ".", "./"} {
		k, err := b.key(rel)
		if err != nil {
			t.Errorf("key(%q): %v", rel, err)
		}
		if k != "pfx/" {
			t.Errorf("key(%q) = %q, want %q", rel, k, "pfx/")
		}
	}
}

func TestKey_EmptyPrefix(t *testing.T) {
	b := &Backend{prefix: ""}
	k, err := b.key("file.txt")
	if err != nil {
		t.Fatal(err)
	}
	if k != "file.txt" {
		t.Errorf("key = %q, want %q", k, "file.txt")
	}
}

func TestKey_TraversalRejected(t *testing.T) {
	b := &Backend{prefix: "videos/"}
	traversals := []string{
		"..",
		"../secret",
		"../../etc/passwd",
		"foo/../../secret",
	}
	for _, rel := range traversals {
		_, err := b.key(rel)
		if err == nil {
			t.Errorf("key(%q) should reject traversal", rel)
		}
	}
}

func TestKeyPrefix(t *testing.T) {
	b := &Backend{prefix: "my-prefix/"}
	if b.KeyPrefix() != "my-prefix/" {
		t.Errorf("KeyPrefix = %q, want %q", b.KeyPrefix(), "my-prefix/")
	}
}

// ---------------------------------------------------------------------------
// IsLocal
// ---------------------------------------------------------------------------

func TestIsLocal(t *testing.T) {
	b := &Backend{}
	if b.IsLocal() {
		t.Error("S3 backend IsLocal() should return false")
	}
}

// ---------------------------------------------------------------------------
// MkdirAll is no-op
// ---------------------------------------------------------------------------

func TestMkdirAll_Noop(t *testing.T) {
	b := &Backend{}
	err := b.MkdirAll(context.Background(), "any/path")
	if err != nil {
		t.Errorf("MkdirAll should be no-op, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// AbsPath
// ---------------------------------------------------------------------------

func TestAbsPath(t *testing.T) {
	b := &Backend{prefix: "videos/"}
	got := b.AbsPath("movie.mp4")
	if got != "videos/movie.mp4" {
		t.Errorf("AbsPath = %q, want %q", got, "videos/movie.mp4")
	}
}

func TestAbsPath_Traversal(t *testing.T) {
	b := &Backend{prefix: "videos/"}
	got := b.AbsPath("../../secret")
	if got != "" {
		t.Errorf("AbsPath should return empty for traversal, got %q", got)
	}
}

// ---------------------------------------------------------------------------
// Interface compliance
// ---------------------------------------------------------------------------

func TestBackendImplementsInterface(_ *testing.T) {
	var _ storage.Backend = (*Backend)(nil)
}

func TestBackendImplementsRangeOpener(_ *testing.T) {
	var _ storage.RangeOpener = (*Backend)(nil)
}

func TestBackendImplementsPresignURLer(_ *testing.T) {
	var _ storage.PresignURLer = (*Backend)(nil)
}
