// Package s3compat implements the storage.Backend interface for S3-compatible
// services (Backblaze B2, AWS S3, MinIO, Cloudflare R2, Wasabi, etc.).
package s3compat

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"path"
	"strings"
	"time"

	"media-server-pro/pkg/storage"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

// Config holds S3-compatible storage settings.
type Config struct {
	Endpoint        string
	Region          string
	AccessKeyID     string
	SecretAccessKey string
	Bucket          string
	Prefix          string // key prefix within the bucket (e.g., "videos/")
	UsePathStyle    bool
}

// Backend stores files in an S3-compatible bucket under a key prefix.
type Backend struct {
	client   *s3.Client
	presign  *s3.PresignClient
	bucket   string
	prefix   string // normalized: always ends with "/" or is empty
}

// New creates an S3 storage backend.
func New(ctx context.Context, cfg Config) (*Backend, error) {
	if cfg.Bucket == "" {
		return nil, fmt.Errorf("s3 storage: bucket name is required")
	}
	if cfg.Endpoint == "" {
		return nil, fmt.Errorf("s3 storage: endpoint is required")
	}

	region := cfg.Region
	if region == "" {
		region = "us-east-1"
	}

	awsCfg, err := config.LoadDefaultConfig(ctx,
		config.WithRegion(region),
		config.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(cfg.AccessKeyID, cfg.SecretAccessKey, ""),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("s3 storage: load config: %w", err)
	}

	endpoint := cfg.Endpoint
	if !strings.HasPrefix(endpoint, "http") {
		endpoint = "https://" + endpoint
	}

	client := s3.NewFromConfig(awsCfg, func(o *s3.Options) {
		o.BaseEndpoint = aws.String(endpoint)
		o.UsePathStyle = cfg.UsePathStyle
	})

	prefix := cfg.Prefix
	if prefix != "" && !strings.HasSuffix(prefix, "/") {
		prefix += "/"
	}

	return &Backend{
		client:  client,
		presign: s3.NewPresignClient(client),
		bucket:  cfg.Bucket,
		prefix:  prefix,
	}, nil
}

// key returns the full S3 object key for a relative path.
func (b *Backend) key(rel string) string {
	cleaned := path.Clean(rel)
	if cleaned == "." || cleaned == "" {
		return b.prefix
	}
	// Strip leading slash or dot-slash
	cleaned = strings.TrimPrefix(cleaned, "/")
	cleaned = strings.TrimPrefix(cleaned, "./")
	return b.prefix + cleaned
}

// Open returns a seekable reader for the object. Uses a buffered download
// for small files. For large files or range-based access, prefer OpenRange.
func (b *Backend) Open(ctx context.Context, p string) (storage.ReadSeekCloser, error) {
	out, err := b.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(b.bucket),
		Key:    aws.String(b.key(p)),
	})
	if err != nil {
		return nil, b.mapError(err)
	}
	// Buffer the entire object so we can seek. For streaming large files,
	// callers should use RangeOpener instead.
	data, err := io.ReadAll(out.Body)
	out.Body.Close()
	if err != nil {
		return nil, fmt.Errorf("s3 storage: read object: %w", err)
	}
	return &readSeekCloser{Reader: bytes.NewReader(data)}, nil
}

// OpenRange returns a reader for a byte range of the object.
// Implements storage.RangeOpener.
func (b *Backend) OpenRange(ctx context.Context, p string, start, end int64) (io.ReadCloser, error) {
	rangeStr := fmt.Sprintf("bytes=%d-%d", start, end)
	out, err := b.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(b.bucket),
		Key:    aws.String(b.key(p)),
		Range:  aws.String(rangeStr),
	})
	if err != nil {
		return nil, b.mapError(err)
	}
	return out.Body, nil
}

// Stat returns object metadata using HeadObject.
func (b *Backend) Stat(ctx context.Context, p string) (*storage.FileInfo, error) {
	k := b.key(p)

	// Try as file first
	out, err := b.client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(b.bucket),
		Key:    aws.String(k),
	})
	if err == nil {
		name := path.Base(k)
		var modTime time.Time
		if out.LastModified != nil {
			modTime = *out.LastModified
		}
		var size int64
		if out.ContentLength != nil {
			size = *out.ContentLength
		}
		return &storage.FileInfo{
			Name:    name,
			Size:    size,
			ModTime: modTime,
			IsDir:   false,
		}, nil
	}

	// Check if it's a "directory" (prefix with children)
	prefix := k
	if !strings.HasSuffix(prefix, "/") {
		prefix += "/"
	}
	listOut, listErr := b.client.ListObjectsV2(ctx, &s3.ListObjectsV2Input{
		Bucket:  aws.String(b.bucket),
		Prefix:  aws.String(prefix),
		MaxKeys: aws.Int32(1),
	})
	if listErr == nil && listOut.KeyCount != nil && *listOut.KeyCount > 0 {
		return &storage.FileInfo{
			Name:  path.Base(k),
			IsDir: true,
		}, nil
	}

	return nil, storage.ErrNotFound
}

