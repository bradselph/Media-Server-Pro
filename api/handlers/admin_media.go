package handlers

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"media-server-pro/internal/media"
	"media-server-pro/internal/thumbnails"
	"media-server-pro/pkg/helpers"
	"media-server-pro/pkg/models"
)

// AdminListMedia returns media items for admin management with sorting, filtering, and pagination.
// When limit > 0, uses DB-level pagination (ListMediaPaginated) so the catalog table is
// referenced and large libraries stay responsive.
func (h *Handler) AdminListMedia(c *gin.Context) {
	sortBy := c.Query("sort")
	if sortBy == "date" {
		sortBy = "date_modified"
	}

	var tags []string
	if t := c.Query("tags"); t != "" {
		tags = strings.Split(t, ",")
	}

	var isMature *bool
	if im := c.Query("is_mature"); im != "" {
		v := im == "true" || im == "1"
		isMature = &v
	}

	filter := media.Filter{
		Type:     models.MediaType(c.Query("type")),
		Category: c.Query("category"),
		Search:   c.Query("search"),
		Tags:     tags,
		IsMature: isMature,
		SortBy:   sortBy,
		SortDesc: c.Query("sort_order") == "desc",
	}

	limit := 50
	if l, err := strconv.Atoi(c.Query("limit")); err == nil && l > 0 {
		if l > 1000 {
			l = 1000
		}
		limit = l
	}

	page := 1
	if p, err := strconv.Atoi(c.Query("page")); err == nil && p > 1 {
		page = p
	}
	offset := (page - 1) * limit

	var items []*models.MediaItem
	var totalItems int64

	if limit > 0 {
		// Use DB-level pagination so media_metadata is queried and large libraries scale
		var err error
		items, totalItems, err = h.media.ListMediaPaginated(c.Request.Context(), filter, limit, offset)
		if err != nil {
			h.log.Warn("ListMediaPaginated failed, falling back to in-memory list: %v", err)
			allItems := h.media.ListMedia(filter)
			if allItems == nil {
				allItems = make([]*models.MediaItem, 0)
			}
			totalItems = int64(len(allItems))
			if offset >= len(allItems) {
				items = []*models.MediaItem{}
			} else {
				items = allItems[offset:]
				if limit < len(items) {
					items = items[:limit]
				}
			}
		}
	} else {
		allItems := h.media.ListMedia(filter)
		if allItems == nil {
			allItems = make([]*models.MediaItem, 0)
		}
		totalItems = int64(len(allItems))
		items = allItems
	}

	totalPages := 1
	if limit > 0 && totalItems > 0 {
		totalPages = int((totalItems + int64(limit) - 1) / int64(limit))
		if totalPages < 1 {
			totalPages = 1
		}
	}

	for _, item := range items {
		if item.ThumbnailURL == "" {
			if !h.thumbnails.HasThumbnail(thumbnails.MediaID(item.ID)) {
				isAudio := item.Type == "audio"
				if _, err := h.thumbnails.GenerateThumbnailRequest(&thumbnails.ThumbnailRequest{MediaPath: item.Path, MediaID: item.ID, IsAudio: isAudio, HighPriority: true}); err != nil && !errors.Is(err, thumbnails.ErrThumbnailPending) {
					h.log.Warn("Failed to queue thumbnail for %s: %v", item.Path, err)
				}
			}
			item.ThumbnailURL = h.thumbnails.GetThumbnailURL(thumbnails.MediaID(item.ID))
		}
	}

	writeSuccess(c, map[string]interface{}{
		"items":       items,
		"total_items": totalItems,
		"total_pages": totalPages,
	})
}

