package handlers

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
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
		filename := ""
		if event.MediaID != "" {
			// Analytics keys are stable UUIDs — resolve to human-readable names.
			if mediaItem, err := h.media.GetMediaByID(event.MediaID); err == nil && mediaItem != nil {
				filename = mediaItem.Name
			} else {
				filename = event.MediaID
			}
		}
		// Resolve UserID → username so non-media events (login, logout,
		// admin_action, etc.) render as "<username> · <type>" instead of
		// blank rows. Auth lookup failures fall back to the raw user_id.
		username := ""
		if event.UserID != "" && h.auth != nil {
			if u, err := h.auth.GetUserByID(c.Request.Context(), event.UserID); err == nil && u != nil {
				username = u.Username
			}
		}
		recentActivity = append(recentActivity, map[string]any{
			"type":       event.Type,
			"media_id":   event.MediaID,
			"filename":   filename,
			"user_id":    event.UserID,
			"username":   username,
			"ip_address": event.IPAddress,
			"timestamp":  event.Timestamp.Unix(),
		})
	}

	writeSuccess(c, map[string]any{
		"total_events":                summary.TotalEvents,
		"active_sessions":             summary.ActiveSessions,
		"today_views":                 summary.TodayViews,
		"total_views":                 summary.TotalViews,
		"total_media":                 summary.TotalMedia,
		"total_watch_time":            summary.TotalWatchTime,
		"unique_clients":              globalStats.UniqueClients,
		"top_viewed":                  topViewed,
		"recent_activity":             recentActivity,
		"today_logins":                summary.TodayLogins,
		"today_logins_failed":         summary.TodayLoginsFailed,
		"today_logouts":               summary.TodayLogouts,
		"today_registrations":         summary.TodayRegistrations,
		"today_age_gate_passes":       summary.TodayAgeGatePasses,
		"today_downloads":             summary.TodayDownloads,
		"today_searches":              summary.TodaySearches,
		"today_favorites_added":       summary.TodayFavoritesAdded,
		"today_favorites_removed":     summary.TodayFavoritesRemoved,
		"today_ratings_set":           summary.TodayRatingsSet,
		"today_playlists_created":     summary.TodayPlaylistsCreated,
		"today_playlists_deleted":     summary.TodayPlaylistsDeleted,
		"today_playlist_items_added":  summary.TodayPlaylistItemsAdded,
		"today_uploads_succeeded":     summary.TodayUploadsSucceeded,
		"today_uploads_failed":        summary.TodayUploadsFailed,
		"today_password_changes":      summary.TodayPasswordChanges,
		"today_account_deletions":     summary.TodayAccountDeletions,
		"today_hls_starts":            summary.TodayHLSStarts,
		"today_hls_errors":            summary.TodayHLSErrors,
		"today_media_deletions":       summary.TodayMediaDeletions,
		"today_api_tokens_created":    summary.TodayAPITokensCreated,
		"today_api_tokens_revoked":    summary.TodayAPITokensRevoked,
		"today_admin_actions":         summary.TodayAdminActions,
		"today_server_errors":         summary.TodayServerErrors,
		"today_stream_starts":         summary.TodayStreamStarts,
		"today_stream_ends":           summary.TodayStreamEnds,
		"today_bytes_served":          summary.TodayBytesServed,
		"today_mature_blocked":        summary.TodayMatureBlocked,
		"today_permission_denied":     summary.TodayPermissionDenied,
		"today_preferences_changes":   summary.TodayPreferencesChanges,
		"today_bulk_deletes":          summary.TodayBulkDeletes,
		"today_bulk_updates":          summary.TodayBulkUpdates,
		"today_user_role_changes":     summary.TodayUserRoleChanges,
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

// resolveAnalyticsTimeWindow turns ?days=N (and optional explicit
// ?since=&until= ISO timestamps) into a (since, until) RFC3339 pair the
// repository can apply as WHERE clauses. Empty pair = no time filter.
//
// Precedence: explicit since/until > days > none. days is the easy case for
// the dashboard ("last 7 days"); since/until is the escape hatch for ad-hoc
// reports that need a specific calendar range.
func resolveAnalyticsTimeWindow(c *gin.Context) (since, until string) {
	since = c.Query("since")
	until = c.Query("until")
	if since == "" && until == "" {
		if d, err := strconv.Atoi(c.Query("days")); err == nil && d > 0 && d <= 365 {
			since = time.Now().AddDate(0, 0, -d).Format(time.RFC3339)
		}
	}
	return since, until
}

// AdminGetTopUsers returns a leaderboard of users ranked by the chosen
// metric. Query params: metric (views|watch_time|uploads|downloads|events,
// default views), limit (1-200, default 10), days (1-365, optional time
// window — defaults to retention window when absent), since/until (ISO
// timestamps, override days). Resolves user_id → username best-effort so
// the dashboard can render names.
func (h *Handler) AdminGetTopUsers(c *gin.Context) {
	if h.analytics == nil {
		writeSuccess(c, []any{})
		return
	}
	metric := c.DefaultQuery("metric", "views")
	limit := 10
	if l, err := strconv.Atoi(c.Query("limit")); err == nil && l > 0 && l <= 200 {
		limit = l
	}
	since, until := resolveAnalyticsTimeWindow(c)
	rows := h.analytics.GetTopUsers(c.Request.Context(), metric, since, until, limit)
	if h.auth != nil {
		for i := range rows {
			if rows[i].UserID == "" {
				continue
			}
			if u, err := h.auth.GetUserByID(c.Request.Context(), rows[i].UserID); err == nil && u != nil {
				rows[i].Username = u.Username
			}
		}
	}
	writeSuccess(c, rows)
}

// AdminGetTopSearches returns the most-frequent search queries with the
// empty-result share alongside, so admins can see what users want and what
// the catalog is missing. Query params: limit (1-100, default 20),
// days (optional time window).
func (h *Handler) AdminGetTopSearches(c *gin.Context) {
	if h.analytics == nil {
		writeSuccess(c, []any{})
		return
	}
	limit := 20
	if l, err := strconv.Atoi(c.Query("limit")); err == nil && l > 0 && l <= 100 {
		limit = l
	}
	since, until := resolveAnalyticsTimeWindow(c)
	writeSuccess(c, h.analytics.GetTopSearches(c.Request.Context(), since, until, limit))
}

// AdminGetFailedLogins returns recent login_failed events for security review.
// Query params: limit (1-200, default 50), days (optional time window).
func (h *Handler) AdminGetFailedLogins(c *gin.Context) {
	if h.analytics == nil {
		writeSuccess(c, []any{})
		return
	}
	limit := 50
	if l, err := strconv.Atoi(c.Query("limit")); err == nil && l > 0 && l <= 200 {
		limit = l
	}
	since, until := resolveAnalyticsTimeWindow(c)
	writeSuccess(c, h.analytics.GetRecentFailedLogins(c.Request.Context(), since, until, limit))
}

// AdminGetErrorPaths returns a (method, path, status) breakdown of recent
// 5xx responses so operators can see which routes are misbehaving without
// drilling raw events. Query params: limit (1-200, default 25), days
// (optional time window).
func (h *Handler) AdminGetErrorPaths(c *gin.Context) {
	if h.analytics == nil {
		writeSuccess(c, []any{})
		return
	}
	limit := 25
	if l, err := strconv.Atoi(c.Query("limit")); err == nil && l > 0 && l <= 200 {
		limit = l
	}
	since, until := resolveAnalyticsTimeWindow(c)
	writeSuccess(c, h.analytics.GetErrorPaths(c.Request.Context(), since, until, limit))
}

// AdminExportAll bundles every supported analytics panel into one ZIP
// download — single-click backup of the dashboard's current state. Each
// panel becomes its own CSV inside the archive, named after the panel.
//
// Errors writing one panel don't abort the whole archive; the panel is
// skipped and a "_errors.txt" entry records which ones failed so the
// admin sees what's missing.
func (h *Handler) AdminExportAll(c *gin.Context) {
	if h.analytics == nil {
		writeError(c, http.StatusServiceUnavailable, "Analytics is not available")
		return
	}
	panels := []string{
		"top-users", "top-searches", "failed-logins", "error-paths",
		"quality", "devices", "content-gaps", "daily", "heatmap",
	}
	filename := "analytics-bundle-" + time.Now().Format("20060102-150405") + ".zip"
	c.Header(headerContentDisposition, safeContentDisposition(filename))
	c.Header(headerContentType, "application/zip")
	c.Status(http.StatusOK)

	zw := zip.NewWriter(c.Writer)
	defer func() { _ = zw.Close() }()

	var errors []string
	for _, panel := range panels {
		rows, headers, err := h.fetchExportRows(c, panel)
		if err != nil {
			errors = append(errors, panel+": "+err.Error())
			continue
		}
		w, err := zw.Create(panel + ".csv")
		if err != nil {
			errors = append(errors, panel+": create entry: "+err.Error())
			continue
		}
		csvWriter := csv.NewWriter(w)
		if writeErr := csvWriter.Write(headers); writeErr != nil {
			errors = append(errors, panel+": write header: "+writeErr.Error())
			continue
		}
		for _, row := range rows {
			rec := make([]string, len(headers))
			for i, h := range headers {
				rec[i] = exportFieldToString(row[h])
			}
			if writeErr := csvWriter.Write(rec); writeErr != nil {
				errors = append(errors, panel+": write row: "+writeErr.Error())
				break
			}
		}
		csvWriter.Flush()
		if writeErr := csvWriter.Error(); writeErr != nil {
			errors = append(errors, panel+": flush: "+writeErr.Error())
		}
	}
	if len(errors) > 0 {
		w, err := zw.Create("_errors.txt")
		if err == nil {
			for _, e := range errors {
				_, _ = w.Write([]byte(e + "\n"))
			}
		}
	}
}

// AdminStreamEvents serves a Server-Sent Events feed of live analytics
// events. Each new TrackEvent broadcasts to every active subscriber; the
// frontend opens an EventSource and renders a live tail panel.
//
// SSE was chosen over WebSocket because:
//   - one-way (server → client) traffic only, which is exactly what SSE does;
//   - native EventSource API works with cookies (CSRF-protected by same-origin
//     since we don't enable CORS for /api by default);
//   - no need for a websocket upgrade handler or per-connection ping/pong.
//
// Backpressure: each subscriber has a 64-event buffer; if the client is
// slow the broadcaster drops events for that subscriber rather than
// blocking other subscribers or the analytics hot path. The dashboard
// will see freshness recover as soon as the consumer catches up.
func (h *Handler) AdminStreamEvents(c *gin.Context) {
	if h.analytics == nil {
		writeError(c, http.StatusServiceUnavailable, "Analytics is not available")
		return
	}
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache, no-transform")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no") // disable nginx response buffering

	sub := h.analytics.Subscribe(64)
	defer sub.Cancel()

	// Send an initial comment so the EventSource fires `open` immediately,
	// which frontends often rely on to update their connection-state UI.
	if _, err := c.Writer.WriteString(": connected\n\n"); err != nil {
		return
	}
	c.Writer.Flush()

	// Heartbeat every 25s — keeps proxies (nginx, Caddy, ingress) from
	// idling-out the connection. SSE comments are ignored by the client.
	heartbeat := time.NewTicker(25 * time.Second)
	defer heartbeat.Stop()

	for {
		select {
		case <-c.Request.Context().Done():
			return
		case <-heartbeat.C:
			if _, err := c.Writer.WriteString(": heartbeat\n\n"); err != nil {
				return
			}
			c.Writer.Flush()
		case ev, ok := <-sub.Events:
			if !ok {
				// Module shutting down — close cleanly.
				return
			}
			payload, err := json.Marshal(ev)
			if err != nil {
				continue
			}
			if _, err := fmt.Fprintf(c.Writer, "event: analytics\ndata: %s\n\n", payload); err != nil {
				return
			}
			c.Writer.Flush()
		}
	}
}

// AdminExportPanel exports the named analytics panel as either JSON or CSV.
// Query: panel (required — top-users | top-searches | failed-logins |
// error-paths | active-streams | quality | devices | content-gaps |
// daily | heatmap), format (json | csv, default csv), days (optional time
// window). Streams the response with a Content-Disposition attachment so
// browsers download instead of rendering inline.
//
// The point of this endpoint is to give admins one consistent way to grab
// any panel's raw data for spreadsheet analysis — without the dashboard
// having to ship per-panel exporters.
func (h *Handler) AdminExportPanel(c *gin.Context) {
	if h.analytics == nil {
		writeError(c, http.StatusServiceUnavailable, "Analytics is not available")
		return
	}
	panel := strings.TrimSpace(c.Query("panel"))
	if panel == "" {
		writeError(c, http.StatusBadRequest, "panel parameter required")
		return
	}
	format := strings.ToLower(c.DefaultQuery("format", "csv"))
	if format != "csv" && format != "json" {
		writeError(c, http.StatusBadRequest, "format must be csv or json")
		return
	}

	rows, headers, err := h.fetchExportRows(c, panel)
	if err != nil {
		writeError(c, http.StatusBadRequest, err.Error())
		return
	}

	filename := "analytics-" + panel + "-" + time.Now().Format("20060102-150405") + "." + format
	c.Header(headerContentDisposition, safeContentDisposition(filename))
	if format == "json" {
		c.Header(headerContentType, "application/json")
		// Wrap in {"panel": ..., "rows": [...]} so consumers can tell which
		// dataset they got even if the filename is lost.
		c.JSON(http.StatusOK, map[string]any{"panel": panel, "rows": rows})
		return
	}
	c.Header(headerContentType, "text/csv")
	w := csv.NewWriter(c.Writer)
	defer w.Flush()
	if err := w.Write(headers); err != nil {
		h.log.Error("export: write csv header: %v", err)
		return
	}
	for _, row := range rows {
		rec := make([]string, len(headers))
		for i, h := range headers {
			rec[i] = exportFieldToString(row[h])
		}
		if err := w.Write(rec); err != nil {
			h.log.Error("export: write csv row: %v", err)
			return
		}
	}
}

// exportFieldToString flattens any value into a CSV-safe string. Time
// values become RFC3339, numbers become decimal, slices/maps become JSON.
func exportFieldToString(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case time.Time:
		return t.Format(time.RFC3339)
	case int:
		return strconv.Itoa(t)
	case int64:
		return strconv.FormatInt(t, 10)
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(t)
	default:
		// Slices, maps, etc. — emit as JSON so the cell is at least readable.
		b, err := jsonMarshal(v)
		if err != nil {
			return ""
		}
		return string(b)
	}
}

