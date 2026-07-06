package handlers

import (
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/gin-gonic/gin"
)

// TestDiscoveryWildcardPath_DecodedExactlyOnce guards the fix for the
// DismissDiscoverySuggestion double-unescape bug. With gin's defaults
// (UseRawPath unset), the *path wildcard param is already percent-decoded by
// net/http, so the handler must consume c.Param("path") directly and must NOT
// call url.PathUnescape on it a second time. A filename containing a literal
// '%' (e.g. "50% Off.mp4") — which the frontend encodes once per segment — would
// otherwise make a second unescape fail with "invalid URL escape" and 400 every
// dismiss request.
func TestDiscoveryWildcardPath_DecodedExactlyOnce(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()

	var got string
	r.DELETE("/api/admin/discovery/*path", func(c *gin.Context) {
		got = c.Param("path")
		c.Status(200)
	})

	// The frontend does path.split('/').map(encodeURIComponent).join('/'), so
	// "/movies/50% Off.mp4" is sent as "/movies/50%25%20Off.mp4": %25 -> '%',
	// %20 -> ' '. net/http (and thus gin) decode this exactly once.
	req := httptest.NewRequest("DELETE", "/api/admin/discovery/movies/50%25%20Off.mp4", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if want := "/movies/50% Off.mp4"; got != want {
		t.Fatalf("gin *path wildcard must be decoded exactly once: got %q, want %q", got, want)
	}

	// This is the crux of the original bug: a SECOND unescape of the already-
	// decoded value fails because "% O" is not a valid percent-escape. The
	// handler relies on NOT doing this. If this ever stops failing, the double-
	// unescape would be silently reintroduced without breaking the test above.
	if _, err := url.PathUnescape(got); err == nil {
		t.Fatalf("expected a second url.PathUnescape(%q) to fail (that was the bug), but it succeeded", got)
	}
}
