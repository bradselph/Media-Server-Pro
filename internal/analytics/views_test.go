package analytics

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"media-server-pro/internal/config"
	"media-server-pro/internal/database"
	"media-server-pro/internal/repositories"
	"media-server-pro/pkg/models"
)

// fakeEventRepo is a richer test stub than noOpAnalyticsRepo — it can return
// pre-seeded events for List() so we can exercise the in-memory aggregation
// logic in GetTopUsers / GetTopSearches / GetFunnel / GetDeviceBreakdown
// etc. without needing a real MySQL instance.
type fakeEventRepo struct {
	noOpAnalyticsRepo
	events []*models.AnalyticsEvent
}

func (f *fakeEventRepo) List(_ context.Context, filter repositories.AnalyticsFilter) ([]*models.AnalyticsEvent, error) {
	out := make([]*models.AnalyticsEvent, 0, len(f.events))
	for _, ev := range f.events {
		if filter.Type != "" && ev.Type != filter.Type {
			continue
		}
		if filter.UserID != "" && ev.UserID != filter.UserID {
			continue
		}
		if filter.MediaID != "" && ev.MediaID != filter.MediaID {
			continue
		}
		if filter.StartDate != "" {
			t, err := time.Parse(time.RFC3339, filter.StartDate)
			if err == nil && ev.Timestamp.Before(t) {
				continue
			}
		}
		if filter.EndDate != "" {
			t, err := time.Parse(time.RFC3339, filter.EndDate)
			if err == nil && ev.Timestamp.After(t) {
				continue
			}
		}
		out = append(out, ev)
		if filter.Limit > 0 && len(out) >= filter.Limit {
			break
		}
	}
	return out, nil
}

func moduleWithEvents(t *testing.T, events []*models.AnalyticsEvent) *Module {
	t.Helper()
	cfg := config.NewManager(filepath.Join(t.TempDir(), "config.json"))
	dbMod := database.NewModule(cfg)
	m, err := NewModule(cfg, dbMod)
	if err != nil {
		t.Fatal(err)
	}
	m.eventRepo = &fakeEventRepo{events: events}
	return m
}

// TestGetTopUsers verifies the leaderboard ranks by the requested metric
// and resolves user_id correctly. Locks in the per-metric ordering so a
// future refactor can't silently swap views and watch_time.
func TestGetTopUsers(t *testing.T) {
	now := time.Now()
	events := []*models.AnalyticsEvent{
		// Alice: 3 views, 2 playbacks (60s + 100s watched).
		{Type: "view", UserID: "alice", Timestamp: now.Add(-1 * time.Hour)},
		{Type: "view", UserID: "alice", Timestamp: now.Add(-2 * time.Hour)},
		{Type: "view", UserID: "alice", Timestamp: now.Add(-3 * time.Hour)},
		{Type: "playback", UserID: "alice", Timestamp: now.Add(-4 * time.Hour),
			Data: map[string]any{"position": 60.0, "duration": 600.0}},
		{Type: "playback", UserID: "alice", Timestamp: now.Add(-5 * time.Hour),
			Data: map[string]any{"position": 100.0, "duration": 600.0}},
		// Bob: 1 view, 1 upload.
		{Type: "view", UserID: "bob", Timestamp: now.Add(-1 * time.Hour)},
		{Type: EventUploadSuccess, UserID: "bob", Timestamp: now.Add(-2 * time.Hour)},
	}
	m := moduleWithEvents(t, events)
	ctx := context.Background()

	t.Run("by_views", func(t *testing.T) {
		rows := m.GetTopUsers(ctx, "views", "", "", 5)
		if len(rows) < 2 {
			t.Fatalf("expected 2 rows, got %d", len(rows))
		}
		if rows[0].UserID != "alice" || rows[0].TotalViews != 3 {
			t.Errorf("expected alice with 3 views first, got %+v", rows[0])
		}
		if rows[1].UserID != "bob" || rows[1].TotalViews != 1 {
			t.Errorf("expected bob with 1 view second, got %+v", rows[1])
		}
	})
	t.Run("by_uploads", func(t *testing.T) {
		rows := m.GetTopUsers(ctx, "uploads", "", "", 5)
		if len(rows) < 1 {
			t.Fatalf("expected at least 1 row")
		}
		// Bob is the only one with uploads.
		if rows[0].UserID != "bob" || rows[0].Metric != 1 {
			t.Errorf("expected bob first by uploads, got %+v", rows[0])
		}
	})
	t.Run("by_watch_time", func(t *testing.T) {
		rows := m.GetTopUsers(ctx, "watch_time", "", "", 5)
		if len(rows) < 1 {
			t.Fatalf("expected at least 1 row")
		}
		// Alice watched 60+100=160s. Metric should equal that.
		if rows[0].UserID != "alice" || rows[0].Metric != 160 {
			t.Errorf("expected alice with 160s watch_time, got %+v", rows[0])
		}
	})
	t.Run("anonymous_excluded", func(t *testing.T) {
		// Add an anonymous view; it must not appear in the leaderboard.
		anonEvents := append([]*models.AnalyticsEvent{}, events...)
		anonEvents = append(anonEvents, &models.AnalyticsEvent{
			Type: "view", UserID: "", Timestamp: now,
		})
		mm := moduleWithEvents(t, anonEvents)
		rows := mm.GetTopUsers(ctx, "views", "", "", 10)
		for _, r := range rows {
			if r.UserID == "" {
				t.Errorf("anonymous user_id leaked into top-users: %+v", r)
			}
		}
	})
}

