package handlers

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"media-server-pro/internal/receiver"
	"media-server-pro/pkg/models"
)

func TestSanitizeReceiverFilename(t *testing.T) {
	cases := []struct{ in, want string }{
		{"normal name", "normal name"},
		{"a/b\\c", "a_b_c"},          // path separators neutralized
		{"bad:name?*", "bad_name__"}, // reserved chars neutralized
		{"  .trim. ", "trim"},        // leading/trailing spaces+dots trimmed
		{"tab\there", "tab_here"},    // control chars neutralized
		{"", ""},                     // empty stays empty (caller substitutes)
	}
	for _, c := range cases {
		if got := sanitizeReceiverFilename(c.in); got != c.want {
			t.Errorf("sanitizeReceiverFilename(%q) = %q, want %q", c.in, got, c.want)
		}
	}
	// Length is bounded on a rune boundary.
	if got := sanitizeReceiverFilename(strings.Repeat("x", 300)); len([]rune(got)) > 150 {
		t.Errorf("expected <=150 runes, got %d", len([]rune(got)))
	}
}

func TestReceiverCopyName(t *testing.T) {
	// Extension comes from the remote path; display name is kept.
	if base, ext := receiverCopyName(&receiver.MediaItem{Name: "My Clip", Path: "/x/y/clip.mkv", MediaType: "video"}); base != "My Clip" || ext != ".mkv" {
		t.Errorf("path ext: base=%q ext=%q, want %q/%q", base, ext, "My Clip", ".mkv")
	}

	// No usable path extension → default by media type.
	if _, ext := receiverCopyName(&receiver.MediaItem{Name: "song", Path: "noext", MediaType: string(models.MediaTypeAudio)}); ext != ".mp3" {
		t.Errorf("audio default ext = %q, want .mp3", ext)
	}
	if _, ext := receiverCopyName(&receiver.MediaItem{Name: "clip", Path: "noext", MediaType: string(models.MediaTypeVideo)}); ext != ".mp4" {
		t.Errorf("video default ext = %q, want .mp4", ext)
	}

	// A name that already ends in the extension must not become "movie.mp4.mp4".
	if base, ext := receiverCopyName(&receiver.MediaItem{Name: "movie.mp4", Path: "movie.mp4", MediaType: "video"}); base != "movie" || ext != ".mp4" {
		t.Errorf("double-ext: base=%q ext=%q, want movie/.mp4", base, ext)
	}

	// Empty display name falls back to the remote path's base name.
	if base, _ := receiverCopyName(&receiver.MediaItem{Name: "", Path: "/a/b/thefile.webm", MediaType: "video"}); base != "thefile" {
		t.Errorf("empty-name fallback base = %q, want thefile", base)
	}
}

func TestMoveToUniqueName(t *testing.T) {
	dir := t.TempDir()
	newSrc := func() string {
		f, err := os.CreateTemp(dir, "src-*")
		if err != nil {
			t.Fatalf("create temp: %v", err)
		}
		_ = f.Close()
		return f.Name()
	}

	p1, err := moveToUniqueName(newSrc(), dir, "clip", ".mp4")
	if err != nil {
		t.Fatalf("first move: %v", err)
	}
	if got := filepath.Base(p1); got != "clip.mp4" {
		t.Errorf("first name = %q, want clip.mp4", got)
	}

	// Second copy of the same name gets a numeric suffix, not an overwrite.
	p2, err := moveToUniqueName(newSrc(), dir, "clip", ".mp4")
	if err != nil {
		t.Fatalf("second move: %v", err)
	}
	if got := filepath.Base(p2); got != "clip_1.mp4" {
		t.Errorf("second name = %q, want clip_1.mp4", got)
	}

	// Both files must still exist (no clobbering).
	for _, p := range []string{p1, p2} {
		if _, statErr := os.Stat(p); statErr != nil {
			t.Errorf("expected %q to exist: %v", p, statErr)
		}
	}
}
