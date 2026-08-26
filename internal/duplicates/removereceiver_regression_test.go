package duplicates

import (
	"context"
	"errors"
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
func (f *fakeReceiverMediaRepo) ListByFingerprints(context.Context, string, []string) ([]*repositories.ReceiverMediaRecord, error) {
	return []*repositories.ReceiverMediaRecord{}, nil
}
func (f *fakeReceiverMediaRepo) DeleteBySlave(context.Context, string) error { return nil }

type fakeReceiverRemover struct {
	removedIDs    []string
	removedSlaves []string
	tombstoneRan  bool
	err           error
}

// RemoveMediaItem mirrors the receiver module's contract: it owns the DB delete
// and cache eviction, and invokes persistTombstone while holding its own
// serialization, before the media row goes away.
func (f *fakeReceiverRemover) RemoveMediaItem(ctx context.Context, itemID, slaveID string, persistTombstone func(context.Context) error) error {
	f.removedIDs = append(f.removedIDs, itemID)
	f.removedSlaves = append(f.removedSlaves, slaveID)
	if f.err != nil {
		return f.err
	}
	if persistTombstone != nil {
		if err := persistTombstone(ctx); err != nil {
			return err
		}
		f.tombstoneRan = true
	}
	return nil
}

// TestRemoveReceiverItem_EvictsInMemoryCatalog guards that resolving a
// receiver-side duplicate hands the item to the receiver module, which owns
// both the receiver_media DB delete and eviction from the live in-memory
// catalog. Without that hand-off the "removed" item kept appearing in the
// unified listing and stayed streamable until the next restart or catalog
// re-push. The tombstone callback must run too, or a re-pushed catalog
// resurrects the item.
func TestRemoveReceiverItem_EvictsInMemoryCatalog(t *testing.T) {
	remover := &fakeReceiverRemover{}
	m := &Module{
		log:            logger.New("test"),
		receiverRepo:   &fakeReceiverMediaRepo{},
		receiverModule: remover,
	}

	p := removeResolutionParams{
		id:         "dup-1",
		action:     "remove_a",
		resolvedBy: "admin",
		itemID:     "item-1",
		slaveID:    "slave-1",
	}
	tombstoneCalls := 0
	err := m.removeReceiverItem(context.Background(), p, func(context.Context) error {
		tombstoneCalls++
		return nil
	})
	if err != nil {
		t.Fatalf("removeReceiverItem: %v", err)
	}

	if len(remover.removedIDs) != 1 || remover.removedIDs[0] != "item-1" {
		t.Fatalf("expected RemoveMediaItem(item-1); got %v", remover.removedIDs)
	}
	if len(remover.removedSlaves) != 1 || remover.removedSlaves[0] != "slave-1" {
		t.Fatalf("expected slave-1 to be passed through; got %v", remover.removedSlaves)
	}
	if tombstoneCalls != 1 || !remover.tombstoneRan {
		t.Fatalf("expected the tombstone callback to run exactly once; got %d (ran=%v)", tombstoneCalls, remover.tombstoneRan)
	}
}

// TestRemoveReceiverItem_NoReceiverModule guards that resolution fails loudly
// rather than reporting success when the receiver module is not wired — the
// DB row and in-memory entry would both survive an apparent "removal".
func TestRemoveReceiverItem_NoReceiverModule(t *testing.T) {
	m := &Module{log: logger.New("test")}
	err := m.removeReceiverItem(context.Background(), removeResolutionParams{itemID: "item-1", slaveID: "slave-1"}, nil)
	if err == nil {
		t.Fatal("expected an error when receiverModule is nil, got nil")
	}
}

// TestRemoveReceiverItem_PropagatesRemoverError guards that a failure inside the
// receiver module reaches the caller, so the duplicate is not marked resolved
// while its item is still present.
func TestRemoveReceiverItem_PropagatesRemoverError(t *testing.T) {
	remover := &fakeReceiverRemover{err: errors.New("boom")}
	m := &Module{
		log:            logger.New("test"),
		receiverModule: remover,
	}
	err := m.removeReceiverItem(context.Background(), removeResolutionParams{itemID: "item-1", slaveID: "slave-1"}, nil)
	if err == nil {
		t.Fatal("expected the remover error to propagate, got nil")
	}
}
