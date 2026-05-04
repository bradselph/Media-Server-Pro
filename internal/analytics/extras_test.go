package analytics

import (
	"context"
	"sync"
	"testing"
	"time"

	"media-server-pro/pkg/models"
)

// ── Cache layer (cache.go) ──────────────────────────────────────────────────

func TestAggCache_GetSetExpiry(t *testing.T) {
	c := newAggCache()
	c.set("k1", 42, 50*time.Millisecond)
	v, ok := c.get("k1")
	if !ok || v.(int) != 42 {
		t.Fatalf("expected 42, got %v ok=%v", v, ok)
	}
	// After TTL the entry must be gone.
	time.Sleep(60 * time.Millisecond)
	if _, ok := c.get("k1"); ok {
		t.Errorf("expected entry to expire after ttl")
	}
}

func TestAggCache_InvalidatePrefix(t *testing.T) {
	c := newAggCache()
	c.set("topusers|views|7", 1, time.Second)
	c.set("topusers|uploads|7", 2, time.Second)
	c.set("cohort", 3, time.Second)
	c.invalidate("topusers")
	if _, ok := c.get("topusers|views|7"); ok {
		t.Errorf("topusers prefix should have been invalidated")
	}
	if _, ok := c.get("topusers|uploads|7"); ok {
		t.Errorf("topusers prefix should have been invalidated")
	}
	// cohort key with different prefix must survive.
	if _, ok := c.get("cohort"); !ok {
		t.Errorf("cohort key wrongly invalidated by topusers prefix")
	}
}

func TestAggCache_InvalidateEmptyClearsAll(t *testing.T) {
	c := newAggCache()
	c.set("a", 1, time.Second)
	c.set("b", 2, time.Second)
	c.invalidate("")
	if _, ok := c.get("a"); ok {
		t.Errorf("empty prefix should clear all keys (a survived)")
	}
	if _, ok := c.get("b"); ok {
		t.Errorf("empty prefix should clear all keys (b survived)")
	}
}

func TestMemo_HitsCacheOnSecondCall(t *testing.T) {
	c := newAggCache()
	calls := 0
	compute := func() int { calls++; return 99 }
	v1 := memo(c, "k", time.Second, compute)
	v2 := memo(c, "k", time.Second, compute)
	if v1 != 99 || v2 != 99 {
		t.Errorf("expected 99 from both calls, got %d %d", v1, v2)
	}
	if calls != 1 {
		t.Errorf("expected compute to run exactly once (cache hit on call 2), got %d", calls)
	}
}

func TestModuleInvalidatesCachesForEvent(t *testing.T) {
	m := moduleWithEvents(t, nil)
	// Pre-seed cache entries that should and should NOT be invalidated by
	// a search event.
	m.cache.set("topsearches|||20", []SearchQueryEntry{{Query: "stale"}}, time.Minute)
	m.cache.set("errorpaths|||25", []ErrorPathEntry{{Path: "stale"}}, time.Minute)
	m.invalidateCachesFor(EventSearch)
	if _, ok := m.cache.get("topsearches|||20"); ok {
		t.Errorf("search event should invalidate topsearches entries")
	}
	if _, ok := m.cache.get("errorpaths|||25"); !ok {
		t.Errorf("search event should NOT invalidate errorpaths entries")
	}
}

// ── Subscriptions (subscriptions.go) ────────────────────────────────────────

func TestSubscribeReceivesEvents(t *testing.T) {
	m := moduleWithEvents(t, nil)
	sub := m.Subscribe(8)
	defer sub.Cancel()

	// Broadcast in a goroutine; if the dispatch were blocking on a slow
	// receiver, the test would deadlock.
	go func() {
		m.broadcastEvent(models.AnalyticsEvent{ID: "e1", Type: "view"})
		m.broadcastEvent(models.AnalyticsEvent{ID: "e2", Type: "view"})
	}()
	got := []string{}
	timeout := time.After(time.Second)
	for len(got) < 2 {
		select {
		case ev := <-sub.Events:
			got = append(got, ev.ID)
		case <-timeout:
			t.Fatalf("timed out waiting for events; got so far: %v", got)
		}
	}
	if got[0] != "e1" || got[1] != "e2" {
		t.Errorf("expected [e1,e2], got %v", got)
	}
}

func TestSubscribeBufferOverflowDrops(t *testing.T) {
	m := moduleWithEvents(t, nil)
	// Tiny buffer so we can blow past it without sending thousands of events.
	sub := m.Subscribe(1)
	defer sub.Cancel()
	// First event fills the buffer; second should be dropped silently.
	m.broadcastEvent(models.AnalyticsEvent{ID: "first"})
	m.broadcastEvent(models.AnalyticsEvent{ID: "dropped"})
	first := <-sub.Events
	if first.ID != "first" {
		t.Errorf("expected first event ID=first, got %s", first.ID)
	}
	// No second event should be readable — confirm via short timeout.
	select {
	case ev := <-sub.Events:
		t.Errorf("expected dropped event NOT to arrive, got %s", ev.ID)
	case <-time.After(50 * time.Millisecond):
		// good — slow consumer's overflow was dropped, hot path stayed fast.
	}
}

func TestSubscribeCancelClosesChannel(t *testing.T) {
	m := moduleWithEvents(t, nil)
	sub := m.Subscribe(4)
	sub.Cancel()
	// Reading from a closed channel must return zero-value + ok=false.
	if _, ok := <-sub.Events; ok {
		t.Errorf("expected channel to be closed after Cancel")
	}
}

