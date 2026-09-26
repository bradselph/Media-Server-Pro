package handlers

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"media-server-pro/internal/config"
	"media-server-pro/internal/logger"
	"media-server-pro/internal/suggestions"
	"media-server-pro/pkg/models"
)

// TestBuildRecentItems_FiltersMatureContent locks in the fix for RX01:
// GetRecentContent and GetNewSinceLastVisit must drop mature-flagged items for
// viewers without mature-content permission, the same way the sibling
// suggestions endpoints (GetSuggestions, GetTrendingSuggestions, ...) do via
// canViewMatureContent.
func TestBuildRecentItems_FiltersMatureContent(t *testing.T) {
	now := time.Now()
	all := []*models.MediaItem{
		{ID: "new-safe", Name: "Safe Newest", DateAdded: now, IsMature: false},
		{ID: "new-mature", Name: "Mature Newest", DateAdded: now.Add(-time.Minute), IsMature: true},
		{ID: "old-safe", Name: "Safe Older", DateAdded: now.Add(-2 * time.Minute), IsMature: false},
	}
	cutoff := now.Add(-time.Hour)

	t.Run("viewer without permission loses mature items but keeps the rest", func(t *testing.T) {
		got := buildRecentItems(all, cutoff, 20, false, nil)
		if len(got) != 2 {
			t.Fatalf("len = %d, want 2 (mature item must be dropped): %+v", len(got), got)
		}
		for _, ri := range got {
			if ri.ID == "new-mature" {
				t.Fatalf("mature item leaked through buildRecentItems: %+v", ri)
			}
		}
	})

	t.Run("viewer with permission keeps everything", func(t *testing.T) {
		got := buildRecentItems(all, cutoff, 20, true, nil)
		if len(got) != len(all) {
			t.Fatalf("len = %d, want %d (nothing should be dropped)", len(got), len(all))
		}
	})

	t.Run("limit is still honored after filtering mature items", func(t *testing.T) {
		got := buildRecentItems(all, cutoff, 1, false, nil)
		if len(got) != 1 {
			t.Fatalf("len = %d, want 1 (limit must still apply)", len(got))
		}
		if got[0].ID != "new-safe" {
			t.Errorf("got[0].ID = %q, want %q", got[0].ID, "new-safe")
		}
	})

	t.Run("cutoff still stops the scan (sorted newest-first)", func(t *testing.T) {
		got := buildRecentItems(all, now.Add(-90*time.Second), 20, true, nil)
		// old-safe (2m ago) is before the 90s cutoff and must be excluded.
		for _, ri := range got {
			if ri.ID == "old-safe" {
				t.Fatalf("item older than cutoff was not excluded: %+v", got)
			}
		}
	})

	t.Run("thumbnail resolver is used for surviving items only", func(t *testing.T) {
		calls := map[string]bool{}
		thumbURL := func(id string) string {
			calls[id] = true
			return "thumb-" + id
		}
		got := buildRecentItems(all, cutoff, 20, false, thumbURL)
		if calls["new-mature"] {
			t.Error("thumbnail resolver should not be called for a filtered-out mature item")
		}
		for _, ri := range got {
			if ri.ThumbnailURL != "thumb-"+ri.ID {
				t.Errorf("item %s: ThumbnailURL = %q, want %q", ri.ID, ri.ThumbnailURL, "thumb-"+ri.ID)
			}
		}
	})
}

// suggestionsGateTestHandler builds a Handler with the Suggestions module present
// (so requireSuggestions's module-presence check always passes) and
// Features.EnableSuggestions set as requested.
func suggestionsGateTestHandler(t *testing.T, enabled bool) *Handler {
	t.Helper()
	m := config.NewManager(filepath.Join(t.TempDir(), "config.json"))
	if err := m.Load(); err != nil {
		t.Fatalf("load config: %v", err)
	}
	if err := m.SetValuesBatch(map[string]any{
		"features": map[string]any{"enable_suggestions": enabled},
	}); err != nil {
		t.Fatalf("set config: %v", err)
	}
	if got := m.Get().Features.EnableSuggestions; got != enabled {
		t.Fatalf("precondition: Features.EnableSuggestions = %v, want %v", got, enabled)
	}
	return &Handler{
		config:      m,
		suggestions: suggestions.NewModule(nil, nil), // non-nil: the gate must fail on the flag, not module presence
		log:         logger.New("suggestions-gate-test"),
	}
}

// TestGetRecentContent_HonorsEnableSuggestionsGate is the regression for R16:
// GetRecentContent must 404 when Features.EnableSuggestions is off, matching
// every sibling suggestions endpoint (GetSuggestions, GetTrendingSuggestions, ...).
func TestGetRecentContent_HonorsEnableSuggestionsGate(t *testing.T) {
	gin.SetMode(gin.TestMode)

	h := suggestionsGateTestHandler(t, false)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/suggestions/recent", http.NoBody)

	// h.media is intentionally left nil: if the gate is missing, the handler
	// would proceed to call h.mergedMediaList and panic on a nil media module
	// instead of cleanly 404ing, making a missing gate impossible to miss.
	h.GetRecentContent(c)

	if w.Code != http.StatusNotFound {
		t.Errorf("GetRecentContent with suggestions disabled: status = %d, want 404", w.Code)
	}
}

// TestGetNewSinceLastVisit_HonorsEnableSuggestionsGate is the regression for R16
// covering GetNewSinceLastVisit's equivalent gate.
func TestGetNewSinceLastVisit_HonorsEnableSuggestionsGate(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("disabled", func(t *testing.T) {
		h := suggestionsGateTestHandler(t, false)
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodGet, "/api/suggestions/new", http.NoBody)

		// No session set up either — if the gate were missing or ordered after
		// the session check, this would 401 instead of 404, still proving a bug.
		h.GetNewSinceLastVisit(c)

		if w.Code != http.StatusNotFound {
			t.Errorf("GetNewSinceLastVisit with suggestions disabled: status = %d, want 404", w.Code)
		}
	})

	t.Run("enabled falls through to the session check", func(t *testing.T) {
		h := suggestionsGateTestHandler(t, true)
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodGet, "/api/suggestions/new", http.NoBody)

		h.GetNewSinceLastVisit(c)

		if w.Code != http.StatusUnauthorized {
			t.Errorf("GetNewSinceLastVisit with suggestions enabled and no session: status = %d, want 401", w.Code)
		}
	})
}
