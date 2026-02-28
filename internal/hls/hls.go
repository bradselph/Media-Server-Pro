// Package hls provides HTTP Live Streaming (HLS) transcoding and serving.
// It handles on-demand transcoding, segment generation, and playlist management.
package hls

import (
	"bufio"
	"bytes"
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
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

	ffmpeg "github.com/u2takey/ffmpeg-go"
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

const (
	errReadCacheDirFmt = "Failed to read HLS cache directory: %v"
	masterPlaylistName = "master.m3u8"
	errJobNotFoundFmt  = "job not found: %s"
)

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
	cleanupTicker *time.Ticker
	cleanupDone   chan struct{}
	ffmpegPath    string
	ffprobePath   string
	accessTracker *AccessTracker
	activeJobs    sync.WaitGroup // Tracks active transcoding jobs for graceful shutdown
	stopping      atomic.Bool    // Set to true during Stop() to distinguish cancellation from real failures
}

// NewModule creates a new HLS module
func NewModule(cfg *config.Manager, dbModule *database.Module) *Module {
	hlsCfg := cfg.Get().HLS
	return &Module{
		config:      cfg,
		log:         logger.New("hls"),
		dbModule:    dbModule,
		jobs:        make(map[string]*models.HLSJob),
		jobCancels:  make(map[string]context.CancelFunc),
		transSem:    make(chan struct{}, hlsCfg.ConcurrentLimit),
		cacheDir:    cfg.Get().Directories.HLSCache,
		cleanupDone: make(chan struct{}),
		accessTracker: &AccessTracker{
			lastAccess: make(map[string]time.Time),
		},
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

	// Check if HLS is enabled in config
	if !cfg.HLS.Enabled {
		m.log.Info("HLS is disabled in configuration, module running in pass-through mode")
		m.healthMu.Lock()
		m.healthy = true
		m.healthMsg = "Disabled (direct streaming only)"
		m.healthMu.Unlock()
		return nil
	}

	// Check for ffmpeg
	ffmpegPath, err := helpers.FindBinary("ffmpeg")
	if err != nil {
		m.log.Warn("ffmpeg not found: %v - HLS transcoding unavailable, falling back to direct streaming", err)
		m.healthMu.Lock()
		m.healthy = true // Module is healthy, just degraded
		m.healthMsg = "Degraded: ffmpeg not found (direct streaming only)"
		m.healthMu.Unlock()
		// Don't return error - allow server to start without HLS transcoding
		return nil
	}
	m.ffmpegPath = ffmpegPath
	m.log.Info("Found ffmpeg at: %s", ffmpegPath)

	// Check for ffprobe (optional, used for progress tracking)
	ffprobePath, err := helpers.FindBinary("ffprobe")
	if err != nil {
		m.log.Warn("ffprobe not found, progress tracking will use estimates")
	} else {
		m.ffprobePath = ffprobePath
		m.log.Info("Found ffprobe at: %s", ffprobePath)
	}

	// Ensure cache directory exists
	if err := os.MkdirAll(m.cacheDir, 0755); err != nil {
		m.log.Error("Failed to create HLS cache directory: %v", err)
		m.healthMu.Lock()
		m.healthy = true
		m.healthMsg = fmt.Sprintf("Degraded: cache dir error: %v", err)
		m.healthMu.Unlock()
		return nil
	}

	// Load existing jobs state
	if err := m.loadJobs(); err != nil {
		m.log.Warn("Failed to load job state: %v", err)
	}

	// Discover existing HLS files on disk (for pregenerated content, recovery after crashes, etc.)
	discovered := m.discoverExistingJobs()
	if discovered > 0 {
		m.log.Info("Discovered %d existing HLS jobs on disk", discovered)
	}

	// Clean up ALL lock files from previous runs. At startup, no transcoding is
	// in progress, so every lock is stale regardless of age.
	if cleaned := m.cleanLocksOnStartup(); cleaned > 0 {
		m.log.Info("Cleaned %d stale HLS lock files from previous run", cleaned)
	}

	// Start cleanup goroutine
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

// Stop gracefully stops the module
func (m *Module) Stop(ctx context.Context) error {
	m.log.Info("Stopping HLS module...")

	// Signal that the module is stopping so transcoding goroutines can
	// distinguish intentional cancellation from real ffmpeg failures.
	m.stopping.Store(true)

	// Stop cleanup
	if m.cleanupTicker != nil {
		m.cleanupTicker.Stop()
		close(m.cleanupDone)
	}

	// Cancel active transcoding jobs and invoke their cancel functions
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

	// Wait for all active transcoding jobs to finish with timeout
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

	// Save jobs state
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

// cleanupLoop periodically removes old HLS segments
func (m *Module) cleanupLoop() {
	for {
		select {
		case <-m.cleanupTicker.C:
			m.cleanupOldSegments()
		case <-m.cleanupDone:
			return
		}
	}
}

// cleanupOldSegments removes HLS segments older than retention period
func (m *Module) cleanupOldSegments() {
	cfg := m.config.Get()
	retention := time.Duration(cfg.HLS.RetentionMinutes) * time.Minute
	cutoff := time.Now().Add(-retention)

	m.log.Debug("Cleaning up HLS segments older than %v", retention)

	entries, err := os.ReadDir(m.cacheDir)
	if err != nil {
		m.log.Error(errReadCacheDirFmt, err)
		return
	}

	removed := 0
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		path := filepath.Join(m.cacheDir, entry.Name())
		info, err := entry.Info()
		if err != nil {
			continue
		}

		if info.ModTime().Before(cutoff) {
			if err := os.RemoveAll(path); err != nil {
				m.log.Warn("Failed to remove HLS directory %s: %v", path, err)
			} else {
				jobID := entry.Name()
				m.jobsMu.Lock()
				delete(m.jobs, jobID)
				m.jobsMu.Unlock()
				m.accessTracker.mu.Lock()
				delete(m.accessTracker.lastAccess, jobID)
				m.accessTracker.mu.Unlock()
				removed++
			}
		}
	}

	if removed > 0 {
		m.log.Info("Cleaned up %d old HLS directories", removed)
	}
}

// GenerateHLS starts HLS transcoding for a media file
func (m *Module) GenerateHLS(ctx context.Context, mediaPath string, qualities []string) (*models.HLSJob, error) {
	// Check if HLS transcoding is available
	if !m.IsAvailable() {
		if m.ffmpegPath == "" {
			return nil, fmt.Errorf("HLS transcoding unavailable: ffmpeg not found. Use direct streaming instead")
		}
		return nil, fmt.Errorf("HLS transcoding is disabled in server configuration")
	}

	// Generate job ID from path
	hash := md5.Sum([]byte(mediaPath))
	jobID := hex.EncodeToString(hash[:])

	// Verify file exists before acquiring lock
	if _, err := os.Stat(mediaPath); err != nil {
		return nil, fmt.Errorf("media file not found: %w", err)
	}

	// Prepare output directory path
	outputDir := filepath.Join(m.cacheDir, jobID)

	// Use default qualities if none specified
	if len(qualities) == 0 {
		cfg := m.config.Get()
		for _, qp := range cfg.HLS.QualityProfiles {
			qualities = append(qualities, qp.Name)
		}
	}

	// Filter out quality levels that exceed the source video's native resolution
	// to avoid wasting CPU/storage on pointless upscales.
	if sourceHeight := m.getSourceHeight(ctx, mediaPath); sourceHeight > 0 {
		filtered := make([]string, 0, len(qualities))
		for _, q := range qualities {
			profile := m.getQualityProfile(q)
			if profile == nil || profile.Height <= sourceHeight {
				filtered = append(filtered, q)
			}
		}
		if len(filtered) > 0 {
			if len(filtered) < len(qualities) {
				m.log.Info("Source %s is %dpx tall — skipping upscale qualities, generating: %v",
					filepath.Base(mediaPath), sourceHeight, filtered)
			}
			qualities = filtered
		}
		// If all configured profiles exceed source height, keep them all (best-effort downscale)
	}

	// Atomically check-and-create job under a single write lock
	m.jobsMu.Lock()
	defer m.jobsMu.Unlock()

	// maxHLSFailures is the number of consecutive failures after which a job
	// will no longer be automatically retried.  This prevents the server from
	// endlessly re-queuing videos that ffmpeg cannot transcode.
	const maxHLSFailures = 3

	// Check if job already exists (now under write lock)
	if existing, ok := m.jobs[jobID]; ok {
		switch existing.Status {
		case models.HLSStatusCompleted, models.HLSStatusRunning:
			return existing, nil
		case models.HLSStatusFailed:
			if existing.FailCount >= maxHLSFailures {
				return existing, fmt.Errorf("HLS generation for %s has failed %d times and will not be retried automatically; use the admin panel to reset", mediaPath, existing.FailCount)
			}
			// Not yet at the limit – allow a retry below
		}
		// Pending/cancelled: allow re-queue below
	}

	// Check if HLS files already exist on disk (even if not in jobs map)
	// This handles cases where files were pregenerated or jobs.json was lost
	if m.validateExistingHLS(outputDir, qualities) {
		m.log.Info("Found existing valid HLS content for %s, reusing files", jobID)

		// Create job entry for the existing content
		now := time.Now()
		job := &models.HLSJob{
			ID:          jobID,
			MediaPath:   mediaPath,
			OutputDir:   outputDir,
			Status:      models.HLSStatusCompleted,
			Progress:    100,
			Qualities:   qualities,
			StartedAt:   now.Add(-1 * time.Hour), // Estimate
			CompletedAt: &now,
		}
		m.jobs[jobID] = job

		// Save job state to persist the discovery
		if err := m.saveJobs(); err != nil {
			m.log.Warn("Failed to save job state after discovering existing HLS: %v", err)
		}

		return job, nil
	}

	// If output directory exists but validation failed (partial/corrupted files),
	// clean it up before regenerating to avoid conflicts
	if _, err := os.Stat(outputDir); err == nil {
		m.log.Warn("Output directory exists but HLS validation failed, cleaning up before regeneration: %s", outputDir)
		if err := os.RemoveAll(outputDir); err != nil {
			m.log.Error("Failed to clean up corrupted HLS directory: %v", err)
		}
	}

	// Create output directory
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create output directory: %w", err)
	}

	// Create job
	job := &models.HLSJob{
		ID:        jobID,
		MediaPath: mediaPath,
		OutputDir: outputDir,
		Status:    models.HLSStatusPending,
		Progress:  0,
		Qualities: qualities,
		StartedAt: time.Now(),
	}

	// Create a cancellable context for this job
	// Use Background() instead of the HTTP request context to allow long-running transcoding
	jobCtx, jobCancel := context.WithCancel(context.Background())

	m.jobs[jobID] = job
	m.jobCancels[jobID] = jobCancel

	// Start transcoding in background with panic recovery and WaitGroup tracking
	m.activeJobs.Add(1)
	go func() {
		defer m.activeJobs.Done()
		defer func() {
			if r := recover(); r != nil {
				m.log.Error("Panic in HLS transcode for job %s: %v", jobID, r)
				m.updateJobStatus(jobID, models.HLSStatusFailed,
					fmt.Sprintf("Internal error: %v", r), 0)
			}
		}()
		m.transcode(jobCtx, job)
	}()

	m.log.Info("Started HLS generation for %s (job: %s)", mediaPath, jobID)
	return job, nil
}

// transcode performs the actual transcoding
func (m *Module) transcode(ctx context.Context, job *models.HLSJob) {
	// Acquire semaphore
	select {
	case m.transSem <- struct{}{}:
		defer func() { <-m.transSem }()
	case <-ctx.Done():
		m.updateJobStatus(job.ID, models.HLSStatusCancelled, "Context cancelled", 0)
		return
	}

	m.updateJobStatus(job.ID, models.HLSStatusRunning, "", 0)

	// Create lock file to prevent duplicate transcodes
	if err := m.createLock(job.ID, job.MediaPath); err != nil {
		m.log.Warn("Failed to create lock file: %v", err)
	}
	defer m.removeLock(job.ID)

	// Probe media duration for accurate progress tracking
	totalDuration := m.getMediaDuration(ctx, job.MediaPath)
	if totalDuration > 0 {
		m.log.Debug("Media duration for %s: %.1fs", job.ID, totalDuration)
	}

	cfg := m.config.Get()

	// Generate variants for each quality
	var variantPlaylists []string
	for i, quality := range job.Qualities {
		profile := m.getQualityProfile(quality)
		if profile == nil {
			m.log.Warn("Unknown quality profile: %s", quality)
			continue
		}

		variantDir := filepath.Join(job.OutputDir, quality)
		if err := os.MkdirAll(variantDir, 0755); err != nil {
			m.updateJobStatus(job.ID, models.HLSStatusFailed, fmt.Sprintf("Failed to create variant dir: %v", err), 0)
			return
		}

		playlistPath := filepath.Join(variantDir, "playlist.m3u8")
		segmentPattern := filepath.Join(variantDir, "segment_%04d.ts")

		m.log.Info("Generating HLS variant %s (%dx%d @ %dkbps)", quality, profile.Width, profile.Height, profile.Bitrate/1000)

		// Build ffmpeg pipeline using ffmpeg-go
		stream := ffmpeg.Input(job.MediaPath)

		// Apply video filters and encoding
		stream = stream.Output(playlistPath,
			ffmpeg.KwArgs{
				// Video codec settings
				"c:v":          "libx264",
				"preset":       "fast",
				"vf":           fmt.Sprintf("scale=%d:%d", profile.Width, profile.Height),
				"b:v":          fmt.Sprintf("%dk", profile.Bitrate/1000),
				"maxrate":      fmt.Sprintf("%dk", profile.Bitrate/1000),
				"bufsize":      fmt.Sprintf("%dk", profile.Bitrate*2/1000),
				"g":            strconv.Itoa(cfg.HLS.SegmentDuration * 30), // GOP size (keyframe interval)
				"sc_threshold": "0",                                        // Disable scene change detection for consistent segment sizes

				// Audio codec settings
				"c:a": "aac",
				"b:a": fmt.Sprintf("%dk", profile.AudioBitrate/1000),
				"ac":  "2", // Stereo

				// HLS-specific settings
				"f":                    "hls",
				"hls_time":             strconv.Itoa(cfg.HLS.SegmentDuration),
				"hls_playlist_type":    "vod",
				"hls_segment_type":     "mpegts",
				"hls_list_size":        "0",
				"hls_segment_filename": segmentPattern,
				"hls_flags":            "independent_segments",
			},
		).OverWriteOutput().SetFfmpegPath(m.ffmpegPath)

		// Create a command from the stream
		cmd := stream.Compile()

		// Apply context for cancellation; use cmd.Path (absolute binary path set by
		// SetFfmpegPath) rather than cmd.Args[0] (bare "ffmpeg" name, PATH lookup).
		cmdWithContext := exec.CommandContext(ctx, cmd.Path, cmd.Args[1:]...)
		cmdWithContext.Env = cmd.Env
		cmdWithContext.Dir = cmd.Dir

		// Capture stderr for progress monitoring and error diagnostics.
		// io.TeeReader writes every byte read by monitorProgress into stderrBuf,
		// so we have the full ffmpeg output available after Wait() returns.
		var stderrBuf bytes.Buffer
		stderrPipe, err := cmdWithContext.StderrPipe()
		if err != nil {
			m.updateJobStatus(job.ID, models.HLSStatusFailed, fmt.Sprintf("Failed to create stderr pipe: %v", err), 0)
			return
		}

		if err := cmdWithContext.Start(); err != nil {
			m.updateJobStatus(job.ID, models.HLSStatusFailed, fmt.Sprintf("Failed to start ffmpeg: %v", err), 0)
			return
		}

		// Monitor progress from stderr, capturing all output for error diagnostics.
		progressDone := make(chan struct{})
		go func() {
			defer close(progressDone)
			m.monitorProgress(job.ID, io.TeeReader(stderrPipe, &stderrBuf), len(job.Qualities), i+1, totalDuration)
		}()

		waitErr := cmdWithContext.Wait()
		<-progressDone // ensure monitorProgress has finished reading before we inspect stderrBuf

		if waitErr != nil {
			// Treat as cancellation if the context is done (cancel() called), the
			// module is shutting down (Stop() called), or ffmpeg was killed by a
			// signal (e.g. SIGINT propagated to the process group on Ctrl+C).
			// All three cause ffmpeg to exit non-zero but none is a real failure.
			stderrStr := stderrBuf.String()
			signalKilled := strings.Contains(stderrStr, "Exiting normally, received signal")
			if ctx.Err() != nil || m.stopping.Load() || signalKilled {
				m.log.Info("HLS transcoding cancelled for job %s quality %s", job.ID, quality)
				m.updateJobStatus(job.ID, models.HLSStatusCancelled, "Transcoding cancelled", 0)
				return
			}
			// Real ffmpeg failure - log stderr output to help diagnose the cause.
			if errOutput := strings.TrimSpace(stderrStr); errOutput != "" {
				if len(errOutput) > 1000 {
					errOutput = "...(truncated)\n" + errOutput[len(errOutput)-1000:]
				}
				m.log.Error("ffmpeg stderr for job %s quality %s:\n%s", job.ID, quality, errOutput)
			}
			// Clean up partial output on failure
			m.log.Warn("Transcoding failed for job %s quality %s, cleaning up partial output", job.ID, quality)
			if removeErr := os.RemoveAll(variantDir); removeErr != nil {
				m.log.Error("Failed to clean up partial HLS variant at %s: %v", variantDir, removeErr)
			}
			m.updateJobStatus(job.ID, models.HLSStatusFailed, fmt.Sprintf("Transcoding failed for %s: %v", quality, waitErr), 0)
			return
		}

		// Verify the playlist was created
		if _, err := os.Stat(playlistPath); err != nil {
			m.log.Error("Playlist not created for quality %s: %v", quality, err)
			m.updateJobStatus(job.ID, models.HLSStatusFailed, fmt.Sprintf("Playlist not created for %s", quality), 0)
			return
		}

		m.log.Info("Successfully generated HLS variant %s", quality)
		variantPlaylists = append(variantPlaylists, quality)
	}

	// Generate master playlist
	if err := m.generateMasterPlaylist(job.OutputDir, variantPlaylists); err != nil {
		m.updateJobStatus(job.ID, models.HLSStatusFailed, fmt.Sprintf("Failed to create master playlist: %v", err), 0)
		return
	}

	// Mark as complete and clean up cancel function
	m.jobsMu.Lock()
	job.Status = models.HLSStatusCompleted
	job.Progress = 100
	completedAt := time.Now()
	job.CompletedAt = &completedAt
	delete(m.jobCancels, job.ID)
	m.jobsMu.Unlock()

	// Save job state immediately after completion for crash recovery
	if err := m.saveJobs(); err != nil {
		m.log.Warn("Failed to save job state after completion: %v", err)
	}

	m.log.Info("HLS generation completed for job %s", job.ID)
}

// monitorProgress monitors ffmpeg progress output and parses time= for progress tracking.
// totalDuration is the media duration in seconds (0 if unknown).
func (m *Module) monitorProgress(jobID string, stderr io.Reader, totalQualities, currentQuality int, totalDuration float64) {
	scanner := bufio.NewScanner(stderr)
	for scanner.Scan() {
		line := scanner.Text()
		// Parse time= from ffmpeg output (format: time=HH:MM:SS.mm or time=SS.mm)
		if idx := strings.Index(line, "time="); idx >= 0 {
			m.handleProgressUpdate(jobID, line[idx+5:], totalQualities, currentQuality, totalDuration)
		}
	}
}

// handleProgressUpdate processes a single ffmpeg progress line and updates job progress.
func (m *Module) handleProgressUpdate(jobID string, rawTimeStr string, totalQualities, currentQuality int, totalDuration float64) {
	timeStr := rawTimeStr
	if spaceIdx := strings.IndexAny(timeStr, " \t"); spaceIdx > 0 {
		timeStr = timeStr[:spaceIdx]
	}
	currentSecs := parseFFmpegTime(timeStr)
	baseProgress := float64(currentQuality-1) / float64(totalQualities) * 100
	qualityProgress := 100.0 / float64(totalQualities)

	variantPct := calculateVariantProgress(currentSecs, totalDuration)
	m.updateJobStatus(jobID, models.HLSStatusRunning, "", baseProgress+qualityProgress*variantPct)
}

// calculateVariantProgress computes the progress fraction for a single quality variant.
func calculateVariantProgress(currentSecs, totalDuration float64) float64 {
	if totalDuration > 0 && currentSecs > 0 {
		// Use actual duration for accurate progress
		pct := currentSecs / totalDuration
		if pct > 0.99 {
			pct = 0.99
		}
		return pct
	} else if currentSecs > 0 {
		// Fallback: estimate slowly approaching 95%
		pct := 0.5 + (currentSecs/7200.0)*0.45
		if pct > 0.95 {
			pct = 0.95
		}
		return pct
	}
	return 0
}

// parseFFmpegTime parses ffmpeg time format (HH:MM:SS.ms or seconds) to seconds
func parseFFmpegTime(timeStr string) float64 {
	// Try HH:MM:SS.ms format
	parts := strings.Split(timeStr, ":")
	if len(parts) == 3 {
		h, _ := strconv.ParseFloat(parts[0], 64)
		m, _ := strconv.ParseFloat(parts[1], 64)
		s, _ := strconv.ParseFloat(parts[2], 64)
		return h*3600 + m*60 + s
	}
	// Try plain seconds
	s, _ := strconv.ParseFloat(timeStr, 64)
	return s
}

// getMediaDuration uses ffmpeg-go's Probe to get media duration in seconds.
// Falls back to raw ffprobe if the ffmpeg-go probe fails.
func (m *Module) getMediaDuration(ctx context.Context, mediaId string) float64 {
	if m.ffprobePath == "" && m.ffmpegPath == "" {
		return 0
	}

	// Use ffmpeg-go Probe for duration detection
	probeJSON, err := ffmpeg.Probe(mediaId)
	if err == nil {
		duration := m.parseProbeDuration(probeJSON)
		if duration > 0 {
			return duration
		}
	}
	m.log.Debug("ffmpeg-go probe failed, trying raw ffprobe: %v", err)

	// Fallback to raw ffprobe if ffmpeg-go probe fails
	if m.ffprobePath == "" {
		return 0
	}

	probeCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	cmd := exec.CommandContext(probeCtx, m.ffprobePath,
		"-v", "quiet",
		"-print_format", "json",
		"-show_format",
		mediaId,
	)
	output, err := cmd.Output()
	if err != nil {
		m.log.Debug("Failed to probe media duration: %v", err)
		return 0
	}

	return m.parseProbeDuration(string(output))
}

// parseProbeDuration extracts duration from ffprobe JSON output
func (m *Module) parseProbeDuration(probeJSON string) float64 {
	var probe struct {
		Format struct {
			Duration string `json:"duration"`
		} `json:"format"`
	}
	if err := json.Unmarshal([]byte(probeJSON), &probe); err != nil {
		m.log.Debug("Failed to parse ffprobe JSON output: %v", err)
		return 0
	}
	duration, err := strconv.ParseFloat(probe.Format.Duration, 64)
	if err != nil {
		return 0
	}
	return duration
}

// getSourceHeight probes the source media file and returns the video stream height in pixels.
// Returns 0 if the height cannot be determined (ffprobe unavailable, not a video file, etc.).
// Uses ffmpeg-go's Probe first, then falls back to raw ffprobe if that fails.
func (m *Module) getSourceHeight(ctx context.Context, mediaId string) int {
	if m.ffmpegPath == "" && m.ffprobePath == "" {
		return 0
	}

	// ffmpeg-go Probe returns format + streams JSON in one call (same probe used for duration).
	probeJSON, err := ffmpeg.Probe(mediaId)
	if err == nil {
		if h := m.parseProbeHeight(probeJSON); h > 0 {
			return h
		}
	}

	// Fallback: raw ffprobe with -show_streams
	if m.ffprobePath == "" {
		return 0
	}

	probeCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	cmd := exec.CommandContext(probeCtx, m.ffprobePath,
		"-v", "quiet",
		"-print_format", "json",
		"-show_streams",
		"-select_streams", "v:0",
		mediaId,
	)
	output, err := cmd.Output()
	if err != nil {
		m.log.Debug("ffprobe stream info failed for %s: %v", filepath.Base(mediaId), err)
		return 0
	}

	return m.parseProbeHeight(string(output))
}

// parseProbeHeight extracts video stream height from ffprobe JSON output.
// Looks for the first stream with a non-zero height.
func (m *Module) parseProbeHeight(probeJSON string) int {
	var probe struct {
		Streams []struct {
			Height    int    `json:"height"`
			CodecType string `json:"codec_type"`
		} `json:"streams"`
	}
	if err := json.Unmarshal([]byte(probeJSON), &probe); err != nil {
		return 0
	}
	for _, s := range probe.Streams {
		if s.Height > 0 {
			return s.Height
		}
	}
	return 0
}

// getQualityProfile returns the quality profile by name
func (m *Module) getQualityProfile(name string) *config.HLSQuality {
	cfg := m.config.Get()
	for _, profile := range cfg.HLS.QualityProfiles {
		if profile.Name == name {
			return &profile
		}
	}
	return nil
}

// generateMasterPlaylist creates the master HLS playlist
func (m *Module) generateMasterPlaylist(outputDir string, variants []string) error {
	masterPath := filepath.Join(outputDir, masterPlaylistName)
	file, err := os.Create(masterPath)
	if err != nil {
		return err
	}
	defer func() {
		if err := file.Close(); err != nil {
			m.log.Warn("Failed to close master playlist file: %v", err)
		}
	}()

	if _, err := fmt.Fprintln(file, "#EXTM3U"); err != nil {
		if removeErr := os.Remove(masterPath); removeErr != nil {
			m.log.Warn("Failed to remove corrupted playlist %s: %v", masterPath, removeErr)
		}
		return fmt.Errorf("failed to write playlist header: %w", err)
	}
	if _, err := fmt.Fprintln(file, "#EXT-X-VERSION:3"); err != nil {
		if removeErr := os.Remove(masterPath); removeErr != nil {
			m.log.Warn("Failed to remove corrupted playlist %s: %v", masterPath, removeErr)
		}
		return fmt.Errorf("failed to write playlist version: %w", err)
	}

	for _, variant := range variants {
		profile := m.getQualityProfile(variant)
		if profile == nil {
			continue
		}

		if _, err := fmt.Fprintf(file, "#EXT-X-STREAM-INF:BANDWIDTH=%d,RESOLUTION=%dx%d,NAME=\"%s\"\n",
			profile.Bitrate+profile.AudioBitrate,
			profile.Width,
			profile.Height,
			variant,
		); err != nil {
			if removeErr := os.Remove(masterPath); removeErr != nil {
				m.log.Warn("Failed to remove corrupted playlist %s: %v", masterPath, removeErr)
			}
			return fmt.Errorf("failed to write stream info: %w", err)
		}
		if _, err := fmt.Fprintf(file, "%s/playlist.m3u8\n", variant); err != nil {
			if removeErr := os.Remove(masterPath); removeErr != nil {
				m.log.Warn("Failed to remove corrupted playlist %s: %v", masterPath, removeErr)
			}
			return fmt.Errorf("failed to write variant path: %w", err)
		}
	}

	return nil
}

// updateJobStatus updates a job's status.
// Transitioning to HLSStatusFailed automatically increments the job's FailCount.
func (m *Module) updateJobStatus(jobID string, status models.HLSStatus, errorMsg string, progress float64) {
	m.jobsMu.Lock()
	defer m.jobsMu.Unlock()

	job, ok := m.jobs[jobID]
	if !ok {
		return
	}

	if status == models.HLSStatusFailed {
		job.FailCount++
	}
	job.Status = status
	if errorMsg != "" {
		job.Error = errorMsg
	}
	if progress > 0 {
		job.Progress = progress
	}
}

// GetJobStatus returns the status of an HLS job
func (m *Module) GetJobStatus(jobID string) (*models.HLSJob, error) {
	m.jobsMu.RLock()
	defer m.jobsMu.RUnlock()

	job, ok := m.jobs[jobID]
	if !ok {
		return nil, fmt.Errorf(errJobNotFoundFmt, jobID)
	}
	return job, nil
}

// GetJobByMediaPath returns job for a media file by its path
func (m *Module) GetJobByMediaPath(mediaPath string) (*models.HLSJob, error) {
	hash := md5.Sum([]byte(mediaPath))
	jobID := hex.EncodeToString(hash[:])
	return m.GetJobStatus(jobID)
}

// HasHLS checks if completed HLS content exists for a media file (with disk verification)
func (m *Module) HasHLS(mediaPath string) bool {
	job, err := m.GetJobByMediaPath(mediaPath)
	if err != nil {
		return false
	}
	if job.Status != models.HLSStatusCompleted {
		return false
	}
	masterPath := filepath.Join(job.OutputDir, masterPlaylistName)
	_, statErr := os.Stat(masterPath)
	return statErr == nil
}

// CheckOrGenerateHLS checks if HLS exists for media path, auto-generates if configured
func (m *Module) CheckOrGenerateHLS(ctx context.Context, mediaPath string) (*models.HLSJob, error) {
	// Try to get existing job
	job, err := m.GetJobByMediaPath(mediaPath)
	if err == nil {
		// For completed jobs, verify the master playlist actually exists on disk.
		// In-memory state can be stale after a restart or if files were deleted.
		if job.Status == models.HLSStatusCompleted {
			masterPath := filepath.Join(job.OutputDir, masterPlaylistName)
			if _, statErr := os.Stat(masterPath); statErr != nil {
				m.log.Warn("HLS job %s marked complete but master.m3u8 missing from disk, will regenerate", job.ID)
				m.jobsMu.Lock()
				delete(m.jobs, job.ID)
				m.jobsMu.Unlock()
				// Fall through to auto-generate
			} else {
				return job, nil
			}
		} else {
			// Running, pending, or failed — return as-is
			return job, nil
		}
	}

	// Job doesn't exist (or was removed due to stale state) - check if AutoGenerate is enabled
	cfg := m.config.Get()
	if !cfg.HLS.AutoGenerate {
		return nil, fmt.Errorf("HLS not available and auto-generation is disabled")
	}

	// Auto-generate HLS
	m.log.Info("Auto-generating HLS for: %s", mediaPath)
	job, err = m.GenerateHLS(ctx, mediaPath, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to start HLS generation: %w", err)
	}

	return job, nil
}

// ServeMasterPlaylist serves the master HLS playlist
func (m *Module) ServeMasterPlaylist(w http.ResponseWriter, r *http.Request, jobID string) error {
	job, err := m.GetJobStatus(jobID)
	if err != nil {
		return err
	}

	if job.Status != models.HLSStatusCompleted {
		return fmt.Errorf("HLS not ready, status: %s", job.Status)
	}

	masterPath := filepath.Join(job.OutputDir, masterPlaylistName)
	w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	http.ServeFile(w, r, masterPath)
	return nil
}

// ServeVariantPlaylist serves a variant HLS playlist
func (m *Module) ServeVariantPlaylist(w http.ResponseWriter, r *http.Request, jobID, quality string) error {
	job, err := m.GetJobStatus(jobID)
	if err != nil {
		return err
	}

	playlistPath := filepath.Join(job.OutputDir, quality, "playlist.m3u8")
	if _, err := os.Stat(playlistPath); err != nil {
		return fmt.Errorf("variant playlist not found: %s", quality)
	}

	w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	http.ServeFile(w, r, playlistPath)
	return nil
}

// ServeSegment serves an HLS segment
func (m *Module) ServeSegment(w http.ResponseWriter, r *http.Request, jobID, quality, segment string) error {
	job, err := m.GetJobStatus(jobID)
	if err != nil {
		return err
	}

	segmentPath := filepath.Join(job.OutputDir, quality, segment)
	if _, err := os.Stat(segmentPath); err != nil {
		return fmt.Errorf("segment not found: %s", segment)
	}

	w.Header().Set("Content-Type", "video/mp2t")
	w.Header().Set("Cache-Control", "public, max-age=31536000")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	http.ServeFile(w, r, segmentPath)
	return nil
}

// ListJobs returns all HLS jobs
func (m *Module) ListJobs() []*models.HLSJob {
	m.jobsMu.RLock()
	defer m.jobsMu.RUnlock()

	jobs := make([]*models.HLSJob, 0, len(m.jobs))
	for _, job := range m.jobs {
		jobs = append(jobs, job)
	}
	return jobs
}

// CancelJob cancels a running job and kills the ffmpeg process.
func (m *Module) CancelJob(jobID string) error {
	m.jobsMu.Lock()
	defer m.jobsMu.Unlock()

	job, ok := m.jobs[jobID]
	if !ok {
		return fmt.Errorf(errJobNotFoundFmt, jobID)
	}

	if job.Status == models.HLSStatusRunning || job.Status == models.HLSStatusPending {
		job.Status = models.HLSStatusCancelled
		// Cancel the context to kill ffmpeg process
		if cancel, ok := m.jobCancels[jobID]; ok {
			cancel()
			delete(m.jobCancels, jobID)
		}
	}

	return nil
}

// DeleteJob removes a job and its files
func (m *Module) DeleteJob(jobID string) error {
	m.jobsMu.Lock()
	job, ok := m.jobs[jobID]
	if !ok {
		m.jobsMu.Unlock()
		return fmt.Errorf(errJobNotFoundFmt, jobID)
	}
	delete(m.jobs, jobID)
	m.jobsMu.Unlock()

	// Remove output directory
	if err := os.RemoveAll(job.OutputDir); err != nil {
		m.log.Warn("Failed to remove HLS directory: %v", err)
	}

	m.log.Info("Deleted HLS job %s", jobID)
	return nil
}

// Persistence — reads/writes via MySQL repository

func (m *Module) loadJobs() error {
	jobs, err := m.repo.List(context.Background())
	if err != nil {
		return err
	}

	m.jobsMu.Lock()
	defer m.jobsMu.Unlock()

	for _, job := range jobs {
		m.jobs[job.ID] = job
	}
	return nil
}

func (m *Module) saveJobs() error {
	m.jobsMu.RLock()
	defer m.jobsMu.RUnlock()

	ctx := context.Background()
	for _, job := range m.jobs {
		if err := m.repo.Save(ctx, job); err != nil {
			return err
		}
	}
	return nil
}

// saveJob persists a single job to the database.
func (m *Module) saveJob(job *models.HLSJob) {
	if err := m.repo.Save(context.Background(), job); err != nil {
		m.log.Error("Failed to persist HLS job %s: %v", job.ID, err)
	}
}

// SaveJobsToFile is a public wrapper for saveJobs() to allow external callers
// (like the pregenerate tool) to persist job state.
func (m *Module) SaveJobsToFile() error {
	return m.saveJobs()
}

// discoverExistingJobs scans the cache directory and creates job entries for existing HLS content.
// This allows the server to reuse HLS files generated by the pregenerate tool, previous server runs,
// or recover from crashes where jobs.json wasn't saved.
func (m *Module) discoverExistingJobs() int {
	entries, err := os.ReadDir(m.cacheDir)
	if err != nil {
		m.log.Debug("Failed to read HLS cache directory during discovery: %v", err)
		return 0
	}

	discovered := 0
	m.jobsMu.Lock()
	defer m.jobsMu.Unlock()

	for _, entry := range entries {
		if !entry.IsDir() || entry.Name() == "." || entry.Name() == ".." {
			continue
		}

		jobID := entry.Name()

		// Skip if we already have this job in memory
		if existing, ok := m.jobs[jobID]; ok {
			if existing.Status == models.HLSStatusCompleted {
				continue // Already tracked and complete
			}
			// If job exists but isn't completed, we'll verify and potentially update it
		}

		outputDir := filepath.Join(m.cacheDir, jobID)

		// Check if master playlist exists
		masterPath := filepath.Join(outputDir, masterPlaylistName)
		if _, err := os.Stat(masterPath); err != nil {
			m.log.Debug("Skipping job %s: no master playlist found", jobID)
			continue
		}

		// Parse master playlist to find variants
		masterData, err := os.ReadFile(masterPath)
		if err != nil {
			m.log.Debug("Skipping job %s: failed to read master playlist: %v", jobID, err)
			continue
		}

		variants := m.parseVariantStreams(string(masterData))
		if len(variants) == 0 {
			m.log.Debug("Skipping job %s: no variants in master playlist", jobID)
			continue
		}

		// Extract quality names from variant paths (e.g., "480p/playlist.m3u8" -> "480p")
		qualities := make([]string, 0, len(variants))
		allVariantsValid := true
		for _, variantPath := range variants {
			qualityName := filepath.Dir(variantPath)
			qualities = append(qualities, qualityName)

			// Verify variant playlist exists
			fullVariantPath := filepath.Join(outputDir, variantPath)
			if _, err := os.Stat(fullVariantPath); err != nil {
				m.log.Debug("Variant %s missing for job %s", variantPath, jobID)
				allVariantsValid = false
				break
			}
		}

		if !allVariantsValid {
			m.log.Debug("Skipping job %s: incomplete variants", jobID)
			continue
		}

		// Try to determine media path from existing job or lock file
		mediaPath := m.findMediaPathForJob(jobID, outputDir)

		// Create or update job entry
		info, err := entry.Info()
		if err != nil {
			m.log.Warn("Failed to stat HLS dir %s: %v", entry.Name(), err)
			continue
		}
		completedTime := info.ModTime()

		job := &models.HLSJob{
			ID:          jobID,
			MediaPath:   mediaPath,
			OutputDir:   outputDir,
			Status:      models.HLSStatusCompleted,
			Progress:    100,
			Qualities:   qualities,
			StartedAt:   completedTime.Add(-1 * time.Hour), // Estimate start time
			CompletedAt: &completedTime,
		}

		m.jobs[jobID] = job
		discovered++
		m.log.Debug("Discovered existing HLS job: %s (qualities: %v)", jobID, qualities)
	}

	return discovered
}

// findMediaPathForJob attempts to determine the original media path for a job.
// It checks the lock file first, then falls back to an empty string if unavailable.
func (m *Module) findMediaPathForJob(jobID, outputDir string) string {
	// Try reading lock file
	lockPath := filepath.Join(outputDir, ".lock")
	data, err := os.ReadFile(lockPath)
	if err == nil {
		var lock LockFile
		if json.Unmarshal(data, &lock) == nil && lock.MediaPath != "" {
			return lock.MediaPath
		}
	}

	// If we can't determine the media path, return empty string
	// The job will still be tracked and HLS will be served, but we won't know the original file
	return ""
}

// validateExistingHLS checks if valid HLS content exists on disk for the given output directory and qualities.
// Returns true if master playlist exists and all requested quality variants are present and valid.
func (m *Module) validateExistingHLS(outputDir string, requestedQualities []string) bool {
	// Check if master playlist exists
	masterPath := filepath.Join(outputDir, masterPlaylistName)
	masterData, err := os.ReadFile(masterPath)
	if err != nil {
		return false // No master playlist
	}

	// Parse master playlist to get existing variants
	existingVariants := m.parseVariantStreams(string(masterData))
	if len(existingVariants) == 0 {
		return false // Master playlist exists but has no variants
	}

	// Build a map of existing quality directories
	existingQualities := make(map[string]bool)
	for _, variantPath := range existingVariants {
		qualityName := filepath.Dir(variantPath)
		existingQualities[qualityName] = true
	}

	// Check if all requested qualities exist
	for _, quality := range requestedQualities {
		if !existingQualities[quality] {
			m.log.Debug("Requested quality %s not found in existing HLS content", quality)
			return false
		}

		// Verify the variant playlist exists and has content
		variantPlaylistPath := filepath.Join(outputDir, quality, "playlist.m3u8")
		variantData, err := os.ReadFile(variantPlaylistPath)
		if err != nil {
			m.log.Debug("Variant playlist missing for quality %s", quality)
			return false
		}

		// Check that the playlist has at least one segment
		if !strings.Contains(string(variantData), ".ts") {
			m.log.Debug("Variant playlist for %s has no segments", quality)
			return false
		}

		// Verify at least one segment file exists
		variantDir := filepath.Join(outputDir, quality)
		entries, err := os.ReadDir(variantDir)
		if err != nil {
			return false
		}

		hasSegments := false
		for _, entry := range entries {
			if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".ts") {
				hasSegments = true
				break
			}
		}

		if !hasSegments {
			m.log.Debug("No segment files found for quality %s", quality)
			return false
		}
	}

	// All checks passed - existing HLS content is valid
	return true
}

