package handlers

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"

	"media-server-pro/internal/thumbnails"
)

// GenerateThumbnail generates a thumbnail for a media file
func (h *Handler) GenerateThumbnail(c *gin.Context) {
	var req struct {
		Path    string `json:"path"`
		IsAudio bool   `json:"is_audio"`
	}
	if c.ShouldBindJSON(&req) != nil {
		writeError(c, http.StatusBadRequest, errInvalidRequest)
		return
	}

	thumbnailPath, err := h.thumbnails.GenerateThumbnail(req.Path, req.IsAudio)
	if err != nil {
		h.log.Error("%v", err)
		writeError(c, http.StatusInternalServerError, "Internal server error")
		return
	}

	writeSuccess(c, map[string]string{
		"message":        "Thumbnail generated",
		"thumbnail_path": thumbnailPath,
	})
}

// GetThumbnail returns a thumbnail image.
func (h *Handler) GetThumbnail(c *gin.Context) {
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

	path := c.Query("path")
	if path == "" {
		writeError(c, http.StatusBadRequest, errPathRequired)
		return
	}

	cfg := h.media.GetConfig()
	validPath := false
	for _, dir := range []string{cfg.Directories.Videos, cfg.Directories.Music, cfg.Directories.Uploads} {
		if dir == "" {
			continue
		}
		cleanDir := filepath.Clean(dir)
		cleanPath := filepath.Clean(path)
		if cleanPath == cleanDir || strings.HasPrefix(cleanPath, cleanDir+string(os.PathSeparator)) {
			validPath = true
			break
		}
	}
	if !validPath {
		h.log.Warn("Thumbnail request for path outside media directories: %s", path)
		writeError(c, http.StatusForbidden, "Invalid media path")
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

	if !h.thumbnails.HasThumbnail(path) {
		isAudio := strings.HasSuffix(strings.ToLower(path), ".mp3") ||
			strings.HasSuffix(strings.ToLower(path), ".wav") ||
			strings.HasSuffix(strings.ToLower(path), ".flac") ||
			strings.HasSuffix(strings.ToLower(path), ".aac") ||
			strings.HasSuffix(strings.ToLower(path), ".ogg")

		_, err := h.thumbnails.GenerateThumbnailSync(path, isAudio)
		if err != nil && err != thumbnails.ErrThumbnailPending {
			h.log.Error("Failed to generate thumbnail for %s: %v", path, err)
			writeError(c, http.StatusNotFound, "Thumbnail generation failed")
			return
		}
	}

	thumbFilePath := h.thumbnails.GetThumbnailFilePath(path)

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

	c.Header("Cache-Control", "private, max-age=604800")
	c.Header("Content-Type", "image/jpeg")
	http.ServeFile(c.Writer, c.Request, thumbFilePath)
}

// ServeThumbnailFile serves a thumbnail image file by filename from the thumbnails directory
func (h *Handler) ServeThumbnailFile(c *gin.Context) {
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

	c.Header("Cache-Control", "public, max-age=604800")
	c.Header("Content-Type", "image/jpeg")
	http.ServeFile(c.Writer, c.Request, filePath)
}

// GetThumbnailPreviews returns the preview thumbnail URLs for a media file
func (h *Handler) GetThumbnailPreviews(c *gin.Context) {
	path := c.Query("path")
	if path == "" {
		writeError(c, http.StatusBadRequest, errPathRequired)
		return
	}

	cfg := h.media.GetConfig()
	count := cfg.Thumbnails.PreviewCount
	if count <= 0 {
		count = 3
	}

	urls := h.thumbnails.GetPreviewURLs(path, count)
	writeSuccess(c, map[string]interface{}{
		"previews": urls,
	})
}

// GetThumbnailStats returns thumbnail generation stats
func (h *Handler) GetThumbnailStats(c *gin.Context) {
	stats := h.thumbnails.GetStats()
	writeSuccess(c, map[string]interface{}{
		"total_thumbnails":   stats.Generated,
		"total_size_mb":      float64(stats.TotalSize) / (1024 * 1024),
		"pending_generation": stats.Pending,
		"generation_errors":  stats.Failed,
	})
}
