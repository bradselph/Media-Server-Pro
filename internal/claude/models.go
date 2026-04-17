package claude

import (
	"encoding/json"
	"time"
)

// Conversation represents a persisted Claude chat session owned by an admin.
type Conversation struct {
	ID        string    `json:"id" gorm:"primaryKey;size:64"`
	UserID    string    `json:"user_id" gorm:"size:255;index;column:user_id"`
	Username  string    `json:"username" gorm:"size:255"`
	Title     string    `json:"title" gorm:"size:255"`
	Mode      string    `json:"mode" gorm:"size:32"`
	Model     string    `json:"model" gorm:"size:128"`
	CreatedAt time.Time `json:"created_at" gorm:"autoCreateTime"`
	UpdatedAt time.Time `json:"updated_at" gorm:"autoUpdateTime"`
}

// TableName maps Conversation to the claude_conversations table.
func (Conversation) TableName() string { return "claude_conversations" }

// Message represents a single turn in a Conversation. Role is "user",
// "assistant", or "tool". ToolCalls and ToolResult hold structured data when
// the message represents tool activity.
type Message struct {
	ID             string          `json:"id" gorm:"primaryKey;size:64"`
	ConversationID string          `json:"conversation_id" gorm:"size:64;index;column:conversation_id"`
	// Seq is a per-conversation monotonic counter used for stable ordering.
	// created_at has second-level granularity in MySQL which is not fine enough
	// when multiple messages land in the same second (e.g. assistant + tool
	// results). Never rely on created_at for ordering.
	Seq            int64           `json:"seq" gorm:"index"`
	Role           string          `json:"role" gorm:"size:32"`
	Content        string          `json:"content" gorm:"type:mediumtext"`
	ToolCalls      json.RawMessage `json:"tool_calls,omitempty" gorm:"type:json"`
	ToolResult     json.RawMessage `json:"tool_result,omitempty" gorm:"type:json"`
	CreatedAt      time.Time       `json:"created_at" gorm:"autoCreateTime"`
}

// TableName maps Message to the claude_messages table.
func (Message) TableName() string { return "claude_messages" }

// ToolCall describes a single tool invocation emitted by Claude and executed
// locally.
type ToolCall struct {
	ID     string          `json:"id"`
	Name   string          `json:"name"`
	Input  json.RawMessage `json:"input"`
	Output string          `json:"output,omitempty"`
	Error  string          `json:"error,omitempty"`
	// RequiresConfirm indicates the server refused to run this call without an
	// admin-side approval (interactive mode or write-gated tool).
	RequiresConfirm bool `json:"requires_confirm,omitempty"`
}

// ChatRequest is the input body for the streaming chat endpoint.
type ChatRequest struct {
	ConversationID string `json:"conversation_id"`
	Message        string `json:"message"`
	// ModeOverride lets the admin temporarily run a turn in a different mode.
	// Accepts "advisory", "interactive", "autonomous".
	ModeOverride string `json:"mode_override,omitempty"`
	// ApprovedToolCalls lists previously-returned tool call IDs the admin has
	// approved for execution this turn. Pass to resume after a confirm gate.
	ApprovedToolCalls []string `json:"approved_tool_calls,omitempty"`
}

// PublicConfig is the client-safe view of ClaudeConfig returned to the admin UI.
// Raw credentials are never serialized; only the *Set booleans are returned.
type PublicConfig struct {
	Enabled                 bool     `json:"enabled"`
	APIKeySet               bool     `json:"api_key_set"`
	WebLoginTokenSet        bool     `json:"web_login_token_set"`
	Model                   string   `json:"model"`
	Mode                    string   `json:"mode"`
	MaxTokens               int      `json:"max_tokens"`
	SystemPrompt            string   `json:"system_prompt"`
	AllowedTools            []string `json:"allowed_tools"`
	AllowedShellCommands    []string `json:"allowed_shell_commands"`
	AllowedPaths            []string `json:"allowed_paths"`
	AllowedServices         []string `json:"allowed_services"`
	RequireConfirmForWrites bool     `json:"require_confirm_for_writes"`
	MaxToolCallsPerTurn     int      `json:"max_tool_calls_per_turn"`
	RateLimitPerMinute      int      `json:"rate_limit_per_minute"`
	KillSwitch              bool     `json:"kill_switch"`
	HistoryRetentionDays    int      `json:"history_retention_days"`
	AvailableTools          []string `json:"available_tools"`
}
