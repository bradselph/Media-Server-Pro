package streaming

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Regression tests for R10 (ETag/Last-Modified validators, If-Range,
// If-None-Match/If-Modified-Since) and R09 (X-Accel-Buffering) on the direct-play
// and download paths in this package.

func newValidatorTestFile(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	fpath := filepath.Join(dir, testMp4)
	content := make([]byte, 500)
	for i := range content {
		content[i] = byte(i % 256)
	}
	if err := os.WriteFile(fpath, content, 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	return fpath
}

func TestStream_SetsETagAndLastModified(t *testing.T) {
	m := newTestModule(t)
	m.Start(context.Background())
	defer m.Stop(context.Background())

	fpath := newValidatorTestFile(t)
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", testStream, nil)
	if err := m.Stream(w, r, StreamRequest{Path: fpath, MediaID: "media-1"}); err != nil {
		t.Fatalf("Stream() error: %v", err)
	}

	if w.Header().Get("ETag") == "" {
		t.Error("expected ETag header to be set")
	}
	if w.Header().Get("Last-Modified") == "" {
		t.Error("expected Last-Modified header to be set")
	}
}

func TestStream_XAccelBufferingHeaderPresent(t *testing.T) {
	m := newTestModule(t)
	m.Start(context.Background())
	defer m.Stop(context.Background())

	fpath := newValidatorTestFile(t)
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", testStream, nil)
	if err := m.Stream(w, r, StreamRequest{Path: fpath, MediaID: "media-1"}); err != nil {
		t.Fatalf("Stream() error: %v", err)
	}

	if got := w.Header().Get("X-Accel-Buffering"); got != "no" {
		t.Errorf("X-Accel-Buffering = %q, want %q", got, "no")
	}
}

func TestDownload_XAccelBufferingHeaderPresent(t *testing.T) {
	m := newTestModule(t)
	fpath := newValidatorTestFile(t)
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/download", nil)
	if err := m.Download(w, r, fpath); err != nil {
		t.Fatalf("Download() error: %v", err)
	}
	if got := w.Header().Get("X-Accel-Buffering"); got != "no" {
		t.Errorf("X-Accel-Buffering = %q, want %q", got, "no")
	}
	if w.Header().Get("ETag") == "" {
		t.Error("expected ETag header to be set on download")
	}
	if w.Header().Get("Last-Modified") == "" {
		t.Error("expected Last-Modified header to be set on download")
	}
}

func TestStream_IfNoneMatch_ReturnsNotModified(t *testing.T) {
	m := newTestModule(t)
	m.Start(context.Background())
	defer m.Stop(context.Background())
	fpath := newValidatorTestFile(t)

	// First request to learn the current ETag.
	w1 := httptest.NewRecorder()
	r1 := httptest.NewRequest("GET", testStream, nil)
	if err := m.Stream(w1, r1, StreamRequest{Path: fpath, MediaID: "media-1"}); err != nil {
		t.Fatalf("Stream() error: %v", err)
	}
	etag := w1.Header().Get("ETag")
	if etag == "" {
		t.Fatal("expected an ETag from the first request")
	}

	// A conditional non-range GET with a matching If-None-Match must 304 with no body.
	w2 := httptest.NewRecorder()
	r2 := httptest.NewRequest("GET", testStream, nil)
	r2.Header.Set("If-None-Match", etag)
	if err := m.Stream(w2, r2, StreamRequest{Path: fpath, MediaID: "media-1"}); err != nil {
		t.Fatalf("Stream() error: %v", err)
	}
	if w2.Code != http.StatusNotModified {
		t.Errorf("status = %d, want %d", w2.Code, http.StatusNotModified)
	}
	if w2.Body.Len() != 0 {
		t.Errorf("304 response body length = %d, want 0", w2.Body.Len())
	}

	// A non-matching If-None-Match must serve the full content normally.
	w3 := httptest.NewRecorder()
	r3 := httptest.NewRequest("GET", testStream, nil)
	r3.Header.Set("If-None-Match", `W/"stale-etag"`)
	if err := m.Stream(w3, r3, StreamRequest{Path: fpath, MediaID: "media-1"}); err != nil {
		t.Fatalf("Stream() error: %v", err)
	}
	if w3.Code != http.StatusOK {
		t.Errorf("status = %d, want 200 for a non-matching If-None-Match", w3.Code)
	}
}

func TestStream_IfModifiedSince_ReturnsNotModified(t *testing.T) {
	m := newTestModule(t)
	m.Start(context.Background())
	defer m.Stop(context.Background())
	fpath := newValidatorTestFile(t)

	w1 := httptest.NewRecorder()
	r1 := httptest.NewRequest("GET", testStream, nil)
	if err := m.Stream(w1, r1, StreamRequest{Path: fpath, MediaID: "media-1"}); err != nil {
		t.Fatalf("Stream() error: %v", err)
	}
	lastMod := w1.Header().Get("Last-Modified")
	if lastMod == "" {
		t.Fatal("expected a Last-Modified from the first request")
	}

	w2 := httptest.NewRecorder()
	r2 := httptest.NewRequest("GET", testStream, nil)
	r2.Header.Set("If-Modified-Since", lastMod)
	if err := m.Stream(w2, r2, StreamRequest{Path: fpath, MediaID: "media-1"}); err != nil {
		t.Fatalf("Stream() error: %v", err)
	}
	if w2.Code != http.StatusNotModified {
		t.Errorf("status = %d, want %d", w2.Code, http.StatusNotModified)
	}
}

func TestStream_IfRangeMatch_ServesPartialContent(t *testing.T) {
	m := newTestModule(t)
	m.Start(context.Background())
	defer m.Stop(context.Background())
	fpath := newValidatorTestFile(t)

	w1 := httptest.NewRecorder()
	r1 := httptest.NewRequest("GET", testStream, nil)
	if err := m.Stream(w1, r1, StreamRequest{Path: fpath, MediaID: "media-1"}); err != nil {
		t.Fatalf("Stream() error: %v", err)
	}
	etag := w1.Header().Get("ETag")

	w2 := httptest.NewRecorder()
	r2 := httptest.NewRequest("GET", testStream, nil)
	r2.Header.Set("Range", "bytes=0-9")
	r2.Header.Set("If-Range", etag)
	if err := m.Stream(w2, r2, StreamRequest{Path: fpath, MediaID: "media-1", RangeHeader: "bytes=0-9"}); err != nil {
		t.Fatalf("Stream() error: %v", err)
	}
	if w2.Code != http.StatusPartialContent {
		t.Errorf("status = %d, want 206 when If-Range matches", w2.Code)
	}
	if w2.Body.Len() != 10 {
		t.Errorf("body length = %d, want 10", w2.Body.Len())
	}
}

func TestStream_IfRangeMismatch_FallsBackToFullContent(t *testing.T) {
	m := newTestModule(t)
	m.Start(context.Background())
	defer m.Stop(context.Background())
	fpath := newValidatorTestFile(t)

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", testStream, nil)
	r.Header.Set("Range", "bytes=0-9")
	r.Header.Set("If-Range", `W/"stale-etag-does-not-match"`)
	if err := m.Stream(w, r, StreamRequest{Path: fpath, MediaID: "media-1", RangeHeader: "bytes=0-9"}); err != nil {
		t.Fatalf("Stream() error: %v", err)
	}
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200 (full content) when If-Range does not match", w.Code)
	}
	if w.Body.Len() != 500 {
		t.Errorf("body length = %d, want 500 (full file)", w.Body.Len())
	}
	if w.Header().Get("Content-Range") != "" {
		t.Error("Content-Range should not be set when falling back to a full response")
	}
}

// ---------------------------------------------------------------------------
// Package-level validator helpers
// ---------------------------------------------------------------------------

func TestComputeValidators_ZeroModTimeSkipsValidators(t *testing.T) {
	etag, hasModTime := computeValidators(100, time.Time{})
	if etag != "" || hasModTime {
		t.Errorf("computeValidators with zero mtime = (%q, %v), want (\"\", false)", etag, hasModTime)
	}
}

func TestComputeValidators_StableAcrossCalls(t *testing.T) {
	mt := time.Now()
	e1, ok1 := computeValidators(1234, mt)
	e2, ok2 := computeValidators(1234, mt)
	if !ok1 || !ok2 || e1 != e2 || e1 == "" {
		t.Errorf("computeValidators should be stable for identical size/mtime: (%q,%v) vs (%q,%v)", e1, ok1, e2, ok2)
	}

	e3, _ := computeValidators(1235, mt)
	if e3 == e1 {
		t.Error("computeValidators should differ when size changes")
	}
}

func TestEtagMatch(t *testing.T) {
	const etag = `W/"64-1a2b3c"`
	tests := []struct {
		header string
		want   bool
	}{
		{"", false},
		{"*", true},
		{etag, true},
		{`"64-1a2b3c"`, true}, // weak-comparison: W/ prefix ignored on either side
		{`W/"other"`, false},
		{`W/"other", ` + etag, true},
	}
	for _, tc := range tests {
		if got := etagMatch(tc.header, etag); got != tc.want {
			t.Errorf("etagMatch(%q, %q) = %v, want %v", tc.header, etag, got, tc.want)
		}
	}
}
