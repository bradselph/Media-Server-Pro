package analytics

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"

	"media-server-pro/internal/repositories"
	"media-server-pro/pkg/models"
)

// ViewParams holds parameters for tracking a view event.
type ViewParams struct {
	MediaID   string
	UserID    string
	SessionID string
	IPAddress string
	UserAgent string
}

// PlaybackParams holds parameters for tracking a playback event.
type PlaybackParams struct {
	MediaID   string
	UserID    string
	SessionID string
	Position  float64
	Duration  float64
}

// Event Types for granular tracking.
const (
	EventPlay          = "play"
	EventPause         = "pause"
	EventResume        = "resume"
	EventSeek          = "seek"
	EventComplete      = "complete"
	EventError         = "error"
	EventQualityChange = "quality_change"
	EventBuffering     = "buffering"
	EventVolumeChange  = "volume_change"
	EventFullscreen    = "fullscreen"

	// EventLogin Traffic / auth events (server-generated, not client-submitted)
	EventLogin       = "login"
	EventLoginFailed = "login_failed"
	EventLogout      = "logout"
	EventRegister    = "register"
	EventAgeGatePass = "age_gate_pass"
	EventDownload    = "download"
	EventSearch      = "search"

	// Server-side action events. These are emitted by the handlers / modules
	// that perform the action, NOT submitted by clients — accepting them from
	// the browser would let any caller forge dashboard counts. Each one maps
	// to a column on daily_stats in updateDailyStatsLocked.
	EventFavoriteAdd      = "favorite_add"
	EventFavoriteRemove   = "favorite_remove"
	EventRatingSet        = "rating_set"
	EventPlaylistCreate   = "playlist_create"
	EventPlaylistDelete   = "playlist_delete"
	EventPlaylistItemAdd  = "playlist_item_add"
	EventUploadSuccess    = "upload_success"
	EventUploadFailed     = "upload_failed"
	EventPasswordChange   = "password_change"
	EventAccountDelete    = "account_delete"
	EventHLSStart         = "hls_start"
	EventHLSError         = "hls_error"
	EventMediaDeleted     = "media_deleted" // tombstone — see DeleteEventsByMedia
	EventAPITokenCreate   = "api_token_create"
	EventAPITokenRevoke   = "api_token_revoke"
	EventAdminAction      = "admin_action"
	EventServerError      = "server_error"

	// Engagement / access-control / admin-bulk events. Each one corresponds to
	// a column on daily_stats and a today_<x> field on the summary.
	EventStreamStart        = "stream_start"
	EventStreamEnd          = "stream_end"
	EventMatureBlocked      = "mature_blocked"
	EventPermissionDenied   = "permission_denied"
	EventPreferencesChange  = "preferences_change"
	EventBulkDelete         = "bulk_delete"
	EventBulkUpdate         = "bulk_update"
	EventUserRoleChange     = "user_role_change"
)

// ClientEventInput holds parameters for SubmitClientEvent.
type ClientEventInput struct {
	Type      string
	MediaID   string
	UserID    string
	SessionID string
	IPAddress string
	UserAgent string
	Data      map[string]any
}

// EventStats holds event statistics.
type EventStats struct {
	TotalEvents  int64            `json:"total_events"`
	EventCounts  map[string]int64 `json:"event_counts"`
	HourlyEvents []int            `json:"hourly_events"`
}

func generateEventID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}

func eventsToSlice(events []*models.AnalyticsEvent) []models.AnalyticsEvent {
	result := make([]models.AnalyticsEvent, len(events))
	for i, event := range events {
		result[i] = *event
	}
	return result
}

// TrackEvent records an analytics event.
func (m *Module) TrackEvent(ctx context.Context, event models.AnalyticsEvent) {
	if !m.config.Get().Analytics.Enabled {
		return
	}
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now()
	}
	if event.ID == "" {
		event.ID = generateEventID()
	}

	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := m.eventRepo.Create(ctx, &event); err != nil {
		m.log.Error("Failed to create analytics event: %v", err)
	}

	if event.SessionID != "" {
		m.updateSession(event)
	}
	m.updateStats(event)
	m.log.Debug("Tracked event: %s for %s", event.Type, event.MediaID)
}

// TrackView records a view event.
func (m *Module) TrackView(ctx context.Context, params ViewParams) {
	cfg := m.config.Get()
	if !cfg.Analytics.TrackViews {
		return
	}
	m.TrackEvent(ctx, models.AnalyticsEvent{
		Type:      "view",
		MediaID:   params.MediaID,
		UserID:    params.UserID,
		SessionID: params.SessionID,
		IPAddress: params.IPAddress,
		UserAgent: params.UserAgent,
		Data:      map[string]any{"timestamp": time.Now()},
	})
}

