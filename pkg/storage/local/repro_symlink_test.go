package local

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// Regression for the symlink-escape write bypass: resolve() previously fell back
// to the raw logical path when EvalSymlinks failed (target not yet existing), so
// a WRITE through a symlinked directory pointing outside root succeeded silently
// even though reads of existing files through the same symlink were rejected.
// The fix resolves the deepest existing ancestor's symlinks for non-existent
// targets, so the escape is now caught on writes too.
func TestSymlinkEscapeOnWrite_IsBlocked(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "root")
	outside := filepath.Join(dir, "outside_secret")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatal(err)
	}

	b, err := New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	linkPath := filepath.Join(b.root, "uploads")
	if err := os.Symlink(outside, linkPath); err != nil {
		t.Skipf("symlink creation failed (likely needs admin/dev mode on Windows): %v", err)
	}

	// resolve() must now REJECT a not-yet-existing file behind an escaping symlink.
	if _, rerr := b.resolve(filepath.Join("uploads", "evil.txt")); rerr == nil {
		t.Fatalf("resolve() should reject a write path escaping root via symlink, but it allowed it")
	}

	// The public write API must also reject it, and nothing must land outside root.
	ctx := context.Background()
	if err := b.WriteFile(ctx, filepath.Join("uploads", "evil.txt"), []byte("pwned")); err == nil {
		t.Fatalf("WriteFile through an escaping symlink should be rejected")
	}
	if _, err := os.Stat(filepath.Join(outside, "evil.txt")); err == nil {
		t.Fatalf("file escaped root and landed outside despite the guard")
	}

	// Reading an existing file through the same symlink stays rejected (unchanged).
	existing := filepath.Join(outside, "already-there.txt")
	if err := os.WriteFile(existing, []byte("pre-existing"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := b.ReadFile(ctx, filepath.Join("uploads", "already-there.txt")); err == nil {
		t.Fatalf("ReadFile of an existing file through an escaping symlink should be rejected")
	}

	// A legitimate not-yet-existing path (no escaping symlink) must still be allowed.
	if _, rerr := b.resolve(filepath.Join("newdir", "newfile.txt")); rerr != nil {
		t.Fatalf("resolve() wrongly rejected a legitimate new path under root: %v", rerr)
	}
}
