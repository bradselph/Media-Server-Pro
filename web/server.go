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

// RegisterStaticRoutes sets up static file serving and template routes.
// This function is safe to call even if embedded files are missing.
func RegisterStaticRoutes(r *gin.Engine, thumbnailDir string) {
	// Try to serve static files
	staticFS, err := fs.Sub(content, "static")
	if err != nil {
		log.Warn("Static files not available: %v", err)
	} else {
		staticHandler := http.StripPrefix("/web/static/", http.FileServer(http.FS(staticFS)))
		r.GET("/web/static/*filepath", func(c *gin.Context) {
			c.Header("Cache-Control", "public, max-age=31536000, immutable")
			staticHandler.ServeHTTP(c.Writer, c.Request)
		})
		r.HEAD("/web/static/*filepath", func(c *gin.Context) {
			c.Header("Cache-Control", "public, max-age=31536000, immutable")
			staticHandler.ServeHTTP(c.Writer, c.Request)
		})
		log.Info("Static file serving enabled at /web/static/")
	}

	// All routes serve the React SPA. React Router handles client-side routing.
	spaHandler := ginServeReactApp()
	r.GET("/", spaHandler)
	r.GET("/login", spaHandler)
	r.GET("/signup", spaHandler)
	r.GET("/admin-login", spaHandler)
	r.GET("/profile", spaHandler)
	r.GET("/player", spaHandler)
	r.GET("/admin", spaHandler)

	// SPA catch-all: any path not matching an API/static/media route serves the
	// React app so that client-side routing (React Router) works on page refresh.
	r.NoRoute(func(c *gin.Context) {
		p := c.Request.URL.Path
		// Only serve the SPA for paths that are NOT API, static assets, or media streams.
		if strings.HasPrefix(p, "/api/") ||
			strings.HasPrefix(p, "/web/static/") ||
			strings.HasPrefix(p, "/media") ||
			strings.HasPrefix(p, "/download") ||
			strings.HasPrefix(p, "/thumbnail") ||
			strings.HasPrefix(p, "/hls/") ||
			strings.HasPrefix(p, "/remote/") {
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
