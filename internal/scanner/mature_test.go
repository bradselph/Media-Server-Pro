package scanner

import (
	"testing"
)

// TestBuildKeywordPatterns verifies that buildKeywordPatterns compiles patterns
// that correctly apply filename-aware boundary matching.
func TestBuildKeywordPatterns(t *testing.T) {
	patterns := buildKeywordPatterns([]string{"ass", "porn", "lap dance"})
	if len(patterns) != 3 {
		t.Fatalf("expected 3 patterns, got %d", len(patterns))
	}

	// "ass" must NOT match substrings embedded inside longer words (false-positive guard).
	// The boundary (?:^|[^a-z0-9]) means the character immediately before the keyword
	// must be non-alphanumeric (or start of string).
	noMatch := []string{"class", "grassy", "grasslands", "assemble", "harass", "cassette", "passage"}
	for _, s := range noMatch {
		if patterns[0].pattern.MatchString(s) {
			t.Errorf("pattern %q incorrectly matched %q (false positive)", patterns[0].raw, s)
		}
	}

	// "ass" must match when it appears as a distinct token separated by non-alphanumeric chars.
	shouldMatch := []string{"ass", "big ass", "ass.mp4", "ass_shot", "ass-shot", "an ass here"}
	for _, s := range shouldMatch {
		if !patterns[0].pattern.MatchString(s) {
			t.Errorf("pattern %q failed to match %q (false negative)", patterns[0].raw, s)
		}
	}

	// "porn" must not match inside "pornstar" — 's' after "porn" is alphanumeric.
	if patterns[1].pattern.MatchString("pornstar_video.mp4") {
		// "pornstar" has its own keyword; "porn" alone should not hit due to boundary
		t.Errorf("pattern 'porn' should not match inside 'pornstar_video.mp4' (no boundary)")
	}
	if !patterns[1].pattern.MatchString("porn.mp4") {
		t.Errorf("pattern 'porn' must match 'porn.mp4'")
	}
	if !patterns[1].pattern.MatchString("porn") {
		t.Errorf("pattern 'porn' must match the standalone word 'porn'")
	}

	// "lap dance" should match variants with space/hyphen/underscore/no separator.
	lapPattern := patterns[2].pattern
	for _, s := range []string{"lap dance", "lap-dance", "lap_dance", "lapdance"} {
		if !lapPattern.MatchString(s) {
			t.Errorf("pattern 'lap dance' failed to match variant %q", s)
		}
	}
}

// TestScanHighConfidenceKeywords verifies that the pre-compiled high-confidence
// patterns detect known explicit filenames while avoiding common false positives.
func TestScanHighConfidenceKeywords(t *testing.T) {
	// These filenames should trigger high-confidence matches.
	// Underscores and hyphens are treated as token separators in filenames.
	positives := []string{
		"xxx_video.mp4",
		"hardcore_scene.mkv",
		"pornstar_interview.avi",
	}
	for _, name := range positives {
		result := &ScanResult{}
		score := scanHighConfidenceKeywords(name, result)
		if score == 0 {
			t.Errorf("scanHighConfidenceKeywords(%q): expected non-zero score, got 0", name)
		}
	}

	// These filenames contain alphanumeric substrings that look like keywords but
	// have no token boundary — they must NOT be flagged.
	negatives := []string{
		"classroom_recording.mp4",    // "ass" is inside "classroom" — no boundary on left
		"grasslands_documentary.mkv", // "ass" is inside "grasslands" — no boundary on left
	}
	for _, name := range negatives {
		result := &ScanResult{}
		score := scanHighConfidenceKeywords(name, result)
		if score > 0 {
			t.Errorf("scanHighConfidenceKeywords(%q): false positive — score=%v, matches=%v",
				name, score, result.HighConfMatches)
		}
	}
}

// TestScanMediumConfidenceKeywords mirrors the above for the medium-confidence list.
func TestScanMediumConfidenceKeywords(t *testing.T) {
	// "sexy" should hit — it appears with a '_' boundary after it.
	result := &ScanResult{}
	score := scanMediumConfidenceKeywords("sexy_bikini_shoot.jpg", result)
	if score == 0 {
		t.Error("scanMediumConfidenceKeywords: expected non-zero score for 'sexy_bikini_shoot.jpg'")
	}

	// "bare" should NOT match inside "barefoot" — 'f' after "bare" is alphanumeric.
	result2 := &ScanResult{}
	score2 := scanMediumConfidenceKeywords("barefoot_hiking.mp4", result2)
	if score2 > 0 {
		t.Errorf("scanMediumConfidenceKeywords: false positive on 'barefoot_hiking.mp4' — score=%v matches=%v",
			score2, result2.MedConfMatches)
	}
}
