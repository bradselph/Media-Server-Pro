package logger

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const (
	testModule  = "test-module"
	testCtxRID  = "ctx-rid"
	testAllGood = "all good"
)

// ---------------------------------------------------------------------------
// Level constants
// ---------------------------------------------------------------------------

func TestLevelOrdering(t *testing.T) {
	if DEBUG >= INFO || INFO >= WARN || WARN >= ERROR || ERROR >= FATAL {
		t.Error("level ordering must be DEBUG < INFO < WARN < ERROR < FATAL")
	}
}

func TestLevelNamesComplete(t *testing.T) {
	for _, lvl := range []Level{DEBUG, INFO, WARN, ERROR, FATAL} {
		if _, ok := levelNames[lvl]; !ok {
			t.Errorf("levelNames missing entry for level %d", lvl)
		}
	}
}

func TestLevelColorsComplete(t *testing.T) {
	for _, lvl := range []Level{DEBUG, INFO, WARN, ERROR, FATAL} {
		if _, ok := levelColors[lvl]; !ok {
			t.Errorf("levelColors missing entry for level %d", lvl)
		}
	}
}

// ---------------------------------------------------------------------------
// Context request ID
// ---------------------------------------------------------------------------

func TestContextWithRequestID_RoundTrip(t *testing.T) {
	ctx := context.Background()
	rid := "req-12345"
	ctx = ContextWithRequestID(ctx, rid)
	got := RequestIDFromContext(ctx)
	if got != rid {
		t.Errorf("RequestIDFromContext = %q, want %q", got, rid)
	}
}

func TestRequestIDFromContext_Empty(t *testing.T) {
	ctx := context.Background()
	got := RequestIDFromContext(ctx)
	if got != "" {
		t.Errorf("RequestIDFromContext on empty context = %q, want empty", got)
	}
}

// ---------------------------------------------------------------------------
// DefaultConfig
// ---------------------------------------------------------------------------

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.MinLevel != INFO {
		t.Errorf("DefaultConfig.MinLevel = %d, want INFO (%d)", cfg.MinLevel, INFO)
	}
	if cfg.ModuleName != "main" {
		t.Errorf("DefaultConfig.ModuleName = %q, want %q", cfg.ModuleName, "main")
	}
	if cfg.MaxSize <= 0 {
		t.Error("DefaultConfig.MaxSize should be > 0")
	}
	if cfg.MaxBackups <= 0 {
		t.Error("DefaultConfig.MaxBackups should be > 0")
	}
}

// ---------------------------------------------------------------------------
// Logger creation and basic operations
// ---------------------------------------------------------------------------

func newTestLogger(module string, minLevel Level) (*Logger, *bytes.Buffer) {
	buf := &bytes.Buffer{}
	l := &Logger{
		module:   module,
		minLevel: minLevel,
		output:   buf,
	}
	return l, buf
}

func TestLoggerModule(t *testing.T) {
	l, _ := newTestLogger(testModule, DEBUG)
	if l.module != testModule {
		t.Errorf("module = %q, want %q", l.module, testModule)
	}
}

func TestSetLevel(t *testing.T) {
	l, _ := newTestLogger("test", DEBUG)
	l.SetLevel(ERROR)
	if l.minLevel != ERROR {
		t.Errorf("minLevel = %d, want ERROR (%d)", l.minLevel, ERROR)
	}
}

// ---------------------------------------------------------------------------
// Level filtering
// ---------------------------------------------------------------------------

func TestLevelFiltering_DebugLevel(t *testing.T) {
	l, buf := newTestLogger("test", DEBUG)
	l.log(DEBUG, "debug message")
	if buf.Len() == 0 {
		t.Error("DEBUG message should be logged when minLevel=DEBUG")
	}
}

func TestLevelFiltering_AboveMinLevel(t *testing.T) {
	l, buf := newTestLogger("test", WARN)
	l.log(DEBUG, "debug message")
	if buf.Len() != 0 {
		t.Error("DEBUG message should NOT be logged when minLevel=WARN")
	}
	l.log(INFO, "info message")
	if buf.Len() != 0 {
		t.Error("INFO message should NOT be logged when minLevel=WARN")
	}
}

