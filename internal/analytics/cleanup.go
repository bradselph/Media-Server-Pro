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

// cleanupOldDailyStats removes daily stats and dailyUsers entries older than cutoffDate.
func (m *Module) cleanupOldDailyStats(cutoffDate string) {
	m.statsMu.Lock()
	defer m.statsMu.Unlock()
	for date := range m.dailyStats {
		if date < cutoffDate {
			delete(m.dailyStats, date)
		}
	}
	for date := range m.dailyUsers {
		if date < cutoffDate {
			delete(m.dailyUsers, date)
		}
	}
}
