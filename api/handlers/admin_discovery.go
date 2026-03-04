package handlers

import (
	"net/http"
	"net/url"
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

	// TODO: req.Directory is accepted from the request body and passed directly to ScanDirectory
	// without validation against the configured media directories (cfg.Directories.Videos,
	// cfg.Directories.Music, cfg.Directories.Uploads). An admin could scan any arbitrary directory
	// on the filesystem, leaking directory listings or triggering ffprobe against unexpected content.
	// Fix: validate that req.Directory starts with (or equals) one of the configured media paths
	// using filepath.HasPrefix or a similar containment check before calling ScanDirectory.
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
