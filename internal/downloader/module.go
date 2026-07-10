// Package downloader provides integration with the standalone media downloader
// service. It acts as a proxy — forwarding HTTP API calls and WebSocket
// connections to the downloader (running on localhost), and providing file
// import capabilities to move completed downloads into MSP's media library.
package downloader

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"media-server-pro/internal/config"
	"media-server-pro/internal/logger"
	"media-server-pro/internal/media"
	"media-server-pro/pkg/helpers"
	"media-server-pro/pkg/models"
)

// Configuration/usage errors the HTTP layer maps to 4xx instead of 500 — these
// mean "the admin's setup or request is incomplete", not "the server broke".
var (
	// ErrDownloadsDirNotConfigured: downloader.downloads_dir is empty, so there
	// is nothing to list or import from.
	ErrDownloadsDirNotConfigured = errors.New("downloads_dir not configured")
	// ErrNoImportDestination: neither downloader.import_dir nor directories.uploads
	// is set, so there is no place to import to.
	ErrNoImportDestination = errors.New("no import destination configured (set downloader.import_dir or directories.uploads)")
	// ErrDestinationReadOnly: the chosen destination is on a read-only mount
	// (e.g. a HiDrive WebDAV share mounted --read-only). Surfaced before the copy
	// is attempted so the admin gets a clear reason, not a raw EROFS.
	ErrDestinationReadOnly = errors.New("destination is read-only; pick a writable location or remount it read-write")
	// ErrUnknownDestination: the destination key didn't match any enumerated
	// target (stale UI, or a crafted request).
	ErrUnknownDestination = errors.New("unknown or unavailable destination")
	// ErrInvalidSubfolder: the optional new sub-folder name failed validation.
	ErrInvalidSubfolder = errors.New("invalid sub-folder name")
)

const defaultDownloaderTimeout = 30 * time.Second
const defaultHealthCheckInterval = 30 * time.Second

// Module manages the connection to the external downloader service.
type Module struct {
	config       *config.Manager
	log          *logger.Logger
	client       *Client
	mediaModule  *media.Module
	postScanHook func()

	healthMu  sync.RWMutex
	healthy   bool
	healthMsg string
	online    bool

	cancelHealth context.CancelFunc
	scanWG       sync.WaitGroup

	// destWritableCache memoizes the (stat + temp-file) writability probe per
	// destination directory for a short TTL, so a batch auto-import sweep targeting
	// the same folder doesn't repeat real (and, on a network mount, round-tripping)
	// filesystem I/O for every file.
	destWritableMu    sync.Mutex
	destWritableCache map[string]destWritableEntry
}

// destWritableEntry is one cached destination-writability probe result.
type destWritableEntry struct {
	ok bool
	at time.Time
}

// destWritableTTL bounds how long a cached writability result is trusted.
const destWritableTTL = 5 * time.Second

// destinationWritableCached wraps destinationWritable with a short per-directory TTL
// cache. The result cannot change between two imports milliseconds apart into the
// same folder, so a sweep of N pending downloads probes each destination once per
// TTL window instead of N times.
func (m *Module) destinationWritableCached(dir string) bool {
	now := time.Now()
	m.destWritableMu.Lock()
	if m.destWritableCache == nil {
		m.destWritableCache = make(map[string]destWritableEntry)
	}
	if e, ok := m.destWritableCache[dir]; ok && now.Sub(e.at) < destWritableTTL {
		m.destWritableMu.Unlock()
		return e.ok
	}
	m.destWritableMu.Unlock()

	ok := destinationWritable(dir)

	m.destWritableMu.Lock()
	m.destWritableCache[dir] = destWritableEntry{ok: ok, at: now}
	m.destWritableMu.Unlock()
	return ok
}

// NewModule creates a new downloader integration module.
func NewModule(cfg *config.Manager) *Module {
	return &Module{
		config: cfg,
		log:    logger.New("downloader"),
	}
}

// SetMediaModule sets the media module reference used for triggering
// library rescans after file imports. Called from main.go to avoid
// circular dependency.
func (m *Module) SetMediaModule(mm *media.Module) {
	m.mediaModule = mm
}

// SetPostScanHook sets a callback invoked after a successful post-import
// rescan. main.go wires this to the suggestions catalog re-feed so imports
// don't leave the suggestions engine serving a stale catalog until the next
// scheduled scan.
func (m *Module) SetPostScanHook(hook func()) {
	m.postScanHook = hook
}

func (m *Module) Name() string { return "downloader" }

