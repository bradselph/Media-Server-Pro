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

	// TODO: ValidateMedia accepts a raw filesystem path from the request body (req.Path) without
	// UUID-to-path resolution. All other media-operating handlers use resolveMediaByID(c, id) or
	// resolveAndValidatePath(c) to convert client-supplied IDs to safe absolute paths. This handler
	// bypasses that pattern, allowing admins to supply arbitrary paths (e.g. "/etc/passwd").
	// While admin-only, this is inconsistent and a potential path injection risk. Fix: accept a
	// media ID, resolve it via h.resolveMediaByID(c, req.ID), and pass the resolved path to ValidateFile.
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

	// TODO: Same raw-path issue as ValidateMedia above — accepts an arbitrary filesystem path
	// without UUID resolution. Fix: accept a media ID and resolve to an absolute path via
	// h.resolveMediaByID before passing to FixFile.
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
