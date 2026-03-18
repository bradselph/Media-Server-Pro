package upload

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"media-server-pro/internal/config"
)

// ---------------------------------------------------------------------------
// Type constants
// ---------------------------------------------------------------------------

func TestUploadStatusConstants(t *testing.T) {
	if UploadStatusUploading != "uploading" {
		t.Errorf("UploadStatusUploading = %q", UploadStatusUploading)
	}
	if UploadStatusCompleted != "completed" {
		t.Errorf("UploadStatusCompleted = %q", UploadStatusCompleted)
	}
	if UploadStatusFailed != "failed" {
		t.Errorf("UploadStatusFailed = %q", UploadStatusFailed)
	}
}

func TestMediaTypeConstants(t *testing.T) {
	if MediaTypeVideo != "video" {
		t.Errorf("MediaTypeVideo = %q", MediaTypeVideo)
	}
	if MediaTypeAudio != "audio" {
		t.Errorf("MediaTypeAudio = %q", MediaTypeAudio)
	}
	if MediaTypeUnknown != "unknown" {
		t.Errorf("MediaTypeUnknown = %q", MediaTypeUnknown)
	}
}

// ---------------------------------------------------------------------------
// resolveMediaType
// ---------------------------------------------------------------------------

func TestResolveMediaType_Video(t *testing.T) {
	videoExts := []string{".mp4", ".mkv", ".avi", ".mov", ".wmv", ".flv", ".webm", ".m4v", ".ts"}
	for _, ext := range videoExts {
		if got := resolveMediaType(ext); got != MediaTypeVideo {
			t.Errorf("resolveMediaType(%q) = %q, want %q", ext, got, MediaTypeVideo)
		}
	}
}

func TestResolveMediaType_Audio(t *testing.T) {
	audioExts := []string{".mp3", ".wav", ".flac", ".aac", ".ogg", ".m4a", ".wma", ".opus"}
	for _, ext := range audioExts {
		if got := resolveMediaType(ext); got != MediaTypeAudio {
			t.Errorf("resolveMediaType(%q) = %q, want %q", ext, got, MediaTypeAudio)
		}
	}
}

func TestResolveMediaType_Unknown(t *testing.T) {
	unknownExts := []string{".txt", ".pdf", ".exe", ".jpg", ""}
	for _, ext := range unknownExts {
		if got := resolveMediaType(ext); got != MediaTypeUnknown {
			t.Errorf("resolveMediaType(%q) = %q, want %q", ext, got, MediaTypeUnknown)
		}
	}
}

// ---------------------------------------------------------------------------
// containsPathTraversal
// ---------------------------------------------------------------------------

func TestContainsPathTraversal(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"video.mp4", false},
		{"my_file.mkv", false},
		{"../etc/passwd", true},
		{"path/to/file", true},
		{`path\to\file`, true},
		{"..", true},
		{"normal", false},
	}
	for _, tc := range tests {
		got := containsPathTraversal(tc.input)
		if got != tc.want {
			t.Errorf("containsPathTraversal(%q) = %v, want %v", tc.input, got, tc.want)
		}
	}
}

// ---------------------------------------------------------------------------
// isEmptyOrSpecialFilename
// ---------------------------------------------------------------------------

func TestIsEmptyOrSpecialFilename(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"", true},
		{".", true},
		{"..", true},
		{"valid.mp4", false},
		{"a", false},
		{" ", false},
	}
	for _, tc := range tests {
		got := isEmptyOrSpecialFilename(tc.input)
		if got != tc.want {
			t.Errorf("isEmptyOrSpecialFilename(%q) = %v, want %v", tc.input, got, tc.want)
		}
	}
}

// ---------------------------------------------------------------------------
// sanitizeFilename
// ---------------------------------------------------------------------------

func newTestModule(t *testing.T) *Module {
	t.Helper()
	cfg := config.NewManager(filepath.Join(t.TempDir(), "config.json"))
	return NewModule(cfg)
}

func TestSanitizeFilename_Valid(t *testing.T) {
	m := newTestModule(t)
	got, err := m.sanitizeFilename("video.mp4")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "video.mp4" {
		t.Errorf("sanitizeFilename(video.mp4) = %q", got)
	}
}

