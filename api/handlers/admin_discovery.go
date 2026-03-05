package handlers

import (
	"net/http"
	"net/url"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"
)

// DiscoverMedia discovers and suggests organization for media files
func (h *Handler) DiscoverMedia(c *gin.Context) {
	if !h.requireAutodiscovery(c) {
		return
	}
	var req struct {
		Directory string `json:"directory"`
	}
	if c.ShouldBindJSON(&req) != nil {
		writeError(c, http.StatusBadRequest, errInvalidRequest)
		return
	}

	// Validate that the requested directory is within a configured media path to prevent
	// arbitrary filesystem traversal by admins.
	dirs := h.config.Get().Directories
	allowedRoots := []string{dirs.Videos, dirs.Music, dirs.Uploads}
	cleanReq := filepath.Clean(req.Directory)
	allowed := false
	for _, root := range allowedRoots {
		if root == "" {
			continue
		}
		cleanRoot := filepath.Clean(root)
		if cleanReq == cleanRoot || strings.HasPrefix(cleanReq, cleanRoot+string(filepath.Separator)) {
			allowed = true
			break
		}
	}
	if !allowed {
		writeError(c, http.StatusBadRequest, "Directory must be within a configured media path")
		return
	}
	scanResults, err := h.autodiscovery.ScanDirectory(req.Directory)
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
	if c.ShouldBindJSON(&req) != nil {
		writeError(c, http.StatusBadRequest, errInvalidRequest)
		return
	}

	if err := h.autodiscovery.ApplySuggestion(req.OriginalPath); err != nil {
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

	h.autodiscovery.ClearSuggestion(path)
	writeSuccess(c, map[string]string{"message": "Suggestion dismissed"})
}
