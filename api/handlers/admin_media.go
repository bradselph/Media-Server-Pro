package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"

	"media-server-pro/internal/media"
	"media-server-pro/internal/thumbnails"
	"media-server-pro/pkg/helpers"
	"media-server-pro/pkg/models"
)

// adminListParams holds parsed query params for AdminListMedia.
type adminListParams struct {
	filter media.Filter
	limit  int
	page   int
}

func parseAdminListSortBy(c *gin.Context) string {
	sortBy := c.Query("sort")
	if sortBy == "date" {
		return "date_modified"
	}
	return sortBy
}

func parseAdminListTags(c *gin.Context) []string {
	t := c.Query("tags")
	if t == "" {
		return nil
	}
	return strings.Split(t, ",")
}

func parseAdminListIsMature(c *gin.Context) *bool {
	im := c.Query("is_mature")
	if im == "" {
		return nil
	}
	return new(im == "true" || im == "1")
}

func parseAdminListLimit(c *gin.Context) int {
	return ParseQueryInt(c, "limit", QueryIntOpts{Default: 50, Min: 1, Max: 1000})
}

func parseAdminListPage(c *gin.Context) int {
	return ParseQueryInt(c, "page", QueryIntOpts{Default: 1, Min: 1, Max: 100000})
}

func parseAdminListQuery(c *gin.Context) adminListParams {
	return adminListParams{
		filter: media.Filter{
			Type:     models.MediaType(c.Query("type")),
			Category: c.Query("category"),
			Search:   c.Query("search"),
			Tags:     parseAdminListTags(c),
			IsMature: parseAdminListIsMature(c),
			SortBy:   parseAdminListSortBy(c),
			SortDesc: c.Query("sort_order") == "desc",
		},
		limit: parseAdminListLimit(c),
		page:  parseAdminListPage(c),
	}
}

func (h *Handler) fetchAdminListItems(ctx context.Context, filter media.Filter, limit, offset int) (items []*models.MediaItem, total int64) {
	if limit > 0 {
		items, total, err := h.media.ListMediaPaginated(ctx, filter, limit, offset)
		if err == nil {
			return items, total
		}
		h.log.Warn("ListMediaPaginated failed, falling back to in-memory list: %v", err)
		allItems := h.media.ListMedia(filter)
		if allItems == nil {
			allItems = make([]*models.MediaItem, 0)
		}
		total = int64(len(allItems))
		if offset >= len(allItems) {
			return []*models.MediaItem{}, total
		}
		items = allItems[offset:]
		if limit < len(items) {
			items = items[:limit]
		}
		return items, total
	}
	allItems := h.media.ListMedia(filter)
	if allItems == nil {
		allItems = make([]*models.MediaItem, 0)
	}
	return allItems, int64(len(allItems))
}

func computeAdminListTotalPages(totalItems int64, limit int) int {
	if limit <= 0 || totalItems <= 0 {
		return 1
	}
	n := int((totalItems + int64(limit) - 1) / int64(limit))
	if n < 1 {
		return 1
	}
	return n
}

func (h *Handler) enrichAdminListThumbnails(items []*models.MediaItem) {
	if h.thumbnails == nil {
		return
	}
	for _, item := range items {
		if item.ThumbnailURL != "" {
			continue
		}
		if !h.thumbnails.HasThumbnail(thumbnails.MediaID(item.ID)) {
			isAudio := item.Type == "audio"
			_, err := h.thumbnails.GenerateThumbnailRequest(&thumbnails.ThumbnailRequest{MediaPath: item.Path, MediaID: item.ID, IsAudio: isAudio, HighPriority: true})
			if err != nil && !errors.Is(err, thumbnails.ErrThumbnailPending) {
				h.log.Warn("Failed to queue thumbnail for %s: %v", item.Path, err)
			}
		}
		item.ThumbnailURL = h.thumbnails.GetThumbnailURL(thumbnails.MediaID(item.ID))
	}
}

