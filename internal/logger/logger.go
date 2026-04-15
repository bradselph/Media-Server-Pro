// Package logger provides verbose, structured logging for the media server.
// It supports multiple log levels, colored output, file logging with rotation,
// and proper shutdown to ensure all logs are persisted before exit.
package logger

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"sync"
	"time"
)

// Level represents the severity of a log message
type Level int

const (
	DEBUG Level = iota
	INFO
	WARN
	ERROR
	FATAL
)

var levelNames = map[Level]string{
	DEBUG: "DEBUG",
	INFO:  "INFO",
	WARN:  "WARN",
	ERROR: "ERROR",
	FATAL: "FATAL",
}

var levelColors = map[Level]string{
	DEBUG: "\033[36m", // Cyan
	INFO:  "\033[32m", // Green
	WARN:  "\033[33m", // Yellow
	ERROR: "\033[31m", // Red
	FATAL: "\033[35m", // Magenta
}

const colorReset = "\033[0m"

// ridKeyType is the context key for request ID propagation.
type ridKeyType struct{}

var ridKey = ridKeyType{}

// ContextWithRequestID returns a new context carrying the given request ID.
// Used by HTTP middleware to propagate the request ID to downstream modules.
func ContextWithRequestID(ctx context.Context, rid string) context.Context {
	return context.WithValue(ctx, ridKey, rid)
}

// RequestIDFromContext extracts the request ID from a context, or "" if absent.
func RequestIDFromContext(ctx context.Context) string {
	if rid, ok := ctx.Value(ridKey).(string); ok {
		return rid
	}
	return ""
}

// Logger provides structured logging with module context
type Logger struct {
	module     string
	minLevel   Level
	output     io.Writer
	fileOutput *os.File
	mu         sync.Mutex
	useColors  bool
	jsonFormat bool
	logToFile  bool
	logDir     string
	maxSize    int64
	maxBackups int
}

// Global logger instance
var globalLogger *Logger
var once sync.Once

// Config holds logger configuration
type Config struct {
	MinLevel   Level
	LogDir     string
	LogToFile  bool
	UseColors  bool
	JSONFormat bool // Output JSON-structured log lines instead of text
	ModuleName string
	MaxSize    int64 // Max file size in bytes before rotation (0 = no rotation)
	MaxBackups int   // Number of rotated log files to keep (0 = keep all)
}

// DefaultConfig returns the default logger configuration.
// Colors are disabled automatically when:
//   - Running under systemd (INVOCATION_ID is set by the service manager)
//   - NO_COLOR env var is set (https://no-color.org)
//   - stdout is not a terminal (piped output)
func DefaultConfig() Config {
	useColors := isColorTerminal()
	return Config{
		MinLevel:   INFO,
		LogDir:     "logs",
		LogToFile:  false,
		UseColors:  useColors,
		ModuleName: "main",
		MaxSize:    100 * 1024 * 1024, // 100MB
		MaxBackups: 5,
	}
}

// isColorTerminal returns true when ANSI color output is appropriate:
// not under systemd, not NO_COLOR, and stdout looks like a terminal.
func isColorTerminal() bool {
	// Respect NO_COLOR convention (https://no-color.org)
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	// systemd sets INVOCATION_ID on every managed process; journald strips
	// ANSI codes from structured journal entries but they look wrong in plain
	// `journalctl` output, so disable colors when detected.
	if os.Getenv("INVOCATION_ID") != "" {
		return false
	}
	// If TERM is unset or "dumb" there is no color support.
	term := os.Getenv("TERM")
	if term == "" || term == "dumb" {
		return false
	}
	return true
}

// Init initializes the global logger with the given configuration
func Init(cfg Config) error {
	var initErr error
	once.Do(func() {
		globalLogger = &Logger{
			module:     cfg.ModuleName,
			minLevel:   cfg.MinLevel,
			output:     os.Stdout,
			useColors:  cfg.UseColors,
			jsonFormat: cfg.JSONFormat,
			logToFile:  cfg.LogToFile,
			logDir:     cfg.LogDir,
			maxSize:    cfg.MaxSize,
			maxBackups: cfg.MaxBackups,
		}

		if cfg.LogToFile {
			if err := os.MkdirAll(cfg.LogDir, 0o755); err != nil { //nolint:gosec // world-readable log dir is intentional
				initErr = fmt.Errorf("failed to create log directory: %w", err)
				return
			}
			logFile := filepath.Join(cfg.LogDir, fmt.Sprintf("server_%s.log", time.Now().Format("2006-01-02")))
			f, err := os.OpenFile(logFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o640) //nolint:gosec // 0640 intentional: allows log group to read files
			if err != nil {
				initErr = fmt.Errorf("failed to open log file: %w", err)
				return
			}
			globalLogger.fileOutput = f
		}
	})
	return initErr
}

