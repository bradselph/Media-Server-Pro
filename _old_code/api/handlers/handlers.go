// Package handlers provides HTTP request handlers for the API.
package handlers

import (
	"bufio"
	"bytes"
	"context"
	cryptorand "crypto/rand"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/mux"

	"media-server-pro/internal/admin"
	"media-server-pro/internal/analytics"
	"media-server-pro/internal/auth"
	"media-server-pro/internal/autodiscovery"
	"media-server-pro/internal/backup"
	"media-server-pro/internal/categorizer"
	"media-server-pro/internal/config"
	"media-server-pro/internal/database"
	"media-server-pro/internal/hls"
	"media-server-pro/internal/logger"
	"media-server-pro/internal/media"
	"media-server-pro/internal/playlist"
	"media-server-pro/internal/remote"
	"media-server-pro/internal/scanner"
	"media-server-pro/internal/security"
	"media-server-pro/internal/streaming"
	"media-server-pro/internal/suggestions"
	"media-server-pro/internal/tasks"
	"media-server-pro/internal/thumbnails"
	"media-server-pro/internal/updater"
	"media-server-pro/internal/upload"
	"media-server-pro/internal/validator"
	"media-server-pro/pkg/helpers"
	"media-server-pro/pkg/middleware"
	"media-server-pro/pkg/models"
)

// Error message constants to avoid duplication (go:S1192).
const (
	errPathRequired      = "Path required"
	errFileNotFound      = "File not found"
	errInvalidRequest    = "Invalid request"
	errNotAuthenticated  = "Not authenticated"
	errUserNotFound      = "User not found"
	errPathParamRequired = "path parameter required"
)

// HTTP header name constants to avoid duplication (go:S1192).
const (
	headerContentType        = "Content-Type"
	headerContentDisposition = "Content-Disposition"
)

// Handler holds dependencies for HTTP handlers
type Handler struct {
	log           *logger.Logger
	version       string
	buildDate     string
	media         *media.Module
	streaming     *streaming.Module
	hls           *hls.Module
	auth          *auth.Module
	analytics     *analytics.Module
	playlist      *playlist.Module
	admin         *admin.Module
	database      *database.Module
	tasks         *tasks.Module
	upload        *upload.Module
	scanner       *scanner.Module
	thumbnails    *thumbnails.Module
	validator     *validator.Module
	backup        *backup.Module
	autodiscovery *autodiscovery.Module
	suggestions   *suggestions.Module
	security      *security.Module
	categorizer   *categorizer.Module
	updater       *updater.Module
	remote        *remote.Module
	config        *config.Manager
}

// HandlerDeps holds all module dependencies needed to create a Handler.
// This avoids passing each dependency as a separate parameter (go:S107).
type HandlerDeps struct {
	Version       string
	BuildDate     string
	Config        *config.Manager
	Media         *media.Module
	Streaming     *streaming.Module
	HLS           *hls.Module
	Auth          *auth.Module
	Analytics     *analytics.Module
	Playlist      *playlist.Module
	Admin         *admin.Module
	Database      *database.Module
	Tasks         *tasks.Module
	Upload        *upload.Module
	Scanner       *scanner.Module
	Thumbnails    *thumbnails.Module
	Validator     *validator.Module
	Backup        *backup.Module
	Autodiscovery *autodiscovery.Module
	Suggestions   *suggestions.Module
	Security      *security.Module
	Categorizer   *categorizer.Module
	Updater       *updater.Module
	Remote        *remote.Module
}

// NewHandler creates a new handler with dependencies.
// Panics if critical modules (Media, Auth, Streaming) are nil.
func NewHandler(deps HandlerDeps) *Handler {
	if deps.Media == nil || deps.Auth == nil || deps.Streaming == nil {
		panic("NewHandler: critical module dependency is nil (Media, Auth, or Streaming)")
	}

	return &Handler{
		log:           logger.New("handlers"),
		version:       deps.Version,
		buildDate:     deps.BuildDate,
		media:         deps.Media,
		streaming:     deps.Streaming,
		hls:           deps.HLS,
		auth:          deps.Auth,
		analytics:     deps.Analytics,
		playlist:      deps.Playlist,
		admin:         deps.Admin,
		database:      deps.Database,
		tasks:         deps.Tasks,
		upload:        deps.Upload,
		scanner:       deps.Scanner,
		thumbnails:    deps.Thumbnails,
		validator:     deps.Validator,
		backup:        deps.Backup,
		autodiscovery: deps.Autodiscovery,
		suggestions:   deps.Suggestions,
		security:      deps.Security,
		categorizer:   deps.Categorizer,
		updater:       deps.Updater,
		remote:        deps.Remote,
		config:        deps.Config,
	}
}

// Helper functions

// handlerLog is a package-level logger for helper functions that don't have access to handler instance
var handlerLog = logger.New("handlers")

// writeJSON encodes data to JSON and writes it to the response writer.
// Uses buffered encoding to ensure atomic writes - either the full response succeeds or an error is returned.
func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	// Buffer the JSON encoding to avoid partial writes
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(data); err != nil {
		handlerLog.Error("JSON encode failed: %v", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	// Encoding succeeded - write atomically to response
	w.Header().Set(headerContentType, "application/json")
	w.WriteHeader(status)
	if _, err := w.Write(buf.Bytes()); err != nil {
		handlerLog.Error("Failed to write JSON response: %v", err)
	}
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, models.APIResponse{
		Success: false,
		Error:   message,
	})
}

// isClientDisconnect returns true for network errors that indicate the client
// closed the connection (broken pipe, connection reset, i/o timeout on write).
// These are not server errors and should not be logged at ERROR level.
func isClientDisconnect(err error) bool {
	if err == nil {
		return false
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true
	}
	msg := err.Error()
	return strings.Contains(msg, "broken pipe") ||
		strings.Contains(msg, "connection reset by peer") ||
		strings.Contains(msg, "write: connection reset") ||
		strings.Contains(msg, "i/o timeout")
}

// isSecureRequest detects HTTPS connections, including behind TLS-terminating reverse proxies
// (nginx, Cloudflare, etc.). Checks X-Forwarded-Proto (standard) and CF-Visitor (Cloudflare).
func isSecureRequest(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	if r.Header.Get("X-Forwarded-Proto") == "https" {
		return true
	}
	// Cloudflare sets CF-Visitor: {"scheme":"https"} even when using Flexible SSL
	// where the origin connection is plain HTTP.
	if strings.Contains(r.Header.Get("Cf-Visitor"), `"scheme":"https"`) {
		return true
	}
	return false
}

func writeSuccess(w http.ResponseWriter, data interface{}) {
	writeJSON(w, http.StatusOK, models.APIResponse{
		Success: true,
		Data:    data,
	})
}

// generateRandomString creates a random string of the given length
func generateRandomString(length int) string {
	const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	b := make([]byte, length)
	for i := range b {
		b[i] = charset[randIntn(len(charset))]
	}
	return string(b)
}

// randIntn returns a random int in [0, n) using crypto/rand.
// Panics if crypto/rand fails, as this indicates a serious system-level problem
// and should not be silently handled with weak randomness.
func randIntn(n int) int {
	if n <= 0 {
		return 0
	}
	maxValue := int64(n)
	nBig, err := cryptorand.Int(cryptorand.Reader, big.NewInt(maxValue))
	if err != nil {
		// crypto/rand failure is a critical system issue - do not fall back to weak randomness
		panic(fmt.Sprintf("crypto/rand failed: %v - system entropy source unavailable", err))
	}
	return int(nBig.Int64())
}

// decodeJSON is a helper that decodes JSON from request body and logs decode errors
func decodeJSON(r *http.Request, v interface{}) error {
	err := json.NewDecoder(r.Body).Decode(v)
	if err != nil {
		handlerLog.Debug("JSON decode error from %s %s: %v", r.Method, r.URL.Path, err)
	}
	return err
}

// Media Handlers

// ListMedia returns all media items
// Response: { success: true, data: [...media items...] }
func (h *Handler) ListMedia(w http.ResponseWriter, r *http.Request) {
	// Use private cache since mature-content filtering is user-specific
	w.Header().Set("Cache-Control", "private, max-age=60")

	sortBy := r.URL.Query().Get("sort")
	// Map frontend sort values to backend field names
	if sortBy == "date" {
		sortBy = "date_modified"
	}

	var limit, offset int
	if l, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && l > 0 {
		if l > 500 {
			l = 500
		}
		limit = l
	}
	if o, err := strconv.Atoi(r.URL.Query().Get("offset")); err == nil && o > 0 {
		if o > 50000 {
			o = 50000
		}
		offset = o
	}

	// Parse tags filter (comma-separated)
	var tags []string
	if t := r.URL.Query().Get("tags"); t != "" {
		tags = strings.Split(t, ",")
	}

	// Parse is_mature filter
	var isMature *bool
	if im := r.URL.Query().Get("is_mature"); im != "" {
		v := im == "true" || im == "1"
		isMature = &v
	}

	// First, get all matching items WITHOUT pagination to calculate total count
	filterNoPagination := media.Filter{
		Type:     models.MediaType(r.URL.Query().Get("type")),
		Category: r.URL.Query().Get("category"),
		Search:   r.URL.Query().Get("search"),
		Tags:     tags,
		IsMature: isMature,
		SortBy:   sortBy,
		SortDesc: r.URL.Query().Get("sort_order") == "desc",
		// No Limit/Offset for total count
	}

	allItems := h.media.ListMedia(filterNoPagination)

	// Mature content filtering rules:
	// - Guest (not logged in): include mature items; frontend renders them blurred
	// - Logged-in, ShowMature=false (default for new users): hide mature items
	// - Logged-in, ShowMature=true: include mature items normally
	session := middleware.GetSession(r)
	hideMature := false // guests: include so frontend can render blurred thumbnails
	if session != nil {
		// For authenticated users, hide mature unless they have explicitly enabled it
		user, err := h.auth.GetUser(r.Context(), session.Username)
		if err != nil || user == nil || !user.Preferences.ShowMature {
			hideMature = true
		}
	}

	if hideMature {
		filtered := make([]*models.MediaItem, 0, len(allItems))
		for _, item := range allItems {
			if !item.IsMature {
				filtered = append(filtered, item)
			}
		}
		allItems = filtered
	}

	// Calculate pagination metadata
	totalItems := len(allItems)
	totalPages := 1
	if limit > 0 {
		totalPages = (totalItems + limit - 1) / limit // Ceiling division
		if totalPages < 1 {
			totalPages = 1
		}
	}

	// Apply pagination to get the current page
	items := allItems
	if items == nil {
		items = []*models.MediaItem{} // guard: nil marshals as JSON null, not []
	}
	if offset > 0 {
		if offset >= len(items) {
			items = []*models.MediaItem{}
		} else {
			items = items[offset:]
		}
	}
	if limit > 0 && limit < len(items) {
		items = items[:limit]
	}

	// Enrich items with thumbnail URLs and queue generation if missing
	for _, item := range items {
		if item.ThumbnailURL == "" {
			if !h.thumbnails.HasThumbnail(item.Path) {
				// Queue thumbnail generation asynchronously - doesn't block HTTP response
				// Frontend will initially see placeholder, then thumbnail appears when ready
				isAudio := item.Type == "audio"
				_, err := h.thumbnails.GenerateThumbnail(item.Path, isAudio)
				if err != nil && !errors.Is(err, thumbnails.ErrThumbnailPending) {
					h.log.Warn("Failed to queue thumbnail for %s: %v", item.Path, err)
				}
				// ErrThumbnailPending is expected - means thumbnail is queued for generation
			}
			item.ThumbnailURL = h.thumbnails.GetThumbnailURL(item.Path)
		}
	}

	// Return paginated response with metadata
	writeSuccess(w, map[string]interface{}{
		"items":       items,
		"total_items": totalItems,
		"total_pages": totalPages,
		"scanning":    h.media.IsScanning(),
	})
}

// GetMedia returns a single media item
func (h *Handler) GetMedia(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id := vars["id"]

	// UseEncodedPath() causes mux.Vars to return the raw percent-encoded value.
	// Decode it so that path-based map lookups work correctly.
	if decoded, err := url.PathUnescape(id); err == nil {
		id = decoded
	}

	// Try path lookup first (frontend sends file path as the "id" parameter)
	item, err := h.media.GetMedia(id)
	if err != nil {
		// Fall back to MD5 hash lookup for direct ID-based access
		item, err = h.media.GetMediaByID(id)
		if err != nil {
			writeError(w, http.StatusNotFound, "Media not found")
			return
		}
	}

	if item.ThumbnailURL == "" {
		if !h.thumbnails.HasThumbnail(item.Path) {
			// Queue thumbnail generation asynchronously for single item request
			isAudio := item.Type == "audio"
			_, err := h.thumbnails.GenerateThumbnail(item.Path, isAudio)
			if err != nil && !errors.Is(err, thumbnails.ErrThumbnailPending) {
				h.log.Warn("Failed to queue thumbnail for %s: %v", item.Path, err)
			}
			// ErrThumbnailPending is expected - means thumbnail is queued
		}
		item.ThumbnailURL = h.thumbnails.GetThumbnailURL(item.Path)
	}

	writeSuccess(w, item)
}

// GetMediaStats returns media statistics
func (h *Handler) GetMediaStats(w http.ResponseWriter, _ *http.Request) {
	stats := h.media.GetStats()
	writeSuccess(w, stats)
}

// ScanMedia initiates a media scan
func (h *Handler) ScanMedia(w http.ResponseWriter, _ *http.Request) {
	go func() {
		if err := h.media.Scan(); err != nil {
			h.log.Error("Media scan failed: %v", err)
		}
	}()
	writeSuccess(w, map[string]string{"message": "Scan started"})
}

// GetCategories returns media categories
func (h *Handler) GetCategories(w http.ResponseWriter, _ *http.Request) {
	categories := h.media.GetCategories()
	writeSuccess(w, categories)
}

// Streaming Handlers

func (h *Handler) StreamMedia(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Query().Get("path")
	if path == "" {
		writeError(w, http.StatusBadRequest, errPathRequired)
		return
	}

	// Check mature content access before streaming
	absPath, ok := h.resolveAndValidatePath(w, path, h.allowedMediaDirs())
	if !ok {
		return
	}

	if !h.checkMatureAccess(w, r, absPath) {
		return
	}

	session := middleware.GetSession(r)
	var userID, sessionID string
	if session != nil {
		userID = session.UserID
		sessionID = session.ID

		user, err := h.auth.GetUser(r.Context(), session.Username)
		if err == nil {
			maxStreams := h.getUserStreamLimit(user.Type)
			if maxStreams > 0 && !h.streaming.CanStartStream(userID, maxStreams) {
				writeError(w, http.StatusTooManyRequests, "Maximum concurrent streams limit reached")
				return
			}
		}
	}

	req := streaming.StreamRequest{
		Path:        absPath,
		Quality:     r.URL.Query().Get("quality"),
		UserID:      userID,
		SessionID:   sessionID,
		IPAddress:   middleware.GetClientIP(r),
		UserAgent:   r.UserAgent(),
		RangeHeader: r.Header.Get("Range"),
	}

	// Track view only for guest sessions (no auth cookie) and only on the initial
	// request (Range: bytes=0-...). Authenticated users are tracked by the frontend
	// player via POST /api/analytics/events, which requires auth. Tracking here for
	// every range request would produce duplicate events per play session.
	rangeHeader := r.Header.Get("Range")
	isInitialRequest := rangeHeader == "" || strings.HasPrefix(rangeHeader, "bytes=0-")
	if isInitialRequest && session == nil && h.analytics != nil {
		h.analytics.TrackView(r.Context(), absPath, userID, sessionID, req.IPAddress, req.UserAgent)
	}

	// Feed view event into suggestions engine for personalized recommendations.
	// This integrates analytics-triggered views with the suggestions module so that
	// actual playback events (not just background scan data) improve suggestion quality.
	if h.suggestions != nil && userID != "" {
		if item, err := h.media.GetMedia(absPath); err == nil {
			h.suggestions.RecordView(userID, absPath, item.Category, string(item.Type), 0)
		}
	}

	// Increment view count
	if err := h.media.IncrementViews(r.Context(), absPath); err != nil {
		h.log.Warn("Failed to increment view count for %s: %v", absPath, err)
	}

	if err := h.streaming.Stream(w, r, req); err != nil {
		if errors.Is(err, streaming.ErrFileNotFound) {
			writeError(w, http.StatusNotFound, errFileNotFound)
		} else {
			h.log.Error("Stream error: %v", err)
			writeError(w, http.StatusInternalServerError, "Stream error")
		}
	}
}

// DownloadMedia downloads a media file
func (h *Handler) DownloadMedia(w http.ResponseWriter, r *http.Request) {
	cfg := h.media.GetConfig()
	session := middleware.GetSession(r)

	if cfg.Download.RequireAuth && session == nil {
		writeError(w, http.StatusUnauthorized, errNotAuthenticated)
		return
	}

	if session != nil {
		user, err := h.auth.GetUser(r.Context(), session.Username)
		if err == nil && !user.Permissions.CanDownload {
			writeError(w, http.StatusForbidden, "Download not allowed for your user type")
			return
		}
	}

	path := r.URL.Query().Get("path")
	if path == "" {
		writeError(w, http.StatusBadRequest, errPathRequired)
		return
	}

	absPath, ok := h.resolveAndValidatePath(w, path, h.allowedMediaDirs())
	if !ok {
		return
	}

	if !h.checkMatureAccess(w, r, absPath) {
		return
	}

	if err := h.streaming.Download(w, r, absPath); err != nil {
		if errors.Is(err, streaming.ErrFileNotFound) {
			writeError(w, http.StatusNotFound, errFileNotFound)
		} else if errors.Is(err, streaming.ErrFileTooLarge) {
			writeError(w, http.StatusRequestEntityTooLarge, "File exceeds maximum download size")
		} else if isClientDisconnect(err) {
			// Client closed the connection mid-transfer; not a server error.
			h.log.Debug("Download cancelled by client: %v", err)
		} else {
			h.log.Error("Download error: %v", err)
			writeError(w, http.StatusInternalServerError, "Download error")
		}
	}
}

// resolveAndValidatePath resolves a file path against allowed directories, prevents path
// traversal, and verifies the file exists. Returns the absolute path and true on success,
// or writes an error response and returns ("", false) on failure.
func (h *Handler) resolveAndValidatePath(w http.ResponseWriter, path string, allowedDirs []string) (string, bool) {
	validPath := h.resolveRelativePath(path, allowedDirs)
	if validPath == "" {
		writeError(w, http.StatusNotFound, errFileNotFound)
		return "", false
	}

	realPath, err := filepath.EvalSymlinks(validPath)
	if err != nil {
		realPath = validPath
	}
	absPath, err := filepath.Abs(realPath)
	if err != nil {
		writeError(w, http.StatusBadRequest, "Invalid path")
		return "", false
	}

	if !isPathWithinDirs(absPath, allowedDirs) {
		h.log.Warn("Path traversal attempt detected: %s", path)
		writeError(w, http.StatusForbidden, "Access denied: path outside allowed directories")
		return "", false
	}

	if _, err := os.Stat(absPath); err != nil {
		if os.IsNotExist(err) {
			writeError(w, http.StatusNotFound, errFileNotFound)
		} else {
			writeError(w, http.StatusInternalServerError, "Error accessing file")
		}
		return "", false
	}

	return absPath, true
}

