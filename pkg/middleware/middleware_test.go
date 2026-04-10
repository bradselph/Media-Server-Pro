package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
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
	r.Use(GinSecurityHeaders("default-src 'self'", 0))
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
	r.Use(GinSecurityHeaders("", 0))
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
	r.Use(GinSecurityHeaders("", 31536000))
	r.GET("/test", func(c *gin.Context) { c.String(200, "ok") })

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/test", nil)
	r.ServeHTTP(w, req)

	if got := w.Header().Get("Strict-Transport-Security"); got != "" {
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
		[]string{"Content-Type"},
	))
	r.GET("/test", func(c *gin.Context) { c.String(200, "ok") })

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Origin", "https://example.com")
	r.ServeHTTP(w, req)

	got := w.Header().Get("Access-Control-Allow-Origin")
	// When allowAll is true, we return literal "*" to prevent credential leakage
	if got != "*" {
		t.Errorf("ACAO = %q, want *", got)
	}
	// Credentials should NOT be set when using wildcard "*"
	if cred := w.Header().Get("Access-Control-Allow-Credentials"); cred == "true" {
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

	got := w.Header().Get("Access-Control-Allow-Origin")
	if got != "*" {
		t.Errorf("ACAO without Origin = %q, want *", got)
	}
}

func TestGinCORS_SpecificOrigin(t *testing.T) {
	r := gin.New()
	r.Use(GinCORS(
		[]string{"https://allowed.com"},
		[]string{"GET", "POST"},
		[]string{"Content-Type"},
	))
	r.GET("/test", func(c *gin.Context) { c.String(200, "ok") })

	// Allowed origin
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Origin", "https://allowed.com")
	r.ServeHTTP(w, req)
	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "https://allowed.com" {
		t.Errorf("allowed origin: ACAO = %q", got)
	}
	if got := w.Header().Get("Access-Control-Allow-Credentials"); got != "true" {
		t.Error("credentials should be true for specific origin")
	}

	// Disallowed origin
	w = httptest.NewRecorder()
	req = httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Origin", "https://evil.com")
	r.ServeHTTP(w, req)
	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("disallowed origin: ACAO = %q, should be empty", got)
	}
}

func TestGinCORS_Preflight(t *testing.T) {
	r := gin.New()
	r.Use(GinCORS(
		[]string{"*"},
		[]string{"GET", "POST", "PUT"},
		[]string{"Content-Type", "Authorization"},
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
	cfg := parseCORSConfig([]string{"https://a.com", "https://b.com"}, []string{"GET"}, []string{"X-Custom"})
	if cfg.allowAll {
		t.Error("allowAll should be false for specific origins")
	}
	if !cfg.allowedOrigins["https://a.com"] {
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
	cfg2 := parseCORSConfig([]string{"https://a.com"}, nil, nil)
	val, ok = cfg2.allowOrigin("https://a.com")
	if !ok || val != "https://a.com" {
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