// jsonMarshal is split out so unit tests can stub it if needed; the
// real impl uses encoding/json.
var jsonMarshal = func(v any) ([]byte, error) {
	return json.Marshal(v)
}

// fetchExportRows dispatches the panel name to the correct analytics
// method and converts the typed results into a generic []map[string]any +
// header order so the export writer doesn't need a switch per format.
func (h *Handler) fetchExportRows(c *gin.Context, panel string) ([]map[string]any, []string, error) {
	since, until := resolveAnalyticsTimeWindow(c)
	limit := 1000
	if l, err := strconv.Atoi(c.Query("limit")); err == nil && l > 0 && l <= 10000 {
		limit = l
	}
	days := 30
	if d, err := strconv.Atoi(c.Query("days")); err == nil && d > 0 && d <= 365 {
		days = d
	}
	switch panel {
	case "top-users":
		metric := c.DefaultQuery("metric", "views")
		rows := h.analytics.GetTopUsers(c.Request.Context(), metric, since, until, limit)
		out := make([]map[string]any, len(rows))
		for i, r := range rows {
			out[i] = map[string]any{
				"user_id": r.UserID, "username": r.Username, "metric": r.Metric,
				"total_views": r.TotalViews, "total_watch_time": r.TotalWatchTime,
				"total_uploads": r.TotalUploads, "total_downloads": r.TotalDownloads,
				"total_events": r.TotalEvents,
			}
		}
		return out, []string{"user_id", "username", "metric", "total_views", "total_watch_time", "total_uploads", "total_downloads", "total_events"}, nil
	case "top-searches":
		rows := h.analytics.GetTopSearches(c.Request.Context(), since, until, limit)
		out := make([]map[string]any, len(rows))
		for i, r := range rows {
			out[i] = map[string]any{"query": r.Query, "count": r.Count, "empty_count": r.EmptyCount}
		}
		return out, []string{"query", "count", "empty_count"}, nil
	case "failed-logins":
		rows := h.analytics.GetRecentFailedLogins(c.Request.Context(), since, until, limit)
		out := make([]map[string]any, len(rows))
		for i, r := range rows {
			out[i] = map[string]any{
				"timestamp": r.Timestamp, "ip_address": r.IPAddress,
				"username": r.Username, "user_agent": r.UserAgent, "reason": r.Reason,
			}
		}
		return out, []string{"timestamp", "ip_address", "username", "user_agent", "reason"}, nil
	case "error-paths":
		rows := h.analytics.GetErrorPaths(c.Request.Context(), since, until, limit)
		out := make([]map[string]any, len(rows))
		for i, r := range rows {
			out[i] = map[string]any{
				"method": r.Method, "path": r.Path, "status": r.Status,
				"count": r.Count, "last_seen": r.LastSeen,
			}
		}
		return out, []string{"method", "path", "status", "count", "last_seen"}, nil
	case "quality":
		rows := h.analytics.GetQualityBreakdown(c.Request.Context(), days)
		out := make([]map[string]any, len(rows))
		for i, r := range rows {
			out[i] = map[string]any{"quality": r.Quality, "streams": r.Streams, "bytes_sent": r.BytesSent}
		}
		return out, []string{"quality", "streams", "bytes_sent"}, nil
	case "devices":
		devs, brws := h.analytics.GetDeviceBreakdown(c.Request.Context(), days)
		out := make([]map[string]any, 0, len(devs)+len(brws))
		for _, d := range devs {
			out = append(out, map[string]any{"category": "device", "family": d.Family, "events": d.Events, "unique_users": d.UniqueUsers})
		}
		for _, b := range brws {
			out = append(out, map[string]any{"category": "browser", "family": b.Family, "events": b.Events, "unique_users": b.UniqueUsers})
		}
		return out, []string{"category", "family", "events", "unique_users"}, nil
	case "content-gaps":
		rows := h.analytics.GetContentGaps(c.Request.Context(), since, until, 2, 0.5, limit)
		out := make([]map[string]any, len(rows))
		for i, r := range rows {
			out[i] = map[string]any{"query": r.Query, "count": r.Count, "empty_count": r.EmptyCount}
		}
		return out, []string{"query", "count", "empty_count"}, nil
	case "daily":
		rows := h.analytics.GetDailyStats(days)
		out := make([]map[string]any, len(rows))
		for i, r := range rows {
			if r == nil {
				continue
			}
			out[i] = map[string]any{
				"date": r.Date, "total_views": r.TotalViews, "unique_users": r.UniqueUsers,
				"total_watch_time": r.TotalWatchTime, "logins": r.Logins, "logins_failed": r.LoginsFailed,
				"logouts": r.Logouts, "registrations": r.Registrations, "downloads": r.Downloads,
				"searches": r.Searches, "favorites_added": r.FavoritesAdded, "ratings_set": r.RatingsSet,
				"playlists_created": r.PlaylistsCreated, "uploads_succeeded": r.UploadsSucceeded,
				"uploads_failed": r.UploadsFailed, "stream_starts": r.StreamStarts, "stream_ends": r.StreamEnds,
				"bytes_served": r.BytesServed, "hls_starts": r.HLSStarts, "hls_errors": r.HLSErrors,
				"server_errors": r.ServerErrors, "admin_actions": r.AdminActions,
			}
		}
		return out, []string{"date", "total_views", "unique_users", "total_watch_time", "logins", "logins_failed", "logouts", "registrations", "downloads", "searches", "favorites_added", "ratings_set", "playlists_created", "uploads_succeeded", "uploads_failed", "stream_starts", "stream_ends", "bytes_served", "hls_starts", "hls_errors", "server_errors", "admin_actions"}, nil
	case "heatmap":
		rows := h.analytics.GetHourlyHeatmap(c.Request.Context(), days)
		out := make([]map[string]any, len(rows))
		for i, r := range rows {
			out[i] = map[string]any{"day_of_week": r.DayOfWeek, "hour": r.Hour, "count": r.Count}
		}
		return out, []string{"day_of_week", "hour", "count"}, nil
	default:
		return nil, nil, fmt.Errorf("unknown panel %q", panel)
	}
}

