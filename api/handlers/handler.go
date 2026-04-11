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
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"media-server-pro/internal/admin"
	"media-server-pro/internal/analytics"
	"media-server-pro/internal/auth"
	"media-server-pro/internal/autodiscovery"
	"media-server-pro/internal/backup"
	"media-server-pro/internal/categorizer"
	"media-server-pro/internal/config"
	"media-server-pro/internal/crawler"
	"media-server-pro/internal/database"
	"media-server-pro/internal/downloader"
	"media-server-pro/internal/duplicates"
	"media-server-pro/internal/extractor"
	"media-server-pro/internal/hls"
	"media-server-pro/internal/logger"
	"media-server-pro/internal/media"
	"media-server-pro/internal/playlist"
	"media-server-pro/internal/receiver"
	"media-server-pro/internal/remote"
	"media-server-pro/internal/repositories"
	repoMysql "media-server-pro/internal/repositories/mysql"
	"media-server-pro/internal/scanner"
	"media-server-pro/internal/security"
	"media-server-pro/internal/streaming"
	"media-server-pro/internal/suggestions"
	"media-server-pro/internal/tasks"
	"media-server-pro/internal/thumbnails"
	"media-server-pro/internal/updater"
	"media-server-pro/internal/upload"
	"media-server-pro/internal/validator"
	"media-server-pro/pkg/middleware"
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
	errInternalServer    = "Internal server error"
	errInvalidCredentials = "Invalid credentials"
)

// HTTP header name constants to avoid duplication.
const (
	headerContentType        = "Content-Type"
	headerContentDisposition = "Content-Disposition"
	headerCacheControl       = "Cache-Control"
	headerRetryAfter         = "Retry-After"
)

// User-facing message constants to avoid duplication.
const (
	msgFailedExport        = "Failed to generate export"
	msgInitializing        = "Server is initializing — media library scan in progress, please try again shortly"
	msgMaxStreams          = "Maximum concurrent streams limit reached"
	msgMaxStreamsConn      = "Maximum concurrent streams limit reached for this connection"
	msgMatureContent       = "This content is marked as mature (18+). Please log in and enable mature content to access it."
	msgMatureAccessDenied  = "Access denied: This content is marked as mature (18+). "
)

// BuildInfo holds version and build metadata (avoids passing raw strings).
type BuildInfo struct {
	Version   string
	BuildDate string
}

// MediaID is the stable UUID for a media item (avoids primitive obsession for ID parameters).
type MediaID string

// ResolvedPath is an absolute, validated path under allowed directories (avoids raw path strings).
type ResolvedPath string

// AllowedDirs is a list of base directories allowed for path resolution (avoids raw []string).
type AllowedDirs []string

// HandlerCoreDeps groups critical and core module dependencies.
type HandlerCoreDeps struct {
	Config    *config.Manager
	Media     *media.Module
	Streaming *streaming.Module
	HLS       *hls.Module
	Auth      *auth.Module
	Database  *database.Module
}

// HandlerOptionalDeps groups optional feature modules (nil when feature is disabled).
type HandlerOptionalDeps struct {
	Admin         *admin.Module
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
	Receiver      *receiver.Module
	Extractor     *extractor.Module
	Crawler       *crawler.Module
	Duplicates    *duplicates.Module
	Analytics     *analytics.Module
	Playlist      *playlist.Module
	Downloader    *downloader.Module
}

// HandlerDeps holds all module dependencies needed to create a Handler.
// Dependencies are grouped to avoid primitive obsession (many separate fields).
type HandlerDeps struct {
	BuildInfo    BuildInfo
	Core         HandlerCoreDeps
	Optional     HandlerOptionalDeps
	ShutdownFunc func() // called before os.Exit to drain connections and stop modules (P1-9)
}

// viewCooldownDuration is the minimum interval between counting repeated views
// of the same media by the same user/IP. Prevents inflated view counts from
// browser Range re-requests, seeks, and HLS quality switches.
const viewCooldownDuration = 5 * time.Minute