// LockFile represents an HLS generation lock
type LockFile struct {
	JobID     string    `json:"job_id"`
	MediaPath string    `json:"media_path"`
	StartedAt time.Time `json:"started_at"`
	PID       int       `json:"pid"`
}

// createLock creates a lock file for a job
func (m *Module) createLock(jobID, mediaPath string) error {
	lock := LockFile{
		JobID:     jobID,
		MediaPath: mediaPath,
		StartedAt: time.Now(),
		PID:       os.Getpid(),
	}

	data, err := json.Marshal(lock)
	if err != nil {
		return err
	}

	lockPath := filepath.Join(m.cacheDir, jobID, ".lock")
	return os.WriteFile(lockPath, data, 0644)
}

// removeLock removes a lock file
func (m *Module) removeLock(jobID string) {
	lockPath := filepath.Join(m.cacheDir, jobID, ".lock")
	if err := os.Remove(lockPath); err != nil && !os.IsNotExist(err) {
		m.log.Warn("Failed to remove lock file %s: %v", lockPath, err)
	}
}

// checkLock checks if a lock file exists and if it's stale
func (m *Module) checkLock(jobID string) (exists bool, stale bool, lock *LockFile) {
	lockPath := filepath.Join(m.cacheDir, jobID, ".lock")
	data, err := os.ReadFile(lockPath)
	if err != nil {
		return false, false, nil
	}

	lock = &LockFile{}
	if json.Unmarshal(data, lock) != nil {
		return true, true, nil // Corrupted lock is stale
	}

	// Check if lock is stale (lock older than 30 minutes is considered stale)
	staleThreshold := 30 * time.Minute
	if time.Since(lock.StartedAt) > staleThreshold {
		return true, true, lock
	}

	return true, false, lock
}

