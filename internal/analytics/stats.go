package analytics

import (
	"context"
	"sort"
	"strconv"
	"strings"
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

// TopUserEntry pairs a user with one numeric metric for leaderboard rendering.
type TopUserEntry struct {
	UserID         string  `json:"user_id"`
	Username       string  `json:"username,omitempty"` // resolved by handler when possible
	Metric         float64 `json:"metric"`
	TotalViews     int     `json:"total_views"`
	TotalWatchTime float64 `json:"total_watch_time"`
	TotalUploads   int     `json:"total_uploads"`
	TotalDownloads int     `json:"total_downloads"`
	TotalEvents    int     `json:"total_events"`
}

// GetTopUsers returns the top N users by the given metric. metric is one of:
// "views", "watch_time", "uploads", "downloads", "events". `since` and
// `until` are RFC3339 timestamps; empty disables that side. Computed by
// scanning events in [since, until] — without time bounds the query falls
// back to the retention window. SQL GROUP BY isn't a good fit here because
// the metric switch (views vs watch-time vs uploads) needs per-event payload
// inspection, so the aggregation stays in Go.
func (m *Module) GetTopUsers(ctx context.Context, metric, since, until string, limit int) []TopUserEntry {
	if m.eventRepo == nil {
		return nil
	}
	if limit <= 0 {
		limit = 10
	}
	// Pull a generous window — repo caps at 10k internally so this is a
	// soft request, not an unbounded scan. retention pruning is the real
	// upper bound on row count.
	events, err := m.eventRepo.List(ctx, repositories.AnalyticsFilter{
		StartDate: since,
		EndDate:   until,
		Limit:     50000,
	})
	if err != nil {
		m.log.Error("GetTopUsers: list events: %v", err)
		return nil
	}

	type bucket struct {
		userID                                                  string
		views, playbacks, completions, uploads, downloads, evts int
		watchTime                                               float64
	}
	rows := make(map[string]*bucket)
	for _, ev := range events {
		if ev.UserID == "" {
			continue
		}
		b, ok := rows[ev.UserID]
		if !ok {
			b = &bucket{userID: ev.UserID}
			rows[ev.UserID] = b
		}
		b.evts++
		switch ev.Type {
		case "view":
			b.views++
		case "playback":
			pos, _ := ev.Data["position"].(float64)
			dur, _ := ev.Data["duration"].(float64)
			if dur > 0 {
				b.playbacks++
				w := pos
				if w > dur {
					w = dur
				}
				if w > 0 {
					b.watchTime += w
				}
			}
		case EventDownload:
			b.downloads++
		case EventUploadSuccess:
			b.uploads++
		}
	}
	out := make([]TopUserEntry, 0, len(rows))
	for _, b := range rows {
		out = append(out, TopUserEntry{
			UserID:         b.userID,
			TotalViews:     b.views,
			TotalWatchTime: b.watchTime,
			TotalUploads:   b.uploads,
			TotalDownloads: b.downloads,
			TotalEvents:    b.evts,
		})
	}
	for i := range out {
		switch metric {
		case "watch_time":
			out[i].Metric = out[i].TotalWatchTime
		case "uploads":
			out[i].Metric = float64(out[i].TotalUploads)
		case "downloads":
			out[i].Metric = float64(out[i].TotalDownloads)
		case "events":
			out[i].Metric = float64(out[i].TotalEvents)
		default: // "views"
			out[i].Metric = float64(out[i].TotalViews)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Metric > out[j].Metric })
	if limit > 0 && limit < len(out) {
		out = out[:limit]
	}
	return out
}

// SearchQueryEntry pairs a normalised search query with its frequency and
// whether the search ever returned zero results. "Empty" queries surface the
// search-experience signal that the audit added to every search event.
type SearchQueryEntry struct {
	Query      string `json:"query"`
	Count      int    `json:"count"`
	EmptyCount int    `json:"empty_count"` // how many of those occurrences returned 0 results
}

// GetTopSearches returns the most frequent search queries seen in
// [since, until], with the empty-result share alongside. limit caps the rows
// returned (default 20). Queries are case-insensitive and trimmed; the
// original casing of the most-recent occurrence is preserved for display.
// Empty since/until disables that side of the filter.
func (m *Module) GetTopSearches(ctx context.Context, since, until string, limit int) []SearchQueryEntry {
	if m.eventRepo == nil {
		return nil
	}
	if limit <= 0 {
		limit = 20
	}
	events, err := m.eventRepo.List(ctx, repositories.AnalyticsFilter{
		Type:      EventSearch,
		StartDate: since,
		EndDate:   until,
		Limit:     10000,
	})
	if err != nil {
		m.log.Error("GetTopSearches: list events: %v", err)
		return nil
	}
	type bucket struct {
		display    string
		count      int
		emptyCount int
	}
	rows := make(map[string]*bucket)
	for _, ev := range events {
		raw, _ := ev.Data["query"].(string)
		q := strings.TrimSpace(raw)
		if q == "" {
			continue
		}
		key := strings.ToLower(q)
		b, ok := rows[key]
		if !ok {
			b = &bucket{display: q}
			rows[key] = b
		}
		b.count++
		if empty, ok := ev.Data["empty"].(bool); ok && empty {
			b.emptyCount++
		}
	}
	out := make([]SearchQueryEntry, 0, len(rows))
	for _, b := range rows {
		out = append(out, SearchQueryEntry{Query: b.display, Count: b.count, EmptyCount: b.emptyCount})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].Query < out[j].Query
	})
	if limit > 0 && limit < len(out) {
		out = out[:limit]
	}
	return out
}

