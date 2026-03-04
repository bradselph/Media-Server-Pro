// Package analytics provides event tracking and statistics.
// It handles view tracking, playback analytics, and data aggregation.
package analytics

import (
	"context"
	"crypto/rand"
	"encoding/csv"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"media-server-pro/internal/config"
	"media-server-pro/internal/database"
	"media-server-pro/internal/logger"
	"media-server-pro/internal/repositories"
	"media-server-pro/internal/repositories/mysql"
	"media-server-pro/pkg/helpers"
	"media-server-pro/pkg/models"
)

const dateFormat = "2006-01-02"

// Module implements analytics tracking
type Module struct {
	config        *config.Manager
	log           *logger.Logger
	dbModule      *database.Module
	eventRepo     repositories.AnalyticsRepository
	sessions      map[string]*sessionData
	dailyStats    map[string]*models.DailyStats
	mediaStats    map[string]*models.ViewStats
	sessionsMu    sync.RWMutex
	statsMu       sync.RWMutex
	healthy       bool
	healthMsg     string
	healthMu      sync.RWMutex
	cleanupTicker *time.Ticker
	done          chan struct{}
	maxEvents     int
}

type sessionData struct {
	ID           string
	UserID       string
	IPAddress    string
	UserAgent    string
	StartedAt    time.Time
	LastActivity time.Time
	MediaViewed  map[string]time.Time
	EventCount   int
}

// NewModule creates a new analytics module.
// The database module is stored but repositories are created during Start().
func NewModule(cfg *config.Manager, dbModule *database.Module) (*Module, error) {
	if dbModule == nil {
		return nil, fmt.Errorf("database module is required for analytics")
	}

	return &Module{
		config:     cfg,
		log:        logger.New("analytics"),
		dbModule:   dbModule,
		sessions:   make(map[string]*sessionData),
		dailyStats: make(map[string]*models.DailyStats),
		mediaStats: make(map[string]*models.ViewStats),
		done:       make(chan struct{}),
		maxEvents:  2000, // enough for accurate stat reconstruction; 10000 caused 500ms+ startup queries
	}, nil
}

// Name returns the module name
func (m *Module) Name() string {
	return "analytics"
}

// Start initializes the analytics module
func (m *Module) Start(_ context.Context) error {
	m.log.Info("Starting analytics module...")

	// Initialize MySQL repository (database is now connected)
	if !m.dbModule.IsConnected() {
		return fmt.Errorf("database is not connected")
	}
	m.log.Info("Using MySQL repository for analytics")
	m.eventRepo = mysql.NewAnalyticsRepository(m.dbModule.GORM())

	// Reconstruct in-memory stats from stored events
	m.reconstructStats()

	cfg := m.config.Get()

	// Start cleanup ticker
	m.cleanupTicker = time.NewTicker(cfg.Analytics.CleanupInterval)

	go m.backgroundLoop()

	m.healthMu.Lock()
	m.healthy = true
	m.healthMsg = "Running"
	m.healthMu.Unlock()
	m.log.Info("Analytics module started")
	return nil
}

// Stop gracefully stops the module
func (m *Module) Stop(_ context.Context) error {
	m.log.Info("Stopping analytics module...")

	close(m.done)
	if m.cleanupTicker != nil {
		m.cleanupTicker.Stop()
	}

	m.healthMu.Lock()
	m.healthy = false
	m.healthMsg = "Stopped"
	m.healthMu.Unlock()
	return nil
}

// Health returns the module health status
func (m *Module) Health() models.HealthStatus {
	m.healthMu.RLock()
	healthy := m.healthy
	msg := m.healthMsg
	m.healthMu.RUnlock()
	return models.HealthStatus{
		Name:      m.Name(),
		Status:    helpers.StatusString(healthy),
		Message:   msg,
		CheckedAt: time.Now(),
	}
}

// backgroundLoop handles periodic cleanup
func (m *Module) backgroundLoop() {
	for {
		select {
		case <-m.cleanupTicker.C:
			m.cleanup()
		case <-m.done:
			return
		}
	}
}

// generateEventID creates a unique event ID using crypto/rand to avoid collisions.
func generateEventID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		// Fallback: this should essentially never happen
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}

