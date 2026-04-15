package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"media-server-pro/pkg/models"
)

// collectionWithItems is the API response for a collection including its ordered items.
type collectionWithItems struct {
	models.MediaCollection
	Items []collectionItemResponse `json:"items"`
}

type collectionItemResponse struct {
	MediaID   string `json:"media_id"`
	MediaName string `json:"media_name,omitempty"`
	Position  int    `json:"position"`
}

// ListCollections returns all collections ordered by name.
// GET /api/collections
func (h *Handler) ListCollections(c *gin.Context) {
	db := h.database.GORM().WithContext(c.Request.Context())
	var cols []models.MediaCollection
	if err := db.Order("name ASC").Find(&cols).Error; err != nil {
		writeError(c, http.StatusInternalServerError, "Failed to list collections: "+err.Error())
		return
	}
	writeSuccess(c, cols)
}

// GetCollection returns a single collection with its ordered media items.
// GET /api/collections/:id
func (h *Handler) GetCollection(c *gin.Context) {
	id := c.Param("id")
	db := h.database.GORM().WithContext(c.Request.Context())
	var col models.MediaCollection
	if err := db.First(&col, "id = ?", id).Error; err != nil {
		writeError(c, http.StatusNotFound, "Collection not found")
		return
	}
	var rows []models.MediaCollectionItem
	if err := db.Where("collection_id = ?", id).Order("position ASC, added_at ASC").Find(&rows).Error; err != nil {
		writeError(c, http.StatusInternalServerError, "Failed to fetch collection items: "+err.Error())
		return
	}

	// Enrich with media names
	ids := make([]string, len(rows))
	for i, r := range rows {
		ids[i] = r.MediaID
	}
	names := h.media.GetMediaNamesByIDs(ids)

	items := make([]collectionItemResponse, len(rows))
	for i, r := range rows {
		items[i] = collectionItemResponse{
			MediaID:   r.MediaID,
			MediaName: names[r.MediaID],
			Position:  r.Position,
		}
	}

	writeSuccess(c, collectionWithItems{MediaCollection: col, Items: items})
}

