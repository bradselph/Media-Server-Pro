package handlers_test

import (
	"net/http"
	"testing"

	"github.com/google/uuid"

	"media-server-pro/internal/testutil"
	"media-server-pro/pkg/models"
)

// TestListCategories_PreviewMediaIDs verifies that GET /api/categories returns,
// per category, up to four member media IDs ordered by position ASC — the
// preview set used to render the mosaic thumbnail. Members are inserted out of
// order to prove the ordering and the 4-item cap.
func TestListCategories_PreviewMediaIDs(t *testing.T) {
	ts := testutil.NewTestServer(t)
	db := ts.Env.DB.GORM()
	if db == nil {
		t.Skip("no GORM handle available")
	}

	catID := uuid.NewString()
	if err := db.Create(&models.MediaCategory{ID: catID, Name: "Preview " + catID[:8]}).Error; err != nil {
		t.Fatalf("create category: %v", err)
	}
	t.Cleanup(func() {
		db.Where("category_id = ?", catID).Delete(&models.MediaCategoryItem{})
		db.Delete(&models.MediaCategory{}, "id = ?", catID)
	})

	// Synthetic media_ids are safe: media_category_items has no FK on media_id
	// (media_metadata's PK is a file path, not a UUID), so the preview query
	// returns the stored IDs verbatim without joining a media table.
	//
	// Insert six members out of position order; the listing must surface the
	// first four by position (a..d), not the insertion order.
	prefix := "prev-" + catID[:8] + "-"
	members := []struct {
		id  string
		pos int
	}{
		{prefix + "e", 4},
		{prefix + "a", 0},
		{prefix + "c", 2},
		{prefix + "f", 5},
		{prefix + "b", 1},
		{prefix + "d", 3},
	}
	for _, m := range members {
		if err := db.Create(&models.MediaCategoryItem{CategoryID: catID, MediaID: m.id, Position: m.pos}).Error; err != nil {
			t.Fatalf("create item %s: %v", m.id, err)
		}
	}

	resp := ts.Request("GET", "/api/categories", nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf(fmtExpect200, resp.StatusCode)
	}
	result := ts.ParseJSON(resp)

	data, ok := result["data"].([]any)
	if !ok {
		t.Fatalf("data is not an array: %T", result["data"])
	}
	var found map[string]any
	for _, raw := range data {
		if obj, _ := raw.(map[string]any); obj != nil && obj["id"] == catID {
			found = obj
			break
		}
	}
	if found == nil {
		t.Fatalf("category %s not present in listing", catID)
	}

	previewsRaw, ok := found["preview_media_ids"].([]any)
	if !ok {
		t.Fatalf("preview_media_ids missing or not an array: %v", found["preview_media_ids"])
	}
	got := make([]string, len(previewsRaw))
	for i, p := range previewsRaw {
		got[i], _ = p.(string)
	}
	want := []string{prefix + "a", prefix + "b", prefix + "c", prefix + "d"}
	if len(got) != len(want) {
		t.Fatalf("preview_media_ids = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("preview_media_ids[%d] = %q, want %q (full: %v)", i, got[i], want[i], got)
		}
	}
}