// TrackPlayback records a playback event.
func (m *Module) TrackPlayback(ctx context.Context, params PlaybackParams) {
	cfg := m.config.Get()
	if !cfg.Analytics.TrackPlayback {
		return
	}
	progress := 0.0
	if params.Duration > 0 {
		progress = params.Position / params.Duration * 100
	}
	m.TrackEvent(ctx, models.AnalyticsEvent{
		Type:      "playback",
		MediaID:   params.MediaID,
		UserID:    params.UserID,
		SessionID: params.SessionID,
		Data: map[string]any{
			"position": params.Position,
			"duration": params.Duration,
			"progress": progress,
		},
	})
}

// TrafficEventParams holds parameters for server-generated traffic events.
type TrafficEventParams struct {
	Type      string
	UserID    string
	SessionID string
	IPAddress string
	UserAgent string
	Data      map[string]any
}

// TrackTrafficEvent records a server-generated traffic event (login, register, age gate, etc.).
func (m *Module) TrackTrafficEvent(ctx context.Context, params TrafficEventParams) {
	data := params.Data
	if data == nil {
		data = make(map[string]any)
	}
	m.TrackEvent(ctx, models.AnalyticsEvent{
		Type:      params.Type,
		UserID:    params.UserID,
		SessionID: params.SessionID,
		IPAddress: params.IPAddress,
		UserAgent: params.UserAgent,
		Data:      data,
	})
}

// TrackServerError records a 5xx response or recovered panic so the dashboard
// can show a real-time server-health signal. Called from middleware so every
// handler benefits without per-call instrumentation.
func (m *Module) TrackServerError(ctx context.Context, params TrafficEventParams) {
	if params.Type == "" {
		params.Type = EventServerError
	}
	m.TrackTrafficEvent(ctx, params)
}

// TrackDownload records a media download event.
func (m *Module) TrackDownload(ctx context.Context, params ViewParams) {
	m.TrackEvent(ctx, models.AnalyticsEvent{
		Type:      EventDownload,
		MediaID:   params.MediaID,
		UserID:    params.UserID,
		SessionID: params.SessionID,
		IPAddress: params.IPAddress,
		UserAgent: params.UserAgent,
		Data:      map[string]any{"timestamp": time.Now()},
	})
}

// clientAllowedTypes lists event types that clients (browser players) may submit.
// Server-only events (login, logout, register, download, view, playback, etc.)
// are intentionally excluded — accepting them from clients would let any caller
// inflate traffic counts simply by POSTing forged JSON. "view" and "playback"
// are tracked exclusively on the server side from the actual streaming and
// playback handlers, never trusted from a client message.
var clientAllowedTypes = map[string]bool{
	EventPlay: true, EventPause: true, EventResume: true, EventSeek: true,
	EventComplete: true, EventError: true, EventQualityChange: true,
	EventBuffering: true, EventVolumeChange: true, EventFullscreen: true,
}

// SubmitClientEvent processes an event submitted by a client.
// Server-only event types (login, register, download, etc.) are rejected to
// prevent clients from forging traffic statistics.
func (m *Module) SubmitClientEvent(ctx context.Context, input ClientEventInput) {
	eventType := input.Type
	if !clientAllowedTypes[eventType] {
		// Reclassify unknown or server-only types as "custom" so they are still
		// recorded but cannot inflate server-side counters (logins, downloads, etc.).
		eventType = "custom"
	}

	m.TrackEvent(ctx, models.AnalyticsEvent{
		Type:      eventType,
		MediaID:   input.MediaID,
		UserID:    input.UserID,
		SessionID: input.SessionID,
		IPAddress: input.IPAddress,
		UserAgent: input.UserAgent,
		Data:      input.Data,
	})
}

func (m *Module) listEvents(ctx context.Context, filter repositories.AnalyticsFilter, errMsg string) []models.AnalyticsEvent {
	events, err := m.eventRepo.List(ctx, filter)
	if err != nil {
		m.log.Error(errMsg, err)
		return []models.AnalyticsEvent{}
	}
	return eventsToSlice(events)
}

// GetRecentEvents returns recent events.
func (m *Module) GetRecentEvents(ctx context.Context, limit int) []models.AnalyticsEvent {
	return m.listEvents(ctx, repositories.AnalyticsFilter{Limit: limit}, "Failed to list recent events: %v")
}

