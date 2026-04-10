package handlers

import (
	"crypto/tls"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/gin-gonic/gin"
)

func init() {
	gin.SetMode(gin.TestMode)
}

// ---------------------------------------------------------------------------
// Response helpers
// ---------------------------------------------------------------------------

func TestWriteSuccess(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	writeSuccess(c, map[string]string{"key": "value"})
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
	body := w.Body.String()
	if body == "" {
		t.Error("response body empty")
	}
	if !contains(body, `"success":true`) {
		t.Errorf("response missing success:true: %s", body)
	}
}

func TestWriteError(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	writeError(c, http.StatusBadRequest, "something went wrong")
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
	body := w.Body.String()
	if !contains(body, `"success":false`) {
		t.Errorf("response missing success:false: %s", body)
	}
	if !contains(body, "something went wrong") {
		t.Errorf("response missing error message: %s", body)
	}
}

// ---------------------------------------------------------------------------
// safeContentDisposition
// ---------------------------------------------------------------------------

func TestSafeContentDisposition(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"video.mp4", `attachment; filename="video.mp4"`},
		{`my"file.mp4`, `attachment; filename="myfile.mp4"`},
		{"new\nline.mp4", `attachment; filename="newline.mp4"`},
		{"ctrl\x01char.mp4", `attachment; filename="ctrlchar.mp4"`},
		{`back\slash.mp4`, `attachment; filename="backslash.mp4"`},
	}
	for _, tc := range tests {
		got := safeContentDisposition(tc.input)
		if got != tc.want {
			t.Errorf("safeContentDisposition(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

// ---------------------------------------------------------------------------
// getSession / getUser
// ---------------------------------------------------------------------------

func TestGetSession_NoSession(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	s := getSession(c)
	if s != nil {
		t.Error("expected nil session when not set")
	}
}

func TestGetUser_NoUser(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	u := getUser(c)
	if u != nil {
		t.Error("expected nil user when not set")
	}
}

// ---------------------------------------------------------------------------
// generateRandomString
// ---------------------------------------------------------------------------

func TestGenerateRandomString(t *testing.T) {
	s := generateRandomString(32)
	if len(s) != 32 {
		t.Errorf("length = %d, want 32", len(s))
	}
	// Should produce different strings
	s2 := generateRandomString(32)
	if s == s2 {
		t.Error("two random strings should be different")
	}
}

func TestGenerateRandomString_ZeroLength(t *testing.T) {
	s := generateRandomString(0)
	if len(s) != 0 {
		t.Errorf("length = %d, want 0", len(s))
	}
}

// ---------------------------------------------------------------------------
// isSecureRequest
// ---------------------------------------------------------------------------

func TestIsSecureRequest(t *testing.T) {
	// Plain HTTP
	req := httptest.NewRequest("GET", "/", nil)
	if isSecureRequest(req) {
		t.Error("plain HTTP should not be secure")
	}

	// X-Forwarded-Proto from trusted proxy (loopback)
	req = httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "127.0.0.1:1234"
	req.Header.Set("X-Forwarded-Proto", "https")
	if !isSecureRequest(req) {
		t.Error("X-Forwarded-Proto: https from trusted proxy should be secure")
	}

	// X-Forwarded-Proto from untrusted client — should NOT be secure
	req = httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "203.0.113.5:1234"
	req.Header.Set("X-Forwarded-Proto", "https")
	if isSecureRequest(req) {
		t.Error("X-Forwarded-Proto: https from untrusted client should not be secure")
	}

	// Cloudflare from trusted proxy
	req = httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "127.0.0.1:1234"
	req.Header.Set("Cf-Visitor", `{"scheme":"https"}`)
	if !isSecureRequest(req) {
		t.Error("Cf-Visitor with https from trusted proxy should be secure")
	}

	// Cf-Visitor from untrusted client — must not be treated as HTTPS
	req = httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "203.0.113.5:1234"
	req.Header.Set("Cf-Visitor", `{"scheme":"https"}`)
	if isSecureRequest(req) {
		t.Error("Cf-Visitor from untrusted client should not be secure")
	}

	// Direct TLS (no proxy headers)
	req = httptest.NewRequest("GET", "/", nil)
	req.TLS = &tls.ConnectionState{}
	if !isSecureRequest(req) {
		t.Error("request with TLS should be secure regardless of headers")
	}
}

// ---------------------------------------------------------------------------
// isClientDisconnect
// ---------------------------------------------------------------------------

func TestIsClientDisconnect(t *testing.T) {
	if isClientDisconnect(nil) {
		t.Error("nil error should not be client disconnect")
	}
	tests := []struct {
		msg  string
		want bool
	}{
		{"write: broken pipe", true},
		{"connection reset by peer", true},
		{"write: connection reset", true},
		{"i/o timeout", true},
		{"some random error", false},
	}
	for _, tc := range tests {
		got := isClientDisconnect(errors.New(tc.msg))
		if got != tc.want {
			t.Errorf("isClientDisconnect(%q) = %v, want %v", tc.msg, got, tc.want)
		}
	}
}

// ---------------------------------------------------------------------------
// isPathUnderDirs
// ---------------------------------------------------------------------------

func TestIsPathUnderDirs(t *testing.T) {
	base := t.TempDir()
	mediaDir := filepath.Join(base, "media")
	etcDir := filepath.Join(base, "etc")
	os.MkdirAll(mediaDir, 0o750)
	os.MkdirAll(filepath.Join(mediaDir, "sub"), 0o750)
	os.MkdirAll(etcDir, 0o750)

	tests := []struct {
		path string
		dirs []string
		want bool
	}{
		{filepath.Join(mediaDir, "video.mp4"), []string{mediaDir}, true},
		{filepath.Join(mediaDir, "sub", "video.mp4"), []string{mediaDir}, true},
		{filepath.Join(etcDir, "passwd"), []string{mediaDir}, false},
		{filepath.Join(mediaDir, "video.mp4"), []string{""}, false},
		{mediaDir, []string{mediaDir}, true},                                        // dir matches itself
		{filepath.Join(mediaDir, "..", "etc", "passwd"), []string{mediaDir}, false}, // traversal
	}
	for _, tc := range tests {
		got := isPathUnderDirs(tc.path, tc.dirs)
		if got != tc.want {
			t.Errorf("isPathUnderDirs(%q, %v) = %v, want %v", tc.path, tc.dirs, got, tc.want)
		}
	}
}

// ---------------------------------------------------------------------------
// resolvePathToAbsoluteNoWrite
// ---------------------------------------------------------------------------

func TestResolvePathToAbsoluteNoWrite_Absolute(t *testing.T) {
	dir := t.TempDir()
	fpath := filepath.Join(dir, "test.mp4")
	os.WriteFile(fpath, []byte("test"), 0o600)

	result, err := resolvePathToAbsoluteNoWrite(fpath, []string{dir})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != fpath {
		t.Errorf("resolved = %q, want %q", result, fpath)
	}
}

func TestResolvePathToAbsoluteNoWrite_Relative(t *testing.T) {
	dir := t.TempDir()
	fpath := filepath.Join(dir, "test.mp4")
	os.WriteFile(fpath, []byte("test"), 0o600)

	result, err := resolvePathToAbsoluteNoWrite("test.mp4", []string{dir})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == "" {
		t.Error("resolved path should not be empty")
	}
}

func TestResolvePathToAbsoluteNoWrite_NotFound(t *testing.T) {
	_, err := resolvePathToAbsoluteNoWrite("nonexistent.mp4", []string{"/tmp"})
	if err == nil {
		t.Error("expected error for nonexistent file")
	}
}

func TestResolvePathToAbsoluteNoWrite_OutsideDirs(t *testing.T) {
	_, err := resolvePathToAbsoluteNoWrite("/etc/passwd", []string{"/home/media"})
	if err == nil {
		t.Error("expected error for path outside allowed dirs")
	}
}

// ---------------------------------------------------------------------------
// isAudioExtension
// ---------------------------------------------------------------------------

func TestIsAudioExtension(t *testing.T) {
	for _, ext := range []string{".mp3", ".wav", ".flac", ".aac", ".ogg", ".m4a", ".wma", ".opus"} {
		if !isAudioExtension(ext) {
			t.Errorf("isAudioExtension(%q) = false, want true", ext)
		}
	}
	for _, ext := range []string{".mp4", ".mkv", ".avi", ".txt", ""} {
		if isAudioExtension(ext) {
			t.Errorf("isAudioExtension(%q) = true, want false", ext)
		}
	}
}

// ---------------------------------------------------------------------------
// requireModule
// ---------------------------------------------------------------------------

func TestRequireModule_Nil(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	ok := requireModule(c, nil, "Test")
	if ok {
		t.Error("nil module should return false")
	}
	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", w.Code)
	}
}

func TestRequireModule_NonNil(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	ok := requireModule(c, "something", "Test")
	if !ok {
		t.Error("non-nil module should return true")
	}
}

// ---------------------------------------------------------------------------
// Error constants
// ---------------------------------------------------------------------------

func TestErrorConstants(t *testing.T) {
	constants := map[string]string{
		"errIDRequired":        errIDRequired,
		"errFileNotFound":      errFileNotFound,
		"errInvalidRequest":    errInvalidRequest,
		"errNotAuthenticated":  errNotAuthenticated,
		"errUserNotFound":      errUserNotFound,
		"errMediaNotFound":     errMediaNotFound,
		"errPathParamRequired": errPathParamRequired,
	}
	for name, val := range constants {
		if val == "" {
			t.Errorf("constant %s is empty", name)
		}
	}
}

// helper
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsSubstr(s, substr))
}

func containsSubstr(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