// TestGetTopSearches verifies search query bucketing is case-insensitive
// and tracks empty_count for "no results" queries.
func TestGetTopSearches(t *testing.T) {
	now := time.Now()
	events := []*models.AnalyticsEvent{
		{Type: EventSearch, Timestamp: now, Data: map[string]any{"query": "Nature", "empty": false}},
		{Type: EventSearch, Timestamp: now, Data: map[string]any{"query": "nature", "empty": false}},
		{Type: EventSearch, Timestamp: now, Data: map[string]any{"query": "NATURE", "empty": true}},
		{Type: EventSearch, Timestamp: now, Data: map[string]any{"query": "obscure", "empty": true}},
	}
	m := moduleWithEvents(t, events)
	rows := m.GetTopSearches(context.Background(), "", "", 10)
	if len(rows) != 2 {
		t.Fatalf("expected 2 unique queries, got %d", len(rows))
	}
	// "nature" should aggregate all 3 case-variants.
	var nature *SearchQueryEntry
	for i := range rows {
		if rows[i].Query == "Nature" || rows[i].Query == "nature" || rows[i].Query == "NATURE" {
			nature = &rows[i]
			break
		}
	}
	if nature == nil || nature.Count != 3 {
		t.Errorf("expected nature query with count=3, got %+v", nature)
	}
	if nature == nil || nature.EmptyCount != 1 {
		t.Errorf("expected nature.empty_count=1, got %+v", nature)
	}
}

