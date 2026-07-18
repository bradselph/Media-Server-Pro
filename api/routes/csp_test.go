package routes

import (
	"strings"
	"testing"
)

// The default CSP shipped in internal/config/defaults.go. Kept here so the test
// exercises hubAugmentCSP against the real policy shape (no frame-src; img-src
// without the embed CDN).
const defaultCSP = "default-src 'self'; script-src 'self' 'unsafe-inline' https://cdn.jsdelivr.net https://static.cloudflareinsights.com; style-src 'self' 'unsafe-inline' https://cdn.jsdelivr.net; font-src 'self' https://cdn.jsdelivr.net https://fonts.gstatic.com; img-src 'self' data: blob:; media-src 'self' blob:; worker-src 'self' blob:; connect-src 'self' blob: https://api.iconify.design"

func TestHubAugmentCSP_AddsFrameAndImgSources(t *testing.T) {
	out := hubAugmentCSP(defaultCSP)

	// frame-src was absent in the base policy — it must be added so the browser
	// stops blocking the embed iframes ("This content is blocked…").
	if !strings.Contains(out, "frame-src") {
		t.Fatalf("expected a frame-src directive, got: %s", out)
	}
	frameDir := directive(out, "frame-src")
	if !strings.Contains(frameDir, hubEmbedFrameSrc) {
		t.Errorf("frame-src missing %q: %s", hubEmbedFrameSrc, frameDir)
	}
	if !strings.Contains(frameDir, "'self'") {
		t.Errorf("added frame-src should be seeded with 'self': %s", frameDir)
	}

	// img-src existed — the CDN must be appended to it (not duplicated).
	if n := strings.Count(out, "img-src"); n != 1 {
		t.Errorf("expected exactly one img-src directive, found %d: %s", n, out)
	}
	imgDir := directive(out, "img-src")
	if !strings.Contains(imgDir, "https://*.phncdn.com") {
		t.Errorf("img-src missing the phncdn CDN: %s", imgDir)
	}
	if !strings.Contains(imgDir, "'self'") || !strings.Contains(imgDir, "data:") {
		t.Errorf("img-src should retain its original sources: %s", imgDir)
	}
}

func TestCSPAddSources_AddsDirectiveWhenAbsent(t *testing.T) {
	out := cspAddSources("default-src 'self'", "frame-src", "https://example.com")
	if directive(out, "frame-src") != "frame-src 'self' https://example.com" {
		t.Errorf("unexpected: %s", out)
	}
}

func TestHubAugmentCSP_EmptyIsNoop(t *testing.T) {
	if hubAugmentCSP("") != "" {
		t.Error("empty policy should stay empty")
	}
}

// directive returns the single ';'-separated directive (trimmed) whose first
// token matches name, or "" if absent.
func directive(csp, name string) string {
	for _, p := range strings.Split(csp, ";") {
		fields := strings.Fields(p)
		if len(fields) > 0 && fields[0] == name {
			return strings.TrimSpace(p)
		}
	}
	return ""
}
