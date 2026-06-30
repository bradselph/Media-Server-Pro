package streaming

import "testing"

// TestStartSession_EnforcesMaxStreamsAtomically verifies the per-user concurrent
// stream cap is enforced inside the same critical section that inserts the session,
// so an over-cap request is rejected (returns nil) and is never tracked. This is the
// authoritative half of the check-then-act race closed in StreamMedia's local path.
func TestStartSession_EnforcesMaxStreamsAtomically(t *testing.T) {
	m := newTestModule(t)

	if s := m.startSession(StreamRequest{Path: "/a", UserID: testUser1, SessionID: "s1", MaxStreams: 2}, 0); s == nil {
		t.Fatal("first session within cap should start")
	}
	if s := m.startSession(StreamRequest{Path: "/b", UserID: testUser1, SessionID: "s2", MaxStreams: 2}, 0); s == nil {
		t.Fatal("second session within cap should start")
	}
	// Third request exceeds the cap: must be rejected and must NOT be tracked.
	if s := m.startSession(StreamRequest{Path: "/c", UserID: testUser1, SessionID: "s3", MaxStreams: 2}, 0); s != nil {
		t.Fatal("third session at cap should be rejected (nil)")
	}
	if got := m.GetActiveStreamCount(testUser1); got != 2 {
		t.Fatalf("active count = %d, want 2 (a rejected session must not be tracked)", got)
	}

	// A different user has their own independent budget.
	if s := m.startSession(StreamRequest{Path: "/d", UserID: testUser2, SessionID: "s4", MaxStreams: 2}, 0); s == nil {
		t.Fatal("a different user within their own cap should start")
	}

	// MaxStreams=0 disables the cap entirely.
	for i := 0; i < 5; i++ {
		if s := m.startSession(StreamRequest{Path: "/u", UserID: "unlimited", SessionID: "", MaxStreams: 0}, 0); s == nil {
			t.Fatal("MaxStreams=0 should always allow")
		}
	}
}

// TestTrackProxyStream_EnforcesCapAndReleases verifies the receiver/proxy path
// enforces the cap and inserts atomically, and that releasing a slot frees capacity.
func TestTrackProxyStream_EnforcesCapAndReleases(t *testing.T) {
	m := newTestModule(t)

	rel1, ok := m.TrackProxyStream(testUser1, 2)
	if !ok {
		t.Fatal("first proxy stream should be allowed")
	}
	if _, ok := m.TrackProxyStream(testUser1, 2); !ok {
		t.Fatal("second proxy stream should be allowed")
	}
	if _, ok := m.TrackProxyStream(testUser1, 2); ok {
		t.Fatal("third proxy stream at cap should be rejected")
	}
	if got := m.GetActiveStreamCount(testUser1); got != 2 {
		t.Fatalf("active count = %d, want 2", got)
	}

	// Releasing one slot must free capacity for a new stream.
	rel1()
	if got := m.GetActiveStreamCount(testUser1); got != 1 {
		t.Fatalf("active count after release = %d, want 1", got)
	}
	if _, ok := m.TrackProxyStream(testUser1, 2); !ok {
		t.Fatal("after releasing a slot a new proxy stream should be allowed")
	}

	// maxStreams<=0 disables the cap.
	for i := 0; i < 5; i++ {
		if _, ok := m.TrackProxyStream(testUser2, 0); !ok {
			t.Fatal("maxStreams=0 should always allow")
		}
	}
}