// resolveRelativePath resolves a possibly-relative path against allowed directories.
// Returns the resolved path, or the original path if it is already absolute.
// Returns "" if a relative path cannot be found in any allowed directory.
func (h *Handler) resolveRelativePath(path string, allowedDirs []string) string {
	if filepath.IsAbs(path) {
		return path
	}
	for _, dir := range allowedDirs {
		testPath := filepath.Join(dir, path)
		if _, err := os.Stat(testPath); err == nil {
			return testPath
		}
	}
	return ""
}

// isPathWithinDirs checks whether absPath resides within at least one of the given directories.
func isPathWithinDirs(absPath string, dirs []string) bool {
	for _, dir := range dirs {
		absDir, err := filepath.Abs(dir)
		if err != nil {
			continue
		}
		relPath, err := filepath.Rel(absDir, absPath)
		if err == nil && !strings.HasPrefix(relPath, ".."+string(filepath.Separator)) && relPath != ".." {
			return true
		}
	}
	return false
}

// checkMatureAccess verifies the current user has permission to access mature content at
// the given path. Returns true if access is allowed or irrelevant, false if denied
// (in which case an error response has already been written).
func (h *Handler) checkMatureAccess(w http.ResponseWriter, r *http.Request, absPath string) bool {
	item, err := h.media.GetMedia(absPath)
	if err != nil || item == nil || !item.IsMature {
		// Not mature content or doesn't exist, allow access
		return true
	}

	// Content is mature - require authentication
	session := middleware.GetSession(r)
	if session == nil {
		writeError(w, http.StatusUnauthorized,
			"Access denied: This content is marked as mature (18+). "+
				"Please log in to access mature content.")
		return false
	}

	// Get user from context (set by session middleware, includes admin users)
	user := middleware.GetUser(r)
	if user == nil {
		h.log.Debug("Mature content access denied for %s: user not found in context", session.Username)
		writeError(w, http.StatusForbidden,
			"Access denied: This content is marked as mature (18+). "+
				"Enable mature content viewing in your profile settings.")
		return false
	}

	// Admin-controlled hard block (CanViewMature defaults to true; admin can revoke per-user)
	if !user.Permissions.CanViewMature {
		h.log.Debug("Mature content access denied for %s: CanViewMature revoked by admin", session.Username)
		writeError(w, http.StatusForbidden,
			"Access denied: Your account does not have permission to view mature content (18+). "+
				"Contact an administrator if you believe this is an error.")
		return false
	}

	// User must explicitly enable the mature content toggle in their profile
	if !user.Preferences.ShowMature {
		h.log.Debug("Mature content access denied for %s: ShowMature preference is false", session.Username)
		writeError(w, http.StatusForbidden,
			"Access denied: This content is marked as mature (18+). "+
				"Enable mature content viewing in your profile settings.")
		return false
	}

	return true
}

// GetPlaybackPosition returns the saved playback position for the current user.
// Route is protected by makeRequireAuth so session is guaranteed non-nil.
func (h *Handler) GetPlaybackPosition(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Query().Get("path")
	if path == "" {
		writeError(w, http.StatusBadRequest, errPathParamRequired)
		return
	}

	session := middleware.GetSession(r)
	if session == nil {
		writeError(w, http.StatusUnauthorized, errNotAuthenticated)
		return
	}
	position := h.media.GetPlaybackPosition(r.Context(), path, session.UserID)
	writeSuccess(w, map[string]float64{"position": position})
}

// TrackPlayback records playback position
func (h *Handler) TrackPlayback(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Path     string  `json:"path"`
		Position float64 `json:"position"`
		Duration float64 `json:"duration"`
	}
	// Decode JSON with error logging (use decodeJSON helper throughout file for consistent error handling)
	if decodeJSON(r, &req) != nil {
		writeError(w, http.StatusBadRequest, errInvalidRequest)
		return
	}

	session := middleware.GetSession(r)
	var userID, sessionID, username string
	if session != nil {
		userID = session.UserID
		sessionID = session.ID
		username = session.Username
	}

	// Only save playback position for authenticated users — the DB has a FK
	// constraint on playback_positions.user_id which rejects empty strings
	if userID != "" {
		if err := h.media.UpdatePlaybackPosition(r.Context(), req.Path, userID, req.Position); err != nil {
			h.log.Warn("Failed to update playback position for %s: %v", req.Path, err)
		}

		// Also update the watch history in the auth module so GET /api/watch-history
		// returns resume data (position + duration + watched_at) for the AudioPlayer
		// resume prompt. Only update when duration is known (position==0 on initial load).
		if req.Duration > 0 && username != "" {
			pathHash := md5.Sum([]byte(req.Path))
			item := models.WatchHistoryItem{
				MediaPath: req.Path,
				MediaID:   hex.EncodeToString(pathHash[:]),
				Position:  req.Position,
				Duration:  req.Duration,
				WatchedAt: time.Now(),
			}
			item.Progress = req.Position / req.Duration
			item.Completed = item.Progress >= 0.9
			if err := h.auth.AddToWatchHistory(r.Context(), username, item); err != nil {
				h.log.Debug("Watch history update skipped for %s: %v", req.Path, err)
			}
		}
	}

	// Track analytics
	if h.analytics != nil {
		h.analytics.TrackPlayback(r.Context(), req.Path, userID, sessionID, req.Position, req.Duration)
	}

	writeSuccess(w, nil)
}

// HLS Handlers

// GenerateHLS starts HLS generation for a media file
func (h *Handler) GenerateHLS(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Path      string   `json:"path"`
		Qualities []string `json:"qualities"`
		Quality   string   `json:"quality"` // singular alias for frontend compatibility
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.log.Debug("Invalid JSON in GenerateHLS request: %v", err)
		writeError(w, http.StatusBadRequest, errInvalidRequest)
		return
	}
	// Promote singular quality to slice if qualities not provided
	if len(req.Qualities) == 0 && req.Quality != "" {
		req.Qualities = []string{req.Quality}
	}

	// Resolve and validate path against allowed directories, matching StreamMedia behavior
	absPath, ok := h.resolveAndValidatePath(w, req.Path, h.allowedMediaDirs())
	if !ok {
		return
	}

	job, err := h.hls.GenerateHLS(r.Context(), absPath, req.Qualities)
	if err != nil {
		h.log.Error("%v", err)
		writeError(w, http.StatusInternalServerError, "Internal server error")
		return
	}

	jobQualities := job.Qualities
	if jobQualities == nil {
		jobQualities = []string{}
	}
	resp := map[string]interface{}{
		"job_id":     job.ID,
		"id":         job.ID,
		"status":     job.Status,
		"media_path": job.MediaPath,
		"progress":   job.Progress,
		"qualities":  jobQualities,
		"error":      job.Error,
		"fail_count": job.FailCount,
		"started_at": job.StartedAt,
		"available":  job.Status == models.HLSStatusCompleted,
		"hls_url":    "",
	}
	if job.Status == models.HLSStatusCompleted {
		resp["hls_url"] = fmt.Sprintf("/hls/%s/master.m3u8", job.ID)
	}
	if job.CompletedAt != nil {
		resp["completed_at"] = job.CompletedAt
	}
	writeSuccess(w, resp)
}

// CheckHLSAvailability checks HLS status by media path and auto-generates if configured
func (h *Handler) CheckHLSAvailability(w http.ResponseWriter, r *http.Request) {
	mediaPath := r.URL.Query().Get("path")
	if mediaPath == "" {
		writeError(w, http.StatusBadRequest, "path parameter required")
		return
	}

	// Resolve path against allowed directories for consistency with GenerateHLS
	absPath, ok := h.resolveAndValidatePath(w, mediaPath, h.allowedMediaDirs())
	if !ok {
		return
	}

	// Try to get existing job by media path, or create it with auto-generate
	job, err := h.hls.CheckOrGenerateHLS(r.Context(), absPath)
	if err != nil {
		h.log.Debug("HLS check/generate failed for %s: %v", absPath, err)
		writeError(w, http.StatusNotFound, "HLS stream not available")
		return
	}

	// Build response
	checkQualities := job.Qualities
	if checkQualities == nil {
		checkQualities = []string{}
	}
	response := map[string]interface{}{
		"id":         job.ID,
		"job_id":     job.ID, // alias for frontend compatibility
		"media_path": job.MediaPath,
		"status":     job.Status,
		"progress":   job.Progress,
		"qualities":  checkQualities,
		"started_at": job.StartedAt,
		"error":      job.Error,
	}

	if job.CompletedAt != nil {
		response["completed_at"] = job.CompletedAt
	}

	// Add available and hls_url fields
	if job.Status == models.HLSStatusCompleted {
		response["available"] = true
		response["hls_url"] = fmt.Sprintf("/hls/%s/master.m3u8", job.ID)
	} else {
		response["available"] = false
		response["hls_url"] = ""
	}

	writeSuccess(w, response)
}

// GetHLSStatus returns HLS generation status by job ID.
// Returns a manual map (not models.HLSJob directly) to exclude "output_dir" from client responses.
func (h *Handler) GetHLSStatus(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	jobID := vars["id"]

	job, err := h.hls.GetJobStatus(jobID)
	if err != nil {
		writeError(w, http.StatusNotFound, "Job not found")
		return
	}

	// Build response with all HLSJob fields
	statusQualities := job.Qualities
	if statusQualities == nil {
		statusQualities = []string{}
	}
	response := map[string]interface{}{
		"id":         job.ID,
		"media_path": job.MediaPath,
		"status":     job.Status,
		"progress":   job.Progress,
		"qualities":  statusQualities,
		"started_at": job.StartedAt,
		"error":      job.Error,
		"fail_count": job.FailCount,
	}

	if job.CompletedAt != nil {
		response["completed_at"] = job.CompletedAt
	}

	// Add available and hls_url fields when job is completed
	if job.Status == models.HLSStatusCompleted {
		response["available"] = true
		response["hls_url"] = fmt.Sprintf("/hls/%s/master.m3u8", job.ID)
	} else {
		response["available"] = false
		response["hls_url"] = ""
	}

	writeSuccess(w, response)
}

// ServeMasterPlaylist serves the HLS master playlist
func (h *Handler) ServeMasterPlaylist(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	jobID := vars["id"]

	// Check mature content access before serving HLS
	job, err := h.hls.GetJobStatus(jobID)
	if err != nil {
		writeError(w, http.StatusNotFound, "HLS job not found")
		return
	}

	if !h.checkMatureAccess(w, r, job.MediaPath) {
		return
	}

	if err := h.hls.ServeMasterPlaylist(w, r, jobID); err != nil {
		writeError(w, http.StatusNotFound, "HLS playlist not found")
		return
	}
}

// ServeVariantPlaylist serves an HLS variant playlist
func (h *Handler) ServeVariantPlaylist(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	jobID := vars["id"]
	quality := vars["quality"]

	// Check mature content access before serving HLS
	job, err := h.hls.GetJobStatus(jobID)
	if err != nil {
		writeError(w, http.StatusNotFound, "HLS job not found")
		return
	}

	if !h.checkMatureAccess(w, r, job.MediaPath) {
		return
	}

	if err := h.hls.ServeVariantPlaylist(w, r, jobID, quality); err != nil {
		writeError(w, http.StatusNotFound, "HLS variant playlist not found")
		return
	}
}

// ServeSegment serves an HLS segment
func (h *Handler) ServeSegment(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	jobID := vars["id"]
	quality := vars["quality"]
	segment := vars["segment"]

	// Check mature content access before serving HLS segment
	job, err := h.hls.GetJobStatus(jobID)
	if err != nil {
		writeError(w, http.StatusNotFound, "HLS job not found")
		return
	}

	if !h.checkMatureAccess(w, r, job.MediaPath) {
		return
	}

	if err := h.hls.ServeSegment(w, r, jobID, quality, segment); err != nil {
		writeError(w, http.StatusNotFound, "HLS segment not found")
		return
	}
}

// GetHLSCapabilities returns whether HLS transcoding is available and its configuration.
// This lets the frontend decide whether to attempt HLS or go straight to direct streaming.
func (h *Handler) GetHLSCapabilities(w http.ResponseWriter, _ *http.Request) {
	caps := h.hls.GetCapabilities()
	writeSuccess(w, caps)
}

// GetHLSStats returns HLS module statistics
func (h *Handler) GetHLSStats(w http.ResponseWriter, _ *http.Request) {
	stats := h.hls.GetStats()
	writeSuccess(w, stats)
}

// ValidateHLS validates an HLS job's playlists and segments
func (h *Handler) ValidateHLS(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	jobID := vars["id"]

	result, err := h.hls.ValidateMasterPlaylist(jobID)
	if err != nil {
		h.log.Error("%v", err)
		writeError(w, http.StatusInternalServerError, "Internal server error")
		return
	}

	writeSuccess(w, result)
}

// CleanHLSStaleLocks removes stale HLS generation locks
func (h *Handler) CleanHLSStaleLocks(w http.ResponseWriter, _ *http.Request) {
	removed := h.hls.CleanStaleLocks()
	writeSuccess(w, map[string]int{"removed": removed})
}

// CleanHLSInactive removes inactive HLS content
func (h *Handler) CleanHLSInactive(w http.ResponseWriter, r *http.Request) {
	// Default to 24 hours if not specified
	threshold := 24 * time.Hour

	// Try JSON body first (frontend sends max_age_hours or threshold_hours)
	var bodyReq struct {
		MaxAgeHours    int `json:"max_age_hours"`
		ThresholdHours int `json:"threshold_hours"`
	}
	if json.NewDecoder(r.Body).Decode(&bodyReq) == nil {
		if bodyReq.MaxAgeHours > 0 {
			threshold = time.Duration(bodyReq.MaxAgeHours) * time.Hour
		} else if bodyReq.ThresholdHours > 0 {
			threshold = time.Duration(bodyReq.ThresholdHours) * time.Hour
		}
	} else if thresholdStr := r.URL.Query().Get("threshold_hours"); thresholdStr != "" {
		// Fall back to query param
		if hours, err := strconv.Atoi(thresholdStr); err == nil && hours > 0 {
			threshold = time.Duration(hours) * time.Hour
		}
	}

	removed := h.hls.CleanInactiveJobs(threshold)
	writeSuccess(w, map[string]interface{}{
		"removed":   removed,
		"threshold": threshold.String(),
	})
}

// ListHLSJobs returns all HLS jobs
func (h *Handler) ListHLSJobs(w http.ResponseWriter, _ *http.Request) {
	jobs := h.hls.ListJobs()
	// Normalize nil Qualities to empty slice — nil marshals as JSON null which crashes JS callers
	for _, j := range jobs {
		if j != nil && j.Qualities == nil {
			j.Qualities = []string{}
		}
	}
	writeSuccess(w, jobs)
}

// DeleteHLSJob removes an HLS job and its files
func (h *Handler) DeleteHLSJob(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	jobID := vars["id"]

	if err := h.hls.DeleteJob(jobID); err != nil {
		writeError(w, http.StatusNotFound, "HLS job not found")
		return
	}

	writeSuccess(w, map[string]string{"deleted": jobID})
}

// Auth Handlers

// Login authenticates a user or admin using the same endpoint
func (h *Handler) Login(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if json.NewDecoder(r.Body).Decode(&req) != nil {
		writeError(w, http.StatusBadRequest, errInvalidRequest)
		return
	}

	// Check if this is an admin login attempt first
	adminSession, adminErr := h.auth.AdminAuthenticate(r.Context(),
		req.Username,
		req.Password,
		middleware.GetClientIP(r),
		r.UserAgent(),
	)

	// Handle admin authentication results
	if adminErr != nil {
		if errors.Is(adminErr, auth.ErrAccountLocked) {
			writeError(w, http.StatusTooManyRequests, "Too many failed login attempts. Please try again later.")
			return
		}
		// ErrAdminWrongPassword means username matched admin but password was wrong
		// This is a failed admin login attempt - don't fall through to user auth
		if errors.Is(adminErr, auth.ErrAdminWrongPassword) {
			writeError(w, http.StatusUnauthorized, "Invalid credentials")
			return
		}
		// ErrNotAdminUsername means this is not an admin login attempt - try user auth
		if !errors.Is(adminErr, auth.ErrNotAdminUsername) {
			// Some other admin auth error
			writeError(w, http.StatusUnauthorized, "Invalid credentials")
			return
		}
	} else {
		// Admin credentials validated — create a regular session so the admin uses
		// the same session_id cookie flow as regular users (permissions from database).
		session, sessErr := h.auth.CreateSessionForUser(
			r.Context(),
			adminSession.Username,
			middleware.GetClientIP(r),
			r.UserAgent(),
		)
		if sessErr != nil {
			h.log.Error("Failed to create admin session: %v", sessErr)
			writeError(w, http.StatusInternalServerError, "Failed to create session")
			return
		}

		http.SetCookie(w, &http.Cookie{
			Name:     "session_id",
			Value:    session.ID,
			Path:     "/",
			Expires:  session.ExpiresAt,
			HttpOnly: true,
			SameSite: http.SameSiteLaxMode,
			Secure:   isSecureRequest(r),
		})
		writeSuccess(w, map[string]interface{}{
			"session_id": session.ID,
			"username":   session.Username,
			"role":       session.Role,
			"is_admin":   session.Role == models.RoleAdmin,
			"expires_at": session.ExpiresAt,
		})
		return
	}

	// Try regular user authentication (only if not admin username)
	session, err := h.auth.Authenticate(r.Context(),
		req.Username,
		req.Password,
		middleware.GetClientIP(r),
		r.UserAgent(),
	)
	if err != nil {
		if errors.Is(err, auth.ErrAccountLocked) {
			writeError(w, http.StatusTooManyRequests, "Too many failed login attempts. Please try again later.")
			return
		}
		writeError(w, http.StatusUnauthorized, "Invalid credentials")
		return
	}

	// Set session cookie
	http.SetCookie(w, &http.Cookie{
		Name:     "session_id",
		Value:    session.ID,
		Path:     "/",
		Expires:  session.ExpiresAt,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   isSecureRequest(r),
	})

	writeSuccess(w, map[string]interface{}{
		"session_id": session.ID,
		"username":   session.Username,
		"role":       session.Role,
		"is_admin":   false,
		"expires_at": session.ExpiresAt,
	})
}

// Logout invalidates a session (both regular and admin)
func (h *Handler) Logout(w http.ResponseWriter, r *http.Request) {
	// Clear regular session
	cookie, err := r.Cookie("session_id")
	if err == nil {
		if logoutErr := h.auth.Logout(r.Context(), cookie.Value); logoutErr != nil {
			h.log.Warn("Failed to logout session: %v", logoutErr)
		}
	}

	// Clear admin session
	adminCookie, err := r.Cookie("admin_session")
	if err == nil {
		if logoutErr := h.auth.LogoutAdmin(r.Context(), adminCookie.Value); logoutErr != nil {
			h.log.Warn("Failed to logout admin session: %v", logoutErr)
		}
	}

	// Clear session_id cookie
	http.SetCookie(w, &http.Cookie{
		Name:     "session_id",
		Value:    "",
		Path:     "/",
		Expires:  time.Unix(0, 0),
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   isSecureRequest(r),
	})

	// Clear admin_session cookie
	http.SetCookie(w, &http.Cookie{
		Name:     "admin_session",
		Value:    "",
		Path:     "/",
		Expires:  time.Unix(0, 0),
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   isSecureRequest(r),
	})

	writeSuccess(w, nil)
}

