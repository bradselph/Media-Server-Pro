package thumbnails

import (
	"testing"
)

// ---------------------------------------------------------------------------
// isPreviewThumbnail
// ---------------------------------------------------------------------------

func TestIsPreviewThumbnail_True(t *testing.T) {
	if !isPreviewThumbnail("/thumbs/video_preview_001.jpg") {
		t.Error("path with _preview_ should return true")
	}
}

func TestIsPreviewThumbnail_False(t *testing.T) {
	if isPreviewThumbnail("/thumbs/video_thumb.jpg") {
		t.Error("path without _preview_ should return false")
	}
}

func TestIsPreviewThumbnail_InMiddle(t *testing.T) {
	if !isPreviewThumbnail("/thumbs/some_preview_frame.jpg") {
		t.Error("_preview_ in middle of filename should return true")
	}
}

// ---------------------------------------------------------------------------
// Module basics
// ---------------------------------------------------------------------------

func TestModuleName(t *testing.T) {
	m := &Module{}
	if m.Name() != "thumbnails" {
		t.Errorf("Name() = %q, want %q", m.Name(), "thumbnails")
	}
}

func TestModuleHealth_Default(t *testing.T) {
	m := &Module{}
	h := m.Health()
	if h.Name != "thumbnails" {
		t.Errorf("Health().Name = %q", h.Name)
	}
}
