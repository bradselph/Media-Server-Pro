package storage

import (
	"context"
	"fmt"
)

// S3Config holds S3-compatible storage settings.
type S3Config struct {
	Endpoint        string            `json:"endpoint"`
	Region          string            `json:"region"`
	AccessKeyID     string            `json:"access_key_id"`
	SecretAccessKey string            `json:"secret_access_key"`
	Bucket          string            `json:"bucket"`
	UsePathStyle    bool              `json:"use_path_style"`
	Prefixes        map[string]string `json:"prefixes"`
}

// StorageConfig selects and configures the storage backend.
type StorageConfig struct {
	Backend string   `json:"backend"` // "local" (default) or "s3"
	S3      S3Config `json:"s3"`
}

// BackendFactory creates storage backends from configuration.
// It is defined as a type so that cmd/server/main.go can use it without
// importing the concrete backend packages directly.
type BackendFactory struct {
	Config   StorageConfig
	NewLocal func(root string) (Backend, error)
	NewS3    func(ctx context.Context, endpoint, region, keyID, secret, bucket, prefix string, pathStyle bool) (Backend, error)
}

// NewBackend creates a Backend for the given directory role.
// For "local" backend, it uses localRoot as the filesystem path.
// For "s3" backend, it uses the S3 config with the appropriate prefix.
func (f *BackendFactory) NewBackend(ctx context.Context, role, localRoot string) (Backend, error) {
	switch f.Config.Backend {
	case "", "local":
		return f.NewLocal(localRoot)
	case "s3":
		prefix := role + "/"
		if p, ok := f.Config.S3.Prefixes[role]; ok && p != "" {
			prefix = p
			if prefix[len(prefix)-1] != '/' {
				prefix += "/"
			}
		}
		return f.NewS3(ctx,
			f.Config.S3.Endpoint,
			f.Config.S3.Region,
			f.Config.S3.AccessKeyID,
			f.Config.S3.SecretAccessKey,
			f.Config.S3.Bucket,
			prefix,
			f.Config.S3.UsePathStyle,
		)
	default:
		return nil, fmt.Errorf("storage: unknown backend %q (expected \"local\" or \"s3\")", f.Config.Backend)
	}
}
