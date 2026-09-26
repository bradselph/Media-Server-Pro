package handlers

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/gin-gonic/gin"

	"media-server-pro/internal/config"
	"media-server-pro/pkg/models"
)

// C16 regression: StreamMedia, DownloadMedia, and resolveHLSJobForServe gate
// auth manually (their routes carry no requireAuth() middleware — see
// hls.go's comment), so commit 97ccdf50's 503+Retry-After fix for a session
// store that could not be reached never covered them; a logged-in user hit a
// flat 401 during the exact outage the fix targeted. All three now share
// checkStreamingAuth (handler.go), which these tests exercise directly and
// through the two handlers that don't require any other module to reach it.

func newStreamAuthTestConfig(t *testing.T) *config.Manager {
	t.Helper()
	m := config.NewManager(filepath.Join(t.TempDir(), "config.json"))
	if err := m.Load(); err != nil {
		t.Fatalf("load config: %v", err)
	}
	return m
}

// ---------------------------------------------------------------------------
// checkStreamingAuth — the shared gate itself
// ---------------------------------------------------------------------------

func TestCheckStreamingAuth_SessionUnavailableIsServiceUnavailable(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/media?id=1", nil)
	c.Set(CtxSessionUnavailable, true)

	if checkStreamingAuth(c, nil, true, "nope") {
		t.Fatal("expected checkStreamingAuth to reject when session store is unavailable")
	}
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d — a 401 here reads as a logout during a session-store "+
			"outage, exactly what commit 97ccdf50 fixed for requireAuth", w.Code, http.StatusServiceUnavailable)
	}
	if got := w.Header().Get(headerRetryAfter); got == "" {
		t.Error("a 503 should carry Retry-After so clients know to retry rather than give up")
	}
}

func TestCheckStreamingAuth_NoSessionIsStillUnauthorized(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/media?id=1", nil)
	// Nothing set: the ordinary "not logged in" case must still be a 401.

	if checkStreamingAuth(c, nil, true, "nope") {
		t.Fatal("expected checkStreamingAuth to reject a genuinely absent session")
	}
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

func TestCheckStreamingAuth_RequireAuthFalseAllowsAnonymous(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/media?id=1", nil)

	if !checkStreamingAuth(c, nil, false, "nope") {
		t.Fatal("expected checkStreamingAuth to allow anonymous access when requireAuth is false")
	}
	if c.IsAborted() {
		t.Error("should not abort when requireAuth is false")
	}
}

func TestCheckStreamingAuth_ValidSessionPasses(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/media?id=1", nil)
	sess := &models.Session{ID: "s1", UserID: "u1"}

	if !checkStreamingAuth(c, sess, true, "nope") {
		t.Fatal("expected checkStreamingAuth to allow a present session")
	}
	if c.IsAborted() {
		t.Error("should not abort when a session is present")
	}
}

// ---------------------------------------------------------------------------
// StreamMedia — reachable without h.media because the auth gate returns first
// ---------------------------------------------------------------------------

func TestStreamMedia_SessionUnavailableIsServiceUnavailable(t *testing.T) {
	gin.SetMode(gin.TestMode)
	m := newStreamAuthTestConfig(t)
	if err := m.Update(func(c *config.Config) { c.Streaming.RequireAuth = true }); err != nil {
		t.Fatalf("update config: %v", err)
	}
	h := &Handler{config: m}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/media?id=abc", nil)
	c.Set(CtxSessionUnavailable, true)

	h.StreamMedia(c)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusServiceUnavailable)
	}
	if got := w.Header().Get(headerRetryAfter); got == "" {
		t.Error("expected Retry-After header on 503")
	}
}

func TestStreamMedia_NoSessionIsUnauthorizedNotUnavailable(t *testing.T) {
	gin.SetMode(gin.TestMode)
	m := newStreamAuthTestConfig(t)
	if err := m.Update(func(c *config.Config) { c.Streaming.RequireAuth = true }); err != nil {
		t.Fatalf("update config: %v", err)
	}
	h := &Handler{config: m}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/media?id=abc", nil)

	h.StreamMedia(c)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d — a genuinely absent session must still be a 401", w.Code, http.StatusUnauthorized)
	}
}

// ---------------------------------------------------------------------------
// DownloadMedia — reachable without h.media/h.auth because the auth gate
// (after the Download.Enabled check) returns first.
// ---------------------------------------------------------------------------

func TestDownloadMedia_SessionUnavailableIsServiceUnavailable(t *testing.T) {
	gin.SetMode(gin.TestMode)
	m := newStreamAuthTestConfig(t)
	if err := m.Update(func(c *config.Config) {
		c.Download.Enabled = true
		c.Download.RequireAuth = true
	}); err != nil {
		t.Fatalf("update config: %v", err)
	}
	h := &Handler{config: m}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/download?id=abc", nil)
	c.Set(CtxSessionUnavailable, true)

	h.DownloadMedia(c)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusServiceUnavailable)
	}
	if got := w.Header().Get(headerRetryAfter); got == "" {
		t.Error("expected Retry-After header on 503")
	}
}

func TestDownloadMedia_NoSessionIsUnauthorizedNotUnavailable(t *testing.T) {
	gin.SetMode(gin.TestMode)
	m := newStreamAuthTestConfig(t)
	if err := m.Update(func(c *config.Config) {
		c.Download.Enabled = true
		c.Download.RequireAuth = true
	}); err != nil {
		t.Fatalf("update config: %v", err)
	}
	h := &Handler{config: m}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/download?id=abc", nil)

	h.DownloadMedia(c)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

// Note: resolveHLSJobForServe (hls.go) shares checkStreamingAuth with the two
// handlers above, exercised directly in TestCheckStreamingAuth_* — the auth
// check runs only after h.hls.GetJobStatus resolves a real job, which needs a
// live HLS module (DB + ffmpeg) to set up and is out of scope for a handler
// unit test.
