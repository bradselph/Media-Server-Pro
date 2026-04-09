package updater

import (
	"testing"
)

// ---------------------------------------------------------------------------
// isNewerVersion
// ---------------------------------------------------------------------------

func TestIsNewerVersion(t *testing.T) {
	tests := []struct {
		latest  string
		current string
		want    bool
	}{
		{"1.1.0", "1.0.0", true},
		{"2.0.0", "1.9.9", true},
		{"1.0.1", "1.0.0", true},
		{"1.0.0", "1.0.0", false},
		{"1.0.0", "1.0.1", false},
		{"1.0.0", "2.0.0", false},
		{"1.2.0", "1.1.0", true},
		{"1.0.0-alpha", "0.9.0", true},
		{"1.1.1", "1.1.0-dev", true}, // patch version bump
	}
	for _, tc := range tests {
		got := isNewerVersion(tc.latest, tc.current)
		if got != tc.want {
			t.Errorf("isNewerVersion(%q, %q) = %v, want %v", tc.latest, tc.current, got, tc.want)
		}
	}
}

func TestIsNewerVersion_MoreParts(t *testing.T) {
	if !isNewerVersion("1.0.0.1", "1.0.0") {
		t.Error("more version parts should be considered newer")
	}
}

// ---------------------------------------------------------------------------
// extractNumericPrefix
// ---------------------------------------------------------------------------

func TestExtractNumericPrefix(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"123", "123"},
		{"1alpha", "1"},
		{"abc", "0"},
		{"", "0"},
		{"42-beta", "42"},
		{"0", "0"},
	}
	for _, tc := range tests {
		got := extractNumericPrefix(tc.input)
		if got != tc.want {
			t.Errorf("extractNumericPrefix(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

// ---------------------------------------------------------------------------
// parseExpectedHashFromChecksum
// ---------------------------------------------------------------------------

func TestParseExpectedHashFromChecksum_Found(t *testing.T) {
	data := []byte(`abc123def456  artifacts/binaries-linux-amd64/media-server-pro-linux-amd64
789xyz  artifacts/binaries-windows-amd64/media-server-pro-windows-amd64.exe
`)
	got := parseExpectedHashFromChecksum(data, "media-server-pro-linux-amd64")
	if got != "abc123def456" {
		t.Errorf("hash = %q, want abc123def456", got)
	}
}

func TestParseExpectedHashFromChecksum_NotFound(t *testing.T) {
	data := []byte(`abc123  some-other-binary
`)
	got := parseExpectedHashFromChecksum(data, "media-server-pro-linux-amd64")
	if got != "" {
		t.Errorf("should return empty for missing asset, got %q", got)
	}
}

func TestParseExpectedHashFromChecksum_CaseInsensitive(t *testing.T) {
	data := []byte(`ABC123  Media-Server-Pro-Linux-AMD64
`)
	got := parseExpectedHashFromChecksum(data, "media-server-pro-linux-amd64")
	if got != "abc123" {
		t.Errorf("hash = %q, want abc123 (case-insensitive)", got)
	}
}

// ---------------------------------------------------------------------------
// findChecksumAssetURL
// ---------------------------------------------------------------------------

func TestFindChecksumAssetURL_Found(t *testing.T) {
	assets := []releaseAsset{
		{Name: "media-server-pro-linux-amd64", BrowserDownloadURL: "https://example.com/binary"},
		{Name: "sha256sums", BrowserDownloadURL: "https://example.com/sha256sums"},
	}
	got := findChecksumAssetURL(assets)
	if got != "https://example.com/sha256sums" {
		t.Errorf("URL = %q", got)
	}
}

func TestFindChecksumAssetURL_Txt(t *testing.T) {
	assets := []releaseAsset{
		{Name: "sha256sums.txt", BrowserDownloadURL: "https://example.com/sums.txt"},
	}
	got := findChecksumAssetURL(assets)
	if got != "https://example.com/sums.txt" {
		t.Errorf("URL = %q", got)
	}
}

func TestFindChecksumAssetURL_NotFound(t *testing.T) {
	assets := []releaseAsset{
		{Name: "binary", BrowserDownloadURL: "https://example.com/binary"},
	}
	got := findChecksumAssetURL(assets)
	if got != "" {
		t.Errorf("should return empty, got %q", got)
	}
}

func TestFindChecksumAssetURL_CaseInsensitive(t *testing.T) {
	assets := []releaseAsset{
		{Name: "SHA256SUMS", BrowserDownloadURL: "https://example.com/sums"},
	}
	got := findChecksumAssetURL(assets)
	if got != "https://example.com/sums" {
		t.Errorf("URL = %q (should be case insensitive)", got)
	}
}
