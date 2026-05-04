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

	// Today's traffic breakdown — every counter that DailyStats tracks gets a
	// today_<x> projection so the dashboard widgets can render without doing
	// their own date math against the daily array.
	TodayLogins             int `json:"today_logins"`
	TodayLoginsFailed       int `json:"today_logins_failed"`
	TodayLogouts            int `json:"today_logouts"`
	TodayRegistrations      int `json:"today_registrations"`
	TodayAgeGatePasses      int `json:"today_age_gate_passes"`
	TodayDownloads          int `json:"today_downloads"`
	TodaySearches           int `json:"today_searches"`
	TodayFavoritesAdded     int `json:"today_favorites_added"`
	TodayFavoritesRemoved   int `json:"today_favorites_removed"`
	TodayRatingsSet         int `json:"today_ratings_set"`
	TodayPlaylistsCreated   int `json:"today_playlists_created"`
	TodayPlaylistsDeleted   int `json:"today_playlists_deleted"`
	TodayPlaylistItemsAdded int `json:"today_playlist_items_added"`
	TodayUploadsSucceeded   int `json:"today_uploads_succeeded"`
	TodayUploadsFailed      int `json:"today_uploads_failed"`
	TodayPasswordChanges    int `json:"today_password_changes"`
	TodayAccountDeletions   int `json:"today_account_deletions"`
	TodayHLSStarts          int `json:"today_hls_starts"`
	TodayHLSErrors          int `json:"today_hls_errors"`
	TodayMediaDeletions     int `json:"today_media_deletions"`
	TodayAPITokensCreated   int `json:"today_api_tokens_created"`
	TodayAPITokensRevoked   int `json:"today_api_tokens_revoked"`
	TodayAdminActions       int `json:"today_admin_actions"`
	TodayServerErrors       int `json:"today_server_errors"`

	// Engagement / access-control / admin-bulk projections.
	TodayStreamStarts       int   `json:"today_stream_starts"`
	TodayStreamEnds         int   `json:"today_stream_ends"`
	TodayBytesServed        int64 `json:"today_bytes_served"`
	TodayMatureBlocked      int   `json:"today_mature_blocked"`
	TodayPermissionDenied   int   `json:"today_permission_denied"`
	TodayPreferencesChanges int   `json:"today_preferences_changes"`
	TodayBulkDeletes        int   `json:"today_bulk_deletes"`
	TodayBulkUpdates        int   `json:"today_bulk_updates"`
	TodayUserRoleChanges    int   `json:"today_user_role_changes"`
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
	// Use event.Timestamp so events with historical timestamps (e.g. replayed
	// or bulk-imported) are bucketed to the correct day, not always "today".
	today := event.Timestamp.Format(dateFormat)
	m.updateDailyStatsLocked(event, today)
	m.updateMediaStatsLocked(event)
	m.statsMu.Unlock()
	// Persistence happens out-of-band on the flush ticker; here we just record
	// the date as dirty so the flush picks it up. Done outside statsMu because
	// markDailyDirty has its own tiny mutex and we want to release the big
	// statsMu writer lock as quickly as possible.
	m.markDailyDirty(today)
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
	case EventFavoriteAdd:
		daily.FavoritesAdded++
	case EventFavoriteRemove:
		daily.FavoritesRemoved++
	case EventRatingSet:
		daily.RatingsSet++
	case EventPlaylistCreate:
		daily.PlaylistsCreated++
	case EventPlaylistDelete:
		daily.PlaylistsDeleted++
	case EventPlaylistItemAdd:
		daily.PlaylistItemsAdded++
	case EventUploadSuccess:
		daily.UploadsSucceeded++
	case EventUploadFailed:
		daily.UploadsFailed++
	case EventPasswordChange:
		daily.PasswordChanges++
	case EventAccountDelete:
		daily.AccountDeletions++
	case EventHLSStart:
		daily.HLSStarts++
	case EventHLSError:
		daily.HLSErrors++
	case EventMediaDeleted:
		daily.MediaDeletions++
	case EventAPITokenCreate:
		daily.APITokensCreated++
	case EventAPITokenRevoke:
		daily.APITokensRevoked++
	case EventAdminAction:
		daily.AdminActions++
	case EventServerError:
		daily.ServerErrors++
	case EventStreamStart:
		daily.StreamStarts++
	case EventStreamEnd:
		daily.StreamEnds++
		// stream_end events carry the bytes_sent total for the session; sum
		// these into BytesServed so the dashboard can show a real bandwidth
		// number rather than a session count alone.
		if bs, ok := event.Data["bytes_sent"].(float64); ok && bs > 0 {
			daily.BytesServed += int64(bs)
		}
		if bs, ok := event.Data["bytes_sent"].(int64); ok && bs > 0 {
			daily.BytesServed += bs
		}
	case EventMatureBlocked:
		daily.MatureBlocked++
	case EventPermissionDenied:
		daily.PermissionDenied++
	case EventPreferencesChange:
		daily.PreferencesChanges++
	case EventBulkDelete:
		daily.BulkDeletes++
	case EventBulkUpdate:
		daily.BulkUpdates++
	case EventUserRoleChange:
		daily.UserRoleChanges++
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
	// Use the actual watched time (position), not the full media duration.
	// Position represents how far the user watched; duration is the total length.
	pos, _ := event.Data["position"].(float64)
	dur, _ := event.Data["duration"].(float64)
	watchTime := pos
	if dur > 0 && watchTime > dur {
		watchTime = dur
	}
	if watchTime > 0 {
		daily.TotalWatchTime += watchTime
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
	stats.LastViewed = event.Timestamp
	if event.UserID != "" {
		m.ensureMediaViewersLocked(event.MediaID)
		m.mediaViewers[event.MediaID][event.UserID] = struct{}{}
		stats.UniqueViewers = len(m.mediaViewers[event.MediaID])
	}
}

