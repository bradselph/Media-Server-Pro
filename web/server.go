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
		"/api/", "/web/static/", "/media", "/download", "/thumbnail", "/hls/", "/remote/",
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

// spaRoutes are the top-level paths that serve the React SPA index.
var spaRoutes = []string{"/", "/login", "/signup", "/admin-login", "/profile", "/player", "/admin"}

// RegisterStaticRoutes sets up static file serving and template routes.
// This function is safe to call even if embedded files are missing.
func RegisterStaticRoutes(r *gin.Engine, thumbnailDir string) {
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