// CleanStaleLocks finds and removes stale lock files
func (m *Module) CleanStaleLocks() int {
	m.log.Debug("Checking for stale HLS locks...")
	removed := 0

	entries, err := os.ReadDir(m.cacheDir)
	if err != nil {
		m.log.Error(errReadCacheDirFmt, err)
		return 0
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		jobID := entry.Name()
		exists, stale, lock := m.checkLock(jobID)

		if exists && stale && lock != nil {
			m.log.Warn("Found stale lock for job %s (started: %v)", jobID, lock.StartedAt)
			m.removeLock(jobID)

			// Update job status if it was marked as running
			m.jobsMu.Lock()
			if job, ok := m.jobs[jobID]; ok && job.Status == models.HLSStatusRunning {
				job.Status = models.HLSStatusFailed
				job.Error = "Job timed out (stale lock)"
			}
			m.jobsMu.Unlock()

			removed++
		}
	}

	if removed > 0 {
		m.log.Info("Cleaned %d stale HLS locks", removed)
	}

	return removed
}

// cleanLocksOnStartup removes ALL lock files unconditionally.
// At startup no transcoding is active, so every lock is leftover from a previous run.
func (m *Module) cleanLocksOnStartup() int {
	entries, err := os.ReadDir(m.cacheDir)
	if err != nil {
		return 0
	}

	removed := 0
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		jobID := entry.Name()
		lockPath := filepath.Join(m.cacheDir, jobID, ".lock")
		if _, err := os.Stat(lockPath); err == nil {
			m.log.Info("Removing leftover lock for job %s", jobID)
			if err := os.Remove(lockPath); err != nil {
				m.log.Warn("Failed to remove lock %s: %v", lockPath, err)
			} else {
				removed++
			}

			// Reset job status from running to pending so it can be retried
			m.jobsMu.Lock()
			if job, ok := m.jobs[jobID]; ok && job.Status == models.HLSStatusRunning {
				job.Status = models.HLSStatusPending
				job.Error = ""
			}
			m.jobsMu.Unlock()
		}
	}
	return removed
}

