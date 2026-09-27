package middleware

import (
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

// C24 regression: GinSecurityHeaders skipped setting the self-set security
// headers (X-Content-Type-Options, X-Frame-Options, X-XSS-Protection,
// Referrer-Policy) whenever the request carried a CF-Ray header, on the
// assumption the connection was proxied through Cloudflare (which sets its
// own equivalents). CF-Ray is an ordinary client-supplied request header, so
// any direct client could spoof it to suppress its own security headers. The
// fix mirrors isHTTPS's existing IsTrustedProxy gate on X-Forwarded-Proto:
// CF-Ray is only honored when the immediate peer is a trusted proxy.

func securityHeadersTestRequest(remoteAddr, cfRay string) *httptest.ResponseRecorder {
	r := gin.New()
	r.Use(GinSecurityHeaders(func() (string, int) { return "", 0 }))
	r.GET("/test", func(c *gin.Context) { c.String(200, "ok") })

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/test", nil)
	req.RemoteAddr = remoteAddr
	if cfRay != "" {
		req.Header.Set("CF-Ray", cfRay)
	}
	r.ServeHTTP(w, req)
	return w
}

func assertSecurityHeadersSet(t *testing.T, w *httptest.ResponseRecorder, want bool) {
	t.Helper()
	for _, name := range []string{"X-Content-Type-Options", "X-Frame-Options", "X-XSS-Protection", "Referrer-Policy"} {
		if got := w.Header().Get(name) != ""; got != want {
			t.Errorf("header %s set = %v, want %v", name, got, want)
		}
	}
}

func TestGinSecurityHeaders_CFRayFromUntrustedPeerIsIgnored(t *testing.T) {
	// A directly-connected public client spoofing CF-Ray must not be able to
	// suppress the server's own security headers.
	w := securityHeadersTestRequest("203.0.113.5:1234", "abc123")
	assertSecurityHeadersSet(t, w, true)
}

func TestGinSecurityHeaders_CFRayFromTrustedProxyIsHonored(t *testing.T) {
	// A trusted reverse proxy (e.g. a local nginx terminating Cloudflare) is
	// allowed to signal "Cloudflare already set these headers".
	w := securityHeadersTestRequest("127.0.0.1:1234", "abc123")
	assertSecurityHeadersSet(t, w, false)
}

func TestGinSecurityHeaders_NoCFRaySetsHeadersRegardlessOfPeer(t *testing.T) {
	// With no CF-Ray at all, headers are always self-set — trusted or not.
	assertSecurityHeadersSet(t, securityHeadersTestRequest("203.0.113.5:1234", ""), true)
	assertSecurityHeadersSet(t, securityHeadersTestRequest("127.0.0.1:1234", ""), true)
}