// AdminBackfillDailyStats recomputes one date's DailyStats from raw events
// and writes it back. Path: POST /admin/analytics/backfill?date=YYYY-MM-DD.
//
// Use cases:
//   - the flush ticker errored on a transient DB hiccup and the persisted
//     row drifted from the live counters;
//   - someone manually edited daily_stats and wants to roll back to truth;
//   - retention pruned events on day N+30, but the day-N row got partially
//     wiped and looks suspect.
func (h *Handler) AdminBackfillDailyStats(c *gin.Context) {
	if h.analytics == nil {
		writeError(c, http.StatusServiceUnavailable, "Analytics is not available")
		return
	}
	date := strings.TrimSpace(c.Query("date"))
	if date == "" {
		writeError(c, http.StatusBadRequest, "date query param required (YYYY-MM-DD)")
		return
	}
	rebuilt, err := h.analytics.BackfillDailyStats(c.Request.Context(), date)
	if err != nil {
		writeError(c, http.StatusBadRequest, err.Error())
		return
	}
	h.logAdminAction(c, &adminLogActionParams{
		Action:  "analytics_backfill_daily_stats",
		Target:  date,
		Details: map[string]any{"date": date},
	})
	writeSuccess(c, rebuilt)
}

// AdminEvaluateAlerts evaluates a list of admin-defined alert rules
// against the current DailyStats. Rules live in browser localStorage on
// the dashboard side; we don't persist them server-side because the
// matching is cheap (in-memory lookup) and this keeps the surface area
// small.
//
// POST body: {"rules": [{ id, name, metric, operator, threshold, window }, …]}
// Response: per-rule {triggered, value, message}.
func (h *Handler) AdminEvaluateAlerts(c *gin.Context) {
	if h.analytics == nil {
		writeSuccess(c, []any{})
		return
	}
	var req struct {
		Rules []analytics.AlertRule `json:"rules"`
	}
	if !BindJSON(c, &req, "") {
		return
	}
	if len(req.Rules) > 100 {
		writeError(c, http.StatusBadRequest, "too many rules (max 100)")
		return
	}
	writeSuccess(c, h.analytics.EvaluateAlerts(req.Rules))
}

