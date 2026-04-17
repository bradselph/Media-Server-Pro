package claude

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"media-server-pro/internal/config"
)

// readLogsTool is the non-mutating tool that tails server log files.
type readLogsTool struct{ cfg *config.Manager }

// NewReadLogsTool constructs the tool. The config manager is kept so the tool
// resolves the current logs directory at call-time.
func NewReadLogsTool(cfg *config.Manager) Tool { return &readLogsTool{cfg: cfg} }

func (t *readLogsTool) Name() string { return "read_logs" }

func (t *readLogsTool) Description() string {
	return "Read the last N lines from the server's application log files, optionally filtered by level or module. Returns newest lines last."
}

func (t *readLogsTool) InputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"limit":  map[string]any{"type": "integer", "minimum": 1, "maximum": 2000, "default": 200},
			"level":  map[string]any{"type": "string", "enum": []string{"debug", "info", "warn", "error", "fatal"}},
			"module": map[string]any{"type": "string", "description": "Filter by module name as written in the log prefix (e.g. 'hls', 'scanner')."},
		},
		"additionalProperties": false,
	}
}

func (t *readLogsTool) IsWrite() bool { return false }

func (t *readLogsTool) Execute(_ context.Context, input json.RawMessage, _ *RunContext) (string, error) {
	var p struct {
		Limit  int    `json:"limit"`
		Level  string `json:"level"`
		Module string `json:"module"`
	}
	if len(input) > 0 {
		if err := json.Unmarshal(input, &p); err != nil {
			return "", err
		}
	}
	if p.Limit <= 0 || p.Limit > 2000 {
		p.Limit = 200
	}

	dir := t.cfg.Get().Directories.Logs
	if dir == "" {
		dir = "logs"
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return "[no log directory]", nil
		}
		return "", err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() > entries[j].Name() })

	var collected []string
	for _, e := range entries {
		if e.IsDir() || e.Type()&os.ModeSymlink != 0 || !strings.HasSuffix(e.Name(), ".log") {
			continue
		}
		path := filepath.Join(dir, e.Name())
		lines, readErr := tailLines(path, p.Limit-len(collected))
		if readErr != nil {
			continue
		}
		for _, ln := range lines {
			if p.Level != "" && !strings.Contains(strings.ToLower(ln), "["+strings.ToLower(p.Level)+"]") {
				continue
			}
			if p.Module != "" && !strings.Contains(strings.ToLower(ln), "["+strings.ToLower(p.Module)+"]") {
				continue
			}
			collected = append(collected, redact(ln))
			if len(collected) >= p.Limit {
				break
			}
		}
		if len(collected) >= p.Limit {
			break
		}
	}
	if len(collected) == 0 {
		return "[no matching log lines]", nil
	}
	return strings.Join(collected, "\n"), nil
}

// tailLines reads the last n lines from path using a ring buffer.
func tailLines(path string, n int) ([]string, error) {
	if n <= 0 {
		return nil, nil
	}
	f, err := os.Open(path) //nolint:gosec // path comes from dir listing of configured logs dir
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	ring := make([]string, n)
	idx := 0
	total := 0
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		ring[idx%n] = sc.Text()
		idx++
		total++
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	count := total
	if count > n {
		count = n
	}
	out := make([]string, 0, count)
	for i := idx - count; i < idx; i++ {
		out = append(out, ring[i%n])
	}
	return out, nil
}

// readConfigTool returns the running config as a redacted map.
type readConfigTool struct {
	cfg         *config.Manager
	getInfoJSON func() (string, error)
}

// NewReadConfigTool constructs the tool.
func NewReadConfigTool(cfg *config.Manager) Tool { return &readConfigTool{cfg: cfg} }

func (t *readConfigTool) Name() string { return "read_config" }

func (t *readConfigTool) Description() string {
	return "Return the running server configuration as JSON. Secret fields (API keys, passwords, tokens) are redacted before return."
}

func (t *readConfigTool) InputSchema() map[string]any {
	return map[string]any{
		"type":                 "object",
		"properties":           map[string]any{},
		"additionalProperties": false,
	}
}

func (t *readConfigTool) IsWrite() bool { return false }

func (t *readConfigTool) Execute(_ context.Context, _ json.RawMessage, _ *RunContext) (string, error) {
	// Marshal + unmarshal through JSON to get a map[string]any we can walk.
	raw, err := json.Marshal(t.cfg.Get())
	if err != nil {
		return "", err
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return "", err
	}
	redacted := redactMap(m)
	out, err := json.MarshalIndent(redacted, "", "  ")
	if err != nil {
		return "", err
	}
	return string(out), nil
}

// systemInfoTool returns process + OS runtime details.
type systemInfoTool struct{}

// NewSystemInfoTool constructs the tool.
func NewSystemInfoTool() Tool { return &systemInfoTool{} }

func (t *systemInfoTool) Name() string { return "system_info" }

func (t *systemInfoTool) Description() string {
	return "Return process, Go runtime, and host OS information (goroutines, memory, CPU count, load averages where available)."
}

func (t *systemInfoTool) InputSchema() map[string]any {
	return map[string]any{
		"type":                 "object",
		"properties":           map[string]any{},
		"additionalProperties": false,
	}
}

func (t *systemInfoTool) IsWrite() bool { return false }

