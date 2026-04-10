package handlers

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"media-server-pro/internal/playlist"
	"media-server-pro/pkg/models"
)

// requirePlaylistIDAndSession ensures playlist module is available, path param "id" is present, and session exists.
// Returns (id, session, true) or ("", nil, false) after writing an error.
func (h *Handler) requirePlaylistIDAndSession(c *gin.Context) (id string, session *models.Session, ok bool) {
	if !h.requirePlaylist(c) {
		return "", nil, false
	}
	id, ok = RequireParamID(c, "id")
	if !ok {
		return "", nil, false
	}
	session = RequireSession(c)
	if session == nil {
		return "", nil, false
	}
	return id, session, true
}

// requireSessionWithPlaylistCreate ensures playlist module, session, and CanCreatePlaylists permission.
// Returns (session, true) or (nil, false) after writing an error.
func (h *Handler) requireSessionWithPlaylistCreate(c *gin.Context) (session *models.Session, ok bool) {
	if !h.requirePlaylist(c) {
		return nil, false
	}
	session = RequireSession(c)
	if session == nil {
		return nil, false
	}
	user, err := h.auth.GetUser(c.Request.Context(), session.Username)
	if err != nil || user == nil {
		writeError(c, http.StatusInternalServerError, "Failed to retrieve user permissions")
		return nil, false
	}
	if !user.Permissions.CanCreatePlaylists {
		writeError(c, http.StatusForbidden, "Playlist creation not allowed for your user type")
		return nil, false
	}
	return session, true
}

// hydratePlaylistTitles fills in empty item titles from the media module.
// Uses a single batch lookup so it only acquires the media lock once.
func (h *Handler) hydratePlaylistTitles(playlists ...*models.Playlist) {
	if h.media == nil {
		return
	}
	// Collect media IDs with missing titles
	var ids []string
	for _, pl := range playlists {
		for i := range pl.Items {
			if pl.Items[i].Title == "" && pl.Items[i].MediaID != "" {
				ids = append(ids, pl.Items[i].MediaID)
			}
		}
	}
	if len(ids) == 0 {
		return
	}
	names := h.media.GetMediaNamesByIDs(ids)
	for _, pl := range playlists {
		for i := range pl.Items {
			if pl.Items[i].Title == "" {
				if name, ok := names[pl.Items[i].MediaID]; ok {
					pl.Items[i].Title = name
				}
			}
		}
	}
}

// ListPlaylists returns user's playlists
func (h *Handler) ListPlaylists(c *gin.Context) {
	if !h.requirePlaylist(c) {
		return
	}
	session := RequireSession(c)
	if session == nil {
		return
	}
	playlists := h.playlist.ListPlaylists(playlist.UserID(session.UserID), true)
	if playlists == nil {
		playlists = []*models.Playlist{}
	}
	h.hydratePlaylistTitles(playlists...)
	writeSuccess(c, playlists)
}

// ListPublicPlaylists returns all playlists marked as public, accessible without auth.
func (h *Handler) ListPublicPlaylists(c *gin.Context) {
	if !h.requirePlaylist(c) {
		return
	}
	playlists := h.playlist.ListPublicPlaylists()
	if playlists == nil {
		playlists = []*models.Playlist{}
	}
	h.hydratePlaylistTitles(playlists...)
	writeSuccess(c, playlists)
}

// CreatePlaylist creates a new playlist
func (h *Handler) CreatePlaylist(c *gin.Context) {
	session, ok := h.requireSessionWithPlaylistCreate(c)
	if !ok {
		return
	}
	var req struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		IsPublic    bool   `json:"is_public"`
	}
	if !BindJSON(c, &req, errInvalidRequest) {
		return
	}
	if req.Name == "" {
		writeError(c, http.StatusBadRequest, "Playlist name required")
		return
	}
	pl, err := h.playlist.CreatePlaylist(c.Request.Context(), playlist.CreatePlaylistInput{
		Name: req.Name, Description: req.Description, UserID: playlist.UserID(session.UserID), IsPublic: req.IsPublic,
	})
	if err != nil {
		h.log.Error("%v", err)
		writeError(c, http.StatusInternalServerError, "Internal server error")
		return
	}
	writeSuccess(c, pl)
}

// GetPlaylist returns a playlist. Route is behind requireAuth(); session is required.
func (h *Handler) GetPlaylist(c *gin.Context) {
	if !h.requirePlaylist(c) {
		return
	}
	id, ok := RequireParamID(c, "id")
	if !ok {
		return
	}
	s := getSession(c)
	if s == nil {
		writeError(c, http.StatusUnauthorized, errNotAuthenticated)
		return
	}
	userID := playlist.UserID(s.UserID)
	pl, err := h.playlist.GetPlaylistForUser(playlist.PlaylistID(id), userID)
	if err != nil {
		writeError(c, http.StatusNotFound, "Playlist not found")
		return
	}

	h.hydratePlaylistTitles(pl)
	writeSuccess(c, pl)
}

