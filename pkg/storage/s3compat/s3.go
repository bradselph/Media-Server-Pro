// Package s3compat implements the storage.Backend interface for S3-compatible
// services (Backblaze B2, AWS S3, MinIO, Cloudflare R2, Wasabi, etc.) using
// the minio-go client.  Key advantages over the AWS SDK:
//   - PutObject with size=-1 automatically uses chunked/multipart upload,
//     so large video files are streamed without buffering in memory.
//   - GetObject returns a *minio.Object that is a true io.ReadSeekCloser —
//     no need to buffer the entire file for Open().
//   - Excellent Backblaze B2 S3-API compatibility.
package s3compat

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"path"
	"strings"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"

	"media-server-pro/pkg/storage"
)

// Config holds S3-compatible storage settings.
type Config struct {
	Endpoint        string
	Region          string
	AccessKeyID     string
	SecretAccessKey string
	Bucket          string
	Prefix          string // key prefix within the bucket (e.g. "videos/")
	UsePathStyle    bool   // true for Backblaze B2 and MinIO; false for AWS virtual-hosted
}

// Backend stores files in an S3-compatible bucket under a key prefix.
type Backend struct {
	client *minio.Client
	bucket string
	prefix string // normalised: always ends with "/" or is empty
}

// New creates an S3-compatible storage backend.
// The endpoint may include or omit the scheme; HTTPS is used unless the endpoint
// explicitly starts with "http://".
func New(_ context.Context, cfg Config) (*Backend, error) {
	if cfg.Bucket == "" {
		return nil, fmt.Errorf("s3 storage: bucket name is required")
	}
	if cfg.Endpoint == "" {
		return nil, fmt.Errorf("s3 storage: endpoint is required")
	}

	// Strip scheme — minio.New takes the bare host[:port].
	secure := true
	endpoint := cfg.Endpoint
	if strings.HasPrefix(endpoint, "http://") {
		endpoint = strings.TrimPrefix(endpoint, "http://")
		secure = false
	} else {
		endpoint = strings.TrimPrefix(endpoint, "https://")
	}

	client, err := minio.New(endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.AccessKeyID, cfg.SecretAccessKey, ""),
		Secure: secure,
		Region: cfg.Region,
	})
	if err != nil {
		return nil, fmt.Errorf("s3 storage: create client: %w", err)
	}

	prefix := cfg.Prefix
	if prefix != "" && !strings.HasSuffix(prefix, "/") {
		prefix += "/"
	}

	return &Backend{
		client: client,
		bucket: cfg.Bucket,
		prefix: prefix,
	}, nil
}

// key returns the full S3 object key for a relative path.
// Returns an error if the cleaned key would escape the configured prefix via
// ".." components (e.g. "../../secrets/admin.json").
func (b *Backend) key(rel string) (string, error) {
	cleaned := path.Clean(rel)
	if cleaned == "." || cleaned == "" {
		return b.prefix, nil
	}
	// Strip leading slash or dot-slash so callers can pass either form.
	cleaned = strings.TrimPrefix(cleaned, "/")
	cleaned = strings.TrimPrefix(cleaned, "./")
	// Reject traversal attempts — path.Clean does not strip ".." from relative paths.
	if cleaned == ".." || strings.HasPrefix(cleaned, "../") || strings.Contains(cleaned, "/../") {
		return "", fmt.Errorf("s3 storage: key %q escapes prefix boundary", rel)
	}
	return b.prefix + cleaned, nil
}

// KeyPrefix returns the configured key prefix for this backend (e.g. "videos/").
// Used by storeFor in the media module to match S3 key paths to their owner backend.
func (b *Backend) KeyPrefix() string { return b.prefix }

// Open returns a seekable reader for the object.
// The returned *minio.Object is a true io.ReadSeekCloser — no memory buffering.
// The caller must close the returned reader.
func (b *Backend) Open(ctx context.Context, p string) (storage.ReadSeekCloser, error) {
	k, err := b.key(p)
	if err != nil {
		return nil, err
	}
	obj, err := b.client.GetObject(ctx, b.bucket, k, minio.GetObjectOptions{})
	if err != nil {
		return nil, b.mapError(err)
	}
	// Trigger the first network request so we can surface 404s immediately.
	if _, err := obj.Stat(); err != nil {
		_ = obj.Close()
		return nil, b.mapError(err)
	}
	return obj, nil
}

