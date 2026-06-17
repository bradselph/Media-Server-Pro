package handlers

import (
	"strings"
	"testing"

	"media-server-pro/pkg/models"
)

func TestToISO8601Duration(t *testing.T) {
	cases := map[float64]string{
		0:    "PT0S",
		-5:   "PT0S",
		45:   "PT45S",
		90:   "PT1M30S",
		3600: "PT1H",
		3661: "PT1H1M1S",
		7200: "PT2H",
		3725: "PT1H2M5S",
	}
	for secs, want := range cases {
		if got := toISO8601Duration(secs); got != want {
			t.Errorf("toISO8601Duration(%v) = %q, want %q", secs, got, want)
		}
	}
}

func TestCleanupShellTitle(t *testing.T) {
	cases := map[string]string{
		"My_Great_Clip.mp4": "My Great Clip",
		"holiday.MP4":       "holiday",
		"a-b-c":             "a-b-c", // hyphens preserved
		"  spaced   out  ":  "spaced out",
		"plain title":       "plain title",
	}
	for in, want := range cases {
		if got := cleanupShellTitle(in); got != want {
			t.Errorf("cleanupShellTitle(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestShellMediaTitle_PrefersMetadataTitle(t *testing.T) {
	item := &models.MediaItem{
		Name:     "raw_file_name.mp4",
		Metadata: map[string]string{"title": "Curated Title"},
	}
	if got := shellMediaTitle(item); got != "Curated Title" {
		t.Errorf("expected metadata title, got %q", got)
	}
}

func TestShellMediaTitle_FallsBackToCleanedName(t *testing.T) {
	item := &models.MediaItem{Name: "raw_file_name.mp4"}
	if got := shellMediaTitle(item); got != "raw file name" {
		t.Errorf("expected cleaned name, got %q", got)
	}
}

func TestShellMediaDescription_GeneratedFallback(t *testing.T) {
	item := &models.MediaItem{Type: models.MediaTypeVideo, Category: "Featured"}
	got := shellMediaDescription(item, "My Clip")
	if !strings.Contains(got, "My Clip") || !strings.Contains(got, "video") || !strings.Contains(got, "Featured") {
		t.Errorf("unexpected generated description: %q", got)
	}
}

// A title containing angle brackets must not be able to break out of the
// <script type="application/ld+json"> block. After escaping, the only raw '<'
// characters left are the two in the <script>...</script> wrapper itself.
func TestPlayerJSONLD_EscapesAngleBracket(t *testing.T) {
	const lt = "<"
	item := &models.MediaItem{Duration: 60}
	out := playerJSONLD("VideoObject", "evil"+lt+"/script"+lt+"script>alert(1)", "desc", item, "")

	if strings.Contains(out, lt+"/script"+lt+"script>") {
		t.Errorf("angle bracket not escaped, XSS possible: %s", out)
	}
	if n := strings.Count(out, lt); n != 2 {
		t.Errorf("expected only the 2 wrapper '<' chars, got %d (payload not escaped): %s", n, out)
	}
	if n := strings.Count(out, "</script>"); n != 1 {
		t.Errorf("expected exactly 1 </script> (the wrapper), got %d: %s", n, out)
	}
}
