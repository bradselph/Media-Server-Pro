package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"media-server-pro/internal/config"
)

func newTestAgeGate(enabled bool) *AgeGate {
	return NewAgeGate(config.AgeGateConfig{
		Enabled:      enabled,
		BypassIPs:    []string{"10.0.0.0/8"},
		IPVerifyTTL:  24 * time.Hour,
		CookieName:   "age_verified",
		CookieMaxAge: 365 * 24 * 60 * 60,
	})
}

// ---------------------------------------------------------------------------
// NewAgeGate
// ---------------------------------------------------------------------------

func TestNewAgeGate(t *testing.T) {
	ag := newTestAgeGate(true)
	if ag == nil {
		t.Fatal("NewAgeGate returned nil")
	}
	if len(ag.bypassNetworks) != 1 {
		t.Errorf("bypass networks = %d, want 1", len(ag.bypassNetworks))
	}
}

func TestNewAgeGate_InvalidCIDR(t *testing.T) {
	ag := NewAgeGate(config.AgeGateConfig{
		Enabled:   true,
		BypassIPs: []string{"invalid-cidr", "10.0.0.0/8"},
	})
	if len(ag.bypassNetworks) != 1 {
		t.Errorf("should skip invalid CIDR, got %d networks", len(ag.bypassNetworks))
	}
}

func TestNewAgeGate_PlainIP(t *testing.T) {
	ag := NewAgeGate(config.AgeGateConfig{
		Enabled:   true,
		BypassIPs: []string{"192.168.1.1"},
	})
	if len(ag.bypassNetworks) != 1 {
		t.Errorf("plain IP should be parsed as /32, got %d networks", len(ag.bypassNetworks))
	}
}

func TestNewAgeGate_IPv6PlainIP(t *testing.T) {
	ag := NewAgeGate(config.AgeGateConfig{
		Enabled:   true,
		BypassIPs: []string{"::1"},
	})
	if len(ag.bypassNetworks) != 1 {
		t.Errorf("IPv6 plain IP should be parsed as /128, got %d networks", len(ag.bypassNetworks))
	}
}

// ---------------------------------------------------------------------------
// IsVerified
// ---------------------------------------------------------------------------

func TestIsVerified_Disabled(t *testing.T) {
	ag := newTestAgeGate(false)
	req := httptest.NewRequest("GET", "/", nil)
	if !ag.IsVerified(req) {
		t.Error("disabled gate should always verify")
	}
}

func TestIsVerified_BypassIP(t *testing.T) {
	ag := newTestAgeGate(true)
	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "10.0.0.5:1234"
	if !ag.IsVerified(req) {
		t.Error("bypass IP should be verified")
	}
}

func TestIsVerified_NonBypassIP_NoCookie(t *testing.T) {
	ag := newTestAgeGate(true)
	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "203.0.113.1:1234"
	if ag.IsVerified(req) {
		t.Error("non-bypass IP without cookie should not be verified")
	}
}

func TestIsVerified_WithCookie(t *testing.T) {
	ag := newTestAgeGate(true)
	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "203.0.113.1:1234"
	req.AddCookie(&http.Cookie{Name: "age_verified", Value: "1"})
	if !ag.IsVerified(req) {
		t.Error("request with age_verified cookie should be verified")
	}
}

func TestIsVerified_WithWrongCookieValue(t *testing.T) {
	ag := newTestAgeGate(true)
	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "203.0.113.1:1234"
	req.AddCookie(&http.Cookie{Name: "age_verified", Value: "0"})
	if ag.IsVerified(req) {
		t.Error("cookie with value '0' should not verify")
	}
}

func TestIsVerified_IPVerified(t *testing.T) {
	ag := newTestAgeGate(true)
	ip := "203.0.113.50"
	ag.mu.Lock()
	ag.verifiedIPs[ip] = time.Now()
	ag.mu.Unlock()

	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = ip + ":1234"
	if !ag.IsVerified(req) {
		t.Error("previously verified IP should pass")
	}
}

func TestIsVerified_ExpiredIPVerification(t *testing.T) {
	ag := NewAgeGate(config.AgeGateConfig{
		Enabled:     true,
		IPVerifyTTL: 1 * time.Millisecond,
		CookieName:  "age_verified",
	})
	ip := "203.0.113.60"
	ag.mu.Lock()
	ag.verifiedIPs[ip] = time.Now().Add(-1 * time.Hour) // long expired
	ag.mu.Unlock()

	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = ip + ":1234"
	if ag.IsVerified(req) {
		t.Error("expired IP verification should not pass")
	}
}

// ---------------------------------------------------------------------------
// isBypass
// ---------------------------------------------------------------------------

func TestIsBypass(t *testing.T) {
	ag := newTestAgeGate(true)
	if !ag.isBypass("10.0.0.1") {
		t.Error("10.0.0.1 should be bypass")
	}
	if ag.isBypass("203.0.113.1") {
		t.Error("203.0.113.1 should not be bypass")
	}
	if ag.isBypass("invalid") {
		t.Error("invalid IP should not be bypass")
	}
}

