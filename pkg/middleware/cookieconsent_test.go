package middleware

import (
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"media-server-pro/internal/config"
)

// TestCookieConsent_UpdateConfig_Live is a regression for the CO-1 wiring fix:
// cookie-consent config edits must apply to the running middleware live (no
// restart). It drives the real status handler before and after UpdateConfig.
func TestCookieConsent_UpdateConfig_Live(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cc := NewCookieConsent(config.CookieConsentConfig{Enabled: false, CookieName: "cookie_consent"})

	required := func() bool {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest("GET", "/api/cookie-consent/status", nil)
		cc.GinStatusHandler()(c)
		var env struct {
			Data struct {
				Required bool `json:"required"`
			} `json:"data"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
			t.Fatalf("decode status body: %v", err)
		}
		return env.Data.Required
	}

	if required() {
		t.Fatal("precondition: banner should not be required when disabled")
	}
	cc.UpdateConfig(config.CookieConsentConfig{Enabled: true, CookieName: "cookie_consent"})
	if !required() {
		t.Error("after UpdateConfig(Enabled=true) the status handler should report required=true live")
	}
}

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
