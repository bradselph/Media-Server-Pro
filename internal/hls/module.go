// Package hls implements a single responsibility: the HLS (HTTP Live Streaming)
// transcoding and serving pipeline for the media server. All types and functions
// in this package serve that pipeline: job lifecycle (create, reuse, persist),
// ffmpeg transcoding and progress tracking, playlist generation and validation,
// lock and cleanup of cache directories, and HTTP delivery of playlists and segments.
package hls

import (
	"context"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"media-server-pro/internal/config"
	"media-server-pro/internal/database"
	"media-server-pro/internal/logger"
	"media-server-pro/internal/repositories"
	mysqlrepo "media-server-pro/internal/repositories/mysql"
	"media-server-pro/pkg/helpers"
	"media-server-pro/pkg/models"
)

const (
	errReadCacheDirFmt = "Failed to read HLS cache directory: %v"
	masterPlaylistName = "master.m3u8"
	errJobNotFoundFmt  = "job not found: %s"
	maxHLSFailures     = 3 // consecutive failures after which a job will not be auto-retried
)

// Capabilities holds information about what the HLS module can do
type Capabilities struct {
	Enabled       bool     `json:"enabled"`
	Available     bool     `json:"available"`
	FFmpegFound   bool     `json:"ffmpeg_found"`
	FFprobeFound  bool     `json:"ffprobe_found"`
	FFmpegPath    string   `json:"-"`
	Healthy       bool     `json:"healthy"`
	Message       string   `json:"message"`
	Qualities     []string `json:"qualities"`
	AutoGenerate  bool     `json:"auto_generate"`
	MaxConcurrent int      `json:"max_concurrent"`
}

// Module implements HLS transcoding and serving
type Module struct {
	config        *config.Manager
	log           *logger.Logger
	dbModule      *database.Module
	repo          repositories.HLSJobRepository
	jobs          map[string]*models.HLSJob
	jobCancels    map[string]context.CancelFunc
	jobsMu        sync.RWMutex
	transSem      chan struct{}
	healthy       bool
	healthMsg     string
	healthMu      sync.RWMutex
	cacheDir      string
	cleanupTicker   *time.Ticker
	cleanupDone     chan struct{}
	cleanupDoneOnce sync.Once
	ffmpegPath    string
	ffprobePath   string
	accessTracker *AccessTracker
	activeJobs    sync.WaitGroup // Tracks active transcoding jobs for graceful shutdown
	stopping      atomic.Bool    // Set to true during Stop() to distinguish cancellation from real failures
	qualityLocks  sync.Map       // Per-quality locks for lazy transcoding (key: "jobID/quality" → *sync.Mutex)
}

// NewModule creates a new HLS module
func NewModule(cfg *config.Manager, dbModule *database.Module) *Module {
	hlsCfg := cfg.Get().HLS
	concurrentLimit := hlsCfg.ConcurrentLimit
	if concurrentLimit <= 0 {
		concurrentLimit = 2
	}
	return &Module{
		config:        cfg,
		log:           logger.New("hls"),
		dbModule:      dbModule,
		jobs:          make(map[string]*models.HLSJob),
		jobCancels:    make(map[string]context.CancelFunc),
		transSem:      make(chan struct{}, concurrentLimit),
		cacheDir:      cfg.Get().Directories.HLSCache,
		cleanupDone:   make(chan struct{}),
		accessTracker: &AccessTracker{lastAccess: make(map[string]time.Time)},
	}
}

// Name returns the module name
func (m *Module) Name() string {
	return "hls"
}

// Start initializes the HLS module.
// If ffmpeg is not found, the module starts in degraded mode instead of failing,
// allowing the server to run with direct streaming as a fallback.
func (m *Module) Start(_ context.Context) error {
	m.log.Info("Starting HLS module...")

	m.repo = mysqlrepo.NewHLSJobRepository(m.dbModule.GORM())
	cfg := m.config.Get()

	if m.applyStartupHealthDisabled(cfg) {
		return nil
	}
	if m.applyStartupHealthFFmpeg() {
		return nil
	}
	if m.applyStartupHealthCacheDir() {
		return nil
	}

	m.runPostLoadStartupTasks()

	if cfg.HLS.CleanupEnabled {
		m.cleanupTicker = time.NewTicker(cfg.HLS.CleanupInterval)
		go m.cleanupLoop()
	}

	m.healthMu.Lock()
	m.healthy = true
	m.healthMsg = "Running"
	m.healthMu.Unlock()
	m.log.Info("HLS module started (ffmpeg-go transcoding available)")
	return nil
}

// applyStartupHealthDisabled sets health to disabled and returns true if HLS is disabled in config.
func (m *Module) applyStartupHealthDisabled(cfg *config.Config) bool {
	if cfg.HLS.Enabled {
		return false
	}
	m.log.Info("HLS is disabled in configuration, module running in pass-through mode")
	m.healthMu.Lock()
	m.healthy = true
	m.healthMsg = "Disabled (direct streaming only)"
	m.healthMu.Unlock()
	return true
}

