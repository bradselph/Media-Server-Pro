package scanner

import (
	"path/filepath"
	"testing"

	"media-server-pro/internal/config"
)

// ---------------------------------------------------------------------------
// MatureScanner.isAllowedExtension (R12): the periodic mature scan must cover
// every extension uploads can accept. Both internal/upload and the scanner
// read the same uploads.allowed_extensions config; a non-empty list must be
// authoritative for both, and an empty list must fall back to the same
// built-in media-extension set (pkg/helpers.IsMediaExtension) that
// internal/upload's built-in fallback uses, so the two never diverge.
// ---------------------------------------------------------------------------

func newTestScanner(t *testing.T) *MatureScanner {
	t.Helper()
	mgr := config.NewManager(filepath.Join(t.TempDir(), "config.json"))
	return NewMatureScanner(mgr)
}

func setScannerAllowedExtensions(t *testing.T, s *MatureScanner, exts []string) {
	t.Helper()
	if err := s.config.Update(func(cfg *config.Config) {
		cfg.Uploads.AllowedExtensions = exts
	}); err != nil {
		t.Fatalf("failed to update config: %v", err)
	}
}

func TestMatureIsAllowedExtension_ConfiguredListIsAuthoritative(t *testing.T) {
	s := newTestScanner(t)
	setScannerAllowedExtensions(t, s, []string{".mp4"})

	if !s.isAllowedExtension(".mp4") {
		t.Error(".mp4 should be scannable: it is in the configured list")
	}
	if s.isAllowedExtension(".mkv") {
		t.Error(".mkv should not be scannable: not in the configured, non-empty allow-list")
	}
}

// TestMatureIsAllowedExtension_EmptyConfigCoversUploadFallback pins the exact
// bug in R12: with no configured allow-list, uploads accept every extension in
// the built-in video/audio fallback (pkg/helpers mediaExtTypes), but the
// periodic scan previously only ever checked the (empty) configured list and
// therefore matched nothing. It must now fall back to the same set.
func TestMatureIsAllowedExtension_EmptyConfigCoversUploadFallback(t *testing.T) {
	s := newTestScanner(t)
	setScannerAllowedExtensions(t, s, []string{})

	// Extensions that internal/upload's built-in fallback accepts (videoExtensions
	// plus helpers.IsAudioExtension) but that were missing from the shipped
	// default uploads.allowed_extensions list.
	previouslySkipped := []string{
		".m4v", ".mpg", ".mpeg", ".3gp", ".ts", ".m2ts", ".vob", ".ogv",
		".opus", ".wma", ".alac", ".ape", ".aiff", ".mka",
	}
	for _, ext := range previouslySkipped {
		if !s.isAllowedExtension(ext) {
			t.Errorf("%s should be scannable via the built-in fallback when no list is configured", ext)
		}
	}
	if s.isAllowedExtension(".exe") {
		t.Error(".exe should not be scannable: not a recognized media extension")
	}
}

func TestMatureIsAllowedExtension_NormalizesMissingLeadingDot(t *testing.T) {
	s := newTestScanner(t)
	setScannerAllowedExtensions(t, s, []string{"mp4"})

	if !s.isAllowedExtension(".mp4") {
		t.Error(".mp4 should match a configured entry missing its leading dot")
	}
}

func TestMatureIsAllowedExtension_ConfiguredEntryCaseInsensitive(t *testing.T) {
	s := newTestScanner(t)
	setScannerAllowedExtensions(t, s, []string{".MP4"})

	if !s.isAllowedExtension(".mp4") {
		t.Error(".mp4 should match a configured entry regardless of case")
	}
}

// TestMatureIsAllowedExtension_ShippedDefaultConfigFallsBackToBuiltins pins
// the same regression as internal/upload's identical test: newTestScanner,
// with no override, starts from defaultUploadsConfig's non-empty
// 13-extension list (internal/config/defaults.go), not an empty one -- the
// real state of every fresh or pre-existing install. The periodic scan must
// still cover every extension internal/upload accepts by default, or the two
// silently drift apart again (the exact divergence R12 fixed).
func TestMatureIsAllowedExtension_ShippedDefaultConfigFallsBackToBuiltins(t *testing.T) {
	s := newTestScanner(t) // no override: exercises the real shipped default

	previouslyScanned := []string{
		".ts", ".m4v", ".mpg", ".mpeg", ".3gp", ".m2ts", ".vob", ".ogv",
		".opus", ".wma", ".alac", ".ape", ".aiff", ".mka",
	}
	for _, ext := range previouslyScanned {
		if !s.isAllowedExtension(ext) {
			t.Errorf("%s should be scannable under the shipped default config", ext)
		}
	}
}

// TestMatureIsAllowedExtension_NarrowerThanShippedDefaultStillNarrows guards
// against the shipped-default detection over-firing: a genuinely narrower
// configured list must still be authoritative.
func TestMatureIsAllowedExtension_NarrowerThanShippedDefaultStillNarrows(t *testing.T) {
	s := newTestScanner(t)
	setScannerAllowedExtensions(t, s, []string{".mp4", ".mkv"})

	if !s.isAllowedExtension(".mp4") {
		t.Error(".mp4 should be scannable: it is in the configured list")
	}
	if s.isAllowedExtension(".mp3") {
		t.Error(".mp3 should not be scannable: configured list is non-empty and narrower than the shipped default")
	}
}