// AdminGetRangeComparison returns A/B totals for every supported metric
// across two arbitrary date ranges. Query params: a_start, a_end, b_start,
// b_end (all YYYY-MM-DD). Empty bounds disable that side of the range.
//
// Use case: "Did the new search algo (deployed 2026-04-15) move any
// metrics?" — the admin sets A=2026-04-08…2026-04-14 and B=2026-04-15…
// 2026-04-21 and reads the deltas off a single table.
func (h *Handler) AdminGetRangeComparison(c *gin.Context) {
	if h.analytics == nil {
		writeSuccess(c, map[string]any{})
		return
	}
	aStart := c.Query("a_start")
	aEnd := c.Query("a_end")
	bStart := c.Query("b_start")
	bEnd := c.Query("b_end")
	if aStart == "" && aEnd == "" {
		writeError(c, http.StatusBadRequest, "a_start and/or a_end required")
		return
	}
	if bStart == "" && bEnd == "" {
		writeError(c, http.StatusBadRequest, "b_start and/or b_end required")
		return
	}
	writeSuccess(c, h.analytics.GetRangeComparison(aStart, aEnd, bStart, bEnd))
}

// AdminGetMetricForecast returns a linear-trend projection for one metric.
// Query: metric (DailyStats JSON tag, default total_views), days (1-90,
// default 14). The response includes slope, projection (tomorrow's value),
// and a residual-stddev confidence band.
func (h *Handler) AdminGetMetricForecast(c *gin.Context) {
	if h.analytics == nil {
		writeSuccess(c, map[string]any{})
		return
	}
	metric := strings.TrimSpace(c.DefaultQuery("metric", "total_views"))
	days := 14
	if d, err := strconv.Atoi(c.Query("days")); err == nil && d > 0 && d <= 90 {
		days = d
	}
	writeSuccess(c, h.analytics.GetMetricForecast(metric, days))
}