// TestGetFunnel verifies view → playback → completion math is correct,
// including the authenticated/anonymous split and the duration guard
// (playbacks without duration must NOT advance the funnel).
func TestGetFunnel(t *testing.T) {
	now := time.Now()
	events := []*models.AnalyticsEvent{
		// Authenticated: 4 views, 2 playbacks, 1 completion.
		{Type: "view", UserID: "alice", Timestamp: now.Add(-1 * time.Hour)},
		{Type: "view", UserID: "alice", Timestamp: now.Add(-2 * time.Hour)},
		{Type: "view", UserID: "bob", Timestamp: now.Add(-3 * time.Hour)},
		{Type: "view", UserID: "bob", Timestamp: now.Add(-4 * time.Hour)},
		{Type: "playback", UserID: "alice", Timestamp: now.Add(-5 * time.Hour),
			Data: map[string]any{"duration": 600.0, "progress": 95.0}},
		{Type: "playback", UserID: "bob", Timestamp: now.Add(-6 * time.Hour),
			Data: map[string]any{"duration": 600.0, "progress": 30.0}},
		// Playback without duration must be ignored.
		{Type: "playback", UserID: "alice", Timestamp: now.Add(-7 * time.Hour),
			Data: map[string]any{"progress": 50.0}},
		// Anonymous: 1 view, 0 playbacks.
		{Type: "view", UserID: "", Timestamp: now.Add(-1 * time.Hour)},
	}
	m := moduleWithEvents(t, events)
	f := m.GetFunnel(context.Background(), 30)

	// Overall: 5 views, 2 playbacks (the duration-less one is skipped), 1 completion.
	if f.Stages[0].Count != 5 {
		t.Errorf("expected 5 total views, got %d", f.Stages[0].Count)
	}
	if f.Stages[1].Count != 2 {
		t.Errorf("expected 2 total playbacks (duration-less skipped), got %d", f.Stages[1].Count)
	}
	if f.Stages[2].Count != 1 {
		t.Errorf("expected 1 completion, got %d", f.Stages[2].Count)
	}
	// Authenticated playbacks should be 2 (alice + bob, alice's no-duration one excluded).
	if f.Authenticated[1].Count != 2 {
		t.Errorf("expected 2 authenticated playbacks, got %d", f.Authenticated[1].Count)
	}
	// Anonymous: 1 view, 0 playbacks.
	if f.Anonymous[0].Count != 1 || f.Anonymous[1].Count != 0 {
		t.Errorf("anonymous funnel wrong: %+v", f.Anonymous)
	}
	// from_top_pct on stage 2 (completion) should be 1/5 = 20%.
	if got := f.Stages[2].FromTopPct; got < 19 || got > 21 {
		t.Errorf("expected ~20%% completion-of-views, got %f", got)
	}
	// from_previous_pct on stage 2 (completion of playbacks) should be 1/2 = 50%.
	if got := f.Stages[2].FromPreviousPct; got < 49 || got > 51 {
		t.Errorf("expected ~50%% completion-of-playbacks, got %f", got)
	}
}

// TestGetDeviceBreakdown verifies the UA classifier groups correctly,
// including that bot detection wins over mobile (some bots impersonate).
func TestGetDeviceBreakdown(t *testing.T) {
	now := time.Now()
	events := []*models.AnalyticsEvent{
		// Real iOS Safari UAs include both "iPhone"/"iPad" and "Safari/" tokens.
		{Type: "view", UserID: "u1", UserAgent: "Mozilla/5.0 (iPhone; CPU iPhone OS 16_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/16.0 Mobile/15E148 Safari/604.1", Timestamp: now},
		{Type: "view", UserID: "u2", UserAgent: "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36", Timestamp: now},
		{Type: "view", UserID: "u3", UserAgent: "Googlebot/2.1 (+http://www.google.com/bot.html)", Timestamp: now},
		{Type: "view", UserID: "u4", UserAgent: "Mozilla/5.0 (iPad; CPU OS 16_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/16.0 Mobile/15E148 Safari/604.1", Timestamp: now},
		{Type: "view", UserID: "u5", UserAgent: "curl/7.84.0", Timestamp: now},
	}
	m := moduleWithEvents(t, events)
	devices, browsers := m.GetDeviceBreakdown(context.Background(), 30)

	devMap := make(map[string]int)
	for _, d := range devices {
		devMap[d.Family] = d.Events
	}
	if devMap["Mobile"] != 1 {
		t.Errorf("expected 1 Mobile event (iPhone), got %d (full: %+v)", devMap["Mobile"], devMap)
	}
	if devMap["Desktop"] != 1 {
		t.Errorf("expected 1 Desktop event (Windows Chrome), got %d", devMap["Desktop"])
	}
	if devMap["Tablet"] != 1 {
		t.Errorf("expected 1 Tablet event (iPad), got %d", devMap["Tablet"])
	}
	if devMap["Bot / Tool"] != 2 {
		t.Errorf("expected 2 Bot events (googlebot + curl), got %d", devMap["Bot / Tool"])
	}

	brwMap := make(map[string]int)
	for _, b := range browsers {
		brwMap[b.Family] = b.Events
	}
	if brwMap["Chrome"] != 1 {
		t.Errorf("expected 1 Chrome browser event, got %d (full: %+v)", brwMap["Chrome"], brwMap)
	}
	if brwMap["Safari"] != 2 {
		// iPhone + iPad both report WebKit/Safari.
		t.Errorf("expected 2 Safari browser events (iPhone + iPad), got %d", brwMap["Safari"])
	}
}

