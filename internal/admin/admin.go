// Package admin provides administrative functionality and audit logging.
// It handles admin operations, system stats, and audit trail.
package admin

import (
	"context"
	"encoding/csv"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	"github.com/google/uuid"

	"media-server-pro/internal/config"
	"media-server-pro/internal/database"
	"media-server-pro/internal/logger"
	"media-server-pro/internal/repositories"
	"media-server-pro/internal/repositories/mysql"
	"media-server-pro/pkg/helpers"
	"media-server-pro/pkg/models"
)

// exportAuditLogMaxRows caps how many audit log rows are loaded for CSV export to avoid OOM.
const exportAuditLogMaxRows = 100_000

// Module implements admin functionality
type Module struct {
	config    *config.Manager
	log       *logger.Logger
	dbModule  *database.Module
	auditRepo repositories.AuditLogRepository
	dataDir   string
	healthy   bool
	healthMsg string
	healthMu  sync.RWMutex
	startTime time.Time
}

// NewModule creates a new admin module.
// The database module is stored but repositories are created during Start().
func NewModule(cfg *config.Manager, dbModule *database.Module) (*Module, error) {
	if dbModule == nil {
		return nil, fmt.Errorf("database module is required for admin")
	}

	return &Module{
		config:   cfg,
		log:      logger.New("admin"),
		dbModule: dbModule,
		dataDir:  cfg.Get().Directories.Data,
	}, nil
}

// Name returns the module name
func (m *Module) Name() string {
	return "admin"
}

// Start initializes the admin module
func (m *Module) Start(_ context.Context) error {
	m.log.Info("Starting admin module...")
	m.startTime = time.Now()

	// Initialize MySQL repository (database is now connected)
	if !m.dbModule.IsConnected() {
		return fmt.Errorf("database is not connected")
	}
	m.log.Info("Using MySQL repository for audit log")
	m.auditRepo = mysql.NewAuditLogRepository(m.dbModule.GORM())

	m.healthMu.Lock()
	m.healthy = true
	m.healthMsg = "Running"
	m.healthMu.Unlock()
	m.log.Info("Admin module started")
	return nil
}

// Stop gracefully stops the module
func (m *Module) Stop(_ context.Context) error {
	m.log.Info("Stopping admin module...")
	m.healthMu.Lock()
	m.healthy = false
	m.healthMsg = "Stopped"
	m.healthMu.Unlock()
	return nil
}

// Health returns the module health status
func (m *Module) Health() models.HealthStatus {
	m.healthMu.RLock()
	defer m.healthMu.RUnlock()
	return models.HealthStatus{
		Name:      m.Name(),
		Status:    helpers.StatusString(m.healthy),
		Message:   m.healthMsg,
		CheckedAt: time.Now(),
	}
}

// AuditLogParams holds parameters for logging an administrative action.
type AuditLogParams struct {
	UserID    string
	Username  string
	Action    string
	Resource  string
	Details   map[string]interface{}
	IPAddress string
	Success   bool
}

// LogAction logs an administrative action to the audit log
func (m *Module) LogAction(ctx context.Context, p *AuditLogParams) {
	entry := &models.AuditLogEntry{
		ID:        uuid.New().String(),
		Timestamp: time.Now(),
		UserID:    p.UserID,
		Username:  p.Username,
		Action:    p.Action,
		Resource:  p.Resource,
		Details:   p.Details,
		IPAddress: p.IPAddress,
		Success:   p.Success,
	}

	if err := m.auditRepo.Create(ctx, entry); err != nil {
		m.log.Error("Failed to save audit log entry: %v", err)
	}

	m.log.Info("AUDIT: %s by %s on %s (success: %v)", p.Action, p.Username, p.Resource, p.Success)
}

// CleanupAuditLogOlderThan deletes audit log entries older than the given retention days.
// Use retentionDays <= 0 to skip.
func (m *Module) CleanupAuditLogOlderThan(ctx context.Context, retentionDays int) error {
	if retentionDays <= 0 {
		return nil
	}
	before := time.Now().AddDate(0, 0, -retentionDays).Format(time.RFC3339)
	return m.auditRepo.DeleteOlderThan(ctx, before)
}

// GetAuditLog returns audit log entries, optionally filtered by userID (empty string = all users).
// Both limit and offset are applied so pagination works when filtering by user.
func (m *Module) GetAuditLog(ctx context.Context, limit, offset int, userID string) []models.AuditLogEntry {
	entries, err := m.auditRepo.List(ctx, repositories.AuditLogFilter{
		UserID: userID,
		Limit:  limit,
		Offset: offset,
	})
	if err != nil {
		if userID != "" {
			m.log.Error("Failed to retrieve audit log for user %s: %v", userID, err)
		} else {
			m.log.Error("Failed to retrieve audit log: %v", err)
		}
		return []models.AuditLogEntry{}
	}

	// Convert pointers to values
	result := make([]models.AuditLogEntry, len(entries))
	for i, entry := range entries {
		result[i] = *entry
	}
	return result
}