func TestCloseAllSubscribersOnShutdown(t *testing.T) {
	m := moduleWithEvents(t, nil)
	subs := []EventSubscription{m.Subscribe(2), m.Subscribe(2), m.Subscribe(2)}
	m.closeAllSubscribers()
	// All channels should now be closed.
	var wg sync.WaitGroup
	for _, s := range subs {
		wg.Add(1)
		go func(s EventSubscription) {
			defer wg.Done()
			for range s.Events {
				// drain
			}
		}(s)
	}
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
		// good — all loops exited because their channels closed.
	case <-time.After(time.Second):
		t.Fatalf("subscriber channels did not close after closeAllSubscribers")
	}
}

// ── Retention grid (retention.go inside stats.go) ──────────────────────────

func TestGetRetentionGrid_BasicShape(t *testing.T) {
	now := time.Now()
	// One user signs up "this week", one signs up "last week" — both stay
	// active in the current week. Cohort sizes and retention[0] should
	// reflect that.
	events := []*models.AnalyticsEvent{
		{Type: EventRegister, UserID: "alice", Timestamp: now.Add(-24 * time.Hour)},
		{Type: "view", UserID: "alice", Timestamp: now.Add(-1 * time.Hour)},
		{Type: EventRegister, UserID: "bob", Timestamp: now.Add(-8 * 24 * time.Hour)},
		{Type: "view", UserID: "bob", Timestamp: now.Add(-1 * time.Hour)},
	}
	m := moduleWithEvents(t, events)
	g := m.GetRetentionGrid(context.Background(), 4)
	if g.CohortWeeks != 4 {
		t.Errorf("expected cohort_weeks=4, got %d", g.CohortWeeks)
	}
	if len(g.Weeks) != 4 {
		t.Fatalf("expected 4 cohort rows, got %d", len(g.Weeks))
	}
	// Both cohorts should have W0=100% (everyone is "active" the week they sign up).
	totalSize := 0
	for _, c := range g.Weeks {
		totalSize += c.CohortSize
		if c.CohortSize > 0 && c.Retention[0] != 100 {
			t.Errorf("cohort %s expected W0=100, got %f", c.CohortStart, c.Retention[0])
		}
	}
	if totalSize != 2 {
		t.Errorf("expected 2 total cohort members, got %d", totalSize)
	}
}

func TestGetRetentionGrid_UpperTriangular(t *testing.T) {
	now := time.Now()
	// Cohort 8 weeks ago + activity this week → that cohort should have
	// retention[7] populated. A cohort 1 week old can't have W3 retention.
	events := []*models.AnalyticsEvent{
		{Type: EventRegister, UserID: "alice", Timestamp: now.Add(-8 * 24 * 7 * time.Hour)},
		{Type: "view", UserID: "alice", Timestamp: now},
	}
	m := moduleWithEvents(t, events)
	g := m.GetRetentionGrid(context.Background(), 12)
	for _, c := range g.Weeks {
		// Find the row index — cohort_start to age in weeks.
		if c.CohortSize == 0 {
			continue
		}
		age := 0
		for i := len(g.Weeks) - 1; i >= 0; i-- {
			if g.Weeks[i].CohortStart == c.CohortStart {
				age = len(g.Weeks) - 1 - i
				break
			}
		}
		// Cells beyond the cohort's age must be zero (frontend renders gap).
		for w := age + 1; w < g.CohortWeeks; w++ {
			if c.Retention[w] != 0 {
				t.Errorf("cohort %s age=%d week=%d should be 0 (beyond age), got %f",
					c.CohortStart, age, w, c.Retention[w])
			}
		}
	}
}

// ── Anomaly detection ──────────────────────────────────────────────────────

func TestGetAnomalies_FlagsAbsoluteSpike(t *testing.T) {
	// Module with no daily history triggers the "baseline near zero" path:
	// any day with >=5 events and >=3× the prior max should flag.
	m := moduleWithEvents(t, nil)
	today := time.Now().Format(dateFormat)
	m.statsMu.Lock()
	m.dailyStats[today] = &models.DailyStats{Date: today, ServerErrors: 50}
	m.statsMu.Unlock()
	r := m.GetAnomalies(2.5, 14)
	found := false
	for _, a := range r.Anomalies {
		if a.Metric == "server_errors" && a.Direction == "spike" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected server_errors anomaly when 50 events vs 0 baseline; got %+v", r.Anomalies)
	}
}

func TestGetAnomalies_NoFlagOnFlatBaseline(t *testing.T) {
	// All-zero baseline + zero today → nothing should flag.
	m := moduleWithEvents(t, nil)
	r := m.GetAnomalies(2.5, 14)
	if len(r.Anomalies) != 0 {
		t.Errorf("expected zero anomalies on empty data, got %+v", r.Anomalies)
	}
}

// ── IP summary ─────────────────────────────────────────────────────────────