// CheckSession returns the current session status (used by frontend for auth checks)
func (h *Handler) CheckSession(w http.ResponseWriter, r *http.Request) {
	cfg := h.media.GetConfig()
	allowGuests := cfg.Auth.AllowGuests

	// The session middleware validates the session_id cookie and places the user in
	// request context. Admin and regular users both use the same session_id flow.
	user := middleware.GetUser(r)
	if user == nil {
		writeSuccess(w, map[string]interface{}{
			"authenticated": false,
			"allow_guests":  allowGuests,
		})
		return
	}

	writeSuccess(w, map[string]interface{}{
		"authenticated": true,
		"allow_guests":  allowGuests,
		"user":          user,
	})
}

// Register creates a new user account
func (h *Handler) Register(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
		Email    string `json:"email"`
	}
	if json.NewDecoder(r.Body).Decode(&req) != nil {
		writeError(w, http.StatusBadRequest, errInvalidRequest)
		return
	}

	// Validate input
	req.Username = strings.TrimSpace(req.Username)
	req.Email = strings.TrimSpace(req.Email)
	if len(req.Username) < 3 || len(req.Username) > 64 {
		writeError(w, http.StatusBadRequest, "Username must be between 3 and 64 characters")
		return
	}
	if len(req.Password) < 8 {
		writeError(w, http.StatusBadRequest, "Password must be at least 8 characters")
		return
	}
	// Only allow alphanumeric, underscore, hyphen in usernames
	for _, c := range req.Username {
		if (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') && (c < '0' || c > '9') && c != '_' && c != '-' {
			writeError(w, http.StatusBadRequest, "Username may only contain letters, numbers, underscores, and hyphens")
			return
		}
	}
	if req.Email != "" && !strings.Contains(req.Email, "@") {
		writeError(w, http.StatusBadRequest, "Invalid email address")
		return
	}

	user, err := h.auth.CreateUser(r.Context(), req.Username, req.Password, req.Email, "standard", models.RoleViewer)
	if err != nil {
		if errors.Is(err, auth.ErrUserExists) {
			writeError(w, http.StatusConflict, "Username is already taken")
			return
		}
		h.log.Error("Failed to create user %s: %v", req.Username, err)
		writeError(w, http.StatusInternalServerError, "Internal server error")
		return
	}

	// Automatically log in the new user by creating a session directly
	// No need to re-authenticate since we just created the user with a valid password
	session, authErr := h.auth.CreateSessionForUser(r.Context(),
		req.Username,
		middleware.GetClientIP(r),
		r.UserAgent(),
	)
	if authErr != nil {
		h.log.Error("Failed to create session for new user %s: %v", req.Username, authErr)
		writeError(w, http.StatusInternalServerError, "Account created but login failed")
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     "session_id",
		Value:    session.ID,
		Path:     "/",
		Expires:  session.ExpiresAt,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   isSecureRequest(r),
	})

	// Return the user directly (matches frontend api.post<User> type)
	writeSuccess(w, user)
}

// User Preferences Handlers

// GetPreferences returns the current user's preferences
func (h *Handler) GetPreferences(w http.ResponseWriter, r *http.Request) {
	session := middleware.GetSession(r)
	if session == nil {
		writeError(w, http.StatusUnauthorized, errNotAuthenticated)
		return
	}

	// For admin users, try DB lookup first to get persisted preferences
	// (middleware pseudo-user always has default/empty preferences)
	var user *models.User
	if session.Role == models.RoleAdmin {
		if dbUser, err := h.auth.GetUser(r.Context(), session.Username); err == nil {
			user = dbUser
		}
	}

	// Fallback to middleware context user, then DB lookup by ID
	if user == nil {
		user = middleware.GetUser(r)
	}
	if user == nil {
		var err error
		user, err = h.auth.GetUserByID(r.Context(), session.UserID)
		if err != nil {
			writeError(w, http.StatusNotFound, errUserNotFound)
			return
		}
	}

	writeSuccess(w, user.Preferences)
}

// UpdatePreferences updates the current user's preferences
func (h *Handler) UpdatePreferences(w http.ResponseWriter, r *http.Request) {
	session := middleware.GetSession(r)
	if session == nil {
		writeError(w, http.StatusUnauthorized, errNotAuthenticated)
		return
	}

	// Decode into a raw map to know which fields were actually sent,
	// avoiding zero-value overwrites for fields the client didn't include.
	var incoming map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&incoming); err != nil {
		h.log.Error("Failed to decode preferences JSON for user %s: %v", session.Username, err)
		writeError(w, http.StatusBadRequest, errInvalidRequest)
		return
	}

	// For admin users, ensure a user record exists so preferences can be persisted
	if session.Role == models.RoleAdmin {
		if _, err := h.auth.GetUser(r.Context(), session.Username); err != nil {
			// Create admin user record with secure random password for preference storage
			// Note: CreateUser has internal locking, so concurrent calls are safe
			randomPassword, pwdErr := h.auth.GenerateSecurePassword(32)
			if pwdErr != nil {
				h.log.Error("Failed to generate password for admin user record: %v", pwdErr)
				randomPassword = "FALLBACK_UNUSED_PASSWORD_" + generateRandomString(24)
			}
			if _, createErr := h.auth.CreateUser(r.Context(), session.Username, randomPassword, "", "admin", models.RoleAdmin); createErr != nil {
				h.log.Warn("Could not create admin user record for preferences: %v", createErr)
			}
		}
	}

	// Get existing preferences to merge with
	user, err := h.auth.GetUser(r.Context(), session.Username)
	if err != nil {
		writeError(w, http.StatusNotFound, errUserNotFound)
		return
	}
	prefs := user.Preferences

	// Merge only the fields that were explicitly sent
	if v, ok := incoming["theme"].(string); ok {
		prefs.Theme = v
	}
	if v, ok := incoming["view_mode"].(string); ok {
		prefs.ViewMode = v
	}
	if v, ok := incoming["default_quality"].(string); ok {
		prefs.DefaultQuality = v
	}
	if v, ok := incoming["auto_play"].(bool); ok {
		prefs.AutoPlay = v
	}
	if v, ok := incoming["autoplay"].(bool); ok {
		prefs.AutoPlay = v
	}
	if v, ok := incoming["playback_speed"].(float64); ok {
		prefs.PlaybackSpeed = v
	}
	if v, ok := incoming["volume"].(float64); ok {
		prefs.Volume = v
	}
	if showMature, ok := incoming["show_mature"].(bool); ok {
		prefs.ShowMature = showMature
		prefs.MaturePreferenceSet = true
	}
	if v, ok := incoming["language"].(string); ok {
		prefs.Language = v
	}
	if v, ok := incoming["equalizer_preset"].(string); ok {
		prefs.EqualizerPreset = v
	} else if v, ok := incoming["equalizer_bands"].(string); ok {
		prefs.EqualizerPreset = v
	}
	if v, ok := incoming["resume_playback"].(bool); ok {
		prefs.ResumePlayback = v
	}
	if v, ok := incoming["show_analytics"].(bool); ok {
		prefs.ShowAnalytics = v
	}
	if v, ok := incoming["items_per_page"].(float64); ok {
		prefs.ItemsPerPage = int(v)
	}
	if v, ok := incoming["sort_by"].(string); ok {
		prefs.SortBy = v
	}
	if v, ok := incoming["sort_order"].(string); ok {
		prefs.SortOrder = v
	}
	if v, ok := incoming["filter_category"].(string); ok {
		prefs.FilterCategory = v
	}
	if v, ok := incoming["filter_media_type"].(string); ok {
		prefs.FilterMediaType = v
	}
	if v, ok := incoming["custom_eq_presets"].(map[string]interface{}); ok {
		prefs.CustomEQPresets = v
	}

	h.log.Debug("Updating preferences for user %s: show_mature=%v, mature_preference_set=%v", session.Username, prefs.ShowMature, prefs.MaturePreferenceSet)

	if err := h.auth.UpdateUserPreferences(r.Context(), session.Username, prefs); err != nil {
		h.log.Error("Failed to update preferences for user %s: %v", session.Username, err)
		writeError(w, http.StatusInternalServerError, "Internal server error")
		return
	}

	writeSuccess(w, prefs)
}

// Watch History Handlers

// GetWatchHistory returns the current user's watch history
func (h *Handler) GetWatchHistory(w http.ResponseWriter, r *http.Request) {
	session := middleware.GetSession(r)
	if session == nil {
		writeError(w, http.StatusUnauthorized, errNotAuthenticated)
		return
	}

	// Use the user from middleware context (works for both admin pseudo-users and regular users)
	user := middleware.GetUser(r)
	if user == nil {
		var err error
		user, err = h.auth.GetUserByID(r.Context(), session.UserID)
		if err != nil {
			writeError(w, http.StatusNotFound, errUserNotFound)
			return
		}
	}

	// Filter by path if specified (returns single-element array for specific file lookup)
	history := user.WatchHistory
	if pathFilter := r.URL.Query().Get("path"); pathFilter != "" {
		var matched []models.WatchHistoryItem
		for _, item := range history {
			if item.MediaPath == pathFilter {
				matched = append(matched, item)
				break
			}
		}
		if matched == nil {
			matched = []models.WatchHistoryItem{}
		}
		writeSuccess(w, matched)
		return
	}

	// Apply limit if specified
	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		if limit, err := strconv.Atoi(limitStr); err == nil && limit > 0 && limit < len(history) {
			history = history[:limit]
		}
	}

	writeSuccess(w, history)
}

// ClearWatchHistory clears the user's watch history.
// If a "path" query param is provided, only that item is removed; otherwise all history is cleared.
func (h *Handler) ClearWatchHistory(w http.ResponseWriter, r *http.Request) {
	session := middleware.GetSession(r)
	if session == nil {
		writeError(w, http.StatusUnauthorized, errNotAuthenticated)
		return
	}

	if mediaPath := r.URL.Query().Get("path"); mediaPath != "" {
		if err := h.auth.RemoveWatchHistoryItem(r.Context(), session.Username, mediaPath); err != nil {
			h.log.Error("%v", err)
			writeError(w, http.StatusInternalServerError, "Internal server error")
			return
		}
		// Also clear the resume position so the player does not show a stale
		// resume prompt the next time this file is opened.
		h.media.ClearPlaybackPosition(r.Context(), mediaPath, session.UserID)
		writeSuccess(w, map[string]string{"status": "removed"})
		return
	}

	if err := h.auth.ClearWatchHistory(r.Context(), session.Username); err != nil {
		h.log.Error("%v", err)
		writeError(w, http.StatusInternalServerError, "Internal server error")
		return
	}
	// Clear all in-memory resume positions for this user so the player does not
	// show stale prompts. DB rows are left in place (see ClearAllPlaybackPositions).
	h.media.ClearAllPlaybackPositions(session.UserID)

	writeSuccess(w, map[string]string{"status": "cleared"})
}

// Permissions Handlers

// GetPermissions returns the current user's permissions and capabilities
func (h *Handler) GetPermissions(w http.ResponseWriter, r *http.Request) {
	// Admin and regular users both use session_id; the session middleware handles both.
	session := middleware.GetSession(r)

	// Return unauthenticated user info if not logged in
	if session == nil {
		writeSuccess(w, map[string]interface{}{
			"authenticated":         false,
			"show_mature":           false,
			"mature_preference_set": false,
			"capabilities": map[string]bool{
				"canUpload":          false,
				"canDownload":        false,
				"canCreatePlaylists": false,
				"canViewMature":      false,
				"canStream":          false,
				"canDelete":          false,
				"canManage":          false,
			},
		})
		return
	}

	user, err := h.auth.GetUserByID(r.Context(), session.UserID)
	if err != nil {
		writeError(w, http.StatusNotFound, errUserNotFound)
		return
	}

	writeSuccess(w, map[string]interface{}{
		"authenticated":         true,
		"username":              user.Username,
		"role":                  user.Role,
		"user_type":             user.Type,
		"show_mature":           user.Preferences.ShowMature,
		"mature_preference_set": user.Preferences.MaturePreferenceSet,
		"capabilities": map[string]bool{
			"canUpload":          user.Permissions.CanUpload,
			"canDownload":        user.Permissions.CanDownload,
			"canCreatePlaylists": user.Permissions.CanCreatePlaylists,
			"canViewMature":      user.Permissions.CanViewMature,
			"canStream":          user.Permissions.CanStream,
			"canDelete":          user.Permissions.CanDelete,
			"canManage":          user.Permissions.CanManage,
		},
		"limits": map[string]interface{}{
			"storage_quota":      h.getUserStorageQuota(user.Type),
			"concurrent_streams": h.getUserStreamLimit(user.Type),
		},
	})
}

// allowedMediaDirs returns the directories from which media can be served.
// D-03: previously constructed inline in four separate handlers.
func (h *Handler) allowedMediaDirs() []string {
	cfg := h.media.GetConfig()
	return []string{cfg.Directories.Videos, cfg.Directories.Music, cfg.Directories.Uploads}
}

// getUserStorageQuota returns storage quota for user type
func (h *Handler) getUserStorageQuota(userType string) int64 {
	cfg := h.media.GetConfig()
	for _, ut := range cfg.Auth.UserTypes {
		if ut.Name == userType {
			return ut.StorageQuota
		}
	}
	// Fallback defaults — key names match config.go Auth.UserTypes defaults (D-01)
	quotas := map[string]int64{
		"basic":    1 * 1024 * 1024 * 1024,   // 1 GB
		"standard": 10 * 1024 * 1024 * 1024,  // 10 GB
		"premium":  100 * 1024 * 1024 * 1024, // 100 GB
		"admin":    -1,                        // unlimited
	}
	if q, ok := quotas[userType]; ok {
		return q
	}
	return quotas["basic"]
}

// getUserStreamLimit returns concurrent stream limit for user type
func (h *Handler) getUserStreamLimit(userType string) int {
	cfg := h.media.GetConfig()
	for _, ut := range cfg.Auth.UserTypes {
		if ut.Name == userType {
			return ut.MaxConcurrentStreams
		}
	}
	// Fallback defaults — key names match config.go Auth.UserTypes defaults (D-01)
	limits := map[string]int{
		"basic":    1,
		"standard": 3,
		"premium":  10,
		"admin":    -1, // unlimited
	}
	if l, ok := limits[userType]; ok {
		return l
	}
	return limits["basic"]
}

// DEPRECATED: R-05 — only called in one place (UploadMedia); inline the lookup there — safe to delete
func (h *Handler) getUserType(cfg *config.Config, user *models.User) *config.UserType {
	for i, ut := range cfg.Auth.UserTypes {
		if ut.Name == user.Type {
			return &cfg.Auth.UserTypes[i]
		}
	}
	return nil
}

// checkRemoteMediaEnabled returns true if remote media feature is enabled
func (h *Handler) checkRemoteMediaEnabled(w http.ResponseWriter) bool {
	cfg := h.media.GetConfig()
	if !cfg.Features.EnableRemoteMedia || !cfg.RemoteMedia.Enabled {
		writeError(w, http.StatusNotFound, "Remote media feature is disabled")
		return false
	}
	return true
}

// Playlist Handlers

// ListPlaylists returns user's playlists
func (h *Handler) ListPlaylists(w http.ResponseWriter, r *http.Request) {
	session := middleware.GetSession(r)
	if session == nil {
		writeError(w, http.StatusUnauthorized, errNotAuthenticated)
		return
	}

	playlists := h.playlist.ListPlaylists(session.UserID, true)
	// Ensure we always return an array, never null (nil slice → null in JSON)
	if playlists == nil {
		playlists = []*models.Playlist{}
	}
	writeSuccess(w, playlists)
}

// CreatePlaylist creates a new playlist
func (h *Handler) CreatePlaylist(w http.ResponseWriter, r *http.Request) {
	session := middleware.GetSession(r)
	if session == nil {
		writeError(w, http.StatusUnauthorized, errNotAuthenticated)
		return
	}

	user, err := h.auth.GetUser(r.Context(), session.Username)
	if err == nil && !user.Permissions.CanCreatePlaylists {
		writeError(w, http.StatusForbidden, "Playlist creation not allowed for your user type")
		return
	}

	var req struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		IsPublic    bool   `json:"is_public"`
	}
	if json.NewDecoder(r.Body).Decode(&req) != nil {
		writeError(w, http.StatusBadRequest, errInvalidRequest)
		return
	}

	pl, err := h.playlist.CreatePlaylist(r.Context(), req.Name, req.Description, session.UserID, req.IsPublic)
	if err != nil {
		h.log.Error("%v", err)
		writeError(w, http.StatusInternalServerError, "Internal server error")
		return
	}

	writeSuccess(w, pl)
}

// GetPlaylist returns a playlist
func (h *Handler) GetPlaylist(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id := vars["id"]

	session := middleware.GetSession(r)
	var userID string
	if session != nil {
		userID = session.UserID
	}

	pl, err := h.playlist.GetPlaylistForUser(id, userID)
	if err != nil {
		writeError(w, http.StatusNotFound, "Playlist not found")
		return
	}

	writeSuccess(w, pl)
}

// DeletePlaylist deletes a playlist
func (h *Handler) DeletePlaylist(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id := vars["id"]

	session := middleware.GetSession(r)
	if session == nil {
		writeError(w, http.StatusUnauthorized, errNotAuthenticated)
		return
	}

	if err := h.playlist.DeletePlaylist(r.Context(), id, session.UserID); err != nil {
		writeError(w, http.StatusForbidden, "Cannot delete playlist")
		return
	}

	writeSuccess(w, nil)
}

// UpdatePlaylist updates playlist metadata (name, description, is_public, cover_image)
func (h *Handler) UpdatePlaylist(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id := vars["id"]

	session := middleware.GetSession(r)
	if session == nil {
		writeError(w, http.StatusUnauthorized, errNotAuthenticated)
		return
	}

	var updates map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&updates); err != nil {
		writeError(w, http.StatusBadRequest, errInvalidRequest)
		return
	}

	if err := h.playlist.UpdatePlaylist(r.Context(), id, session.UserID, updates); err != nil {
		writeError(w, http.StatusForbidden, "Cannot update playlist")
		return
	}

	// Return the updated playlist
	playlist, err := h.playlist.GetPlaylistForUser(id, session.UserID)
	if err != nil {
		h.log.Warn("UpdatePlaylist: update succeeded but failed to fetch updated playlist %s: %v", id, err)
		writeSuccess(w, nil)
		return
	}
	writeSuccess(w, playlist)
}

// ExportPlaylist exports a playlist in JSON format
func (h *Handler) ExportPlaylist(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id := vars["id"]

	session := middleware.GetSession(r)
	if session == nil {
		writeError(w, http.StatusUnauthorized, errNotAuthenticated)
		return
	}

	format := r.URL.Query().Get("format")
	if format == "" {
		format = "json"
	}

	export, err := h.playlist.ExportPlaylist(id, session.UserID, format)
	if err != nil {
		writeError(w, http.StatusForbidden, "Cannot export playlist")
		return
	}

	// Serve M3U format as plain text download
	if (format == "m3u" || format == "m3u8") && export.M3UContent != "" {
		ext := format
		w.Header().Set(headerContentDisposition, "attachment; filename=\""+export.Name+"."+ext+"\"")
		w.Header().Set(headerContentType, "audio/x-mpegurl")
		if _, err := w.Write([]byte(export.M3UContent)); err != nil {
			h.log.Error("Failed to write M3U content: %v", err)
		}
		return
	}

	// Default: JSON format
	w.Header().Set(headerContentDisposition, "attachment; filename=\""+export.Name+".json\"")
	writeSuccess(w, export)
}

