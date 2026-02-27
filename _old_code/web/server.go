package web

import (
	"embed"
	"io/fs"
	"net/http"

	"github.com/gorilla/mux"

	"media-server-pro/internal/logger"
)

//go:embed static/*
var content embed.FS

// log is the web package logger
var log = logger.New("web")

// cacheMiddleware adds cache headers for static assets
func cacheMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		next.ServeHTTP(w, r)
	})
}

// RegisterStaticRoutes sets up static file serving and template routes
// This function is safe to call even if embedded files are missing
func RegisterStaticRoutes(r *mux.Router, thumbnailDir string) {
	// Try to serve static files
	staticFS, err := fs.Sub(content, "static")
	if err != nil {
		log.Warn("Static files not available: %v", err)
	} else {
		staticHandler := http.StripPrefix("/web/static/", http.FileServer(http.FS(staticFS)))
		r.PathPrefix("/web/static/").Handler(cacheMiddleware(staticHandler))
		log.Info("Static file serving enabled at /web/static/")
	}

	// All routes serve the React SPA. React Router handles client-side routing.
	// /admin and /profile have lightweight cookie-presence guards so bots cannot
	// scrape the admin UI without first hitting the login redirect.
	r.HandleFunc("/", serveReactApp())
	r.HandleFunc("/login", serveReactApp())
	r.HandleFunc("/signup", serveReactApp())
	r.HandleFunc("/admin-login", serveReactApp())
	r.HandleFunc("/profile", requireEitherCookie("session_id", "admin_session", "/login", serveReactApp()))
	r.HandleFunc("/player", serveReactApp())
	r.HandleFunc("/admin", requireSessionCookie("admin_session", "/admin-login", serveReactApp()))

	log.Info("Web routes registered")
}

// requireSessionCookie wraps a handler so that requests without the named cookie
// are redirected to redirectTo instead. This provides a lightweight server-side
// auth gate for template routes (the real auth validation happens in the API layer).
func requireSessionCookie(cookieName, redirectTo string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, err := r.Cookie(cookieName); err != nil {
			http.Redirect(w, r, redirectTo, http.StatusFound)
			return
		}
		next(w, r)
	}
}

// requireEitherCookie wraps a handler so that requests without at least one of
// the named cookies are redirected to redirectTo. Used for pages accessible to
// both regular users (session_id) and admin users (admin_session).
func requireEitherCookie(cookie1, cookie2, redirectTo string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		_, err1 := r.Cookie(cookie1)
		_, err2 := r.Cookie(cookie2)
		if err1 != nil && err2 != nil {
			http.Redirect(w, r, redirectTo, http.StatusFound)
			return
		}
		next(w, r)
	}
}

// serveReactApp returns a handler that serves the React SPA's index.html.
// The React app handles client-side routing via React Router, so all converted
// routes serve the same index.html and let the JS router take over.
func serveReactApp() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		data, err := content.ReadFile("static/react/index.html")
		if err != nil {
			// Fall back to old template if React build not present
			log.Warn("React SPA not available, falling back: %v", err)
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(http.StatusNotFound)
			if _, writeErr := w.Write([]byte(`<!DOCTYPE html>
<html>
<head><title>Page Not Found</title></head>
<body>
<h1>React App Not Built</h1>
<p>The React frontend has not been built yet. Run: cd web/frontend && npm run build</p>
<p><a href="/">Return to Home</a></p>
</body>
</html>`)); writeErr != nil {
				log.Warn("Failed to write error page: %v", writeErr)
			}
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if _, err := w.Write(data); err != nil {
			log.Warn("Failed to write React SPA response: %v", err)
		}
	}
}
