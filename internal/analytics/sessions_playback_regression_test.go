package analytics

import (
	"testing"
	"time"

	"media-server-pro/pkg/models"
)

// TestUpdateSession_PlaybackCountedAfterView guards the fix for TotalPlaybacks
// never incrementing. A "view" event fires before the first playback heartbeat
// with the same session/media; when both shared one dedup set, the first
// heartbeat looked non-new, so applyPlaybackToMediaStatsLocked never ran
// TotalPlaybacks++ (and CompletionRate stayed 0). isNewMedia (the playback
// gate) must be computed from a playback-only set.
func TestUpdateSession_PlaybackCountedAfterView(t *testing.T) {
	m := &Module{sessions: map[string]*sessionData{}}
	base := time.Unix(1_700_000_000, 0)

	// 1) A view event for media X in session s1 (fires first, on stream start).
	m.updateSession(models.AnalyticsEvent{
		Type: "view", SessionID: "s1", MediaID: "X", Timestamp: base,
	})

	// 2) First playback heartbeat for the same session/media MUST be counted.
	_, isNew, _ := m.updateSession(models.AnalyticsEvent{
		Type: "playback", SessionID: "s1", MediaID: "X", Timestamp: base.Add(time.Second),
		Data: map[string]any{"position": 5.0, "duration": 100.0},
	})
	if !isNew {
		t.Fatal("first playback heartbeat after a view must be counted (isNewMedia=true)")
	}

	// 3) A subsequent heartbeat for the same session/media MUST NOT re-count.
	_, isNew2, _ := m.updateSession(models.AnalyticsEvent{
		Type: "playback", SessionID: "s1", MediaID: "X", Timestamp: base.Add(2 * time.Second),
		Data: map[string]any{"position": 12.0, "duration": 100.0},
	})
	if isNew2 {
		t.Fatal("subsequent playback heartbeats must not be counted again")
	}
}