// AddPlaylistItem adds an item to a playlist
func (h *Handler) AddPlaylistItem(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	playlistID := vars["id"]

	session := middleware.GetSession(r)
	if session == nil {
		writeError(w, http.StatusUnauthorized, errNotAuthenticated)
		return
	}

	var req struct {
		MediaID   string `json:"media_id"`
		MediaPath string `json:"media_path"`
		Title     string `json:"title"`
		Path      string `json:"path"`
		Name      string `json:"name"`
	}
	if json.NewDecoder(r.Body).Decode(&req) != nil {
		writeError(w, http.StatusBadRequest, errInvalidRequest)
		return
	}

	// Accept both frontend field names (path/name) and backend canonical names (media_path/title)
	mediaPath := req.MediaPath
	if mediaPath == "" {
		mediaPath = req.Path
	}
	title := req.Title
	if title == "" {
		title = req.Name
	}

	if err := h.playlist.AddItem(r.Context(), playlistID, session.UserID, req.MediaID, mediaPath, title); err != nil {
		writeError(w, http.StatusForbidden, "Cannot add item to playlist")
		return
	}

	writeSuccess(w, nil)
}

// RemovePlaylistItem removes an item from a playlist
func (h *Handler) RemovePlaylistItem(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	playlistID := vars["id"]

	session := middleware.GetSession(r)
	if session == nil {
		writeError(w, http.StatusUnauthorized, errNotAuthenticated)
		return
	}

	// Read media path from request body (frontend sends JSON body with DELETE)
	var req struct {
		ItemID    string `json:"item_id"`
		MediaPath string `json:"media_path"`
		Path      string `json:"path"`
	}
	if json.NewDecoder(r.Body).Decode(&req) != nil {
		// Fall back to query params for backwards compatibility
		req.MediaPath = r.URL.Query().Get("media_path")
		if req.MediaPath == "" {
			req.Path = r.URL.Query().Get("path")
		}
	}

	// Use whichever field is provided: item_id, media_path, or path
	mediaPath := req.MediaPath
	if mediaPath == "" {
		mediaPath = req.ItemID
	}
	if mediaPath == "" {
		mediaPath = req.Path
	}

	if mediaPath == "" {
		writeError(w, http.StatusBadRequest, "media_path, item_id, or path required")
		return
	}

	if err := h.playlist.RemoveItem(r.Context(), playlistID, session.UserID, mediaPath); err != nil {
		writeError(w, http.StatusForbidden, "Cannot remove item from playlist")
		return
	}

	writeSuccess(w, nil)
}

// Analytics Handlers

// GetAnalyticsSummary returns analytics summary with top viewed and recent activity
func (h *Handler) GetAnalyticsSummary(w http.ResponseWriter, r *http.Request) {
	if h.analytics == nil {
		writeSuccess(w, map[string]interface{}{"analytics_disabled": true})
		return
	}
	summary := h.analytics.GetSummary(r.Context())
	globalStats := h.analytics.GetStats()

	// Build top_viewed list from analytics data
	topMedia := h.analytics.GetTopMedia(10)
	topViewed := make([]map[string]interface{}, 0, len(topMedia))
	for _, item := range topMedia {
		// Try to resolve the media filename from the media module
		filename := item.MediaID
		if mediaItem, err := h.media.GetMedia(item.MediaID); err == nil && mediaItem != nil {
			filename = mediaItem.Name
		}
		topViewed = append(topViewed, map[string]interface{}{
			"media_id": item.MediaID,
			"filename": filename,
			"views":    item.Views,
		})
	}

	// Build recent_activity from recent events
	recentEvents := h.analytics.GetRecentEvents(r.Context(), 20)
	recentActivity := make([]map[string]interface{}, 0, len(recentEvents))
	for _, event := range recentEvents {
		filename := event.MediaID
		if mediaItem, err := h.media.GetMedia(event.MediaID); err == nil && mediaItem != nil {
			filename = mediaItem.Name
		}
		recentActivity = append(recentActivity, map[string]interface{}{
			"type":      event.Type,
			"media_id":  event.MediaID,
			"filename":  filename,
			"timestamp": event.Timestamp.Unix(),
		})
	}

	writeSuccess(w, map[string]interface{}{
		"total_events":    summary.TotalEvents,
		"active_sessions": summary.ActiveSessions,
		"today_views":     summary.TodayViews,
		"total_views":     summary.TotalViews,
		"total_media":     summary.TotalMedia,
		"unique_clients":  globalStats.UniqueClients,
		"top_viewed":      topViewed,
		"recent_activity": recentActivity,
	})
}

// GetDailyStats returns daily statistics
func (h *Handler) GetDailyStats(w http.ResponseWriter, r *http.Request) {
	if h.analytics == nil {
		writeSuccess(w, []interface{}{})
		return
	}
	days := 30
	if d, err := strconv.Atoi(r.URL.Query().Get("days")); err == nil && d > 0 && d <= 365 {
		days = d
	}

	stats := h.analytics.GetDailyStats(days)
	if stats == nil {
		stats = make([]*models.DailyStats, 0)
	}
	// Ensure TopMedia is never null in JSON (nil slice → empty array)
	for _, s := range stats {
		if s.TopMedia == nil {
			s.TopMedia = make([]string, 0)
		}
	}
	writeSuccess(w, stats)
}

// GetTopMedia returns top viewed media
func (h *Handler) GetTopMedia(w http.ResponseWriter, r *http.Request) {
	if h.analytics == nil {
		writeSuccess(w, []interface{}{})
		return
	}
	limit := 10
	if l, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && l > 0 && l <= 500 {
		limit = l
	}

	top := h.analytics.GetTopMedia(limit)
	// Enrich results with filenames from media module
	enriched := make([]map[string]interface{}, 0, len(top))
	for _, item := range top {
		filename := item.MediaID
		mediaPath := ""
		if mediaItem, err := h.media.GetMedia(item.MediaID); err == nil && mediaItem != nil {
			filename = mediaItem.Name
			mediaPath = mediaItem.Path
		}
		entry := map[string]interface{}{
			"media_id": item.MediaID,
			"filename": filename,
			"views":    item.Views,
		}
		if mediaPath != "" {
			entry["media_path"] = mediaPath
		}
		enriched = append(enriched, entry)
	}
	writeSuccess(w, enriched)
}

// SubmitEvent receives and processes analytics events from clients
func (h *Handler) SubmitEvent(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Type      string                 `json:"type"`
		MediaID   string                 `json:"media_id"`
		SessionID string                 `json:"session_id"`
		Duration  float64                `json:"duration"`
		Data      map[string]interface{} `json:"data"`
	}
	if json.NewDecoder(r.Body).Decode(&req) != nil {
		writeError(w, http.StatusBadRequest, errInvalidRequest)
		return
	}
	// Merge duration into data map if provided
	if req.Duration > 0 {
		if req.Data == nil {
			req.Data = make(map[string]interface{})
		}
		if _, exists := req.Data["duration"]; !exists {
			req.Data["duration"] = req.Duration
		}
	}

	session := middleware.GetSession(r)
	userID := ""
	// Prefer server-side session ID (authoritative); fall back to client-supplied value only
	// if present, which allows future clients to correlate events with a specific session.
	sessionID := req.SessionID
	if session != nil {
		userID = session.UserID
		if sessionID == "" {
			sessionID = session.ID
		}
	}

	if h.analytics != nil {
		h.analytics.SubmitClientEvent(r.Context(),
			req.Type,
			req.MediaID,
			userID,
			sessionID,
			middleware.GetClientIP(r),
			r.UserAgent(),
			req.Data,
		)
	}

	// Wire completion events to the suggestions engine so it learns user behaviour (DC-02)
	if req.Type == "complete" && h.suggestions != nil && req.MediaID != "" && userID != "" {
		h.suggestions.RecordCompletion(userID, req.MediaID)
	}

	writeSuccess(w, map[string]string{"status": "recorded"})
}

// GetEventStats returns detailed event statistics
func (h *Handler) GetEventStats(w http.ResponseWriter, r *http.Request) {
	if h.analytics == nil {
		writeSuccess(w, map[string]interface{}{})
		return
	}
	stats := h.analytics.GetEventStats(r.Context())
	writeSuccess(w, stats)
}

// GetEventsByType returns events filtered by type
func (h *Handler) GetEventsByType(w http.ResponseWriter, r *http.Request) {
	eventType := r.URL.Query().Get("type")
	limit := 100
	if l, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && l > 0 && l <= 1000 {
		limit = l
	}

	if h.analytics == nil {
		writeSuccess(w, []interface{}{})
		return
	}
	events := h.analytics.GetEventsByType(r.Context(), eventType, limit)
	writeSuccess(w, events)
}

// GetEventsByMedia returns events for a specific media item
func (h *Handler) GetEventsByMedia(w http.ResponseWriter, r *http.Request) {
	if h.analytics == nil {
		writeSuccess(w, []interface{}{})
		return
	}
	mediaID := r.URL.Query().Get("media_id")
	if mediaID == "" {
		writeError(w, http.StatusBadRequest, "media_id parameter required")
		return
	}
	limit := 100
	if l, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && l > 0 && l <= 1000 {
		limit = l
	}

	events := h.analytics.GetEventsByMedia(r.Context(), mediaID, limit)
	writeSuccess(w, events)
}

// GetEventTypeCounts returns counts of each event type
func (h *Handler) GetEventTypeCounts(w http.ResponseWriter, r *http.Request) {
	if h.analytics == nil {
		writeSuccess(w, map[string]interface{}{})
		return
	}
	counts := h.analytics.GetEventTypeCounts(r.Context())
	writeSuccess(w, counts)
}

// Admin Handlers

// AdminGetStats returns admin statistics.
func (h *Handler) AdminGetStats(w http.ResponseWriter, r *http.Request) {
	adminStats := h.admin.GetServerStats()
	mediaStats := h.media.GetStats()
	streamStats := h.streaming.GetStats()
	hlsStats := h.hls.GetStats()

	totalUsers := len(h.auth.ListUsers(r.Context()))

	var totalViews int
	if h.analytics != nil {
		totalViews = h.analytics.GetStats().TotalViews
	}

	// Disk space from the media directory (best-effort)
	var diskTotal, diskFree uint64
	cfg := h.media.GetConfig()
	if cfg.Directories.Videos != "" {
		if du, err := helpers.GetDiskUsage(cfg.Directories.Videos); err == nil {
			diskTotal = du.Total
			diskFree = du.Available
		}
	}
	var diskUsed uint64
	if diskTotal > diskFree {
		diskUsed = diskTotal - diskFree
	}

	writeSuccess(w, map[string]interface{}{
		"total_videos":       mediaStats.VideoCount,
		"total_audio":        mediaStats.AudioCount,
		"active_sessions":    streamStats.ActiveStreams,
		"total_users":        totalUsers,
		"disk_usage":         diskUsed,
		"disk_total":         diskTotal,
		"disk_free":          diskFree,
		"hls_jobs_running":   hlsStats.RunningJobs,
		"hls_jobs_completed": hlsStats.CompletedJobs,
		"server_uptime":      int64(adminStats.Uptime.Seconds()),
		"total_views":        totalViews,
	})
}

// AdminListUsers returns all users
func (h *Handler) AdminListUsers(w http.ResponseWriter, r *http.Request) {
	users := h.auth.ListUsers(r.Context())
	writeSuccess(w, users)
}

// AdminCreateUser creates a user
func (h *Handler) AdminCreateUser(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string          `json:"username"`
		Password string          `json:"password"`
		Email    string          `json:"email"`
		Type     string          `json:"type"`
		Role     models.UserRole `json:"role"`
	}
	if json.NewDecoder(r.Body).Decode(&req) != nil {
		writeError(w, http.StatusBadRequest, errInvalidRequest)
		return
	}

	req.Username = strings.TrimSpace(req.Username)
	if len(req.Username) < 3 || len(req.Username) > 64 {
		writeError(w, http.StatusBadRequest, "Username must be between 3 and 64 characters")
		return
	}
	for _, c := range req.Username {
		if (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') && (c < '0' || c > '9') && c != '_' && c != '-' {
			writeError(w, http.StatusBadRequest, "Username may only contain letters, numbers, underscores, and hyphens")
			return
		}
	}
	if len(req.Password) < 8 {
		writeError(w, http.StatusBadRequest, "Password must be at least 8 characters")
		return
	}

	if req.Type == "" {
		req.Type = "standard"
	}
	user, err := h.auth.CreateUser(r.Context(), req.Username, req.Password, req.Email, req.Type, req.Role)
	if err != nil {
		if errors.Is(err, auth.ErrUserExists) {
			writeError(w, http.StatusConflict, "Username is already taken")
			return
		}
		h.log.Error("Failed to create user %s: %v", req.Username, err)
		writeError(w, http.StatusInternalServerError, "Internal server error")
		return
	}

	h.admin.LogAction(r.Context(), "admin", "admin", "create_user", req.Username, nil, middleware.GetClientIP(r), true)

	writeSuccess(w, user)
}

// AdminDeleteUser deletes a user
func (h *Handler) AdminDeleteUser(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	username := vars["username"]

	if err := h.auth.DeleteUser(r.Context(), username); err != nil {
		writeError(w, http.StatusNotFound, "User not found")
		return
	}

	h.admin.LogAction(r.Context(), "admin", "admin", "delete_user", username, nil, middleware.GetClientIP(r), true)
	writeSuccess(w, nil)
}

// AdminGetUser returns a single user's details
func (h *Handler) AdminGetUser(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	username := vars["username"]

	user, err := h.auth.GetUser(r.Context(), username)
	if err != nil {
		writeError(w, http.StatusNotFound, errUserNotFound)
		return
	}

	writeSuccess(w, user)
}

// AdminUpdateUser updates a user's details
func (h *Handler) AdminUpdateUser(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	username := vars["username"]

	var updates map[string]interface{}
	if json.NewDecoder(r.Body).Decode(&updates) != nil {
		writeError(w, http.StatusBadRequest, errInvalidRequest)
		return
	}

	if err := h.auth.UpdateUser(r.Context(), username, updates); err != nil {
		h.log.Error("%v", err)
		writeError(w, http.StatusInternalServerError, "Internal server error")
		return
	}

	h.admin.LogAction(r.Context(), "admin", "admin", "update_user", username, updates, middleware.GetClientIP(r), true)

	// Return updated user
	user, err := h.auth.GetUser(r.Context(), username)
	if err != nil {
		h.log.Error("Failed to fetch updated user %s: %v", username, err)
		writeSuccess(w, map[string]string{"message": "User updated"})
		return
	}
	writeSuccess(w, user)
}

// AdminBulkUsers performs a bulk action (delete, enable, disable) on multiple users.
// POST /api/admin/users/bulk
// Body: { usernames: string[], action: "delete"|"enable"|"disable" }
// Returns: { success: int, failed: int, errors: []string }
func (h *Handler) AdminBulkUsers(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Usernames []string `json:"usernames"`
		Action    string   `json:"action"`
	}
	if json.NewDecoder(r.Body).Decode(&req) != nil {
		writeError(w, http.StatusBadRequest, errInvalidRequest)
		return
	}
	if len(req.Usernames) == 0 {
		writeError(w, http.StatusBadRequest, "usernames must not be empty")
		return
	}
	if len(req.Usernames) > 200 {
		writeError(w, http.StatusBadRequest, "too many usernames (max 200)")
		return
	}
	if req.Action != "delete" && req.Action != "enable" && req.Action != "disable" {
		writeError(w, http.StatusBadRequest, `action must be "delete", "enable", or "disable"`)
		return
	}

	var successCount, failedCount int
	var errs []string
	clientIP := middleware.GetClientIP(r)

	for _, username := range req.Usernames {
		if username == "" || username == "admin" {
			continue // never bulk-modify the built-in admin account
		}
		var opErr error
		switch req.Action {
		case "delete":
			opErr = h.auth.DeleteUser(r.Context(), username)
			if opErr == nil {
				h.admin.LogAction(r.Context(), "admin", "admin", "bulk_delete_user", username, nil, clientIP, true)
			}
		case "enable":
			opErr = h.auth.UpdateUser(r.Context(), username, map[string]interface{}{"enabled": true})
			if opErr == nil {
				h.admin.LogAction(r.Context(), "admin", "admin", "bulk_enable_user", username, nil, clientIP, true)
			}
		case "disable":
			opErr = h.auth.UpdateUser(r.Context(), username, map[string]interface{}{"enabled": false})
			if opErr == nil {
				h.admin.LogAction(r.Context(), "admin", "admin", "bulk_disable_user", username, nil, clientIP, true)
			}
		}
		if opErr != nil {
			h.log.Error("bulk %s user %s: %v", req.Action, username, opErr)
			failedCount++
			errs = append(errs, fmt.Sprintf("%s: %v", username, opErr))
		} else {
			successCount++
		}
	}

	if errs == nil {
		errs = []string{}
	}
	writeSuccess(w, map[string]interface{}{
		"success": successCount,
		"failed":  failedCount,
		"errors":  errs,
	})
}

// ChangePassword allows a user to change their own password.
// Admin sessions are routed through the config-based admin password path.
func (h *Handler) ChangePassword(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r)
	if user == nil {
		writeError(w, http.StatusUnauthorized, errNotAuthenticated)
		return
	}

	var req struct {
		CurrentPassword string `json:"current_password"`
		NewPassword     string `json:"new_password"`
	}
	if json.NewDecoder(r.Body).Decode(&req) != nil {
		writeError(w, http.StatusBadRequest, errInvalidRequest)
		return
	}

	if req.CurrentPassword == "" || req.NewPassword == "" {
		writeError(w, http.StatusBadRequest, "Current and new password required")
		return
	}

	if len(req.NewPassword) < 8 {
		writeError(w, http.StatusBadRequest, "New password must be at least 8 characters")
		return
	}

	// Admin account uses a config-stored bcrypt hash, not the user-repo password.
	// Route admin password changes through the dedicated auth method.
	if user.Role == models.RoleAdmin && user.ID == "admin" {
		if err := h.auth.ChangeAdminPassword(r.Context(), req.CurrentPassword, req.NewPassword); err != nil {
			writeError(w, http.StatusUnauthorized, "Current password is incorrect")
			return
		}
		writeSuccess(w, map[string]string{"status": "password_changed"})
		return
	}

	// Regular user: verify current password then update
	if h.auth.VerifyPassword(user.Username, req.CurrentPassword) != nil {
		writeError(w, http.StatusUnauthorized, "Current password is incorrect")
		return
	}

	if err := h.auth.SetPassword(r.Context(), user.Username, req.NewPassword); err != nil {
		h.log.Error("%v", err)
		writeError(w, http.StatusInternalServerError, "Internal server error")
		return
	}

	writeSuccess(w, map[string]string{"status": "password_changed"})
}

