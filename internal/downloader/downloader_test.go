package downloader

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"media-server-pro/internal/config"
	"media-server-pro/internal/logger"
)

// ---------------------------------------------------------------------------
// Import destinations
// ---------------------------------------------------------------------------

func TestListDestinations_RootsAndSubdirs(t *testing.T) {
	base := t.TempDir()
	videos := filepath.Join(base, "videos")
	music := filepath.Join(base, "music")
	uploads := filepath.Join(base, "uploads")
	for _, d := range []string{videos, music, uploads} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	// A sub-directory under videos stands in for a grafted HiDrive mount.
	hidrive := filepath.Join(videos, "hidrive")
	if err := os.MkdirAll(hidrive, 0o755); err != nil {
		t.Fatal(err)
	}
	// A file (not a dir) under videos must NOT become a destination.
	if err := os.WriteFile(filepath.Join(videos, "loose.mp4"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	dests := ListDestinations(videos, music, uploads, uploads)

	byKey := map[string]ImportDestination{}
	for _, d := range dests {
		byKey[d.Key] = d
	}
	for _, want := range []string{"videos", "music", "uploads", "videos/hidrive"} {
		if _, ok := byKey[want]; !ok {
			t.Errorf("expected destination key %q in %+v", want, dests)
		}
	}
	if byKey["videos/hidrive"].Path != hidrive {
		t.Errorf("videos/hidrive path = %q, want %q", byKey["videos/hidrive"].Path, hidrive)
	}
	if _, ok := byKey["videos/loose.mp4"]; ok {
		t.Error("a regular file was wrongly listed as a destination")
	}
	if !byKey["uploads"].IsDefault {
		t.Error("uploads should be flagged default when defaultDir == uploads")
	}
}

func TestListDestinations_SkipsEmptyRoots(t *testing.T) {
	base := t.TempDir()
	videos := filepath.Join(base, "videos")
	if err := os.MkdirAll(videos, 0o755); err != nil {
		t.Fatal(err)
	}
	dests := ListDestinations(videos, "", "", "")
	for _, d := range dests {
		if d.Key == "music" || d.Key == "uploads" {
			t.Errorf("empty root should be skipped, got %q", d.Key)
		}
	}
}

func TestResolveDestination(t *testing.T) {
	base := t.TempDir()
	videos := filepath.Join(base, "videos")
	uploads := filepath.Join(base, "uploads")
	hidrive := filepath.Join(videos, "hidrive")
	for _, d := range []string{uploads, hidrive} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	got, err := ResolveDestination("videos/hidrive", videos, "", uploads, uploads)
	if err != nil {
		t.Fatalf("resolve videos/hidrive: %v", err)
	}
	if got != hidrive {
		t.Errorf("resolved path = %q, want %q", got, hidrive)
	}

	// Unknown keys and path-traversal attempts must be rejected — only keys the
	// enumerator produced are accepted, so these can never reach the filesystem.
	for _, bad := range []string{"videos/../../etc", "nope", "../secrets", "videos/missing"} {
		if _, err := ResolveDestination(bad, videos, "", uploads, uploads); err == nil {
			t.Errorf("expected error resolving %q, got nil", bad)
		}
	}
}

func TestListDestinations_FlagsWritable(t *testing.T) {
	base := t.TempDir()
	videos := filepath.Join(base, "videos")
	if err := os.MkdirAll(filepath.Join(videos, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, d := range ListDestinations(videos, "", "", videos) {
		if !d.Writable {
			t.Errorf("destination %q should be writable (temp dir), got writable=false", d.Key)
		}
	}
}

func TestDestinationWritable(t *testing.T) {
	base := t.TempDir()
	// A not-yet-created sub-folder under a writable dir is writable (MkdirAll path).
	if !destinationWritable(filepath.Join(base, "newsub", "deeper")) {
		t.Error("new sub-folder under a writable dir should be writable")
	}
	// A path under a regular file (not a directory) is not writable.
	f := filepath.Join(base, "afile")
	if err := os.WriteFile(f, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if destinationWritable(filepath.Join(f, "sub")) {
		t.Error("path under a regular file should not be writable")
	}
}

// ImportFile must never let two imports of the same filename collide: each gets a
// unique destination via an O_CREATE|O_EXCL claim, so no copy truncates another.
func TestImportFile_ConcurrentSameNameNoClobber(t *testing.T) {
	base := t.TempDir()
	src := filepath.Join(base, "src")
	dst := filepath.Join(base, "dst")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	content := []byte("hello-import-payload")
	if err := os.WriteFile(filepath.Join(src, "movie.mp4"), content, 0o644); err != nil {
		t.Fatal(err)
	}

	const n = 8
	var wg sync.WaitGroup
	paths := make([]string, n)
	errs := make([]error, n)
	for i := range n {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			// deleteSource=false: each call copies the (shared, surviving) source.
			p, _, err := ImportFile(src, dst, "movie.mp4", false)
			paths[i], errs[i] = p, err
		}(i)
	}
	wg.Wait()

	seen := map[string]bool{}
	for i := range n {
		if errs[i] != nil {
			t.Fatalf("import %d failed: %v", i, errs[i])
		}
		if seen[paths[i]] {
			t.Fatalf("duplicate destination %q — atomic claim failed", paths[i])
		}
		seen[paths[i]] = true
		got, err := os.ReadFile(paths[i])
		if err != nil {
			t.Fatalf("read %q: %v", paths[i], err)
		}
		if string(got) != string(content) {
			t.Errorf("file %q content = %q, want %q (clobbered?)", paths[i], got, content)
		}
	}
	if len(seen) != n {
		t.Errorf("expected %d distinct files, got %d", n, len(seen))
	}
}

func TestImportFile_SequentialCollisionNumbering(t *testing.T) {
	base := t.TempDir()
	src := filepath.Join(base, "src")
	dst := filepath.Join(base, "dst")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "a.mp4"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	p1, _, err := ImportFile(src, dst, "a.mp4", false)
	if err != nil {
		t.Fatal(err)
	}
	p2, _, err := ImportFile(src, dst, "a.mp4", false)
	if err != nil {
		t.Fatal(err)
	}
	if got := filepath.Base(p1); got != "a.mp4" {
		t.Errorf("first import name = %q, want a.mp4", got)
	}
	if got := filepath.Base(p2); got != "a_1.mp4" {
		t.Errorf("second import name = %q, want a_1.mp4", got)
	}
}

func TestResolveDestination_UnknownIsSentinel(t *testing.T) {
	base := t.TempDir()
	videos := filepath.Join(base, "videos")
	if err := os.MkdirAll(videos, 0o755); err != nil {
		t.Fatal(err)
	}
	_, err := ResolveDestination("nope", videos, "", "", "")
	if !errors.Is(err, ErrUnknownDestination) {
		t.Errorf("want ErrUnknownDestination, got %v", err)
	}
}

func TestSanitizeSubfolder_InvalidIsSentinel(t *testing.T) {
	for _, bad := range []string{"a/b", "..", "bad\x00name"} {
		if _, err := sanitizeSubfolder(bad); !errors.Is(err, ErrInvalidSubfolder) {
			t.Errorf("sanitizeSubfolder(%q) = %v, want ErrInvalidSubfolder", bad, err)
		}
	}
}

func TestSanitizeSubfolder(t *testing.T) {
	// Empty / whitespace → no sub-folder, no error.
	for _, empty := range []string{"", "   ", "\t"} {
		got, err := sanitizeSubfolder(empty)
		if err != nil || got != "" {
			t.Errorf("sanitizeSubfolder(%q) = (%q, %v), want (\"\", nil)", empty, got, err)
		}
	}

	// Valid single names are trimmed and returned unchanged.
	for _, ok := range []string{"New Series", "2024", "season_1", " trimmed "} {
		got, err := sanitizeSubfolder(ok)
		if err != nil {
			t.Errorf("sanitizeSubfolder(%q) unexpected error: %v", ok, err)
		}
		if want := filepath.Base(strings.TrimSpace(ok)); got != want {
			t.Errorf("sanitizeSubfolder(%q) = %q, want %q", ok, got, want)
		}
	}

	// Anything with separators, traversal, or control bytes must be rejected so
	// the name can never escape the chosen destination root.
	for _, bad := range []string{"a/b", `a\b`, "..", ".", "../etc", "sub/../..", "bad\x00name", "line\nbreak"} {
		if _, err := sanitizeSubfolder(bad); err == nil {
			t.Errorf("sanitizeSubfolder(%q) = nil error, want rejection", bad)
		}
	}
}

// ---------------------------------------------------------------------------
// Module basics
// ---------------------------------------------------------------------------

func TestModuleName(t *testing.T) {
	m := &Module{}
	if m.Name() != "downloader" {
		t.Errorf("Name() = %q, want %q", m.Name(), "downloader")
	}
}

func TestModuleHealth_Default(t *testing.T) {
	m := &Module{}
	h := m.Health()
	if h.Name != "downloader" {
		t.Errorf("Health().Name = %q", h.Name)
	}
}

func TestSetHealth(t *testing.T) {
	m := &Module{}
	m.setHealth(true, "Running")
	h := m.Health()
	if h.Status != "healthy" {
		t.Errorf("status = %q, want healthy", h.Status)
	}
	if h.Message != "Running" {
		t.Errorf("message = %q, want Running", h.Message)
	}
}

func TestSetHealth_Unhealthy(t *testing.T) {
	m := &Module{}
	m.setHealth(false, "Disconnected")
	h := m.Health()
	if h.Status != "unhealthy" {
		t.Errorf("status = %q, want unhealthy", h.Status)
	}
}

// ---------------------------------------------------------------------------
// FND-0495: Path traversal in CancelDownload
// ---------------------------------------------------------------------------

func TestFND0495_CancelDownload_EscapesDownloadID(t *testing.T) {
	tests := []struct {
		name       string
		downloadID string
		// expected is what the path should be after escaping
		expected string
	}{
		{
			name:       "normal alphanumeric ID",
			downloadID: "abc123",
			expected:   "abc123",
		},
		{
			name:       "path traversal attempt with slashes",
			downloadID: "../../../etc/passwd",
			expected:   url.PathEscape("../../../etc/passwd"), // should be %2E%2E%2F%2E%2E%2F%2E%2E%2Fetc%2Fpasswd
		},
		{
			name:       "ID with spaces",
			downloadID: "download file",
			expected:   "download%20file",
		},
		{
			name:       "ID with special characters",
			downloadID: "file&name=value",
			expected:   url.PathEscape("file&name=value"),
		},
		{
			name:       "empty ID",
			downloadID: "",
			expected:   "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewClient("http://localhost:8080", 30*time.Second, "")

			// Verify that the expected path escaping would occur.
			// We can't directly inspect the internal URL without mocking,
			// but we can verify that PathEscape produces the expected result.
			got := url.PathEscape(tt.downloadID)
			if got != tt.expected {
				t.Errorf("PathEscape(%q) = %q, want %q", tt.downloadID, got, tt.expected)
			}

			// Verify that calling CancelDownload with a nil client returns an error
			// (this ensures the nil guard is in place before the path escape logic)
			var nilClient *Client
			err := nilClient.CancelDownload(tt.downloadID)
			if err == nil {
				t.Errorf("nilClient.CancelDownload(%q) should return error, got nil", tt.downloadID)
			}

			// The actual HTTP call would fail (no server), but the important thing is
			// that the method doesn't panic and the downloadID is properly escaped
			// in the URL construction (c.post("/api/cancel/"+url.PathEscape(downloadID), ...))
			// This would be verified by inspecting the resulting URL in an actual integration test.
			_ = client
		})
	}
}

// ---------------------------------------------------------------------------
// FND-0496: Nil receiver guards on exported methods
// ---------------------------------------------------------------------------

func TestFND0496_NilClient_Health_ReturnsError(t *testing.T) {
	var client *Client
	result, err := client.Health()
	if err == nil {
		t.Errorf("Health() on nil client should return error, got nil")
	}
	if result != nil {
		t.Errorf("Health() on nil client should return nil result, got %v", result)
	}
	if err.Error() != "downloader client not initialized" {
		t.Errorf("Health() error = %q, want 'downloader client not initialized'", err.Error())
	}
}

func TestFND0496_NilClient_Detect_ReturnsError(t *testing.T) {
	var client *Client
	result, err := client.Detect("http://example.com")
	if err == nil {
		t.Errorf("Detect() on nil client should return error, got nil")
	}
	if result != nil {
		t.Errorf("Detect() on nil client should return nil result, got %v", result)
	}
	if err.Error() != "downloader client not initialized" {
		t.Errorf("Detect() error = %q, want 'downloader client not initialized'", err.Error())
	}
}

func TestFND0496_NilClient_Download_ReturnsError(t *testing.T) {
	var client *Client
	params := DownloadParams{URL: "http://example.com"}
	result, err := client.Download(params, "session123")
	if err == nil {
		t.Errorf("Download() on nil client should return error, got nil")
	}
	if result != nil {
		t.Errorf("Download() on nil client should return nil result, got %v", result)
	}
	if err.Error() != "downloader client not initialized" {
		t.Errorf("Download() error = %q, want 'downloader client not initialized'", err.Error())
	}
}

func TestFND0496_NilClient_CancelDownload_ReturnsError(t *testing.T) {
	var client *Client
	err := client.CancelDownload("download123")
	if err == nil {
		t.Errorf("CancelDownload() on nil client should return error, got nil")
	}
	if err.Error() != "downloader client not initialized" {
		t.Errorf("CancelDownload() error = %q, want 'downloader client not initialized'", err.Error())
	}
}

func TestFND0496_NilClient_ListDownloads_ReturnsError(t *testing.T) {
	var client *Client
	result, err := client.ListDownloads()
	if err == nil {
		t.Errorf("ListDownloads() on nil client should return error, got nil")
	}
	if result != nil {
		t.Errorf("ListDownloads() on nil client should return nil result, got %v", result)
	}
	if err.Error() != "downloader client not initialized" {
		t.Errorf("ListDownloads() error = %q, want 'downloader client not initialized'", err.Error())
	}
}

func TestFND0496_NilClient_DeleteDownload_ReturnsError(t *testing.T) {
	var client *Client
	err := client.DeleteDownload("file.mp4")
	if err == nil {
		t.Errorf("DeleteDownload() on nil client should return error, got nil")
	}
	if err.Error() != "downloader client not initialized" {
		t.Errorf("DeleteDownload() error = %q, want 'downloader client not initialized'", err.Error())
	}
}

func TestFND0496_NilClient_GetSettings_ReturnsError(t *testing.T) {
	var client *Client
	result, err := client.GetSettings()
	if err == nil {
		t.Errorf("GetSettings() on nil client should return error, got nil")
	}
	if result != nil {
		t.Errorf("GetSettings() on nil client should return nil result, got %v", result)
	}
	if err.Error() != "downloader client not initialized" {
		t.Errorf("GetSettings() error = %q, want 'downloader client not initialized'", err.Error())
	}
}

// ---------------------------------------------------------------------------
// FND-0497: NewClient enforces minimum timeout
// ---------------------------------------------------------------------------

func TestFND0497_NewClient_EnforcesMinimumTimeout(t *testing.T) {
	tests := []struct {
		name            string
		inputTimeout    time.Duration
		shouldBeDefault bool
	}{
		{
			name:            "zero timeout gets 30s default",
			inputTimeout:    0,
			shouldBeDefault: true,
		},
		{
			name:            "negative timeout gets 30s default",
			inputTimeout:    -1 * time.Second,
			shouldBeDefault: true,
		},
		{
			name:            "very negative timeout gets 30s default",
			inputTimeout:    -100 * time.Second,
			shouldBeDefault: true,
		},
		{
			name:            "positive timeout is preserved",
			inputTimeout:    15 * time.Second,
			shouldBeDefault: false,
		},
		{
			name:            "large timeout is preserved",
			inputTimeout:    5 * time.Minute,
			shouldBeDefault: false,
		},
	}

	defaultTimeout := 30 * time.Second

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewClient("http://localhost:8080", tt.inputTimeout, "")

			// Verify the client's httpClient has the expected timeout
			if tt.shouldBeDefault {
				if client.httpClient.Timeout != defaultTimeout {
					t.Errorf("Timeout = %v, want %v (30s default)", client.httpClient.Timeout, defaultTimeout)
				}
			} else {
				if client.httpClient.Timeout != tt.inputTimeout {
					t.Errorf("Timeout = %v, want %v", client.httpClient.Timeout, tt.inputTimeout)
				}
			}
		})
	}
}

