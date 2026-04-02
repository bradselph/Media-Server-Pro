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

	// Traffic / auth events (server-generated, not client-submitted)
	EventLogin       = "login"
	EventLoginFailed = "login_failed"
	EventLogout      = "logout"
	EventRegister    = "register"
	EventAgeGatePass = "age_gate_pass"
	EventDownload    = "download"
	EventSearch      = "search"
)

// ClientEventInput holds parameters for SubmitClientEvent.
type ClientEventInput struct {
	Type      string
	MediaID   string
	UserID    string
	SessionID string
	IPAddress string
	UserAgent string
	Data      map[string]interface{}
}

// EventStats holds event statistics.
type EventStats struct {
	TotalEvents  int            `json:"total_events"`
	EventCounts  map[string]int `json:"event_counts"`
	HourlyEvents []int          `json:"hourly_events"`
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
		Data:      map[string]interface{}{"timestamp": time.Now()},
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
		Data: map[string]interface{}{
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
	Data      map[string]interface{}
}

// TrackTrafficEvent records a server-generated traffic event (login, register, age gate, etc.).
func (m *Module) TrackTrafficEvent(ctx context.Context, params TrafficEventParams) {
	data := params.Data
	if data == nil {
		data = make(map[string]interface{})
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

// TrackDownload records a media download event.
func (m *Module) TrackDownload(ctx context.Context, params ViewParams) {
	m.TrackEvent(ctx, models.AnalyticsEvent{
		Type:      EventDownload,
		MediaID:   params.MediaID,
		UserID:    params.UserID,
		SessionID: params.SessionID,
		IPAddress: params.IPAddress,
		UserAgent: params.UserAgent,
		Data:      map[string]interface{}{"timestamp": time.Now()},
	})
}

// SubmitClientEvent processes an event submitted by a client.
func (m *Module) SubmitClientEvent(ctx context.Context, input ClientEventInput) {
	validTypes := map[string]bool{
		EventPlay: true, EventPause: true, EventResume: true, EventSeek: true,
		EventComplete: true, EventError: true, EventQualityChange: true,
		EventBuffering: true, EventVolumeChange: true, EventFullscreen: true,
		EventLogin: true, EventLoginFailed: true, EventLogout: true,
		EventRegister: true, EventAgeGatePass: true, EventDownload: true,
		EventSearch: true,
		"view": true, "playback": true,
	}

	eventType := input.Type
	if !validTypes[eventType] {
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

// GetEventsByMedia returns events for a specific media item.
func (m *Module) GetEventsByMedia(ctx context.Context, mediaID string, limit int) []models.AnalyticsEvent {
	return m.listEvents(ctx, repositories.AnalyticsFilter{MediaID: mediaID, Limit: limit}, "Failed to get events by media: %v")
}

// GetEventsByUser returns events for a specific user.
func (m *Module) GetEventsByUser(ctx context.Context, userID string, limit int) []models.AnalyticsEvent {
	return m.listEvents(ctx, repositories.AnalyticsFilter{UserID: userID, Limit: limit}, "Failed to get events by user: %v")
}

// GetEventTypeCounts returns counts of each event type.
func (m *Module) GetEventTypeCounts(ctx context.Context) map[string]int {
	counts, err := m.eventRepo.CountByType(ctx)
	if err != nil {
		m.log.Error("Failed to get event type counts: %v", err)
		return make(map[string]int)
	}
	return counts
}

// GetEventStats returns detailed event statistics.
func (m *Module) GetEventStats(ctx context.Context) EventStats {
	eventCounts := m.GetEventTypeCounts(ctx)

	totalEvents := 0
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