// UpdatePlaylist updates playlist metadata (name, description, is_public, cover_image)
func (h *Handler) UpdatePlaylist(c *gin.Context) {
	id, session, ok := h.requirePlaylistIDAndSession(c)
	if !ok {
		return
	}
	var updates map[string]interface{}
	if !BindJSON(c, &updates, errInvalidRequest) {
		return
	}
	if err := h.playlist.UpdatePlaylist(c.Request.Context(), playlist.PlaylistID(id), playlist.UserID(session.UserID), updates); err != nil {
		if errors.Is(err, playlist.ErrPlaylistNotFound) {
			writeError(c, http.StatusNotFound, "Playlist not found")
			return
		}
		if errors.Is(err, playlist.ErrAccessDenied) {
			writeError(c, http.StatusForbidden, "Cannot update playlist")
			return
		}
		h.log.Warn("UpdatePlaylist failed: %v", err)
		writeError(c, http.StatusInternalServerError, "Failed to update playlist")
		return
	}

	updatedPlaylist, err := h.playlist.GetPlaylistForUser(playlist.PlaylistID(id), playlist.UserID(session.UserID))
	if err != nil || updatedPlaylist == nil {
		h.log.Warn("UpdatePlaylist: update succeeded but failed to fetch updated playlist %s: %v", id, err)
		writeSuccess(c, map[string]string{"message": "Playlist updated"})
		return
	}
	h.hydratePlaylistTitles(updatedPlaylist)
	writeSuccess(c, updatedPlaylist)
}

// DeletePlaylist deletes a playlist
func (h *Handler) DeletePlaylist(c *gin.Context) {
	id, session, ok := h.requirePlaylistIDAndSession(c)
	if !ok {
		return
	}
	if err := h.playlist.DeletePlaylist(c.Request.Context(), playlist.PlaylistID(id), playlist.UserID(session.UserID)); err != nil {
		if errors.Is(err, playlist.ErrPlaylistNotFound) {
			writeError(c, http.StatusNotFound, "Playlist not found")
			return
		}
		if errors.Is(err, playlist.ErrAccessDenied) {
			writeError(c, http.StatusForbidden, "Cannot delete playlist")
			return
		}
		h.log.Warn("DeletePlaylist failed: %v", err)
		writeError(c, http.StatusInternalServerError, "Failed to delete playlist")
		return
	}

	writeSuccess(c, nil)
}

// ExportPlaylist exports a playlist in JSON or M3U format
func (h *Handler) ExportPlaylist(c *gin.Context) {
	id, session, ok := h.requirePlaylistIDAndSession(c)
	if !ok {
		return
	}
	format := c.Query("format")
	if format == "" {
		format = "json"
	}
	export, err := h.playlist.ExportPlaylist(playlist.PlaylistID(id), playlist.UserID(session.UserID), format)
	if err != nil {
		writeError(c, http.StatusForbidden, "Cannot export playlist")
		return
	}
	h.writeExportResponse(c, format, export)
}

func (h *Handler) writeExportResponse(c *gin.Context, format string, export *playlist.Export) {
	isM3U := format == "m3u" || format == "m3u8"
	hasM3UContent := export.M3UContent != ""
	if isM3U && hasM3UContent {
		ext := format
		c.Header(headerContentDisposition, safeContentDisposition(export.Name+"."+ext))
		c.Header(headerContentType, "audio/x-mpegurl")
		if _, err := c.Writer.WriteString(export.M3UContent); err != nil {
			h.log.Error("Failed to write M3U content: %v", err)
		}
		return
	}
	c.Header(headerContentDisposition, safeContentDisposition(export.Name+".json"))
	writeSuccess(c, export)
}

// AddPlaylistItem adds an item to a playlist
func (h *Handler) AddPlaylistItem(c *gin.Context) {
	playlistID, session, ok := h.requirePlaylistIDAndSession(c)
	if !ok {
		return
	}
	var req struct {
		MediaID string `json:"media_id"`
		Title   string `json:"title"`
		Name    string `json:"name"`
	}
	if !BindJSON(c, &req, errInvalidRequest) {
		return
	}
	if req.MediaID == "" {
		writeError(c, http.StatusBadRequest, errIDRequired)
		return
	}

	mediaPath, itemName, ok := h.resolveMediaPathOrReceiver(c, req.MediaID)
	if !ok {
		return
	}

	title := req.Title
	if title == "" {
		title = req.Name
	}
	// Fall back to the resolved item name (covers local, receiver, and extractor items)
	if title == "" {
		title = itemName
	}

	if err := h.playlist.AddItem(c.Request.Context(), playlist.AddItemInput{
		PlaylistID: playlist.PlaylistID(playlistID),
		UserID:     playlist.UserID(session.UserID),
		MediaID:    req.MediaID,
		MediaPath:  mediaPath,
		Title:      title,
	}); err != nil {
		writeError(c, http.StatusForbidden, "Cannot add item to playlist")
		return
	}

	writeSuccess(c, nil)
}