func TestLevelFiltering_AtMinLevel(t *testing.T) {
	l, buf := newTestLogger("test", WARN)
	l.log(WARN, "warn message")
	if buf.Len() == 0 {
		t.Error("WARN message should be logged when minLevel=WARN")
	}
}

func TestLevelFiltering_ErrorAlwaysLogged(t *testing.T) {
	l, buf := newTestLogger("test", ERROR)
	l.log(ERROR, "error message")
	if buf.Len() == 0 {
		t.Error("ERROR message should be logged when minLevel=ERROR")
	}
}

// ---------------------------------------------------------------------------
// Format functions
// ---------------------------------------------------------------------------

func TestFormatMessage_WithoutRequestID(t *testing.T) {
	l, _ := newTestLogger("mymod", DEBUG)
	l.useColors = false
	msg := l.formatMessage(INFO, "", "hello %s", 0, "world")
	if !strings.Contains(msg, "[INFO]") {
		t.Errorf("message should contain [INFO]: %s", msg)
	}
	if !strings.Contains(msg, "[mymod]") {
		t.Errorf("message should contain [mymod]: %s", msg)
	}
	if !strings.Contains(msg, "hello world") {
		t.Errorf("message should contain formatted text: %s", msg)
	}
}

func TestFormatMessage_WithRequestID(t *testing.T) {
	l, _ := newTestLogger("mymod", DEBUG)
	l.useColors = false
	msg := l.formatMessage(INFO, "req-123", "hello", 0)
	if !strings.Contains(msg, "[req-123]") {
		t.Errorf("message should contain request ID: %s", msg)
	}
}

func TestFormatMessagePlain_NoColors(t *testing.T) {
	l, _ := newTestLogger("plain", DEBUG)
	msg := l.formatMessagePlain(ERROR, "", "test error", 0)
	if strings.Contains(msg, "\033[") {
		t.Error("plain message should not contain ANSI codes")
	}
	if !strings.Contains(msg, "ERROR") {
		t.Errorf("plain message should contain level name: %s", msg)
	}
}

func TestFormatMessageJSON_ValidJSON(t *testing.T) {
	l, _ := newTestLogger("jsonmod", DEBUG)
	msg := l.formatMessageJSON(INFO, "rid-1", "test message", 0)
	var parsed map[string]any
	if err := json.Unmarshal([]byte(msg), &parsed); err != nil {
		t.Fatalf("JSON log should be valid JSON: %v\nraw: %s", err, msg)
	}
	if parsed["level"] != "INFO" {
		t.Errorf("level = %v, want INFO", parsed["level"])
	}
	if parsed["module"] != "jsonmod" {
		t.Errorf("module = %v, want jsonmod", parsed["module"])
	}
	if parsed["msg"] != "test message" {
		t.Errorf("msg = %v, want 'test message'", parsed["msg"])
	}
	if parsed["request_id"] != "rid-1" {
		t.Errorf("request_id = %v, want rid-1", parsed["request_id"])
	}
}

func TestFormatMessageJSON_NoRequestID(t *testing.T) {
	l, _ := newTestLogger("jsonmod", DEBUG)
	msg := l.formatMessageJSON(DEBUG, "", "no rid", 0)
	var parsed map[string]any
	if err := json.Unmarshal([]byte(msg), &parsed); err != nil {
		t.Fatalf("JSON log should be valid JSON: %v", err)
	}
	if _, ok := parsed["request_id"]; ok {
		t.Error("request_id should be omitted when empty")
	}
}

func TestFormatMessageJSON_WithArgs(t *testing.T) {
	l, _ := newTestLogger("test", DEBUG)
	msg := l.formatMessageJSON(INFO, "", "count=%d name=%s", 0, 42, "foo")
	var parsed map[string]any
	if err := json.Unmarshal([]byte(msg), &parsed); err != nil {
		t.Fatalf("JSON should parse: %v", err)
	}
	if !strings.Contains(parsed["msg"].(string), "count=42") {
		t.Errorf("msg should contain formatted args: %v", parsed["msg"])
	}
}

