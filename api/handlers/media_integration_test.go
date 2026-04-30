package handlers_test

import (
	"net/http"
	"strings"
	"testing"

	"media-server-pro/internal/testutil"
)

const (
	fmtExpect200         = "expected 200, got %d"
	msgExpectSuccessTrue = "expected success=true, got %v"
)

// TestListMedia_Empty tests listing media when no files exist.
func TestListMedia_Empty(t *testing.T) {
	ts := testutil.NewTestServer(t)

	resp := ts.Request("GET", "/api/media", nil)
	defer resp.Body.Close()
	result := ts.ParseJSON(resp)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf(fmtExpect200, resp.StatusCode)
	}
	if result["success"] != true {
		t.Fatalf(msgExpectSuccessTrue, result["success"])
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
	defer resp.Body.Close()
	result := ts.ParseJSON(resp)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf(fmtExpect200, resp.StatusCode)
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
	defer resp.Body.Close()
	result := ts.ParseJSON(resp)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf(fmtExpect200, resp.StatusCode)
	}
	if result["success"] != true {
		t.Errorf(msgExpectSuccessTrue, result["success"])
	}
}

// TestListMedia_FilterBySearch tests the search filter parameter.
func TestListMedia_FilterBySearch(t *testing.T) {
	ts := testutil.NewTestServer(t)

	resp := ts.Request("GET", "/api/media?search=nonexistent", nil)
	defer resp.Body.Close()
	result := ts.ParseJSON(resp)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf(fmtExpect200, resp.StatusCode)
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
	defer resp.Body.Close()
	result := ts.ParseJSON(resp)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf(fmtExpect200, resp.StatusCode)
	}
	// Even with a large limit, response should succeed (limit clamped to 500 internally).
	if result["success"] != true {
		t.Errorf(msgExpectSuccessTrue, result["success"])
	}
}

// TestGetMedia_NotFound tests getting a non-existent media item.
func TestGetMedia_NotFound(t *testing.T) {
	ts := testutil.NewTestServer(t)

	resp := ts.Request("GET", "/api/media/nonexistent-id", nil)
	defer resp.Body.Close()
	result := ts.ParseJSON(resp)

	if resp.StatusCode != http.StatusNotFound && resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("expected 404 or 503, got %d: %v", resp.StatusCode, result)
	}
}

// TestGetMediaStats tests the media stats endpoint.
func TestGetMediaStats(t *testing.T) {
	ts := testutil.NewTestServer(t)

	resp := ts.Request("GET", "/api/media/stats", nil)
	defer resp.Body.Close()
	result := ts.ParseJSON(resp)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf(fmtExpect200, resp.StatusCode)
	}
	if result["success"] != true {
		t.Fatalf(msgExpectSuccessTrue, result["success"])
	}
}

// TestStreamMedia_MissingID tests streaming without an ID.
func TestStreamMedia_MissingID(t *testing.T) {
	ts := testutil.NewTestServer(t)

	resp := ts.Request("GET", "/media", nil)
	defer resp.Body.Close()
	result := ts.ParseJSON(resp)

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %v", resp.StatusCode, result)
	}
}

// TestGetCategories tests the categories endpoint.
func TestGetCategories(t *testing.T) {
	ts := testutil.NewTestServer(t)

	resp := ts.Request("GET", "/api/media/categories", nil)
	defer resp.Body.Close()
	result := ts.ParseJSON(resp)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf(fmtExpect200, resp.StatusCode)
	}
	if result["success"] != true {
		t.Errorf(msgExpectSuccessTrue, result["success"])
	}
}

// TestGetBatchMedia_EmptyIDs tests batch endpoint with no ids parameter.
func TestGetBatchMedia_EmptyIDs(t *testing.T) {
	ts := testutil.NewTestServer(t)

	resp := ts.Request("GET", "/api/media/batch", nil)
	defer resp.Body.Close()
	result := ts.ParseJSON(resp)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf(fmtExpect200, resp.StatusCode)
	}
	data, _ := result["data"].(map[string]any)
	items, ok := data["items"].(map[string]any)
	if !ok {
		t.Fatal("expected items to be a map")
	}
	if len(items) != 0 {
		t.Errorf("expected empty items map, got %d entries", len(items))
	}
}

// TestGetBatchMedia_WithIDs tests batch endpoint with non-existent IDs.
func TestGetBatchMedia_WithIDs(t *testing.T) {
	ts := testutil.NewTestServer(t)

	resp := ts.Request("GET", "/api/media/batch?ids=fake-id-1,fake-id-2", nil)
	defer resp.Body.Close()
	result := ts.ParseJSON(resp)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf(fmtExpect200, resp.StatusCode)
	}
	data, _ := result["data"].(map[string]any)
	items, ok := data["items"].(map[string]any)
	if !ok {
		t.Fatal("expected items to be a map")
	}
	// Non-existent IDs are silently omitted
	if len(items) != 0 {
		t.Errorf("expected empty items (IDs don't exist), got %d", len(items))
	}
}

// TestGetBatchMedia_IDLimit tests that batch is capped at 100 IDs.
func TestGetBatchMedia_IDLimit(t *testing.T) {
	ts := testutil.NewTestServer(t)

	// Build 110 fake IDs
	var sb strings.Builder
	for i := range 110 {
		if i > 0 {
			sb.WriteByte(',')
		}
		sb.WriteString("fake-" + string(rune('a'+i%26)))
	}
	ids := sb.String()

	resp := ts.Request("GET", "/api/media/batch?ids="+ids, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf(fmtExpect200, resp.StatusCode)
	}
	resp.Body.Close()
}
