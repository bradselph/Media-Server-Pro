package handlers

import (
	"time"

	"github.com/gin-gonic/gin"

	"media-server-pro/internal/upload"
)

// AdminGetActiveStreams returns the list of active streaming sessions.
func (h *Handler) AdminGetActiveStreams(c *gin.Context) {
	writeSuccess(c, h.streaming.GetActiveSessions())
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