// FailedLoginEntry summarises a recent failed login attempt for the security
// review panel — IP, attempted username (when present in the event payload),
// and timestamp. Recent N entries; deduplication is the caller's call.
type FailedLoginEntry struct {
	IPAddress string    `json:"ip_address"`
	Username  string    `json:"username,omitempty"`
	UserAgent string    `json:"user_agent,omitempty"`
	Timestamp time.Time `json:"timestamp"`
	Reason    string    `json:"reason,omitempty"`
}

// GetRecentFailedLogins returns up to limit recent login_failed events
// in [since, until]. Sorted newest first — same order the repo returns from
// List.
func (m *Module) GetRecentFailedLogins(ctx context.Context, since, until string, limit int) []FailedLoginEntry {
	if m.eventRepo == nil {
		return nil
	}
	if limit <= 0 {
		limit = 50
	}
	events, err := m.eventRepo.List(ctx, repositories.AnalyticsFilter{
		Type:      EventLoginFailed,
		StartDate: since,
		EndDate:   until,
		Limit:     limit,
	})
	if err != nil {
		m.log.Error("GetRecentFailedLogins: list events: %v", err)
		return nil
	}
	out := make([]FailedLoginEntry, 0, len(events))
	for _, ev := range events {
		username, _ := ev.Data["username"].(string)
		reason, _ := ev.Data["reason"].(string)
		out = append(out, FailedLoginEntry{
			IPAddress: ev.IPAddress,
			Username:  username,
			UserAgent: ev.UserAgent,
			Timestamp: ev.Timestamp,
			Reason:    reason,
		})
	}
	return out
}

// ErrorPathEntry groups server_error events by HTTP path so operators can see
// which routes are failing without scanning the raw event stream.
type ErrorPathEntry struct {
	Path     string    `json:"path"`
	Method   string    `json:"method"`
	Status   int       `json:"status"`
	Count    int       `json:"count"`
	LastSeen time.Time `json:"last_seen"`
}

