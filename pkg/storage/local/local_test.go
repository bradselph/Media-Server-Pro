package local

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"media-server-pro/pkg/storage"
)

func TestNew(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "newroot")
	b, err := New(root)
	if err != nil {
		t.Fatalf("New(%q): %v", root, err)
	}
	if b.Root() != root {
		// Root returns abs path; on Windows might differ in case
		if !strings.EqualFold(b.Root(), root) {
			t.Errorf("Root() = %q, want %q", b.Root(), root)
		}
	}
	// Directory should have been created
	fi, err := os.Stat(b.Root())
	if err != nil {
		t.Fatalf("root dir not created: %v", err)
	}
	if !fi.IsDir() {
		t.Error("root should be a directory")
	}
}

func TestNew_InvalidRoot(t *testing.T) {
	// NUL byte in path should cause error
	_, err := New(string([]byte{0}))
	if err == nil {
		t.Error("expected error for NUL in path")
	}
}

func TestIsLocal(t *testing.T) {
	b, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if !b.IsLocal() {
		t.Error("IsLocal() should return true")
	}
}

func TestCreateAndReadFile(t *testing.T) {
	b := newTestBackend(t)
	ctx := context.Background()
	content := []byte("hello, world!")

	n, err := b.Create(ctx, "test.txt", bytes.NewReader(content))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if n != int64(len(content)) {
		t.Errorf("Create returned %d bytes, want %d", n, len(content))
	}

	got, err := b.ReadFile(ctx, "test.txt")
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !bytes.Equal(got, content) {
		t.Errorf("ReadFile = %q, want %q", got, content)
	}
}

func TestWriteFileAndReadFile(t *testing.T) {
	b := newTestBackend(t)
	ctx := context.Background()
	data := []byte("atomic write data")

	if err := b.WriteFile(ctx, "sub/dir/file.dat", data); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	got, err := b.ReadFile(ctx, "sub/dir/file.dat")
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !bytes.Equal(got, data) {
		t.Errorf("ReadFile = %q, want %q", got, data)
	}
}

func TestOpen(t *testing.T) {
	b := newTestBackend(t)
	ctx := context.Background()
	content := []byte("seekable content")
	if err := b.WriteFile(ctx, "open.txt", content); err != nil {
		t.Fatal(err)
	}

	r, err := b.Open(ctx, "open.txt")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer r.Close()

	got, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if !bytes.Equal(got, content) {
		t.Errorf("Open content = %q, want %q", got, content)
	}

	// Test seek
	_, err = r.Seek(0, io.SeekStart)
	if err != nil {
		t.Fatalf("Seek: %v", err)
	}
	got2, _ := io.ReadAll(r)
	if !bytes.Equal(got2, content) {
		t.Error("re-read after seek should match")
	}
}

func TestOpen_NotFound(t *testing.T) {
	b := newTestBackend(t)
	_, err := b.Open(context.Background(), "nonexistent.txt")
	if !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("Open nonexistent: got %v, want ErrNotFound", err)
	}
}

func TestStat(t *testing.T) {
	b := newTestBackend(t)
	ctx := context.Background()
	data := []byte("stat me")
	if err := b.WriteFile(ctx, "stat.txt", data); err != nil {
		t.Fatal(err)
	}

	info, err := b.Stat(ctx, "stat.txt")
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if info.Name != "stat.txt" {
		t.Errorf("Name = %q, want %q", info.Name, "stat.txt")
	}
	if info.Size != int64(len(data)) {
		t.Errorf("Size = %d, want %d", info.Size, len(data))
	}
	if info.IsDir {
		t.Error("IsDir should be false for a file")
	}
}

func TestStat_NotFound(t *testing.T) {
	b := newTestBackend(t)
	_, err := b.Stat(context.Background(), "nope.txt")
	if !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("Stat nonexistent: got %v, want ErrNotFound", err)
	}
}

func TestStat_Directory(t *testing.T) {
	b := newTestBackend(t)
	ctx := context.Background()
	if err := b.MkdirAll(ctx, "mydir"); err != nil {
		t.Fatal(err)
	}
	info, err := b.Stat(ctx, "mydir")
	if err != nil {
		t.Fatalf("Stat dir: %v", err)
	}
	if !info.IsDir {
		t.Error("IsDir should be true for a directory")
	}
}