func TestSanitizeFilename_Hidden(t *testing.T) {
	m := newTestModule(t)
	_, err := m.sanitizeFilename(".hidden")
	if err == nil {
		t.Error("hidden files should be rejected")
	}
}

func TestSanitizeFilename_PathTraversal(t *testing.T) {
	m := newTestModule(t)
	// filepath.Base strips directory components, so "../etc/passwd" becomes "passwd"
	got, err := m.sanitizeFilename("../etc/passwd")
	if err != nil {
		// Depending on OS, filepath.Base may strip to just "passwd"
		t.Logf("got error (acceptable): %v", err)
		return
	}
	if strings.Contains(got, "..") {
		t.Error("path traversal should be rejected")
	}
}

func TestSanitizeFilename_Empty(t *testing.T) {
	m := newTestModule(t)
	_, err := m.sanitizeFilename("")
	if err == nil {
		t.Error("empty filename should be rejected")
	}
}

func TestSanitizeFilename_DangerousChars(t *testing.T) {
	m := newTestModule(t)
	_, err := m.sanitizeFilename("file<>name.mp4")
	if err == nil {
		t.Error("filename with dangerous chars should be rejected")
	}
}

func TestSanitizeFilename_LongName(t *testing.T) {
	m := newTestModule(t)
	longName := strings.Repeat("a", 300) + ".mp4"
	got, err := m.sanitizeFilename(longName)
	if err != nil {
		t.Fatalf("long filename should be truncated, not rejected: %v", err)
	}
	if len(got) > 255 {
		t.Errorf("truncated filename should be <= 255 chars, got %d", len(got))
	}
	if !strings.HasSuffix(got, ".mp4") {
		t.Error("truncated filename should preserve extension")
	}
}

// ---------------------------------------------------------------------------
// sanitizeCategory
// ---------------------------------------------------------------------------

func TestSanitizeCategory_Valid(t *testing.T) {
	m := newTestModule(t)
	got := m.sanitizeCategory("movies")
	if got != "movies" {
		t.Errorf("sanitizeCategory(movies) = %q", got)
	}
}

func TestSanitizeCategory_Empty(t *testing.T) {
	m := newTestModule(t)
	got := m.sanitizeCategory("")
	if got != "uncategorized" {
		t.Errorf("sanitizeCategory('') = %q, want 'uncategorized'", got)
	}
}

func TestSanitizeCategory_PathTraversal(t *testing.T) {
	m := newTestModule(t)
	got := m.sanitizeCategory("../../etc")
	if strings.Contains(got, "..") {
		t.Errorf("category should strip path traversal: %q", got)
	}
}

func TestSanitizeCategory_SpecialChars(t *testing.T) {
	m := newTestModule(t)
	got := m.sanitizeCategory("cat<egory>")
	if strings.ContainsAny(got, "<>") {
		t.Errorf("category should strip dangerous chars: %q", got)
	}
}

// ---------------------------------------------------------------------------
// validateUploadSize
// ---------------------------------------------------------------------------

func TestValidateUploadSize_WithinLimit(t *testing.T) {
	cfg := &config.Config{}
	cfg.Uploads.MaxFileSize = 1024 * 1024 // 1MB
	err := validateUploadSize(cfg, 512*1024)
	if err != nil {
		t.Errorf("size within limit should pass: %v", err)
	}
}

func TestValidateUploadSize_ExceedsLimit(t *testing.T) {
	cfg := &config.Config{}
	cfg.Uploads.MaxFileSize = 1024
	err := validateUploadSize(cfg, 2048)
	if err == nil {
		t.Error("size exceeding limit should fail")
	}
}

func TestValidateUploadSize_NoLimit(t *testing.T) {
	cfg := &config.Config{}
	cfg.Uploads.MaxFileSize = 0 // no limit
	err := validateUploadSize(cfg, 1024*1024*1024)
	if err != nil {
		t.Errorf("zero limit should allow any size: %v", err)
	}
}

