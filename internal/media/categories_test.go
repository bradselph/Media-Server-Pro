package media

import (
	"context"
	"testing"
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
