package media

import (
	"os"
	"path/filepath"
	"testing"
)

// ---------------------------------------------------------------------------
// computeContentFingerprint
// ---------------------------------------------------------------------------

func TestComputeContentFingerprint_ValidFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.mp4")
	// Create a file with enough content for fingerprinting
	data := make([]byte, 128*1024) // 128KB
	for i := range data {
		data[i] = byte(i % 256)
	}
	os.WriteFile(path, data, 0o600)

	fp, err := computeContentFingerprint(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if fp == "" {
		t.Error("fingerprint should not be empty")
	}
}

func TestComputeContentFingerprint_Deterministic(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.mp4")
	data := make([]byte, 128*1024)
	for i := range data {
		data[i] = byte(i % 256)
	}
	os.WriteFile(path, data, 0o600)

	fp1, _ := computeContentFingerprint(path)
	fp2, _ := computeContentFingerprint(path)
	if fp1 != fp2 {
		t.Error("same file should produce same fingerprint")
	}
}

func TestComputeContentFingerprint_DifferentFiles(t *testing.T) {
	dir := t.TempDir()
	p1 := filepath.Join(dir, "a.mp4")
	p2 := filepath.Join(dir, "b.mp4")
	os.WriteFile(p1, make([]byte, 128*1024), 0o600)
	d2 := make([]byte, 128*1024)
	d2[0] = 0xFF
	os.WriteFile(p2, d2, 0o600)

	fp1, _ := computeContentFingerprint(p1)
	fp2, _ := computeContentFingerprint(p2)
	if fp1 == fp2 {
		t.Error("different files should produce different fingerprints")
	}
}

func TestComputeContentFingerprint_NonexistentFile(t *testing.T) {
	_, err := computeContentFingerprint("/nonexistent/file.mp4")
	if err == nil {
		t.Error("nonexistent file should return error")
	}
}

// ---------------------------------------------------------------------------
// Module basics
// ---------------------------------------------------------------------------

func TestModuleName(t *testing.T) {
	m := &Module{}
	if m.Name() != "media" {
		t.Errorf("Name() = %q, want %q", m.Name(), "media")
	}
}