// GetEventsByType returns events filtered by type.
func (m *Module) GetEventsByType(ctx context.Context, eventType string, limit int) []models.AnalyticsEvent {
	return m.listEvents(ctx, repositories.AnalyticsFilter{Type: eventType, Limit: limit}, "Failed to get events by type: %v")
}

// DeleteEventsByMedia removes raw events for a deleted media item AND emits a
// permanent tombstone (`media_deleted`) carrying the historical view/playback
// counts. The tombstone is NOT keyed by media_id (the media is gone, foreign
// references would hold orphaned values), so it survives the purge and shows
// up in audit-style queries even after the media row is removed.
//
// Without the tombstone, the dashboard's "media deletions today" count would
// always be zero and historical totals would silently drop on every delete.
func (m *Module) DeleteEventsByMedia(ctx context.Context, mediaID string) {
	// Snapshot the cached per-media stats BEFORE the purge so the tombstone
	// preserves them. The values come from the in-memory map, not the DB,
	// because we're about to delete the DB rows.
	m.statsMu.RLock()
	var totalViews, totalPlaybacks, totalCompletions int
	var lastViewed time.Time
	if stats, ok := m.mediaStats[mediaID]; ok && stats != nil {
		totalViews = stats.TotalViews
		totalPlaybacks = stats.TotalPlaybacks
		totalCompletions = stats.TotalCompletions
		lastViewed = stats.LastViewed
	}
	m.statsMu.RUnlock()

	tombstone := models.AnalyticsEvent{
		ID:        generateEventID(),
		Type:      EventMediaDeleted,
		Timestamp: time.Now(),
		Data: map[string]any{
			"media_id":          mediaID,
			"total_views":       totalViews,
			"total_playbacks":   totalPlaybacks,
			"total_completions": totalCompletions,
			"last_viewed":       lastViewed,
		},
	}
	if err := m.eventRepo.Create(ctx, &tombstone); err != nil {
		m.log.Warn("Failed to write tombstone for deleted media %s: %v", mediaID, err)
	}
	m.updateStats(tombstone)

	if err := m.eventRepo.DeleteByMediaID(ctx, mediaID); err != nil {
		m.log.Warn("Failed to purge analytics events for deleted media %s: %v", mediaID, err)
	}

	// Drop the in-memory media stats now that the row is gone. Without this,
	// GetTopMedia and GetContentPerformance would still return phantom rows
	// for the deleted item until the LRU eviction window kicked in.
	m.statsMu.Lock()
	delete(m.mediaStats, mediaID)
	delete(m.mediaViewers, mediaID)
	delete(m.mediaDurationSamples, mediaID)
	m.statsMu.Unlock()
}

// GetEventsByMedia returns events for a specific media item.
func (m *Module) GetEventsByMedia(ctx context.Context, mediaID string, limit int) []models.AnalyticsEvent {
	return m.listEvents(ctx, repositories.AnalyticsFilter{MediaID: mediaID, Limit: limit}, "Failed to get events by media: %v")
}

// GetEventsByUser returns events for a specific user.
func (m *Module) GetEventsByUser(ctx context.Context, userID string, limit int) []models.AnalyticsEvent {
	return m.listEvents(ctx, repositories.AnalyticsFilter{UserID: userID, Limit: limit}, "Failed to get events by user: %v")
}

// GetEventTypeCounts returns counts of each event type.
func (m *Module) GetEventTypeCounts(ctx context.Context) map[string]int64 {
	counts, err := m.eventRepo.CountByType(ctx)
	if err != nil {
		m.log.Error("Failed to get event type counts: %v", err)
		return make(map[string]int64)
	}
	return counts
}

// GetEventStats returns detailed event statistics.
func (m *Module) GetEventStats(ctx context.Context) EventStats {
	eventCounts := m.GetEventTypeCounts(ctx)

	var totalEvents int64
	for _, c := range eventCounts {
		totalEvents += c
	}

	now := time.Now()
	todayStr := now.Format(dateFormat)
	todayEvents, err := m.eventRepo.List(ctx, repositories.AnalyticsFilter{StartDate: todayStr})
	if err != nil {
		m.log.Error("Failed to get today's events for hourly stats: %v", err)
		todayEvents = nil
	}

	loc := now.Location()
	hourly := make([]int, 24)
	for _, event := range todayEvents {
		hourly[event.Timestamp.In(loc).Hour()]++
	}

	return EventStats{
		TotalEvents:  totalEvents,
		EventCounts:  eventCounts,
		HourlyEvents: hourly,
	}
}