// ExportAuditLog exports audit log to CSV. The caller (handler) should remove the file after sending the response.
func (m *Module) ExportAuditLog(ctx context.Context) (string, error) {
	filename := filepath.Join(m.dataDir, fmt.Sprintf("audit_log_%s.csv", time.Now().Format("20060102_150405")))
	file, err := os.Create(filename)
	if err != nil {
		return "", err
	}

	succeeded := false
	defer func() {
		if err := file.Close(); err != nil {
			m.log.Warn("Failed to close audit log file: %v", err)
		}
		if !succeeded {
			if removeErr := os.Remove(filename); removeErr != nil && !os.IsNotExist(removeErr) {
				m.log.Warn("Failed to remove partial export file %s: %v", filename, removeErr)
			}
		}
	}()

	writer := csv.NewWriter(file)

	header := []string{"Timestamp", "Username", "Action", "Resource", "IP Address", "Success"}
	if err := writer.Write(header); err != nil {
		return "", err
	}

	entries, err := m.auditRepo.List(ctx, repositories.AuditLogFilter{
		Limit: exportAuditLogMaxRows,
	})
	if err != nil {
		return "", fmt.Errorf("failed to retrieve audit log: %w", err)
	}

	for _, entry := range entries {
		row := []string{
			entry.Timestamp.Format(time.RFC3339),
			entry.Username,
			entry.Action,
			entry.Resource,
			entry.IPAddress,
			fmt.Sprintf("%v", entry.Success),
		}
		if err := writer.Write(row); err != nil {
			return "", err
		}
	}

	writer.Flush()
	if err := writer.Error(); err != nil {
		return "", fmt.Errorf("failed to flush CSV writer: %w", err)
	}

	succeeded = true
	m.log.Info("Exported audit log to %s", filename)
	return filename, nil
}

// GetServerStats returns process-level stats (uptime, memory).
// Per-module stats (video counts, HLS jobs, disk usage, etc.) are aggregated
// directly in the AdminGetStats handler from individual module GetStats() calls.
func (m *Module) GetServerStats() models.ServerStats {
	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)

	return models.ServerStats{
		Uptime:      time.Since(m.startTime),
		MemoryUsage: memStats.Alloc,
	}
}

// GetConfig returns current configuration (read-only view)
func (m *Module) GetConfig() *config.Config {
	return m.config.Get()
}

// UpdateConfig updates configuration (each SetValue persists immediately; no atomic rollback on partial failure).
func (m *Module) UpdateConfig(updates map[string]interface{}) error {
	for path, value := range updates {
		if err := m.config.SetValue(path, value); err != nil {
			return fmt.Errorf("failed to update %s: %w", path, err)
		}
	}
	return nil
}

// GetUptimeSecs returns the number of seconds since the module started.
func (m *Module) GetUptimeSecs() int64 {
	return int64(time.Since(m.startTime).Seconds())
}

// GetSystemInfo returns system information
func (m *Module) GetSystemInfo() SystemInfo {
	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)

	return SystemInfo{
		GoVersion:    runtime.Version(),
		NumCPU:       runtime.NumCPU(),
		NumGoroutine: runtime.NumGoroutine(),
		MemAlloc:     memStats.Alloc,
		// MemTotal uses Sys (total memory obtained from OS) — approximates RSS and is stable.
		// TotalAlloc was previously used but it is cumulative-only and would make the memory
		// usage bar show nonsensical low percentages after even brief operation.
		MemTotal:  memStats.Sys,
		MemSys:    memStats.Sys,
		GCCycles:  memStats.NumGC,
		Uptime:    time.Since(m.startTime).String(),
		StartTime: m.startTime,
	}
}

// SystemInfo holds system information
type SystemInfo struct {
	GoVersion    string    `json:"go_version"`
	NumCPU       int       `json:"num_cpu"`
	NumGoroutine int       `json:"num_goroutine"`
	MemAlloc     uint64    `json:"mem_alloc"`
	MemTotal     uint64    `json:"mem_total"`
	MemSys       uint64    `json:"mem_sys"`
	GCCycles     uint32    `json:"gc_cycles"`
	Uptime       string    `json:"uptime"`
	StartTime    time.Time `json:"start_time"`
}

// configMapSection builds one top-level section of the admin config map.
type configMapSection func(cfg *config.Config, qualityNames []string) map[string]interface{}

func buildConfigServerMap(cfg *config.Config, _ []string) map[string]interface{} {
	return map[string]interface{}{
		"port":         cfg.Server.Port,
		"host":         cfg.Server.Host,
		"enable_https": cfg.Server.EnableHTTPS,
	}
}

func buildConfigFeaturesMap(cfg *config.Config, _ []string) map[string]interface{} {
	return map[string]interface{}{
		"enable_thumbnails":  cfg.Features.EnableThumbnails,
		"enable_hls":         cfg.Features.EnableHLS,
		"enable_analytics":   cfg.Features.EnableAnalytics,
		"enable_uploads":     cfg.Features.EnableUploads,
		"enable_huggingface": cfg.Features.EnableHuggingFace,
	}
}

