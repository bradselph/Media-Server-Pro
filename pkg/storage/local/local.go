// Package local implements the storage.Backend interface using the local
// filesystem. All paths are resolved relative to a configured root directory.
package local

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"media-server-pro/pkg/storage"
)

// Backend stores files on the local filesystem under a root directory.
type Backend struct {
	root string
}

// New creates a local storage backend rooted at the given directory.
// The directory is created if it does not exist.
func New(root string) (*Backend, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("local storage: resolve root %q: %w", root, err)
	}
	if err := os.MkdirAll(abs, 0755); err != nil {
		return nil, fmt.Errorf("local storage: create root %q: %w", abs, err)
	}
	return &Backend{root: abs}, nil
}

// resolve returns the absolute path for a relative path, ensuring it stays
// within the root directory to prevent path traversal attacks.
func (b *Backend) resolve(rel string) (string, error) {
	cleaned := filepath.Clean(rel)
	if filepath.IsAbs(cleaned) {
		// Allow absolute paths that are already under root (legacy code passes these)
		if strings.HasPrefix(cleaned, b.root) {
			return cleaned, nil
		}
		return "", fmt.Errorf("local storage: absolute path %q outside root %q", cleaned, b.root)
	}
	full := filepath.Join(b.root, cleaned)
	// Verify the resolved path is still under root after cleaning
	if !strings.HasPrefix(full, b.root) {
		return "", fmt.Errorf("local storage: path traversal %q", rel)
	}
	return full, nil
}

func toFileInfo(fi os.FileInfo) storage.FileInfo {
	return storage.FileInfo{
		Name:    fi.Name(),
		Size:    fi.Size(),
		ModTime: fi.ModTime(),
		IsDir:   fi.IsDir(),
	}
}

func mapError(err error) error {
	if os.IsNotExist(err) {
		return storage.ErrNotFound
	}
	return err
}

// Open opens a file for reading. The caller must close the returned reader.
func (b *Backend) Open(_ context.Context, path string) (storage.ReadSeekCloser, error) {
	abs, err := b.resolve(path)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(abs)
	if err != nil {
		return nil, mapError(err)
	}
	return f, nil
}

// Stat returns file metadata.
func (b *Backend) Stat(_ context.Context, path string) (*storage.FileInfo, error) {
	abs, err := b.resolve(path)
	if err != nil {
		return nil, err
	}
	fi, err := os.Stat(abs)
	if err != nil {
		return nil, mapError(err)
	}
	return new(toFileInfo(fi)), nil
}

// Walk recursively visits entries under prefix.
func (b *Backend) Walk(_ context.Context, prefix string, fn storage.WalkFunc) error {
	abs, err := b.resolve(prefix)
	if err != nil {
		return err
	}
	return filepath.Walk(abs, func(path string, fi os.FileInfo, walkErr error) error {
		rel, relErr := filepath.Rel(b.root, path)
		if relErr != nil {
			rel = path
		}
		var info storage.FileInfo
		if fi != nil {
			info = toFileInfo(fi)
		}
		return fn(rel, info, walkErr)
	})
}

// ReadDir lists the immediate children of a directory.
func (b *Backend) ReadDir(_ context.Context, path string) ([]storage.FileInfo, error) {
	abs, err := b.resolve(path)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(abs)
	if err != nil {
		return nil, mapError(err)
	}
	result := make([]storage.FileInfo, 0, len(entries))
	for _, e := range entries {
		fi, err := e.Info()
		if err != nil {
			continue
		}
		result = append(result, toFileInfo(fi))
	}
	return result, nil
}

// ReadFile reads an entire file into memory.
func (b *Backend) ReadFile(_ context.Context, path string) ([]byte, error) {
	abs, err := b.resolve(path)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		return nil, mapError(err)
	}
	return data, nil
}

// Create writes data to a file, creating parent directories as needed.
func (b *Backend) Create(_ context.Context, path string, r io.Reader) (int64, error) {
	abs, err := b.resolve(path)
	if err != nil {
		return 0, err
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0755); err != nil {
		return 0, fmt.Errorf("local storage: mkdir: %w", err)
	}
	// Write to temp file + rename for atomicity
	tmp, err := os.CreateTemp(filepath.Dir(abs), ".tmp-*")
	if err != nil {
		return 0, fmt.Errorf("local storage: create temp: %w", err)
	}
	tmpName := tmp.Name()
	n, copyErr := io.Copy(tmp, r)
	closeErr := tmp.Close()
	if copyErr != nil {
		os.Remove(tmpName)
		return 0, fmt.Errorf("local storage: write: %w", copyErr)
	}
	if closeErr != nil {
		os.Remove(tmpName)
		return 0, fmt.Errorf("local storage: close: %w", closeErr)
	}
	if err := os.Rename(tmpName, abs); err != nil {
		os.Remove(tmpName)
		return 0, fmt.Errorf("local storage: rename: %w", err)
	}
	return n, nil
}

// MkdirAll creates a directory and all parents.
func (b *Backend) MkdirAll(_ context.Context, path string) error {
	abs, err := b.resolve(path)
	if err != nil {
		return err
	}
	return os.MkdirAll(abs, 0755)
}

// Remove deletes a single file.
func (b *Backend) Remove(_ context.Context, path string) error {
	abs, err := b.resolve(path)
	if err != nil {
		return err
	}
	return mapError(os.Remove(abs))
}

// RemoveAll deletes a path and everything under it.
func (b *Backend) RemoveAll(_ context.Context, path string) error {
	abs, err := b.resolve(path)
	if err != nil {
		return err
	}
	return os.RemoveAll(abs)
}

// Rename moves a file from src to dst within the root.
func (b *Backend) Rename(_ context.Context, src, dst string) error {
	srcAbs, err := b.resolve(src)
	if err != nil {
		return err
	}
	dstAbs, err := b.resolve(dst)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dstAbs), 0755); err != nil {
		return fmt.Errorf("local storage: mkdir for rename: %w", err)
	}
	return os.Rename(srcAbs, dstAbs)
}

// WriteFile writes data to a file atomically.
func (b *Backend) WriteFile(ctx context.Context, path string, data []byte) error {
	abs, err := b.resolve(path)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0755); err != nil {
		return fmt.Errorf("local storage: mkdir: %w", err)
	}
	// Write to temp + rename for atomicity
	tmp, err := os.CreateTemp(filepath.Dir(abs), ".tmp-*")
	if err != nil {
		return fmt.Errorf("local storage: create temp: %w", err)
	}
	tmpName := tmp.Name()
	_, writeErr := tmp.Write(data)
	closeErr := tmp.Close()
	if writeErr != nil {
		os.Remove(tmpName)
		return fmt.Errorf("local storage: write: %w", writeErr)
	}
	if closeErr != nil {
		os.Remove(tmpName)
		return fmt.Errorf("local storage: close: %w", closeErr)
	}
	return os.Rename(tmpName, abs)
}

// AbsPath returns the absolute filesystem path.
func (b *Backend) AbsPath(path string) string {
	abs, err := b.resolve(path)
	if err != nil {
		return filepath.Join(b.root, filepath.Clean(path))
	}
	return abs
}

// IsLocal returns true — this is the local filesystem backend.
func (b *Backend) IsLocal() bool { return true }

// Root returns the root directory of this backend.
func (b *Backend) Root() string { return b.root }
