// Package routes sets up API routes for the media server.
package routes

import (
	"bytes"
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-contrib/gzip"
	"github.com/gin-gonic/gin"

	"media-server-pro/api/handlers"
	"media-server-pro/internal/auth"
	"media-server-pro/internal/config"
	"media-server-pro/internal/logger"
	"media-server-pro/internal/security"
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
)

// sessionAuth loads session/user context from the session_id cookie and stores
// both on the gin context so downstream handlers and auth middleware can read them.
// Both admin and regular users share the session_id cookie.
func sessionAuth(authModule *auth.Module) gin.HandlerFunc {
	return func(c *gin.Context) {
		cookie, err := c.Cookie("session_id")
		if err == nil && cookie != "" {
			session, user, err := authModule.ValidateSession(c.Request.Context(), cookie)
			if err == nil {
				c.Set("session", session)
				c.Set("user", user)
			} else {
				// Clear stale/expired cookie so the browser stops resending it
				secure := c.Request.TLS != nil || c.GetHeader("X-Forwarded-Proto") == "https"
				c.SetCookie("session_id", "", -1, "/", "", secure, true)
			}
		}
		c.Next()
	}
}

// adminAuth requires an authenticated session with role=admin.
// Admin and regular users both use the session_id cookie; this checks the role set by sessionAuth.
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
			c.JSON(http.StatusUnauthorized, gin.H{"success": false, "error": "Unauthorized"})
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

// ginETags is a Gin middleware that adds content-based ETag support for GET/HEAD
// requests on /api/* routes. It buffers the response, computes an FNV-1a hash,
// and sets the ETag header. Clients that send a matching If-None-Match header
// receive a 304 Not Modified without the response body. Only applied to
// successful (2xx) responses.
func ginETags() gin.HandlerFunc {
	return func(c *gin.Context) {
		// Only apply ETag logic to GET/HEAD requests on API routes
		if (c.Request.Method != http.MethodGet && c.Request.Method != http.MethodHead) ||
			!strings.HasPrefix(c.Request.URL.Path, "/api/") {
			c.Next()
			return
		}

		// Use a buffered writer to capture the response body
		blw := &bodyLogWriter{body: bytes.NewBufferString(""), ResponseWriter: c.Writer}
		c.Writer = blw
		c.Next()

		// Only apply ETag to 2xx responses
		if blw.Status() < 200 || blw.Status() >= 300 {
			return
		}

		etag := `"` + hashFNV1a(blw.body.Bytes()) + `"`
		c.Header("ETag", etag)

		if match := c.GetHeader("If-None-Match"); match == etag {
			c.Status(http.StatusNotModified)
		}
	}
}

// bodyLogWriter wraps gin.ResponseWriter to capture the response body for ETag calculation.
type bodyLogWriter struct {
	gin.ResponseWriter
	body *bytes.Buffer
}

func (w *bodyLogWriter) Write(b []byte) (int, error) {
	w.body.Write(b)
	return w.ResponseWriter.Write(b)
}

// hashFNV1a computes an FNV-1a hash of the given bytes and returns it as a hex string.
func hashFNV1a(data []byte) string {
	h := uint32(2166136261)
	for _, b := range data {
		h ^= uint32(b)
		h *= 16777619
	}
	return fmt.Sprintf("%x", h)
}

