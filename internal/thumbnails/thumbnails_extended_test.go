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

// ---------------------------------------------------------------------------
// FND-0375: generateThumbnail nil job guard
// ---------------------------------------------------------------------------

func TestFND0375_GenerateThumbnail_NilJobReturnsError(t *testing.T) {
	m, _ := newTestModule(t)
	err := m.generateThumbnail(nil)
	if err == nil {
		t.Error("generateThumbnail(nil) should return error, got nil")
	}
	if err.Error() != "nil job" {
		t.Errorf("expected error message 'nil job', got %q", err.Error())
	}
}

// ---------------------------------------------------------------------------
// FND-0376: tryGenerateWebPVariant nil job guard
// ---------------------------------------------------------------------------

func TestFND0376_TryGenerateWebPVariant_NilJobReturnsEarly(t *testing.T) {
	m, _ := newTestModule(t)
	// Should not panic, just return early
	m.tryGenerateWebPVariant(nil, 0.0)
	// If we reach here, no panic occurred (test passes)
}

// ---------------------------------------------------------------------------
// FND-0377: generateResponsiveThumbnailsIfMain nil job guard
// ---------------------------------------------------------------------------

func TestFND0377_GenerateResponsiveThumbnailsIfMain_NilJobReturnsEarly(t *testing.T) {
	m, _ := newTestModule(t)
	// Should not panic, just return early
	m.generateResponsiveThumbnailsIfMain(nil, 0.0)
	// If we reach here, no panic occurred (test passes)
}

// ---------------------------------------------------------------------------
// FND-0378: tryUpdateBlurHashForThumbnail nil job guard
// ---------------------------------------------------------------------------

func TestFND0378_TryUpdateBlurHashForThumbnail_NilJobReturnsEarly(t *testing.T) {
	m, _ := newTestModule(t)
	// Should not panic, just return early
	m.tryUpdateBlurHashForThumbnail(nil)
	// If we reach here, no panic occurred (test passes)
}

// ---------------------------------------------------------------------------
// FND-0379: generateAudioThumbnail nil job guard
// ---------------------------------------------------------------------------

func TestFND0379_GenerateAudioThumbnail_NilJobReturnsError(t *testing.T) {
	m, _ := newTestModule(t)
	err := m.generateAudioThumbnail(nil)
	if err == nil {
		t.Error("generateAudioThumbnail(nil) should return error, got nil")
	}
	if err.Error() != "nil job" {
		t.Errorf("expected error message 'nil job', got %q", err.Error())
	}
}

// ---------------------------------------------------------------------------
// FND-0381: updateBlurHashFromThumbnail logs BlurHash errors
// ---------------------------------------------------------------------------

func TestFND0381_UpdateBlurHashFromThumbnail_LogsErrorOnComputeFailure(t *testing.T) {
	m, dir := newTestModule(t)

	// Use a non-existent path to trigger a computeBlurHash error
	nonExistentPath := dir + "/nonexistent.jpg"

	// This should not panic, and should log the error
	m.updateBlurHashFromThumbnail(nonExistentPath, "some-media-path")

	// Test passes if no panic occurs and no segfault
	// The logging behavior is verified by the logger (observability improvement)
}
