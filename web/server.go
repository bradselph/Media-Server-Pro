package web

import (
	"embed"
	"io/fs"
	"net/http"
	"os"
	"strings"
	"sync"

	"github.com/gin-gonic/gin"

	"media-server-pro/internal/logger"
)

//go:embed static/*
var content embed.FS

// log is the web package logger
var log = logger.New("web")

// overlayFS checks the disk directory first; if the file doesn't exist there
// it falls back to the embedded filesystem. This lets users drop custom files
// into a directory to override the compiled-in defaults.
type overlayFS struct {
	disk     fs.FS // on-disk overrides (may be nil)
	embedded fs.FS // compiled-in defaults
}

func (o *overlayFS) Open(name string) (fs.File, error) {
	if o.disk != nil {
		if f, err := o.disk.Open(name); err == nil {
			return f, nil
		}
	}
	return o.embedded.Open(name)
}

// buildStaticFS returns an fs.FS rooted at "static/" that layers the optional
// on-disk customDir over the embedded defaults. If customDir is empty or does
// not exist, only the embedded FS is used.
func buildStaticFS(customDir string) (fs.FS, error) {
	embeddedStatic, err := fs.Sub(content, "static")
	if err != nil {
		return nil, err
	}
	if customDir == "" {
		return embeddedStatic, nil
	}
	info, err := os.Stat(customDir)
	if err != nil || !info.IsDir() {
		log.Info("Custom static dir %q not found, using embedded defaults", customDir)
		return embeddedStatic, nil
	}
	log.Info("Custom static overrides loaded from %s", customDir)
	return &overlayFS{
		disk:     os.DirFS(customDir),
		embedded: embeddedStatic,
	}, nil
}

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

// registerStaticHandler registers static file routes using the given filesystem.
// Returns true if successful.
func registerStaticHandler(r *gin.Engine, staticFS fs.FS) bool {
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
// served via NoRoute. Keep in sync with web/frontend/src/App.tsx when adding top-level routes.
var spaRoutes = []string{"/", "/login", "/signup", "/admin-login", "/profile", "/player", "/admin"}

// RegisterStaticRoutes sets up static file serving and template routes.
// customDir is the path to an optional on-disk directory whose files take
// precedence over the compiled-in embedded assets. Pass "" to use only defaults.
// This function is safe to call even if embedded files are missing.
func RegisterStaticRoutes(r *gin.Engine, customDir string) {
	staticFS, err := buildStaticFS(customDir)
	if err != nil {
		log.Warn("Static files not available: %v", err)
	} else {
		registerStaticHandler(r, staticFS)
	}

	spaHandler := ginServeReactApp(customDir)
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

// ginServeReactApp returns a gin handler that serves the React SPA's index.html.
// It checks the custom directory first for an override, then falls back to the
// embedded default. The result is cached on first request.
func ginServeReactApp(customDir string) gin.HandlerFunc {
	const fallbackHTML = `<!DOCTYPE html>
<html>
<head><title>Page Not Found</title></head>
<body>
<h1>React App Not Built</h1>
<p>The React frontend has not been built yet. Run: cd web/frontend && npm run build</p>
<p><a href="/">Return to Home</a></p>
</body>
</html>`
	return func(c *gin.Context) {
		indexHTMLOnce.Do(func() {
			// Try on-disk override first
			if customDir != "" {
				diskPath := customDir + "/react/index.html"
				if data, err := os.ReadFile(diskPath); err == nil {
					cachedIndex = data
					log.Info("Serving custom index.html from %s", diskPath)
					return
				}
			}
			// Fall back to embedded
			cachedIndex, cachedIndexErr = content.ReadFile("static/react/index.html")
		})
		if cachedIndexErr != nil {
			log.Warn("React SPA not available, falling back: %v", cachedIndexErr)
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