// AdminListMedia returns media items for admin management with sorting, filtering, and pagination.
// When limit > 0, uses DB-level pagination (ListMediaPaginated) so the catalog table is
// referenced and large libraries stay responsive.
func (h *Handler) AdminListMedia(c *gin.Context) {
	params := parseAdminListQuery(c)
	offset := (params.page - 1) * params.limit

	items, totalItems := h.fetchAdminListItems(c.Request.Context(), params.filter, params.limit, offset)
	totalPages := computeAdminListTotalPages(totalItems, params.limit)
	h.enrichAdminListThumbnails(items)

	writeSuccess(c, map[string]any{
		"items":       items,
		"total_items": totalItems,
		"total_pages": totalPages,
	})
}

// adminUpdateRequest holds parsed fields from the update JSON body.
type adminUpdateRequest struct {
	updates map[string]any
	name    string
}

// decodeAdminUpdateField unmarshals one optional JSON field. Returns "" on success or when key is absent, or an error message on decode failure.
func decodeAdminUpdateField(rawBody map[string]json.RawMessage, key string, dest any, invalidMsg string) string {
	raw, ok := rawBody[key]
	if !ok {
		return ""
	}
	if err := json.Unmarshal(raw, dest); err != nil {
		return invalidMsg
	}
	return ""
}

// validateAdminMetadata checks metadata keys/values and returns the sanitized map or an error message.
func validateAdminMetadata(metadata map[string]string) (sanitized map[string]string, errMsg string) {
	for k, v := range metadata {
		if !helpers.ValidateMetadataKey(k) {
			return nil, fmt.Sprintf("Invalid metadata key: %s", k)
		}
		if !helpers.ValidateMetadataValue(v) {
			return nil, fmt.Sprintf("Metadata value too large for key: %s", k)
		}
	}
	return helpers.SanitizeMap(metadata), ""
}

// parseAdminUpdateBody decodes the JSON body and builds the updates map.
// Returns an error message (for writeError) or empty string on success.
func parseAdminUpdateBody(rawBody map[string]json.RawMessage) (req adminUpdateRequest, errMsg string) {
	var reqName string
	var reqTags []string
	var reqCategory string
	var reqIsMature bool
	var reqMatureContent bool
	var reqMetadata map[string]string

	if msg := decodeAdminUpdateField(rawBody, "name", &reqName, "Invalid 'name' field"); msg != "" {
		return adminUpdateRequest{}, msg
	}
	if msg := decodeAdminUpdateField(rawBody, "tags", &reqTags, "Invalid 'tags' field"); msg != "" {
		return adminUpdateRequest{}, msg
	}
	if msg := decodeAdminUpdateField(rawBody, "category", &reqCategory, "Invalid 'category' field"); msg != "" {
		return adminUpdateRequest{}, msg
	}
	if msg := decodeAdminUpdateField(rawBody, "metadata", &reqMetadata, "Invalid 'metadata' field"); msg != "" {
		return adminUpdateRequest{}, msg
	}
	if reqMetadata != nil {
		var errMsg string
		reqMetadata, errMsg = validateAdminMetadata(reqMetadata)
		if errMsg != "" {
			return adminUpdateRequest{}, errMsg
		}
	}

	updates := make(map[string]any)
	if reqTags != nil {
		updates["tags"] = reqTags
	}
	if _, ok := rawBody["category"]; ok {
		updates["category"] = reqCategory
	}
	if msg := decodeAdminUpdateField(rawBody, "is_mature", &reqIsMature, "Invalid 'is_mature' field"); msg != "" {
		return adminUpdateRequest{}, msg
	}
	if _, ok := rawBody["is_mature"]; ok {
		updates["is_mature"] = reqIsMature
	}
	if msg := decodeAdminUpdateField(rawBody, "mature_content", &reqMatureContent, "Invalid 'mature_content' field"); msg != "" {
		return adminUpdateRequest{}, msg
	}
	if _, ok := rawBody["mature_content"]; ok {
		// mature_content is a legacy alias; only apply if is_mature wasn't explicitly set
		if _, alreadySet := rawBody["is_mature"]; !alreadySet {
			updates["is_mature"] = reqMatureContent
		}
	}
	// Reserved keys are handled as typed values above; silently skip any
	// collision from the custom metadata map so a key like "is_mature" in the
	// metadata object cannot overwrite the explicitly-decoded bool and cause
	// applyMetadataField to receive the wrong type (string instead of bool).
	reservedMetadataKeys := map[string]bool{
		"tags": true, "is_mature": true, "mature_content": true,
		"mature_score": true, "category": true, "views": true,
	}
	for k, v := range reqMetadata {
		if !reservedMetadataKeys[k] {
			updates[k] = v
		}
	}

	return adminUpdateRequest{updates: updates, name: strings.TrimSpace(reqName)}, ""
}