// ValidateMasterPlaylist validates a master playlist and its variants
func (m *Module) ValidateMasterPlaylist(jobID string) (*ValidationResult, error) {
	result := &ValidationResult{
		JobID:  jobID,
		Valid:  true,
		Errors: make([]string, 0),
	}

	outputDir := filepath.Join(m.cacheDir, jobID)

	// Check master playlist exists
	masterPath := filepath.Join(outputDir, masterPlaylistName)
	masterData, err := os.ReadFile(masterPath)
	if err != nil {
		result.Valid = false
		result.Errors = append(result.Errors, fmt.Sprintf("Master playlist not found: %v", err))
		return result, nil
	}

	// Parse master playlist for variant streams
	variants := m.parseVariantStreams(string(masterData))
	result.VariantCount = len(variants)

	if len(variants) == 0 {
		result.Valid = false
		result.Errors = append(result.Errors, "Master playlist has no variant streams")
		return result, nil
	}

	// Validate each variant playlist
	for _, variant := range variants {
		variantPath := filepath.Join(outputDir, variant)

		// Check variant playlist exists
		variantData, err := os.ReadFile(variantPath)
		if err != nil {
			result.Valid = false
			result.Errors = append(result.Errors, fmt.Sprintf("Variant %s not found: %v", variant, err))
			continue
		}

		// Check variant is not empty
		if len(variantData) == 0 {
			result.Valid = false
			result.Errors = append(result.Errors, fmt.Sprintf("Variant %s is empty", variant))
			continue
		}

		// Validate segments exist
		segments := m.parseSegments(string(variantData))
		for _, segment := range segments {
			segmentPath := filepath.Join(outputDir, filepath.Dir(variant), segment)
			if _, err := os.Stat(segmentPath); os.IsNotExist(err) {
				result.Valid = false
				result.Errors = append(result.Errors, fmt.Sprintf("Segment %s missing", segment))
			}
		}
		result.SegmentCount += len(segments)
	}

	return result, nil
}

