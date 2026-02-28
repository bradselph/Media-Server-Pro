package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/gin-gonic/gin"

	"media-server-pro/pkg/models"
)

// ListPlaylists returns user's playlists
func (h *Handler) ListPlaylists(c *gin.Context) {
	if !h.requirePlaylist(c) {
		return
	}
	session := getSession(c)
	if session == nil {
		writeError(c, http.StatusUnauthorized, errNotAuthenticated)
		return
	}

	playlists := h.playlist.ListPlaylists(session.UserID, true)
	if playlists == nil {
		playlists = []*models.Playlist{}
	}
	writeSuccess(c, playlists)
}

// CreatePlaylist creates a new playlist
func (h *Handler) CreatePlaylist(c *gin.Context) {
	if !h.requirePlaylist(c) {
		return
	}
	session := getSession(c)
	if session == nil {
		writeError(c, http.StatusUnauthorized, errNotAuthenticated)
		return
	}

	user, err := h.auth.GetUser(c.Request.Context(), session.Username)
	if err != nil || user == nil {
		writeError(c, http.StatusInternalServerError, "Failed to retrieve user permissions")
		return
	}
	if !user.Permissions.CanCreatePlaylists {
		writeError(c, http.StatusForbidden, "Playlist creation not allowed for your user type")
		return
	}

	var req struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		IsPublic    bool   `json:"is_public"`
	}
	if c.ShouldBindJSON(&req) != nil {
		writeError(c, http.StatusBadRequest, errInvalidRequest)
		return
	}

	pl, err := h.playlist.CreatePlaylist(c.Request.Context(), req.Name, req.Description, session.UserID, req.IsPublic)
	if err != nil {
		h.log.Error("%v", err)
		writeError(c, http.StatusInternalServerError, "Internal server error")
		return
	}

	writeSuccess(c, pl)
}

// GetPlaylist returns a playlist
func (h *Handler) GetPlaylist(c *gin.Context) {
	if !h.requirePlaylist(c) {
		return
	}
	id := c.Param("id")

	session := getSession(c)
	var userID string
	if session != nil {
		userID = session.UserID
	}

	pl, err := h.playlist.GetPlaylistForUser(id, userID)
	if err != nil {
		writeError(c, http.StatusNotFound, "Playlist not found")
		return
	}

	writeSuccess(c, pl)
}

// UpdatePlaylist updates playlist metadata (name, description, is_public, cover_image)
func (h *Handler) UpdatePlaylist(c *gin.Context) {
	if !h.requirePlaylist(c) {
		return
	}
	id := c.Param("id")

	session := getSession(c)
	if session == nil {
		writeError(c, http.StatusUnauthorized, errNotAuthenticated)
		return
	}

	var updates map[string]interface{}
	if err := json.NewDecoder(c.Request.Body).Decode(&updates); err != nil {
		writeError(c, http.StatusBadRequest, errInvalidRequest)
		return
	}

	if err := h.playlist.UpdatePlaylist(c.Request.Context(), id, session.UserID, updates); err != nil {
		writeError(c, http.StatusForbidden, "Cannot update playlist")
		return
	}

	updatedPlaylist, err := h.playlist.GetPlaylistForUser(id, session.UserID)
	if err != nil || updatedPlaylist == nil {
		h.log.Warn("UpdatePlaylist: update succeeded but failed to fetch updated playlist %s: %v", id, err)
		writeSuccess(c, map[string]string{"message": "Playlist updated"})
		return
	}
	writeSuccess(c, updatedPlaylist)
}

// DeletePlaylist deletes a playlist
func (h *Handler) DeletePlaylist(c *gin.Context) {
	if !h.requirePlaylist(c) {
		return
	}
	id := c.Param("id")

	session := getSession(c)
	if session == nil {
		writeError(c, http.StatusUnauthorized, errNotAuthenticated)
		return
	}

	if err := h.playlist.DeletePlaylist(c.Request.Context(), id, session.UserID); err != nil {
		writeError(c, http.StatusForbidden, "Cannot delete playlist")
		return
	}

	writeSuccess(c, nil)
}

// ExportPlaylist exports a playlist in JSON format
func (h *Handler) ExportPlaylist(c *gin.Context) {
	if !h.requirePlaylist(c) {
		return
	}
	id := c.Param("id")

	session := getSession(c)
	if session == nil {
		writeError(c, http.StatusUnauthorized, errNotAuthenticated)
		return
	}

	format := c.Query("format")
	if format == "" {
		format = "json"
	}

	export, err := h.playlist.ExportPlaylist(id, session.UserID, format)
	if err != nil {
		writeError(c, http.StatusForbidden, "Cannot export playlist")
		return
	}

	if (format == "m3u" || format == "m3u8") && export.M3UContent != "" {
		ext := format
		c.Header(headerContentDisposition, safeContentDisposition(export.Name+"."+ext))
		c.Header(headerContentType, "audio/x-mpegurl")
		if _, err := c.Writer.Write([]byte(export.M3UContent)); err != nil {
			h.log.Error("Failed to write M3U content: %v", err)
		}
		return
	}

	c.Header(headerContentDisposition, safeContentDisposition(export.Name+".json"))
	writeSuccess(c, export)
}

// AddPlaylistItem adds an item to a playlist
func (h *Handler) AddPlaylistItem(c *gin.Context) {
	if !h.requirePlaylist(c) {
		return
	}
	playlistID := c.Param("id")

	session := getSession(c)
	if session == nil {
		writeError(c, http.StatusUnauthorized, errNotAuthenticated)
		return
	}

	var req struct {
		MediaID string `json:"media_id"`
		Title   string `json:"title"`
		Name    string `json:"name"`
	}
	if c.ShouldBindJSON(&req) != nil {
		writeError(c, http.StatusBadRequest, errInvalidRequest)
		return
	}

	if req.MediaID == "" {
		writeError(c, http.StatusBadRequest, errIDRequired)
		return
	}

	mediaPath, ok := h.resolveMediaByID(c, req.MediaID)
	if !ok {
		return
	}

	title := req.Title
	if title == "" {
		title = req.Name
	}

	if err := h.playlist.AddItem(c.Request.Context(), playlistID, session.UserID, req.MediaID, mediaPath, title); err != nil {
		writeError(c, http.StatusForbidden, "Cannot add item to playlist")
		return
	}

	writeSuccess(c, nil)
}

// RemovePlaylistItem removes an item from a playlist
func (h *Handler) RemovePlaylistItem(c *gin.Context) {
	if !h.requirePlaylist(c) {
		return
	}
	playlistID := c.Param("id")

	session := getSession(c)
	if session == nil {
		writeError(c, http.StatusUnauthorized, errNotAuthenticated)
		return
	}

	var req struct {
		ItemID  string `json:"item_id"`
		MediaID string `json:"media_id"`
	}
	if err := json.NewDecoder(c.Request.Body).Decode(&req); err != nil {
		req.MediaID = c.Query("media_id")
		if req.MediaID == "" {
			req.ItemID = c.Query("item_id")
		}
	}

	// Resolve the identifier for removal — prefer media_id, fall back to item_id
	removeKey := req.MediaID
	if removeKey == "" {
		removeKey = req.ItemID
	}
	if removeKey == "" {
		writeError(c, http.StatusBadRequest, "media_id or item_id required")
		return
	}

	if err := h.playlist.RemoveItem(c.Request.Context(), playlistID, session.UserID, removeKey); err != nil {
		writeError(c, http.StatusForbidden, "Cannot remove item from playlist")
		return
	}

	writeSuccess(c, nil)
}
