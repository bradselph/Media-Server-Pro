package analytics

import (
	"context"
	"testing"
	"time"

	"media-server-pro/pkg/models"
)

// TestWatchTime_SumsForwardDeltasNotRawPositions is a regression test for the
// ~120x watch-time inflation (R5): GetUserStats and computeTopUsers must sum the
// forward position DELTAS within a single (session, media), not the raw cumulative
// heartbeat positions. Ten heartbeats on ONE session/media give a delta sum equal
// to the final position (1000); the old raw-position-sum bug reported 5500. This
// runs on the fake event repo (no MySQL), and — unlike the earlier distinct-session
// test — would FAIL against the pre-fix code, so it actually locks the fix in.
func TestWatchTime_SumsForwardDeltasNotRawPositions(t *testing.T) {
	now := time.Now()
	var events []*models.AnalyticsEvent
	for i := 1; i <= 10; i++ {
		events = append(events, &models.AnalyticsEvent{
			Type:      "playback",
			UserID:    "carol",
			SessionID: "s1",
			MediaID:   "m1",
			Timestamp: now.Add(time.Duration(-i) * time.Minute),
			Data:      map[string]any{"position": float64(i * 100), "duration": 2000.0},
		})
	}
	m := moduleWithEvents(t, events)
	ctx := context.Background()

	rows := m.GetTopUsers(ctx, "watch_time", "", "", 5)
	if len(rows) != 1 {
		t.Fatalf("expected 1 leaderboard row, got %d", len(rows))
	}
	if rows[0].Metric != 1000 {
		t.Errorf("computeTopUsers watch_time = %g; want 1000 (sum of forward deltas), not 5500 (raw-position-sum bug)", rows[0].Metric)
	}

	us := m.GetUserStats(ctx, "carol", 100)
	if us.TotalWatchTime != 1000 {
		t.Errorf("GetUserStats TotalWatchTime = %g; want 1000 (delta sum), not 5500 (raw-sum bug)", us.TotalWatchTime)
	}
}
