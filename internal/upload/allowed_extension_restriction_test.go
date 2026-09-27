package upload

import (
	"testing"

	"media-server-pro/internal/config"
)

// ---------------------------------------------------------------------------
// isAllowedExtension — configured allowed_extensions must actually restrict
// uploads (R05): a non-empty configured list is authoritative and narrows
// accepted types below the built-in video/audio fallback, not just adds to it.
// ---------------------------------------------------------------------------

// setAllowedExtensions replaces the module's configured allow-list for the test.
func setAllowedExtensions(t *testing.T, m *Module, exts []string) {
	t.Helper()
	if err := m.config.Update(func(cfg *config.Config) {
		cfg.Uploads.AllowedExtensions = exts
	}); err != nil {
		t.Fatalf("failed to update config: %v", err)
	}
}

func TestIsAllowedExtension_ConfiguredListNarrowsBelowBuiltins(t *testing.T) {
	m := newTestModule(t)
	setAllowedExtensions(t, m, []string{".mp4"})

	if !m.isAllowedExtension(".mp4") {
		t.Error(".mp4 should be allowed: it is in the configured list")
	}
	// .mkv and .mp3 are both accepted by the built-in fallback lists, but the
	// admin restricted uploads to .mp4 only — the configured list must win.
	for _, ext := range []string{".mkv", ".mp3", ".avi", ".wav"} {
		if m.isAllowedExtension(ext) {
			t.Errorf("%s should be rejected: not in the configured allow-list, and the list is non-empty", ext)
		}
	}
}

func TestIsAllowedExtension_EmptyConfigFallsBackToBuiltins(t *testing.T) {
	m := newTestModule(t)
	setAllowedExtensions(t, m, []string{})

	// With no configured list at all, the built-in video/audio sets remain the default.
	if !m.isAllowedExtension(".mp4") {
		t.Error(".mp4 should be allowed via the built-in fallback when no list is configured")
	}
	if !m.isAllowedExtension(".mp3") {
		t.Error(".mp3 should be allowed via the built-in fallback when no list is configured")
	}
	if m.isAllowedExtension(".exe") {
		t.Error(".exe should still be rejected: not in the built-in fallback")
	}
}

func TestIsAllowedExtension_NormalizesMissingLeadingDot(t *testing.T) {
	m := newTestModule(t)
	// Admin-entered values may omit the leading dot (e.g. "mp4" instead of ".mp4").
	setAllowedExtensions(t, m, []string{"mp4"})

	if !m.isAllowedExtension(".mp4") {
		t.Error(".mp4 should match a configured entry missing its leading dot")
	}
	if m.isAllowedExtension(".mkv") {
		t.Error(".mkv should still be rejected under a narrowed, non-empty list")
	}
}

func TestIsAllowedExtension_ConfiguredEntryCaseInsensitive(t *testing.T) {
	m := newTestModule(t)
	setAllowedExtensions(t, m, []string{".MP4"})

	if !m.isAllowedExtension(".mp4") {
		t.Error(".mp4 should match a configured entry regardless of case")
	}
}

// TestIsAllowedExtension_ShippedDefaultConfigFallsBackToBuiltins pins the
// regression a reviewer flagged in the R05 fix: config.NewManager (and every
// existing config.json that predates a custom setting) starts from
// defaultUploadsConfig's non-empty 13-extension list, not an empty one. Using
// newTestModule directly, with no override at all, reproduces exactly that
// "fresh/existing install, admin never touched the setting" state. Extensions
// that the built-in fallback has always accepted, but that are missing from
// the shipped default list, must still be allowed -- otherwise every install
// silently rejects .ts/.opus/etc. uploads that worked before the R05 fix.
func TestIsAllowedExtension_ShippedDefaultConfigFallsBackToBuiltins(t *testing.T) {
	m := newTestModule(t) // no setAllowedExtensions call: exercises the real shipped default

	previouslyWorking := []string{
		".ts", ".m4v", ".mpg", ".mpeg", ".3gp", ".m2ts", ".vob", ".ogv",
		".opus", ".wma", ".alac", ".ape", ".aiff", ".mka",
	}
	for _, ext := range previouslyWorking {
		if !m.isAllowedExtension(ext) {
			t.Errorf("%s should be allowed under the shipped default config, as it was before the R05 fix", ext)
		}
	}
	if m.isAllowedExtension(".exe") {
		t.Error(".exe should still be rejected: not a recognized media extension")
	}
}

// TestIsAllowedExtension_ReconfiguredShippedDefaultFallsBackToBuiltins covers
// an admin who saves the settings form without changing the shipped default
// list (so it round-trips through config.json unchanged): behavior must stay
// identical to leaving the setting untouched.
func TestIsAllowedExtension_ReconfiguredShippedDefaultFallsBackToBuiltins(t *testing.T) {
	m := newTestModule(t)
	setAllowedExtensions(t, m, []string{
		".mp4", ".mkv", ".avi", ".mov", ".wmv", ".flv", ".webm",
		".mp3", ".wav", ".flac", ".aac", ".ogg", ".m4a",
	})

	if !m.isAllowedExtension(".ts") {
		t.Error(".ts should be allowed: the configured list is still exactly the shipped default")
	}
	if !m.isAllowedExtension(".opus") {
		t.Error(".opus should be allowed: the configured list is still exactly the shipped default")
	}
}

// TestIsAllowedExtension_NarrowerThanShippedDefaultStillNarrows guards against
// the shipped-default detection from over-firing: a genuinely narrower list
// (fewer entries than the shipped default) must still be treated as an
// authoritative, restrictive allow-list, not mistaken for "unconfigured".
func TestIsAllowedExtension_NarrowerThanShippedDefaultStillNarrows(t *testing.T) {
	m := newTestModule(t)
	setAllowedExtensions(t, m, []string{".mp4", ".mkv"})

	if !m.isAllowedExtension(".mp4") {
		t.Error(".mp4 should be allowed: it is in the configured list")
	}
	if m.isAllowedExtension(".mp3") {
		t.Error(".mp3 should be rejected: configured list is non-empty and narrower than the shipped default")
	}
}
