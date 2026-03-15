package analytics

import (
	"context"
	"sort"
	"time"
)

// maxMediaStatsEntries caps in-memory media stats to avoid unbounded growth (P1-40).
// Entries with oldest LastViewed are evicted when over cap.
const maxMediaStatsEntries = 100000

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
	m.evictExcessMediaStats()
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

// evictExcessMediaStats keeps mediaStats/mediaViewers/mediaDurationSamples under maxMediaStatsEntries
// by removing entries with oldest LastViewed to prevent unbounded memory growth.
func (m *Module) evictExcessMediaStats() {
	m.statsMu.Lock()
	defer m.statsMu.Unlock()
	n := len(m.mediaStats)
	if n <= maxMediaStatsEntries {
		return
	}
	evict := n - maxMediaStatsEntries
	type entry struct {
		mediaID string
		last    time.Time
	}
	var entries []entry
	for id, stats := range m.mediaStats {
		t := time.Time{}
		if stats != nil {
			t = stats.LastViewed
		}
		entries = append(entries, entry{id, t})
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].last.Before(entries[j].last)
	})
	for i := 0; i < evict && i < len(entries); i++ {
		id := entries[i].mediaID
		delete(m.mediaStats, id)
		delete(m.mediaViewers, id)
		delete(m.mediaDurationSamples, id)
	}
	if evict > 0 {
		m.log.Debug("Evicted %d oldest media stats entries (cap %d)", evict, maxMediaStatsEntries)
	}
}
