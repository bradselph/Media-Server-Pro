package handlers

import (
	"net/http"
	"net/url"
	"strings"

	"github.com/gin-gonic/gin"
)

// DiscoverMedia discovers and suggests organization for media files
func (h *Handler) DiscoverMedia(c *gin.Context) {
	var req struct {
		Directory string `json:"directory"`
	}
	if c.ShouldBindJSON(&req) != nil {
		writeError(c, http.StatusBadRequest, errInvalidRequest)
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
	discoverySuggestions := h.autodiscovery.GetSuggestions()
	writeSuccess(c, discoverySuggestions)
}

// ApplyDiscoverySuggestion applies a suggested organization
func (h *Handler) ApplyDiscoverySuggestion(c *gin.Context) {
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
	rawPath := strings.TrimPrefix(c.Param("path"), "/")
	path, err := url.PathUnescape(rawPath)
	if err != nil || path == "" {
		writeError(c, http.StatusBadRequest, errPathParamRequired)
		return
	}

	h.autodiscovery.ClearSuggestion(path)
	writeSuccess(c, map[string]string{"message": "Suggestion dismissed"})
}
