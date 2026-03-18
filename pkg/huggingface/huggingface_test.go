package huggingface

import (
	"math"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// resolveBaseURL
// ---------------------------------------------------------------------------

func TestResolveBaseURL_Custom(t *testing.T) {
	got := resolveBaseURL("https://custom.hf.api/v1/")
	if got != "https://custom.hf.api/v1" {
		t.Errorf("resolveBaseURL = %q, want trailing slash stripped", got)
	}
}

func TestResolveBaseURL_Empty(t *testing.T) {
	got := resolveBaseURL("")
	if got != defaultBaseURL {
		t.Errorf("resolveBaseURL('') = %q, want %q", got, defaultBaseURL)
	}
}

func TestResolveBaseURL_NoTrailingSlash(t *testing.T) {
	got := resolveBaseURL("https://api.hf.co")
	if got != "https://api.hf.co" {
		t.Errorf("resolveBaseURL = %q", got)
	}
}

// ---------------------------------------------------------------------------
// parseErrorBody
// ---------------------------------------------------------------------------

func TestParseErrorBody_Valid(t *testing.T) {
	body := []byte(`{"error": "Model not found"}`)
	got := parseErrorBody(body)
	if got != "Model not found" {
		t.Errorf("parseErrorBody = %q, want %q", got, "Model not found")
	}
}

func TestParseErrorBody_Empty(t *testing.T) {
	got := parseErrorBody([]byte(`{}`))
	if got != "" {
		t.Errorf("parseErrorBody({}) = %q, want empty", got)
	}
}

func TestParseErrorBody_InvalidJSON(t *testing.T) {
	got := parseErrorBody([]byte(`not json`))
	if got != "" {
		t.Errorf("parseErrorBody(invalid) = %q, want empty", got)
	}
}

func TestParseErrorBody_NilBody(t *testing.T) {
	got := parseErrorBody(nil)
	if got != "" {
		t.Errorf("parseErrorBody(nil) = %q, want empty", got)
	}
}

// ---------------------------------------------------------------------------
// bodySummary
// ---------------------------------------------------------------------------

func TestBodySummary_EmptyBody(t *testing.T) {
	got := bodySummary(nil, 200)
	if got != "" {
		t.Errorf("bodySummary(nil) = %q, want empty", got)
	}
}

func TestBodySummary_ShortBody(t *testing.T) {
	got := bodySummary([]byte(`{"ok": true}`), 200)
	if got == "" {
		t.Error("bodySummary should return non-empty for short body")
	}
}

// ---------------------------------------------------------------------------
// parseWordsAsTags
// ---------------------------------------------------------------------------

func TestParseWordsAsTags_Simple(t *testing.T) {
	tags := parseWordsAsTags("A beautiful sunset over the ocean")
	if len(tags) == 0 {
		t.Fatal("should extract tags")
	}
	// Should lowercase and filter short words
	for _, tag := range tags {
		if tag != strings.ToLower(tag) {
			t.Errorf("tag should be lowercase: %q", tag)
		}
		if len(tag) < 2 {
			t.Errorf("tag should be 2+ chars: %q", tag)
		}
	}
}

func TestParseWordsAsTags_Empty(t *testing.T) {
	tags := parseWordsAsTags("")
	if len(tags) != 0 {
		t.Errorf("empty caption should produce no tags, got %d", len(tags))
	}
}

func TestParseWordsAsTags_Deduplication(t *testing.T) {
	tags := parseWordsAsTags("the the the cat cat")
	seen := make(map[string]bool)
	for _, tag := range tags {
		if seen[tag] {
			t.Errorf("duplicate tag: %q", tag)
		}
		seen[tag] = true
	}
}

// ---------------------------------------------------------------------------
// computeTimestamps
// ---------------------------------------------------------------------------

func TestComputeTimestamps_Basic(t *testing.T) {
	ts := computeTimestamps(100.0, 3)
	if len(ts) != 3 {
		t.Fatalf("expected 3 timestamps, got %d", len(ts))
	}
	// Evenly spaced: 25, 50, 75
	for i, expected := range []float64{25.0, 50.0, 75.0} {
		if math.Abs(ts[i]-expected) > 0.01 {
			t.Errorf("timestamp[%d] = %f, want %f", i, ts[i], expected)
		}
	}
}

func TestComputeTimestamps_Single(t *testing.T) {
	ts := computeTimestamps(60.0, 1)
	if len(ts) != 1 {
		t.Fatalf("expected 1 timestamp, got %d", len(ts))
	}
	if math.Abs(ts[0]-30.0) > 0.01 {
		t.Errorf("single timestamp = %f, want 30.0", ts[0])
	}
}

// ---------------------------------------------------------------------------
// sanitizeBaseNameRune
// ---------------------------------------------------------------------------

func TestSanitizeBaseNameRune_Alpha(t *testing.T) {
	if sanitizeBaseNameRune('a') != 'a' {
		t.Error("lowercase letter should be kept")
	}
	if sanitizeBaseNameRune('Z') != 'Z' {
		t.Error("uppercase letter should be kept")
	}
}

func TestSanitizeBaseNameRune_Digit(t *testing.T) {
	if sanitizeBaseNameRune('5') != '5' {
		t.Error("digit should be kept")
	}
}

func TestSanitizeBaseNameRune_Special(t *testing.T) {
	if sanitizeBaseNameRune('-') != '-' {
		t.Error("hyphen should be kept")
	}
	if sanitizeBaseNameRune('_') != '_' {
		t.Error("underscore should be kept")
	}
}

func TestSanitizeBaseNameRune_Other(t *testing.T) {
	if sanitizeBaseNameRune(' ') != '_' {
		t.Error("space should become underscore")
	}
	if sanitizeBaseNameRune('@') != '_' {
		t.Error("@ should become underscore")
	}
}

// ---------------------------------------------------------------------------
// sanitizeBaseName
// ---------------------------------------------------------------------------

func TestSanitizeBaseName_Normal(t *testing.T) {
	got := sanitizeBaseName("/videos/my-video.mp4")
	if got != "my-video" {
		t.Errorf("sanitizeBaseName = %q, want my-video", got)
	}
}

func TestSanitizeBaseName_SpecialChars(t *testing.T) {
	got := sanitizeBaseName("/videos/my video @2024.mp4")
	if strings.ContainsAny(got, " @") {
		t.Errorf("sanitizeBaseName should replace special chars: %q", got)
	}
}

// ---------------------------------------------------------------------------
// checkVideoExtension
// ---------------------------------------------------------------------------

func TestCheckVideoExtension_Valid(t *testing.T) {
	for _, ext := range []string{".mp4", ".mkv", ".avi", ".mov", ".webm"} {
		if err := checkVideoExtension(ext); err != nil {
			t.Errorf("checkVideoExtension(%q) should pass: %v", ext, err)
		}
	}
}

func TestCheckVideoExtension_Invalid(t *testing.T) {
	for _, ext := range []string{".txt", ".pdf", ".exe", ".doc"} {
		if err := checkVideoExtension(ext); err == nil {
			t.Errorf("checkVideoExtension(%q) should fail", ext)
		}
	}
}