// Walk lists all objects under prefix and calls fn for each.
func (b *Backend) Walk(ctx context.Context, prefix string, fn storage.WalkFunc) error {
	s3Prefix := b.key(prefix)
	if s3Prefix != "" && !strings.HasSuffix(s3Prefix, "/") {
		s3Prefix += "/"
	}

	paginator := s3.NewListObjectsV2Paginator(b.client, &s3.ListObjectsV2Input{
		Bucket: aws.String(b.bucket),
		Prefix: aws.String(s3Prefix),
	})

	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return fmt.Errorf("s3 storage: list objects: %w", err)
		}
		for _, obj := range page.Contents {
			if obj.Key == nil {
				continue
			}
			// Make path relative to backend prefix
			rel := strings.TrimPrefix(*obj.Key, b.prefix)
			if rel == "" {
				continue
			}
			var modTime time.Time
			if obj.LastModified != nil {
				modTime = *obj.LastModified
			}
			var size int64
			if obj.Size != nil {
				size = *obj.Size
			}
			info := storage.FileInfo{
				Name:    path.Base(rel),
				Size:    size,
				ModTime: modTime,
				IsDir:   strings.HasSuffix(rel, "/"),
			}
			if err := fn(rel, info, nil); err != nil {
				return err
			}
		}
	}
	return nil
}

// ReadDir lists the immediate children of a "directory" prefix.
func (b *Backend) ReadDir(ctx context.Context, p string) ([]storage.FileInfo, error) {
	prefix := b.key(p)
	if prefix != "" && !strings.HasSuffix(prefix, "/") {
		prefix += "/"
	}

	delimiter := "/"
	out, err := b.client.ListObjectsV2(ctx, &s3.ListObjectsV2Input{
		Bucket:    aws.String(b.bucket),
		Prefix:    aws.String(prefix),
		Delimiter: aws.String(delimiter),
	})
	if err != nil {
		return nil, fmt.Errorf("s3 storage: list: %w", err)
	}

	var result []storage.FileInfo

	// Common prefixes = subdirectories
	for _, cp := range out.CommonPrefixes {
		if cp.Prefix == nil {
			continue
		}
		name := strings.TrimSuffix(strings.TrimPrefix(*cp.Prefix, prefix), "/")
		if name != "" {
			result = append(result, storage.FileInfo{Name: name, IsDir: true})
		}
	}

	// Objects = files
	for _, obj := range out.Contents {
		if obj.Key == nil {
			continue
		}
		name := strings.TrimPrefix(*obj.Key, prefix)
		if name == "" || strings.Contains(name, "/") {
			continue
		}
		var modTime time.Time
		if obj.LastModified != nil {
			modTime = *obj.LastModified
		}
		var size int64
		if obj.Size != nil {
			size = *obj.Size
		}
		result = append(result, storage.FileInfo{
			Name:    name,
			Size:    size,
			ModTime: modTime,
		})
	}

	return result, nil
}

// ReadFile downloads an entire object into memory.
func (b *Backend) ReadFile(ctx context.Context, p string) ([]byte, error) {
	out, err := b.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(b.bucket),
		Key:    aws.String(b.key(p)),
	})
	if err != nil {
		return nil, b.mapError(err)
	}
	defer out.Body.Close()
	return io.ReadAll(out.Body)
}

// Create writes data to an S3 object.
func (b *Backend) Create(ctx context.Context, p string, r io.Reader) (int64, error) {
	// Buffer the content to know size for PutObject.
	// For large files, callers should use multipart upload instead.
	data, err := io.ReadAll(r)
	if err != nil {
		return 0, fmt.Errorf("s3 storage: read input: %w", err)
	}
	_, err = b.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(b.bucket),
		Key:    aws.String(b.key(p)),
		Body:   bytes.NewReader(data),
	})
	if err != nil {
		return 0, fmt.Errorf("s3 storage: put object: %w", err)
	}
	return int64(len(data)), nil
}

