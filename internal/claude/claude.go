// Package claude wraps the `claude` CLI (Claude Code) so authorized admins
// can drive it from the web UI. The CLI is executed as a subprocess on each
// turn; it owns the tool layer (Read/Edit/Bash/etc.) and its own OAuth
// credentials stored at ~/.claude/.credentials.json (authenticate with
// `claude login` on the VPS). Conversations are persisted to MySQL for
// transcript replay; the CLI's own session id is stored per-row so resuming a
// conversation simply passes --resume <session-id>.
package claude

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

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

// NewModule creates the module.
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
		limiter:  newRateLimiter(),
	}, nil
}

// Name satisfies server.Module.
func (m *Module) Name() string { return "claude" }

// Start validates config and probes the CLI. A missing or unauthenticated CLI
// is logged as a warning but does not fail startup — the admin can resolve it
// from the Settings tab.
func (m *Module) Start(ctx context.Context) error {
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
	if path, ver, err := CheckCLIAvailable(ctx, c); err != nil {
		m.log.Warn("Claude CLI not ready: %v", err)
	} else {
		m.log.Info("Claude CLI ready: %s (%s)", path, ver)
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
	defer m.healthMu.Unlock()
	m.healthy = ok
	m.healthMsg = msg
}

// PublicConfig returns the client-safe config view.
func (m *Module) PublicConfig() PublicConfig {
	c := m.cfg.Get().Claude
	return PublicConfig{
		Enabled:                 c.Enabled,
		BinaryPath:              c.BinaryPath,
		Workdir:                 c.Workdir,
		Model:                   c.Model,
		Mode:                    c.Mode,
		MaxTokens:               c.MaxTokens,
		SystemPrompt:            c.SystemPrompt,
		RequireConfirmForWrites: c.RequireConfirmForWrites,
		MaxToolCallsPerTurn:     c.MaxToolCallsPerTurn,
		RateLimitPerMinute:      c.RateLimitPerMinute,
		KillSwitch:              c.KillSwitch,
		HistoryRetentionDays:    c.HistoryRetentionDays,
	}
}

// AuthStatus reports whether the `claude` CLI is installed and authenticated
// on the host. Returned directly to the admin UI so the operator knows when
// to run `claude login` on the VPS.
type AuthStatus struct {
	Installed     bool   `json:"installed"`
	BinaryPath    string `json:"binary_path,omitempty"`
	Version       string `json:"version,omitempty"`
	Authenticated bool   `json:"authenticated"`
	Message       string `json:"message,omitempty"`
}

// GetAuthStatus runs a fast probe against the CLI and summarizes the result.
func (m *Module) GetAuthStatus(ctx context.Context) AuthStatus {
	c := m.cfg.Get().Claude
	path, ver, err := CheckCLIAvailable(ctx, c)
	if err != nil {
		return AuthStatus{Installed: false, Message: err.Error()}
	}
	status := AuthStatus{Installed: true, BinaryPath: path, Version: ver, Authenticated: true}
	if probeErr := ProbeAuth(ctx, c); probeErr != nil {
		status.Authenticated = false
		status.Message = probeErr.Error()
	}
	return status
}

// UpdateSettings applies a partial settings update. Keys are validated and
// missing/invalid ones are silently skipped.
func (m *Module) UpdateSettings(updates map[string]any) error {
	batch := map[string]any{}
	for k, v := range updates {
		switch k {
		case "enabled":
			if b, ok := v.(bool); ok {
				// Drive through the feature-toggle master so it is consistent with
				// all other modules. syncFeatureToggles propagates this to
				// claude.enabled automatically after SetValuesBatch returns.
				batch["features.enable_claude"] = b
			}
		case "binary_path":
			if s, ok := v.(string); ok {
				batch["claude.binary_path"] = strings.TrimSpace(s)
			}
		case "workdir":
			if s, ok := v.(string); ok {
				batch["claude.workdir"] = strings.TrimSpace(s)
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
	return m.cfg.SetValuesBatch(batch)
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
		Order("seq ASC, created_at ASC").
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
// Seq is assigned here by reading MAX(seq) for the conversation so that messages
// retain insertion order regardless of created_at clock resolution.
func (m *Module) appendMessage(ctx context.Context, convID, role, content string, toolCalls, toolResult json.RawMessage) error {
	var maxSeq int64
	m.db.GORM().WithContext(ctx).
		Model(&Message{}).
		Where("conversation_id = ?", convID).
		Select("COALESCE(MAX(seq), 0)").
		Scan(&maxSeq)

	msg := &Message{
		ID:             uuid.New().String(),
		ConversationID: convID,
		Seq:            maxSeq + 1,
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
// role and operational boundaries. This is passed to the CLI via
// --append-system-prompt, so it layers on top of Claude Code's own system
// prompt rather than replacing it.
func (m *Module) buildSystemPrompt(c config.ClaudeConfig, mode string) string {
	var b strings.Builder
	b.WriteString("You are acting as the autonomous self-repairing systems administrator for Media Server Pro 4, deployed on host ")
	b.WriteString(hostIdentity())
	b.WriteString(".\n\n")
	b.WriteString("## Deployment context\n")
	b.WriteString("- Stack: Go backend (media-server-pro binary), Nuxt/Vue 3 frontend (nuxt-ui), MySQL 8, systemd services\n")
	b.WriteString("- App dir: /home/deployment/media-server-pro (or UPDATER_APP_DIR if set)\n")
	b.WriteString("- Deploy script: bash deploy.sh --dev (always use this, never raw rsync/ssh shortcuts)\n")
	b.WriteString("- Live domain: xmodsxtreme.com\n")
	b.WriteString("- Config: .env file in app dir; hot-reload via admin API\n")
	b.WriteString("- Logs: LOGS_DIR (default ./logs); also journalctl -u media-server-pro\n")
	b.WriteString("- Downloader sidecar: http://localhost:4000 (DOWNLOADER_URL)\n")
	b.WriteString("- Key binaries: ffmpeg, mysql, systemctl, git, go, node, npm\n\n")
	fmt.Fprintf(&b, "## Operational mode: %s\n", mode)
	switch mode {
	case ModeAdvisory:
		b.WriteString("Advisory (plan mode): diagnose and propose fixes. Tools are restricted to planning.\n")
	case ModeAutonomous:
		b.WriteString("Autonomous: execute tools directly, investigate first, then act. Narrate each action briefly. Proceed through multi-step repairs end-to-end.\n")
	default:
		b.WriteString("Interactive: state intent clearly before each write.\n")
	}
	b.WriteString("\n## Core rules\n")
	b.WriteString("- Never print secrets, API keys, passwords, or session tokens in plain text.\n")
	b.WriteString("- When a tool returns an error, diagnose before retrying — don't repeat a failing call unchanged.\n")
	b.WriteString("- For database operations prefer SELECT before UPDATE/DELETE; show a plan for destructive queries.\n")
	b.WriteString("- systemctl restart media-server-pro will briefly drop active streams — only do this if necessary.\n")
	if s := strings.TrimSpace(c.SystemPrompt); s != "" {
		b.WriteString("\n## Operator notes\n")
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
		m = ModeAutonomous
	}
	return m
}
