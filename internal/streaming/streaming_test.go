package streaming

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"media-server-pro/internal/config"
	"media-server-pro/pkg/helpers"
)

func newTestModule(t *testing.T) *Module {
	t.Helper()
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.json")
	cfg := config.NewManager(cfgPath)
	return NewModule(cfg)
}

// ---------------------------------------------------------------------------
// Module lifecycle
// ---------------------------------------------------------------------------

func TestModule_Name(t *testing.T) {
	m := newTestModule(t)
	if m.Name() != "streaming" {
		t.Errorf("Name() = %q, want streaming", m.Name())
	}
}

func TestModule_StartStop(t *testing.T) {
	m := newTestModule(t)
	if err := m.Start(context.Background()); err != nil {
		t.Fatalf("Start() error: %v", err)
	}
	h := m.Health()
	if h.Status != "healthy" {
		t.Errorf("after Start, health status = %q, want healthy", h.Status)
	}
	if err := m.Stop(context.Background()); err != nil {
		t.Fatalf("Stop() error: %v", err)
	}
	h = m.Health()
	if h.Status != "unhealthy" {
		t.Errorf("after Stop, health status = %q, want unhealthy", h.Status)
	}
}

// ---------------------------------------------------------------------------
// helpers.SafeContentDispositionFilename (canonical implementation in pkg/helpers)
// ---------------------------------------------------------------------------