// ValidationResult holds HLS validation results
type ValidationResult struct {
	JobID        string   `json:"job_id"`
	Valid        bool     `json:"valid"`
	VariantCount int      `json:"variant_count"`
	SegmentCount int      `json:"segment_count"`
	Errors       []string `json:"errors,omitempty"`
}

// parseVariantStreams extracts variant stream paths from master playlist
func (m *Module) parseVariantStreams(content string) []string {
	var variants []string
	// Handle both Unix (\n) and Windows (\r\n) line endings
	content = strings.ReplaceAll(content, "\r\n", "\n")
	lines := strings.Split(content, "\n")

	for i, line := range lines {
		// Trim any remaining whitespace (including \r)
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "#EXT-X-STREAM-INF:") {
			// Next line is the variant path
			if i+1 < len(lines) {
				variant := strings.TrimSpace(lines[i+1])
				if variant != "" && !strings.HasPrefix(variant, "#") {
					variants = append(variants, variant)
				}
			}
		}
	}

	return variants
}

// parseSegments extracts segment filenames from a variant playlist
func (m *Module) parseSegments(content string) []string {
	var segments []string
	// Handle both Unix (\n) and Windows (\r\n) line endings
	content = strings.ReplaceAll(content, "\r\n", "\n")
	lines := strings.Split(content, "\n")

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" && !strings.HasPrefix(line, "#") && strings.HasSuffix(line, ".ts") {
			segments = append(segments, line)
		}
	}

	return segments
}

