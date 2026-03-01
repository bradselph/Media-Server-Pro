package handlers

import (
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"media-server-pro/pkg/models"
)

// GetAnalyticsSummary returns analytics summary with top viewed and recent activity
func (h *Handler) GetAnalyticsSummary(c *gin.Context) {
	if h.analytics == nil {
		writeSuccess(c, map[string]interface{}{"analytics_disabled": true})
		return
	}
	summary := h.analytics.GetSummary(c.Request.Context())
	globalStats := h.analytics.GetStats()

	topMedia := h.analytics.GetTopMedia(10)
	topViewed := make([]map[string]interface{}, 0, len(topMedia))
	for _, item := range topMedia {
		filename := item.MediaID
		// Analytics keys are stable UUIDs — resolve to human-readable names.
		if mediaItem, err := h.media.GetMediaByID(item.MediaID); err == nil && mediaItem != nil {
			filename = mediaItem.Name
		}
		topViewed = append(topViewed, map[string]interface{}{
			"media_id": item.MediaID,
			"filename": filename,
			"views":    item.Views,
		})
	}

	recentEvents := h.analytics.GetRecentEvents(c.Request.Context(), 20)
	recentActivity := make([]map[string]interface{}, 0, len(recentEvents))
	for _, event := range recentEvents {
		filename := event.MediaID
		// Analytics keys are stable UUIDs — resolve to human-readable names.
		if mediaItem, err := h.media.GetMediaByID(event.MediaID); err == nil && mediaItem != nil {
			filename = mediaItem.Name
		}
		recentActivity = append(recentActivity, map[string]interface{}{
			"type":      event.Type,
			"media_id":  event.MediaID,
			"filename":  filename,
			"timestamp": event.Timestamp.Unix(),
		})
	}

	writeSuccess(c, map[string]interface{}{
		"total_events":    summary.TotalEvents,
		"active_sessions": summary.ActiveSessions,
		"today_views":     summary.TodayViews,
		"total_views":     summary.TotalViews,
		"total_media":     summary.TotalMedia,
		"unique_clients":  globalStats.UniqueClients,
		"top_viewed":      topViewed,
		"recent_activity": recentActivity,
	})
}

// GetDailyStats returns daily statistics
func (h *Handler) GetDailyStats(c *gin.Context) {
	if h.analytics == nil {
		writeSuccess(c, []interface{}{})
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
		writeSuccess(c, []interface{}{})
		return
	}
	limit := 10
	if l, err := strconv.Atoi(c.Query("limit")); err == nil && l > 0 && l <= 500 {
		limit = l
	}

	top := h.analytics.GetTopMedia(limit)
	enriched := make([]map[string]interface{}, 0, len(top))
	for _, item := range top {
		filename := item.MediaID
		// Analytics keys are stable UUIDs — resolve to human-readable names.
		if mediaItem, err := h.media.GetMediaByID(item.MediaID); err == nil && mediaItem != nil {
			filename = mediaItem.Name
		}
		entry := map[string]interface{}{
			"media_id": item.MediaID,
			"filename": filename,
			"views":    item.Views,
		}
		enriched = append(enriched, entry)
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
		Data      map[string]interface{} `json:"data"`
	}
	if c.ShouldBindJSON(&req) != nil {
		writeError(c, http.StatusBadRequest, errInvalidRequest)
		return
	}
	if req.Duration > 0 {
		if req.Data == nil {
			req.Data = make(map[string]interface{})
		}
		if _, exists := req.Data["duration"]; !exists {
			req.Data["duration"] = req.Duration
		}
	}

	session := getSession(c)
	userID := ""
	sessionID := req.SessionID
	if session != nil {
		userID = session.UserID
		if sessionID == "" {
			sessionID = session.ID
		}
	}

	if h.analytics != nil {
		h.analytics.SubmitClientEvent(c.Request.Context(),
			req.Type,
			req.MediaID,
			userID,
			sessionID,
			c.ClientIP(),
			c.Request.UserAgent(),
			req.Data,
		)
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

// GetEventStats returns detailed event statistics
func (h *Handler) GetEventStats(c *gin.Context) {
	if h.analytics == nil {
		writeSuccess(c, map[string]interface{}{})
		return
	}
	stats := h.analytics.GetEventStats(c.Request.Context())
	writeSuccess(c, stats)
}

// GetEventsByType returns events filtered by type
func (h *Handler) GetEventsByType(c *gin.Context) {
	eventType := c.Query("type")
	limit := 100
	if l, err := strconv.Atoi(c.Query("limit")); err == nil && l > 0 && l <= 1000 {
		limit = l
	}

	if h.analytics == nil {
		writeSuccess(c, []interface{}{})
		return
	}
	events := h.analytics.GetEventsByType(c.Request.Context(), eventType, limit)
	writeSuccess(c, events)
}

// GetEventsByMedia returns events for a specific media item
func (h *Handler) GetEventsByMedia(c *gin.Context) {
	if h.analytics == nil {
		writeSuccess(c, []interface{}{})
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

// GetEventTypeCounts returns counts of each event type
func (h *Handler) GetEventTypeCounts(c *gin.Context) {
	if h.analytics == nil {
		writeSuccess(c, map[string]interface{}{})
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
		if t, err := time.Parse("2006-01-02", v); err == nil {
			startDate = t
		}
	}
	if v := c.Query("end_date"); v != "" {
		if t, err := time.Parse("2006-01-02", v); err == nil {
			endDate = t
		}
	}

	filename, err := h.analytics.ExportCSV(c.Request.Context(), startDate, endDate)
	if err != nil {
		h.log.Error("%v", err)
		writeError(c, http.StatusInternalServerError, "Internal server error")
		return
	}

	c.Header(headerContentDisposition, safeContentDisposition(pathBase(filename)))
	c.Header(headerContentType, "text/csv")
	http.ServeFile(c.Writer, c.Request, filename)
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