// MkdirAll is a no-op for S3 (no real directories).
func (b *Backend) MkdirAll(_ context.Context, _ string) error { return nil }

// Remove deletes a single object.
func (b *Backend) Remove(ctx context.Context, p string) error {
	_, err := b.client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(b.bucket),
		Key:    aws.String(b.key(p)),
	})
	return err
}

// RemoveAll deletes all objects under a prefix.
func (b *Backend) RemoveAll(ctx context.Context, p string) error {
	prefix := b.key(p)
	if !strings.HasSuffix(prefix, "/") {
		// First try to delete as a single object
		b.client.DeleteObject(ctx, &s3.DeleteObjectInput{
			Bucket: aws.String(b.bucket),
			Key:    aws.String(prefix),
		})
		prefix += "/"
	}

	// Delete all objects under the prefix
	paginator := s3.NewListObjectsV2Paginator(b.client, &s3.ListObjectsV2Input{
		Bucket: aws.String(b.bucket),
		Prefix: aws.String(prefix),
	})

	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return fmt.Errorf("s3 storage: list for delete: %w", err)
		}
		if len(page.Contents) == 0 {
			continue
		}
		objects := make([]types.ObjectIdentifier, len(page.Contents))
		for i, obj := range page.Contents {
			objects[i] = types.ObjectIdentifier{Key: obj.Key}
		}
		_, err = b.client.DeleteObjects(ctx, &s3.DeleteObjectsInput{
			Bucket: aws.String(b.bucket),
			Delete: &types.Delete{Objects: objects, Quiet: aws.Bool(true)},
		})
		if err != nil {
			return fmt.Errorf("s3 storage: batch delete: %w", err)
		}
	}
	return nil
}

// Rename copies src to dst then deletes src. S3 has no native rename.
func (b *Backend) Rename(ctx context.Context, src, dst string) error {
	srcKey := b.key(src)
	dstKey := b.key(dst)

	// Copy
	_, err := b.client.CopyObject(ctx, &s3.CopyObjectInput{
		Bucket:     aws.String(b.bucket),
		CopySource: aws.String(b.bucket + "/" + srcKey),
		Key:        aws.String(dstKey),
	})
	if err != nil {
		return fmt.Errorf("s3 storage: copy for rename: %w", err)
	}

	// Delete original
	_, err = b.client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(b.bucket),
		Key:    aws.String(srcKey),
	})
	if err != nil {
		return fmt.Errorf("s3 storage: delete after rename: %w", err)
	}
	return nil
}

// WriteFile writes data to an S3 object.
func (b *Backend) WriteFile(ctx context.Context, p string, data []byte) error {
	_, err := b.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(b.bucket),
		Key:    aws.String(b.key(p)),
		Body:   bytes.NewReader(data),
	})
	if err != nil {
		return fmt.Errorf("s3 storage: write file: %w", err)
	}
	return nil
}

// AbsPath returns the full S3 key for a relative path.
func (b *Backend) AbsPath(p string) string {
	return b.key(p)
}

// IsLocal returns false — this is a remote S3 backend.
func (b *Backend) IsLocal() bool { return false }

// PresignGetURL generates a presigned GET URL for the object.
func (b *Backend) PresignGetURL(ctx context.Context, p string, ttl time.Duration) (string, error) {
	out, err := b.presign.PresignGetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(b.bucket),
		Key:    aws.String(b.key(p)),
	}, s3.WithPresignExpires(ttl))
	if err != nil {
		return "", fmt.Errorf("s3 storage: presign get: %w", err)
	}
	return out.URL, nil
}

// PresignPutURL generates a presigned PUT URL for direct uploads.
func (b *Backend) PresignPutURL(ctx context.Context, p string, ttl time.Duration) (string, error) {
	out, err := b.presign.PresignPutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(b.bucket),
		Key:    aws.String(b.key(p)),
	}, s3.WithPresignExpires(ttl))
	if err != nil {
		return "", fmt.Errorf("s3 storage: presign put: %w", err)
	}
	return out.URL, nil
}

// mapError converts S3 errors to storage errors.
func (b *Backend) mapError(err error) error {
	if err == nil {
		return nil
	}
	msg := err.Error()
	if strings.Contains(msg, "NoSuchKey") || strings.Contains(msg, "NotFound") || strings.Contains(msg, "404") {
		return storage.ErrNotFound
	}
	return err
}

// readSeekCloser wraps a bytes.Reader to implement storage.ReadSeekCloser.
type readSeekCloser struct {
	*bytes.Reader
}

func (r *readSeekCloser) Close() error { return nil }
