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
		config:    cfg,
		log:       logger.New("admin"),
		dbModule:  dbModule,
		dataDir:   cfg.Get().Directories.Data,
		startTime: time.Now(),
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

// AuditLogResponse holds paginated audit log results with total count.
type AuditLogResponse struct {
	Items      []models.AuditLogEntry `json:"items"`
	Total      int64                  `json:"total"`
	TotalPages int                    `json:"total_pages"`
}

// GetAuditLog returns audit log entries with pagination, optionally filtered by userID (empty string = all users).
// Returns the page of entries and the total count across all entries matching the filter.
func (m *Module) GetAuditLog(ctx context.Context, limit, offset int, userID string) AuditLogResponse {
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
		return AuditLogResponse{Items: []models.AuditLogEntry{}, Total: 0, TotalPages: 0}
	}

	// Convert pointers to values
	items := make([]models.AuditLogEntry, len(entries))
	for i, entry := range entries {
		items[i] = *entry
	}

	// Query total count for the filter (may differ from len(entries) if fewer than limit returned)
	total, err := m.auditRepo.Count(ctx, repositories.AuditLogFilter{UserID: userID})
	if err != nil {
		m.log.Warn("Failed to count audit log entries: %v", err)
		total = int64(len(items)) // fallback: use returned items count
	}

	totalPages := 0
	if limit > 0 && total > 0 {
		totalPages = int((total + int64(limit) - 1) / int64(limit))
	}

	return AuditLogResponse{
		Items:      items,
		Total:      total,
		TotalPages: totalPages,
	}
}