func TestMkdirAll(t *testing.T) {
	b := newTestBackend(t)
	ctx := context.Background()
	if err := b.MkdirAll(ctx, "a/b/c"); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	info, err := b.Stat(ctx, "a/b/c")
	if err != nil {
		t.Fatalf("Stat after MkdirAll: %v", err)
	}
	if !info.IsDir {
		t.Error("a/b/c should be a directory")
	}
}

func TestReadDir(t *testing.T) {
	b := newTestBackend(t)
	ctx := context.Background()
	b.WriteFile(ctx, "dir/a.txt", []byte("a"))
	b.WriteFile(ctx, "dir/b.txt", []byte("bb"))
	b.MkdirAll(ctx, "dir/sub")

	entries, err := b.ReadDir(ctx, "dir")
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) != 3 {
		t.Fatalf("ReadDir got %d entries, want 3", len(entries))
	}
	names := map[string]bool{}
	for _, e := range entries {
		names[e.Name] = true
	}
	for _, want := range []string{"a.txt", "b.txt", "sub"} {
		if !names[want] {
			t.Errorf("ReadDir missing %q", want)
		}
	}
}

func TestReadDir_NotFound(t *testing.T) {
	b := newTestBackend(t)
	_, err := b.ReadDir(context.Background(), "nodir")
	if !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("ReadDir nonexistent: got %v, want ErrNotFound", err)
	}
}

func TestRemove(t *testing.T) {
	b := newTestBackend(t)
	ctx := context.Background()
	b.WriteFile(ctx, "rm.txt", []byte("data"))

	if err := b.Remove(ctx, "rm.txt"); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	_, err := b.Stat(ctx, "rm.txt")
	if !errors.Is(err, storage.ErrNotFound) {
		t.Error("file should be gone after Remove")
	}
}

func TestRemove_NotFound(t *testing.T) {
	b := newTestBackend(t)
	err := b.Remove(context.Background(), "nope.txt")
	if !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("Remove nonexistent: got %v, want ErrNotFound", err)
	}
}

func TestRemoveAll(t *testing.T) {
	b := newTestBackend(t)
	ctx := context.Background()
	b.WriteFile(ctx, "tree/a.txt", []byte("a"))
	b.WriteFile(ctx, "tree/sub/b.txt", []byte("b"))

	if err := b.RemoveAll(ctx, "tree"); err != nil {
		t.Fatalf("RemoveAll: %v", err)
	}
	_, err := b.Stat(ctx, "tree")
	if !errors.Is(err, storage.ErrNotFound) {
		t.Error("tree should be gone after RemoveAll")
	}
}

func TestRename(t *testing.T) {
	b := newTestBackend(t)
	ctx := context.Background()
	b.WriteFile(ctx, "old.txt", []byte("content"))

	if err := b.Rename(ctx, "old.txt", "sub/new.txt"); err != nil {
		t.Fatalf("Rename: %v", err)
	}
	// Old should be gone
	_, err := b.Stat(ctx, "old.txt")
	if !errors.Is(err, storage.ErrNotFound) {
		t.Error("old file should be gone after Rename")
	}
	// New should exist with same content
	got, err := b.ReadFile(ctx, "sub/new.txt")
	if err != nil {
		t.Fatalf("ReadFile after Rename: %v", err)
	}
	if string(got) != "content" {
		t.Errorf("content after Rename = %q, want %q", got, "content")
	}
}

func TestWalk(t *testing.T) {
	b := newTestBackend(t)
	ctx := context.Background()
	b.WriteFile(ctx, "walk/a.txt", []byte("a"))
	b.WriteFile(ctx, "walk/sub/b.txt", []byte("b"))

	var paths []string
	err := b.Walk(ctx, "walk", func(path string, _ storage.FileInfo, err error) error {
		if err != nil {
			return err
		}
		paths = append(paths, path)
		return nil
	})
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	if len(paths) < 3 {
		t.Errorf("Walk visited %d entries, want at least 3 (dir + 2 files)", len(paths))
	}
}

func TestAbsPath(t *testing.T) {
	b := newTestBackend(t)
	abs := b.AbsPath("some/file.txt")
	if abs == "" {
		t.Error("AbsPath should not be empty")
	}
	if !filepath.IsAbs(abs) {
		t.Errorf("AbsPath returned non-absolute path: %q", abs)
	}
}

