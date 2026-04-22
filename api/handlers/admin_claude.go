package handlers

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"media-server-pro/internal/admin"
	"media-server-pro/internal/claude"
)

// AdminClaudeGetConfig returns the client-safe Claude settings. Credentials
// live in the CLI (~/.claude/.credentials.json) and are never serialized.
func (h *Handler) AdminClaudeGetConfig(c *gin.Context) {
	if !h.requireAdminModule(c) {
		return
	}
	if h.claude == nil {
		cfg := h.config.Get().Claude
		writeSuccess(c, map[string]any{
			"enabled":       cfg.Enabled,
			"binary_path":   cfg.BinaryPath,
			"workdir":       cfg.Workdir,
			"mode":          cfg.Mode,
			"model":         cfg.Model,
			"module_loaded": false,
		})
		return
	}
	writeSuccess(c, h.claude.PublicConfig())
}

// AdminClaudeAuthStatus reports whether the `claude` CLI is installed and
// authenticated on the host so the admin UI can prompt for `claude login`.
func (h *Handler) AdminClaudeAuthStatus(c *gin.Context) {
	if !h.requireAdminModule(c) {
		return
	}
	if h.claude == nil {
		writeSuccess(c, map[string]any{
			"installed":     false,
			"authenticated": false,
			"message":       "Claude module is not loaded",
		})
		return
	}
	writeSuccess(c, h.claude.GetAuthStatus(c.Request.Context()))
}

// AdminClaudeUpdateConfig applies a partial settings update. Allowed fields
// mirror PublicConfig.
func (h *Handler) AdminClaudeUpdateConfig(c *gin.Context) {
	if !h.requireAdminModule(c) {
		return
	}
	if h.claude == nil {
		writeError(c, http.StatusServiceUnavailable, "Claude module is not loaded")
		return
	}
	var body map[string]any
	if err := c.ShouldBindJSON(&body); err != nil {
		writeError(c, http.StatusBadRequest, errInvalidRequest)
		return
	}
	if err := h.claude.UpdateSettings(body); err != nil {
		writeError(c, http.StatusBadRequest, err.Error())
		return
	}
	h.logAdminAction(c, &adminLogActionParams{
		Action: "claude.settings.update", Target: "claude", Details: body,
	})
	writeSuccess(c, h.claude.PublicConfig())
}

