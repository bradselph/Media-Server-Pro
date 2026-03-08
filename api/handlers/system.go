package handlers

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"media-server-pro/pkg/models"
)

// serverStartTime records when the server started, for uptime metrics.
var serverStartTime = time.Now()

// GetHealth returns server health status for uptime monitors, nginx health checks, and the
// systemd healthcheck script. Returns 200 when healthy, 503 when any critical module is
// degraded or unhealthy. This endpoint is intentionally unauthenticated.
func (h *Handler) GetHealth(c *gin.Context) {
	type moduleEntry struct {
		name   string
		health func() models.HealthStatus
	}
	critical := []moduleEntry{
		{"database", h.database.Health},
		{"auth", h.auth.Health},
		{"media", h.media.Health},
		{"streaming", h.streaming.Health},
		{"security", h.security.Health},
		{"tasks", h.tasks.Health},
	}

	modules := make(map[string]string, len(critical))
	var problems []string
	for _, m := range critical {
		hs := m.health()
		modules[m.name] = hs.Status
		if hs.Status != models.StatusHealthy {
			problems = append(problems, fmt.Sprintf("%s: %s", m.name, hs.Message))
		}
	}

	// If the media module hasn't finished its initial scan, report as initializing.
	// This causes the deploy.sh health poll to keep waiting until media is ready.
	if !h.media.IsReady() {
		problems = append(problems, "media: initial scan in progress")
	}

	status := "ok"
	httpCode := http.StatusOK
	if len(problems) > 0 {
		status = "degraded"
		httpCode = http.StatusServiceUnavailable
	}

	c.Header("Cache-Control", "no-cache, no-store")

	// Only expose module details and version to authenticated users
	user := getUser(c)
	if user == nil {
		c.JSON(httpCode, map[string]interface{}{
			"status":    status,
			"timestamp": time.Now().Unix(),
		})
		return
	}

	resp := map[string]interface{}{
		"status":    status,
		"version":   h.version,
		"timestamp": time.Now().Unix(),
		"modules":   modules,
	}
	if len(problems) > 0 {
		resp["problems"] = problems
	}
	c.JSON(httpCode, resp)
}

