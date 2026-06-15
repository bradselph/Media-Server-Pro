// Package web serves the embedded frontend static assets.
package web

import (
	"embed"
	"io/fs"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"sync"

	"github.com/gin-gonic/gin"

	"media-server-pro/internal/logger"
)

//go:embed all:static
var content embed.FS

// log is the web package logger
var log = logger.New("web")

// pathExcludedFromSPA reports whether path is an API, static, or media route that
// should not be served by the SPA (should return 404 instead).
func pathExcludedFromSPA(path string) bool {
	excludedPrefixes := []string{
		"/api/", "/web/static/", "/media", "/download", "/thumbnail", "/thumbnails/", "/hls/", "/remote/",
		"/extractor/", "/ws/", "/health", "/metrics",
		"/_nuxt/",  // Nuxt UI build assets
		"/_fonts/", // Nuxt UI cached fonts
	}
	if slices.ContainsFunc(excludedPrefixes, func(prefix string) bool {
		return strings.HasPrefix(path, prefix)
	}) {
		return true
	}
	// Exact-match crawler endpoints — these are served by the Go router
	// (api/handlers/seo.go) and must not be swallowed by the SPA index.
	return path == "/sitemap.xml" || path == "/robots.txt"
}

// registerEmbeddedStatic registers embedded static file routes. Returns true if successful.
func registerEmbeddedStatic(r *gin.Engine) bool {
	staticFS, err := fs.Sub(content, "static")
	if err != nil {
		log.Warn("Static files not available: %v", err)
		return false
	}
	staticHandler := http.StripPrefix("/web/static/", http.FileServer(http.FS(staticFS)))
	for _, method := range []string{"GET", "HEAD"} {
		m := method
		r.Handle(m, "/web/static/*filepath", func(c *gin.Context) {
			c.Header("Cache-Control", "public, max-age=31536000, immutable")
			staticHandler.ServeHTTP(c.Writer, c.Request)
		})
	}
	log.Info("Static file serving enabled at /web/static/")
	return true
}

// spaRoutes are pre-registered so Gin matches them directly; other SPA paths are still
// served via NoRoute. Keep in sync with web/nuxt-ui/pages/ when adding top-level routes.
var spaRoutes = []string{"/", "/login", "/signup", "/admin-login", "/profile", "/player", "/admin", "/2257", "/dmca"}

// ShellMeta carries server-rendered SEO additions spliced into the SPA shell
// for a single request. The Nuxt frontend is a pure client-side SPA (ssr:false),
// so the served index.html contains no rendered markup — only an empty
// <div id="__nuxt">. A crawler that does not execute JavaScript (notably the
// Internet Archive's Heritrix crawler, but also social-card scrapers) therefore
// indexes nothing. Splicing real metadata + a <noscript> fallback into the shell
// gives those clients real HTML and a crawlable link graph without running the
// bundle or replaying API calls. All fields are optional; empty fields are skipped.
//
// Title and Description are spliced into HTML text/attribute positions and MUST
// be HTML-escaped by the producer. Head and NoScript are raw HTML fragments the
// producer is responsible for escaping safely (attribute values, JSON-LD).
type ShellMeta struct {
	Title       string // replaces the shell <title> when non-empty
	Description string // replaces the shell <meta name="description"> when non-empty
	Head        string // raw HTML spliced before </head> (OG/Twitter/canonical/JSON-LD)
	NoScript    string // raw HTML wrapped in <noscript> and spliced before </body>
}

// ShellEnricher produces per-request SEO additions for indexable routes. It
// returns the zero ShellMeta for routes that need no enrichment, in which case
// the shell is served byte-for-byte unchanged. May be nil.
type ShellEnricher func(c *gin.Context) ShellMeta

var (
	shellTitleRe = regexp.MustCompile(`(?s)<title>.*?</title>`)
	shellDescRe  = regexp.MustCompile(`<meta name="description"[^>]*>`)
)

// applyShellMeta splices m into the SPA shell. When m is empty it returns shell
// unchanged (and uncopied), so the common non-indexable path stays allocation-free.
func applyShellMeta(shell []byte, m ShellMeta) []byte {
	if m.Title == "" && m.Description == "" && m.Head == "" && m.NoScript == "" {
		return shell
	}
	s := string(shell)
	if m.Title != "" {
		// ReplaceAllLiteralString so a '$' in the title is not treated as a
		// regexp replacement reference. There is only one <title> in the shell.
		s = shellTitleRe.ReplaceAllLiteralString(s, "<title>"+m.Title+"</title>")
	}
	if m.Description != "" {
		s = shellDescRe.ReplaceAllLiteralString(s, `<meta name="description" content="`+m.Description+`">`)
	}
	if m.Head != "" {
		s = strings.Replace(s, "</head>", m.Head+"</head>", 1)
	}
	if m.NoScript != "" {
		s = strings.Replace(s, "</body>", "<noscript>"+m.NoScript+"</noscript></body>", 1)
	}
	return []byte(s)
}