// AdminChangeOwnPassword lets an admin change the admin account password directly
// from the admin panel (no current-password required — admin is already authenticated).
func (h *Handler) AdminChangeOwnPassword(w http.ResponseWriter, r *http.Request) {
	var req struct {
		CurrentPassword string `json:"current_password"`
		NewPassword     string `json:"new_password"`
	}
	if json.NewDecoder(r.Body).Decode(&req) != nil {
		writeError(w, http.StatusBadRequest, errInvalidRequest)
		return
	}

	if req.CurrentPassword == "" || req.NewPassword == "" {
		writeError(w, http.StatusBadRequest, "Current and new password required")
		return
	}

	if len(req.NewPassword) < 8 {
		writeError(w, http.StatusBadRequest, "New password must be at least 8 characters")
		return
	}

	if err := h.auth.ChangeAdminPassword(r.Context(), req.CurrentPassword, req.NewPassword); err != nil {
		writeError(w, http.StatusUnauthorized, "Current password is incorrect")
		return
	}

	h.admin.LogAction(r.Context(), "admin", "admin", "change_admin_password", "", nil, middleware.GetClientIP(r), true)
	writeSuccess(w, map[string]string{"status": "password_changed"})
}

// DeleteAccount allows an authenticated user to permanently delete their own account.
// The user must confirm with their current password. All sessions for the user are
// invalidated before deletion. Admin accounts cannot use this endpoint.
func (h *Handler) DeleteAccount(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r)
	if user == nil {
		writeError(w, http.StatusUnauthorized, errNotAuthenticated)
		return
	}

	// Admins should not self-delete via this endpoint to prevent accidental lockout
	if user.Role == "admin" {
		writeError(w, http.StatusForbidden, "Admin accounts cannot be deleted via this endpoint")
		return
	}

	var req struct {
		Password string `json:"password"`
	}
	if json.NewDecoder(r.Body).Decode(&req) != nil {
		writeError(w, http.StatusBadRequest, errInvalidRequest)
		return
	}

	if req.Password == "" {
		writeError(w, http.StatusBadRequest, "Password confirmation required")
		return
	}

	// Verify password before deleting
	if h.auth.VerifyPassword(user.Username, req.Password) != nil {
		writeError(w, http.StatusUnauthorized, "Incorrect password")
		return
	}

	// Invalidate the current session before deleting the account
	session := middleware.GetSession(r)
	if session != nil {
		if err := h.auth.Logout(r.Context(), session.ID); err != nil {
			h.log.Warn("Failed to invalidate session before account deletion for %s: %v", user.Username, err)
		}
		// Clear session cookie
		http.SetCookie(w, &http.Cookie{
			Name:     "session_id",
			Value:    "",
			Path:     "/",
			MaxAge:   -1,
			HttpOnly: true,
			SameSite: http.SameSiteLaxMode,
			Secure:   isSecureRequest(r),
		})
	}

	if err := h.auth.DeleteUser(r.Context(), user.Username); err != nil {
		h.log.Error("Failed to delete account: %v", err)
		writeError(w, http.StatusInternalServerError, "Failed to delete account")
		return
	}

	h.log.Info("User %s deleted their account", user.Username)
	writeSuccess(w, map[string]string{"status": "account_deleted", "message": "Your account has been permanently deleted"})
}

// AdminChangePassword changes a user's password (admin action)
func (h *Handler) AdminChangePassword(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	username := vars["username"]

	var req struct {
		NewPassword string `json:"new_password"`
	}
	if json.NewDecoder(r.Body).Decode(&req) != nil {
		writeError(w, http.StatusBadRequest, errInvalidRequest)
		return
	}

	if req.NewPassword == "" {
		writeError(w, http.StatusBadRequest, "New password required")
		return
	}

	if len(req.NewPassword) < 8 {
		writeError(w, http.StatusBadRequest, "New password must be at least 8 characters")
		return
	}

	// Admin can set password without knowing old password
	if err := h.auth.SetPassword(r.Context(), username, req.NewPassword); err != nil {
		h.log.Error("%v", err)
		writeError(w, http.StatusInternalServerError, "Internal server error")
		return
	}

	h.admin.LogAction(r.Context(), "admin", "admin", "change_password", username, nil, middleware.GetClientIP(r), true)
	writeSuccess(w, map[string]string{"status": "password_changed"})
}

// AdminGetAuditLog returns audit log
func (h *Handler) AdminGetAuditLog(w http.ResponseWriter, r *http.Request) {
	limit := 100
	offset := 0
	if l, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && l > 0 && l <= 1000 {
		limit = l
	}
	if o, err := strconv.Atoi(r.URL.Query().Get("offset")); err == nil && o >= 0 {
		offset = o
	}

	log := h.admin.GetAuditLog(r.Context(), limit, offset)
	writeSuccess(w, log)
}

// AdminExportAuditLog exports the audit log as a CSV file download
func (h *Handler) AdminExportAuditLog(w http.ResponseWriter, r *http.Request) {
	filename, err := h.admin.ExportAuditLog(r.Context())
	if err != nil {
		h.log.Error("%v", err)
		writeError(w, http.StatusInternalServerError, "Internal server error")
		return
	}

	w.Header().Set(headerContentDisposition, fmt.Sprintf("attachment; filename=%q", filepath.Base(filename)))
	w.Header().Set(headerContentType, "text/csv")
	http.ServeFile(w, r, filename)
}

// GetServerLogs reads recent entries from the server log files.
// Processes at most 50 log files (newest first) and respects the caller's line limit.
func (h *Handler) GetServerLogs(w http.ResponseWriter, r *http.Request) {
	limit := 200
	if l, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && l > 0 && l <= 2000 {
		limit = l
	}

	cfg := h.media.GetConfig()
	logsDir := cfg.Directories.Logs
	if logsDir == "" {
		logsDir = "logs"
	}

	// Find all log files, sorted newest first
	entries, err := os.ReadDir(logsDir)
	if err != nil {
		// If no log files, return empty array
		writeSuccess(w, []interface{}{})
		return
	}

	// Sort log files by name descending (newest first since they use date format)
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Name() > entries[j].Name()
	})

	var logLines []map[string]interface{}

	const maxLogFiles = 50
	filesProcessed := 0
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".log") {
			continue
		}

		filePath := filepath.Join(logsDir, entry.Name())
		lines, readErr := readLastNLines(filePath, limit-len(logLines))
		if readErr != nil {
			h.log.Debug("Failed to read log file %s: %v", filePath, readErr)
			continue
		}

		for _, line := range lines {
			logEntry := parseLogLine(line)
			logLines = append(logLines, logEntry)
		}

		filesProcessed++
		if len(logLines) >= limit || filesProcessed >= maxLogFiles {
			break
		}
	}

	// Reverse so newest is last (chronological order)
	for i, j := 0, len(logLines)-1; i < j; i, j = i+1, j-1 {
		logLines[i], logLines[j] = logLines[j], logLines[i]
	}

	// Filter by level and/or module if query params are present
	levelFilter := strings.ToLower(r.URL.Query().Get("level"))
	moduleFilter := strings.ToLower(r.URL.Query().Get("module"))
	if levelFilter != "" || moduleFilter != "" {
		filtered := logLines[:0]
		for _, entry := range logLines {
			if levelFilter != "" {
				entryLevel, _ := entry["level"].(string)
				if strings.ToLower(entryLevel) != levelFilter {
					continue
				}
			}
			if moduleFilter != "" {
				entryModule, _ := entry["module"].(string)
				if !strings.Contains(strings.ToLower(entryModule), moduleFilter) {
					continue
				}
			}
			filtered = append(filtered, entry)
		}
		logLines = filtered
	}

	writeSuccess(w, logLines)
}

// readLastNLines reads the last N lines from a file
func readLastNLines(filePath string, n int) ([]string, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = file.Close()
	}()

	var lines []string
	sc := bufio.NewScanner(file)
	for sc.Scan() {
		lines = append(lines, sc.Text())
	}

	// Return last n lines
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return lines, sc.Err()
}

// parseLogLine parses a server log line into a structured entry
func parseLogLine(line string) map[string]interface{} {
	entry := map[string]interface{}{
		"raw":       line,
		"timestamp": "",
		"level":     "info",
		"module":    "",
		"message":   line,
	}

	// Parse format: [2006-01-02 15:04:05.000] [LEVEL] [module] [caller] message
	if len(line) > 25 && line[0] == '[' {
		// Extract timestamp
		if idx := strings.Index(line[1:], "]"); idx > 0 {
			entry["timestamp"] = line[1 : idx+1]
			rest := strings.TrimSpace(line[idx+2:])

			// Extract level
			if len(rest) > 0 && rest[0] == '[' {
				if idx2 := strings.Index(rest[1:], "]"); idx2 > 0 {
					level := strings.TrimSpace(rest[1 : idx2+1])
					entry["level"] = strings.ToLower(level)
					rest = strings.TrimSpace(rest[idx2+2:])
				}
			}

			// Extract module
			if len(rest) > 0 && rest[0] == '[' {
				if idx3 := strings.Index(rest[1:], "]"); idx3 > 0 {
					entry["module"] = rest[1 : idx3+1]
					rest = strings.TrimSpace(rest[idx3+2:])
				}
			}

			// Skip caller info
			if len(rest) > 0 && rest[0] == '[' {
				if idx4 := strings.Index(rest[1:], "]"); idx4 > 0 {
					rest = strings.TrimSpace(rest[idx4+2:])
				}
			}

			entry["message"] = rest
		}
	}

	return entry
}

// AdminExportAnalytics exports analytics data as a CSV file download.
// Accepts optional query params: start_date (YYYY-MM-DD), end_date (YYYY-MM-DD).
// Defaults to the last 30 days when not provided. (IC-07)
func (h *Handler) AdminExportAnalytics(w http.ResponseWriter, r *http.Request) {
	endDate := time.Now()
	startDate := endDate.AddDate(0, -1, 0) // Default: last month

	if v := r.URL.Query().Get("start_date"); v != "" {
		if t, err := time.Parse("2006-01-02", v); err == nil {
			startDate = t
		}
	}
	if v := r.URL.Query().Get("end_date"); v != "" {
		if t, err := time.Parse("2006-01-02", v); err == nil {
			endDate = t
		}
	}

	filename, err := h.analytics.ExportCSV(r.Context(), startDate, endDate)
	if err != nil {
		h.log.Error("%v", err)
		writeError(w, http.StatusInternalServerError, "Internal server error")
		return
	}

	w.Header().Set(headerContentDisposition, fmt.Sprintf("attachment; filename=%q", filepath.Base(filename)))
	w.Header().Set(headerContentType, "text/csv")
	http.ServeFile(w, r, filename)
}

// AdminGetConfig returns the current configuration
func (h *Handler) AdminGetConfig(w http.ResponseWriter, _ *http.Request) {
	config := h.admin.GetConfigMap()
	writeSuccess(w, config)
}

// AdminUpdateConfig updates the configuration
func (h *Handler) AdminUpdateConfig(w http.ResponseWriter, r *http.Request) {
	var updates map[string]interface{}
	if json.NewDecoder(r.Body).Decode(&updates) != nil {
		writeError(w, http.StatusBadRequest, errInvalidRequest)
		return
	}

	if err := h.admin.UpdateConfig(updates); err != nil {
		h.log.Error("%v", err)
		writeError(w, http.StatusInternalServerError, "Internal server error")
		return
	}

	h.admin.LogAction(r.Context(), "admin", "admin", "update_config", "configuration", updates, middleware.GetClientIP(r), true)
	writeSuccess(w, h.admin.GetConfigMap())
}

// AdminListTasks returns scheduled tasks
func (h *Handler) AdminListTasks(w http.ResponseWriter, _ *http.Request) {
	taskList := h.tasks.ListTasks()
	writeSuccess(w, taskList)
}

// AdminRunTask runs a task immediately
func (h *Handler) AdminRunTask(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	taskID := vars["id"]

	if err := h.tasks.RunNow(taskID); err != nil {
		writeError(w, http.StatusNotFound, "Task not found")
		return
	}

	writeSuccess(w, map[string]string{"message": "Task started"})
}

// AdminEnableTask enables a background task
func (h *Handler) AdminEnableTask(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	taskID := vars["id"]

	if err := h.tasks.EnableTask(taskID); err != nil {
		writeError(w, http.StatusNotFound, "Task not found")
		return
	}

	writeSuccess(w, map[string]string{"message": "Task enabled"})
}

// AdminDisableTask disables a background task
func (h *Handler) AdminDisableTask(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	taskID := vars["id"]

	if err := h.tasks.DisableTask(taskID); err != nil {
		writeError(w, http.StatusNotFound, "Task not found")
		return
	}

	writeSuccess(w, map[string]string{"message": "Task disabled"})
}

// AdminStopTask force-cancels a running task without disabling future runs
func (h *Handler) AdminStopTask(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	taskID := vars["id"]

	if err := h.tasks.StopTask(taskID); err != nil {
		writeError(w, http.StatusBadRequest, "Cannot stop task")
		return
	}

	writeSuccess(w, map[string]string{"message": "Task stopped"})
}

// AdminListPlaylists returns all playlists for admin with optional search and pagination.
// Query params: search (substring match on name), page (1-based), limit (default 100).
func (h *Handler) AdminListPlaylists(w http.ResponseWriter, r *http.Request) {
	all := h.playlist.ListAllPlaylists()

	search := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("search")))
	limitStr := r.URL.Query().Get("limit")
	pageStr := r.URL.Query().Get("page")

	limit := 100
	if limitStr != "" {
		if v, err := strconv.Atoi(limitStr); err == nil && v > 0 {
			limit = v
		}
	}
	page := 1
	if pageStr != "" {
		if v, err := strconv.Atoi(pageStr); err == nil && v > 0 {
			page = v
		}
	}

	filtered := all
	if search != "" {
		kept := all[:0]
		for _, p := range all {
			if strings.Contains(strings.ToLower(p.Name), search) {
				kept = append(kept, p)
			}
		}
		filtered = kept
	}

	start := (page - 1) * limit
	if start >= len(filtered) {
		writeSuccess(w, []*models.Playlist{})
		return
	}
	end := start + limit
	if end > len(filtered) {
		end = len(filtered)
	}

	writeSuccess(w, filtered[start:end])
}

// AdminPlaylistStats returns playlist statistics
func (h *Handler) AdminPlaylistStats(w http.ResponseWriter, _ *http.Request) {
	stats := h.playlist.GetStats()
	writeSuccess(w, stats)
}

// AdminDeletePlaylist deletes a playlist as admin
func (h *Handler) AdminDeletePlaylist(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	playlistID := vars["id"]

	if err := h.playlist.AdminDeletePlaylist(r.Context(), playlistID); err != nil {
		writeError(w, http.StatusNotFound, "Playlist not found")
		return
	}

	writeSuccess(w, map[string]string{"message": "Playlist deleted"})
}

// AdminBulkDeletePlaylists deletes multiple playlists by ID.
// POST /api/admin/playlists/bulk
// Body: { ids: string[] }
// Returns: { success: int, failed: int, errors: []string }
func (h *Handler) AdminBulkDeletePlaylists(w http.ResponseWriter, r *http.Request) {
	var req struct {
		IDs []string `json:"ids"`
	}
	if json.NewDecoder(r.Body).Decode(&req) != nil {
		writeError(w, http.StatusBadRequest, errInvalidRequest)
		return
	}
	if len(req.IDs) == 0 {
		writeError(w, http.StatusBadRequest, "ids must not be empty")
		return
	}
	if len(req.IDs) > 500 {
		writeError(w, http.StatusBadRequest, "too many ids (max 500)")
		return
	}

	var successCount, failedCount int
	var errs []string
	for _, id := range req.IDs {
		if id == "" {
			continue
		}
		if err := h.playlist.AdminDeletePlaylist(r.Context(), id); err != nil {
			failedCount++
			errs = append(errs, fmt.Sprintf("%s: %v", id, err))
		} else {
			successCount++
		}
	}
	if errs == nil {
		errs = []string{}
	}
	h.admin.LogAction(r.Context(), "admin", "admin", "bulk_delete_playlists",
		fmt.Sprintf("%d playlists", successCount), nil, middleware.GetClientIP(r), failedCount == 0)
	writeSuccess(w, map[string]interface{}{
		"success": successCount,
		"failed":  failedCount,
		"errors":  errs,
	})
}

// AdminGetSystemInfo returns system information shaped for the frontend SystemInfo type.
func (h *Handler) AdminGetSystemInfo(w http.ResponseWriter, _ *http.Request) {
	info := h.admin.GetSystemInfo()
	uptimeSecs := h.admin.GetUptimeSecs()

	// Collect health from all registered modules. A local interface avoids importing the server package.
	type healthier interface {
		Health() models.HealthStatus
	}
	allModules := []healthier{
		h.security, h.database, h.auth, h.media, h.streaming, h.hls,
		h.analytics, h.playlist, h.admin, h.tasks, h.upload, h.scanner,
		h.thumbnails, h.validator, h.backup, h.autodiscovery, h.suggestions,
		h.categorizer, h.updater, h.remote,
	}
	type moduleHealthItem struct {
		Name      string `json:"name"`
		Status    string `json:"status"`
		Message   string `json:"message,omitempty"`
		LastCheck string `json:"last_check,omitempty"`
	}
	moduleHealths := make([]moduleHealthItem, 0, len(allModules))
	for _, p := range allModules {
		hs := p.Health()
		moduleHealths = append(moduleHealths, moduleHealthItem{
			Name:      hs.Name,
			Status:    hs.Status,
			Message:   hs.Message,
			LastCheck: hs.CheckedAt.Format(time.RFC3339),
		})
	}

	writeSuccess(w, map[string]interface{}{
		"version":      h.version,
		"build_date":   h.buildDate,
		"os":           runtime.GOOS,
		"arch":         runtime.GOARCH,
		"go_version":   info.GoVersion,
		"cpu_count":    info.NumCPU,
		"memory_used":  info.MemAlloc,
		"memory_total": info.MemTotal,
		"uptime":       uptimeSecs,
		"modules":      moduleHealths,
	})
}

// Update Management Handlers

// CheckForUpdates checks GitHub for new versions
func (h *Handler) CheckForUpdates(w http.ResponseWriter, _ *http.Request) {
	result, err := h.updater.CheckForUpdates()
	if err != nil {
		// Still return the result even if there was an error
		// The error will be included in the result
		if result != nil {
			writeSuccess(w, result)
			return
		}
		h.log.Error("%v", err)
		writeError(w, http.StatusInternalServerError, "Internal server error")
		return
	}

	writeSuccess(w, result)
}

// GetUpdateStatus returns the last update check result.
// Always returns the same shape as UpdateCheckResult; when no check has been
// performed yet, update_available is false and current_version is populated.
func (h *Handler) GetUpdateStatus(w http.ResponseWriter, _ *http.Request) {
	result := h.updater.GetLastCheck()
	if result == nil {
		version := h.updater.GetVersion()
		currentVersion, _ := version["version"].(string)
		writeSuccess(w, map[string]interface{}{
			"current_version":  currentVersion,
			"latest_version":   "",
			"update_available": false,
			"checked_at":       nil,
		})
		return
	}

	writeSuccess(w, result)
}