func TestAbsPath_Traversal(t *testing.T) {
	b := newTestBackend(t)
	abs := b.AbsPath("../../etc/passwd")
	if abs != "" {
		t.Errorf("AbsPath should return empty for traversal, got %q", abs)
	}
}

// ---------------------------------------------------------------------------
// Path traversal prevention
// ---------------------------------------------------------------------------

func TestResolve_PathTraversal(t *testing.T) {
	b := newTestBackend(t)
	tests := []string{
		"../../etc/passwd",
		"../secret",
		"foo/../../etc/passwd",
	}
	for _, path := range tests {
		_, err := b.resolve(path)
		if err == nil {
			t.Errorf("resolve(%q) should reject traversal", path)
		}
	}
}

func TestResolve_AbsolutePathOutsideRoot(t *testing.T) {
	b := newTestBackend(t)
	// Use a path that definitely can't be under a temp dir
	outsidePath := filepath.Join(filepath.VolumeName(b.root), string(filepath.Separator), "definitely-outside", "secret.txt")
	if outsidePath == b.root || strings.HasPrefix(outsidePath, b.rootWithSep()) {
		t.Skip("generated path is unexpectedly under root")
	}
	_, err := b.resolve(outsidePath)
	if err == nil {
		t.Errorf("resolve(%q) should be rejected outside root %q", outsidePath, b.root)
	}
}

func TestResolve_AbsolutePathInsideRoot(t *testing.T) {
	b := newTestBackend(t)
	inside := filepath.Join(b.root, "valid.txt")
	got, err := b.resolve(inside)
	if err != nil {
		t.Fatalf("resolve absolute inside root: %v", err)
	}
	if got != inside {
		t.Errorf("resolve = %q, want %q", got, inside)
	}
}

func TestResolve_AbsolutePathSiblingPrefix(t *testing.T) {
	// Ensures /data/media-evil doesn't match root=/data/media
	dir := t.TempDir()
	root := filepath.Join(dir, "media")
	os.MkdirAll(root, 0o750)
	b, _ := New(root)
	evil := filepath.Join(dir, "media-evil", "secret.txt")
	_, err := b.resolve(evil)
	if err == nil {
		t.Error("resolve should reject sibling with same prefix")
	}
}

// ---------------------------------------------------------------------------
// Create with nested directories
// ---------------------------------------------------------------------------

func TestCreate_CreatesParentDirs(t *testing.T) {
	b := newTestBackend(t)
	ctx := context.Background()
	_, err := b.Create(ctx, "deep/nested/dir/file.txt", bytes.NewReader([]byte("data")))
	if err != nil {
		t.Fatalf("Create with nested dirs: %v", err)
	}
	got, _ := b.ReadFile(ctx, "deep/nested/dir/file.txt")
	if string(got) != "data" {
		t.Errorf("content = %q, want %q", got, "data")
	}
}

// ---------------------------------------------------------------------------
// Overwrite existing file
// ---------------------------------------------------------------------------

func TestCreate_OverwriteExisting(t *testing.T) {
	b := newTestBackend(t)
	ctx := context.Background()
	b.WriteFile(ctx, "overwrite.txt", []byte("old"))

	_, err := b.Create(ctx, "overwrite.txt", bytes.NewReader([]byte("new")))
	if err != nil {
		t.Fatalf("Create overwrite: %v", err)
	}
	got, _ := b.ReadFile(ctx, "overwrite.txt")
	if string(got) != "new" {
		t.Errorf("after overwrite = %q, want %q", got, "new")
	}
}

// ---------------------------------------------------------------------------
// ReadFile not found
// ---------------------------------------------------------------------------

func TestReadFile_NotFound(t *testing.T) {
	b := newTestBackend(t)
	_, err := b.ReadFile(context.Background(), "missing.dat")
	if !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("ReadFile nonexistent: got %v, want ErrNotFound", err)
	}
}

// ---------------------------------------------------------------------------
// Interface compliance
// ---------------------------------------------------------------------------

func TestBackendImplementsInterface(_ *testing.T) {
	var _ storage.Backend = (*Backend)(nil)
}

// ---------------------------------------------------------------------------
// helper
// ---------------------------------------------------------------------------

func newTestBackend(t *testing.T) *Backend {
	t.Helper()
	b, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return b
}
