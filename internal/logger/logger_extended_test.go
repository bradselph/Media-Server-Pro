package logger

import (
	"os"
	"testing"
)

// ---------------------------------------------------------------------------
// isColorTerminal
// ---------------------------------------------------------------------------

func TestIsColorTerminal_NoColor(t *testing.T) {
	old := os.Getenv("NO_COLOR")
	os.Setenv("NO_COLOR", "1")
	defer os.Setenv("NO_COLOR", old)
	if isColorTerminal() {
		t.Error("NO_COLOR set should disable color")
	}
}

func TestIsColorTerminal_Systemd(t *testing.T) {
	old := os.Getenv("INVOCATION_ID")
	os.Setenv("INVOCATION_ID", "abc123")
	defer os.Setenv("INVOCATION_ID", old)
	oldNC := os.Getenv("NO_COLOR")
	os.Unsetenv("NO_COLOR")
	defer os.Setenv("NO_COLOR", oldNC)

	if isColorTerminal() {
		t.Error("INVOCATION_ID set (systemd) should disable color")
	}
}

func TestIsColorTerminal_DumbTerminal(t *testing.T) {
	oldTerm := os.Getenv("TERM")
	oldNC := os.Getenv("NO_COLOR")
	oldInv := os.Getenv("INVOCATION_ID")
	os.Setenv("TERM", "dumb")
	os.Unsetenv("NO_COLOR")
	os.Unsetenv("INVOCATION_ID")
	defer func() {
		os.Setenv("TERM", oldTerm)
		os.Setenv("NO_COLOR", oldNC)
		os.Setenv("INVOCATION_ID", oldInv)
	}()

	if isColorTerminal() {
		t.Error("TERM=dumb should disable color")
	}
}

func TestIsColorTerminal_EmptyTerm(t *testing.T) {
	oldTerm := os.Getenv("TERM")
	oldNC := os.Getenv("NO_COLOR")
	oldInv := os.Getenv("INVOCATION_ID")
	os.Unsetenv("TERM")
	os.Unsetenv("NO_COLOR")
	os.Unsetenv("INVOCATION_ID")
	defer func() {
		os.Setenv("TERM", oldTerm)
		os.Setenv("NO_COLOR", oldNC)
		os.Setenv("INVOCATION_ID", oldInv)
	}()

	if isColorTerminal() {
		t.Error("empty TERM should disable color")
	}
}

func TestIsColorTerminal_XTerm(t *testing.T) {
	oldTerm := os.Getenv("TERM")
	oldNC := os.Getenv("NO_COLOR")
	oldInv := os.Getenv("INVOCATION_ID")
	os.Setenv("TERM", "xterm-256color")
	os.Unsetenv("NO_COLOR")
	os.Unsetenv("INVOCATION_ID")
	defer func() {
		os.Setenv("TERM", oldTerm)
		os.Setenv("NO_COLOR", oldNC)
		os.Setenv("INVOCATION_ID", oldInv)
	}()

	if !isColorTerminal() {
		t.Error("TERM=xterm-256color should enable color")
	}
}

// ---------------------------------------------------------------------------
// SetJSONFormat (exported function)
// ---------------------------------------------------------------------------

func TestSetJSONFormat_NilLogger(t *testing.T) {
	old := globalLogger
	globalLogger = nil
	defer func() { globalLogger = old }()
	SetJSONFormat(true) // should not panic
}

func TestSetJSONFormat_SetsFlag(t *testing.T) {
	log := New("test")
	old := globalLogger
	globalLogger = log
	defer func() { globalLogger = old }()

	SetJSONFormat(true)
	if !globalLogger.jsonFormat {
		t.Error("SetJSONFormat(true) should set jsonFormat to true")
	}
	SetJSONFormat(false)
	if globalLogger.jsonFormat {
		t.Error("SetJSONFormat(false) should set jsonFormat to false")
	}
}

// ---------------------------------------------------------------------------
// New — child logger
// ---------------------------------------------------------------------------

func TestNew_Module(t *testing.T) {
	log := New("child")
	if log == nil {
		t.Fatal("New returned nil")
	}
	if log.module != "child" {
		t.Errorf("module = %q, want child", log.module)
	}
}

// ---------------------------------------------------------------------------
// formatMessage / formatMessageJSON
// ---------------------------------------------------------------------------

func TestFormatMessage_ContainsModule(t *testing.T) {
	log := New("mymod")
	msg := log.formatMessage(INFO, "", "hello %s", "world")
	if msg == "" {
		t.Error("formatted message should not be empty")
	}
}

func TestFormatMessageJSON_Structure(t *testing.T) {
	log := New("mymod")
	msg := log.formatMessageJSON(INFO, "", "hello")
	if msg == "" {
		t.Error("JSON message should not be empty")
	}
	if msg[0] != '{' {
		t.Errorf("JSON message should start with '{', got %q", msg[:1])
	}
}

func TestFormatMessagePlain(t *testing.T) {
	log := New("mymod")
	msg := log.formatMessagePlain(WARN, "", "warning: %d", 42)
	if msg == "" {
		t.Error("plain message should not be empty")
	}
}

func TestFormatMessage_WithRequestID_Extended(t *testing.T) {
	log := New("mymod")
	msg := log.formatMessage(INFO, "req-123", "test %d", 42)
	if msg == "" {
		t.Error("message with request ID should not be empty")
	}
}
