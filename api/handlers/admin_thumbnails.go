package handlers

import (
	"io"
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

	// Validate file type by magic bytes to prevent SVG/HTML/script injection.
	sniff := make([]byte, 512)
	n, readErr := file.Read(sniff)
	if readErr != nil && readErr != io.EOF {
		h.log.Warn("UploadCustomThumbnail: failed to read magic bytes: %v", readErr)
		writeError(c, http.StatusBadRequest, "failed to process uploaded file")
		return
	}
	detected := http.DetectContentType(sniff[:n])
	allowed := map[string]bool{"image/jpeg": true, "image/png": true, "image/webp": true}
	if !allowed[detected] {
		writeError(c, http.StatusBadRequest, "file must be a JPEG, PNG, or WebP image")
		return
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		writeError(c, http.StatusBadRequest, "failed to process uploaded file")
		return
	}

	if err := h.thumbnails.SaveCustomThumbnail(id, file); err != nil {
		h.log.Error("UploadCustomThumbnail: %v", err)
		writeError(c, http.StatusInternalServerError, "Failed to save thumbnail")
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
		writeError(c, http.StatusInternalServerError, "Cleanup failed")
		return
	}

	writeSuccess(c, map[string]any{
		"orphans_removed": result.OrphansRemoved,
		"excess_removed":  result.ExcessRemoved,
		"corrupt_removed": result.CorruptRemoved,
		"bytes_freed":     result.BytesFreed,
	})
}
