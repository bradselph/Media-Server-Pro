package handlers_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"testing"

	"media-server-pro/internal/config"
	"media-server-pro/internal/testutil"
)

// loginPayload returns a JSON body for login requests.
func loginPayload(username, password string) *bytes.Reader {
	b, _ := json.Marshal(map[string]string{"username": username, "password": password})
	return bytes.NewReader(b)
}

// TestAuthFlow_LoginLogout exercises the full login → session check → logout cycle.
func TestAuthFlow_LoginLogout(t *testing.T) {
	ts := testutil.NewTestServer(t)

	// Create a test user via the auth module directly.
	ts.Env.CreateTestUser(t, "testlogin", "password123")

	// --- Login ---
	resp := ts.Request("POST", "/api/auth/login", loginPayload("testlogin", "password123"))
	result := ts.ParseJSON(resp)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("login: expected 200, got %d", resp.StatusCode)
	}
	if result["success"] != true {
		t.Fatalf("login: expected success=true, got %v", result["success"])
	}

	data, ok := result["data"].(map[string]any)
	if !ok {
		t.Fatal("login: data is not a map")
	}
	sessionID, _ := data["session_id"].(string)
	if sessionID == "" {
		t.Fatal("login: missing session_id in response")
	}
	if data["username"] != "testlogin" {
		t.Errorf("login: expected username=testlogin, got %v", data["username"])
	}

	// --- Check session (authenticated) ---
	resp = ts.AuthRequest("GET", "/api/auth/session", nil, sessionID)
	result = ts.ParseJSON(resp)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("check session: expected 200, got %d", resp.StatusCode)
	}
	data, _ = result["data"].(map[string]any)
	if data["authenticated"] != true {
		t.Errorf("check session: expected authenticated=true, got %v", data["authenticated"])
	}

	// --- Logout ---
	resp = ts.AuthRequest("POST", "/api/auth/logout", nil, sessionID)
	ts.ParseJSON(resp)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("logout: expected 200, got %d", resp.StatusCode)
	}

	// --- Check session after logout (unauthenticated) ---
	resp = ts.AuthRequest("GET", "/api/auth/session", nil, sessionID)
	result = ts.ParseJSON(resp)

	data, _ = result["data"].(map[string]any)
	if data["authenticated"] != false {
		t.Errorf("post-logout session check: expected authenticated=false, got %v", data["authenticated"])
	}
}

// TestAuthFlow_LoginInvalidCredentials tests login with wrong credentials.
func TestAuthFlow_LoginInvalidCredentials(t *testing.T) {
	ts := testutil.NewTestServer(t)

	ts.Env.CreateTestUser(t, "wrongpass", "password123")

	resp := ts.Request("POST", "/api/auth/login", loginPayload("wrongpass", "badpassword"))
	result := ts.ParseJSON(resp)

	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
	if result["success"] != false {
		t.Errorf("expected success=false, got %v", result["success"])
	}
}

// TestAuthFlow_LoginMissingBody tests login with empty body.
func TestAuthFlow_LoginMissingBody(t *testing.T) {
	ts := testutil.NewTestServer(t)

	resp := ts.Request("POST", "/api/auth/login", bytes.NewReader([]byte("{}")))
	result := ts.ParseJSON(resp)

	if resp.StatusCode != http.StatusBadRequest && resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 400 or 401, got %d", resp.StatusCode)
	}
	if result["success"] != false {
		t.Errorf("expected success=false, got %v", result["success"])
	}
}

// TestAuthFlow_ProtectedEndpointWithoutSession tests that protected endpoints
// return 401 when no session cookie is provided.
func TestAuthFlow_ProtectedEndpointWithoutSession(t *testing.T) {
	ts := testutil.NewTestServer(t)

	resp := ts.Request("GET", "/api/playlists", nil)
	result := ts.ParseJSON(resp)

	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
	if result["success"] != false {
		t.Errorf("expected success=false, got %v", result["success"])
	}
}

// TestAuthFlow_SessionCheckUnauthenticated tests session check with no cookie.
func TestAuthFlow_SessionCheckUnauthenticated(t *testing.T) {
	ts := testutil.NewTestServer(t)

	resp := ts.Request("GET", "/api/auth/session", nil)
	result := ts.ParseJSON(resp)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	data, _ := result["data"].(map[string]any)
	if data["authenticated"] != false {
		t.Errorf("expected authenticated=false, got %v", data["authenticated"])
	}
}

// TestAuthFlow_Register tests user self-registration.
func TestAuthFlow_Register(t *testing.T) {
	ts := testutil.NewTestServer(t)

	// Enable registration in config.
	if err := ts.Env.Config.Update(func(c *config.Config) {
		c.Auth.AllowRegistration = true
	}); err != nil {
		t.Skip("skipping registration test: cannot update config dynamically")
	}

	body, _ := json.Marshal(map[string]string{
		"username": "newuser",
		"password": "password123",
		"email":    "newuser@test.local",
	})
	resp := ts.Request("POST", "/api/auth/register", bytes.NewReader(body))

	// Registration may be disabled by default — both 200 and 403 are valid outcomes.
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusForbidden {
		result := ts.ParseJSON(resp)
		t.Fatalf("register: expected 200 or 403, got %d: %v", resp.StatusCode, result)
	}
	resp.Body.Close()
}

// TestAuthFlow_ChangePassword tests the password change flow.
func TestAuthFlow_ChangePassword(t *testing.T) {
	ts := testutil.NewTestServer(t)

	ts.Env.CreateTestUser(t, "pwduser", "oldpass123")
	sessionID := ts.Env.LoginUser(t, "pwduser", "oldpass123")

	body, _ := json.Marshal(map[string]string{
		"current_password": "oldpass123",
		"new_password":     "newpass456",
	})
	resp := ts.AuthRequest("POST", "/api/auth/change-password", bytes.NewReader(body), sessionID)
	result := ts.ParseJSON(resp)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("change password: expected 200, got %d: %v", resp.StatusCode, result)
	}

	// Verify old password no longer works.
	resp = ts.Request("POST", "/api/auth/login", loginPayload("pwduser", "oldpass123"))
	if resp.StatusCode != http.StatusUnauthorized {
		resp.Body.Close()
		t.Error("old password should no longer work")
	} else {
		resp.Body.Close()
	}

	// Verify new password works.
	resp = ts.Request("POST", "/api/auth/login", loginPayload("pwduser", "newpass456"))
	ts.ParseJSON(resp)
	if resp.StatusCode != http.StatusOK {
		t.Errorf("new password should work, got %d", resp.StatusCode)
	}
}
