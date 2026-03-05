// Package backup provides backup and restore functionality for server data.
package backup

import (
	"archive/zip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"media-server-pro/internal/config"
	"media-server-pro/internal/database"
	"media-server-pro/internal/logger"
	"media-server-pro/internal/repositories"
	mysqlrepo "media-server-pro/internal/repositories/mysql"
	"media-server-pro/pkg/helpers"
	"media-server-pro/pkg/models"
)

const manifestSuffix = "_manifest.json"

// Module handles backup and restore operations
type Module struct {
	config    *config.Manager
	log       *logger.Logger
	dbModule  *database.Module
	repo      repositories.BackupManifestRepository
	backupDir string
	dataDir   string
	mu        sync.RWMutex
	restoreMu sync.Mutex // Prevents concurrent restores
	healthy   bool
	healthMsg string
	healthMu  sync.RWMutex
}

// Manifest describes a backup
type Manifest struct {
	ID          string    `json:"id"`
	Filename    string    `json:"filename"`
	CreatedAt   time.Time `json:"created_at"`
	Size        int64     `json:"size"`
	Type        string    `json:"type"` // full, config, data
	Description string    `json:"description"`
	Files       []string  `json:"files"`
	Errors      []string  `json:"errors,omitempty"`
	Version     string    `json:"version"`
}

// NewModule creates a new backup module
func NewModule(cfg *config.Manager, dbModule *database.Module) *Module {
	return &Module{
		config:    cfg,
		log:       logger.New("backup"),
		dbModule:  dbModule,
		backupDir: filepath.Join(cfg.Get().Directories.Data, "backups"),
		dataDir:   cfg.Get().Directories.Data,
	}
}

// Name returns the module name
func (m *Module) Name() string {
	return "backup"
}

// Start initializes the backup module
func (m *Module) Start(_ context.Context) error {
	m.log.Info("Starting backup module...")

	m.repo = mysqlrepo.NewBackupManifestRepository(m.dbModule.GORM())

	// Ensure backup directory exists
	if err := os.MkdirAll(m.backupDir, 0755); err != nil {
		m.log.Error("Failed to create backup directory: %v", err)
		m.healthMu.Lock()
		m.healthy = false
		m.healthMsg = fmt.Sprintf("Directory error: %v", err)
		m.healthMu.Unlock()
		return err
	}

	m.healthMu.Lock()
	m.healthy = true
	m.healthMsg = "Running"
	m.healthMu.Unlock()
	m.log.Info("Backup module started")
	return nil
}

