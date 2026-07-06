package media

import (
	"context"
	"errors"
	"testing"

	"media-server-pro/internal/logger"
	"media-server-pro/internal/repositories"
)

// fakePersistRepo implements MediaMetadataRepository; only Upsert and Delete
// carry behaviour for the persistPathChange regression test.
type fakePersistRepo struct {
	upsertErr    error
	deletedPaths []string
}

func (f *fakePersistRepo) Upsert(context.Context, string, *repositories.MediaMetadata) error {
	return f.upsertErr
}
func (f *fakePersistRepo) Delete(_ context.Context, path string) error {
	f.deletedPaths = append(f.deletedPaths, path)
	return nil
}
func (f *fakePersistRepo) BulkUpsert(context.Context, map[string]*repositories.MediaMetadata) (int, error) {
	return 0, nil
}
func (f *fakePersistRepo) Get(context.Context, string) (*repositories.MediaMetadata, error) {
	return nil, nil
}
func (f *fakePersistRepo) List(context.Context) (map[string]*repositories.MediaMetadata, error) {
	return nil, nil
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
	return nil, nil
}
func (f *fakePersistRepo) DeleteAllPlaybackPositionsByUser(context.Context, string) error { return nil }
func (f *fakePersistRepo) DeletePlaybackPositionsByPath(context.Context, string) error    { return nil }
func (f *fakePersistRepo) UpdateBlurHash(context.Context, string, string) error           { return nil }
func (f *fakePersistRepo) GetPathByStableID(context.Context, string) (string, error) {
	return "", nil
}
func (f *fakePersistRepo) ListDuplicateCandidates(context.Context) (map[string]*repositories.MediaMetadata, error) {
	return nil, nil
}

func newPersistTestModule(repo repositories.MediaMetadataRepository) *Module {
	return &Module{
		log:          logger.New("media-test"),
		metadataRepo: repo,
		metadata:     map[string]*Metadata{},
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
