package storage

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

const (
	testLocalRoot      = "/data/videos"
	testIgnoredPath    = "/ignored"
	testS3Endpoint     = "s3.example.com"
	testAlreadySlash   = "already-slash/"
)

func TestNewBackend_LocalDefault(t *testing.T) {
	var localCalled bool
	f := &BackendFactory{
		Config: StorageConfig{Backend: ""},
		NewLocal: func(root string) (Backend, error) {
			localCalled = true
			if root != testLocalRoot {
				t.Errorf("NewLocal root = %q, want /data/videos", root)
			}
			return nil, nil //nolint:nilnil // test stub; caller ignores the returned backend
		},
	}
	_, _ = f.NewBackend(context.Background(), "videos", testLocalRoot)
	if !localCalled {
		t.Error("NewLocal was not called for empty backend config")
	}
}

func TestNewBackend_LocalExplicit(t *testing.T) {
	var localCalled bool
	f := &BackendFactory{
		Config: StorageConfig{Backend: "local"},
		NewLocal: func(_ string) (Backend, error) {
			localCalled = true
			return nil, nil //nolint:nilnil // test stub; caller ignores the returned backend
		},
	}
	_, _ = f.NewBackend(context.Background(), "videos", testLocalRoot)
	if !localCalled {
		t.Error("NewLocal was not called for backend=local")
	}
}

func TestNewBackend_S3_DefaultPrefix(t *testing.T) {
	var gotPrefix string
	f := &BackendFactory{
		Config: StorageConfig{
			Backend: "s3",
			S3: S3Config{
				Endpoint:        "s3.us-east-005.backblazeb2.com",
				Region:          "us-east-005",
				AccessKeyID:     "key",
				SecretAccessKey: "secret",
				Bucket:          "mybucket",
			},
		},
		NewS3: func(_ context.Context, _, _, _, _, _, prefix string, _ bool) (Backend, error) {
			gotPrefix = prefix
			return nil, nil //nolint:nilnil // test stub; caller ignores the returned backend
		},
	}
	_, _ = f.NewBackend(context.Background(), "videos", testIgnoredPath)
	if gotPrefix != "videos/" {
		t.Errorf("default prefix = %q, want %q", gotPrefix, "videos/")
	}
}

func TestNewBackend_S3_CustomPrefix(t *testing.T) {
	var gotPrefix string
	f := &BackendFactory{
		Config: StorageConfig{
			Backend: "s3",
			S3: S3Config{
				Endpoint: testS3Endpoint,
				Bucket:   "mybucket",
				Prefixes: map[string]string{"videos": "custom-prefix"},
			},
		},
		NewS3: func(_ context.Context, _, _, _, _, _, prefix string, _ bool) (Backend, error) {
			gotPrefix = prefix
			return nil, nil //nolint:nilnil // test stub; caller ignores the returned backend
		},
	}
	_, _ = f.NewBackend(context.Background(), "videos", testIgnoredPath)
	if gotPrefix != "custom-prefix/" {
		t.Errorf("custom prefix = %q, want %q", gotPrefix, "custom-prefix/")
	}
}

func TestNewBackend_S3_CustomPrefixWithTrailingSlash(t *testing.T) {
	var gotPrefix string
	f := &BackendFactory{
		Config: StorageConfig{
			Backend: "s3",
			S3: S3Config{
				Endpoint: testS3Endpoint,
				Bucket:   "mybucket",
				Prefixes: map[string]string{"videos": testAlreadySlash},
			},
		},
		NewS3: func(_ context.Context, _, _, _, _, _, prefix string, _ bool) (Backend, error) {
			gotPrefix = prefix
			return nil, nil //nolint:nilnil // test stub; caller ignores the returned backend
		},
	}
	_, _ = f.NewBackend(context.Background(), "videos", testIgnoredPath)
	if gotPrefix != testAlreadySlash {
		t.Errorf("prefix with trailing slash = %q, want %q", gotPrefix, testAlreadySlash)
	}
}

func TestNewBackend_S3_PassesAllConfig(t *testing.T) {
	var (
		gotEndpoint, gotRegion, gotKeyID, gotSecret, gotBucket string
		gotPathStyle                                           bool
	)
	f := &BackendFactory{
		Config: StorageConfig{
			Backend: "s3",
			S3: S3Config{
				Endpoint:        testS3Endpoint,
				Region:          "us-west-2",
				AccessKeyID:     "AKID",
				SecretAccessKey: "SKEY",
				Bucket:          "testbucket",
				UsePathStyle:    true,
			},
		},
		NewS3: func(_ context.Context, endpoint, region, keyID, secret, bucket, _ string, pathStyle bool) (Backend, error) {
			gotEndpoint = endpoint
			gotRegion = region
			gotKeyID = keyID
			gotSecret = secret
			gotBucket = bucket
			gotPathStyle = pathStyle
			return nil, nil //nolint:nilnil // test stub; caller ignores the returned backend
		},
	}
	_, _ = f.NewBackend(context.Background(), "uploads", testIgnoredPath)

	if gotEndpoint != testS3Endpoint {
		t.Errorf("endpoint = %q", gotEndpoint)
	}
	if gotRegion != "us-west-2" {
		t.Errorf("region = %q", gotRegion)
	}
	if gotKeyID != "AKID" {
		t.Errorf("keyID = %q", gotKeyID)
	}
	if gotSecret != "SKEY" {
		t.Errorf("secret = %q", gotSecret)
	}
	if gotBucket != "testbucket" {
		t.Errorf("bucket = %q", gotBucket)
	}
	if !gotPathStyle {
		t.Error("pathStyle should be true")
	}
}

func TestNewBackend_UnknownBackend(t *testing.T) {
	f := &BackendFactory{
		Config: StorageConfig{Backend: "gcs"},
	}
	_, err := f.NewBackend(context.Background(), "videos", "/data")
	if err == nil {
		t.Fatal("expected error for unknown backend")
	}
	if !strings.Contains(err.Error(), `unknown backend "gcs"`) {
		t.Errorf("error = %q, want to contain 'unknown backend'", err.Error())
	}
}

func TestNewBackend_LocalError(t *testing.T) {
	f := &BackendFactory{
		Config: StorageConfig{Backend: "local"},
		NewLocal: func(_ string) (Backend, error) {
			return nil, fmt.Errorf("disk full")
		},
	}
	_, err := f.NewBackend(context.Background(), "videos", "/data")
	if err == nil {
		t.Error("expected error from NewLocal")
	}
}

func TestNewBackend_S3Error(t *testing.T) {
	f := &BackendFactory{
		Config: StorageConfig{
			Backend: "s3",
			S3:      S3Config{Endpoint: testS3Endpoint, Bucket: "b"},
		},
		NewS3: func(_ context.Context, _, _, _, _, _, _ string, _ bool) (Backend, error) {
			return nil, fmt.Errorf("connection refused")
		},
	}
	_, err := f.NewBackend(context.Background(), "videos", "/data")
	if err == nil {
		t.Error("expected error from NewS3")
	}
}
