package media

import (
	"testing"
	"time"

	"media-server-pro/pkg/models"
)

// newPageTestModule builds a Module whose in-memory catalog holds the given
// items keyed by path, which is all ListMediaPage reads.
func newPageTestModule(items ...*models.MediaItem) *Module {
	m := &Module{media: make(map[string]*models.MediaItem, len(items))}
	for _, it := range items {
		m.media[it.Path] = it
	}
	return m
}

func pageTestItems() []*models.MediaItem {
	now := time.Now()
	return []*models.MediaItem{
		{ID: "1", Name: "Alpha", Path: "/a", Type: models.MediaTypeVideo, Views: 5, DateAdded: now},
		{ID: "2", Name: "Bravo", Path: "/b", Type: models.MediaTypeVideo, Views: 1, IsMature: true, DateAdded: now.Add(-time.Hour)},
		{ID: "3", Name: "Cello", Path: "/c", Type: models.MediaTypeAudio, Views: 9, DateAdded: now.Add(-2 * time.Hour)},
		{ID: "4", Name: "Delta", Path: "/d", Type: models.MediaTypeVideo, Views: 3, DateAdded: now.Add(-3 * time.Hour)},
		{ID: "5", Name: "Echo", Path: "/e", Type: models.MediaTypeAudio, Views: 2, IsMature: true, DateAdded: now.Add(-4 * time.Hour)},
	}
}

func ids(items []*models.MediaItem) []string {
	out := make([]string, len(items))
	for i, it := range items {
		out[i] = it.ID
	}
	return out
}

// reference computes what the handler's full path produces: ListMedia (filtered
// + sorted, every match copied) then a manual offset/limit window.
func reference(m *Module, filter Filter, limit, offset int) (page []string, total int) {
	all := m.ListMedia(filter)
	total = len(all)
	lo := min(max(offset, 0), len(all))
	hi := len(all)
	if limit > 0 && lo+limit < hi {
		hi = lo + limit
	}
	return ids(all[lo:hi]), total
}

// TestListMediaPage_MatchesListMedia is the core safety claim: for every
// filter/limit/offset combination the gated fast path must return exactly what
// the full path (ListMedia + windowing) would, including the total count.
func TestListMediaPage_MatchesListMedia(t *testing.T) {
	m := newPageTestModule(pageTestItems()...)
	mature := true

	cases := []struct {
		name   string
		filter Filter
		limit  int
		offset int
	}{
		{"all default sort, first page", Filter{}, 2, 0},
		{"all default sort, middle page", Filter{}, 2, 1},
		{"all default sort, last partial page", Filter{}, 2, 4},
		{"no limit returns everything", Filter{}, 0, 0},
		{"offset past end is empty", Filter{}, 10, 100},
		{"sort by views desc", Filter{SortBy: "views", SortDesc: true}, 3, 0},
		{"sort by date_added", Filter{SortBy: "date_added"}, 5, 0},
		{"type filter video", Filter{Type: models.MediaTypeVideo}, 10, 0},
		{"type filter audio with paging", Filter{Type: models.MediaTypeAudio}, 1, 1},
		{"is_mature true", Filter{IsMature: &mature}, 10, 0},
		{"search by name", Filter{Search: "ell"}, 10, 0}, // matches "Cello"
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			wantPage, wantTotal := reference(m, tc.filter, tc.limit, tc.offset)

			gotItems, gotTotal, _ := m.ListMediaPage(tc.filter, tc.limit, tc.offset)
			gotPage := ids(gotItems)

			if gotTotal != wantTotal {
				t.Errorf("total = %d, want %d", gotTotal, wantTotal)
			}
			if len(gotPage) != len(wantPage) {
				t.Fatalf("page = %v, want %v", gotPage, wantPage)
			}
			for i := range wantPage {
				if gotPage[i] != wantPage[i] {
					t.Errorf("page = %v, want %v", gotPage, wantPage)
					break
				}
			}
		})
	}
}

// TestListMediaPage_TypeCounts verifies counts are tallied over the FULL matched
// set, not just the returned page.
func TestListMediaPage_TypeCounts(t *testing.T) {
	m := newPageTestModule(pageTestItems()...)

	// One small page, but counts must reflect all 5 items (3 video, 2 audio).
	_, total, counts := m.ListMediaPage(Filter{}, 1, 0)
	if total != 5 {
		t.Errorf("total = %d, want 5", total)
	}
	if counts["video"] != 3 || counts["audio"] != 2 {
		t.Errorf("type counts = %v, want video:3 audio:2", counts)
	}

	// With a filter, counts cover only the filtered set.
	_, _, vcounts := m.ListMediaPage(Filter{Type: models.MediaTypeVideo}, 1, 0)
	if vcounts["video"] != 3 || vcounts["audio"] != 0 {
		t.Errorf("filtered type counts = %v, want video:3 audio:0", vcounts)
	}
}

// TestListMediaPage_ReturnsCopies guarantees the page is deep-copied so callers
// can't mutate the stored catalog (the reason ListMedia copies in the first place).
func TestListMediaPage_ReturnsCopies(t *testing.T) {
	items := pageTestItems()
	m := newPageTestModule(items...)

	page, _, _ := m.ListMediaPage(Filter{Type: models.MediaTypeVideo}, 1, 0)
	if len(page) == 0 {
		t.Fatal("expected at least one item")
	}
	page[0].Name = "MUTATED"

	for _, stored := range m.media {
		if stored.Name == "MUTATED" {
			t.Error("mutating the returned page leaked into the stored catalog")
		}
	}
}
