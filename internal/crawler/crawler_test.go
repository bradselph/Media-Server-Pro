package crawler

import (
	"errors"
	"net/url"
	"strings"
	"testing"
)

const (
	testExampleURL     = "https://example.com"
	testExamplePageURL = "https://example.com/page"
	testStreamM3U8URL  = "https://cdn.example.com/stream.m3u8"
	errFmtErrDisabled  = "expected errDisabled, got %v"
)

// ---------------------------------------------------------------------------
// generateTargetID
// ---------------------------------------------------------------------------

func TestGenerateTargetID_Deterministic(t *testing.T) {
	id1 := generateTargetID(testExampleURL)
	id2 := generateTargetID(testExampleURL)
	if id1 != id2 {
		t.Error("same input should produce same ID")
	}
}

func TestGenerateTargetID_Prefix(t *testing.T) {
	id := generateTargetID(testExampleURL)
	if !strings.HasPrefix(id, "ct_") {
		t.Errorf("target ID should start with 'ct_': %s", id)
	}
}

func TestGenerateTargetID_DifferentInputs(t *testing.T) {
	id1 := generateTargetID("https://example.com/a")
	id2 := generateTargetID("https://example.com/b")
	if id1 == id2 {
		t.Error("different inputs should produce different IDs")
	}
}

// ---------------------------------------------------------------------------
// generateDiscoveryID
// ---------------------------------------------------------------------------

func TestGenerateDiscoveryID_Deterministic(t *testing.T) {
	id1 := generateDiscoveryID(testStreamM3U8URL)
	id2 := generateDiscoveryID(testStreamM3U8URL)
	if id1 != id2 {
		t.Error("same input should produce same ID")
	}
}

func TestGenerateDiscoveryID_Prefix(t *testing.T) {
	id := generateDiscoveryID(testStreamM3U8URL)
	if !strings.HasPrefix(id, "cd_") {
		t.Errorf("discovery ID should start with 'cd_': %s", id)
	}
}

// ---------------------------------------------------------------------------
// resolveHref
// ---------------------------------------------------------------------------

func TestResolveHref_AbsoluteHTTPS(t *testing.T) {
	base, _ := url.Parse(testExamplePageURL)
	got := resolveHref("https://other.com/video", base)
	if got != "https://other.com/video" {
		t.Errorf("absolute URL should be returned as-is: %s", got)
	}
}

func TestResolveHref_AbsoluteHTTP(t *testing.T) {
	base, _ := url.Parse(testExamplePageURL)
	got := resolveHref("http://other.com/video", base)
	if got != "http://other.com/video" {
		t.Errorf("absolute HTTP URL should be returned as-is: %s", got)
	}
}

func TestResolveHref_ProtocolRelative(t *testing.T) {
	base, _ := url.Parse(testExamplePageURL)
	got := resolveHref("//cdn.example.com/video", base)
	if got != "https://cdn.example.com/video" {
		t.Errorf("protocol-relative should inherit scheme: %s", got)
	}
}

func TestResolveHref_RootRelative(t *testing.T) {
	base, _ := url.Parse("https://example.com/page/sub")
	got := resolveHref("/video/123", base)
	if got != "https://example.com/video/123" {
		t.Errorf("root-relative should resolve to host root: %s", got)
	}
}

func TestResolveHref_JavaScript(t *testing.T) {
	base, _ := url.Parse(testExamplePageURL)
	got := resolveHref("javascript:void(0)", base)
	if got != "" {
		t.Errorf("javascript: should return empty: %s", got)
	}
}

func TestResolveHref_Mailto(t *testing.T) {
	base, _ := url.Parse(testExamplePageURL)
	got := resolveHref("mailto:test@example.com", base)
	if got != "" {
		t.Errorf("mailto: should return empty: %s", got)
	}
}

func TestResolveHref_Fragment(t *testing.T) {
	base, _ := url.Parse(testExamplePageURL)
	got := resolveHref("#section", base)
	if got != "" {
		t.Errorf("fragment should return empty: %s", got)
	}
}

func TestResolveHref_Relative(t *testing.T) {
	base, _ := url.Parse("https://example.com/videos/")
	got := resolveHref("video123", base)
	if got != "https://example.com/videos/video123" {
		t.Errorf("relative should resolve against base: %s", got)
	}
}

// ---------------------------------------------------------------------------
// extractTitleFromURL
// ---------------------------------------------------------------------------

func TestExtractTitleFromURL_WithPath(t *testing.T) {
	got := extractTitleFromURL("https://example.com/videos/my-awesome-video")
	if got != "my awesome video" {
		t.Errorf("extractTitleFromURL = %q, want %q", got, "my awesome video")
	}
}

