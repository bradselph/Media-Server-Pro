package playlist

import (
	"testing"

	"media-server-pro/pkg/models"
)

// ---------------------------------------------------------------------------
// itemMatchesKey
// ---------------------------------------------------------------------------

func TestItemMatchesKey_ByMediaPath(t *testing.T) {
	item := &models.PlaylistItem{MediaPath: "/videos/movie.mp4", MediaID: "id-1", ID: "item-1"}
	if !itemMatchesKey(item, "/videos/movie.mp4") {
		t.Error("should match by MediaPath")
	}
}

func TestItemMatchesKey_ByMediaID(t *testing.T) {
	item := &models.PlaylistItem{MediaPath: "/videos/movie.mp4", MediaID: "id-1", ID: "item-1"}
	if !itemMatchesKey(item, "id-1") {
		t.Error("should match by MediaID")
	}
}

func TestItemMatchesKey_ByItemID(t *testing.T) {
	item := &models.PlaylistItem{MediaPath: "/videos/movie.mp4", MediaID: "id-1", ID: "item-1"}
	if !itemMatchesKey(item, "item-1") {
		t.Error("should match by item ID")
	}
}

func TestItemMatchesKey_NoMatch(t *testing.T) {
	item := &models.PlaylistItem{MediaPath: "/videos/movie.mp4", MediaID: "id-1", ID: "item-1"}
	if itemMatchesKey(item, "nonexistent") {
		t.Error("should not match nonexistent key")
	}
}

func TestItemMatchesKey_EmptyKey(t *testing.T) {
	item := &models.PlaylistItem{MediaPath: "/videos/movie.mp4", MediaID: "id-1", ID: "item-1"}
	if itemMatchesKey(item, "") {
		t.Error("empty key should not match non-empty fields")
	}
}
