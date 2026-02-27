package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"path/filepath"
	"strconv"

	"github.com/gin-gonic/gin"

	"media-server-pro/internal/media"
	"media-server-pro/internal/thumbnails"
	"media-server-pro/pkg/helpers"
	"media-server-pro/pkg/models"
)

// AdminListMedia returns media items for admin management with optional search and pagination.
func (h *Handler) AdminListMedia(c *gin.Context) {
	filter := media.Filter{
		Search: c.Query("search"),
	}
	limit := 50
	if l, err := strconv.Atoi(c.Query("limit")); err == nil && l > 0 {
		limit = l
	}
	filter.Limit = limit
	if p, err := strconv.Atoi(c.Query("page")); err == nil && p > 1 {
		filter.Offset = (p - 1) * limit
	}

	items := h.media.ListMedia(filter)
	if items == nil {
		items = make([]*models.MediaItem, 0)
	}

	for _, item := range items {
		if item.ThumbnailURL == "" {
			if !h.thumbnails.HasThumbnail(item.Path) {
				isAudio := item.Type == "audio"
				if _, err := h.thumbnails.GenerateThumbnail(item.Path, isAudio); err != nil && err != thumbnails.ErrThumbnailPending {
					h.log.Warn("Failed to queue thumbnail for %s: %v", item.Path, err)
				}
			}
			item.ThumbnailURL = h.thumbnails.GetThumbnailURL(item.Path)
		}
	}

	writeSuccess(c, items)
}

// AdminUpdateMedia updates media metadata
func (h *Handler) AdminUpdateMedia(c *gin.Context) {
	rawPath := c.Param("path")
	path, _ := url.PathUnescape(rawPath)

	if path == "" {
		writeError(c, http.StatusBadRequest, errPathParamRequired)
		return
	}

	var rawBody map[string]json.RawMessage
	if json.NewDecoder(c.Request.Body).Decode(&rawBody) != nil {
		writeError(c, http.StatusBadRequest, errInvalidRequest)
		return
	}

	var reqName string
	var reqTags []string
	var reqCategory string
	var reqIsMature bool
	var reqMatureContent bool
	var reqMetadata map[string]string

	if raw, ok := rawBody["name"]; ok {
		if err := json.Unmarshal(raw, &reqName); err != nil {
			writeError(c, http.StatusBadRequest, "Invalid 'name' field")
			return
		}
	}
	if raw, ok := rawBody["tags"]; ok {
		if err := json.Unmarshal(raw, &reqTags); err != nil {
			writeError(c, http.StatusBadRequest, "Invalid 'tags' field")
			return
		}
	}
	if raw, ok := rawBody["category"]; ok {
		if err := json.Unmarshal(raw, &reqCategory); err != nil {
			writeError(c, http.StatusBadRequest, "Invalid 'category' field")
			return
		}
	}
	if raw, ok := rawBody["metadata"]; ok {
		if err := json.Unmarshal(raw, &reqMetadata); err != nil {
			writeError(c, http.StatusBadRequest, "Invalid 'metadata' field")
			return
		}
		for k, v := range reqMetadata {
			if !helpers.ValidateMetadataKey(k) {
				writeError(c, http.StatusBadRequest, fmt.Sprintf("Invalid metadata key: %s", k))
				return
			}
			if !helpers.ValidateMetadataValue(v) {
				writeError(c, http.StatusBadRequest, fmt.Sprintf("Metadata value too large for key: %s", k))
				return
			}
		}
		reqMetadata = helpers.SanitizeMap(reqMetadata)
	}

	updates := make(map[string]interface{})
	if reqTags != nil {
		updates["tags"] = reqTags
	}
	if reqCategory != "" {
		updates["category"] = reqCategory
	}
	if raw, ok := rawBody["is_mature"]; ok {
		if err := json.Unmarshal(raw, &reqIsMature); err != nil {
			writeError(c, http.StatusBadRequest, "Invalid 'is_mature' field")
			return
		}
		updates["is_mature"] = reqIsMature
	}
	if raw, ok := rawBody["mature_content"]; ok {
		if err := json.Unmarshal(raw, &reqMatureContent); err != nil {
			writeError(c, http.StatusBadRequest, "Invalid 'mature_content' field")
			return
		}
		updates["is_mature"] = reqMatureContent
	}
	for k, v := range reqMetadata {
		updates[k] = v
	}

	reqName = trimSpace(reqName)

	if err := h.media.UpdateMetadata(path, updates); err != nil {
		h.log.Error("%v", err)
		writeError(c, http.StatusInternalServerError, "Internal server error")
		return
	}

	if reqName != "" {
		currentName := filepath.Base(path)
		if reqName != currentName {
			newPath, err := h.media.RenameMedia(path, reqName)
			if err != nil {
				h.log.Error("%v", err)
				writeError(c, http.StatusInternalServerError, "Internal server error")
				return
			}
			path = newPath
		}
	}

	h.admin.LogAction(c.Request.Context(), "admin", "admin", "update_media", path, nil, c.ClientIP(), true)

	if updatedItem, err := h.media.GetMedia(path); err == nil && updatedItem != nil {
		writeSuccess(c, updatedItem)
	} else {
		writeSuccess(c, map[string]string{"message": "Media updated", "path": path})
	}
}

