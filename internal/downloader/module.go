// Package downloader provides integration with the standalone media downloader
// service. It acts as a proxy — forwarding HTTP API calls and WebSocket
// connections to the downloader (running on localhost), and providing file
// import capabilities to move completed downloads into MSP's media library.
package downloader

import (
	"context"
	"fmt"
	"sync"
	"time"

	"media-server-pro/internal/config"
	"media-server-pro/internal/logger"
	"media-server-pro/internal/media"
	"media-server-pro/pkg/helpers"
	"media-server-pro/pkg/models"
)

// Module manages the connection to the external downloader service.
type Module struct {
	config      *config.Manager
	log         *logger.Logger
	client      *Client
	mediaModule *media.Module

	healthMu  sync.RWMutex
	healthy   bool
	healthMsg string
	online    bool

	cancelHealth context.CancelFunc
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

func (m *Module) Name() string { return "downloader" }

func (m *Module) Start(_ context.Context) error {
	m.log.Info("Starting downloader module...")

	cfg := m.config.Get()
	if !cfg.Downloader.Enabled {
		m.log.Info("Downloader integration is disabled")
		m.setHealth(true, "Disabled")
		return nil
	}

	if cfg.Downloader.URL == "" {
		m.setHealth(false, "No downloader URL configured")
		return nil
	}

	m.client = NewClient(cfg.Downloader.URL, cfg.Downloader.RequestTimeout)

	if cfg.Downloader.DownloadsDir == "" {
		m.log.Warn("Downloader downloads_dir not configured — file import will be unavailable")
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
	if cfg.Downloader.DownloadsDir == "" {
		return nil, fmt.Errorf("downloads_dir not configured")
	}
	return ListImportableFiles(cfg.Downloader.DownloadsDir)
}

// Import moves a file from the downloader's downloads directory to MSP's
// import directory and optionally triggers a media library rescan.
func (m *Module) Import(filename string, deleteSource bool, triggerScan bool) (string, error) {
	cfg := m.config.Get()
	if cfg.Downloader.DownloadsDir == "" {
		return "", fmt.Errorf("downloads_dir not configured")
	}

	destDir := cfg.Downloader.ImportDir
	if destDir == "" {
		destDir = cfg.Directories.Uploads
	}
	if destDir == "" {
		return "", fmt.Errorf("no import destination configured (set downloader.import_dir or directories.uploads)")
	}

	destPath, err := ImportFile(cfg.Downloader.DownloadsDir, destDir, filename, deleteSource)
	if err != nil {
		return "", err
	}

	m.log.Info("Imported %s → %s", filename, destPath)

	if triggerScan && m.mediaModule != nil {
		go func() {
			if err := m.mediaModule.Scan(); err != nil {
				m.log.Warn("Media rescan after import failed: %v", err)
			} else {
				m.log.Info("Media rescan triggered after import")
			}
		}()
	}

	return destPath, nil
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
		interval = 30 * time.Second
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