// AdminGetIPSummary returns unique-IP count + top IPs by events / bytes.
// Query: days (1-365, default 30), limit (1-100, default 20).
func (h *Handler) AdminGetIPSummary(c *gin.Context) {
	if h.analytics == nil {
		writeSuccess(c, map[string]any{})
		return
	}
	days := 30
	if d, err := strconv.Atoi(c.Query("days")); err == nil && d > 0 && d <= 365 {
		days = d
	}
	limit := 20
	if l, err := strconv.Atoi(c.Query("limit")); err == nil && l > 0 && l <= 100 {
		limit = l
	}
	writeSuccess(c, h.analytics.GetIPSummary(c.Request.Context(), days, limit))
}

// AdminGetAnalyticsDiagnostics exposes the analytics module's internal
// health counters (cache size, dirty-day flush queue, active SSE subs,
// in-memory session/media counts).
func (h *Handler) AdminGetAnalyticsDiagnostics(c *gin.Context) {
	if h.analytics == nil {
		writeSuccess(c, map[string]any{"available": false})
		return
	}
	writeSuccess(c, h.analytics.GetDiagnostics())
}

// AdminGetAnalyticsHealth returns a compact health snapshot suitable for
// external uptime monitors and cron pollers. Reports module healthy state,
// flush lag, and live subscriber count. Returns 503 with available=false if
// the analytics module is not initialised so monitors don't get a stale 200.
func (h *Handler) AdminGetAnalyticsHealth(c *gin.Context) {
	if h.analytics == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"available": false,
			"healthy":   false,
		})
		return
	}
	writeSuccess(c, h.analytics.AnalyticsHealth())
}

