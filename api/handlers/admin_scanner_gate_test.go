package handlers

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/gin-gonic/gin"

	"media-server-pro/internal/config"
	"media-server-pro/internal/logger"
	"media-server-pro/internal/scanner"
)

// TestRequireMatureScanner_HonorsEnabledToggle is a regression for the FE-1
// wiring fix: the mature-content scanner's Enabled toggle must actually gate the
// active scan path. Before the fix the flag was read only for validation/display,
// so disabling it in the admin panel had no effect on the background scan or the
// on-demand ScanContent endpoint.
func TestRequireMatureScanner_HonorsEnabledToggle(t *testing.T) {
	gin.SetMode(gin.TestMode)

	newHandler := func(enabled bool) *Handler {
		m := config.NewManager(filepath.Join(t.TempDir(), "config.json"))
		if err := m.Load(); err != nil {
			t.Fatalf("load config: %v", err)
		}
		if err := m.SetValuesBatch(map[string]any{
			"features": map[string]any{"enable_mature_scanner": enabled},
		}); err != nil {
			t.Fatalf("set config: %v", err)
		}
		if got := m.Get().MatureScanner.Enabled; got != enabled {
			t.Fatalf("precondition: MatureScanner.Enabled = %v, want %v", got, enabled)
		}
		return &Handler{
			config:  m,
			scanner: &scanner.Module{}, // non-nil: the gate must fail on the flag, not module presence
			log:     logger.New("scanner-gate-test"),
		}
	}

	// Disabled → gate blocks with 404 (feature disabled), even though the module is present.
	hOff := newHandler(false)
	wOff := httptest.NewRecorder()
	cOff, _ := gin.CreateTestContext(wOff)
	cOff.Request = httptest.NewRequest("POST", "/api/admin/scanner/scan", nil)
	if hOff.requireMatureScanner(cOff) {
		t.Error("requireMatureScanner should return false when the scanner is disabled")
	}
	if wOff.Code != http.StatusNotFound {
		t.Errorf("disabled scanner should respond 404, got %d", wOff.Code)
	}

	// Enabled → gate passes.
	hOn := newHandler(true)
	wOn := httptest.NewRecorder()
	cOn, _ := gin.CreateTestContext(wOn)
	cOn.Request = httptest.NewRequest("POST", "/api/admin/scanner/scan", nil)
	if !hOn.requireMatureScanner(cOn) {
		t.Errorf("requireMatureScanner should return true when the scanner is enabled (status %d)", wOn.Code)
	}
}