// TrackEvent records an analytics event
func (m *Module) TrackEvent(ctx context.Context, event models.AnalyticsEvent) {
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now()
	}
	if event.ID == "" {
		event.ID = generateEventID()
	}

	// Use a short deadline so a slow DB doesn't block the handler goroutine.
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := m.eventRepo.Create(ctx, &event); err != nil {
		m.log.Error("Failed to create analytics event: %v", err)
	}

	// Update session
	if event.SessionID != "" {
		m.updateSession(event)
	}

	// Update stats
	m.updateStats(event)

	m.log.Debug("Tracked event: %s for %s", event.Type, event.MediaID)
}

func (m *Module) TrackView(ctx context.Context, mediaID, userID, sessionID, ipAddress, userAgent string) {
	cfg := m.config.Get()
	if !cfg.Analytics.TrackViews {
		return
	}
	m.TrackEvent(ctx, models.AnalyticsEvent{
		Type:      "view",
		MediaID:   mediaID,
		UserID:    userID,
		SessionID: sessionID,
		IPAddress: ipAddress,
		UserAgent: userAgent,
		Data:      map[string]interface{}{"timestamp": time.Now()},
	})
}

func (m *Module) TrackPlayback(ctx context.Context, mediaID, userID, sessionID string, position, duration float64) {
	cfg := m.config.Get()
	if !cfg.Analytics.TrackPlayback {
		return
	}
	progress := 0.0
	if duration > 0 {
		progress = position / duration * 100
	}
	m.TrackEvent(ctx, models.AnalyticsEvent{
		Type:      "playback",
		MediaID:   mediaID,
		UserID:    userID,
		SessionID: sessionID,
		Data: map[string]interface{}{
			"position": position,
			"duration": duration,
			"progress": progress,
		},
	})
}

// updateSession updates session tracking
func (m *Module) updateSession(event models.AnalyticsEvent) {
	m.sessionsMu.Lock()
	defer m.sessionsMu.Unlock()

	session, exists := m.sessions[event.SessionID]
	if !exists {
		session = &sessionData{
			ID:          event.SessionID,
			UserID:      event.UserID,
			IPAddress:   event.IPAddress,
			UserAgent:   event.UserAgent,
			StartedAt:   time.Now(),
			MediaViewed: make(map[string]time.Time),
		}
		m.sessions[event.SessionID] = session
	}

	session.LastActivity = time.Now()
	session.EventCount++

	if event.MediaID != "" {
		session.MediaViewed[event.MediaID] = time.Now()
	}
}

// updateStats updates aggregate statistics
func (m *Module) updateStats(event models.AnalyticsEvent) {
	m.statsMu.Lock()
	defer m.statsMu.Unlock()

	// Update daily stats
	today := time.Now().Format(dateFormat)
	daily, exists := m.dailyStats[today]
	if !exists {
		daily = &models.DailyStats{
			Date: today,
		}
		m.dailyStats[today] = daily
	}

	if event.Type == "view" {
		daily.TotalViews++
		// TODO: DailyStats.UniqueUsers, TotalWatchTime, NewUsers, and TopMedia are never updated here.
		// UniqueUsers: use a per-day set of userIDs (e.g. map[string]map[string]struct{}) to count distinct.
		// TotalWatchTime: accumulate from "playback" events that carry a "duration" field in event.Data.
		// NewUsers: requires auth.CreateUser to emit a synthetic event; or query user.CreatedAt on flush.
		// TopMedia: maintain a sorted slice of (mediaID, count) pairs updated on each "view" event.
		// Until these are implemented, GetDailyStats returns zeroes for all four fields.
	}

	// Update media stats
	if event.MediaID != "" {
		stats, exists := m.mediaStats[event.MediaID]
		if !exists {
			stats = &models.ViewStats{}
			m.mediaStats[event.MediaID] = stats
		}

		if event.Type == "view" {
			stats.TotalViews++
			stats.LastViewed = time.Now()
		}

		if event.Type == "playback" {
			if data, ok := event.Data["progress"].(float64); ok && data >= 90 {
				// Prevent division by zero if TotalViews is 0
				if stats.TotalViews > 0 {
					stats.CompletionRate = (stats.CompletionRate*float64(stats.TotalViews-1) + 1) / float64(stats.TotalViews)
				} else {
					// No views recorded yet, treat completion rate as 100% for this one completion
					stats.CompletionRate = 1.0
				}
			}
		}
	}
}

