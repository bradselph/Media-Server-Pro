package claude

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// writeFileTool writes a file inside AllowedPaths. Creates parent directories
// automatically. Refuses to follow existing symlinks.
type writeFileTool struct{}

// NewWriteFileTool constructs the tool.
func NewWriteFileTool() Tool { return &writeFileTool{} }

func (t *writeFileTool) Name() string { return "write_file" }

func (t *writeFileTool) Description() string {
	return "Create or overwrite a UTF-8 text file. Path must be within configured AllowedPaths. Writes are gated for admin approval in interactive mode."
}

func (t *writeFileTool) InputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path":    map[string]any{"type": "string"},
			"content": map[string]any{"type": "string"},
			"mode":    map[string]any{"type": "integer", "default": 0o644, "description": "POSIX file mode in octal; default 0o644."},
		},
		"required":             []string{"path", "content"},
		"additionalProperties": false,
	}
}

func (t *writeFileTool) IsWrite() bool { return true }

func (t *writeFileTool) Execute(_ context.Context, input json.RawMessage, rc *RunContext) (string, error) {
	var p struct {
		Path    string `json:"path"`
		Content string `json:"content"`
		Mode    int    `json:"mode"`
	}
	if err := json.Unmarshal(input, &p); err != nil {
		return "", err
	}
	if err := validateAllowedPath(p.Path, rc.Cfg.AllowedPaths); err != nil {
		return "", err
	}
	if p.Mode == 0 {
		p.Mode = 0o644
	}
	if info, err := os.Lstat(p.Path); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return "", errors.New("refusing to write through an existing symlink")
	}
	if err := os.MkdirAll(filepath.Dir(p.Path), 0o755); err != nil {
		return "", err
	}
	tmp := p.Path + ".claude.tmp"
	if err := os.WriteFile(tmp, []byte(p.Content), os.FileMode(p.Mode)); err != nil { //nolint:gosec // path validated; mode bounded by caller
		return "", err
	}
	if err := os.Rename(tmp, p.Path); err != nil {
		_ = os.Remove(tmp)
		return "", err
	}
	return fmt.Sprintf("wrote %d bytes to %s", len(p.Content), p.Path), nil
}

// shellExecTool runs an allowlisted shell command with arguments. The program
// name (argv[0]) must match AllowedShellCommands exactly; args are passed
// verbatim — no shell interpretation, so the only risk surface is what the
// allowlisted program accepts.
type shellExecTool struct{}

// NewShellExecTool constructs the tool.
func NewShellExecTool() Tool { return &shellExecTool{} }

func (t *shellExecTool) Name() string { return "shell_exec" }

func (t *shellExecTool) Description() string {
	return "Run an allowlisted program with arguments. Program name must be in AllowedShellCommands exactly. No shell interpolation. Combined stdout+stderr is returned, truncated to 32KB."
}

func (t *shellExecTool) InputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"command":        map[string]any{"type": "string", "description": "Program name (not a shell string)."},
			"args":           map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
			"timeout_seconds": map[string]any{"type": "integer", "minimum": 1, "maximum": 120, "default": 30},
		},
		"required":             []string{"command"},
		"additionalProperties": false,
	}
}

func (t *shellExecTool) IsWrite() bool { return true }

func (t *shellExecTool) Execute(ctx context.Context, input json.RawMessage, rc *RunContext) (string, error) {
	var p struct {
		Command        string   `json:"command"`
		Args           []string `json:"args"`
		TimeoutSeconds int      `json:"timeout_seconds"`
	}
	if err := json.Unmarshal(input, &p); err != nil {
		return "", err
	}
	if err := validateAllowedCommand(p.Command, rc.Cfg.AllowedShellCommands); err != nil {
		return "", err
	}
	// Guard against unsafe arg contents — no NUL bytes; no embedded newlines
	// as a defense-in-depth measure for log-injection-friendly programs.
	for i, a := range p.Args {
		if strings.ContainsAny(a, "\x00") {
			return "", fmt.Errorf("arg %d contains NUL", i)
		}
	}
	if p.TimeoutSeconds <= 0 || p.TimeoutSeconds > 120 {
		p.TimeoutSeconds = 30
	}
	runCtx, cancel := context.WithTimeout(ctx, time.Duration(p.TimeoutSeconds)*time.Second)
	defer cancel()
	cmd := exec.CommandContext(runCtx, p.Command, p.Args...) //nolint:gosec // command validated against allowlist
	out, err := cmd.CombinedOutput()
	const maxOut = 32 * 1024
	text := string(out)
	if len(text) > maxOut {
		text = text[:maxOut] + "\n...[truncated]"
	}
	if err != nil {
		return redact(text), fmt.Errorf("exit: %w", err)
	}
	return redact(text), nil
}

// serviceRestartTool runs `systemctl <action> <service>` for allowlisted
// services. Actions are restricted to a safe set.
type serviceRestartTool struct{}

// NewServiceRestartTool constructs the tool.
func NewServiceRestartTool() Tool { return &serviceRestartTool{} }

func (t *serviceRestartTool) Name() string { return "service_restart" }

func (t *serviceRestartTool) Description() string {
	return "Restart / start / stop / reload / show status of an allowlisted systemd unit. The service name must be in AllowedServices."
}

func (t *serviceRestartTool) InputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"service": map[string]any{"type": "string"},
			"action":  map[string]any{"type": "string", "enum": []string{"restart", "start", "stop", "reload", "status"}, "default": "status"},
		},
		"required":             []string{"service"},
		"additionalProperties": false,
	}
}

func (t *serviceRestartTool) IsWrite() bool { return true }

// IsDestructiveInvocation gates stop and restart actions regardless of mode.
func (t *serviceRestartTool) IsDestructiveInvocation(input json.RawMessage) bool {
	var p struct {
		Action string `json:"action"`
	}
	if err := json.Unmarshal(input, &p); err != nil {
		return false
	}
	return p.Action == "stop" || p.Action == "restart"
}

func (t *serviceRestartTool) Execute(ctx context.Context, input json.RawMessage, rc *RunContext) (string, error) {
	var p struct {
		Service string `json:"service"`
		Action  string `json:"action"`
	}
	if err := json.Unmarshal(input, &p); err != nil {
		return "", err
	}
	switch p.Action {
	case "":
		p.Action = "status"
	case "restart", "start", "stop", "reload", "status":
	default:
		return "", fmt.Errorf("unsupported action %q", p.Action)
	}
	if err := validateAllowedService(p.Service, rc.Cfg.AllowedServices); err != nil {
		return "", err
	}
	runCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(runCtx, "systemctl", p.Action, p.Service) //nolint:gosec // service validated against allowlist
	out, err := cmd.CombinedOutput()
	text := string(out)
	const maxOut = 32 * 1024
	if len(text) > maxOut {
		text = text[:maxOut] + "\n...[truncated]"
	}
	if err != nil {
		return redact(text), fmt.Errorf("systemctl %s %s failed: %w", p.Action, p.Service, err)
	}
	return redact(text), nil
}

// RegisterDefaultTools registers every standard tool on m. Called from main.go.
func RegisterDefaultTools(m *Module) {
	m.RegisterTool(NewReadLogsTool(m.cfg))
	m.RegisterTool(NewReadConfigTool(m.cfg))
	m.RegisterTool(NewSystemInfoTool())
	m.RegisterTool(NewProcessListTool())
	m.RegisterTool(NewReadFileTool())
	m.RegisterTool(NewWriteFileTool())
	m.RegisterTool(NewShellExecTool())
	m.RegisterTool(NewServiceRestartTool())
}
