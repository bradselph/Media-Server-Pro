package analytics

import (
	"context"
	"testing"

	"media-server-pro/pkg/models"
)

// Hub (BETA) engagement events must flow through the same daily-counter path as
// every other traffic event so the admin dashboard trends Hub usage exactly like
// local media. These tests lock in that wiring — live counters, the summary
// projection, and the metric lookup used by timelines/forecasts/alerts — so it
// cannot silently drift the way an untracked feature would.
func TestTrackTrafficEvent_IncrementsHubDailyCounters(t *testing.T) {
	m := testAnalyticsModule(t)
	ctx := context.Background()

	m.TrackTrafficEvent(ctx, TrafficEventParams{Type: EventHubBrowse})
	m.TrackTrafficEvent(ctx, TrafficEventParams{Type: EventHubBrowse})
	m.TrackTrafficEvent(ctx, TrafficEventParams{Type: EventHubView})
	m.TrackTrafficEvent(ctx, TrafficEventParams{Type: EventHubSearch})
	m.TrackTrafficEvent(ctx, TrafficEventParams{Type: EventHubPlaylistAdd})

	d := todayDaily(t, m)
	if d.HubBrowses != 2 {
		t.Errorf("HubBrowses = %d, want 2", d.HubBrowses)
	}
	if d.HubViews != 1 {
		t.Errorf("HubViews = %d, want 1", d.HubViews)
	}
	if d.HubSearches != 1 {
		t.Errorf("HubSearches = %d, want 1", d.HubSearches)
	}
	if d.HubPlaylistAdds != 1 {
		t.Errorf("HubPlaylistAdds = %d, want 1", d.HubPlaylistAdds)
	}
}

func TestGetSummary_IncludesHubEngagement(t *testing.T) {
	m := testAnalyticsModule(t)
	ctx := context.Background()
	m.TrackTrafficEvent(ctx, TrafficEventParams{Type: EventHubBrowse})
	m.TrackTrafficEvent(ctx, TrafficEventParams{Type: EventHubView})
	m.TrackTrafficEvent(ctx, TrafficEventParams{Type: EventHubView})

	sum := m.GetSummary(ctx)
	if sum.TodayHubBrowses != 1 {
		t.Errorf("TodayHubBrowses = %d, want 1", sum.TodayHubBrowses)
	}
	if sum.TodayHubViews != 2 {
		t.Errorf("TodayHubViews = %d, want 2", sum.TodayHubViews)
	}
}

// dailyStatField backs every timeline/forecast/comparison/alert query, so the
// Hub metrics must resolve there or the dashboard's Hub sparkline would silently
// read zeros even while the counters increment.
func TestDailyStatField_ResolvesHubMetrics(t *testing.T) {
	d := &models.DailyStats{HubBrowses: 3, HubViews: 5, HubSearches: 7, HubPlaylistAdds: 9}
	cases := map[string]float64{
		"hub_browses":       3,
		"hub_views":         5,
		"hub_searches":      7,
		"hub_playlist_adds": 9,
	}
	for metric, want := range cases {
		if got := dailyStatField(d, metric); got != want {
			t.Errorf("dailyStatField(%q) = %v, want %v", metric, got, want)
		}
	}
}