// AdminClaudeKillSwitch toggles the global kill-switch. Body: {"on": true|false}
func (h *Handler) AdminClaudeKillSwitch(c *gin.Context) {
	if !h.requireAdminModule(c) {
		return
	}
	if h.claude == nil {
		writeError(c, http.StatusServiceUnavailable, "Claude module is not loaded")
		return
	}
	var body struct {
		On bool `json:"on"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		writeError(c, http.StatusBadRequest, errInvalidRequest)
		return
	}
	if err := h.claude.SetKillSwitch(body.On); err != nil {
		writeError(c, http.StatusInternalServerError, err.Error())
		return
	}
	h.logAdminAction(c, &adminLogActionParams{
		Action: "claude.kill_switch", Target: "claude", Details: map[string]any{"on": body.On},
	})
	writeSuccess(c, map[string]any{"kill_switch": h.claude.PublicConfig().KillSwitch})
}

// AdminClaudeListConversations returns conversations owned by the signed-in admin.
func (h *Handler) AdminClaudeListConversations(c *gin.Context) {
	if !h.requireClaude(c) {
		return
	}
	session := getSession(c)
	if session == nil {
		writeError(c, http.StatusUnauthorized, errNotAuthenticated)
		return
	}
	limit := ParseQueryInt(c, "limit", QueryIntOpts{Default: 50, Min: 1, Max: 200})
	convs, err := h.claude.ListConversations(c.Request.Context(), session.UserID, limit)
	if err != nil {
		writeError(c, http.StatusInternalServerError, err.Error())
		return
	}
	writeSuccess(c, convs)
}

// AdminClaudeGetConversation returns a single conversation's transcript.
func (h *Handler) AdminClaudeGetConversation(c *gin.Context) {
	if !h.requireClaude(c) {
		return
	}
	session := getSession(c)
	if session == nil {
		writeError(c, http.StatusUnauthorized, errNotAuthenticated)
		return
	}
	id := strings.TrimSpace(c.Param("id"))
	if id == "" {
		writeError(c, http.StatusBadRequest, "conversation id required")
		return
	}
	conv, msgs, err := h.claude.GetConversation(c.Request.Context(), session.UserID, id)
	if err != nil {
		writeError(c, http.StatusNotFound, err.Error())
		return
	}
	writeSuccess(c, map[string]any{"conversation": conv, "messages": msgs})
}

// AdminClaudeDeleteConversation deletes a conversation.
func (h *Handler) AdminClaudeDeleteConversation(c *gin.Context) {
	if !h.requireClaude(c) {
		return
	}
	session := getSession(c)
	if session == nil {
		writeError(c, http.StatusUnauthorized, errNotAuthenticated)
		return
	}
	id := strings.TrimSpace(c.Param("id"))
	if id == "" {
		writeError(c, http.StatusBadRequest, "conversation id required")
		return
	}
	if err := h.claude.DeleteConversation(c.Request.Context(), session.UserID, id); err != nil {
		writeError(c, http.StatusNotFound, err.Error())
		return
	}
	h.logAdminAction(c, &adminLogActionParams{
		Action: "claude.conversation.delete", Target: id,
	})
	writeSuccess(c, map[string]any{"deleted": id})
}

// AdminClaudeChat runs a single chat turn and streams events as SSE.
//
// Response content-type is text/event-stream. Each event is a JSON-encoded
// claude.Event on a single `data:` line.
func (h *Handler) AdminClaudeChat(c *gin.Context) {
	if !h.requireClaude(c) {
		return
	}
	session := getSession(c)
	if session == nil {
		writeError(c, http.StatusUnauthorized, errNotAuthenticated)
		return
	}
	var req claude.ChatRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeError(c, http.StatusBadRequest, errInvalidRequest)
		return
	}
	if strings.TrimSpace(req.Message) == "" {
		writeError(c, http.StatusBadRequest, "message is required")
		return
	}

	// Prepare SSE headers. Nginx buffering is disabled so events flush promptly.
	c.Writer.Header().Set("Content-Type", "text/event-stream")
	c.Writer.Header().Set(headerCacheControl, "no-cache, no-transform")
	c.Writer.Header().Set("Connection", "keep-alive")
	c.Writer.Header().Set("X-Accel-Buffering", "no")
	c.Writer.WriteHeader(http.StatusOK)

	flusher, _ := c.Writer.(http.Flusher)
	if flusher != nil {
		flusher.Flush()
	}

	var writeEvent func(ev claude.Event)
	writeEvent = func(ev claude.Event) {
		b, marshalErr := json.Marshal(ev)
		if marshalErr != nil {
			errJSON := `data: {"type":"error","error":"internal marshal error"}` + "\n\n"
			_, _ = io.WriteString(c.Writer, errJSON)
			if flusher != nil {
				flusher.Flush()
			}
			return
		}
		_, _ = io.WriteString(c.Writer, "data: ")
		_, _ = c.Writer.Write(b)
		_, _ = io.WriteString(c.Writer, "\n\n")
		if flusher != nil {
			flusher.Flush()
		}
	}

	// Use a context that is NOT canceled when the SSE client disconnects.
	// If the request context cancels mid-turn, in-flight DB writes (appendMessage,
	// audit log) would fail, corrupting the conversation history. The turn runs to
	// completion regardless of client state; the emitter simply stops writing if
	// the connection has gone away.
	turnCtx := context.WithoutCancel(c.Request.Context())

	convID, _, err := h.claude.ChatTurn(turnCtx, session.UserID, session.Username, c.ClientIP(), req, writeEvent)

	if err != nil {
		writeEvent(claude.Event{Type: "error", Error: err.Error()})
	} else {
		writeEvent(claude.Event{Type: "done"})
	}

	// Audit the turn itself (not each tool — tools audit independently).
	if h.admin != nil {
		h.admin.LogAction(turnCtx, &admin.AuditLogParams{
			UserID:    session.UserID,
			Username:  session.Username,
			Action:    "claude.chat",
			Resource:  convID,
			Details:   map[string]any{"bytes": len(req.Message), "had_approvals": len(req.ApprovedToolCalls) > 0, "mode_override": req.ModeOverride},
			IPAddress: c.ClientIP(),
			Success:   err == nil,
		})
	}
}

