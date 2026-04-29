package claude

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"media-server-pro/internal/admin"
)

// Event is a single SSE-ready event emitted during a chat turn. The handler
// serializes this to JSON and writes it framed as SSE.
type Event struct {
	Type           string    `json:"type"` // "delta" | "tool_call" | "tool_result" | "final" | "error" | "info"
	Text           string    `json:"text,omitempty"`
	ToolCall       *ToolCall `json:"tool_call,omitempty"`
	ConversationID string    `json:"conversation_id,omitempty"`
	Mode           string    `json:"mode,omitempty"`
	StopReason     string    `json:"stop_reason,omitempty"`
	Error          string    `json:"error,omitempty"`
}

// Emitter is the callback the runtime invokes for every event. Handlers
// typically write events to an SSE stream and flush.
type Emitter func(Event)

// runContext bundles the identity/config slice forwarded into audit entries.
type runContext struct {
	UserID   string
	Username string
	IP       string
}

// rateLimiter is a simple per-user sliding-window limiter keyed by (userID).
type rateLimiter struct {
	mu      sync.Mutex
	buckets map[string][]time.Time
}

func newRateLimiter() *rateLimiter {
	return &rateLimiter{buckets: make(map[string][]time.Time)}
}

// allow reports whether this user has budget for another chat turn under limit
// (per minute). Zero limit disables the check.
func (r *rateLimiter) allow(userID string, limit int) bool {
	if limit <= 0 {
		return true
	}
	now := time.Now()
	cutoff := now.Add(-time.Minute)
	r.mu.Lock()
	defer r.mu.Unlock()
	bucket := r.buckets[userID]
	fresh := bucket[:0]
	for _, t := range bucket {
		if t.After(cutoff) {
			fresh = append(fresh, t)
		}
	}
	if len(fresh) >= limit {
		r.buckets[userID] = fresh
		return false
	}
	fresh = append(fresh, now)
	r.buckets[userID] = fresh
	return true
}

// ChatTurn runs a single user turn by delegating to the `claude` CLI. It
// persists the user message, then streams CLI events both to the caller
// (via emit) and into claude_messages rows for UI transcript replay.
//
// ApprovedToolCalls is accepted for API compatibility but is a no-op under the
// CLI driver — Claude Code handles approval internally via --permission-mode.
func (m *Module) ChatTurn(ctx context.Context, userID, username, ip string, req ChatRequest, emit Emitter) (convID, finalText string, err error) {
	cfg := m.cfg.Get().Claude

	if cfg.KillSwitch {
		err = errors.New("Claude admin assistant is currently disabled by the kill-switch")
		emit(Event{Type: "error", Error: err.Error()})
		return "", "", err
	}
	if !cfg.Enabled {
		err = errors.New("Claude admin assistant is disabled in config")
		emit(Event{Type: "error", Error: err.Error()})
		return "", "", err
	}
	if !m.limiter.allow(userID, cfg.RateLimitPerMinute) {
		err = errors.New("rate limit exceeded; try again in a moment")
		emit(Event{Type: "error", Error: err.Error()})
		return "", "", err
	}

	// Load or create the conversation.
	var conv *Conversation
	if strings.TrimSpace(req.ConversationID) != "" {
		existing, _, loadErr := m.GetConversation(ctx, userID, req.ConversationID)
		if loadErr != nil {
			emit(Event{Type: "error", Error: loadErr.Error()})
			return "", "", loadErr
		}
		conv = existing
	} else {
		title := summarize(req.Message, 80)
		created, createErr := m.createConversation(ctx, userID, username, title, cfg.Mode, cfg.Model)
		if createErr != nil {
			emit(Event{Type: "error", Error: createErr.Error()})
			return "", "", createErr
		}
		conv = created
		emit(Event{Type: "info", ConversationID: conv.ID})
	}

	mode := selectMode(cfg.Mode, req.ModeOverride)
	emit(Event{Type: "info", ConversationID: conv.ID, Mode: mode})

	// Persist the user turn up-front so the transcript reflects intent even
	// if the CLI fails mid-stream.
	if err = m.appendMessage(ctx, conv.ID, "user", req.Message, nil, nil); err != nil {
		emit(Event{Type: "error", Error: "failed to persist user message: " + err.Error()})
		return conv.ID, "", err
	}

	rc := &runContext{UserID: userID, Username: username, IP: ip}

	// Aggregator: while the CLI streams events, we accumulate assistant text
	// and tool activity so we can persist compact rows at the end and forward
	// each event to the caller in real time.
	agg := newTurnAggregator(m, ctx, conv.ID, rc, emit)

	opts := cliRunOpts{
		cfg:             cfg,
		message:         req.Message,
		resumeSessionID: conv.CLISessionID,
		mode:            mode,
		systemPromptAdd: m.buildSystemPrompt(cfg, mode),
	}

	runRes, runErr := runClaudeCLI(ctx, opts, agg.handle)
	agg.flush()

	if runRes != nil && runRes.SessionID != "" && runRes.SessionID != conv.CLISessionID {
		if upErr := m.setSessionID(ctx, conv.ID, runRes.SessionID); upErr != nil {
			m.log.Warn("persist cli_session_id: %v", upErr)
		}
	}

	if runErr != nil {
		emit(Event{Type: "error", Error: runErr.Error()})
		return conv.ID, agg.finalText(), runErr
	}

	stop := ""
	text := ""
	if runRes != nil {
		stop = runRes.StopReason
		text = runRes.FinalText
	}
	if text == "" {
		text = agg.finalText()
	}
	emit(Event{Type: "final", Text: text, StopReason: stop, ConversationID: conv.ID})
	return conv.ID, text, nil
}

