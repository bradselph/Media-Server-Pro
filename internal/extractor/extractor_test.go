package extractor

import (
	"strings"
	"testing"
	"time"

	"media-server-pro/internal/repositories"
)

// ---------------------------------------------------------------------------
// generateID
// ---------------------------------------------------------------------------

func TestGenerateID_Deterministic(t *testing.T) {
	id1 := generateID("https://example.com/stream.m3u8")
	id2 := generateID("https://example.com/stream.m3u8")
	if id1 != id2 {
		t.Error("same input should produce same ID")
	}
}

func TestGenerateID_Prefix(t *testing.T) {
	id := generateID("https://example.com/stream.m3u8")
	if !strings.HasPrefix(id, "ext_") {
		t.Errorf("ID should start with 'ext_': %s", id)
	}
}

func TestGenerateID_DifferentInputs(t *testing.T) {
	id1 := generateID("https://example.com/a.m3u8")
	id2 := generateID("https://example.com/b.m3u8")
	if id1 == id2 {
		t.Error("different inputs should produce different IDs")
	}
}

// ---------------------------------------------------------------------------
// resolveBaseURL
// ---------------------------------------------------------------------------

func TestResolveBaseURL(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"https://cdn.example.com/hls/stream/master.m3u8", "https://cdn.example.com/hls/stream/"},
		{"https://cdn.example.com/video.m3u8", "https://cdn.example.com/"},
		{"https://cdn.example.com/path/to/playlist.m3u8?token=abc", "https://cdn.example.com/path/to/"},
	}
	for _, tc := range tests {
		got := resolveBaseURL(tc.input)
		if got != tc.want {
			t.Errorf("resolveBaseURL(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestResolveBaseURL_InvalidURL(t *testing.T) {
	// Should return the raw URL on parse failure
	got := resolveBaseURL("://bad")
	if got == "" {
		t.Error("should return something for invalid URL")
	}
}

// ---------------------------------------------------------------------------
// resolveURL
// ---------------------------------------------------------------------------

func TestResolveURL_AbsoluteURL(t *testing.T) {
	got := resolveURL("https://cdn.example.com/hls/", "https://other.com/segment.ts")
	if got != "https://other.com/segment.ts" {
		t.Errorf("absolute URL should be returned as-is: %s", got)
	}
}

func TestResolveURL_RelativeURL(t *testing.T) {
	got := resolveURL("https://cdn.example.com/hls/stream/", "segment001.ts")
	if got != "https://cdn.example.com/hls/stream/segment001.ts" {
		t.Errorf("relative URL should be resolved: %s", got)
	}
}

func TestResolveURL_RootRelative(t *testing.T) {
	got := resolveURL("https://cdn.example.com/hls/stream/", "/absolute/segment.ts")
	if got != "https://cdn.example.com/absolute/segment.ts" {
		t.Errorf("root-relative URL should be resolved: %s", got)
	}
}

func TestResolveURL_HTTPPrefix(t *testing.T) {
	got := resolveURL("https://cdn.example.com/", "http://insecure.com/seg.ts")
	if got != "http://insecure.com/seg.ts" {
		t.Errorf("http:// URL should be returned as-is: %s", got)
	}
}

// ---------------------------------------------------------------------------
// extractSegmentFilename
// ---------------------------------------------------------------------------

func TestExtractSegmentFilename_Simple(t *testing.T) {
	got := extractSegmentFilename("segment001.ts")
	if got != "segment001.ts" {
		t.Errorf("extractSegmentFilename = %q, want %q", got, "segment001.ts")
	}
}

func TestExtractSegmentFilename_WithPath(t *testing.T) {
	got := extractSegmentFilename("https://cdn.example.com/hls/segment001.ts")
	if got != "segment001.ts" {
		t.Errorf("extractSegmentFilename = %q, want %q", got, "segment001.ts")
	}
}

func TestExtractSegmentFilename_WithQuery(t *testing.T) {
	got := extractSegmentFilename("https://cdn.example.com/hls/seg.ts?token=abc")
	if got != "seg.ts" {
		t.Errorf("extractSegmentFilename = %q, want %q", got, "seg.ts")
	}
}

func TestExtractSegmentFilename_EmptyPath(t *testing.T) {
	got := extractSegmentFilename("https://cdn.example.com/")
	// Should fallback to hash-based name
	if !strings.HasPrefix(got, "seg_") {
		t.Errorf("empty path should produce hash-based name: %s", got)
	}
}

// ---------------------------------------------------------------------------
// recordToItem / itemToRecord
// ---------------------------------------------------------------------------

func TestRecordToItem(t *testing.T) {
	now := time.Now()
	rec := &repositories.ExtractorItemRecord{
		ID:        "ext_abc123",
		Title:     "Test Stream",
		StreamURL: "https://example.com/stream.m3u8",
		Status:    "active",
		AddedBy:   "admin",
		CreatedAt: now,
	}
	item := recordToItem(rec)
	if item.ID != "ext_abc123" {
		t.Errorf("ID = %q", item.ID)
	}
	if item.Title != "Test Stream" {
		t.Errorf("Title = %q", item.Title)
	}
	if item.Status != "active" {
		t.Errorf("Status = %q", item.Status)
	}
}

func TestItemToRecord(t *testing.T) {
	now := time.Now()
	item := &ExtractedItem{
		ID:        "ext_abc123",
		Title:     "Test Stream",
		StreamURL: "https://example.com/stream.m3u8",
		Status:    "active",
		AddedBy:   "admin",
		CreatedAt: now,
	}
	rec := itemToRecord(item)
	if rec.ID != "ext_abc123" {
		t.Errorf("ID = %q", rec.ID)
	}
	if rec.StreamType != "hls" {
		t.Errorf("StreamType = %q, want hls", rec.StreamType)
	}
	if rec.SourceURL != item.StreamURL {
		t.Errorf("SourceURL = %q, want %q", rec.SourceURL, item.StreamURL)
	}
}

// ---------------------------------------------------------------------------
// Module basics
// ---------------------------------------------------------------------------

func TestModuleName(t *testing.T) {
	m := &Module{}
	if m.Name() != "extractor" {
		t.Errorf("Name() = %q, want %q", m.Name(), "extractor")
	}
}

func TestModuleHealth_Default(t *testing.T) {
	m := &Module{}
	h := m.Health()
	if h.Name != "extractor" {
		t.Errorf("Health().Name = %q", h.Name)
	}
}

func TestSetHealth(t *testing.T) {
	m := &Module{}
	m.setHealth(true, "Running")
	h := m.Health()
	if h.Status != "healthy" {
		t.Errorf("status = %q, want healthy", h.Status)
	}
	if h.Message != "Running" {
		t.Errorf("message = %q, want Running", h.Message)
	}
}

// ---------------------------------------------------------------------------
// GetItem / GetAllItems / GetStats (in-memory operations)
// ---------------------------------------------------------------------------

func TestGetItem_Found(t *testing.T) {
	m := &Module{items: map[string]*ExtractedItem{
		"id1": {ID: "id1", Title: "Stream 1", Status: "active"},
	}}
	item := m.GetItem("id1")
	if item == nil {
		t.Fatal("expected item to be found")
	}
	if item.Title != "Stream 1" {
		t.Errorf("Title = %q", item.Title)
	}
}

func TestGetItem_NotFound(t *testing.T) {
	m := &Module{items: make(map[string]*ExtractedItem)}
	item := m.GetItem("nonexistent")
	if item != nil {
		t.Error("expected nil for nonexistent item")
	}
}

func TestGetAllItems(t *testing.T) {
	m := &Module{items: map[string]*ExtractedItem{
		"id1": {ID: "id1", Status: "active"},
		"id2": {ID: "id2", Status: "error"},
	}}
	items := m.GetAllItems()
	if len(items) != 2 {
		t.Errorf("expected 2 items, got %d", len(items))
	}
}

func TestGetAllItems_Empty(t *testing.T) {
	m := &Module{items: make(map[string]*ExtractedItem)}
	items := m.GetAllItems()
	if len(items) != 0 {
		t.Errorf("expected 0 items, got %d", len(items))
	}
}

func TestGetStats(t *testing.T) {
	m := &Module{items: map[string]*ExtractedItem{
		"id1": {ID: "id1", Status: "active"},
		"id2": {ID: "id2", Status: "active"},
		"id3": {ID: "id3", Status: "error"},
	}}
	stats := m.GetStats()
	if stats.TotalItems != 3 {
		t.Errorf("TotalItems = %d, want 3", stats.TotalItems)
	}
	if stats.ActiveItems != 2 {
		t.Errorf("ActiveItems = %d, want 2", stats.ActiveItems)
	}
	if stats.ErrorItems != 1 {
		t.Errorf("ErrorItems = %d, want 1", stats.ErrorItems)
	}
}

// ---------------------------------------------------------------------------
// RemoveItem (in-memory only, no repo)
// ---------------------------------------------------------------------------

func TestRemoveItem_Exists(t *testing.T) {
	m := &Module{items: map[string]*ExtractedItem{
		"id1": {ID: "id1"},
	}}
	err := m.RemoveItem("id1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m.GetItem("id1") != nil {
		t.Error("item should be removed")
	}
}

func TestRemoveItem_NotFound(t *testing.T) {
	m := &Module{items: make(map[string]*ExtractedItem)}
	err := m.RemoveItem("nonexistent")
	if err != ErrNotFound {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// ErrNotFound
// ---------------------------------------------------------------------------

func TestErrNotFound(t *testing.T) {
	if ErrNotFound == nil {
		t.Error("ErrNotFound should not be nil")
	}
	if ErrNotFound.Error() != "extractor item not found" {
		t.Errorf("ErrNotFound.Error() = %q", ErrNotFound.Error())
	}
}

// ---------------------------------------------------------------------------
// playlistCacheTTL
// ---------------------------------------------------------------------------

func TestPlaylistCacheTTL(t *testing.T) {
	if playlistCacheTTL != 5*time.Minute {
		t.Errorf("playlistCacheTTL = %v, want 5m", playlistCacheTTL)
	}
}