// GetDailyStats returns daily statistics
func (m *Module) GetDailyStats(days int) []*models.DailyStats {
	m.statsMu.RLock()
	defer m.statsMu.RUnlock()

	var stats []*models.DailyStats
	now := time.Now()

	for i := 0; i < days; i++ {
		date := now.AddDate(0, 0, -i).Format(dateFormat)
		if daily, ok := m.dailyStats[date]; ok {
			stats = append(stats, daily)
		}
	}

	return stats
}

// GetMediaStats returns statistics for a media item
func (m *Module) GetMediaStats(mediaID string) *models.ViewStats {
	m.statsMu.RLock()
	defer m.statsMu.RUnlock()

	if stats, ok := m.mediaStats[mediaID]; ok {
		return stats
	}
	return &models.ViewStats{}
}

// GetTopMedia returns most viewed media
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

// MediaViewCount pairs media ID with view count
type MediaViewCount struct {
	MediaID string `json:"media_id"`
	Views   int    `json:"views"`
}

// countActiveSessions counts sessions active within the configured timeout.
// Caller must hold sessionsMu at least for RLock before calling.
func (m *Module) countActiveSessions(timeout time.Duration) int {
	active := 0
	for _, session := range m.sessions {
		if time.Since(session.LastActivity) < timeout {
			active++
		}
	}
	return active
}

// DEPRECATED: R-09 — never called from any handler; GetSummary/GetStats provide active session counts — safe to delete
func (m *Module) GetActiveSessions() int {
	m.sessionsMu.RLock()
	defer m.sessionsMu.RUnlock()
	cfg := m.config.Get()
	return m.countActiveSessions(cfg.Analytics.SessionTimeout)
}

// GetRecentEvents returns recent events
func (m *Module) GetRecentEvents(ctx context.Context, limit int) []models.AnalyticsEvent {
	events, err := m.eventRepo.List(ctx, repositories.AnalyticsFilter{Limit: limit})
	if err != nil {
		m.log.Error("Failed to list recent events: %v", err)
		return []models.AnalyticsEvent{}
	}

	result := make([]models.AnalyticsEvent, len(events))
	for i, event := range events {
		result[i] = *event
	}
	return result
}

// GetSummary returns analytics summary.
func (m *Module) GetSummary(ctx context.Context) Summary {
	totalEvents, err := m.eventRepo.Count(ctx, repositories.AnalyticsFilter{})
	if err != nil {
		m.log.Error("Failed to count events: %v", err)
		totalEvents = 0
	}

	m.sessionsMu.RLock()
	cfg := m.config.Get()
	activeSessions := m.countActiveSessions(cfg.Analytics.SessionTimeout)
	m.sessionsMu.RUnlock()

	m.statsMu.RLock()
	summary := Summary{
		TotalEvents:    int(totalEvents),
		ActiveSessions: activeSessions,
		TotalMedia:     len(m.mediaStats),
	}
	today := time.Now().Format(dateFormat)
	if daily, ok := m.dailyStats[today]; ok {
		summary.TodayViews = daily.TotalViews
	}
	for _, stats := range m.mediaStats {
		summary.TotalViews += stats.TotalViews
	}
	m.statsMu.RUnlock()

	return summary
}

// Summary holds summary statistics
type Summary struct {
	TotalEvents    int `json:"total_events"`
	ActiveSessions int `json:"active_sessions"`
	TodayViews     int `json:"today_views"`
	TotalViews     int `json:"total_views"`
	TotalMedia     int `json:"total_media"`
}

// Stats holds statistics for metrics export
type Stats struct {
	TotalViews     int
	UniqueClients  int
	ActiveSessions int
}