func TestGetIPSummary_AggregatesAndRanks(t *testing.T) {
	now := time.Now()
	events := []*models.AnalyticsEvent{
		// IP A: many small events → top by events
		{Type: "view", IPAddress: "1.1.1.1", UserID: "u1", Timestamp: now},
		{Type: "view", IPAddress: "1.1.1.1", UserID: "u1", Timestamp: now},
		{Type: "view", IPAddress: "1.1.1.1", UserID: "u2", Timestamp: now},
		{Type: "search", IPAddress: "1.1.1.1", Timestamp: now},
		// IP B: one event but huge bytes → top by bytes
		{Type: EventStreamEnd, IPAddress: "2.2.2.2", UserID: "u3", Timestamp: now,
			Data: map[string]any{"bytes_sent": 1_000_000_000.0}},
		// Empty IP must be ignored.
		{Type: "view", IPAddress: "", Timestamp: now},
	}
	m := moduleWithEvents(t, events)
	s := m.GetIPSummary(context.Background(), 30, 10)
	if s.UniqueIPs != 2 {
		t.Errorf("expected 2 unique IPs, got %d", s.UniqueIPs)
	}
	if len(s.TopByEvents) == 0 || s.TopByEvents[0].IPAddress != "1.1.1.1" {
		t.Errorf("expected 1.1.1.1 first by events, got %+v", s.TopByEvents)
	}
	if len(s.TopByBytes) == 0 || s.TopByBytes[0].IPAddress != "2.2.2.2" {
		t.Errorf("expected 2.2.2.2 first by bytes, got %+v", s.TopByBytes)
	}
	if s.TopByEvents[0].UniqueUserIDs != 2 {
		t.Errorf("expected 2 unique user_ids on 1.1.1.1, got %d", s.TopByEvents[0].UniqueUserIDs)
	}
}

func TestGetIPSummary_BytesFromStreamEndOnly(t *testing.T) {
	// Bytes from non-stream_end events must be ignored — stream_start
	// events also carry quality but never bytes_sent yet, and accidentally
	// summing arbitrary numeric "bytes_sent" fields would corrupt the
	// bandwidth ranking.
	now := time.Now()
	events := []*models.AnalyticsEvent{
		{Type: "view", IPAddress: "1.1.1.1", Timestamp: now,
			Data: map[string]any{"bytes_sent": 999.0}},
	}
	m := moduleWithEvents(t, events)
	s := m.GetIPSummary(context.Background(), 30, 10)
	if len(s.TopByBytes) > 0 && s.TopByBytes[0].BytesSent != 0 {
		t.Errorf("non-stream_end bytes_sent should be ignored, got %d", s.TopByBytes[0].BytesSent)
	}
}

// ── Diagnostics ────────────────────────────────────────────────────────────

func TestGetDiagnostics_ReportsModuleState(t *testing.T) {
	m := moduleWithEvents(t, nil)
	// Pre-populate state so the counts aren't all zero.
	m.cache.set("k", 1, time.Minute)
	m.markDailyDirty(time.Now().Format(dateFormat))
	sub := m.Subscribe(4)
	defer sub.Cancel()
	d := m.GetDiagnostics()
	if d.CacheEntries != 1 {
		t.Errorf("expected 1 cache entry, got %d", d.CacheEntries)
	}
	if d.DirtyDays != 1 {
		t.Errorf("expected 1 dirty day, got %d", d.DirtyDays)
	}
	if d.ActiveSubscribers != 1 {
		t.Errorf("expected 1 subscriber, got %d", d.ActiveSubscribers)
	}
}

// ── Abandonment histogram ──────────────────────────────────────────────────

func TestGetMediaDetail_AbandonmentHistogram(t *testing.T) {
	now := time.Now()
	events := []*models.AnalyticsEvent{
		// Three playbacks at different progress points.
		{Type: "playback", MediaID: "m1", Timestamp: now,
			Data: map[string]any{"duration": 600.0, "progress": 5.0}},
		{Type: "playback", MediaID: "m1", Timestamp: now,
			Data: map[string]any{"duration": 600.0, "progress": 55.0}},
		{Type: "playback", MediaID: "m1", Timestamp: now,
			Data: map[string]any{"duration": 600.0, "progress": 100.0}},
	}
	m := moduleWithEvents(t, events)
	d := m.GetMediaDetail(context.Background(), "m1", 30)
	if len(d.Abandonment) != 10 {
		t.Fatalf("expected 10 abandonment buckets, got %d", len(d.Abandonment))
	}
	// 0-10% bucket should have 1 (progress=5).
	if d.Abandonment[0].Count != 1 {
		t.Errorf("expected 1 in 0-10%% bucket, got %d", d.Abandonment[0].Count)
	}
	// 50-60% bucket should have 1 (progress=55).
	if d.Abandonment[5].Count != 1 {
		t.Errorf("expected 1 in 50-60%% bucket, got %d", d.Abandonment[5].Count)
	}
	// 90-100% bucket should have 1 (progress=100; landed in last bucket
	// per the inclusive-upper-bound policy).
	if d.Abandonment[9].Count != 1 {
		t.Errorf("expected 1 in 90-100%% bucket, got %d", d.Abandonment[9].Count)
	}
}

func TestGetMediaDetail_AbandonmentClampsOutOfRange(t *testing.T) {
	now := time.Now()
	events := []*models.AnalyticsEvent{
		// Forged out-of-range progress values must clamp into 0-100.
		{Type: "playback", MediaID: "m1", Timestamp: now,
			Data: map[string]any{"duration": 600.0, "progress": -50.0}},
		{Type: "playback", MediaID: "m1", Timestamp: now,
			Data: map[string]any{"duration": 600.0, "progress": 200.0}},
	}
	m := moduleWithEvents(t, events)
	d := m.GetMediaDetail(context.Background(), "m1", 30)
	// -50 → bucket 0; 200 → bucket 9. Anywhere else means the clamp leaked.
	if d.Abandonment[0].Count != 1 {
		t.Errorf("negative progress should clamp to bucket 0, got %d", d.Abandonment[0].Count)
	}
	if d.Abandonment[9].Count != 1 {
		t.Errorf("over-100 progress should clamp to bucket 9, got %d", d.Abandonment[9].Count)
	}
	for i := 1; i <= 8; i++ {
		if d.Abandonment[i].Count != 0 {
			t.Errorf("bucket %d should be empty after clamp, got %d", i, d.Abandonment[i].Count)
		}
	}
}