// OpenRange returns a reader for a byte range of the object.
// Implements storage.RangeOpener — used by the streaming module for HTTP 206.
func (b *Backend) OpenRange(ctx context.Context, p string, start, end int64) (io.ReadCloser, error) {
	k, err := b.key(p)
	if err != nil {
		return nil, err
	}
	opts := minio.GetObjectOptions{}
	if rangeErr := opts.SetRange(start, end); rangeErr != nil {
		return nil, fmt.Errorf("s3 storage: set range: %w", rangeErr)
	}
	obj, err := b.client.GetObject(ctx, b.bucket, k, opts)
	if err != nil {
		return nil, b.mapError(err)
	}
	// Surface 404s before returning — otherwise they appear mid-stream as a
	// truncated HTTP response body with no error status code.
	if _, err := obj.Stat(); err != nil {
		_ = obj.Close()
		return nil, b.mapError(err)
	}
	return obj, nil
}

// Stat returns object metadata without downloading the body.
func (b *Backend) Stat(ctx context.Context, p string) (*storage.FileInfo, error) {
	k, err := b.key(p)
	if err != nil {
		return nil, err
	}
	// Try as a regular object first.
	info, err := b.client.StatObject(ctx, b.bucket, k, minio.StatObjectOptions{})
	if err == nil {
		return &storage.FileInfo{
			Name:    path.Base(k),
			Size:    info.Size,
			ModTime: info.LastModified,
			IsDir:   false,
		}, nil
	}

	// If not found as a file, check whether it exists as a "directory" prefix.
	errResp := minio.ToErrorResponse(err)
	if errResp.Code == "NoSuchKey" || errResp.StatusCode == 404 {
		prefix := k
		if !strings.HasSuffix(prefix, "/") {
			prefix += "/"
		}
		for obj := range b.client.ListObjects(ctx, b.bucket, minio.ListObjectsOptions{
			Prefix:  prefix,
			MaxKeys: 1,
		}) {
			if obj.Err == nil {
				return &storage.FileInfo{Name: path.Base(k), IsDir: true}, nil
			}
		}
		return nil, storage.ErrNotFound
	}

	return nil, err
}

// Walk lists all objects under prefix and calls fn for each.
func (b *Backend) Walk(ctx context.Context, prefix string, fn storage.WalkFunc) error {
	s3Prefix, err := b.key(prefix)
	if err != nil {
		return err
	}
	if s3Prefix != "" && !strings.HasSuffix(s3Prefix, "/") {
		s3Prefix += "/"
	}

	for obj := range b.client.ListObjects(ctx, b.bucket, minio.ListObjectsOptions{
		Prefix:    s3Prefix,
		Recursive: true,
	}) {
		if obj.Err != nil {
			return fmt.Errorf("s3 storage: list objects: %w", obj.Err)
		}
		// Make path relative to backend prefix.
		rel := strings.TrimPrefix(obj.Key, b.prefix)
		if rel == "" {
			continue
		}
		info := storage.FileInfo{
			Name:    path.Base(rel),
			Size:    obj.Size,
			ModTime: obj.LastModified,
			IsDir:   strings.HasSuffix(rel, "/"),
		}
		if err := fn(rel, info, nil); err != nil {
			return err
		}
	}
	return nil
}

// ReadDir lists the immediate children of a "directory" prefix.
func (b *Backend) ReadDir(ctx context.Context, p string) ([]storage.FileInfo, error) {
	prefix, err := b.key(p)
	if err != nil {
		return nil, err
	}
	if prefix != "" && !strings.HasSuffix(prefix, "/") {
		prefix += "/"
	}

	var result []storage.FileInfo

	for obj := range b.client.ListObjects(ctx, b.bucket, minio.ListObjectsOptions{
		Prefix:    prefix,
		Recursive: false, // delimiter "/" is implied when Recursive=false
	}) {
		if obj.Err != nil {
			return nil, fmt.Errorf("s3 storage: read dir: %w", obj.Err)
		}

		// When Recursive=false the client returns common-prefix entries for
		// "directories" — these have a trailing slash.
		if strings.HasSuffix(obj.Key, "/") {
			name := strings.TrimSuffix(strings.TrimPrefix(obj.Key, prefix), "/")
			if name != "" {
				result = append(result, storage.FileInfo{Name: name, IsDir: true})
			}
			continue
		}

		name := strings.TrimPrefix(obj.Key, prefix)
		if name == "" || strings.Contains(name, "/") {
			continue // skip unexpected nested paths
		}
		result = append(result, storage.FileInfo{
			Name:    name,
			Size:    obj.Size,
			ModTime: obj.LastModified,
		})
	}

	return result, nil
}