// GetErrorPaths aggregates server_error events into a (method, path, status)
// table sorted by count descending. Events are scanned in [since, until]
// (default = retention window). Returned rows capped by limit (default 25).
func (m *Module) GetErrorPaths(ctx context.Context, since, until string, limit int) []ErrorPathEntry {
	if m.eventRepo == nil {
		return nil
	}
	if limit <= 0 {
		limit = 25
	}
	events, err := m.eventRepo.List(ctx, repositories.AnalyticsFilter{
		Type:      EventServerError,
		StartDate: since,
		EndDate:   until,
		Limit:     1000,
	})
	if err != nil {
		m.log.Error("GetErrorPaths: list events: %v", err)
		return nil
	}
	type bucket struct {
		path, method string
		status       int
		count        int
		last         time.Time
	}
	rows := make(map[string]*bucket)
	for _, ev := range events {
		path, _ := ev.Data["path"].(string)
		method, _ := ev.Data["method"].(string)
		status := 500
		switch s := ev.Data["status"].(type) {
		case int:
			status = s
		case float64:
			status = int(s)
		}
		key := method + " " + path + " " + strconv.Itoa(status)
		b, ok := rows[key]
		if !ok {
			b = &bucket{path: path, method: method, status: status}
			rows[key] = b
		}
		b.count++
		if ev.Timestamp.After(b.last) {
			b.last = ev.Timestamp
		}
	}
	out := make([]ErrorPathEntry, 0, len(rows))
	for _, b := range rows {
		out = append(out, ErrorPathEntry{
			Path:     b.path,
			Method:   b.method,
			Status:   b.status,
			Count:    b.count,
			LastSeen: b.last,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].LastSeen.After(out[j].LastSeen)
	})
	if limit > 0 && limit < len(out) {
		out = out[:limit]
	}
	return out
}

// MetricTimelineEntry is one bucket on a daily-stats time-series chart.
type MetricTimelineEntry struct {
	Date  string  `json:"date"`
	Value float64 `json:"value"`
}

// CohortMetrics holds rolling unique-user counts. DAU = unique active users
// in the last 24h, WAU = last 7 days, MAU = last 30 days. Computed from raw
// events so any user-attributed event counts as activity.
type CohortMetrics struct {
	DAU       int     `json:"dau"`
	WAU       int     `json:"wau"`
	MAU       int     `json:"mau"`
	StickinessDAUWAU float64 `json:"stickiness_dau_wau"` // DAU/WAU, 0..1
	StickinessDAUMAU float64 `json:"stickiness_dau_mau"` // DAU/MAU, 0..1
}

// GetCohortMetrics computes DAU, WAU, MAU and stickiness ratios from the raw
// event stream. Events without a user_id (anonymous traffic) are excluded —
// those numbers belong on a separate "unique IPs" metric.
func (m *Module) GetCohortMetrics(ctx context.Context) CohortMetrics {
	out := CohortMetrics{}
	if m.eventRepo == nil {
		return out
	}
	now := time.Now()
	cutoff := now.AddDate(0, 0, -30).Format(time.RFC3339)
	events, err := m.eventRepo.List(ctx, repositories.AnalyticsFilter{
		StartDate: cutoff,
		Limit:     50000,
	})
	if err != nil {
		m.log.Error("GetCohortMetrics: list events: %v", err)
		return out
	}
	dayCutoff := now.AddDate(0, 0, -1)
	weekCutoff := now.AddDate(0, 0, -7)
	dau, wau, mau := make(map[string]struct{}), make(map[string]struct{}), make(map[string]struct{})
	for _, ev := range events {
		if ev.UserID == "" {
			continue
		}
		mau[ev.UserID] = struct{}{}
		if ev.Timestamp.After(weekCutoff) {
			wau[ev.UserID] = struct{}{}
		}
		if ev.Timestamp.After(dayCutoff) {
			dau[ev.UserID] = struct{}{}
		}
	}
	out.DAU = len(dau)
	out.WAU = len(wau)
	out.MAU = len(mau)
	if out.WAU > 0 {
		out.StickinessDAUWAU = float64(out.DAU) / float64(out.WAU)
	}
	if out.MAU > 0 {
		out.StickinessDAUMAU = float64(out.DAU) / float64(out.MAU)
	}
	return out
}

// HourlyHeatmapCell is one (day-of-week, hour) bucket. DayOfWeek follows
// time.Weekday: 0=Sunday … 6=Saturday. Hour is 0..23 in the server's local
// timezone (matches GetEventStats).
type HourlyHeatmapCell struct {
	DayOfWeek int `json:"day_of_week"`
	Hour      int `json:"hour"`
	Count     int `json:"count"`
}