func (t *systemInfoTool) Execute(_ context.Context, _ json.RawMessage, _ *RunContext) (string, error) {
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)
	host, _ := os.Hostname()
	info := map[string]any{
		"hostname":      host,
		"os":            runtime.GOOS,
		"arch":          runtime.GOARCH,
		"go_version":    runtime.Version(),
		"num_cpu":       runtime.NumCPU(),
		"num_goroutine": runtime.NumGoroutine(),
		"mem_alloc":     mem.Alloc,
		"mem_sys":       mem.Sys,
		"gc_cycles":     mem.NumGC,
		"pid":           os.Getpid(),
	}
	if data, err := os.ReadFile("/proc/loadavg"); err == nil {
		info["loadavg"] = strings.TrimSpace(string(data))
	}
	if data, err := os.ReadFile("/proc/meminfo"); err == nil {
		info["meminfo_excerpt"] = firstNLines(string(data), 6)
	}
	b, _ := json.MarshalIndent(info, "", "  ")
	return string(b), nil
}

func firstNLines(s string, n int) string {
	lines := strings.SplitN(s, "\n", n+1)
	if len(lines) > n {
		lines = lines[:n]
	}
	return strings.Join(lines, "\n")
}

// processListTool enumerates processes on Linux hosts via /proc. Returns JSON.
type processListTool struct{}

// NewProcessListTool constructs the tool.
func NewProcessListTool() Tool { return &processListTool{} }

func (t *processListTool) Name() string { return "list_processes" }

func (t *processListTool) Description() string {
	return "List running processes with PID, command, and RSS (Linux only). Returns up to 'limit' rows sorted by RSS desc."
}

func (t *processListTool) InputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"limit": map[string]any{"type": "integer", "minimum": 1, "maximum": 200, "default": 40},
		},
		"additionalProperties": false,
	}
}

func (t *processListTool) IsWrite() bool { return false }

func (t *processListTool) Execute(_ context.Context, input json.RawMessage, _ *RunContext) (string, error) {
	if runtime.GOOS != "linux" {
		return "", fmt.Errorf("list_processes is only supported on Linux")
	}
	var p struct {
		Limit int `json:"limit"`
	}
	if len(input) > 0 {
		_ = json.Unmarshal(input, &p)
	}
	if p.Limit <= 0 || p.Limit > 200 {
		p.Limit = 40
	}
	procs, err := listLinuxProcesses()
	if err != nil {
		return "", err
	}
	sort.Slice(procs, func(i, j int) bool { return procs[i].RSSKB > procs[j].RSSKB })
	if len(procs) > p.Limit {
		procs = procs[:p.Limit]
	}
	b, _ := json.MarshalIndent(procs, "", "  ")
	return string(b), nil
}

type procInfo struct {
	PID   int    `json:"pid"`
	Comm  string `json:"comm"`
	RSSKB int64  `json:"rss_kb"`
	Cmd   string `json:"cmd"`
}

func listLinuxProcesses() ([]procInfo, error) {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil, err
	}
	out := make([]procInfo, 0, 200)
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		pid := 0
		if _, err := fmt.Sscanf(e.Name(), "%d", &pid); err != nil || pid == 0 {
			continue
		}
		statm, err := os.ReadFile(filepath.Join("/proc", e.Name(), "statm"))
		if err != nil {
			continue
		}
		var pages int64
		fields := strings.Fields(string(statm))
		if len(fields) >= 2 {
			_, _ = fmt.Sscanf(fields[1], "%d", &pages)
		}
		comm, _ := os.ReadFile(filepath.Join("/proc", e.Name(), "comm"))
		cmdline, _ := os.ReadFile(filepath.Join("/proc", e.Name(), "cmdline"))
		cmd := strings.ReplaceAll(strings.TrimSpace(string(cmdline)), "\x00", " ")
		out = append(out, procInfo{
			PID:   pid,
			Comm:  strings.TrimSpace(string(comm)),
			RSSKB: pages * 4, // page size approximated at 4K
			Cmd:   redact(cmd),
		})
	}
	return out, nil
}

// readFileTool reads files inside AllowedPaths.
type readFileTool struct{}

// NewReadFileTool constructs the tool.
func NewReadFileTool() Tool { return &readFileTool{} }

func (t *readFileTool) Name() string { return "read_file" }

func (t *readFileTool) Description() string {
	return "Read a UTF-8 text file. Path must be within configured AllowedPaths. Returns up to 'max_bytes' (default 64KB)."
}

func (t *readFileTool) InputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path":      map[string]any{"type": "string"},
			"max_bytes": map[string]any{"type": "integer", "minimum": 1, "maximum": 1048576, "default": 65536},
		},
		"required":             []string{"path"},
		"additionalProperties": false,
	}
}

func (t *readFileTool) IsWrite() bool { return false }

func (t *readFileTool) Execute(_ context.Context, input json.RawMessage, rc *RunContext) (string, error) {
	var p struct {
		Path     string `json:"path"`
		MaxBytes int    `json:"max_bytes"`
	}
	if err := json.Unmarshal(input, &p); err != nil {
		return "", err
	}
	if err := validateAllowedPath(p.Path, rc.Cfg.AllowedPaths); err != nil {
		return "", err
	}
	if p.MaxBytes <= 0 || p.MaxBytes > 1<<20 {
		p.MaxBytes = 64 * 1024
	}
	if info, err := os.Lstat(p.Path); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return "", errors.New("refusing to read through a symlink")
	}
	f, err := os.Open(p.Path) //nolint:gosec // path validated against allowlist
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()
	buf := make([]byte, p.MaxBytes)
	n, err := f.Read(buf)
	if err != nil && err.Error() != "EOF" {
		return "", err
	}
	return redact(string(buf[:n])), nil
}
