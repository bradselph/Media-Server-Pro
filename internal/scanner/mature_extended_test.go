package scanner

import (
	"testing"
)

// ---------------------------------------------------------------------------
// stableReviewID
// ---------------------------------------------------------------------------

func TestStableReviewID_Deterministic(t *testing.T) {
	id1 := stableReviewID("/path/to/file.mp4")
	id2 := stableReviewID("/path/to/file.mp4")
	if id1 != id2 {
		t.Error("stableReviewID should be deterministic")
	}
}

func TestStableReviewID_DifferentPaths(t *testing.T) {
	id1 := stableReviewID("/path/a.mp4")
	id2 := stableReviewID("/path/b.mp4")
	if id1 == id2 {
		t.Error("different paths should produce different IDs")
	}
}

func TestStableReviewID_UUIDFormat(t *testing.T) {
	id := stableReviewID("/test")
	// Should be UUID-like: xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx
	if len(id) != 36 {
		t.Errorf("stableReviewID length = %d, want 36 (UUID format)", len(id))
	}
	// Check dashes at correct positions
	if id[8] != '-' || id[13] != '-' || id[18] != '-' || id[23] != '-' {
		t.Errorf("stableReviewID format invalid: %s", id)
	}
}

// ---------------------------------------------------------------------------
// scanDirectoryPatterns
// ---------------------------------------------------------------------------

func TestScanDirectoryPatterns_NoMatch(t *testing.T) {
	result := &ScanResult{}
	score := scanDirectoryPatterns("/home/videos/family", result)
	if score > 0 {
		t.Errorf("family videos dir should not match, score = %f", score)
	}
}

func TestScanDirectoryPatterns_EmptyPath(t *testing.T) {
	result := &ScanResult{}
	score := scanDirectoryPatterns("", result)
	if score > 0 {
		t.Errorf("empty path should not match, score = %f", score)
	}
}

// ---------------------------------------------------------------------------
// scanMatureRegexPatterns
// ---------------------------------------------------------------------------

func TestScanMatureRegexPatterns_NoMatch(t *testing.T) {
	result := &ScanResult{}
	score := scanMatureRegexPatterns("holiday_vacation_2024.mp4", result)
	if score > 0 {
		t.Errorf("innocent filename should not match, score = %f", score)
	}
}

func TestScanMatureRegexPatterns_EmptyFilename(t *testing.T) {
	result := &ScanResult{}
	score := scanMatureRegexPatterns("", result)
	if score > 0 {
		t.Errorf("empty filename should not match, score = %f", score)
	}
}
