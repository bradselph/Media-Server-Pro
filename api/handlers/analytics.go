package handlers

import (
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"media-server-pro/internal/analytics"
	"media-server-pro/pkg/models"
)

// GetAnalyticsSummary returns analytics summary with top viewed and recent activity
func (h *Handler) GetAnalyticsSummary(c *gin.Context) {
	if h.analytics == nil {
		writeSuccess(c, map[string]any{"analytics_disabled": true})
		return
	}
	summary := h.analytics.GetSummary(c.Request.Context())
	globalStats := h.analytics.GetStats()

	topMedia := h.analytics.GetTopMedia(10)
	topViewed := make([]map[string]any, 0, len(topMedia))
	for _, item := range topMedia {
		filename := item.MediaID
		// Analytics keys are stable UUIDs — resolve to human-readable names.
		if mediaItem, err := h.media.GetMediaByID(item.MediaID); err == nil && mediaItem != nil {
			filename = mediaItem.Name
		}
		topViewed = append(topViewed, map[string]any{
			"media_id": item.MediaID,
			"filename": filename,
			"views":    item.Views,
		})
	}

	recentEvents := h.analytics.GetRecentEvents(c.Request.Context(), 20)
	recentActivity := make([]map[string]any, 0, len(recentEvents))
	for _, event := range recentEvents {
		filename := event.MediaID
		// Analytics keys are stable UUIDs — resolve to human-readable names.
		if mediaItem, err := h.media.GetMediaByID(event.MediaID); err == nil && mediaItem != nil {
			filename = mediaItem.Name
		}
		recentActivity = append(recentActivity, map[string]any{
			"type":      event.Type,
			"media_id":  event.MediaID,
			"filename":  filename,
			"timestamp": event.Timestamp.Unix(),
		})
	}

	writeSuccess(c, map[string]any{
		"total_events":          summary.TotalEvents,
		"active_sessions":       summary.ActiveSessions,
		"today_views":           summary.TodayViews,
		"total_views":           summary.TotalViews,
		"total_media":           summary.TotalMedia,
		"total_watch_time":      summary.TotalWatchTime,
		"unique_clients":        globalStats.UniqueClients,
		"top_viewed":            topViewed,
		"recent_activity":       recentActivity,
		"today_logins":          summary.TodayLogins,
		"today_logins_failed":   summary.TodayLoginsFailed,
		"today_registrations":   summary.TodayRegistrations,
		"today_age_gate_passes": summary.TodayAgeGatePasses,
		"today_downloads":       summary.TodayDownloads,
		"today_searches":        summary.TodaySearches,
	})
}

// GetDailyStats returns daily statistics
func (h *Handler) GetDailyStats(c *gin.Context) {
	if h.analytics == nil {
		writeSuccess(c, []any{})
		return
	}
	days := 30
	if d, err := strconv.Atoi(c.Query("days")); err == nil && d > 0 && d <= 365 {
		days = d
	}

	stats := h.analytics.GetDailyStats(days)
	if stats == nil {
		stats = make([]*models.DailyStats, 0)
	}
	for _, s := range stats {
		if s.TopMedia == nil {
			s.TopMedia = make([]string, 0)
		}
	}
	writeSuccess(c, stats)
}

// GetTopMedia returns top viewed media
func (h *Handler) GetTopMedia(c *gin.Context) {
	if h.analytics == nil {
		writeSuccess(c, []any{})
		return
	}
	limit := 10
	if l, err := strconv.Atoi(c.Query("limit")); err == nil && l > 0 && l <= 500 {
		limit = l
	}

	top := h.analytics.GetTopMedia(limit)
	enriched := make([]map[string]any, 0, len(top))
	for _, item := range top {
		filename := item.MediaID
		// Analytics keys are stable UUIDs — resolve to human-readable names.
		if mediaItem, err := h.media.GetMediaByID(item.MediaID); err == nil && mediaItem != nil {
			filename = mediaItem.Name
		}
		entry := map[string]any{
			"media_id": item.MediaID,
			"filename": filename,
			"views":    item.Views,
		}
		enriched = append(enriched, entry)
	}
	writeSuccess(c, enriched)
}

