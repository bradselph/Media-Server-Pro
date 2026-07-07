package receiver

import (
	"testing"
	"time"
)

// TestGetSlaves_ReturnsSnapshotCopies guards the fix for the GetSlaves() data
// race. SlaveNode instances are mutated in place under m.mu (Heartbeat,
// markStaleSlaves, PushCatalog write Status/LastSeen/MediaCount on the stored
// struct). GetSlaves must therefore hand back value copies, not the live
// pointers — otherwise a caller reading the result (AdminReceiverListSlaves ->
// c.JSON, no lock held) races those mutating goroutines.
func TestGetSlaves_ReturnsSnapshotCopies(t *testing.T) {
	m := &Module{
		slaves: map[string]*SlaveNode{
			"s1": {ID: "s1", Name: "Node A", Status: "online", MediaCount: 3},
		},
	}

	snap := m.GetSlaves()
	if len(snap) != 1 {
		t.Fatalf("expected 1 slave, got %d", len(snap))
	}

	// Mutate the live node the way Heartbeat/markStaleSlaves/PushCatalog do
	// (under the module lock) after the snapshot was taken.
	m.mu.Lock()
	m.slaves["s1"].Status = "offline"
	m.slaves["s1"].LastSeen = time.Now()
	m.slaves["s1"].MediaCount = 99
	m.mu.Unlock()

	// The already-returned slice must be an isolated snapshot: a caller
	// JSON-marshaling it must not observe the concurrent in-place writes.
	if snap[0].Status != "online" || snap[0].MediaCount != 3 {
		t.Fatalf("GetSlaves must return copies, not live pointers: snapshot leaked mutation Status=%q MediaCount=%d",
			snap[0].Status, snap[0].MediaCount)
	}

	// And the returned pointer must not alias the internal one.
	m.mu.RLock()
	live := m.slaves["s1"]
	m.mu.RUnlock()
	if snap[0] == live {
		t.Fatal("GetSlaves returned the live *SlaveNode pointer; it must return a copy")
	}
}
