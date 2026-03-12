package handlers

import (
	"github.com/gin-gonic/gin"

	"media-server-pro/internal/upload"
	"media-server-pro/pkg/models"
)

// AdminGetActiveStreams returns the list of active streaming sessions.
func (h *Handler) AdminGetActiveStreams(c *gin.Context) {
	sessions := h.streaming.GetActiveSessions()
	if sessions == nil {
		sessions = []*models.StreamSession{}
	}
	writeSuccess(c, sessions)
}

// AdminGetActiveUploads returns the list of in-progress uploads.
func (h *Handler) AdminGetActiveUploads(c *gin.Context) {
	if h.upload == nil {
		writeSuccess(c, []*upload.Progress{})
		return
	}
	uploads := h.upload.GetActiveUploads()
	if uploads == nil {
		uploads = []*upload.Progress{}
	}
	writeSuccess(c, uploads)
}

// AdminGetUserSessions returns active sessions for a specific user.
func (h *Handler) AdminGetUserSessions(c *gin.Context) {
	username := c.Param("username")
	sessions := h.auth.GetActiveSessions(username)
	if sessions == nil {
		sessions = []*models.Session{}
	}
	writeSuccess(c, sessions)
}