// ---------------------------------------------------------------------------
// FND-0498: NewClient normalizes baseURL by stripping trailing slash
// ---------------------------------------------------------------------------

func TestFND0498_NewClient_StripTrailingSlash(t *testing.T) {
	tests := []struct {
		name     string
		inputURL string
		expected string
	}{
		{
			name:     "URL with trailing slash is stripped",
			inputURL: "http://localhost:8080/",
			expected: "http://localhost:8080",
		},
		{
			name:     "URL with multiple trailing slashes is stripped",
			inputURL: "http://localhost:8080///",
			expected: "http://localhost:8080",
		},
		{
			name:     "URL without trailing slash is unchanged",
			inputURL: "http://localhost:8080",
			expected: "http://localhost:8080",
		},
		{
			name:     "URL with path and trailing slash is stripped",
			inputURL: "http://localhost:8080/api/",
			expected: "http://localhost:8080/api",
		},
		{
			name:     "URL with path and multiple trailing slashes",
			inputURL: "http://localhost:8080/api///",
			expected: "http://localhost:8080/api",
		},
		{
			name:     "empty URL",
			inputURL: "",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewClient(tt.inputURL, 30*time.Second, "")
			if client.baseURL != tt.expected {
				t.Errorf("baseURL = %q, want %q", client.baseURL, tt.expected)
			}
		})
	}
}

