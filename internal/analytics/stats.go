package analytics

import (
	"context"
	"sort"
	"time"

	"media-server-pro/internal/repositories"
	"media-server-pro/pkg/models"
)

// MediaViewCount pairs media ID with view count.
type MediaViewCount struct {
	MediaID string `json:"media_id"`
	Views   int    `json:"views"`
}

// Summary holds summary statistics.
type Summary struct {
	TotalEvents    int     `json:"total_events"`
	ActiveSessions int     `json:"active_sessions"`
	TodayViews     int     `json:"today_views"`
	TotalViews     int     `json:"total_views"`
	TotalMedia     int     `json:"total_media"`
	TotalWatchTime float64 `json:"total_watch_time"`

	// Today's traffic breakdown
	TodayLogins        int `json:"today_logins"`
	TodayLoginsFailed  int `json:"today_logins_failed"`
	TodayRegistrations int `json:"today_registrations"`
	TodayAgeGatePasses int `json:"today_age_gate_passes"`
	TodayDownloads     int `json:"today_downloads"`
	TodaySearches      int `json:"today_searches"`
}

// Stats holds statistics for metrics export.
type Stats struct {
	TotalViews     int
	UniqueClients  int
	ActiveSessions int
}

func (m *Module) ensureDailyUsersLocked(today string) {
	if m.dailyUsers[today] == nil {
		m.dailyUsers[today] = make(map[string]struct{})
	}
}

func (m *Module) ensureMediaViewersLocked(mediaID string) {
	if m.mediaViewers[mediaID] == nil {
		m.mediaViewers[mediaID] = make(map[string]struct{})
	}
}

func (m *Module) ensureMediaStatsLocked(mediaID string) *models.ViewStats {
	stats, exists := m.mediaStats[mediaID]
	if !exists {
		stats = &models.ViewStats{}
		m.mediaStats[mediaID] = stats
	}
	return stats
}

func (m *Module) updateStats(event models.AnalyticsEvent) {
	m.statsMu.Lock()
	defer m.statsMu.Unlock()

	// Use event.Timestamp so events with historical timestamps (e.g. replayed
	// or bulk-imported) are bucketed to the correct day, not always "today".
	today := event.Timestamp.Format(dateFormat)
	m.updateDailyStatsLocked(event, today)
	m.updateMediaStatsLocked(event)
}

func (m *Module) updateDailyStatsLocked(event models.AnalyticsEvent, today string) {
	daily := m.ensureDailyStatsLocked(today)
	switch event.Type {
	case "view":
		m.applyViewToDailyStatsLocked(event, daily, today)
	case "playback":
		m.applyPlaybackToDailyStatsLocked(event, daily)
	case EventLogin:
		daily.Logins++
	case EventLoginFailed:
		daily.LoginsFailed++
	case EventLogout:
		daily.Logouts++
	case EventRegister:
		daily.NewUsers++
		daily.Registrations++
	case EventAgeGatePass:
		daily.AgeGatePasses++
	case EventDownload:
		daily.Downloads++
	case EventSearch:
		daily.Searches++
	}
}

func (m *Module) ensureDailyStatsLocked(today string) *models.DailyStats {
	daily, exists := m.dailyStats[today]
	if !exists {
		daily = &models.DailyStats{Date: today}
		m.dailyStats[today] = daily
	}
	return daily
}

func (m *Module) applyViewToDailyStatsLocked(event models.AnalyticsEvent, daily *models.DailyStats, today string) {
	daily.TotalViews++
	if event.UserID != "" {
		m.ensureDailyUsersLocked(today)
		m.dailyUsers[today][event.UserID] = struct{}{}
		daily.UniqueUsers = len(m.dailyUsers[today])
	}
}

func (m *Module) applyPlaybackToDailyStatsLocked(event models.AnalyticsEvent, daily *models.DailyStats) {
	if dur, ok := event.Data["duration"].(float64); ok && dur > 0 {
		daily.TotalWatchTime += dur
	}
}

func (m *Module) updateMediaStatsLocked(event models.AnalyticsEvent) {
	m.applyMediaEventLocked(event)
}

func (m *Module) applyMediaEventLocked(event models.AnalyticsEvent) {
	if event.MediaID == "" {
		return
	}
	stats := m.ensureMediaStatsLocked(event.MediaID)
	switch event.Type {
	case "view":
		m.applyViewToMediaStatsLocked(event, stats)
	case "playback":
		m.applyPlaybackToMediaStatsLocked(event, stats)
	}
}

func (m *Module) applyViewToMediaStatsLocked(event models.AnalyticsEvent, stats *models.ViewStats) {
	stats.TotalViews++
	stats.LastViewed = time.Now()
	if event.UserID != "" {
		m.ensureMediaViewersLocked(event.MediaID)
		m.mediaViewers[event.MediaID][event.UserID] = struct{}{}
		stats.UniqueViewers = len(m.mediaViewers[event.MediaID])
	}
}