// EnableFileLogging enables file logging on the already-initialized global logger.
// This is used after config is loaded to wire config-based logging settings.
func EnableFileLogging(logDir string, maxSize int64, maxBackups int) error {
	if globalLogger == nil {
		return fmt.Errorf("logger not initialized")
	}

	globalLogger.mu.Lock()
	defer globalLogger.mu.Unlock()

	if err := os.MkdirAll(logDir, 0o755); err != nil { //nolint:gosec // world-readable log dir is intentional
		return fmt.Errorf("failed to create log directory %s: %w", logDir, err)
	}

	logFile := filepath.Join(logDir, fmt.Sprintf("server_%s.log", time.Now().Format("2006-01-02")))
	f, err := os.OpenFile(logFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o640) //nolint:gosec // 0640 intentional: allows log group to read files
	if err != nil {
		return fmt.Errorf("failed to open log file %s: %w", logFile, err)
	}

	// Close any existing file output
	if globalLogger.fileOutput != nil {
		if err := globalLogger.fileOutput.Sync(); err != nil {
			_ = err // best-effort sync
		}
		if err := globalLogger.fileOutput.Close(); err != nil {
			_ = err // best-effort close
		}
	}

	globalLogger.fileOutput = f
	globalLogger.logToFile = true
	globalLogger.logDir = logDir
	if maxSize > 0 {
		globalLogger.maxSize = maxSize
	}
	if maxBackups > 0 {
		globalLogger.maxBackups = maxBackups
	}

	return nil
}

// Shutdown flushes and closes the global logger's file output.
// Should be called during server shutdown to ensure all logs are persisted.
func Shutdown() {
	if globalLogger == nil {
		return
	}
	globalLogger.mu.Lock()
	defer globalLogger.mu.Unlock()

	if globalLogger.fileOutput != nil {
		// Sync to ensure all buffered data is written to disk
		if err := globalLogger.fileOutput.Sync(); err != nil {
			_ = err // best-effort sync
		}
		if err := globalLogger.fileOutput.Close(); err != nil {
			_ = err // best-effort close
		}
		globalLogger.fileOutput = nil
	}
}

// New creates a new logger for a specific module.
// Child loggers delegate file writes to globalLogger to avoid stale file handles after rotation.
func New(module string) *Logger {
	if globalLogger == nil {
		// Initialize with defaults if not already done
		if err := Init(DefaultConfig()); err != nil {
			_ = err // fallback init
		}
	}
	return &Logger{
		module:     module,
		minLevel:   globalLogger.minLevel,
		output:     globalLogger.output,
		useColors:  globalLogger.useColors,
		jsonFormat: globalLogger.jsonFormat,
		logToFile:  globalLogger.logToFile,
		logDir:     globalLogger.logDir,
		maxSize:    globalLogger.maxSize,
		maxBackups: globalLogger.maxBackups,
		// fileOutput intentionally NOT copied - file writes delegated to globalLogger
	}
}

// SetLevel changes the minimum log level
func (l *Logger) SetLevel(level Level) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.minLevel = level
}

// SetJSONFormat enables or disables JSON-structured log output on the global logger.
// Call after Init() to switch output format based on config (e.g. LOG_FORMAT=json).
func SetJSONFormat(enabled bool) {
	if globalLogger == nil {
		return
	}
	globalLogger.mu.Lock()
	globalLogger.jsonFormat = enabled
	globalLogger.mu.Unlock()
}

// formatMessageJSON creates a JSON-structured log line.
func (l *Logger) formatMessageJSON(level Level, requestID, msg string, args ...any) string {
	formattedMsg := msg
	if len(args) > 0 {
		formattedMsg = fmt.Sprintf(msg, args...)
	}

	_, file, line, ok := runtime.Caller(3)
	caller := "unknown"
	if ok {
		caller = fmt.Sprintf("%s:%d", filepath.Base(file), line)
	}

	entry := map[string]any{
		"time":   time.Now().Format(time.RFC3339Nano),
		"level":  levelNames[level],
		"module": l.module,
		"caller": caller,
		"msg":    formattedMsg,
	}
	if requestID != "" {
		entry["request_id"] = requestID
	}

	data, err := json.Marshal(entry)
	if err != nil {
		return fmt.Sprintf(`{"error":"json marshal failed","msg":%q}`, formattedMsg)
	}
	return string(data)
}

