package analytics

import (
	"context"
	"time"
)

// cleanup removes old data.
func (m *Module) cleanup() {
	cfg := m.config.Get()
	cutoff := time.Now().AddDate(0, 0, -cfg.Analytics.RetentionDays)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	before := cutoff.Format(time.RFC3339)
	if err := m.eventRepo.DeleteOlderThan(ctx, before); err != nil {
		m.log.Error("Failed to cleanup old events: %v", err)
	}

	m.cleanupStaleSessions(cfg.Analytics.SessionTimeout)
	m.cleanupOldDailyStats(cutoff.Format(dateFormat))
	m.log.Debug("Completed cleanup of old analytics data")
}

// cleanupOldDailyStats removes daily stats older than cutoffDate.
// TODO: Incomplete cleanup — dailyUsers map is never cleaned up here. It is keyed by date
// like dailyStats, but only dailyStats entries are deleted. Over time, dailyUsers will
// accumulate stale date entries that leak memory. Should also delete m.dailyUsers[date]
// for old dates.
func (m *Module) cleanupOldDailyStats(cutoffDate string) {
	m.statsMu.Lock()
	defer m.statsMu.Unlock()
	for date := range m.dailyStats {
		if date < cutoffDate {
			delete(m.dailyStats, date)
		}
	}
}