func (m *Module) applyPlaybackToMediaStatsLocked(event models.AnalyticsEvent, stats *models.ViewStats) {
	if dur, ok := event.Data["duration"].(float64); ok && dur > 0 {
		stats.TotalPlaybacks++
		m.updateAvgWatchDurationLocked(event.MediaID, stats, dur)
	}
	if progress, ok := event.Data["progress"].(float64); ok && progress >= 90 {
		stats.TotalCompletions++
		stats.CompletionRate = completionRateFromCounts(stats.TotalCompletions, stats.TotalPlaybacks)
	}
}

// updateAvgWatchDurationLocked updates the running average using playback-event count as denominator.
// Caller must hold m.statsMu.
func (m *Module) updateAvgWatchDurationLocked(mediaID string, stats *models.ViewStats, dur float64) {
	n := m.mediaDurationSamples[mediaID]
	n++
	m.mediaDurationSamples[mediaID] = n
	if n == 1 {
		stats.AvgWatchDuration = dur
	} else {
		stats.AvgWatchDuration = (stats.AvgWatchDuration*float64(n-1) + dur) / float64(n)
	}
}

// completionRateFromCounts returns completions/playbacks when playbacks > 0, else 0.
func completionRateFromCounts(completions, playbacks int) float64 {
	if playbacks <= 0 {
		return 0
	}
	return float64(completions) / float64(playbacks)
}

// GetDailyStats returns copies of daily statistics so callers cannot mutate internal state.
func (m *Module) GetDailyStats(days int) []*models.DailyStats {
	m.statsMu.RLock()
	defer m.statsMu.RUnlock()

	var stats []*models.DailyStats
	now := time.Now()

	for i := 0; i < days; i++ {
		date := now.AddDate(0, 0, -i).Format(dateFormat)
		if daily, ok := m.dailyStats[date]; ok {
			stats = append(stats, new(*daily))
		}
	}

	return stats
}

// GetMediaStats returns a copy of statistics for a media item so callers cannot mutate internal state.
func (m *Module) GetMediaStats(mediaID string) *models.ViewStats {
	m.statsMu.RLock()
	defer m.statsMu.RUnlock()

	if stats, ok := m.mediaStats[mediaID]; ok {
		return new(*stats)
	}
	return &models.ViewStats{}
}

// ContentPerformance holds per-media performance metrics.
type ContentPerformance struct {
	MediaID          string  `json:"media_id"`
	TotalViews       int     `json:"total_views"`
	TotalPlaybacks   int     `json:"total_playbacks"`
	TotalCompletions int     `json:"total_completions"`
	CompletionRate   float64 `json:"completion_rate"`
	AvgWatchDuration float64 `json:"avg_watch_duration"`
	UniqueViewers    int     `json:"unique_viewers"`
}

// GetContentPerformance returns media items sorted by completions/views with rich metrics.
func (m *Module) GetContentPerformance(limit int) []ContentPerformance {
	m.statsMu.RLock()
	defer m.statsMu.RUnlock()

	var items []ContentPerformance
	for mediaID, stats := range m.mediaStats {
		items = append(items, ContentPerformance{
			MediaID:          mediaID,
			TotalViews:       stats.TotalViews,
			TotalPlaybacks:   stats.TotalPlaybacks,
			TotalCompletions: stats.TotalCompletions,
			CompletionRate:   stats.CompletionRate,
			AvgWatchDuration: stats.AvgWatchDuration,
			UniqueViewers:    stats.UniqueViewers,
		})
	}

	// Sort by completion count descending, break ties by views
	sort.Slice(items, func(i, j int) bool {
		if items[i].TotalCompletions != items[j].TotalCompletions {
			return items[i].TotalCompletions > items[j].TotalCompletions
		}
		return items[i].TotalViews > items[j].TotalViews
	})

	if limit > 0 && limit < len(items) {
		items = items[:limit]
	}
	return items
}

// GetTotalWatchTime returns the sum of all daily watch time tracked.
func (m *Module) GetTotalWatchTime() float64 {
	m.statsMu.RLock()
	defer m.statsMu.RUnlock()

	var total float64
	for _, ds := range m.dailyStats {
		total += ds.TotalWatchTime
	}
	return total
}

// GetTopMedia returns most viewed media.
func (m *Module) GetTopMedia(limit int) []MediaViewCount {
	m.statsMu.RLock()
	defer m.statsMu.RUnlock()

	var counts []MediaViewCount
	for mediaID, stats := range m.mediaStats {
		counts = append(counts, MediaViewCount{
			MediaID: mediaID,
			Views:   stats.TotalViews,
		})
	}

	sort.Slice(counts, func(i, j int) bool {
		return counts[i].Views > counts[j].Views
	})

	if limit > 0 && limit < len(counts) {
		counts = counts[:limit]
	}

	return counts
}

