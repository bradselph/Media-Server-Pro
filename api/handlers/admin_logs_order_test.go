package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"media-server-pro/internal/config"
	"media-server-pro/internal/logger"
)

// TestGetServerLogs_OrdersNewestFirstAcrossFiles guards the merge order of the
// admin log viewer.
//
// GetServerLogs walks log files newest-first (that ordering is load-bearing: it
// is what makes the `limit` cut-off keep the most recent lines) while
// readLastNLines yields each file's lines oldest-first. Reversing the whole
// accumulated slice once, as the handler used to, flipped both dimensions — so
// lines came out newest-first *within* a file but the older file's block was
// emitted ahead of the newer one (B3,B2,B1,A3,A2,A1). The UI renders the
// response as-is, so operators saw a shuffled log.
//
// The response must be strictly newest-first overall: A3,A2,A1,B3,B2,B1.
func TestGetServerLogs_OrdersNewestFirstAcrossFiles(t *testing.T) {
	gin.SetMode(gin.TestMode)

	logsDir := t.TempDir()
	// Names sort lexicographically, so the later date is the newer file.
	writeLog(t, logsDir, "server_2026-07-12.log", "B1\nB2\nB3\n")
	writeLog(t, logsDir, "server_2026-07-13.log", "A1\nA2\nA3\n")

	h := newLogsTestHandler(t, logsDir)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/admin/logs?limit=200", nil)

	h.GetServerLogs(c)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body: %s)", w.Code, w.Body.String())
	}

	got := decodeLogMessages(t, w.Body.Bytes())
	want := []string{"A3", "A2", "A1", "B3", "B2", "B1"}
	if len(got) != len(want) {
		t.Fatalf("got %d lines %v, want %d %v", len(got), got, len(want), want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("log order = %v, want %v (first mismatch at index %d)", got, want, i)
		}
	}
}

// TestGetServerLogs_LimitKeepsNewestLines guards that the newest-first file walk
// survives: with a limit smaller than the total, the lines that are kept must
// come from the newest file, not the oldest.
func TestGetServerLogs_LimitKeepsNewestLines(t *testing.T) {
	gin.SetMode(gin.TestMode)

	logsDir := t.TempDir()
	writeLog(t, logsDir, "server_2026-07-12.log", "B1\nB2\nB3\n")
	writeLog(t, logsDir, "server_2026-07-13.log", "A1\nA2\nA3\n")

	h := newLogsTestHandler(t, logsDir)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/admin/logs?limit=3", nil)

	h.GetServerLogs(c)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body: %s)", w.Code, w.Body.String())
	}

	got := decodeLogMessages(t, w.Body.Bytes())
	want := []string{"A3", "A2", "A1"}
	if len(got) != len(want) {
		t.Fatalf("got %d lines %v, want %d %v", len(got), got, len(want), want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("limited log = %v, want the newest file's lines %v", got, want)
		}
	}
}

func writeLog(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}

func newLogsTestHandler(t *testing.T, logsDir string) *Handler {
	t.Helper()
	m := config.NewManager(filepath.Join(t.TempDir(), "config.json"))
	if err := m.Load(); err != nil {
		t.Fatalf("load config: %v", err)
	}
	if err := m.SetValuesBatch(map[string]any{
		"directories": map[string]any{"logs": logsDir},
	}); err != nil {
		t.Fatalf("set logs dir: %v", err)
	}
	if got := m.Get().Directories.Logs; got != logsDir {
		t.Fatalf("precondition: Directories.Logs = %q, want %q", got, logsDir)
	}
	return &Handler{config: m, log: logger.New("admin-logs-test")}
}

// decodeLogMessages pulls the raw log text back out of the response so the
// assertions read in terms of the lines that were written.
func decodeLogMessages(t *testing.T, body []byte) []string {
	t.Helper()
	var resp struct {
		Data []map[string]any `json:"data"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("decode response: %v (body: %s)", err, body)
	}
	out := make([]string, 0, len(resp.Data))
	for _, entry := range resp.Data {
		out = append(out, logEntryText(entry))
	}
	return out
}

// logEntryText recovers the original line from a parsed entry. parseLogLine puts
// unstructured text in "message", but fall back to any string field so this test
// keeps working if the parser's shape changes.
func logEntryText(entry map[string]any) string {
	if msg, ok := entry["message"].(string); ok && strings.TrimSpace(msg) != "" {
		return strings.TrimSpace(msg)
	}
	if raw, ok := entry["raw"].(string); ok {
		return strings.TrimSpace(raw)
	}
	return ""
}
