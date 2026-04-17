package config

import (
	"strings"
	"time"
)

// applyClaudeEnvOverrides maps CLAUDE_* environment variables onto ClaudeConfig.
// Enable/disable is driven by FEATURE_CLAUDE (handled in applyFeatureEnvOverrides),
// which is the authoritative toggle and propagated to Claude.Enabled via
// syncFeatureToggles. CLAUDE_ENABLED is accepted as an alias there.
func (m *Manager) applyClaudeEnvOverrides() {
	c := &m.config.Claude

	if val := envGetStr("CLAUDE_API_KEY", "ANTHROPIC_API_KEY"); val != "" {
		c.APIKey = val
	}
	if val := envGetStr("CLAUDE_WEB_LOGIN_TOKEN"); val != "" {
		c.WebLoginToken = val
	}
	if val := envGetStr("CLAUDE_MODEL"); val != "" {
		c.Model = val
	}
	if val := envGetStr("CLAUDE_MODE"); val != "" {
		switch strings.ToLower(val) {
		case "advisory", "interactive", "autonomous":
			c.Mode = strings.ToLower(val)
		}
	}
	if val, ok := envGetInt("CLAUDE_MAX_TOKENS"); ok {
		c.MaxTokens = val
	}
	if val := envGetStr("CLAUDE_SYSTEM_PROMPT"); val != "" {
		c.SystemPrompt = val
	}
	if val := envGetStr("CLAUDE_ALLOWED_TOOLS"); val != "" {
		c.AllowedTools = splitTrimmed(val)
	}
	if val := envGetStr("CLAUDE_ALLOWED_SHELL_COMMANDS"); val != "" {
		c.AllowedShellCommands = splitTrimmed(val)
	}
	if val := envGetStr("CLAUDE_ALLOWED_PATHS"); val != "" {
		c.AllowedPaths = splitTrimmed(val)
	}
	if val := envGetStr("CLAUDE_ALLOWED_SERVICES"); val != "" {
		c.AllowedServices = splitTrimmed(val)
	}
	if val, ok := envGetBool("CLAUDE_REQUIRE_CONFIRM_FOR_WRITES"); ok {
		c.RequireConfirmForWrites = val
	}
	if val, ok := envGetInt("CLAUDE_MAX_TOOL_CALLS_PER_TURN"); ok {
		c.MaxToolCallsPerTurn = val
	}
	if val, ok := envGetInt("CLAUDE_RATE_LIMIT_PER_MINUTE"); ok {
		c.RateLimitPerMinute = val
	}
	if val, ok := envGetBool("CLAUDE_KILL_SWITCH"); ok {
		c.KillSwitch = val
	}
	if val, ok := envGetDuration(time.Second, "CLAUDE_REQUEST_TIMEOUT"); ok {
		c.RequestTimeout = val
	}
	if val, ok := envGetInt("CLAUDE_HISTORY_RETENTION_DAYS"); ok {
		c.HistoryRetentionDays = val
	}
}
