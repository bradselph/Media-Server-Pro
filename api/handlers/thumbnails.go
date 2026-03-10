package handlers

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"

	"media-server-pro/internal/thumbnails"
	"media-server-pro/pkg/helpers"
)

const batchThumbnailMaxIDs = 50

// acceptsWebP returns true if the request's Accept header includes image/webp
func acceptsWebP(r *http.Request) bool {
	accept := r.Header.Get("Accept")
	return strings.Contains(accept, "image/webp")
}

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

	_, err := h.thumbnails.GenerateThumbnailRequest(&thumbnails.ThumbnailRequest{MediaPath: absPath, MediaID: req.ID, IsAudio: req.IsAudio, HighPriority: true})
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

	if !h.thumbnails.HasThumbnail(thumbnails.MediaID(id)) {
		isAudio := helpers.IsAudioExtension(filepath.Ext(path))

		_, err := h.thumbnails.GenerateThumbnailSyncRequest(&thumbnails.ThumbnailSyncRequest{MediaPath: path, MediaID: id, IsAudio: isAudio})
		if err != nil && !errors.Is(err, thumbnails.ErrThumbnailPending) {
			h.log.Error("Failed to generate thumbnail for %s: %v", path, err)
			writeError(c, http.StatusNotFound, "Thumbnail generation failed")
			return
		}
	}

	// Responsive size: ?w=160|320|640 serves -sm/-md/-lg.webp when available
	widthParam := strings.TrimSpace(c.Query("w"))
	var thumbFilePath string
	contentType := "image/jpeg"
	wantWebP := acceptsWebP(c.Request)
	if widthParam != "" {
		var w int
		if _, err := fmt.Sscanf(widthParam, "%d", &w); err == nil {
			if fp := h.thumbnails.GetThumbnailFilePathForSize(thumbnails.MediaID(id), w); fp != "" {
				thumbFilePath = fp
				contentType = "image/webp" // responsive sizes are WebP-only
			}
		}
	}
	if thumbFilePath == "" {
		// Fall back to main thumbnail
		thumbFilePath = h.thumbnails.GetThumbnailFilePath(thumbnails.MediaID(id))
		if wantWebP {
			if webpPath := h.thumbnails.GetThumbnailFilePathWebp(thumbnails.MediaID(id)); webpPath != "" {
				thumbFilePath = webpPath
				contentType = "image/webp"
			}
		}
	}

	if _, err := os.Stat(thumbFilePath); os.IsNotExist(err) {
		h.log.Error("Thumbnail file does not exist: %s", thumbFilePath)
		writeError(c, http.StatusNotFound, "Thumbnail not found")
		return
	}

	if c.Request.Method == http.MethodHead {
		c.Header(headerContentType, contentType)
		c.Status(http.StatusOK)
		return
	}

	c.Header("Cache-Control", "public, max-age=604800")
	c.Header("Content-Type", contentType)
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

	// Content negotiation: serve WebP when client accepts it
	contentType := "image/jpeg"
	if acceptsWebP(c.Request) {
		webpPath := strings.TrimSuffix(filePath, ".jpg") + ".webp"
		if webpPath != filePath {
			if _, err := os.Stat(webpPath); err == nil {
				filePath = webpPath
				contentType = "image/webp"
			}
		}
	}

	c.Header("Cache-Control", "public, max-age=604800")
	c.Header("Content-Type", contentType)
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

// GetThumbnailBatch returns thumbnail URLs for multiple media IDs in one request.
// Query: ?ids=id1,id2,id3 (comma-separated, max 50). Optional ?w=320 for responsive sizes (Phase 1.2).
func (h *Handler) GetThumbnailBatch(c *gin.Context) {
	if !h.requireThumbnails(c) {
		return
	}
	idsParam := c.Query("ids")
	if idsParam == "" {
		writeError(c, http.StatusBadRequest, "ids parameter required")
		return
	}
	rawIDs := strings.Split(idsParam, ",")
	ids := make([]string, 0, len(rawIDs))
	seen := make(map[string]bool)
	for _, id := range rawIDs {
		id = strings.TrimSpace(id)
		if id != "" && !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	if len(ids) > batchThumbnailMaxIDs {
		ids = ids[:batchThumbnailMaxIDs]
	}
	w := strings.TrimSpace(c.Query("w"))

	thumbnailsMap := make(map[string]string, len(ids))
	for _, id := range ids {
		url := "/thumbnail?id=" + id
		if w != "" {
			url += "&w=" + w
		}
		thumbnailsMap[id] = url
	}
	writeSuccess(c, map[string]interface{}{"thumbnails": thumbnailsMap})
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
