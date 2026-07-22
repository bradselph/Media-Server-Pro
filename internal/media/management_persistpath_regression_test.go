package media

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"media-server-pro/internal/config"
	"media-server-pro/internal/logger"
	"media-server-pro/internal/repositories"
	"media-server-pro/pkg/models"
)

// fakePersistRepo implements MediaMetadataRepository; only Upsert and Delete
// carry behavior for the persistPathChange regression test.
type fakePersistRepo struct {
	upsertErr     error
	deleteErr     error
	clearAllErr   error
	upsertedPaths []string
	deletedPaths  []string
}

func (f *fakePersistRepo) Upsert(_ context.Context, path string, _ *repositories.MediaMetadata) error {
	f.upsertedPaths = append(f.upsertedPaths, path)
	return f.upsertErr
}
func (f *fakePersistRepo) Delete(_ context.Context, path string) error {
	f.deletedPaths = append(f.deletedPaths, path)
	return f.deleteErr
}
func (f *fakePersistRepo) BulkUpsert(context.Context, map[string]*repositories.MediaMetadata) (int, error) {
	return 0, nil
}
func (f *fakePersistRepo) Get(context.Context, string) (*repositories.MediaMetadata, error) {
	return nil, repositories.ErrMetadataNotFound
}
func (f *fakePersistRepo) List(context.Context) (map[string]*repositories.MediaMetadata, error) {
	return map[string]*repositories.MediaMetadata{}, nil
}
func (f *fakePersistRepo) ListFiltered(context.Context, repositories.MediaFilter) ([]*repositories.MediaMetadata, int64, error) {
	return nil, 0, nil
}
func (f *fakePersistRepo) IncrementViews(context.Context, string) error { return nil }
func (f *fakePersistRepo) UpdatePlaybackPosition(context.Context, string, string, float64, float64, float64) error {
	return nil
}
func (f *fakePersistRepo) GetPlaybackPosition(context.Context, string, string) (float64, error) {
	return 0, nil
}
func (f *fakePersistRepo) BatchGetPlaybackPositions(context.Context, []string, string) (map[string]float64, error) {
	return map[string]float64{}, nil
}
func (f *fakePersistRepo) DeleteAllPlaybackPositionsByUser(context.Context, string) error {
	return f.clearAllErr
}
func (f *fakePersistRepo) DeletePlaybackPositionsByPath(context.Context, string) error { return nil }
func (f *fakePersistRepo) UpdateBlurHash(context.Context, string, string) error        { return nil }
func (f *fakePersistRepo) GetPathByStableID(context.Context, string) (string, error) {
	return "", nil
}
func (f *fakePersistRepo) ListDuplicateCandidates(context.Context) (map[string]*repositories.MediaMetadata, error) {
	return map[string]*repositories.MediaMetadata{}, nil
}

func newPersistTestModule(repo repositories.MediaMetadataRepository) *Module {
	return &Module{
		log:              logger.New("media-test"),
		metadataRepo:     repo,
		metadata:         map[string]*Metadata{},
		media:            map[string]*models.MediaItem{},
		mediaByID:        map[string]*models.MediaItem{},
		fingerprintIndex: map[string]string{},
	}
}

func newDeleteTestModule(t *testing.T, repo repositories.MediaMetadataRepository, videosDir string) *Module {
	t.Helper()
	cfg := config.NewManager(filepath.Join(t.TempDir(), "config.json"))
	if err := cfg.Update(func(current *config.Config) {
		current.Directories.Videos = videosDir
	}); err != nil {
		t.Fatalf("configure videos directory: %v", err)
	}
	m := newPersistTestModule(repo)
	m.config = cfg
	return m
}