// applyAdminRenameIfNeeded renames the media file when name is non-empty and different from current. Returns the path to use (possibly updated).
func (h *Handler) applyAdminRenameIfNeeded(path, reqName string) (string, error) {
	if reqName == "" {
		return path, nil
	}
	currentName := filepath.Base(path)
	if reqName == currentName {
		return path, nil
	}
	newPath, err := h.media.RenameMedia(path, reqName)
	if err != nil {
		return "", err
	}
	return newPath, nil
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

	parsed, errMsg := parseAdminUpdateBody(rawBody)
	if errMsg != "" {
		writeError(c, http.StatusBadRequest, errMsg)
		return
	}

	// Rename first: if the rename fails we must not commit metadata changes that
	// reference a filename/path that doesn't exist on disk yet.
	newPath, err := h.applyAdminRenameIfNeeded(path, parsed.name)
	if err != nil {
		h.log.Error("%v", err)
		writeError(c, http.StatusInternalServerError, errInternalServer)
		return
	}

	if err := h.media.UpdateMetadata(newPath, parsed.updates); err != nil {
		h.log.Error("%v", err)
		writeError(c, http.StatusInternalServerError, errInternalServer)
		return
	}

	path = newPath

	h.logAdminAction(c, &adminLogActionParams{Action: "update_media", Target: path})

	updatedItem, getErr := h.media.GetMedia(path)
	if getErr == nil && updatedItem != nil {
		writeSuccess(c, updatedItem)
		return
	}
	writeSuccess(c, map[string]string{"message": "Media updated"})
}

// AdminDeleteMedia deletes a media file and cleans up all associated data.
func (h *Handler) AdminDeleteMedia(c *gin.Context) {
	id := c.Param("id")
	path, ok := h.resolveMediaByID(c, id)
	if !ok {
		return
	}

	if err := h.media.DeleteMedia(c.Request.Context(), path); err != nil {
		h.log.Error("%v", err)
		writeError(c, http.StatusInternalServerError, errInternalServer)
		return
	}

	// Clean up associated data that DeleteMedia does not handle.
	// These are best-effort — failures are logged but do not block the delete response.
	h.cleanupDeletedMedia(c.Request.Context(), id, path)

	h.logAdminAction(c, &adminLogActionParams{Action: "delete_media", Target: path})
	writeSuccess(c, map[string]string{"message": "Media deleted"})
}

// cleanupDeletedMedia removes HLS cache, thumbnails, and other data associated
// with a media item that was just deleted. All operations are best-effort.
func (h *Handler) cleanupDeletedMedia(ctx context.Context, mediaID, mediaPath string) {
	// HLS cache and job
	if h.hls != nil {
		if job, err := h.hls.GetJobByMediaPath(mediaPath); err == nil && job != nil {
			if delErr := h.hls.DeleteJob(job.ID); delErr != nil {
				h.log.Warn("Failed to cleanup HLS job for deleted media %s: %v", mediaID, delErr)
			}
		}
	}

	// Thumbnails (main + previews)
	if h.thumbnails != nil {
		thumbID := thumbnails.MediaID(mediaID)
		thumbPath := h.thumbnails.GetThumbnailPath(thumbID)
		if thumbPath != "" {
			_ = os.Remove(thumbPath)
			// Also remove WebP variant and preview frames
			_ = os.Remove(thumbPath[:len(thumbPath)-len(filepath.Ext(thumbPath))] + ".webp")
			// Preview frames: <id>_preview_0.jpg, _preview_1.jpg, etc.
			dir := filepath.Dir(thumbPath)
			base := string(thumbID)
			if entries, err := os.ReadDir(dir); err == nil {
				for _, e := range entries {
					if strings.HasPrefix(e.Name(), base+"_preview_") {
						_ = os.Remove(filepath.Join(dir, e.Name()))
					}
				}
			}
		}
	}

	// Purge analytics events and playback positions that have no FK cascade.
	// Favorites, playlist items, and watch history entries have ON DELETE CASCADE
	// FK constraints and are cleaned automatically by the DB.
	if h.analytics != nil {
		h.analytics.DeleteEventsByMedia(ctx, mediaID)
	}
	if h.media != nil {
		h.media.DeletePlaybackPositionsByPath(ctx, mediaPath)
	}
	// Purge suggestion view history rows keyed by media path (no FK cascade on that column).
	if h.suggestions != nil {
		h.suggestions.PurgeMediaPath(mediaPath)
	}

	// Remove path-keyed rows that have no FK cascade to media_metadata.
	if h.scanner != nil {
		h.scanner.RemoveByPath(mediaPath)
	}
	if h.validator != nil {
		h.validator.ClearResult(mediaPath)
	}
	if h.categorizer != nil {
		h.categorizer.RemoveByPath(mediaPath)
	}
}