// AdminUpdateMedia updates media metadata
func (h *Handler) AdminUpdateMedia(c *gin.Context) {
	id := c.Param("id")
	path, ok := h.resolveMediaByID(c, id)
	if !ok {
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
	if _, ok := rawBody["category"]; ok {
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

	reqName = strings.TrimSpace(reqName)

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

	h.logAdminAction(c, &adminLogActionParams{UserID: "admin", Username: "admin", Action: "update_media", Target: path})

	if updatedItem, err := h.media.GetMedia(path); err == nil && updatedItem != nil {
		writeSuccess(c, updatedItem)
	} else {
		writeSuccess(c, map[string]string{"message": "Media updated"})
	}
}

// AdminDeleteMedia deletes a media file
func (h *Handler) AdminDeleteMedia(c *gin.Context) {
	id := c.Param("id")
	path, ok := h.resolveMediaByID(c, id)
	if !ok {
		return
	}

	if err := h.media.DeleteMedia(c.Request.Context(), path); err != nil {
		h.log.Error("%v", err)
		writeError(c, http.StatusInternalServerError, "Internal server error")
		return
	}

	h.logAdminAction(c, &adminLogActionParams{UserID: "admin", Username: "admin", Action: "delete_media", Target: path})
	writeSuccess(c, map[string]string{"message": "Media deleted"})
}

// AdminBulkMedia performs a bulk action (delete or update) on multiple media files.
func (h *Handler) AdminBulkMedia(c *gin.Context) {
	var req struct {
		IDs    []string               `json:"ids"`
		Action string                 `json:"action"`
		Data   map[string]interface{} `json:"data"`
	}
	if c.ShouldBindJSON(&req) != nil {
		writeError(c, http.StatusBadRequest, errInvalidRequest)
		return
	}

	if len(req.IDs) == 0 {
		writeError(c, http.StatusBadRequest, "ids must not be empty")
		return
	}
	if len(req.IDs) > 500 {
		writeError(c, http.StatusBadRequest, "too many ids (max 500)")
		return
	}
	if req.Action != "delete" && req.Action != "update" {
		writeError(c, http.StatusBadRequest, `action must be "delete" or "update"`)
		return
	}

	// Pre-validate update data before entering the loop so we don't partially
	// process items and then return an error mid-way.
	if req.Action == "update" {
		hasValidField := false
		if _, ok := req.Data["category"].(string); ok {
			hasValidField = true
		}
		if _, ok := req.Data["is_mature"].(bool); ok {
			hasValidField = true
		}
		if _, ok := req.Data["tags"]; ok {
			hasValidField = true
		}
		if !hasValidField {
			writeError(c, http.StatusBadRequest, "no valid fields in data for update action")
			return
		}
	}

	var successCount, failedCount int
	errs := make([]string, 0)

	for _, id := range req.IDs {
		if id == "" {
			continue
		}
		item, lookupErr := h.media.GetMediaByID(id)
		if lookupErr != nil || item == nil {
			failedCount++
			errs = append(errs, fmt.Sprintf("%s: media not found", id))
			continue
		}
		path := item.Path

		var opErr error
		switch req.Action {
		case "delete":
			opErr = h.media.DeleteMedia(c.Request.Context(), path)
			if opErr == nil {
				h.logAdminAction(c, &adminLogActionParams{UserID: "admin", Username: "admin", Action: "bulk_delete_media", Target: id})
			}
		case "update":
			updates := make(map[string]interface{})
			if cat, ok := req.Data["category"].(string); ok {
				updates["category"] = cat
			}
			if mature, ok := req.Data["is_mature"].(bool); ok {
				updates["is_mature"] = mature
			}
			if tagsRaw, ok := req.Data["tags"]; ok {
				// JSON decoding yields []interface{}, not []string
				if tv, ok := tagsRaw.([]interface{}); ok {
					tags := make([]string, 0, len(tv))
					for _, t := range tv {
						if s, ok := t.(string); ok {
							tags = append(tags, s)
						}
					}
					updates["tags"] = tags
				}
			}
			opErr = h.media.UpdateMetadata(path, updates)
			if opErr == nil {
				h.logAdminAction(c, &adminLogActionParams{UserID: "admin", Username: "admin", Action: "bulk_update_media", Target: id})
			}
		}
		if opErr != nil {
			h.log.Error("bulk %s %s: %v", req.Action, id, opErr)
			failedCount++
			errs = append(errs, fmt.Sprintf("%s: %v", id, opErr))
		} else {
			successCount++
		}
	}

	writeSuccess(c, map[string]interface{}{
		"success": successCount,
		"failed":  failedCount,
		"errors":  errs,
	})
}