// AdminDeleteMedia deletes a media file
func (h *Handler) AdminDeleteMedia(c *gin.Context) {
	rawPath := c.Param("path")
	path, _ := url.PathUnescape(rawPath)

	if path == "" {
		writeError(c, http.StatusBadRequest, errPathParamRequired)
		return
	}

	if err := h.media.DeleteMedia(c.Request.Context(), path); err != nil {
		h.log.Error("%v", err)
		writeError(c, http.StatusInternalServerError, "Internal server error")
		return
	}

	h.admin.LogAction(c.Request.Context(), "admin", "admin", "delete_media", path, nil, c.ClientIP(), true)
	writeSuccess(c, map[string]string{"message": "Media deleted"})
}

// AdminBulkMedia performs a bulk action (delete or update) on multiple media files.
func (h *Handler) AdminBulkMedia(c *gin.Context) {
	var req struct {
		Paths  []string               `json:"paths"`
		Action string                 `json:"action"`
		Data   map[string]interface{} `json:"data"`
	}
	if c.ShouldBindJSON(&req) != nil {
		writeError(c, http.StatusBadRequest, errInvalidRequest)
		return
	}

	if len(req.Paths) == 0 {
		writeError(c, http.StatusBadRequest, "paths must not be empty")
		return
	}
	if len(req.Paths) > 500 {
		writeError(c, http.StatusBadRequest, "too many paths (max 500)")
		return
	}
	if req.Action != "delete" && req.Action != "update" {
		writeError(c, http.StatusBadRequest, `action must be "delete" or "update"`)
		return
	}

	var successCount, failedCount int
	var errs []string
	clientIP := c.ClientIP()

	for _, path := range req.Paths {
		if path == "" {
			continue
		}
		var opErr error
		switch req.Action {
		case "delete":
			opErr = h.media.DeleteMedia(c.Request.Context(), path)
			if opErr == nil {
				h.admin.LogAction(c.Request.Context(), "admin", "admin", "bulk_delete_media", path, nil, clientIP, true)
			}
		case "update":
			updates := make(map[string]interface{})
			if cat, ok := req.Data["category"].(string); ok && cat != "" {
				updates["category"] = cat
			}
			if mature, ok := req.Data["is_mature"].(bool); ok {
				updates["is_mature"] = mature
			}
			if len(updates) == 0 {
				writeError(c, http.StatusBadRequest, "no valid fields in data for update action")
				return
			}
			opErr = h.media.UpdateMetadata(path, updates)
			if opErr == nil {
				h.admin.LogAction(c.Request.Context(), "admin", "admin", "bulk_update_media", path, nil, clientIP, true)
			}
		}
		if opErr != nil {
			h.log.Error("bulk %s %s: %v", req.Action, path, opErr)
			failedCount++
			errs = append(errs, fmt.Sprintf("%s: %v", path, opErr))
		} else {
			successCount++
		}
	}

	if errs == nil {
		errs = []string{}
	}
	writeSuccess(c, map[string]interface{}{
		"success": successCount,
		"failed":  failedCount,
		"errors":  errs,
	})
}

// trimSpace trims whitespace from a string (local helper to avoid import of strings in this file).
func trimSpace(s string) string {
	start, end := 0, len(s)
	for start < end && (s[start] == ' ' || s[start] == '\t' || s[start] == '\n' || s[start] == '\r') {
		start++
	}
	for end > start && (s[end-1] == ' ' || s[end-1] == '\t' || s[end-1] == '\n' || s[end-1] == '\r') {
		end--
	}
	return s[start:end]
}
