package handlers

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/gin-gonic/gin"

	"media-server-pro/internal/config"
	"media-server-pro/internal/logger"
	"media-server-pro/internal/playlist"
	"media-server-pro/pkg/models"
)

// TestFilterMatureMediaItems locks in the fix for R06: PreviewSmartPlaylist (and
// any other smart-playlist surface) must drop mature-flagged items for viewers
// without mature-content permission, the same way ListPublicPlaylists does for
// regular playlists.
func TestFilterMatureMediaItems(t *testing.T) {
	items := []*models.MediaItem{
		{ID: "safe1", IsMature: false},
		{ID: "mature1", IsMature: true},
		{ID: "safe2", IsMature: false},
		{ID: "mature2", IsMature: true},
	}

	t.Run("viewer without permission loses mature items", func(t *testing.T) {
		got := filterMatureMediaItems(items, false)
		if len(got) != 2 {
			t.Fatalf("len = %d, want 2 (mature items must be dropped): %+v", len(got), got)
		}
		for _, it := range got {
			if it.IsMature {
				t.Errorf("mature item %q leaked through filter", it.ID)
			}
		}
	})

	t.Run("viewer with permission keeps everything", func(t *testing.T) {
		got := filterMatureMediaItems(items, true)
		if len(got) != len(items) {
			t.Fatalf("len = %d, want %d (nothing should be dropped)", len(got), len(items))
		}
	})

	t.Run("does not mutate the input slice's backing array", func(t *testing.T) {
		original := make([]*models.MediaItem, len(items))
		copy(original, items)
		_ = filterMatureMediaItems(items, false)
		for i := range items {
			if items[i] != original[i] {
				t.Fatalf("input slice element %d was mutated", i)
			}
		}
	})
}

// smartPlaylistGateTestHandler builds a Handler with the Playlist module present
// (so requirePlaylist's module-presence check always passes) and
// Features.EnablePlaylists set as requested. The database is deliberately left
// nil: if a smart-playlist handler forgot to call requirePlaylist before touching
// h.database, this would surface as a 503 (or panic) instead of the expected 404,
// making a missing gate obvious.
func smartPlaylistGateTestHandler(t *testing.T, enabled bool) *Handler {
	t.Helper()
	m := config.NewManager(filepath.Join(t.TempDir(), "config.json"))
	if err := m.Load(); err != nil {
		t.Fatalf("load config: %v", err)
	}
	if err := m.SetValuesBatch(map[string]any{
		"features": map[string]any{"enable_playlists": enabled},
	}); err != nil {
		t.Fatalf("set config: %v", err)
	}
	if got := m.Get().Features.EnablePlaylists; got != enabled {
		t.Fatalf("precondition: Features.EnablePlaylists = %v, want %v", got, enabled)
	}
	return &Handler{
		config:   m,
		playlist: &playlist.Module{}, // non-nil: the gate must fail on the flag, not module presence
		log:      logger.New("smart-playlist-gate-test"),
	}
}

// TestSmartPlaylistHandlers_HonorEnablePlaylistsGate is the regression for R15:
// every smart-playlist CRUD/preview handler must honor Features.EnablePlaylists
// via the same h.requirePlaylist helper the regular playlist handlers use.
func TestSmartPlaylistHandlers_HonorEnablePlaylistsGate(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cases := []struct {
		name   string
		method string
		path   string
		call   func(h *Handler, c *gin.Context)
	}{
		{"ListSmartPlaylists", http.MethodGet, "/api/smart-playlists", (*Handler).ListSmartPlaylists},
		{"CreateSmartPlaylist", http.MethodPost, "/api/smart-playlists", (*Handler).CreateSmartPlaylist},
		{"GetSmartPlaylist", http.MethodGet, "/api/smart-playlists/x", (*Handler).GetSmartPlaylist},
		{"UpdateSmartPlaylist", http.MethodPut, "/api/smart-playlists/x", (*Handler).UpdateSmartPlaylist},
		{"DeleteSmartPlaylist", http.MethodDelete, "/api/smart-playlists/x", (*Handler).DeleteSmartPlaylist},
		{"PreviewSmartPlaylist", http.MethodGet, "/api/smart-playlists/x/preview", (*Handler).PreviewSmartPlaylist},
	}

	for _, tc := range cases {
		t.Run(tc.name+"/disabled", func(t *testing.T) {
			h := smartPlaylistGateTestHandler(t, false)
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest(tc.method, tc.path, http.NoBody)
			c.Params = gin.Params{{Key: "id", Value: "x"}}

			// No session is set up at all — if the gate is missing or wired after
			// the session check, this would 401 instead of 404, still proving a bug.
			tc.call(h, c)

			if w.Code != http.StatusNotFound {
				t.Errorf("%s with playlists disabled: status = %d, want 404 (feature disabled)", tc.name, w.Code)
			}
		})

		t.Run(tc.name+"/enabled falls through to auth", func(t *testing.T) {
			h := smartPlaylistGateTestHandler(t, true)
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest(tc.method, tc.path, http.NoBody)
			c.Params = gin.Params{{Key: "id", Value: "x"}}

			tc.call(h, c)

			// With no session set, every handler's next check (RequireSession) must
			// reject with 401 — proving the gate passed through instead of
			// (incorrectly) blocking every request outright.
			if w.Code != http.StatusUnauthorized {
				t.Errorf("%s with playlists enabled and no session: status = %d, want 401 (gate should pass, session check should fail)", tc.name, w.Code)
			}
		})
	}
}