// ApplyUpdate downloads and installs an update
func (h *Handler) ApplyUpdate(w http.ResponseWriter, r *http.Request) {
	// Guard against concurrent installs (409 Conflict like the source update does).
	if h.updater.IsUpdateRunning() {
		writeError(w, http.StatusConflict, "A binary update is already in progress")
		return
	}

	status, err := h.updater.ApplyUpdate(r.Context())
	if err != nil {
		h.log.Error("ApplyUpdate: %v", err)
		// Surface the real error so the admin sees what actually failed
		// (e.g. "no binary asset found", "auth failed", "not a valid executable").
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	h.admin.LogAction(r.Context(), "admin", "admin", "apply_update", status.Stage, nil, middleware.GetClientIP(r), status.Error == "")
	writeSuccess(w, status)
}

// ApplySourceUpdate starts an async source build (git pull → npm build → go build).
// Returns 202 Accepted immediately; poll GET /api/admin/update/source/progress for status.
// Returns 409 Conflict if a build is already running.
func (h *Handler) ApplySourceUpdate(w http.ResponseWriter, r *http.Request) {
	if h.updater.IsBuildRunning() {
		writeError(w, http.StatusConflict, "A source build is already in progress")
		return
	}
	clientIP := middleware.GetClientIP(r)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
		defer cancel()
		status, err := h.updater.SourceUpdate(ctx)
		if err != nil {
			h.log.Error("Source update failed: %v", err)
		}
		h.admin.LogAction(context.Background(), "admin", "admin", "apply_source_update",
			status.Stage, nil, clientIP, status.Error == "")
	}()
	initial := h.updater.GetActiveBuildStatus()
	if initial == nil {
		initial = &updater.UpdateStatus{InProgress: true, Stage: "starting", Progress: 0}
	}
	writeJSON(w, http.StatusAccepted, models.APIResponse{Success: true, Data: initial})
}

// GetSourceUpdateProgress returns the live progress of a running source build,
// or a completed/idle status if no build is active.
func (h *Handler) GetSourceUpdateProgress(w http.ResponseWriter, _ *http.Request) {
	status := h.updater.GetActiveBuildStatus()
	if status == nil {
		writeSuccess(w, map[string]interface{}{
			"in_progress": false,
			"stage":       "",
			"progress":    0,
		})
		return
	}
	writeSuccess(w, status)
}

// CheckForSourceUpdates fetches remote git refs and reports whether new commits
// are available on the tracked branch.
func (h *Handler) CheckForSourceUpdates(w http.ResponseWriter, r *http.Request) {
	hasUpdates, remoteHash, err := h.updater.CheckForSourceUpdates(r.Context())
	if err != nil {
		h.log.Error("Source update check failed: %v", err)
		writeError(w, http.StatusInternalServerError, "Internal server error")
		return
	}
	writeSuccess(w, map[string]interface{}{
		"updates_available": hasUpdates,
		"remote_commit":     remoteHash,
	})
}

// GetUpdateConfig returns the current updater configuration so the frontend
// can display the correct update method and branch options.
func (h *Handler) GetUpdateConfig(w http.ResponseWriter, _ *http.Request) {
	cfg := h.config.Get()
	method := cfg.Updater.UpdateMethod
	if method == "" {
		method = "source"
	}
	branch := cfg.Updater.Branch
	if branch == "" {
		branch = "main"
	}
	writeSuccess(w, map[string]interface{}{
		"update_method": method,
		"branch":        branch,
	})
}

