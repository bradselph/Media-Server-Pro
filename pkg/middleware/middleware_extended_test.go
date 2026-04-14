package middleware

import (
	"crypto/tls"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

const testRequestID = "abc-123-def"

// ---------------------------------------------------------------------------
// sanitizeRequestID
// ---------------------------------------------------------------------------

func TestSanitizeRequestID_Normal(t *testing.T) {
	got := sanitizeRequestID(testRequestID)
	if got != testRequestID {
		t.Errorf("sanitizeRequestID(%q) = %q", testRequestID, got)
	}
}

func TestSanitizeRequestID_StripsControlChars(t *testing.T) {
	input := "req\x00id\x01with\ncontrol\rchars"
	got := sanitizeRequestID(input)
	if strings.ContainsAny(got, "\x00\x01\n\r") {
		t.Errorf("should strip control chars, got %q", got)
	}
}

func TestSanitizeRequestID_Truncates(t *testing.T) {
	long := strings.Repeat("a", 200)
	got := sanitizeRequestID(long)
	if len(got) > 64 {
		t.Errorf("should truncate to max length, got len=%d", len(got))
	}
}

func TestSanitizeRequestID_TrimsWhitespace(t *testing.T) {
	got := sanitizeRequestID("  hello  ")
	if got != "hello" {
		t.Errorf("should trim whitespace, got %q", got)
	}
}

func TestSanitizeRequestID_Empty(t *testing.T) {
	got := sanitizeRequestID("")
	if got != "" {
		t.Errorf("empty input should return empty, got %q", got)
	}
}

func TestSanitizeRequestID_Unicode(t *testing.T) {
	got := sanitizeRequestID("req-id-日本語")
	if got != "req-id-日本語" {
		t.Errorf("should preserve printable unicode, got %q", got)
	}
}

func TestSanitizeRequestID_OnlyControlChars(t *testing.T) {
	got := sanitizeRequestID("\x00\x01\x02\x03")
	if got != "" {
		t.Errorf("only control chars should produce empty string, got %q", got)
	}
}

// ---------------------------------------------------------------------------
// isHTTPS
// ---------------------------------------------------------------------------

func TestIsHTTPS_DirectTLS(t *testing.T) {
	r := gin.New()
	r.GET("/test", func(c *gin.Context) {
		c.Request.TLS = &tls.ConnectionState{}
		if !isHTTPS(c) {
			t.Error("direct TLS should be HTTPS")
		}
		c.String(200, "ok")
	})
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/test", nil)
	r.ServeHTTP(w, req)
}

func TestIsHTTPS_TrustedProxy(t *testing.T) {
	r := gin.New()
	r.GET("/test", func(c *gin.Context) {
		c.Request.RemoteAddr = "127.0.0.1:1234"
		c.Request.Header.Set("X-Forwarded-Proto", "https")
		if !isHTTPS(c) {
			t.Error("X-Forwarded-Proto https from trusted proxy should be HTTPS")
		}
		c.String(200, "ok")
	})
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/test", nil)
	r.ServeHTTP(w, req)
}

func TestIsHTTPS_UntrustedProxy(t *testing.T) {
	r := gin.New()
	r.GET("/test", func(c *gin.Context) {
		c.Request.RemoteAddr = "203.0.113.5:1234"
		c.Request.Header.Set("X-Forwarded-Proto", "https")
		if isHTTPS(c) {
			t.Error("X-Forwarded-Proto from untrusted client should NOT be HTTPS")
		}
		c.String(200, "ok")
	})
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/test", nil)
	r.ServeHTTP(w, req)
}

func TestIsHTTPS_PlainHTTP(t *testing.T) {
	r := gin.New()
	r.GET("/test", func(c *gin.Context) {
		c.Request.RemoteAddr = "127.0.0.1:1234"
		if isHTTPS(c) {
			t.Error("plain HTTP without TLS or proxy header should not be HTTPS")
		}
		c.String(200, "ok")
	})
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/test", nil)
	r.ServeHTTP(w, req)
}

// ---------------------------------------------------------------------------
// GinSecurityHeaders — HSTS with HTTPS
// ---------------------------------------------------------------------------

func TestGinSecurityHeaders_HSTS_HTTPS(t *testing.T) {
	r := gin.New()
	// Set TLS before the middleware runs by using a wrapping middleware
	r.Use(func(c *gin.Context) {
		c.Request.TLS = &tls.ConnectionState{}
		c.Next()
	})
	r.Use(GinSecurityHeaders(func() (string, int) { return "", 31536000 }))
	r.GET("/test", func(c *gin.Context) {
		c.String(200, "ok")
	})
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/test", nil)
	r.ServeHTTP(w, req)
	hsts := w.Header().Get("Strict-Transport-Security")
	if hsts == "" {
		t.Error("HSTS header should be set for HTTPS requests")
	}
}

// ---------------------------------------------------------------------------
// GinRequestID — propagates and sanitizes client header
// ---------------------------------------------------------------------------

func TestGinRequestID_PropagatesClientHeader(t *testing.T) {
	r := gin.New()
	r.Use(GinRequestID())
	var gotID string
	r.GET("/test", func(c *gin.Context) {
		id, _ := c.Get(string(RequestIDKey))
		gotID = id.(string)
		c.String(200, "ok")
	})
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("X-Request-ID", "my-custom-id")
	r.ServeHTTP(w, req)
	if gotID != "my-custom-id" {
		t.Errorf("should propagate client X-Request-ID, got %q", gotID)
	}
}

func TestGinRequestID_SanitizesClientHeader(t *testing.T) {
	r := gin.New()
	r.Use(GinRequestID())
	var gotID string
	r.GET("/test", func(c *gin.Context) {
		id, _ := c.Get(string(RequestIDKey))
		gotID = id.(string)
		c.String(200, "ok")
	})
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("X-Request-ID", "bad\x00id\nnewline")
	r.ServeHTTP(w, req)
	if strings.ContainsAny(gotID, "\x00\n") {
		t.Errorf("request ID should be sanitized, got %q", gotID)
	}
}

// ---------------------------------------------------------------------------
// IsTrustedProxy
// ---------------------------------------------------------------------------

func TestIsTrustedProxy_Loopback(t *testing.T) {
	if !IsTrustedProxy("127.0.0.1") {
		t.Error("127.0.0.1 should be a trusted proxy")
	}
}

func TestIsTrustedProxy_Private(t *testing.T) {
	if !IsTrustedProxy("10.0.0.1") {
		t.Error("10.0.0.1 should be a trusted proxy")
	}
	if !IsTrustedProxy("192.168.1.1") {
		t.Error("192.168.1.1 should be a trusted proxy")
	}
}

func TestIsTrustedProxy_Public(t *testing.T) {
	if IsTrustedProxy("203.0.113.1") {
		t.Error("203.0.113.1 should NOT be a trusted proxy")
	}
}
