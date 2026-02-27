// Package routes sets up API routes for the media server.
package routes

import (
	"encoding/json"
	"net/http"

	"github.com/gorilla/mux"

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
	pathPlaylistByID      = "/playlists/{id}"
	pathUserByUsername    = "/users/{username}"
	pathSecurityWhitelist = "/security/whitelist"
	pathSecurityBlacklist = "/security/blacklist"
	pathScannerQueue      = "/scanner/queue"
	pathStats             = "/stats"

	headerContentType = "Content-Type"
	contentTypeJSON   = "application/json"
)

// makeSessionAuth creates middleware that loads session/user context from cookies.
// Both admin and regular users use the session_id cookie.
func makeSessionAuth(authModule *auth.Module) mux.MiddlewareFunc {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			cookie, err := r.Cookie("session_id")
			if err == nil && cookie.Value != "" {
				session, user, err := authModule.ValidateSession(r.Context(), cookie.Value)
				if err == nil {
					r = middleware.SetSession(r, session)
					r = middleware.SetUser(r, user)
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}

// makeAdminAuth creates middleware that requires an authenticated session with role=admin.
// Admin and regular users both use the session_id cookie; this checks the role in context.
func makeAdminAuth(_ *auth.Module) mux.MiddlewareFunc {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			user := middleware.GetUser(r)
			if user == nil || user.Role != models.RoleAdmin {
				w.Header().Set(headerContentType, contentTypeJSON)
				w.WriteHeader(http.StatusUnauthorized)
				_ = json.NewEncoder(w).Encode(map[string]interface{}{
					"success": false,
					"error":   "Unauthorized",
				})
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// makeRequireAuth creates middleware that requires an authenticated, non-expired session.
func makeRequireAuth() mux.MiddlewareFunc {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			session := middleware.GetSession(r)
			if session == nil || session.IsExpired() {
				w.Header().Set(headerContentType, contentTypeJSON)
				w.WriteHeader(http.StatusUnauthorized)
				_ = json.NewEncoder(w).Encode(map[string]interface{}{
					"success": false,
					"error":   "Unauthorized",
				})
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// Setup configures all routes on the router
func Setup(router *mux.Router, h *handlers.Handler, authModule *auth.Module, securityModule *security.Module, cfg *config.Manager, ageGate *middleware.AgeGate) {
	log := logger.New("routes")

	// Apply compression middleware for all responses (except media streams)
	router.Use(middleware.Compression)

	// Apply ETag caching for GET/HEAD /api/* JSON responses
	router.Use(middleware.ETags)

	// Apply security middleware (rate limiting, IP filtering)
	router.Use(securityModule.Middleware)

	// Apply session middleware to all routes
	router.Use(makeSessionAuth(authModule))

	// API routes group
	api := router.PathPrefix("/api").Subrouter()

	// Media routes (mostly public)
	api.HandleFunc(pathMedia, h.ListMedia).Methods("GET")
	api.HandleFunc(pathMedia+pathStats, h.GetMediaStats).Methods("GET")
	api.HandleFunc(pathMedia+"/categories", h.GetCategories).Methods("GET")
	api.HandleFunc(pathMedia+"/{id}", h.GetMedia).Methods("GET")

	// ROUTE PATTERN EXPLANATION:
	// /api/media  - API endpoint returning JSON list of media items (ListMedia handler)
	// /media      - Streaming endpoint serving actual media files (StreamMedia handler)
	//
	// These use different base paths by design:
	// - /api/* routes are for JSON API responses (via 'api' subrouter)
	// - Direct /* routes are for file serving/streaming (via 'router')
	//
	// This pattern separates API operations from file serving, allowing different middleware:
	// - API routes can require authentication, apply rate limits, return JSON errors
	// - Streaming routes need range request support, chunked transfer, binary responses
	//
	// Frontend usage:
	// - fetch('/api/media') → get list of available media
	// - <video src="/media?path=..."> → stream the actual file
	router.HandleFunc(pathMedia, h.StreamMedia).Methods("GET")
	router.HandleFunc("/download", h.DownloadMedia).Methods("GET")
	api.Handle("/playback", makeRequireAuth()(http.HandlerFunc(h.GetPlaybackPosition))).Methods("GET")
	api.HandleFunc("/playback", h.TrackPlayback).Methods("POST")

	// HLS routes
	api.HandleFunc("/hls/capabilities", h.GetHLSCapabilities).Methods("GET") // Check if HLS transcoding is available
	api.HandleFunc("/hls/check", h.CheckHLSAvailability).Methods("GET")      // Check availability by path with auto-generate
	api.HandleFunc("/hls/generate", h.GenerateHLS).Methods("POST")
	api.HandleFunc("/hls/status/{id}", h.GetHLSStatus).Methods("GET")
	router.HandleFunc("/hls/{id}/master.m3u8", h.ServeMasterPlaylist).Methods("GET")
	router.HandleFunc("/hls/{id}/{quality}/playlist.m3u8", h.ServeVariantPlaylist).Methods("GET")
	router.HandleFunc("/hls/{id}/{quality}/{segment}", h.ServeSegment).Methods("GET")

	// Auth routes (public)
	api.HandleFunc("/auth/login", h.Login).Methods("POST")
	api.HandleFunc("/auth/logout", h.Logout).Methods("POST")
	api.HandleFunc("/auth/register", h.Register).Methods("POST")
	api.HandleFunc("/auth/session", h.CheckSession).Methods("GET")

	// Permissions route (public - returns different info based on auth status)
	api.HandleFunc("/permissions", h.GetPermissions).Methods("GET")

	// System routes
	// /health — unauthenticated, returns 200 OK or 503 depending on critical module status.
	// Used by systemd healthcheck scripts, uptime monitors, and nginx upstream health checks.
	router.HandleFunc("/health", h.GetHealth).Methods("GET")
	// /metrics is for Prometheus scraping — admin-protected, no frontend caller by design
	router.Handle("/metrics", makeAdminAuth(authModule)(http.HandlerFunc(h.GetMetrics))).Methods("GET")
	// Storage usage requires authentication to prevent resource exhaustion attacks
	// and information leakage to anonymous users
	api.Handle("/storage-usage", makeRequireAuth()(http.HandlerFunc(h.GetStorageUsage))).Methods("GET")
	// Server settings returns only public feature flags and UI config; no auth required.
	// The frontend fetches this on initial load before any login occurs.
	api.HandleFunc("/server-settings", h.GetServerSettings).Methods("GET")

	// Age gate — public, no auth required (must be accessible before user logs in)
	api.HandleFunc("/age-gate/status", ageGate.StatusHandler).Methods("GET")
	api.HandleFunc("/age-verify", ageGate.VerifyHandler).Methods("POST")

	// User preferences routes (protected)
	api.Handle("/preferences", makeRequireAuth()(http.HandlerFunc(h.GetPreferences))).Methods("GET")
	api.Handle("/preferences", makeRequireAuth()(http.HandlerFunc(h.UpdatePreferences))).Methods("POST")

	// User password change (protected)
	api.Handle("/auth/change-password", makeRequireAuth()(http.HandlerFunc(h.ChangePassword))).Methods("POST")

	// Self-service account deletion (protected) — user must confirm with their password
	api.Handle("/auth/delete-account", makeRequireAuth()(http.HandlerFunc(h.DeleteAccount))).Methods("POST")

	// Watch history routes (protected)
	api.Handle(pathWatchHistory, makeRequireAuth()(http.HandlerFunc(h.GetWatchHistory))).Methods("GET")
	api.Handle(pathWatchHistory, makeRequireAuth()(http.HandlerFunc(h.ClearWatchHistory))).Methods("DELETE")

	// Playlist routes (protected)
	api.Handle(pathPlaylists, makeRequireAuth()(http.HandlerFunc(h.ListPlaylists))).Methods("GET")
	api.Handle(pathPlaylists, makeRequireAuth()(http.HandlerFunc(h.CreatePlaylist))).Methods("POST")
	api.Handle(pathPlaylistByID, makeRequireAuth()(http.HandlerFunc(h.GetPlaylist))).Methods("GET")
	api.Handle(pathPlaylistByID, makeRequireAuth()(http.HandlerFunc(h.DeletePlaylist))).Methods("DELETE")
	api.Handle(pathPlaylistByID, makeRequireAuth()(http.HandlerFunc(h.UpdatePlaylist))).Methods("PUT")
	api.Handle("/playlists/{id}/export", makeRequireAuth()(http.HandlerFunc(h.ExportPlaylist))).Methods("GET")
	api.Handle("/playlists/{id}/items", makeRequireAuth()(http.HandlerFunc(h.AddPlaylistItem))).Methods("POST")
	api.Handle("/playlists/{id}/items", makeRequireAuth()(http.HandlerFunc(h.RemovePlaylistItem))).Methods("DELETE")

	// Analytics routes
	// POST /analytics/events — user auth (users submit their own events)
	// Aggregate analytics endpoints — admin-only (cross-user sensitive stats)
	api.Handle("/analytics", makeAdminAuth(authModule)(http.HandlerFunc(h.GetAnalyticsSummary))).Methods("GET") // ST-01: was makeRequireAuth — analytics summary is server-wide data
	api.Handle("/analytics/daily", makeAdminAuth(authModule)(http.HandlerFunc(h.GetDailyStats))).Methods("GET")
	api.Handle("/analytics/top", makeAdminAuth(authModule)(http.HandlerFunc(h.GetTopMedia))).Methods("GET")

	api.Handle("/analytics/events", makeRequireAuth()(http.HandlerFunc(h.SubmitEvent))).Methods("POST")
	api.Handle("/analytics/events/stats", makeAdminAuth(authModule)(http.HandlerFunc(h.GetEventStats))).Methods("GET")
	api.Handle("/analytics/events/by-type", makeAdminAuth(authModule)(http.HandlerFunc(h.GetEventsByType))).Methods("GET")
	api.Handle("/analytics/events/by-media", makeAdminAuth(authModule)(http.HandlerFunc(h.GetEventsByMedia))).Methods("GET")
	api.Handle("/analytics/events/counts", makeAdminAuth(authModule)(http.HandlerFunc(h.GetEventTypeCounts))).Methods("GET")

	// Thumbnail routes (public)
	router.HandleFunc("/thumbnail", h.GetThumbnail).Methods("GET", "HEAD")
	router.HandleFunc("/thumbnails/{filename}", h.ServeThumbnailFile).Methods("GET", "HEAD")
	api.HandleFunc("/thumbnails/previews", h.GetThumbnailPreviews).Methods("GET")

	// Suggestions routes (public)
	api.HandleFunc("/suggestions", h.GetSuggestions).Methods("GET")
	api.HandleFunc("/suggestions/trending", h.GetTrendingSuggestions).Methods("GET")
	api.HandleFunc("/suggestions/similar", h.GetSimilarMedia).Methods("GET")

	// Protected suggestions routes
	api.Handle("/suggestions/continue", makeRequireAuth()(http.HandlerFunc(h.GetContinueWatching))).Methods("GET")
	api.Handle("/suggestions/personalized", makeRequireAuth()(http.HandlerFunc(h.GetPersonalizedSuggestions))).Methods("GET")
	api.Handle("/ratings", makeRequireAuth()(http.HandlerFunc(h.RecordRating))).Methods("POST")

	// Upload routes (protected)
	api.Handle("/upload", makeRequireAuth()(http.HandlerFunc(h.UploadMedia))).Methods("POST")
	api.Handle("/upload/{id}/progress", makeRequireAuth()(http.HandlerFunc(h.GetUploadProgress))).Methods("GET")

	// Admin routes (under /api/admin to match frontend API helper)
	adminRouter := api.PathPrefix("/admin").Subrouter()

	// Protected admin routes - apply middleware to each route
	adminAuth := makeAdminAuth(authModule)

	adminRouter.Handle(pathStats, adminAuth(http.HandlerFunc(h.AdminGetStats))).Methods("GET")
	adminRouter.Handle("/system", adminAuth(http.HandlerFunc(h.AdminGetSystemInfo))).Methods("GET")
	adminRouter.Handle("/cache/clear", adminAuth(http.HandlerFunc(h.ClearMediaCache))).Methods("POST")

	// Update management routes
	adminRouter.Handle("/update/check", adminAuth(http.HandlerFunc(h.CheckForUpdates))).Methods("GET")
	adminRouter.Handle("/update/status", adminAuth(http.HandlerFunc(h.GetUpdateStatus))).Methods("GET")
	adminRouter.Handle("/update/apply", adminAuth(http.HandlerFunc(h.ApplyUpdate))).Methods("POST")
	adminRouter.Handle("/update/source/check", adminAuth(http.HandlerFunc(h.CheckForSourceUpdates))).Methods("GET")
	adminRouter.Handle("/update/source/apply", adminAuth(http.HandlerFunc(h.ApplySourceUpdate))).Methods("POST")
	adminRouter.Handle("/update/source/progress", adminAuth(http.HandlerFunc(h.GetSourceUpdateProgress))).Methods("GET")
	adminRouter.Handle("/update/config", adminAuth(http.HandlerFunc(h.GetUpdateConfig))).Methods("GET")
	adminRouter.Handle("/update/config", adminAuth(http.HandlerFunc(h.SetUpdateConfig))).Methods("PUT")

	// Server management routes
	adminRouter.Handle("/server/restart", adminAuth(http.HandlerFunc(h.RestartServer))).Methods("POST")
	adminRouter.Handle("/server/shutdown", adminAuth(http.HandlerFunc(h.ShutdownServer))).Methods("POST")

	adminRouter.Handle("/users", adminAuth(http.HandlerFunc(h.AdminListUsers))).Methods("GET")
	adminRouter.Handle("/users", adminAuth(http.HandlerFunc(h.AdminCreateUser))).Methods("POST")
	adminRouter.Handle("/users/bulk", adminAuth(http.HandlerFunc(h.AdminBulkUsers))).Methods("POST")
	adminRouter.Handle(pathUserByUsername, adminAuth(http.HandlerFunc(h.AdminGetUser))).Methods("GET")
	adminRouter.Handle(pathUserByUsername, adminAuth(http.HandlerFunc(h.AdminUpdateUser))).Methods("PUT")
	adminRouter.Handle(pathUserByUsername, adminAuth(http.HandlerFunc(h.AdminDeleteUser))).Methods("DELETE")
	adminRouter.Handle("/users/{username}/password", adminAuth(http.HandlerFunc(h.AdminChangePassword))).Methods("POST")
	adminRouter.Handle("/change-password", adminAuth(http.HandlerFunc(h.AdminChangeOwnPassword))).Methods("POST")
	adminRouter.Handle("/audit-log", adminAuth(http.HandlerFunc(h.AdminGetAuditLog))).Methods("GET")
	adminRouter.Handle("/audit-log/export", adminAuth(http.HandlerFunc(h.AdminExportAuditLog))).Methods("GET")
	adminRouter.Handle("/logs", adminAuth(http.HandlerFunc(h.GetServerLogs))).Methods("GET")
	adminRouter.Handle("/analytics/export", adminAuth(http.HandlerFunc(h.AdminExportAnalytics))).Methods("GET")

	// Configuration management routes
	adminRouter.Handle("/config", adminAuth(http.HandlerFunc(h.AdminGetConfig))).Methods("GET")
	adminRouter.Handle("/config", adminAuth(http.HandlerFunc(h.AdminUpdateConfig))).Methods("PUT")

	adminRouter.Handle("/tasks", adminAuth(http.HandlerFunc(h.AdminListTasks))).Methods("GET")
	adminRouter.Handle("/tasks/{id}/run", adminAuth(http.HandlerFunc(h.AdminRunTask))).Methods("POST")
	adminRouter.Handle("/tasks/{id}/enable", adminAuth(http.HandlerFunc(h.AdminEnableTask))).Methods("POST")
	adminRouter.Handle("/tasks/{id}/disable", adminAuth(http.HandlerFunc(h.AdminDisableTask))).Methods("POST")
	adminRouter.Handle("/tasks/{id}/stop", adminAuth(http.HandlerFunc(h.AdminStopTask))).Methods("POST")

	// Admin playlist management — /bulk must be before {id} wildcard
	adminRouter.Handle(pathPlaylists, adminAuth(http.HandlerFunc(h.AdminListPlaylists))).Methods("GET")
	adminRouter.Handle(pathPlaylists+pathStats, adminAuth(http.HandlerFunc(h.AdminPlaylistStats))).Methods("GET")
	adminRouter.Handle(pathPlaylists+"/bulk", adminAuth(http.HandlerFunc(h.AdminBulkDeletePlaylists))).Methods("POST")
	adminRouter.Handle(pathPlaylistByID, adminAuth(http.HandlerFunc(h.AdminDeletePlaylist))).Methods("DELETE")

	adminRouter.Handle("/media/scan", adminAuth(http.HandlerFunc(h.ScanMedia))).Methods("POST")

	// Scanner (content moderation) routes
	adminRouter.Handle("/scanner/scan", adminAuth(http.HandlerFunc(h.ScanContent))).Methods("POST")
	adminRouter.Handle("/scanner/stats", adminAuth(http.HandlerFunc(h.GetScannerStats))).Methods("GET")
	adminRouter.Handle(pathScannerQueue, adminAuth(http.HandlerFunc(h.GetReviewQueue))).Methods("GET")
	adminRouter.Handle(pathScannerQueue, adminAuth(http.HandlerFunc(h.BatchReviewAction))).Methods("POST")
	adminRouter.Handle(pathScannerQueue, adminAuth(http.HandlerFunc(h.ClearReviewQueue))).Methods("DELETE")
	adminRouter.Handle("/scanner/approve/{path:.*}", adminAuth(http.HandlerFunc(h.ApproveContent))).Methods("POST")
	adminRouter.Handle("/scanner/reject/{path:.*}", adminAuth(http.HandlerFunc(h.RejectContent))).Methods("POST")

	// Thumbnail admin routes
	adminRouter.Handle("/thumbnails/generate", adminAuth(http.HandlerFunc(h.GenerateThumbnail))).Methods("POST")
	adminRouter.Handle("/thumbnails/stats", adminAuth(http.HandlerFunc(h.GetThumbnailStats))).Methods("GET")

	// HLS admin routes
	adminRouter.Handle("/hls/stats", adminAuth(http.HandlerFunc(h.GetHLSStats))).Methods("GET")
	adminRouter.Handle("/hls/jobs", adminAuth(http.HandlerFunc(h.ListHLSJobs))).Methods("GET")
	adminRouter.Handle("/hls/jobs/{id}", adminAuth(http.HandlerFunc(h.DeleteHLSJob))).Methods("DELETE")
	adminRouter.Handle("/hls/validate/{id}", adminAuth(http.HandlerFunc(h.ValidateHLS))).Methods("GET")
	adminRouter.Handle("/hls/clean/locks", adminAuth(http.HandlerFunc(h.CleanHLSStaleLocks))).Methods("POST")
	adminRouter.Handle("/hls/clean/inactive", adminAuth(http.HandlerFunc(h.CleanHLSInactive))).Methods("POST")

	// Validator routes
	adminRouter.Handle("/validator/validate", adminAuth(http.HandlerFunc(h.ValidateMedia))).Methods("POST")
	adminRouter.Handle("/validator/fix", adminAuth(http.HandlerFunc(h.FixMedia))).Methods("POST")
	adminRouter.Handle("/validator/stats", adminAuth(http.HandlerFunc(h.GetValidatorStats))).Methods("GET")

	// Database admin routes
	adminRouter.Handle("/database/status", adminAuth(http.HandlerFunc(h.AdminGetDatabaseStatus))).Methods("GET")
	adminRouter.Handle("/database/query", adminAuth(http.HandlerFunc(h.AdminExecuteQuery))).Methods("POST")

	// Backup v2 routes (using backup module)
	adminRouter.Handle("/backups/v2", adminAuth(http.HandlerFunc(h.ListBackupsV2))).Methods("GET")
	adminRouter.Handle("/backups/v2", adminAuth(http.HandlerFunc(h.CreateBackupV2))).Methods("POST")
	adminRouter.Handle("/backups/v2/{id}/restore", adminAuth(http.HandlerFunc(h.RestoreBackup))).Methods("POST")
	adminRouter.Handle("/backups/v2/{id}", adminAuth(http.HandlerFunc(h.DeleteBackup))).Methods("DELETE")

	// Auto-discovery routes
	adminRouter.Handle("/discovery/scan", adminAuth(http.HandlerFunc(h.DiscoverMedia))).Methods("POST")
	adminRouter.Handle("/discovery/suggestions", adminAuth(http.HandlerFunc(h.GetDiscoverySuggestions))).Methods("GET")
	adminRouter.Handle("/discovery/apply", adminAuth(http.HandlerFunc(h.ApplyDiscoverySuggestion))).Methods("POST")
	adminRouter.Handle("/discovery/{path:.*}", adminAuth(http.HandlerFunc(h.DismissDiscoverySuggestion))).Methods("DELETE")

	// Suggestion stats (admin)
	adminRouter.Handle("/suggestions/stats", adminAuth(http.HandlerFunc(h.GetSuggestionStats))).Methods("GET")

	// Security routes (admin)
	adminRouter.Handle("/security/stats", adminAuth(http.HandlerFunc(h.GetSecurityStats))).Methods("GET")
	adminRouter.Handle(pathSecurityWhitelist, adminAuth(http.HandlerFunc(h.GetWhitelist))).Methods("GET")
	adminRouter.Handle(pathSecurityWhitelist, adminAuth(http.HandlerFunc(h.AddToWhitelist))).Methods("POST")
	adminRouter.Handle(pathSecurityWhitelist, adminAuth(http.HandlerFunc(h.RemoveFromWhitelist))).Methods("DELETE")
	adminRouter.Handle(pathSecurityBlacklist, adminAuth(http.HandlerFunc(h.GetBlacklist))).Methods("GET")
	adminRouter.Handle(pathSecurityBlacklist, adminAuth(http.HandlerFunc(h.AddToBlacklist))).Methods("POST")
	adminRouter.Handle(pathSecurityBlacklist, adminAuth(http.HandlerFunc(h.RemoveFromBlacklist))).Methods("DELETE")
	adminRouter.Handle("/security/banned", adminAuth(http.HandlerFunc(h.GetBannedIPs))).Methods("GET")
	adminRouter.Handle("/security/ban", adminAuth(http.HandlerFunc(h.BanIP))).Methods("POST")
	adminRouter.Handle("/security/unban", adminAuth(http.HandlerFunc(h.UnbanIP))).Methods("POST")

	// Categorizer routes (admin)
	adminRouter.Handle("/categorizer/file", adminAuth(http.HandlerFunc(h.CategorizeFile))).Methods("POST")
	adminRouter.Handle("/categorizer/directory", adminAuth(http.HandlerFunc(h.CategorizeDirectory))).Methods("POST")
	adminRouter.Handle("/categorizer/stats", adminAuth(http.HandlerFunc(h.GetCategoryStats))).Methods("GET")
	adminRouter.Handle("/categorizer/set", adminAuth(http.HandlerFunc(h.SetMediaCategory))).Methods("POST")
	adminRouter.Handle("/categorizer/by-category", adminAuth(http.HandlerFunc(h.GetByCategory))).Methods("GET")
	adminRouter.Handle("/categorizer/clean", adminAuth(http.HandlerFunc(h.CleanStaleCategories))).Methods("POST")

	// Remote media routes (admin)
	adminRouter.Handle("/remote/sources", adminAuth(http.HandlerFunc(h.GetRemoteSources))).Methods("GET")
	adminRouter.Handle("/remote/sources", adminAuth(http.HandlerFunc(h.CreateRemoteSource))).Methods("POST")
	adminRouter.Handle("/remote/stats", adminAuth(http.HandlerFunc(h.GetRemoteStats))).Methods("GET")
	adminRouter.Handle("/remote/media", adminAuth(http.HandlerFunc(h.GetRemoteMedia))).Methods("GET")
	adminRouter.Handle("/remote/sources/{source}/media", adminAuth(http.HandlerFunc(h.GetRemoteSourceMedia))).Methods("GET")
	adminRouter.Handle("/remote/sources/{source}/sync", adminAuth(http.HandlerFunc(h.SyncRemoteSource))).Methods("POST")
	adminRouter.Handle("/remote/sources/{source}", adminAuth(http.HandlerFunc(h.DeleteRemoteSource))).Methods("DELETE")
	adminRouter.Handle("/remote/cache", adminAuth(http.HandlerFunc(h.CacheRemoteMedia))).Methods("POST")
	adminRouter.Handle("/remote/cache/clean", adminAuth(http.HandlerFunc(h.CleanRemoteCache))).Methods("POST")

	// Admin media management routes — /bulk must be registered before the {path:.*} wildcard
	adminRouter.Handle(pathMedia, adminAuth(http.HandlerFunc(h.AdminListMedia))).Methods("GET")
	adminRouter.Handle(pathMedia+"/bulk", adminAuth(http.HandlerFunc(h.AdminBulkMedia))).Methods("POST")
	adminRouter.Handle(pathMedia+"/{path:.*}", adminAuth(http.HandlerFunc(h.AdminUpdateMedia))).Methods("PUT")
	adminRouter.Handle(pathMedia+"/{path:.*}", adminAuth(http.HandlerFunc(h.AdminDeleteMedia))).Methods("DELETE")

	// Remote streaming (public, with optional auth)
	router.HandleFunc("/remote/stream", h.StreamRemoteMedia).Methods("GET")

	// Static file serving and template routes (using embedded filesystem)
	web.RegisterStaticRoutes(router, cfg.Get().Directories.Thumbnails)

	log.Info("Routes configured")
}
