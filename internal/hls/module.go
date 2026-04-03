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
	"path/filepath"
	"runtime/debug"
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
	"media-server-pro/pkg/storage"
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
	config             *config.Manager
	log                *logger.Logger
	dbModule           *database.Module
	repo               repositories.HLSJobRepository
	jobs               map[string]*models.HLSJob
	jobCancels         map[string]context.CancelFunc
	jobsMu             sync.RWMutex
	transSem           chan struct{}
	healthy            bool
	healthMsg          string
	healthMu           sync.RWMutex
	cacheDir           string
	cleanupTicker      *time.Ticker
	cleanupDone        chan struct{}
	cleanupDoneOnce    sync.Once
	ffmpegPath         string
	ffprobePath        string
	accessTracker      *AccessTracker
	activeJobs         sync.WaitGroup     // Tracks active transcoding jobs for graceful shutdown
	stopping           atomic.Bool        // Set to true during Stop() to distinguish cancellation from real failures
	qualityLocks       sync.Map           // Per-quality locks for lazy transcoding (key: "jobID/quality" → *sync.Mutex)
	store              storage.Backend    // optional storage backend for HLS cache I/O
	mediaInputResolver MediaInputResolver // resolves S3 media keys to ffmpeg-readable URLs
}

// MediaInputResolver converts a stored media path (possibly an S3 key) to a
// form that ffmpeg can read — an absolute local path or a presigned HTTPS URL.
type MediaInputResolver interface {
	ResolveForFFmpeg(ctx context.Context, mediaPath string) (string, error)
}

// SetStore sets the storage backend for HLS cache I/O.
func (m *Module) SetStore(s storage.Backend) {
	m.store = s
}

// SetMediaInputResolver sets the resolver used to convert S3 media keys to
// ffmpeg-readable URLs (presigned GET URLs). Must be called before Start().
func (m *Module) SetMediaInputResolver(r MediaInputResolver) {
	m.mediaInputResolver = r
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
		accessTracker: &AccessTracker{lastAccess: make(map[string]time.Time), lastSaved: make(map[string]time.Time)},
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

	// Automatic cleanup is intentionally disabled: HLS cache is never deleted
	// without an explicit admin action (POST /api/admin/hls/clean/inactive or
	// DELETE /api/admin/hls/jobs/:id). The CleanupEnabled config flag is ignored.

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

// runPostLoadStartupTasks loads job state, discovers existing jobs on disk, cleans stale lock files, and resumes interrupted jobs.
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
	resumed := m.resumeInterruptedJobs()
	if resumed > 0 {
		m.log.Info("Resumed %d interrupted HLS jobs from previous run", resumed)
	}
}

// resumeInterruptedJobs re-enqueues pending or retryable-failed jobs that were
// interrupted by a previous shutdown. Called once during startup after loadJobs
// and cleanLocksOnStartup have run. Resumes at most ConcurrentLimit jobs to
// avoid overloading the system on startup; remaining jobs will be picked up
// by the hls-pregenerate background task in subsequent cycles.
func (m *Module) resumeInterruptedJobs() int {
	cfg := m.config.Get()
	limit := cfg.HLS.ConcurrentLimit
	if limit <= 0 {
		limit = 2
	}

	m.jobsMu.Lock()
	defer m.jobsMu.Unlock()

	resumed := 0
	for _, job := range m.jobs {
		if resumed >= limit {
			break
		}
		if !m.shouldResumeJob(job) {
			continue
		}
		// Only verify existence for absolute local paths. S3 object keys
		// (e.g. "videos/foo.mp4") are not absolute and cannot be checked
		// with os.Stat; they will be validated by ffmpeg at transcode time.
		if filepath.IsAbs(job.MediaPath) {
			if _, err := os.Stat(job.MediaPath); err != nil {
				m.log.Warn("Skipping resume of HLS job %s: media file no longer exists at %s", job.ID, job.MediaPath)
				job.Status = models.HLSStatusFailed
				job.Error = "Media file not found on startup resume"
				continue
			}
		}
		m.log.Info("Resuming interrupted HLS job %s for %s", job.ID, job.MediaPath)
		jobCtx, jobCancel := context.WithCancel(context.Background())
		m.jobCancels[job.ID] = jobCancel
		job.Status = models.HLSStatusPending
		job.Error = ""
		capturedJob := job
		m.activeJobs.Add(1)
		go func() {
			defer m.activeJobs.Done()
			defer func() {
				if r := recover(); r != nil {
					m.log.Error("Panic in resumed HLS transcode for job %s: %v\n%s", capturedJob.ID, r, debug.Stack())
					m.updateJobStatus(&updateJobStatusParams{JobID: capturedJob.ID, Status: models.HLSStatusFailed, ErrorMsg: fmt.Sprintf("Internal error: %v", r), Progress: 0})
				}
			}()
			m.transcode(jobCtx, capturedJob)
		}()
		resumed++
	}
	return resumed
}

// shouldResumeJob returns true if a job should be re-enqueued on startup.
func (m *Module) shouldResumeJob(job *models.HLSJob) bool {
	switch job.Status {
	case models.HLSStatusPending, models.HLSStatusRunning:
		return true
	case models.HLSStatusFailed:
		return job.FailCount < maxHLSFailures
	default:
		return false
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

// cleanQualityLocks removes all qualityLocks entries for a given job ID.
// Keys are formatted as "jobID/quality". Uses sync.Map.Range to find and delete matching entries.
func (m *Module) cleanQualityLocks(jobID string) {
	prefix := jobID + "/"
	m.qualityLocks.Range(func(key, _ any) bool {
		if k, ok := key.(string); ok && len(k) > len(prefix) && k[:len(prefix)] == prefix {
			m.qualityLocks.Delete(key)
		}
		return true
	})
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
		if qp.Enabled {
			qualities = append(qualities, qp.Name)
		}
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
