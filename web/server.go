package web

import (
	"embed"
	"io/fs"
	"net/http"

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
	r.GET("/", ginServeReactApp())
	r.GET("/login", ginServeReactApp())
	r.GET("/signup", ginServeReactApp())
	r.GET("/admin-login", ginServeReactApp())
	r.GET("/profile", ginRequireEitherCookie("session_id", "admin_session", "/login", ginServeReactApp()))
	r.GET("/player", ginServeReactApp())
	r.GET("/admin", ginRequireSessionCookie("admin_session", "/admin-login", ginServeReactApp()))

	log.Info("Web routes registered")
}

// ginRequireSessionCookie wraps a handler so that requests without the named cookie
// are redirected to redirectTo instead.
func ginRequireSessionCookie(cookieName, redirectTo string, next gin.HandlerFunc) gin.HandlerFunc {
	return func(c *gin.Context) {
		if _, err := c.Cookie(cookieName); err != nil {
			c.Redirect(http.StatusFound, redirectTo)
			return
		}
		next(c)
	}
}

// ginRequireEitherCookie wraps a handler so that requests without at least one of
// the named cookies are redirected to redirectTo.
func ginRequireEitherCookie(cookie1, cookie2, redirectTo string, next gin.HandlerFunc) gin.HandlerFunc {
	return func(c *gin.Context) {
		_, err1 := c.Cookie(cookie1)
		_, err2 := c.Cookie(cookie2)
		if err1 != nil && err2 != nil {
			c.Redirect(http.StatusFound, redirectTo)
			return
		}
		next(c)
	}
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
