package handlers

import (
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

// makeCtxWithQuery returns a *gin.Context whose Request URL carries the given
// raw query string. We don't need a router or middleware -- resolveAnalyticsTimeWindow
// only reads c.Query, which is sourced from c.Request.URL.RawQuery.
func makeCtxWithQuery(raw string) *gin.Context {
	req := httptest.NewRequest("GET", "/?"+raw, nil)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = req
	return c
}

// TestResolveAnalyticsTimeWindow_SwapsBackwardsPair verifies that when the
// operator types since/until in the wrong order -- which the admin UI's
// date pickers happily accept -- the resolver silently swaps them rather
// than handing the repository a `ts >= future AND ts <= past` clause that
// matches zero rows. The previous behavior was a frustrating dead end:
// dashboards rendered "no data" with no hint that the inputs were reversed.
func TestResolveAnalyticsTimeWindow_SwapsBackwardsPair(t *testing.T) {
	gin.SetMode(gin.TestMode)

	since := "2026-05-20T00:00:00Z"
	until := "2026-05-10T00:00:00Z" // 10 days BEFORE since
	c := makeCtxWithQuery("since=" + since + "&until=" + until)

	gotSince, gotUntil := resolveAnalyticsTimeWindow(c)

	if gotSince != until {
		t.Errorf("backwards pair: since = %q, want %q (the original until)", gotSince, until)
	}
	if gotUntil != since {
		t.Errorf("backwards pair: until = %q, want %q (the original since)", gotUntil, since)
	}
}

// TestResolveAnalyticsTimeWindow_KeepsCorrectPair confirms the common case
// (since < until) is passed through untouched.
func TestResolveAnalyticsTimeWindow_KeepsCorrectPair(t *testing.T) {
	gin.SetMode(gin.TestMode)
	since := "2026-05-10T00:00:00Z"
	until := "2026-05-20T00:00:00Z"
	c := makeCtxWithQuery("since=" + since + "&until=" + until)

	gotSince, gotUntil := resolveAnalyticsTimeWindow(c)

	if gotSince != since {
		t.Errorf("correct pair: since = %q, want %q", gotSince, since)
	}
	if gotUntil != until {
		t.Errorf("correct pair: until = %q, want %q", gotUntil, until)
	}
}

// TestResolveAnalyticsTimeWindow_OnlyOneSide_NotSwapped guards against
// over-eager swapping when only one bound is supplied -- that's an
// open-ended range and there's nothing to compare against.
func TestResolveAnalyticsTimeWindow_OnlyOneSide_NotSwapped(t *testing.T) {
	gin.SetMode(gin.TestMode)
	// Only `until` set; `since` empty -> open range from epoch to until.
	c := makeCtxWithQuery("until=2026-05-10T00:00:00Z")
	gotSince, gotUntil := resolveAnalyticsTimeWindow(c)
	if gotSince != "" {
		t.Errorf("open-start range: since = %q, want empty", gotSince)
	}
	if gotUntil != "2026-05-10T00:00:00Z" {
		t.Errorf("open-start range: until = %q, want unchanged", gotUntil)
	}
}

// TestResolveAnalyticsTimeWindow_DaysFallback confirms ?days=N still wins
// when no explicit since/until is given.
func TestResolveAnalyticsTimeWindow_DaysFallback(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c := makeCtxWithQuery("days=7")
	gotSince, gotUntil := resolveAnalyticsTimeWindow(c)
	if gotUntil != "" {
		t.Errorf("days fallback: until should remain empty, got %q", gotUntil)
	}
	if gotSince == "" {
		t.Fatalf("days fallback: since should be populated")
	}
	// Sanity-check the computed timestamp is within 1 minute of (now - 7d).
	parsed, err := time.Parse(time.RFC3339, gotSince)
	if err != nil {
		t.Fatalf("days fallback: since = %q, not RFC3339: %v", gotSince, err)
	}
	want := time.Now().AddDate(0, 0, -7)
	if diff := parsed.Sub(want); diff < -time.Minute || diff > time.Minute {
		t.Errorf("days fallback: since drift = %v, want < 1m", diff)
	}
}

// TestResolveAnalyticsTimeWindow_MalformedNotSwapped guards against swapping
// when either side fails to parse -- we leave it alone so the repository's
// own validation surfaces a clearer error than a silent swap would.
func TestResolveAnalyticsTimeWindow_MalformedNotSwapped(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c := makeCtxWithQuery("since=garbage&until=2026-05-10T00:00:00Z")
	gotSince, gotUntil := resolveAnalyticsTimeWindow(c)
	if gotSince != "garbage" {
		t.Errorf("malformed: since = %q, want %q (passed through)", gotSince, "garbage")
	}
	if gotUntil != "2026-05-10T00:00:00Z" {
		t.Errorf("malformed: until = %q, want unchanged", gotUntil)
	}
}