func buildConfigSecurityMap(cfg *config.Config, _ []string) map[string]interface{} {
	return map[string]interface{}{
		"rate_limit_enabled":  cfg.Security.RateLimitEnabled,
		"rate_limit_requests": cfg.Security.RateLimitRequests,
		"enable_ip_whitelist": cfg.Security.EnableIPWhitelist,
		"enable_ip_blacklist": cfg.Security.EnableIPBlacklist,
	}
}

func buildConfigHLSMap(cfg *config.Config, qualityNames []string) map[string]interface{} {
	return map[string]interface{}{
		"enabled":          cfg.HLS.Enabled,
		"auto_generate":    cfg.HLS.AutoGenerate,
		"concurrent_limit": cfg.HLS.ConcurrentLimit,
		"segment_duration": cfg.HLS.SegmentDuration,
		"quality_profiles": qualityNames,
	}
}

func buildConfigThumbnailsMap(cfg *config.Config, _ []string) map[string]interface{} {
	return map[string]interface{}{
		"auto_generate":      cfg.Thumbnails.AutoGenerate,
		"width":              cfg.Thumbnails.Width,
		"height":             cfg.Thumbnails.Height,
		"quality":            cfg.Thumbnails.Quality,
		"video_interval":     cfg.Thumbnails.VideoInterval,
		"preview_count":      cfg.Thumbnails.PreviewCount,
		"generate_on_access": cfg.Thumbnails.GenerateOnAccess,
	}
}

func buildConfigAnalyticsMap(cfg *config.Config, _ []string) map[string]interface{} {
	return map[string]interface{}{
		"enabled":        cfg.Features.EnableAnalytics,
		"track_playback": cfg.Analytics.TrackPlayback,
		"track_views":    cfg.Analytics.TrackViews,
	}
}

func buildConfigMatureScannerMap(cfg *config.Config, _ []string) map[string]interface{} {
	return map[string]interface{}{
		"enabled":                     cfg.MatureScanner.Enabled,
		"auto_flag":                   cfg.MatureScanner.AutoFlag,
		"high_confidence_threshold":   cfg.MatureScanner.HighConfidenceThreshold,
		"medium_confidence_threshold": cfg.MatureScanner.MediumConfidenceThreshold,
		"require_review":              cfg.MatureScanner.RequireReview,
	}
}

func buildConfigHuggingFaceMap(cfg *config.Config, _ []string) map[string]interface{} {
	return map[string]interface{}{
		"enabled":        cfg.HuggingFace.Enabled,
		"api_key_set":    len(cfg.HuggingFace.APIKey) > 0,
		"model":          cfg.HuggingFace.Model,
		"endpoint_url":   cfg.HuggingFace.EndpointURL,
		"max_frames":     cfg.HuggingFace.MaxFrames,
		"timeout_secs":   cfg.HuggingFace.TimeoutSecs,
		"rate_limit":     cfg.HuggingFace.RateLimit,
		"max_concurrent": cfg.HuggingFace.MaxConcurrent,
	}
}

func buildConfigDatabaseMap(cfg *config.Config, _ []string) map[string]interface{} {
	return map[string]interface{}{
		"enabled":           cfg.Database.Enabled,
		"host":              cfg.Database.Host,
		"port":              cfg.Database.Port,
		"name":              cfg.Database.Name,
		"username":          cfg.Database.Username,
		"max_open_conns":    cfg.Database.MaxOpenConns,
		"max_idle_conns":    cfg.Database.MaxIdleConns,
		"conn_max_lifetime": cfg.Database.ConnMaxLifetime,
		"timeout":           cfg.Database.Timeout,
		"max_retries":       cfg.Database.MaxRetries,
		"retry_interval":    cfg.Database.RetryInterval,
	}
}

// GetConfigMap returns config as a map for JSON serialization
func (m *Module) GetConfigMap() map[string]interface{} {
	cfg := m.config.Get()
	qualityNames := make([]string, 0, len(cfg.HLS.QualityProfiles))
	for _, qp := range cfg.HLS.QualityProfiles {
		qualityNames = append(qualityNames, qp.Name)
	}
	sections := []struct {
		key string
		fn  configMapSection
	}{
		{"server", buildConfigServerMap},
		{"features", buildConfigFeaturesMap},
		{"security", buildConfigSecurityMap},
		{"hls", buildConfigHLSMap},
		{"thumbnails", buildConfigThumbnailsMap},
		{"analytics", buildConfigAnalyticsMap},
		{"mature_scanner", buildConfigMatureScannerMap},
		{"huggingface", buildConfigHuggingFaceMap},
		{"database", buildConfigDatabaseMap},
	}
	out := make(map[string]interface{}, len(sections)+1)
	out["directories"] = map[string]interface{}{"configured": true}
	for _, s := range sections {
		out[s.key] = s.fn(cfg, qualityNames)
	}
	return out
}