// GetMetrics returns Prometheus-style metrics including server info, media
// stats, streaming stats, analytics, system runtime metrics, and module health.
func (h *Handler) GetMetrics(c *gin.Context) {
	var b strings.Builder

	// Server info
	_, _ = fmt.Fprintf(&b, "# HELP media_server_info Server information\n")
	_, _ = fmt.Fprintf(&b, "# TYPE media_server_info gauge\n")
	_, _ = fmt.Fprintf(&b, "media_server_info{version=\"%s\"} 1\n", h.version)

	// Media stats
	mediaStats := h.media.GetStats()
	_, _ = fmt.Fprintf(&b, "# HELP media_total_videos Total number of tracked videos\n")
	_, _ = fmt.Fprintf(&b, "# TYPE media_total_videos gauge\n")
	_, _ = fmt.Fprintf(&b, "media_total_videos %d\n", mediaStats.VideoCount)

	_, _ = fmt.Fprintf(&b, "# HELP media_total_audio Total number of tracked audio files\n")
	_, _ = fmt.Fprintf(&b, "# TYPE media_total_audio gauge\n")
	_, _ = fmt.Fprintf(&b, "media_total_audio %d\n", mediaStats.AudioCount)

	// Streaming stats
	streamStats := h.streaming.GetStats()
	_, _ = fmt.Fprintf(&b, "# HELP media_active_sessions Current active streaming sessions\n")
	_, _ = fmt.Fprintf(&b, "# TYPE media_active_sessions gauge\n")
	_, _ = fmt.Fprintf(&b, "media_active_sessions %d\n", streamStats.ActiveStreams)

	_, _ = fmt.Fprintf(&b, "# HELP media_total_streams_count Total stream count\n")
	_, _ = fmt.Fprintf(&b, "# TYPE media_total_streams_count counter\n")
	_, _ = fmt.Fprintf(&b, "media_total_streams_count %d\n", streamStats.TotalStreams)

	_, _ = fmt.Fprintf(&b, "# HELP media_total_bytes_sent Total bytes sent via streaming\n")
	_, _ = fmt.Fprintf(&b, "# TYPE media_total_bytes_sent counter\n")
	_, _ = fmt.Fprintf(&b, "media_total_bytes_sent %d\n", streamStats.TotalBytesSent)

	// Analytics
	if h.analytics != nil {
		analyticsStats := h.analytics.GetStats()
		_, _ = fmt.Fprintf(&b, "# HELP media_total_views Total view count\n")
		_, _ = fmt.Fprintf(&b, "# TYPE media_total_views counter\n")
		_, _ = fmt.Fprintf(&b, "media_total_views %d\n", analyticsStats.TotalViews)

		_, _ = fmt.Fprintf(&b, "# HELP media_unique_clients Total unique clients\n")
		_, _ = fmt.Fprintf(&b, "# TYPE media_unique_clients gauge\n")
		_, _ = fmt.Fprintf(&b, "media_unique_clients %d\n", analyticsStats.UniqueClients)
	}

	// Security stats
	if h.security != nil {
		secStats := h.security.GetStats()
		_, _ = fmt.Fprintf(&b, "# HELP media_security_blocked_total Total blocked requests\n")
		_, _ = fmt.Fprintf(&b, "# TYPE media_security_blocked_total counter\n")
		_, _ = fmt.Fprintf(&b, "media_security_blocked_total %d\n", secStats.TotalBlocked)

		_, _ = fmt.Fprintf(&b, "# HELP media_security_rate_limited_total Total rate-limited requests\n")
		_, _ = fmt.Fprintf(&b, "# TYPE media_security_rate_limited_total counter\n")
		_, _ = fmt.Fprintf(&b, "media_security_rate_limited_total %d\n", secStats.TotalRateLimited)

		_, _ = fmt.Fprintf(&b, "# HELP media_security_banned_ips Current number of banned IPs\n")
		_, _ = fmt.Fprintf(&b, "# TYPE media_security_banned_ips gauge\n")
		_, _ = fmt.Fprintf(&b, "media_security_banned_ips %d\n", secStats.BannedIPs)
	}

	// Go runtime metrics
	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)

	_, _ = fmt.Fprintf(&b, "# HELP media_go_goroutines Number of active goroutines\n")
	_, _ = fmt.Fprintf(&b, "# TYPE media_go_goroutines gauge\n")
	_, _ = fmt.Fprintf(&b, "media_go_goroutines %d\n", runtime.NumGoroutine())

	_, _ = fmt.Fprintf(&b, "# HELP media_go_memory_alloc_bytes Current heap allocation in bytes\n")
	_, _ = fmt.Fprintf(&b, "# TYPE media_go_memory_alloc_bytes gauge\n")
	_, _ = fmt.Fprintf(&b, "media_go_memory_alloc_bytes %d\n", memStats.Alloc)

	_, _ = fmt.Fprintf(&b, "# HELP media_go_memory_sys_bytes Total memory obtained from the OS\n")
	_, _ = fmt.Fprintf(&b, "# TYPE media_go_memory_sys_bytes gauge\n")
	_, _ = fmt.Fprintf(&b, "media_go_memory_sys_bytes %d\n", memStats.Sys)

	_, _ = fmt.Fprintf(&b, "# HELP media_go_gc_runs_total Total number of GC runs\n")
	_, _ = fmt.Fprintf(&b, "# TYPE media_go_gc_runs_total counter\n")
	_, _ = fmt.Fprintf(&b, "media_go_gc_runs_total %d\n", memStats.NumGC)

	// Server uptime
	_, _ = fmt.Fprintf(&b, "# HELP media_uptime_seconds Server uptime in seconds\n")
	_, _ = fmt.Fprintf(&b, "# TYPE media_uptime_seconds gauge\n")
	_, _ = fmt.Fprintf(&b, "media_uptime_seconds %.0f\n", time.Since(serverStartTime).Seconds())

	// Module health (1 = healthy, 0 = unhealthy)
	type moduleEntry struct {
		name   string
		health func() models.HealthStatus
	}
	modules := []moduleEntry{
		{"database", h.database.Health},
		{"auth", h.auth.Health},
		{"media", h.media.Health},
		{"streaming", h.streaming.Health},
	}
	if h.security != nil {
		modules = append(modules, moduleEntry{"security", h.security.Health})
	}

	_, _ = fmt.Fprintf(&b, "# HELP media_module_healthy Module health status (1=healthy, 0=unhealthy)\n")
	_, _ = fmt.Fprintf(&b, "# TYPE media_module_healthy gauge\n")
	for _, m := range modules {
		hs := m.health()
		val := 0
		if hs.Status == models.StatusHealthy {
			val = 1
		}
		_, _ = fmt.Fprintf(&b, "media_module_healthy{module=\"%s\"} %d\n", m.name, val)
	}

	c.Header(headerContentType, "text/plain; version=0.0.4")
	c.Header("Cache-Control", "no-cache")
	c.Status(http.StatusOK)
	if _, err := c.Writer.Write([]byte(b.String())); err != nil {
		h.log.Error("Failed to write metrics output: %v", err)
	}
}

