// Package routes sets up API routes for the media server.
package routes

import (
	"bytes"
	"fmt"
	"net"
	"net/http"
	"strings"

	"github.com/gin-contrib/gzip"
	"github.com/gin-gonic/gin"

	"media-server-pro/api/handlers"
	"media-server-pro/internal/auth"
	"media-server-pro/internal/config"
	"media-server-pro/internal/logger"
	"media-server-pro/internal/security"
	"media-server-pro/internal/server"
	"media-server-pro/pkg/middleware"
	"media-server-pro/pkg/models"
	"media-server-pro/web"
)

const (
	pathMedia             = "/media"
	pathWatchHistory      = "/watch-history"
	pathPlaylists         = "/playlists"
	pathSecurityWhitelist = "/security/whitelist"
	pathSecurityBlacklist = "/security/blacklist"
	pathScannerQueue      = "/scanner/queue"
	pathStats             = "/stats"

	routeMedia        = "/media"
	routeThumbnail    = "/thumbnail"
	routePlaylistByID = "/playlists/:id"
	routeUserByName   = "/users/:username"
)

// sessionAuth loads session/user context from the session_id cookie (or a Bearer
// API token) and stores both on the gin context so downstream handlers can read them.
func sessionAuth(authModule *auth.Module) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Cookie-based session (browser clients)
		cookie, err := c.Cookie("session_id")
		if err == nil && cookie != "" {
			session, user, err := authModule.ValidateSession(c.Request.Context(), cookie)
			if err == nil {
				c.Set("session", session)
				c.Set("user", user)
			} else if auth.IsSessionError(err) {
				// Clear stale/expired/invalid cookie so the browser stops resending it.
				// DB/transient errors are NOT treated as invalid sessions — the cookie is
				// preserved so the user is not silently logged out during a DB outage.
				// Only trust proxy headers (X-Forwarded-Proto, Cf-Visitor) from
				// trusted proxy IPs to prevent clients from spoofing HTTPS.
				secure := c.Request.TLS != nil
				if !secure {
					remoteIP, _, splitErr := net.SplitHostPort(c.Request.RemoteAddr)
					if splitErr != nil {
						remoteIP = c.Request.RemoteAddr
					}
					if middleware.IsTrustedProxy(remoteIP) {
						secure = c.GetHeader("X-Forwarded-Proto") == "https" ||
							strings.Contains(c.GetHeader("Cf-Visitor"), `"scheme":"https"`)
					}
				}
				http.SetCookie(c.Writer, &http.Cookie{
					Name:     "session_id",
					Value:    "",
					Path:     "/",
					MaxAge:   -1,
					HttpOnly: true,
					Secure:   secure,
					SameSite: http.SameSiteStrictMode,
				})
			}
			c.Next()
			return
		}
		// Bearer API token via Authorization header (programmatic / headless clients)
		bearer := strings.TrimPrefix(c.GetHeader("Authorization"), "Bearer ")
		// URL query token fallback — used by RSS readers and other clients that
		// cannot set arbitrary request headers (e.g. ?token=<api-token>).
		// Only accepted on the /api/feed route to limit the surface area.
		if bearer == "" && c.FullPath() == "/api/feed" {
			bearer = c.Query("token")
		}
		if bearer != "" {
			session, user, tokenErr := authModule.ValidateAPIToken(c.Request.Context(), bearer)
			if tokenErr == nil {
				c.Set("session", session)
				c.Set("user", user)
			} else {
				// Store the rejection reason so requireAuth can return a specific
				// error instead of a generic 401.
				c.Set("bearer_error", tokenErr.Error())
			}
		}
		c.Next()
	}
}

// adminAuth requires an authenticated session with role=admin and an enabled account.
// Admin and regular users both use the session_id cookie; this checks the role set by sessionAuth.
// Disabled admins receive 403 Forbidden (authenticated but not authorized).
func adminAuth(_ *auth.Module) gin.HandlerFunc {
	return func(c *gin.Context) {
		userVal, exists := c.Get("user")
		if !exists {
			c.JSON(http.StatusUnauthorized, gin.H{"success": false, "error": "Unauthorized"})
			c.Abort()
			return
		}
		user, ok := userVal.(*models.User)
		if !ok || user.Role != models.RoleAdmin {
			c.JSON(http.StatusForbidden, gin.H{"success": false, "error": "Forbidden"})
			c.Abort()
			return
		}
		if !user.Enabled {
			c.JSON(http.StatusForbidden, gin.H{"success": false, "error": "Account disabled"})
			c.Abort()
			return
		}
		c.Next()
	}
}