// Stop gracefully stops the module
func (m *Module) Stop(_ context.Context) error {
	m.log.Info("Stopping backup module...")
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

// CreateBackup creates a full backup of server data
func (m *Module) CreateBackup(description, backupType string) (*Manifest, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	timestamp := time.Now().Format("20060102_150405")
	backupID := fmt.Sprintf("backup_%s", timestamp)
	filename := backupID + ".zip"
	backupPath := filepath.Join(m.backupDir, filename)

	manifest := &Manifest{
		ID:          backupID,
		Filename:    filename,
		CreatedAt:   time.Now(),
		Type:        backupType,
		Description: description,
		Files:       make([]string, 0),
		Errors:      make([]string, 0),
		Version:     "3.0.0",
	}

	// Create zip file
	zipFile, err := os.Create(backupPath)
	if err != nil {
		return nil, fmt.Errorf("failed to create backup file: %w", err)
	}
	defer func() {
		if err := zipFile.Close(); err != nil {
			m.log.Warn("Failed to close backup zip file: %v", err)
		}
	}()

	zipWriter := zip.NewWriter(zipFile)

	// Files to backup based on type
	filesToBackup := m.getFilesToBackup(backupType)

	for _, file := range filesToBackup {
		if err := m.addFileToZip(zipWriter, file, manifest); err != nil {
			manifest.Errors = append(manifest.Errors, fmt.Sprintf("%s: %v", file, err))
		}
	}

	// Close zip writer first to finalize and calculate size
	if err := zipWriter.Close(); err != nil {
		// Remove corrupted backup file if finalization failed
		if removeErr := os.Remove(backupPath); removeErr != nil {
			m.log.Warn("Failed to remove corrupted backup %s: %v", backupPath, removeErr)
		}
		return nil, fmt.Errorf("failed to finalize backup archive: %w", err)
	}

	// Get file size before writing manifest (so it has correct size)
	if info, err := os.Stat(backupPath); err == nil {
		manifest.Size = info.Size()
	}

	// Save manifest separately for quick access
	if err := m.saveManifest(manifest); err != nil {
		m.log.Warn("Failed to save backup manifest: %v", err)
	}

	m.log.Info("Created backup: %s (%d files, %d bytes)", backupID, len(manifest.Files), manifest.Size)
	return manifest, nil
}

// getFilesToBackup returns list of files to backup based on type
func (m *Module) getFilesToBackup(backupType string) []string {
	var files []string

	switch backupType {
	case "config":
		files = []string{
			"config.json",
		}
	// Note: all application data (playlists, analytics, scan results, etc.) is stored in MySQL
	// and is not included in file-based backups. Use a database-level backup tool for full data
	// protection. The "data" type backs up config only; "full" is an alias for the same.
	case "data", "full":
		files = []string{
			"config.json",
		}
	}

	// Convert to full paths
	var fullPaths []string
	for _, f := range files {
		fullPath := filepath.Join(m.dataDir, f)
		if _, err := os.Stat(fullPath); err == nil {
			fullPaths = append(fullPaths, fullPath)
		}
	}

	return fullPaths
}

// addFileToZip adds a file to the zip archive
func (m *Module) addFileToZip(zipWriter *zip.Writer, filePath string, manifest *Manifest) error {
	file, err := os.Open(filePath)
	if err != nil {
		return err
	}
	defer func() {
		if err := file.Close(); err != nil {
			m.log.Warn("Failed to close file %s: %v", filePath, err)
		}
	}()

	info, err := file.Stat()
	if err != nil {
		return err
	}

	// Create a relative path for the zip
	relPath, err := filepath.Rel(m.dataDir, filePath)
	if err != nil {
		relPath = filepath.Base(filePath)
	}

	header, err := zip.FileInfoHeader(info)
	if err != nil {
		return err
	}
	header.Name = relPath
	header.Method = zip.Deflate

	writer, err := zipWriter.CreateHeader(header)
	if err != nil {
		return err
	}

	_, err = io.Copy(writer, file)
	if err != nil {
		return err
	}

	manifest.Files = append(manifest.Files, relPath)
	return nil
}

// saveManifest saves manifest to the database
func (m *Module) saveManifest(manifest *Manifest) error {
	rec := &repositories.BackupManifestRecord{
		ID:          manifest.ID,
		Filename:    manifest.Filename,
		CreatedAt:   manifest.CreatedAt,
		Size:        manifest.Size,
		Type:        manifest.Type,
		Description: manifest.Description,
		Files:       manifest.Files,
		Errors:      manifest.Errors,
		Version:     manifest.Version,
	}
	return m.repo.Save(context.Background(), rec)
}

// RestoreBackup restores from a backup. All modules maintain in-memory caches
// (auth users/sessions, media metadata, etc.) that are loaded at startup. After
// the data files are overwritten by restore, those caches become stale. A server
// restart is required for all modules to reload fresh state from the restored files.
func (m *Module) RestoreBackup(backupID string) error {
	// Prevent concurrent restores to avoid race conditions
	m.restoreMu.Lock()
	defer m.restoreMu.Unlock()

	// Verify backup exists
	backupPath := filepath.Join(m.backupDir, backupID+".zip")
	if _, err := os.Stat(backupPath); err != nil {
		return fmt.Errorf("backup not found: %s", backupID)
	}

	// Create a pre-restore backup as safety measure
	// CreateBackup acquires m.mu internally, so we don't hold it here
	// restoreMu serializes restore operations preventing concurrent restores
	m.log.Info("Creating pre-restore backup...")
	preRestore, err := m.CreateBackup("pre-restore automatic backup", "full")
	if err != nil {
		m.log.Warn("Failed to create pre-restore backup: %v", err)
	} else {
		m.log.Info("Pre-restore backup created: %s", preRestore.ID)
	}

	// Now acquire m.mu for the actual restore operation
	m.mu.Lock()
	defer m.mu.Unlock()

	// Open zip file
	reader, err := zip.OpenReader(backupPath)
	if err != nil {
		return fmt.Errorf("failed to open backup: %w", err)
	}
	defer func() {
		if err := reader.Close(); err != nil {
			m.log.Warn("Failed to close backup reader: %v", err)
		}
	}()

	// Validate total uncompressed size before extracting (zip bomb guard).
	// Per-file limit is 100 MB (in extractFile); here we cap the archive total.
	const maxTotalExtractSize = 500 * 1024 * 1024 // 500 MB
	var totalUncompressed uint64
	for _, f := range reader.File {
		totalUncompressed += f.UncompressedSize64
		if totalUncompressed > maxTotalExtractSize {
			return fmt.Errorf("backup archive total uncompressed size exceeds the %d MB safety limit", maxTotalExtractSize/(1024*1024))
		}
	}

	// Extract files
	for _, file := range reader.File {
		if err := m.extractFile(file); err != nil {
			m.log.Error("Failed to extract %s: %v", file.Name, err)
		}
	}

	m.log.Info("Restored from backup: %s", backupID)
	m.log.Warn("Restore complete — a server restart is required for all modules to reload their in-memory caches from the restored data files")
	return nil
}

// extractFile extracts a file from the zip
func (m *Module) extractFile(file *zip.File) error {
	// Skip directories
	if file.FileInfo().IsDir() {
		return nil
	}

	// Skip manifest
	if file.Name == "manifest.json" {
		return nil
	}

	// Validate path (prevent zip slip vulnerability)
	// Use filepath.Rel to detect path traversal attempts - it returns an error
	// if the target path would escape the data directory
	destPath := filepath.Join(m.dataDir, file.Name)
	rel, err := filepath.Rel(m.dataDir, destPath)
	if err != nil || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) || strings.HasPrefix(rel, "..") {
		m.log.Warn("Zip slip attempt detected: %s (rel: %s)", file.Name, rel)
		return fmt.Errorf("illegal file path: %s", file.Name)
	}
	// Additional check: ensure the cleaned destination path is still within dataDir
	cleanedDest := filepath.Clean(destPath)
	cleanedBase := filepath.Clean(m.dataDir)
	if !strings.HasPrefix(cleanedDest, cleanedBase+string(os.PathSeparator)) && cleanedDest != cleanedBase {
		m.log.Warn("Path escape attempt detected: %s", file.Name)
		return fmt.Errorf("illegal file path: %s", file.Name)
	}

	// Ensure directory exists
	if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
		return err
	}

	// Create destination file
	destFile, err := os.Create(destPath)
	if err != nil {
		return err
	}
	defer func() {
		if err := destFile.Close(); err != nil {
			m.log.Warn("Failed to close destination file %s: %v", destPath, err)
		}
	}()

	// Open source file in zip
	srcFile, err := file.Open()
	if err != nil {
		return err
	}
	defer func() {
		if err := srcFile.Close(); err != nil {
			m.log.Warn("Failed to close source file: %v", err)
		}
	}()

	// Limit extracted file size to 100MB to prevent zip bomb attacks
	const maxExtractSize = 100 * 1024 * 1024
	limitedReader := io.LimitReader(srcFile, maxExtractSize+1)
	n, err := io.Copy(destFile, limitedReader)
	if err != nil {
		return err
	}
	if n > maxExtractSize {
		return fmt.Errorf("file %s exceeds maximum extract size of %d bytes", file.Name, maxExtractSize)
	}
	return nil
}

