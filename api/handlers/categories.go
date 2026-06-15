package handlers

import (
	"context"
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"media-server-pro/internal/analytics"
	"media-server-pro/pkg/models"
)

// categoryNamesByIDs resolves curated category IDs to their names, omitting any
// ID that no longer corresponds to a category. Used by personalization surfaces
// (profile "Top categories", rated items) to render names instead of leaking
// opaque UUIDs, and to drop stale scores whose category was deleted (or that
// predate the curated-category migration).
func (h *Handler) categoryNamesByIDs(ctx context.Context, ids []string) map[string]string {
	out := make(map[string]string)
	if len(ids) == 0 {
		return out
	}
	gdb := h.database.GORM()
	if gdb == nil {
		return out
	}
	type row struct {
		ID   string
		Name string
	}
	var rows []row
	if err := gdb.WithContext(ctx).
		Model(&models.MediaCategory{}).
		Select("id, name").
		Where("id IN ?", ids).
		Scan(&rows).Error; err != nil {
		h.log.Warn("categoryNamesByIDs: %v", err)
		return out
	}
	for _, r := range rows {
		out[r.ID] = r.Name
	}
	return out
}

// Bounds for category inputs. Name maps to a VARCHAR(255) column; the
// description goes into a TEXT column but is still capped to keep payloads
// reasonable and prevent UI/format edge cases.
const (
	categoryMaxNameLength        = 255
	categoryMaxDescriptionLength = 5000
)

// categoryWithItems is the API response for a category including its ordered items.
type categoryWithItems struct {
	models.MediaCategory
	Items []categoryItemResponse `json:"items"`
}

type categoryItemResponse struct {
	MediaID   string `json:"media_id"`
	MediaName string `json:"media_name,omitempty"`
	Position  int    `json:"position"`
}

// ListCategories returns all categories ordered by name, each with its member
// count so callers (e.g. the home "Top categories" strip) can rank by size.
// GET /api/categories
func (h *Handler) ListCategories(c *gin.Context) {
	db := h.database.GORM().WithContext(c.Request.Context())
	var cats []models.MediaCategory
	if err := db.Order("name ASC").Find(&cats).Error; err != nil {
		writeError(c, http.StatusInternalServerError, "Failed to list categories: "+err.Error())
		return
	}
	// One grouped query for all member counts, then fan out onto each category.
	type catCount struct {
		CategoryID string
		Cnt        int
	}
	var counts []catCount
	if err := db.Model(&models.MediaCategoryItem{}).
		Select("category_id, COUNT(*) as cnt").
		Group("category_id").
		Scan(&counts).Error; err != nil {
		// Counts are best-effort; a failure leaves item_count at 0 rather than
		// failing the whole listing.
		h.log.Warn("ListCategories: failed to load item counts: %v", err)
	}
	countByID := make(map[string]int, len(counts))
	for _, cc := range counts {
		countByID[cc.CategoryID] = cc.Cnt
	}
	for i := range cats {
		cats[i].ItemCount = countByID[cats[i].ID]
	}
	writeSuccess(c, cats)
}

// GetCategory returns a single category with its ordered media items.
// GET /api/categories/:id
func (h *Handler) GetCategory(c *gin.Context) {
	id := c.Param("id")
	db := h.database.GORM().WithContext(c.Request.Context())
	var cat models.MediaCategory
	if err := db.First(&cat, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			writeError(c, http.StatusNotFound, "Category not found")
		} else {
			h.log.Error("GetCategory fetch: %v", err)
			writeError(c, http.StatusInternalServerError, errInternalServer)
		}
		return
	}
	var rows []models.MediaCategoryItem
	if err := db.Where("category_id = ?", id).Order("position ASC, added_at ASC").Find(&rows).Error; err != nil {
		writeError(c, http.StatusInternalServerError, "Failed to fetch category items: "+err.Error())
		return
	}

	// Enrich with media names
	ids := make([]string, len(rows))
	for i, r := range rows {
		ids[i] = r.MediaID
	}
	names := h.media.GetMediaNamesByIDs(ids)

	items := make([]categoryItemResponse, len(rows))
	for i, r := range rows {
		items[i] = categoryItemResponse{
			MediaID:   r.MediaID,
			MediaName: names[r.MediaID],
			Position:  r.Position,
		}
	}

	writeSuccess(c, categoryWithItems{MediaCategory: cat, Items: items})
}