func (m *Module) Start(_ context.Context) error {
	m.log.Info("Starting downloader module...")

	cfg := m.config.Get()
	if cfg == nil {
		return fmt.Errorf("config not available")
	}
	if !cfg.Downloader.Enabled {
		m.log.Info("Downloader integration is disabled")
		m.setHealth(true, "Disabled")
		return nil
	}

	if cfg.Downloader.URL == "" {
		m.setHealth(false, "No downloader URL configured")
		return nil
	}

	timeout := cfg.Downloader.RequestTimeout
	if timeout <= 0 {
		timeout = defaultDownloaderTimeout
	}
	m.client = NewClient(cfg.Downloader.URL, timeout, cfg.Downloader.InternalToken)
	if cfg.Downloader.InternalToken == "" {
		m.log.Warn("DOWNLOADER_INTERNAL_TOKEN not set — bearer-token admins will not be able to use server-side storage")
	}

	if cfg.Downloader.DownloadsDir == "" {
		m.log.Warn("Downloader downloads_dir not configured — file import will be unavailable")
	}
	if !importDirUnderRoot(cfg) {
		m.log.Warn("Downloader import_dir %q is not under any configured library root (videos/music/uploads); imported files will land there but the media scanner will not discover them", cfg.Downloader.ImportDir)
	}

	// Start background health checker
	ctx, cancel := context.WithCancel(context.Background())
	m.cancelHealth = cancel
	go m.healthCheckLoop(ctx, cfg.Downloader.HealthInterval)

	m.setHealth(true, "Starting")
	m.log.Info("Downloader module started (target: %s)", cfg.Downloader.URL)
	return nil
}

func (m *Module) Stop(_ context.Context) error {
	m.log.Info("Stopping downloader module...")
	if m.cancelHealth != nil {
		m.cancelHealth()
	}
	done := make(chan struct{})
	go func() { m.scanWG.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		m.log.Warn("Background media rescan still running at shutdown; leaving to media module")
	}
	m.setHealth(false, "Stopped")
	return nil
}

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

// IsOnline returns whether the downloader service is reachable.
func (m *Module) IsOnline() bool {
	m.healthMu.RLock()
	defer m.healthMu.RUnlock()
	return m.online
}

// GetClient returns the downloader HTTP client.
func (m *Module) GetClient() *Client {
	return m.client
}

// ListImportable returns files in the downloader's downloads directory
// that are completed and ready to import.
func (m *Module) ListImportable() ([]ImportableFile, error) {
	cfg := m.config.Get()
	if cfg == nil {
		return nil, fmt.Errorf("config not available")
	}
	if cfg.Downloader.DownloadsDir == "" {
		return nil, ErrDownloadsDirNotConfigured
	}
	return ListImportableFiles(cfg.Downloader.DownloadsDir)
}

// defaultImportDir returns the destination used when the caller doesn't pick
// one: the configured import dir, falling back to the uploads dir.
func defaultImportDir(cfg *config.Config) string {
	if cfg.Downloader.ImportDir != "" {
		return cfg.Downloader.ImportDir
	}
	return cfg.Directories.Uploads
}

