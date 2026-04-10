package handlers_test

import (
	"net/http"
	"strings"
	"testing"

	"media-server-pro/internal/testutil"
)

const (
	msgExpect401or403 = "expected 401 or 403 without auth, got %d"
)

// TestAnalyticsSummary_RequiresAdmin tests that analytics summary requires admin auth.
func TestAnalyticsSummary_RequiresAdmin(t *testing.T) {
	ts := testutil.NewTestServer(t)

	resp := ts.Request("GET", "/api/analytics", nil)
	defer resp.Body.Close()

	// Without auth, should get 401 or 403
	if resp.StatusCode != http.StatusUnauthorized && resp.StatusCode != http.StatusForbidden {
		t.Errorf(msgExpect401or403, resp.StatusCode)
	}
}

// TestAnalyticsDailyStats_RequiresAdmin tests that daily stats requires admin auth.
func TestAnalyticsDailyStats_RequiresAdmin(t *testing.T) {
	ts := testutil.NewTestServer(t)

	resp := ts.Request("GET", "/api/analytics/daily", nil)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized && resp.StatusCode != http.StatusForbidden {
		t.Errorf(msgExpect401or403, resp.StatusCode)
	}
}

// TestAnalyticsTopMedia_RequiresAdmin tests that top media requires admin auth.
func TestAnalyticsTopMedia_RequiresAdmin(t *testing.T) {
	ts := testutil.NewTestServer(t)

	resp := ts.Request("GET", "/api/analytics/top", nil)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized && resp.StatusCode != http.StatusForbidden {
		t.Errorf(msgExpect401or403, resp.StatusCode)
	}
}

// TestAnalyticsContentPerformance_RequiresAdmin tests that content performance requires admin auth.
func TestAnalyticsContentPerformance_RequiresAdmin(t *testing.T) {
	ts := testutil.NewTestServer(t)

	resp := ts.Request("GET", "/api/analytics/content", nil)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized && resp.StatusCode != http.StatusForbidden {
		t.Errorf(msgExpect401or403, resp.StatusCode)
	}
}

// TestAnalyticsEventSubmit_RequiresAuth tests that event submission requires authentication.
func TestAnalyticsEventSubmit_RequiresAuth(t *testing.T) {
	ts := testutil.NewTestServer(t)

	body := `{"type":"view","media_id":"test-id"}`
	resp := ts.Request("POST", "/api/analytics/events", strings.NewReader(body))
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized && resp.StatusCode != http.StatusForbidden {
		t.Errorf(msgExpect401or403, resp.StatusCode)
	}
}

// TestAnalyticsExport_RequiresAdmin tests that analytics export requires admin auth.
func TestAnalyticsExport_RequiresAdmin(t *testing.T) {
	ts := testutil.NewTestServer(t)

	resp := ts.Request("GET", "/api/admin/analytics/export", nil)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized && resp.StatusCode != http.StatusForbidden {
		t.Errorf(msgExpect401or403, resp.StatusCode)
	}
}