// ReadFile downloads an entire object into memory.
func (b *Backend) ReadFile(ctx context.Context, p string) ([]byte, error) {
	k, err := b.key(p)
	if err != nil {
		return nil, err
	}
	obj, err := b.client.GetObject(ctx, b.bucket, k, minio.GetObjectOptions{})
	if err != nil {
		return nil, b.mapError(err)
	}
	defer func() { _ = obj.Close() }()
	data, err := io.ReadAll(obj)
	if err != nil {
		return nil, b.mapError(err)
	}
	return data, nil
}

// Create writes data from r to an S3 object.
// Passing size=-1 triggers automatic chunked/multipart upload in minio-go,
// so large video files are streamed without buffering the entire body.
// Returns the number of bytes written.
func (b *Backend) Create(ctx context.Context, p string, r io.Reader) (int64, error) {
	k, err := b.key(p)
	if err != nil {
		return 0, err
	}
	info, err := b.client.PutObject(ctx, b.bucket, k, r, -1,
		minio.PutObjectOptions{
			// Instruct B2/S3 to compute the checksum server-side.
			SendContentMd5: true,
		},
	)
	if err != nil {
		return 0, fmt.Errorf("s3 storage: put object: %w", err)
	}
	return info.Size, nil
}

// MkdirAll is a no-op for S3 (no real directories).
func (b *Backend) MkdirAll(_ context.Context, _ string) error { return nil }

// Remove deletes a single object.
func (b *Backend) Remove(ctx context.Context, p string) error {
	k, err := b.key(p)
	if err != nil {
		return err
	}
	return b.mapError(b.client.RemoveObject(ctx, b.bucket, k, minio.RemoveObjectOptions{}))
}

// RemoveAll deletes all objects under a prefix (recursive).
// Keys are collected synchronously before deletion so that listing errors are
// surfaced immediately rather than being swallowed by a goroutine.
func (b *Backend) RemoveAll(ctx context.Context, p string) error {
	prefix, err := b.key(p)
	if err != nil {
		return err
	}

	// First try to delete as a single object (non-directory path).
	_ = b.client.RemoveObject(ctx, b.bucket, prefix, minio.RemoveObjectOptions{})

	// Then delete everything under the directory prefix.
	if !strings.HasSuffix(prefix, "/") {
		prefix += "/"
	}

	// Collect all keys first so that any listing error is returned before we
	// begin deleting, and so the goroutine feeding RemoveObjects cannot silently
	// drop a listing error mid-walk.
	var toDelete []minio.ObjectInfo
	for obj := range b.client.ListObjects(ctx, b.bucket, minio.ListObjectsOptions{
		Prefix:    prefix,
		Recursive: true,
	}) {
		if obj.Err != nil {
			return fmt.Errorf("s3 storage: list for delete: %w", obj.Err)
		}
		toDelete = append(toDelete, obj)
	}
	if len(toDelete) == 0 {
		return nil
	}

	objectsCh := make(chan minio.ObjectInfo, len(toDelete))
	for _, o := range toDelete {
		objectsCh <- o
	}
	close(objectsCh)

	for rErr := range b.client.RemoveObjects(ctx, b.bucket, objectsCh, minio.RemoveObjectsOptions{}) {
		if rErr.Err != nil {
			return fmt.Errorf("s3 storage: batch delete: %w", rErr.Err)
		}
	}
	return nil
}

