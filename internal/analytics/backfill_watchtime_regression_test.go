package analytics

import (
	"context"
	"testing"
	"time"

	"media-server-pro/pkg/models"
)

// TestBackfillDailyStats_WatchTimeIsSumOfDeltas guards the fix for
// BackfillDailyStats inflating TotalWatchTime. The player heartbeats the video's
// cumulative position every ~15s, so watch time is the sum of forward per-session
// deltas — not the raw positions. 20 heartbeats at positions 15,30,...,300 in one
// session represent 300s watched; summing the raw positions would give
// 15+30+...+300 = 3150s (~10x too high), which the live/reconstruct paths never do.
func TestBackfillDailyStats_WatchTimeIsSumOfDeltas(t *testing.T) {
	loc := time.Now().Location()
	day, _ := time.ParseInLocation(dateFormat, "2026-05-01", loc)

	var events []*models.AnalyticsEvent
	for i := 1; i <= 20; i++ {
		events = append(events, &models.AnalyticsEvent{
			Type:      "playback",
			SessionID: "sess-1",
			MediaID:   "m1",
			UserID:    "alice",
			Timestamp: day.Add(time.Duration(i) * time.Minute),
			Data:      map[string]any{"position": float64(i * 15), "duration": 1000.0},
		})
	}

	m := moduleWithEvents(t, events)
	got, err := m.BackfillDailyStats(context.Background(), "2026-05-01")
	if err != nil {
		t.Fatalf("backfill: %v", err)
	}
	if got.TotalWatchTime != 300.0 {
		t.Fatalf("TotalWatchTime: got %f, want 300 (sum of 15s deltas, not raw cumulative positions)", got.TotalWatchTime)
	}
}