// GetServerSettings returns public server settings
func (h *Handler) GetServerSettings(c *gin.Context) {
	cfg := h.media.GetConfig()

	settings := map[string]interface{}{
		"thumbnails": map[string]interface{}{
			"enabled":             cfg.Thumbnails.Enabled,
			"autoGenerate":        cfg.Thumbnails.AutoGenerate,
			"width":               cfg.Thumbnails.Width,
			"height":              cfg.Thumbnails.Height,
			"video_preview_count": cfg.Thumbnails.PreviewCount,
		},
		"streaming": map[string]interface{}{
			"mobileOptimization": cfg.Streaming.MobileOptimization,
		},
		"analytics": map[string]interface{}{
			"enabled": cfg.Analytics.Enabled,
		},
		"features": map[string]interface{}{
			"enableThumbnails": cfg.Thumbnails.Enabled,
			"enableHLS":        cfg.HLS.Enabled,
			"enableAnalytics":  cfg.Analytics.Enabled,
		},
		"uploads": map[string]interface{}{
			"enabled":     cfg.Uploads.Enabled,
			"maxFileSize": cfg.Uploads.MaxFileSize,
		},
		"admin": map[string]interface{}{
			"enabled": cfg.Admin.Enabled,
		},
		"ui": map[string]interface{}{
			"items_per_page":        cfg.UI.ItemsPerPage,
			"mobile_items_per_page": cfg.UI.MobileItemsPerPage,
			"mobile_grid_columns":   cfg.UI.MobileGridColumns,
		},
		"age_gate": map[string]interface{}{
			"enabled": cfg.AgeGate.Enabled,
		},
	}

	writeSuccess(c, settings)
}

// GetStorageUsage returns storage usage information for the current user.
func (h *Handler) GetStorageUsage(c *gin.Context) {
	session := getSession(c)

	userType := "standard"
	var storageQuotaGB int64
	username := ""

	if session != nil {
		username = session.Username
		user, err := h.auth.GetUser(c.Request.Context(), username)
		if err == nil && user != nil && user.Type != "" {
			userType = user.Type
		}
	}

	storageQuotaGB = h.getUserStorageQuota(userType)

	var totalSize int64
	if username != "" && h.upload != nil {
		used, err := h.upload.GetUserStorageUsed(username)
		if err != nil {
			h.log.Warn("Error getting user storage for %s: %v", username, err)
		}
		totalSize = used
	} else {
		cfg := h.media.GetConfig()
		uploadsDir := cfg.Directories.Uploads
		const maxFiles = 100000
		if _, err := os.Stat(uploadsDir); err == nil {
			fileCount := 0
			if err := filepath.Walk(uploadsDir, func(path string, info os.FileInfo, err error) error {
				if err == nil && !info.IsDir() {
					totalSize += info.Size()
					fileCount++
					if fileCount >= maxFiles {
						return filepath.SkipAll
					}
				}
				return nil
			}); err != nil && !errors.Is(err, filepath.SkipAll) {
				h.log.Warn("Error calculating storage usage: %v", err)
			}
		}
	}

	usedGB := float64(totalSize) / (1024 * 1024 * 1024)
	// storageQuotaGB == -1 means unlimited (admin accounts); keep as -1 so the
	// frontend can distinguish unlimited from zero.
	var quotaGB float64
	if storageQuotaGB < 0 {
		quotaGB = -1
	} else {
		quotaGB = float64(storageQuotaGB) / (1024 * 1024 * 1024)
	}
	percentage := 0.0
	if quotaGB > 0 {
		percentage = (usedGB / quotaGB) * 100
	}

	storageInfo := map[string]interface{}{
		"used_bytes":       totalSize,
		"used_gb":          usedGB,
		"quota_gb":         quotaGB,
		"percentage":       percentage,
		"user_type":        userType,
		"is_authenticated": username != "",
	}

	writeSuccess(c, storageInfo)
}

// ClearMediaCache clears the media cache and rescans
func (h *Handler) ClearMediaCache(c *gin.Context) {
	if err := h.media.Scan(); err != nil {
		h.log.Error("Failed to clear cache and rescan media: %v", err)
		writeError(c, http.StatusInternalServerError, "Failed to clear cache")
		return
	}

	writeSuccess(c, map[string]string{
		"status":  "success",
		"message": "Cache cleared and media rescanned",
	})
}

