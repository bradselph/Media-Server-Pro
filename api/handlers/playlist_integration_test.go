package handlers_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"testing"

	"media-server-pro/internal/testutil"
)

// playlistPayload returns a JSON body for playlist creation.
func playlistPayload(name, description string) *bytes.Reader {
	b, _ := json.Marshal(map[string]string{"name": name, "description": description})
	return bytes.NewReader(b)
}

// TestPlaylistCRUD exercises the full playlist lifecycle:
// create → list → get → update → delete.
func TestPlaylistCRUD(t *testing.T) {
	ts := testutil.NewTestServer(t)

	ts.Env.CreateTestUser(t, "playlistuser", "password123")
	sessionID := ts.Env.LoginUser(t, "playlistuser", "password123")

	// --- Create playlist ---
	resp := ts.AuthRequest("POST", "/api/playlists", playlistPayload("My Playlist", "A test playlist"), sessionID)
	result := ts.ParseJSON(resp)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("create: expected 200, got %d: %v", resp.StatusCode, result)
	}
	if result["success"] != true {
		t.Fatalf("create: expected success=true, got %v", result["success"])
	}

	data, ok := result["data"].(map[string]any)
	if !ok {
		t.Fatal("create: data is not a map")
	}
	playlistID, _ := data["id"].(string)
	if playlistID == "" {
		t.Fatal("create: missing playlist id")
	}
	if data["name"] != "My Playlist" {
		t.Errorf("create: expected name=My Playlist, got %v", data["name"])
	}

	resp.Body.Close()
	// --- List playlists ---
	resp = ts.AuthRequest("GET", "/api/playlists", nil, sessionID)
	result = ts.ParseJSON(resp)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list: expected 200, got %d", resp.StatusCode)
	}
	playlists, ok := result["data"].([]any)
	if !ok {
		t.Fatal("list: data is not an array")
	}
	if len(playlists) < 1 {
		t.Error("list: expected at least 1 playlist")
	}

	resp.Body.Close()
	// --- Get playlist ---
	resp = ts.AuthRequest("GET", "/api/playlists/"+playlistID, nil, sessionID)
	result = ts.ParseJSON(resp)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get: expected 200, got %d", resp.StatusCode)
	}
	data, _ = result["data"].(map[string]any)
	if data["id"] != playlistID {
		t.Errorf("get: expected id=%s, got %v", playlistID, data["id"])
	}

	resp.Body.Close()
	// --- Update playlist ---
	updateBody, _ := json.Marshal(map[string]string{"name": "Updated Playlist"})
	resp = ts.AuthRequest("PUT", "/api/playlists/"+playlistID, bytes.NewReader(updateBody), sessionID)
	result = ts.ParseJSON(resp)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("update: expected 200, got %d: %v", resp.StatusCode, result)
	}

	resp.Body.Close()
	// --- Delete playlist ---
	resp = ts.AuthRequest("DELETE", "/api/playlists/"+playlistID, nil, sessionID)
	result = ts.ParseJSON(resp)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("delete: expected 200, got %d: %v", resp.StatusCode, result)
	}

	resp.Body.Close()
	// --- Verify deletion ---
	resp = ts.AuthRequest("GET", "/api/playlists/"+playlistID, nil, sessionID)
	if resp.StatusCode != http.StatusNotFound {
		resp.Body.Close()
		t.Errorf("get after delete: expected 404, got %d", resp.StatusCode)
	} else {
		resp.Body.Close()
	}
}

// TestPlaylistCRUD_Unauthenticated tests that playlist endpoints require auth.
func TestPlaylistCRUD_Unauthenticated(t *testing.T) {
	ts := testutil.NewTestServer(t)

	tests := []struct {
		name   string
		method string
		path   string
	}{
		{"list", "GET", "/api/playlists"},
		{"create", "POST", "/api/playlists"},
		{"get", "GET", "/api/playlists/fake-id"},
		{"delete", "DELETE", "/api/playlists/fake-id"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var body *bytes.Reader
			if tc.method == "POST" {
				body = playlistPayload("Test", "")
			}
			var resp *http.Response
			if body != nil {
				resp = ts.Request(tc.method, tc.path, body)
			} else {
				resp = ts.Request(tc.method, tc.path, nil)
			}
			if resp.StatusCode != http.StatusUnauthorized {
				resp.Body.Close()
				t.Errorf("expected 401, got %d", resp.StatusCode)
			} else {
				resp.Body.Close()
			}
		})
	}
}

// TestPlaylistCreate_EmptyName tests that creating a playlist without a name fails.
func TestPlaylistCreate_EmptyName(t *testing.T) {
	ts := testutil.NewTestServer(t)

	ts.Env.CreateTestUser(t, "emptyname", "password123")
	sessionID := ts.Env.LoginUser(t, "emptyname", "password123")

	resp := ts.AuthRequest("POST", "/api/playlists", playlistPayload("", "no name"), sessionID)
	defer resp.Body.Close()
	result := ts.ParseJSON(resp)

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %v", resp.StatusCode, result)
	}
}

// TestPlaylistGet_NotFound tests getting a non-existent playlist.
func TestPlaylistGet_NotFound(t *testing.T) {
	ts := testutil.NewTestServer(t)

	ts.Env.CreateTestUser(t, "notfound", "password123")
	sessionID := ts.Env.LoginUser(t, "notfound", "password123")

	resp := ts.AuthRequest("GET", "/api/playlists/nonexistent-id", nil, sessionID)

	if resp.StatusCode != http.StatusNotFound {
		resp.Body.Close()
		t.Fatalf("expected 404, got %d", resp.StatusCode)
	}
	resp.Body.Close()
}

// TestPlaylistDelete_NotOwner tests that a user cannot delete another user's playlist.
func TestPlaylistDelete_NotOwner(t *testing.T) {
	ts := testutil.NewTestServer(t)

	// User A creates a playlist.
	ts.Env.CreateTestUser(t, "owner", "password123")
	ownerSession := ts.Env.LoginUser(t, "owner", "password123")

	resp := ts.AuthRequest("POST", "/api/playlists", playlistPayload("Owner's Playlist", ""), ownerSession)
	defer resp.Body.Close()
	result := ts.ParseJSON(resp)

	data, _ := result["data"].(map[string]any)
	playlistID, _ := data["id"].(string)

	// User B tries to delete it.
	ts.Env.CreateTestUser(t, "intruder", "password123")
	intruderSession := ts.Env.LoginUser(t, "intruder", "password123")

	resp = ts.AuthRequest("DELETE", "/api/playlists/"+playlistID, nil, intruderSession)

	if resp.StatusCode != http.StatusForbidden && resp.StatusCode != http.StatusNotFound {
		resp.Body.Close()
		t.Errorf("expected 403 or 404, got %d", resp.StatusCode)
	} else {
		resp.Body.Close()
	}
}
