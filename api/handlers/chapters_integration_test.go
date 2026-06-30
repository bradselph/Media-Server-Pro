package handlers_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"testing"

	"media-server-pro/internal/testutil"
)

// chapterBody marshals a chapter request body. Using map[string]any lets each
// test omit fields (e.g. patch only start_time) to exercise the partial-update
// paths in UpdateChapter.
func chapterBody(t *testing.T, fields map[string]any) *bytes.Reader {
	t.Helper()
	b, err := json.Marshal(fields)
	if err != nil {
		t.Fatalf("marshal chapter body: %v", err)
	}
	return bytes.NewReader(b)
}

// TestChapterCreateRejectsInvertedRange verifies CreateChapter rejects a body
// whose end_time is not strictly greater than start_time.
func TestChapterCreateRejectsInvertedRange(t *testing.T) {
	ts := testutil.NewTestServer(t)
	ts.Env.CreateTestAdmin(t, "chap_admin_a", "admin_pass_123")
	sessionID := ts.Env.LoginUser(t, "chap_admin_a", "admin_pass_123")

	resp := ts.AuthRequest("POST", "/api/chapters", chapterBody(t, map[string]any{
		"media_id":   "media-1",
		"start_time": 5.0,
		"end_time":   1.0,
		"label":      "Intro",
	}), sessionID)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("create with inverted range: expected 400, got %d", resp.StatusCode)
	}
}

// TestChapterUpdateCrossFieldInversionGuard locks in the cross-field guard:
// patching only start_time past the stored end_time must be rejected (this is
// the branch that's easy to regress because it compares the request's
// start_time against the DB record's end_time), while a start_time that stays
// below the stored end_time succeeds.
func TestChapterUpdateCrossFieldInversionGuard(t *testing.T) {
	ts := testutil.NewTestServer(t)
	ts.Env.CreateTestAdmin(t, "chap_admin_b", "admin_pass_123")
	sessionID := ts.Env.LoginUser(t, "chap_admin_b", "admin_pass_123")

	// Create a chapter spanning [0, 10].
	createResp := ts.AuthRequest("POST", "/api/chapters", chapterBody(t, map[string]any{
		"media_id":   "media-2",
		"start_time": 0.0,
		"end_time":   10.0,
		"label":      "Scene",
	}), sessionID)
	createResult := ts.ParseJSON(createResp)
	createResp.Body.Close()
	if createResp.StatusCode != http.StatusOK {
		t.Fatalf("create chapter: expected 200, got %d: %v", createResp.StatusCode, createResult)
	}
	data, ok := createResult["data"].(map[string]any)
	if !ok {
		t.Fatalf("create: missing data object: %v", createResult)
	}
	chapterID, _ := data["id"].(string)
	if chapterID == "" {
		t.Fatalf("create: missing chapter id: %v", createResult)
	}

	// Patch only start_time to 15 — past the stored end_time of 10 → reject.
	badResp := ts.AuthRequest("PUT", "/api/chapters/"+chapterID, chapterBody(t, map[string]any{
		"start_time": 15.0,
	}), sessionID)
	badResp.Body.Close()
	if badResp.StatusCode != http.StatusBadRequest {
		t.Errorf("update start_time past stored end_time: expected 400, got %d", badResp.StatusCode)
	}

	// Patch start_time to 5 — still below the stored end_time of 10 → accept.
	okResp := ts.AuthRequest("PUT", "/api/chapters/"+chapterID, chapterBody(t, map[string]any{
		"start_time": 5.0,
	}), sessionID)
	okResp.Body.Close()
	if okResp.StatusCode != http.StatusOK {
		t.Errorf("update start_time below stored end_time: expected 200, got %d", okResp.StatusCode)
	}
}

// TestChapterUpdateNotFound verifies a PUT against a non-existent chapter 404s.
func TestChapterUpdateNotFound(t *testing.T) {
	ts := testutil.NewTestServer(t)
	ts.Env.CreateTestAdmin(t, "chap_admin_c", "admin_pass_123")
	sessionID := ts.Env.LoginUser(t, "chap_admin_c", "admin_pass_123")

	resp := ts.AuthRequest("PUT", "/api/chapters/00000000-0000-0000-0000-000000000000",
		chapterBody(t, map[string]any{"label": "renamed"}), sessionID)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("update non-existent chapter: expected 404, got %d", resp.StatusCode)
	}
}
