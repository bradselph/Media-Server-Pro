// Package handlers provides HTTP request handlers for the API.
package handlers

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"media-server-pro/pkg/middleware"
	"media-server-pro/pkg/models"
)

// GetMetrics returns Prometheus-style metrics
func (h *Handler) GetMetrics(w http.ResponseWriter, _ *http.Request) {
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
	_, _ = fmt.Fprintf(&b, "# HELP media_active_sessions Current active sessions\n")
	_, _ = fmt.Fprintf(&b, "# TYPE media_active_sessions gauge\n")
	_, _ = fmt.Fprintf(&b, "media_active_sessions %d\n", streamStats.ActiveStreams)

	_, _ = fmt.Fprintf(&b, "# HELP media_total_streams_count Total stream count\n")
	_, _ = fmt.Fprintf(&b, "# TYPE media_total_streams_count counter\n")
	_, _ = fmt.Fprintf(&b, "media_total_streams_count %d\n", streamStats.TotalStreams)

	_, _ = fmt.Fprintf(&b, "# HELP media_total_bytes_sent Total bytes sent\n")
	_, _ = fmt.Fprintf(&b, "# TYPE media_total_bytes_sent counter\n")
	_, _ = fmt.Fprintf(&b, "media_total_bytes_sent %d\n", streamStats.TotalBytesSent)

	// Analytics stats if available
	if h.analytics != nil {
		analyticsStats := h.analytics.GetStats()
		_, _ = fmt.Fprintf(&b, "# HELP media_total_views Total view count\n")
		_, _ = fmt.Fprintf(&b, "# TYPE media_total_views counter\n")
		_, _ = fmt.Fprintf(&b, "media_total_views %d\n", analyticsStats.TotalViews)

		_, _ = fmt.Fprintf(&b, "# HELP media_unique_clients Total unique clients\n")
		_, _ = fmt.Fprintf(&b, "# TYPE media_unique_clients gauge\n")
		_, _ = fmt.Fprintf(&b, "media_unique_clients %d\n", analyticsStats.UniqueClients)
	}

	w.Header().Set(headerContentType, "text/plain; version=0.0.4")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	if _, err := w.Write([]byte(b.String())); err != nil {
		h.log.Error("Failed to write metrics output: %v", err)
	}
}

// GetHealth returns server health status for uptime monitors, nginx health
// checks, and the systemd healthcheck script. Returns 200 when healthy,
// 503 when any critical module is degraded or unhealthy.
// This endpoint is intentionally unauthenticated.
func (h *Handler) GetHealth(w http.ResponseWriter, _ *http.Request) {
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

	status := "ok"
	httpCode := http.StatusOK
	if len(problems) > 0 {
		status = "degraded"
		httpCode = http.StatusServiceUnavailable
	}

	w.Header().Set("Cache-Control", "no-cache, no-store")
	resp := map[string]interface{}{
		"status":    status,
		"version":   h.version,
		"timestamp": time.Now().Unix(),
		"modules":   modules,
	}
	if len(problems) > 0 {
		resp["problems"] = problems
	}
	writeJSON(w, httpCode, resp)
}

// GetServerSettings returns public server settings
func (h *Handler) GetServerSettings(w http.ResponseWriter, _ *http.Request) {
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
			"enableThumbnails":   cfg.Thumbnails.Enabled,
			"enableHLS":          cfg.HLS.Enabled,
			"enableAnalytics":    cfg.Analytics.Enabled,
			"analytics_tracking": cfg.Analytics.Enabled,
		},
		"uploads": map[string]interface{}{
			"enabled":     cfg.Uploads.Enabled,
			"maxFileSize": cfg.Uploads.MaxFileSize,
		},
		"admin": map[string]interface{}{
			"enabled": cfg.Admin.Enabled,
		},
		"ui": map[string]interface{}{
			"items_per_page":        48,
			"mobile_items_per_page": 24,
			"mobile_grid_columns":   2,
		},
		"age_gate": map[string]interface{}{
			"enabled": cfg.AgeGate.Enabled,
		},
	}

	writeSuccess(w, settings)
}

