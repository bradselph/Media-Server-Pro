package media

import (
	"context"
	"testing"
	"time"

	"media-server-pro/internal/logger"
	"media-server-pro/pkg/models"
)

const testStarWars = "Star Wars"

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
	// Category filtering is now curated-membership based: CategoryIDSet holds the
	// member media IDs, so only those items pass.
	f := Filter{CategoryIDSet: map[string]bool{"id-member": true}}
	member := &models.MediaItem{ID: "id-member"}
	nonMember := &models.MediaItem{ID: "id-other"}
	if !f.Matches(member) {
		t.Error("member item should match category filter")
	}
	if f.Matches(nonMember) {
		t.Error("non-member item should not match category filter")
	}
}

func TestFilter_Matches_EmptyCategorySetFailsClosed(t *testing.T) {
	// A non-nil but empty member set (category with no items) must match nothing,
	// never fall through to matching the whole library.
	f := Filter{CategoryIDSet: map[string]bool{}}
	if f.Matches(&models.MediaItem{ID: "anything"}) {
		t.Error("empty category set should match no items (fail closed)")
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

func TestFilter_Matches_SearchIgnoresCategory(t *testing.T) {
	// Search now matches only name and tags — the retired path-detected category
	// string is no longer part of the search surface.
	f := Filter{Search: "anime"}
	item := &models.MediaItem{Name: "Some Show", Category: "anime"}
	if f.Matches(item) {
		t.Error("search should not match against the category field")
	}
}

func TestFilter_Matches_ByMature(t *testing.T) {
	f := Filter{IsMature: new(true)}
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
	f := Filter{Type: models.MediaTypeVideo, CategoryIDSet: map[string]bool{"m1": true}, Search: "star"}
	match := &models.MediaItem{ID: "m1", Name: testStarWars, Type: models.MediaTypeVideo}
	wrongType := &models.MediaItem{ID: "m1", Name: testStarWars, Type: models.MediaTypeAudio}
	wrongCat := &models.MediaItem{ID: "other", Name: testStarWars, Type: models.MediaTypeVideo}
	if !f.Matches(match) {
		t.Error("should match all criteria")
	}
	if f.Matches(wrongType) {
		t.Error("wrong type should not match")
	}
	if f.Matches(wrongCat) {
		t.Error("non-member category should not match")
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

func TestSortItems_Empty(_ *testing.T) {
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

// ---------------------------------------------------------------------------
// RegisterUploadedFileWithSize
// ---------------------------------------------------------------------------

func TestRegisterUploadedFileWithSize_IndexesRemotePath(t *testing.T) {
	// RegisterUploadedFileWithSize must index a remote-store key (no local file).
	// It should not call os.Stat — the path does not need to exist on disk.
	m := &Module{
		media:     make(map[string]*models.MediaItem),
		mediaByID: make(map[string]*models.MediaItem),
		metadata:  make(map[string]*Metadata),
		log:       logger.New("media-test"),
	}
	path := "remote/uploads/user123/video.mp4"
	size := int64(1024 * 1024)
	modTime := time.Now()

	err := m.RegisterUploadedFileWithSize(path, size, modTime)
	if err != nil {
		t.Fatalf("RegisterUploadedFileWithSize returned error: %v", err)
	}

	m.mu.RLock()
	item, ok := m.media[path]
	m.mu.RUnlock()
	if !ok {
		t.Fatal("item should be in the in-memory index after registration")
	}
	if item.Size != size {
		t.Errorf("item.Size = %d, want %d", item.Size, size)
	}
	if item.Type != models.MediaTypeVideo {
		t.Errorf("item.Type = %s, want video (inferred from .mp4)", item.Type)
	}
}

// ---------------------------------------------------------------------------
// ReindexMovedFile (autodiscovery apply-suggestion re-key)
// ---------------------------------------------------------------------------

func TestReindexMovedFile_RekeysIndexes(t *testing.T) {
	// After autodiscovery moves a file on disk, ReindexMovedFile must re-key the
	// in-memory catalog so the item is no longer indexed under the old path.
	m := &Module{
		media:            make(map[string]*models.MediaItem),
		mediaByID:        make(map[string]*models.MediaItem),
		metadata:         make(map[string]*Metadata),
		fingerprintIndex: make(map[string]string),
		log:              logger.New("media-test"),
	}
	oldPath := "/videos/unsorted/clip.mp4"
	newPath := "/videos/sorted/Studio/clip-720p.mp4"
	item := &models.MediaItem{ID: "id1", Path: oldPath, Name: "clip.mp4"}
	m.media[oldPath] = item
	m.mediaByID[item.ID] = item
	m.metadata[oldPath] = &Metadata{ContentFingerprint: "fp-abc"}
	m.fingerprintIndex["fp-abc"] = oldPath
	beforeVersion := m.version

	m.ReindexMovedFile(oldPath, newPath)

	if _, ok := m.media[oldPath]; ok {
		t.Error("old path should be removed from the media index")
	}
	got, ok := m.media[newPath]
	if !ok {
		t.Fatal("new path should be present in the media index")
	}
	if got.Path != newPath {
		t.Errorf("item.Path = %q, want %q", got.Path, newPath)
	}
	if got.Name != "clip-720p.mp4" {
		t.Errorf("item.Name = %q, want clip-720p.mp4", got.Name)
	}
	// Same *MediaItem retained, so existing ID lookups still resolve.
	if m.mediaByID["id1"] != got {
		t.Error("mediaByID should still point at the moved item")
	}
	if _, ok := m.metadata[oldPath]; ok {
		t.Error("old metadata key should be removed")
	}
	if _, ok := m.metadata[newPath]; !ok {
		t.Error("metadata should be re-keyed to the new path")
	}
	if m.fingerprintIndex["fp-abc"] != newPath {
		t.Errorf("fingerprintIndex should map to new path, got %q", m.fingerprintIndex["fp-abc"])
	}
	if m.version == beforeVersion {
		t.Error("catalog version should bump after a move")
	}
}

func TestReindexMovedFile_NoopGuards(t *testing.T) {
	m := &Module{
		media:            make(map[string]*models.MediaItem),
		metadata:         make(map[string]*Metadata),
		fingerprintIndex: make(map[string]string),
		log:              logger.New("media-test"),
	}
	// Unknown old path: must not create a phantom entry or panic.
	m.ReindexMovedFile("/videos/missing.mp4", "/videos/new.mp4")
	if len(m.media) != 0 {
		t.Error("reindex of an unindexed path must not add entries")
	}
	// Empty / identical paths are no-ops.
	m.ReindexMovedFile("", "/x")
	m.ReindexMovedFile("/x", "/x")
}

// ---------------------------------------------------------------------------
// UpdateBlurHash (thumbnails -> in-memory catalog sync)
// ---------------------------------------------------------------------------

func TestUpdateBlurHash_SyncsInMemoryCatalog(t *testing.T) {
	// With no metadataRepo wired the DB write is skipped, but the in-memory
	// MediaItem and Metadata must still receive the hash so list/detail endpoints
	// serve the LQIP placeholder without waiting for the next scan.
	m := &Module{
		media:    map[string]*models.MediaItem{"/videos/a.mp4": {ID: "id-a", Path: "/videos/a.mp4"}},
		metadata: map[string]*Metadata{"/videos/a.mp4": {}},
		log:      logger.New("media-test"),
	}
	const hash = "LKO2?U%2Tw=w]~RBVZRi};RPxuwH"
	if err := m.UpdateBlurHash(context.Background(), "/videos/a.mp4", hash); err != nil {
		t.Fatalf("UpdateBlurHash returned error: %v", err)
	}
	if got := m.media["/videos/a.mp4"].BlurHash; got != hash {
		t.Errorf("MediaItem.BlurHash not synced: got %q, want %q", got, hash)
	}
	if got := m.metadata["/videos/a.mp4"].BlurHash; got != hash {
		t.Errorf("Metadata.BlurHash not synced: got %q, want %q", got, hash)
	}
}

func TestUpdateBlurHash_NoopOnEmptyArgs(t *testing.T) {
	m := &Module{
		media:    make(map[string]*models.MediaItem),
		metadata: make(map[string]*Metadata),
		log:      logger.New("media-test"),
	}
	if err := m.UpdateBlurHash(context.Background(), "", "hash"); err != nil {
		t.Fatalf("empty path should be a no-op, got %v", err)
	}
	if err := m.UpdateBlurHash(context.Background(), "/videos/a.mp4", ""); err != nil {
		t.Fatalf("empty hash should be a no-op, got %v", err)
	}
}
