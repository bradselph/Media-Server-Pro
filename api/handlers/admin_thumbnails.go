package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// CleanupThumbnails triggers a manual thumbnail cleanup that removes orphans,
// excess previews, and corrupt (0-byte) files.
func (h *Handler) CleanupThumbnails(c *gin.Context) {
	if !h.requireThumbnails(c) {
		return
	}

	result, err := h.thumbnails.Cleanup()
	if err != nil {
		h.log.Error("Thumbnail cleanup failed: %v", err)
		writeError(c, http.StatusInternalServerError, "Cleanup failed: "+err.Error())
		return
	}

	writeSuccess(c, map[string]interface{}{
		"orphans_removed": result.OrphansRemoved,
		"excess_removed":  result.ExcessRemoved,
		"corrupt_removed": result.CorruptRemoved,
		"bytes_freed":     result.BytesFreed,
	})
}