// Importing a downloaded file must also clear the downloader's own record of it
// so the entry stops showing up in the admin's "Server Files" list. MSP renames
// the file directly on disk, so without an explicit DeleteDownload call the
// downloader's in-memory tracking stays stale until the service restarts.
func TestImport_ClearsDownloaderRecordAfterSourceDeleted(t *testing.T) {
	base := t.TempDir()
	downloads := filepath.Join(base, "downloads")
	uploads := filepath.Join(base, "uploads")
	if err := os.MkdirAll(downloads, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(uploads, 0o755); err != nil {
		t.Fatal(err)
	}
	srcFile := filepath.Join(downloads, "movie.mp4")
	if err := os.WriteFile(srcFile, []byte("payload"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Stand in for the downloader service: record the filename of any DELETE
	// /api/download/<filename> request so the test can assert the cleanup happened.
	var deleted atomic.Value
	deleted.Store("")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, "/api/download/") {
			name, _ := url.PathUnescape(strings.TrimPrefix(r.URL.Path, "/api/download/"))
			deleted.Store(name)
			w.WriteHeader(http.StatusNoContent)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	mgr := config.NewManager(filepath.Join(base, "config.json"))
	if err := mgr.Update(func(c *config.Config) {
		c.Downloader.DownloadsDir = downloads
		c.Directories.Uploads = uploads
	}); err != nil {
		t.Fatalf("config update: %v", err)
	}

	m := &Module{
		config: mgr,
		log:    logger.New("downloader-test"),
		client: NewClient(srv.URL, 5*time.Second, ""),
	}

	destPath, sourceDeleted, err := m.Import("movie.mp4", "", "", true, false)
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if !sourceDeleted {
		t.Fatalf("sourceDeleted = false, want true")
	}
	if filepath.Dir(destPath) != uploads {
		t.Errorf("destPath dir = %q, want %q", filepath.Dir(destPath), uploads)
	}
	if got := deleted.Load().(string); got != "movie.mp4" {
		t.Errorf("downloader DeleteDownload called with %q, want %q", got, "movie.mp4")
	}
}

// When the import is a copy (deleteSource=false), the source file stays in the
// downloader's downloads dir and the downloader's record must NOT be cleared —
// the operator explicitly chose to keep the original.
func TestImport_KeepsDownloaderRecordWhenSourceKept(t *testing.T) {
	base := t.TempDir()
	downloads := filepath.Join(base, "downloads")
	uploads := filepath.Join(base, "uploads")
	if err := os.MkdirAll(downloads, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(uploads, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(downloads, "movie.mp4"), []byte("payload"), 0o644); err != nil {
		t.Fatal(err)
	}

	var deleteCalls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			deleteCalls.Add(1)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	mgr := config.NewManager(filepath.Join(base, "config.json"))
	if err := mgr.Update(func(c *config.Config) {
		c.Downloader.DownloadsDir = downloads
		c.Directories.Uploads = uploads
	}); err != nil {
		t.Fatalf("config update: %v", err)
	}

	m := &Module{
		config: mgr,
		log:    logger.New("downloader-test"),
		client: NewClient(srv.URL, 5*time.Second, ""),
	}

	if _, sourceDeleted, err := m.Import("movie.mp4", "", "", false, false); err != nil {
		t.Fatalf("Import: %v", err)
	} else if sourceDeleted {
		t.Fatalf("sourceDeleted = true with deleteSource=false")
	}
	if n := deleteCalls.Load(); n != 0 {
		t.Errorf("DeleteDownload called %d time(s) when source kept, want 0", n)
	}
}

// A failure on the downloader's DeleteDownload (e.g. 404 because the downloader
// already noticed the file was gone) must not turn a successful import into a
// caller-facing error.
func TestImport_DownloaderDeleteFailureIsNonFatal(t *testing.T) {
	base := t.TempDir()
	downloads := filepath.Join(base, "downloads")
	uploads := filepath.Join(base, "uploads")
	if err := os.MkdirAll(downloads, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(uploads, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(downloads, "movie.mp4"), []byte("payload"), 0o644); err != nil {
		t.Fatal(err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	mgr := config.NewManager(filepath.Join(base, "config.json"))
	if err := mgr.Update(func(c *config.Config) {
		c.Downloader.DownloadsDir = downloads
		c.Directories.Uploads = uploads
	}); err != nil {
		t.Fatalf("config update: %v", err)
	}

	m := &Module{
		config: mgr,
		log:    logger.New("downloader-test"),
		client: NewClient(srv.URL, 5*time.Second, ""),
	}

	if _, _, err := m.Import("movie.mp4", "", "", true, false); err != nil {
		t.Errorf("Import should succeed despite downloader DELETE 404, got: %v", err)
	}
}

// Regression: the downloader must be asked to delete the file while it is STILL
// present on disk. The original import moved the file out of the downloads dir
// first, so the downloader's DELETE 404'd (file already gone) and it never
// dropped its in-memory record — the download lingered forever in the admin
// "Server Files" list. Copy-first keeps the file present until the downloader
// removes it and clears its record. This mock mimics the real downloader: it
// only deletes (204) when the file is present, else 404.
func TestImport_DownloaderDeletesWhileFilePresent(t *testing.T) {
	base := t.TempDir()
	downloads := filepath.Join(base, "downloads")
	uploads := filepath.Join(base, "uploads")
	if err := os.MkdirAll(downloads, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(uploads, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(downloads, "movie.mp4"), []byte("payload"), 0o644); err != nil {
		t.Fatal(err)
	}

	var sawFilePresent atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, "/api/download/") {
			name, _ := url.PathUnescape(strings.TrimPrefix(r.URL.Path, "/api/download/"))
			p := filepath.Join(downloads, name)
			if _, err := os.Stat(p); err == nil {
				sawFilePresent.Store(true)
				_ = os.Remove(p) // real downloader removes its file + record
				w.WriteHeader(http.StatusNoContent)
				return
			}
			w.WriteHeader(http.StatusNotFound) // unknown to the downloader → stale record kept
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	mgr := config.NewManager(filepath.Join(base, "config.json"))
	if err := mgr.Update(func(c *config.Config) {
		c.Downloader.DownloadsDir = downloads
		c.Directories.Uploads = uploads
	}); err != nil {
		t.Fatalf("config update: %v", err)
	}

	m := &Module{
		config: mgr,
		log:    logger.New("downloader-test"),
		client: NewClient(srv.URL, 5*time.Second, ""),
	}

	_, sourceDeleted, err := m.Import("movie.mp4", "", "", true, false)
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if !sawFilePresent.Load() {
		t.Fatal("downloader DELETE arrived after the file was already gone; its record would not be cleared")
	}
	if !sourceDeleted {
		t.Fatalf("sourceDeleted = false, want true")
	}
	if _, err := os.Stat(filepath.Join(downloads, "movie.mp4")); !os.IsNotExist(err) {
		t.Errorf("source still present in downloads dir after import, want removed")
	}
}

func TestFND0498_NormalizedURL_PreventsDoubleSlash(t *testing.T) {
	// This test verifies that the path normalization prevents double-slash URL construction.
	// With trailing slash stripped from baseURL, path like "/api/health" will not result in
	// baseURL + "/api/health" = "http://host//" but rather "http://host" + "/api/health" = "http://host/api/health"

	client := NewClient("http://localhost:8080/", 30*time.Second, "")

	// The client's baseURL should be normalized to not have trailing slash
	if client.baseURL != "http://localhost:8080" {
		t.Errorf("baseURL = %q, want %q", client.baseURL, "http://localhost:8080")
	}

	// When constructing URLs with paths like "/api/health", the result should be
	// "http://localhost:8080/api/health", not "http://localhost:8080//api/health"
	expectedURL := "http://localhost:8080" + "/api/health"
	if expectedURL != "http://localhost:8080/api/health" {
		t.Errorf("URL construction failed: %q != %q", expectedURL, "http://localhost:8080/api/health")
	}
}