// formatMessage creates a formatted log message with metadata
func (l *Logger) formatMessage(level Level, requestID, msg string, args ...any) string {
	timestamp := time.Now().Format("2006-01-02 15:04:05.000")
	formattedMsg := msg
	if len(args) > 0 {
		formattedMsg = fmt.Sprintf(msg, args...)
	}

	// Get caller information
	_, file, line, ok := runtime.Caller(3)
	caller := "unknown"
	if ok {
		caller = fmt.Sprintf("%s:%d", filepath.Base(file), line)
	}

	levelStr := levelNames[level]
	if l.useColors {
		levelStr = fmt.Sprintf("%s%s%s", levelColors[level], levelStr, colorReset)
	}

	if requestID != "" {
		return fmt.Sprintf("[%s] [%s] [%s] [%s] [%s] %s",
			timestamp,
			levelStr,
			l.module,
			requestID,
			caller,
			formattedMsg,
		)
	}

	return fmt.Sprintf("[%s] [%s] [%s] [%s] %s",
		timestamp,
		levelStr,
		l.module,
		caller,
		formattedMsg,
	)
}

// log writes a log message at the specified level
func (l *Logger) log(level Level, msg string, args ...any) {
	l.logWithRID(level, "", msg, args...)
}

// logWithRID writes a log message at the specified level with an optional request ID.
func (l *Logger) logWithRID(level Level, requestID, msg string, args ...any) {
	if level < l.minLevel {
		return
	}

	l.mu.Lock()
	var formatted string
	if l.jsonFormat {
		formatted = l.formatMessageJSON(level, requestID, msg, args...)
	} else {
		formatted = l.formatMessage(level, requestID, msg, args...)
	}

	// Write to stdout
	_, _ = fmt.Fprintln(l.output, formatted)
	l.mu.Unlock()

	// Delegate file writes to globalLogger to avoid stale file handles after rotation
	// Thread-safe: globalLogger.mu is held during rotation and file writes, and Shutdown()
	// also acquires this lock before closing the file, preventing writes to closed files
	if l.logToFile && globalLogger != nil {
		globalLogger.mu.Lock()
		defer globalLogger.mu.Unlock()
		if globalLogger.fileOutput != nil {
			globalLogger.rotateIfNeeded()
			var fileFormatted string
			if globalLogger.jsonFormat {
				fileFormatted = l.formatMessageJSON(level, requestID, msg, args...)
			} else {
				fileFormatted = l.formatMessagePlain(level, requestID, msg, args...)
			}
			_, _ = fmt.Fprintln(globalLogger.fileOutput, fileFormatted)
		}
	}
}

// rotateIfNeeded checks the current log file size and rotates if it exceeds maxSize.
// Must be called while holding l.mu.
func (l *Logger) rotateIfNeeded() {
	if l.maxSize <= 0 || l.fileOutput == nil {
		return
	}

	info, err := l.fileOutput.Stat()
	if err != nil {
		return
	}

	if info.Size() < l.maxSize {
		return
	}

	currentPath := l.fileOutput.Name()

	// Close the current file
	_ = l.fileOutput.Sync()  // best-effort sync
	_ = l.fileOutput.Close() // best-effort close

	// Rename current to a timestamped backup so that successive rotations
	// produce distinct files (.2006-01-02T150405) rather than overwriting the
	// single .1 backup that the old approach created.
	rotatedPath := currentPath + "." + time.Now().Format("20060102T150405")
	_ = os.Rename(currentPath, rotatedPath) // best-effort rename

	// Clean up old backups beyond maxBackups
	if l.maxBackups > 0 {
		l.cleanOldBackups(currentPath)
	}

	// Open a new log file
	f, err := os.OpenFile(currentPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o640) //nolint:gosec // 0640 intentional: group-readable for log aggregation
	if err != nil {
		// Fall back to writing without file
		l.fileOutput = nil
		return
	}
	l.fileOutput = f

	// Update globalLogger's fileOutput so new loggers get the rotated file
	if globalLogger != nil && globalLogger.logDir == l.logDir {
		globalLogger.fileOutput = f
	}
}

