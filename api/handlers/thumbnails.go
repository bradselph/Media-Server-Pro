package handlers

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"

	"media-server-pro/internal/thumbnails"
)

// GenerateThumbnail generates a thumbnail for a media file
func (h *Handler) GenerateThumbnail(c *gin.Context) {
	if !h.requireThumbnails(c) {
		return
	}
	var req struct {
		ID      string `json:"id"`
		IsAudio bool   `json:"is_audio"`
	}
	if c.ShouldBindJSON(&req) != nil {
		writeError(c, http.StatusBadRequest, errInvalidRequest)
		return
	}

	absPath, ok := h.resolveMediaByID(c, req.ID)
	if !ok {
		return
	}

	_, err := h.thumbnails.GenerateThumbnail(absPath, req.ID, req.IsAudio)
	if err != nil && !errors.Is(err, thumbnails.ErrThumbnailPending) {
		h.log.Error("%v", err)
		writeError(c, http.StatusInternalServerError, "Internal server error")
		return
	}

	msg := "Thumbnail generated"
	if errors.Is(err, thumbnails.ErrThumbnailPending) {
		msg = "Thumbnail generation queued"
	}
	writeSuccess(c, map[string]string{
		"message": msg,
	})
}

// GetThumbnail returns a thumbnail image.
func (h *Handler) GetThumbnail(c *gin.Context) {
	if !h.requireThumbnails(c) {
		return
	}
	thumbnailType := c.Query("type")

	if thumbnailType == "placeholder" || thumbnailType == "audio_placeholder" || thumbnailType == "censored" {
		placeholderPath, err := h.thumbnails.GetPlaceholderPath(thumbnailType)
		if err != nil {
			writeError(c, http.StatusInternalServerError, "Failed to get placeholder")
			return
		}
		if c.Request.Method == http.MethodHead {
			c.Header(headerContentType, "image/jpeg")
			c.Status(http.StatusOK)
			return
		}
		c.Header("Cache-Control", "public, max-age=2592000, immutable")
		c.Header("Content-Type", "image/jpeg")
		http.ServeFile(c.Writer, c.Request, placeholderPath)
		return
	}

	id := c.Query("id")

	// Receiver items have no local file — serve a placeholder instead of 404.
	if id != "" {
		if _, err := h.media.GetMediaByID(id); err != nil && h.receiver != nil {
			if ri := h.receiver.GetMediaItem(id); ri != nil {
				placeholderType := "placeholder"
				if ri.MediaType == "audio" {
					placeholderType = "audio_placeholder"
				}
				if h.isReceiverItemMature(ri.ContentFingerprint) && !h.canViewMatureContent(c) {
					placeholderType = "censored"
				}
				if ph, pErr := h.thumbnails.GetPlaceholderPath(placeholderType); pErr == nil {
					c.Header("Cache-Control", "public, max-age=86400")
					c.Header("Content-Type", "image/jpeg")
					http.ServeFile(c.Writer, c.Request, ph)
					return
				}
			}
		}
	}

	path, ok := h.resolveMediaByID(c, id)
	if !ok {
		return
	}

	if item, err := h.media.GetMedia(path); err == nil && item != nil && item.IsMature {
		canView := false
		if user := getUser(c); user != nil {
			canView = user.Permissions.CanViewMature && user.Preferences.ShowMature
		}
		if !canView {
			censoredPath, cErr := h.thumbnails.GetPlaceholderPath("censored")
			if cErr == nil {
				c.Header("Cache-Control", "public, max-age=2592000, immutable")
				c.Header("Content-Type", "image/jpeg")
				http.ServeFile(c.Writer, c.Request, censoredPath)
			} else {
				writeError(c, http.StatusForbidden, "Mature content")
			}
			return
		}
	}

	if !h.thumbnails.HasThumbnail(id) {
		// TODO: This audio extension check is incomplete. It covers only 5 extensions (.mp3, .wav,
		// .flac, .aac, .ogg) while the upload module's audioExtensions map (internal/upload/upload.go)
		// supports 10: .mp3, .wav, .flac, .aac, .ogg, .m4a, .wma, .aiff, .alac, .opus.
		// Files with the missing extensions (.m4a, .wma, .aiff, .alac, .opus) will have isAudio=false,
		// causing GenerateThumbnailSync to attempt ffmpeg video frame extraction instead of audio
		// waveform generation, likely producing a blank or wrong thumbnail.
		// Fix: use helpers.IsAudioExtension(ext) or sync this set with upload.audioExtensions.
		isAudio := strings.HasSuffix(strings.ToLower(path), ".mp3") ||
			strings.HasSuffix(strings.ToLower(path), ".wav") ||
			strings.HasSuffix(strings.ToLower(path), ".flac") ||
			strings.HasSuffix(strings.ToLower(path), ".aac") ||
			strings.HasSuffix(strings.ToLower(path), ".ogg")

		_, err := h.thumbnails.GenerateThumbnailSync(path, id, isAudio)
		if err != nil && !errors.Is(err, thumbnails.ErrThumbnailPending) {
			h.log.Error("Failed to generate thumbnail for %s: %v", path, err)
			writeError(c, http.StatusNotFound, "Thumbnail generation failed")
			return
		}
	}

	thumbFilePath := h.thumbnails.GetThumbnailFilePath(id)

	if _, err := os.Stat(thumbFilePath); os.IsNotExist(err) {
		h.log.Error("Thumbnail file does not exist: %s", thumbFilePath)
		writeError(c, http.StatusNotFound, "Thumbnail not found")
		return
	}

	if c.Request.Method == http.MethodHead {
		c.Header(headerContentType, "image/jpeg")
		c.Status(http.StatusOK)
		return
	}

	c.Header("Cache-Control", "public, max-age=604800")
	c.Header("Content-Type", "image/jpeg")
	http.ServeFile(c.Writer, c.Request, thumbFilePath)
}

