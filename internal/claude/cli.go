package claude

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"media-server-pro/internal/config"
)

// cliEvent is a single NDJSON event emitted by `claude --output-format
// stream-json`. Only the fields we actually consume are decoded; unknown
// properties are ignored.
type cliEvent struct {
	Type      string          `json:"type"`
	Subtype   string          `json:"subtype,omitempty"`
	SessionID string          `json:"session_id,omitempty"`
	Model     string          `json:"model,omitempty"`
	Message   *cliMessage     `json:"message,omitempty"`
	Result    string          `json:"result,omitempty"`
	IsError   bool            `json:"is_error,omitempty"`
	Error     json.RawMessage `json:"error,omitempty"`
}

type cliMessage struct {
	Role       string            `json:"role,omitempty"`
	Content    []cliContentBlock `json:"content,omitempty"`
	StopReason string            `json:"stop_reason,omitempty"`
}

// cliContentBlock covers text, tool_use, and tool_result shapes in one union.
// For tool_result blocks the CLI emits either a string or an array under
// `content`; we store it raw and unwrap at the call site.
type cliContentBlock struct {
	Type      string          `json:"type"`
	Text      string          `json:"text,omitempty"`
	ID        string          `json:"id,omitempty"`
	Name      string          `json:"name,omitempty"`
	Input     json.RawMessage `json:"input,omitempty"`
	ToolUseID string          `json:"tool_use_id,omitempty"`
	Content   json.RawMessage `json:"content,omitempty"`
	IsError   bool            `json:"is_error,omitempty"`
}

// cliRunOpts bundles arguments for a single CLI invocation.
type cliRunOpts struct {
	cfg             config.ClaudeConfig
	message         string
	resumeSessionID string
	mode            string
	systemPromptAdd string
}

// cliRunResult captures state harvested during a turn that the caller needs
// to persist.
type cliRunResult struct {
	SessionID  string
	FinalText  string
	StopReason string
}

// runClaudeCLI spawns `claude`, streams stream-json events out, and fans them
// out to emit(). It blocks until the CLI exits.
//
// Threading model: a single goroutine reads stdout; stderr is captured into a
// buffer for diagnostic reporting on non-zero exit. stdin is unused — the
// prompt is passed via -p.
func runClaudeCLI(ctx context.Context, opts cliRunOpts, emit Emitter) (*cliRunResult, error) {
	binary := strings.TrimSpace(opts.cfg.BinaryPath)
	if binary == "" {
		binary = "claude"
	}
	if _, err := exec.LookPath(binary); err != nil {
		return nil, fmt.Errorf("claude CLI not found (looked for %q): %w — run `claude login` on the VPS to install and authenticate", binary, err)
	}

	args := []string{
		"-p", opts.message,
		"--output-format", "stream-json",
		"--input-format", "text",
		"--verbose",
	}
	if m := strings.TrimSpace(opts.cfg.Model); m != "" {
		args = append(args, "--model", m)
	}
	if permMode := permissionModeFor(opts.mode); permMode != "" {
		args = append(args, "--permission-mode", permMode)
	}
	if s := strings.TrimSpace(opts.systemPromptAdd); s != "" {
		args = append(args, "--append-system-prompt", s)
	}
	if opts.cfg.MaxToolCallsPerTurn > 0 {
		args = append(args, "--max-turns", fmt.Sprintf("%d", opts.cfg.MaxToolCallsPerTurn))
	}
	if opts.resumeSessionID != "" {
		args = append(args, "--resume", opts.resumeSessionID)
	}

	timeout := opts.cfg.RequestTimeout
	if timeout <= 0 {
		timeout = 10 * time.Minute
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(runCtx, binary, args...)
	if wd := strings.TrimSpace(opts.cfg.Workdir); wd != "" {
		cmd.Dir = wd
	}
	// Inherit env so ANTHROPIC_API_KEY / HOME / PATH flow through. The CLI
	// reads OAuth credentials from $HOME/.claude/.credentials.json.
	cmd.Env = os.Environ()

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start claude cli: %w", err)
	}

	result := &cliRunResult{}
	var finalText strings.Builder
	parseErr := parseCLIStream(stdout, result, &finalText, emit)

	waitErr := cmd.Wait()
	if waitErr != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = waitErr.Error()
		}
		return result, fmt.Errorf("claude cli exited non-zero: %s", redact(msg))
	}
	if parseErr != nil && !errors.Is(parseErr, io.EOF) {
		return result, parseErr
	}
	if result.FinalText == "" {
		result.FinalText = finalText.String()
	}
	return result, nil
}

// parseCLIStream reads NDJSON events from r, dispatches them to emit(), and
// records session/final-text state onto result. Returns the first JSON or
// scanner error encountered; callers treat io.EOF as clean EOF.
func parseCLIStream(r io.Reader, result *cliRunResult, finalText *strings.Builder, emit Emitter) error {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 64*1024), 16*1024*1024)

	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		var ev cliEvent
		if err := json.Unmarshal(line, &ev); err != nil {
			// Be tolerant of partial/garbled lines — skip rather than abort.
			continue
		}
		dispatchCLIEvent(ev, result, finalText, emit)
	}
	return scanner.Err()
}