// SetUpdateConfig updates the updater configuration (method, branch).
func (h *Handler) SetUpdateConfig(w http.ResponseWriter, r *http.Request) {
	var req struct {
		UpdateMethod string `json:"update_method"`
		Branch       string `json:"branch"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if req.UpdateMethod != "" && req.UpdateMethod != "source" && req.UpdateMethod != "binary" {
		writeError(w, http.StatusBadRequest, "update_method must be \"source\" or \"binary\"")
		return
	}

	if err := h.config.Update(func(cfg *config.Config) {
		if req.UpdateMethod != "" {
			cfg.Updater.UpdateMethod = req.UpdateMethod
		}
		if req.Branch != "" {
			cfg.Updater.Branch = req.Branch
		}
	}); err != nil {
		h.log.Error("Failed to update updater config: %v", err)
		writeError(w, http.StatusInternalServerError, "Failed to save configuration")
		return
	}

	h.admin.LogAction(r.Context(), "admin", "admin", "update_updater_config", "updater_settings",
		map[string]interface{}{"update_method": req.UpdateMethod, "branch": req.Branch},
		middleware.GetClientIP(r), true)

	cfg := h.config.Get()
	writeSuccess(w, map[string]interface{}{
		"update_method": cfg.Updater.UpdateMethod,
		"branch":        cfg.Updater.Branch,
	})
}

// RestartServer initiates a graceful server restart via self-exec.
// It resolves the current executable path and spawns a new instance with the
// same arguments before exiting the current process. If the executable path
// cannot be resolved, it falls back to a clean exit (process manager required).
func (h *Handler) RestartServer(w http.ResponseWriter, r *http.Request) {
	h.log.Warn("Server restart requested by admin")
	h.admin.LogAction(r.Context(), "admin", "admin", "restart_server", "initiated", nil, middleware.GetClientIP(r), true)

	writeSuccess(w, map[string]interface{}{
		"message": "Server restart initiated. The server will restart in a few seconds.",
		"status":  "restarting",
	})

	go func() {
		time.Sleep(1 * time.Second)

		// Under systemd (INVOCATION_ID is always set for managed services), just exit and let
		// systemd restart the process. Self-exec would create an orphan that systemd doesn't track.
		if os.Getenv("INVOCATION_ID") != "" {
			h.log.Info("Running under systemd — exiting for service manager restart")
			os.Exit(0)
			return
		}

		h.log.Info("Initiating server restart via self-exec...")

		exe, err := os.Executable()
		if err != nil {
			h.log.Error("Failed to resolve executable path for restart: %v — falling back to exit", err)
			os.Exit(0)
			return
		}

		exe, err = filepath.EvalSymlinks(exe)
		if err != nil {
			h.log.Error("Failed to evaluate symlinks for restart: %v — falling back to exit", err)
			os.Exit(0)
			return
		}

		cmd := exec.Command(exe, os.Args[1:]...)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		cmd.Stdin = os.Stdin

		if err := cmd.Start(); err != nil {
			h.log.Error("Failed to start replacement process: %v — falling back to exit", err)
			os.Exit(1)
			return
		}

		h.log.Info("Replacement process started (PID %d), exiting current instance", cmd.Process.Pid)
		os.Exit(0)
	}()
}

// ShutdownServer initiates a graceful server shutdown
func (h *Handler) ShutdownServer(w http.ResponseWriter, r *http.Request) {
	h.log.Warn("Server shutdown requested by admin")
	h.admin.LogAction(r.Context(), "admin", "admin", "shutdown_server", "initiated", nil, middleware.GetClientIP(r), true)

	writeSuccess(w, map[string]interface{}{
		"message": "Server shutdown initiated. The server will shut down in a few seconds.",
		"status":  "shutting_down",
	})

	go func() {
		time.Sleep(1 * time.Second)
		h.log.Info("Initiating server shutdown...")
		os.Exit(0)
	}()
}

// AdminListMedia returns media items for admin management with optional search and pagination.
// Returns a flat MediaItem array (not wrapped) so the frontend can use it directly.
func (h *Handler) AdminListMedia(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	filter := media.Filter{
		Search: q.Get("search"),
	}
	limit := 50
	if l, err := strconv.Atoi(q.Get("limit")); err == nil && l > 0 {
		limit = l
	}
	filter.Limit = limit
	if p, err := strconv.Atoi(q.Get("page")); err == nil && p > 1 {
		filter.Offset = (p - 1) * limit
	}

	items := h.media.ListMedia(filter)
	if items == nil {
		items = make([]*models.MediaItem, 0)
	}

	// Ensure thumbnails are populated before returning
	for _, item := range items {
		if item.ThumbnailURL == "" {
			if !h.thumbnails.HasThumbnail(item.Path) {
				isAudio := item.Type == "audio"
				if _, err := h.thumbnails.GenerateThumbnail(item.Path, isAudio); err != nil && !errors.Is(err, thumbnails.ErrThumbnailPending) {
					h.log.Warn("Failed to queue thumbnail for %s: %v", item.Path, err)
				}
			}
			item.ThumbnailURL = h.thumbnails.GetThumbnailURL(item.Path)
		}
	}

	writeSuccess(w, items)
}

// AdminDeleteMedia deletes a media file
func (h *Handler) AdminDeleteMedia(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	path, _ := url.PathUnescape(vars["path"])

	if path == "" {
		writeError(w, http.StatusBadRequest, errPathParamRequired)
		return
	}

	if err := h.media.DeleteMedia(r.Context(), path); err != nil {
		h.log.Error("%v", err)
		writeError(w, http.StatusInternalServerError, "Internal server error")
		return
	}

	h.admin.LogAction(r.Context(), "admin", "admin", "delete_media", path, nil, middleware.GetClientIP(r), true)
	writeSuccess(w, map[string]string{"message": "Media deleted"})
}

// AdminUpdateMedia updates media metadata
func (h *Handler) AdminUpdateMedia(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	path, _ := url.PathUnescape(vars["path"])

	if path == "" {
		writeError(w, http.StatusBadRequest, errPathParamRequired)
		return
	}

	// Use a raw map to detect which fields were actually sent
	var rawBody map[string]json.RawMessage
	if json.NewDecoder(r.Body).Decode(&rawBody) != nil {
		writeError(w, http.StatusBadRequest, errInvalidRequest)
		return
	}

	var reqName string
	var reqTags []string
	var reqCategory string
	var reqIsMature bool
	var reqMatureContent bool
	var reqMetadata map[string]string

	if raw, ok := rawBody["name"]; ok {
		if err := json.Unmarshal(raw, &reqName); err != nil {
			writeError(w, http.StatusBadRequest, "Invalid 'name' field")
			return
		}
	}
	if raw, ok := rawBody["tags"]; ok {
		if err := json.Unmarshal(raw, &reqTags); err != nil {
			writeError(w, http.StatusBadRequest, "Invalid 'tags' field")
			return
		}
	}
	if raw, ok := rawBody["category"]; ok {
		if err := json.Unmarshal(raw, &reqCategory); err != nil {
			writeError(w, http.StatusBadRequest, "Invalid 'category' field")
			return
		}
	}
	if raw, ok := rawBody["metadata"]; ok {
		if err := json.Unmarshal(raw, &reqMetadata); err != nil {
			writeError(w, http.StatusBadRequest, "Invalid 'metadata' field")
			return
		}
		// Validate metadata keys and values to prevent abuse
		for k, v := range reqMetadata {
			if !helpers.ValidateMetadataKey(k) {
				writeError(w, http.StatusBadRequest, fmt.Sprintf("Invalid metadata key: %s", k))
				return
			}
			if !helpers.ValidateMetadataValue(v) {
				writeError(w, http.StatusBadRequest, fmt.Sprintf("Metadata value too large for key: %s", k))
				return
			}
		}
		// Sanitize metadata to prevent XSS attacks
		reqMetadata = helpers.SanitizeMap(reqMetadata)
	}

	// Build updates map - only include fields that were actually sent
	updates := make(map[string]interface{})
	if reqTags != nil {
		updates["tags"] = reqTags
	}
	if reqCategory != "" {
		updates["category"] = reqCategory
	}
	// Support both field names: is_mature and mature_content
	if raw, ok := rawBody["is_mature"]; ok {
		if err := json.Unmarshal(raw, &reqIsMature); err != nil {
			writeError(w, http.StatusBadRequest, "Invalid 'is_mature' field")
			return
		}
		updates["is_mature"] = reqIsMature
	}
	if raw, ok := rawBody["mature_content"]; ok {
		if err := json.Unmarshal(raw, &reqMatureContent); err != nil {
			writeError(w, http.StatusBadRequest, "Invalid 'mature_content' field")
			return
		}
		updates["is_mature"] = reqMatureContent
	}
	for k, v := range reqMetadata {
		updates[k] = v
	}

	reqName = strings.TrimSpace(reqName)

	if err := h.media.UpdateMetadata(path, updates); err != nil {
		h.log.Error("%v", err)
		writeError(w, http.StatusInternalServerError, "Internal server error")
		return
	}

	// Handle rename if name is provided and actually different from current name
	if reqName != "" {
		currentName := filepath.Base(path)
		if reqName != currentName {
			newPath, err := h.media.RenameMedia(path, reqName)
			if err != nil {
				h.log.Error("%v", err)
				writeError(w, http.StatusInternalServerError, "Internal server error")
				return
			}
			path = newPath
		}
	}

	h.admin.LogAction(r.Context(), "admin", "admin", "update_media", path, nil, middleware.GetClientIP(r), true)

	// Return the full updated MediaItem
	if updatedItem, err := h.media.GetMedia(path); err == nil && updatedItem != nil {
		writeSuccess(w, updatedItem)
	} else {
		writeSuccess(w, map[string]string{"message": "Media updated", "path": path})
	}
}

// AdminBulkMedia performs a bulk action (delete or update) on multiple media files.
// POST /api/admin/media/bulk
// Body: { paths: string[], action: "delete"|"update", data?: { category?: string, is_mature?: bool } }
// Returns: { success: int, failed: int, errors: []string }
func (h *Handler) AdminBulkMedia(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Paths  []string               `json:"paths"`
		Action string                 `json:"action"`
		Data   map[string]interface{} `json:"data"`
	}
	if json.NewDecoder(r.Body).Decode(&req) != nil {
		writeError(w, http.StatusBadRequest, errInvalidRequest)
		return
	}

	if len(req.Paths) == 0 {
		writeError(w, http.StatusBadRequest, "paths must not be empty")
		return
	}
	if len(req.Paths) > 500 {
		writeError(w, http.StatusBadRequest, "too many paths (max 500)")
		return
	}
	if req.Action != "delete" && req.Action != "update" {
		writeError(w, http.StatusBadRequest, `action must be "delete" or "update"`)
		return
	}

	var successCount, failedCount int
	var errs []string
	clientIP := middleware.GetClientIP(r)

	for _, path := range req.Paths {
		if path == "" {
			continue
		}
		var opErr error
		switch req.Action {
		case "delete":
			opErr = h.media.DeleteMedia(r.Context(), path)
			if opErr == nil {
				h.admin.LogAction(r.Context(), "admin", "admin", "bulk_delete_media", path, nil, clientIP, true)
			}
		case "update":
			// Build updates map from data; only include recognised fields
			updates := make(map[string]interface{})
			if cat, ok := req.Data["category"].(string); ok && cat != "" {
				updates["category"] = cat
			}
			if mature, ok := req.Data["is_mature"].(bool); ok {
				updates["is_mature"] = mature
			}
			if len(updates) == 0 {
				writeError(w, http.StatusBadRequest, "no valid fields in data for update action")
				return
			}
			opErr = h.media.UpdateMetadata(path, updates)
			if opErr == nil {
				h.admin.LogAction(r.Context(), "admin", "admin", "bulk_update_media", path, nil, clientIP, true)
			}
		}
		if opErr != nil {
			h.log.Error("bulk %s %s: %v", req.Action, path, opErr)
			failedCount++
			errs = append(errs, fmt.Sprintf("%s: %v", path, opErr))
		} else {
			successCount++
		}
	}

	if errs == nil {
		errs = []string{}
	}
	writeSuccess(w, map[string]interface{}{
		"success": successCount,
		"failed":  failedCount,
		"errors":  errs,
	})
}

// Upload Handlers

func (h *Handler) UploadMedia(w http.ResponseWriter, r *http.Request) {
	session := middleware.GetSession(r)
	if session == nil {
		writeError(w, http.StatusUnauthorized, errNotAuthenticated)
		return
	}

	// Use the user already resolved by session middleware (handles both regular
	// users and the admin pseudo-user created from admin_session).
	user := middleware.GetUser(r)
	if user == nil {
		writeError(w, http.StatusUnauthorized, errNotAuthenticated)
		return
	}

	if !user.Permissions.CanUpload {
		writeError(w, http.StatusForbidden, "Upload not allowed for your account")
		return
	}

	cfg := h.media.GetConfig()

	if !cfg.Uploads.Enabled {
		writeError(w, http.StatusForbidden, "Uploads are disabled")
		return
	}

	// Enforce max body size before parsing
	if cfg.Uploads.MaxFileSize > 0 {
		r.Body = http.MaxBytesReader(w, r.Body, cfg.Uploads.MaxFileSize)
	}

	if err := r.ParseMultipartForm(32 << 20); err != nil {
		writeError(w, http.StatusBadRequest, "Failed to parse upload form")
		return
	}
	defer func() {
		if err := r.MultipartForm.RemoveAll(); err != nil {
			h.log.Warn("Failed to clean multipart form: %v", err)
		}
	}()

	// Accept "files" (frontend) with fallback to "file" (single-file legacy)
	fileHeaders := r.MultipartForm.File["files"]
	if len(fileHeaders) == 0 {
		fileHeaders = r.MultipartForm.File["file"]
	}
	if len(fileHeaders) == 0 {
		writeError(w, http.StatusBadRequest, "No files provided")
		return
	}

	// Check storage quota against the combined size of all incoming files
	userType := h.getUserType(cfg, user)
	if userType != nil && userType.StorageQuota > 0 {
		var totalIncoming int64
		for _, fh := range fileHeaders {
			totalIncoming += fh.Size
		}
		if user.StorageUsed+totalIncoming > userType.StorageQuota {
			writeError(w, http.StatusForbidden, "Storage quota exceeded")
			return
		}
	}

	category := r.FormValue("category")

	type uploadedEntry struct {
		Filename string `json:"filename"`
		Size     int64  `json:"size"`
	}
	type errorEntry struct {
		Filename string `json:"filename"`
		Error    string `json:"error"`
	}

	uploaded := make([]uploadedEntry, 0, len(fileHeaders))
	errors := make([]errorEntry, 0)
	var totalAdded int64

	for _, fh := range fileHeaders {
		result, err := h.upload.ProcessFileHeader(fh, session.UserID, category)
		if err != nil {
			h.log.Error("Upload failed for %s: %v", fh.Filename, err)
			errors = append(errors, errorEntry{Filename: fh.Filename, Error: "Upload failed"})
			continue
		}
		uploaded = append(uploaded, uploadedEntry{Filename: result.Filename, Size: result.Size})
		totalAdded += result.Size

		// Auto-scan uploaded file for mature content
		if cfg.Uploads.ScanForMature && result.Path != "" {
			if scanResult := h.scanner.ScanFile(result.Path); scanResult != nil && scanResult.IsMature && cfg.MatureScanner.AutoFlag {
				if _, err := h.media.GetMedia(result.Path); err == nil {
					updates := map[string]interface{}{"is_mature": true}
					if len(scanResult.Reasons) > 0 {
						updates["mature_reason"] = scanResult.Reasons[0]
					}
					if err := h.media.UpdateMetadata(result.Path, updates); err != nil {
						h.log.Error("Failed to flag uploaded file as mature: %v", err)
					}
				}
			}
		}
	}

	// Update storage counter for real (non-admin-pseudo) users
	if totalAdded > 0 && user.ID != "admin" {
		if err := h.auth.UpdateUser(r.Context(), user.Username, map[string]interface{}{
			"storage_used": user.StorageUsed + totalAdded,
		}); err != nil {
			h.log.Error("Failed to update user storage: %v", err)
		}
	}

	writeSuccess(w, map[string]interface{}{
		"uploaded": uploaded,
		"errors":   errors,
	})
}

// GetUploadProgress returns upload progress
func (h *Handler) GetUploadProgress(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	uploadID := vars["id"]

	progress, ok := h.upload.GetProgress(uploadID)
	if !ok {
		writeError(w, http.StatusNotFound, "Upload not found")
		return
	}

	writeSuccess(w, progress)
}

// Scanner Handlers

// ScanContent scans media files for mature content
func (h *Handler) ScanContent(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Path         string `json:"path"`
		AutoApply    bool   `json:"auto_apply"`
		ScanMetadata bool   `json:"scan_metadata"`
	}
	if json.NewDecoder(r.Body).Decode(&req) != nil {
		writeError(w, http.StatusBadRequest, errInvalidRequest)
		return
	}

	// If path is provided, scan single file
	if req.Path != "" {
		result := h.scanner.ScanFile(req.Path)
		writeSuccess(w, result)
		return
	}

	// Otherwise, scan all media directories
	cfg := h.media.GetConfig()
	allResults := make([]*scanner.ScanResult, 0)

	// Scan videos directory
	if cfg.Directories.Videos != "" {
		results, err := h.scanner.ScanDirectory(cfg.Directories.Videos)
		if err != nil {
			h.log.Error("Failed to scan videos directory: %v", err)
		} else {
			allResults = append(allResults, results...)
		}
	}

	// Scan music directory
	if cfg.Directories.Music != "" {
		results, err := h.scanner.ScanDirectory(cfg.Directories.Music)
		if err != nil {
			h.log.Error("Failed to scan music directory: %v", err)
		} else {
			allResults = append(allResults, results...)
		}
	}

	// Scan uploads directory
	if cfg.Directories.Uploads != "" {
		results, err := h.scanner.ScanDirectory(cfg.Directories.Uploads)
		if err != nil {
			h.log.Error("Failed to scan uploads directory: %v", err)
		} else {
			allResults = append(allResults, results...)
		}
	}

	// Count results
	autoFlagged := 0
	reviewNeeded := 0
	clean := 0
	for _, result := range allResults {
		if result.AutoFlagged {
			autoFlagged++
		}
		if result.NeedsReview {
			reviewNeeded++
		}
		if !result.IsMature && !result.NeedsReview {
			clean++
		}

		// If auto_apply is enabled and result is mature, update the media library
		if req.AutoApply && result.IsMature {
			if err := h.media.SetMatureFlag(result.Path, true, result.Confidence, result.Reasons); err != nil {
				h.log.Error("Failed to set mature flag for %s: %v", result.Path, err)
			}
		}
	}

	stats := h.scanner.GetStats()
	writeSuccess(w, map[string]interface{}{
		"stats":              stats,
		"scanned":            len(allResults),
		"auto_flagged_count": autoFlagged,
		"review_queue_count": reviewNeeded,
		"clean":              clean,
		"message":            fmt.Sprintf("Scanned %d files", len(allResults)),
	})
}

// GetScannerStats returns scanner statistics
func (h *Handler) GetScannerStats(w http.ResponseWriter, _ *http.Request) {
	stats := h.scanner.GetStats()
	writeSuccess(w, stats)
}

// GetReviewQueue returns items pending review as a flat array
func (h *Handler) GetReviewQueue(w http.ResponseWriter, _ *http.Request) {
	queue := h.scanner.GetReviewQueue()
	writeSuccess(w, queue)
}

// ApproveContent approves content from the review queue
func (h *Handler) ApproveContent(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	path, _ := url.PathUnescape(vars["path"])

	if err := h.scanner.ApproveContent(r.Context(), path); err != nil {
		writeError(w, http.StatusNotFound, "Item not found in review queue")
		return
	}

	// Update media library to mark as mature
	result, ok := h.scanner.GetScanResult(path)
	if ok {
		if err := h.media.SetMatureFlag(path, true, result.Confidence, result.Reasons); err != nil {
			h.log.Error("Failed to update media library mature flag: %v", err)
		}
	}

	writeSuccess(w, nil)
}

// RejectContent rejects content from the review queue
func (h *Handler) RejectContent(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	path, _ := url.PathUnescape(vars["path"])

	if err := h.scanner.RejectContent(r.Context(), path); err != nil {
		writeError(w, http.StatusNotFound, "Item not found in review queue")
		return
	}

	// Update media library to mark as not mature
	if err := h.media.SetMatureFlag(path, false, 0, nil); err != nil {
		h.log.Error("Failed to update media library mature flag: %v", err)
	}

	writeSuccess(w, nil)
}

// BatchReviewAction applies approve/reject action to multiple review queue items
func (h *Handler) BatchReviewAction(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Action string   `json:"action"`
		Paths  []string `json:"paths"`
	}
	if json.NewDecoder(r.Body).Decode(&req) != nil {
		writeError(w, http.StatusBadRequest, errInvalidRequest)
		return
	}

	if req.Action != "approve" && req.Action != "reject" {
		writeError(w, http.StatusBadRequest, "Invalid action: must be 'approve' or 'reject'")
		return
	}

	updated := 0
	for _, path := range req.Paths {
		var err error
		if req.Action == "approve" {
			err = h.scanner.ApproveContent(r.Context(), path)
			if err == nil {
				// Also update media library to mark as mature
				result, ok := h.scanner.GetScanResult(path)
				if ok {
					if setErr := h.media.SetMatureFlag(path, true, result.Confidence, result.Reasons); setErr != nil {
						h.log.Error("Failed to update media library mature flag for %s: %v", path, setErr)
					}
				}
			}
		} else {
			err = h.scanner.RejectContent(r.Context(), path)
			if err == nil {
				// Also update media library to mark as not mature
				if setErr := h.media.SetMatureFlag(path, false, 0, nil); setErr != nil {
					h.log.Error("Failed to update media library mature flag for %s: %v", path, setErr)
				}
			}
		}
		if err == nil {
			updated++
		}
	}

	writeSuccess(w, map[string]interface{}{
		"updated": updated,
		"total":   len(req.Paths),
	})
}

// ClearReviewQueue clears all items from the scanner review queue
func (h *Handler) ClearReviewQueue(w http.ResponseWriter, _ *http.Request) {
	h.scanner.ClearReviewQueue()
	writeSuccess(w, map[string]interface{}{
		"message": "Review queue cleared",
	})
}

// Thumbnail Handlers

// GenerateThumbnail generates a thumbnail for a media file
func (h *Handler) GenerateThumbnail(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Path    string `json:"path"`
		IsAudio bool   `json:"is_audio"`
	}
	if json.NewDecoder(r.Body).Decode(&req) != nil {
		writeError(w, http.StatusBadRequest, errInvalidRequest)
		return
	}

	thumbnailPath, err := h.thumbnails.GenerateThumbnail(req.Path, req.IsAudio)
	if err != nil {
		h.log.Error("%v", err)
		writeError(w, http.StatusInternalServerError, "Internal server error")
		return
	}

	writeSuccess(w, map[string]string{
		"message":        "Thumbnail generated",
		"thumbnail_path": thumbnailPath,
	})
}

// GetThumbnail returns a thumbnail image. Supports:
//   - ?type=placeholder, ?type=audio_placeholder, or ?type=censored - returns a placeholder image
//   - ?path=<media_path> - returns the thumbnail for a media file
func (h *Handler) GetThumbnail(w http.ResponseWriter, r *http.Request) {
	thumbnailType := r.URL.Query().Get("type")

	// Handle placeholder requests (ONLY for mature content censorship)
	if thumbnailType == "placeholder" || thumbnailType == "audio_placeholder" || thumbnailType == "censored" {
		placeholderPath, err := h.thumbnails.GetPlaceholderPath(thumbnailType)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "Failed to get placeholder")
			return
		}
		if r.Method == http.MethodHead {
			w.Header().Set(headerContentType, "image/jpeg")
			w.WriteHeader(http.StatusOK)
			return
		}
		// Cache placeholder images for 30 days (they never change)
		w.Header().Set("Cache-Control", "public, max-age=2592000, immutable")
		w.Header().Set("Content-Type", "image/jpeg")
		http.ServeFile(w, r, placeholderPath)
		return
	}

	path := r.URL.Query().Get("path")
	if path == "" {
		writeError(w, http.StatusBadRequest, errPathRequired)
		return
	}

	// Validate path to prevent path traversal attacks
	// Path must be within configured media directories (Videos, Music, or Uploads)
	cfg := h.media.GetConfig()
	validPath := false
	for _, dir := range []string{cfg.Directories.Videos, cfg.Directories.Music, cfg.Directories.Uploads} {
		if dir == "" {
			continue
		}
		cleanDir := filepath.Clean(dir)
		cleanPath := filepath.Clean(path)
		// Use separator-aware prefix check to prevent partial directory name matches
		// (e.g. /media/videos2/... must not pass when only /media/videos is configured)
		if cleanPath == cleanDir || strings.HasPrefix(cleanPath, cleanDir+string(os.PathSeparator)) {
			validPath = true
			break
		}
	}
	if !validPath {
		h.log.Warn("Thumbnail request for path outside media directories: %s", path)
		writeError(w, http.StatusForbidden, "Invalid media path")
		return
	}

	// For mature content: serve censored placeholder to guests and users who haven't
	// enabled mature content; only serve the real thumbnail to authorised users.
	// (Streaming uses checkMatureAccess which returns 401/403, but for thumbnails we
	// prefer showing a censored image so the media card still renders with the gate overlay.)
	if item, err := h.media.GetMedia(path); err == nil && item != nil && item.IsMature {
		canView := false
		if user := middleware.GetUser(r); user != nil {
			canView = user.Permissions.CanViewMature && user.Preferences.ShowMature
		}
		if !canView {
			censoredPath, cErr := h.thumbnails.GetPlaceholderPath("censored")
			if cErr == nil {
				w.Header().Set("Cache-Control", "public, max-age=2592000, immutable")
				w.Header().Set("Content-Type", "image/jpeg")
				http.ServeFile(w, r, censoredPath)
			} else {
				writeError(w, http.StatusForbidden, "Mature content")
			}
			return
		}
	}

	if !h.thumbnails.HasThumbnail(path) {
		// Try to generate thumbnail on-demand
		isAudio := strings.HasSuffix(strings.ToLower(path), ".mp3") ||
			strings.HasSuffix(strings.ToLower(path), ".wav") ||
			strings.HasSuffix(strings.ToLower(path), ".flac") ||
			strings.HasSuffix(strings.ToLower(path), ".aac") ||
			strings.HasSuffix(strings.ToLower(path), ".ogg")

		_, err := h.thumbnails.GenerateThumbnailSync(path, isAudio)
		if err != nil && err != thumbnails.ErrThumbnailPending {
			// Generation failed - return 404, do NOT serve placeholder
			// Placeholders are ONLY for mature content censorship
			h.log.Error("Failed to generate thumbnail for %s: %v", path, err)
			writeError(w, http.StatusNotFound, "Thumbnail generation failed")
			return
		}
	}

	// Serve the actual thumbnail image file
	thumbFilePath := h.thumbnails.GetThumbnailFilePath(path)

	// Check if file exists
	if _, err := os.Stat(thumbFilePath); os.IsNotExist(err) {
		h.log.Error("Thumbnail file does not exist: %s", thumbFilePath)
		writeError(w, http.StatusNotFound, "Thumbnail not found")
		return
	}

	if r.Method == http.MethodHead {
		w.Header().Set(headerContentType, "image/jpeg")
		w.WriteHeader(http.StatusOK)
		return
	}

	// Cache actual thumbnails for 7 days; private because per-user mature content checks apply
	w.Header().Set("Cache-Control", "private, max-age=604800")
	w.Header().Set("Content-Type", "image/jpeg")
	http.ServeFile(w, r, thumbFilePath)
}

// ServeThumbnailFile serves a thumbnail image file by filename from the thumbnails directory
func (h *Handler) ServeThumbnailFile(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	filename := vars["filename"]

	if filename == "" {
		writeError(w, http.StatusBadRequest, "filename required")
		return
	}

	// Sanitize filename to prevent path traversal
	filename = filepath.Base(filename)
	filePath := filepath.Join(h.thumbnails.GetThumbnailDir(), filename)

	// Only serve .jpg and .png files
	ext := strings.ToLower(filepath.Ext(filename))
	if ext != ".jpg" && ext != ".jpeg" && ext != ".png" {
		writeError(w, http.StatusBadRequest, "Invalid thumbnail format")
		return
	}

	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		// Return 404 for missing thumbnails - do NOT serve placeholder
		// Placeholders are ONLY for mature content censorship
		writeError(w, http.StatusNotFound, "Thumbnail not found")
		return
	}

	// Cache thumbnails for 7 days
	w.Header().Set("Cache-Control", "public, max-age=604800")
	w.Header().Set("Content-Type", "image/jpeg")
	http.ServeFile(w, r, filePath)
}

// GetThumbnailPreviews returns the preview thumbnail URLs for a media file
func (h *Handler) GetThumbnailPreviews(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Query().Get("path")
	if path == "" {
		writeError(w, http.StatusBadRequest, errPathRequired)
		return
	}

	cfg := h.media.GetConfig()
	count := cfg.Thumbnails.PreviewCount
	if count <= 0 {
		count = 3
	}

	urls := h.thumbnails.GetPreviewURLs(path, count)
	writeSuccess(w, map[string]interface{}{
		"previews": urls,
	})
}

// GetThumbnailStats returns thumbnail generation stats
func (h *Handler) GetThumbnailStats(w http.ResponseWriter, _ *http.Request) {
	stats := h.thumbnails.GetStats()
	// thumbnails.Stats has PascalCase fields (no JSON tags); transform to snake_case
	// with MB conversion for the frontend.
	writeSuccess(w, map[string]interface{}{
		"total_thumbnails":   stats.Generated,
		"total_size_mb":      float64(stats.TotalSize) / (1024 * 1024),
		"pending_generation": stats.Pending,
		"generation_errors":  stats.Failed,
	})
}

// Validator Handlers

// ValidateMedia validates a media file
func (h *Handler) ValidateMedia(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Path string `json:"path"`
	}
	if json.NewDecoder(r.Body).Decode(&req) != nil {
		writeError(w, http.StatusBadRequest, errInvalidRequest)
		return
	}

	result, err := h.validator.ValidateFile(req.Path)
	if err != nil {
		h.log.Error("%v", err)
		writeError(w, http.StatusInternalServerError, "Internal server error")
		return
	}

	writeSuccess(w, result)
}

// FixMedia attempts to fix an invalid media file
func (h *Handler) FixMedia(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Path string `json:"path"`
	}
	if json.NewDecoder(r.Body).Decode(&req) != nil {
		writeError(w, http.StatusBadRequest, errInvalidRequest)
		return
	}

	result, err := h.validator.FixFile(req.Path)
	if err != nil {
		h.log.Error("%v", err)
		writeError(w, http.StatusInternalServerError, "Internal server error")
		return
	}

	writeSuccess(w, result)
}

// GetValidatorStats returns validator statistics
func (h *Handler) GetValidatorStats(w http.ResponseWriter, _ *http.Request) {
	stats := h.validator.GetStats()
	writeSuccess(w, stats)
}

// Backup Handlers (using backup module instead of admin)

// CreateBackupV2 creates a backup using the backup module
func (h *Handler) CreateBackupV2(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Description string `json:"description"`
		BackupType  string `json:"backup_type"`
	}
	// Decode is best-effort; an empty or missing body is acceptable
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.log.Debug("CreateBackupV2: could not decode request body: %v", err)
	}

	if req.BackupType == "" {
		req.BackupType = "full"
	}

	backupInfo, err := h.backup.CreateBackup(req.Description, req.BackupType)
	if err != nil {
		h.log.Error("%v", err)
		writeError(w, http.StatusInternalServerError, "Internal server error")
		return
	}

	writeSuccess(w, backupInfo)
}

// ListBackupsV2 lists backups using the backup module
func (h *Handler) ListBackupsV2(w http.ResponseWriter, _ *http.Request) {
	backups, err := h.backup.ListBackups()
	if err != nil {
		h.log.Error("%v", err)
		writeError(w, http.StatusInternalServerError, "Internal server error")
		return
	}
	writeSuccess(w, backups)
}

// RestoreBackup restores from a backup (v2 API - by ID path param)
func (h *Handler) RestoreBackup(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	backupID := vars["id"]

	if err := h.backup.RestoreBackup(backupID); err != nil {
		h.log.Error("%v", err)
		writeError(w, http.StatusInternalServerError, "Internal server error")
		return
	}

	writeSuccess(w, map[string]string{"message": "Backup restored"})
}

// DeleteBackup deletes a backup
func (h *Handler) DeleteBackup(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	backupID := vars["id"]

	if err := h.backup.DeleteBackup(backupID); err != nil {
		h.log.Error("%v", err)
		writeError(w, http.StatusInternalServerError, "Internal server error")
		return
	}

	writeSuccess(w, nil)
}

// Auto-Discovery Handlers

// DiscoverMedia discovers and suggests organization for media files
func (h *Handler) DiscoverMedia(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Directory string `json:"directory"`
	}
	if json.NewDecoder(r.Body).Decode(&req) != nil {
		writeError(w, http.StatusBadRequest, errInvalidRequest)
		return
	}

	scanResults, err := h.autodiscovery.ScanDirectory(req.Directory)
	if err != nil {
		h.log.Error("%v", err)
		writeError(w, http.StatusInternalServerError, "Internal server error")
		return
	}

	writeSuccess(w, scanResults)
}

// GetDiscoverySuggestions returns organization suggestions
func (h *Handler) GetDiscoverySuggestions(w http.ResponseWriter, _ *http.Request) {
	discoverySuggestions := h.autodiscovery.GetSuggestions()
	writeSuccess(w, discoverySuggestions)
}

// ApplyDiscoverySuggestion applies a suggested organization
func (h *Handler) ApplyDiscoverySuggestion(w http.ResponseWriter, r *http.Request) {
	var req struct {
		OriginalPath string `json:"original_path"`
	}
	if json.NewDecoder(r.Body).Decode(&req) != nil {
		writeError(w, http.StatusBadRequest, errInvalidRequest)
		return
	}

	if err := h.autodiscovery.ApplySuggestion(req.OriginalPath); err != nil {
		h.log.Error("%v", err)
		writeError(w, http.StatusInternalServerError, "Internal server error")
		return
	}

	writeSuccess(w, map[string]string{"message": "Suggestion applied"})
}

// DismissDiscoverySuggestion removes a suggestion without applying it
func (h *Handler) DismissDiscoverySuggestion(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	path, _ := url.PathUnescape(vars["path"])

	if path == "" {
		writeError(w, http.StatusBadRequest, errPathParamRequired)
		return
	}

	h.autodiscovery.ClearSuggestion(path)
	writeSuccess(w, map[string]string{"message": "Suggestion dismissed"})
}

// Suggestions Handlers

// enrichSuggestionThumbnails populates thumbnail URLs for suggestions
func (h *Handler) enrichSuggestionThumbnails(items []*suggestions.Suggestion) {
	for _, item := range items {
		if item.ThumbnailURL == "" {
			if !h.thumbnails.HasThumbnail(item.MediaPath) {
				// ST-02: use helpers.IsAudioExtension — covers all audio formats, not just mp3/wav/flac
				isAudio := helpers.IsAudioExtension(strings.ToLower(filepath.Ext(item.MediaPath)))
				if _, err := h.thumbnails.GenerateThumbnail(item.MediaPath, isAudio); err != nil && !errors.Is(err, thumbnails.ErrThumbnailPending) {
					h.log.Warn("Failed to queue thumbnail for %s: %v", item.MediaPath, err)
				}
			}
			item.ThumbnailURL = h.thumbnails.GetThumbnailURL(item.MediaPath)
		}
	}
}

// GetSuggestions returns personalized content suggestions
func (h *Handler) GetSuggestions(w http.ResponseWriter, r *http.Request) {
	session := middleware.GetSession(r)
	var userID string
	if session != nil {
		userID = session.UserID
	}

	limit := 10
	if l, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && l > 0 && l <= 100 {
		limit = l
	}

	contentSuggestions := h.suggestions.GetSuggestions(userID, limit)
	h.enrichSuggestionThumbnails(contentSuggestions)
	writeSuccess(w, contentSuggestions)
}

// GetTrendingSuggestions returns trending content
func (h *Handler) GetTrendingSuggestions(w http.ResponseWriter, r *http.Request) {
	limit := 10
	if l, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && l > 0 && l <= 100 {
		limit = l
	}

	trending := h.suggestions.GetTrendingSuggestions(limit)
	h.enrichSuggestionThumbnails(trending)
	writeSuccess(w, trending)
}

// GetSimilarMedia returns similar media to a given item
func (h *Handler) GetSimilarMedia(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Query().Get("path")
	if path == "" {
		writeError(w, http.StatusBadRequest, errPathRequired)
		return
	}

	limit := 10
	if l, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && l > 0 && l <= 100 {
		limit = l
	}

	similar := h.suggestions.GetSimilarMedia(path, limit)
	h.enrichSuggestionThumbnails(similar)
	writeSuccess(w, similar)
}

// GetContinueWatching returns items the user started but didn't finish
func (h *Handler) GetContinueWatching(w http.ResponseWriter, r *http.Request) {
	session := middleware.GetSession(r)
	if session == nil {
		writeError(w, http.StatusUnauthorized, errNotAuthenticated)
		return
	}

	limit := 10
	if l, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && l > 0 && l <= 50 {
		limit = l
	}

	items := h.suggestions.GetContinueWatching(session.UserID, limit)
	h.enrichSuggestionThumbnails(items)
	writeSuccess(w, items)
}

// DEPRECATED: D-02 — functionally identical to GetSuggestions; auth-gated route adds no distinct logic.
// Route GET /api/suggestions/personalized should be removed — safe to delete
func (h *Handler) GetPersonalizedSuggestions(w http.ResponseWriter, r *http.Request) {
	session := middleware.GetSession(r)
	if session == nil {
		writeError(w, http.StatusUnauthorized, errNotAuthenticated)
		return
	}

	limit := 10
	if l, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && l > 0 && l <= 100 {
		limit = l
	}

	contentSuggestions := h.suggestions.GetSuggestions(session.UserID, limit)
	h.enrichSuggestionThumbnails(contentSuggestions)
	writeSuccess(w, contentSuggestions)
}

// RecordRating records a user rating for a media item
func (h *Handler) RecordRating(w http.ResponseWriter, r *http.Request) {
	session := middleware.GetSession(r)
	if session == nil {
		writeError(w, http.StatusUnauthorized, errNotAuthenticated)
		return
	}

	var req struct {
		Path   string  `json:"path"`
		Rating float64 `json:"rating"`
	}
	if json.NewDecoder(r.Body).Decode(&req) != nil {
		writeError(w, http.StatusBadRequest, errInvalidRequest)
		return
	}

	h.suggestions.RecordRating(session.UserID, req.Path, req.Rating)
	writeSuccess(w, nil)
}

// GetSuggestionStats returns suggestion module statistics
func (h *Handler) GetSuggestionStats(w http.ResponseWriter, _ *http.Request) {
	stats := h.suggestions.GetStats()
	writeSuccess(w, stats)
}

// Security Handlers

// GetSecurityStats returns security module statistics
func (h *Handler) GetSecurityStats(w http.ResponseWriter, _ *http.Request) {
	stats := h.security.GetStats()
	// Transform to frontend-expected field names (backend struct uses different names)
	writeSuccess(w, map[string]interface{}{
		"banned_ips":         stats.BannedIPs,
		"whitelisted_ips":    stats.WhitelistCount,
		"blacklisted_ips":    stats.BlacklistCount,
		"active_rate_limits": stats.ActiveClients,
		"total_blocks_today": stats.TotalBlocked,
	})
}

// GetWhitelist returns the IP whitelist as a flat array with an "ip" field (frontend expects IPEntry[]).
func (h *Handler) GetWhitelist(w http.ResponseWriter, _ *http.Request) {
	type ipEntry struct {
		IP        string     `json:"ip"`
		Comment   string     `json:"comment"`
		AddedBy   string     `json:"added_by"`
		AddedAt   time.Time  `json:"added_at"`
		ExpiresAt *time.Time `json:"expires_at,omitempty"`
	}
	raw := h.security.GetWhitelist().Snapshot()
	entries := make([]ipEntry, len(raw))
	for i, e := range raw {
		entries[i] = ipEntry{IP: e.Value, Comment: e.Comment, AddedBy: e.AddedBy, AddedAt: e.AddedAt, ExpiresAt: e.ExpiresAt}
	}
	writeSuccess(w, entries)
}

// AddToWhitelist adds an IP to the whitelist
func (h *Handler) AddToWhitelist(w http.ResponseWriter, r *http.Request) {
	var req struct {
		IP        string     `json:"ip"`
		Comment   string     `json:"comment"`
		ExpiresAt *time.Time `json:"expires_at,omitempty"`
	}
	if json.NewDecoder(r.Body).Decode(&req) != nil {
		writeError(w, http.StatusBadRequest, errInvalidRequest)
		return
	}

	session := middleware.GetSession(r)
	addedBy := "admin"
	if session != nil {
		addedBy = session.Username
	}

	if err := h.security.AddToWhitelist(req.IP, req.Comment, addedBy, req.ExpiresAt); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid IP address")
		return
	}

	writeSuccess(w, map[string]string{"message": "Added to whitelist"})
}

// RemoveFromWhitelist removes an IP from the whitelist
func (h *Handler) RemoveFromWhitelist(w http.ResponseWriter, r *http.Request) {
	var req struct {
		IP string `json:"ip"`
	}
	if json.NewDecoder(r.Body).Decode(&req) != nil {
		writeError(w, http.StatusBadRequest, errInvalidRequest)
		return
	}

	if !h.security.RemoveFromWhitelist(req.IP) {
		writeError(w, http.StatusNotFound, "IP not found in whitelist")
		return
	}

	writeSuccess(w, nil)
}

// GetBlacklist returns the IP blacklist as a flat array with an "ip" field (frontend expects IPEntry[]).
func (h *Handler) GetBlacklist(w http.ResponseWriter, _ *http.Request) {
	type ipEntry struct {
		IP        string     `json:"ip"`
		Comment   string     `json:"comment"`
		AddedBy   string     `json:"added_by"`
		AddedAt   time.Time  `json:"added_at"`
		ExpiresAt *time.Time `json:"expires_at,omitempty"`
	}
	raw := h.security.GetBlacklist().Snapshot()
	entries := make([]ipEntry, len(raw))
	for i, e := range raw {
		entries[i] = ipEntry{IP: e.Value, Comment: e.Comment, AddedBy: e.AddedBy, AddedAt: e.AddedAt, ExpiresAt: e.ExpiresAt}
	}
	writeSuccess(w, entries)
}

// AddToBlacklist adds an IP to the blacklist
func (h *Handler) AddToBlacklist(w http.ResponseWriter, r *http.Request) {
	var req struct {
		IP        string     `json:"ip"`
		Comment   string     `json:"comment"`
		ExpiresAt *time.Time `json:"expires_at,omitempty"`
	}
	if json.NewDecoder(r.Body).Decode(&req) != nil {
		writeError(w, http.StatusBadRequest, errInvalidRequest)
		return
	}

	session := middleware.GetSession(r)
	addedBy := "admin"
	if session != nil {
		addedBy = session.Username
	}

	if err := h.security.AddToBlacklist(req.IP, req.Comment, addedBy, req.ExpiresAt); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid IP address")
		return
	}

	writeSuccess(w, map[string]string{"message": "Added to blacklist"})
}

// RemoveFromBlacklist removes an IP from the blacklist
func (h *Handler) RemoveFromBlacklist(w http.ResponseWriter, r *http.Request) {
	var req struct {
		IP string `json:"ip"`
	}
	if json.NewDecoder(r.Body).Decode(&req) != nil {
		writeError(w, http.StatusBadRequest, errInvalidRequest)
		return
	}

	if !h.security.RemoveFromBlacklist(req.IP) {
		writeError(w, http.StatusNotFound, "IP not found in blacklist")
		return
	}

	writeSuccess(w, nil)
}

// GetBannedIPs returns currently banned IPs as a typed array (frontend expects BannedIP[]).
// security.GetBannedIPs() returns map[string]time.Time (ip → expiry), which we transform here.
func (h *Handler) GetBannedIPs(w http.ResponseWriter, _ *http.Request) {
	banned := h.security.GetBannedIPs() // map[string]time.Time (expiry time)
	type bannedIP struct {
		IP        string     `json:"ip"`
		BannedAt  time.Time  `json:"banned_at"`
		ExpiresAt *time.Time `json:"expires_at,omitempty"`
		Reason    string     `json:"reason"`
	}
	now := time.Now()
	result := make([]bannedIP, 0, len(banned))
	for ip, expiresAt := range banned {
		entry := bannedIP{
			IP:       ip,
			BannedAt: now,
			Reason:   "Rate limit violation",
		}
		if !expiresAt.IsZero() {
			entry.ExpiresAt = &expiresAt
		}
		result = append(result, entry)
	}
	writeSuccess(w, result)
}

// BanIP manually bans an IP
func (h *Handler) BanIP(w http.ResponseWriter, r *http.Request) {
	var req struct {
		IP       string `json:"ip"`
		Duration int    `json:"duration_minutes"`
	}
	if json.NewDecoder(r.Body).Decode(&req) != nil {
		writeError(w, http.StatusBadRequest, errInvalidRequest)
		return
	}

	duration := time.Duration(req.Duration) * time.Minute
	if duration == 0 {
		duration = 15 * time.Minute
	}

	h.security.BanIP(req.IP, duration)
	writeSuccess(w, map[string]string{"message": "IP banned"})
}

// UnbanIP removes a ban on an IP
func (h *Handler) UnbanIP(w http.ResponseWriter, r *http.Request) {
	var req struct {
		IP string `json:"ip"`
	}
	if json.NewDecoder(r.Body).Decode(&req) != nil {
		writeError(w, http.StatusBadRequest, errInvalidRequest)
		return
	}

	h.security.UnbanIP(req.IP)
	writeSuccess(w, nil)
}

// Categorizer Handlers

// CategorizeFile categorizes a single file and propagates the result to the
// media module so MediaItem.Category is updated in the metadata store.
func (h *Handler) CategorizeFile(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Path string `json:"path"`
	}
	if json.NewDecoder(r.Body).Decode(&req) != nil {
		writeError(w, http.StatusBadRequest, errInvalidRequest)
		return
	}

	result := h.categorizer.CategorizeFile(req.Path)
	if result != nil && string(result.Category) != "" {
		if err := h.media.UpdateMetadata(req.Path, map[string]interface{}{
			"category": string(result.Category),
		}); err != nil {
			h.log.Warn("Categorizer: failed to update media metadata for %s: %v", req.Path, err)
		}
	}
	writeSuccess(w, result)
}

// CategorizeDirectory categorizes all files in a directory and propagates each
// result to the media module so MediaItem.Category is updated in the metadata store.
func (h *Handler) CategorizeDirectory(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Directory string `json:"directory"`
	}
	if json.NewDecoder(r.Body).Decode(&req) != nil {
		writeError(w, http.StatusBadRequest, errInvalidRequest)
		return
	}

	results, err := h.categorizer.CategorizeDirectory(req.Directory)
	if err != nil {
		h.log.Error("%v", err)
		writeError(w, http.StatusInternalServerError, "Internal server error")
		return
	}

	for _, item := range results {
		if item != nil && string(item.Category) != "" {
			if updateErr := h.media.UpdateMetadata(item.Path, map[string]interface{}{
				"category": string(item.Category),
			}); updateErr != nil {
				h.log.Warn("Categorizer: failed to update media metadata for %s: %v", item.Path, updateErr)
			}
		}
	}

	writeSuccess(w, results)
}

// GetCategoryStats returns categorization statistics
func (h *Handler) GetCategoryStats(w http.ResponseWriter, _ *http.Request) {
	stats := h.categorizer.GetStats()
	writeSuccess(w, stats)
}

// SetMediaCategory manually sets a category for a file
func (h *Handler) SetMediaCategory(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Path     string               `json:"path"`
		Category categorizer.Category `json:"category"`
	}
	if json.NewDecoder(r.Body).Decode(&req) != nil {
		writeError(w, http.StatusBadRequest, errInvalidRequest)
		return
	}

	h.categorizer.SetCategory(req.Path, req.Category)
	writeSuccess(w, map[string]string{"message": "Category set"})
}

// GetByCategory returns all items in a category
func (h *Handler) GetByCategory(w http.ResponseWriter, r *http.Request) {
	category := categorizer.Category(r.URL.Query().Get("category"))
	items := h.categorizer.GetByCategory(category)
	writeSuccess(w, items)
}

// CleanStaleCategories removes entries for deleted files
func (h *Handler) CleanStaleCategories(w http.ResponseWriter, _ *http.Request) {
	removed := h.categorizer.CleanStale()
	writeSuccess(w, map[string]int{"removed": removed})
}

// Remote Media Handlers

// GetRemoteSources returns all configured remote media sources
func (h *Handler) GetRemoteSources(w http.ResponseWriter, _ *http.Request) {
	if !h.checkRemoteMediaEnabled(w) {
		return
	}
	sources := h.remote.GetSources()
	writeSuccess(w, sources)
}

// CreateRemoteSource adds a new remote source at runtime and persists it to config
func (h *Handler) CreateRemoteSource(w http.ResponseWriter, r *http.Request) {
	if !h.checkRemoteMediaEnabled(w) {
		return
	}

	var source config.RemoteSource
	if err := json.NewDecoder(r.Body).Decode(&source); err != nil {
		writeError(w, http.StatusBadRequest, errInvalidRequest)
		return
	}

	if source.Name == "" || source.URL == "" {
		writeError(w, http.StatusBadRequest, "name and url are required")
		return
	}
	source.Enabled = true

	// Register in-memory
	if err := h.remote.AddSource(source); err != nil {
		writeError(w, http.StatusConflict, "Source already exists")
		return
	}

	// Persist to config file so it survives restarts
	if err := h.config.Update(func(cfg *config.Config) {
		cfg.RemoteMedia.Sources = append(cfg.RemoteMedia.Sources, source)
	}); err != nil {
		h.log.Warn("Failed to persist new remote source to config: %v", err)
	}

	writeSuccess(w, source)
}

// GetRemoteStats returns remote media statistics
func (h *Handler) GetRemoteStats(w http.ResponseWriter, _ *http.Request) {
	if !h.checkRemoteMediaEnabled(w) {
		return
	}
	stats := h.remote.GetStats()
	writeSuccess(w, stats)
}

// GetRemoteMedia returns all remote media from all sources
func (h *Handler) GetRemoteMedia(w http.ResponseWriter, _ *http.Request) {
	if !h.checkRemoteMediaEnabled(w) {
		return
	}
	remoteMedia := h.remote.GetAllRemoteMedia()
	writeSuccess(w, remoteMedia)
}

// GetRemoteSourceMedia returns media from a specific source
func (h *Handler) GetRemoteSourceMedia(w http.ResponseWriter, r *http.Request) {
	if !h.checkRemoteMediaEnabled(w) {
		return
	}
	vars := mux.Vars(r)
	sourceName := vars["source"]

	sourceMedia, err := h.remote.GetSourceMedia(sourceName)
	if err != nil {
		writeError(w, http.StatusNotFound, "Source not found")
		return
	}

	writeSuccess(w, sourceMedia)
}

// StreamRemoteMedia streams media from a remote source
func (h *Handler) StreamRemoteMedia(w http.ResponseWriter, r *http.Request) {
	if !h.checkRemoteMediaEnabled(w) {
		return
	}

	remoteURL := r.URL.Query().Get("url")
	sourceName := r.URL.Query().Get("source")

	if remoteURL == "" {
		writeError(w, http.StatusBadRequest, "url parameter required")
		return
	}

	if err := h.remote.ProxyRemoteWithCache(w, r, remoteURL, sourceName); err != nil {
		h.log.Error("Remote stream error: %v", err)
		writeError(w, http.StatusBadGateway, "Failed to stream from remote")
	}
}

// SyncRemoteSource triggers a sync for a specific remote source
func (h *Handler) SyncRemoteSource(w http.ResponseWriter, r *http.Request) {
	if !h.checkRemoteMediaEnabled(w) {
		return
	}
	vars := mux.Vars(r)
	sourceName := vars["source"]

	sources := h.remote.GetSources()
	var found bool
	for _, s := range sources {
		if s.Source.Name == sourceName {
			found = true
			break
		}
	}

	if !found {
		writeError(w, http.StatusNotFound, "Source not found")
		return
	}

	h.log.Info("Triggering background sync for remote source: %s", sourceName)
	go func() {
		if err := h.remote.SyncSource(sourceName); err != nil {
			h.log.Warn("Background sync failed for source %s: %v", sourceName, err)
		} else {
			h.log.Info("Background sync completed for remote source: %s", sourceName)
		}
	}()

	writeSuccess(w, map[string]string{"status": "sync_started"})
}

// DeleteRemoteSource removes a remote source
func (h *Handler) DeleteRemoteSource(w http.ResponseWriter, r *http.Request) {
	if !h.checkRemoteMediaEnabled(w) {
		return
	}
	vars := mux.Vars(r)
	sourceName := vars["source"]

	if sourceName == "" {
		writeError(w, http.StatusBadRequest, "source name required")
		return
	}

	if err := h.remote.RemoveSource(sourceName); err != nil {
		writeError(w, http.StatusNotFound, "Source not found")
		return
	}

	// Persist deletion to config so removed source doesn't reappear after restart
	if err := h.config.Update(func(cfg *config.Config) {
		filtered := cfg.RemoteMedia.Sources[:0]
		for _, s := range cfg.RemoteMedia.Sources {
			if s.Name != sourceName {
				filtered = append(filtered, s)
			}
		}
		cfg.RemoteMedia.Sources = filtered
	}); err != nil {
		h.log.Warn("Failed to persist remote source deletion to config: %v", err)
	}

	writeSuccess(w, map[string]string{"message": "Source removed"})
}

// CacheRemoteMedia caches a remote media file locally
func (h *Handler) CacheRemoteMedia(w http.ResponseWriter, r *http.Request) {
	if !h.checkRemoteMediaEnabled(w) {
		return
	}
	var req struct {
		URL        string `json:"url"`
		SourceName string `json:"source_name"`
	}
	if json.NewDecoder(r.Body).Decode(&req) != nil {
		writeError(w, http.StatusBadRequest, errInvalidRequest)
		return
	}

	cached, err := h.remote.CacheMedia(req.URL, req.SourceName)
	if err != nil {
		h.log.Error("%v", err)
		writeError(w, http.StatusInternalServerError, "Internal server error")
		return
	}

	writeSuccess(w, cached)
}

// CleanRemoteCache cleans the remote media cache
func (h *Handler) CleanRemoteCache(w http.ResponseWriter, _ *http.Request) {
	if !h.checkRemoteMediaEnabled(w) {
		return
	}
	removed := h.remote.CleanCache()
	writeSuccess(w, map[string]int{"removed": removed})
}