// TestGetCohortMetrics verifies DAU/WAU/MAU bucketing by event timestamp.
func TestGetCohortMetrics(t *testing.T) {
	now := time.Now()
	events := []*models.AnalyticsEvent{
		// alice active today AND in week AND in month.
		{Type: "view", UserID: "alice", Timestamp: now.Add(-1 * time.Hour)},
		// bob active 3 days ago — in week + month, not in day.
		{Type: "view", UserID: "bob", Timestamp: now.Add(-72 * time.Hour)},
		// carol active 14 days ago — in month only.
		{Type: "view", UserID: "carol", Timestamp: now.Add(-14 * 24 * time.Hour)},
		// anon traffic must be excluded.
		{Type: "view", UserID: "", Timestamp: now},
	}
	m := moduleWithEvents(t, events)
	c := m.GetCohortMetrics(context.Background())
	if c.DAU != 1 {
		t.Errorf("expected DAU=1 (alice), got %d", c.DAU)
	}
	if c.WAU != 2 {
		t.Errorf("expected WAU=2 (alice+bob), got %d", c.WAU)
	}
	if c.MAU != 3 {
		t.Errorf("expected MAU=3 (alice+bob+carol), got %d", c.MAU)
	}
}

// TestGetHourlyHeatmap verifies the 7×24 grid is always returned (gap-filled)
// and counts events into the correct bucket by local time.
func TestGetHourlyHeatmap(t *testing.T) {
	now := time.Now()
	events := []*models.AnalyticsEvent{
		{Type: "view", Timestamp: now},
		{Type: "view", Timestamp: now},
	}
	m := moduleWithEvents(t, events)
	grid := m.GetHourlyHeatmap(context.Background(), 30)
	if len(grid) != 7*24 {
		t.Fatalf("expected 168 cells, got %d", len(grid))
	}
	loc := time.Now().Location()
	dow := int(now.In(loc).Weekday())
	hour := now.In(loc).Hour()
	idx := dow*24 + hour
	if grid[idx].Count != 2 {
		t.Errorf("expected 2 events in cell [%d][%d], got %d", dow, hour, grid[idx].Count)
	}
}

// TestGetQualityBreakdown verifies streams come from start events and bytes
// from end events, and the unspecified bucket catches missing fields.
func TestGetQualityBreakdown(t *testing.T) {
	now := time.Now()
	events := []*models.AnalyticsEvent{
		{Type: EventStreamStart, Timestamp: now, Data: map[string]any{"quality": "1080p"}},
		{Type: EventStreamStart, Timestamp: now, Data: map[string]any{"quality": "1080p"}},
		{Type: EventStreamStart, Timestamp: now, Data: map[string]any{"quality": "720p"}},
		{Type: EventStreamStart, Timestamp: now, Data: map[string]any{}},
		{Type: EventStreamEnd, Timestamp: now, Data: map[string]any{"quality": "1080p", "bytes_sent": 1000.0}},
		{Type: EventStreamEnd, Timestamp: now, Data: map[string]any{"quality": "720p", "bytes_sent": 500.0}},
	}
	m := moduleWithEvents(t, events)
	rows := m.GetQualityBreakdown(context.Background(), 30)
	got := make(map[string]QualityBucket)
	for _, r := range rows {
		got[r.Quality] = r
	}
	if got["1080p"].Streams != 2 || got["1080p"].BytesSent != 1000 {
		t.Errorf("1080p bucket wrong: %+v", got["1080p"])
	}
	if got["720p"].Streams != 1 || got["720p"].BytesSent != 500 {
		t.Errorf("720p bucket wrong: %+v", got["720p"])
	}
	if got["(unspecified)"].Streams != 1 {
		t.Errorf("unspecified bucket wrong: %+v", got["(unspecified)"])
	}
}

