package config

import (
	"strings"
	"time"
)

// applyClaudeEnvOverrides maps CLAUDE_* environment variables onto ClaudeConfig.
// Enable/disable is driven by FEATURE_CLAUDE (handled in applyFeatureEnvOverrides),
// which is the authoritative toggle and propagated to Claude.Enabled via
// syncFeatureToggles. CLAUDE_ENABLED is accepted as an alias there.
//
// Authentication is NOT handled here — the `claude` CLI reads its own
// credentials from ~/.claude/.credentials.json (set up via `claude login` on
// the VPS) or from ANTHROPIC_API_KEY in the process environment as a fallback.
func (m *Manager) applyClaudeEnvOverrides() {
	c := &m.config.Claude

	if val := envGetStr("CLAUDE_BINARY_PATH"); val != "" {
		c.BinaryPath = val
	}
	if val := envGetStr("CLAUDE_WORKDIR"); val != "" {
		c.Workdir = val
	}
	if val := envGetStr("CLAUDE_MODEL"); val != "" {
		c.Model = val
	}
	if val := strings.TrimSpace(envGetStr("CLAUDE_MODE")); val != "" {
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