// AdminGetAnomalies returns daily metrics that spiked or dipped beyond
// the rolling-window threshold. Query: z (default 2.5), window (1-90,
// default 14).
func (h *Handler) AdminGetAnomalies(c *gin.Context) {
	if h.analytics == nil {
		writeSuccess(c, map[string]any{})
		return
	}
	z := 2.5
	if v, err := strconv.ParseFloat(c.Query("z"), 64); err == nil && v > 0 {
		z = v
	}
	window := 14
	if v, err := strconv.Atoi(c.Query("window")); err == nil && v > 0 && v <= 90 {
		window = v
	}
	writeSuccess(c, h.analytics.GetAnomalies(z, window))
}

// AdminGetRetention returns the week-over-week retention grid: rows are
// signup-week cohorts, columns are weeks elapsed since signup, cells are
// % of the cohort still active that week. Query: weeks (1-52, default 12).
func (h *Handler) AdminGetRetention(c *gin.Context) {
	if h.analytics == nil {
		writeSuccess(c, map[string]any{})
		return
	}
	weeks := 12
	if w, err := strconv.Atoi(c.Query("weeks")); err == nil && w > 0 && w <= 52 {
		weeks = w
	}
	writeSuccess(c, h.analytics.GetRetentionGrid(c.Request.Context(), weeks))
}

