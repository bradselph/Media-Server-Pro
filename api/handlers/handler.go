// Package handlers provides HTTP request handlers for the API.
package handlers

import (
	cryptorand "crypto/rand"
	"errors"
	"fmt"
	"math/big"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"

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
	"media-server-pro/pkg/models"
)

// Error message constants to avoid duplication.
const (
	errIDRequired        = "Media ID required"
	errFileNotFound      = "File not found"
	errInvalidRequest    = "Invalid request"
	errNotAuthenticated  = "Not authenticated"
	errUserNotFound      = "User not found"
	errMediaNotFound     = "Media not found"
	errPathParamRequired = "path parameter required" // admin route params only
)

// HTTP header name constants to avoid duplication.
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
// This avoids passing each dependency as a separate parameter.
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

// getSession retrieves the session from the gin context.
func getSession(c *gin.Context) *models.Session {
	if v, exists := c.Get("session"); exists {
		if s, ok := v.(*models.Session); ok {
			return s
		}
	}
	return nil
}

// getUser retrieves the user from the gin context.
func getUser(c *gin.Context) *models.User {
	if v, exists := c.Get("user"); exists {
		if u, ok := v.(*models.User); ok {
			return u
		}
	}
	return nil
}

// writeSuccess writes a successful JSON response.
func writeSuccess(c *gin.Context, data interface{}) {
	c.JSON(http.StatusOK, models.APIResponse{Success: true, Data: data})
}

// writeError writes an error JSON response.
func writeError(c *gin.Context, status int, message string) {
	c.JSON(status, models.APIResponse{Success: false, Error: message})
}

// safeContentDisposition returns a Content-Disposition header value with the
// filename sanitized to prevent header injection. Characters that could break
// the header (quotes, backslashes, newlines, control chars) are removed.
func safeContentDisposition(filename string) string {
	var safe strings.Builder
	for _, r := range filename {
		if r == '"' || r == '\\' || r == '\n' || r == '\r' || r < 0x20 {
			continue
		}
		safe.WriteRune(r)
	}
	return fmt.Sprintf("attachment; filename=\"%s\"", safe.String())
}

// isClientDisconnect returns true for network errors that indicate the client
// closed the connection (broken pipe, connection reset, i/o timeout on write).
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