func (m *Module) applyPlaybackToMediaStatsLocked(event models.AnalyticsEvent, stats *models.ViewStats) {
	pos, _ := event.Data["position"].(float64)
	dur, _ := event.Data["duration"].(float64)
	if dur > 0 {
		stats.TotalPlaybacks++
		// AvgWatchDuration is the average time a viewer actually watched, not
		// the average length of the media itself — feed the running average
		// the watched seconds (clamped to total duration to defend against a
		// forged position > duration).
		watched := pos
		if watched > dur {
			watched = dur
		}
		if watched < 0 {
			watched = 0
		}
		m.updateAvgWatchDurationLocked(event.MediaID, stats, watched)
	}
	if progress, ok := event.Data["progress"].(float64); ok && progress >= 90 {
		stats.TotalCompletions++
	}
	// Always recalculate CompletionRate after any change to TotalPlaybacks or
	// TotalCompletions. Previously this was inside the progress>=90 block, which
	// meant partial-play events incremented TotalPlaybacks without updating the
	// rate — causing the live stats to diverge (overstated) from reconstructStats,
	// which always recalculates unconditionally.
	stats.CompletionRate = completionRateFromCounts(stats.TotalCompletions, stats.TotalPlaybacks)
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

// GetDailyStats returns copies of daily statistics so callers cannot mutate
// internal state. TopMedia is filled from the most recent global view-counts
// snapshot — it isn't persisted per-day (rolling top-N is a property of the
// whole library at query time, not of any one date), but exposing it here
// keeps the dashboard's "top items" widget and the daily-stats payload
// consistent so frontend consumers don't need a second round-trip.
func (m *Module) GetDailyStats(days int) []*models.DailyStats {
	// Compute the top media list under the same lock so the snapshot we attach
	// to every returned row is internally consistent with the counters.
	m.statsMu.RLock()
	defer m.statsMu.RUnlock()

	const topMediaCount = 10
	type tm struct {
		id    string
		views int
	}
	tops := make([]tm, 0, len(m.mediaStats))
	for id, s := range m.mediaStats {
		if s == nil {
			continue
		}
		tops = append(tops, tm{id, s.TotalViews})
	}
	sort.Slice(tops, func(i, j int) bool { return tops[i].views > tops[j].views })
	if len(tops) > topMediaCount {
		tops = tops[:topMediaCount]
	}
	topIDs := make([]string, len(tops))
	for i, t := range tops {
		topIDs[i] = t.id
	}

	var stats []*models.DailyStats
	now := time.Now()

	for i := 0; i < days; i++ {
		date := now.AddDate(0, 0, -i).Format(dateFormat)
		if daily, ok := m.dailyStats[date]; ok {
			d := *daily
			// Defensive copy of the slice — avoid sharing backing storage with
			// other returned days or the underlying media-stats map iteration.
			d.TopMedia = append([]string(nil), topIDs...)
			stats = append(stats, &d)
		}
	}

	return stats
}

// UserStats holds per-user aggregate metrics computed from raw events.
//
// Computed on demand rather than maintained as a long-lived in-memory map so
// the dashboard's per-user view always reflects the database (events purged by
// retention drop out, deletion-tombstones are honored, etc.) without a
// separate refresh path. Cheap on small per-user event counts; capped at the
// repository-level analytics_events query limit.
type UserStats struct {
	UserID            string    `json:"user_id"`
	TotalEvents       int       `json:"total_events"`
	TotalViews        int       `json:"total_views"`
	TotalPlaybacks    int       `json:"total_playbacks"`
	TotalCompletions  int       `json:"total_completions"`
	TotalWatchTime    float64   `json:"total_watch_time"`
	TotalDownloads    int       `json:"total_downloads"`
	TotalSearches     int       `json:"total_searches"`
	FavoritesAdded    int       `json:"favorites_added"`
	FavoritesRemoved  int       `json:"favorites_removed"`
	RatingsSet        int       `json:"ratings_set"`
	PlaylistsCreated  int       `json:"playlists_created"`
	PlaylistsDeleted  int       `json:"playlists_deleted"`
	UploadsSucceeded  int       `json:"uploads_succeeded"`
	UploadsFailed     int       `json:"uploads_failed"`
	Logins            int       `json:"logins"`
	LoginsFailed      int       `json:"logins_failed"`
	Logouts           int       `json:"logouts"`
	UniqueMedia       int       `json:"unique_media"`
	FirstSeen         time.Time `json:"first_seen,omitempty"`
	LastSeen          time.Time `json:"last_seen,omitempty"`
	MostViewedMediaID string    `json:"most_viewed_media_id,omitempty"`
	MostViewedCount   int       `json:"most_viewed_count"`
}

// GetUserStats computes aggregate metrics for a user from the raw event stream.
// limit caps how many events are scanned (defaults to 10000); higher values
// give more accurate totals on heavy users at the cost of a larger query.
func (m *Module) GetUserStats(ctx context.Context, userID string, limit int) UserStats {
	stats := UserStats{UserID: userID}
	if userID == "" || m.eventRepo == nil {
		return stats
	}
	if limit <= 0 {
		limit = 10000
	}
	events, err := m.eventRepo.List(ctx, repositories.AnalyticsFilter{
		UserID: userID,
		Limit:  limit,
	})
	if err != nil {
		m.log.Error("GetUserStats: list events for %s: %v", userID, err)
		return stats
	}
	stats.TotalEvents = len(events)
	mediaSeen := make(map[string]struct{})
	mediaViewCounts := make(map[string]int)
	for _, ev := range events {
		if !ev.Timestamp.IsZero() {
			if stats.FirstSeen.IsZero() || ev.Timestamp.Before(stats.FirstSeen) {
				stats.FirstSeen = ev.Timestamp
			}
			if ev.Timestamp.After(stats.LastSeen) {
				stats.LastSeen = ev.Timestamp
			}
		}
		if ev.MediaID != "" {
			mediaSeen[ev.MediaID] = struct{}{}
		}
		switch ev.Type {
		case "view":
			stats.TotalViews++
			if ev.MediaID != "" {
				mediaViewCounts[ev.MediaID]++
			}
		case "playback":
			pos, _ := ev.Data["position"].(float64)
			dur, _ := ev.Data["duration"].(float64)
			if dur > 0 {
				stats.TotalPlaybacks++
				watched := pos
				if watched > dur {
					watched = dur
				}
				if watched < 0 {
					watched = 0
				}
				stats.TotalWatchTime += watched
			}
			if progress, ok := ev.Data["progress"].(float64); ok && progress >= 90 {
				stats.TotalCompletions++
			}
		case EventDownload:
			stats.TotalDownloads++
		case EventSearch:
			stats.TotalSearches++
		case EventFavoriteAdd:
			stats.FavoritesAdded++
		case EventFavoriteRemove:
			stats.FavoritesRemoved++
		case EventRatingSet:
			stats.RatingsSet++
		case EventPlaylistCreate:
			stats.PlaylistsCreated++
		case EventPlaylistDelete:
			stats.PlaylistsDeleted++
		case EventUploadSuccess:
			stats.UploadsSucceeded++
		case EventUploadFailed:
			stats.UploadsFailed++
		case EventLogin:
			stats.Logins++
		case EventLoginFailed:
			stats.LoginsFailed++
		case EventLogout:
			stats.Logouts++
		}
	}
	stats.UniqueMedia = len(mediaSeen)
	for id, n := range mediaViewCounts {
		if n > stats.MostViewedCount {
			stats.MostViewedCount = n
			stats.MostViewedMediaID = id
		}
	}
	return stats
}

// GetMediaStats returns a copy of statistics for a media item so callers cannot mutate internal state.
func (m *Module) GetMediaStats(mediaID string) *models.ViewStats {
	m.statsMu.RLock()
	defer m.statsMu.RUnlock()

	if stats, ok := m.mediaStats[mediaID]; ok {
		s := *stats
		return &s
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
		summary.TodayLogouts = daily.Logouts
		summary.TodayRegistrations = daily.Registrations
		summary.TodayAgeGatePasses = daily.AgeGatePasses
		summary.TodayDownloads = daily.Downloads
		summary.TodaySearches = daily.Searches
		summary.TodayFavoritesAdded = daily.FavoritesAdded
		summary.TodayFavoritesRemoved = daily.FavoritesRemoved
		summary.TodayRatingsSet = daily.RatingsSet
		summary.TodayPlaylistsCreated = daily.PlaylistsCreated
		summary.TodayPlaylistsDeleted = daily.PlaylistsDeleted
		summary.TodayPlaylistItemsAdded = daily.PlaylistItemsAdded
		summary.TodayUploadsSucceeded = daily.UploadsSucceeded
		summary.TodayUploadsFailed = daily.UploadsFailed
		summary.TodayPasswordChanges = daily.PasswordChanges
		summary.TodayAccountDeletions = daily.AccountDeletions
		summary.TodayHLSStarts = daily.HLSStarts
		summary.TodayHLSErrors = daily.HLSErrors
		summary.TodayMediaDeletions = daily.MediaDeletions
		summary.TodayAPITokensCreated = daily.APITokensCreated
		summary.TodayAPITokensRevoked = daily.APITokensRevoked
		summary.TodayAdminActions = daily.AdminActions
		summary.TodayServerErrors = daily.ServerErrors
		summary.TodayStreamStarts = daily.StreamStarts
		summary.TodayStreamEnds = daily.StreamEnds
		summary.TodayBytesServed = daily.BytesServed
		summary.TodayMatureBlocked = daily.MatureBlocked
		summary.TodayPermissionDenied = daily.PermissionDenied
		summary.TodayPreferencesChanges = daily.PreferencesChanges
		summary.TodayBulkDeletes = daily.BulkDeletes
		summary.TodayBulkUpdates = daily.BulkUpdates
		summary.TodayUserRoleChanges = daily.UserRoleChanges
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
//
// This runs AFTER loadDailyStats has already restored persisted aggregates, so
// it overwrites those values for any day represented in the event window. That
// is intentional: the persisted row may lag the raw events by up to one flush
// interval (~30s), and reconstruction is the canonical truth for the period it
// covers. Days outside the event window keep their persisted values.
func (m *Module) reconstructStats() {
	cfg := m.config.Get()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	retention := cfg.Analytics.RetentionDays
	if retention <= 0 {
		retention = 30
	}
	cutoff := time.Now().AddDate(0, 0, -retention).Format(time.RFC3339)
	events, err := m.eventRepo.List(ctx, repositories.AnalyticsFilter{
		StartDate: cutoff,
		Limit:     m.maxEvents,
	})
	if err != nil {
		m.log.Warn("Failed to load events for stat reconstruction: %v", err)
		return
	}

	// Reset same-day aggregates we're about to recompute so the reconstruction
	// pass matches the live counter logic exactly. Persisted rows for older
	// days are preserved.
	touchedDays := make(map[string]struct{})
	for _, ev := range events {
		touchedDays[ev.Timestamp.Format(dateFormat)] = struct{}{}
	}

	m.statsMu.Lock()
	for date := range touchedDays {
		// Drop the persisted row for this day; reconstruction will rebuild it
		// from the raw events below. This avoids double-counting.
		m.dailyStats[date] = &models.DailyStats{Date: date}
		delete(m.dailyUsers, date)
	}
	for _, ev := range events {
		m.rebuildStatsFromEvent(*ev)
	}
	m.statsMu.Unlock()

	if len(events) >= m.maxEvents && m.maxEvents > 0 {
		// We almost certainly truncated. Warn loudly so operators raise the cap
		// or shorten the retention window — silently dropping recent activity
		// is exactly the kind of invisible inaccuracy this whole system fights.
		m.log.Warn(
			"Analytics reconstruction hit the event cap (%d). Recent activity may be missing — "+
				"persisted daily_stats rows still cover earlier days. Increase analytics.max_reconstruct_events "+
				"or lower analytics.retention_days.",
			m.maxEvents,
		)
	}
	m.log.Info("Reconstructed stats from %d events across %d distinct day(s)", len(events), len(touchedDays))

	// Mark every reconstructed day dirty so the next flush re-persists the
	// canonical numbers — otherwise a restart-then-immediate-shutdown could
	// leave the table with the older, stale values.
	for date := range touchedDays {
		m.markDailyDirty(date)
	}
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
	case EventFavoriteAdd:
		daily.FavoritesAdded++
	case EventFavoriteRemove:
		daily.FavoritesRemoved++
	case EventRatingSet:
		daily.RatingsSet++
	case EventPlaylistCreate:
		daily.PlaylistsCreated++
	case EventPlaylistDelete:
		daily.PlaylistsDeleted++
	case EventPlaylistItemAdd:
		daily.PlaylistItemsAdded++
	case EventUploadSuccess:
		daily.UploadsSucceeded++
	case EventUploadFailed:
		daily.UploadsFailed++
	case EventPasswordChange:
		daily.PasswordChanges++
	case EventAccountDelete:
		daily.AccountDeletions++
	case EventHLSStart:
		daily.HLSStarts++
	case EventHLSError:
		daily.HLSErrors++
	case EventMediaDeleted:
		daily.MediaDeletions++
	case EventAPITokenCreate:
		daily.APITokensCreated++
	case EventAPITokenRevoke:
		daily.APITokensRevoked++
	case EventAdminAction:
		daily.AdminActions++
	case EventServerError:
		daily.ServerErrors++
	case EventStreamStart:
		daily.StreamStarts++
	case EventStreamEnd:
		daily.StreamEnds++
		if bs, ok := event.Data["bytes_sent"].(float64); ok && bs > 0 {
			daily.BytesServed += int64(bs)
		}
		if bs, ok := event.Data["bytes_sent"].(int64); ok && bs > 0 {
			daily.BytesServed += bs
		}
	case EventMatureBlocked:
		daily.MatureBlocked++
	case EventPermissionDenied:
		daily.PermissionDenied++
	case EventPreferencesChange:
		daily.PreferencesChanges++
	case EventBulkDelete:
		daily.BulkDeletes++
	case EventBulkUpdate:
		daily.BulkUpdates++
	case EventUserRoleChange:
		daily.UserRoleChanges++
	case "playback":
		// Reconstruct TotalWatchTime in daily stats (mirrors live path in applyPlaybackToDailyStatsLocked).
		m.applyPlaybackToDailyStatsLocked(event, daily)
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
		pos, _ := event.Data["position"].(float64)
		dur, _ := event.Data["duration"].(float64)
		if dur > 0 {
			stats.TotalPlaybacks++
			watched := pos
			if watched > dur {
				watched = dur
			}
			if watched < 0 {
				watched = 0
			}
			// Reconstruct AvgWatchDuration using the same running-average helper
			// used by the live path so the values are consistent.
			m.updateAvgWatchDurationLocked(event.MediaID, stats, watched)
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
