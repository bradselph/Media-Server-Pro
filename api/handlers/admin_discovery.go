package handlers

import (
	"net/http"
	"path/filepath"
	"slices"
	"strings"

	"github.com/gin-gonic/gin"

	"media-server-pro/internal/analytics"
	"media-server-pro/internal/autodiscovery"
)

// isDirectoryWithinMediaPaths returns true if cleanPath is under one of the allowed roots.
// Used to prevent arbitrary filesystem traversal by admins.
func isDirectoryWithinMediaPaths(cleanPath string, allowedRoots []string) bool {
	return slices.ContainsFunc(allowedRoots, func(root string) bool {
		if root == "" {
			return false
		}
		cleanRoot := filepath.Clean(root)
		return cleanPath == cleanRoot || strings.HasPrefix(cleanPath, cleanRoot+string(filepath.Separator))
	})
}

// DiscoverMedia discovers and suggests organization for media files
func (h *Handler) DiscoverMedia(c *gin.Context) {
	if !h.requireAutodiscovery(c) {
		return
	}
	var req struct {
		Directory string `json:"directory"`
	}
	if !BindJSON(c, &req, errInvalidRequest) {
		return
	}
	dirs := h.config.Get().Directories
	// EvalSymlinks resolves symlinks so a symlink pointing outside the allowed
	// roots cannot bypass the allow-list check via filepath.Clean alone.
	resolvedDir, err := filepath.EvalSymlinks(req.Directory)
	if err != nil {
		writeError(c, http.StatusBadRequest, "Invalid directory path")
		return
	}
	allowedRoots := []string{dirs.Videos, dirs.Music, dirs.Uploads}
	if !isDirectoryWithinMediaPaths(resolvedDir, allowedRoots) {
		writeError(c, http.StatusBadRequest, "Directory must be within a configured media path")
		return
	}
	scanResults, err := h.autodiscovery.ScanDirectory(autodiscovery.FilePath(resolvedDir))
	if err != nil {
		h.log.Error("%v", err)
		writeError(c, http.StatusInternalServerError, errInternalServer)
		return
	}
	h.trackServerEvent(c, analytics.EventDiscoveryRun, map[string]any{
		"directory": resolvedDir,
		"results":   len(scanResults),
	})
	writeSuccess(c, scanResults)
}

// GetDiscoverySuggestions returns organization suggestions
func (h *Handler) GetDiscoverySuggestions(c *gin.Context) {
	if !h.requireAutodiscovery(c) {
		return
	}
	discoverySuggestions := h.autodiscovery.GetSuggestions()
	writeSuccess(c, discoverySuggestions)
}

// ApplyDiscoverySuggestion applies a suggested organization
func (h *Handler) ApplyDiscoverySuggestion(c *gin.Context) {
	if !h.requireAutodiscovery(c) {
		return
	}
	var req struct {
		OriginalPath string `json:"original_path"`
	}
	if !BindJSON(c, &req, errInvalidRequest) {
		return
	}
	absPath, ok := h.resolvePathForAdmin(c, req.OriginalPath, false)
	if !ok {
		return
	}
	newPath, err := h.autodiscovery.ApplySuggestion(autodiscovery.FilePath(absPath))
	if err != nil {
		h.log.Error("%v", err)
		writeError(c, http.StatusInternalServerError, errInternalServer)
		return
	}
	// ApplySuggestion performs its own os.Rename, so the media catalog and the
	// path-keyed indexes must be re-keyed to the new location — otherwise the
	// item stays indexed under the old (now-missing) path until the next full
	// scan. Mirrors the post-rename fix-ups in applyAdminRenameIfNeeded.
	if newPath != "" && newPath != absPath {
		if h.media != nil {
			h.media.ReindexMovedFile(absPath, newPath)
		}
		if h.suggestions != nil {
			h.suggestions.RenameMediaPath(absPath, newPath)
		}
		if h.scanner != nil {
			h.scanner.RenamePath(absPath, newPath)
		}
	}

	h.trackServerEvent(c, analytics.EventDiscoveryRun, map[string]any{"scope": "apply", "path": absPath})
	writeSuccess(c, map[string]string{"message": "Suggestion applied"})
}

// DismissDiscoverySuggestion removes a suggestion without applying it
func (h *Handler) DismissDiscoverySuggestion(c *gin.Context) {
	if !h.requireAutodiscovery(c) {
		return
	}
	// Gin's *path wildcard always carries a leading '/'; keep it so the
	// suggestion's absolute path survives intact (apply gets it via JSON body).
	// gin already returns the percent-decoded value here (UseRawPath is unset,
	// so routing uses net/http's already-unescaped URL.Path). Do NOT unescape
	// again — a filename containing a literal '%' (e.g. "50% Off.mp4") would make
	// a second url.PathUnescape fail ("invalid URL escape") and 400 every dismiss.
	path := c.Param("path")
	if path == "" || path == "/" {
		writeError(c, http.StatusBadRequest, errPathParamRequired)
		return
	}
	absPath, ok := h.resolvePathForAdmin(c, path, false)
	if !ok {
		return
	}

	if err := h.autodiscovery.ClearSuggestion(autodiscovery.FilePath(absPath)); err != nil {
		h.log.Error("%v", err)
		writeError(c, http.StatusInternalServerError, errInternalServer)
		return
	}
	h.trackServerEvent(c, analytics.EventDiscoveryRun, map[string]any{"scope": "dismiss", "path": absPath})
	writeSuccess(c, map[string]string{"message": "Suggestion dismissed"})
}
