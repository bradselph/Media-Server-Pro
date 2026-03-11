package handlers

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"media-server-pro/pkg/models"
)

// parsePositiveQueryInt returns the integer value if s is a valid positive integer, otherwise defaultVal.
func parsePositiveQueryInt(s string, defaultVal int) int {
	if s == "" {
		return defaultVal
	}
	v, err := strconv.Atoi(s)
	if err != nil || v <= 0 {
		return defaultVal
	}
	return v
}

// parseAdminPlaylistListParams reads limit and page from query; returns defaults 100 and 1 when missing or invalid.
func parseAdminPlaylistListParams(c *gin.Context) (limit, page int) {
	limit = parsePositiveQueryInt(c.Query("limit"), 100)
	page = parsePositiveQueryInt(c.Query("page"), 1)
	return limit, page
}

// filterPlaylistsBySearch returns playlists whose name contains the given search term (case-insensitive).
func filterPlaylistsBySearch(playlists []*models.Playlist, search string) []*models.Playlist {
	if search == "" {
		return playlists
	}
	kept := make([]*models.Playlist, 0, len(playlists))
	for _, p := range playlists {
		if strings.Contains(strings.ToLower(p.Name), search) {
			kept = append(kept, p)
		}
	}
	return kept
}

// paginatePlaylists returns the slice for the given page and limit, or empty if start is past the end.
func paginatePlaylists(playlists []*models.Playlist, page, limit int) []*models.Playlist {
	start := (page - 1) * limit
	if start >= len(playlists) {
		return []*models.Playlist{}
	}
	end := start + limit
	if end > len(playlists) {
		end = len(playlists)
	}
	return playlists[start:end]
}

// AdminListPlaylists returns all playlists for admin with optional search and pagination.
func (h *Handler) AdminListPlaylists(c *gin.Context) {
	if !h.requirePlaylist(c) {
		return
	}
	all := h.playlist.ListAllPlaylists()
	search := strings.ToLower(strings.TrimSpace(c.Query("search")))
	limit, page := parseAdminPlaylistListParams(c)
	filtered := filterPlaylistsBySearch(all, search)
	writeSuccess(c, paginatePlaylists(filtered, page, limit))
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
	ids, ok := h.validateBulkDeletePlaylistsRequest(c)
	if !ok {
		return
	}
	successCount, failedCount, errs := h.bulkDeletePlaylistsByIDs(c.Request.Context(), ids)
	if errs == nil {
		errs = []string{}
	}
	h.logAdminActionResult(c, &adminLogResultParams{
		UserID: "admin", Username: "admin", Action: "bulk_delete_playlists",
		Target: fmt.Sprintf("%d playlists", successCount), Details: nil, Success: failedCount == 0,
	})
	writeSuccess(c, map[string]interface{}{
		"success": successCount,
		"failed":  failedCount,
		"errors":  errs,
	})
}

// validateBulkDeletePlaylistsRequest binds and validates the bulk delete request. On failure writes the error and returns (nil, false).
func (h *Handler) validateBulkDeletePlaylistsRequest(c *gin.Context) ([]string, bool) {
	var req struct {
		IDs []string `json:"ids"`
	}
	if c.ShouldBindJSON(&req) != nil {
		writeError(c, http.StatusBadRequest, errInvalidRequest)
		return nil, false
	}
	if len(req.IDs) == 0 {
		writeError(c, http.StatusBadRequest, "ids must not be empty")
		return nil, false
	}
	if len(req.IDs) > 500 {
		writeError(c, http.StatusBadRequest, "too many ids (max 500)")
		return nil, false
	}
	return req.IDs, true
}

// bulkDeletePlaylistsByIDs deletes each non-empty ID and returns success count, failed count, and error messages.
func (h *Handler) bulkDeletePlaylistsByIDs(ctx context.Context, ids []string) (successCount, failedCount int, errs []string) {
	for _, id := range ids {
		if id == "" {
			continue
		}
		if err := h.playlist.AdminDeletePlaylist(ctx, id); err != nil {
			failedCount++
			errs = append(errs, fmt.Sprintf("%s: %v", id, err))
		} else {
			successCount++
		}
	}
	return successCount, failedCount, errs
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
