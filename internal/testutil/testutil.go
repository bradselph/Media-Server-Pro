// Package testutil provides shared helpers for unit tests across internal packages.
// Import this package in test files (not production code) to get common fixtures.
package testutil

import (
	"os"
	"testing"
)

// TempDir creates a temporary directory for test use and registers a cleanup
// function that removes it when the test ends.
func TempDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "mediaserver-test-*")
	if err != nil {
		t.Fatalf("testutil.TempDir: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	return dir
}

// WriteTempFile creates a file inside dir with the given content and returns its path.
func WriteTempFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := dir + "/" + name
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("testutil.WriteTempFile: %v", err)
	}
	return path
}
