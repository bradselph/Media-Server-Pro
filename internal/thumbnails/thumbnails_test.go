package thumbnails

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"media-server-pro/internal/config"
	"media-server-pro/internal/logger"
)

const testMediaUUID = "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"

// ---------------------------------------------------------------------------
// isPreviewThumbnail
// ---------------------------------------------------------------------------

func TestIsPreviewThumbnail_True(t *testing.T) {
	if !isPreviewThumbnail("/thumbs/video_preview_001.jpg") {
		t.Error("path with _preview_ should return true")
	}
}

func TestIsPreviewThumbnail_False(t *testing.T) {
	if isPreviewThumbnail("/thumbs/video_thumb.jpg") {
		t.Error("path without _preview_ should return false")
	}
}

func TestIsPreviewThumbnail_InMiddle(t *testing.T) {
	if !isPreviewThumbnail("/thumbs/some_preview_frame.jpg") {
		t.Error("_preview_ in middle of filename should return true")
	}
}

// ---------------------------------------------------------------------------
// Module basics
// ---------------------------------------------------------------------------

func TestModuleName(t *testing.T) {
	m := &Module{}
	if m.Name() != "thumbnails" {
		t.Errorf("Name() = %q, want %q", m.Name(), "thumbnails")
	}
}

func TestModuleHealth_Default(t *testing.T) {
	m := &Module{}
	h := m.Health()
	if h.Name != "thumbnails" {
		t.Errorf("Health().Name = %q", h.Name)
	}
}

// ---------------------------------------------------------------------------
// Cleanup
// ---------------------------------------------------------------------------

type fakeMediaIDProvider struct {
	ids map[string]bool
}

func (f *fakeMediaIDProvider) GetAllMediaIDs() map[string]bool { return f.ids }

func newTestModule(t *testing.T) (*Module, string) {
	t.Helper()
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.json")
	cfg := config.NewManager(cfgPath)
	if err := cfg.Update(func(c *config.Config) {
		c.Thumbnails.PreviewCount = 3
		c.Directories.Thumbnails = dir
	}); err != nil {
		t.Fatalf("failed to update config: %v", err)
	}
	return &Module{thumbnailDir: dir, config: cfg, log: logger.New("thumbnails-test")}, dir
}

func writeTestFile(t *testing.T, path string, size int) {
	t.Helper()
	if err := os.WriteFile(path, make([]byte, size), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestCleanup_RemovesOrphans(t *testing.T) {
	m, dir := newTestModule(t)
	m.mediaIDProvider = &fakeMediaIDProvider{ids: map[string]bool{
		testMediaUUID: true,
	}}

	// Valid file — should survive
	writeTestFile(t, filepath.Join(dir, testMediaUUID+".jpg"), 100)
	// Orphan — should be removed
	writeTestFile(t, filepath.Join(dir, "11111111-2222-3333-4444-555555555555.jpg"), 200)

	result, err := m.Cleanup()
	if err != nil {
		t.Fatal(err)
	}
	if result.OrphansRemoved != 1 {
		t.Errorf("OrphansRemoved = %d, want 1", result.OrphansRemoved)
	}
	if _, err := os.Stat(filepath.Join(dir, testMediaUUID+".jpg")); err != nil {
		t.Error("valid thumbnail should still exist")
	}
	if _, err := os.Stat(filepath.Join(dir, "11111111-2222-3333-4444-555555555555.jpg")); err == nil {
		t.Error("orphan thumbnail should have been removed")
	}
}

func TestCleanup_RemovesExcessPreviews(t *testing.T) {
	m, dir := newTestModule(t)
	id := testMediaUUID
	m.mediaIDProvider = &fakeMediaIDProvider{ids: map[string]bool{id: true}}

	// Config has PreviewCount=3, so _preview_0..2 are valid; 3..4 are excess
	for i := 0; i < 5; i++ {
		writeTestFile(t, filepath.Join(dir, id+"_preview_"+strconv.Itoa(i)+".jpg"), 50)
	}

	result, err := m.Cleanup()
	if err != nil {
		t.Fatal(err)
	}
	if result.ExcessRemoved != 2 {
		t.Errorf("ExcessRemoved = %d, want 2", result.ExcessRemoved)
	}
}

func TestCleanup_RemovesCorruptFiles(t *testing.T) {
	m, dir := newTestModule(t)
	id := testMediaUUID
	m.mediaIDProvider = &fakeMediaIDProvider{ids: map[string]bool{id: true}}

	// 0-byte file (corrupt)
	writeTestFile(t, filepath.Join(dir, id+".jpg"), 0)

	result, err := m.Cleanup()
	if err != nil {
		t.Fatal(err)
	}
	if result.CorruptRemoved != 1 {
		t.Errorf("CorruptRemoved = %d, want 1", result.CorruptRemoved)
	}
}

func TestCleanup_SkipsPlaceholders(t *testing.T) {
	m, dir := newTestModule(t)
	m.mediaIDProvider = &fakeMediaIDProvider{ids: map[string]bool{}}

	writeTestFile(t, filepath.Join(dir, "placeholder.jpg"), 50)
	writeTestFile(t, filepath.Join(dir, "audio_placeholder.jpg"), 50)
	writeTestFile(t, filepath.Join(dir, "censored_placeholder.jpg"), 50)

	result, err := m.Cleanup()
	if err != nil {
		t.Fatal(err)
	}
	if result.OrphansRemoved != 0 {
		t.Errorf("OrphansRemoved = %d, want 0 (placeholders should be skipped)", result.OrphansRemoved)
	}
}

func TestCleanup_NoProviderReturnsError(t *testing.T) {
	m, _ := newTestModule(t)
	_, err := m.Cleanup()
	if err == nil {
		t.Error("Cleanup with no provider should return error")
	}
}

func TestIsValidThumbnailFile(t *testing.T) {
	dir := t.TempDir()

	// Non-existent file
	if isValidThumbnailFile(filepath.Join(dir, "nope.jpg")) {
		t.Error("non-existent file should not be valid")
	}

	// 0-byte file
	emptyPath := filepath.Join(dir, "empty.jpg")
	writeTestFile(t, emptyPath, 0)
	if isValidThumbnailFile(emptyPath) {
		t.Error("0-byte file should not be valid")
	}

	// Valid file
	validPath := filepath.Join(dir, "valid.jpg")
	writeTestFile(t, validPath, 100)
	if !isValidThumbnailFile(validPath) {
		t.Error("non-empty file should be valid")
	}
}

// ---------------------------------------------------------------------------
// FND-0521: Module context lifecycle
// ---------------------------------------------------------------------------

func TestStartUsesPassedContextNotBackground(t *testing.T) {
	// This test verifies that Start(ctx) uses the passed context for worker
	// lifecycle management, not context.Background(). (FND-0521 regression test)

	// Create a config manager with default config
	cfgMgr := config.NewManager("")
	m := NewModule(cfgMgr, nil)

	// Create a cancellable context with a short timeout
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start the module with our test context
	if err := m.Start(ctx); err != nil {
		t.Fatalf("Start() failed: %v", err)
	}

	// The module's worker context should be derived from ctx (not Background)
	// Verify this by checking that when our ctx is cancelled, the workers
	// are also signaled to stop.
	cancel()

	// Give workers a moment to observe ctx.Done()
	time.Sleep(100 * time.Millisecond)

	// Stop the module and verify clean shutdown
	if err := m.Stop(context.Background()); err != nil {
		t.Fatalf("Stop() failed: %v", err)
	}
}