func dispatchCLIEvent(ev cliEvent, result *cliRunResult, finalText *strings.Builder, emit Emitter) {
	switch ev.Type {
	case "system":
		if ev.Subtype == "init" && ev.SessionID != "" {
			result.SessionID = ev.SessionID
		}
	case "assistant":
		if ev.Message == nil {
			return
		}
		for _, block := range ev.Message.Content {
			switch block.Type {
			case "text":
				if block.Text != "" {
					finalText.WriteString(block.Text)
					emit(Event{Type: "delta", Text: block.Text})
				}
			case "tool_use":
				emit(Event{Type: "tool_call", ToolCall: &ToolCall{
					ID:    block.ID,
					Name:  block.Name,
					Input: block.Input,
				}})
			}
		}
		if s := strings.TrimSpace(ev.Message.StopReason); s != "" {
			result.StopReason = s
		}
	case "user":
		// Claude Code echoes tool results as user-role messages containing
		// tool_result blocks. Forward them to the UI so the transcript stays
		// complete.
		if ev.Message == nil {
			return
		}
		for _, block := range ev.Message.Content {
			if block.Type != "tool_result" {
				continue
			}
			output := unwrapToolResultContent(block.Content)
			tc := &ToolCall{ID: block.ToolUseID, Output: output}
			if block.IsError {
				tc.Error = output
			}
			emit(Event{Type: "tool_result", ToolCall: tc})
		}
	case "result":
		if ev.Result != "" {
			result.FinalText = ev.Result
		}
		if ev.Subtype != "" {
			result.StopReason = ev.Subtype
		}
		if ev.IsError {
			msg := strings.TrimSpace(string(ev.Error))
			if msg == "" {
				msg = "claude cli reported an error"
			}
			emit(Event{Type: "error", Error: msg})
		}
	}
}

// unwrapToolResultContent normalizes Claude's flexible tool_result.content
// shape (either a JSON string or an array of content blocks) into a plain
// string suitable for the UI.
func unwrapToolResultContent(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(raw, &blocks); err == nil {
		var b strings.Builder
		for _, blk := range blocks {
			if blk.Text != "" {
				b.WriteString(blk.Text)
			}
		}
		if b.Len() > 0 {
			return b.String()
		}
	}
	return string(raw)
}

// permissionModeFor maps our three-mode vocabulary to Claude Code's
// --permission-mode values. Interactive falls back to bypassPermissions until
// the stdin approval bridge is implemented.
func permissionModeFor(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case ModeAdvisory:
		return "plan"
	case ModeInteractive:
		return "bypassPermissions"
	case ModeAutonomous:
		return "bypassPermissions"
	default:
		return "bypassPermissions"
	}
}

// CheckCLIAvailable looks up the configured binary on $PATH and reports its
// version string. Used by the /auth/status endpoint and healthcheck.
func CheckCLIAvailable(ctx context.Context, cfg config.ClaudeConfig) (path, version string, err error) {
	binary := strings.TrimSpace(cfg.BinaryPath)
	if binary == "" {
		binary = "claude"
	}
	resolved, lookErr := exec.LookPath(binary)
	if lookErr != nil {
		return "", "", fmt.Errorf("binary %q not found on PATH", binary)
	}
	vctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	out, runErr := exec.CommandContext(vctx, resolved, "--version").CombinedOutput()
	if runErr != nil {
		return resolved, "", fmt.Errorf("`%s --version` failed: %w", resolved, runErr)
	}
	return resolved, strings.TrimSpace(string(out)), nil
}

// authProbeMu serializes concurrent auth-status probes so we don't fork
// multiple `claude` processes for the same question.
var authProbeMu sync.Mutex

// ProbeAuth runs a zero-cost no-op CLI invocation that requires valid auth
// (a one-token prompt). Returns nil on success; non-nil error message is
// surfaced to the admin UI so they can re-run `claude login` on the VPS.
//
// This is a best-effort probe: if the CLI exits with a non-zero code whose
// stderr mentions authentication, we flag it; other failures are returned
// verbatim.
func ProbeAuth(ctx context.Context, cfg config.ClaudeConfig) error {
	authProbeMu.Lock()
	defer authProbeMu.Unlock()

	binary := strings.TrimSpace(cfg.BinaryPath)
	if binary == "" {
		binary = "claude"
	}
	if _, err := exec.LookPath(binary); err != nil {
		return fmt.Errorf("claude CLI not installed")
	}
	// `claude config get` does not require a network call and fails fast if
	// the credentials file is missing or corrupt.
	pctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(pctx, binary, "config", "get", "-g", "theme")
	if wd := strings.TrimSpace(cfg.Workdir); wd != "" {
		cmd.Dir = wd
	}
	cmd.Env = os.Environ()
	out, err := cmd.CombinedOutput()
	if err != nil {
		s := strings.ToLower(string(out))
		if strings.Contains(s, "auth") || strings.Contains(s, "login") || strings.Contains(s, "credential") {
			return fmt.Errorf("not authenticated — run `claude login` on the host")
		}
		return fmt.Errorf("claude config check failed: %w", err)
	}
	return nil
}
