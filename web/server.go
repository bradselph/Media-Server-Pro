package web

import (
	"embed"
	"io/fs"
	"mime"
	"net/http"
	"path"
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

// fixMimeResponseWriter forces Content-Type from the URL basename when the upstream
// left it empty or used text/plain (common with nginx error bodies or bad mime.types).
type fixMimeResponseWriter struct {
	gin.ResponseWriter
	urlPath     string
	headerFixed bool
}

func (w *fixMimeResponseWriter) WriteHeader(code int) {
	if !w.headerFixed {
		w.headerFixed = true
		p := strings.Split(w.urlPath, "?")[0]
		base := path.Base(p)
		if ext := path.Ext(base); ext != "" {
			ct := w.Header().Get("Content-Type")
			lower := strings.ToLower(ct)
			if ct == "" || strings.HasPrefix(lower, "text/plain") {
				w.Header().Set("Content-Type", contentTypeForPath(base))
			}
		}
	}
	w.ResponseWriter.WriteHeader(code)
}

//go:embed static/*
// Nuxt/React build output under web/static/ is baked in at compile time; the running binary
// serves these bytes over HTTP (no separate UI tree needed on the host).
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

// registerEmbeddedStatic registers embedded files at /web/static/ (React + Nuxt output).
func registerEmbeddedStatic(r *gin.Engine) bool {
	staticFS, err := fs.Sub(content, "static")
	if err != nil {
		log.Warn("Static files not available: %v", err)
		return false
	}
	fileServer := http.StripPrefix("/web/static/", http.FileServer(http.FS(staticFS)))
	for _, method := range []string{"GET", "HEAD"} {
		m := method
		r.Handle(m, "/web/static/*filepath", func(c *gin.Context) {
			c.Header("Cache-Control", "public, max-age=31536000, immutable")
			w := &fixMimeResponseWriter{ResponseWriter: c.Writer, urlPath: c.Request.URL.Path}
			fileServer.ServeHTTP(w, c.Request)
		})
	}
	log.Info("Static file serving enabled at /web/static/")
	return true
}

// registerNuxtLegacyRedirect maps /_nuxt/* → /web/static/react/_nuxt/* for old cached HTML
// or edge rules that never forwarded /_nuxt to the app. Canonical asset URLs are under /web/static/react/.
func registerNuxtLegacyRedirect(r *gin.Engine) {
	handler := func(c *gin.Context) {
		suffix := strings.TrimPrefix(c.Request.URL.Path, "/_nuxt/")
		suffix = strings.TrimPrefix(suffix, "/")
		loc := "/web/static/react/_nuxt/" + suffix
		if c.Request.URL.RawQuery != "" {
			loc += "?" + c.Request.URL.RawQuery
		}
		c.Redirect(http.StatusPermanentRedirect, loc)
	}
	r.GET("/_nuxt/*filepath", handler)
	r.HEAD("/_nuxt/*filepath", handler)
}

// spaLegacyPaths are redirected to the Nuxt app base so the client router matches the URL bar.
var spaLegacyPaths = []string{"/login", "/signup", "/admin-login", "/profile", "/player", "/admin"}

// RegisterStaticRoutes sets up static file serving and template routes.
// This function is safe to call even if embedded files are missing.
func RegisterStaticRoutes(r *gin.Engine) {
	registerEmbeddedStatic(r)
	registerNuxtLegacyRedirect(r)

	spaHandler := ginServeReactApp()

	// Canonical app lives under /web/static/react/ (matches Nuxt app.baseURL).
	r.GET("/", func(c *gin.Context) {
		target := "/web/static/react/"
		if q := c.Request.URL.RawQuery; q != "" {
			target += "?" + q
		}
		c.Redirect(http.StatusFound, target)
	})
	for _, p := range spaLegacyPaths {
		route := p
		r.GET(route, func(c *gin.Context) {
			target := "/web/static/react" + route
			if q := c.Request.URL.RawQuery; q != "" {
				target += "?" + q
			}
			c.Redirect(http.StatusFound, target)
		})
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
<p>The SPA has not been built yet. From the repo root run one of:</p>
<ul>
<li><code>cd web/nuxt-ui &amp;&amp; npm ci &amp;&amp; npm run build</code> (default deploy)</li>
<li><code>cd web/frontend &amp;&amp; npm ci &amp;&amp; npm run build</code> (set <code>WEB_UI=react</code> for deploy)</li>
</ul>
<p>Then rebuild the Go server so <code>web/static/react/</code> is embedded.</p>
<p><a href="/web/static/react/">Open app</a></p>
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
