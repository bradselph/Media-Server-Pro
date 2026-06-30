package media

import (
	"context"
	"testing"

	"media-server-pro/pkg/models"
)

// These cover the documented fail-safe contract of the curated-category
// membership helpers without a live DB: they must never panic and must return
// the "nothing to filter by" sentinels when no DB module is wired. The DB-backed
// happy path is exercised by the handler integration tests; the in-memory
// fail-closed filtering behaviour is covered by TestFilter_Matches_ByCategory and
// TestFilter_Matches_EmptyCategorySetFailsClosed.

func TestGetCategoryMemberIDs_NoDB(t *testing.T) {
	m := &Module{} // dbModule is nil
	set, err := m.GetCategoryMemberIDs(context.Background(), "cat-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if set != nil {
		t.Errorf("expected nil set when no DB is wired, got %v", set)
	}
}

func TestGetCategoryMemberIDs_EmptyCategoryID(t *testing.T) {
	m := &Module{}
	set, err := m.GetCategoryMemberIDs(context.Background(), "")
	if err != nil || set != nil {
		t.Errorf("expected (nil, nil) for empty categoryID, got (%v, %v)", set, err)
	}
}

func TestGetCategoryIDsForItem_NoDB(t *testing.T) {
	m := &Module{}
	if got := m.GetCategoryIDsForItem(context.Background(), ""); got != nil {
		t.Errorf("expected nil for empty media id, got %v", got)
	}
	if got := m.GetCategoryIDsForItem(context.Background(), "media-1"); got != nil {
		t.Errorf("expected nil when no DB is wired, got %v", got)
	}
}

func TestGetCategoryIDsForItems_NoDB(t *testing.T) {
	m := &Module{}
	got, err := m.GetCategoryIDsForItems(context.Background(), []string{"a", "b"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected empty map when no DB is wired, got %v", got)
	}
}

// MediaIDsWithTag must key the returned set by MediaItem.ID, not by the m.media
// map key (which is a path). The set is matched against item.ID in Filter.Matches
// and used as a media_id by the category-items handler; a path key would mean
// tag-backed ("smart") categories never match any live member.
func TestMediaIDsWithTag_KeysByID(t *testing.T) {
	m := &Module{
		media: map[string]*models.MediaItem{
			"/videos/a.mp4": {ID: "id-a", Path: "/videos/a.mp4", Tags: []string{"hd", "fav"}},
			"/videos/b.mp4": {ID: "id-b", Path: "/videos/b.mp4", Tags: []string{"fav"}},
			"/videos/c.mp4": {ID: "id-c", Path: "/videos/c.mp4", Tags: []string{"sd"}},
		},
	}
	set := m.MediaIDsWithTag("fav")
	if len(set) != 2 {
		t.Fatalf("expected 2 members for tag 'fav', got %d: %v", len(set), set)
	}
	if !set["id-a"] || !set["id-b"] {
		t.Errorf("set must be keyed by media ID; got %v", set)
	}
	if set["/videos/a.mp4"] {
		t.Errorf("set must not be keyed by path; got %v", set)
	}
}

func TestMediaIDsWithTag_EmptyTag(t *testing.T) {
	m := &Module{media: map[string]*models.MediaItem{"/p": {ID: "x", Tags: []string{"t"}}}}
	if set := m.MediaIDsWithTag(""); len(set) != 0 {
		t.Errorf("empty tag must return empty set, got %v", set)
	}
}