// AccessTracker tracks last access time for HLS jobs
type AccessTracker struct {
	lastAccess map[string]time.Time
	mu         sync.RWMutex
}

// RecordAccess records an access to an HLS job
func (m *Module) RecordAccess(jobID string) {
	m.accessTracker.mu.Lock()
	defer m.accessTracker.mu.Unlock()
	m.accessTracker.lastAccess[jobID] = time.Now()
}

// GetLastAccess returns the last access time for a job
func (m *Module) GetLastAccess(jobID string) (time.Time, bool) {
	m.accessTracker.mu.RLock()
	defer m.accessTracker.mu.RUnlock()
	t, ok := m.accessTracker.lastAccess[jobID]
	return t, ok
}

// CleanInactiveJobs removes HLS content that hasn't been accessed recently
func (m *Module) CleanInactiveJobs(inactiveThreshold time.Duration) int {
	m.log.Debug("Cleaning inactive HLS jobs (threshold: %v)", inactiveThreshold)
	removed := 0
	cutoff := time.Now().Add(-inactiveThreshold)

	entries, err := os.ReadDir(m.cacheDir)
	if err != nil {
		m.log.Error(errReadCacheDirFmt, err)
		return 0
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if m.cleanInactiveJob(entry, cutoff) {
			removed++
		}
	}

	if removed > 0 {
		m.log.Info("Cleaned %d inactive HLS jobs", removed)
	}

	return removed
}