// GetStats returns analytics statistics for metrics.
// Lock ordering: sessionsMu -> statsMu (consistent with cleanup).
func (m *Module) GetStats() Stats {
	m.sessionsMu.RLock()
	cfg := m.config.Get()
	uniqueClients := len(m.sessions)
	activeSessions := m.countActiveSessions(cfg.Analytics.SessionTimeout)
	m.sessionsMu.RUnlock()

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

// ExportCSV exports analytics data to CSV
func (m *Module) ExportCSV(ctx context.Context, startDate, endDate time.Time) (string, error) {
	events, err := m.eventRepo.List(ctx, repositories.AnalyticsFilter{
		StartDate: startDate.Format(time.RFC3339),
		EndDate:   endDate.Format(time.RFC3339),
	})
	if err != nil {
		return "", fmt.Errorf("failed to fetch events: %w", err)
	}

	filename := filepath.Join(m.config.Get().Directories.Analytics, fmt.Sprintf("export_%s.csv", time.Now().Format("20060102_150405")))
	file, err := os.Create(filename)
	if err != nil {
		return "", fmt.Errorf("failed to create export file: %w", err)
	}
	defer func() {
		if err := file.Close(); err != nil {
			m.log.Warn("Failed to close CSV export file: %v", err)
		}
	}()

	writer := csv.NewWriter(file)
	defer writer.Flush()

	// Write header
	header := []string{"Timestamp", "Type", "MediaID", "UserID", "SessionID", "IPAddress"}
	if err := writer.Write(header); err != nil {
		return "", err
	}

	// Write events
	for _, event := range events {
		row := []string{
			event.Timestamp.Format(time.RFC3339),
			event.Type,
			event.MediaID,
			event.UserID,
			event.SessionID,
			event.IPAddress,
		}
		if err := writer.Write(row); err != nil {
			return "", err
		}
	}

	m.log.Info("Exported analytics to %s", filename)
	return filename, nil
}

// cleanup removes old data
func (m *Module) cleanup() {
	cfg := m.config.Get()
	cutoff := time.Now().AddDate(0, 0, -cfg.Analytics.RetentionDays)

	// Cleanup old events — use a generous timeout since this is a background job
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	before := cutoff.Format(time.RFC3339)
	if err := m.eventRepo.DeleteOlderThan(ctx, before); err != nil {
		m.log.Error("Failed to cleanup old events: %v", err)
	}

	// Cleanup old sessions
	m.sessionsMu.Lock()
	timeout := cfg.Analytics.SessionTimeout
	for id, session := range m.sessions {
		if time.Since(session.LastActivity) > timeout*2 {
			delete(m.sessions, id)
		}
	}
	m.sessionsMu.Unlock()

	// Cleanup old daily stats
	m.statsMu.Lock()
	cutoffDate := cutoff.Format(dateFormat)
	for date := range m.dailyStats {
		if date < cutoffDate {
			delete(m.dailyStats, date)
		}
	}
	m.statsMu.Unlock()

	m.log.Debug("Completed cleanup of old analytics data")
}

// reconstructStats rebuilds in-memory daily and media stats from recent database events.
// This is called on startup so the in-memory aggregates reflect stored history.
func (m *Module) reconstructStats() {
	// 60 s timeout — this runs at startup; if the DB is unresponsive we should
	// fail fast rather than blocking the module from reporting healthy.
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

// rebuildStatsFromEvent updates the in-memory maps from a single stored event.
// Must be called with m.statsMu held.
func (m *Module) rebuildStatsFromEvent(event models.AnalyticsEvent) {
	today := event.Timestamp.Format(dateFormat)
	daily, exists := m.dailyStats[today]
	if !exists {
		daily = &models.DailyStats{Date: today}
		m.dailyStats[today] = daily
	}
	if event.Type == "view" {
		daily.TotalViews++
	}

	if event.MediaID != "" {
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
		}
	}
}

// Event Types for granular tracking
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
)

// DEPRECATED: DC-01 — never called from any handler; use SubmitClientEvent instead — safe to delete
func (m *Module) TrackPlay(ctx context.Context, mediaID, userID, sessionID, ipAddress string) {
	m.TrackEvent(ctx, models.AnalyticsEvent{
		Type:      EventPlay,
		MediaID:   mediaID,
		UserID:    userID,
		SessionID: sessionID,
		IPAddress: ipAddress,
		Data: map[string]interface{}{
			"action": "play",
		},
	})
}

// DEPRECATED: DC-01 — never called from any handler; use SubmitClientEvent instead — safe to delete
func (m *Module) TrackPause(ctx context.Context, mediaID, userID, sessionID string, position float64) {
	m.TrackEvent(ctx, models.AnalyticsEvent{
		Type:      EventPause,
		MediaID:   mediaID,
		UserID:    userID,
		SessionID: sessionID,
		Data: map[string]interface{}{
			"position": position,
		},
	})
}

