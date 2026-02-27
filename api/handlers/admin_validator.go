package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// TODO(api-contract): RESPONSE SHAPE OPAQUE — ValidateMedia passes the raw result of
// h.validator.ValidateFile() directly to writeSuccess() without transforming or documenting
// its shape. Frontend adminApi.validateMedia() (web/frontend/src/api/endpoints.ts) assumes the
// result is { valid: boolean; errors?: string[] }. If the validator module's result struct
// differs (e.g., uses "is_valid" instead of "valid", or includes extra fields), the frontend
// type will silently misalign at runtime. Explicitly document or serialize the validator result
// into a known shape here. Frontend: web/frontend/src/api/endpoints.ts adminApi.validateMedia().
//
// ValidateMedia validates a media file
func (h *Handler) ValidateMedia(c *gin.Context) {
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

// TODO(api-contract): RESPONSE SHAPE OPAQUE — FixMedia passes the raw result of
// h.validator.FixFile() directly to writeSuccess() without transforming or documenting its
// shape. Frontend adminApi.fixMedia() (web/frontend/src/api/endpoints.ts) assumes the result is
// { fixed: boolean; message?: string }. If the validator module's fix result struct differs,
// the frontend type silently misaligns at runtime. Document or explicitly serialize the fix
// result. Frontend: web/frontend/src/api/endpoints.ts adminApi.fixMedia().
//
// FixMedia attempts to fix an invalid media file
func (h *Handler) FixMedia(c *gin.Context) {
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

// TODO(api-contract): RESPONSE SHAPE OPAQUE — GetValidatorStats passes the raw result of
// h.validator.GetStats() directly to writeSuccess(). Frontend adminApi.getValidatorStats()
// (web/frontend/src/api/endpoints.ts) assumes the shape is
// { total, validated, needs_fix, fixed, failed, unsupported }. If the validator Stats struct
// uses different field names (e.g., "total_files" instead of "total", "needs_repair" instead
// of "needs_fix") the frontend will receive undefined for each misaligned field without any
// error. Document or explicitly serialize the stats struct here.
// Frontend: web/frontend/src/api/endpoints.ts adminApi.getValidatorStats().
//
// GetValidatorStats returns validator statistics
func (h *Handler) GetValidatorStats(c *gin.Context) {
	stats := h.validator.GetStats()
	writeSuccess(c, stats)
}
