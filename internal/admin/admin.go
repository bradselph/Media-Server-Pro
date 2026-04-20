// Package admin provides administrative functionality and audit logging.
// It handles admin operations, system stats, and audit trail.
package admin

import (
	"bytes"
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
	Details   map[string]any
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
	filename := filepath.Join(m.dataDir, fmt.Sprintf("audit_log_%s_%d.csv", time.Now().Format("20060102_150405"), time.Now().UnixNano()%1_000_000))
	file, err := os.Create(filename)
	if err != nil {
		return "", err
	}

	succeeded := false
	defer func() {
		if closeErr := file.Close(); closeErr != nil {
			m.log.Warn("Failed to close audit log file: %v", closeErr)
		}
		if !succeeded {
			if removeErr := os.Remove(filename); removeErr != nil && !os.IsNotExist(removeErr) {
				m.log.Warn("Failed to remove partial export file %s: %v", filename, removeErr)
			}
		}
	}()

	writer := csv.NewWriter(file)

	header := []string{"Timestamp", "Username", "Action", "Resource", "IP Address", "Success"}
	if writeErr := writer.Write(header); writeErr != nil {
		return "", writeErr
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

// UpdateConfig updates configuration atomically (write to temp file + rename).
func (m *Module) UpdateConfig(updates map[string]any) error {
	if err := m.config.SetValuesBatch(updates); err != nil {
		return fmt.Errorf("failed to update config: %w", err)
	}
	return nil
}

// GetUptimeSecs returns the number of seconds since the module started.
func (m *Module) GetUptimeSecs() int64 {
	return int64(time.Since(m.startTime).Seconds())
}

// readProcMeminfo reads /proc/meminfo and returns (totalBytes, usedBytes).
// Used bytes = MemTotal - MemAvailable (matches how `free` computes "used").
// Returns (0, 0) on any read/parse error so the caller can fall back gracefully.
func readProcMeminfo() (total, used uint64) {
	data, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return 0, 0
	}
	var memTotal, memAvailable uint64
	var foundTotal, foundAvail bool
	for _, line := range bytes.Split(data, []byte{'\n'}) {
		if bytes.HasPrefix(line, []byte("MemTotal:")) {
			fields := bytes.Fields(line)
			if len(fields) >= 2 {
				var kb uint64
				if _, err := fmt.Sscanf(string(fields[1]), "%d", &kb); err == nil {
					memTotal = kb * 1024
					foundTotal = true
				}
			}
		} else if bytes.HasPrefix(line, []byte("MemAvailable:")) {
			fields := bytes.Fields(line)
			if len(fields) >= 2 {
				var kb uint64
				if _, err := fmt.Sscanf(string(fields[1]), "%d", &kb); err == nil {
					memAvailable = kb * 1024
					foundAvail = true
				}
			}
		}
		if foundTotal && foundAvail {
			break
		}
	}
	if !foundTotal {
		return 0, 0
	}
	var memUsed uint64
	if memAvailable <= memTotal {
		memUsed = memTotal - memAvailable
	}
	return memTotal, memUsed
}

// GetSystemInfo returns system information
func (m *Module) GetSystemInfo() SystemInfo {
	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)

	// Use actual physical RAM from /proc/meminfo when available (Linux).
	// Falls back to Go runtime Sys/Alloc if /proc/meminfo is unavailable.
	osMemTotal, osMemUsed := readProcMeminfo()
	memTotal := osMemTotal
	memAlloc := osMemUsed
	if memTotal == 0 {
		memTotal = memStats.Sys
		memAlloc = memStats.Alloc
	}

	return SystemInfo{
		GoVersion:    runtime.Version(),
		NumCPU:       runtime.NumCPU(),
		NumGoroutine: runtime.NumGoroutine(),
		MemAlloc:     memAlloc,
		MemTotal:     memTotal,
		MemSys:       memStats.Sys,
		GCCycles:     memStats.NumGC,
		Uptime:       time.Since(m.startTime).String(),
		StartTime:    m.startTime,
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
type configMapSection func(cfg *config.Config, qualityNames []string) map[string]any

func buildConfigServerMap(cfg *config.Config, _ []string) map[string]any {
	return map[string]any{
		"port":         cfg.Server.Port,
		"host":         cfg.Server.Host,
		"enable_https": cfg.Server.EnableHTTPS,
		"cert_file":    cfg.Server.CertFile,
		"key_file":     cfg.Server.KeyFile,
	}
}

func buildConfigFeaturesMap(cfg *config.Config, _ []string) map[string]any {
	return map[string]any{
		"enable_thumbnails":         cfg.Features.EnableThumbnails,
		"enable_hls":                cfg.Features.EnableHLS,
		"enable_analytics":          cfg.Features.EnableAnalytics,
		"enable_uploads":            cfg.Features.EnableUploads,
		"enable_huggingface":        cfg.Features.EnableHuggingFace,
		"enable_playlists":          cfg.Features.EnablePlaylists,
		"enable_suggestions":        cfg.Features.EnableSuggestions,
		"enable_auto_discovery":     cfg.Features.EnableAutoDiscovery,
		"enable_mature_scanner":     cfg.Features.EnableMatureScanner,
		"enable_remote_media":       cfg.Features.EnableRemoteMedia,
		"enable_receiver":           cfg.Features.EnableReceiver,
		"enable_extractor":          cfg.Features.EnableExtractor,
		"enable_crawler":            cfg.Features.EnableCrawler,
		"enable_duplicate_detection": cfg.Features.EnableDuplicateDetection,
		"enable_downloader":         cfg.Features.EnableDownloader,
		"enable_claude":             cfg.Features.EnableClaude,
	}
}

func buildConfigSecurityMap(cfg *config.Config, _ []string) map[string]any {
	return map[string]any{
		"rate_limit_enabled":  cfg.Security.RateLimitEnabled,
		"rate_limit_requests": cfg.Security.RateLimitRequests,
		"rate_limit_window":   cfg.Security.RateLimitWindow,
		"burst_limit":         cfg.Security.BurstLimit,
		"auth_rate_limit":     cfg.Security.AuthRateLimit,
		"auth_burst_limit":    cfg.Security.AuthBurstLimit,
		"violations_for_ban":  cfg.Security.ViolationsForBan,
		"enable_ip_whitelist": cfg.Security.EnableIPWhitelist,
		"enable_ip_blacklist": cfg.Security.EnableIPBlacklist,
		"csp_enabled":         cfg.Security.CSPEnabled,
		"hsts_enabled":        cfg.Security.HSTSEnabled,
		"hsts_max_age":        cfg.Security.HSTSMaxAge,
		"cors_enabled":        cfg.Security.CORSEnabled,
		"max_file_size_mb":    cfg.Security.MaxFileSizeMB,
	}
}

func buildConfigHLSMap(cfg *config.Config, _ []string) map[string]any {
	profiles := make([]map[string]any, 0, len(cfg.HLS.QualityProfiles))
	for _, qp := range cfg.HLS.QualityProfiles {
		profiles = append(profiles, map[string]any{
			"name":          qp.Name,
			"width":         qp.Width,
			"height":        qp.Height,
			"bitrate":       qp.Bitrate,
			"audio_bitrate": qp.AudioBitrate,
			"enabled":       qp.Enabled,
		})
	}
	return map[string]any{
		"enabled":                    cfg.HLS.Enabled,
		"auto_generate":              cfg.HLS.AutoGenerate,
		"concurrent_limit":           cfg.HLS.ConcurrentLimit,
		"segment_duration":           cfg.HLS.SegmentDuration,
		"cleanup_enabled":            cfg.HLS.CleanupEnabled,
		"retention_minutes":          cfg.HLS.RetentionMinutes,
		"lazy_transcode":             cfg.HLS.LazyTranscode,
		"cdn_base_url":               cfg.HLS.CDNBaseURL,
		"pre_generate_interval_hours": cfg.HLS.PreGenerateIntervalHours,
		"quality_profiles":           profiles,
	}
}

func buildConfigAdminMap(cfg *config.Config, _ []string) map[string]any {
	return map[string]any{
		"max_query_rows": cfg.Admin.MaxQueryRows,
	}
}

func buildConfigThumbnailsMap(cfg *config.Config, _ []string) map[string]any {
	return map[string]any{
		"auto_generate":      cfg.Thumbnails.AutoGenerate,
		"width":              cfg.Thumbnails.Width,
		"height":             cfg.Thumbnails.Height,
		"quality":            cfg.Thumbnails.Quality,
		"video_interval":     cfg.Thumbnails.VideoInterval,
		"preview_count":      cfg.Thumbnails.PreviewCount,
		"generate_on_access": cfg.Thumbnails.GenerateOnAccess,
		"worker_count":       cfg.Thumbnails.WorkerCount,
	}
}

func buildConfigAnalyticsMap(cfg *config.Config, _ []string) map[string]any {
	return map[string]any{
		"enabled":        cfg.Analytics.Enabled,
		"track_playback": cfg.Analytics.TrackPlayback,
		"track_views":    cfg.Analytics.TrackViews,
		"retention_days": cfg.Analytics.RetentionDays,
	}
}

func buildConfigMatureScannerMap(cfg *config.Config, _ []string) map[string]any {
	return map[string]any{
		"enabled":                     cfg.MatureScanner.Enabled,
		"auto_flag":                   cfg.MatureScanner.AutoFlag,
		"high_confidence_threshold":   cfg.MatureScanner.HighConfidenceThreshold,
		"medium_confidence_threshold": cfg.MatureScanner.MediumConfidenceThreshold,
		"require_review":              cfg.MatureScanner.RequireReview,
	}
}

func buildConfigHuggingFaceMap(cfg *config.Config, _ []string) map[string]any {
	return map[string]any{
		"enabled":        cfg.HuggingFace.Enabled,
		"api_key_set":    cfg.HuggingFace.APIKey != "",
		"model":          cfg.HuggingFace.Model,
		"endpoint_url":   cfg.HuggingFace.EndpointURL,
		"max_frames":     cfg.HuggingFace.MaxFrames,
		"timeout_secs":   cfg.HuggingFace.TimeoutSecs,
		"rate_limit":     cfg.HuggingFace.RateLimit,
		"max_concurrent": cfg.HuggingFace.MaxConcurrent,
	}
}

func buildConfigDatabaseMap(cfg *config.Config, _ []string) map[string]any {
	return map[string]any{
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

func buildConfigStreamingMap(cfg *config.Config, _ []string) map[string]any {
	return map[string]any{
		"require_auth":        cfg.Streaming.RequireAuth,
		"mobile_optimization": cfg.Streaming.MobileOptimization,
		"unauth_stream_limit": cfg.Streaming.UnauthStreamLimit,
		"keep_alive_enabled":  cfg.Streaming.KeepAliveEnabled,
		"adaptive":            cfg.Streaming.Adaptive,
	}
}

func buildConfigDownloadMap(cfg *config.Config, _ []string) map[string]any {
	return map[string]any{
		"enabled":       cfg.Download.Enabled,
		"require_auth":  cfg.Download.RequireAuth,
		"chunk_size_kb": cfg.Download.ChunkSizeKB,
	}
}

func buildConfigLoggingMap(cfg *config.Config, _ []string) map[string]any {
	return map[string]any{
		"level":         cfg.Logging.Level,
		"file_enabled":  cfg.Logging.FileEnabled,
		"file_rotation": cfg.Logging.FileRotation,
		"max_backups":   cfg.Logging.MaxBackups,
	}
}

func buildConfigAgeGateMap(cfg *config.Config, _ []string) map[string]any {
	return map[string]any{
		"enabled":        cfg.AgeGate.Enabled,
		"cookie_name":    cfg.AgeGate.CookieName,
		"cookie_max_age": cfg.AgeGate.CookieMaxAge,
	}
}

func buildConfigUploadsMap(cfg *config.Config, _ []string) map[string]any {
	return map[string]any{
		"max_file_size":   cfg.Uploads.MaxFileSize,
		"allowed_types":   cfg.Uploads.AllowedExtensions,
		"scan_for_mature": cfg.Uploads.ScanForMature,
		"require_auth":    cfg.Uploads.RequireAuth,
	}
}

func buildConfigUIMap(cfg *config.Config, _ []string) map[string]any {
	return map[string]any{
		"items_per_page":        cfg.UI.ItemsPerPage,
		"mobile_items_per_page": cfg.UI.MobileItemsPerPage,
		"mobile_grid_columns":   cfg.UI.MobileGridColumns,
	}
}

func buildConfigDownloaderMap(cfg *config.Config, _ []string) map[string]any {
	return map[string]any{
		"enabled":       cfg.Downloader.Enabled,
		"url":           cfg.Downloader.URL,
		"downloads_dir": cfg.Downloader.DownloadsDir,
		"import_dir":    cfg.Downloader.ImportDir,
	}
}

func buildConfigStorageMap(cfg *config.Config, _ []string) map[string]any {
	return map[string]any{
		"backend": cfg.Storage.Backend,
		"s3": map[string]any{
			"endpoint":       cfg.Storage.S3.Endpoint,
			"region":         cfg.Storage.S3.Region,
			"bucket":         cfg.Storage.S3.Bucket,
			"use_path_style": cfg.Storage.S3.UsePathStyle,
			"access_key_set": cfg.Storage.S3.AccessKeyID != "",
			"secret_key_set": cfg.Storage.S3.SecretAccessKey != "",
		},
	}
}

func buildConfigBackupMap(cfg *config.Config, _ []string) map[string]any {
	return map[string]any{
		"retention_count": cfg.Backup.RetentionCount,
	}
}

func buildConfigUpdaterMap(cfg *config.Config, _ []string) map[string]any {
	return map[string]any{
		"update_method": cfg.Updater.UpdateMethod,
		"branch":        cfg.Updater.Branch,
	}
}

func buildConfigRemoteMediaMap(cfg *config.Config, _ []string) map[string]any {
	return map[string]any{
		"enabled":       cfg.RemoteMedia.Enabled,
		"cache_enabled": cfg.RemoteMedia.CacheEnabled,
	}
}

func buildConfigCrawlerMap(cfg *config.Config, _ []string) map[string]any {
	return map[string]any{
		"enabled":         cfg.Crawler.Enabled,
		"browser_enabled": cfg.Crawler.BrowserEnabled,
		"max_pages":       cfg.Crawler.MaxPages,
	}
}

func buildConfigExtractorMap(cfg *config.Config, _ []string) map[string]any {
	return map[string]any{
		"enabled":   cfg.Extractor.Enabled,
		"max_items": cfg.Extractor.MaxItems,
	}
}

func buildConfigClaudeMap(cfg *config.Config, _ []string) map[string]any {
	c := cfg.Claude
	return map[string]any{
		"enabled":                    c.Enabled,
		"binary_path":                c.BinaryPath,
		"workdir":                    c.Workdir,
		"model":                      c.Model,
		"mode":                       c.Mode,
		"max_tokens":                 c.MaxTokens,
		"require_confirm_for_writes": c.RequireConfirmForWrites,
		"max_tool_calls_per_turn":    c.MaxToolCallsPerTurn,
		"rate_limit_per_minute":      c.RateLimitPerMinute,
		"kill_switch":                c.KillSwitch,
		"history_retention_days":     c.HistoryRetentionDays,
	}
}

// GetConfigMap returns config as a map for JSON serialization
func (m *Module) GetConfigMap() map[string]any {
	cfg := m.config.Get()
	sections := []struct {
		key string
		fn  configMapSection
	}{
		{"server", buildConfigServerMap},
		{"admin", buildConfigAdminMap},
		{"features", buildConfigFeaturesMap},
		{"security", buildConfigSecurityMap},
		{"hls", buildConfigHLSMap},
		{"thumbnails", buildConfigThumbnailsMap},
		{"analytics", buildConfigAnalyticsMap},
		{"mature_scanner", buildConfigMatureScannerMap},
		{"huggingface", buildConfigHuggingFaceMap},
		{"database", buildConfigDatabaseMap},
		{"streaming", buildConfigStreamingMap},
		{"download", buildConfigDownloadMap},
		{"logging", buildConfigLoggingMap},
		{"age_gate", buildConfigAgeGateMap},
		{"uploads", buildConfigUploadsMap},
		{"ui", buildConfigUIMap},
		{"downloader", buildConfigDownloaderMap},
		{"storage", buildConfigStorageMap},
		{"backup", buildConfigBackupMap},
		{"updater", buildConfigUpdaterMap},
		{"remote_media", buildConfigRemoteMediaMap},
		{"crawler", buildConfigCrawlerMap},
		{"extractor", buildConfigExtractorMap},
		{"claude", buildConfigClaudeMap},
	}
	out := make(map[string]any, len(sections)+1)
	out["directories"] = map[string]any{"configured": true}
	for _, s := range sections {
		out[s.key] = s.fn(cfg, nil)
	}
	return out
}
