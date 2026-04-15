package handlers

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"media-server-pro/internal/thumbnails"
	"media-server-pro/pkg/helpers"
)

const batchThumbnailMaxIDs = 50

const (
	mimeWebP = "image/webp"
	mimeJPEG = "image/jpeg"
)

// acceptsWebP returns true if the request's Accept header includes image/webp
func acceptsWebP(r *http.Request) bool {
	accept := r.Header.Get("Accept")
	return strings.Contains(accept, mimeWebP)
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
	if !BindJSON(c, &req, errInvalidRequest) {
		return
	}
	absPath, ok := h.resolveMediaByID(c, req.ID)
	if !ok {
		return
	}

	_, err := h.thumbnails.GenerateThumbnailRequest(&thumbnails.ThumbnailRequest{MediaPath: absPath, MediaID: req.ID, IsAudio: req.IsAudio, HighPriority: true})
	if err != nil && !errors.Is(err, thumbnails.ErrThumbnailPending) {
		h.log.Error("%v", err)
		writeError(c, http.StatusInternalServerError, errInternalServer)
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

// getResponsiveThumbPath returns the path and content type for ?w=N when available. ok is false when not used.
func (h *Handler) getResponsiveThumbPath(c *gin.Context, id string) (path, contentType string, ok bool) {
	widthParam := strings.TrimSpace(c.Query("w"))
	if widthParam == "" {
		return "", "", false
	}
	var w int
	if _, err := fmt.Sscanf(widthParam, "%d", &w); err != nil {
		return "", "", false
	}
	fp := h.thumbnails.GetThumbnailFilePathForSize(thumbnails.MediaID(id), w)
	if fp == "" {
		return "", "", false
	}
	return fp, mimeWebP, true
}

// getWebPThumbPath returns the WebP path when the client accepts WebP and it exists. ok is false otherwise.
func (h *Handler) getWebPThumbPath(id string, wantWebP bool) (path, contentType string, ok bool) {
	if !wantWebP {
		return "", "", false
	}
	webpPath := h.thumbnails.GetThumbnailFilePathWebp(thumbnails.MediaID(id))
	if webpPath == "" {
		return "", "", false
	}
	return webpPath, mimeWebP, true
}

// getThumbnailFilePathAndType resolves the thumbnail file path and content type (including responsive w= and WebP).
func (h *Handler) getThumbnailFilePathAndType(c *gin.Context, id string) (thumbFilePath, contentType string) {
	if fp, ct, ok := h.getResponsiveThumbPath(c, id); ok {
		return fp, ct
	}
	thumbFilePath = h.thumbnails.GetThumbnailFilePath(thumbnails.MediaID(id))
	if fp, ct, ok := h.getWebPThumbPath(id, acceptsWebP(c.Request)); ok {
		return fp, ct
	}
	return thumbFilePath, mimeJPEG
}

// tryServePlaceholderByType serves a placeholder image when type is placeholder/audio_placeholder/censored. Returns true if served.
func (h *Handler) tryServePlaceholderByType(c *gin.Context, thumbnailType string) bool {
	if thumbnailType != "placeholder" && thumbnailType != "audio_placeholder" && thumbnailType != "censored" {
		return false
	}
	placeholderPath, err := h.thumbnails.GetPlaceholderPath(thumbnailType)
	if err != nil {
		writeError(c, http.StatusInternalServerError, "Failed to get placeholder")
		return true // signal caller to return
	}
	if c.Request.Method == http.MethodHead {
		c.Header(headerContentType, mimeJPEG)
		c.Status(http.StatusOK)
		return true
	}
	c.Header(headerCacheControl,"public, max-age=2592000, immutable")
	c.Header(headerContentType,mimeJPEG)
	http.ServeFile(c.Writer, c.Request, placeholderPath)
	return true
}

// tryServeReceiverThumbnail serves a placeholder for receiver (remote) items that have no local file. Returns true if served.
func (h *Handler) tryServeReceiverThumbnail(c *gin.Context, id string) bool {
	if id == "" || h.receiver == nil {
		return false
	}
	if _, err := h.media.GetMediaByID(id); err == nil {
		return false
	}
	ri := h.receiver.GetMediaItem(id)
	if ri == nil {
		return false
	}
	placeholderType := "placeholder"
	if ri.MediaType == "audio" {
		placeholderType = "audio_placeholder"
	}
	if h.isReceiverItemMature(ri.ContentFingerprint) && !h.canViewMatureContent(c) {
		placeholderType = "censored"
	}
	ph, pErr := h.thumbnails.GetPlaceholderPath(placeholderType)
	if pErr != nil {
		writeError(c, http.StatusInternalServerError, "Failed to get placeholder")
		return true
	}
	c.Header(headerCacheControl,"public, max-age=86400")
	c.Header(headerContentType,mimeJPEG)
	http.ServeFile(c.Writer, c.Request, ph)
	return true
}

// serveCensoredPlaceholderOrForbidden serves the censored placeholder image or writes 403 if unavailable.
// no-store prevents the browser from caching this response under the same URL as the real thumbnail;
// without it, a guest visit would poison the browser cache so the censored image shows for authenticated users too.
func (h *Handler) serveCensoredPlaceholderOrForbidden(c *gin.Context) {
	censoredPath, err := h.thumbnails.GetPlaceholderPath("censored")
	if err != nil {
		writeError(c, http.StatusForbidden, "Mature content")
		return
	}
	c.Header(headerCacheControl,"no-store")
	c.Header(headerContentType,mimeJPEG)
	http.ServeFile(c.Writer, c.Request, censoredPath)
}

// tryServeCensoredIfMature serves censored placeholder when item is mature and user cannot view. Returns true if served or error written.
func (h *Handler) tryServeCensoredIfMature(c *gin.Context, path, _ string) bool {
	item, err := h.media.GetMedia(path)
	if err != nil || item == nil || !item.IsMature {
		return false
	}
	if h.canViewMatureContent(c) {
		return false
	}
	h.serveCensoredPlaceholderOrForbidden(c)
	return true
}

// ensureThumbnailGenerated generates the thumbnail synchronously if missing. Returns false if generation failed (error already written).
func (h *Handler) ensureThumbnailGenerated(c *gin.Context, path, id string) bool {
	if h.thumbnails.HasThumbnail(thumbnails.MediaID(id)) {
		return true
	}
	isAudio := helpers.IsAudioExtension(filepath.Ext(path))
	_, err := h.thumbnails.GenerateThumbnailSyncRequest(&thumbnails.ThumbnailSyncRequest{MediaPath: path, MediaID: id, IsAudio: isAudio})
	if err != nil && !errors.Is(err, thumbnails.ErrThumbnailPending) {
		h.log.Error("Failed to generate thumbnail for %s: %v", path, err)
		writeError(c, http.StatusNotFound, "Thumbnail generation failed")
		return false
	}
	return true
}

// serveThumbnailFileResponse writes the thumbnail file to the response (or HEAD). Returns false if file missing (error written).
// mature controls whether to use private caching (mature content must not be shared across users by proxies or browsers).
func (h *Handler) serveThumbnailFileResponse(c *gin.Context, thumbFilePath, contentType string, mature bool) bool {
	if _, err := os.Stat(thumbFilePath); os.IsNotExist(err) {
		h.log.Error("Thumbnail file does not exist: %s", thumbFilePath)
		writeError(c, http.StatusNotFound, "Thumbnail not found")
		return false
	}
	if c.Request.Method == http.MethodHead {
		c.Header(headerContentType, contentType)
		c.Status(http.StatusOK)
		return true
	}
	cacheControl := "public, max-age=604800"
	if mature {
		// private: browser may cache for the current user only; CDNs/proxies must not share it.
		cacheControl = "private, max-age=604800"
	}
	c.Header(headerCacheControl,cacheControl)
	c.Header(headerContentType,contentType)
	http.ServeFile(c.Writer, c.Request, thumbFilePath)
	return true
}

// GetThumbnail returns a thumbnail image.
func (h *Handler) GetThumbnail(c *gin.Context) {
	if !h.requireThumbnails(c) {
		return
	}
	if h.tryServePlaceholderByType(c, c.Query("type")) {
		return
	}
	id := c.Query("id")
	if h.tryServeReceiverThumbnail(c, id) {
		return
	}
	path, ok := h.resolveMediaByID(c, id)
	if !ok {
		return
	}
	if h.tryServeCensoredIfMature(c, path, id) {
		return
	}
	if !h.ensureThumbnailGenerated(c, path, id) {
		return
	}
	// Determine whether this item is mature so we can use private caching.
	isMature := false
	if item, err := h.media.GetMediaByID(id); err == nil && item != nil {
		isMature = item.IsMature
	}
	thumbFilePath, contentType := h.getThumbnailFilePathAndType(c, id)
	h.serveThumbnailFileResponse(c, thumbFilePath, contentType, isMature)
}

// ServeThumbnailFile serves a thumbnail image file by filename from the thumbnails directory.
func (h *Handler) ServeThumbnailFile(c *gin.Context) {
	if !h.requireThumbnails(c) {
		return
	}
	filename, ok := RequireParamID(c, "filename")
	if !ok {
		return
	}

	filename = filepath.Base(filename)
	filePath := filepath.Join(h.thumbnails.GetThumbnailDir(), filename)

	ext := strings.ToLower(filepath.Ext(filename))
	switch ext {
	case ".jpg", ".jpeg", ".png", ".webp":
	default:
		writeError(c, http.StatusBadRequest, "Invalid thumbnail format")
		return
	}

	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		writeError(c, http.StatusNotFound, "Thumbnail not found")
		return
	}

	// Mature content check: extract media ID from filename, stripping responsive
	// size suffixes (-sm, -md, -lg) and preview suffixes (_preview_N) so that
	// "uuid-sm.webp" and "uuid_preview_1.jpg" are correctly identified as "uuid".
	// Without this, GetMediaByID would fail and the mature check would be skipped.
	mediaID := strings.TrimSuffix(filename, ext)
	// Strip responsive size suffixes (-sm / -md / -lg)
	for _, sfx := range []string{"-sm", "-md", "-lg"} {
		if stripped := strings.TrimSuffix(mediaID, sfx); stripped != mediaID {
			mediaID = stripped
			break
		}
	}
	// Strip preview frame suffixes (_preview_N)
	if idx := strings.LastIndex(mediaID, "_preview_"); idx != -1 {
		mediaID = mediaID[:idx]
	}
	isMature := false
	if item, err := h.media.GetMediaByID(mediaID); err == nil && item != nil && item.IsMature {
		isMature = true
		canView := false
		if user := getUser(c); user != nil {
			canView = user.Permissions.CanViewMature && user.Preferences.ShowMature
		}
		if !canView {
			if censoredPath, cErr := h.thumbnails.GetPlaceholderPath("censored"); cErr == nil {
				// no-store prevents browser caching the censored image under the real thumbnail URL,
				// which would cause authenticated users to see the red placeholder after a guest visit.
				c.Header(headerCacheControl,"no-store")
				c.Header(headerContentType,mimeJPEG)
				http.ServeFile(c.Writer, c.Request, censoredPath)
				return
			}
			writeError(c, http.StatusForbidden, "Mature content")
			return
		}
	}

	// Content negotiation: serve WebP when client accepts it
	contentType := mimeJPEG
	switch ext {
	case ".png":
		contentType = "image/png"
	case ".webp":
		contentType = mimeWebP
	}
	if acceptsWebP(c.Request) && ext != ".webp" {
		webpPath := strings.TrimSuffix(filePath, ext) + ".webp"
		if webpPath != filePath {
			if _, err := os.Stat(webpPath); err == nil {
				filePath = webpPath
				contentType = mimeWebP
			}
		}
	}

	cacheControl := "public, max-age=604800"
	if isMature {
		cacheControl = "private, max-age=604800"
	}
	c.Header(headerCacheControl,cacheControl)
	c.Header(headerContentType,contentType)
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
				writeSuccess(c, map[string]any{"previews": []string{}})
				return
			}
		}
	}

	path, ok := h.resolveMediaByID(c, id)
	if !ok {
		return
	}

	// Block preview thumbnails for mature content when user is not authorized.
	if item, err := h.media.GetMedia(path); err == nil && item != nil && item.IsMature {
		canView := false
		if user := getUser(c); user != nil {
			canView = user.Permissions.CanViewMature && user.Preferences.ShowMature
		}
		if !canView {
			writeSuccess(c, map[string]any{"previews": []string{}})
			return
		}
	}

	cfg := h.media.GetConfig()
	count := cfg.Thumbnails.PreviewCount
	if count <= 0 {
		count = 3
	}

	urls := h.thumbnails.GetPreviewURLs(path, id, count)
	writeSuccess(c, map[string]any{
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
	// Validate w as a positive integer to prevent arbitrary string injection into URLs
	w := ""
	if raw := strings.TrimSpace(c.Query("w")); raw != "" {
		if wInt, err := strconv.Atoi(raw); err == nil && wInt > 0 {
			w = strconv.Itoa(wInt)
		}
	}

	thumbnailsMap := make(map[string]string, len(ids))
	for _, id := range ids {
		url := "/thumbnail?id=" + id
		if w != "" {
			url += "&w=" + w
		}
		thumbnailsMap[id] = url
	}
	writeSuccess(c, map[string]any{"thumbnails": thumbnailsMap})
}

// GetThumbnailStats returns thumbnail generation stats
func (h *Handler) GetThumbnailStats(c *gin.Context) {
	if !h.requireThumbnails(c) {
		return
	}
	stats := h.thumbnails.GetStats()
	resp := map[string]any{
		"total_thumbnails":   stats.Generated,
		"total_size_mb":      float64(stats.TotalSize) / (1024 * 1024),
		"pending_generation": stats.Pending,
		"generation_errors":  stats.Failed,
		"orphans_removed":    stats.OrphansRemoved,
		"excess_removed":     stats.ExcessRemoved,
		"corrupt_removed":    stats.CorruptRemoved,
	}
	if !stats.LastCleanup.IsZero() {
		resp["last_cleanup"] = stats.LastCleanup
	}
	writeSuccess(c, resp)
}