// extractStringSlice converts a []any from JSON into []string, ignoring non-string elements.
func extractStringSlice(tagsRaw any) ([]string, bool) {
	tv, ok := tagsRaw.([]any)
	if !ok {
		return nil, false
	}
	tags := make([]string, 0, len(tv))
	for _, t := range tv {
		if s, ok := t.(string); ok {
			tags = append(tags, s)
		}
	}
	return tags, true
}

// buildBulkUpdateFields builds an updates map from request data for bulk update.
// Returns the map and true if at least one valid field was present.
func buildBulkUpdateFields(data map[string]any) (map[string]any, bool) {
	updates := make(map[string]any)
	hasValid := false
	if cat, ok := data["category"].(string); ok {
		updates["category"] = cat
		hasValid = true
	}
	if mature, ok := data["is_mature"].(bool); ok {
		updates["is_mature"] = mature
		hasValid = true
	}
	if tagsRaw, ok := data["tags"]; ok {
		if tags, ok := extractStringSlice(tagsRaw); ok {
			updates["tags"] = tags
			hasValid = true
		}
	}
	return updates, hasValid
}

// processOneBulkMediaItem runs delete or update for a single media ID.
// Returns nil on success, or an error message string on failure.
func (h *Handler) processOneBulkMediaItem(c *gin.Context, id, action string, updates map[string]any) error {
	if id == "" {
		return nil
	}
	item, lookupErr := h.media.GetMediaByID(id)
	if lookupErr != nil || item == nil {
		return fmt.Errorf("%s: media not found", id)
	}
	path := item.Path
	switch action {
	case "delete":
		if err := h.media.DeleteMedia(c.Request.Context(), path); err != nil {
			return err
		}
		h.cleanupDeletedMedia(c.Request.Context(), id, path)
		h.logAdminAction(c, &adminLogActionParams{Action: "bulk_delete_media", Target: id})
		return nil
	case "update":
		if err := h.media.UpdateMetadata(path, updates); err != nil {
			return err
		}
		h.logAdminAction(c, &adminLogActionParams{Action: "bulk_update_media", Target: id})
		return nil
	default:
		return fmt.Errorf("unsupported bulk action: %s", action)
	}
}

// AdminBulkMedia performs a bulk action (delete or update) on multiple media files.
func (h *Handler) AdminBulkMedia(c *gin.Context) {
	var req struct {
		IDs    []string               `json:"ids"`
		Action string                 `json:"action"`
		Data   map[string]any `json:"data"`
	}
	if !BindJSON(c, &req, "") {
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

	var updates map[string]any
	if req.Action == "update" {
		var hasValid bool
		updates, hasValid = buildBulkUpdateFields(req.Data)
		if !hasValid {
			writeError(c, http.StatusBadRequest, "no valid fields in data for update action")
			return
		}
	}

	var successCount, failedCount int
	errs := make([]string, 0)
	for _, id := range req.IDs {
		opErr := h.processOneBulkMediaItem(c, id, req.Action, updates)
		if opErr != nil {
			h.log.Error("bulk %s %s: %v", req.Action, id, opErr)
			failedCount++
			errs = append(errs, opErr.Error())
		} else if id != "" {
			successCount++
		}
	}

	writeSuccess(c, map[string]any{
		"success": successCount,
		"failed":  failedCount,
		"errors":  errs,
	})
}