// DEPRECATED: DC-01 — never called from any handler; use SubmitClientEvent instead — safe to delete
func (m *Module) TrackResume(ctx context.Context, mediaID, userID, sessionID string, position float64) {
	m.TrackEvent(ctx, models.AnalyticsEvent{
		Type:      EventResume,
		MediaID:   mediaID,
		UserID:    userID,
		SessionID: sessionID,
		Data: map[string]interface{}{
			"position": position,
		},
	})
}

// DEPRECATED: DC-01 — never called from any handler; use SubmitClientEvent instead — safe to delete
func (m *Module) TrackSeek(ctx context.Context, mediaID, userID, sessionID string, fromPos, toPos float64) {
	m.TrackEvent(ctx, models.AnalyticsEvent{
		Type:      EventSeek,
		MediaID:   mediaID,
		UserID:    userID,
		SessionID: sessionID,
		Data: map[string]interface{}{
			"from_position": fromPos,
			"to_position":   toPos,
			"seek_delta":    toPos - fromPos,
		},
	})
}

// DEPRECATED: DC-01 — never called from any handler; use SubmitClientEvent instead — safe to delete
func (m *Module) TrackComplete(ctx context.Context, mediaID, userID, sessionID string, duration, watchTime float64) {
	var completionRate float64
	if duration > 0 {
		completionRate = watchTime / duration * 100
	}

	m.TrackEvent(ctx, models.AnalyticsEvent{
		Type:      EventComplete,
		MediaID:   mediaID,
		UserID:    userID,
		SessionID: sessionID,
		Data: map[string]interface{}{
			"duration":        duration,
			"watch_time":      watchTime,
			"completion_rate": completionRate,
		},
	})

	// Update media completion stats
	m.statsMu.Lock()
	if stats, exists := m.mediaStats[mediaID]; exists {
		if duration > 0 && stats.TotalViews > 0 {
			// Clamp completion percentage to [0, 1] range
			completionPct := watchTime / duration
			if completionPct > 1.0 {
				completionPct = 1.0
			} else if completionPct < 0 {
				completionPct = 0
			}

			// Guard against negative denominator from concurrent cleanup
			if stats.TotalViews > 1 {
				stats.CompletionRate = (stats.CompletionRate*float64(stats.TotalViews-1) + completionPct) / float64(stats.TotalViews)
			} else {
				stats.CompletionRate = completionPct
			}
		}
	}
	m.statsMu.Unlock()
}

// DEPRECATED: DC-01 — never called from any handler; use SubmitClientEvent instead — safe to delete
func (m *Module) TrackError(ctx context.Context, mediaID, userID, sessionID, errorCode, errorMessage string) {
	m.TrackEvent(ctx, models.AnalyticsEvent{
		Type:      EventError,
		MediaID:   mediaID,
		UserID:    userID,
		SessionID: sessionID,
		Data: map[string]interface{}{
			"error_code":    errorCode,
			"error_message": errorMessage,
		},
	})
}

// DEPRECATED: DC-01 — never called from any handler; use SubmitClientEvent instead — safe to delete
func (m *Module) TrackQualityChange(ctx context.Context, mediaID, userID, sessionID, fromQuality, toQuality string) {
	m.TrackEvent(ctx, models.AnalyticsEvent{
		Type:      EventQualityChange,
		MediaID:   mediaID,
		UserID:    userID,
		SessionID: sessionID,
		Data: map[string]interface{}{
			"from_quality": fromQuality,
			"to_quality":   toQuality,
		},
	})
}

// DEPRECATED: DC-01 — never called from any handler; use SubmitClientEvent instead — safe to delete
func (m *Module) TrackBuffering(ctx context.Context, mediaID, userID, sessionID string, bufferDuration float64) {
	m.TrackEvent(ctx, models.AnalyticsEvent{
		Type:      EventBuffering,
		MediaID:   mediaID,
		UserID:    userID,
		SessionID: sessionID,
		Data: map[string]interface{}{
			"buffer_duration_ms": bufferDuration,
		},
	})
}