// isSecureRequest detects HTTPS connections, including behind TLS-terminating reverse proxies.
func isSecureRequest(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	if r.Header.Get("X-Forwarded-Proto") == "https" {
		return true
	}
	if strings.Contains(r.Header.Get("Cf-Visitor"), `"scheme":"https"`) {
		return true
	}
	return false
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
// Panics if crypto/rand fails, as this indicates a serious system-level problem.
func randIntn(n int) int {
	if n <= 0 {
		return 0
	}
	nBig, err := cryptorand.Int(cryptorand.Reader, big.NewInt(int64(n)))
	if err != nil {
		panic(fmt.Sprintf("crypto/rand failed: %v - system entropy source unavailable", err))
	}
	return int(nBig.Int64())
}

// requireAdmin checks that the admin module is available. Returns false (and writes
// a 503 error) if the module failed to initialise. Use at the top of handlers that
// call h.admin methods other than LogAction.
func (h *Handler) requireAdmin(c *gin.Context) bool {
	if h.admin == nil {
		writeError(c, http.StatusServiceUnavailable, "Admin module is not available")
		return false
	}
	return true
}

// requirePlaylist checks that the playlist module is available. Returns false
// (and writes a 503 error) if the module failed to initialise.
func (h *Handler) requirePlaylist(c *gin.Context) bool {
	if h.playlist == nil {
		writeError(c, http.StatusServiceUnavailable, "Playlist feature is not available")
		return false
	}
	return true
}

// requireHLS checks that the HLS module is available.
func (h *Handler) requireHLS(c *gin.Context) bool {
	if h.hls == nil {
		writeError(c, http.StatusServiceUnavailable, "HLS feature is not available")
		return false
	}
	return true
}

// requireSuggestions checks that the suggestions module is available.
func (h *Handler) requireSuggestions(c *gin.Context) bool {
	if h.suggestions == nil {
		writeError(c, http.StatusServiceUnavailable, "Suggestions feature is not available")
		return false
	}
	return true
}

// requireScanner checks that the scanner module is available.
func (h *Handler) requireScanner(c *gin.Context) bool {
	if h.scanner == nil {
		writeError(c, http.StatusServiceUnavailable, "Scanner is not available")
		return false
	}
	return true
}

// requireValidator checks that the validator module is available.
func (h *Handler) requireValidator(c *gin.Context) bool {
	if h.validator == nil {
		writeError(c, http.StatusServiceUnavailable, "Validator is not available")
		return false
	}
	return true
}

// requireBackup checks that the backup module is available.
func (h *Handler) requireBackup(c *gin.Context) bool {
	if h.backup == nil {
		writeError(c, http.StatusServiceUnavailable, "Backup feature is not available")
		return false
	}
	return true
}

// requireCategorizer checks that the categorizer module is available.
func (h *Handler) requireCategorizer(c *gin.Context) bool {
	if h.categorizer == nil {
		writeError(c, http.StatusServiceUnavailable, "Categorizer is not available")
		return false
	}
	return true
}

// requireAutodiscovery checks that the autodiscovery module is available.
func (h *Handler) requireAutodiscovery(c *gin.Context) bool {
	if h.autodiscovery == nil {
		writeError(c, http.StatusServiceUnavailable, "Auto-discovery is not available")
		return false
	}
	return true
}

// requireUpdater checks that the updater module is available.
func (h *Handler) requireUpdater(c *gin.Context) bool {
	if h.updater == nil {
		writeError(c, http.StatusServiceUnavailable, "Updater is not available")
		return false
	}
	return true
}

// requireUpload checks that the upload module is available.
func (h *Handler) requireUpload(c *gin.Context) bool {
	if h.upload == nil {
		writeError(c, http.StatusServiceUnavailable, "Upload feature is not available")
		return false
	}
	return true
}

// requireThumbnails checks that the thumbnails module is available.
func (h *Handler) requireThumbnails(c *gin.Context) bool {
	if h.thumbnails == nil {
		writeError(c, http.StatusServiceUnavailable, "Thumbnails feature is not available")
		return false
	}
	return true
}

// logAdminAction is a nil-safe wrapper around h.admin.LogAction. Audit logging
// is best-effort — if the admin module is unavailable the action is silently
// skipped so that the primary operation (user create, media delete, etc.) still
// succeeds.
func (h *Handler) logAdminAction(c *gin.Context, userID, username, action, target string, details map[string]interface{}) {
	if h.admin != nil {
		h.admin.LogAction(c.Request.Context(), userID, username, action, target, details, c.ClientIP(), true)
	}
}

// logAdminActionResult is like logAdminAction but lets the caller specify success/failure.
func (h *Handler) logAdminActionResult(c *gin.Context, userID, username, action, target string, details map[string]interface{}, success bool) {
	if h.admin != nil {
		h.admin.LogAction(c.Request.Context(), userID, username, action, target, details, c.ClientIP(), success)
	}
}

// resolveAndValidatePath resolves a file path against allowed directories, prevents path
// traversal, and verifies the file exists. Returns the absolute path and true on success,
// or writes an error response and returns ("", false) on failure.
func (h *Handler) resolveAndValidatePath(c *gin.Context, path string, allowedDirs []string) (string, bool) {
	validPath := h.resolveRelativePath(path, allowedDirs)
	if validPath == "" {
		writeError(c, http.StatusNotFound, errFileNotFound)
		return "", false
	}

	realPath, err := filepath.EvalSymlinks(validPath)
	if err != nil {
		h.log.Debug("EvalSymlinks failed for %s (using raw path): %v", validPath, err)
		realPath = validPath
	}
	absPath, err := filepath.Abs(realPath)
	if err != nil {
		writeError(c, http.StatusBadRequest, "Invalid path")
		return "", false
	}

	if !isPathWithinDirs(absPath, allowedDirs) {
		h.log.Warn("Path traversal attempt detected: %s", path)
		writeError(c, http.StatusForbidden, "Access denied: path outside allowed directories")
		return "", false
	}

	if _, err := os.Stat(absPath); err != nil {
		if os.IsNotExist(err) {
			writeError(c, http.StatusNotFound, errFileNotFound)
		} else {
			writeError(c, http.StatusInternalServerError, "Error accessing file")
		}
		return "", false
	}

	return absPath, true
}

// resolveRelativePath resolves a relative path against the allowed directories.
// Absolute paths are rejected: callers should only pass filename/relative paths;
// absolute paths must go through resolveAndValidatePath which enforces dir checks.
func (h *Handler) resolveRelativePath(path string, allowedDirs []string) string {
	if filepath.IsAbs(path) {
		h.log.Warn("resolveRelativePath: rejecting absolute path input: %s", path)
		return ""
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
// the given path. Returns true if access is allowed or irrelevant, false if denied.
func (h *Handler) checkMatureAccess(c *gin.Context, absPath string) bool {
	item, err := h.media.GetMedia(absPath)
	if err != nil || item == nil || !item.IsMature {
		return true
	}

	session := getSession(c)
	if session == nil {
		writeError(c, http.StatusUnauthorized,
			"Access denied: This content is marked as mature (18+). "+
				"Please log in to access mature content.")
		return false
	}

	user := getUser(c)
	if user == nil {
		h.log.Debug("Mature content access denied for %s: user not found in context", session.Username)
		writeError(c, http.StatusForbidden,
			"Access denied: This content is marked as mature (18+). "+
				"Enable mature content viewing in your profile settings.")
		return false
	}

	if !user.Permissions.CanViewMature {
		h.log.Debug("Mature content access denied for %s: CanViewMature revoked by admin", session.Username)
		writeError(c, http.StatusForbidden,
			"Access denied: Your account does not have permission to view mature content (18+). "+
				"Contact an administrator if you believe this is an error.")
		return false
	}

	if !user.Preferences.ShowMature {
		h.log.Debug("Mature content access denied for %s: ShowMature preference is false", session.Username)
		writeError(c, http.StatusForbidden,
			"Access denied: This content is marked as mature (18+). "+
				"Enable mature content viewing in your profile settings.")
		return false
	}

	return true
}

// allowedMediaDirs returns the directories from which media can be served.
func (h *Handler) allowedMediaDirs() []string {
	cfg := h.media.GetConfig()
	return []string{cfg.Directories.Videos, cfg.Directories.Music, cfg.Directories.Uploads}
}

// resolveMediaByID looks up a media item by its opaque ID and returns the
// server-side file path. The ID is an MD5 hash of the path, generated during
// media scanning. Returns the absolute path and true on success, or writes an
// error response and returns ("", false) on failure.
//
// If the initial media scan has not yet completed (server just started), returns
// 503 instead of 404 so clients know to retry rather than treating the item as
// permanently missing.
func (h *Handler) resolveMediaByID(c *gin.Context, id string) (string, bool) {
	if id == "" {
		writeError(c, http.StatusBadRequest, errIDRequired)
		return "", false
	}
	item, err := h.media.GetMediaByID(id)
	if err != nil {
		if !h.media.IsReady() {
			writeError(c, http.StatusServiceUnavailable, "Server is initializing — media library scan in progress, please try again shortly")
			return "", false
		}
		writeError(c, http.StatusNotFound, errMediaNotFound)
		return "", false
	}
	return item.Path, true
}

// getUserStorageQuota returns storage quota for user type
func (h *Handler) getUserStorageQuota(userType string) int64 {
	cfg := h.media.GetConfig()
	for _, ut := range cfg.Auth.UserTypes {
		if ut.Name == userType {
			return ut.StorageQuota
		}
	}
	quotas := map[string]int64{
		"basic":    1 * 1024 * 1024 * 1024,
		"standard": 10 * 1024 * 1024 * 1024,
		"premium":  100 * 1024 * 1024 * 1024,
		"admin":    -1,
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
	limits := map[string]int{
		"basic":    1,
		"standard": 3,
		"premium":  10,
		"admin":    -1,
	}
	if l, ok := limits[userType]; ok {
		return l
	}
	return limits["basic"]
}

// getUserType returns the UserType config entry for the given user, or nil if not found.
func (h *Handler) getUserType(cfg *config.Config, user *models.User) *config.UserType {
	for i, ut := range cfg.Auth.UserTypes {
		if ut.Name == user.Type {
			return &cfg.Auth.UserTypes[i]
		}
	}
	return nil
}

// checkRemoteMediaEnabled returns true if remote media feature is enabled
func (h *Handler) checkRemoteMediaEnabled(c *gin.Context) bool {
	if h.remote == nil {
		writeError(c, http.StatusServiceUnavailable, "Remote media is not available")
		return false
	}
	cfg := h.media.GetConfig()
	if !cfg.Features.EnableRemoteMedia || !cfg.RemoteMedia.Enabled {
		writeError(c, http.StatusNotFound, "Remote media feature is disabled")
		return false
	}
	return true
}

// enrichSuggestionThumbnails populates thumbnail URLs for suggestions
func (h *Handler) enrichSuggestionThumbnails(items []*suggestions.Suggestion) {
	if h.thumbnails == nil {
		return
	}
	for _, item := range items {
		if item.ThumbnailURL == "" {
			if !h.thumbnails.HasThumbnail(item.MediaPath) {
				ext := strings.ToLower(filepath.Ext(item.MediaPath))
				isAudio := isAudioExtension(ext)
				if _, err := h.thumbnails.GenerateThumbnail(item.MediaPath, isAudio); err != nil && !errors.Is(err, thumbnails.ErrThumbnailPending) {
					h.log.Warn("Failed to queue thumbnail for %s: %v", item.MediaPath, err)
				}
			}
			item.ThumbnailURL = h.thumbnails.GetThumbnailURL(item.MediaPath)
		}
	}
}

// isAudioExtension returns true if the extension is a known audio format.
func isAudioExtension(ext string) bool {
	switch ext {
	case ".mp3", ".wav", ".flac", ".aac", ".ogg", ".m4a", ".wma", ".opus":
		return true
	}
	return false
}