func TestSafeContentDispositionFilename(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"video.mp4", `attachment; filename="video.mp4"`},
		{`my"file.mp4`, `attachment; filename="myfile.mp4"`},
		{"my\\file.mp4", `attachment; filename="myfile.mp4"`},
		{"new\nline.mp4", `attachment; filename="newline.mp4"`},
		{"control\x01char.mp4", `attachment; filename="controlchar.mp4"`},
	}
	for _, tc := range tests {
		got := helpers.SafeContentDispositionFilename(tc.input)
		if got != tc.want {
			t.Errorf("SafeContentDispositionFilename(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

// ---------------------------------------------------------------------------
// parseRange
// ---------------------------------------------------------------------------

func TestParseRange_NoRange(t *testing.T) {
	m := newTestModule(t)
	start, end, err := m.parseRange("", 1000)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if start != 0 || end != 999 {
		t.Errorf("empty range: got (%d, %d), want (0, 999)", start, end)
	}
}

func TestParseRange_Standard(t *testing.T) {
	m := newTestModule(t)
	start, end, err := m.parseRange("bytes=0-499", 1000)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if start != 0 || end != 499 {
		t.Errorf("got (%d, %d), want (0, 499)", start, end)
	}
}

func TestParseRange_OpenEnd(t *testing.T) {
	m := newTestModule(t)
	start, end, err := m.parseRange("bytes=500-", 1000)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if start != 500 || end != 999 {
		t.Errorf("got (%d, %d), want (500, 999)", start, end)
	}
}

func TestParseRange_Suffix(t *testing.T) {
	m := newTestModule(t)
	start, end, err := m.parseRange("bytes=-200", 1000)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if start != 800 || end != 999 {
		t.Errorf("suffix range: got (%d, %d), want (800, 999)", start, end)
	}
}

func TestParseRange_SuffixLargerThanFile(t *testing.T) {
	m := newTestModule(t)
	start, end, err := m.parseRange("bytes=-2000", 1000)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if start != 0 || end != 999 {
		t.Errorf("large suffix: got (%d, %d), want (0, 999)", start, end)
	}
}

func TestParseRange_InvalidPrefix(t *testing.T) {
	m := newTestModule(t)
	_, _, err := m.parseRange("chars=0-499", 1000)
	if !errors.Is(err, ErrInvalidRange) {
		t.Errorf("expected ErrInvalidRange, got %v", err)
	}
}

func TestParseRange_StartAfterEnd(t *testing.T) {
	m := newTestModule(t)
	_, _, err := m.parseRange("bytes=500-100", 1000)
	if !errors.Is(err, ErrInvalidRange) {
		t.Errorf("expected ErrInvalidRange for start>end, got %v", err)
	}
}

func TestParseRange_EndBeyondFile(t *testing.T) {
	m := newTestModule(t)
	_, _, err := m.parseRange("bytes=0-1000", 1000)
	if !errors.Is(err, ErrInvalidRange) {
		t.Errorf("expected ErrInvalidRange for end>=fileSize, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// getContentType
// ---------------------------------------------------------------------------

func TestGetContentType(t *testing.T) {
	m := newTestModule(t)
	tests := []struct {
		path string
		want string
	}{
		{"video.mp4", "video/mp4"},
		{"video.webm", "video/webm"},
		{"video.mkv", "video/x-matroska"},
		{"video.avi", "video/x-msvideo"},
		{"audio.mp3", "audio/mpeg"},
		{"audio.flac", "audio/flac"},
		{"audio.ogg", "audio/ogg"},
		{"audio.m4a", "audio/mp4"},
		{"video.mov", "video/quicktime"},
	}
	for _, tc := range tests {
		got := m.getContentType(tc.path)
		if got != tc.want {
			t.Errorf("getContentType(%q) = %q, want %q", tc.path, got, tc.want)
		}
	}
}

func TestGetContentType_CaseInsensitive(t *testing.T) {
	m := newTestModule(t)
	got := m.getContentType("video.MP4")
	if got != "video/mp4" {
		t.Errorf("getContentType(MP4) = %q, want video/mp4", got)
	}
}

func TestGetContentType_Unknown(t *testing.T) {
	m := newTestModule(t)
	got := m.getContentType("file.xyz123")
	if got != "application/octet-stream" {
		t.Errorf("getContentType(unknown) = %q, want application/octet-stream", got)
	}
}

// ---------------------------------------------------------------------------
// isMobileDevice
// ---------------------------------------------------------------------------

func TestIsMobileDevice(t *testing.T) {
	m := newTestModule(t)
	tests := []struct {
		ua   string
		want bool
	}{
		{"Mozilla/5.0 (iPhone; CPU iPhone OS 15_0 like Mac OS X)", true},
		{"Mozilla/5.0 (Linux; Android 12; Pixel 6)", true},
		{"Mozilla/5.0 (iPad; CPU OS 15_0 like Mac OS X)", true},
		{"Mozilla/5.0 (Windows NT 10.0; Win64; x64)", false},
		{"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7)", false},
		{"Opera Mini/9.80", true},
		{"", false},
	}
	for _, tc := range tests {
		got := m.isMobileDevice(tc.ua)
		if got != tc.want {
			t.Errorf("isMobileDevice(%q) = %v, want %v", tc.ua, got, tc.want)
		}
	}
}

// ---------------------------------------------------------------------------
// Session management
// ---------------------------------------------------------------------------

func TestSessionLifecycle(t *testing.T) {
	m := newTestModule(t)

	sessions := m.GetActiveSessions()
	if len(sessions) != 0 {
		t.Fatalf("expected 0 active sessions, got %d", len(sessions))
	}

	req := StreamRequest{
		Path:      "/test/video.mp4",
		MediaID:   "media-1",
		UserID:    "user-1",
		SessionID: "test",
	}
	session := m.startSession(req, 0)
	if session.ID == "" {
		t.Error("session ID should not be empty")
	}

	sessions = m.GetActiveSessions()
	if len(sessions) != 1 {
		t.Fatalf("expected 1 active session, got %d", len(sessions))
	}

	m.endSession(session.ID)
	sessions = m.GetActiveSessions()
	if len(sessions) != 0 {
		t.Errorf("expected 0 active sessions after end, got %d", len(sessions))
	}
}

func TestGetActiveStreamCount(t *testing.T) {
	m := newTestModule(t)
	m.startSession(StreamRequest{Path: "/a", UserID: "user-1", SessionID: "s1"}, 0)
	m.startSession(StreamRequest{Path: "/b", UserID: "user-1", SessionID: "s2"}, 0)
	m.startSession(StreamRequest{Path: "/c", UserID: "user-2", SessionID: "s3"}, 0)

	if got := m.GetActiveStreamCount("user-1"); got != 2 {
		t.Errorf("user-1 stream count = %d, want 2", got)
	}
	if got := m.GetActiveStreamCount("user-2"); got != 1 {
		t.Errorf("user-2 stream count = %d, want 1", got)
	}
	if got := m.GetActiveStreamCount("user-3"); got != 0 {
		t.Errorf("user-3 stream count = %d, want 0", got)
	}
}

func TestCanStartStream(t *testing.T) {
	m := newTestModule(t)
	m.startSession(StreamRequest{Path: "/a", UserID: "user-1", SessionID: "s1"}, 0)
	m.startSession(StreamRequest{Path: "/b", UserID: "user-1", SessionID: "s2"}, 0)

	if !m.CanStartStream("user-1", 0) {
		t.Error("maxStreams=0 should always allow")
	}
	if !m.CanStartStream("user-1", 3) {
		t.Error("2 streams < 3 max, should allow")
	}
	if m.CanStartStream("user-1", 2) {
		t.Error("2 streams = 2 max, should not allow")
	}
	if !m.CanStartStream("user-2", 2) {
		t.Error("user-2 has 0 streams, should allow")
	}
}

// ---------------------------------------------------------------------------
// Stats
// ---------------------------------------------------------------------------

func TestGetStats(t *testing.T) {
	m := newTestModule(t)
	s := m.startSession(StreamRequest{Path: "/a", UserID: "u1", SessionID: "s"}, 0)
	stats := m.GetStats()
	if stats.TotalStreams != 1 {
		t.Errorf("TotalStreams = %d, want 1", stats.TotalStreams)
	}
	if stats.ActiveStreams != 1 {
		t.Errorf("ActiveStreams = %d, want 1", stats.ActiveStreams)
	}
	if stats.PeakConcurrent != 1 {
		t.Errorf("PeakConcurrent = %d, want 1", stats.PeakConcurrent)
	}

	m.endSession(s.ID)
	stats = m.GetStats()
	if stats.ActiveStreams != 0 {
		t.Errorf("ActiveStreams after end = %d, want 0", stats.ActiveStreams)
	}
	if stats.PeakConcurrent != 1 {
		t.Errorf("PeakConcurrent should remain 1, got %d", stats.PeakConcurrent)
	}
}

// ---------------------------------------------------------------------------
// generateSessionID
// ---------------------------------------------------------------------------

func TestGenerateSessionID(t *testing.T) {
	id1 := generateSessionID("test")
	id2 := generateSessionID("test")
	if id1 == "" || id2 == "" {
		t.Error("session IDs should not be empty")
	}
	if id1 == id2 {
		t.Error("session IDs should be unique")
	}
}

// ---------------------------------------------------------------------------
// Stream — integration with real file
// ---------------------------------------------------------------------------

func TestStream_FileNotFound(t *testing.T) {
	m := newTestModule(t)
	m.Start(context.Background())
	defer m.Stop(context.Background())

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/stream", nil)
	err := m.Stream(w, r, StreamRequest{
		Path: "/nonexistent/file.mp4",
	})
	if !errors.Is(err, ErrFileNotFound) {
		t.Errorf("expected ErrFileNotFound, got %v", err)
	}
}

func TestStream_FullFile(t *testing.T) {
	m := newTestModule(t)
	m.Start(context.Background())
	defer m.Stop(context.Background())

	dir := t.TempDir()
	fpath := filepath.Join(dir, "test.mp4")
	content := []byte("fake video content for testing")
	os.WriteFile(fpath, content, 0644)

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/stream", nil)
	err := m.Stream(w, r, StreamRequest{
		Path:    fpath,
		MediaID: "test-id",
	})
	if err != nil {
		t.Fatalf("Stream() error: %v", err)
	}
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
	if w.Body.Len() != len(content) {
		t.Errorf("body length = %d, want %d", w.Body.Len(), len(content))
	}
}

func TestStream_RangeRequest(t *testing.T) {
	m := newTestModule(t)
	m.Start(context.Background())
	defer m.Stop(context.Background())

	dir := t.TempDir()
	fpath := filepath.Join(dir, "test.mp4")
	content := make([]byte, 1000)
	for i := range content {
		content[i] = byte(i % 256)
	}
	os.WriteFile(fpath, content, 0644)

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/stream", nil)
	err := m.Stream(w, r, StreamRequest{
		Path:        fpath,
		RangeHeader: "bytes=0-99",
	})
	if err != nil {
		t.Fatalf("Stream() error: %v", err)
	}
	if w.Code != http.StatusPartialContent {
		t.Errorf("status = %d, want 206", w.Code)
	}
	if w.Body.Len() != 100 {
		t.Errorf("body length = %d, want 100", w.Body.Len())
	}
}

// ---------------------------------------------------------------------------
// Download
// ---------------------------------------------------------------------------

func TestDownload_FileNotFound(t *testing.T) {
	m := newTestModule(t)
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/download", nil)
	err := m.Download(w, r, "/nonexistent/file.mp4")
	if !errors.Is(err, ErrFileNotFound) {
		t.Errorf("expected ErrFileNotFound, got %v", err)
	}
}

func TestDownload_FullFile(t *testing.T) {
	m := newTestModule(t)
	dir := t.TempDir()
	fpath := filepath.Join(dir, "test.mp4")
	content := []byte("download test content")
	os.WriteFile(fpath, content, 0644)

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/download", nil)
	err := m.Download(w, r, fpath)
	if err != nil {
		t.Fatalf("Download() error: %v", err)
	}
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
	if w.Header().Get("Content-Disposition") == "" {
		t.Error("Content-Disposition header missing")
	}
	if w.Body.Len() != len(content) {
		t.Errorf("body length = %d, want %d", w.Body.Len(), len(content))
	}
}

// ---------------------------------------------------------------------------
// Error constants
// ---------------------------------------------------------------------------

func TestErrorConstants(t *testing.T) {
	if ErrFileNotFound.Error() != "file not found" {
		t.Errorf("ErrFileNotFound = %q", ErrFileNotFound.Error())
	}
	if ErrInvalidRange.Error() != "invalid range" {
		t.Errorf("ErrInvalidRange = %q", ErrInvalidRange.Error())
	}
	if ErrFileTooLarge.Error() != "file too large" {
		t.Errorf("ErrFileTooLarge = %q", ErrFileTooLarge.Error())
	}
}
