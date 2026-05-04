package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// GetFavorites returns the authenticated user's favorite media items.
func (h *Handler) GetFavorites(c *gin.Context) {
	session := RequireSession(c)
	if session == nil {
		return
	}
	recs, err := h.auth.GetFavorites(c.Request.Context(), session.UserID)
	if err != nil {
		h.log.Error("GetFavorites: %v", err)
		writeError(c, http.StatusInternalServerError, "Failed to retrieve favorites")
		return
	}

	type favoriteItem struct {
		ID      string `json:"id"`
		MediaID string `json:"media_id"`
		AddedAt string `json:"added_at"`
	}
	items := make([]favoriteItem, len(recs))
	for i, r := range recs {
		items[i] = favoriteItem{
			ID:      r.ID,
			MediaID: r.MediaID,
			AddedAt: r.AddedAt.Format(timeFormatRFC3339Ext),
		}
	}
	writeSuccess(c, items)
}

// AddFavorite adds a media item to the user's favorites.
// Body: {"media_id": "<stable-id>"}
func (h *Handler) AddFavorite(c *gin.Context) {
	session := RequireSession(c)
	if session == nil {
		return
	}
	var req struct {
		MediaID string `json:"media_id"`
	}
	if !BindJSON(c, &req, "") {
		return
	}
	if req.MediaID == "" {
		writeError(c, http.StatusBadRequest, "media_id is required")
		return
	}

	// Resolve media path from stable ID for storage.
	mediaPath, _, ok := h.resolveMediaPathOrReceiver(c, req.MediaID)
	if !ok {
		return
	}

	if err := h.auth.AddFavorite(c.Request.Context(), session.UserID, req.MediaID, mediaPath); err != nil {
		h.log.Error("AddFavorite: %v", err)
		writeError(c, http.StatusInternalServerError, "Failed to add favorite")
		return
	}
	h.trackServerEvent(c, "favorite_add", map[string]any{"media_id": req.MediaID})
	writeSuccess(c, nil)
}

// RemoveFavorite removes a media item from the user's favorites.
// URL param :media_id is the stable media UUID.
func (h *Handler) RemoveFavorite(c *gin.Context) {
	session := RequireSession(c)
	if session == nil {
		return
	}
	mediaID, ok := RequireParamID(c, "media_id")
	if !ok {
		return
	}

	if err := h.auth.RemoveFavorite(c.Request.Context(), session.UserID, mediaID); err != nil {
		h.log.Error("RemoveFavorite: %v", err)
		writeError(c, http.StatusInternalServerError, "Failed to remove favorite")
		return
	}
	h.trackServerEvent(c, "favorite_remove", map[string]any{"media_id": mediaID})
	writeSuccess(c, nil)
}

// CheckFavorite returns whether the given media ID is in the user's favorites.
func (h *Handler) CheckFavorite(c *gin.Context) {
	session := RequireSession(c)
	if session == nil {
		return
	}
	mediaID, ok := RequireParamID(c, "media_id")
	if !ok {
		return
	}
	exists, err := h.auth.IsFavorite(c.Request.Context(), session.UserID, mediaID)
	if err != nil {
		h.log.Error("CheckFavorite: %v", err)
		writeError(c, http.StatusInternalServerError, "Failed to check favorite")
		return
	}
	writeSuccess(c, gin.H{"is_favorite": exists})
}