// AdminGetFunnel returns the view → playback → completion conversion funnel.
// Query: days (1-365, default 30).
func (h *Handler) AdminGetFunnel(c *gin.Context) {
	if h.analytics == nil {
		writeSuccess(c, map[string]any{})
		return
	}
	days := 30
	if d, err := strconv.Atoi(c.Query("days")); err == nil && d > 0 && d <= 365 {
		days = d
	}
	writeSuccess(c, h.analytics.GetFunnel(c.Request.Context(), days))
}

// AdminGetDeviceBreakdown returns event counts by device family AND by
// browser family. Query: days (1-365, default 30).
func (h *Handler) AdminGetDeviceBreakdown(c *gin.Context) {
	if h.analytics == nil {
		writeSuccess(c, map[string]any{})
		return
	}
	days := 30
	if d, err := strconv.Atoi(c.Query("days")); err == nil && d > 0 && d <= 365 {
		days = d
	}
	devices, browsers := h.analytics.GetDeviceBreakdown(c.Request.Context(), days)
	writeSuccess(c, map[string]any{
		"devices":  devices,
		"browsers": browsers,
	})
}

// AdminGetMediaAnalytics returns per-media drill-down: cached stats plus a
// 30-day view + playback timeline. Mirrors the per-user pattern.
// URL param: id (media stable UUID). Query: days (1-365, default 30).
func (h *Handler) AdminGetMediaAnalytics(c *gin.Context) {
	if h.analytics == nil {
		writeSuccess(c, map[string]any{})
		return
	}
	id, ok := RequireParamID(c, "id")
	if !ok {
		return
	}
	days := 30
	if d, err := strconv.Atoi(c.Query("days")); err == nil && d > 0 && d <= 365 {
		days = d
	}
	writeSuccess(c, h.analytics.GetMediaDetail(c.Request.Context(), id, days))
}

// AdminGetCohortMetrics returns DAU / WAU / MAU plus stickiness ratios.
func (h *Handler) AdminGetCohortMetrics(c *gin.Context) {
	if h.analytics == nil {
		writeSuccess(c, map[string]any{})
		return
	}
	writeSuccess(c, h.analytics.GetCohortMetrics(c.Request.Context()))
}

// AdminGetHourlyHeatmap returns a 7×24 grid of event counts in the local
// timezone. Query: days (1-365, default 30).
func (h *Handler) AdminGetHourlyHeatmap(c *gin.Context) {
	if h.analytics == nil {
		writeSuccess(c, []any{})
		return
	}
	days := 30
	if d, err := strconv.Atoi(c.Query("days")); err == nil && d > 0 && d <= 365 {
		days = d
	}
	writeSuccess(c, h.analytics.GetHourlyHeatmap(c.Request.Context(), days))
}

