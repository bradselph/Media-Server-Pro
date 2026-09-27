package routes_test

import (
	"net/http"
	"testing"

	"media-server-pro/internal/config"
	"media-server-pro/internal/testutil"
)

// C25 regression: sessionAuth's inline stale-cookie clear (routes.go, invoked
// when a request carries a session_id cookie that ValidateSession rejects as
// genuinely invalid) duplicated setSessionCookie/clearSessionCookie's Secure
// auto-detection but never consulted auth.secure_cookies, so an operator who
// set it had a cookie that was set Secure but sometimes cleared without it —
// browsers treat those as different cookies and never clear the one they hold.
//
// Requires a live test database (TEST_DB_USER/TEST_DB_NAME); skips otherwise,
// same as the rest of this package's integration tests.
func TestSessionAuth_StaleCookieClearHonorsSecureCookiesConfig(t *testing.T) {
	ts := testutil.NewTestServer(t)

	if err := ts.Env.Config.Update(func(c *config.Config) {
		c.Auth.SecureCookies = true
	}); err != nil {
		t.Fatalf("update config: %v", err)
	}

	// A syntactically valid but unknown session_id: ValidateSession rejects it
	// as ErrSessionNotFound (IsSessionError), taking the stale-cookie-clear path.
	resp := ts.AuthRequest(http.MethodGet, "/api/auth/session", nil, "does-not-exist")
	defer resp.Body.Close()

	var cleared *http.Cookie
	for _, ck := range resp.Cookies() {
		if ck.Name == "session_id" {
			cleared = ck
		}
	}
	if cleared == nil {
		t.Fatal("expected sessionAuth to clear the stale session_id cookie")
	}
	if !cleared.Secure {
		t.Error("cleared cookie should be Secure when auth.secure_cookies is set, even over plain HTTP")
	}
}