// GetHourlyHeatmap returns a 7×24 grid of event counts, scanned over the
// last `days` days (default 30). The frontend renders this as a calendar
// heatmap so admins can see traffic peak hours per weekday at a glance.
func (m *Module) GetHourlyHeatmap(ctx context.Context, days int) []HourlyHeatmapCell {
	if m.eventRepo == nil {
		return nil
	}
	if days <= 0 {
		days = 30
	}
	cutoff := time.Now().AddDate(0, 0, -days).Format(time.RFC3339)
	events, err := m.eventRepo.List(ctx, repositories.AnalyticsFilter{
		StartDate: cutoff,
		Limit:     100000,
	})
	if err != nil {
		m.log.Error("GetHourlyHeatmap: list events: %v", err)
		return nil
	}
	// 7 days × 24 hours = 168 cells, always emit the full grid (zero-filled)
	// so the frontend can render a clean rectangle without per-cell guards.
	grid := make([]HourlyHeatmapCell, 7*24)
	for d := 0; d < 7; d++ {
		for h := 0; h < 24; h++ {
			grid[d*24+h] = HourlyHeatmapCell{DayOfWeek: d, Hour: h}
		}
	}
	loc := time.Now().Location()
	for _, ev := range events {
		t := ev.Timestamp.In(loc)
		dow := int(t.Weekday())
		hour := t.Hour()
		if dow >= 0 && dow < 7 && hour >= 0 && hour < 24 {
			grid[dow*24+hour].Count++
		}
	}
	return grid
}

// QualityBucket captures stream count and bytes-served per quality tier.
// Sourced from stream_start / stream_end events whose Data carries quality.
type QualityBucket struct {
	Quality    string `json:"quality"`
	Streams    int    `json:"streams"`
	BytesSent  int64  `json:"bytes_sent"`
}

// GetQualityBreakdown groups stream activity by reported quality tier so
// admins can see how much of their bandwidth is going to which resolution.
// Empty / unknown quality buckets under "(unspecified)" rather than being
// dropped — bytes still need to be accounted for somewhere.
func (m *Module) GetQualityBreakdown(ctx context.Context, days int) []QualityBucket {
	if m.eventRepo == nil {
		return nil
	}
	if days <= 0 {
		days = 30
	}
	cutoff := time.Now().AddDate(0, 0, -days).Format(time.RFC3339)
	starts, err := m.eventRepo.List(ctx, repositories.AnalyticsFilter{
		Type:      EventStreamStart,
		StartDate: cutoff,
		Limit:     50000,
	})
	if err != nil {
		m.log.Error("GetQualityBreakdown: list start events: %v", err)
		return nil
	}
	ends, err := m.eventRepo.List(ctx, repositories.AnalyticsFilter{
		Type:      EventStreamEnd,
		StartDate: cutoff,
		Limit:     50000,
	})
	if err != nil {
		m.log.Error("GetQualityBreakdown: list end events: %v", err)
	}
	buckets := make(map[string]*QualityBucket)
	get := func(q string) *QualityBucket {
		if q == "" {
			q = "(unspecified)"
		}
		b, ok := buckets[q]
		if !ok {
			b = &QualityBucket{Quality: q}
			buckets[q] = b
		}
		return b
	}
	for _, ev := range starts {
		q, _ := ev.Data["quality"].(string)
		get(q).Streams++
	}
	for _, ev := range ends {
		q, _ := ev.Data["quality"].(string)
		bs, _ := ev.Data["bytes_sent"].(float64)
		if bs <= 0 {
			if v, ok := ev.Data["bytes_sent"].(int64); ok {
				bs = float64(v)
			}
		}
		if bs > 0 {
			get(q).BytesSent += int64(bs)
		}
	}
	out := make([]QualityBucket, 0, len(buckets))
	for _, b := range buckets {
		out = append(out, *b)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Streams != out[j].Streams {
			return out[i].Streams > out[j].Streams
		}
		return out[i].BytesSent > out[j].BytesSent
	})
	return out
}

// PeriodComparison captures an absolute current value, an absolute previous
// value (same window length, ending where the current window begins), and
// the percent change. Metric is one of the JSON tags on DailyStats.
type PeriodComparison struct {
	Metric        string  `json:"metric"`
	Current       float64 `json:"current"`
	Previous      float64 `json:"previous"`
	DeltaAbsolute float64 `json:"delta_absolute"`
	DeltaPct      float64 `json:"delta_pct"` // (current - previous) / max(1, previous)
	WindowDays    int     `json:"window_days"`
}

