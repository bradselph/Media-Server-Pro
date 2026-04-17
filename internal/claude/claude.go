// Package claude implements the Claude-powered admin assistant module.
// It wraps the Anthropic Go SDK, exposes a typed tool-calling layer over the
// running server (logs, config, processes, shell, services), persists
// conversations to MySQL via GORM, and feeds every action through the existing
// admin audit log. Authorization is handled at the route layer — this package
// assumes the caller is an authenticated admin.
package claude

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"media-server-pro/internal/admin"
	"media-server-pro/internal/config"
	"media-server-pro/internal/database"
	"media-server-pro/internal/logger"
	"media-server-pro/pkg/helpers"
	"media-server-pro/pkg/models"
)

// ModeAdvisory, ModeInteractive, ModeAutonomous are the supported run modes.
const (
	ModeAdvisory    = "advisory"
	ModeInteractive = "interactive"
	ModeAutonomous  = "autonomous"
)

// Module is the Claude admin assistant service.
type Module struct {
	cfg      *config.Manager
	log      *logger.Logger
	db       *database.Module
	adminMod *adminLogger

	clientMu sync.RWMutex
	client   *anthropic.Client
	apiKey   string // last used API key; compared to detect rotation

	toolsMu sync.RWMutex
	tools   map[string]Tool

	limiter *rateLimiter

	healthMu  sync.RWMutex
	healthy   bool
	healthMsg string
	startTime time.Time
}

// adminLogger is a tiny wrapper around *admin.Module that tolerates nil so
// Claude can be constructed before the admin module finishes starting.
type adminLogger struct {
	a *admin.Module
}

func (a *adminLogger) log(ctx context.Context, p *admin.AuditLogParams) {
	if a == nil || a.a == nil {
		return
	}
	a.a.LogAction(ctx, p)
}

// Deps holds constructor dependencies for the Claude module.
type Deps struct {
	Config *config.Manager
	DB     *database.Module
	Admin  *admin.Module
}

// NewModule creates the module. Tools must be registered by callers (typically
// in cmd/server/main.go via RegisterDefaultTools) before Start().
func NewModule(d Deps) (*Module, error) {
	if d.Config == nil {
		return nil, errors.New("claude: config manager required")
	}
	if d.DB == nil {
		return nil, errors.New("claude: database module required")
	}
	return &Module{
		cfg:      d.Config,
		log:      logger.New("claude"),
		db:       d.DB,
		adminMod: &adminLogger{a: d.Admin},
		tools:    make(map[string]Tool),
		limiter:  newRateLimiter(),
	}, nil
}

// Name satisfies server.Module.
func (m *Module) Name() string { return "claude" }

// Start satisfies server.Module. It validates config, initializes the SDK
// client if an API key is configured, and schedules a retention sweep.
func (m *Module) Start(_ context.Context) error {
	m.startTime = time.Now()
	c := m.cfg.Get().Claude
	if !c.Enabled {
		m.setHealth(true, "disabled")
		m.log.Info("Claude admin assistant disabled by config")
		return nil
	}
	if !m.db.IsConnected() {
		return errors.New("claude: database not connected")
	}
	if err := m.ensureClient(c); err != nil {
		// Not fatal — admin can configure API key later via UI; module stays
		// healthy-but-idle so the tab remains reachable.
		m.log.Warn("Claude SDK client init deferred: %v", err)
	}
	m.setHealth(true, "Running")
	m.log.Info("Claude admin assistant started (model=%s, mode=%s)", c.Model, c.Mode)
	return nil
}

// Stop satisfies server.Module.
func (m *Module) Stop(_ context.Context) error {
	m.setHealth(false, "Stopped")
	return nil
}

// Health satisfies server.Module.
func (m *Module) Health() models.HealthStatus {
	m.healthMu.RLock()
	defer m.healthMu.RUnlock()
	return models.HealthStatus{
		Name:      m.Name(),
		Status:    helpers.StatusString(m.healthy),
		Message:   m.healthMsg,
		CheckedAt: time.Now(),
	}
}

