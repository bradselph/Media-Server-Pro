package web

import (
	"embed"
	"io"
	"io/fs"
	"mime"
	"net/http"
	"path"
	"strconv"
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

// contentTypeForPath returns a strict Content-Type for SPA assets (nosniff-safe).
func contentTypeForPath(rel string) string {
	ext := strings.ToLower(path.Ext(rel))
	if ext != "" {
		if t := mime.TypeByExtension(ext); t != "" {
			return t
		}
	}
	switch ext {
	case ".js", ".mjs":
		return "text/javascript; charset=utf-8"
	case ".css":
		return "text/css; charset=utf-8"
	case ".json", ".map":
		return "application/json; charset=utf-8"
	case ".svg":
		return "image/svg+xml"
	case ".woff":
		return "font/woff"
	case ".woff2":
		return "font/woff2"
	case ".ttf":
		return "font/ttf"
	case ".webp":
		return "image/webp"
	case ".ico":
		return "image/x-icon"
	case ".html":
		return "text/html; charset=utf-8"
	default:
		return "application/octet-stream"
	}
}

// serveEmbedFSImmutable serves one file from root with explicit Content-Type (no sniffing).
func serveEmbedFSImmutable(c *gin.Context, root fs.FS, urlPrefix string) {
	p := c.Request.URL.Path
	if !strings.HasPrefix(p, urlPrefix) {
		c.Status(http.StatusNotFound)
		return
	}
	rel := strings.TrimPrefix(strings.TrimPrefix(p, urlPrefix), "/")
	if rel == "" || strings.Contains(rel, "..") {
		c.Status(http.StatusNotFound)
		return
	}
	f, err := root.Open(rel)
	if err != nil {
		c.Status(http.StatusNotFound)
		return
	}
	defer f.Close()
	stat, err := f.Stat()
	if err != nil || stat.IsDir() {
		c.Status(http.StatusNotFound)
		return
	}
	rs, ok := f.(io.ReadSeeker)
	if !ok {
		c.Status(http.StatusInternalServerError)
		return
	}
	ct := contentTypeForPath(rel)
	c.Header("Cache-Control", "public, max-age=31536000, immutable")
	c.Header("Content-Type", ct)
	c.Header("Content-Length", strconv.FormatInt(stat.Size(), 10))
	if c.Request.Method == http.MethodHead {
		c.Status(http.StatusOK)
		return
	}
	c.Status(http.StatusOK)
	if _, err := io.Copy(c.Writer, rs); err != nil {
		// Client disconnected — ignore
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
		"/api/", "/web/static/", "/_nuxt/", "/media", "/download", "/thumbnail", "/thumbnails/", "/hls/", "/remote/",
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
	for _, method := range []string{"GET", "HEAD"} {
		m := method
		r.Handle(m, "/web/static/*filepath", func(c *gin.Context) {
			serveEmbedFSImmutable(c, staticFS, "/web/static/")
		})
	}
	log.Info("Static file serving enabled at /web/static/")
	return true
}

// registerNuxtAssetsAtRoot serves Nuxt/Vite client chunks at /_nuxt/ when app.baseURL is /.
// Embeds live under web/static/react/_nuxt/ (same tree as /web/static/react/_nuxt/).
func registerNuxtAssetsAtRoot(r *gin.Engine) bool {
	staticFS, err := fs.Sub(content, "static")
	if err != nil {
		return false
	}
	reactFS, err := fs.Sub(staticFS, "react")
	if err != nil {
		return false
	}
	nuxtFS, err := fs.Sub(reactFS, "_nuxt")
	if err != nil {
		// No _nuxt dir (e.g. React-only build) — skip quietly
		return true
	}
	for _, method := range []string{"GET", "HEAD"} {
		m := method
		r.Handle(m, "/_nuxt/*filepath", func(c *gin.Context) {
			serveEmbedFSImmutable(c, nuxtFS, "/_nuxt/")
		})
	}
	log.Info("Nuxt client assets enabled at /_nuxt/")
	return true
}

// spaRoutes are pre-registered so Gin matches them directly; other SPA paths are still
// served via NoRoute. Keep in sync with web/nuxt-ui/pages/ and web/frontend/src/App.tsx.
var spaRoutes = []string{"/", "/login", "/signup", "/admin-login", "/profile", "/player", "/admin"}

// RegisterStaticRoutes sets up static file serving and template routes.
// This function is safe to call even if embedded files are missing.
func RegisterStaticRoutes(r *gin.Engine) {
	registerEmbeddedStatic(r)
	registerNuxtAssetsAtRoot(r)

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