func TestExtractTitleFromURL_WithUnderscores(t *testing.T) {
	got := extractTitleFromURL("https://example.com/videos/my_cool_video")
	if got != "my cool video" {
		t.Errorf("extractTitleFromURL = %q, want %q", got, "my cool video")
	}
}

func TestExtractTitleFromURL_RootOnly(t *testing.T) {
	got := extractTitleFromURL("https://example.com/")
	if got != "Untitled" {
		t.Errorf("extractTitleFromURL for root = %q, want %q", got, "Untitled")
	}
}

func TestExtractTitleFromURL_InvalidURL(t *testing.T) {
	got := extractTitleFromURL("://bad")
	if got != "Untitled" {
		t.Errorf("extractTitleFromURL for invalid URL = %q, want %q", got, "Untitled")
	}
}

// ---------------------------------------------------------------------------
// Module basics
// ---------------------------------------------------------------------------

func TestModuleName(t *testing.T) {
	m := &Module{}
	if m.Name() != "crawler" {
		t.Errorf("Name() = %q, want %q", m.Name(), "crawler")
	}
}

func TestModuleHealth_Default(t *testing.T) {
	m := &Module{}
	h := m.Health()
	if h.Name != "crawler" {
		t.Errorf("Health().Name = %q", h.Name)
	}
}

func TestSetHealth(t *testing.T) {
	m := &Module{}
	m.setHealth(true, "Running")
	h := m.Health()
	if h.Status != "healthy" {
		t.Errorf("status = %q, want healthy", h.Status)
	}
}

func TestIsCrawling_Default(t *testing.T) {
	m := &Module{}
	if m.IsCrawling() {
		t.Error("should not be crawling by default")
	}
}

// ---------------------------------------------------------------------------
// errDisabled
// ---------------------------------------------------------------------------

func TestErrDisabled(t *testing.T) {
	if errDisabled == nil {
		t.Error("errDisabled should not be nil")
	}
}

// ---------------------------------------------------------------------------
// Disabled operations (nil repos)
// ---------------------------------------------------------------------------

func TestAddTarget_Disabled(t *testing.T) {
	m := &Module{}
	_, err := m.AddTarget("test", testExampleURL)
	if !errors.Is(err, errDisabled) {
		t.Errorf(errFmtErrDisabled, err)
	}
}

func TestRemoveTarget_Disabled(t *testing.T) {
	m := &Module{}
	err := m.RemoveTarget("id")
	if !errors.Is(err, errDisabled) {
		t.Errorf(errFmtErrDisabled, err)
	}
}

func TestGetTargets_Disabled(t *testing.T) {
	m := &Module{}
	_, err := m.GetTargets()
	if !errors.Is(err, errDisabled) {
		t.Errorf(errFmtErrDisabled, err)
	}
}

func TestGetDiscoveries_Disabled(t *testing.T) {
	m := &Module{}
	_, err := m.GetDiscoveries("pending")
	if !errors.Is(err, errDisabled) {
		t.Errorf(errFmtErrDisabled, err)
	}
}

func TestGetStats_NilRepos(t *testing.T) {
	m := &Module{}
	stats := m.GetStats()
	if stats.TotalTargets != 0 || stats.TotalDiscoveries != 0 {
		t.Errorf("stats should be zero with nil repos: %+v", stats)
	}
	if stats.Crawling {
		t.Error("should not be crawling")
	}
}

// ---------------------------------------------------------------------------
// Regex patterns existence
// ---------------------------------------------------------------------------

func TestRegexPatterns(t *testing.T) {
	if hrefRegex == nil {
		t.Error("hrefRegex should be compiled")
	}
	if m3u8Regex == nil {
		t.Error("m3u8Regex should be compiled")
	}
	if titleRegex == nil {
		t.Error("titleRegex should be compiled")
	}
}

func TestHrefRegex_Match(t *testing.T) {
	matches := hrefRegex.FindAllStringSubmatch(`<a href="https://example.com/video">link</a>`, -1)
	if len(matches) != 1 || matches[0][1] != "https://example.com/video" {
		t.Errorf("hrefRegex should match href: %v", matches)
	}
}

func TestM3u8Regex_Match(t *testing.T) {
	matches := m3u8Regex.FindAllString(`var url = "https://cdn.example.com/stream.m3u8?token=abc"`, -1)
	if len(matches) != 1 {
		t.Errorf("m3u8Regex should match M3U8 URL: %v", matches)
	}
}