func (m *Module) setHealth(ok bool, msg string) {
	m.healthMu.Lock()
	m.healthy = ok
	m.healthMsg = msg
	m.healthMu.Unlock()
}

// ensureClient lazy-initializes the Anthropic SDK client. Re-creates the
// client when the API key changes so rotations take effect immediately.
func (m *Module) ensureClient(c config.ClaudeConfig) error {
	if c.APIKey == "" {
		return errors.New("CLAUDE_API_KEY is not set")
	}
	m.clientMu.Lock()
	defer m.clientMu.Unlock()
	if m.client != nil && m.apiKey == c.APIKey {
		return nil
	}
	cli := anthropic.NewClient(option.WithAPIKey(c.APIKey))
	m.client = &cli
	m.apiKey = c.APIKey
	return nil
}

// RegisterTool adds a tool to the registry. Tool registration happens once at
// startup; handlers should not register tools per-request.
func (m *Module) RegisterTool(t Tool) {
	if t == nil || t.Name() == "" {
		return
	}
	m.toolsMu.Lock()
	defer m.toolsMu.Unlock()
	m.tools[t.Name()] = t
}

// listAvailableTools returns tool names currently enabled by config.
func (m *Module) listAvailableTools() []string {
	c := m.cfg.Get().Claude
	m.toolsMu.RLock()
	defer m.toolsMu.RUnlock()
	names := make([]string, 0, len(m.tools))
	for name := range m.tools {
		if !m.toolEnabledForConfig(name, c) {
			continue
		}
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func (m *Module) toolEnabledForConfig(name string, c config.ClaudeConfig) bool {
	if len(c.AllowedTools) == 0 {
		return true
	}
	for _, a := range c.AllowedTools {
		if a == name {
			return true
		}
	}
	return false
}

// PublicConfig returns the client-safe config view.
func (m *Module) PublicConfig() PublicConfig {
	c := m.cfg.Get().Claude
	return PublicConfig{
		Enabled:                 c.Enabled,
		APIKeySet:               strings.TrimSpace(c.APIKey) != "",
		Model:                   c.Model,
		Mode:                    c.Mode,
		MaxTokens:               c.MaxTokens,
		SystemPrompt:            c.SystemPrompt,
		AllowedTools:            append([]string(nil), c.AllowedTools...),
		AllowedShellCommands:    append([]string(nil), c.AllowedShellCommands...),
		AllowedPaths:            append([]string(nil), c.AllowedPaths...),
		AllowedServices:         append([]string(nil), c.AllowedServices...),
		RequireConfirmForWrites: c.RequireConfirmForWrites,
		MaxToolCallsPerTurn:     c.MaxToolCallsPerTurn,
		RateLimitPerMinute:      c.RateLimitPerMinute,
		KillSwitch:              c.KillSwitch,
		HistoryRetentionDays:    c.HistoryRetentionDays,
		AvailableTools:          m.listAvailableTools(),
	}
}

// UpdateSettings applies a partial settings update. Keys are validated and
// missing/invalid ones are silently skipped. The raw API key can only be set
// via the dedicated "api_key" field; it is never returned.
func (m *Module) UpdateSettings(updates map[string]any) error {
	batch := map[string]any{}
	for k, v := range updates {
		switch k {
		case "enabled":
			if b, ok := v.(bool); ok {
				batch["claude.enabled"] = b
			}
		case "api_key":
			if s, ok := v.(string); ok {
				batch["claude.api_key"] = s
			}
		case "model":
			if s, ok := v.(string); ok {
				batch["claude.model"] = s
			}
		case "mode":
			if s, ok := v.(string); ok {
				s = strings.ToLower(s)
				if s != ModeAdvisory && s != ModeInteractive && s != ModeAutonomous {
					return fmt.Errorf("invalid mode %q", s)
				}
				batch["claude.mode"] = s
			}
		case "max_tokens":
			if n, ok := toInt(v); ok {
				batch["claude.max_tokens"] = n
			}
		case "system_prompt":
			if s, ok := v.(string); ok {
				batch["claude.system_prompt"] = s
			}
		case "allowed_tools":
			batch["claude.allowed_tools"] = toStringSlice(v)
		case "allowed_shell_commands":
			batch["claude.allowed_shell_commands"] = toStringSlice(v)
		case "allowed_paths":
			batch["claude.allowed_paths"] = toStringSlice(v)
		case "allowed_services":
			batch["claude.allowed_services"] = toStringSlice(v)
		case "require_confirm_for_writes":
			if b, ok := v.(bool); ok {
				batch["claude.require_confirm_for_writes"] = b
			}
		case "max_tool_calls_per_turn":
			if n, ok := toInt(v); ok {
				batch["claude.max_tool_calls_per_turn"] = n
			}
		case "rate_limit_per_minute":
			if n, ok := toInt(v); ok {
				batch["claude.rate_limit_per_minute"] = n
			}
		case "kill_switch":
			if b, ok := v.(bool); ok {
				batch["claude.kill_switch"] = b
			}
		case "history_retention_days":
			if n, ok := toInt(v); ok {
				batch["claude.history_retention_days"] = n
			}
		}
	}
	if len(batch) == 0 {
		return nil
	}
	if err := m.cfg.SetValuesBatch(batch); err != nil {
		return err
	}
	// Rotate client if API key changed.
	_ = m.ensureClient(m.cfg.Get().Claude)
	return nil
}

// SetKillSwitch is a focused helper exposed to the handler layer so an admin
// can disable all activity in a single click.
func (m *Module) SetKillSwitch(on bool) error {
	return m.cfg.SetValuesBatch(map[string]any{"claude.kill_switch": on})
}

func toInt(v any) (int, bool) {
	switch x := v.(type) {
	case int:
		return x, true
	case int64:
		return int(x), true
	case float64:
		return int(x), true
	case string:
		var n int
		_, err := fmt.Sscanf(x, "%d", &n)
		return n, err == nil
	}
	return 0, false
}

func toStringSlice(v any) []string {
	switch x := v.(type) {
	case []string:
		return append([]string(nil), x...)
	case []any:
		out := make([]string, 0, len(x))
		for _, it := range x {
			if s, ok := it.(string); ok && strings.TrimSpace(s) != "" {
				out = append(out, strings.TrimSpace(s))
			}
		}
		return out
	case string:
		parts := strings.Split(x, ",")
		out := make([]string, 0, len(parts))
		for _, p := range parts {
			if s := strings.TrimSpace(p); s != "" {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}

// ListConversations returns conversations for the given admin user.
func (m *Module) ListConversations(ctx context.Context, userID string, limit int) ([]Conversation, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	var out []Conversation
	err := m.db.GORM().WithContext(ctx).
		Where("user_id = ?", userID).
		Order("updated_at DESC").
		Limit(limit).
		Find(&out).Error
	return out, err
}

// GetConversation returns a single conversation if it belongs to userID.
func (m *Module) GetConversation(ctx context.Context, userID, id string) (*Conversation, []Message, error) {
	var conv Conversation
	if err := m.db.GORM().WithContext(ctx).
		Where("id = ? AND user_id = ?", id, userID).
		First(&conv).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil, errors.New("conversation not found")
		}
		return nil, nil, err
	}
	var msgs []Message
	if err := m.db.GORM().WithContext(ctx).
		Where("conversation_id = ?", id).
		Order("created_at ASC").
		Find(&msgs).Error; err != nil {
		return nil, nil, err
	}
	return &conv, msgs, nil
}

// DeleteConversation deletes a conversation (and cascades messages).
func (m *Module) DeleteConversation(ctx context.Context, userID, id string) error {
	res := m.db.GORM().WithContext(ctx).
		Where("id = ? AND user_id = ?", id, userID).
		Delete(&Conversation{})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return errors.New("conversation not found")
	}
	return nil
}

// PurgeOld deletes conversations older than HistoryRetentionDays. Safe to call
// periodically.
func (m *Module) PurgeOld(ctx context.Context) error {
	days := m.cfg.Get().Claude.HistoryRetentionDays
	if days <= 0 {
		return nil
	}
	cutoff := time.Now().AddDate(0, 0, -days)
	return m.db.GORM().WithContext(ctx).
		Where("updated_at < ?", cutoff).
		Delete(&Conversation{}).Error
}

// createConversation inserts a new conversation owned by the given admin.
func (m *Module) createConversation(ctx context.Context, userID, username, title, mode, model string) (*Conversation, error) {
	c := &Conversation{
		ID:       uuid.New().String(),
		UserID:   userID,
		Username: username,
		Title:    title,
		Mode:     mode,
		Model:    model,
	}
	if err := m.db.GORM().WithContext(ctx).Create(c).Error; err != nil {
		return nil, err
	}
	return c, nil
}

// appendMessage persists a single conversation message.
func (m *Module) appendMessage(ctx context.Context, convID, role, content string, toolCalls, toolResult json.RawMessage) error {
	msg := &Message{
		ID:             uuid.New().String(),
		ConversationID: convID,
		Role:           role,
		Content:        content,
		ToolCalls:      toolCalls,
		ToolResult:     toolResult,
	}
	if err := m.db.GORM().WithContext(ctx).Create(msg).Error; err != nil {
		return err
	}
	// Bump conversation updated_at so the sidebar sort order stays fresh.
	return m.db.GORM().WithContext(ctx).
		Model(&Conversation{}).
		Where("id = ?", convID).
		Update("updated_at", time.Now()).Error
}

// hostIdentity reports who/where the process is running so the system prompt
// can make Claude aware of its environment.
func hostIdentity() string {
	h, _ := os.Hostname()
	if h == "" {
		h = "unknown"
	}
	return h
}

// buildSystemPrompt produces the system prompt that frames the assistant's
// role and operational boundaries. Appended to the admin's custom prompt.
func (m *Module) buildSystemPrompt(c config.ClaudeConfig, mode string) string {
	var b strings.Builder
	b.WriteString("You are an operations assistant embedded in Media Server Pro 4, running on host ")
	b.WriteString(hostIdentity())
	b.WriteString(".\n\nYour job is to help the signed-in administrator observe, diagnose, and fix issues in the live deployment. ")
	b.WriteString("You have direct, read-level access to logs, running config, system info, and files in an allowlisted set of paths. ")
	b.WriteString("You can also execute allowlisted shell commands and restart allowlisted services.\n\n")
	fmt.Fprintf(&b, "Current operational mode: %s.\n", mode)
	switch mode {
	case ModeAdvisory:
		b.WriteString("- Advisory mode: propose diagnoses and fixes as text; DO NOT invoke any tools that modify state (file_write, shell_exec, service_restart). Read tools are permitted.\n")
	case ModeAutonomous:
		b.WriteString("- Autonomous mode: you may execute allowlisted tools directly. Still prefer read-only investigation first, then smallest reversible change. Narrate every action.\n")
	default:
		b.WriteString("- Interactive mode: you may call read tools directly; for any tool that modifies state the server will gate execution behind admin approval. Explain what you intend before each write.\n")
	}
	b.WriteString("\nOperational rules:\n")
	b.WriteString("- Never print secrets (API keys, passwords, session tokens). Assume redaction is imperfect.\n")
	b.WriteString("- Prefer the smallest, reversible change. State your hypothesis before acting.\n")
	b.WriteString("- When a tool returns an error, read it carefully; don't retry the same failing call.\n")
	b.WriteString("- If an action would affect running streams or user data, call it out explicitly and ask for confirmation.\n")
	if s := strings.TrimSpace(c.SystemPrompt); s != "" {
		b.WriteString("\nOperator-provided guidance:\n")
		b.WriteString(s)
		b.WriteString("\n")
	}
	return b.String()
}

// selectMode picks the effective mode for a turn. Override must be one of the
// three valid modes; anything else falls back to the config mode.
func selectMode(cfgMode, override string) string {
	m := strings.ToLower(strings.TrimSpace(override))
	if m != "" && (m == ModeAdvisory || m == ModeInteractive || m == ModeAutonomous) {
		return m
	}
	m = strings.ToLower(strings.TrimSpace(cfgMode))
	if m != ModeAdvisory && m != ModeInteractive && m != ModeAutonomous {
		m = ModeInteractive
	}
	return m
}
