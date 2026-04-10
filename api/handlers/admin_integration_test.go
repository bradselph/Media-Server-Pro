package handlers_test

import (
	"net/http"
	"testing"

	"media-server-pro/internal/testutil"
)

// TestAdminEndpoints_RequireAuth verifies that all admin endpoints reject unauthenticated requests.
func TestAdminEndpoints_RequireAuth(t *testing.T) {
	ts := testutil.NewTestServer(t)

	endpoints := []struct {
		method string
		path   string
	}{
		{"GET", "/api/admin/stats"},
		{"GET", "/api/admin/system"},
		{"GET", "/api/admin/streams"},
		{"GET", "/api/admin/uploads/active"},
		{"POST", "/api/admin/cache/clear"},
		{"GET", "/api/admin/users"},
		{"GET", "/api/admin/media"},
		{"POST", "/api/admin/media/scan"},
		{"GET", "/api/admin/hls/stats"},
		{"GET", "/api/admin/hls/jobs"},
		{"GET", "/api/admin/thumbnails/stats"},
		{"POST", "/api/admin/thumbnails/generate"},
		{"POST", "/api/admin/thumbnails/cleanup"},
		{"GET", "/api/admin/tasks"},
		{"GET", "/api/admin/config"},
		{"GET", "/api/admin/audit-log"},
		{"GET", "/api/admin/backups/v2"},
		{"GET", "/api/admin/security/stats"},
		{"GET", "/api/admin/database/status"},
		{"GET", "/api/admin/validator/stats"},
		{"GET", "/api/admin/categorizer/stats"},
		{"GET", "/api/admin/playlists"},
		{"GET", "/api/admin/playlists/stats"},
		{"GET", "/api/admin/suggestions/stats"},
	}

	for _, ep := range endpoints {
		resp := ts.Request(ep.method, ep.path, nil)
		resp.Body.Close()

		if resp.StatusCode != http.StatusUnauthorized && resp.StatusCode != http.StatusForbidden {
			t.Errorf("%s %s: expected 401 or 403 without auth, got %d", ep.method, ep.path, resp.StatusCode)
		}
	}
}

// TestAdminStats_WithAdminAuth tests that admin stats returns 200 for admin users.
func TestAdminStats_WithAdminAuth(t *testing.T) {
	ts := testutil.NewTestServer(t)

	// Create admin user
	ts.Env.CreateTestAdmin(t, "admin_test", "admin_pass_123")
	sessionID := ts.Env.LoginUser(t, "admin_test", "admin_pass_123")

	resp := ts.AuthRequest("GET", "/api/admin/stats", nil, sessionID)
	defer resp.Body.Close()
	result := ts.ParseJSON(resp)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d: %v", resp.StatusCode, result)
	}
	if result["success"] != true {
		t.Errorf("expected success=true, got %v", result["success"])
	}

	data, ok := result["data"].(map[string]any)
	if !ok {
		t.Fatal("expected data to be a map")
	}
	// Should contain key stats
	for _, key := range []string{"videos", "audio", "users"} {
		if _, exists := data[key]; !exists {
			t.Errorf("expected %q in stats response", key)
		}
	}
}

// TestAdminSystem_WithAdminAuth tests system info endpoint returns valid data.
func TestAdminSystem_WithAdminAuth(t *testing.T) {
	ts := testutil.NewTestServer(t)

	ts.Env.CreateTestAdmin(t, "admin_sys", "admin_pass_123")
	sessionID := ts.Env.LoginUser(t, "admin_sys", "admin_pass_123")

	resp := ts.AuthRequest("GET", "/api/admin/system", nil, sessionID)
	defer resp.Body.Close()
	result := ts.ParseJSON(resp)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	data, _ := result["data"].(map[string]any)
	if data["go_version"] == nil {
		t.Error("expected go_version in system info")
	}
	if data["os"] == nil {
		t.Error("expected os in system info")
	}
}

// TestAdminTasks_WithAdminAuth tests that task list is accessible to admins.
func TestAdminTasks_WithAdminAuth(t *testing.T) {
	ts := testutil.NewTestServer(t)

	ts.Env.CreateTestAdmin(t, "admin_tasks", "admin_pass_123")
	sessionID := ts.Env.LoginUser(t, "admin_tasks", "admin_pass_123")

	resp := ts.AuthRequest("GET", "/api/admin/tasks", nil, sessionID)
	defer resp.Body.Close()
	result := ts.ParseJSON(resp)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if result["success"] != true {
		t.Errorf("expected success=true, got %v", result["success"])
	}
}

// TestAdminEndpoints_ViewerDenied tests that viewer-role users cannot access admin endpoints.
func TestAdminEndpoints_ViewerDenied(t *testing.T) {
	ts := testutil.NewTestServer(t)

	ts.Env.CreateTestUser(t, "viewer_test", "viewer_pass_123")
	sessionID := ts.Env.LoginUser(t, "viewer_test", "viewer_pass_123")

	endpoints := []string{
		"/api/admin/stats",
		"/api/admin/system",
		"/api/admin/users",
		"/api/admin/config",
	}

	for _, path := range endpoints {
		resp := ts.AuthRequest("GET", path, nil, sessionID)
		resp.Body.Close()

		if resp.StatusCode != http.StatusForbidden && resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("GET %s: viewer should get 403, got %d", path, resp.StatusCode)
		}
	}
}
