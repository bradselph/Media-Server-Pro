package middleware

import (
	"net/http/httptest"
	"testing"

	"media-server-pro/internal/config"
)

// FND-0024: CookieConsent.isSameOrigin must be fail-OPEN for absent Origin/Referer
// (matching AgeGate.isSameOrigin), so non-browser callers (curl, native apps) are
// not rejected. Browsers always send Origin on cross-origin POSTs, so genuine CSRF
// attempts still carry a header and fail the host check below.
func TestCookieConsent_isSameOrigin_NoHeaders(t *testing.T) {
	cc := NewCookieConsent(config.CookieConsentConfig{})
	req := httptest.NewRequest("POST", "/api/cookie-consent", nil)
	req.Host = testExampleHost
	if !cc.isSameOrigin(req) {
		t.Error("absent Origin/Referer should be treated as same-origin (fail-open)")
	}
}

func TestCookieConsent_isSameOrigin_MatchingOrigin(t *testing.T) {
	cc := NewCookieConsent(config.CookieConsentConfig{})
	req := httptest.NewRequest("POST", "/api/cookie-consent", nil)
	req.Host = testExampleHost
	req.Header.Set("Origin", "http://example.com")
	if !cc.isSameOrigin(req) {
		t.Error("matching Origin header should be same-origin")
	}
}

func TestCookieConsent_isSameOrigin_CrossOrigin(t *testing.T) {
	cc := NewCookieConsent(config.CookieConsentConfig{})
	req := httptest.NewRequest("POST", "/api/cookie-consent", nil)
	req.Host = testExampleHost
	req.Header.Set("Origin", "http://evil.com")
	if cc.isSameOrigin(req) {
		t.Error("cross-origin POST with mismatched Origin must be rejected")
	}
}