// ExportAuditLog exports audit log to CSV. The caller (handler) should remove the file after sending the response.
func (m *Module) ExportAuditLog(ctx context.Context) (filename string, retErr error) {
	filename = filepath.Join(m.dataDir, fmt.Sprintf("audit_log_%s_%d.csv", time.Now().Format("20060102_150405"), time.Now().UnixNano()%1_000_000))
	file, err := os.Create(filename)
	if err != nil {
		return "", err
	}

	succeeded := false
	defer func() {
		if closeErr := file.Close(); closeErr != nil {
			m.log.Warn("Failed to close audit log file: %v", closeErr)
			// On networked storage (e.g. IONOS HiDrive) the final write is committed
			// during Close(), so a close failure can mean the CSV is incomplete.
			// Surface it and drop the partial file rather than serving a corrupt one.
			if retErr == nil {
				retErr = fmt.Errorf("failed to close audit log export: %w", closeErr)
			}
			succeeded = false
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
	for line := range bytes.SplitSeq(data, []byte{'\n'}) {
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
type configMapSection func(cfg *config.Config) map[string]any

func buildConfigServerMap(cfg *config.Config) map[string]any {
	return map[string]any{
		"port":                 cfg.Server.Port,
		"host":                 cfg.Server.Host,
		"enable_https":         cfg.Server.EnableHTTPS,
		"cert_file":            cfg.Server.CertFile,
		"key_file":             cfg.Server.KeyFile,
		"read_header_timeout":  cfg.Server.ReadHeaderTimeout,
		"read_timeout":         cfg.Server.ReadTimeout,
		"write_timeout":        cfg.Server.WriteTimeout,
		"idle_timeout":         cfg.Server.IdleTimeout,
		"max_header_bytes":     cfg.Server.MaxHeaderBytes,
		"shutdown_timeout":     cfg.Server.ShutdownTimeout,
		"memory_limit_percent": cfg.Server.MemoryLimitPercent,
	}
}

func buildConfigFeaturesMap(cfg *config.Config) map[string]any {
	return map[string]any{
		"enable_thumbnails":          cfg.Features.EnableThumbnails,
		"enable_hls":                 cfg.Features.EnableHLS,
		"enable_analytics":           cfg.Features.EnableAnalytics,
		"enable_uploads":             cfg.Features.EnableUploads,
		"enable_huggingface":         cfg.Features.EnableHuggingFace,
		"enable_playlists":           cfg.Features.EnablePlaylists,
		"enable_suggestions":         cfg.Features.EnableSuggestions,
		"enable_auto_discovery":      cfg.Features.EnableAutoDiscovery,
		"enable_mature_scanner":      cfg.Features.EnableMatureScanner,
		"enable_remote_media":        cfg.Features.EnableRemoteMedia,
		"enable_receiver":            cfg.Features.EnableReceiver,
		"enable_extractor":           cfg.Features.EnableExtractor,
		"enable_duplicate_detection": cfg.Features.EnableDuplicateDetection,
		"enable_downloader":          cfg.Features.EnableDownloader,
		"enable_hub":                 cfg.Features.EnableHub,
		"enable_user_auth":           cfg.Features.EnableUserAuth,
		"enable_admin_panel":         cfg.Features.EnableAdminPanel,
	}
}

func buildConfigHubMap(cfg *config.Config) map[string]any {
	return map[string]any{
		"enabled":           cfg.Hub.Enabled,
		"csv_path":          cfg.Hub.CSVPath,
		"source_url":        cfg.Hub.SourceURL,
		"work_dir":          cfg.Hub.WorkDir,
		"auto_import":       cfg.Hub.AutoImport,
		"page_size":         cfg.Hub.PageSize,
		"import_batch_size": cfg.Hub.ImportBatchSize,
	}
}

func buildConfigSecurityMap(cfg *config.Config) map[string]any {
	return map[string]any{
		"rate_limit_enabled":  cfg.Security.RateLimitEnabled,
		"rate_limit_requests": cfg.Security.RateLimitRequests,
		"rate_limit_window":   cfg.Security.RateLimitWindow,
		"burst_limit":         cfg.Security.BurstLimit,
		"burst_window":        cfg.Security.BurstWindow,
		"auth_rate_limit":     cfg.Security.AuthRateLimit,
		"auth_burst_limit":    cfg.Security.AuthBurstLimit,
		"violations_for_ban":  cfg.Security.ViolationsForBan,
		"ban_duration":        cfg.Security.BanDuration,
		"enable_ip_whitelist": cfg.Security.EnableIPWhitelist,
		"enable_ip_blacklist": cfg.Security.EnableIPBlacklist,
		"ip_whitelist":        cfg.Security.IPWhitelist,
		"ip_blacklist":        cfg.Security.IPBlacklist,
		"trusted_proxy_cidrs": cfg.Security.TrustedProxyCIDRs,
		"csp_enabled":         cfg.Security.CSPEnabled,
		"csp_policy":          cfg.Security.CSPPolicy,
		"hsts_enabled":        cfg.Security.HSTSEnabled,
		"hsts_max_age":        cfg.Security.HSTSMaxAge,
		"cors_enabled":        cfg.Security.CORSEnabled,
		"cors_origins":        cfg.Security.CORSOrigins,
		"max_file_size_mb":    cfg.Security.MaxFileSizeMB,
	}
}

func buildConfigHLSMap(cfg *config.Config) map[string]any {
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
		"enabled":                     cfg.HLS.Enabled,
		"auto_generate":               cfg.HLS.AutoGenerate,
		"concurrent_limit":            cfg.HLS.ConcurrentLimit,
		"hardware_accel":              cfg.HLS.HardwareAccel,
		"segment_duration":            cfg.HLS.SegmentDuration,
		"playlist_length":             cfg.HLS.PlaylistLength,
		"cleanup_enabled":             cfg.HLS.CleanupEnabled,
		"cleanup_interval":            cfg.HLS.CleanupInterval,
		"retention_minutes":           cfg.HLS.RetentionMinutes,
		"lazy_transcode":              cfg.HLS.LazyTranscode,
		"cdn_base_url":                cfg.HLS.CDNBaseURL,
		"pre_generate_interval_hours": cfg.HLS.PreGenerateIntervalHours,
		"quality_profiles":            profiles,
		"max_consecutive_failures":    cfg.HLS.MaxConsecutiveFailures,
		"probe_timeout":               cfg.HLS.ProbeTimeout,
		"stale_lock_threshold":        cfg.HLS.StaleLockThreshold,
	}
}

func buildConfigAdminMap(cfg *config.Config) map[string]any {
	return map[string]any{
		"max_query_rows":           cfg.Admin.MaxQueryRows,
		"audit_log_retention_days": cfg.Admin.AuditLogRetentionDays,
		"session_timeout":          cfg.Admin.SessionTimeout,
	}
}

func buildConfigThumbnailsMap(cfg *config.Config) map[string]any {
	return map[string]any{
		"enabled":                   cfg.Thumbnails.Enabled,
		"auto_generate":             cfg.Thumbnails.AutoGenerate,
		"width":                     cfg.Thumbnails.Width,
		"height":                    cfg.Thumbnails.Height,
		"quality":                   cfg.Thumbnails.Quality,
		"video_interval":            cfg.Thumbnails.VideoInterval,
		"preview_count":             cfg.Thumbnails.PreviewCount,
		"generate_on_access":        cfg.Thumbnails.GenerateOnAccess,
		"queue_size":                cfg.Thumbnails.QueueSize,
		"worker_count":              cfg.Thumbnails.WorkerCount,
		"inflight_eviction_timeout": cfg.Thumbnails.InFlightEvictionTimeout,
		"inflight_scan_interval":    cfg.Thumbnails.InFlightScanInterval,
	}
}

func buildConfigAnalyticsMap(cfg *config.Config) map[string]any {
	return map[string]any{
		"enabled":                cfg.Analytics.Enabled,
		"track_playback":         cfg.Analytics.TrackPlayback,
		"track_views":            cfg.Analytics.TrackViews,
		"retention_days":         cfg.Analytics.RetentionDays,
		"session_timeout":        cfg.Analytics.SessionTimeout,
		"cleanup_interval":       cfg.Analytics.CleanupInterval,
		"view_cooldown":          cfg.Analytics.ViewCooldown,
		"max_reconstruct_events": cfg.Analytics.MaxReconstructEvents,
	}
}

func buildConfigMatureScannerMap(cfg *config.Config) map[string]any {
	return map[string]any{
		"enabled":                     cfg.MatureScanner.Enabled,
		"auto_flag":                   cfg.MatureScanner.AutoFlag,
		"high_confidence_threshold":   cfg.MatureScanner.HighConfidenceThreshold,
		"medium_confidence_threshold": cfg.MatureScanner.MediumConfidenceThreshold,
		"high_confidence_keywords":    cfg.MatureScanner.HighConfidenceKeywords,
		"medium_confidence_keywords":  cfg.MatureScanner.MediumConfidenceKeywords,
		"require_review":              cfg.MatureScanner.RequireReview,
	}
}

func buildConfigHuggingFaceMap(cfg *config.Config) map[string]any {
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

func buildConfigDatabaseMap(cfg *config.Config) map[string]any {
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

func buildConfigStreamingMap(cfg *config.Config) map[string]any {
	return map[string]any{
		"require_auth":        cfg.Streaming.RequireAuth,
		"mobile_optimization": cfg.Streaming.MobileOptimization,
		"unauth_stream_limit": cfg.Streaming.UnauthStreamLimit,
		"keep_alive_enabled":  cfg.Streaming.KeepAliveEnabled,
		"keep_alive_timeout":  cfg.Streaming.KeepAliveTimeout,
		"adaptive":            cfg.Streaming.Adaptive,
		"default_chunk_size":  cfg.Streaming.DefaultChunkSize,
		"max_chunk_size":      cfg.Streaming.MaxChunkSize,
		"buffer_size":         cfg.Streaming.BufferSize,
		"mobile_chunk_size":   cfg.Streaming.MobileChunkSize,
	}
}

func buildConfigDownloadMap(cfg *config.Config) map[string]any {
	return map[string]any{
		"enabled":       cfg.Download.Enabled,
		"require_auth":  cfg.Download.RequireAuth,
		"chunk_size_kb": cfg.Download.ChunkSizeKB,
	}
}

func buildConfigLoggingMap(cfg *config.Config) map[string]any {
	return map[string]any{
		"level":         cfg.Logging.Level,
		"format":        cfg.Logging.Format,
		"file_enabled":  cfg.Logging.FileEnabled,
		"file_rotation": cfg.Logging.FileRotation,
		"max_file_size": cfg.Logging.MaxFileSize,
		"max_backups":   cfg.Logging.MaxBackups,
		"color_enabled": cfg.Logging.ColorEnabled,
	}
}

func buildConfigAgeGateMap(cfg *config.Config) map[string]any {
	return map[string]any{
		"enabled":        cfg.AgeGate.Enabled,
		"bypass_ips":     cfg.AgeGate.BypassIPs,
		"ip_verify_ttl":  cfg.AgeGate.IPVerifyTTL,
		"cookie_name":    cfg.AgeGate.CookieName,
		"cookie_max_age": cfg.AgeGate.CookieMaxAge,
	}
}

// CookieConsent (GDPR/CCPA banner) is admin-visible so operators can toggle
// the banner and adjust the consent cookie lifetime without restarting.
func buildConfigCookieConsentMap(cfg *config.Config) map[string]any {
	return map[string]any{
		"enabled":        cfg.CookieConsent.Enabled,
		"cookie_name":    cfg.CookieConsent.CookieName,
		"cookie_max_age": cfg.CookieConsent.CookieMaxAge,
	}
}

func buildConfigUploadsMap(cfg *config.Config) map[string]any {
	return map[string]any{
		"enabled":            cfg.Uploads.Enabled,
		"max_file_size":      cfg.Uploads.MaxFileSize,
		"allowed_extensions": cfg.Uploads.AllowedExtensions,
		"scan_for_mature":    cfg.Uploads.ScanForMature,
		"require_auth":       cfg.Uploads.RequireAuth,
	}
}

func buildConfigUIMap(cfg *config.Config) map[string]any {
	return map[string]any{
		"items_per_page":        cfg.UI.ItemsPerPage,
		"mobile_items_per_page": cfg.UI.MobileItemsPerPage,
		"mobile_grid_columns":   cfg.UI.MobileGridColumns,
		"feed_max_items":        cfg.UI.FeedMaxItems,
		"feed_default_items":    cfg.UI.FeedDefaultItems,
	}
}

func buildConfigDownloaderMap(cfg *config.Config) map[string]any {
	return map[string]any{
		"enabled":         cfg.Downloader.Enabled,
		"url":             cfg.Downloader.URL,
		"downloads_dir":   cfg.Downloader.DownloadsDir,
		"import_dir":      cfg.Downloader.ImportDir,
		"health_interval": cfg.Downloader.HealthInterval,
		"request_timeout": cfg.Downloader.RequestTimeout,
		// Shared secret with the downloader service — surface presence only, never
		// the value (mirrors the *_set pattern used for tokens/keys elsewhere).
		"internal_token_set": cfg.Downloader.InternalToken != "",
	}
}

// buildConfigDirectoriesMap exposes the configured library/runtime paths so the
// admin's read-only Directories panel can show where media actually lives. These
// are infra settings (changed via env/config file, not the UI), so they are
// surfaced for visibility only — there is no write path for them.
func buildConfigDirectoriesMap(cfg *config.Config) map[string]any {
	return map[string]any{
		"videos":     cfg.Directories.Videos,
		"music":      cfg.Directories.Music,
		"thumbnails": cfg.Directories.Thumbnails,
		"playlists":  cfg.Directories.Playlists,
		"uploads":    cfg.Directories.Uploads,
		"analytics":  cfg.Directories.Analytics,
		"hls_cache":  cfg.Directories.HLSCache,
		"data":       cfg.Directories.Data,
		"logs":       cfg.Directories.Logs,
		"temp":       cfg.Directories.Temp,
	}
}

func buildConfigStorageMap(cfg *config.Config) map[string]any {
	return map[string]any{
		"backend": cfg.Storage.Backend,
		"s3": map[string]any{
			"endpoint":       cfg.Storage.S3.Endpoint,
			"region":         cfg.Storage.S3.Region,
			"bucket":         cfg.Storage.S3.Bucket,
			"use_path_style": cfg.Storage.S3.UsePathStyle,
			"prefixes":       cfg.Storage.S3.Prefixes,
			"access_key_set": cfg.Storage.S3.AccessKeyID != "",
			"secret_key_set": cfg.Storage.S3.SecretAccessKey != "",
		},
	}
}

func buildConfigBackupMap(cfg *config.Config) map[string]any {
	return map[string]any{
		"retention_count": cfg.Backup.RetentionCount,
	}
}

func buildConfigUpdaterMap(cfg *config.Config) map[string]any {
	return map[string]any{
		"update_method":   cfg.Updater.UpdateMethod,
		"branch":          cfg.Updater.Branch,
		"app_dir":         cfg.Updater.AppDir,
		"github_username": cfg.Updater.GitHubUsername,
		// Secrets surface as "*_set" flags so the admin UI can show whether they
		// are populated without exposing the value itself.
		"github_token_set":    cfg.Updater.GitHubToken != "",
		"deploy_key_path_set": cfg.Updater.DeployKeyPath != "",
	}
}

func buildConfigRemoteMediaMap(cfg *config.Config) map[string]any {
	return map[string]any{
		"enabled":                  cfg.RemoteMedia.Enabled,
		"cache_enabled":            cfg.RemoteMedia.CacheEnabled,
		"sync_interval":            cfg.RemoteMedia.SyncInterval,
		"cache_size":               cfg.RemoteMedia.CacheSize,
		"cache_ttl":                cfg.RemoteMedia.CacheTTL,
		"http_timeout":             cfg.RemoteMedia.HTTPTimeout,
		"max_concurrent_downloads": cfg.RemoteMedia.MaxConcurrentDownloads,
	}
}

func buildConfigExtractorMap(cfg *config.Config) map[string]any {
	return map[string]any{
		"enabled":       cfg.Extractor.Enabled,
		"max_items":     cfg.Extractor.MaxItems,
		"proxy_timeout": cfg.Extractor.ProxyTimeout,
	}
}

// GetConfigMap returns config as a map for JSON serialization.
//
// The Follower and Receiver sections are intentionally excluded: they carry
// federation secrets (Follower.APIKey, Receiver.APIKeys) and are managed via
// dedicated, redaction-aware endpoints (/api/admin/follower/*, /api/admin/
// receiver/*) that reload the follower module in-place. Surfacing them through
// the unified config map would either leak those secrets or require duplicating
// the redaction + hot-reload logic here, so they are deliberately omitted.
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
		{"cookie_consent", buildConfigCookieConsentMap},
		{"uploads", buildConfigUploadsMap},
		{"ui", buildConfigUIMap},
		{"downloader", buildConfigDownloaderMap},
		{"storage", buildConfigStorageMap},
		{"backup", buildConfigBackupMap},
		{"updater", buildConfigUpdaterMap},
		{"remote_media", buildConfigRemoteMediaMap},
		{"extractor", buildConfigExtractorMap},
		{"hub", buildConfigHubMap},
		{"directories", buildConfigDirectoriesMap},
	}
	out := make(map[string]any, len(sections))
	for _, s := range sections {
		out[s.key] = s.fn(cfg)
	}
	return out
}