// AdminGetQualityBreakdown groups stream activity by reported quality.
// Query: days (1-365, default 30).
func (h *Handler) AdminGetQualityBreakdown(c *gin.Context) {
	if h.analytics == nil {
		writeSuccess(c, []any{})
		return
	}
	days := 30
	if d, err := strconv.Atoi(c.Query("days")); err == nil && d > 0 && d <= 365 {
		days = d
	}
	writeSuccess(c, h.analytics.GetQualityBreakdown(c.Request.Context(), days))
}

// AdminGetContentGaps returns search queries that mostly returned no
// results, sorted by frequency. Query: days, limit (1-50, default 15),
// min_empty (default 2), min_empty_share (0..1, default 0.5).
func (h *Handler) AdminGetContentGaps(c *gin.Context) {
	if h.analytics == nil {
		writeSuccess(c, []any{})
		return
	}
	limit := 15
	if l, err := strconv.Atoi(c.Query("limit")); err == nil && l > 0 && l <= 50 {
		limit = l
	}
	minEmpty := 2
	if m, err := strconv.Atoi(c.Query("min_empty")); err == nil && m > 0 {
		minEmpty = m
	}
	minShare := 0.5
	if s, err := strconv.ParseFloat(c.Query("min_empty_share"), 64); err == nil && s >= 0 && s <= 1 {
		minShare = s
	}
	since, until := resolveAnalyticsTimeWindow(c)
	writeSuccess(c, h.analytics.GetContentGaps(c.Request.Context(), since, until, minEmpty, minShare, limit))
}

// AdminGetPeriodComparison returns current vs previous totals for one
// metric over a rolling window. Query: metric (DailyStats JSON tag,
// default total_views), days (1-365, default 7).
func (h *Handler) AdminGetPeriodComparison(c *gin.Context) {
	if h.analytics == nil {
		writeSuccess(c, map[string]any{})
		return
	}
	metric := strings.TrimSpace(c.DefaultQuery("metric", "total_views"))
	days := 7
	if d, err := strconv.Atoi(c.Query("days")); err == nil && d > 0 && d <= 365 {
		days = d
	}
	writeSuccess(c, h.analytics.GetPeriodComparison(metric, days))
}

// AdminGetMetricTimeline returns a per-day series for charting.
// Query params: metric (one of the DailyStats JSON tags), days (1-365, default 30).
// Returns gap-filled entries (zeros for missing days) so charts are continuous.
func (h *Handler) AdminGetMetricTimeline(c *gin.Context) {
	if h.analytics == nil {
		writeSuccess(c, []any{})
		return
	}
	metric := strings.TrimSpace(c.DefaultQuery("metric", "total_views"))
	days := 30
	if d, err := strconv.Atoi(c.Query("days")); err == nil && d > 0 && d <= 365 {
		days = d
	}
	writeSuccess(c, h.analytics.GetMetricTimeline(metric, days))
}

// AdminGetUserAnalytics returns aggregated per-user analytics for the user
// whose username is in the URL param (mounted under /users/:username/analytics
// to match the rest of the admin user routes). Restricted to admins.
//
// Response includes total_views, total_watch_time, total_downloads,
// favorites_added/removed, ratings_set, playlists_created/deleted, login
// counts, first_seen, last_seen, unique_media, and most_viewed_media_id —
// everything the per-user admin dashboard needs in one round-trip.
func (h *Handler) AdminGetUserAnalytics(c *gin.Context) {
	if h.analytics == nil {
		writeSuccess(c, map[string]any{"analytics_disabled": true})
		return
	}
	username := strings.TrimSpace(c.Param("username"))
	if username == "" {
		writeError(c, http.StatusBadRequest, "username required")
		return
	}
	user, err := h.auth.GetUser(c.Request.Context(), username)
	if err != nil || user == nil {
		writeError(c, http.StatusNotFound, "user not found")
		return
	}
	limit := 10000
	if l, err := strconv.Atoi(c.Query("limit")); err == nil && l > 0 && l <= 100000 {
		limit = l
	}
	stats := h.analytics.GetUserStats(c.Request.Context(), user.ID, limit)
	writeSuccess(c, stats)
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
