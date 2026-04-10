package playlist

import (
	"errors"
	"testing"

	"media-server-pro/pkg/models"
)

// ---------------------------------------------------------------------------
// Error sentinels
// ---------------------------------------------------------------------------

func TestErrorSentinels(t *testing.T) {
	if ErrPlaylistNotFound == nil {
		t.Error("ErrPlaylistNotFound should not be nil")
	}
	if ErrItemNotFound == nil {
		t.Error("ErrItemNotFound should not be nil")
	}
	if ErrAccessDenied == nil {
		t.Error("ErrAccessDenied should not be nil")
	}
	// Ensure distinct
	if errors.Is(ErrPlaylistNotFound, ErrItemNotFound) {
		t.Error("error sentinels should be distinct")
	}
	if errors.Is(ErrPlaylistNotFound, ErrAccessDenied) {
		t.Error("error sentinels should be distinct")
	}
}

// ---------------------------------------------------------------------------
// PlaylistID type
// ---------------------------------------------------------------------------

func TestPlaylistID(t *testing.T) {
	id := PlaylistID("pl-123")
	if string(id) != "pl-123" {
		t.Errorf("PlaylistID conversion failed: %s", id)
	}
}

// ---------------------------------------------------------------------------
// copyPlaylist
// ---------------------------------------------------------------------------

func TestCopyPlaylist_DeepCopy(t *testing.T) {
	original := &models.Playlist{
		ID:   "pl-1",
		Name: "My Playlist",
		Items: []models.PlaylistItem{
			{MediaPath: "/videos/a.mp4", Position: 1},
			{MediaPath: "/videos/b.mp4", Position: 2},
		},
	}
	copied := copyPlaylist(original)
	if copied.ID != original.ID {
		t.Errorf("ID = %q, want %q", copied.ID, original.ID)
	}
	if copied.Name != original.Name {
		t.Errorf("Name = %q, want %q", copied.Name, original.Name)
	}
	if len(copied.Items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(copied.Items))
	}

	// Mutating the copy should not affect the original
	copied.Items[0].MediaPath = "/changed"
	if original.Items[0].MediaPath == "/changed" {
		t.Error("mutation of copy should not affect original")
	}
}

func TestCopyPlaylist_EmptyItems(t *testing.T) {
	original := &models.Playlist{
		ID:    "pl-2",
		Name:  "Empty",
		Items: []models.PlaylistItem{},
	}
	copied := copyPlaylist(original)
	if len(copied.Items) != 0 {
		t.Errorf("expected 0 items, got %d", len(copied.Items))
	}
}

// ---------------------------------------------------------------------------
// Module basics
// ---------------------------------------------------------------------------

func TestModuleName(t *testing.T) {
	m := &Module{}
	if m.Name() != "playlist" {
		t.Errorf("Name() = %q, want %q", m.Name(), "playlist")
	}
}
