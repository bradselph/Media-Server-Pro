package routes

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// A gin handler that is written, reviewed and merged but never bound to a path is
// invisible: it compiles, it is covered by its own unit tests, and it 404s in
// production. That is exactly how GET /api/hub/embeds/:id/playback shipped broken
// — the frontend calls it as the first step of server-side Hub playback and treats
// any non-2xx as "this item cannot be proxied", so the whole feature was
// unreachable no matter how hub.proxy_enabled was configured.
//
// This test closes that gap for every handler at once rather than pinning the one
// route that happened to be missed.

// ginHandlerRe matches the canonical handler signature used throughout
// api/handlers: func (h *Handler) Name(c *gin.Context).
var ginHandlerRe = regexp.MustCompile(`(?m)^func \(h \*Handler\) ([A-Z]\w*)\(c \*gin\.Context\)`)

// handlersReferencedOutsideRoutes lists handler methods that are deliberately not
// bound to a path in routes.go. Empty today: every handler is registered. Add an
// entry here (with a reason) only when a handler is genuinely invoked some other
// way — otherwise the correct fix is to register the route.
var handlersReferencedOutsideRoutes = map[string]string{}

func TestEveryGinHandlerIsRegistered(t *testing.T) {
	routesSrc, err := os.ReadFile("routes.go")
	if err != nil {
		t.Fatalf("read routes.go: %v", err)
	}
	routes := string(routesSrc)

	files, err := filepath.Glob(filepath.Join("..", "handlers", "*.go"))
	if err != nil {
		t.Fatalf("glob handlers: %v", err)
	}
	if len(files) == 0 {
		t.Fatal("no handler files found — has the package layout changed?")
	}

	var found int
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		src, readErr := os.ReadFile(file)
		if readErr != nil {
			t.Fatalf("read %s: %v", file, readErr)
		}
		for _, m := range ginHandlerRe.FindAllStringSubmatch(string(src), -1) {
			name := m[1]
			found++
			if reason, ok := handlersReferencedOutsideRoutes[name]; ok {
				t.Logf("%s: not routed by design (%s)", name, reason)
				continue
			}
			// Word-boundary match on the h.<Name> call site, so GetHubEmbed does
			// not accidentally satisfy GetHubEmbedPlayback.
			ref := regexp.MustCompile(`\bh\.` + regexp.QuoteMeta(name) + `\b`)
			if !ref.MatchString(routes) {
				t.Errorf("handler %s (%s) is never registered in routes.go — it will 404 in production",
					name, filepath.Base(file))
			}
		}
	}
	if found == 0 {
		t.Fatal("matched no gin handlers — the signature convention likely changed, making this test vacuous")
	}
	t.Logf("checked %d gin handlers", found)
}

// TestHubProxyRoutesRegistered pins the specific set of routes that server-side
// Hub playback depends on. The capability probe is listed first because it is the
// one the frontend calls before it will attach a player at all: if it is missing,
// none of the byte routes below it are ever requested.
func TestHubProxyRoutesRegistered(t *testing.T) {
	routesSrc, err := os.ReadFile("routes.go")
	if err != nil {
		t.Fatalf("read routes.go: %v", err)
	}
	routes := string(routesSrc)

	for _, want := range []struct{ path, handler string }{
		{"/hub/embeds/:id/playback", "GetHubEmbedPlayback"},
		{"/hub/proxy/:id/master.m3u8", "HubProxyMaster"},
		{"/hub/proxy/:id/stream", "HubProxyStream"},
		{"/hub/proxy/:id/:quality/playlist.m3u8", "HubProxyVariant"},
		{"/hub/proxy/:id/:quality/:asset", "HubProxyAsset"},
		{"/hub/img/:id/t", "HubProxyThumb"},
		{"/hub/img/:id/p/:idx", "HubProxyPreview"},
	} {
		if !strings.Contains(routes, `"`+want.path+`"`) {
			t.Errorf("route %s is not registered", want.path)
		}
		if !strings.Contains(routes, "h."+want.handler) {
			t.Errorf("handler %s for %s is not registered", want.handler, want.path)
		}
	}
}