func TestRebaseScannedItemLockedPreservesLatestMutableMetadata(t *testing.T) {
	path := "/videos/a.mp4"
	lastPlayed := time.Date(2026, time.July, 21, 12, 30, 0, 0, time.UTC)
	dateAdded := lastPlayed.Add(-24 * time.Hour)
	m := newPersistTestModule(nil)
	m.metadata[path] = &Metadata{
		Views:       17,
		LastPlayed:  &lastPlayed,
		DateAdded:   dateAdded,
		IsMature:    true,
		MatureScore: 0.91,
		Tags:        []string{"latest", "curated"},
		CustomMeta:  map[string]string{"studio": "latest", "collision": "admin"},
		BlurHash:    "latest-blurhash",
		Duration:    123.5,
	}
	previous := &models.MediaItem{
		ID:           "id-a",
		Path:         path,
		ThumbnailURL: "/thumbnail?id=id-a",
		Bitrate:      8_000,
		Width:        1920,
		Height:       1080,
		Codec:        "h264",
		Container:    "mp4",
	}
	scanned := &models.MediaItem{
		ID:          "id-a",
		Path:        path,
		Views:       1,
		IsMature:    false,
		MatureScore: 0.1,
		Tags:        []string{"stale"},
		BlurHash:    "stale-blurhash",
		Metadata:    map[string]string{"probe": "kept", "collision": "probe"},
	}

	m.mu.Lock()
	keep := m.rebaseScannedItemLocked(scanned, previous)
	m.mu.Unlock()
	if !keep {
		t.Fatal("live metadata entry should keep the scanned item")
	}
	if scanned.Views != 17 || scanned.LastPlayed == nil || !scanned.LastPlayed.Equal(lastPlayed) {
		t.Fatalf("runtime metadata was not rebased: views=%d last_played=%v", scanned.Views, scanned.LastPlayed)
	}
	if !scanned.IsMature || scanned.MatureScore != 0.91 {
		t.Fatalf("maturity metadata was not rebased: mature=%v score=%v", scanned.IsMature, scanned.MatureScore)
	}
	if len(scanned.Tags) != 2 || scanned.Tags[0] != "latest" || scanned.Tags[1] != "curated" {
		t.Fatalf("tags were not rebased: %v", scanned.Tags)
	}
	if scanned.BlurHash != "latest-blurhash" || scanned.Duration != 123.5 || !scanned.DateAdded.Equal(dateAdded) {
		t.Fatalf("persisted fields were not rebased: blurhash=%q duration=%v date_added=%v", scanned.BlurHash, scanned.Duration, scanned.DateAdded)
	}
	if scanned.Metadata["probe"] != "kept" || scanned.Metadata["studio"] != "latest" || scanned.Metadata["collision"] != "admin" {
		t.Fatalf("probe/custom metadata merge was not preserved: %v", scanned.Metadata)
	}
	if scanned.ThumbnailURL != previous.ThumbnailURL || scanned.Bitrate != previous.Bitrate || scanned.Width != previous.Width || scanned.Height != previous.Height || scanned.Codec != previous.Codec || scanned.Container != previous.Container {
		t.Fatalf("unchanged probe fields were not preserved: %+v", scanned)
	}

	// Rebased slices, maps, and pointers must not alias the authoritative cache.
	scanned.Tags[0] = "mutated"
	scanned.Metadata["studio"] = "mutated"
	*scanned.LastPlayed = scanned.LastPlayed.Add(time.Hour)
	if m.metadata[path].Tags[0] != "latest" || m.metadata[path].CustomMeta["studio"] != "latest" || !m.metadata[path].LastPlayed.Equal(lastPlayed) {
		t.Fatal("rebased item aliases authoritative metadata")
	}
}

func TestRebaseScannedItemLockedDropsConcurrentlyDeletedItem(t *testing.T) {
	m := newPersistTestModule(nil)
	item := &models.MediaItem{ID: "deleted", Path: "/videos/deleted.mp4"}
	m.mu.Lock()
	keep := m.rebaseScannedItemLocked(item, item)
	m.mu.Unlock()
	if keep {
		t.Fatal("scan must not republish an item whose metadata was concurrently deleted")
	}
}