// getEffectiveLastAccess returns the last access time for a job, falling back to
// the directory modification time if no access has been recorded.
func (m *Module) getEffectiveLastAccess(jobID string, entry os.DirEntry) time.Time {
	lastAccess, ok := m.GetLastAccess(jobID)
	if ok {
		return lastAccess
	}
	// If no access recorded, use file modification time
	info, err := entry.Info()
	if err != nil {
		return time.Time{}
	}
	return info.ModTime()
}

// cleanInactiveJob checks a single job directory and removes it if inactive.
// Returns true if the job was removed.
func (m *Module) cleanInactiveJob(entry os.DirEntry, cutoff time.Time) bool {
	jobID := entry.Name()
	lastAccess := m.getEffectiveLastAccess(jobID, entry)
	if lastAccess.IsZero() {
		return false
	}
	if !lastAccess.Before(cutoff) {
		return false
	}

	// Check if job is completed (don't remove running jobs)
	m.jobsMu.RLock()
	job, exists := m.jobs[jobID]
	m.jobsMu.RUnlock()

	if exists && (job.Status == models.HLSStatusRunning || job.Status == models.HLSStatusPending) {
		return false
	}

	path := filepath.Join(m.cacheDir, jobID)
	if err := os.RemoveAll(path); err != nil {
		m.log.Warn("Failed to remove inactive HLS job %s: %v", jobID, err)
		return false
	}

	// Clean up job record
	m.jobsMu.Lock()
	delete(m.jobs, jobID)
	m.jobsMu.Unlock()

	// Clean up access record
	m.accessTracker.mu.Lock()
	delete(m.accessTracker.lastAccess, jobID)
	m.accessTracker.mu.Unlock()

	m.log.Debug("Removed inactive HLS job: %s (last access: %v)", jobID, lastAccess)
	return true
}

