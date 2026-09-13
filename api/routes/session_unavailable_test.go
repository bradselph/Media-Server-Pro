package routes

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"media-server-pro/pkg/models"
)

// sessionAuth cannot tell a missing cookie from a session store it could not
// reach, but the two mean very different things to a client. A 401 says "you are
// logged out", and the web client acts on it by bouncing to /login — where a
// still-valid session sends the user straight back, producing the hard-reload
// loop that showed a blank page after login.
//
// So a transient store failure has to be reported as 503, not 401. These pin
// that down for both guards, and pin the ordinary 401 paths so the fix cannot
// swallow a genuine rejection.

func newCtx(t *testing.T) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/favorites", nil)
	return c, rec
}

func TestRequireAuth_TransientSessionStoreFailureIsServiceUnavailable(t *testing.T) {
	c, rec := newCtx(t)
	// What sessionAuth sets when ValidateSession fails for a non-session reason
	// (DB timeout, exhausted pool): no session on the context, but the cookie is
	// deliberately preserved.
	c.Set(ctxSessionUnavailable, true)

	requireAuth()(c)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d — a 401 here reads as a logout and makes the "+
			"client redirect to /login, which bounces a still-valid session straight back",
			rec.Code, http.StatusServiceUnavailable)
	}
	if !c.IsAborted() {
		t.Error("handler chain should be aborted")
	}
	if got := rec.Header().Get(headerRetryAfter); got == "" {
		t.Error("a 503 should carry Retry-After so clients know to retry rather than give up")
	}
}

func TestRequireAuth_NoSessionIsStillUnauthorized(t *testing.T) {
	c, rec := newCtx(t)
	// Nothing set at all: the ordinary logged-out case.
	requireAuth()(c)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d — a genuinely absent session must still be a 401",
			rec.Code, http.StatusUnauthorized)
	}
}

func TestRequireAuth_ValidSessionPasses(t *testing.T) {
	c, rec := newCtx(t)
	c.Set("session", &models.Session{
		ID:        "s1",
		UserID:    "u1",
		ExpiresAt: time.Now().Add(time.Hour),
	})

	requireAuth()(c)

	if c.IsAborted() {
		t.Fatalf("a valid session must pass through; got status %d", rec.Code)
	}
}

func TestRequireAuth_ExpiredSessionIsUnauthorizedNotUnavailable(t *testing.T) {
	c, rec := newCtx(t)
	c.Set("session", &models.Session{
		ID:        "s1",
		UserID:    "u1",
		ExpiresAt: time.Now().Add(-time.Hour),
	})

	requireAuth()(c)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d — an expired session is a definitive rejection",
			rec.Code, http.StatusUnauthorized)
	}
}

func TestAdminAuth_TransientSessionStoreFailureIsServiceUnavailable(t *testing.T) {
	c, rec := newCtx(t)
	c.Set(ctxSessionUnavailable, true)

	adminAuth(nil)(c)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusServiceUnavailable)
	}
}

func TestAdminAuth_NoUserIsStillUnauthorized(t *testing.T) {
	c, rec := newCtx(t)

	adminAuth(nil)(c)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}