// ServeThumbnailFile serves a thumbnail image file by filename from the thumbnails directory
func (h *Handler) ServeThumbnailFile(c *gin.Context) {
	if !h.requireThumbnails(c) {
		return
	}
	filename := c.Param("filename")

	if filename == "" {
		writeError(c, http.StatusBadRequest, "filename required")
		return
	}

	filename = filepath.Base(filename)
	filePath := filepath.Join(h.thumbnails.GetThumbnailDir(), filename)

	ext := strings.ToLower(filepath.Ext(filename))
	if ext != ".jpg" && ext != ".jpeg" && ext != ".png" {
		writeError(c, http.StatusBadRequest, "Invalid thumbnail format")
		return
	}

	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		writeError(c, http.StatusNotFound, "Thumbnail not found")
		return
	}

	// Mature content check: extract media ID from filename (e.g. "uuid.jpg" → "uuid"),
	// look up the media item, and serve a censored placeholder if the user isn't authorised.
	mediaID := strings.TrimSuffix(filename, ext)
	if item, err := h.media.GetMediaByID(mediaID); err == nil && item != nil && item.IsMature {
		canView := false
		if user := getUser(c); user != nil {
			canView = user.Permissions.CanViewMature && user.Preferences.ShowMature
		}
		if !canView {
			if censoredPath, cErr := h.thumbnails.GetPlaceholderPath("censored"); cErr == nil {
				c.Header("Cache-Control", "private, max-age=300")
				c.Header("Content-Type", "image/jpeg")
				http.ServeFile(c.Writer, c.Request, censoredPath)
				return
			}
			writeError(c, http.StatusForbidden, "Mature content")
			return
		}
	}

	c.Header("Cache-Control", "public, max-age=604800")
	c.Header("Content-Type", "image/jpeg")
	http.ServeFile(c.Writer, c.Request, filePath)
}

// GetThumbnailPreviews returns the preview thumbnail URLs for a media file
func (h *Handler) GetThumbnailPreviews(c *gin.Context) {
	if !h.requireThumbnails(c) {
		return
	}
	id := c.Query("id")

	// Receiver items have no local file for preview generation.
	if id != "" {
		if _, err := h.media.GetMediaByID(id); err != nil && h.receiver != nil {
			if h.receiver.GetMediaItem(id) != nil {
				writeSuccess(c, map[string]interface{}{"previews": []string{}})
				return
			}
		}
	}

	path, ok := h.resolveMediaByID(c, id)
	if !ok {
		return
	}

	// Block preview thumbnails for mature content when user is not authorised.
	if item, err := h.media.GetMedia(path); err == nil && item != nil && item.IsMature {
		canView := false
		if user := getUser(c); user != nil {
			canView = user.Permissions.CanViewMature && user.Preferences.ShowMature
		}
		if !canView {
			writeSuccess(c, map[string]interface{}{"previews": []string{}})
			return
		}
	}

	cfg := h.media.GetConfig()
	count := cfg.Thumbnails.PreviewCount
	if count <= 0 {
		count = 3
	}

	urls := h.thumbnails.GetPreviewURLs(path, id, count)
	writeSuccess(c, map[string]interface{}{
		"previews": urls,
	})
}

// GetThumbnailStats returns thumbnail generation stats
func (h *Handler) GetThumbnailStats(c *gin.Context) {
	if !h.requireThumbnails(c) {
		return
	}
	stats := h.thumbnails.GetStats()
	writeSuccess(c, map[string]interface{}{
		"total_thumbnails":   stats.Generated,
		"total_size_mb":      float64(stats.TotalSize) / (1024 * 1024),
		"pending_generation": stats.Pending,
		"generation_errors":  stats.Failed,
	})
}