// SubmitClientEvent processes an event submitted by a client
func (m *Module) SubmitClientEvent(ctx context.Context, eventType, mediaID, userID, sessionID, ipAddress, userAgent string, data map[string]interface{}) {
	// Validate event type
	validTypes := map[string]bool{
		EventPlay: true, EventPause: true, EventResume: true, EventSeek: true,
		EventComplete: true, EventError: true, EventQualityChange: true,
		EventBuffering: true, EventVolumeChange: true, EventFullscreen: true,
		"view": true, "playback": true,
	}

	if !validTypes[eventType] {
		eventType = "custom"
	}

	m.TrackEvent(ctx, models.AnalyticsEvent{
		Type:      eventType,
		MediaID:   mediaID,
		UserID:    userID,
		SessionID: sessionID,
		IPAddress: ipAddress,
		UserAgent: userAgent,
		Data:      data,
	})
}

// GetEventsByType returns events filtered by type
func (m *Module) GetEventsByType(ctx context.Context, eventType string, limit int) []models.AnalyticsEvent {
	events, err := m.eventRepo.List(ctx, repositories.AnalyticsFilter{
		Type:  eventType,
		Limit: limit,
	})
	if err != nil {
		m.log.Error("Failed to get events by type: %v", err)
		return []models.AnalyticsEvent{}
	}

	result := make([]models.AnalyticsEvent, len(events))
	for i, event := range events {
		result[i] = *event
	}
	return result
}

// GetEventsByMedia returns events for a specific media item
func (m *Module) GetEventsByMedia(ctx context.Context, mediaID string, limit int) []models.AnalyticsEvent {
	events, err := m.eventRepo.GetByMediaID(ctx, mediaID)
	if err != nil {
		m.log.Error("Failed to get events by media: %v", err)
		return []models.AnalyticsEvent{}
	}

	if limit > 0 && len(events) > limit {
		events = events[:limit]
	}

	result := make([]models.AnalyticsEvent, len(events))
	for i, event := range events {
		result[i] = *event
	}
	return result
}

// GetEventTypeCounts returns counts of each event type using a SQL GROUP BY query.
func (m *Module) GetEventTypeCounts(ctx context.Context) map[string]int {
	counts, err := m.eventRepo.CountByType(ctx)
	if err != nil {
		m.log.Error("Failed to get event type counts: %v", err)
		return make(map[string]int)
	}
	return counts
}

// GetEventStats returns detailed event statistics.
// Uses CountByType (SQL GROUP BY) for totals and a date-scoped query for hourly distribution.
func (m *Module) GetEventStats(ctx context.Context) EventStats {
	eventCounts, err := m.eventRepo.CountByType(ctx)
	if err != nil {
		m.log.Error("Failed to get event type counts for stats: %v", err)
		eventCounts = make(map[string]int)
	}

	totalEvents := 0
	for _, c := range eventCounts {
		totalEvents += c
	}

	// For hourly distribution, scope query to today only — avoids full table scan
	now := time.Now()
	todayStr := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()).Format("2006-01-02")
	todayEvents, err := m.eventRepo.List(ctx, repositories.AnalyticsFilter{StartDate: todayStr})
	if err != nil {
		m.log.Error("Failed to get today's events for hourly stats: %v", err)
		todayEvents = nil
	}

	hourly := make([]int, 24)
	for _, event := range todayEvents {
		// TODO: Timezone mismatch in hourly distribution. Events are stored with UTC timestamps
		// (gorm:"autoCreateTime" uses UTC), but todayStr is filtered using the server's local timezone
		// via time.Now().Location(). If the server is in a non-UTC timezone, events near midnight may
		// be attributed to the wrong bucket (event.Timestamp.Hour() returns UTC hour while todayStr
		// filters by local date). Fix: convert event.Timestamp to the server's local timezone before
		// calling .Hour(), or store all timestamps in local time consistently.
		hourly[event.Timestamp.Hour()]++
	}

	return EventStats{
		TotalEvents:  totalEvents,
		EventCounts:  eventCounts,
		HourlyEvents: hourly,
	}
}

// EventStats holds event statistics
type EventStats struct {
	TotalEvents  int            `json:"total_events"`
	EventCounts  map[string]int `json:"event_counts"`
	HourlyEvents []int          `json:"hourly_events"`
}