// GetPeriodComparison sums the named DailyStats metric over the last
// `windowDays` days and the prior `windowDays` days, returning both totals
// plus the delta. Use to render "vs last week" indicators on summary cards.
func (m *Module) GetPeriodComparison(metric string, windowDays int) PeriodComparison {
	if windowDays <= 0 {
		windowDays = 7
	}
	out := PeriodComparison{Metric: metric, WindowDays: windowDays}
	m.statsMu.RLock()
	defer m.statsMu.RUnlock()
	now := time.Now()
	for i := 0; i < windowDays; i++ {
		date := now.AddDate(0, 0, -i).Format(dateFormat)
		if d, ok := m.dailyStats[date]; ok && d != nil {
			out.Current += dailyStatField(d, metric)
		}
	}
	for i := windowDays; i < 2*windowDays; i++ {
		date := now.AddDate(0, 0, -i).Format(dateFormat)
		if d, ok := m.dailyStats[date]; ok && d != nil {
			out.Previous += dailyStatField(d, metric)
		}
	}
	out.DeltaAbsolute = out.Current - out.Previous
	if out.Previous > 0 {
		out.DeltaPct = (out.Current - out.Previous) / out.Previous * 100
	} else if out.Current > 0 {
		// Previous = 0, current > 0 — percent is meaningless ("∞%").
		// Emit a sentinel large value the frontend can detect.
		out.DeltaPct = 100
	}
	return out
}

// GetContentGaps surfaces the popular-but-unanswered searches separately so
// the dashboard can render them as a "things users want we don't have" panel.
// Returns search queries whose empty_count >= minEmpty AND empty share >=
// minEmptyShare (0..1). limit caps the rows.
func (m *Module) GetContentGaps(ctx context.Context, since, until string, minEmpty int, minEmptyShare float64, limit int) []SearchQueryEntry {
	if minEmpty < 1 {
		minEmpty = 2
	}
	if minEmptyShare < 0 {
		minEmptyShare = 0.5
	}
	if limit <= 0 {
		limit = 15
	}
	all := m.GetTopSearches(ctx, since, until, 200)
	out := make([]SearchQueryEntry, 0, len(all))
	for _, q := range all {
		if q.EmptyCount < minEmpty {
			continue
		}
		share := float64(q.EmptyCount) / float64(q.Count)
		if share < minEmptyShare {
			continue
		}
		out = append(out, q)
	}
	if limit > 0 && limit < len(out) {
		out = out[:limit]
	}
	return out
}

// FunnelStage is one step on a conversion funnel — name, count, and the
// percentage of the previous stage that reached this stage. The first stage's
// FromPreviousPct is 100.
type FunnelStage struct {
	Stage           string  `json:"stage"`
	Count           int64   `json:"count"`
	FromPreviousPct float64 `json:"from_previous_pct"`
	FromTopPct      float64 `json:"from_top_pct"`
}

// Funnel holds the canonical view → playback → completion conversion funnel
// plus aggregate breakdown by authenticated vs anonymous traffic. Anonymous
// traffic typically has different conversion characteristics so it gets its
// own row in the dashboard.
type Funnel struct {
	WindowDays    int           `json:"window_days"`
	Stages        []FunnelStage `json:"stages"`        // overall
	Authenticated []FunnelStage `json:"authenticated"` // user_id != ""
	Anonymous     []FunnelStage `json:"anonymous"`     // user_id == ""
}