// GetContentPerformance returns media items with rich performance metrics
// (completion rate, avg watch duration, unique viewers).
func (h *Handler) GetContentPerformance(c *gin.Context) {
	if h.analytics == nil {
		writeSuccess(c, []any{})
		return
	}
	limit := 20
	if l, err := strconv.Atoi(c.Query("limit")); err == nil && l > 0 && l <= 500 {
		limit = l
	}

	items := h.analytics.GetContentPerformance(limit)
	enriched := make([]map[string]any, 0, len(items))
	for _, item := range items {
		filename := item.MediaID
		if mediaItem, err := h.media.GetMediaByID(item.MediaID); err == nil && mediaItem != nil {
			filename = mediaItem.Name
		}
		enriched = append(enriched, map[string]any{
			"media_id":           item.MediaID,
			"filename":           filename,
			"total_views":        item.TotalViews,
			"total_playbacks":    item.TotalPlaybacks,
			"total_completions":  item.TotalCompletions,
			"completion_rate":    item.CompletionRate,
			"avg_watch_duration": item.AvgWatchDuration,
			"unique_viewers":     item.UniqueViewers,
		})
	}
	writeSuccess(c, enriched)
}

// SubmitEvent receives and processes analytics events from clients
func (h *Handler) SubmitEvent(c *gin.Context) {
	var req struct {
		Type      string                 `json:"type"`
		MediaID   string                 `json:"media_id"`
		SessionID string                 `json:"session_id"`
		Duration  float64                `json:"duration"`
		Data      map[string]any `json:"data"`
	}
	if !BindJSON(c, &req, "") {
		return
	}
	if req.Type == "" {
		writeError(c, http.StatusBadRequest, "event type required")
		return
	}
	if req.MediaID == "" {
		writeError(c, http.StatusBadRequest, "media_id required")
		return
	}
	if req.Duration > 0 {
		if req.Data == nil {
			req.Data = make(map[string]any)
		}
		if _, exists := req.Data["duration"]; !exists {
			req.Data["duration"] = req.Duration
		}
	}

	session := getSession(c)
	userID := ""
	// Always use server-side session IDs — never trust client-supplied values.
	// For authenticated users, use the session ID from the cookie.
	// For anonymous users, generate a deterministic ID from IP+UserAgent so
	// events from the same browser are grouped without trusting client input.
	var sessionID string
	if session != nil {
		userID = session.UserID
		sessionID = session.ID
	} else {
		hash := sha256.Sum256([]byte(c.ClientIP() + "|" + c.Request.UserAgent()))
		sessionID = "anon-" + fmt.Sprintf("%x", hash[:8])
	}

	if h.analytics != nil {
		h.analytics.SubmitClientEvent(c.Request.Context(), analytics.ClientEventInput{
			Type:      req.Type,
			MediaID:   req.MediaID,
			UserID:    userID,
			SessionID: sessionID,
			IPAddress: c.ClientIP(),
			UserAgent: c.Request.UserAgent(),
			Data:      req.Data,
		})
	}

	if req.Type == "complete" && h.suggestions != nil && req.MediaID != "" && userID != "" {
		// Resolve UUID to filesystem path — RecordCompletion matches against
		// ViewHistory.MediaPath which stores paths (set by RecordView).
		if item, err := h.media.GetMediaByID(req.MediaID); err == nil {
			h.suggestions.RecordCompletion(userID, item.Path)
		}
	}

	writeSuccess(c, map[string]string{"status": "recorded"})
}

// GetEventStats returns detailed event statistics. When analytics is disabled, returns zero-value EventStats.
func (h *Handler) GetEventStats(c *gin.Context) {
	if h.analytics == nil {
		writeSuccess(c, map[string]any{
			"total_events":  0,
			"event_counts":  map[string]any{},
			"hourly_events": []int{},
		})
		return
	}
	stats := h.analytics.GetEventStats(c.Request.Context())
	writeSuccess(c, stats)
}

// GetEventsByType returns events filtered by type
func (h *Handler) GetEventsByType(c *gin.Context) {
	eventType := c.Query("type")
	if eventType == "" {
		writeError(c, http.StatusBadRequest, "type parameter required")
		return
	}
	limit := 100
	if l, err := strconv.Atoi(c.Query("limit")); err == nil && l > 0 && l <= 1000 {
		limit = l
	}

	if h.analytics == nil {
		writeSuccess(c, []any{})
		return
	}
	events := h.analytics.GetEventsByType(c.Request.Context(), eventType, limit)
	writeSuccess(c, events)
}