// AdminGetDatabaseStatus returns the current database connection status
func (h *Handler) AdminGetDatabaseStatus(c *gin.Context) {
	if h.database == nil {
		writeError(c, http.StatusServiceUnavailable, "Database module not available")
		return
	}

	health := h.database.Health()
	connected := health.Status == models.StatusHealthy

	repositoryType := "JSON"
	if connected {
		repositoryType = "MySQL"
	}

	cfg := h.media.GetConfig()
	status := map[string]interface{}{
		"connected":       connected,
		"app_version":     h.version,
		"repository_type": repositoryType,
		"message":         health.Message,
		"checked_at":      health.CheckedAt,
		"host":            cfg.Database.Host,
		"database":        cfg.Database.Name,
	}

	writeSuccess(c, status)
}

// AdminExecuteQuery executes a SQL query and returns the results
func (h *Handler) AdminExecuteQuery(c *gin.Context) {
	if h.database == nil {
		writeError(c, http.StatusServiceUnavailable, "Database module not available")
		return
	}

	if !h.database.IsConnected() {
		writeError(c, http.StatusServiceUnavailable, "Database not connected")
		return
	}

	var req struct {
		Query string `json:"query"`
	}
	if c.ShouldBindJSON(&req) != nil {
		writeError(c, http.StatusBadRequest, "Invalid request")
		return
	}

	query := strings.TrimSpace(req.Query)
	if query == "" {
		writeError(c, http.StatusBadRequest, "Query cannot be empty")
		return
	}

	session := getSession(c)
	username := "unknown"
	if session != nil {
		username = session.Username
	}

	h.log.Info("Admin %s executing query: %s", username, query)

	isSelect := strings.HasPrefix(strings.ToUpper(query), "SELECT") ||
		strings.HasPrefix(strings.ToUpper(query), "SHOW") ||
		strings.HasPrefix(strings.ToUpper(query), "DESCRIBE") ||
		strings.HasPrefix(strings.ToUpper(query), "EXPLAIN")

	queryTimeout := h.media.GetConfig().Admin.QueryTimeout
	if queryTimeout <= 0 {
		queryTimeout = 30 * time.Second
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), queryTimeout)
	defer cancel()
	db := h.database.DB()

	if !isSelect {
		h.log.Warn("Admin %s attempted disallowed mutating query", username)
		if h.admin != nil {
			h.admin.LogAction(c.Request.Context(), username, username, "execute_query", "database", map[string]interface{}{"query": query}, c.ClientIP(), false)
		}
		writeError(c, http.StatusForbidden, "Only SELECT, SHOW, DESCRIBE, and EXPLAIN queries are permitted")
		return
	}

	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		if h.admin != nil {
			h.admin.LogAction(c.Request.Context(), username, username, "execute_query", "database", map[string]interface{}{"query": query}, c.ClientIP(), false)
		}
		h.log.Error("Query execution failed: %v", err)
		writeError(c, http.StatusBadRequest, "Query execution failed")
		return
	}
	defer func() {
		if err := rows.Close(); err != nil {
			h.log.Warn("Failed to close rows: %v", err)
		}
	}()

	columns, err := rows.Columns()
	if err != nil {
		h.log.Error("Failed to get columns: %v", err)
		writeError(c, http.StatusInternalServerError, "Query execution failed")
		return
	}

	maxRows := h.media.GetConfig().Admin.MaxQueryRows
	if maxRows <= 0 {
		maxRows = 1000
	}
	var results [][]interface{}
	for rows.Next() && len(results) < maxRows {
		values := make([]interface{}, len(columns))
		valuePtrs := make([]interface{}, len(columns))
		for i := range values {
			valuePtrs[i] = &values[i]
		}

		if err := rows.Scan(valuePtrs...); err != nil {
			h.log.Error("Failed to scan row: %v", err)
			writeError(c, http.StatusInternalServerError, "Query execution failed")
			return
		}

		row := make([]interface{}, len(columns))
		for i, val := range values {
			if b, ok := val.([]byte); ok {
				row[i] = string(b)
			} else {
				row[i] = val
			}
		}
		results = append(results, row)
	}

	if err := rows.Err(); err != nil {
		h.log.Error("Error reading rows: %v", err)
		writeError(c, http.StatusInternalServerError, "Query execution failed")
		return
	}

	if h.admin != nil {
		h.admin.LogAction(c.Request.Context(), username, username, "execute_query", "database", map[string]interface{}{"query": query, "rows": len(results)}, c.ClientIP(), true)
	}

	writeSuccess(c, map[string]interface{}{
		"columns":       columns,
		"rows":          results,
		"rows_affected": len(results),
		"truncated":     len(results) >= maxRows,
	})
}
