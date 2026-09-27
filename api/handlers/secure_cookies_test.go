package handlers

import (
	"net/http/httptest"
	"testing"

	"media-server-pro/internal/config"
	"media-server-pro/pkg/models"
)

// C25 regression: Auth.SecureCookies was declared, defaulted, and env-overridable
// but never read by setSessionCookie/clearSessionCookie — an operator setting
// AUTH_SECURE_COOKIES=true had no effect on cookie behavior. Secure must now be
// isSecureRequest(r) || cfg.Auth.SecureCookies for both set and clear paths, and
// the two must agree or the browser treats them as different cookies.

func sessionCookieTestHandler(t *testing.T, secureCookies bool) *Handler {
	t.Helper()
	m := newStreamAuthTestConfig(t)
	if err := m.Update(func(c *config.Config) { c.Auth.SecureCookies = secureCookies }); err != nil {
		t.Fatalf("update config: %v", err)
	}
	return &Handler{config: m}
}

func TestSetSessionCookie_SecureCookiesConfigForcesSecure(t *testing.T) {
	h := sessionCookieTestHandler(t, true)
	w := httptest.NewRecorder()
	// Plain HTTP, no trusted-proxy headers — isSecureRequest alone would say false.
	req := httptest.NewRequest("GET", "/", nil)

	h.setSessionCookie(w, req, &models.Session{ID: "s1"})

	cookies := w.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("expected 1 cookie, got %d", len(cookies))
	}
	if !cookies[0].Secure {
		t.Error("Secure should be true when auth.secure_cookies is set, even over plain HTTP")
	}
}

func TestSetSessionCookie_SecureCookiesConfigOffFallsBackToAutoDetect(t *testing.T) {
	h := sessionCookieTestHandler(t, false)
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/", nil)

	h.setSessionCookie(w, req, &models.Session{ID: "s1"})

	cookies := w.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("expected 1 cookie, got %d", len(cookies))
	}
	if cookies[0].Secure {
		t.Error("Secure should be false over plain HTTP when auth.secure_cookies is unset")
	}
}

func TestClearSessionCookie_SecureCookiesConfigForcesSecure(t *testing.T) {
	h := sessionCookieTestHandler(t, true)
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/", nil)

	h.clearSessionCookie(w, req)

	cookies := w.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("expected 1 cookie, got %d", len(cookies))
	}
	if !cookies[0].Secure {
		t.Error("clearSessionCookie must match setSessionCookie's Secure attribute, or the " +
			"browser treats them as different cookies and never clears the one it holds")
	}
}

func TestClearSessionCookie_SecureCookiesConfigOffFallsBackToAutoDetect(t *testing.T) {
	h := sessionCookieTestHandler(t, false)
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/", nil)

	h.clearSessionCookie(w, req)

	cookies := w.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("expected 1 cookie, got %d", len(cookies))
	}
	if cookies[0].Secure {
		t.Error("Secure should be false over plain HTTP when auth.secure_cookies is unset")
	}
}
