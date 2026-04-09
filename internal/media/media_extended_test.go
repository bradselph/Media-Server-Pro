package media

import (
	"testing"
	"time"

	"media-server-pro/pkg/models"
)

// ---------------------------------------------------------------------------
// Filter.Matches
// ---------------------------------------------------------------------------

func TestFilter_Matches_All(t *testing.T) {
	f := Filter{}
	item := &models.MediaItem{Name: "test.mp4", Type: models.MediaTypeVideo, Category: "movies"}
	if !f.Matches(item) {
		t.Error("empty filter should match everything")
	}
}

func TestFilter_Matches_ByType(t *testing.T) {
	f := Filter{Type: models.MediaTypeVideo}
	video := &models.MediaItem{Type: models.MediaTypeVideo}
	audio := &models.MediaItem{Type: models.MediaTypeAudio}
	if !f.Matches(video) {
		t.Error("video should match video filter")
	}
	if f.Matches(audio) {
		t.Error("audio should not match video filter")
	}
}

func TestFilter_Matches_ByCategory(t *testing.T) {
	f := Filter{Category: "movies"}
	movie := &models.MediaItem{Category: "movies"}
	music := &models.MediaItem{Category: "music"}
	if !f.Matches(movie) {
		t.Error("movies should match movies filter")
	}
	if f.Matches(music) {
		t.Error("music should not match movies filter")
	}
}

func TestFilter_Matches_BySearch(t *testing.T) {
	f := Filter{Search: "star wars"}
	match := &models.MediaItem{Name: "Star Wars Episode IV"}
	noMatch := &models.MediaItem{Name: "The Matrix"}
	if !f.Matches(match) {
		t.Error("Star Wars should match 'star wars' search")
	}
	if f.Matches(noMatch) {
		t.Error("The Matrix should not match 'star wars' search")
	}
}

func TestFilter_Matches_BySearchTag(t *testing.T) {
	f := Filter{Search: "action"}
	item := &models.MediaItem{Name: "Some Movie", Tags: []string{"action", "thriller"}}
	if !f.Matches(item) {
		t.Error("item with 'action' tag should match 'action' search")
	}
}

func TestFilter_Matches_BySearchCategory(t *testing.T) {
	f := Filter{Search: "anime"}
	item := &models.MediaItem{Name: "Some Show", Category: "anime"}
	if !f.Matches(item) {
		t.Error("item in 'anime' category should match 'anime' search")
	}
}

func TestFilter_Matches_ByMature(t *testing.T) {
	mature := true
	f := Filter{IsMature: &mature}
	matureItem := &models.MediaItem{IsMature: true}
	cleanItem := &models.MediaItem{IsMature: false}
	if !f.Matches(matureItem) {
		t.Error("mature item should match mature filter")
	}
	if f.Matches(cleanItem) {
		t.Error("clean item should not match mature filter")
	}
}

func TestFilter_Matches_Combined(t *testing.T) {
	f := Filter{Type: models.MediaTypeVideo, Category: "movies", Search: "star"}
	match := &models.MediaItem{Name: "Star Wars", Type: models.MediaTypeVideo, Category: "movies"}
	wrongType := &models.MediaItem{Name: "Star Wars", Type: models.MediaTypeAudio, Category: "movies"}
	wrongCat := &models.MediaItem{Name: "Star Wars", Type: models.MediaTypeVideo, Category: "music"}
	if !f.Matches(match) {
		t.Error("should match all criteria")
	}
	if f.Matches(wrongType) {
		t.Error("wrong type should not match")
	}
	if f.Matches(wrongCat) {
		t.Error("wrong category should not match")
	}
}

// ---------------------------------------------------------------------------
// Filter.matchesSearch
// ---------------------------------------------------------------------------

func TestMatchesSearch_Empty(t *testing.T) {
	f := Filter{Search: ""}
	item := &models.MediaItem{Name: "anything"}
	if !f.matchesSearch(item) {
		t.Error("empty search should match everything")
	}
}

func TestMatchesSearch_CaseInsensitive(t *testing.T) {
	f := Filter{Search: "MATRIX"}
	item := &models.MediaItem{Name: "the matrix"}
	if !f.matchesSearch(item) {
		t.Error("search should be case-insensitive")
	}
}

// ---------------------------------------------------------------------------
// Filter.SortItems
// ---------------------------------------------------------------------------

func TestSortItems_ByName(t *testing.T) {
	items := []*models.MediaItem{
		{Name: "Charlie"},
		{Name: "Alpha"},
		{Name: "Bravo"},
	}
	f := Filter{SortBy: "name"}
	f.SortItems(items)
	if items[0].Name != "Alpha" || items[1].Name != "Bravo" || items[2].Name != "Charlie" {
		t.Errorf("sort by name: %s, %s, %s", items[0].Name, items[1].Name, items[2].Name)
	}
}

func TestSortItems_ByNameDesc(t *testing.T) {
	items := []*models.MediaItem{
		{Name: "Alpha"},
		{Name: "Charlie"},
		{Name: "Bravo"},
	}
	f := Filter{SortBy: "name", SortDesc: true}
	f.SortItems(items)
	if items[0].Name != "Charlie" {
		t.Errorf("desc sort first = %q, want Charlie", items[0].Name)
	}
}

func TestSortItems_BySize(t *testing.T) {
	items := []*models.MediaItem{
		{Name: "big", Size: 1000},
		{Name: "small", Size: 100},
		{Name: "med", Size: 500},
	}
	f := Filter{SortBy: "size"}
	f.SortItems(items)
	if items[0].Size != 100 || items[2].Size != 1000 {
		t.Error("should sort by size ascending")
	}
}

func TestSortItems_ByDateAdded(t *testing.T) {
	now := time.Now()
	items := []*models.MediaItem{
		{Name: "new", DateAdded: now},
		{Name: "old", DateAdded: now.Add(-24 * time.Hour)},
	}
	f := Filter{SortBy: "date_added"}
	f.SortItems(items)
	if items[0].Name != "old" {
		t.Error("older item should come first in ascending order")
	}
}

func TestSortItems_Empty(t *testing.T) {
	var items []*models.MediaItem
	f := Filter{SortBy: "name"}
	f.SortItems(items) // should not panic
}

func TestSortItems_Default(t *testing.T) {
	items := []*models.MediaItem{
		{Name: "B"},
		{Name: "A"},
	}
	f := Filter{SortBy: "unknown_field"}
	f.SortItems(items)
	if items[0].Name != "A" {
		t.Error("default sort should be by name")
	}
}
