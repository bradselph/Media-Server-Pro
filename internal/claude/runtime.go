package claude

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/anthropics/anthropic-sdk-go"

	"media-server-pro/internal/admin"
	"media-server-pro/internal/config"
)

// Event is a single SSE-ready event emitted during a chat turn. The handler
// serializes this to JSON and writes it framed as SSE.
type Event struct {
	Type           string    `json:"type"` // "delta" | "tool_call" | "tool_result" | "tool_pending" | "final" | "error" | "info"
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

// ChatTurn runs a single user turn against the configured Claude model. It
// handles the tool-use loop internally: it keeps calling the API, executing
// tools, and appending their results until the model ends the turn. It
// persists every message and audits every tool invocation.
//
// Returns the conversation ID (creates one if request.ConversationID is empty)
// and the final assistant text. Errors are reported both via return and via
// emit(Event{Type:"error"}).
func (m *Module) ChatTurn(ctx context.Context, userID, username, ip string, req ChatRequest, emit Emitter) (convID, finalText string, err error) {
	cfg := m.cfg.Get().Claude

	// Hard gates: kill-switch and module-enabled.
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
	if err = m.ensureClient(cfg); err != nil {
		emit(Event{Type: "error", Error: err.Error()})
		return "", "", err
	}

	// Load or create the conversation and its prior message history.
	var conv *Conversation
	var history []Message
	if strings.TrimSpace(req.ConversationID) != "" {
		var loadErr error
		conv, history, loadErr = m.GetConversation(ctx, userID, req.ConversationID)
		if loadErr != nil {
			emit(Event{Type: "error", Error: loadErr.Error()})
			return "", "", loadErr
		}
	} else {
		title := summarize(req.Message, 80)
		var createErr error
		conv, createErr = m.createConversation(ctx, userID, username, title, cfg.Mode, cfg.Model)
		if createErr != nil {
			emit(Event{Type: "error", Error: createErr.Error()})
			return "", "", createErr
		}
		emit(Event{Type: "info", ConversationID: conv.ID})
	}

	mode := selectMode(cfg.Mode, req.ModeOverride)
	emit(Event{Type: "info", ConversationID: conv.ID, Mode: mode})

	approved := map[string]struct{}{}
	for _, id := range req.ApprovedToolCalls {
		approved[id] = struct{}{}
	}

	// Persist the user's message immediately so the transcript is durable
	// even if the API call fails.
	if err = m.appendMessage(ctx, conv.ID, "user", req.Message, nil, nil); err != nil {
		emit(Event{Type: "error", Error: "failed to persist user message: " + err.Error()})
		return conv.ID, "", err
	}

	// Build SDK messages from history + this turn.
	sdkMessages, err := buildSDKMessages(history, req.Message)
	if err != nil {
		emit(Event{Type: "error", Error: err.Error()})
		return conv.ID, "", err
	}

	rc := &RunContext{Cfg: cfg, UserID: userID, Username: username, IP: ip}
	system := m.buildSystemPrompt(cfg, mode)

	turnCtx := ctx
	if cfg.RequestTimeout > 0 {
		var cancel context.CancelFunc
		turnCtx, cancel = context.WithTimeout(ctx, cfg.RequestTimeout*2)
		defer cancel()
	}

	maxIter := cfg.MaxToolCallsPerTurn
	if maxIter <= 0 {
		maxIter = 16
	}

	toolUnion := m.buildToolUnion(cfg)
	modelID := cfg.Model
	if modelID == "" {
		modelID = string(anthropic.ModelClaudeSonnet4_6)
	}
	maxTokens := int64(cfg.MaxTokens)
	if maxTokens <= 0 {
		maxTokens = 4096
	}

	var finalTextBuf strings.Builder

	// Tool-use loop.
	for iter := 0; iter < maxIter; iter++ {
		params := anthropic.MessageNewParams{
			Model:     anthropic.Model(modelID),
			MaxTokens: maxTokens,
			System:    []anthropic.TextBlockParam{{Text: system}},
			Messages:  sdkMessages,
			Tools:     toolUnion,
		}

		m.clientMu.RLock()
		client := m.client
		m.clientMu.RUnlock()
		if client == nil {
			e := errors.New("Claude SDK client is not configured")
			emit(Event{Type: "error", Error: e.Error()})
			return conv.ID, finalTextBuf.String(), e
		}

		resp, apiErr := client.Messages.New(turnCtx, params)
		if apiErr != nil {
			emit(Event{Type: "error", Error: apiErr.Error()})
			return conv.ID, finalTextBuf.String(), apiErr
		}

		// Append assistant message to SDK history and persist its text.
		sdkMessages = append(sdkMessages, resp.ToParam())

		var iterText strings.Builder
		var iterToolCalls []ToolCall
		for _, block := range resp.Content {
			switch block.Type {
			case "text":
				iterText.WriteString(block.Text)
				if block.Text != "" {
					emit(Event{Type: "delta", Text: block.Text})
				}
			case "tool_use":
				tc := ToolCall{
					ID:    block.ID,
					Name:  block.Name,
					Input: block.Input,
				}
				iterToolCalls = append(iterToolCalls, tc)
			}
		}

		// Persist the assistant turn (text + tool calls).
		var toolCallsJSON json.RawMessage
		if len(iterToolCalls) > 0 {
			b, _ := json.Marshal(iterToolCalls)
			toolCallsJSON = b
		}
		if err := m.appendMessage(ctx, conv.ID, "assistant", iterText.String(), toolCallsJSON, nil); err != nil {
			m.log.Warn("persist assistant message: %v", err)
		}
		finalTextBuf.WriteString(iterText.String())

		if resp.StopReason != anthropic.StopReasonToolUse || len(iterToolCalls) == 0 {
			emit(Event{Type: "final", Text: iterText.String(), StopReason: string(resp.StopReason), ConversationID: conv.ID})
			return conv.ID, finalTextBuf.String(), nil
		}

		// Execute each tool the model requested.
		toolResults := make([]anthropic.ContentBlockParamUnion, 0, len(iterToolCalls))
		pendingHit := false
		for _, tc := range iterToolCalls {
			emit(Event{Type: "tool_call", ToolCall: &ToolCall{ID: tc.ID, Name: tc.Name, Input: tc.Input}})

			outText, gated, execErr := m.invokeTool(turnCtx, tc, rc, mode, approved)
			completed := tc
			completed.Output = outText
			if execErr != nil {
				completed.Error = execErr.Error()
			}
			completed.RequiresConfirm = gated

			if gated {
				pendingHit = true
				emit(Event{Type: "tool_pending", ToolCall: &completed})
				// Persist the gate so the UI sees it in the transcript.
				b, _ := json.Marshal(completed)
				_ = m.appendMessage(ctx, conv.ID, "tool", "", nil, b)
				// Provide the model with a synthetic tool_result so it can
				// continue reasoning while it waits for approval.
				msg := "Tool execution paused pending admin approval. Do not retry automatically."
				toolResults = append(toolResults, anthropic.NewToolResultBlock(tc.ID, msg, true))
			} else {
				// Audit success/failure. Redact input before writing.
				m.auditToolCall(ctx, rc, &completed, execErr == nil)

				emit(Event{Type: "tool_result", ToolCall: &completed})
				b, _ := json.Marshal(completed)
				_ = m.appendMessage(ctx, conv.ID, "tool", "", nil, b)

				content := outText
				isErr := execErr != nil
				if isErr {
					content = "ERROR: " + execErr.Error()
				}
				content = redact(content)
				const maxToolResultBytes = 32 * 1024
				if len(content) > maxToolResultBytes {
					content = content[:maxToolResultBytes] + "\n...[truncated]"
				}
				toolResults = append(toolResults, anthropic.NewToolResultBlock(tc.ID, content, isErr))
			}
		}
		sdkMessages = append(sdkMessages, anthropic.NewUserMessage(toolResults...))

		if pendingHit {
			// Stop the loop and wait for the admin to approve specific tool
			// calls by resubmitting with ApprovedToolCalls populated.
			emit(Event{Type: "final", StopReason: "awaiting_approval", ConversationID: conv.ID})
			return conv.ID, finalTextBuf.String(), nil
		}
	}

	emit(Event{Type: "final", StopReason: "max_iterations", ConversationID: conv.ID})
	return conv.ID, finalTextBuf.String(), nil
}

// invokeTool dispatches to the registered Tool after running mode/allowlist
// gates. Returns (output, gated, error). When gated=true the tool was refused
// pending admin approval.
func (m *Module) invokeTool(ctx context.Context, tc ToolCall, rc *RunContext, mode string, approved map[string]struct{}) (string, bool, error) {
	m.toolsMu.RLock()
	tool := m.tools[tc.Name]
	m.toolsMu.RUnlock()
	if tool == nil {
		return "", false, fmt.Errorf("tool %q is not registered", tc.Name)
	}
	if !m.toolEnabledForConfig(tc.Name, rc.Cfg) {
		return "", false, fmt.Errorf("tool %q is not in AllowedTools", tc.Name)
	}

	// Advisory mode: block writes entirely.
	if mode == ModeAdvisory && tool.IsWrite() {
		return "", false, errors.New("advisory mode: write tools are disabled")
	}

	// Interactive mode OR (autonomous + RequireConfirmForWrites): gate writes.
	needConfirm := tool.IsWrite() && (mode == ModeInteractive || rc.Cfg.RequireConfirmForWrites)
	if needConfirm {
		if _, ok := approved[tc.ID]; !ok {
			return "", true, nil
		}
	}

	// Execute with a per-tool timeout to keep a misbehaving tool from hanging
	// the whole turn.
	toolCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	out, err := tool.Execute(toolCtx, tc.Input, rc)
	if err != nil {
		return "", false, err
	}
	return out, false, nil
}

// auditToolCall records a tool execution in the admin audit log.
func (m *Module) auditToolCall(ctx context.Context, rc *RunContext, tc *ToolCall, success bool) {
	details := map[string]any{
		"tool":        tc.Name,
		"input":       redact(string(tc.Input)),
		"output_size": len(tc.Output),
	}
	if tc.Error != "" {
		details["error"] = tc.Error
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

// buildToolUnion converts registered tools (filtered by allowlist) into the
// SDK's ToolUnionParam format for use in MessageNewParams.Tools.
func (m *Module) buildToolUnion(cfg config.ClaudeConfig) []anthropic.ToolUnionParam {
	m.toolsMu.RLock()
	defer m.toolsMu.RUnlock()
	out := make([]anthropic.ToolUnionParam, 0, len(m.tools))
	for _, t := range m.tools {
		if !m.toolEnabledForConfig(t.Name(), cfg) {
			continue
		}
		schema := anthropic.ToolInputSchemaParam{
			ExtraFields: t.InputSchema(),
		}
		tool := anthropic.ToolParam{
			Name:        t.Name(),
			InputSchema: schema,
		}
		if desc := t.Description(); desc != "" {
			tool.Description = anthropic.String(desc)
		}
		out = append(out, anthropic.ToolUnionParam{OfTool: &tool})
	}
	return out
}

// buildSDKMessages reconstructs an anthropic.MessageParam slice from the stored
// conversation history and the new user turn. Assistant text turns are
// preserved; tool_use/tool_result pairs are replayed as-is from their stored
// JSON so Claude's reasoning remains stable across restarts.
func buildSDKMessages(history []Message, newUser string) ([]anthropic.MessageParam, error) {
	msgs := make([]anthropic.MessageParam, 0, len(history)+1)

	var pendingToolResults []anthropic.ContentBlockParamUnion
	flushToolResults := func() {
		if len(pendingToolResults) > 0 {
			msgs = append(msgs, anthropic.NewUserMessage(pendingToolResults...))
			pendingToolResults = nil
		}
	}

	for _, h := range history {
		switch h.Role {
		case "user":
			flushToolResults()
			msgs = append(msgs, anthropic.NewUserMessage(anthropic.NewTextBlock(h.Content)))
		case "assistant":
			flushToolResults()
			blocks := []anthropic.ContentBlockParamUnion{}
			if strings.TrimSpace(h.Content) != "" {
				blocks = append(blocks, anthropic.NewTextBlock(h.Content))
			}
			if len(h.ToolCalls) > 0 {
				var tcs []ToolCall
				if err := json.Unmarshal(h.ToolCalls, &tcs); err == nil {
					for _, tc := range tcs {
						var input any
						if len(tc.Input) > 0 {
							_ = json.Unmarshal(tc.Input, &input)
						}
						blocks = append(blocks, anthropic.NewToolUseBlock(tc.ID, input, tc.Name))
					}
				}
			}
			if len(blocks) > 0 {
				msgs = append(msgs, anthropic.NewAssistantMessage(blocks...))
			}
		case "tool":
			if len(h.ToolResult) > 0 {
				var tc ToolCall
				if err := json.Unmarshal(h.ToolResult, &tc); err == nil && tc.ID != "" {
					content := tc.Output
					if tc.Error != "" {
						content = "ERROR: " + tc.Error
					}
					pendingToolResults = append(pendingToolResults, anthropic.NewToolResultBlock(tc.ID, content, tc.Error != ""))
				}
			}
		}
	}
	flushToolResults()

	msgs = append(msgs, anthropic.NewUserMessage(anthropic.NewTextBlock(newUser)))
	return msgs, nil
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
// streams. Kept here to keep the handler layer thin. Returns true on success.
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