// GetFunnel scans events in the last `days` days and computes view →
// playback → completion conversion rates, both overall and split by
// authenticated/anonymous traffic. days <= 0 falls back to 30.
func (m *Module) GetFunnel(ctx context.Context, days int) Funnel {
	if days <= 0 {
		days = 30
	}
	out := Funnel{WindowDays: days}
	if m.eventRepo == nil {
		return out
	}
	cutoff := time.Now().AddDate(0, 0, -days).Format(time.RFC3339)
	events, err := m.eventRepo.List(ctx, repositories.AnalyticsFilter{
		StartDate: cutoff,
		Limit:     100000,
	})
	if err != nil {
		m.log.Error("GetFunnel: list events: %v", err)
		return out
	}
	var (
		views, playbacks, completions             int64
		viewsAuth, playbacksAuth, completionsAuth int64
		viewsAnon, playbacksAnon, completionsAnon int64
	)
	for _, ev := range events {
		auth := ev.UserID != ""
		switch ev.Type {
		case "view":
			views++
			if auth {
				viewsAuth++
			} else {
				viewsAnon++
			}
		case "playback":
			// Only count playback events that actually represent a real
			// playback attempt (have a duration). Without this guard, the
			// frontend can flood playback events that never carried a
			// duration and inflate the middle of the funnel.
			if dur, ok := ev.Data["duration"].(float64); ok && dur > 0 {
				playbacks++
				if auth {
					playbacksAuth++
				} else {
					playbacksAnon++
				}
				if progress, ok := ev.Data["progress"].(float64); ok && progress >= 90 {
					completions++
					if auth {
						completionsAuth++
					} else {
						completionsAnon++
					}
				}
			}
		}
	}
	out.Stages = buildFunnelStages(views, playbacks, completions)
	out.Authenticated = buildFunnelStages(viewsAuth, playbacksAuth, completionsAuth)
	out.Anonymous = buildFunnelStages(viewsAnon, playbacksAnon, completionsAnon)
	return out
}

func buildFunnelStages(views, playbacks, completions int64) []FunnelStage {
	stages := []FunnelStage{
		{Stage: "Views", Count: views},
		{Stage: "Playbacks", Count: playbacks},
		{Stage: "Completions", Count: completions},
	}
	// Top is always 100% by definition.
	if views > 0 {
		stages[0].FromPreviousPct = 100
		stages[0].FromTopPct = 100
		// Playbacks vs views.
		stages[1].FromPreviousPct = float64(playbacks) / float64(views) * 100
		stages[1].FromTopPct = stages[1].FromPreviousPct
		// Completions vs playbacks (NOT views — that would dilute the
		// completion-rate signal with people who never started playing).
		if playbacks > 0 {
			stages[2].FromPreviousPct = float64(completions) / float64(playbacks) * 100
		}
		stages[2].FromTopPct = float64(completions) / float64(views) * 100
	}
	return stages
}

// DeviceBucket is one row of the device-family breakdown — bucket label,
// event count, and unique-user count.
type DeviceBucket struct {
	Family      string `json:"family"`
	Events      int    `json:"events"`
	UniqueUsers int    `json:"unique_users"`
}

// GetDeviceBreakdown classifies events by user-agent family (mobile / tablet
// / desktop / bot / unknown) and by browser/OS family. Returns two slices
// the dashboard renders side by side. Pure parsing — no external geoip /
// device-detection service.
//
// The classification is intentionally coarse: full UA-parsing libraries
// have heavy dependency cost and frequent false positives. The categories
// here cover the realistic decision-making the admin needs ("are mobile
// users completing playbacks?") without pretending to identify exact
// browser versions.
func (m *Module) GetDeviceBreakdown(ctx context.Context, days int) (devices, browsers []DeviceBucket) {
	if days <= 0 {
		days = 30
	}
	if m.eventRepo == nil {
		return nil, nil
	}
	cutoff := time.Now().AddDate(0, 0, -days).Format(time.RFC3339)
	events, err := m.eventRepo.List(ctx, repositories.AnalyticsFilter{
		StartDate: cutoff,
		Limit:     100000,
	})
	if err != nil {
		m.log.Error("GetDeviceBreakdown: list events: %v", err)
		return nil, nil
	}
	type bucket struct {
		events int
		users  map[string]struct{}
	}
	devMap := make(map[string]*bucket)
	brwMap := make(map[string]*bucket)
	add := func(m map[string]*bucket, key, userID string) {
		b, ok := m[key]
		if !ok {
			b = &bucket{users: make(map[string]struct{})}
			m[key] = b
		}
		b.events++
		if userID != "" {
			b.users[userID] = struct{}{}
		}
	}
	for _, ev := range events {
		if ev.UserAgent == "" {
			add(devMap, "(unknown)", ev.UserID)
			add(brwMap, "(unknown)", ev.UserID)
			continue
		}
		add(devMap, classifyDeviceFamily(ev.UserAgent), ev.UserID)
		add(brwMap, classifyBrowserFamily(ev.UserAgent), ev.UserID)
	}
	flatten := func(m map[string]*bucket) []DeviceBucket {
		out := make([]DeviceBucket, 0, len(m))
		for k, v := range m {
			out = append(out, DeviceBucket{Family: k, Events: v.events, UniqueUsers: len(v.users)})
		}
		sort.Slice(out, func(i, j int) bool {
			if out[i].Events != out[j].Events {
				return out[i].Events > out[j].Events
			}
			return out[i].Family < out[j].Family
		})
		return out
	}
	return flatten(devMap), flatten(brwMap)
}

