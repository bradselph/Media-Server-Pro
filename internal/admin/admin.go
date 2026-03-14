// Package admin provides administrative functionality and audit logging.
// It handles admin operations, system stats, and audit trail.
package admin

import (
	"context"
	"encoding/csv"
	"encoding/json"
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
	backups   []models.BackupInfo
	backupMu  sync.RWMutex
	dataDir   string
	backupDir string
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
		backups:   make([]models.BackupInfo, 0),
		dataDir:   cfg.Get().Directories.Data,
		backupDir: filepath.Join(cfg.Get().Directories.Data, "backups"),
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

	// Ensure directories exist
	if err := os.MkdirAll(m.backupDir, 0755); err != nil {
		m.log.Error("Failed to create backup directory: %v", err)
		return fmt.Errorf("failed to create backup directory: %w", err)
	}

	// Load backup info
	m.scanBackups()

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
	defer func() {
		if err := file.Close(); err != nil {
			m.log.Warn("Failed to close audit log file: %v", err)
		}
	}()

	writer := csv.NewWriter(file)
	defer writer.Flush()

	// Header
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

	// Write data
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

// CreateBackup creates a backup of server configuration data
// Note: This backs up config only, not media metadata or playlists.
func (m *Module) CreateBackup(description string) (*models.BackupInfo, error) {
	backupID := fmt.Sprintf("backup_%s", time.Now().Format("20060102_150405.000000000"))
	backupPath := filepath.Join(m.backupDir, backupID+".json")

	// Gather data to back up
	backupData := map[string]interface{}{
		"timestamp":   time.Now(),
		"description": description,
		"config":      m.config.Get(),
	}

	data, err := json.MarshalIndent(backupData, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("failed to marshal backup: %w", err)
	}

	if err := os.WriteFile(backupPath, data, 0600); err != nil {
		return nil, fmt.Errorf("failed to write backup: %w", err)
	}

	// Get file size for backup info
	info, err := os.Stat(backupPath)
	if err != nil {
		// If stat fails, use data length as fallback
		m.log.Warn("Failed to stat backup file, using data length: %v", err)
	}

	fileSize := int64(len(data))
	if info != nil {
		fileSize = info.Size()
	}

	backup := &models.BackupInfo{
		ID:          backupID,
		Filename:    backupID + ".json",
		Size:        fileSize,
		CreatedAt:   time.Now(),
		Type:        "config",
		Description: description,
	}

	m.backupMu.Lock()
	m.backups = append(m.backups, *backup)
	m.backupMu.Unlock()

	m.log.Info("Created backup: %s", backupID)
	return backup, nil
}

// ListBackups returns all available backups
func (m *Module) ListBackups() []models.BackupInfo {
	m.backupMu.RLock()
	defer m.backupMu.RUnlock()

	result := make([]models.BackupInfo, len(m.backups))
	copy(result, m.backups)
	return result
}

// DeleteBackup removes a backup
func (m *Module) DeleteBackup(id string) error {
	m.backupMu.Lock()
	defer m.backupMu.Unlock()

	found := false
	var newBackups []models.BackupInfo
	for _, backup := range m.backups {
		if backup.ID == id {
			found = true
			path := filepath.Join(m.backupDir, backup.Filename)
			if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
				return fmt.Errorf("failed to remove backup file %s: %w", path, err)
			}
		} else {
			newBackups = append(newBackups, backup)
		}
	}

	if !found {
		return fmt.Errorf("backup not found: %s", id)
	}

	m.backups = newBackups
	m.log.Info("Deleted backup: %s", id)
	return nil
}

// findBackupFilename returns the backup filename for the given ID, or an error if not found.
func (m *Module) findBackupFilename(id string) (string, error) {
	m.backupMu.RLock()
	defer m.backupMu.RUnlock()
	for _, backup := range m.backups {
		if backup.ID == id {
			return backup.Filename, nil
		}
	}
	return "", fmt.Errorf("backup not found: %s", id)
}

// loadBackupConfig reads a backup file and returns the parsed config, or an error.
func (m *Module) loadBackupConfig(path string) (*config.Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read backup: %w", err)
	}
	var backupData map[string]interface{}
	if err := json.Unmarshal(data, &backupData); err != nil {
		return nil, fmt.Errorf("failed to parse backup: %w", err)
	}
	cfgData, ok := backupData["config"]
	if !ok {
		return nil, fmt.Errorf("backup does not contain configuration data")
	}
	cfgBytes, err := json.Marshal(cfgData)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal config from backup: %w", err)
	}
	var cfg config.Config
	if err := json.Unmarshal(cfgBytes, &cfg); err != nil {
		return nil, fmt.Errorf("backup contains invalid configuration: %w", err)
	}
	return &cfg, nil
}

// RestoreBackup restores from a backup. It creates a pre-restore backup of the
// current config before applying the restored config, and validates the backup
// contains parseable configuration data.
func (m *Module) RestoreBackup(id string) error {
	filename, err := m.findBackupFilename(id)
	if err != nil {
		return err
	}
	path := filepath.Join(m.backupDir, filename)
	cfg, err := m.loadBackupConfig(path)
	if err != nil {
		return err
	}
	if _, err := m.CreateBackup(fmt.Sprintf("pre-restore automatic backup (before restoring %s)", id)); err != nil {
		return fmt.Errorf("failed to create pre-restore backup: %w", err)
	}
	if err := m.config.Update(func(c *config.Config) {
		*c = *cfg
	}); err != nil {
		return fmt.Errorf("failed to update config during restore: %w", err)
	}
	m.log.Info("Restored from backup: %s", id)
	return nil
}

// scanBackups scans the backup directory for existing backups.
// Replaces m.backups with a fresh list so repeated calls do not duplicate entries.
func (m *Module) scanBackups() {
	entries, err := os.ReadDir(m.backupDir)
	if err != nil {
		m.log.Warn("Failed to read backup directory %s: %v", m.backupDir, err)
		return
	}

	m.backupMu.Lock()
	defer m.backupMu.Unlock()
	m.backups = nil

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if filepath.Ext(entry.Name()) != ".json" {
			continue
		}

		info, err := entry.Info()
		if err != nil {
			m.log.Warn("Failed to stat backup entry %s: %v", entry.Name(), err)
			continue
		}

		backup := models.BackupInfo{
			ID:        entry.Name()[:len(entry.Name())-5], // Remove .json
			Filename:  entry.Name(),
			Size:      info.Size(),
			CreatedAt: info.ModTime(),
			Type:      "config",
		}
		m.backups = append(m.backups, backup)
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