// CreateCategory creates a new media category.
// POST /api/admin/categories
func (h *Handler) CreateCategory(c *gin.Context) {
	var body struct {
		Name         string `json:"name" binding:"required"`
		Description  string `json:"description"`
		CoverMediaID string `json:"cover_media_id"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		writeError(c, http.StatusBadRequest, "Invalid request: "+err.Error())
		return
	}
	if len(body.Name) > categoryMaxNameLength {
		writeError(c, http.StatusBadRequest, "name is too long")
		return
	}
	if len(body.Description) > categoryMaxDescriptionLength {
		writeError(c, http.StatusBadRequest, "description is too long")
		return
	}
	cat := models.MediaCategory{
		ID:           uuid.New().String(),
		Name:         body.Name,
		Description:  body.Description,
		CoverMediaID: body.CoverMediaID,
	}
	if err := h.database.GORM().WithContext(c.Request.Context()).Create(&cat).Error; err != nil {
		writeError(c, http.StatusInternalServerError, "Failed to create category: "+err.Error())
		return
	}
	h.trackServerEvent(c, analytics.EventCategoryCreate, map[string]any{
		"category_id": cat.ID,
		"name":        cat.Name,
	})
	writeSuccess(c, cat)
}

// UpdateCategory updates category metadata.
// PUT /api/admin/categories/:id
func (h *Handler) UpdateCategory(c *gin.Context) {
	id := c.Param("id")
	db := h.database.GORM().WithContext(c.Request.Context())
	var cat models.MediaCategory
	if err := db.First(&cat, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			writeError(c, http.StatusNotFound, "Category not found")
		} else {
			h.log.Error("UpdateCategory fetch: %v", err)
			writeError(c, http.StatusInternalServerError, errInternalServer)
		}
		return
	}
	var body struct {
		Name         *string `json:"name"`
		Description  *string `json:"description"`
		CoverMediaID *string `json:"cover_media_id"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		writeError(c, http.StatusBadRequest, "Invalid request: "+err.Error())
		return
	}
	if body.Name != nil {
		if *body.Name == "" {
			writeError(c, http.StatusBadRequest, "name cannot be empty")
			return
		}
		if len(*body.Name) > categoryMaxNameLength {
			writeError(c, http.StatusBadRequest, "name is too long")
			return
		}
		cat.Name = *body.Name
	}
	if body.Description != nil {
		if len(*body.Description) > categoryMaxDescriptionLength {
			writeError(c, http.StatusBadRequest, "description is too long")
			return
		}
		cat.Description = *body.Description
	}
	if body.CoverMediaID != nil {
		cat.CoverMediaID = *body.CoverMediaID
	}
	if err := db.Save(&cat).Error; err != nil {
		writeError(c, http.StatusInternalServerError, "Failed to update category: "+err.Error())
		return
	}
	h.trackServerEvent(c, analytics.EventCategoryUpdate, map[string]any{
		"category_id": cat.ID,
	})
	writeSuccess(c, cat)
}

// DeleteCategory deletes a category and all its item associations.
// DELETE /api/admin/categories/:id
func (h *Handler) DeleteCategory(c *gin.Context) {
	id := c.Param("id")
	db := h.database.GORM().WithContext(c.Request.Context())
	var rowsAffected int64
	if err := db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("category_id = ?", id).Delete(&models.MediaCategoryItem{}).Error; err != nil {
			return err
		}
		result := tx.Delete(&models.MediaCategory{}, "id = ?", id)
		rowsAffected = result.RowsAffected
		return result.Error
	}); err != nil {
		h.log.Error("DeleteCategory: %v", err)
		writeError(c, http.StatusInternalServerError, "Failed to delete category: "+err.Error())
		return
	}
	if rowsAffected == 0 {
		writeError(c, http.StatusNotFound, "Category not found")
		return
	}
	h.trackServerEvent(c, analytics.EventCategoryDelete, map[string]any{
		"category_id": id,
	})
	writeSuccess(c, gin.H{"message": "Category deleted"})
}

