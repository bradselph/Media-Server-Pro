package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"media-server-pro/internal/analytics"
)

// ValidateMedia validates a media file
func (h *Handler) ValidateMedia(c *gin.Context) {
	if !h.requireValidator(c) {
		return
	}
	var req struct {
		ID string `json:"id"`
	}
	if !BindJSON(c, &req, "") {
		return
	}

	absPath, ok := h.resolveMediaByID(c, req.ID)
	if !ok {
		return
	}
	result, err := h.validator.ValidateFile(absPath)
	if err != nil {
		h.log.Error("%v", err)
		writeError(c, http.StatusInternalServerError, errInternalServer)
		return
	}

	h.trackServerEvent(c, analytics.EventValidatorRun, map[string]any{"scope": "validate", "media_id": req.ID})
	writeSuccess(c, result)
}

// FixMedia attempts to fix an invalid media file
func (h *Handler) FixMedia(c *gin.Context) {
	if !h.requireValidator(c) {
		return
	}
	var req struct {
		ID string `json:"id"`
	}
	if !BindJSON(c, &req, "") {
		return
	}

	absPath, ok := h.resolveMediaByID(c, req.ID)
	if !ok {
		return
	}
	result, err := h.validator.FixFile(absPath)
	if err != nil {
		h.log.Error("%v", err)
		writeError(c, http.StatusInternalServerError, errInternalServer)
		return
	}

	h.trackServerEvent(c, analytics.EventValidatorRun, map[string]any{"scope": "fix", "media_id": req.ID})
	writeSuccess(c, result)
}

// GetValidatorStats returns validator statistics
func (h *Handler) GetValidatorStats(c *gin.Context) {
	if !h.requireValidator(c) {
		return
	}
	stats := h.validator.GetStats()
	writeSuccess(c, stats)
}
