package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

const (
	testContentType    = "Content-Type"
	testAllowOriginHdr = "Access-Control-Allow-Origin"
	testAllowedOrigin  = "https://allowed.com"
	testOriginA        = "https://a.com"
)

func init() {
	gin.SetMode(gin.TestMode)
}

// ---------------------------------------------------------------------------
// GinRequestID
// ---------------------------------------------------------------------------

func TestGinRequestID_GeneratesID(t *testing.T) {
	r := gin.New()
	r.Use(GinRequestID())
	r.GET("/test", func(c *gin.Context) {
		id, exists := c.Get(string(RequestIDKey))
		if !exists || id == "" {
			t.Error("request ID not set in context")
		}
		c.String(200, "ok")
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/test", nil)
	r.ServeHTTP(w, req)

	if w.Header().Get("X-Request-ID") == "" {
		t.Error("X-Request-ID header not set in response")
	}
}

func TestGinRequestID_PropagsExisting(t *testing.T) {
	r := gin.New()
	r.Use(GinRequestID())
	r.GET("/test", func(c *gin.Context) {
		id := c.GetString(string(RequestIDKey))
		if id != "my-custom-id" {
			t.Errorf("request ID = %q, want my-custom-id", id)
		}
		c.String(200, "ok")
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("X-Request-ID", "my-custom-id")
	r.ServeHTTP(w, req)
}

func TestGinRequestID_UniquePerRequest(t *testing.T) {
	r := gin.New()
	r.Use(GinRequestID())

	var ids []string
	r.GET("/test", func(c *gin.Context) {
		ids = append(ids, c.GetString(string(RequestIDKey)))
		c.String(200, "ok")
	})

	for i := 0; i < 5; i++ {
		w := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/test", nil)
		r.ServeHTTP(w, req)
	}

	seen := make(map[string]bool)
	for _, id := range ids {
		if seen[id] {
			t.Errorf("duplicate request ID: %s", id)
		}
		seen[id] = true
	}
}

// ---------------------------------------------------------------------------
// GinSecurityHeaders
// ---------------------------------------------------------------------------

func TestGinSecurityHeaders_AlwaysSet(t *testing.T) {
	r := gin.New()
	r.Use(GinSecurityHeaders(func() (string, int) { return "default-src 'self'", 0 }))
	r.GET("/test", func(c *gin.Context) { c.String(200, "ok") })

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/test", nil)
	r.ServeHTTP(w, req)

	headers := map[string]string{
		"X-Content-Type-Options":  "nosniff",
		"X-Frame-Options":         "DENY",
		"X-XSS-Protection":        "1; mode=block",
		"Referrer-Policy":         "strict-origin-when-cross-origin",
		"Content-Security-Policy": "default-src 'self'",
	}
	for name, want := range headers {
		got := w.Header().Get(name)
		if got != want {
			t.Errorf("header %s = %q, want %q", name, got, want)
		}
	}
}

func TestGinSecurityHeaders_NoCSP(t *testing.T) {
	r := gin.New()
	r.Use(GinSecurityHeaders(func() (string, int) { return "", 0 }))
	r.GET("/test", func(c *gin.Context) { c.String(200, "ok") })

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/test", nil)
	r.ServeHTTP(w, req)

	if got := w.Header().Get("Content-Security-Policy"); got != "" {
		t.Errorf("CSP should be empty when not configured, got %q", got)
	}
}

func TestGinSecurityHeaders_NoHSTS_NotHTTPS(t *testing.T) {
	r := gin.New()
	r.Use(GinSecurityHeaders(func() (string, int) { return "", 31536000 }))
	r.GET("/test", func(c *gin.Context) { c.String(200, "ok") })

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/test", nil)
	r.ServeHTTP(w, req)

	if w.Header().Get("Strict-Transport-Security") != "" {
		t.Error("HSTS should not be set for non-HTTPS requests")
	}
}

// ---------------------------------------------------------------------------
// GinCORS
// ---------------------------------------------------------------------------

func TestGinCORS_AllowAll(t *testing.T) {
	r := gin.New()
	r.Use(GinCORS(
		[]string{"*"},
		[]string{"GET", "POST"},
		[]string{testContentType},
	))
	r.GET("/test", func(c *gin.Context) { c.String(200, "ok") })

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Origin", "https://example.com")
	r.ServeHTTP(w, req)

	got := w.Header().Get(testAllowOriginHdr)
	// When allowAll is true, we return literal "*" to prevent credential leakage
	if got != "*" {
		t.Errorf("ACAO = %q, want *", got)
	}
	// Credentials should NOT be set when using wildcard "*"
	if w.Header().Get("Access-Control-Allow-Credentials") == "true" {
		t.Error("Credentials should not be set with wildcard origin")
	}
}

func TestGinCORS_AllowAll_NoOrigin(t *testing.T) {
	r := gin.New()
	r.Use(GinCORS([]string{"*"}, []string{"GET"}, []string{}))
	r.GET("/test", func(c *gin.Context) { c.String(200, "ok") })

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/test", nil)
	r.ServeHTTP(w, req)

	got := w.Header().Get(testAllowOriginHdr)
	if got != "*" {
		t.Errorf("ACAO without Origin = %q, want *", got)
	}
}

func TestGinCORS_SpecificOrigin(t *testing.T) {
	r := gin.New()
	r.Use(GinCORS(
		[]string{testAllowedOrigin},
		[]string{"GET", "POST"},
		[]string{testContentType},
	))
	r.GET("/test", func(c *gin.Context) { c.String(200, "ok") })

	// Allowed origin
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Origin", testAllowedOrigin)
	r.ServeHTTP(w, req)
	if got := w.Header().Get(testAllowOriginHdr); got != testAllowedOrigin {
		t.Errorf("allowed origin: ACAO = %q", got)
	}
	if w.Header().Get("Access-Control-Allow-Credentials") != "true" {
		t.Error("credentials should be true for specific origin")
	}

	// Disallowed origin
	w = httptest.NewRecorder()
	req = httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Origin", "https://evil.com")
	r.ServeHTTP(w, req)
	if got := w.Header().Get(testAllowOriginHdr); got != "" {
		t.Errorf("disallowed origin: ACAO = %q, should be empty", got)
	}
}

func TestGinCORS_Preflight(t *testing.T) {
	r := gin.New()
	r.Use(GinCORS(
		[]string{"*"},
		[]string{"GET", "POST", "PUT"},
		[]string{testContentType, "Authorization"},
	))
	r.OPTIONS("/test", func(c *gin.Context) { c.String(200, "ok") })

	w := httptest.NewRecorder()
	req := httptest.NewRequest("OPTIONS", "/test", nil)
	req.Header.Set("Origin", "https://example.com")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Errorf("OPTIONS status = %d, want 204", w.Code)
	}
	if got := w.Header().Get("Access-Control-Max-Age"); got != "86400" {
		t.Errorf("Max-Age = %q, want 86400", got)
	}
}

// ---------------------------------------------------------------------------
// CORS config helpers
// ---------------------------------------------------------------------------

func TestParseCORSConfig(t *testing.T) {
	cfg := parseCORSConfig([]string{testOriginA, "https://b.com"}, []string{"GET"}, []string{"X-Custom"})
	if cfg.allowAll {
		t.Error("allowAll should be false for specific origins")
	}
	if !cfg.allowedOrigins[testOriginA] {
		t.Error("a.com should be allowed")
	}
	if !cfg.allowedOrigins["https://b.com"] {
		t.Error("b.com should be allowed")
	}
	if cfg.methodsStr != "GET" {
		t.Errorf("methods = %q", cfg.methodsStr)
	}
}

func TestParseCORSConfig_Wildcard(t *testing.T) {
	cfg := parseCORSConfig([]string{"*"}, nil, nil)
	if !cfg.allowAll {
		t.Error("allowAll should be true for wildcard")
	}
}

func TestAllowOrigin(t *testing.T) {
	// Wildcard — always returns literal "*" regardless of origin
	cfg := parseCORSConfig([]string{"*"}, nil, nil)
	val, ok := cfg.allowOrigin("https://any.com")
	if !ok || val != "*" {
		t.Errorf("wildcard: value=%q, ok=%v", val, ok)
	}

	// Specific
	cfg2 := parseCORSConfig([]string{testOriginA}, nil, nil)
	val, ok = cfg2.allowOrigin(testOriginA)
	if !ok || val != testOriginA {
		t.Errorf("allowed: value=%q, ok=%v", val, ok)
	}
	val, ok = cfg2.allowOrigin("https://evil.com")
	if ok {
		t.Errorf("disallowed should not be ok, got value=%q", val)
	}
}

// ---------------------------------------------------------------------------
// IsTrustedProxy
// ---------------------------------------------------------------------------

func TestIsTrustedProxy(t *testing.T) {
	tests := []struct {
		ip   string
		want bool
	}{
		{"127.0.0.1", true},
		{"10.0.0.1", true},
		{"192.168.1.1", true},
		{"172.16.0.1", true},
		{"8.8.8.8", false},
		{"203.0.113.1", false},
		{"::1", true},
		{"invalid", false},
	}
	for _, tc := range tests {
		got := IsTrustedProxy(tc.ip)
		if got != tc.want {
			t.Errorf("IsTrustedProxy(%q) = %v, want %v", tc.ip, got, tc.want)
		}
	}
}

// ---------------------------------------------------------------------------
// SetExtraTrustedProxies + IsTrustedProxy with configured CIDRs
// ---------------------------------------------------------------------------

func TestIsTrustedProxy_ExtraCIDR(t *testing.T) {
	// Save and restore the package state so this test doesn't leak into others.
	saved := extraTrustedNets.Load()
	t.Cleanup(func() {
		if saved == nil {
			extraTrustedNets.Store(nil)
		} else {
			extraTrustedNets.Store(saved)
		}
	})

	// Baseline: 203.0.113.5 is a public IP, untrusted by default.
	if IsTrustedProxy("203.0.113.5") {
		t.Fatalf("baseline: 203.0.113.5 should not be trusted before SetExtraTrustedProxies")
	}

	// Register a public CIDR as trusted (simulates a cloud load balancer).
	SetExtraTrustedProxies([]string{"203.0.113.0/24"})

	if !IsTrustedProxy("203.0.113.5") {
		t.Errorf("after SetExtraTrustedProxies: 203.0.113.5 should be trusted (203.0.113.0/24)")
	}
	if IsTrustedProxy("198.51.100.5") {
		t.Errorf("after SetExtraTrustedProxies: 198.51.100.5 must remain untrusted")
	}
	// Built-in private ranges must continue to work.
	if !IsTrustedProxy("10.0.0.1") {
		t.Errorf("built-in private range 10.0.0.1 lost trust after SetExtraTrustedProxies")
	}

	// Reverting to empty must drop the extra trust again.
	SetExtraTrustedProxies(nil)
	if IsTrustedProxy("203.0.113.5") {
		t.Errorf("after SetExtraTrustedProxies(nil): 203.0.113.5 should no longer be trusted")
	}
}

func TestSetExtraTrustedProxies_IgnoresInvalid(t *testing.T) {
	saved := extraTrustedNets.Load()
	t.Cleanup(func() { extraTrustedNets.Store(saved) })

	// Mix valid, invalid, blank, and whitespace-padded entries; only the valid
	// one should be honored. Invalid strings must be silently dropped so a typo
	// in the admin UI doesn't take down the whole list.
	SetExtraTrustedProxies([]string{"203.0.113.0/24", "not-a-cidr", "", "  192.0.2.0/24  "})

	if !IsTrustedProxy("203.0.113.1") {
		t.Errorf("203.0.113.1 should be trusted (203.0.113.0/24)")
	}
	if !IsTrustedProxy("192.0.2.1") {
		t.Errorf("192.0.2.1 should be trusted (whitespace-padded 192.0.2.0/24)")
	}
}

// ---------------------------------------------------------------------------
// GinCORSDynamic
// ---------------------------------------------------------------------------

func TestGinCORSDynamic_ReflectsConfigChange(t *testing.T) {
	// Drive a mutable origin list to simulate an admin edit between requests.
	var origins []string
	r := gin.New()
	r.Use(GinCORSDynamic(
		func() []string { return origins },
		[]string{"GET", "POST"},
		[]string{"Content-Type"},
	))
	r.GET("/x", func(c *gin.Context) { c.String(200, "ok") })

	// Initially no origins configured: no CORS headers written.
	w1 := httptest.NewRecorder()
	req1 := httptest.NewRequest("GET", "/x", nil)
	req1.Header.Set("Origin", testAllowedOrigin)
	r.ServeHTTP(w1, req1)
	if got := w1.Header().Get(testAllowOriginHdr); got != "" {
		t.Errorf("with empty origins, expected no CORS header, got %q", got)
	}

	// Admin edits cors_origins to include testAllowedOrigin.
	origins = []string{testAllowedOrigin}

	w2 := httptest.NewRecorder()
	req2 := httptest.NewRequest("GET", "/x", nil)
	req2.Header.Set("Origin", testAllowedOrigin)
	r.ServeHTTP(w2, req2)
	if got := w2.Header().Get(testAllowOriginHdr); got != testAllowedOrigin {
		t.Errorf("after config change, expected Allow-Origin %q, got %q", testAllowedOrigin, got)
	}

	// And the next admin edit removes it: subsequent request must lose the header.
	origins = nil
	w3 := httptest.NewRecorder()
	req3 := httptest.NewRequest("GET", "/x", nil)
	req3.Header.Set("Origin", testAllowedOrigin)
	r.ServeHTTP(w3, req3)
	if got := w3.Header().Get(testAllowOriginHdr); got != "" {
		t.Errorf("after origins cleared, expected no CORS header, got %q", got)
	}
}

func TestGinCORSDynamic_HandlesOptionsPreflight(t *testing.T) {
	r := gin.New()
	r.Use(GinCORSDynamic(
		func() []string { return []string{testAllowedOrigin} },
		[]string{"GET", "POST"},
		[]string{"Content-Type"},
	))
	// No route registered for /x — middleware must short-circuit OPTIONS itself.

	w := httptest.NewRecorder()
	req := httptest.NewRequest("OPTIONS", "/x", nil)
	req.Header.Set("Origin", testAllowedOrigin)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Errorf("OPTIONS preflight: expected 204, got %d", w.Code)
	}
	if got := w.Header().Get(testAllowOriginHdr); got != testAllowedOrigin {
		t.Errorf("OPTIONS preflight: expected Allow-Origin %q, got %q", testAllowedOrigin, got)
	}
}
