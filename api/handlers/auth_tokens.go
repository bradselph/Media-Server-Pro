package handlers

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"media-server-pro/pkg/models"
)

const (
	timeFormatRFC3339Ext = "2006-01-02T15:04:05Z07:00"
)

// ListAPITokens returns the user's API tokens (without the raw token value).
// Restricted to admin users only.
func (h *Handler) ListAPITokens(c *gin.Context) {
	session := RequireSession(c)
	if session == nil {
		return
	}
	if session.Role != models.RoleAdmin {
		writeError(c, http.StatusForbidden, "API token management requires elevated privileges")
		return
	}
	tokens, err := h.auth.ListAPITokens(c.Request.Context(), session.UserID)
	if err != nil {
		h.log.Error("ListAPITokens: %v", err)
		writeError(c, http.StatusInternalServerError, "Failed to list API tokens")
		return
	}

	type tokenView struct {
		ID         string  `json:"id"`
		Name       string  `json:"name"`
		LastUsedAt *string `json:"last_used_at"`
		ExpiresAt  *string `json:"expires_at"`
		CreatedAt  string  `json:"created_at"`
	}
	views := make([]tokenView, len(tokens))
	for i, t := range tokens {
		v := tokenView{
			ID:        t.ID,
			Name:      t.Name,
			CreatedAt: t.CreatedAt.Format(timeFormatRFC3339Ext),
		}
		if t.LastUsedAt != nil {
			v.LastUsedAt = new(t.LastUsedAt.Format(timeFormatRFC3339Ext))
		}
		if t.ExpiresAt != nil {
			v.ExpiresAt = new(t.ExpiresAt.Format(timeFormatRFC3339Ext))
		}
		views[i] = v
	}
	writeSuccess(c, views)
}

// CreateAPIToken generates a new API token for the user.
// Body: {"name": "My Script"}
// The raw token value is returned once and never stored in plaintext.
// Restricted to admin users only.
func (h *Handler) CreateAPIToken(c *gin.Context) {
	session := RequireSession(c)
	if session == nil {
		return
	}
	if session.Role != models.RoleAdmin {
		writeError(c, http.StatusForbidden, "API token management requires elevated privileges")
		return
	}
	var req struct {
		Name       string `json:"name"`
		TTLSeconds int    `json:"ttl_seconds"` // optional; 0 = no expiry
	}
	if !BindJSON(c, &req, "") {
		return
	}
	if req.Name == "" {
		writeError(c, http.StatusBadRequest, "name is required")
		return
	}
	var ttl time.Duration
	if req.TTLSeconds > 0 {
		ttl = time.Duration(req.TTLSeconds) * time.Second
	}

	raw, rec, err := h.auth.CreateAPIToken(c.Request.Context(), session.UserID, req.Name, ttl)
	if err != nil {
		h.log.Error("CreateAPIToken: %v", err)
		writeError(c, http.StatusInternalServerError, "Failed to create API token")
		return
	}
	resp := gin.H{
		"id":           rec.ID,
		"name":         rec.Name,
		"token":        raw,
		"created_at":   rec.CreatedAt.Format(timeFormatRFC3339Ext),
		"last_used_at": nil,  // always null on creation
		"expires_at":   nil,  // null when no expiry (consistent with list response)
	}
	if rec.ExpiresAt != nil {
		resp["expires_at"] = rec.ExpiresAt.Format(timeFormatRFC3339Ext)
	}
	h.logAdminAction(c, &adminLogActionParams{Action: "create_api_token", Target: rec.ID, Details: map[string]any{"name": rec.Name}})
	writeSuccess(c, resp)
}

// DeleteAPIToken revokes an API token by ID.
// Restricted to admin users only.
func (h *Handler) DeleteAPIToken(c *gin.Context) {
	session := RequireSession(c)
	if session == nil {
		return
	}
	if session.Role != models.RoleAdmin {
		writeError(c, http.StatusForbidden, "API token management requires elevated privileges")
		return
	}
	tokenID, ok := RequireParamID(c, "id")
	if !ok {
		return
	}
	if err := h.auth.DeleteAPIToken(c.Request.Context(), tokenID, session.UserID); err != nil {
		h.log.Error("DeleteAPIToken: %v", err)
		writeError(c, http.StatusInternalServerError, "Failed to delete API token")
		return
	}
	h.logAdminAction(c, &adminLogActionParams{Action: "delete_api_token", Target: tokenID})
	writeSuccess(c, nil)
}
