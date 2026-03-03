package handlers

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"media-server-pro/pkg/models"
)

// AdminListPlaylists returns all playlists for admin with optional search and pagination.
func (h *Handler) AdminListPlaylists(c *gin.Context) {
	if !h.requirePlaylist(c) {
		return
	}
	all := h.playlist.ListAllPlaylists()

	search := strings.ToLower(strings.TrimSpace(c.Query("search")))
	limitStr := c.Query("limit")
	pageStr := c.Query("page")

	limit := 100
	if limitStr != "" {
		if v, err := strconv.Atoi(limitStr); err == nil && v > 0 {
			limit = v
		}
	}
	page := 1
	if pageStr != "" {
		if v, err := strconv.Atoi(pageStr); err == nil && v > 0 {
			page = v
		}
	}

	filtered := all
	if search != "" {
		kept := make([]*models.Playlist, 0, len(all))
		for _, p := range all {
			if strings.Contains(strings.ToLower(p.Name), search) {
				kept = append(kept, p)
			}
		}
		filtered = kept
	}

	start := (page - 1) * limit
	if start >= len(filtered) {
		writeSuccess(c, []*models.Playlist{})
		return
	}
	end := start + limit
	if end > len(filtered) {
		end = len(filtered)
	}

	writeSuccess(c, filtered[start:end])
}

// AdminPlaylistStats returns playlist statistics
func (h *Handler) AdminPlaylistStats(c *gin.Context) {
	if !h.requirePlaylist(c) {
		return
	}
	stats := h.playlist.GetStats()
	writeSuccess(c, stats)
}

// AdminBulkDeletePlaylists deletes multiple playlists by ID.
func (h *Handler) AdminBulkDeletePlaylists(c *gin.Context) {
	if !h.requirePlaylist(c) {
		return
	}
	var req struct {
		IDs []string `json:"ids"`
	}
	if c.ShouldBindJSON(&req) != nil {
		writeError(c, http.StatusBadRequest, errInvalidRequest)
		return
	}
	if len(req.IDs) == 0 {
		writeError(c, http.StatusBadRequest, "ids must not be empty")
		return
	}
	if len(req.IDs) > 500 {
		writeError(c, http.StatusBadRequest, "too many ids (max 500)")
		return
	}

	var successCount, failedCount int
	var errs []string
	for _, id := range req.IDs {
		if id == "" {
			continue
		}
		if err := h.playlist.AdminDeletePlaylist(c.Request.Context(), id); err != nil {
			failedCount++
			errs = append(errs, fmt.Sprintf("%s: %v", id, err))
		} else {
			successCount++
		}
	}
	if errs == nil {
		errs = []string{}
	}
	h.logAdminActionResult(c, "admin", "admin", "bulk_delete_playlists",
		fmt.Sprintf("%d playlists", successCount), nil, failedCount == 0)
	writeSuccess(c, map[string]interface{}{
		"success": successCount,
		"failed":  failedCount,
		"errors":  errs,
	})
}

// AdminDeletePlaylist deletes a playlist as admin
func (h *Handler) AdminDeletePlaylist(c *gin.Context) {
	if !h.requirePlaylist(c) {
		return
	}
	playlistID := c.Param("id")

	if err := h.playlist.AdminDeletePlaylist(c.Request.Context(), playlistID); err != nil {
		writeError(c, http.StatusNotFound, "Playlist not found")
		return
	}

	writeSuccess(c, map[string]string{"message": "Playlist deleted"})
}