// GetEventsByMedia returns events for a specific media item
func (h *Handler) GetEventsByMedia(c *gin.Context) {
	if h.analytics == nil {
		writeSuccess(c, []any{})
		return
	}
	mediaID := c.Query("media_id")
	if mediaID == "" {
		writeError(c, http.StatusBadRequest, "media_id parameter required")
		return
	}
	limit := 100
	if l, err := strconv.Atoi(c.Query("limit")); err == nil && l > 0 && l <= 1000 {
		limit = l
	}

	events := h.analytics.GetEventsByMedia(c.Request.Context(), mediaID, limit)
	writeSuccess(c, events)
}

// GetEventsByUser returns events for a specific user (user_id query param required)
func (h *Handler) GetEventsByUser(c *gin.Context) {
	if h.analytics == nil {
		writeSuccess(c, []any{})
		return
	}
	userID := c.Query("user_id")
	if userID == "" {
		writeError(c, http.StatusBadRequest, "user_id parameter required")
		return
	}
	limit := 100
	if l, err := strconv.Atoi(c.Query("limit")); err == nil && l > 0 && l <= 1000 {
		limit = l
	}
	if session := getSession(c); session != nil {
		h.log.Info("admin %s queried analytics events for user %s", session.Username, userID)
	}
	events := h.analytics.GetEventsByUser(c.Request.Context(), userID, limit)
	writeSuccess(c, events)
}

// GetEventTypeCounts returns counts of each event type
func (h *Handler) GetEventTypeCounts(c *gin.Context) {
	if h.analytics == nil {
		writeSuccess(c, map[string]any{})
		return
	}
	counts := h.analytics.GetEventTypeCounts(c.Request.Context())
	writeSuccess(c, counts)
}

// AdminExportAnalytics exports analytics data as a CSV file download.
func (h *Handler) AdminExportAnalytics(c *gin.Context) {
	if h.analytics == nil {
		writeError(c, http.StatusServiceUnavailable, "Analytics is not available")
		return
	}
	endDate := time.Now()
	startDate := endDate.AddDate(0, -1, 0)

	if v := c.Query("start_date"); v != "" {
		t, err := time.Parse("2006-01-02", v)
		if err != nil {
			writeError(c, http.StatusBadRequest, "start_date must be YYYY-MM-DD")
			return
		}
		startDate = t
	}
	if v := c.Query("end_date"); v != "" {
		t, err := time.Parse("2006-01-02", v)
		if err != nil {
			writeError(c, http.StatusBadRequest, "end_date must be YYYY-MM-DD")
			return
		}
		endDate = t
	}

	if startDate.After(endDate) {
		writeError(c, http.StatusBadRequest, "start_date must be before end_date")
		return
	}
	if endDate.Sub(startDate) > 365*24*time.Hour {
		writeError(c, http.StatusBadRequest, "date range cannot exceed 365 days")
		return
	}

	filename, err := h.analytics.ExportCSV(c.Request.Context(), startDate, endDate)
	if err != nil {
		h.log.Error("%v", err)
		writeError(c, http.StatusInternalServerError, errInternalServer)
		return
	}

	f, openErr := os.Open(filename)
	if openErr != nil {
		_ = os.Remove(filename)
		h.log.Error("%v", openErr)
		writeError(c, http.StatusInternalServerError, errInternalServer)
		return
	}
	defer func() { _ = f.Close(); _ = os.Remove(filename) }()

	fi, statErr := f.Stat()
	c.Header(headerContentDisposition, safeContentDisposition(pathBase(filename)))
	c.Header(headerContentType, "text/csv")
	if statErr != nil || fi == nil {
		// Fallback: stream with size cap when stat unavailable (no range support but content is served)
		c.Writer.WriteHeader(http.StatusOK)
		if _, err := io.Copy(c.Writer, io.LimitReader(f, 64*1024*1024)); err != nil {
			h.log.Error("Failed to stream CSV file: %v", err)
		}
		return
	}
	http.ServeContent(c.Writer, c.Request, fi.Name(), fi.ModTime(), f)
}

// pathBase is a local helper to get the base name of a path (avoids import of path/filepath in this file).
func pathBase(p string) string {
	for i := len(p) - 1; i >= 0; i-- {
		if p[i] == '/' || p[i] == '\\' {
			return p[i+1:]
		}
	}
	return p
}