// Handler holds dependencies for HTTP handlers
type Handler struct {
	log              *logger.Logger
	buildInfo        BuildInfo
	media            *media.Module
	streaming        *streaming.Module
	hls              *hls.Module
	auth             *auth.Module
	analytics        *analytics.Module
	playlist         *playlist.Module
	admin            *admin.Module
	database         *database.Module
	tasks            *tasks.Module
	upload           *upload.Module
	scanner          *scanner.Module
	thumbnails       *thumbnails.Module
	validator        *validator.Module
	backup           *backup.Module
	autodiscovery    *autodiscovery.Module
	suggestions      *suggestions.Module
	security         *security.Module
	categorizer      *categorizer.Module
	updater          *updater.Module
	remote           *remote.Module
	receiver         *receiver.Module
	extractor        *extractor.Module
	crawler          *crawler.Module
	duplicates       *duplicates.Module
	downloader       *downloader.Module
	config           *config.Manager
	shutdownFunc     func()
	deletionRequests repositories.DataDeletionRequestRepository
	viewCooldown     sync.Map // key: "userID|mediaID" → value: time.Time of last counted view
}

// tryRecordView returns true if the view should be counted (not within the
// cooldown window for this user+media pair). When true, it also updates the
// cooldown timestamp. This prevents inflated view counts from repeated Range
// requests, seeks back to 0, and HLS quality switches.
func (h *Handler) tryRecordView(userID, mediaID string) bool {
	key := userID + "|" + mediaID
	now := time.Now()
	cooldown := h.config.Get().Analytics.ViewCooldown
	if cooldown <= 0 {
		cooldown = viewCooldownDuration
	}
	if prev, ok := h.viewCooldown.Load(key); ok {
		if now.Sub(prev.(time.Time)) < cooldown { //nolint:errcheck // sync.Map always stores time.Time
			return false
		}
	}
	h.viewCooldown.Store(key, now)
	return true
}

// startViewCooldownSweeper runs a periodic sweep to evict expired entries from
// the viewCooldown map. This replaces per-entry time.AfterFunc timers which
// create one goroutine per view under high traffic.
func (h *Handler) startViewCooldownSweeper() {
	ticker := time.NewTicker(viewCooldownDuration)
	go func() {
		for range ticker.C {
			now := time.Now()
			cooldown := h.config.Get().Analytics.ViewCooldown
			if cooldown <= 0 {
				cooldown = viewCooldownDuration
			}
			evictBefore := now.Add(-cooldown * 2)
			h.viewCooldown.Range(func(key, value any) bool {
				if value.(time.Time).Before(evictBefore) { //nolint:errcheck // sync.Map always stores time.Time
					h.viewCooldown.Delete(key)
				}
				return true
			})
		}
	}()
}

// NewHandler creates a new handler with dependencies.
// Panics if critical modules (Media, Auth, Streaming) are nil.
func NewHandler(deps HandlerDeps) *Handler {
	c, o := deps.Core, deps.Optional
	missingCritical := c.Config == nil || c.Media == nil || c.Auth == nil || c.Streaming == nil || c.Database == nil
	if missingCritical {
		panic("NewHandler: critical core dependency is nil (Config, Database, Media, Auth, or Streaming)")
	}

	shutdownFunc := deps.ShutdownFunc
	if shutdownFunc == nil {
		shutdownFunc = func() { /* no-op default for tests */ }
	}

	h := &Handler{
		log:              logger.New("handlers"),
		buildInfo:        deps.BuildInfo,
		media:            c.Media,
		streaming:        c.Streaming,
		hls:              c.HLS,
		auth:             c.Auth,
		database:         c.Database,
		config:           c.Config,
		deletionRequests: repoMysql.NewDataDeletionRequestRepository(c.Database.GORM()),
		analytics:        o.Analytics,
		playlist:         o.Playlist,
		admin:            o.Admin,
		tasks:            o.Tasks,
		upload:           o.Upload,
		scanner:          o.Scanner,
		thumbnails:       o.Thumbnails,
		validator:        o.Validator,
		backup:           o.Backup,
		autodiscovery:    o.Autodiscovery,
		suggestions:      o.Suggestions,
		security:         o.Security,
		categorizer:      o.Categorizer,
		updater:          o.Updater,
		remote:           o.Remote,
		receiver:         o.Receiver,
		extractor:        o.Extractor,
		crawler:          o.Crawler,
		duplicates:       o.Duplicates,
		downloader:       o.Downloader,
		shutdownFunc:     shutdownFunc,
	}
	h.startViewCooldownSweeper()
	return h
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

// isClientDisconnect returns true for network errors that indicate the client
// closed the connection (broken pipe, connection reset, i/o timeout on write).
func isClientDisconnect(err error) bool {
	if err == nil {
		return false
	}
	if netErr, ok := errors.AsType[net.Error](err); ok && netErr.Timeout() {
		return true
	}
	msg := err.Error()
	return strings.Contains(msg, "broken pipe") ||
		strings.Contains(msg, "connection reset by peer") ||
		strings.Contains(msg, "write: connection reset") ||
		strings.Contains(msg, "i/o timeout")
}

// isSecureRequest detects HTTPS connections, including behind TLS-terminating reverse proxies.
// X-Forwarded-Proto and Cf-Visitor are only honored when the request comes from a trusted proxy
// to prevent clients from spoofing HTTPS on plain HTTP connections.
func isSecureRequest(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	remoteIP, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		remoteIP = r.RemoteAddr
	}
	if middleware.IsTrustedProxy(remoteIP) {
		if r.Header.Get("X-Forwarded-Proto") == "https" {
			return true
		}
		if strings.Contains(r.Header.Get("Cf-Visitor"), `"scheme":"https"`) {
			return true
		}
	}
	return false
}