// AddCategoryItems adds media items to a category.
// POST /api/admin/categories/:id/items
// Body: { "media_ids": ["id1","id2"], "position_start": 0 }
func (h *Handler) AddCategoryItems(c *gin.Context) {
	categoryID := c.Param("id")
	db := h.database.GORM().WithContext(c.Request.Context())
	// Verify category exists
	var cat models.MediaCategory
	if err := db.First(&cat, "id = ?", categoryID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			writeError(c, http.StatusNotFound, "Category not found")
		} else {
			h.log.Error("AddCategoryItems fetch: %v", err)
			writeError(c, http.StatusInternalServerError, errInternalServer)
		}
		return
	}
	var body struct {
		MediaIDs      []string `json:"media_ids" binding:"required"`
		PositionStart int      `json:"position_start"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		writeError(c, http.StatusBadRequest, "Invalid request: "+err.Error())
		return
	}
	// Bound the batch so a single request can't trigger an unbounded per-item DB
	// loop. (binding:"required" already rejects an empty array.)
	if len(body.MediaIDs) > 500 {
		writeError(c, http.StatusBadRequest, "too many media_ids (max 500)")
		return
	}
	for i, mediaID := range body.MediaIDs {
		item := models.MediaCategoryItem{
			CategoryID: categoryID,
			MediaID:    mediaID,
			Position:   body.PositionStart + i,
		}
		// INSERT IGNORE equivalent — skip if already a member
		if err := db.Where(models.MediaCategoryItem{CategoryID: categoryID, MediaID: mediaID}).
			FirstOrCreate(&item).Error; err != nil {
			h.log.Error("AddCategoryItems: failed to insert %s into %s: %v", mediaID, categoryID, err)
			writeError(c, http.StatusInternalServerError, errInternalServer)
			return
		}
	}
	h.trackServerEvent(c, analytics.EventCategoryItemsAdd, map[string]any{
		"category_id": categoryID,
		"count":       len(body.MediaIDs),
	})
	writeSuccess(c, gin.H{"message": "Items added", "count": len(body.MediaIDs)})
}

// RemoveCategoryItem removes a single media item from a category.
// DELETE /api/admin/categories/:id/items/:media_id
func (h *Handler) RemoveCategoryItem(c *gin.Context) {
	categoryID := c.Param("id")
	mediaID := c.Param("media_id")
	result := h.database.GORM().WithContext(c.Request.Context()).
		Where("category_id = ? AND media_id = ?", categoryID, mediaID).
		Delete(&models.MediaCategoryItem{})
	if result.Error != nil {
		writeError(c, http.StatusInternalServerError, "Failed to remove item: "+result.Error.Error())
		return
	}
	if result.RowsAffected == 0 {
		writeError(c, http.StatusNotFound, "Item not found in category")
		return
	}
	h.trackServerEvent(c, analytics.EventCategoryItemRemove, map[string]any{
		"category_id": categoryID,
		"media_id":    mediaID,
	})
	writeSuccess(c, gin.H{"message": "Item removed"})
}

// GetMediaCategories returns all categories that contain the given media ID.
// GET /api/media/:id/categories
func (h *Handler) GetMediaCategories(c *gin.Context) {
	mediaID := c.Param("id")
	db := h.database.GORM().WithContext(c.Request.Context())
	var rows []models.MediaCategoryItem
	if err := db.Where("media_id = ?", mediaID).Find(&rows).Error; err != nil {
		writeError(c, http.StatusInternalServerError, "Failed to query category memberships: "+err.Error())
		return
	}
	if len(rows) == 0 {
		writeSuccess(c, []categoryWithItems{})
		return
	}
	catIDs := make([]string, len(rows))
	posMap := make(map[string]int, len(rows))
	for i, r := range rows {
		catIDs[i] = r.CategoryID
		posMap[r.CategoryID] = r.Position
	}
	var cats []models.MediaCategory
	if err := db.Where("id IN ?", catIDs).Find(&cats).Error; err != nil {
		writeError(c, http.StatusInternalServerError, "Failed to fetch categories: "+err.Error())
		return
	}

	results := make([]gin.H, len(cats))
	for i, cat := range cats {
		// Get all items for this category (for prev/next navigation)
		var items []models.MediaCategoryItem
		if err := db.Where("category_id = ?", cat.ID).Order("position ASC, added_at ASC").Find(&items).Error; err != nil {
			h.log.Error("GetMediaCategories: failed to fetch items for category %s: %v", cat.ID, err)
			writeError(c, http.StatusInternalServerError, errInternalServer)
			return
		}
		names := h.media.GetMediaNamesByIDs(func() []string {
			ids := make([]string, len(items))
			for j, it := range items {
				ids[j] = it.MediaID
			}
			return ids
		}())
		itemResp := make([]categoryItemResponse, len(items))
		for j, it := range items {
			itemResp[j] = categoryItemResponse{
				MediaID:   it.MediaID,
				MediaName: names[it.MediaID],
				Position:  it.Position,
			}
		}
		results[i] = gin.H{
			"id":             cat.ID,
			"name":           cat.Name,
			"description":    cat.Description,
			"cover_media_id": cat.CoverMediaID,
			"created_at":     cat.CreatedAt,
			"updated_at":     cat.UpdatedAt,
			"items":          itemResp,
		}
	}
	writeSuccess(c, results)
}