// ---------------------------------------------------------------------------
// Logger methods (Debug, Info, Warn, Error)
// ---------------------------------------------------------------------------

func TestLoggerDebug(t *testing.T) {
	l, buf := newTestLogger("test", DEBUG)
	l.Debug("debug %d", 1)
	if !strings.Contains(buf.String(), "debug 1") {
		t.Errorf("Debug output missing: %s", buf.String())
	}
}

func TestLoggerInfo(t *testing.T) {
	l, buf := newTestLogger("test", DEBUG)
	l.Info("info %s", "msg")
	if !strings.Contains(buf.String(), "info msg") {
		t.Errorf("Info output missing: %s", buf.String())
	}
}

func TestLoggerWarn(t *testing.T) {
	l, buf := newTestLogger("test", DEBUG)
	l.Warn("warn %v", true)
	if !strings.Contains(buf.String(), "warn true") {
		t.Errorf("Warn output missing: %s", buf.String())
	}
}

func TestLoggerError(t *testing.T) {
	l, buf := newTestLogger("test", DEBUG)
	l.Error("error happened")
	if !strings.Contains(buf.String(), "error happened") {
		t.Errorf("Error output missing: %s", buf.String())
	}
}

// ---------------------------------------------------------------------------
// Context-aware logging
// ---------------------------------------------------------------------------

func TestLoggerDebugCtx(t *testing.T) {
	l, buf := newTestLogger("test", DEBUG)
	ctx := ContextWithRequestID(context.Background(), testCtxRID)
	l.DebugCtx(ctx, "ctx debug")
	if !strings.Contains(buf.String(), "ctx debug") {
		t.Errorf("DebugCtx output missing: %s", buf.String())
	}
}

func TestLoggerInfoCtx(t *testing.T) {
	l, buf := newTestLogger("test", DEBUG)
	ctx := ContextWithRequestID(context.Background(), testCtxRID)
	l.InfoCtx(ctx, "ctx info")
	if !strings.Contains(buf.String(), "ctx info") {
		t.Errorf("InfoCtx output missing: %s", buf.String())
	}
}

func TestLoggerWarnCtx(t *testing.T) {
	l, buf := newTestLogger("test", DEBUG)
	ctx := ContextWithRequestID(context.Background(), testCtxRID)
	l.WarnCtx(ctx, "ctx warn")
	if !strings.Contains(buf.String(), "ctx warn") {
		t.Errorf("WarnCtx output missing: %s", buf.String())
	}
}

func TestLoggerErrorCtx(t *testing.T) {
	l, buf := newTestLogger("test", DEBUG)
	ctx := ContextWithRequestID(context.Background(), testCtxRID)
	l.ErrorCtx(ctx, "ctx error")
	if !strings.Contains(buf.String(), "ctx error") {
		t.Errorf("ErrorCtx output missing: %s", buf.String())
	}
}

// ---------------------------------------------------------------------------
// HealthReporter
// ---------------------------------------------------------------------------

func TestNewHealthReporter(t *testing.T) {
	hr := NewHealthReporter()
	if hr == nil {
		t.Fatal("NewHealthReporter returned nil")
	}
	if hr.statuses == nil {
		t.Error("statuses map should be initialized")
	}
}

func TestHealthReporter_ReportHealthy(t *testing.T) {
	hr := NewHealthReporter()
	hr.Report("testmod", true, nil, testAllGood)
	hr.mu.RLock()
	defer hr.mu.RUnlock()
	s, ok := hr.statuses["testmod"]
	if !ok {
		t.Fatal("status for testmod should exist")
	}
	if !s.Healthy {
		t.Error("should be healthy")
	}
	if s.Message != testAllGood {
		t.Errorf("message = %q, want %q", s.Message, testAllGood)
	}
}

