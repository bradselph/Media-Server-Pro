package handlers

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"media-server-pro/pkg/models"
)

// ListChapters returns chapters for a given media ID, sorted by start_time.
// Query param: media_id (required)
func (h *Handler) ListChapters(c *gin.Context) {
	mediaID := c.Query("media_id")
	if mediaID == "" {
		writeError(c, http.StatusBadRequest, "media_id is required")
		return
	}

	db := h.database.GORM()
	if db == nil {
		h.log.Error("ListChapters: database unavailable")
		writeError(c, http.StatusServiceUnavailable, errInternalServer)
		return
	}

	var chapters []models.MediaChapter
	if err := db.Where("media_id = ?", mediaID).Order("start_time ASC").Find(&chapters).Error; err != nil {
		h.log.Error("ListChapters: %v", err)
		writeError(c, http.StatusInternalServerError, errInternalServer)
		return
	}

	if chapters == nil {
		chapters = []models.MediaChapter{}
	}
	writeSuccess(c, chapters)
}

// CreateChapter creates a new chapter for a media item.
// Requires admin role — chapters are metadata managed from the admin panel.
// Body: { media_id, start_time, end_time?, label }
func (h *Handler) CreateChapter(c *gin.Context) {
	session := RequireSession(c)
	if session == nil {
		return
	}
	if session.Role != models.RoleAdmin {
		writeError(c, http.StatusForbidden, "Chapter management requires admin privileges")
		return
	}

	var req struct {
		MediaID   string   `json:"media_id"`
		StartTime float64  `json:"start_time"`
		EndTime   *float64 `json:"end_time"`
		Label     string   `json:"label"`
	}
	if !BindJSON(c, &req, "") {
		return
	}

	// Validate input
	if req.MediaID == "" {
		writeError(c, http.StatusBadRequest, "media_id is required")
		return
	}
	if req.Label == "" {
		writeError(c, http.StatusBadRequest, "label is required")
		return
	}
	if req.StartTime < 0 {
		writeError(c, http.StatusBadRequest, "start_time must be >= 0")
		return
	}
	if req.EndTime != nil && *req.EndTime <= req.StartTime {
		writeError(c, http.StatusBadRequest, "end_time must be > start_time")
		return
	}

	db := h.database.GORM()
	if db == nil {
		h.log.Error("CreateChapter: database unavailable")
		writeError(c, http.StatusServiceUnavailable, errInternalServer)
		return
	}

	chapter := &models.MediaChapter{
		ID:        uuid.New().String(),
		MediaID:   req.MediaID,
		StartTime: req.StartTime,
		EndTime:   req.EndTime,
		Label:     req.Label,
	}

	if err := db.Create(chapter).Error; err != nil {
		h.log.Error("CreateChapter: %v", err)
		writeError(c, http.StatusInternalServerError, errInternalServer)
		return
	}

	writeSuccess(c, chapter)
}

// UpdateChapter updates an existing chapter.
// Requires admin role.
// URL param: id
// Body: { start_time?, end_time?, label? }
func (h *Handler) UpdateChapter(c *gin.Context) {
	session := RequireSession(c)
	if session == nil {
		return
	}
	if session.Role != models.RoleAdmin {
		writeError(c, http.StatusForbidden, "Chapter management requires admin privileges")
		return
	}

	id, ok := RequireParamID(c, "id")
	if !ok {
		return
	}

	var req struct {
		StartTime *float64 `json:"start_time"`
		EndTime   *float64 `json:"end_time"`
		Label     *string  `json:"label"`
	}
	if !BindJSON(c, &req, "") {
		return
	}

	db := h.database.GORM()
	if db == nil {
		h.log.Error("UpdateChapter: database unavailable")
		writeError(c, http.StatusServiceUnavailable, errInternalServer)
		return
	}

	// Fetch the existing chapter
	var chapter models.MediaChapter
	if err := db.First(&chapter, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			writeError(c, http.StatusNotFound, "Chapter not found")
			return
		}
		h.log.Error("UpdateChapter fetch: %v", err)
		writeError(c, http.StatusInternalServerError, errInternalServer)
		return
	}

	// Validate updates
	newStartTime := chapter.StartTime
	newEndTime := chapter.EndTime
	newLabel := chapter.Label

	if req.StartTime != nil {
		if *req.StartTime < 0 {
			writeError(c, http.StatusBadRequest, "start_time must be >= 0")
			return
		}
		newStartTime = *req.StartTime
	}
	if req.EndTime != nil {
		if *req.EndTime <= newStartTime {
			writeError(c, http.StatusBadRequest, "end_time must be > start_time")
			return
		}
		newEndTime = req.EndTime
	}
	if req.Label != nil {
		if *req.Label == "" {
			writeError(c, http.StatusBadRequest, "label cannot be empty")
			return
		}
		newLabel = *req.Label
	}
	// Cross-field validation: if only start_time was provided, check it against
	// the (unchanged) end_time. Without this a caller can silently invert a
	// range by pushing start_time past the existing end_time.
	if newEndTime != nil && *newEndTime <= newStartTime {
		writeError(c, http.StatusBadRequest, "end_time must be > start_time")
		return
	}

	// Update the chapter
	chapter.StartTime = newStartTime
	chapter.EndTime = newEndTime
	chapter.Label = newLabel

	if err := db.Save(&chapter).Error; err != nil {
		h.log.Error("UpdateChapter save: %v", err)
		writeError(c, http.StatusInternalServerError, errInternalServer)
		return
	}

	writeSuccess(c, chapter)
}

// DeleteChapter deletes a chapter.
// Requires admin role.
// URL param: id
func (h *Handler) DeleteChapter(c *gin.Context) {
	session := RequireSession(c)
	if session == nil {
		return
	}
	if session.Role != models.RoleAdmin {
		writeError(c, http.StatusForbidden, "Chapter management requires admin privileges")
		return
	}

	id, ok := RequireParamID(c, "id")
	if !ok {
		return
	}

	db := h.database.GORM()
	if db == nil {
		h.log.Error("DeleteChapter: database unavailable")
		writeError(c, http.StatusServiceUnavailable, errInternalServer)
		return
	}

	result := db.Delete(&models.MediaChapter{}, "id = ?", id)
	if result.Error != nil {
		h.log.Error("DeleteChapter: %v", result.Error)
		writeError(c, http.StatusInternalServerError, errInternalServer)
		return
	}

	if result.RowsAffected == 0 {
		writeError(c, http.StatusNotFound, "Chapter not found")
		return
	}

	writeSuccess(c, nil)
}