// classifyDeviceFamily groups a UA string into device categories. Order
// matters — bot detection comes first because some bots impersonate mobile
// browsers and we don't want them in the "Mobile" bucket.
func classifyDeviceFamily(ua string) string {
	low := strings.ToLower(ua)
	switch {
	case strings.Contains(low, "bot"),
		strings.Contains(low, "spider"),
		strings.Contains(low, "crawler"),
		strings.Contains(low, "curl/"),
		strings.Contains(low, "wget/"),
		strings.Contains(low, "python-requests"),
		strings.Contains(low, "go-http-client"):
		return "Bot / Tool"
	case strings.Contains(low, "ipad"),
		strings.Contains(low, "tablet"):
		return "Tablet"
	case strings.Contains(low, "iphone"),
		strings.Contains(low, "android"),
		strings.Contains(low, "mobile"):
		return "Mobile"
	case strings.Contains(low, "smarttv"),
		strings.Contains(low, "tizen"),
		strings.Contains(low, "webos"),
		strings.Contains(low, "googletv"),
		strings.Contains(low, "appletv"):
		return "TV"
	case strings.Contains(low, "windows"),
		strings.Contains(low, "macintosh"),
		strings.Contains(low, "linux"),
		strings.Contains(low, "x11"):
		return "Desktop"
	default:
		return "Other"
	}
}

// classifyBrowserFamily extracts a coarse browser family. Order matters
// because Edge UAs contain "Chrome" too; check Edge first.
func classifyBrowserFamily(ua string) string {
	low := strings.ToLower(ua)
	switch {
	case strings.Contains(low, "edg/"):
		return "Edge"
	case strings.Contains(low, "firefox/"):
		return "Firefox"
	case strings.Contains(low, "opr/"), strings.Contains(low, "opera/"):
		return "Opera"
	case strings.Contains(low, "vivaldi/"):
		return "Vivaldi"
	case strings.Contains(low, "chrome/"):
		return "Chrome"
	case strings.Contains(low, "safari/"):
		return "Safari"
	default:
		return "Other"
	}
}

// MediaDetail aggregates everything an admin would want to see for a single
// media item — the cached ViewStats plus a 30-day timeline of views and
// playbacks. Returned by GET /admin/analytics/media/:id.
type MediaDetail struct {
	MediaID         string                `json:"media_id"`
	Stats           models.ViewStats      `json:"stats"`
	ViewTimeline    []MetricTimelineEntry `json:"view_timeline"`
	PlaybackTimeline []MetricTimelineEntry `json:"playback_timeline"`
}

// GetMediaDetail returns the per-media analytics drill panel. Builds
// per-day timelines from raw events filtered to this MediaID, falling
// back to zeros when the day has no activity.
func (m *Module) GetMediaDetail(ctx context.Context, mediaID string, days int) MediaDetail {
	out := MediaDetail{MediaID: mediaID}
	if mediaID == "" || m.eventRepo == nil {
		return out
	}
	if days <= 0 {
		days = 30
	}
	out.Stats = *m.GetMediaStats(mediaID)
	cutoff := time.Now().AddDate(0, 0, -days).Format(time.RFC3339)
	events, err := m.eventRepo.List(ctx, repositories.AnalyticsFilter{
		MediaID:   mediaID,
		StartDate: cutoff,
		Limit:     20000,
	})
	if err != nil {
		m.log.Error("GetMediaDetail: list events for %s: %v", mediaID, err)
		return out
	}
	viewByDay := make(map[string]int)
	playbackByDay := make(map[string]int)
	for _, ev := range events {
		date := ev.Timestamp.Format(dateFormat)
		switch ev.Type {
		case "view":
			viewByDay[date]++
		case "playback":
			if dur, ok := ev.Data["duration"].(float64); ok && dur > 0 {
				playbackByDay[date]++
			}
		}
	}
	out.ViewTimeline = make([]MetricTimelineEntry, 0, days)
	out.PlaybackTimeline = make([]MetricTimelineEntry, 0, days)
	now := time.Now()
	for i := days - 1; i >= 0; i-- {
		date := now.AddDate(0, 0, -i).Format(dateFormat)
		out.ViewTimeline = append(out.ViewTimeline, MetricTimelineEntry{Date: date, Value: float64(viewByDay[date])})
		out.PlaybackTimeline = append(out.PlaybackTimeline, MetricTimelineEntry{Date: date, Value: float64(playbackByDay[date])})
	}
	return out
}

