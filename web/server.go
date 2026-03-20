package web

import (
	"embed"
	"io/fs"
	"mime"
	"net/http"
	"strings"
	"sync"

	"github.com/gin-gonic/gin"

	"media-server-pro/internal/logger"
)

func init() {
	// Minimal and VPS hosts often lack full /etc/mime.types. Without a known type,
	// http.FileServer falls back to sniffing and can label minified JS as text/plain;
	// X-Content-Type-Options: nosniff then blocks module scripts (strict MIME check).
	for ext, typ := range map[string]string{
		".css":   "text/css; charset=utf-8",
		".html":  "text/html; charset=utf-8",
		".ico":   "image/x-icon",
		".js":    "text/javascript; charset=utf-8",
		".json":  "application/json; charset=utf-8",
		".map":   "application/json; charset=utf-8",
		".mjs":   "text/javascript; charset=utf-8",
		".svg":   "image/svg+xml",
		".ttf":   "font/ttf",
		".webp":  "image/webp",
		".woff":  "font/woff",
		".woff2": "font/woff2",
	} {
		_ = mime.AddExtensionType(ext, typ)
	}
}

//go:embed static/*
var content embed.FS

// log is the web package logger
var log = logger.New("web")

// pathExcludedFromSPA reports whether path is an API, static, or media route that
// should not be served by the React SPA (should return 404 instead).
func pathExcludedFromSPA(path string) bool {
	excludedPrefixes := []string{
		"/api/", "/web/static/", "/media", "/download", "/thumbnail", "/thumbnails/", "/hls/", "/remote/",
		"/extractor/", "/ws/", "/health", "/metrics",
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
// served via NoRoute. Keep in sync with web/nuxt-ui/pages/ and web/frontend/src/App.tsx.
var spaRoutes = []string{"/", "/login", "/signup", "/admin-login", "/profile", "/player", "/admin"}

// RegisterStaticRoutes sets up static file serving and template routes.
// This function is safe to call even if embedded files are missing.
func RegisterStaticRoutes(r *gin.Engine) {
	registerEmbeddedStatic(r)

	spaHandler := ginServeReactApp()
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

var (
	indexHTMLOnce  sync.Once
	cachedIndex    []byte
	cachedIndexErr error
)

// ginServeReactApp returns a gin handler that serves the embedded SPA index.html
// (Nuxt or React build output under web/static/react/).
// Index HTML is read once on first request and cached to avoid per-request embed reads.
func ginServeReactApp() gin.HandlerFunc {
	const fallbackHTML = `<!DOCTYPE html>
<html>
<head><title>Page Not Found</title></head>
<body>
<h1>Web UI Not Built</h1>
<p>The SPA has not been built into the binary yet. From the repo root run one of:</p>
<ul>
<li><code>cd web/nuxt-ui &amp;&amp; npm ci &amp;&amp; npm run build</code> (default deploy)</li>
<li><code>cd web/frontend &amp;&amp; npm ci &amp;&amp; npm run build</code> (set <code>WEB_UI=react</code> for deploy)</li>
</ul>
<p>Then rebuild the Go server so <code>web/static/react/</code> is embedded.</p>
<p><a href="/">Return to Home</a></p>
</body>
</html>`
	return func(c *gin.Context) {
		indexHTMLOnce.Do(func() {
			cachedIndex, cachedIndexErr = content.ReadFile("static/react/index.html")
		})
		if cachedIndexErr != nil {
			log.Warn("Embedded SPA (web/static/react) not available, falling back: %v", cachedIndexErr)
			c.Header("Content-Type", "text/html; charset=utf-8")
			c.Status(http.StatusNotFound)
			_, _ = c.Writer.WriteString(fallbackHTML)
			return
		}
		c.Header("Content-Type", "text/html; charset=utf-8")
		c.Header("Cache-Control", "no-cache")
		_, _ = c.Writer.Write(cachedIndex)
	}
}
