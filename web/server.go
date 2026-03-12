package web

import (
	"embed"
	"io/fs"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"media-server-pro/internal/logger"
)

//go:embed static/*
var content embed.FS

// log is the web package logger
var log = logger.New("web")

// pathExcludedFromSPA reports whether path is an API, static, or media route that
// should not be served by the React SPA (should return 404 instead).
func pathExcludedFromSPA(path string) bool {
	excludedPrefixes := []string{
		"/api/", "/web/static/", "/media", "/download", "/thumbnail", "/thumbnails/", "/hls/", "/remote/",
		"/extractor/", "/health", "/metrics",
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

// TODO: Incomplete feature — spaRoutes is a hardcoded list that must be manually kept
// in sync with the React router in web/frontend/src/App.tsx. If a new route is added
// to the frontend (e.g. "/settings"), it will work via the NoRoute fallback but will
// not be pre-registered, meaning Gin's route tree won't match it directly. This works
// but is fragile. Consider removing the explicit route registrations and relying solely
// on the NoRoute handler for SPA routing, since the NoRoute handler already serves the
// SPA for non-excluded paths.
var spaRoutes = []string{"/", "/login", "/signup", "/admin-login", "/profile", "/player", "/admin"}

// TODO: Redundant code — the second parameter (_ string) is accepted but ignored.
// The call site passes cfg.Get().Directories.Thumbnails but it is discarded. Either
// remove the parameter to clarify the API, or use it if thumbnail directory info is
// needed for static serving.

// RegisterStaticRoutes sets up static file serving and template routes.
// This function is safe to call even if embedded files are missing.
func RegisterStaticRoutes(r *gin.Engine, _ string) {
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

// TODO: Performance — ginServeReactApp reads the embedded index.html from the
// embed.FS on every request via content.ReadFile. While embed.FS reads are fast
// (in-memory), this still involves a map lookup and byte slice copy per request.
// The file content is immutable at runtime, so it should be read once at init time
// and served from a pre-read []byte. Also, no Cache-Control header is set for the
// SPA HTML, so browsers may cache a stale index.html after deployments. Consider
// adding Cache-Control: no-cache or using ETag-based caching for the HTML.

// ginServeReactApp returns a gin handler that serves the React SPA's index.html.
func ginServeReactApp() gin.HandlerFunc {
	return func(c *gin.Context) {
		data, err := content.ReadFile("static/react/index.html")
		if err != nil {
			log.Warn("React SPA not available, falling back: %v", err)
			c.Header("Content-Type", "text/html; charset=utf-8")
			c.Status(http.StatusNotFound)
			_, _ = c.Writer.WriteString(`<!DOCTYPE html>
<html>
<head><title>Page Not Found</title></head>
<body>
<h1>React App Not Built</h1>
<p>The React frontend has not been built yet. Run: cd web/frontend && npm run build</p>
<p><a href="/">Return to Home</a></p>
</body>
</html>`)
			return
		}
		c.Header("Content-Type", "text/html; charset=utf-8")
		_, _ = c.Writer.Write(data)
	}
}