// ── Period comparison ──────────────────────────────────────────────────────

func TestGetPeriodComparison_ComputesDelta(t *testing.T) {
	m := moduleWithEvents(t, nil)
	now := time.Now()
	// Seed dailyStats with synthetic numbers spanning two windows of 3 days.
	for i := 0; i < 3; i++ {
		date := now.AddDate(0, 0, -i).Format(dateFormat)
		m.dailyStats[date] = &models.DailyStats{Date: date, TotalViews: 20}
	}
	for i := 3; i < 6; i++ {
		date := now.AddDate(0, 0, -i).Format(dateFormat)
		m.dailyStats[date] = &models.DailyStats{Date: date, TotalViews: 10}
	}
	cmp := m.GetPeriodComparison("total_views", 3)
	if cmp.Current != 60 {
		t.Errorf("expected current=60 (3×20), got %f", cmp.Current)
	}
	if cmp.Previous != 30 {
		t.Errorf("expected previous=30 (3×10), got %f", cmp.Previous)
	}
	if cmp.DeltaAbsolute != 30 {
		t.Errorf("expected delta=30, got %f", cmp.DeltaAbsolute)
	}
	if cmp.DeltaPct < 99 || cmp.DeltaPct > 101 {
		t.Errorf("expected ~100%% delta, got %f", cmp.DeltaPct)
	}
}

func TestGetPeriodComparison_SentinelOnZeroPrevious(t *testing.T) {
	m := moduleWithEvents(t, nil)
	today := time.Now().Format(dateFormat)
	m.dailyStats[today] = &models.DailyStats{Date: today, TotalViews: 50}
	cmp := m.GetPeriodComparison("total_views", 1)
	// previous=0 + current>0 → sentinel 100% (frontend treats specially).
	if cmp.DeltaPct != 100 {
		t.Errorf("expected sentinel 100%% on zero-prev, got %f", cmp.DeltaPct)
	}
}

// ── Metric timeline ────────────────────────────────────────────────────────

func TestGetMetricTimeline_GapFills(t *testing.T) {
	m := moduleWithEvents(t, nil)
	today := time.Now().Format(dateFormat)
	m.dailyStats[today] = &models.DailyStats{Date: today, TotalViews: 99}
	tl := m.GetMetricTimeline("total_views", 7)
	if len(tl) != 7 {
		t.Fatalf("expected 7 entries (gap-filled), got %d", len(tl))
	}
	// Today's value is the last entry (index 6) and must equal 99.
	if tl[6].Value != 99 {
		t.Errorf("expected today's value=99, got %f", tl[6].Value)
	}
	// Days with no DailyStats entry must come back as 0, not omitted.
	for i := 0; i < 6; i++ {
		if tl[i].Value != 0 {
			t.Errorf("expected gap day %s to be 0, got %f", tl[i].Date, tl[i].Value)
		}
	}
}

func TestGetMetricTimeline_UnknownMetric(t *testing.T) {
	m := moduleWithEvents(t, nil)
	today := time.Now().Format(dateFormat)
	m.dailyStats[today] = &models.DailyStats{Date: today, TotalViews: 99}
	tl := m.GetMetricTimeline("not_a_metric", 5)
	if len(tl) != 5 {
		t.Fatalf("expected 5 entries even for unknown metric, got %d", len(tl))
	}
	for _, e := range tl {
		if e.Value != 0 {
			t.Errorf("unknown metric should yield all zeros, got %s=%f", e.Date, e.Value)
		}
	}
}

// ── Forecast ──────────────────────────────────────────────────────────────

func TestGetMetricForecast_TrendDirection(t *testing.T) {
	m := moduleWithEvents(t, nil)
	now := time.Now()
	// Strictly increasing values over 7 days → forecast should call this "up".
	for i := 0; i < 7; i++ {
		date := now.AddDate(0, 0, -i).Format(dateFormat)
		// Today (i=0) is the highest, going back gets smaller.
		m.dailyStats[date] = &models.DailyStats{Date: date, TotalViews: 100 - i*10}
	}
	f := m.GetMetricForecast("total_views", 7)
	if f.Direction != "up" {
		t.Errorf("expected up direction on rising series, got %s", f.Direction)
	}
	if f.Slope <= 0 {
		t.Errorf("expected positive slope, got %f", f.Slope)
	}
}

func TestGetMetricForecast_FlatSeriesIsFlat(t *testing.T) {
	m := moduleWithEvents(t, nil)
	now := time.Now()
	// Constant value → slope ~0 → direction "flat".
	for i := 0; i < 7; i++ {
		date := now.AddDate(0, 0, -i).Format(dateFormat)
		m.dailyStats[date] = &models.DailyStats{Date: date, TotalViews: 50}
	}
	f := m.GetMetricForecast("total_views", 7)
	if f.Direction != "flat" {
		t.Errorf("expected flat direction on constant series, got %s", f.Direction)
	}
}

