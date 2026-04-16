package handlers

import (
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"media-server-pro/internal/media"
	"media-server-pro/pkg/models"
)

// ListAutoTagRules returns all auto-tag rules ordered by priority desc.
// GET /api/admin/auto-tag-rules
func (h *Handler) ListAutoTagRules(c *gin.Context) {
	db := h.database.GORM().WithContext(c.Request.Context())
	var rules []models.AutoTagRule
	if err := db.Order("priority DESC, created_at ASC").Find(&rules).Error; err != nil {
		writeError(c, http.StatusInternalServerError, "Failed to list rules: "+err.Error())
		return
	}
	writeSuccess(c, rules)
}

// CreateAutoTagRule creates a new auto-tag rule.
// POST /api/admin/auto-tag-rules
func (h *Handler) CreateAutoTagRule(c *gin.Context) {
	var body struct {
		Name     string `json:"name" binding:"required"`
		Pattern  string `json:"pattern" binding:"required"`
		Tags     string `json:"tags" binding:"required"`
		Priority int    `json:"priority"`
		Enabled  *bool  `json:"enabled"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		writeError(c, http.StatusBadRequest, "Invalid request: "+err.Error())
		return
	}
	enabled := true
	if body.Enabled != nil {
		enabled = *body.Enabled
	}
	rule := models.AutoTagRule{
		ID:       uuid.New().String(),
		Name:     body.Name,
		Pattern:  body.Pattern,
		Tags:     body.Tags,
		Priority: body.Priority,
		Enabled:  enabled,
	}
	if err := h.database.GORM().WithContext(c.Request.Context()).Create(&rule).Error; err != nil {
		writeError(c, http.StatusInternalServerError, "Failed to create rule: "+err.Error())
		return
	}
	session := getSession(c)
	if session != nil {
		h.logAdminAction(c, &adminLogActionParams{
			UserID: session.UserID, Username: session.Username,
			Action: "create_auto_tag_rule", Target: rule.ID,
		})
	}
	writeSuccess(c, rule)
}

// UpdateAutoTagRule updates an existing auto-tag rule.
// PUT /api/admin/auto-tag-rules/:id
func (h *Handler) UpdateAutoTagRule(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		writeError(c, http.StatusBadRequest, "rule id is required")
		return
	}
	var body struct {
		Name     *string `json:"name"`
		Pattern  *string `json:"pattern"`
		Tags     *string `json:"tags"`
		Priority *int    `json:"priority"`
		Enabled  *bool   `json:"enabled"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		writeError(c, http.StatusBadRequest, "Invalid request: "+err.Error())
		return
	}
	db := h.database.GORM().WithContext(c.Request.Context())
	var rule models.AutoTagRule
	if err := db.First(&rule, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			writeError(c, http.StatusNotFound, "Rule not found")
		} else {
			writeError(c, http.StatusInternalServerError, "Failed to fetch rule: "+err.Error())
		}
		return
	}
	if body.Name != nil {
		rule.Name = *body.Name
	}
	if body.Pattern != nil {
		rule.Pattern = *body.Pattern
	}
	if body.Tags != nil {
		rule.Tags = *body.Tags
	}
	if body.Priority != nil {
		rule.Priority = *body.Priority
	}
	if body.Enabled != nil {
		rule.Enabled = *body.Enabled
	}
	if err := db.Save(&rule).Error; err != nil {
		writeError(c, http.StatusInternalServerError, "Failed to update rule: "+err.Error())
		return
	}
	writeSuccess(c, rule)
}

// DeleteAutoTagRule deletes an auto-tag rule by ID.
// DELETE /api/admin/auto-tag-rules/:id
func (h *Handler) DeleteAutoTagRule(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		writeError(c, http.StatusBadRequest, "rule id is required")
		return
	}
	if err := h.database.GORM().WithContext(c.Request.Context()).Delete(&models.AutoTagRule{}, "id = ?", id).Error; err != nil {
		writeError(c, http.StatusInternalServerError, "Failed to delete rule: "+err.Error())
		return
	}
	session := getSession(c)
	if session != nil {
		h.logAdminAction(c, &adminLogActionParams{
			UserID: session.UserID, Username: session.Username,
			Action: "delete_auto_tag_rule", Target: id,
		})
	}
	writeSuccess(c, gin.H{"message": "Rule deleted"})
}

// ApplyAutoTagRules applies all enabled rules to every media item in the library.
// Returns a count of items that had tags applied.
// POST /api/admin/auto-tag-rules/apply
func (h *Handler) ApplyAutoTagRules(c *gin.Context) {
	db := h.database.GORM().WithContext(c.Request.Context())
	var rules []models.AutoTagRule
	if err := db.Where("enabled = ?", true).Order("priority DESC, created_at ASC").Find(&rules).Error; err != nil {
		writeError(c, http.StatusInternalServerError, "Failed to load rules: "+err.Error())
		return
	}
	if len(rules) == 0 {
		writeSuccess(c, gin.H{"applied": 0, "items_affected": 0})
		return
	}

	allMedia := h.media.ListMedia(media.Filter{})
	affected := 0
	for _, item := range allMedia {
		pathLower := strings.ToLower(item.Path)
		for _, rule := range rules {
			if rule.Pattern == "" {
				continue
			}
			if !strings.Contains(pathLower, strings.ToLower(rule.Pattern)) {
				continue
			}
			tags := parseTags(rule.Tags)
			if len(tags) == 0 {
				continue
			}
			if err := h.media.UpdateTags(item.Path, tags); err != nil {
				h.log.Warn("ApplyAutoTagRules: tag update failed for %s: %v", item.Path, err)
			} else {
				affected++
			}
			break // one match per item (highest-priority rule wins)
		}
	}

	session := getSession(c)
	if session != nil {
		h.logAdminAction(c, &adminLogActionParams{
			UserID: session.UserID, Username: session.Username,
			Action: "apply_auto_tag_rules",
		})
	}
	writeSuccess(c, gin.H{"applied": len(rules), "items_affected": affected})
}

// parseTags splits a comma-separated tag string into a trimmed, non-empty slice.
func parseTags(raw string) []string {
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if t := strings.TrimSpace(p); t != "" {
			out = append(out, t)
		}
	}
	return out
}
