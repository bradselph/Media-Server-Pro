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