func TestDeleteMediaMetadataFailureLeavesFileAndCachesRetryable(t *testing.T) {
	videosDir := t.TempDir()
	filePath := filepath.Join(videosDir, "retry.mp4")
	if err := os.WriteFile(filePath, []byte("video"), 0o600); err != nil {
		t.Fatalf("create media file: %v", err)
	}
	repo := &fakePersistRepo{deleteErr: errors.New("database unavailable")}
	m := newDeleteTestModule(t, repo, videosDir)
	item := &models.MediaItem{ID: "id-retry", Path: filePath}
	meta := &Metadata{StableID: item.ID, ContentFingerprint: "fp-retry"}
	m.media[filePath] = item
	m.mediaByID[item.ID] = item
	m.metadata[filePath] = meta
	m.fingerprintIndex[meta.ContentFingerprint] = filePath

	if err := m.DeleteMedia(context.Background(), filePath); err == nil {
		t.Fatal("expected metadata delete failure")
	}
	if _, err := os.Stat(filePath); err != nil {
		t.Fatalf("file must remain after metadata delete failure: %v", err)
	}
	if m.media[filePath] != item || m.mediaByID[item.ID] != item || m.metadata[filePath] != meta || m.fingerprintIndex[meta.ContentFingerprint] != filePath {
		t.Fatal("caches changed after metadata delete failure")
	}

	repo.deleteErr = nil
	if err := m.DeleteMedia(context.Background(), filePath); err != nil {
		t.Fatalf("retry delete: %v", err)
	}
	if _, err := os.Stat(filePath); !os.IsNotExist(err) {
		t.Fatalf("file should be gone after successful retry, stat err=%v", err)
	}
	if _, ok := m.media[filePath]; ok {
		t.Fatal("media cache still contains deleted path")
	}
	if _, ok := m.metadata[filePath]; ok {
		t.Fatal("metadata cache still contains deleted path")
	}
	if _, ok := m.mediaByID[item.ID]; ok {
		t.Fatal("ID cache still contains deleted item")
	}
	if _, ok := m.fingerprintIndex[meta.ContentFingerprint]; ok {
		t.Fatal("fingerprint cache still contains deleted item")
	}
	if len(repo.deletedPaths) != 2 {
		t.Fatalf("expected one failed attempt and one retry, got %v", repo.deletedPaths)
	}
}