func TestValidateUploadSize_ExactLimit(t *testing.T) {
	cfg := &config.Config{}
	cfg.Uploads.MaxFileSize = 1024
	err := validateUploadSize(cfg, 1024)
	if err != nil {
		t.Errorf("size at exact limit should pass: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Module lifecycle
// ---------------------------------------------------------------------------

func TestModuleName(t *testing.T) {
	m := newTestModule(t)
	if m.Name() != "upload" {
		t.Errorf("Name() = %q, want %q", m.Name(), "upload")
	}
}

func TestModuleStartStop(t *testing.T) {
	dir := t.TempDir()
	cfg := config.NewManager(filepath.Join(dir, "config.json"))
	cfg.Update(func(c *config.Config) {
		c.Directories.Uploads = filepath.Join(dir, "uploads")
	})

	m := NewModule(cfg)
	m.uploadDir = filepath.Join(dir, "uploads")

	if err := m.Start(context.TODO()); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	h := m.Health()
	if h.Status != "healthy" {
		t.Errorf("after Start, status = %q, want healthy", h.Status)
	}

	if err := m.Stop(context.TODO()); err != nil {
		t.Fatalf("Stop failed: %v", err)
	}

	h = m.Health()
	if h.Status != "unhealthy" {
		t.Errorf("after Stop, status = %q, want unhealthy", h.Status)
	}
}

// ---------------------------------------------------------------------------
// generateUploadID
// ---------------------------------------------------------------------------

func TestGenerateUploadID_Unique(t *testing.T) {
	m := newTestModule(t)
	id1 := m.generateUploadID()
	id2 := m.generateUploadID()
	if id1 == id2 {
		t.Error("two generated IDs should be different")
	}
	if len(id1) == 0 {
		t.Error("generated ID should not be empty")
	}
}

// ---------------------------------------------------------------------------
// Progress tracking
// ---------------------------------------------------------------------------

func TestRegisterAndGetProgress(t *testing.T) {
	m := newTestModule(t)
	m.activeUploads = make(map[UploadID]*Progress)

	id := UploadID("test-upload-1")
	m.registerUploadProgress(ProgressRegistration{
		UploadID: id,
		Filename: "video.mp4",
		UserID:   "user1",
		Size:     1024,
	})

	p, ok := m.GetProgress(id)
	if !ok {
		t.Fatal("progress should be found")
	}
	if p.Filename != "video.mp4" {
		t.Errorf("filename = %q, want %q", p.Filename, "video.mp4")
	}
	if p.Status != UploadStatusUploading {
		t.Errorf("status = %q, want %q", p.Status, UploadStatusUploading)
	}
}

func TestGetProgress_NotFound(t *testing.T) {
	m := newTestModule(t)
	m.activeUploads = make(map[UploadID]*Progress)
	_, ok := m.GetProgress("nonexistent")
	if ok {
		t.Error("nonexistent upload should not be found")
	}
}

func TestGetActiveUploads(t *testing.T) {
	m := newTestModule(t)
	m.activeUploads = make(map[UploadID]*Progress)

	m.registerUploadProgress(ProgressRegistration{UploadID: "u1", Filename: "a.mp4", UserID: "u"})
	m.registerUploadProgress(ProgressRegistration{UploadID: "u2", Filename: "b.mp4", UserID: "u"})

	uploads := m.GetActiveUploads()
	if len(uploads) != 2 {
		t.Errorf("expected 2 active uploads, got %d", len(uploads))
	}
}

// ---------------------------------------------------------------------------
// createUniqueUploadFile
// ---------------------------------------------------------------------------

func TestCreateUniqueUploadFile_NoConflict(t *testing.T) {
	dir := t.TempDir()
	m := newTestModule(t)

	destPath, f, err := m.createUniqueUploadFile(dir, "video.mp4")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer f.Close()

	if !strings.HasSuffix(destPath, "video.mp4") {
		t.Errorf("destPath should end with video.mp4: %s", destPath)
	}
}

func TestCreateUniqueUploadFile_WithConflict(t *testing.T) {
	dir := t.TempDir()
	m := newTestModule(t)

	// Create existing temp file to cause conflict
	os.WriteFile(filepath.Join(dir, "video.mp4.tmp"), []byte("existing"), 0644)

	destPath, f, err := m.createUniqueUploadFile(dir, "video.mp4")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer f.Close()

	if destPath == filepath.Join(dir, "video.mp4") {
		t.Error("should have generated a numbered variant")
	}
	if !strings.Contains(destPath, "video_1.mp4") {
		t.Errorf("expected numbered variant, got: %s", destPath)
	}
}

// ---------------------------------------------------------------------------
// GetUserStorageUsed
// ---------------------------------------------------------------------------

func TestGetUserStorageUsed_EmptyDir(t *testing.T) {
	dir := t.TempDir()
	m := newTestModule(t)
	m.uploadDir = dir

	size, err := m.GetUserStorageUsed("user1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if size != 0 {
		t.Errorf("empty dir should have 0 bytes, got %d", size)
	}
}

func TestGetUserStorageUsed_WithFiles(t *testing.T) {
	dir := t.TempDir()
	userDir := filepath.Join(dir, "user1")
	os.MkdirAll(userDir, 0755)
	os.WriteFile(filepath.Join(userDir, "file1.mp4"), make([]byte, 1024), 0644)
	os.WriteFile(filepath.Join(userDir, "file2.mp4"), make([]byte, 2048), 0644)

	m := newTestModule(t)
	m.uploadDir = dir

	size, err := m.GetUserStorageUsed("user1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if size != 3072 {
		t.Errorf("expected 3072 bytes, got %d", size)
	}
}

// ---------------------------------------------------------------------------
// CheckQuota
// ---------------------------------------------------------------------------

func TestCheckQuota_NoLimit(t *testing.T) {
	m := newTestModule(t)
	m.uploadDir = t.TempDir()

	ok, err := m.CheckQuota("user1", 1024*1024, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Error("zero quota should allow any size")
	}
}

func TestCheckQuota_WithinQuota(t *testing.T) {
	dir := t.TempDir()
	m := newTestModule(t)
	m.uploadDir = dir

	ok, err := m.CheckQuota("user1", 1024, 2048)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Error("should be within quota")
	}
}

func TestCheckQuota_ExceedsQuota(t *testing.T) {
	dir := t.TempDir()
	userDir := filepath.Join(dir, "user1")
	os.MkdirAll(userDir, 0755)
	os.WriteFile(filepath.Join(userDir, "existing.mp4"), make([]byte, 1024), 0644)

	m := newTestModule(t)
	m.uploadDir = dir

	ok, err := m.CheckQuota("user1", 1024, 1500)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Error("should exceed quota")
	}
}

// ---------------------------------------------------------------------------
// isAllowedExtension
// ---------------------------------------------------------------------------

func TestIsAllowedExtension_Video(t *testing.T) {
	m := newTestModule(t)
	if !m.isAllowedExtension(".mp4") {
		t.Error(".mp4 should be allowed")
	}
}

func TestIsAllowedExtension_Audio(t *testing.T) {
	m := newTestModule(t)
	if !m.isAllowedExtension(".mp3") {
		t.Error(".mp3 should be allowed")
	}
}

func TestIsAllowedExtension_Rejected(t *testing.T) {
	m := newTestModule(t)
	if m.isAllowedExtension(".exe") {
		t.Error(".exe should not be allowed")
	}
}

// ---------------------------------------------------------------------------
// buildUploadDestDir
// ---------------------------------------------------------------------------

func TestBuildUploadDestDir_Valid(t *testing.T) {
	m := newTestModule(t)
	m.uploadDir = "/uploads"

	dir, err := m.buildUploadDestDir(UploadScope{UserID: "user1"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(dir, "user1") {
		t.Errorf("dir should contain user ID: %s", dir)
	}
}

func TestBuildUploadDestDir_WithCategory(t *testing.T) {
	m := newTestModule(t)
	m.uploadDir = "/uploads"

	dir, err := m.buildUploadDestDir(UploadScope{UserID: "user1", Category: "movies"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(dir, "movies") {
		t.Errorf("dir should contain category: %s", dir)
	}
}

func TestBuildUploadDestDir_InvalidUserID(t *testing.T) {
	m := newTestModule(t)
	m.uploadDir = "/uploads"

	_, err := m.buildUploadDestDir(UploadScope{UserID: ".."})
	if err == nil {
		t.Error("should reject '..' as user ID")
	}
}