// clearSessionCookie clears the session_id cookie with consistent Path, HttpOnly, Secure, SameSite
// so logout and account-deletion paths invalidate the cookie reliably across browsers.
func clearSessionCookie(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     "session_id",
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		Expires:  time.Unix(0, 0),
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
		Secure:   isSecureRequest(r),
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

// requireModule checks that the given module pointer is non-nil. Returns false
// (and writes a 503 error) if the module failed to initialize or is disabled.
// Use at the top of handlers that depend on optional modules.
// Handles both interface nil and typed nil (e.g. (*extractor.Module)(nil)).
func requireModule(c *gin.Context, module any, name string) bool {
	if module == nil {
		writeError(c, http.StatusServiceUnavailable, name+" is not available")
		return false
	}
	v := reflect.ValueOf(module)
	if v.Kind() == reflect.Pointer && v.IsNil() {
		writeError(c, http.StatusServiceUnavailable, name+" is not available")
		return false
	}
	return true
}

// requireAdminModule checks that the admin module is initialized (not an auth check).
func (h *Handler) requireAdminModule(c *gin.Context) bool {
	return requireModule(c, h.admin, "Admin module")
}
func (h *Handler) requirePlaylist(c *gin.Context) bool {
	return requireModule(c, h.playlist, "Playlist feature")
}
func (h *Handler) requireHLS(c *gin.Context) bool { return requireModule(c, h.hls, "HLS feature") }
func (h *Handler) requireSuggestions(c *gin.Context) bool {
	return requireModule(c, h.suggestions, "Suggestions feature")
}
func (h *Handler) requireScanner(c *gin.Context) bool { return requireModule(c, h.scanner, "Scanner") }
func (h *Handler) requireValidator(c *gin.Context) bool {
	return requireModule(c, h.validator, "Validator")
}
func (h *Handler) requireBackup(c *gin.Context) bool {
	return requireModule(c, h.backup, "Backup feature")
}
func (h *Handler) requireCategorizer(c *gin.Context) bool {
	return requireModule(c, h.categorizer, "Categorizer")
}
func (h *Handler) requireAutodiscovery(c *gin.Context) bool {
	return requireModule(c, h.autodiscovery, "Auto-discovery")
}
func (h *Handler) requireUpdater(c *gin.Context) bool { return requireModule(c, h.updater, "Updater") }
func (h *Handler) requireUpload(c *gin.Context) bool {
	return requireModule(c, h.upload, "Upload feature")
}
func (h *Handler) requireThumbnails(c *gin.Context) bool {
	return requireModule(c, h.thumbnails, "Thumbnails feature")
}
func (h *Handler) requireSecurity(c *gin.Context) bool {
	return requireModule(c, h.security, "Security feature")
}

// adminLogActionParams groups arguments for logAdminAction to avoid excess parameters.
type adminLogActionParams struct {
	UserID   string
	Username string
	Action   string
	Target   string
	Details  map[string]any
}

// logAdminAction is a nil-safe wrapper around h.admin.LogAction. Audit logging
// is best-effort — if the admin module is unavailable the action is silently
// skipped so that the primary operation (user create, media delete, etc.) still
// succeeds. Uses the session user when available so audit logs distinguish admins.
func (h *Handler) logAdminAction(c *gin.Context, p *adminLogActionParams) {
	if h.admin == nil {
		return
	}
	userID, username := p.UserID, p.Username
	if session := getSession(c); session != nil {
		userID = session.UserID
		username = session.Username
	}
	h.admin.LogAction(c.Request.Context(), &admin.AuditLogParams{
		UserID: userID, Username: username, Action: p.Action, Resource: p.Target,
		Details: p.Details, IPAddress: c.ClientIP(), Success: true,
	})
}

// adminLogResultParams groups arguments for logAdminActionResult to avoid excess parameters.
type adminLogResultParams struct {
	UserID   string
	Username string
	Action   string
	Target   string
	Details  map[string]any
	Success  bool
}

// logAdminActionResult is like logAdminAction but lets the caller specify success/failure.
func (h *Handler) logAdminActionResult(c *gin.Context, p *adminLogResultParams) {
	if h.admin == nil {
		return
	}
	userID, username := p.UserID, p.Username
	if session := getSession(c); session != nil {
		userID = session.UserID
		username = session.Username
	}
	h.admin.LogAction(c.Request.Context(), &admin.AuditLogParams{
		UserID: userID, Username: username, Action: p.Action, Resource: p.Target,
		Details: p.Details, IPAddress: c.ClientIP(), Success: p.Success,
	})
}

// checkMatureAccess verifies the current user has permission to access mature content at
// the given path. Returns true if access is allowed or irrelevant, false if denied.
func (h *Handler) checkMatureAccess(c *gin.Context, absPath string) bool {
	item, err := h.media.GetMedia(absPath)
	if err != nil {
		// Log a warning so DB outages or scan gaps don't silently bypass mature protection.
		h.log.Warn("checkMatureAccess: media lookup failed for %s: %v — allowing access (item may not be in library)", absPath, err)
		return true
	}
	if item == nil {
		return true
	}
	if !item.IsMature {
		return true
	}

	session := getSession(c)
	if session == nil {
		writeError(c, http.StatusUnauthorized,
			msgMatureAccessDenied+
				"Please log in to access mature content.")
		return false
	}

	user := getUser(c)
	if user == nil {
		h.log.Debug("Mature content access denied for %s: user not found in context", session.Username)
		writeError(c, http.StatusForbidden,
			msgMatureAccessDenied+
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
			msgMatureAccessDenied+
				"Enable mature content viewing in your profile settings.")
		return false
	}

	return true
}

// allowedMediaDirs returns the directories from which media can be served.
func (h *Handler) allowedMediaDirs() AllowedDirs {
	cfg := h.media.GetConfig()
	return AllowedDirs{cfg.Directories.Videos, cfg.Directories.Music, cfg.Directories.Uploads}
}

// resolvePathToAbsolute resolves path (absolute or relative) to an absolute path under
// allowedDirs. Writes error and returns ("", false) on failure.
func resolvePathToAbsolute(c *gin.Context, path string, allowedDirs []string) (string, bool) {
	absPath, err := resolvePathToAbsoluteNoWrite(path, allowedDirs)
	if err == nil {
		return absPath, true
	}
	writePathResolveError(c, err)
	return "", false
}

func writePathResolveError(c *gin.Context, err error) {
	if errors.Is(err, errInvalidPath) {
		writeError(c, http.StatusBadRequest, "Invalid path")
		return
	}
	writeError(c, http.StatusNotFound, "Path not found under media directories")
}

var (
	errInvalidPath  = errors.New("invalid path")
	errPathNotFound = errors.New("path not found")
)

func resolveAbsPath(path string) (string, error) {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return "", errInvalidPath
	}
	return absPath, nil
}