// Rename copies src to dst then deletes src. S3 has no native rename.
// For objects larger than 5 GB (Backblaze B2's single-part copy limit),
// CopyObject returns EntityTooLarge; we fall back to a streaming download +
// re-upload so that large video files can always be renamed.
//
// NOTE: S3 has no atomic rename. This is a copy+delete operation. If the delete
// fails after a successful copy, the file exists at both src and dst. Callers
// should handle partial rename errors (dst exists, src still exists) gracefully.
func (b *Backend) Rename(ctx context.Context, src, dst string) error {
	srcKey, err := b.key(src)
	if err != nil {
		return err
	}
	dstKey, err := b.key(dst)
	if err != nil {
		return err
	}

	_, err = b.client.CopyObject(ctx,
		minio.CopyDestOptions{Bucket: b.bucket, Object: dstKey},
		minio.CopySrcOptions{Bucket: b.bucket, Object: srcKey},
	)
	if err != nil {
		// Fall back to streaming copy for objects >5 GB (B2 CopyObject limit).
		resp := minio.ToErrorResponse(err)
		if resp.Code == "EntityTooLarge" || resp.StatusCode == 413 {
			if streamErr := b.streamCopy(ctx, srcKey, dstKey); streamErr != nil {
				// Clean up any partial object at dstKey to avoid leaving orphaned data.
				_ = b.client.RemoveObject(ctx, b.bucket, dstKey, minio.RemoveObjectOptions{})
				return fmt.Errorf("s3 storage: stream copy for rename: %w", streamErr)
			}
		} else {
			return fmt.Errorf("s3 storage: copy for rename: %w", err)
		}
	}

	if err := b.client.RemoveObject(ctx, b.bucket, srcKey, minio.RemoveObjectOptions{}); err != nil {
		return fmt.Errorf("s3 storage: delete after rename: %w", err)
	}
	return nil
}

// streamCopy downloads srcKey and re-uploads it as dstKey using PutObject with
// automatic multipart.  Used as a fallback for objects larger than 5 GB where
// CopyObject is not supported by B2's S3-compatible API.
func (b *Backend) streamCopy(ctx context.Context, srcKey, dstKey string) error {
	obj, err := b.client.GetObject(ctx, b.bucket, srcKey, minio.GetObjectOptions{})
	if err != nil {
		return fmt.Errorf("get source: %w", err)
	}
	defer func() { _ = obj.Close() }()

	stat, err := obj.Stat()
	if err != nil {
		return fmt.Errorf("stat source: %w", err)
	}

	_, err = b.client.PutObject(ctx, b.bucket, dstKey, obj, stat.Size,
		minio.PutObjectOptions{SendContentMd5: true},
	)
	return err
}

// WriteFile writes data to an S3 object atomically.
func (b *Backend) WriteFile(ctx context.Context, p string, data []byte) error {
	k, err := b.key(p)
	if err != nil {
		return err
	}
	_, err = b.client.PutObject(ctx, b.bucket, k,
		bytes.NewReader(data), int64(len(data)),
		minio.PutObjectOptions{SendContentMd5: true},
	)
	if err != nil {
		return fmt.Errorf("s3 storage: write file: %w", err)
	}
	return nil
}

// AbsPath returns the full S3 key for a relative path.
// Callers use this as the canonical identifier for an object (analogous to
// the absolute filesystem path returned by the local backend).
// Returns an empty string if the path contains traversal components.
func (b *Backend) AbsPath(p string) string {
	k, err := b.key(p)
	if err != nil {
		return ""
	}
	return k
}

// IsLocal returns false — this is a remote S3 backend.
func (b *Backend) IsLocal() bool { return false }

// PresignGetURL generates a pre-signed GET URL for the object.
// Implements storage.PresignURLer.
func (b *Backend) PresignGetURL(ctx context.Context, p string, ttl time.Duration) (string, error) {
	k, err := b.key(p)
	if err != nil {
		return "", err
	}
	u, err := b.client.PresignedGetObject(ctx, b.bucket, k, ttl, nil)
	if err != nil {
		return "", fmt.Errorf("s3 storage: presign get: %w", err)
	}
	return u.String(), nil
}

// PresignPutURL generates a pre-signed PUT URL for direct client-side uploads.
// Implements storage.PresignURLer.
func (b *Backend) PresignPutURL(ctx context.Context, p string, ttl time.Duration) (string, error) {
	k, err := b.key(p)
	if err != nil {
		return "", err
	}
	u, err := b.client.PresignedPutObject(ctx, b.bucket, k, ttl)
	if err != nil {
		return "", fmt.Errorf("s3 storage: presign put: %w", err)
	}
	return u.String(), nil
}

// mapError converts minio errors to storage sentinel errors.
func (b *Backend) mapError(err error) error {
	if err == nil {
		return nil
	}
	resp := minio.ToErrorResponse(err)
	if resp.Code == "NoSuchKey" || resp.StatusCode == 404 {
		return storage.ErrNotFound
	}
	return err
}
