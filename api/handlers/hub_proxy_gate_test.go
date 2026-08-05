package handlers

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/gin-gonic/gin"

	"media-server-pro/internal/config"
	"media-server-pro/internal/hub"
	"media-server-pro/internal/logger"
	"media-server-pro/pkg/models"
)

// Gate tests for server-side Hub playback.
//
// The proxy routes register under ordinary session auth, not admin auth, so that
// widening the feature to all users is a config change rather than a route
// change. That makes requireHubProxyAccess the only thing standing between a
// non-admin and the proxy, so it is worth testing directly.

// hubProxyTestHandler builds a Handler with the Hub feature on and the given
// proxy knobs.
func hubProxyTestHandler(t *testing.T, proxyEnabled, allUsers bool) *Handler {
	t.Helper()
	m := config.NewManager(filepath.Join(t.TempDir(), "config.json"))
	if err := m.Load(); err != nil {
		t.Fatalf("load config: %v", err)
	}
	if err := m.SetValuesBatch(map[string]any{
		"features": map[string]any{"enable_hub": true},
		"hub": map[string]any{
			"proxy_enabled":   proxyEnabled,
			"proxy_all_users": allUsers,
		},
	}); err != nil {
		t.Fatalf("set config: %v", err)
	}
	return &Handler{
		config: m,
		// Non-nil so the gate fails on a flag rather than on module absence.
		hub: &hub.Module{},
		log: logger.New("hub-proxy-gate-test"),
	}
}

// matureViewer returns a context carrying a logged-in user allowed to see mature
// content, which is the precondition for every Hub endpoint.
func matureViewer(role models.UserRole) (*httptest.ResponseRecorder, *gin.Context) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/hub/proxy/abc/master.m3u8", http.NoBody)

	user := &models.User{Username: "viewer", Role: role, Enabled: true}
	user.Permissions.CanViewMature = true
	user.Preferences.ShowMature = true

	c.Set("session", &models.Session{Username: "viewer"})
	c.Set("user", user)
	return w, c
}

// With the proxy switched off the endpoints must be invisible, even to an admin.
func TestRequireHubProxyAccess_DisabledIsNotFound(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := hubProxyTestHandler(t, false, false)

	w, c := matureViewer(models.RoleAdmin)
	if h.requireHubProxyAccess(c) {
		t.Error("gate should refuse when hub.proxy_enabled is false")
	}
	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404 (a disabled feature should not be discoverable)", w.Code)
	}
}

// The rollout phase: enabled, but restricted to admins.
func TestRequireHubProxyAccess_AdminOnlyPhase(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := hubProxyTestHandler(t, true, false)

	t.Run("admin is allowed", func(t *testing.T) {
		w, c := matureViewer(models.RoleAdmin)
		if !h.requireHubProxyAccess(c) {
			t.Fatalf("admin should pass the gate, got status %d", w.Code)
		}
	})

	t.Run("ordinary user is refused", func(t *testing.T) {
		w, c := matureViewer(models.RoleViewer)
		if h.requireHubProxyAccess(c) {
			t.Fatal("a non-admin must not reach the proxy while it is admin-only")
		}
		if w.Code != http.StatusForbidden {
			t.Errorf("status = %d, want 403", w.Code)
		}
	})
}

// Flipping one config value opens the feature to everyone — no route or code
// change. This is the test that proves the rollout switch actually works.
func TestRequireHubProxyAccess_AllUsersFlipOpensAccess(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := hubProxyTestHandler(t, true, true)

	w, c := matureViewer(models.RoleViewer)
	if !h.requireHubProxyAccess(c) {
		t.Fatalf("an ordinary user should pass once proxy_all_users is set, got status %d", w.Code)
	}
}

// The catalog is 18+, so the age gate still applies regardless of the proxy
// knobs — an anonymous caller must never reach the upstream fetch.
func TestRequireHubProxyAccess_StillRequiresMatureAccess(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := hubProxyTestHandler(t, true, true)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/hub/proxy/abc/master.m3u8", http.NoBody)

	if h.requireHubProxyAccess(c) {
		t.Fatal("an anonymous caller must not pass the mature-content gate")
	}
	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", w.Code)
	}
}

// Artwork proxying is a separate switch: it stays available to ordinary viewers
// even while video playback is admin-only, because its whole purpose is to stop
// blocked users seeing broken thumbnails.
func TestRequireHubImageAccess_IndependentOfPlaybackRollout(t *testing.T) {
	gin.SetMode(gin.TestMode)
	// Video proxying off and admin-only; images default to on.
	h := hubProxyTestHandler(t, false, false)

	w, c := matureViewer(models.RoleViewer)
	if !h.requireHubImageAccess(c) {
		t.Fatalf("an ordinary viewer should get proxied artwork, got status %d", w.Code)
	}
}

func TestRequireHubImageAccess_RespectsItsOwnSwitch(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := hubProxyTestHandler(t, true, true)
	if err := h.config.SetValuesBatch(map[string]any{
		"hub": map[string]any{"proxy_images": false},
	}); err != nil {
		t.Fatalf("set config: %v", err)
	}

	w, c := matureViewer(models.RoleAdmin)
	if h.requireHubImageAccess(c) {
		t.Error("gate should refuse when hub.proxy_images is false")
	}
	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
}
