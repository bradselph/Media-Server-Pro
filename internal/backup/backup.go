// Package backup provides backup and restore functionality for server data.
package backup

import (
	"archive/zip"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
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

// CreateBackupOptions holds options for creating a backup
type CreateBackupOptions struct {
	Description string // Human-readable description
	Type        string // "full", "config", or "data"
}

// Manifest describes a backup
type Manifest struct {
	ID          string    `json:"id"`
	Filename    string    `json:"filename"`
	CreatedAt   time.Time `json:"created_at"`
	Size        int64     `json:"size"`
	Type        string    `json:"type"` // full, config, data
	Description string    `json:"description,omitempty"`
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
func (m *Module) CreateBackup(opts CreateBackupOptions) (*Manifest, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	backupType := opts.Type
	if backupType == "" {
		backupType = "full"
	}

	timestamp := time.Now().Format("20060102_150405")
	backupID := fmt.Sprintf("backup_%s", timestamp)
	filename := backupID + ".zip"
	backupPath := filepath.Join(m.backupDir, filename)

	manifest := &Manifest{
		ID:          backupID,
		Filename:    filename,
		CreatedAt:   time.Now(),
		Type:        backupType,
		Description: opts.Description,
		Files:       make([]string, 0),
		Errors:      make([]string, 0),
		Version:     "3.0.0",
	}

	if err := m.writeBackupArchive(backupPath, manifest); err != nil {
		return nil, err
	}

	if info, err := os.Stat(backupPath); err == nil {
		manifest.Size = info.Size()
	}

	if err := m.saveManifest(manifest); err != nil {
		m.log.Warn("Failed to save backup manifest: %v", err)
	}

	m.log.Info("Created backup: %s (%d files, %d bytes)", backupID, len(manifest.Files), manifest.Size)
	return manifest, nil
}

// writeBackupArchive creates the zip file and adds all files from manifest.Type.
func (m *Module) writeBackupArchive(backupPath string, manifest *Manifest) error {
	zipFile, err := os.Create(backupPath)
	if err != nil {
		return fmt.Errorf("failed to create backup file: %w", err)
	}
	defer m.closeAndWarn(zipFile.Close, "Failed to close backup zip file: %v")

	zipWriter := zip.NewWriter(zipFile)
	filesToBackup := m.getFilesToBackup(manifest.Type)

	for _, file := range filesToBackup {
		if addErr := m.addFileToZip(zipWriter, file, manifest); addErr != nil {
			manifest.Errors = append(manifest.Errors, fmt.Sprintf("%s: %v", file, addErr))
		}
	}

	if err := zipWriter.Close(); err != nil {
		m.removeFileQuietly(removeFileOpts{Path: backupPath, Label: "corrupted backup"})
		return fmt.Errorf("failed to finalize backup archive: %w", err)
	}

	return nil
}

// closeAndWarn invokes closeFn and logs a warning if it returns an error.
func (m *Module) closeAndWarn(closeFn func() error, msg string) {
	if err := closeFn(); err != nil {
		m.log.Warn(msg, err)
	}
}

// removeFileOpts specifies which file to remove and how to label it in logs.
type removeFileOpts struct {
	Path  string
	Label string
}

// removeFileQuietly attempts to remove the file at opts.Path and logs a warning on failure.
func (m *Module) removeFileQuietly(opts removeFileOpts) {
	if err := os.Remove(opts.Path); err != nil {
		m.log.Warn("Failed to remove %s %s: %v", opts.Label, opts.Path, err)
	}
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
	m.restoreMu.Lock()
	defer m.restoreMu.Unlock()

	backupPath := filepath.Join(m.backupDir, backupID+".zip")
	if _, err := os.Stat(backupPath); err != nil {
		return fmt.Errorf("backup not found: %s", backupID)
	}

	m.createPreRestoreBackup()

	m.mu.Lock()
	defer m.mu.Unlock()

	reader, err := zip.OpenReader(backupPath)
	if err != nil {
		return fmt.Errorf("failed to open backup: %w", err)
	}
	defer func() {
		if cerr := reader.Close(); cerr != nil {
			m.log.Warn("Failed to close backup reader: %v", cerr)
		}
	}()

	if err := m.validateBackupArchiveSize(reader.File); err != nil {
		return err
	}

	m.extractAllFiles(reader.File)

	m.log.Info("Restored from backup: %s", backupID)
	m.log.Warn("Restore complete — a server restart is required for all modules to reload their in-memory caches from the restored data files")
	return nil
}

// createPreRestoreBackup creates an automatic backup before restore as a safety measure.
func (m *Module) createPreRestoreBackup() {
	m.log.Info("Creating pre-restore backup...")
	preRestore, err := m.CreateBackup(CreateBackupOptions{Description: "pre-restore automatic backup", Type: "full"})
	if err != nil {
		m.log.Warn("Failed to create pre-restore backup: %v", err)
		return
	}
	m.log.Info("Pre-restore backup created: %s", preRestore.ID)
}

const maxTotalExtractSize = 500 * 1024 * 1024 // 500 MB (zip bomb guard)

// validateBackupArchiveSize ensures the total uncompressed size does not exceed the safety limit.
func (m *Module) validateBackupArchiveSize(files []*zip.File) error {
	var totalUncompressed uint64
	for _, f := range files {
		totalUncompressed += f.UncompressedSize64
		if totalUncompressed > maxTotalExtractSize {
			return fmt.Errorf("backup archive total uncompressed size exceeds the %d MB safety limit", maxTotalExtractSize/(1024*1024))
		}
	}
	return nil
}

// extractAllFiles extracts all files from the zip archive.
func (m *Module) extractAllFiles(files []*zip.File) {
	for _, file := range files {
		if err := m.extractFile(file); err != nil {
			m.log.Error("Failed to extract %s: %v", file.Name, err)
		}
	}
}

// extractFile extracts a file from the zip
func (m *Module) extractFile(file *zip.File) error {
	if file.FileInfo().IsDir() || file.Name == "manifest.json" {
		return nil
	}
	destPath, err := m.validateExtractPath(file.Name)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
		return err
	}
	return m.copyZipEntryToFile(file, destPath)
}

// pathCheckArgs holds path arguments for validation to avoid string-parameter confusion.
type pathCheckArgs struct {
	zipName  string // original name from zip archive
	destPath string // resolved destination path
}

// pathScopeArgs holds path and base for scope checks.
type pathScopeArgs struct {
	path string
	base string
}

// validateExtractPath validates the destination path and prevents zip slip attacks.
func (m *Module) validateExtractPath(name string) (string, error) {
	destPath := filepath.Join(m.dataDir, name)
	check := pathCheckArgs{zipName: name, destPath: destPath}
	if m.isPathTraversal(check) {
		return "", fmt.Errorf("illegal file path: %s", name)
	}
	if !pathWithinBase(pathScopeArgs{path: destPath, base: m.dataDir}) {
		m.log.Warn("Path escape attempt detected: %s", name)
		return "", fmt.Errorf("illegal file path: %s", name)
	}
	return destPath, nil
}

// isPathTraversal returns true if the path attempts to escape the data directory.
func (m *Module) isPathTraversal(args pathCheckArgs) bool {
	rel, err := filepath.Rel(m.dataDir, args.destPath)
	if err != nil {
		return true
	}
	if strings.HasPrefix(rel, ".."+string(os.PathSeparator)) || strings.HasPrefix(rel, "..") {
		m.log.Warn("Zip slip attempt detected: %s (rel: %s)", args.zipName, rel)
		return true
	}
	return false
}

// pathWithinBase returns true if path is within or equal to base.
func pathWithinBase(args pathScopeArgs) bool {
	dest := filepath.Clean(args.path)
	b := filepath.Clean(args.base)
	return strings.HasPrefix(dest, b+string(os.PathSeparator)) || dest == b
}

const maxExtractSize = 100 * 1024 * 1024 // 100MB limit to prevent zip bomb attacks

// copyZipEntryToFile copies a zip entry to the destination, enforcing size limits.
func (m *Module) copyZipEntryToFile(file *zip.File, destPath string) error {
	destFile, err := os.Create(destPath)
	if err != nil {
		return err
	}
	defer func() {
		if cerr := destFile.Close(); cerr != nil {
			m.log.Warn("Failed to close destination file %s: %v", destPath, cerr)
		}
	}()

	srcFile, err := file.Open()
	if err != nil {
		return err
	}
	defer m.closeAndWarn(srcFile.Close, "Failed to close source file: %v")

	limitedReader := io.LimitReader(srcFile, maxExtractSize+1)
	n, err := io.Copy(destFile, limitedReader)
	if err != nil {
		return err
	}
	if n > maxExtractSize {
		m.removeFileQuietly(removeFileOpts{Path: destPath, Label: "oversize extracted file"})
		return fmt.Errorf("file %s exceeds maximum extract size of %d bytes", file.Name, maxExtractSize)
	}
	return nil
}

// ListBackups returns all available backups from the database.
func (m *Module) ListBackups() ([]*Manifest, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	records, err := m.repo.List(context.Background())
	if err != nil {
		return nil, err
	}

	backups := make([]*Manifest, 0, len(records))
	for _, rec := range records {
		backups = append(backups, m.recordToManifest(rec))
	}

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

	// Delete manifest from database
	if err := m.repo.Delete(context.Background(), backupID); err != nil {
		m.log.Warn("Failed to delete backup manifest from database: %v", err)
	}

	m.log.Info("Deleted backup: %s", backupID)
	return nil
}

// GetBackup returns a specific backup manifest from the database
func (m *Module) GetBackup(backupID string) (*Manifest, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	rec, err := m.repo.Get(context.Background(), backupID)
	if err != nil {
		return nil, err
	}
	if rec == nil {
		return nil, fmt.Errorf("backup not found: %s", backupID)
	}

	return m.recordToManifest(rec), nil
}

// recordToManifest converts a repository BackupManifestRecord to a Manifest.
func (m *Module) recordToManifest(rec *repositories.BackupManifestRecord) *Manifest {
	return &Manifest{
		ID:          rec.ID,
		Filename:    rec.Filename,
		CreatedAt:   rec.CreatedAt,
		Size:        rec.Size,
		Type:        rec.Type,
		Description: rec.Description,
		Files:       rec.Files,
		Errors:      rec.Errors,
		Version:     rec.Version,
	}
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