// RegisterStaticRoutes sets up static file serving and template routes.
// This function is safe to call even if embedded files are missing. enrich may
// be nil; when set, it injects per-route SEO metadata into the served shell.
func RegisterStaticRoutes(r *gin.Engine, enrich ShellEnricher) {
	registerEmbeddedStatic(r)
	registerNuxtAssets(r)

	spaHandler := ginServeSPA(enrich)
	for _, path := range spaRoutes {
		r.GET(path, spaHandler)
	}

	r.NoRoute(func(c *gin.Context) {
		if pathExcludedFromSPA(c.Request.URL.Path) {
			c.JSON(http.StatusNotFound, gin.H{"error": "Not found"})
			return
		}
		spaHandler(c)
	})

	log.Info("Web routes registered")
}

// registerNuxtAssets serves Nuxt UI's /_nuxt/ and /_fonts/ asset bundles from the embedded FS.
func registerNuxtAssets(r *gin.Engine) {
	staticFS, err := fs.Sub(content, "static")
	if err != nil {
		log.Warn("Nuxt assets not available: %v", err)
		return
	}

	// Serve embedded assets directly — the request path is rewritten to match the
	// embedded FS layout (static/react/_nuxt/..., static/react/_fonts/...).
	// No StripPrefix needed because we set the full path manually.
	fileServer := http.FileServer(http.FS(staticFS))

	// /_nuxt/ — hashed JS/CSS chunks
	for _, method := range []string{"GET", "HEAD"} {
		m := method
		r.Handle(m, "/_nuxt/*filepath", func(c *gin.Context) {
			c.Header("Cache-Control", "public, max-age=31536000, immutable")
			c.Request.URL.Path = "/react/_nuxt/" + strings.TrimPrefix(c.Param("filepath"), "/")
			fileServer.ServeHTTP(c.Writer, c.Request)
		})
	}

	// /_fonts/ — locally cached web fonts
	for _, method := range []string{"GET", "HEAD"} {
		m := method
		r.Handle(m, "/_fonts/*filepath", func(c *gin.Context) {
			c.Header("Cache-Control", "public, max-age=31536000, immutable")
			c.Request.URL.Path = "/react/_fonts/" + strings.TrimPrefix(c.Param("filepath"), "/")
			fileServer.ServeHTTP(c.Writer, c.Request)
		})
	}
}

var (
	indexHTMLOnce  sync.Once
	cachedIndex    []byte
	errCachedIndex error
)

// ginServeSPA returns a gin handler that serves the SPA's index.html.
// Index HTML is read once on first request and cached to avoid per-request embed reads.
// When enrich is non-nil, its per-route ShellMeta is spliced into the shell so
// non-JS crawlers receive real metadata and a <noscript> content fallback.
func ginServeSPA(enrich ShellEnricher) gin.HandlerFunc {
	const fallbackHTML = `<!DOCTYPE html>
<html>
<head><title>Frontend Not Built</title></head>
<body>
<h1>Frontend Not Built</h1>
<p>Run: <code>cd web/nuxt-ui &amp;&amp; npm run build</code></p>
<p><a href="/">Return to Home</a></p>
</body>
</html>`
	return func(c *gin.Context) {
		indexHTMLOnce.Do(func() {
			cachedIndex, errCachedIndex = content.ReadFile("static/react/index.html")
		})
		if errCachedIndex != nil {
			log.Warn("React SPA not available, falling back: %v", errCachedIndex)
			c.Header("Content-Type", "text/html; charset=utf-8")
			c.Header("Cache-Control", "no-cache")
			c.Status(http.StatusNotFound)
			_, _ = c.Writer.WriteString(fallbackHTML)
			return
		}
		c.Header("Content-Type", "text/html; charset=utf-8")
		c.Header("Cache-Control", "no-cache")
		out := cachedIndex
		if enrich != nil {
			// applyShellMeta returns cachedIndex unchanged for non-indexable
			// routes (zero ShellMeta), so this stays a no-op on the hot path.
			out = applyShellMeta(cachedIndex, enrich(c))
		}
		_, _ = c.Writer.Write(out)
	}
}