func TestGetMetricForecast_NegativeProjectionClampedToZero(t *testing.T) {
	m := moduleWithEvents(t, nil)
	now := time.Now()
	// A line so steep downward that the projection would naturally land
	// below zero. Forecast must clamp to 0 — negative event counts are
	// nonsense and would render as a confusing "-12 views tomorrow".
	for i := 0; i < 7; i++ {
		date := now.AddDate(0, 0, -i).Format(dateFormat)
		m.dailyStats[date] = &models.DailyStats{Date: date, TotalViews: i * 5}
	}
	f := m.GetMetricForecast("total_views", 7)
	if f.Projection < 0 {
		t.Errorf("expected projection clamped to 0, got %f", f.Projection)
	}
}

// ── Range comparison ──────────────────────────────────────────────────────

func TestGetRangeComparison_Math(t *testing.T) {
	m := moduleWithEvents(t, nil)
	// Range A: 2026-04-01..2026-04-03 (3 days × 10 views = 30)
	for d := 1; d <= 3; d++ {
		date := time.Date(2026, 4, d, 0, 0, 0, 0, time.Local).Format(dateFormat)
		m.dailyStats[date] = &models.DailyStats{Date: date, TotalViews: 10}
	}
	// Range B: 2026-04-04..2026-04-06 (3 days × 25 views = 75)
	for d := 4; d <= 6; d++ {
		date := time.Date(2026, 4, d, 0, 0, 0, 0, time.Local).Format(dateFormat)
		m.dailyStats[date] = &models.DailyStats{Date: date, TotalViews: 25}
	}
	r := m.GetRangeComparison("2026-04-01", "2026-04-03", "2026-04-04", "2026-04-06")
	var views *RangeMetric
	for i := range r.Metrics {
		if r.Metrics[i].Metric == "total_views" {
			views = &r.Metrics[i]
		}
	}
	if views == nil {
		t.Fatalf("expected total_views row in result")
	}
	if views.A != 30 {
		t.Errorf("expected A=30, got %f", views.A)
	}
	if views.B != 75 {
		t.Errorf("expected B=75, got %f", views.B)
	}
	if views.DeltaAbsolute != 45 {
		t.Errorf("expected delta=45, got %f", views.DeltaAbsolute)
	}
	// 45/30 = 150% increase
	if views.DeltaPct < 149 || views.DeltaPct > 151 {
		t.Errorf("expected ~150%% delta, got %f", views.DeltaPct)
	}
}

func TestGetRangeComparison_ZeroABehavesSensibly(t *testing.T) {
	m := moduleWithEvents(t, nil)
	// Only range B has data → delta_pct sentinel = 100 (frontend treats specially)
	date := time.Date(2026, 4, 4, 0, 0, 0, 0, time.Local).Format(dateFormat)
	m.dailyStats[date] = &models.DailyStats{Date: date, TotalViews: 50}
	r := m.GetRangeComparison("2026-04-01", "2026-04-03", "2026-04-04", "2026-04-04")
	for _, row := range r.Metrics {
		if row.Metric == "total_views" {
			if row.A != 0 || row.B != 50 || row.DeltaPct != 100 {
				t.Errorf("expected A=0 B=50 delta_pct=100, got %+v", row)
			}
			return
		}
	}
	t.Fatalf("total_views row missing from result")
}

// ── Custom alerts ─────────────────────────────────────────────────────────

func TestEvaluateAlerts_OperatorsTrigger(t *testing.T) {
	m := moduleWithEvents(t, nil)
	today := time.Now().Format(dateFormat)
	m.dailyStats[today] = &models.DailyStats{Date: today, ServerErrors: 10}

	rules := []AlertRule{
		{ID: "1", Name: "gt-trip", Metric: "server_errors", Operator: "gt", Threshold: 5, Window: 1},
		{ID: "2", Name: "gt-noop", Metric: "server_errors", Operator: "gt", Threshold: 50, Window: 1},
		{ID: "3", Name: "le-trip", Metric: "server_errors", Operator: "le", Threshold: 10, Window: 1},
		{ID: "4", Name: "eq-trip", Metric: "server_errors", Operator: "eq", Threshold: 10, Window: 1},
		{ID: "5", Name: "lt-trip", Metric: "server_errors", Operator: "lt", Threshold: 11, Window: 1},
	}
	results := m.EvaluateAlerts(rules)
	wants := map[string]bool{"1": true, "2": false, "3": true, "4": true, "5": true}
	for _, r := range results {
		want, ok := wants[r.Rule.ID]
		if !ok {
			t.Fatalf("unexpected rule id %s", r.Rule.ID)
		}
		if r.Triggered != want {
			t.Errorf("rule %s (%s %s %f): expected triggered=%v got %v (value=%f)",
				r.Rule.ID, r.Rule.Metric, r.Rule.Operator, r.Rule.Threshold, want, r.Triggered, r.Value)
		}
	}
}

func TestEvaluateAlerts_WindowSumsTrailingDays(t *testing.T) {
	m := moduleWithEvents(t, nil)
	now := time.Now()
	// Today=2, yesterday=3 — sum over a 2-day window should be 5.
	m.dailyStats[now.Format(dateFormat)] = &models.DailyStats{Date: now.Format(dateFormat), Logins: 2}
	yesterday := now.AddDate(0, 0, -1).Format(dateFormat)
	m.dailyStats[yesterday] = &models.DailyStats{Date: yesterday, Logins: 3}

	results := m.EvaluateAlerts([]AlertRule{
		{ID: "w", Metric: "logins", Operator: "ge", Threshold: 5, Window: 2},
	})
	if len(results) != 1 || !results[0].Triggered || results[0].Value != 5 {
		t.Errorf("expected triggered=true value=5, got %+v", results[0])
	}
}

