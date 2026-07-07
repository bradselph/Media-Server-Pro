package duplicates

import (
	"context"
	"testing"

	"media-server-pro/internal/logger"
	"media-server-pro/internal/repositories"
)

type fakeReceiverMediaRepo struct {
	deletedIDs []string
}

func (f *fakeReceiverMediaRepo) DeleteByID(_ context.Context, id string) error {
	f.deletedIDs = append(f.deletedIDs, id)
	return nil
}
func (f *fakeReceiverMediaRepo) UpsertBatch(context.Context, string, []*repositories.ReceiverMediaRecord) error {
	return nil
}
func (f *fakeReceiverMediaRepo) ReplaceSlaveMedia(context.Context, string, []*repositories.ReceiverMediaRecord) error {
	return nil
}
func (f *fakeReceiverMediaRepo) ListAll(context.Context) ([]*repositories.ReceiverMediaRecord, error) {
	return []*repositories.ReceiverMediaRecord{}, nil
}
func (f *fakeReceiverMediaRepo) DeleteBySlave(context.Context, string) error { return nil }

type fakeReceiverRemover struct {
	removedIDs []string
}

func (f *fakeReceiverRemover) RemoveMediaItem(id string) { f.removedIDs = append(f.removedIDs, id) }

// TestRemoveReceiverItem_EvictsInMemoryCatalog guards that resolving a
// receiver-side duplicate not only deletes the receiver_media DB row but also
// evicts the item from the receiver module's live in-memory catalog. Without the
// eviction the "removed" item kept appearing in the unified listing and stayed
// streamable/downloadable until the next restart or full catalog re-push.
func TestRemoveReceiverItem_EvictsInMemoryCatalog(t *testing.T) {
	repo := &fakeReceiverMediaRepo{}
	remover := &fakeReceiverRemover{}
	m := &Module{
		log:            logger.New("test"),
		receiverRepo:   repo,
		receiverModule: remover,
	}

	if err := m.removeReceiverItem(context.Background(), "item-1"); err != nil {
		t.Fatalf("removeReceiverItem: %v", err)
	}

	if len(repo.deletedIDs) != 1 || repo.deletedIDs[0] != "item-1" {
		t.Fatalf("expected DB DeleteByID(item-1); got %v", repo.deletedIDs)
	}
	if len(remover.removedIDs) != 1 || remover.removedIDs[0] != "item-1" {
		t.Fatalf("expected in-memory RemoveMediaItem(item-1); got %v", remover.removedIDs)
	}
}
