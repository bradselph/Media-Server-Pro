package handlers

import (
	"net/http"
	"net/url"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"

	"media-server-pro/internal/autodiscovery"
)

// isDirectoryWithinMediaPaths returns true if cleanPath is under one of the allowed roots.
// Used to prevent arbitrary filesystem traversal by admins.
func isDirectoryWithinMediaPaths(cleanPath string, allowedRoots []string) bool {
	for _, root := range allowedRoots {
		if root == "" {
			continue
		}
		cleanRoot := filepath.Clean(root)
		if cleanPath == cleanRoot || strings.HasPrefix(cleanPath, cleanRoot+string(filepath.Separator)) {
			return true
		}
	}
	return false
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
		writeError(c, http.StatusInternalServerError, "Internal server error")
		return
	}
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
	if err := h.autodiscovery.ApplySuggestion(autodiscovery.FilePath(absPath)); err != nil {
		h.log.Error("%v", err)
		writeError(c, http.StatusInternalServerError, "Internal server error")
		return
	}

	writeSuccess(c, map[string]string{"message": "Suggestion applied"})
}

// DismissDiscoverySuggestion removes a suggestion without applying it
func (h *Handler) DismissDiscoverySuggestion(c *gin.Context) {
	if !h.requireAutodiscovery(c) {
		return
	}
	rawPath := strings.TrimPrefix(c.Param("path"), "/")
	path, err := url.PathUnescape(rawPath)
	if err != nil || path == "" {
		writeError(c, http.StatusBadRequest, errPathParamRequired)
		return
	}
	absPath, ok := h.resolvePathForAdmin(c, path, false)
	if !ok {
		return
	}

	h.autodiscovery.ClearSuggestion(autodiscovery.FilePath(absPath))
	writeSuccess(c, map[string]string{"message": "Suggestion dismissed"})
}
