package security

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"media-server-pro/internal/config"
)

// C17 regression: the rate-limit exemption list in GinMiddleware carried a
// dead "/stream" prefix (no registered route ever starts with it) and never
// exempted the real remote-media byte-serving endpoint, /remote/stream
// (routes.go: `r.GET("/remote/stream", requireAuth(), h.StreamRemoteMedia)`),
// so a user seeking within remote-sourced playback could be 429'd mid-stream
// on an endpoint that is functionally identical to the exempted /media.
func newRateLimitExemptTestModule(t *testing.T) *Module {
	t.Helper()
	gin.SetMode(gin.TestMode)
	cfgPath := filepath.Join(t.TempDir(), "config.json")
	m := config.NewManager(cfgPath)
	if err := m.Load(); err != nil {
		t.Fatalf("load config: %v", err)
	}
	if err := m.Update(func(c *config.Config) {
		c.Security.RateLimitEnabled = true
		// Deliberately tight so a single non-exempt request beyond the first
		// is rejected, without waiting on real time or relying on the burst
		// window's own timing.
		c.Security.RateLimitRequests = 1
		c.Security.RateLimitWindow = time.Minute
		c.Security.BurstLimit = 1000
		c.Security.BurstWindow = time.Minute
		c.Security.ViolationsForBan = 1000 // avoid an auto-ban from masking the exemption check
	}); err != nil {
		t.Fatalf("update config: %v", err)
	}
	return NewModule(m, nil)
}

func newRateLimitTestEngine(mod *Module) *gin.Engine {
	engine := gin.New()
	engine.Use(mod.GinMiddleware())
	engine.GET("/remote/stream", func(c *gin.Context) { c.Status(http.StatusOK) })
	engine.GET("/stream", func(c *gin.Context) { c.Status(http.StatusOK) })
	engine.GET("/api/media/list", func(c *gin.Context) { c.Status(http.StatusOK) })
	return engine
}

func doRateLimitedRequest(engine *gin.Engine, path, remoteAddr string) int {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.RemoteAddr = remoteAddr
	w := httptest.NewRecorder()
	engine.ServeHTTP(w, req)
	return w.Code
}

func TestGinMiddleware_RemoteStreamExemptFromRateLimit(t *testing.T) {
	engine := newRateLimitTestEngine(newRateLimitExemptTestModule(t))
	const ip = "203.0.113.9:1234"

	for i := 0; i < 5; i++ {
		if code := doRateLimitedRequest(engine, "/remote/stream", ip); code != http.StatusOK {
			t.Fatalf("request %d to /remote/stream: status = %d, want %d (should be rate-limit exempt, "+
				"same as /media)", i, code, http.StatusOK)
		}
	}
}

// TestGinMiddleware_BareStreamPrefixNoLongerExempt pins down that the dead
// "/stream" exemption is gone: no registered route is literally "/stream" (or
// under it), so a hypothetical request there is rate-limited like any other
// path, not silently exempted the way the leftover prefix used to allow.
func TestGinMiddleware_BareStreamPrefixNoLongerExempt(t *testing.T) {
	engine := newRateLimitTestEngine(newRateLimitExemptTestModule(t))
	const ip = "203.0.113.10:1234"

	if code := doRateLimitedRequest(engine, "/stream", ip); code != http.StatusOK {
		t.Fatalf("first request to /stream: status = %d, want %d", code, http.StatusOK)
	}
	if code := doRateLimitedRequest(engine, "/stream", ip); code != http.StatusTooManyRequests {
		t.Fatalf("second request to /stream: status = %d, want %d — the dead \"/stream\" "+
			"exemption prefix should no longer skip rate limiting", code, http.StatusTooManyRequests)
	}
}

func TestGinMiddleware_NonExemptEndpointStillRateLimited(t *testing.T) {
	engine := newRateLimitTestEngine(newRateLimitExemptTestModule(t))
	const ip = "203.0.113.11:1234"

	if code := doRateLimitedRequest(engine, "/api/media/list", ip); code != http.StatusOK {
		t.Fatalf("first request: status = %d, want %d", code, http.StatusOK)
	}
	if code := doRateLimitedRequest(engine, "/api/media/list", ip); code != http.StatusTooManyRequests {
		t.Fatalf("second request: status = %d, want %d — rate limiting should still apply to "+
			"ordinary API endpoints", code, http.StatusTooManyRequests)
	}
}