// requireAuth requires an authenticated, non-expired session with an enabled user.
func requireAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		sessionVal, exists := c.Get("session")
		if !exists {
			c.JSON(http.StatusUnauthorized, gin.H{"success": false, "error": "Unauthorized"})
			c.Abort()
			return
		}
		session, ok := sessionVal.(*models.Session)
		if !ok || session == nil || session.IsExpired() {
			c.JSON(http.StatusUnauthorized, gin.H{"success": false, "error": "Unauthorized"})
			c.Abort()
			return
		}
		// Reject disabled users even if they hold a valid session
		if userVal, ok := c.Get("user"); ok {
			if user, ok := userVal.(*models.User); ok && !user.Enabled {
				c.JSON(http.StatusForbidden, gin.H{"success": false, "error": "Account disabled"})
				c.Abort()
				return
			}
		}
		c.Next()
	}
}

const etagMaxBodySize = 1024 * 1024 // 1 MB; larger responses stream through without ETag

// ginETags adds content-based ETag for GET/HEAD on /api/*. Responses larger than
// etagMaxBodySize are streamed directly to avoid buffering large bodies in memory.
func ginETags() gin.HandlerFunc {
	return func(c *gin.Context) {
		if (c.Request.Method != http.MethodGet && c.Request.Method != http.MethodHead) ||
			!strings.HasPrefix(c.Request.URL.Path, "/api/") {
			c.Next()
			return
		}

		blw := &etagBufferWriter{
			ResponseWriter: c.Writer,
			body:           bytes.NewBuffer(nil),
			maxSize:        etagMaxBodySize,
		}
		c.Writer = blw
		c.Next()

		if blw.overflowed {
			return // already streamed
		}
		if blw.Status() < 200 || blw.Status() >= 300 {
			if blw.body.Len() > 0 {
				_, _ = blw.ResponseWriter.Write(blw.body.Bytes())
			}
			return
		}

		etag := `"` + hashFNV1a(blw.body.Bytes()) + `"`
		c.Header("ETag", etag)
		if c.GetHeader("If-None-Match") == etag {
			c.Status(http.StatusNotModified)
			return
		}
		if blw.body.Len() > 0 {
			_, _ = blw.ResponseWriter.Write(blw.body.Bytes())
		}
	}
}

// etagBufferWriter buffers the response up to maxSize; beyond that it streams directly.
type etagBufferWriter struct {
	gin.ResponseWriter
	body       *bytes.Buffer
	maxSize    int
	overflowed bool
}

func (w *etagBufferWriter) Write(b []byte) (int, error) {
	if w.overflowed {
		return w.ResponseWriter.Write(b)
	}
	if w.body.Len()+len(b) > w.maxSize {
		w.overflowed = true
		if w.body.Len() > 0 {
			_, _ = w.ResponseWriter.Write(w.body.Bytes())
			w.body.Reset()
		}
		return w.ResponseWriter.Write(b)
	}
	return w.body.Write(b)
}

// hashFNV1a computes a 64-bit FNV-1a hash of the given bytes and returns it as a hex string.
// 64-bit reduces birthday collision probability vs the original 32-bit version.
func hashFNV1a(data []byte) string {
	h := uint64(14695981039346656037)
	for _, b := range data {
		h ^= uint64(b)
		h *= 1099511628211
	}
	return fmt.Sprintf("%x", h)
}