func TestEvaluateAlerts_EmptyRulesReturnsEmpty(t *testing.T) {
	m := moduleWithEvents(t, nil)
	results := m.EvaluateAlerts(nil)
	if len(results) != 0 {
		t.Errorf("expected empty result for empty rule list, got %+v", results)
	}
}

func TestEvaluateAlerts_UnknownOperatorDoesNotTrigger(t *testing.T) {
	m := moduleWithEvents(t, nil)
	today := time.Now().Format(dateFormat)
	m.dailyStats[today] = &models.DailyStats{Date: today, ServerErrors: 100}
	results := m.EvaluateAlerts([]AlertRule{
		{ID: "x", Metric: "server_errors", Operator: "BOGUS", Threshold: 1, Window: 1},
	})
	if len(results) != 1 || results[0].Triggered {
		t.Errorf("unknown operator should not trigger, got %+v", results)
	}
}

// ── Health snapshot (stats.go AnalyticsHealth) ──────────────────────────────

func TestAnalyticsHealth_BeforeFirstFlushReportsZeroLag(t *testing.T) {
	m := moduleWithEvents(t, nil)
	m.healthMu.Lock()
	m.healthy = true
	m.healthMsg = "Running"
	m.healthMu.Unlock()
	h := m.AnalyticsHealth()
	if !h.Healthy || h.Status != "Running" {
		t.Errorf("expected healthy=true status=Running, got %+v", h)
	}
	// LastFlush is the zero time before the first flush, and FlushLagSeconds
	// must NOT report time.Since(zero) — that would be ~57 years on every
	// call and trigger every alert immediately.
	if !h.LastFlush.IsZero() {
		t.Errorf("expected zero last_flush before first flush, got %v", h.LastFlush)
	}
	if h.FlushLagSeconds != 0 {
		t.Errorf("expected 0s flush lag before first flush, got %f", h.FlushLagSeconds)
	}
}

func TestAnalyticsHealth_AfterFlushReportsLag(t *testing.T) {
	m := moduleWithEvents(t, nil)
	m.lastFlushMu.Lock()
	m.lastFlush = time.Now().Add(-45 * time.Second)
	m.lastFlushMu.Unlock()
	h := m.AnalyticsHealth()
	if h.FlushLagSeconds < 44 || h.FlushLagSeconds > 60 {
		t.Errorf("expected ~45s flush lag, got %f", h.FlushLagSeconds)
	}
}

// ── Backfill (stats.go BackfillDailyStats) ─────────────────────────────────

func TestBackfillDailyStats_RebuildsFromRawEvents(t *testing.T) {
	// Backfill is the recovery path when persisted DailyStats drift away
	// from the raw event truth. This test verifies the per-event arithmetic
	// matches updateDailyStatsLocked column-by-column for the most common
	// event types — locks the parity in so a future event addition can't
	// silently make backfill diverge from the live counter path.
	loc := time.Now().Location()
	day, _ := time.ParseInLocation(dateFormat, "2026-04-15", loc)
	events := []*models.AnalyticsEvent{
		{Type: "view", UserID: "alice", Timestamp: day.Add(1 * time.Hour)},
		{Type: "view", UserID: "alice", Timestamp: day.Add(2 * time.Hour)}, // same user — UniqueUsers stays 1
		{Type: "view", UserID: "bob", Timestamp: day.Add(3 * time.Hour)},
		{Type: "playback", UserID: "alice", Timestamp: day.Add(4 * time.Hour),
			Data: map[string]any{"position": 120.0, "duration": 600.0}},
		{Type: EventLogin, UserID: "alice", Timestamp: day.Add(5 * time.Hour)},
		{Type: EventLoginFailed, Timestamp: day.Add(6 * time.Hour)},
		{Type: EventRegister, UserID: "carol", Timestamp: day.Add(7 * time.Hour)},
		{Type: EventDownload, UserID: "alice", Timestamp: day.Add(8 * time.Hour)},
		{Type: EventSearch, UserID: "alice", Timestamp: day.Add(9 * time.Hour)},
		{Type: EventStreamEnd, UserID: "bob", Timestamp: day.Add(10 * time.Hour),
			Data: map[string]any{"bytes_sent": float64(1_500_000)}},
		{Type: EventStreamEnd, UserID: "bob", Timestamp: day.Add(11 * time.Hour),
			Data: map[string]any{"bytes_sent": int64(500_000)}},
		{Type: EventServerError, Timestamp: day.Add(12 * time.Hour)},
		// An event from a *different* day must NOT be counted.
		{Type: "view", UserID: "alice", Timestamp: day.AddDate(0, 0, 1).Add(1 * time.Hour)},
	}
	m := moduleWithEvents(t, events)

	got, err := m.BackfillDailyStats(context.Background(), "2026-04-15")
	if err != nil {
		t.Fatalf("backfill: %v", err)
	}
	if got.TotalViews != 3 {
		t.Errorf("TotalViews: got %d, want 3", got.TotalViews)
	}
	if got.UniqueUsers != 2 {
		t.Errorf("UniqueUsers: got %d, want 2", got.UniqueUsers)
	}
	if got.TotalWatchTime != 120.0 {
		t.Errorf("TotalWatchTime: got %f, want 120", got.TotalWatchTime)
	}
	if got.Logins != 1 {
		t.Errorf("Logins: got %d, want 1", got.Logins)
	}
	if got.LoginsFailed != 1 {
		t.Errorf("LoginsFailed: got %d, want 1", got.LoginsFailed)
	}
	if got.Registrations != 1 || got.NewUsers != 1 {
		t.Errorf("Registrations/NewUsers: got %d/%d, want 1/1", got.Registrations, got.NewUsers)
	}
	if got.Downloads != 1 {
		t.Errorf("Downloads: got %d, want 1", got.Downloads)
	}
	if got.Searches != 1 {
		t.Errorf("Searches: got %d, want 1", got.Searches)
	}
	if got.StreamEnds != 2 {
		t.Errorf("StreamEnds: got %d, want 2", got.StreamEnds)
	}
	if got.BytesServed != 2_000_000 {
		t.Errorf("BytesServed: got %d, want 2,000,000 (sums float64+int64 payloads)", got.BytesServed)
	}
	if got.ServerErrors != 1 {
		t.Errorf("ServerErrors: got %d, want 1", got.ServerErrors)
	}
}