// cleanOldBackups removes log backup files exceeding maxBackups count.
// Must be called while holding l.mu.
func (l *Logger) cleanOldBackups(basePath string) {
	pattern := basePath + ".*"
	matches, err := filepath.Glob(pattern)
	if err != nil || len(matches) <= l.maxBackups {
		return
	}

	// Sort by modification time (oldest first)
	sort.Slice(matches, func(i, j int) bool {
		infoI, errI := os.Stat(matches[i])
		infoJ, errJ := os.Stat(matches[j])
		if errI != nil || errJ != nil {
			return matches[i] < matches[j]
		}
		return infoI.ModTime().Before(infoJ.ModTime())
	})

	// Remove oldest files exceeding the backup limit
	for i := 0; i < len(matches)-l.maxBackups; i++ {
		if err := os.Remove(matches[i]); err != nil {
			_ = err // best-effort cleanup
		}
	}
}

// formatMessagePlain creates a formatted log message without colors
func (l *Logger) formatMessagePlain(level Level, requestID, msg string, args ...any) string {
	timestamp := time.Now().Format("2006-01-02 15:04:05.000")
	formattedMsg := msg
	if len(args) > 0 {
		formattedMsg = fmt.Sprintf(msg, args...)
	}

	_, file, line, ok := runtime.Caller(4)
	caller := "unknown"
	if ok {
		caller = fmt.Sprintf("%s:%d", filepath.Base(file), line)
	}

	if requestID != "" {
		return fmt.Sprintf("[%s] [%s] [%s] [%s] [%s] %s",
			timestamp,
			levelNames[level],
			l.module,
			requestID,
			caller,
			formattedMsg,
		)
	}

	return fmt.Sprintf("[%s] [%s] [%s] [%s] %s",
		timestamp,
		levelNames[level],
		l.module,
		caller,
		formattedMsg,
	)
}

// Debug logs a debug message
func (l *Logger) Debug(msg string, args ...any) {
	l.log(DEBUG, msg, args...)
}

// Info logs an informational message
func (l *Logger) Info(msg string, args ...any) {
	l.log(INFO, msg, args...)
}

// Warn logs a warning message
func (l *Logger) Warn(msg string, args ...any) {
	l.log(WARN, msg, args...)
}

// Error logs an error message
func (l *Logger) Error(msg string, args ...any) {
	l.log(ERROR, msg, args...)
}

// Fatal logs a fatal message and exits the process immediately with code 1.
// WARNING: os.Exit() does not run deferred functions or allow graceful cleanup.
// Use this only for unrecoverable errors during initialization.
// For recoverable errors in running services, prefer returning errors and using
// coordinated shutdown mechanisms that allow proper cleanup.
func (l *Logger) Fatal(msg string, args ...any) {
	l.log(FATAL, msg, args...)
	Shutdown() // Flush logger's file buffer
	os.Exit(1) // Immediate termination - no defers run, other goroutines interrupted
}

// DebugCtx logs a debug message with request ID extracted from context.
func (l *Logger) DebugCtx(ctx context.Context, msg string, args ...any) {
	l.logWithRID(DEBUG, RequestIDFromContext(ctx), msg, args...)
}

// InfoCtx logs an informational message with request ID extracted from context.
func (l *Logger) InfoCtx(ctx context.Context, msg string, args ...any) {
	l.logWithRID(INFO, RequestIDFromContext(ctx), msg, args...)
}

// WarnCtx logs a warning message with request ID extracted from context.
func (l *Logger) WarnCtx(ctx context.Context, msg string, args ...any) {
	l.logWithRID(WARN, RequestIDFromContext(ctx), msg, args...)
}

// ErrorCtx logs an error message with request ID extracted from context.
func (l *Logger) ErrorCtx(ctx context.Context, msg string, args ...any) {
	l.logWithRID(ERROR, RequestIDFromContext(ctx), msg, args...)
}

// ModuleHealth tracks the health status of a module
type ModuleHealth struct {
	Module    string
	Healthy   bool
	LastError error
	LastCheck time.Time
	Message   string
}

// HealthReporter tracks and reports module health
type HealthReporter struct {
	mu       sync.RWMutex
	statuses map[string]*ModuleHealth
	logger   *Logger
}

// NewHealthReporter creates a new health reporter
func NewHealthReporter() *HealthReporter {
	return &HealthReporter{
		statuses: make(map[string]*ModuleHealth),
		logger:   New("health"),
	}
}

// Report updates the health status of a module
func (hr *HealthReporter) Report(module string, healthy bool, err error, msg string) {
	hr.mu.Lock()
	defer hr.mu.Unlock()

	hr.statuses[module] = &ModuleHealth{
		Module:    module,
		Healthy:   healthy,
		LastError: err,
		LastCheck: time.Now(),
		Message:   msg,
	}

	if healthy {
		hr.logger.Info("Module %s is healthy: %s", module, msg)
	} else {
		hr.logger.Error("Module %s is unhealthy: %s (error: %v)", module, msg, err)
	}
}