func TestDeleteMediaStorageFailureRestoresMetadata(t *testing.T) {
	videosDir := t.TempDir()
	filePath := filepath.Join(videosDir, "non-empty-directory")
	if err := os.Mkdir(filePath, 0o700); err != nil {
		t.Fatalf("create directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(filePath, "child"), []byte("keep"), 0o600); err != nil {
		t.Fatalf("create child: %v", err)
	}
	repo := &fakePersistRepo{deleteErr: repositories.ErrMetadataNotFound}
	m := newDeleteTestModule(t, repo, videosDir)
	item := &models.MediaItem{ID: "id-restore", Path: filePath}
	meta := &Metadata{StableID: item.ID, Tags: []string{"keep"}}
	m.media[filePath] = item
	m.mediaByID[item.ID] = item
	m.metadata[filePath] = meta

	if err := m.DeleteMedia(context.Background(), filePath); err == nil {
		t.Fatal("expected storage delete failure")
	}
	if len(repo.upsertedPaths) != 1 || repo.upsertedPaths[0] != filePath {
		t.Fatalf("metadata should be restored after storage failure, got upserts %v", repo.upsertedPaths)
	}
	if m.media[filePath] != item || m.metadata[filePath] != meta {
		t.Fatal("cache should remain available after compensated storage failure")
	}
}

func TestRemoveMediaMetadataFailureKeepsCacheAndGhostCleanupRetries(t *testing.T) {
	path := "/videos/remove.mp4"
	repo := &fakePersistRepo{deleteErr: errors.New("database unavailable")}
	m := newPersistTestModule(repo)
	item := &models.MediaItem{ID: "id-remove", Path: path}
	meta := &Metadata{StableID: item.ID, ContentFingerprint: "fp-remove"}
	m.media[path] = item
	m.mediaByID[item.ID] = item
	m.metadata[path] = meta
	m.fingerprintIndex[meta.ContentFingerprint] = path

	if err := m.RemoveMedia(path); err == nil {
		t.Fatal("expected metadata delete failure")
	}
	if m.media[path] != item || m.mediaByID[item.ID] != item || m.metadata[path] != meta || m.fingerprintIndex[meta.ContentFingerprint] != path {
		t.Fatal("cache changed after failed metadata delete")
	}

	repo.deleteErr = nil
	if err := m.RemoveMedia(path); err != nil {
		t.Fatalf("retry remove: %v", err)
	}
	if _, ok := m.media[path]; ok {
		t.Fatal("media cache still contains removed path")
	}
	if _, ok := m.metadata[path]; ok {
		t.Fatal("metadata cache still contains removed path")
	}

	// A missing cache entry must not skip DB cleanup: this is the ghost-row
	// repair case that previously returned before calling the repository.
	if err := m.RemoveMedia(path); err != nil {
		t.Fatalf("idempotent ghost cleanup: %v", err)
	}
	if len(repo.deletedPaths) != 3 {
		t.Fatalf("expected failed delete, retry, and ghost cleanup calls; got %v", repo.deletedPaths)
	}
}

func TestUpdateMetadata_SaveFailureLeavesCachesUnchanged(t *testing.T) {
	path := "/videos/a.mp4"
	repo := &fakePersistRepo{upsertErr: errors.New("database unavailable")}
	m := newPersistTestModule(repo)
	m.metadata[path] = &Metadata{StableID: "id-a", Tags: []string{"old"}, CustomMeta: map[string]string{"studio": "old"}}
	m.media[path] = &models.MediaItem{ID: "id-a", Path: path, Tags: []string{"old"}, Metadata: map[string]string{"studio": "old"}}
	m.mediaByID["id-a"] = m.media[path]

	err := m.UpdateMetadata(path, map[string]any{"tags": []string{"new"}, "studio": "new"})
	if err == nil {
		t.Fatal("expected persistence error")
	}
	if got := m.metadata[path].Tags; len(got) != 1 || got[0] != "old" {
		t.Fatalf("metadata cache changed after failed save: %v", got)
	}
	if got := m.media[path].Metadata["studio"]; got != "old" {
		t.Fatalf("media cache changed after failed save: %q", got)
	}
}

func TestClearAllPlaybackPositions_SaveFailureLeavesCacheUnchanged(t *testing.T) {
	path := "/videos/a.mp4"
	m := newPersistTestModule(&fakePersistRepo{clearAllErr: errors.New("database unavailable")})
	m.metadata[path] = &Metadata{PlaybackPos: map[string]float64{"user-1": 42}}

	if err := m.ClearAllPlaybackPositions(context.Background(), "user-1"); err == nil {
		t.Fatal("expected persistence error")
	}
	if got := m.metadata[path].PlaybackPos["user-1"]; got != 42 {
		t.Fatalf("playback cache changed after failed delete: %v", got)
	}
}

func TestHasFingerprintRequiresLiveCatalogPath(t *testing.T) {
	m := newPersistTestModule(nil)
	m.fingerprintIndex["fp"] = "/videos/deleted.mp4"
	if m.HasFingerprint("fp") {
		t.Fatal("stale fingerprint mapping must not report deleted media as local")
	}
	m.media["/videos/live.mp4"] = &models.MediaItem{ID: "live", Path: "/videos/live.mp4"}
	m.fingerprintIndex["fp"] = "/videos/live.mp4"
	if !m.HasFingerprint("fp") {
		t.Fatal("live fingerprint mapping should be reported")
	}
}

// TestPersistPathChange_KeepsOldRowWhenUpsertFails guards the contract that the
// old metadata row is deleted only AFTER the new-path row is safely persisted.
// If saveMetadataItem(newPath) fails (e.g. a transient DB lock-wait timeout),
// deleting the old row would strand the moved file with zero DB rows and lose
// its stable ID / tags / is_mature on the next restart.
func TestPersistPathChange_KeepsOldRowWhenUpsertFails(t *testing.T) {
	repo := &fakePersistRepo{upsertErr: errors.New("lock wait timeout")}
	m := newPersistTestModule(repo)
	m.metadata["/videos/new.mp4"] = &Metadata{StableID: "X", Tags: []string{"classic"}, IsMature: true}

	m.persistPathChange("/videos/old.mp4", "/videos/new.mp4")

	if len(repo.deletedPaths) != 0 {
		t.Fatalf("old row must NOT be deleted when the new-path upsert fails; got deletes for %v", repo.deletedPaths)
	}
}

// TestPersistPathChange_DeletesOldRowOnSuccess confirms the happy path still
// deletes the stale old-path row once the new row is persisted.
func TestPersistPathChange_DeletesOldRowOnSuccess(t *testing.T) {
	repo := &fakePersistRepo{}
	m := newPersistTestModule(repo)
	m.metadata["/videos/new.mp4"] = &Metadata{StableID: "X"}

	m.persistPathChange("/videos/old.mp4", "/videos/new.mp4")

	if len(repo.deletedPaths) != 1 || repo.deletedPaths[0] != "/videos/old.mp4" {
		t.Fatalf("old row should be deleted exactly once on success; got %v", repo.deletedPaths)
	}
}