// applyStartupHealthFFmpeg locates ffmpeg (and optionally ffprobe), sets module paths, and returns true if startup should stop (degraded).
func (m *Module) applyStartupHealthFFmpeg() bool {
	ffmpegPath, err := helpers.FindBinary("ffmpeg")
	if err != nil {
		m.log.Warn("ffmpeg not found: %v - HLS transcoding unavailable, falling back to direct streaming", err)
		m.healthMu.Lock()
		m.healthy = true
		m.healthMsg = "Degraded: ffmpeg not found (direct streaming only)"
		m.healthMu.Unlock()
		return true
	}
	m.ffmpegPath = ffmpegPath
	m.log.Info("Found ffmpeg at: %s", ffmpegPath)

	ffprobePath, err := helpers.FindBinary("ffprobe")
	if err != nil {
		m.log.Warn("ffprobe not found, progress tracking will use estimates")
	} else {
		m.ffprobePath = ffprobePath
		m.log.Info("Found ffprobe at: %s", ffprobePath)
	}
	return false
}

// applyStartupHealthCacheDir creates the HLS cache directory; on failure sets degraded health and returns true.
func (m *Module) applyStartupHealthCacheDir() bool {
	if err := os.MkdirAll(m.cacheDir, 0755); err != nil {
		m.log.Error("Failed to create HLS cache directory: %v", err)
		m.healthMu.Lock()
		m.healthy = true
		m.healthMsg = fmt.Sprintf("Degraded: cache dir error: %v", err)
		m.healthMu.Unlock()
		return true
	}
	return false
}

// runPostLoadStartupTasks loads job state, discovers existing jobs on disk, and cleans stale lock files.
func (m *Module) runPostLoadStartupTasks() {
	if err := m.loadJobs(); err != nil {
		m.log.Warn("Failed to load job state: %v", err)
	}
	discovered := m.discoverExistingJobs()
	if discovered > 0 {
		m.log.Info("Discovered %d existing HLS jobs on disk", discovered)
	}
	cleaned := m.cleanLocksOnStartup()
	if cleaned > 0 {
		m.log.Info("Cleaned %d stale HLS lock files from previous run", cleaned)
	}
}

// Stop gracefully stops the module
func (m *Module) Stop(ctx context.Context) error {
	m.log.Info("Stopping HLS module...")

	m.stopping.Store(true)

	if m.cleanupTicker != nil {
		m.cleanupTicker.Stop()
		m.cleanupDoneOnce.Do(func() { close(m.cleanupDone) })
	}

	m.jobsMu.Lock()
	for _, job := range m.jobs {
		if job.Status == models.HLSStatusRunning {
			job.Status = models.HLSStatusCancelled
		}
	}
	for id, cancel := range m.jobCancels {
		m.log.Debug("Cancelling HLS job: %s", id)
		cancel()
		delete(m.jobCancels, id)
	}
	m.jobsMu.Unlock()

	m.log.Info("Waiting for active HLS transcoding jobs to stop...")
	done := make(chan struct{})
	go func() {
		m.activeJobs.Wait()
		close(done)
	}()

	select {
	case <-done:
		m.log.Info("All HLS transcoding jobs stopped")
	case <-ctx.Done():
		m.log.Warn("HLS shutdown timeout - some ffmpeg processes may still be running")
	}

	if err := m.saveJobs(); err != nil {
		m.log.Error("Failed to save job state: %v", err)
	}

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

// IsAvailable returns true if HLS transcoding is available (ffmpeg found and module enabled)
func (m *Module) IsAvailable() bool {
	cfg := m.config.Get()
	return cfg.HLS.Enabled && m.ffmpegPath != ""
}

// GetCapabilities returns the current HLS module capabilities for the frontend
func (m *Module) GetCapabilities() Capabilities {
	cfg := m.config.Get()
	m.healthMu.RLock()
	defer m.healthMu.RUnlock()

	qualities := make([]string, 0, len(cfg.HLS.QualityProfiles))
	for _, qp := range cfg.HLS.QualityProfiles {
		qualities = append(qualities, qp.Name)
	}

	return Capabilities{
		Enabled:       cfg.HLS.Enabled,
		Available:     cfg.HLS.Enabled && m.ffmpegPath != "",
		FFmpegFound:   m.ffmpegPath != "",
		FFprobeFound:  m.ffprobePath != "",
		FFmpegPath:    m.ffmpegPath,
		Healthy:       m.healthy,
		Message:       m.healthMsg,
		Qualities:     qualities,
		AutoGenerate:  cfg.HLS.AutoGenerate,
		MaxConcurrent: cfg.HLS.ConcurrentLimit,
	}
}