// CreateCollection creates a new media collection.
// POST /api/admin/collections
func (h *Handler) CreateCollection(c *gin.Context) {
	var body struct {
		Name         string `json:"name" binding:"required"`
		Description  string `json:"description"`
		CoverMediaID string `json:"cover_media_id"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		writeError(c, http.StatusBadRequest, "Invalid request: "+err.Error())
		return
	}
	col := models.MediaCollection{
		ID:           uuid.New().String(),
		Name:         body.Name,
		Description:  body.Description,
		CoverMediaID: body.CoverMediaID,
	}
	if err := h.database.GORM().WithContext(c.Request.Context()).Create(&col).Error; err != nil {
		writeError(c, http.StatusInternalServerError, "Failed to create collection: "+err.Error())
		return
	}
	session := getSession(c)
	if session != nil {
		h.logAdminAction(c, &adminLogActionParams{
			UserID: session.UserID, Username: session.Username,
			Action: "create_collection", Target: col.ID,
		})
	}
	writeSuccess(c, col)
}

// UpdateCollection updates collection metadata.
// PUT /api/admin/collections/:id
func (h *Handler) UpdateCollection(c *gin.Context) {
	id := c.Param("id")
	db := h.database.GORM().WithContext(c.Request.Context())
	var col models.MediaCollection
	if err := db.First(&col, "id = ?", id).Error; err != nil {
		writeError(c, http.StatusNotFound, "Collection not found")
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
		col.Name = *body.Name
	}
	if body.Description != nil {
		col.Description = *body.Description
	}
	if body.CoverMediaID != nil {
		col.CoverMediaID = *body.CoverMediaID
	}
	if err := db.Save(&col).Error; err != nil {
		writeError(c, http.StatusInternalServerError, "Failed to update collection: "+err.Error())
		return
	}
	writeSuccess(c, col)
}

// DeleteCollection deletes a collection and all its item associations.
// DELETE /api/admin/collections/:id
func (h *Handler) DeleteCollection(c *gin.Context) {
	id := c.Param("id")
	db := h.database.GORM().WithContext(c.Request.Context())
	// Remove items first, then the collection; surface errors so admins know if cleanup failed.
	if err := db.Where("collection_id = ?", id).Delete(&models.MediaCollectionItem{}).Error; err != nil {
		h.log.Error("DeleteCollection: failed to remove items for %s: %v", id, err)
		writeError(c, http.StatusInternalServerError, "Failed to remove collection items: "+err.Error())
		return
	}
	if err := db.Delete(&models.MediaCollection{}, "id = ?", id).Error; err != nil {
		writeError(c, http.StatusInternalServerError, "Failed to delete collection: "+err.Error())
		return
	}
	session := getSession(c)
	if session != nil {
		h.logAdminAction(c, &adminLogActionParams{
			UserID: session.UserID, Username: session.Username,
			Action: "delete_collection", Target: id,
		})
	}
	writeSuccess(c, gin.H{"message": "Collection deleted"})
}

// AddCollectionItems adds media items to a collection.
// POST /api/admin/collections/:id/items
// Body: { "media_ids": ["id1","id2"], "position_start": 0 }
func (h *Handler) AddCollectionItems(c *gin.Context) {
	collectionID := c.Param("id")
	db := h.database.GORM().WithContext(c.Request.Context())
	// Verify collection exists
	var col models.MediaCollection
	if err := db.First(&col, "id = ?", collectionID).Error; err != nil {
		writeError(c, http.StatusNotFound, "Collection not found")
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
	for i, mediaID := range body.MediaIDs {
		item := models.MediaCollectionItem{
			CollectionID: collectionID,
			MediaID:      mediaID,
			Position:     body.PositionStart + i,
		}
		// INSERT IGNORE equivalent — skip if already a member
		if err := db.Where(models.MediaCollectionItem{CollectionID: collectionID, MediaID: mediaID}).
			FirstOrCreate(&item).Error; err != nil {
			h.log.Error("AddCollectionItems: failed to insert %s into %s: %v", mediaID, collectionID, err)
			writeError(c, http.StatusInternalServerError, errInternalServer)
			return
		}
	}
	writeSuccess(c, gin.H{"message": "Items added", "count": len(body.MediaIDs)})
}

// RemoveCollectionItem removes a single media item from a collection.
// DELETE /api/admin/collections/:id/items/:media_id
func (h *Handler) RemoveCollectionItem(c *gin.Context) {
	collectionID := c.Param("id")
	mediaID := c.Param("media_id")
	result := h.database.GORM().WithContext(c.Request.Context()).
		Where("collection_id = ? AND media_id = ?", collectionID, mediaID).
		Delete(&models.MediaCollectionItem{})
	if result.Error != nil {
		writeError(c, http.StatusInternalServerError, "Failed to remove item: "+result.Error.Error())
		return
	}
	if result.RowsAffected == 0 {
		writeError(c, http.StatusNotFound, "Item not found in collection")
		return
	}
	writeSuccess(c, gin.H{"message": "Item removed"})
}

// GetMediaCollections returns all collections that contain the given media ID.
// GET /api/media/:id/collections
func (h *Handler) GetMediaCollections(c *gin.Context) {
	mediaID := c.Param("id")
	db := h.database.GORM().WithContext(c.Request.Context())
	var rows []models.MediaCollectionItem
	if err := db.Where("media_id = ?", mediaID).Find(&rows).Error; err != nil {
		writeError(c, http.StatusInternalServerError, "Failed to query collection memberships: "+err.Error())
		return
	}
	if len(rows) == 0 {
		writeSuccess(c, []collectionWithItems{})
		return
	}
	colIDs := make([]string, len(rows))
	posMap := make(map[string]int, len(rows))
	for i, r := range rows {
		colIDs[i] = r.CollectionID
		posMap[r.CollectionID] = r.Position
	}
	var cols []models.MediaCollection
	if err := db.Where("id IN ?", colIDs).Find(&cols).Error; err != nil {
		writeError(c, http.StatusInternalServerError, "Failed to fetch collections: "+err.Error())
		return
	}

	results := make([]gin.H, len(cols))
	for i, col := range cols {
		// Get all items for this collection (for prev/next navigation)
		var items []models.MediaCollectionItem
		if err := db.Where("collection_id = ?", col.ID).Order("position ASC, added_at ASC").Find(&items).Error; err != nil {
			h.log.Error("GetMediaCollections: failed to fetch items for collection %s: %v", col.ID, err)
		}
		names := h.media.GetMediaNamesByIDs(func() []string {
			ids := make([]string, len(items))
			for j, it := range items {
				ids[j] = it.MediaID
			}
			return ids
		}())
		itemResp := make([]collectionItemResponse, len(items))
		for j, it := range items {
			itemResp[j] = collectionItemResponse{
				MediaID:   it.MediaID,
				MediaName: names[it.MediaID],
				Position:  it.Position,
			}
		}
		results[i] = gin.H{
			"id":             col.ID,
			"name":           col.Name,
			"description":    col.Description,
			"cover_media_id": col.CoverMediaID,
			"created_at":     col.CreatedAt,
			"updated_at":     col.UpdatedAt,
			"items":          itemResp,
		}
	}
	writeSuccess(c, results)
}