func TestBackfillDailyStats_RejectsBadDate(t *testing.T) {
	m := moduleWithEvents(t, nil)
	if _, err := m.BackfillDailyStats(context.Background(), ""); err == nil {
		t.Error("expected error for empty date")
	}
	if _, err := m.BackfillDailyStats(context.Background(), "2026/04/15"); err == nil {
		t.Error("expected error for non-ISO date format")
	}
	if _, err := m.BackfillDailyStats(context.Background(), "yesterday"); err == nil {
		t.Error("expected error for non-date string")
	}
}

func TestBackfillDailyStats_ReplacesInMemoryEntry(t *testing.T) {
	// If persisted DailyStats drifted upward (e.g. double-counted events),
	// backfill must overwrite the in-memory map rather than add to it.
	day, _ := time.ParseInLocation(dateFormat, "2026-04-16", time.Now().Location())
	events := []*models.AnalyticsEvent{
		{Type: "view", UserID: "alice", Timestamp: day.Add(1 * time.Hour)},
	}
	m := moduleWithEvents(t, events)
	// Pre-seed an inflated entry to simulate drift.
	m.dailyStats["2026-04-16"] = &models.DailyStats{Date: "2026-04-16", TotalViews: 999}

	got, err := m.BackfillDailyStats(context.Background(), "2026-04-16")
	if err != nil {
		t.Fatalf("backfill: %v", err)
	}
	if got.TotalViews != 1 {
		t.Errorf("backfill didn't replace inflated counter: got %d, want 1", got.TotalViews)
	}
	if m.dailyStats["2026-04-16"].TotalViews != 1 {
		t.Errorf("in-memory entry not replaced: got %d, want 1", m.dailyStats["2026-04-16"].TotalViews)
	}
}

// ── IP summary truncation ──────────────────────────────────────────────────

func TestGetIPSummary_HonorsLimit(t *testing.T) {
	// computeIPSummary truncates TopByEvents and TopByBytes to `limit`
	// independently. This locks in that contract — without it, accidentally
	// applying the limit to the unsorted `flat` slice would silently drop
	// the heaviest IPs from the report.
	now := time.Now()
	var events []*models.AnalyticsEvent
	ips := []string{"10.0.0.1", "10.0.0.2", "10.0.0.3", "10.0.0.4", "10.0.0.5", "10.0.0.6", "10.0.0.7", "10.0.0.8"}
	for i, ip := range ips {
		events = append(events, &models.AnalyticsEvent{
			Type: "view", UserID: "u-" + ip, IPAddress: ip, Timestamp: now.Add(-time.Duration(i) * time.Hour),
		})
	}
	m := moduleWithEvents(t, events)
	out := m.GetIPSummary(context.Background(), 7, 3)
	if out.UniqueIPs != 8 {
		t.Errorf("UniqueIPs: got %d, want 8", out.UniqueIPs)
	}
	if len(out.TopByEvents) != 3 {
		t.Errorf("TopByEvents truncation: got %d, want 3", len(out.TopByEvents))
	}
	if len(out.TopByBytes) != 3 {
		t.Errorf("TopByBytes truncation: got %d, want 3", len(out.TopByBytes))
	}
}

// ── Diagnostics — extended (stats.go GetDiagnostics) ───────────────────────

func TestGetDiagnostics_ReportsRuntimeState(t *testing.T) {
	m := moduleWithEvents(t, nil)
	m.healthMu.Lock()
	m.healthy = true
	m.healthMu.Unlock()
	m.markDailyDirty("2026-04-01")
	m.markDailyDirty("2026-04-02")
	m.cache.set("k1", 1, time.Second)
	m.cache.set("k2", 2, time.Second)
	m.statsMu.Lock()
	m.mediaStats["m1"] = &models.ViewStats{}
	m.statsMu.Unlock()
	m.sessionsMu.Lock()
	m.sessions["sess-a"] = &sessionData{}
	m.sessions["sess-b"] = &sessionData{}
	m.sessionsMu.Unlock()
	m.maxEvents = 12345

	d := m.GetDiagnostics()
	if !d.Healthy {
		t.Error("Healthy: got false, want true")
	}
	if d.DirtyDays != 2 {
		t.Errorf("DirtyDays: got %d, want 2", d.DirtyDays)
	}
	if d.CacheEntries != 2 {
		t.Errorf("CacheEntries: got %d, want 2", d.CacheEntries)
	}
	if d.MediaTracked != 1 {
		t.Errorf("MediaTracked: got %d, want 1", d.MediaTracked)
	}
	if d.SessionsTracked != 2 {
		t.Errorf("SessionsTracked: got %d, want 2", d.SessionsTracked)
	}
	if d.MaxReconstruct != 12345 {
		t.Errorf("MaxReconstruct: got %d, want 12345", d.MaxReconstruct)
	}
}

