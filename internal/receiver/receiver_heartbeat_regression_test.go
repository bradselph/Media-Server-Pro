package receiver

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"media-server-pro/internal/config"
	"media-server-pro/internal/logger"
	"media-server-pro/internal/repositories"
)

// deadlineCapturingSlaveRepo records whether Upsert received a context with a
// deadline. The other methods are never exercised by Heartbeat.
type deadlineCapturingSlaveRepo struct {
	upsertCalled      bool
	upsertHadDeadline bool
}

func (r *deadlineCapturingSlaveRepo) Upsert(ctx context.Context, _ *repositories.ReceiverSlaveRecord) error {
	r.upsertCalled = true
	_, r.upsertHadDeadline = ctx.Deadline()
	return nil
}
func (r *deadlineCapturingSlaveRepo) Get(context.Context, string) (*repositories.ReceiverSlaveRecord, error) {
	return nil, errors.New("unused")
}
func (r *deadlineCapturingSlaveRepo) Delete(context.Context, string) error { return nil }
func (r *deadlineCapturingSlaveRepo) List(context.Context) ([]*repositories.ReceiverSlaveRecord, error) {
	return nil, errors.New("unused")
}

// TestHeartbeat_BoundsDBWriteContext guards that Heartbeat persists the slave's
// last-seen via a *bounded* context. Heartbeat runs synchronously in the
// per-slave WebSocket read loop, so an unbounded context.Background() would let a
// slow/hung DB block that connection's read loop indefinitely (the failure mode
// RegisterSlave/markStaleSlaves were already bounded to prevent).
func TestHeartbeat_BoundsDBWriteContext(t *testing.T) {
	repo := &deadlineCapturingSlaveRepo{}
	m := &Module{
		config:    config.NewManager(filepath.Join(t.TempDir(), "config.json")),
		log:       logger.New("test"),
		slaveRepo: repo,
		slaves: map[string]*SlaveNode{
			// LastSeen well past any debounce window so the DB write actually fires.
			"s1": {ID: "s1", Name: "S1", LastSeen: time.Now().Add(-time.Hour)},
		},
	}

	if err := m.Heartbeat("s1"); err != nil {
		t.Fatalf("Heartbeat returned error: %v", err)
	}
	if !repo.upsertCalled {
		t.Fatal("expected Upsert to run once the debounce window had elapsed")
	}
	if !repo.upsertHadDeadline {
		t.Fatal("Heartbeat must pass a deadline-bounded context to Upsert so a hung DB can't stall the WS read loop")
	}
}
