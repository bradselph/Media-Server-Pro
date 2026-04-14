package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// UploadCustomThumbnail accepts a multipart image upload and stores it as the
// thumbnail for the given media ID, replacing any existing thumbnail.
// POST /api/admin/media/:id/thumbnail  (multipart/form-data, field "thumbnail")
func (h *Handler) UploadCustomThumbnail(c *gin.Context) {
	if !h.requireThumbnails(c) {
		return
	}
	id, ok := RequireParamID(c, "id")
	if !ok {
		return
	}

	file, _, err := c.Request.FormFile("thumbnail")
	if err != nil {
		writeError(c, http.StatusBadRequest, "thumbnail file is required")
		return
	}
	defer file.Close()

	if err := h.thumbnails.SaveCustomThumbnail(id, file); err != nil {
		h.log.Error("UploadCustomThumbnail: %v", err)
		writeError(c, http.StatusInternalServerError, "Failed to save thumbnail: "+err.Error())
		return
	}

	session := getSession(c)
	if session != nil {
		h.logAdminAction(c, &adminLogActionParams{
			UserID: session.UserID, Username: session.Username,
			Action: "upload_custom_thumbnail", Target: id,
		})
	}
	writeSuccess(c, gin.H{"message": "thumbnail updated"})
}

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
