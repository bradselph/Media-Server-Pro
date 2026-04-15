package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"

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

// buildSmartQuery applies rules to a GORM query against the media table.
// Returns nil if no conditions are valid.
func buildSmartQuery(db *gorm.DB, rules *SmartPlaylistRules) *gorm.DB {
	q := db.Table("media")
	var clauses []string
	var args []any
	for _, cond := range rules.Conditions {
		switch cond.Field {
		case "type":
			if cond.Op == "eq" && cond.Value != "" {
				clauses = append(clauses, "type = ?")
				args = append(args, cond.Value)
			}
		case "category":
			if cond.Op == "eq" && cond.Value != "" {
				clauses = append(clauses, "category = ?")
				args = append(args, cond.Value)
			}
		case "tags":
			// Normalize needle to match REPLACE(tags, ' ', '') applied to the haystack.
			needle := strings.ReplaceAll(cond.Value, " ", "")
			if needle != "" && !strings.Contains(needle, ",") {
				clauses = append(clauses, "FIND_IN_SET(?, REPLACE(tags, ' ', ''))")
				args = append(args, needle)
			}
		case "duration":
			if durVal, err := strconv.ParseFloat(cond.Value, 64); err == nil && durVal >= 0 {
				switch cond.Op {
				case "gte":
					clauses = append(clauses, "duration >= ?")
					args = append(args, durVal)
				case "lte":
					clauses = append(clauses, "duration <= ?")
					args = append(args, durVal)
				}
			}
		case "date_added_days":
			if cond.Op == "lte" && cond.Value != "" {
				cutoff := time.Now().AddDate(0, 0, -int(parseInt(cond.Value)))
				clauses = append(clauses, "date_added >= ?")
				args = append(args, cutoff)
			}
		case "views":
			if viewVal, err := strconv.ParseInt(cond.Value, 10, 64); err == nil && viewVal >= 0 {
				switch cond.Op {
				case "gte":
					clauses = append(clauses, "views >= ?")
					args = append(args, viewVal)
				case "lte":
					clauses = append(clauses, "views <= ?")
					args = append(args, viewVal)
				}
			}
		case "is_mature":
			if cond.Op == "eq" {
				val := cond.Value == "true" || cond.Value == "1"
				clauses = append(clauses, "is_mature = ?")
				args = append(args, val)
			}
		}
	}
	if len(clauses) == 0 {
		return q
	}
	sep := " AND "
	if rules.Match == "any" {
		sep = " OR "
	}
	combined := ""
	for i, cl := range clauses {
		if i > 0 {
			combined += sep
		}
		combined += cl
	}
	combined = "(" + combined + ")"
	q = q.Where(combined, args...)
	return q
}

func parseInt(s string) int64 {
	var v int64
	fmt.Sscanf(s, "%d", &v)
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
		writeError(c, http.StatusNotFound, "Smart playlist not found")
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
		writeError(c, http.StatusNotFound, "Smart playlist not found")
		return
	}
	if req.Name != nil {
		if *req.Name == "" {
			writeError(c, http.StatusBadRequest, "name cannot be empty")
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
		writeError(c, http.StatusNotFound, "Smart playlist not found")
		return
	}
	rules, err := parseSmartRules(sp.Rules)
	if err != nil {
		writeError(c, http.StatusInternalServerError, "Invalid rules stored in playlist")
		return
	}
	q := buildSmartQuery(db, rules)
	col := "date_added"
	switch rules.OrderBy {
	case "name", "duration", "views":
		col = rules.OrderBy
	}
	dir := "DESC"
	if rules.OrderDir == "asc" {
		dir = "ASC"
	}
	q = q.Order(col + " " + dir).Limit(rules.Limit)
	var items []models.MediaItem
	if err := q.Find(&items).Error; err != nil {
		h.log.Error("PreviewSmartPlaylist: %v", err)
		writeError(c, http.StatusInternalServerError, errInternalServer)
		return
	}
	if items == nil {
		items = []models.MediaItem{}
	}
	writeSuccess(c, items)
}
