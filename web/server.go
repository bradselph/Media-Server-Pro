// Package web serves the embedded frontend static assets.
package web

import (
	"embed"
	"io/fs"
	"net/http"
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
	for _, prefix := range excludedPrefixes {
		if strings.HasPrefix(path, prefix) {
			return true
		}
	}
	return false
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
var spaRoutes = []string{"/", "/login", "/signup", "/admin-login", "/profile", "/player", "/admin"}

// RegisterStaticRoutes sets up static file serving and template routes.
// This function is safe to call even if embedded files are missing.
func RegisterStaticRoutes(r *gin.Engine) {
	registerEmbeddedStatic(r)
	registerNuxtAssets(r)

	spaHandler := ginServeSPA()
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
func ginServeSPA() gin.HandlerFunc {
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
		_, _ = c.Writer.Write(cachedIndex)
	}
}
