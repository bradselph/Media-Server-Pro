package receiver

import (
	"context"
	"errors"
	"testing"

	"media-server-pro/internal/logger"
	"media-server-pro/internal/repositories"
)

// fakeUnregSlaveRepo records Delete calls and can be made to fail.
type fakeUnregSlaveRepo struct {
	deleteErr   error
	deleteCalls int
}

func (f *fakeUnregSlaveRepo) Upsert(context.Context, *repositories.ReceiverSlaveRecord) error {
	return nil
}
func (f *fakeUnregSlaveRepo) Get(context.Context, string) (*repositories.ReceiverSlaveRecord, error) {
	return nil, nil
}
func (f *fakeUnregSlaveRepo) Delete(context.Context, string) error {
	f.deleteCalls++
	return f.deleteErr
}
func (f *fakeUnregSlaveRepo) List(context.Context) ([]*repositories.ReceiverSlaveRecord, error) {
	return nil, nil
}

// fakeUnregMediaRepo records DeleteBySlave calls and can be made to fail.
type fakeUnregMediaRepo struct {
	deleteBySlaveErr   error
	deleteBySlaveCalls int
}

func (f *fakeUnregMediaRepo) UpsertBatch(context.Context, string, []*repositories.ReceiverMediaRecord) error {
	return nil
}
func (f *fakeUnregMediaRepo) ReplaceSlaveMedia(context.Context, string, []*repositories.ReceiverMediaRecord) error {
	return nil
}
func (f *fakeUnregMediaRepo) ListAll(context.Context) ([]*repositories.ReceiverMediaRecord, error) {
	return nil, nil
}
func (f *fakeUnregMediaRepo) ListByFingerprints(context.Context, string, []string) ([]*repositories.ReceiverMediaRecord, error) {
	return nil, nil
}
func (f *fakeUnregMediaRepo) DeleteBySlave(context.Context, string) error {
	f.deleteBySlaveCalls++
	return f.deleteBySlaveErr
}
func (f *fakeUnregMediaRepo) DeleteByID(context.Context, string) error { return nil }

func newUnregModule(sRepo *fakeUnregSlaveRepo, mRepo *fakeUnregMediaRepo) *Module {
	return &Module{
		log:       logger.New("receiver"),
		slaveRepo: sRepo,
		mediaRepo: mRepo,
		slaves:    map[string]*SlaveNode{testSlaveID1: {ID: testSlaveID1, Name: "Node A"}},
		media: map[string]*MediaItem{
			"m1":    {ID: "m1", SlaveID: testSlaveID1},
			"m2":    {ID: "m2", SlaveID: testSlaveID1},
			"other": {ID: "other", SlaveID: "another-slave"},
		},
	}
}

// TestUnregisterSlave_ClearsCachesOnlyAfterDBSuccess verifies the happy path:
// both DB deletes run and only then are the in-memory caches pruned (the other
// slave's media is left intact).
func TestUnregisterSlave_ClearsCachesOnlyAfterDBSuccess(t *testing.T) {
	sRepo := &fakeUnregSlaveRepo{}
	mRepo := &fakeUnregMediaRepo{}
	m := newUnregModule(sRepo, mRepo)

	if err := m.UnregisterSlave(testSlaveID1); err != nil {
		t.Fatalf("UnregisterSlave returned error: %v", err)
	}
	if mRepo.deleteBySlaveCalls != 1 {
		t.Errorf("mediaRepo.DeleteBySlave calls = %d, want 1", mRepo.deleteBySlaveCalls)
	}
	if sRepo.deleteCalls != 1 {
		t.Errorf("slaveRepo.Delete calls = %d, want 1", sRepo.deleteCalls)
	}
	if _, ok := m.slaves[testSlaveID1]; ok {
		t.Error("slave should be removed from cache after successful DB deletes")
	}
	if _, ok := m.media["m1"]; ok {
		t.Error("slave media m1 should be removed from cache")
	}
	if _, ok := m.media["other"]; !ok {
		t.Error("media belonging to a different slave must be left intact")
	}
}

// TestUnregisterSlave_KeepsCacheWhenSlaveDeleteFails verifies the reorder fix:
// when the DB slave-row delete fails, the in-memory caches are NOT pruned, so the
// node does not silently vanish from memory while its row still exists in the DB
// (which previously caused the slave to reappear as a phantom on restart).
func TestUnregisterSlave_KeepsCacheWhenSlaveDeleteFails(t *testing.T) {
	sRepo := &fakeUnregSlaveRepo{deleteErr: errors.New("db down")}
	mRepo := &fakeUnregMediaRepo{}
	m := newUnregModule(sRepo, mRepo)

	if err := m.UnregisterSlave(testSlaveID1); err == nil {
		t.Fatal("expected an error when slaveRepo.Delete fails")
	}
	if _, ok := m.slaves[testSlaveID1]; !ok {
		t.Error("slave must remain in cache when the DB delete failed (no silent divergence)")
	}
	if _, ok := m.media["m1"]; !ok {
		t.Error("slave media must remain in cache when the DB delete failed")
	}
}

// TestUnregisterSlave_StopsBeforeSlaveDeleteWhenMediaDeleteFails verifies that a
// failed media delete short-circuits before the slave row is touched and leaves
// the caches intact.
func TestUnregisterSlave_StopsBeforeSlaveDeleteWhenMediaDeleteFails(t *testing.T) {
	sRepo := &fakeUnregSlaveRepo{}
	mRepo := &fakeUnregMediaRepo{deleteBySlaveErr: errors.New("db down")}
	m := newUnregModule(sRepo, mRepo)

	if err := m.UnregisterSlave(testSlaveID1); err == nil {
		t.Fatal("expected an error when mediaRepo.DeleteBySlave fails")
	}
	if sRepo.deleteCalls != 0 {
		t.Errorf("slaveRepo.Delete should not run after media delete fails; calls = %d", sRepo.deleteCalls)
	}
	if _, ok := m.slaves[testSlaveID1]; !ok {
		t.Error("slave must remain in cache when the media delete failed")
	}
}