// GetStats returns HLS module statistics
func (m *Module) GetStats() Stats {
	m.jobsMu.RLock()
	defer m.jobsMu.RUnlock()

	stats := Stats{
		TotalJobs: len(m.jobs),
		CacheDir:  m.cacheDir,
	}

	for _, job := range m.jobs {
		switch job.Status {
		case models.HLSStatusCompleted:
			stats.CompletedJobs++
		case models.HLSStatusRunning:
			stats.RunningJobs++
		case models.HLSStatusFailed:
			stats.FailedJobs++
		case models.HLSStatusPending:
			stats.PendingJobs++
		}
	}

	// Calculate cache size
	stats.CacheSize = m.calculateCacheSize()

	return stats
}

// Stats holds HLS module statistics
type Stats struct {
	TotalJobs     int    `json:"total_jobs"`
	RunningJobs   int    `json:"running_jobs"`
	CompletedJobs int    `json:"completed_jobs"`
	FailedJobs    int    `json:"failed_jobs"`
	PendingJobs   int    `json:"pending_jobs"`
	CacheSize     int64  `json:"cache_size_bytes"`
	CacheDir      string `json:"-"`
}

func (m *Module) calculateCacheSize() int64 {
	var size int64

	if err := filepath.Walk(m.cacheDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if !info.IsDir() {
			size += info.Size()
		}
		return nil
	}); err != nil {
		m.log.Warn("Failed to calculate cache size: %v", err)
	}

	return size
}
