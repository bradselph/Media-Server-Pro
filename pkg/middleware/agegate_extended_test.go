package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"media-server-pro/internal/config"
)

const (
	testAgeVerifyURL = "http://example.com/api/age-verify"
	testExampleHost  = "example.com"
	testClientIP192  = "192.168.1.1"
)

// ---------------------------------------------------------------------------
// isSameOrigin
// ---------------------------------------------------------------------------

func TestIsSameOrigin_NoHeaders(t *testing.T) {
	ag := newTestAgeGate(true)
	req := httptest.NewRequest("POST", testAgeVerifyURL, nil)
	req.Host = testExampleHost
	if !ag.isSameOrigin(req) {
		t.Error("absent Origin/Referer should be treated as same-origin")
	}
}

func TestIsSameOrigin_MatchingOrigin(t *testing.T) {
	ag := newTestAgeGate(true)
	req := httptest.NewRequest("POST", testAgeVerifyURL, nil)
	req.Host = testExampleHost
	req.Header.Set("Origin", "http://example.com")
	if !ag.isSameOrigin(req) {
		t.Error("matching Origin header should be same-origin")
	}
}

func TestIsSameOrigin_DifferentOrigin(t *testing.T) {
	ag := newTestAgeGate(true)
	req := httptest.NewRequest("POST", testAgeVerifyURL, nil)
	req.Host = testExampleHost
	req.Header.Set("Origin", "http://evil.com")
	if ag.isSameOrigin(req) {
		t.Error("different Origin header should NOT be same-origin")
	}
}

func TestIsSameOrigin_RefererFallback(t *testing.T) {
	ag := newTestAgeGate(true)
	req := httptest.NewRequest("POST", testAgeVerifyURL, nil)
	req.Host = testExampleHost
	req.Header.Set("Referer", "http://example.com/page")
	if !ag.isSameOrigin(req) {
		t.Error("matching Referer should be same-origin")
	}
}

func TestIsSameOrigin_RefererDifferent(t *testing.T) {
	ag := newTestAgeGate(true)
	req := httptest.NewRequest("POST", testAgeVerifyURL, nil)
	req.Host = testExampleHost
	req.Header.Set("Referer", "http://evil.com/page")
	if ag.isSameOrigin(req) {
		t.Error("different Referer should NOT be same-origin")
	}
}

func TestIsSameOrigin_InvalidURL(t *testing.T) {
	ag := newTestAgeGate(true)
	req := httptest.NewRequest("POST", testAgeVerifyURL, nil)
	req.Host = testExampleHost
	req.Header.Set("Origin", "://invalid")
	if ag.isSameOrigin(req) {
		t.Error("invalid Origin URL should NOT be same-origin")
	}
}

func TestIsSameOrigin_WithPort(t *testing.T) {
	ag := newTestAgeGate(true)
	req := httptest.NewRequest("POST", "http://example.com:8080/api/age-verify", nil)
	req.Host = "example.com:8080"
	req.Header.Set("Origin", "http://example.com:8080")
	if !ag.isSameOrigin(req) {
		t.Error("matching Origin with port should be same-origin")
	}
}

// ---------------------------------------------------------------------------
// AgeGate verification with isSameOrigin CSRF check
// ---------------------------------------------------------------------------

func TestGinVerifyHandler_CrossOrigin(t *testing.T) {
	ag := NewAgeGate(config.AgeGateConfig{
		Enabled:      true,
		CookieName:   "age_verified",
		CookieMaxAge: 3600,
	})

	r := gin.New()
	r.POST("/api/age-verify", ag.GinVerifyHandler())
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/age-verify", nil)
	req.Host = testExampleHost
	req.Header.Set("Origin", "http://evil.com")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("cross-origin verify should return 403, got %d", w.Code)
	}
}

// ---------------------------------------------------------------------------
// AgeGate IsVerified — IP TTL expiry
// ---------------------------------------------------------------------------

func TestAgeGate_IPVerify_TTLExpiry(t *testing.T) {
	ag := NewAgeGate(config.AgeGateConfig{
		Enabled:     true,
		IPVerifyTTL: 50 * time.Millisecond,
		CookieName:  "age_verified",
	})

	ag.mu.Lock()
	ag.verifiedIPs["10.0.0.99"] = time.Now().Add(-100 * time.Millisecond)
	ag.mu.Unlock()

	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "10.0.0.99:1234"
	if ag.IsVerified(req) {
		t.Error("expired IP verification should not be considered verified")
	}
}

// ---------------------------------------------------------------------------
// extractClientIP
// ---------------------------------------------------------------------------

func TestExtractClientIP_WithPort(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = testClientIP192 + ":12345"
	ip := extractClientIP(req)
	if ip != testClientIP192 {
		t.Errorf("extractClientIP = %q, want 192.168.1.1", ip)
	}
}

func TestExtractClientIP_WithoutPort(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = testClientIP192
	ip := extractClientIP(req)
	if ip != testClientIP192 {
		t.Errorf("extractClientIP = %q, want 192.168.1.1", ip)
	}
}

func TestExtractClientIP_XForwardedFor(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "127.0.0.1:1234"
	req.Header.Set("X-Forwarded-For", "203.0.113.50")
	ip := extractClientIP(req)
	if ip == "" {
		t.Error("should return non-empty IP")
	}
}

// ---------------------------------------------------------------------------
// ageGateSecure
// ---------------------------------------------------------------------------

func TestAgeGateSecure_ProxiedHTTPS(t *testing.T) {
	req := httptest.NewRequest("GET", "http://example.com/", nil)
	req.RemoteAddr = "127.0.0.1:1234"
	req.Header.Set("X-Forwarded-Proto", "https")
	if !ageGateSecure(req) {
		t.Error("X-Forwarded-Proto https from trusted proxy should be secure")
	}
}

func TestAgeGateSecure_PlainHTTP(t *testing.T) {
	req := httptest.NewRequest("GET", "http://example.com/", nil)
	req.RemoteAddr = "203.0.113.1:1234"
	if ageGateSecure(req) {
		t.Error("plain HTTP from untrusted IP should not be secure")
	}
}
