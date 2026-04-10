package s3compat

import (
	"context"
	"testing"

	"media-server-pro/pkg/storage"
)

const (
	testS3Endpoint    = "s3.example.com"
	testVideoPrefix   = "videos/"
	testVideoMovieMp4 = "videos/movie.mp4"
	testKeyFmt        = "key = %q, want %q"
	testDataFileTxt   = "data/file.txt"
	testFileTxt       = "file.txt"
	testMyPrefix      = "my-prefix/"
)

// ---------------------------------------------------------------------------
// Config validation in New()
// ---------------------------------------------------------------------------

func TestNew_MissingBucket(t *testing.T) {
	_, err := New(context.Background(), Config{
		Endpoint: testS3Endpoint,
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
		Endpoint:        testS3Endpoint,
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
	if b.prefix != testVideoPrefix {
		t.Errorf("prefix = %q, want %q", b.prefix, testVideoPrefix)
	}
	if b.bucket != "testbucket" {
		t.Errorf("bucket = %q, want %q", b.bucket, "testbucket")
	}
}

func TestNew_PrefixAlreadyHasSlash(t *testing.T) {
	b, err := New(context.Background(), Config{
		Endpoint: testS3Endpoint,
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
		Endpoint: testS3Endpoint,
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
	b := &Backend{prefix: testVideoPrefix}
	k, err := b.key("movie.mp4")
	if err != nil {
		t.Fatal(err)
	}
	if k != testVideoMovieMp4 {
		t.Errorf(testKeyFmt, k, testVideoMovieMp4)
	}
}

func TestKey_Nested(t *testing.T) {
	b := &Backend{prefix: "media/"}
	k, err := b.key("sub/dir/file.mp4")
	if err != nil {
		t.Fatal(err)
	}
	if k != "media/sub/dir/file.mp4" {
		t.Errorf(testKeyFmt, k, "media/sub/dir/file.mp4")
	}
}

func TestKey_LeadingSlash(t *testing.T) {
	b := &Backend{prefix: "data/"}
	k, err := b.key("/file.txt")
	if err != nil {
		t.Fatal(err)
	}
	if k != testDataFileTxt {
		t.Errorf(testKeyFmt, k, testDataFileTxt)
	}
}

func TestKey_DotSlash(t *testing.T) {
	b := &Backend{prefix: "data/"}
	k, err := b.key("./file.txt")
	if err != nil {
		t.Fatal(err)
	}
	if k != testDataFileTxt {
		t.Errorf(testKeyFmt, k, testDataFileTxt)
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
	k, err := b.key(testFileTxt)
	if err != nil {
		t.Fatal(err)
	}
	if k != testFileTxt {
		t.Errorf(testKeyFmt, k, testFileTxt)
	}
}

func TestKey_TraversalRejected(t *testing.T) {
	b := &Backend{prefix: testVideoPrefix}
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
	b := &Backend{prefix: testMyPrefix}
	if b.KeyPrefix() != testMyPrefix {
		t.Errorf("KeyPrefix = %q, want %q", b.KeyPrefix(), testMyPrefix)
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
	b := &Backend{prefix: testVideoPrefix}
	got := b.AbsPath("movie.mp4")
	if got != testVideoMovieMp4 {
		t.Errorf("AbsPath = %q, want %q", got, testVideoMovieMp4)
	}
}

func TestAbsPath_Traversal(t *testing.T) {
	b := &Backend{prefix: testVideoPrefix}
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