// Setup configures all routes on the gin engine.
// securityModule.GinMiddleware() is defined in internal/security/security.go.
func Setup(r *gin.Engine, srv *server.Server, h *handlers.Handler, authModule *auth.Module, securityModule *security.Module, cfg *config.Manager, ageGate *middleware.AgeGate, cookieConsent *middleware.CookieConsent) {
	// srv may be nil in tests; status/modules routes are skipped when nil
	log := logger.New("routes")

	// Request ID for tracing
	r.Use(middleware.GinRequestID())

	// Security headers (CSP, HSTS, X-Frame-Options, etc.)
	// getCfg is called on every request so CSPEnabled/HSTSEnabled changes take
	// effect immediately without a server restart.
	r.Use(middleware.GinSecurityHeaders(func() (csp string, hstsMaxAge int) {
		sc := cfg.Get().Security
		if sc.CSPEnabled {
			csp = sc.CSPPolicy
		}
		if sc.HSTSEnabled {
			hstsMaxAge = sc.HSTSMaxAge
		}
		return csp, hstsMaxAge
	}))

	// CORS — only applied when explicitly configured.
	// When auth is enabled and CORS origins contains only "*", replace the
	// wildcard with the server's own public URL to prevent accidental open
	// CORS in production.  HLS/extractor modules handle their own CORS for
	// media player compatibility.
	secCfg := cfg.Get().Security
	if secCfg.CORSEnabled && len(secCfg.CORSOrigins) > 0 {
		corsOrigins := secCfg.CORSOrigins
		authCfg := cfg.Get().Auth
		if authCfg.Enabled && len(corsOrigins) == 1 && corsOrigins[0] == "*" {
			serverCfg := cfg.Get().Server
			scheme := "http"
			if serverCfg.EnableHTTPS {
				scheme = "https"
			}
			host := serverCfg.Host
			if host == "" || host == "0.0.0.0" || host == "127.0.0.1" {
				// Cannot determine public origin; disable CORS entirely
				// since same-origin requests don't need it.
				log.Warn("CORS wildcard origin (*) with auth enabled — disabling CORS. " +
					"Set cors_origins to your frontend domain to enable cross-origin access.")
				corsOrigins = nil
			} else {
				port := serverCfg.Port
				origin := fmt.Sprintf("%s://%s", scheme, host)
				if (scheme == "http" && port != 80) || (scheme == "https" && port != 443) {
					origin = fmt.Sprintf("%s:%d", origin, port)
				}
				log.Warn("CORS wildcard origin (*) with auth enabled — restricting to %s. "+
					"Set cors_origins explicitly to override.", origin)
				corsOrigins = []string{origin}
			}
		}
		if len(corsOrigins) > 0 {
			r.Use(middleware.GinCORS(
				corsOrigins,
				[]string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
				[]string{"Content-Type", "Authorization", "X-Requested-With"},
			))
		}
	}

	// Apply compression middleware for all responses (except media streams).
	// gin-contrib/gzip skips paths that start with the excluded prefixes.
	r.Use(gzip.Gzip(gzip.DefaultCompression, gzip.WithExcludedPaths([]string{
		routeMedia,
		"/download",
		"/hls/",
		routeThumbnail,
		"/thumbnails/",
		"/remote/stream",
		"/receiver/stream",
		"/extractor/hls/",
		// Embedded SPA assets: correct Content-Type + nosniff; avoid gzip wrapper edge cases
		"/web/static/",
		"/_nuxt/",
	})))

	// Apply ETag caching for GET/HEAD /api/* JSON responses
	r.Use(ginETags())

	// Apply security middleware (rate limiting, IP filtering)
	r.Use(securityModule.GinMiddleware())

	// Apply session middleware to all routes
	r.Use(sessionAuth(authModule))

	// -----------------------------------------------------------------------
	// Direct routes (not under /api)
	// -----------------------------------------------------------------------

	// ROUTE PATTERN EXPLANATION:
	// /api/media  - API endpoint returning JSON list of media items (ListMedia handler)
	// /media      - Streaming endpoint serving actual media files (StreamMedia handler)
	//
	// These use different base paths by design:
	// - /api/* routes are for JSON API responses
	// - Direct /* routes are for file serving/streaming
	//
	// Frontend usage:
	// - fetch('/api/media') → get list of available media
	// - <video src="/media?path=..."> → stream the actual file
	// /media and /download: no route-level auth; handlers use cfg.Download.RequireAuth and stream limits.
	r.GET(routeMedia, h.StreamMedia)
	r.GET("/download", h.DownloadMedia)

	// HLS streaming (direct, high-frequency — excluded from rate limiting and gzip)
	r.GET("/hls/:id/master.m3u8", h.ServeMasterPlaylist)
	r.GET("/hls/:id/:quality/playlist.m3u8", h.ServeVariantPlaylist)
	r.GET("/hls/:id/:quality/:segment", h.ServeSegment)

	// Thumbnail serving (direct)
	r.GET(routeThumbnail, h.GetThumbnail)
	r.HEAD(routeThumbnail, h.GetThumbnail)
	r.GET("/thumbnails/:filename", h.ServeThumbnailFile)
	r.HEAD("/thumbnails/:filename", h.ServeThumbnailFile)

	// /health — unauthenticated, returns 200 OK or 503 depending on critical module status.
	// Used by systemd healthcheck scripts, uptime monitors, and nginx upstream health checks.
	r.GET("/health", h.GetHealth)

	// /metrics is for Prometheus scraping — admin-protected, no frontend caller by design
	r.GET("/metrics", adminAuth(authModule), h.GetMetrics)

	// Remote streaming — frontend uses mediaApi.getRemoteStreamUrl()
	r.GET("/remote/stream", requireAuth(), h.StreamRemoteMedia)

	// Extractor HLS proxy — session auth required; excluded from gzip; handlers validate item exists.
	r.GET("/extractor/hls/:id/master.m3u8", requireAuth(), h.ExtractorHLSMaster)
	r.GET("/extractor/hls/:id/:quality/playlist.m3u8", requireAuth(), h.ExtractorHLSVariant)
	r.GET("/extractor/hls/:id/:quality/:segment", requireAuth(), h.ExtractorHLSSegment)

	// Receiver WebSocket — middleware enforces valid X-API-Key or api_key before upgrade.
	r.GET("/ws/receiver", h.RequireReceiverWithAPIKey(), h.ReceiverWebSocket)

	// Downloader WebSocket proxy — admin only (session + admin auth applied before upgrade)
	r.GET("/ws/admin/downloader", adminAuth(authModule), h.AdminDownloaderWebSocket)

	// Downloader verify — the downloader calls this to verify admin identity before allowing server storage
	r.GET("/api/admin/downloader/verify", adminAuth(authModule), h.AdminDownloaderVerify)

	// -----------------------------------------------------------------------
	// API routes group (/api)
	// -----------------------------------------------------------------------
	api := r.Group("/api")

	// Admin-only status/modules (prevent fingerprinting and info leakage)
	if srv != nil {
		api.GET("/status", adminAuth(authModule), srv.HandleStatus)
		api.GET("/modules", adminAuth(authModule), srv.HandleModules)
		api.GET("/modules/:name/health", adminAuth(authModule), srv.HandleModuleHealth)
	}

	// Version — public, no auth (index page footer)
	api.GET("/version", h.GetVersion)

	// Media routes (mostly public)
	api.GET(pathMedia, h.ListMedia)
	api.GET(pathMedia+pathStats, h.GetMediaStats)
	api.GET(pathMedia+"/categories", h.GetCategories)
	api.GET(pathMedia+"/batch", h.GetBatchMedia)
	api.GET(pathMedia+"/:id", h.GetMedia)
	api.GET(pathMedia+"/:id/collections", h.GetMediaCollections)

	// Playback
	api.GET("/playback", requireAuth(), h.GetPlaybackPosition)
	api.GET("/playback/batch", requireAuth(), h.GetBatchPlaybackPositions)
	api.POST("/playback", requireAuth(), h.TrackPlayback)

	// HLS API routes (capabilities and status require auth to prevent fingerprinting)
	api.GET("/hls/capabilities", requireAuth(), h.GetHLSCapabilities)
	api.GET("/hls/check", requireAuth(), h.CheckHLSAvailability)
	api.POST("/hls/generate", requireAuth(), h.GenerateHLS)
	api.GET("/hls/status/:id", requireAuth(), h.GetHLSStatus)

	// Auth routes (public)
	api.POST("/auth/login", h.Login)
	api.POST("/auth/logout", h.Logout)
	api.GET("/auth/register-token", h.GetRegistrationToken)
	api.POST("/auth/register", h.Register)
	api.GET("/auth/session", h.CheckSession)

	// Permissions route (public — returns different info based on auth status)
	api.GET("/permissions", h.GetPermissions)

	// Storage usage requires authentication to prevent resource exhaustion attacks
	// and information leakage to anonymous users
	api.GET("/storage-usage", requireAuth(), h.GetStorageUsage)

	// Server settings returns only public feature flags and UI config; no auth required.
	// The frontend fetches this on initial load before any login occurs.
	api.GET("/server-settings", h.GetServerSettings)

	// OpenAPI specification — requires auth to prevent unauthenticated schema discovery.
	api.GET("/docs", requireAuth(), h.GetOpenAPISpec)

	// RSS/Atom feed — returns latest media as Atom XML; optional ?category=X&type=video&limit=N
	api.GET("/feed", requireAuth(), h.GetRSSFeed)

	// Age gate — public, no auth required (must be accessible before user logs in)
	api.GET("/age-gate/status", ageGate.GinStatusHandler())
	api.POST("/age-verify", ageGate.GinVerifyHandler())

	// Cookie consent — public, no auth required (must be accessible to all visitors)
	if cookieConsent != nil {
		api.GET("/cookie-consent/status", cookieConsent.GinStatusHandler())
		api.POST("/cookie-consent", cookieConsent.GinAcceptHandler())
	}

	// User preferences routes (protected)
	api.GET("/preferences", requireAuth(), h.GetPreferences)
	api.POST("/preferences", requireAuth(), h.UpdatePreferences)

	// User password change (protected)
	api.POST("/auth/change-password", requireAuth(), h.ChangePassword)

	// Data deletion request — users submit a request; admins review and decide
	api.POST("/auth/data-deletion-request", requireAuth(), h.RequestDataDeletion)

	// Self-service account deletion (requires password confirmation)
	api.POST("/auth/delete-account", requireAuth(), h.DeleteAccount)

	// User API tokens — admin-only; adminAuth enforces this at middleware level
	api.GET("/auth/tokens", adminAuth(authModule), h.ListAPITokens)
	api.POST("/auth/tokens", adminAuth(authModule), h.CreateAPIToken)
	api.DELETE("/auth/tokens/:id", adminAuth(authModule), h.DeleteAPIToken)

	// Favorites (Watch Later)
	api.GET("/favorites", requireAuth(), h.GetFavorites)
	api.POST("/favorites", requireAuth(), h.AddFavorite)
	api.GET("/favorites/:media_id", requireAuth(), h.CheckFavorite)
	api.DELETE("/favorites/:media_id", requireAuth(), h.RemoveFavorite)

	// Chapters (scene markers / act chapters) — public read, auth write
	api.GET("/chapters", h.ListChapters)
	api.POST("/chapters", requireAuth(), h.CreateChapter)
	api.PUT("/chapters/:id", requireAuth(), h.UpdateChapter)
	api.DELETE("/chapters/:id", requireAuth(), h.DeleteChapter)

	// Watch history routes (protected)
	api.GET(pathWatchHistory, requireAuth(), h.GetWatchHistory)
	api.DELETE(pathWatchHistory, requireAuth(), h.ClearWatchHistory)
	api.GET(pathWatchHistory+"/export", requireAuth(), h.ExportWatchHistory)

	// Public playlist browsing — no auth required
	api.GET("/playlists/public", h.ListPublicPlaylists)

	// Playlist routes (protected)
	api.GET(pathPlaylists, requireAuth(), h.ListPlaylists)
	api.POST(pathPlaylists, requireAuth(), h.CreatePlaylist)
	api.POST("/playlists/bulk-delete", requireAuth(), h.BulkDeletePlaylists)
	api.GET(routePlaylistByID, requireAuth(), h.GetPlaylist)
	api.DELETE(routePlaylistByID, requireAuth(), h.DeletePlaylist)
	api.PUT(routePlaylistByID, requireAuth(), h.UpdatePlaylist)
	api.GET(routePlaylistByID+"/export", requireAuth(), h.ExportPlaylist)
	api.POST(routePlaylistByID+"/items", requireAuth(), h.AddPlaylistItem)
	api.DELETE(routePlaylistByID+"/items", requireAuth(), h.RemovePlaylistItem)
	api.PUT(routePlaylistByID+"/reorder", requireAuth(), h.ReorderPlaylistItems)
	api.DELETE(routePlaylistByID+"/clear", requireAuth(), h.ClearPlaylist)
	api.POST(routePlaylistByID+"/copy", requireAuth(), h.CopyPlaylist)

	// Smart playlists routes (protected)
	// Collections (public read)
	api.GET("/collections", h.ListCollections)
	api.GET("/collections/:id", h.GetCollection)

	api.GET("/smart-playlists", requireAuth(), h.ListSmartPlaylists)
	api.POST("/smart-playlists", requireAuth(), h.CreateSmartPlaylist)
	api.GET("/smart-playlists/:id", requireAuth(), h.GetSmartPlaylist)
	api.PUT("/smart-playlists/:id", requireAuth(), h.UpdateSmartPlaylist)
	api.DELETE("/smart-playlists/:id", requireAuth(), h.DeleteSmartPlaylist)
	api.GET("/smart-playlists/:id/preview", requireAuth(), h.PreviewSmartPlaylist)

	// Analytics routes
	// POST /analytics/events — user auth (users submit their own events)
	// Aggregate analytics endpoints — admin-only (cross-user sensitive stats)
	api.GET("/analytics", adminAuth(authModule), h.GetAnalyticsSummary)
	api.GET("/analytics/daily", adminAuth(authModule), h.GetDailyStats)
	api.GET("/analytics/top", adminAuth(authModule), h.GetTopMedia)
	api.GET("/analytics/content", adminAuth(authModule), h.GetContentPerformance)
	api.POST("/analytics/events", requireAuth(), h.SubmitEvent)
	api.GET("/analytics/events/stats", adminAuth(authModule), h.GetEventStats)
	api.GET("/analytics/events/by-type", adminAuth(authModule), h.GetEventsByType)
	api.GET("/analytics/events/by-media", adminAuth(authModule), h.GetEventsByMedia)
	api.GET("/analytics/events/by-user", adminAuth(authModule), h.GetEventsByUser)
	api.GET("/analytics/events/counts", adminAuth(authModule), h.GetEventTypeCounts)

	// Thumbnail previews (public) — frontend uses mediaApi.getThumbnailPreviews()
	api.GET("/thumbnails/previews", h.GetThumbnailPreviews)
	// Batch thumbnail URLs — ?ids=id1,id2&w=320 (max 50 ids)
	api.GET("/thumbnails/batch", h.GetThumbnailBatch)

	// Suggestions routes (public)
	api.GET("/suggestions", h.GetSuggestions)
	api.GET("/suggestions/trending", h.GetTrendingSuggestions)
	api.GET("/suggestions/similar", h.GetSimilarMedia)

	// Protected suggestions routes
	api.GET("/suggestions/continue", requireAuth(), h.GetContinueWatching)
	api.GET("/suggestions/personalized", requireAuth(), h.GetPersonalizedSuggestions)
	api.GET("/suggestions/profile", requireAuth(), h.GetMyProfile)
	api.DELETE("/suggestions/profile", requireAuth(), h.ResetMyProfile)
	api.GET("/suggestions/recent", h.GetRecentContent)
	api.GET("/suggestions/new", requireAuth(), h.GetNewSinceLastVisit)
	api.GET("/suggestions/on-deck", requireAuth(), h.GetOnDeck)
	api.POST("/ratings", requireAuth(), h.RecordRating)
	api.GET("/ratings", requireAuth(), h.GetMyRatings)

	// User-facing category browse (requires auth, returns categorized items)
	api.GET("/browse/categories", requireAuth(), h.GetCategoryBrowse)

	// Upload routes — auth enforced in handler based on cfg.Uploads.RequireAuth
	api.POST("/upload", h.UploadMedia)
	api.GET("/upload/:id/progress", h.GetUploadProgress)

	// Receiver slave API routes (authenticated via X-API-Key; RequireReceiverWithAPIKey checks enabled + nil first).
	receiverSlave := api.Group("/receiver", h.RequireReceiverWithAPIKey())
	receiverSlave.POST("/register", h.ReceiverRegisterSlave)
	receiverSlave.POST("/catalog", h.ReceiverPushCatalog)
	receiverSlave.POST("/heartbeat", h.ReceiverHeartbeat)
	// Stream push — slave delivers file data in response to a WS stream_request.
	// Part of the receiver slave group: RequireReceiverWithAPIKey middleware handles auth.
	receiverSlave.POST("/stream-push/:token", h.ReceiverStreamPush)

	// Receiver media browsing — admin only (exposes slave IDs, paths, internal topology).
	// Regular users see slave media seamlessly through the unified /api/media listing.
	api.GET("/receiver/media", adminAuth(authModule), h.ReceiverListMedia)
	api.GET("/receiver/media/:id", adminAuth(authModule), h.ReceiverGetMedia)

	// -----------------------------------------------------------------------
	// Admin routes group (/api/admin)
	// All routes under this group require adminAuth.
	// -----------------------------------------------------------------------
	adminGrp := api.Group("/admin", adminAuth(authModule))

	adminGrp.GET(pathStats, h.AdminGetStats)
	adminGrp.GET("/system", h.AdminGetSystemInfo)
	adminGrp.POST("/cache/clear", h.ClearMediaCache)

	// Update management routes
	adminGrp.GET("/update/check", h.CheckForUpdates)
	adminGrp.GET("/update/status", h.GetUpdateStatus)
	adminGrp.POST("/update/apply", h.ApplyUpdate)
	adminGrp.GET("/update/source/check", h.CheckForSourceUpdates)
	adminGrp.POST("/update/source/apply", h.ApplySourceUpdate)
	adminGrp.GET("/update/source/progress", h.GetSourceUpdateProgress)
	adminGrp.GET("/update/config", h.GetUpdateConfig)
	adminGrp.PUT("/update/config", h.SetUpdateConfig)

	// Server management routes
	adminGrp.POST("/server/restart", h.RestartServer)
	adminGrp.POST("/server/shutdown", h.ShutdownServer)

	// Active sessions and uploads
	adminGrp.GET("/streams", h.AdminGetActiveStreams)
	adminGrp.GET("/uploads/active", h.AdminGetActiveUploads)

	// User management
	adminGrp.GET("/users", h.AdminListUsers)
	adminGrp.POST("/users", h.AdminCreateUser)
	adminGrp.POST("/users/bulk", h.AdminBulkUsers)
	adminGrp.GET(routeUserByName, h.AdminGetUser)
	adminGrp.PUT(routeUserByName, h.AdminUpdateUser)
	adminGrp.DELETE(routeUserByName, h.AdminDeleteUser)
	adminGrp.POST(routeUserByName+"/password", h.AdminChangePassword)
	adminGrp.GET(routeUserByName+"/sessions", h.AdminGetUserSessions)
	adminGrp.GET("/data-deletion-requests", h.AdminListDeletionRequests)
	adminGrp.POST("/data-deletion-requests/:id/process", h.AdminProcessDeletionRequest)
	adminGrp.POST("/change-password", h.AdminChangeOwnPassword)
	adminGrp.GET("/audit-log", h.AdminGetAuditLog)
	adminGrp.GET("/audit-log/export", h.AdminExportAuditLog)
	adminGrp.GET("/logs", h.GetServerLogs)
	adminGrp.GET("/analytics/export", h.AdminExportAnalytics)

	// Configuration management routes
	adminGrp.GET("/config", h.AdminGetConfig)
	adminGrp.PUT("/config", h.AdminUpdateConfig)

	// Task management
	adminGrp.GET("/tasks", h.AdminListTasks)
	adminGrp.POST("/tasks/:id/run", h.AdminRunTask)
	adminGrp.POST("/tasks/:id/enable", h.AdminEnableTask)
	adminGrp.POST("/tasks/:id/disable", h.AdminDisableTask)
	adminGrp.POST("/tasks/:id/stop", h.AdminStopTask)

	// Admin playlist management — /bulk and /stats must be before :id wildcard
	adminGrp.GET(pathPlaylists, h.AdminListPlaylists)
	adminGrp.GET(pathPlaylists+pathStats, h.AdminPlaylistStats)
	adminGrp.POST(pathPlaylists+"/bulk", h.AdminBulkDeletePlaylists)
	adminGrp.DELETE(routePlaylistByID, h.AdminDeletePlaylist)

	// Admin media scan
	adminGrp.POST("/media/scan", h.ScanMedia)

	// Scanner (content moderation) routes
	adminGrp.POST("/scanner/scan", h.ScanContent)
	adminGrp.GET("/scanner/stats", h.GetScannerStats)
	adminGrp.GET(pathScannerQueue, h.GetReviewQueue)
	adminGrp.POST(pathScannerQueue, h.BatchReviewAction)
	adminGrp.DELETE(pathScannerQueue, h.ClearReviewQueue)
	adminGrp.POST("/scanner/approve/:id", h.ApproveContent)
	adminGrp.POST("/scanner/reject/:id", h.RejectContent)

	// Hugging Face visual classification (admin)
	adminGrp.GET("/classify/status", h.ClassifyStatus)
	adminGrp.GET("/classify/stats", h.ClassifyStats)
	adminGrp.POST("/classify/file", h.ClassifyFile)
	adminGrp.POST("/classify/directory", h.ClassifyDirectory)
	adminGrp.POST("/classify/run-task", h.ClassifyRunTask)
	adminGrp.POST("/classify/clear-tags", h.ClassifyClearTags)
	adminGrp.POST("/classify/all-pending", h.ClassifyAllPending)

	// Thumbnail admin routes
	adminGrp.POST("/thumbnails/generate", h.GenerateThumbnail)
	adminGrp.POST("/media/:id/thumbnail", h.UploadCustomThumbnail)
	adminGrp.POST("/thumbnails/cleanup", h.CleanupThumbnails)
	adminGrp.GET("/thumbnails/stats", h.GetThumbnailStats)

	// Auto-tag rules
	adminGrp.GET("/auto-tag-rules", h.ListAutoTagRules)
	adminGrp.POST("/auto-tag-rules", h.CreateAutoTagRule)
	adminGrp.PUT("/auto-tag-rules/:id", h.UpdateAutoTagRule)
	adminGrp.DELETE("/auto-tag-rules/:id", h.DeleteAutoTagRule)
	adminGrp.POST("/auto-tag-rules/apply", h.ApplyAutoTagRules)

	// Collections (admin management)
	adminGrp.POST("/collections", h.CreateCollection)
	adminGrp.PUT("/collections/:id", h.UpdateCollection)
	adminGrp.DELETE("/collections/:id", h.DeleteCollection)
	adminGrp.POST("/collections/:id/items", h.AddCollectionItems)
	adminGrp.DELETE("/collections/:id/items/:media_id", h.RemoveCollectionItem)

	// HLS admin routes
	adminGrp.GET("/hls/stats", h.GetHLSStats)
	adminGrp.GET("/hls/jobs", h.ListHLSJobs)
	adminGrp.DELETE("/hls/jobs/:id", h.DeleteHLSJob)
	adminGrp.GET("/hls/validate/:id", h.ValidateHLS)
	adminGrp.POST("/hls/clean/locks", h.CleanHLSStaleLocks)
	adminGrp.POST("/hls/clean/inactive", h.CleanHLSInactive)

	// Validator routes
	adminGrp.POST("/validator/validate", h.ValidateMedia)
	adminGrp.POST("/validator/fix", h.FixMedia)
	adminGrp.GET("/validator/stats", h.GetValidatorStats)

	// Database admin routes
	adminGrp.GET("/database/status", h.AdminGetDatabaseStatus)
	adminGrp.POST("/database/query", h.AdminExecuteQuery)

	// Backup v2 routes (using backup module)
	adminGrp.GET("/backups/v2", h.ListBackupsV2)
	adminGrp.POST("/backups/v2", h.CreateBackupV2)
	adminGrp.POST("/backups/v2/:id/restore", h.RestoreBackup)
	adminGrp.DELETE("/backups/v2/:id", h.DeleteBackup)

	// Auto-discovery routes
	adminGrp.POST("/discovery/scan", h.DiscoverMedia)
	adminGrp.GET("/discovery/suggestions", h.GetDiscoverySuggestions)
	adminGrp.POST("/discovery/apply", h.ApplyDiscoverySuggestion)
	// Wildcard for dismiss: gorilla /{path:.*} → gin /*path
	adminGrp.DELETE("/discovery/*path", h.DismissDiscoverySuggestion)

	// Suggestion stats (admin)
	adminGrp.GET("/suggestions/stats", h.GetSuggestionStats)

	// Security routes (admin)
	adminGrp.GET("/security/stats", h.GetSecurityStats)
	adminGrp.GET(pathSecurityWhitelist, h.GetWhitelist)
	adminGrp.POST(pathSecurityWhitelist, h.AddToWhitelist)
	adminGrp.DELETE(pathSecurityWhitelist, h.RemoveFromWhitelist)
	adminGrp.GET(pathSecurityBlacklist, h.GetBlacklist)
	adminGrp.POST(pathSecurityBlacklist, h.AddToBlacklist)
	adminGrp.DELETE(pathSecurityBlacklist, h.RemoveFromBlacklist)
	adminGrp.GET("/security/banned", h.GetBannedIPs)
	adminGrp.POST("/security/ban", h.BanIP)
	adminGrp.POST("/security/unban", h.UnbanIP)

	// Categorizer routes (admin)
	adminGrp.POST("/categorizer/file", h.CategorizeFile)
	adminGrp.POST("/categorizer/directory", h.CategorizeDirectory)
	adminGrp.GET("/categorizer/stats", h.GetCategoryStats)
	adminGrp.POST("/categorizer/set", h.SetMediaCategory)
	adminGrp.GET("/categorizer/by-category", h.GetByCategory)
	adminGrp.POST("/categorizer/clean", h.CleanStaleCategories)

	// Remote media routes (admin)
	adminGrp.GET("/remote/sources", h.GetRemoteSources)
	adminGrp.POST("/remote/sources", h.CreateRemoteSource)
	adminGrp.GET("/remote/stats", h.GetRemoteStats)
	adminGrp.GET("/remote/media", h.GetRemoteMedia)
	adminGrp.GET("/remote/sources/:source/media", h.GetRemoteSourceMedia)
	adminGrp.POST("/remote/sources/:source/sync", h.SyncRemoteSource)
	adminGrp.DELETE("/remote/sources/:source", h.DeleteRemoteSource)
	adminGrp.POST("/remote/cache", h.CacheRemoteMedia)
	adminGrp.POST("/remote/cache/clean", h.CleanRemoteCache)

	// Extractor routes (admin)
	adminGrp.GET("/extractor/items", h.ListExtractorItems)
	adminGrp.POST("/extractor/items", h.AddExtractorItem)
	adminGrp.DELETE("/extractor/items/:id", h.RemoveExtractorItem)
	adminGrp.GET("/extractor/stats", h.GetExtractorStats)

	// Crawler routes (admin)
	adminGrp.GET("/crawler/targets", h.ListCrawlerTargets)
	adminGrp.POST("/crawler/targets", h.AddCrawlerTarget)
	adminGrp.DELETE("/crawler/targets/:id", h.RemoveCrawlerTarget)
	adminGrp.POST("/crawler/targets/:id/crawl", h.CrawlTarget)
	adminGrp.GET("/crawler/discoveries", h.ListCrawlerDiscoveries)
	adminGrp.POST("/crawler/discoveries/:id/approve", h.ApproveCrawlerDiscovery)
	adminGrp.POST("/crawler/discoveries/:id/ignore", h.IgnoreCrawlerDiscovery)
	adminGrp.DELETE("/crawler/discoveries/:id", h.DeleteCrawlerDiscovery)
	adminGrp.GET("/crawler/stats", h.GetCrawlerStats)

	// Receiver (master-slave proxy) routes (admin)
	adminGrp.GET("/receiver/slaves", h.AdminReceiverListSlaves)
	adminGrp.GET("/receiver/stats", h.AdminReceiverGetStats)
	adminGrp.DELETE("/receiver/slaves/:id", h.AdminReceiverRemoveSlave)
	adminGrp.GET("/duplicates", h.AdminListDuplicates)
	adminGrp.POST("/duplicates/scan", h.AdminScanLocalDuplicates)
	adminGrp.POST("/duplicates/:id/resolve", h.AdminResolveDuplicate)

	// Downloader routes (admin)
	adminGrp.GET("/downloader/health", h.AdminDownloaderHealth)
	adminGrp.POST("/downloader/detect", h.AdminDownloaderDetect)
	adminGrp.POST("/downloader/download", h.AdminDownloaderDownload)
	adminGrp.POST("/downloader/cancel/:id", h.AdminDownloaderCancel)
	adminGrp.GET("/downloader/downloads", h.AdminDownloaderListDownloads)
	adminGrp.DELETE("/downloader/downloads/:filename", h.AdminDownloaderDeleteDownload)
	adminGrp.GET("/downloader/settings", h.AdminDownloaderSettings)
	adminGrp.GET("/downloader/importable", h.AdminDownloaderImportable)
	adminGrp.POST("/downloader/import", h.AdminDownloaderImport)

	// Claude admin assistant routes
	adminGrp.GET("/claude/config", h.AdminClaudeGetConfig)
	adminGrp.PUT("/claude/config", h.AdminClaudeUpdateConfig)
	adminGrp.POST("/claude/kill-switch", h.AdminClaudeKillSwitch)
	adminGrp.GET("/claude/auth-status", h.AdminClaudeAuthStatus)
	adminGrp.GET("/claude/conversations", h.AdminClaudeListConversations)
	adminGrp.GET("/claude/conversations/:id", h.AdminClaudeGetConversation)
	adminGrp.DELETE("/claude/conversations/:id", h.AdminClaudeDeleteConversation)
	adminGrp.POST("/claude/chat", h.AdminClaudeChat)

	// Admin media management routes
	adminGrp.GET(pathMedia, h.AdminListMedia)
	adminGrp.POST(pathMedia+"/bulk", h.AdminBulkMedia)
	adminGrp.PUT(pathMedia+"/:id", h.AdminUpdateMedia)
	adminGrp.DELETE(pathMedia+"/:id", h.AdminDeleteMedia)

	// Static file serving and template routes (using embedded filesystem)
	web.RegisterStaticRoutes(r)

	log.Info("Routes configured")
}