func resolvePathToAbsoluteNoWrite(path string, allowedDirs []string) (string, error) {
	if filepath.IsAbs(path) {
		absPath, err := resolveAbsPath(path)
		if err != nil {
			return "", err
		}
		if !isPathUnderDirs(absPath, allowedDirs) {
			return "", errPathNotFound
		}
		return absPath, nil
	}
	absPath, ok := resolveRelativePathInDirs(path, allowedDirs)
	if ok {
		return absPath, nil
	}
	return "", errPathNotFound
}

// isPathUnderDirs returns true if absPath is under at least one of the given directories.
func isPathUnderDirs(absPath string, dirs []string) bool {
	cleanPath := filepath.Clean(absPath)
	for _, d := range dirs {
		if d == "" {
			continue
		}
		cleanDir := filepath.Clean(d)
		if cleanDir == cleanPath || strings.HasPrefix(cleanPath, cleanDir+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

func resolveRelativePathInDirs(path string, allowedDirs []string) (string, bool) {
	for _, d := range allowedDirs {
		if absPath, ok := resolveRelativeInDir(path, d); ok {
			return absPath, true
		}
	}
	return "", false
}

func resolveRelativeInDir(path, dir string) (string, bool) {
	if dir == "" {
		return "", false
	}
	candidate := filepath.Join(dir, path)
	if _, err := os.Stat(candidate); err != nil {
		return "", false
	}
	absPath, err := filepath.Abs(candidate)
	if err != nil {
		return "", false
	}
	return absPath, true
}

// statPathForValidation stats absPath and writes an appropriate error on failure.
// Returns (info, true) on success, (nil, false) on failure.
func statPathForValidation(c *gin.Context, absPath string) (os.FileInfo, bool) {
	info, err := os.Stat(absPath)
	if err != nil {
		if os.IsNotExist(err) {
			writeError(c, http.StatusNotFound, "Path not found")
		} else {
			writeError(c, http.StatusInternalServerError, "Error accessing path")
		}
		return nil, false
	}
	return info, true
}

// validatePathType checks that info matches mustBeDir (true = directory, false = file).
// Writes error and returns false on mismatch.
func validatePathType(c *gin.Context, info os.FileInfo, mustBeDir bool) bool {
	if mustBeDir && !info.IsDir() {
		writeError(c, http.StatusBadRequest, "Path must be a directory")
		return false
	}
	if !mustBeDir && info.IsDir() {
		writeError(c, http.StatusBadRequest, "Path must be a file")
		return false
	}
	return true
}

// validatePathInDirsAndStat checks absPath is under allowedDirs, exists, and matches
// mustBeDir (true = directory, false = file). Writes error and returns false on failure.
func validatePathInDirsAndStat(c *gin.Context, absPath string, allowedDirs []string, mustBeDir bool) bool {
	if !isPathUnderDirs(absPath, allowedDirs) {
		writeError(c, http.StatusForbidden, "Access denied: path outside allowed media directories")
		return false
	}
	info, ok := statPathForValidation(c, absPath)
	if !ok {
		return false
	}
	return validatePathType(c, info, mustBeDir)
}

// resolvePathForAdmin validates a path (absolute or relative) for admin operations such as
// classify file/directory. It ensures the path is under allowed media directories and exists.
// If mustBeDir is true, the path must be a directory; otherwise it must be a regular file.
// Returns the absolute path and true on success; on failure writes an error and returns ("", false).
func (h *Handler) resolvePathForAdmin(c *gin.Context, path string, mustBeDir bool) (string, bool) {
	path = strings.TrimSpace(path)
	if path == "" {
		writeError(c, http.StatusBadRequest, "path is required")
		return "", false
	}
	allowedDirs := h.allowedMediaDirs()
	absPath, ok := resolvePathToAbsolute(c, path, allowedDirs)
	if !ok {
		return "", false
	}
	if !validatePathInDirsAndStat(c, absPath, allowedDirs, mustBeDir) {
		return "", false
	}
	return absPath, true
}

// resolveMediaByID looks up a media item by its stable UUID and returns the
// server-side file path. The UUID is generated once per file on first scan and
// persisted in the database. Returns the absolute path and true on success, or
// writes an error response and returns ("", false) on failure.
//
// If the initial media scan has not yet completed (server just started), returns
// 503 instead of 404 so clients know to retry rather than treating the item as
// permanently missing.
func (h *Handler) resolveMediaByID(c *gin.Context, id string) (string, bool) {
	mid := MediaID(strings.TrimSpace(id))
	if mid == "" {
		writeError(c, http.StatusBadRequest, errIDRequired)
		return "", false
	}
	item, err := h.media.GetMediaByID(string(mid))
	if err != nil {
		if !h.media.IsReady() {
			c.Header(headerRetryAfter, "3")
			writeError(c, http.StatusServiceUnavailable, msgInitializing)
			return "", false
		}
		writeError(c, http.StatusNotFound, errMediaNotFound)
		return "", false
	}
	return item.Path, true
}

// resolveMediaPathOrReceiver is like resolveMediaByID but falls back to receiver
// and extractor items when the ID is not found locally. For receiver items it
// returns a synthetic path "receiver:<id>" and for extractor items "extractor:<id>".
// These synthetic paths are suitable as database keys (position tracking, watch
// history, ratings) but NOT for local file operations.
func (h *Handler) resolveMediaPathOrReceiver(c *gin.Context, id string) (path, itemName string, ok bool) {
	mid := MediaID(strings.TrimSpace(id))
	if mid == "" {
		writeError(c, http.StatusBadRequest, errIDRequired)
		return "", "", false
	}
	item, err := h.media.GetMediaByID(string(mid))
	if err == nil {
		return item.Path, item.Name, true
	}
	// Fallback: check receiver media
	if h.receiver != nil {
		if ri := h.receiver.GetMediaItem(string(mid)); ri != nil {
			return "receiver:" + string(mid), ri.Name, true
		}
	}
	// Fallback: check extractor items
	if h.extractor != nil {
		if ei := h.extractor.GetItem(string(mid)); ei != nil && ei.Status == "active" {
			return "extractor:" + string(mid), ei.Title, true
		}
	}
	if !h.media.IsReady() {
		writeError(c, http.StatusServiceUnavailable, msgInitializing)
		return "", "", false
	}
	writeError(c, http.StatusNotFound, errMediaNotFound)
	return "", "", false
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

// checkFeatureEnabled checks that a module is non-nil and that a config flag
// reports the feature as enabled. Returns false (and writes the appropriate
// 503 or 404 error) if either check fails.
func checkFeatureEnabled(c *gin.Context, module any, name string, enabled func() bool) bool {
	if !requireModule(c, module, name) {
		return false
	}
	if !enabled() {
		writeError(c, http.StatusNotFound, name+" feature is disabled")
		return false
	}
	return true
}

func (h *Handler) checkExtractorEnabled(c *gin.Context) bool {
	return checkFeatureEnabled(c, h.extractor, "Extractor", func() bool {
		return h.media.GetConfig().Extractor.Enabled
	})
}

func (h *Handler) checkCrawlerEnabled(c *gin.Context) bool {
	return checkFeatureEnabled(c, h.crawler, "Crawler", func() bool {
		return h.media.GetConfig().Crawler.Enabled
	})
}

func (h *Handler) checkRemoteMediaEnabled(c *gin.Context) bool {
	return checkFeatureEnabled(c, h.remote, "Remote media", func() bool {
		return h.media.GetConfig().RemoteMedia.Enabled
	})
}

// enrichSuggestionThumbnails populates thumbnail URLs for suggestions.
// Uses the stable MediaID (UUID) for the public URL so thumbnails survive
// file path changes. Falls back to queuing generation if the thumbnail file
// doesn't exist yet on disk.
func (h *Handler) enrichSuggestionThumbnails(items []*suggestions.Suggestion) {
	if h.thumbnails == nil {
		return
	}
	for _, item := range items {
		if item.ThumbnailURL != "" {
			continue
		}
		mediaID := thumbnails.MediaID(item.MediaID)
		if h.thumbnails.HasThumbnail(mediaID) {
			item.ThumbnailURL = h.thumbnails.GetThumbnailURL(mediaID)
			continue
		}
		ext := strings.ToLower(filepath.Ext(item.MediaPath))
		isAudio := isAudioExtension(ext)
		_, err := h.thumbnails.GenerateThumbnailRequest(&thumbnails.ThumbnailRequest{
			MediaPath: item.MediaPath, MediaID: item.MediaID, IsAudio: isAudio, HighPriority: true,
		})
		if err != nil && !errors.Is(err, thumbnails.ErrThumbnailPending) {
			h.log.Warn("Failed to queue thumbnail for %s: %v", item.MediaPath, err)
		}
		item.ThumbnailURL = h.thumbnails.GetThumbnailURL(mediaID)
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