// ---------------------------------------------------------------------------
// evictExpired
// ---------------------------------------------------------------------------

func TestEvictExpired(t *testing.T) {
	ag := NewAgeGate(config.AgeGateConfig{
		Enabled:     true,
		IPVerifyTTL: 1 * time.Hour,
		CookieName:  "age_verified",
	})
	ag.mu.Lock()
	ag.verifiedIPs["fresh"] = time.Now()
	ag.verifiedIPs["stale"] = time.Now().Add(-2 * time.Hour)
	ag.mu.Unlock()

	ag.evictExpired()

	ag.mu.RLock()
	defer ag.mu.RUnlock()
	if _, ok := ag.verifiedIPs["fresh"]; !ok {
		t.Error("fresh entry should not be evicted")
	}
	if _, ok := ag.verifiedIPs["stale"]; ok {
		t.Error("stale entry should be evicted")
	}
}

// ---------------------------------------------------------------------------
// Gin handlers
// ---------------------------------------------------------------------------

func TestGinStatusHandler(t *testing.T) {
	ag := newTestAgeGate(true)
	r := gin.New()
	r.GET("/status", ag.GinStatusHandler())

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/status", nil)
	req.RemoteAddr = "203.0.113.1:1234"
	r.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Errorf("status code = %d, want 200", w.Code)
	}
}

func TestGinVerifyHandler(t *testing.T) {
	ag := newTestAgeGate(true)
	r := gin.New()
	r.POST("/verify", ag.GinVerifyHandler())

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/verify", nil)
	req.RemoteAddr = "203.0.113.1:1234"
	r.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Errorf("status code = %d, want 200", w.Code)
	}

	// Check cookie was set
	cookies := w.Result().Cookies()
	found := false
	for _, c := range cookies {
		if c.Name == "age_verified" && c.Value == "1" {
			found = true
		}
	}
	if !found {
		t.Error("age_verified cookie not set after verify")
	}

	// Check IP was recorded
	ag.mu.RLock()
	_, ok := ag.verifiedIPs["203.0.113.1"]
	ag.mu.RUnlock()
	if !ok {
		t.Error("IP should be recorded after verify")
	}
}

// ---------------------------------------------------------------------------
// extractClientIP
// ---------------------------------------------------------------------------

func TestExtractClientIP(t *testing.T) {
	// Direct connection
	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "203.0.113.1:1234"
	if got := extractClientIP(req); got != "203.0.113.1" {
		t.Errorf("direct: got %q", got)
	}

	// Via trusted proxy with X-Forwarded-For
	req = httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "127.0.0.1:1234"
	req.Header.Set("X-Forwarded-For", "203.0.113.50, 10.0.0.1")
	if got := extractClientIP(req); got != "203.0.113.50" {
		t.Errorf("XFF via trusted proxy: got %q, want 203.0.113.50", got)
	}

	// Via untrusted proxy (should ignore XFF)
	req = httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "8.8.8.8:1234"
	req.Header.Set("X-Forwarded-For", "1.2.3.4")
	if got := extractClientIP(req); got != "8.8.8.8" {
		t.Errorf("XFF via untrusted proxy: got %q, want 8.8.8.8", got)
	}
}

// ---------------------------------------------------------------------------
// parseBypassCIDR
// ---------------------------------------------------------------------------

func TestParseBypassCIDR(t *testing.T) {
	ag := newTestAgeGate(true) // just to get a logger
	tests := []struct {
		input string
		ok    bool
	}{
		{"10.0.0.0/8", true},
		{"192.168.1.1", true},
		{"::1", true},
		{"", false},
		{"  ", false},
		{"not-an-ip", false},
	}
	for _, tc := range tests {
		_, ok := parseBypassCIDR(ag.log, tc.input)
		if ok != tc.ok {
			t.Errorf("parseBypassCIDR(%q) ok=%v, want %v", tc.input, ok, tc.ok)
		}
	}
}

// ---------------------------------------------------------------------------
// ageGateSecure
// ---------------------------------------------------------------------------

func TestAgeGateSecure(t *testing.T) {
	// Regular HTTP
	req := httptest.NewRequest("GET", "/", nil)
	if ageGateSecure(req) {
		t.Error("plain HTTP should not be secure")
	}

	// X-Forwarded-Proto
	req = httptest.NewRequest("GET", "/", nil)
	req.Header.Set("X-Forwarded-Proto", "https")
	if !ageGateSecure(req) {
		t.Error("X-Forwarded-Proto: https should be secure")
	}

	// Cloudflare
	req = httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Cf-Visitor", `{"scheme":"https"}`)
	if !ageGateSecure(req) {
		t.Error("Cf-Visitor with https should be secure")
	}
}
