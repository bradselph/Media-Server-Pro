package handlers

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"media-server-pro/internal/media"
	"media-server-pro/pkg/models"
)

// SmartPlaylistRules is the parsed structure of a smart playlist's rules blob.
type SmartPlaylistRules struct {
	Match      string            `json:"match"`       // "all" or "any"
	Conditions []SmartCondition  `json:"conditions"`
	OrderBy    string            `json:"order_by"`    // date_added|name|duration|views
	OrderDir   string            `json:"order_dir"`   // asc|desc
	Limit      int               `json:"limit"`
}

type SmartCondition struct {
	Field string `json:"field"` // type|category|tags|duration|date_added_days|views|is_mature
	Op    string `json:"op"`    // eq|gte|lte|includes
	Value string `json:"value"`
}

func parseSmartRules(raw string) (*SmartPlaylistRules, error) {
	var r SmartPlaylistRules
	if err := json.Unmarshal([]byte(raw), &r); err != nil {
		return nil, err
	}
	if len(r.Conditions) > 100 {
		return nil, fmt.Errorf("too many conditions (max 100)")
	}
	if r.Match != "any" {
		r.Match = "all"
	}
	switch r.OrderBy {
	case "name", "duration", "views", "date_added":
	default:
		r.OrderBy = "date_added"
	}
	if r.OrderDir != "asc" {
		r.OrderDir = "desc"
	}
	if r.Limit <= 0 || r.Limit > 200 {
		r.Limit = 50
	}
	return &r, nil
}

// applySmartRules filters and sorts the media library using the parsed smart playlist rules.
// Filtering runs in-memory against the live media module — this is consistent with how all
// other media endpoints operate and avoids any dependency on the DB having a separate
// denormalised "media" table (which does not exist; media metadata lives in media_metadata).
func applySmartRules(all []*models.MediaItem, rules *SmartPlaylistRules) []*models.MediaItem {
	matchAll := rules.Match != "any"
	var out []*models.MediaItem
	if len(rules.Conditions) == 0 {
		out = make([]*models.MediaItem, len(all))
		copy(out, all)
	} else {
		for _, item := range all {
			if matchesSmartRules(item, rules.Conditions, matchAll) {
				out = append(out, item)
			}
		}
	}
	col := rules.OrderBy
	desc := rules.OrderDir == "desc"
	sort.SliceStable(out, func(i, j int) bool {
		var less bool
		switch col {
		case "name":
			less = out[i].Name < out[j].Name
		case "duration":
			less = out[i].Duration < out[j].Duration
		case "views":
			less = out[i].Views < out[j].Views
		default: // date_added
			less = out[i].DateAdded.Before(out[j].DateAdded)
		}
		if desc {
			return !less
		}
		return less
	})
	if rules.Limit > 0 && len(out) > rules.Limit {
		out = out[:rules.Limit]
	}
	return out
}

func matchesSmartRules(item *models.MediaItem, conds []SmartCondition, matchAll bool) bool {
	for _, cond := range conds {
		matched := matchSmartCondition(item, cond)
		if matchAll && !matched {
			return false
		}
		if !matchAll && matched {
			return true
		}
	}
	return matchAll
}

func matchSmartCondition(item *models.MediaItem, cond SmartCondition) bool {
	switch cond.Field {
	case "type":
		return cond.Op == "eq" && string(item.Type) == cond.Value
	case "category":
		return cond.Op == "eq" && item.Category == cond.Value
	case "tags":
		needle := strings.ToLower(strings.ReplaceAll(cond.Value, " ", ""))
		if needle == "" {
			return false
		}
		for _, t := range item.Tags {
			if strings.ToLower(strings.ReplaceAll(t, " ", "")) == needle {
				return true
			}
		}
		return false
	case "duration":
		durVal, err := strconv.ParseFloat(cond.Value, 64)
		if err != nil || durVal < 0 {
			return false
		}
		switch cond.Op {
		case "gte":
			return item.Duration >= durVal
		case "lte":
			return item.Duration <= durVal
		}
	case "date_added_days":
		if cond.Op != "lte" || cond.Value == "" {
			return false
		}
		cutoff := time.Now().AddDate(0, 0, -int(parseInt(cond.Value)))
		return !item.DateAdded.Before(cutoff)
	case "views":
		viewVal, err := strconv.ParseInt(cond.Value, 10, 64)
		if err != nil || viewVal < 0 {
			return false
		}
		switch cond.Op {
		case "gte":
			return int64(item.Views) >= viewVal
		case "lte":
			return int64(item.Views) <= viewVal
		}
	case "is_mature":
		val := cond.Value == "true" || cond.Value == "1"
		return cond.Op == "eq" && item.IsMature == val
	}
	return false
}

