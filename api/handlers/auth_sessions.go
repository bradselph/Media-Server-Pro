// User-facing session enumeration and revocation. Lets a logged-in user see
// every device that holds an active session under their account and revoke
// any one of them — checklist §9 ("Sessions list: device, IP, last active,
// revoke button").
package handlers

import (
	"errors"
	"net/http"
	"sort"

	"github.com/gin-gonic/gin"

	"media-server-pro/internal/auth"
)

// userSession is the redacted projection sent to clients. The full session ID
// never leaves the server — only a short prefix the user can match against
// the "current device" hint, and the row's stable client-side identifier
// (also the prefix) used by the revoke endpoint. This prevents cross-tab
// session hijack via a stolen API response.
type userSession struct {
	ID            string `json:"id"`
	IPAddress     string `json:"ip_address"`
	UserAgent     string `json:"user_agent"`
	CreatedAt     int64  `json:"created_at"`
	LastActivity  int64  `json:"last_activity"`
	ExpiresAt     int64  `json:"expires_at"`
	IsCurrent     bool   `json:"is_current"`
}

// ListMySessions returns every active session belonging to the caller, with
// the current session flagged so the UI can disable its "revoke" button.
func (h *Handler) ListMySessions(c *gin.Context) {
	current := getSession(c)
	if current == nil {
		writeError(c, http.StatusUnauthorized, "Authentication required")
		return
	}
	sessions := h.auth.GetActiveSessionsByUserID(current.UserID)
	out := make([]userSession, 0, len(sessions))
	for _, s := range sessions {
		out = append(out, userSession{
			ID:           shortSessionID(s.ID),
			IPAddress:    s.IPAddress,
			UserAgent:    s.UserAgent,
			CreatedAt:    s.CreatedAt.Unix(),
			LastActivity: s.LastActivity.Unix(),
			ExpiresAt:    s.ExpiresAt.Unix(),
			IsCurrent:    s.ID == current.ID,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].LastActivity > out[j].LastActivity
	})
	writeSuccess(c, out)
}

// RevokeMySession invalidates the session whose short ID matches the path
// parameter — but ONLY if it belongs to the caller. The current session
// cannot be revoked through this endpoint (the user should use /auth/logout
// for that, which also clears the cookie).
func (h *Handler) RevokeMySession(c *gin.Context) {
	current := getSession(c)
	if current == nil {
		writeError(c, http.StatusUnauthorized, "Authentication required")
		return
	}
	shortID := c.Param("id")
	if shortID == "" {
		writeError(c, http.StatusBadRequest, "Session id is required")
		return
	}
	// Resolve the short prefix back to a full session ID by walking the
	// caller's own sessions only — a stolen short ID for another user's
	// session must NOT be revocable from this endpoint.
	target := ""
	for _, s := range h.auth.GetActiveSessionsByUserID(current.UserID) {
		if shortSessionID(s.ID) == shortID {
			target = s.ID
			break
		}
	}
	if target == "" {
		writeError(c, http.StatusNotFound, "Session not found")
		return
	}
	if target == current.ID {
		writeError(c, http.StatusBadRequest, "Use /auth/logout to revoke the current session")
		return
	}
	if err := h.auth.RevokeUserSession(c.Request.Context(), current.UserID, target); err != nil {
		if errors.Is(err, auth.ErrSessionNotFound) {
			writeError(c, http.StatusNotFound, "Session not found")
			return
		}
		writeError(c, http.StatusInternalServerError, "Failed to revoke session")
		return
	}
	writeSuccess(c, nil)
}

// shortSessionID exposes only the first 12 chars of the session ID. The
// admin endpoint uses 8+"..."; the user endpoint uses a longer prefix because
// the caller already proved ownership of the full ID via the session cookie.
func shortSessionID(id string) string {
	if len(id) <= 12 {
		return id
	}
	return id[:12]
}
