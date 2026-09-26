package streaming

import (
	"context"
	"errors"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// Regression tests for R00: the per-user/per-IP concurrent-stream cap must count
// DISTINCT media being streamed, not every HTTP Range request. Ordinary
// seeking/scrubbing on a native <video> element aborts the in-flight Range
// request and issues a new one for the very same video; without deduping by
// MediaID that transiently looks like a second concurrent stream and trips even
// a cap as low as 1 (the "basic"/"guest" default), aborting playback with 429.
// See countUserStreamsLocked, startSession, GetActiveStreamCount, and
// CanStartStreamForMedia.
//
// Regression tests below also cover the follow-up fix: the same-media
// exemption from R00 is bounded by maxSessionsPerMedia, not unconditional.
// Without a bound, once any session existed for a (user, mediaID) pair, every
// subsequent request for that exact media was admitted forever regardless of
// how many rows already existed — an unbounded resource-exhaustion vector on
// routes exempt from the general rate limiter (see api/handlers/media.go's
// StreamMedia). The bound still tolerates one stale-but-not-yet-reaped row
// from an ordinary seek.

// TestStartSession_SameMediaNeverCountsTwiceAtCapOne verifies that a second
// session for media the user is already streaming is admitted even when the cap
// is already exhausted by that same media, and that GetActiveStreamCount reports
// the distinct-media count (1), not the raw row count (2).
func TestStartSession_SameMediaNeverCountsTwiceAtCapOne(t *testing.T) {
	m := newTestModule(t)

	first := m.startSession(StreamRequest{
		Path: "/video.mp4", MediaID: "media-1", UserID: testUser1, SessionID: "s1", MaxStreams: 1,
	}, 0)
	if first == nil {
		t.Fatal("first request for media-1 should start within cap")
	}

	// A second, overlapping Range request for the exact same media (e.g. a seek)
	// must not be rejected, even though the cap is 1 and a session already exists.
	second := m.startSession(StreamRequest{
		Path: "/video.mp4", MediaID: "media-1", UserID: testUser1, SessionID: "s2", MaxStreams: 1,
	}, 500)
	if second == nil {
		t.Fatal("overlapping request for the SAME media must not be rejected by the cap")
	}
	if first.ID == second.ID {
		t.Fatal("expected two distinct session rows for the two Range requests")
	}

	if got := m.GetActiveStreamCount(testUser1); got != 1 {
		t.Fatalf("active distinct-media count = %d, want 1 (two rows, one media)", got)
	}

	// A request for a DIFFERENT media must still be rejected: the user already has
	// 1 distinct media active and the cap is 1.
	other := m.startSession(StreamRequest{
		Path: "/other.mp4", MediaID: "media-2", UserID: testUser1, SessionID: "s3", MaxStreams: 1,
	}, 0)
	if other != nil {
		t.Fatal("a different media at cap=1 should be rejected")
	}

	// Ending both rows for media-1 frees the slot for a different media.
	m.endSession(first.ID)
	m.endSession(second.ID)
	if got := m.GetActiveStreamCount(testUser1); got != 0 {
		t.Fatalf("active count after ending both rows = %d, want 0", got)
	}
	if s := m.startSession(StreamRequest{
		Path: "/other.mp4", MediaID: "media-2", UserID: testUser1, SessionID: "s4", MaxStreams: 1,
	}, 0); s == nil {
		t.Fatal("after freeing the slot, a different media should be allowed")
	}
}

// TestStream_OverlappingRangeRequestsSameMedia_BothSucceedAtCapOne exercises the
// fix through the public Stream() entry point rather than the private
// startSession helper: with one session already open for a media (simulating the
// stale-but-not-yet-reaped session from a prior seek), a second overlapping Range
// request for the SAME media at cap=1 must succeed, while a request for a
// different media must be rejected, and ending the stale session frees the slot.
func TestStream_OverlappingRangeRequestsSameMedia_BothSucceedAtCapOne(t *testing.T) {
	m := newTestModule(t)
	if err := m.Start(context.Background()); err != nil {
		t.Fatalf("Start() error: %v", err)
	}
	defer m.Stop(context.Background())

	dir := t.TempDir()
	fpath := filepath.Join(dir, testMp4)
	content := make([]byte, 1000)
	for i := range content {
		content[i] = byte(i % 256)
	}
	if err := os.WriteFile(fpath, content, 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	// Simulate an in-flight session left over from a prior request for this media
	// that the sweep hasn't reaped yet.
	stale := m.startSession(StreamRequest{
		Path: fpath, MediaID: "media-1", UserID: testUser1, SessionID: "stale", MaxStreams: 1,
	}, 0)
	if stale == nil {
		t.Fatal("stale session should start")
	}

	// A new overlapping Range request for the SAME media must succeed.
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", testStream, nil)
	r.Header.Set("Range", "bytes=0-99")
	err := m.Stream(w, r, StreamRequest{
		Path: fpath, MediaID: "media-1", UserID: testUser1, SessionID: "new", MaxStreams: 1, RangeHeader: "bytes=0-99",
	})
	if err != nil {
		t.Fatalf("overlapping request for the same media should succeed, got error: %v", err)
	}
	if w.Code != 206 {
		t.Errorf("status = %d, want 206", w.Code)
	}

	// The transient session created by Stream() has already ended (defer runs at
	// return), so only the stale session remains — distinct count is still 1.
	if got := m.GetActiveStreamCount(testUser1); got != 1 {
		t.Fatalf("active distinct-media count after overlapping request = %d, want 1", got)
	}

	// A DIFFERENT media is correctly rejected while the stale session holds the cap.
	w2 := httptest.NewRecorder()
	r2 := httptest.NewRequest("GET", testStream, nil)
	err = m.Stream(w2, r2, StreamRequest{
		Path: fpath, MediaID: "media-2", UserID: testUser1, SessionID: "other", MaxStreams: 1,
	})
	if !errors.Is(err, ErrStreamLimitExceeded) {
		t.Fatalf("different media at cap=1 should be rejected, got err=%v", err)
	}

	// Cleaning up the stale session frees the slot for the different media.
	m.endSession(stale.ID)
	if got := m.GetActiveStreamCount(testUser1); got != 0 {
		t.Fatalf("active count after cleanup = %d, want 0", got)
	}
	w3 := httptest.NewRecorder()
	r3 := httptest.NewRequest("GET", testStream, nil)
	if err := m.Stream(w3, r3, StreamRequest{
		Path: fpath, MediaID: "media-2", UserID: testUser1, SessionID: "other2", MaxStreams: 1,
	}); err != nil {
		t.Fatalf("different media should now succeed after cleanup, got error: %v", err)
	}
}

// TestCanStartStreamForMedia_AlreadyStreamingBypassesCap verifies the exported
// pre-flight helper: a request for media the user is already streaming is
// allowed even when the cap is exhausted by that same media, but only up to
// maxSessionsPerMedia concurrent rows — beyond that bound the same media is
// rejected too, so a single media ID can't be flooded with unlimited
// concurrent sessions once any one of them exists. A different media is
// rejected as expected once the (unrelated) cap is reached.
func TestCanStartStreamForMedia_AlreadyStreamingBypassesCap(t *testing.T) {
	m := newTestModule(t)

	for i := 0; i < maxSessionsPerMedia; i++ {
		if !m.CanStartStreamForMedia(testUser1, "media-1", 1) {
			t.Fatalf("setup: same media should be allowed while under maxSessionsPerMedia (row %d)", i)
		}
		s := m.startSession(StreamRequest{
			Path: "/v.mp4", MediaID: "media-1", UserID: testUser1, SessionID: fmt.Sprintf("s%d", i), MaxStreams: 1,
		}, 0)
		if s == nil {
			t.Fatalf("setup: session %d for media-1 should start (within maxSessionsPerMedia)", i)
		}
	}

	if m.CanStartStreamForMedia(testUser1, "media-1", 1) {
		t.Error("same media beyond maxSessionsPerMedia must be rejected, not bypassed forever")
	}
	if m.CanStartStreamForMedia(testUser1, "media-2", 1) {
		t.Error("a different media at cap=1 should not be allowed")
	}
	if !m.CanStartStreamForMedia(testUser1, "media-2", 0) {
		t.Error("maxStreams=0 should always allow")
	}
}

// TestStartSession_SameMediaFloodIsBounded is the direct regression test for
// the unbounded-bypass bug: flooding a single (user, mediaID) pair with far
// more concurrent requests than maxSessionsPerMedia must not admit them all —
// only maxSessionsPerMedia rows may ever be concurrently active for one media,
// even though same-media requests are otherwise exempt from the distinct-media
// cap.
func TestStartSession_SameMediaFloodIsBounded(t *testing.T) {
	m := newTestModule(t)

	const floodAttempts = maxSessionsPerMedia + 10
	admitted := 0
	for i := 0; i < floodAttempts; i++ {
		s := m.startSession(StreamRequest{
			Path: "/v.mp4", MediaID: "media-1", UserID: testUser1, SessionID: fmt.Sprintf("flood-%d", i), MaxStreams: 1,
		}, 0)
		if s != nil {
			admitted++
		}
	}
	if admitted != maxSessionsPerMedia {
		t.Fatalf("admitted %d concurrent sessions for one media, want exactly maxSessionsPerMedia (%d)", admitted, maxSessionsPerMedia)
	}
	if got := len(m.activeSessions); got != maxSessionsPerMedia {
		t.Fatalf("active session rows = %d, want %d", got, maxSessionsPerMedia)
	}
}

// TestTrackProxyStreamForMedia_DedupesByMedia verifies the receiver/proxy path
// gets the same distinct-media accounting as the local direct-play path when a
// mediaID is supplied, while preserving TrackProxyStream's existing
// no-dedup-without-a-media-identity behavior for backward compatibility.
func TestTrackProxyStreamForMedia_DedupesByMedia(t *testing.T) {
	m := newTestModule(t)

	rel1, ok := m.TrackProxyStreamForMedia(testUser1, "media-1", 1)
	if !ok {
		t.Fatal("first proxy stream for media-1 should be allowed")
	}
	rel2, ok := m.TrackProxyStreamForMedia(testUser1, "media-1", 1)
	if !ok {
		t.Error("a second overlapping proxy request for the SAME media should be allowed at cap=1")
	}
	// A third concurrent proxy request for the SAME media exceeds
	// maxSessionsPerMedia and must be rejected — the same-media exemption is
	// bounded, not unlimited.
	if _, ok := m.TrackProxyStreamForMedia(testUser1, "media-1", 1); ok {
		t.Error("a proxy request for the SAME media beyond maxSessionsPerMedia should be rejected")
	}
	if _, ok := m.TrackProxyStreamForMedia(testUser1, "media-2", 1); ok {
		t.Error("a proxy request for a DIFFERENT media at cap=1 should be rejected")
	}
	rel1()
	rel2()
}
