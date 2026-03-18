package remote

import (
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// validateURL
// ---------------------------------------------------------------------------

func TestValidateURL_ValidHTTPS(t *testing.T) {
	err := validateURL("https://example.com/media/video.mp4")
	if err != nil {
		t.Errorf("valid HTTPS URL should pass: %v", err)
	}
}

func TestValidateURL_ValidHTTP(t *testing.T) {
	err := validateURL("http://example.com/media/video.mp4")
	if err != nil {
		t.Errorf("valid HTTP URL should pass: %v", err)
	}
}

func TestValidateURL_InvalidScheme(t *testing.T) {
	err := validateURL("ftp://example.com/file.mp4")
	if err == nil {
		t.Error("ftp:// should be rejected")
	}
}

func TestValidateURL_NoScheme(t *testing.T) {
	err := validateURL("example.com/file.mp4")
	if err == nil {
		t.Error("URL without scheme should be rejected")
	}
}

func TestValidateURL_Empty(t *testing.T) {
	err := validateURL("")
	if err == nil {
		t.Error("empty URL should be rejected")
	}
}

func TestValidateURL_Localhost(t *testing.T) {
	err := validateURL("http://localhost/file.mp4")
	if err == nil {
		t.Error("localhost should be rejected (SSRF)")
	}
}

func TestValidateURL_PrivateIP(t *testing.T) {
	err := validateURL("http://192.168.1.1/file.mp4")
	if err == nil {
		t.Error("private IP should be rejected (SSRF)")
	}
}

func TestValidateURL_Loopback(t *testing.T) {
	err := validateURL("http://127.0.0.1/file.mp4")
	if err == nil {
		t.Error("loopback should be rejected (SSRF)")
	}
}

// ---------------------------------------------------------------------------
// generateID
// ---------------------------------------------------------------------------

func TestGenerateID_Deterministic(t *testing.T) {
	id1 := generateID("https://example.com/video.mp4")
	id2 := generateID("https://example.com/video.mp4")
	if id1 != id2 {
		t.Error("same input should produce same ID")
	}
}

func TestGenerateID_DifferentInputs(t *testing.T) {
	id1 := generateID("https://example.com/a.mp4")
	id2 := generateID("https://example.com/b.mp4")
	if id1 == id2 {
		t.Error("different inputs should produce different IDs")
	}
}

func TestGenerateID_Length(t *testing.T) {
	id := generateID("test")
	if len(id) != 16 {
		t.Errorf("ID length = %d, want 16", len(id))
	}
}

// ---------------------------------------------------------------------------
// generateCacheFilename
// ---------------------------------------------------------------------------

func TestGenerateCacheFilename_WithExtension(t *testing.T) {
	filename := generateCacheFilename("https://cdn.example.com/media/video.mp4")
	if !strings.HasSuffix(filename, ".mp4") {
		t.Errorf("cache filename should preserve extension: %s", filename)
	}
}

func TestGenerateCacheFilename_DifferentURLs(t *testing.T) {
	f1 := generateCacheFilename("https://cdn.example.com/a.mp4")
	f2 := generateCacheFilename("https://cdn.example.com/b.mp4")
	if f1 == f2 {
		t.Error("different URLs should produce different filenames")
	}
}

func TestGenerateCacheFilename_InvalidURL(t *testing.T) {
	filename := generateCacheFilename("://invalid")
	if filename == "" {
		t.Error("should fallback to hash-based filename for invalid URL")
	}
}

// ---------------------------------------------------------------------------
// Module basics
// ---------------------------------------------------------------------------

func TestModuleName(t *testing.T) {
	m := &Module{}
	if m.Name() != "remote" {
		t.Errorf("Name() = %q, want %q", m.Name(), "remote")
	}
}

func TestModuleHealth_Default(t *testing.T) {
	m := &Module{}
	h := m.Health()
	if h.Name != "remote" {
		t.Errorf("Health().Name = %q", h.Name)
	}
}