// ListBackups returns all available backups
func (m *Module) ListBackups() ([]*Manifest, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	entries, err := os.ReadDir(m.backupDir)
	if err != nil {
		return nil, err
	}

	backups := make([]*Manifest, 0)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if !strings.HasSuffix(entry.Name(), manifestSuffix) {
			continue
		}

		manifestPath := filepath.Join(m.backupDir, entry.Name())
		data, err := os.ReadFile(manifestPath)
		if err != nil {
			continue
		}

		var manifest Manifest
		if err := json.Unmarshal(data, &manifest); err != nil {
			m.log.Warn("Failed to parse backup manifest %s: %v", entry.Name(), err)
			continue
		}

		backups = append(backups, &manifest)
	}

	// Sort by date, newest first
	sort.Slice(backups, func(i, j int) bool {
		return backups[i].CreatedAt.After(backups[j].CreatedAt)
	})

	return backups, nil
}

// DeleteBackup removes a backup
func (m *Module) DeleteBackup(backupID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Delete zip file
	backupPath := filepath.Join(m.backupDir, backupID+".zip")
	if err := os.Remove(backupPath); err != nil && !os.IsNotExist(err) {
		return err
	}

	// Delete manifest
	manifestPath := filepath.Join(m.backupDir, backupID+manifestSuffix)
	if err := os.Remove(manifestPath); err != nil && !os.IsNotExist(err) {
		return err
	}

	m.log.Info("Deleted backup: %s", backupID)
	return nil
}

// GetBackup returns a specific backup manifest
func (m *Module) GetBackup(backupID string) (*Manifest, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	manifestPath := filepath.Join(m.backupDir, backupID+manifestSuffix)
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return nil, fmt.Errorf("backup not found: %s", backupID)
	}

	var manifest Manifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return nil, err
	}

	return &manifest, nil
}

// CleanOldBackups removes backups older than retention period
func (m *Module) CleanOldBackups(keepCount int) (int, error) {
	backups, err := m.ListBackups()
	if err != nil {
		return 0, err
	}

	if len(backups) <= keepCount {
		return 0, nil
	}

	// Backups are already sorted newest first
	removed := 0
	for i := keepCount; i < len(backups); i++ {
		if err := m.DeleteBackup(backups[i].ID); err != nil {
			m.log.Warn("Failed to delete old backup %s: %v", backups[i].ID, err)
		} else {
			removed++
		}
	}

	if removed > 0 {
		m.log.Info("Cleaned up %d old backups", removed)
	}

	return removed, nil
}

// GetBackupStats returns backup statistics
func (m *Module) GetBackupStats() Stats {
	backups, _ := m.ListBackups()

	stats := Stats{
		Count: len(backups),
	}

	for _, b := range backups {
		stats.TotalSize += b.Size
	}

	if len(backups) > 0 {
		stats.LatestBackup = &backups[0].CreatedAt
		stats.OldestBackup = &backups[len(backups)-1].CreatedAt
	}

	return stats
}

// Stats holds backup statistics.
type Stats struct {
	Count        int        `json:"count"`
	TotalSize    int64      `json:"total_size"`
	LatestBackup *time.Time `json:"latest_backup,omitempty"`
	OldestBackup *time.Time `json:"oldest_backup,omitempty"`
}