func parseInt(s string) int64 {
	v, err := strconv.ParseInt(s, 10, 64)
	if err != nil || v < 0 {
		return 0
	}
	return v
}

// ListSmartPlaylists returns all smart playlists for the current user.
func (h *Handler) ListSmartPlaylists(c *gin.Context) {
	session := RequireSession(c)
	if session == nil {
		return
	}
	db := h.database.GORM()
	if db == nil {
		writeError(c, http.StatusServiceUnavailable, errInternalServer)
		return
	}
	var sps []models.SmartPlaylist
	if err := db.Where("user_id = ?", session.UserID).Order("created_at DESC").Find(&sps).Error; err != nil {
		h.log.Error("ListSmartPlaylists: %v", err)
		writeError(c, http.StatusInternalServerError, errInternalServer)
		return
	}
	if sps == nil {
		sps = []models.SmartPlaylist{}
	}
	writeSuccess(c, sps)
}

// CreateSmartPlaylist creates a new smart playlist.
func (h *Handler) CreateSmartPlaylist(c *gin.Context) {
	session := RequireSession(c)
	if session == nil {
		return
	}
	var req struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Rules       string `json:"rules"`
	}
	if !BindJSON(c, &req, errInvalidRequest) {
		return
	}
	if req.Name == "" {
		writeError(c, http.StatusBadRequest, "name is required")
		return
	}
	if len(req.Name) > 255 {
		writeError(c, http.StatusBadRequest, "name too long (max 255)")
		return
	}
	if req.Rules == "" {
		req.Rules = `{"match":"all","conditions":[],"order_by":"date_added","order_dir":"desc","limit":50}`
	}
	if _, err := parseSmartRules(req.Rules); err != nil {
		writeError(c, http.StatusBadRequest, "invalid rules JSON")
		return
	}
	sp := &models.SmartPlaylist{
		ID:          uuid.New().String(),
		Name:        req.Name,
		Description: req.Description,
		UserID:      session.UserID,
		Rules:       req.Rules,
	}
	db := h.database.GORM()
	if db == nil {
		writeError(c, http.StatusServiceUnavailable, errInternalServer)
		return
	}
	if err := db.Create(sp).Error; err != nil {
		h.log.Error("CreateSmartPlaylist: %v", err)
		writeError(c, http.StatusInternalServerError, errInternalServer)
		return
	}
	writeSuccess(c, sp)
}

// GetSmartPlaylist returns a single smart playlist by ID.
func (h *Handler) GetSmartPlaylist(c *gin.Context) {
	session := RequireSession(c)
	if session == nil {
		return
	}
	id, ok := RequireParamID(c, "id")
	if !ok {
		return
	}
	db := h.database.GORM()
	if db == nil {
		writeError(c, http.StatusServiceUnavailable, errInternalServer)
		return
	}
	var sp models.SmartPlaylist
	if err := db.First(&sp, "id = ? AND user_id = ?", id, session.UserID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			writeError(c, http.StatusNotFound, "Smart playlist not found")
		} else {
			h.log.Error("GetSmartPlaylist: db error: %v", err)
			writeError(c, http.StatusInternalServerError, errInternalServer)
		}
		return
	}
	writeSuccess(c, sp)
}

