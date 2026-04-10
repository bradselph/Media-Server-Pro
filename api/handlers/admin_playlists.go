package handlers

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"media-server-pro/internal/playlist"
	"media-server-pro/pkg/models"
)

// filterPlaylistsBySearch returns playlists whose name or user_id contains the search term (case-insensitive).
func filterPlaylistsBySearch(playlists []*models.Playlist, search string) []*models.Playlist {
	if search == "" {
		return playlists
	}
	kept := make([]*models.Playlist, 0, len(playlists))
	low := strings.ToLower(search)
	for _, p := range playlists {
		if strings.Contains(strings.ToLower(p.Name), low) || strings.Contains(strings.ToLower(p.UserID), low) {
			kept = append(kept, p)
		}
	}
	return kept
}

// filterPlaylistsByVisibility filters by is_public. visibility: "public" | "private" | ""
func filterPlaylistsByVisibility(playlists []*models.Playlist, visibility string) []*models.Playlist {
	if visibility == "" {
		return playlists
	}
	kept := make([]*models.Playlist, 0, len(playlists))
	for _, p := range playlists {
		if visibility == "public" && p.IsPublic {
			kept = append(kept, p)
		} else if visibility == "private" && !p.IsPublic {
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

// AdminListPlaylists returns playlists for admin with optional search and pagination.
// Returns { items, total_items, total_pages } for correct pagination UI.
func (h *Handler) AdminListPlaylists(c *gin.Context) {
	if !h.requirePlaylist(c) {
		return
	}
	all := h.playlist.ListAllPlaylists()
	search := strings.ToLower(strings.TrimSpace(c.Query("search")))
	visibility := strings.TrimSpace(c.Query("visibility")) // "public" | "private" | ""
	limit := ParseQueryInt(c, "limit", QueryIntOpts{Default: 100, Min: 1, Max: 1000})
	page := ParseQueryInt(c, "page", QueryIntOpts{Default: 1, Min: 1, Max: 10000})
	filtered := filterPlaylistsBySearch(all, search)
	filtered = filterPlaylistsByVisibility(filtered, visibility)
	totalItems := len(filtered)
	totalPages := 1
	if limit > 0 && totalItems > 0 {
		totalPages = (totalItems + limit - 1) / limit
		if totalPages < 1 {
			totalPages = 1
		}
	}
	items := paginatePlaylists(filtered, page, limit)
	writeSuccess(c, map[string]interface{}{
		"items":       items,
		"total_items": totalItems,
		"total_pages": totalPages,
	})
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
		Action: "bulk_delete_playlists",
		Target: fmt.Sprintf("%d playlists", successCount), Success: failedCount == 0,
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
	if !BindJSON(c, &req, errInvalidRequest) {
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
		if err := h.playlist.AdminDeletePlaylist(ctx, playlist.PlaylistID(id)); err != nil {
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
	playlistID, ok := RequireParamID(c, "id")
	if !ok {
		return
	}
	if err := h.playlist.AdminDeletePlaylist(c.Request.Context(), playlist.PlaylistID(playlistID)); err != nil {
		writeError(c, http.StatusNotFound, msgPlaylistNotFound)
		return
	}

	writeSuccess(c, map[string]string{"message": "Playlist deleted"})
}
