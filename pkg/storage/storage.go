// Package storage defines the abstraction layer for file storage backends.
// Two implementations are provided: local filesystem and S3-compatible
// (Backblaze B2, AWS S3, MinIO, Cloudflare R2, etc.).
//
// Each configured directory (Videos, Uploads, Thumbnails, HLSCache, etc.)
// gets its own Backend instance. Paths passed to methods are always
// relative to that backend's root/prefix.
package storage

import (
	"context"
	"errors"
	"io"
	"time"
)

// Sentinel errors returned by Backend methods.
var (
	ErrNotFound = errors.New("storage: file not found")
	ErrIsDir    = errors.New("storage: path is a directory")
	ErrNotDir   = errors.New("storage: path is not a directory")
)

// FileInfo holds metadata about a stored file or directory.
type FileInfo struct {
	Name    string
	Size    int64
	ModTime time.Time
	IsDir   bool
}

// ReadSeekCloser combines io.ReadSeeker and io.Closer.
// Local backends return *os.File which satisfies this. S3 backends
// return a wrapper that issues ranged GET requests on Seek.
type ReadSeekCloser interface {
	io.ReadSeeker
	io.Closer
}

// WalkFunc is the callback for Backend.Walk. Returning a non-nil error
// stops the walk. Return filepath.SkipDir to skip a directory subtree.
type WalkFunc func(path string, info FileInfo, err error) error

// Backend abstracts filesystem operations for a single storage root.
type Backend interface {
	// Open returns a seekable reader for the file at the given relative path.
	// The caller must close the returned reader.
	Open(ctx context.Context, path string) (ReadSeekCloser, error)

	// Stat returns file metadata without opening the file body.
	Stat(ctx context.Context, path string) (*FileInfo, error)

	// Walk recursively visits every entry under prefix, calling fn for each.
	// Paths passed to fn are relative to the backend root.
	Walk(ctx context.Context, prefix string, fn WalkFunc) error

	// ReadDir lists the immediate children of the directory at path.
	ReadDir(ctx context.Context, path string) ([]FileInfo, error)

	// ReadFile reads the entire file into memory.
	ReadFile(ctx context.Context, path string) ([]byte, error)

	// Create writes data from r to the given path, creating parent dirs as needed.
	// If the file exists it is overwritten. Returns bytes written.
	Create(ctx context.Context, path string, r io.Reader) (int64, error)

	// MkdirAll creates a directory and all parents.
	// No-op for S3 backends (no real directories).
	MkdirAll(ctx context.Context, path string) error

	// Remove deletes a single file.
	Remove(ctx context.Context, path string) error

	// RemoveAll deletes a path and everything under it.
	RemoveAll(ctx context.Context, path string) error

	// Rename moves src to dst within the same backend.
	Rename(ctx context.Context, src, dst string) error

	// WriteFile writes data to the given path atomically.
	WriteFile(ctx context.Context, path string, data []byte) error

	// AbsPath returns the absolute filesystem path (local) or the full
	// S3 key (remote). Used for logging and for passing to ffmpeg.
	AbsPath(path string) string

	// IsLocal returns true for the local filesystem backend.
	// Callers that shell out to ffmpeg can check this to decide strategy.
	IsLocal() bool
}

// RangeOpener is optionally implemented by backends that support efficient
// byte-range reads without seeking (e.g., S3 ranged GET). The streaming
// module uses this to avoid downloading entire files for range requests.
type RangeOpener interface {
	OpenRange(ctx context.Context, path string, start, end int64) (io.ReadCloser, error)
}

// PresignURLer is optionally implemented by backends that can generate
// time-limited direct-access URLs. Used to pass media URLs to ffmpeg
// when the backend is remote.
type PresignURLer interface {
	PresignGetURL(ctx context.Context, path string, ttl time.Duration) (string, error)
	PresignPutURL(ctx context.Context, path string, ttl time.Duration) (string, error)
}
