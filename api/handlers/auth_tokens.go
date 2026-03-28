package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// ListAPITokens returns the user's API tokens (without the raw token value).
func (h *Handler) ListAPITokens(c *gin.Context) {
	session := RequireSession(c)
	if session == nil {
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
		CreatedAt  string  `json:"created_at"`
	}
	views := make([]tokenView, len(tokens))
	for i, t := range tokens {
		v := tokenView{
			ID:        t.ID,
			Name:      t.Name,
			CreatedAt: t.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
		}
		if t.LastUsedAt != nil {
			s := t.LastUsedAt.Format("2006-01-02T15:04:05Z07:00")
			v.LastUsedAt = &s
		}
		views[i] = v
	}
	writeSuccess(c, views)
}

// CreateAPIToken generates a new API token for the user.
// Body: {"name": "My Script"}
// The raw token value is returned once and never stored in plaintext.
func (h *Handler) CreateAPIToken(c *gin.Context) {
	session := RequireSession(c)
	if session == nil {
		return
	}
	var req struct {
		Name string `json:"name"`
	}
	if !BindJSON(c, &req, "") {
		return
	}
	if req.Name == "" {
		writeError(c, http.StatusBadRequest, "name is required")
		return
	}

	raw, rec, err := h.auth.CreateAPIToken(c.Request.Context(), session.UserID, req.Name)
	if err != nil {
		h.log.Error("CreateAPIToken: %v", err)
		writeError(c, http.StatusInternalServerError, "Failed to create API token")
		return
	}
	writeSuccess(c, gin.H{
		"id":         rec.ID,
		"name":       rec.Name,
		"token":      raw,
		"created_at": rec.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
	})
}

// DeleteAPIToken revokes an API token by ID.
func (h *Handler) DeleteAPIToken(c *gin.Context) {
	session := RequireSession(c)
	if session == nil {
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
	writeSuccess(c, nil)
}
