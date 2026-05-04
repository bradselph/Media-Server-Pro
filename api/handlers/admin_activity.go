package handlers

import (
	"time"

	"github.com/gin-gonic/gin"

	"media-server-pro/internal/upload"
)

// AdminGetActiveStreams returns the list of active streaming sessions with
// each session enriched with the human-readable media name so the operations
// dashboard doesn't have to do its own per-row media lookups. Falls back to
// the raw media_id when the lookup fails (e.g. extractor / receiver items).
func (h *Handler) AdminGetActiveStreams(c *gin.Context) {
	if h.streaming == nil {
		writeSuccess(c, []any{})
		return
	}
	sessions := h.streaming.GetActiveSessions()
	out := make([]map[string]any, 0, len(sessions))
	for _, s := range sessions {
		if s == nil {
			continue
		}
		filename := s.MediaID
		if h.media != nil {
			if mi, err := h.media.GetMediaByID(s.MediaID); err == nil && mi != nil {
				filename = mi.Name
			}
		}
		out = append(out, map[string]any{
			"id":          s.ID,
			"media_id":    s.MediaID,
			"filename":    filename,
			"user_id":     s.UserID,
			"ip_address":  s.IPAddress,
			"quality":     s.Quality,
			"position":    s.Position,
			"started_at":  s.StartedAt.Unix(),
			"last_update": s.LastUpdate.Unix(),
			"bytes_sent":  s.BytesSent,
		})
	}
	writeSuccess(c, out)
}

// AdminGetActiveUploads returns the list of in-progress uploads.
func (h *Handler) AdminGetActiveUploads(c *gin.Context) {
	if h.upload == nil {
		writeSuccess(c, []*upload.Progress{})
		return
	}
	writeSuccess(c, h.upload.GetActiveUploads())
}

// AdminGetUserSessions returns active sessions for a specific user.
// Session token IDs are truncated to prevent session hijacking between admins.
func (h *Handler) AdminGetUserSessions(c *gin.Context) {
	username := c.Param("username")
	sessions := h.auth.GetActiveSessions(username)
	type safeSession struct {
		ID           string    `json:"id"`
		UserID       string    `json:"user_id"`
		Username     string    `json:"username"`
		Role         string    `json:"role"`
		CreatedAt    time.Time `json:"created_at"`
		ExpiresAt    time.Time `json:"expires_at"`
		LastActivity time.Time `json:"last_activity"`
		IPAddress    string    `json:"ip_address"`
		UserAgent    string    `json:"user_agent"`
	}
	safe := make([]safeSession, len(sessions))
	for i, s := range sessions {
		truncatedID := s.ID
		if len(truncatedID) > 8 {
			truncatedID = truncatedID[:8] + "..."
		}
		safe[i] = safeSession{
			ID:           truncatedID,
			UserID:       s.UserID,
			Username:     s.Username,
			Role:         string(s.Role),
			CreatedAt:    s.CreatedAt,
			ExpiresAt:    s.ExpiresAt,
			LastActivity: s.LastActivity,
			IPAddress:    s.IPAddress,
			UserAgent:    s.UserAgent,
		}
	}
	writeSuccess(c, safe)
}