// UpdateSmartPlaylist updates a smart playlist's name, description, or rules.
func (h *Handler) UpdateSmartPlaylist(c *gin.Context) {
	session := RequireSession(c)
	if session == nil {
		return
	}
	id, ok := RequireParamID(c, "id")
	if !ok {
		return
	}
	var req struct {
		Name        *string `json:"name"`
		Description *string `json:"description"`
		Rules       *string `json:"rules"`
	}
	if !BindJSON(c, &req, errInvalidRequest) {
		return
	}
	db := h.database.GORM()
	if db == nil {
		writeError(c, http.StatusServiceUnavailable, errInternalServer)
		return
	}
	var sp models.SmartPlaylist
	if err := db.First(&sp, "id = ? AND user_id = ?", id, session.UserID).Error; err != nil {
		// FND-0295: distinguish not-found from server errors so 5xx incidents are visible.
		if errors.Is(err, gorm.ErrRecordNotFound) {
			writeError(c, http.StatusNotFound, "Smart playlist not found")
		} else {
			h.log.Error("UpdateSmartPlaylist: db error: %v", err)
			writeError(c, http.StatusInternalServerError, errInternalServer)
		}
		return
	}
	if req.Name != nil {
		if *req.Name == "" {
			writeError(c, http.StatusBadRequest, "name cannot be empty")
			return
		}
		// FND-0297: enforce max length on update path too.
		if len(*req.Name) > 255 {
			writeError(c, http.StatusBadRequest, "name too long (max 255)")
			return
		}
		sp.Name = *req.Name
	}
	if req.Description != nil {
		sp.Description = *req.Description
	}
	if req.Rules != nil {
		if _, err := parseSmartRules(*req.Rules); err != nil {
			writeError(c, http.StatusBadRequest, "invalid rules JSON")
			return
		}
		sp.Rules = *req.Rules
	}
	if err := db.Save(&sp).Error; err != nil {
		h.log.Error("UpdateSmartPlaylist: %v", err)
		writeError(c, http.StatusInternalServerError, errInternalServer)
		return
	}
	writeSuccess(c, sp)
}

// DeleteSmartPlaylist deletes a smart playlist.
func (h *Handler) DeleteSmartPlaylist(c *gin.Context) {
	session := RequireSession(c)
	if session == nil {
		return
	}
	id, ok := RequireParamID(c, "id")
	if !ok {
		return
	}
	db := h.database.GORM()
	if db == nil {
		writeError(c, http.StatusServiceUnavailable, errInternalServer)
		return
	}
	result := db.Delete(&models.SmartPlaylist{}, "id = ? AND user_id = ?", id, session.UserID)
	if result.Error != nil {
		h.log.Error("DeleteSmartPlaylist: %v", result.Error)
		writeError(c, http.StatusInternalServerError, errInternalServer)
		return
	}
	if result.RowsAffected == 0 {
		writeError(c, http.StatusNotFound, "Smart playlist not found")
		return
	}
	writeSuccess(c, nil)
}

// PreviewSmartPlaylist executes the smart playlist rules and returns matching media items.
// Filtering runs against the in-memory media module so results include all filesystem
// metadata (type, tags, duration) that are not persisted to the DB as queryable columns.
func (h *Handler) PreviewSmartPlaylist(c *gin.Context) {
	session := RequireSession(c)
	if session == nil {
		return
	}
	id, ok := RequireParamID(c, "id")
	if !ok {
		return
	}
	db := h.database.GORM()
	if db == nil {
		writeError(c, http.StatusServiceUnavailable, errInternalServer)
		return
	}
	var sp models.SmartPlaylist
	if err := db.First(&sp, "id = ? AND user_id = ?", id, session.UserID).Error; err != nil {
		// FND-0295: distinguish not-found from server errors so 5xx incidents are visible.
		if errors.Is(err, gorm.ErrRecordNotFound) {
			writeError(c, http.StatusNotFound, "Smart playlist not found")
		} else {
			h.log.Error("PreviewSmartPlaylist: db error: %v", err)
			writeError(c, http.StatusInternalServerError, errInternalServer)
		}
		return
	}
	rules, err := parseSmartRules(sp.Rules)
	if err != nil {
		writeError(c, http.StatusInternalServerError, "Invalid rules stored in playlist")
		return
	}
	all := h.media.ListMedia(media.Filter{})
	items := applySmartRules(all, rules)
	if items == nil {
		items = []*models.MediaItem{}
	}
	writeSuccess(c, items)
}