// GetMetricTimeline returns a per-day series of the named metric over the
// last `days` days, gap-filled with zeros so charts render evenly. The metric
// names are the same as the JSON tags on DailyStats (e.g. "total_views",
// "bytes_served", "logins"). Unknown metrics return all zeros.
func (m *Module) GetMetricTimeline(metric string, days int) []MetricTimelineEntry {
	if days <= 0 {
		days = 30
	}
	m.statsMu.RLock()
	defer m.statsMu.RUnlock()
	now := time.Now()
	out := make([]MetricTimelineEntry, 0, days)
	for i := days - 1; i >= 0; i-- {
		date := now.AddDate(0, 0, -i).Format(dateFormat)
		entry := MetricTimelineEntry{Date: date, Value: 0}
		if d, ok := m.dailyStats[date]; ok && d != nil {
			entry.Value = dailyStatField(d, metric)
		}
		out = append(out, entry)
	}
	return out
}

// dailyStatField returns the numeric value of the named metric on a
// DailyStats row. Mirrors the JSON tags used in API responses.
func dailyStatField(d *models.DailyStats, metric string) float64 {
	switch metric {
	case "total_views":
		return float64(d.TotalViews)
	case "unique_users":
		return float64(d.UniqueUsers)
	case "total_watch_time":
		return d.TotalWatchTime
	case "new_users":
		return float64(d.NewUsers)
	case "logins":
		return float64(d.Logins)
	case "logins_failed":
		return float64(d.LoginsFailed)
	case "logouts":
		return float64(d.Logouts)
	case "registrations":
		return float64(d.Registrations)
	case "age_gate_passes":
		return float64(d.AgeGatePasses)
	case "downloads":
		return float64(d.Downloads)
	case "searches":
		return float64(d.Searches)
	case "favorites_added":
		return float64(d.FavoritesAdded)
	case "favorites_removed":
		return float64(d.FavoritesRemoved)
	case "ratings_set":
		return float64(d.RatingsSet)
	case "playlists_created":
		return float64(d.PlaylistsCreated)
	case "playlists_deleted":
		return float64(d.PlaylistsDeleted)
	case "playlist_items_added":
		return float64(d.PlaylistItemsAdded)
	case "uploads_succeeded":
		return float64(d.UploadsSucceeded)
	case "uploads_failed":
		return float64(d.UploadsFailed)
	case "password_changes":
		return float64(d.PasswordChanges)
	case "account_deletions":
		return float64(d.AccountDeletions)
	case "hls_starts":
		return float64(d.HLSStarts)
	case "hls_errors":
		return float64(d.HLSErrors)
	case "media_deletions":
		return float64(d.MediaDeletions)
	case "api_tokens_created":
		return float64(d.APITokensCreated)
	case "api_tokens_revoked":
		return float64(d.APITokensRevoked)
	case "admin_actions":
		return float64(d.AdminActions)
	case "server_errors":
		return float64(d.ServerErrors)
	case "stream_starts":
		return float64(d.StreamStarts)
	case "stream_ends":
		return float64(d.StreamEnds)
	case "bytes_served":
		return float64(d.BytesServed)
	case "mature_blocked":
		return float64(d.MatureBlocked)
	case "permission_denied":
		return float64(d.PermissionDenied)
	case "preferences_changes":
		return float64(d.PreferencesChanges)
	case "bulk_deletes":
		return float64(d.BulkDeletes)
	case "bulk_updates":
		return float64(d.BulkUpdates)
	case "user_role_changes":
		return float64(d.UserRoleChanges)
	}
	return 0
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
