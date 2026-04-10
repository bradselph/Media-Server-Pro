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

// noOpAnalyticsRepo satisfies AnalyticsRepository for unit tests; persistence is not exercised.
type noOpAnalyticsRepo struct{}

func (noOpAnalyticsRepo) Create(context.Context, *models.AnalyticsEvent) error { return nil }

func (noOpAnalyticsRepo) List(context.Context, repositories.AnalyticsFilter) ([]*models.AnalyticsEvent, error) {
	return nil, nil
}

func (noOpAnalyticsRepo) GetByMediaID(context.Context, string) ([]*models.AnalyticsEvent, error) {
	return nil, nil
}

func (noOpAnalyticsRepo) GetByUserID(context.Context, string) ([]*models.AnalyticsEvent, error) {
	return nil, nil
}

func (noOpAnalyticsRepo) DeleteOlderThan(context.Context, string) error { return nil }

func (noOpAnalyticsRepo) Count(context.Context, repositories.AnalyticsFilter) (int64, error) {
	return 0, nil
}

func (noOpAnalyticsRepo) CountByType(context.Context) (map[string]int, error) {
	return nil, nil
}

func testAnalyticsModule(t *testing.T) *Module {
	t.Helper()
	cfg := config.NewManager(filepath.Join(t.TempDir(), "config.json"))
	dbMod := database.NewModule(cfg)
	m, err := NewModule(cfg, dbMod)
	if err != nil {
		t.Fatal(err)
	}
	m.eventRepo = noOpAnalyticsRepo{}
	return m
}

func todayDaily(t *testing.T, m *Module) *models.DailyStats {
	t.Helper()
	today := time.Now().Format(dateFormat)
	for _, d := range m.GetDailyStats(1) {
		if d.Date == today {
			return new(*d)
		}
	}
	t.Fatalf("no daily stats for today %q", today)
	return nil
}

// TrackTrafficEvent is the path used by auth/media handlers for server-side traffic analytics.
// These tests lock in daily counter semantics so admin dashboards and breakdowns cannot silently drift.
func TestTrackTrafficEvent_IncrementsDailyTrafficBreakdown(t *testing.T) {
	m := testAnalyticsModule(t)
	ctx := context.Background()

	m.TrackTrafficEvent(ctx, TrafficEventParams{Type: EventLogin})
	m.TrackTrafficEvent(ctx, TrafficEventParams{Type: EventLogin})
	m.TrackTrafficEvent(ctx, TrafficEventParams{Type: EventLoginFailed})
	m.TrackTrafficEvent(ctx, TrafficEventParams{Type: EventLogout})
	m.TrackTrafficEvent(ctx, TrafficEventParams{Type: EventRegister})
	m.TrackTrafficEvent(ctx, TrafficEventParams{Type: EventAgeGatePass})
	m.TrackTrafficEvent(ctx, TrafficEventParams{Type: EventDownload})
	m.TrackTrafficEvent(ctx, TrafficEventParams{Type: EventSearch})

	d := todayDaily(t, m)
	if d.Logins != 2 {
		t.Errorf("Logins = %d, want 2", d.Logins)
	}
	if d.LoginsFailed != 1 {
		t.Errorf("LoginsFailed = %d, want 1", d.LoginsFailed)
	}
	if d.Logouts != 1 {
		t.Errorf("Logouts = %d, want 1", d.Logouts)
	}
	if d.Registrations != 1 || d.NewUsers != 1 {
		t.Errorf("Registrations=%d NewUsers=%d, want 1 and 1", d.Registrations, d.NewUsers)
	}
	if d.AgeGatePasses != 1 {
		t.Errorf("AgeGatePasses = %d, want 1", d.AgeGatePasses)
	}
	if d.Downloads != 1 {
		t.Errorf("Downloads = %d, want 1", d.Downloads)
	}
	if d.Searches != 1 {
		t.Errorf("Searches = %d, want 1", d.Searches)
	}
}

func TestTrackTrafficEvent_NilDataUsesEmptyMap(t *testing.T) {
	m := testAnalyticsModule(t)
	ctx := context.Background()
	m.TrackTrafficEvent(ctx, TrafficEventParams{Type: EventLogin, Data: nil})
	d := todayDaily(t, m)
	if d.Logins != 1 {
		t.Errorf("Logins = %d, want 1", d.Logins)
	}
}

func TestGetSummary_IncludesTodayTrafficBreakdown(t *testing.T) {
	m := testAnalyticsModule(t)
	ctx := context.Background()
	m.TrackTrafficEvent(ctx, TrafficEventParams{Type: EventLogin})
	m.TrackTrafficEvent(ctx, TrafficEventParams{Type: EventRegister})

	sum := m.GetSummary(ctx)
	if sum.TodayLogins != 1 {
		t.Errorf("TodayLogins = %d, want 1", sum.TodayLogins)
	}
	if sum.TodayRegistrations != 1 {
		t.Errorf("TodayRegistrations = %d, want 1", sum.TodayRegistrations)
	}
}