// GetStorageUsage returns storage usage information for the current user.
func (h *Handler) GetStorageUsage(w http.ResponseWriter, r *http.Request) {
	session := middleware.GetSession(r)

	userType := "standard"
	var storageQuotaGB int64
	username := ""

	if session != nil {
		username = session.Username
		user, err := h.auth.GetUser(r.Context(), username)
		if err == nil && user != nil && user.Type != "" {
			userType = user.Type
		}
	}

	// Get quota based on user type
	storageQuotaGB = h.getUserStorageQuota(userType)

	// Use per-user storage from upload module when user is authenticated
	// Upload module is non-critical and may be nil, so check before using
	var totalSize int64
	if username != "" && h.upload != nil {
		used, err := h.upload.GetUserStorageUsed(username)
		if err != nil {
			h.log.Warn("Error getting user storage for %s: %v", username, err)
		}
		totalSize = used
	} else {
		// For unauthenticated users, walk the uploads directory with a file count cap
		// to prevent resource exhaustion on very large directories.
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
	percentage := 0.0
	if storageQuotaGB > 0 {
		percentage = (usedGB / float64(storageQuotaGB)) * 100
	}

	storageInfo := map[string]interface{}{
		"used_bytes":       totalSize,
		"used_gb":          usedGB,
		"quota_gb":         storageQuotaGB,
		"percentage":       percentage,
		"user_type":        userType,
		"is_authenticated": username != "",
	}

	writeSuccess(w, storageInfo)
}

func (h *Handler) ClearMediaCache(w http.ResponseWriter, _ *http.Request) {
	// Trigger a rescan
	if err := h.media.Scan(); err != nil {
		h.log.Error("Failed to clear cache and rescan media: %v", err)
		writeError(w, http.StatusInternalServerError, "Failed to clear cache")
		return
	}

	writeSuccess(w, map[string]string{
		"status":  "success",
		"message": "Cache cleared and media rescanned",
	})
}

// AdminGetDatabaseStatus returns the current database connection status
func (h *Handler) AdminGetDatabaseStatus(w http.ResponseWriter, r *http.Request) {
	if h.database == nil {
		writeError(w, http.StatusServiceUnavailable, "Database module not available")
		return
	}

	health := h.database.Health()
	connected := health.Status == models.StatusHealthy

	// Get schema version if connected
	var schemaVersion int
	var repositoryType string
	if connected && h.database.DB() != nil {
		ctx := r.Context()
		err := h.database.DB().QueryRowContext(ctx, "SELECT COALESCE(MAX(version), 0) FROM schema_migrations").Scan(&schemaVersion)
		if err != nil {
			h.log.Warn("Failed to get schema version: %v", err)
		}
		repositoryType = "MySQL"
	} else {
		repositoryType = "JSON"
	}

	cfg := h.media.GetConfig()
	status := map[string]interface{}{
		"connected":       connected,
		"schema_version":  schemaVersion,
		"repository_type": repositoryType,
		"message":         health.Message,
		"checked_at":      health.CheckedAt,
		"host":            cfg.Database.Host,
		"database":        cfg.Database.Name,
	}

	writeSuccess(w, status)
}

// AdminExecuteQuery executes a SQL query and returns the results
func (h *Handler) AdminExecuteQuery(w http.ResponseWriter, r *http.Request) {
	if h.database == nil {
		writeError(w, http.StatusServiceUnavailable, "Database module not available")
		return
	}

	if !h.database.IsConnected() {
		writeError(w, http.StatusServiceUnavailable, "Database not connected")
		return
	}

	var req struct {
		Query string `json:"query"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request")
		return
	}

	query := strings.TrimSpace(req.Query)
	if query == "" {
		writeError(w, http.StatusBadRequest, "Query cannot be empty")
		return
	}

	session := middleware.GetSession(r)
	username := "unknown"
	if session != nil {
		username = session.Username
	}

	// Log the query attempt
	h.log.Info("Admin %s executing query: %s", username, query)

	// Check if this is a SELECT query (read-only)
	isSelect := strings.HasPrefix(strings.ToUpper(query), "SELECT") ||
		strings.HasPrefix(strings.ToUpper(query), "SHOW") ||
		strings.HasPrefix(strings.ToUpper(query), "DESCRIBE") ||
		strings.HasPrefix(strings.ToUpper(query), "EXPLAIN")

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	db := h.database.DB()

	if isSelect {
		// Execute SELECT query and return results
		rows, err := db.QueryContext(ctx, query)
		if err != nil {
			if h.admin != nil {
				h.admin.LogAction(r.Context(), username, username, "execute_query", "database", map[string]interface{}{"query": query}, middleware.GetClientIP(r), false)
			}
			h.log.Error("Query execution failed: %v", err)
			writeError(w, http.StatusBadRequest, "Query execution failed")
			return
		}
		defer func() {
			if err := rows.Close(); err != nil {
				h.log.Warn("Failed to close rows: %v", err)
			}
		}()

		// Get column names
		columns, err := rows.Columns()
		if err != nil {
			h.log.Error("Failed to get columns: %v", err)
			writeError(w, http.StatusInternalServerError, "Query execution failed")
			return
		}

		// Read all rows as 2D array for consistent JSON representation
		const maxRows = 1000
		var results [][]interface{}
		for rows.Next() && len(results) < maxRows {
			// Create a slice of interface{} to hold each column's value
			values := make([]interface{}, len(columns))
			valuePtrs := make([]interface{}, len(columns))
			for i := range values {
				valuePtrs[i] = &values[i]
			}

			if err := rows.Scan(valuePtrs...); err != nil {
				h.log.Error("Failed to scan row: %v", err)
				writeError(w, http.StatusInternalServerError, "Query execution failed")
				return
			}

			// Build row as a flat slice, converting []byte to string
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
			writeError(w, http.StatusInternalServerError, "Query execution failed")
			return
		}

		// Log success
		if h.admin != nil {
			h.admin.LogAction(r.Context(), username, username, "execute_query", "database", map[string]interface{}{"query": query, "rows": len(results)}, middleware.GetClientIP(r), true)
		}

		writeSuccess(w, map[string]interface{}{
			"columns":       columns,
			"rows":          results,
			"rows_affected": len(results),
			"truncated":     len(results) >= maxRows,
		})
	} else {
		// Execute write query (INSERT, UPDATE, DELETE, etc.)
		result, err := db.ExecContext(ctx, query)
		if err != nil {
			if h.admin != nil {
				h.admin.LogAction(r.Context(), username, username, "execute_query", "database", map[string]interface{}{"query": query}, middleware.GetClientIP(r), false)
			}
			h.log.Error("Query execution failed: %v", err)
			writeError(w, http.StatusBadRequest, "Query execution failed")
			return
		}

		rowsAffected, _ := result.RowsAffected()

		// Log success
		if h.admin != nil {
			h.admin.LogAction(r.Context(), username, username, "execute_query", "database", map[string]interface{}{"query": query, "affected": rowsAffected}, middleware.GetClientIP(r), true)
		}

		writeSuccess(w, map[string]interface{}{
			"rows_affected": rowsAffected,
			"message":       fmt.Sprintf("Query executed successfully. Rows affected: %d", rowsAffected),
		})
	}
}
