package thumbnails

import (
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// formatBytes
// ---------------------------------------------------------------------------

func TestFormatBytes(t *testing.T) {
	tests := []struct {
		input int64
		want  string
	}{
		{0, "0 B"},
		{500, "500 B"},
		{1023, "1023 B"},
		{1024, "1.0 KB"},
		{1536, "1.5 KB"},
		{1048576, "1.0 MB"},
		{1073741824, "1.0 GB"},
	}
	for _, tc := range tests {
		got := formatBytes(tc.input)
		if got != tc.want {
			t.Errorf("formatBytes(%d) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

// ---------------------------------------------------------------------------
// getThumbnailPathByIndex (method on *Module)
// ---------------------------------------------------------------------------

func TestGetThumbnailPathByIndex_Zero(t *testing.T) {
	m := &Module{thumbnailDir: "/data/thumbs"}
	got := m.getThumbnailPathByIndex("abc-123", 0)
	if got == "" {
		t.Error("path should not be empty")
	}
	// Should end with .jpg
	if len(got) < 4 || got[len(got)-4:] != ".jpg" {
		t.Errorf("path should end with .jpg, got %q", got)
	}
}

func TestGetThumbnailPathByIndex_Preview(t *testing.T) {
	m := &Module{thumbnailDir: "/data/thumbs"}
	got := m.getThumbnailPathByIndex("abc-123", 3)
	if got == "" {
		t.Error("path should not be empty")
	}
	// Preview index is N-1 in the filename
	if !strings.Contains(got, "preview_2") {
		t.Errorf("index 3 should produce preview_2 filename, got %q", got)
	}
}

// ---------------------------------------------------------------------------
// getThumbnailPathWebp
// ---------------------------------------------------------------------------

func TestGetThumbnailPathWebp(t *testing.T) {
	m := &Module{}
	got := m.getThumbnailPathWebp("/data/thumbs/abc.jpg")
	if got != "/data/thumbs/abc.webp" {
		t.Errorf("getThumbnailPathWebp = %q, want /data/thumbs/abc.webp", got)
	}
}

func TestGetThumbnailPathWebp_NoJpg(t *testing.T) {
	m := &Module{}
	got := m.getThumbnailPathWebp("/data/thumbs/abc.png")
	// Should just append .webp since there's no .jpg to strip
	if got == "" {
		t.Error("should return non-empty")
	}
}