// TestGetMediaDetail verifies per-media drill-down builds correct timelines.
func TestGetMediaDetail(t *testing.T) {
	now := time.Now()
	events := []*models.AnalyticsEvent{
		{Type: "view", MediaID: "media-1", Timestamp: now.Add(-1 * time.Hour)},
		{Type: "view", MediaID: "media-1", Timestamp: now.Add(-25 * time.Hour)},
		{Type: "view", MediaID: "media-2", Timestamp: now.Add(-1 * time.Hour)},
		{Type: "playback", MediaID: "media-1", Timestamp: now.Add(-2 * time.Hour),
			Data: map[string]any{"duration": 600.0}},
	}
	m := moduleWithEvents(t, events)
	d := m.GetMediaDetail(context.Background(), "media-1", 30)
	if d.MediaID != "media-1" {
		t.Errorf("expected media-1, got %s", d.MediaID)
	}
	// Last entry in timeline = today, should have 1 view (the recent one).
	if len(d.ViewTimeline) != 30 {
		t.Errorf("expected 30 days in view timeline, got %d", len(d.ViewTimeline))
	}
	totalViews := 0
	for _, e := range d.ViewTimeline {
		totalViews += int(e.Value)
	}
	if totalViews != 2 {
		t.Errorf("expected 2 views for media-1 across timeline, got %d", totalViews)
	}
	totalPlay := 0
	for _, e := range d.PlaybackTimeline {
		totalPlay += int(e.Value)
	}
	if totalPlay != 1 {
		t.Errorf("expected 1 playback for media-1 across timeline, got %d", totalPlay)
	}
}

// TestGetErrorPaths verifies error grouping by (method, path, status).
func TestGetErrorPaths(t *testing.T) {
	now := time.Now()
	events := []*models.AnalyticsEvent{
		{Type: EventServerError, Timestamp: now, Data: map[string]any{"method": "GET", "path": "/api/foo", "status": 500.0}},
		{Type: EventServerError, Timestamp: now, Data: map[string]any{"method": "GET", "path": "/api/foo", "status": 500.0}},
		{Type: EventServerError, Timestamp: now, Data: map[string]any{"method": "POST", "path": "/api/bar", "status": 502.0}},
	}
	m := moduleWithEvents(t, events)
	rows := m.GetErrorPaths(context.Background(), "", "", 10)
	if len(rows) != 2 {
		t.Fatalf("expected 2 distinct error paths, got %d", len(rows))
	}
	// First row should be the higher-count one.
	if rows[0].Count != 2 || rows[0].Path != "/api/foo" {
		t.Errorf("expected /api/foo with count=2 first, got %+v", rows[0])
	}
}

// TestGetContentGaps verifies the empty-share filter is applied.
func TestGetContentGaps(t *testing.T) {
	now := time.Now()
	events := []*models.AnalyticsEvent{
		// "missing" — 3 searches, all empty → should be a gap.
		{Type: EventSearch, Timestamp: now, Data: map[string]any{"query": "missing", "empty": true}},
		{Type: EventSearch, Timestamp: now, Data: map[string]any{"query": "missing", "empty": true}},
		{Type: EventSearch, Timestamp: now, Data: map[string]any{"query": "missing", "empty": true}},
		// "popular" — 3 searches, none empty → not a gap.
		{Type: EventSearch, Timestamp: now, Data: map[string]any{"query": "popular", "empty": false}},
		{Type: EventSearch, Timestamp: now, Data: map[string]any{"query": "popular", "empty": false}},
		{Type: EventSearch, Timestamp: now, Data: map[string]any{"query": "popular", "empty": false}},
		// "rare" — 1 search empty (below default min_empty=2) → not a gap.
		{Type: EventSearch, Timestamp: now, Data: map[string]any{"query": "rare", "empty": true}},
	}
	m := moduleWithEvents(t, events)
	rows := m.GetContentGaps(context.Background(), "", "", 2, 0.5, 10)
	if len(rows) != 1 {
		t.Fatalf("expected exactly 1 gap (missing), got %d: %+v", len(rows), rows)
	}
	if rows[0].Query != "missing" {
		t.Errorf("expected query=missing, got %s", rows[0].Query)
	}
}