// ── Search clickthrough (stats.go searchClickthroughForMedia) ──────────────

func TestSearchClickthrough_BasicCorrelation(t *testing.T) {
	// Three users search and immediately view different media. Only views
	// preceded by a search within 5 minutes (and from the same session)
	// should be attributed.
	now := time.Now()
	events := []*models.AnalyticsEvent{
		// alice/sess1: searches "cats", views media-1 90s later → counted.
		{Type: EventSearch, UserID: "alice", SessionID: "sess1", Timestamp: now.Add(-10 * time.Minute),
			Data: map[string]any{"query": "cats"}},
		{Type: "view", UserID: "alice", SessionID: "sess1", MediaID: "media-1", Timestamp: now.Add(-10*time.Minute + 90*time.Second)},
		// bob/sess2: searches "cats" too, views media-1 → counted.
		{Type: EventSearch, UserID: "bob", SessionID: "sess2", Timestamp: now.Add(-7 * time.Minute),
			Data: map[string]any{"query": "cats"}},
		{Type: "view", UserID: "bob", SessionID: "sess2", MediaID: "media-1", Timestamp: now.Add(-7*time.Minute + 30*time.Second)},
		// carol/sess3: searches "dogs", views media-1 → counted as "dogs".
		{Type: EventSearch, UserID: "carol", SessionID: "sess3", Timestamp: now.Add(-5 * time.Minute),
			Data: map[string]any{"query": "dogs"}},
		{Type: "view", UserID: "carol", SessionID: "sess3", MediaID: "media-1", Timestamp: now.Add(-5*time.Minute + 60*time.Second)},
		// dave/sess4: searches "cars" but waits 10 minutes → outside window, NOT counted.
		{Type: EventSearch, UserID: "dave", SessionID: "sess4", Timestamp: now.Add(-30 * time.Minute),
			Data: map[string]any{"query": "cars"}},
		{Type: "view", UserID: "dave", SessionID: "sess4", MediaID: "media-1", Timestamp: now.Add(-15 * time.Minute)},
		// eve/sess5: views media-1 with no preceding search → not counted.
		{Type: "view", UserID: "eve", SessionID: "sess5", MediaID: "media-1", Timestamp: now.Add(-2 * time.Minute)},
	}
	m := moduleWithEvents(t, events)
	got := m.searchClickthroughForMedia(context.Background(), "media-1", 30, 10)
	if len(got) != 2 {
		t.Fatalf("expected 2 distinct queries (cats, dogs), got %d: %+v", len(got), got)
	}
	// "cats" should rank first (2 hits) over "dogs" (1 hit).
	if got[0].Query != "cats" || got[0].Count != 2 {
		t.Errorf("expected cats=2 first, got %+v", got[0])
	}
	if got[1].Query != "dogs" || got[1].Count != 1 {
		t.Errorf("expected dogs=1 second, got %+v", got[1])
	}
}

func TestSearchClickthrough_FallsBackToUserIDWhenNoSession(t *testing.T) {
	// When events lack session_id, the correlator must still match by user_id
	// — older events written before session tracking landed should not be lost.
	now := time.Now()
	events := []*models.AnalyticsEvent{
		{Type: EventSearch, UserID: "alice", Timestamp: now.Add(-3 * time.Minute),
			Data: map[string]any{"query": "fallback"}},
		{Type: "view", UserID: "alice", MediaID: "media-9", Timestamp: now.Add(-1 * time.Minute)},
	}
	m := moduleWithEvents(t, events)
	got := m.searchClickthroughForMedia(context.Background(), "media-9", 30, 10)
	if len(got) != 1 || got[0].Query != "fallback" || got[0].Count != 1 {
		t.Errorf("expected 1×fallback, got %+v", got)
	}
}

func TestSearchClickthrough_DifferentSessionsDontCrossAttribute(t *testing.T) {
	// alice/sess1 searches but a view from a *different* session must not
	// be attributed to her search.
	now := time.Now()
	events := []*models.AnalyticsEvent{
		{Type: EventSearch, UserID: "alice", SessionID: "sess1", Timestamp: now.Add(-2 * time.Minute),
			Data: map[string]any{"query": "leak"}},
		{Type: "view", UserID: "alice", SessionID: "sess2", MediaID: "media-7", Timestamp: now.Add(-1 * time.Minute)},
	}
	m := moduleWithEvents(t, events)
	got := m.searchClickthroughForMedia(context.Background(), "media-7", 30, 10)
	if len(got) != 0 {
		t.Errorf("expected empty result (different sessions), got %+v", got)
	}
}

func TestAnalyticsHealth_CountsDirtyDays(t *testing.T) {
	m := moduleWithEvents(t, nil)
	m.markDailyDirty("2026-01-01")
	m.markDailyDirty("2026-01-02")
	m.markDailyDirty("2026-01-03")
	if h := m.AnalyticsHealth(); h.DirtyDays != 3 {
		t.Errorf("expected 3 dirty days, got %d", h.DirtyDays)
	}
}