// ReorderPlaylistItems reorders items in a playlist
func (h *Handler) ReorderPlaylistItems(c *gin.Context) {
	playlistID, session, ok := h.requirePlaylistIDAndSession(c)
	if !ok {
		return
	}
	var req struct {
		Positions []int `json:"positions"`
	}
	if !BindJSON(c, &req, errInvalidRequest) {
		return
	}
	if err := h.playlist.ReorderItems(c.Request.Context(), playlist.PlaylistID(playlistID), playlist.UserID(session.UserID), req.Positions); err != nil {
		writeError(c, http.StatusForbidden, "Cannot reorder playlist items")
		return
	}

	writeSuccess(c, nil)
}

// ClearPlaylist removes all items from a playlist
func (h *Handler) ClearPlaylist(c *gin.Context) {
	playlistID, session, ok := h.requirePlaylistIDAndSession(c)
	if !ok {
		return
	}
	if err := h.playlist.ClearPlaylist(c.Request.Context(), playlist.PlaylistID(playlistID), playlist.UserID(session.UserID)); err != nil {
		writeError(c, http.StatusForbidden, "Cannot clear playlist")
		return
	}

	writeSuccess(c, nil)
}

// CopyPlaylist duplicates a playlist with a new name
func (h *Handler) CopyPlaylist(c *gin.Context) {
	sourceID, session, ok := h.requirePlaylistIDAndSession(c)
	if !ok {
		return
	}
	var req struct {
		Name string `json:"name"`
	}
	if !BindJSON(c, &req, errInvalidRequest) {
		return
	}
	if req.Name == "" {
		writeError(c, http.StatusBadRequest, "Playlist name required")
		return
	}

	pl, err := h.playlist.CopyPlaylist(c.Request.Context(), playlist.PlaylistID(sourceID), playlist.UserID(session.UserID), req.Name)
	if err != nil {
		h.log.Error("%v", err)
		writeError(c, http.StatusInternalServerError, "Internal server error")
		return
	}

	writeSuccess(c, pl)
}

// RemovePlaylistItem removes an item from a playlist
func (h *Handler) RemovePlaylistItem(c *gin.Context) {
	playlistID, session, ok := h.requirePlaylistIDAndSession(c)
	if !ok {
		return
	}
	removeKey := c.Query("media_id")
	if removeKey == "" {
		removeKey = c.Query("item_id")
	}
	if removeKey == "" {
		writeError(c, http.StatusBadRequest, "media_id or item_id required")
		return
	}

	if err := h.playlist.RemoveItem(c.Request.Context(), playlist.PlaylistID(playlistID), playlist.UserID(session.UserID), removeKey); err != nil {
		writeError(c, http.StatusForbidden, "Cannot remove item from playlist")
		return
	}

	writeSuccess(c, nil)
}

// BulkDeletePlaylists deletes multiple playlists owned by the requesting user.
// Accepts { "ids": ["id1","id2",...] }. Each ID is deleted only if it belongs
// to the caller; foreign-owned or missing IDs are counted as failures.
func (h *Handler) BulkDeletePlaylists(c *gin.Context) {
	if !h.requirePlaylist(c) {
		return
	}
	session := RequireSession(c)
	if session == nil {
		return
	}
	var req struct {
		IDs []string `json:"ids"`
	}
	if !BindJSON(c, &req, "invalid request") {
		return
	}
	if len(req.IDs) == 0 {
		writeError(c, http.StatusBadRequest, "ids must not be empty")
		return
	}
	if len(req.IDs) > 100 {
		writeError(c, http.StatusBadRequest, "too many ids (max 100)")
		return
	}
	successCount, failedCount := 0, 0
	for _, id := range req.IDs {
		if id == "" {
			continue
		}
		if err := h.playlist.DeletePlaylist(c.Request.Context(), playlist.PlaylistID(id), playlist.UserID(session.UserID)); err != nil {
			failedCount++
		} else {
			successCount++
		}
	}
	writeSuccess(c, map[string]int{"deleted": successCount, "failed": failedCount})
}