// importDirUnderRoot reports whether a configured downloader.import_dir is empty
// (falls back to uploads — always fine) or nested under a configured library
// root. The media scanner only walks videos/music/uploads, so files imported
// outside every root land on disk but never appear in the library; this drives a
// startup warning so a mis-set import_dir is diagnosable.
func importDirUnderRoot(cfg *config.Config) bool {
	importDir := cfg.Downloader.ImportDir
	if importDir == "" {
		return true
	}
	abs, err := filepath.Abs(importDir)
	if err != nil {
		abs = filepath.Clean(importDir)
	}
	for _, root := range []string{cfg.Directories.Videos, cfg.Directories.Music, cfg.Directories.Uploads} {
		if root == "" {
			continue
		}
		rabs, rerr := filepath.Abs(root)
		if rerr != nil {
			rabs = filepath.Clean(root)
		}
		if abs == rabs || strings.HasPrefix(abs, rabs+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

// ImportDestinations returns the library locations a download can be imported
// into (library roots + their sub-directories), with the default flagged.
func (m *Module) ImportDestinations() []ImportDestination {
	cfg := m.config.Get()
	if cfg == nil {
		return nil
	}
	return ListDestinations(
		cfg.Directories.Videos,
		cfg.Directories.Music,
		cfg.Directories.Uploads,
		defaultImportDir(cfg),
	)
}

// Import moves a file from the downloader's downloads directory into a library
// destination and optionally triggers a media library rescan. An empty (or
// "default") destination uses the configured import/uploads dir; any other value
// must match a key from ImportDestinations. An optional subfolder (a single safe
// name) creates/uses a new directory under the chosen destination. Returns the
// destination path and whether the source file was deleted.
func (m *Module) Import(filename, destination, subfolder string, deleteSource, triggerScan bool) (destPath string, sourceDeleted bool, err error) {
	cfg := m.config.Get()
	if cfg == nil {
		return "", false, fmt.Errorf("config not available")
	}
	if cfg.Downloader.DownloadsDir == "" {
		return "", false, ErrDownloadsDirNotConfigured
	}

	var destDir string
	if destination == "" || destination == "default" {
		destDir = defaultImportDir(cfg)
	} else {
		destDir, err = ResolveDestination(
			destination,
			cfg.Directories.Videos,
			cfg.Directories.Music,
			cfg.Directories.Uploads,
			defaultImportDir(cfg),
		)
		if err != nil {
			return "", false, err
		}
	}
	if destDir == "" {
		return "", false, ErrNoImportDestination
	}

	// Optional new sub-folder under the chosen destination. ImportFile MkdirAlls
	// the final dir, so a not-yet-existing sub-folder is created on import.
	cleanSub, err := sanitizeSubfolder(subfolder)
	if err != nil {
		return "", false, err
	}
	if cleanSub != "" {
		destDir = filepath.Join(destDir, cleanSub)
	}

	// Pre-flight writability check: catch a read-only destination (e.g. a HiDrive
	// mount mounted --read-only) here, with a clear message, instead of letting
	// the admin discover it as a raw EROFS after the copy is attempted.
	if !m.destinationWritableCached(destDir) {
		return "", false, ErrDestinationReadOnly
	}

	// Always import as a copy; source removal is handled below. Moving the file
	// out of the downloads dir here (the old deleteSource path) before telling the
	// downloader makes the downloader's DELETE 404 — it returns an error and keeps
	// its in-memory record, so the download lingers in the "Server Files" list
	// until restart. Copying first lets the downloader delete its own file and drop
	// the record while the file still exists.
	destPath, _, err = ImportFile(cfg.Downloader.DownloadsDir, destDir, filename, false)
	if err != nil {
		return "", false, err
	}

	m.log.Info("Imported %s → %s", filename, destPath)

	if deleteSource {
		sourceDeleted = m.clearImportedSource(cfg.Downloader.DownloadsDir, filename)
	}

	if triggerScan && m.mediaModule != nil {
		m.scanWG.Go(func() {
			if err := m.mediaModule.Scan(); err != nil {
				m.log.Warn("Media rescan after import failed: %v", err)
				return
			}
			m.log.Info("Media rescan triggered after import")
			if m.postScanHook != nil {
				m.postScanHook()
			}
		})
	}

	return destPath, sourceDeleted, nil
}

// clearImportedSource removes a just-copied download from the downloads dir after
// an import. It asks the downloader to delete the file first so the downloader
// also drops its in-memory tracking record (the "Server Files"/Downloads list);
// deleting the file from disk first would make that DELETE 404 and leave a stale
// record. Falls back to removing the file directly when the downloader can't
// (offline, not tracking this file, or any error). Returns whether the source is
// gone afterwards.
func (m *Module) clearImportedSource(downloadsDir, filename string) bool {
	if m.client != nil {
		if err := m.client.DeleteDownload(filename); err == nil {
			return true
		} else {
			m.log.Debug("Downloader could not delete %s (%v); removing source directly", filename, err)
		}
	}
	srcPath := filepath.Join(downloadsDir, filepath.Base(filename))
	if err := os.Remove(srcPath); err != nil {
		if os.IsNotExist(err) {
			return true // already gone — treat as deleted
		}
		m.log.Warn("Could not remove source %s after import: %v", srcPath, err)
		return false
	}
	return true
}

func (m *Module) setHealth(healthy bool, msg string) {
	m.healthMu.Lock()
	defer m.healthMu.Unlock()
	m.healthy = healthy
	m.healthMsg = msg
}

func (m *Module) setOnline(online bool) {
	m.healthMu.Lock()
	defer m.healthMu.Unlock()
	m.online = online
}

func (m *Module) healthCheckLoop(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = defaultHealthCheckInterval
	}

	// Initial check
	m.checkHealth()

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.checkHealth()
		}
	}
}

func (m *Module) checkHealth() {
	if m.client == nil {
		m.setOnline(false)
		m.setHealth(false, "No client configured")
		return
	}

	health, err := m.client.Health()
	if err != nil {
		wasOnline := m.IsOnline()
		m.setOnline(false)
		m.setHealth(true, "Downloader offline")
		if wasOnline {
			m.log.Warn("Downloader went offline: %v", err)
		}
		return
	}

	m.setOnline(true)
	msg := fmt.Sprintf("Online — %d active, %d queued", health.ActiveDownloads, health.QueuedDownloads)
	m.setHealth(true, msg)
}