// Setup configures all routes on the gin engine.
// securityModule.GinMiddleware() is defined in internal/security/security.go.
func Setup(r *gin.Engine, h *handlers.Handler, authModule *auth.Module, securityModule *security.Module, cfg *config.Manager, ageGate *middleware.AgeGate) {
	log := logger.New("routes")

	// Request ID for tracing
	r.Use(middleware.GinRequestID())

	// Security headers (CSP, HSTS, X-Frame-Options, etc.)
	secCfg := cfg.Get().Security
	r.Use(middleware.GinSecurityHeaders(secCfg.CSPPolicy, secCfg.HSTSMaxAge))

	// CORS — only applied when explicitly configured
	if secCfg.CORSEnabled && len(secCfg.CORSOrigins) > 0 {
		r.Use(middleware.GinCORS(
			secCfg.CORSOrigins,
			[]string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
			[]string{"Content-Type", "Authorization", "X-Requested-With"},
		))
	}

	// Apply compression middleware for all responses (except media streams).
	// gin-contrib/gzip skips paths that start with the excluded prefixes.
	r.Use(gzip.Gzip(gzip.DefaultCompression, gzip.WithExcludedPaths([]string{
		"/media",
		"/download",
		"/hls/",
		"/thumbnail",
		"/thumbnails/",
		"/remote/stream",
		"/receiver/stream",
		"/extractor/hls/",
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
	r.GET("/media", h.StreamMedia)
	r.GET("/download", h.DownloadMedia)

	// HLS streaming (direct, high-frequency — excluded from rate limiting and gzip)
	r.GET("/hls/:id/master.m3u8", h.ServeMasterPlaylist)
	r.GET("/hls/:id/:quality/playlist.m3u8", h.ServeVariantPlaylist)
	r.GET("/hls/:id/:quality/:segment", h.ServeSegment)

	// Thumbnail serving (direct)
	r.GET("/thumbnail", h.GetThumbnail)
	r.HEAD("/thumbnail", h.GetThumbnail)
	r.GET("/thumbnails/:filename", h.ServeThumbnailFile)
	r.HEAD("/thumbnails/:filename", h.ServeThumbnailFile)

	// /health — unauthenticated, returns 200 OK or 503 depending on critical module status.
	// Used by systemd healthcheck scripts, uptime monitors, and nginx upstream health checks.
	r.GET("/health", h.GetHealth)

	// /metrics is for Prometheus scraping — admin-protected, no frontend caller by design
	r.GET("/metrics", adminAuth(authModule), h.GetMetrics)

	// Remote streaming — frontend uses mediaApi.getRemoteStreamUrl()
	r.GET("/remote/stream", requireAuth(), h.StreamRemoteMedia)

	// Extractor HLS proxy (direct, high-frequency — excluded from gzip)
	r.GET("/extractor/hls/:id/master.m3u8", h.ExtractorHLSMaster)
	r.GET("/extractor/hls/:id/:quality/playlist.m3u8", h.ExtractorHLSVariant)
	r.GET("/extractor/hls/:id/:quality/:segment", h.ExtractorHLSSegment)

	// Receiver WebSocket — slave nodes connect here (authenticated via X-API-Key / api_key query)
	r.GET("/ws/receiver", h.ReceiverWebSocket)

	// -----------------------------------------------------------------------
	// API routes group (/api)
	// -----------------------------------------------------------------------
	api := r.Group("/api")

	// Media routes (mostly public)
	api.GET(pathMedia, h.ListMedia)
	api.GET(pathMedia+pathStats, h.GetMediaStats)
	api.GET(pathMedia+"/categories", h.GetCategories)
	api.GET(pathMedia+"/:id", h.GetMedia)

	// Playback
	api.GET("/playback", requireAuth(), h.GetPlaybackPosition)
	api.POST("/playback", requireAuth(), h.TrackPlayback)

	// HLS API routes
	api.GET("/hls/capabilities", h.GetHLSCapabilities)           // Check if HLS transcoding is available
	api.GET("/hls/check", requireAuth(), h.CheckHLSAvailability) // Check availability by path with auto-generate
	api.POST("/hls/generate", requireAuth(), h.GenerateHLS)      // Trigger HLS transcoding
	api.GET("/hls/status/:id", h.GetHLSStatus)

	// Auth routes (public)
	api.POST("/auth/login", h.Login)
	api.POST("/auth/logout", h.Logout)
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

	// Age gate — public, no auth required (must be accessible before user logs in)
	api.GET("/age-gate/status", ageGate.GinStatusHandler())
	api.POST("/age-verify", ageGate.GinVerifyHandler())

	// User preferences routes (protected)
	api.GET("/preferences", requireAuth(), h.GetPreferences)
	api.POST("/preferences", requireAuth(), h.UpdatePreferences)

	// User password change (protected)
	api.POST("/auth/change-password", requireAuth(), h.ChangePassword)

	// Self-service account deletion (protected) — user must confirm with their password
	api.POST("/auth/delete-account", requireAuth(), h.DeleteAccount)

	// Watch history routes (protected)
	api.GET(pathWatchHistory, requireAuth(), h.GetWatchHistory)
	api.DELETE(pathWatchHistory, requireAuth(), h.ClearWatchHistory)

	// Playlist routes (protected)
	api.GET(pathPlaylists, requireAuth(), h.ListPlaylists)
	api.POST(pathPlaylists, requireAuth(), h.CreatePlaylist)
	api.GET("/playlists/:id", requireAuth(), h.GetPlaylist)
	api.DELETE("/playlists/:id", requireAuth(), h.DeletePlaylist)
	api.PUT("/playlists/:id", requireAuth(), h.UpdatePlaylist)
	api.GET("/playlists/:id/export", requireAuth(), h.ExportPlaylist)
	api.POST("/playlists/:id/items", requireAuth(), h.AddPlaylistItem)
	api.DELETE("/playlists/:id/items", requireAuth(), h.RemovePlaylistItem)
	api.PUT("/playlists/:id/reorder", requireAuth(), h.ReorderPlaylistItems)
	api.DELETE("/playlists/:id/clear", requireAuth(), h.ClearPlaylist)
	api.POST("/playlists/:id/copy", requireAuth(), h.CopyPlaylist)

	// Analytics routes
	// POST /analytics/events — user auth (users submit their own events)
	// Aggregate analytics endpoints — admin-only (cross-user sensitive stats)
	api.GET("/analytics", adminAuth(authModule), h.GetAnalyticsSummary)
	api.GET("/analytics/daily", adminAuth(authModule), h.GetDailyStats)
	api.GET("/analytics/top", adminAuth(authModule), h.GetTopMedia)
	api.POST("/analytics/events", requireAuth(), h.SubmitEvent)
	api.GET("/analytics/events/stats", adminAuth(authModule), h.GetEventStats)
	api.GET("/analytics/events/by-type", adminAuth(authModule), h.GetEventsByType)
	api.GET("/analytics/events/by-media", adminAuth(authModule), h.GetEventsByMedia)
	api.GET("/analytics/events/counts", adminAuth(authModule), h.GetEventTypeCounts)

	// Thumbnail previews (public) — frontend uses mediaApi.getThumbnailPreviews()
	api.GET("/thumbnails/previews", h.GetThumbnailPreviews)

	// Suggestions routes (public)
	api.GET("/suggestions", h.GetSuggestions)
	api.GET("/suggestions/trending", h.GetTrendingSuggestions)
	api.GET("/suggestions/similar", h.GetSimilarMedia)

	// Protected suggestions routes
	api.GET("/suggestions/continue", requireAuth(), h.GetContinueWatching)
	api.GET("/suggestions/personalized", requireAuth(), h.GetPersonalizedSuggestions)
	api.POST("/ratings", requireAuth(), h.RecordRating)

	// Upload routes (protected)
	api.POST("/upload", requireAuth(), h.UploadMedia)
	api.GET("/upload/:id/progress", requireAuth(), h.GetUploadProgress)

	// Receiver slave API routes (authenticated via X-API-Key, not session cookie)
	api.POST("/receiver/register", h.ReceiverRegisterSlave)
	api.POST("/receiver/catalog", h.ReceiverPushCatalog)
	api.POST("/receiver/heartbeat", h.ReceiverHeartbeat)
	// Stream push — slave delivers file data in response to a WS stream_request
	api.POST("/receiver/stream-push/:token", h.ReceiverStreamPush)

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
	adminGrp.GET("/users/:username", h.AdminGetUser)
	adminGrp.PUT("/users/:username", h.AdminUpdateUser)
	adminGrp.DELETE("/users/:username", h.AdminDeleteUser)
	adminGrp.POST("/users/:username/password", h.AdminChangePassword)
	adminGrp.GET("/users/:username/sessions", h.AdminGetUserSessions)
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
	adminGrp.DELETE("/playlists/:id", h.AdminDeletePlaylist)

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

	// Thumbnail admin routes
	adminGrp.POST("/thumbnails/generate", h.GenerateThumbnail)
	adminGrp.GET("/thumbnails/stats", h.GetThumbnailStats)

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

	// Admin media management routes
	adminGrp.GET(pathMedia, h.AdminListMedia)
	adminGrp.POST(pathMedia+"/bulk", h.AdminBulkMedia)
	adminGrp.PUT(pathMedia+"/:id", h.AdminUpdateMedia)
	adminGrp.DELETE(pathMedia+"/:id", h.AdminDeleteMedia)

	// Static file serving and template routes (using embedded filesystem)
	web.RegisterStaticRoutes(r, cfg.Get().Directories.Thumbnails)

	log.Info("Routes configured")
}