// GetSummary returns analytics summary.
func (m *Module) GetSummary(ctx context.Context) Summary {
	totalEvents, err := m.eventRepo.Count(ctx, repositories.AnalyticsFilter{})
	if err != nil {
		m.log.Error("Failed to count events: %v", err)
		totalEvents = 0
	}

	cfg := m.config.Get()
	activeSessions := m.countActiveSessions(cfg.Analytics.SessionTimeout)

	m.statsMu.RLock()
	summary := Summary{
		TotalEvents:    int(totalEvents),
		ActiveSessions: activeSessions,
		TotalMedia:     len(m.mediaStats),
	}
	today := time.Now().Format(dateFormat)
	if daily, ok := m.dailyStats[today]; ok {
		summary.TodayViews = daily.TotalViews
		summary.TodayLogins = daily.Logins
		summary.TodayLoginsFailed = daily.LoginsFailed
		summary.TodayRegistrations = daily.Registrations
		summary.TodayAgeGatePasses = daily.AgeGatePasses
		summary.TodayDownloads = daily.Downloads
		summary.TodaySearches = daily.Searches
	}
	for _, stats := range m.mediaStats {
		summary.TotalViews += stats.TotalViews
	}
	for _, ds := range m.dailyStats {
		summary.TotalWatchTime += ds.TotalWatchTime
	}
	m.statsMu.RUnlock()

	return summary
}

// GetStats returns analytics statistics for metrics.
func (m *Module) GetStats() Stats {
	cfg := m.config.Get()
	m.sessionsMu.RLock()
	uniqueClients := len(m.sessions)
	m.sessionsMu.RUnlock()
	activeSessions := m.countActiveSessions(cfg.Analytics.SessionTimeout)

	m.statsMu.RLock()
	totalViews := 0
	for _, mediaStats := range m.mediaStats {
		totalViews += mediaStats.TotalViews
	}
	m.statsMu.RUnlock()

	return Stats{
		TotalViews:     totalViews,
		UniqueClients:  uniqueClients,
		ActiveSessions: activeSessions,
	}
}

// reconstructStats rebuilds in-memory daily and media stats from recent database events.
func (m *Module) reconstructStats() {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cutoff := time.Now().AddDate(0, 0, -30).Format(time.RFC3339)
	events, err := m.eventRepo.List(ctx, repositories.AnalyticsFilter{
		StartDate: cutoff,
		Limit:     m.maxEvents,
	})
	if err != nil {
		m.log.Warn("Failed to load events for stat reconstruction: %v", err)
		return
	}

	m.statsMu.Lock()
	defer m.statsMu.Unlock()
	for _, ev := range events {
		m.rebuildStatsFromEvent(*ev)
	}
	m.log.Debug("Reconstructed stats from %d events", len(events))
}

// rebuildStatsFromEvent updates in-memory maps from a stored event.
// Must be called with m.statsMu held for writing.
func (m *Module) rebuildStatsFromEvent(event models.AnalyticsEvent) {
	date := event.Timestamp.Format(dateFormat)
	daily, exists := m.dailyStats[date]
	if !exists {
		daily = &models.DailyStats{Date: date}
		m.dailyStats[date] = daily
	}
	switch event.Type {
	case "view":
		daily.TotalViews++
		// Reconstruct UniqueUsers so it matches what updateStats would produce.
		if event.UserID != "" {
			m.ensureDailyUsersLocked(date)
			m.dailyUsers[date][event.UserID] = struct{}{}
			daily.UniqueUsers = len(m.dailyUsers[date])
		}
	case EventLogin:
		daily.Logins++
	case EventLoginFailed:
		daily.LoginsFailed++
	case EventLogout:
		daily.Logouts++
	case EventRegister:
		daily.NewUsers++
		daily.Registrations++
	case EventAgeGatePass:
		daily.AgeGatePasses++
	case EventDownload:
		daily.Downloads++
	case EventSearch:
		daily.Searches++
	}

	if event.MediaID == "" {
		return
	}
	stats, exists := m.mediaStats[event.MediaID]
	if !exists {
		stats = &models.ViewStats{}
		m.mediaStats[event.MediaID] = stats
	}
	if event.Type == "view" {
		stats.TotalViews++
		if event.Timestamp.After(stats.LastViewed) {
			stats.LastViewed = event.Timestamp
		}
		// Reconstruct UniqueViewers.
		if event.UserID != "" {
			m.ensureMediaViewersLocked(event.MediaID)
			m.mediaViewers[event.MediaID][event.UserID] = struct{}{}
			stats.UniqueViewers = len(m.mediaViewers[event.MediaID])
		}
		return
	}
	if event.Type == "playback" {
		if dur, ok := event.Data["duration"].(float64); ok && dur > 0 {
			stats.TotalPlaybacks++
			// Reconstruct AvgWatchDuration using the same running-average helper
			// used by the live path so the values are consistent.
			m.updateAvgWatchDurationLocked(event.MediaID, stats, dur)
		}
		if progress, ok := event.Data["progress"].(float64); ok && progress >= 90 {
			stats.TotalCompletions++
		}
		stats.CompletionRate = completionRateFromCounts(stats.TotalCompletions, stats.TotalPlaybacks)
		if event.Timestamp.After(stats.LastViewed) {
			stats.LastViewed = event.Timestamp
		}
	}
}