func TestHealthReporter_ReportUnhealthy(t *testing.T) {
	hr := NewHealthReporter()
	testErr := os.ErrNotExist
	hr.Report("failmod", false, testErr, "disk missing")
	hr.mu.RLock()
	defer hr.mu.RUnlock()
	s := hr.statuses["failmod"]
	if s.Healthy {
		t.Error("should be unhealthy")
	}
	if !errors.Is(s.LastError, testErr) {
		t.Errorf("LastError = %v, want %v", s.LastError, testErr)
	}
}

func TestHealthReporter_ReportOverwrite(t *testing.T) {
	hr := NewHealthReporter()
	hr.Report("mod", false, nil, "bad")
	hr.Report("mod", true, nil, "good")
	hr.mu.RLock()
	defer hr.mu.RUnlock()
	if !hr.statuses["mod"].Healthy {
		t.Error("second report should overwrite first")
	}
}

// ---------------------------------------------------------------------------
// File rotation helpers
// ---------------------------------------------------------------------------

func TestCleanOldBackups(t *testing.T) {
	dir := t.TempDir()
	basePath := filepath.Join(dir, "server.log")

	// Create 5 backup files
	for i := 1; i <= 5; i++ {
		path := basePath + "." + string(rune('0'+i))
		if err := os.WriteFile(path, []byte("log"), 0o600); err != nil {
			t.Fatalf("failed to create test backup: %v", err)
		}
	}

	l := &Logger{maxBackups: 2}
	l.cleanOldBackups(basePath)

	matches, _ := filepath.Glob(basePath + ".*")
	if len(matches) > 2 {
		t.Errorf("expected <= 2 backup files, got %d", len(matches))
	}
}

func TestRotateIfNeeded_NoRotationBelowMax(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "test.log")
	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY, 0o600) //nolint:gosec // test file, restrictive permissions fine
	if err != nil {
		t.Fatal(err)
	}
	// Write small content
	f.WriteString("small")

	l := &Logger{
		maxSize:    1024 * 1024, // 1MB
		fileOutput: f,
		logDir:     dir,
	}
	l.rotateIfNeeded()

	// File should still be open and same file
	if l.fileOutput == nil {
		t.Error("fileOutput should not be nil after no rotation")
	}
	l.fileOutput.Close()
}

func TestRotateIfNeeded_NoRotationWhenMaxSizeZero(_ *testing.T) {
	l := &Logger{maxSize: 0}
	l.rotateIfNeeded() // should not panic
}

func TestRotateIfNeeded_NoRotationWhenNilFile(_ *testing.T) {
	l := &Logger{maxSize: 1024, fileOutput: nil}
	l.rotateIfNeeded() // should not panic
}

// ---------------------------------------------------------------------------
// JSON format toggle
// ---------------------------------------------------------------------------

func TestLoggerJSONOutput(t *testing.T) {
	l, buf := newTestLogger("jsontest", DEBUG)
	l.jsonFormat = true
	l.log(INFO, "json log entry")
	output := buf.String()
	// Should be valid JSON
	var parsed map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(output)), &parsed); err != nil {
		t.Fatalf("JSON output should be valid: %v\nraw: %s", err, output)
	}
}

// ---------------------------------------------------------------------------
// Color format
// ---------------------------------------------------------------------------

func TestFormatMessage_WithColors(t *testing.T) {
	l, _ := newTestLogger("test", DEBUG)
	l.useColors = true
	msg := l.formatMessage(ERROR, "", "colored", 0)
	if !strings.Contains(msg, "\033[") {
		t.Error("colored message should contain ANSI codes")
	}
}

func TestFormatMessage_WithoutColors(t *testing.T) {
	l, _ := newTestLogger("test", DEBUG)
	l.useColors = false
	msg := l.formatMessage(ERROR, "", "plain", 0)
	if strings.Contains(msg, "\033[") {
		t.Error("non-colored message should not contain ANSI codes")
	}
}
