package handlers_test

import (
	"net/http"
	"testing"

	"media-server-pro/internal/testutil"
)

// TestListMedia_Empty tests listing media when no files exist.
func TestListMedia_Empty(t *testing.T) {
	ts := testutil.NewTestServer(t)

	resp := ts.Request("GET", "/api/media", nil)
	result := ts.ParseJSON(resp)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if result["success"] != true {
		t.Fatalf("expected success=true, got %v", result["success"])
	}

	data, ok := result["data"].(map[string]any)
	if !ok {
		t.Fatal("data is not a map")
	}

	items, ok := data["items"].([]any)
	if !ok {
		t.Fatal("items is not an array")
	}
	if len(items) != 0 {
		t.Errorf("expected 0 items, got %d", len(items))
	}

	totalItems, ok := data["total_items"].(float64)
	if !ok {
		t.Fatal("total_items missing")
	}
	if totalItems != 0 {
		t.Errorf("expected total_items=0, got %v", totalItems)
	}
}

// TestListMedia_WithPagination tests that pagination params are respected.
func TestListMedia_WithPagination(t *testing.T) {
	ts := testutil.NewTestServer(t)

	resp := ts.Request("GET", "/api/media?limit=10&offset=0", nil)
	result := ts.ParseJSON(resp)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	data, _ := result["data"].(map[string]any)
	if data["total_pages"] == nil {
		t.Error("total_pages should be present in response")
	}
}

// TestListMedia_FilterByType tests the type filter parameter.
func TestListMedia_FilterByType(t *testing.T) {
	ts := testutil.NewTestServer(t)

	resp := ts.Request("GET", "/api/media?type=video", nil)
	result := ts.ParseJSON(resp)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if result["success"] != true {
		t.Errorf("expected success=true, got %v", result["success"])
	}
}

// TestListMedia_FilterBySearch tests the search filter parameter.
func TestListMedia_FilterBySearch(t *testing.T) {
	ts := testutil.NewTestServer(t)

	resp := ts.Request("GET", "/api/media?search=nonexistent", nil)
	result := ts.ParseJSON(resp)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	data, _ := result["data"].(map[string]any)
	items, _ := data["items"].([]any)
	if len(items) != 0 {
		t.Errorf("expected 0 results for nonexistent search, got %d", len(items))
	}
}

// TestListMedia_SortParam tests that sort parameters don't cause errors.
func TestListMedia_SortParam(t *testing.T) {
	ts := testutil.NewTestServer(t)

	for _, sort := range []string{"date", "views", "date_modified"} {
		resp := ts.Request("GET", "/api/media?sort="+sort+"&sort_order=desc", nil)
		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			t.Errorf("sort=%s: expected 200, got %d", sort, resp.StatusCode)
			continue
		}
		resp.Body.Close()
	}
}

// TestListMedia_LimitClamping tests that limit is clamped to 500.
func TestListMedia_LimitClamping(t *testing.T) {
	ts := testutil.NewTestServer(t)

	resp := ts.Request("GET", "/api/media?limit=9999", nil)
	result := ts.ParseJSON(resp)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	// Even with a large limit, response should succeed (limit clamped to 500 internally).
	if result["success"] != true {
		t.Errorf("expected success=true, got %v", result["success"])
	}
}

// TestGetMedia_NotFound tests getting a non-existent media item.
func TestGetMedia_NotFound(t *testing.T) {
	ts := testutil.NewTestServer(t)

	resp := ts.Request("GET", "/api/media/nonexistent-id", nil)
	result := ts.ParseJSON(resp)

	if resp.StatusCode != http.StatusNotFound && resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("expected 404 or 503, got %d: %v", resp.StatusCode, result)
	}
}

// TestGetMediaStats tests the media stats endpoint.
func TestGetMediaStats(t *testing.T) {
	ts := testutil.NewTestServer(t)

	resp := ts.Request("GET", "/api/media/stats", nil)
	result := ts.ParseJSON(resp)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if result["success"] != true {
		t.Fatalf("expected success=true, got %v", result["success"])
	}
}

// TestStreamMedia_MissingID tests streaming without an ID.
func TestStreamMedia_MissingID(t *testing.T) {
	ts := testutil.NewTestServer(t)

	resp := ts.Request("GET", "/media", nil)
	result := ts.ParseJSON(resp)

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %v", resp.StatusCode, result)
	}
}

// TestGetCategories tests the categories endpoint.
func TestGetCategories(t *testing.T) {
	ts := testutil.NewTestServer(t)

	resp := ts.Request("GET", "/api/media/categories", nil)
	result := ts.ParseJSON(resp)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if result["success"] != true {
		t.Errorf("expected success=true, got %v", result["success"])
	}
}
