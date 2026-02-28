package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// ValidateMedia validates a media file
func (h *Handler) ValidateMedia(c *gin.Context) {
	if !h.requireValidator(c) {
		return
	}
	var req struct {
		Path string `json:"path"`
	}
	if c.ShouldBindJSON(&req) != nil {
		writeError(c, http.StatusBadRequest, errInvalidRequest)
		return
	}

	result, err := h.validator.ValidateFile(req.Path)
	if err != nil {
		h.log.Error("%v", err)
		writeError(c, http.StatusInternalServerError, "Internal server error")
		return
	}

	writeSuccess(c, result)
}

// FixMedia attempts to fix an invalid media file
func (h *Handler) FixMedia(c *gin.Context) {
	if !h.requireValidator(c) {
		return
	}
	var req struct {
		Path string `json:"path"`
	}
	if c.ShouldBindJSON(&req) != nil {
		writeError(c, http.StatusBadRequest, errInvalidRequest)
		return
	}

	result, err := h.validator.FixFile(req.Path)
	if err != nil {
		h.log.Error("%v", err)
		writeError(c, http.StatusInternalServerError, "Internal server error")
		return
	}

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