// turnAggregator batches streaming CLI events into DB rows. The goal is one
// assistant row per assistant-message event (keeping text + tool_uses together)
// and one tool row per tool_result, matching the existing transcript schema.
type turnAggregator struct {
	m      *Module
	ctx    context.Context
	convID string
	rc     *runContext
	emit   Emitter

	mu sync.Mutex
	// Pending assistant buffer — flushed when a tool_result arrives or at turn end.
	assistantText  strings.Builder
	assistantTools []ToolCall
	// Track emitted tool_use ids so audit entries match their results.
	toolUseByID map[string]ToolCall
	// Canonical running transcript for caller's return value.
	finalBuf strings.Builder
}

func newTurnAggregator(m *Module, ctx context.Context, convID string, rc *runContext, emit Emitter) *turnAggregator {
	return &turnAggregator{
		m:           m,
		ctx:         ctx,
		convID:      convID,
		rc:          rc,
		emit:        emit,
		toolUseByID: make(map[string]ToolCall),
	}
}

func (a *turnAggregator) handle(ev Event) {
	// Always forward to caller first so the UI updates live.
	a.emit(ev)

	a.mu.Lock()
	defer a.mu.Unlock()

	switch ev.Type {
	case "delta":
		a.assistantText.WriteString(ev.Text)
		a.finalBuf.WriteString(ev.Text)
	case "tool_call":
		if ev.ToolCall != nil {
			tc := *ev.ToolCall
			a.assistantTools = append(a.assistantTools, tc)
			a.toolUseByID[tc.ID] = tc
		}
	case "tool_result":
		if ev.ToolCall == nil {
			return
		}
		// A tool_result marks the end of the current assistant turn-chunk —
		// flush the assistant row first so ordering stays user → assistant →
		// tool → assistant → tool → ...
		a.flushAssistantLocked()
		tc := *ev.ToolCall
		if prior, ok := a.toolUseByID[tc.ID]; ok {
			if tc.Name == "" {
				tc.Name = prior.Name
			}
			if len(tc.Input) == 0 {
				tc.Input = prior.Input
			}
		}
		b, marshalErr := json.Marshal(tc)
		if marshalErr != nil {
			a.m.log.Warn("marshal tool result for %s: %v", tc.Name, marshalErr)
			b = []byte(fmt.Sprintf(`{"id":%q,"name":%q,"error":"marshal failure"}`, tc.ID, tc.Name))
		}
		if err := a.m.appendMessage(a.ctx, a.convID, "tool", "", nil, b); err != nil {
			a.m.log.Warn("persist tool message: %v", err)
		}
		a.m.auditToolCall(a.ctx, a.rc, &tc, tc.Error == "")
	}
}

func (a *turnAggregator) flush() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.flushAssistantLocked()
}

func (a *turnAggregator) flushAssistantLocked() {
	text := a.assistantText.String()
	tools := a.assistantTools
	if text == "" && len(tools) == 0 {
		return
	}
	var toolsJSON json.RawMessage
	if len(tools) > 0 {
		if b, err := json.Marshal(tools); err == nil {
			toolsJSON = b
		}
	}
	if err := a.m.appendMessage(a.ctx, a.convID, "assistant", text, toolsJSON, nil); err != nil {
		a.m.log.Warn("persist assistant message: %v", err)
	}
	a.assistantText.Reset()
	a.assistantTools = nil
}

func (a *turnAggregator) finalText() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.finalBuf.String()
}

// auditToolCall records a tool execution in the admin audit log.
func (m *Module) auditToolCall(ctx context.Context, rc *runContext, tc *ToolCall, success bool) {
	const auditOutputCap = 2048
	outputPreview := tc.Output
	if len(outputPreview) > auditOutputCap {
		outputPreview = outputPreview[:auditOutputCap] + "…[truncated]"
	}
	details := map[string]any{
		"tool":           tc.Name,
		"input":          redact(string(tc.Input)),
		"output_size":    len(tc.Output),
		"output_preview": redact(outputPreview),
	}
	if tc.Error != "" {
		details["error"] = redact(tc.Error)
	}
	m.adminMod.log(ctx, &admin.AuditLogParams{
		UserID:    rc.UserID,
		Username:  rc.Username,
		Action:    "claude.tool." + tc.Name,
		Resource:  tc.Name,
		Details:   details,
		IPAddress: rc.IP,
		Success:   success,
	})
}

// summarize returns the first n characters of s with newlines collapsed — used
// to generate conversation titles.
func summarize(s string, n int) string {
	s = strings.TrimSpace(strings.ReplaceAll(s, "\n", " "))
	if len(s) <= n {
		if s == "" {
			return "New conversation"
		}
		return s
	}
	return s[:n] + "…"
}

// writeSSE is a tiny helper callers can use to wrap Emitter for http.Flusher
// streams. Returns true on success.
func writeSSE(w io.Writer, flusher interface{ Flush() }, ev Event) bool {
	b, err := json.Marshal(ev)
	if err != nil {
		return false
	}
	if _, err := fmt.Fprintf(w, "data: %s\n\n", b); err != nil {
		return false
	}
	if flusher != nil {
		flusher.Flush()
	}
	return true
}

// setSessionID persists the CLI session id on the conversation row.
func (m *Module) setSessionID(ctx context.Context, convID, sessionID string) error {
	return m.db.GORM().WithContext(ctx).
		Model(&Conversation{}).
		Where("id = ?", convID).
		Update("cli_session_id", sessionID).Error
}
