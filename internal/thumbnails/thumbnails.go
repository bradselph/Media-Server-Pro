// Package thumbnails provides thumbnail generation for media files.
// It uses dedicated types (e.g. ResponsiveVariant, ThumbnailJob, and opts structs)
// instead of raw primitives to clarify intent and avoid primitive obsession.
package thumbnails

import (
	"container/heap"
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"media-server-pro/internal/config"
	"media-server-pro/internal/logger"
	"media-server-pro/pkg/helpers"
	"media-server-pro/pkg/models"

	"github.com/buckket/go-blurhash"
	ffmpeg "github.com/u2takey/ffmpeg-go"
)

var (
	// ErrThumbnailPending indicates thumbnail is being generated
	ErrThumbnailPending = fmt.Errorf("thumbnail generation pending")

	// Responsive variants (16:9: 160x90, 320x180, 640x360) — single source of truth for width and URL suffix
	responsiveVariants = []ResponsiveVariant{
		{Width: 160, Suffix: "-sm"},
		{Width: 320, Suffix: "-md"},
		{Width: 640, Suffix: "-lg"},
	}
)

// ResponsiveVariant defines a responsive thumbnail size and its file suffix (avoids primitive obsession over []int + map[int]string).
type ResponsiveVariant struct {
	Width  int
	Suffix string
}

// ThumbnailRequest groups parameters for thumbnail generation to avoid primitive obsession at the API boundary.
type ThumbnailRequest struct {
	MediaPath    string
	MediaID      string
	IsAudio      bool
	HighPriority bool
}

// PreviewThumbnailsRequest groups parameters for preview thumbnails generation.
type PreviewThumbnailsRequest struct {
	MediaPath    string
	MediaID      string
	HighPriority bool
}

// BlurHashUpdater updates BlurHash in metadata storage (e.g. MediaMetadataRepository)
type BlurHashUpdater interface {
	UpdateBlurHash(ctx context.Context, path string, hash string) error
}

// Module handles thumbnail generation
type Module struct {
	log             *logger.Logger
	config          *config.Manager
	thumbnailDir    string
	ffmpegPath      string
	ffprobePath     string
	jobHeap         jobHeap
	jobMu           sync.Mutex
	jobCond         *sync.Cond
	jobCap          int // max queue size
	ctx             context.Context
	cancel          context.CancelFunc
	wg              sync.WaitGroup
	stats           Stats
	statsMu         sync.RWMutex
	healthMu        sync.RWMutex
	healthy         bool
	healthMsg       string
	blurHashUpdater BlurHashUpdater
	// inFlight tracks output paths currently queued or being processed to
	// prevent duplicate jobs when the background task and HTTP handlers both
	// call GenerateThumbnail for the same file before it is written to disk.
	// The value stored is a time.Time (enqueue timestamp) so that a background
	// cleanup goroutine can evict entries that are stale (e.g. from a worker
	// that exited without completing its job during shutdown).
	inFlight sync.Map // map[outputPath string]time.Time
}

// ThumbnailJob represents a thumbnail generation task
type ThumbnailJob struct {
	MediaPath  string
	OutputPath string
	Width      int
	Height     int
	Timestamp  float64
	IsAudio    bool
}

// priorityJob wraps ThumbnailJob with priority (0=high/user-triggered, 1=low/background)
type priorityJob struct {
	job      *ThumbnailJob
	priority int
}

// tryGeneratePreviewOpts holds arguments for tryGeneratePreview to avoid string-heavy function arguments.
type tryGeneratePreviewOpts struct {
	MediaPath   string
	PreviewPath string
	PreviewURL  string
	Timestamp   float64
}

// buildPreviewURLListOpts holds arguments for buildPreviewURLList to avoid string-heavy and excess function arguments.
type buildPreviewURLListOpts struct {
	MediaPath string
	MediaID   string
	Count     int
	Duration  float64
	Cfg       *config.Config
}

// queuePreviewThumbnailsLoopOpts holds arguments for queuePreviewThumbnailsLoop to avoid string-heavy and excess function arguments.
type queuePreviewThumbnailsLoopOpts struct {
	MediaPath        string
	MediaID          string
	PreviewCount     int
	StartOffset      float64
	UsableDuration   float64
	HighPriority     bool
}

// generateThumbnailRequest holds arguments for generateThumbnailFromRequest to avoid string-heavy function arguments.
type generateThumbnailRequest struct {
	MediaPath    string
	MediaID      string
	IsAudio      bool
	HighPriority bool
}

// generatePreviewThumbnailsRequest holds arguments for generatePreviewThumbnailsFromRequest to avoid string-heavy function arguments.
type generatePreviewThumbnailsRequest struct {
	MediaPath    string
	MediaID      string
	HighPriority bool
}

// generateThumbnailSyncRequest holds arguments for generateThumbnailSyncFromRequest to avoid string-heavy function arguments.
type generateThumbnailSyncRequest struct {
	MediaPath string
	MediaID   string
	IsAudio   bool
}

// getPreviewURLsRequest holds arguments for getPreviewURLsFromRequest to avoid string-heavy function arguments.
type getPreviewURLsRequest struct {
	MediaPath string
	MediaID   string
	Count     int
}

// queueMainPreviewThumbnailOpts holds arguments for queueMainPreviewThumbnail to avoid string-heavy function arguments.
type queueMainPreviewThumbnailOpts struct {
	MediaPath    string
	MainPath    string
	Timestamp   float64
	HighPriority bool
}

// buildPreviewJobOpts holds arguments for buildPreviewJob to avoid string-heavy function arguments.
type buildPreviewJobOpts struct {
	MediaPath   string
	OutputPath  string
	Timestamp   float64
}

// webPFromAudioOpts holds arguments for generateWebPFromAudio to avoid string-heavy function arguments.
type webPFromAudioOpts struct {
	MediaPath  string
	OutputPath string
	Width      int
	Height     int
}

// jobHeap implements heap.Interface for priority queue (lower priority value = higher priority)
type jobHeap []*priorityJob

func (h jobHeap) Len() int            { return len(h) }
func (h jobHeap) Less(i, j int) bool  { return h[i].priority < h[j].priority }
func (h jobHeap) Swap(i, j int)       { h[i], h[j] = h[j], h[i] }
func (h *jobHeap) Push(x interface{}) { *h = append(*h, x.(*priorityJob)) }
func (h *jobHeap) Pop() interface{} {
	old := *h
	n := len(old)
	item := old[n-1]
	*h = old[0 : n-1]
	return item
}

// Stats holds thumbnail generation statistics
type Stats struct {
	Generated int64
	Failed    int64
	Pending   int64
	TotalSize int64
}

// Name returns the module name
func (m *Module) Name() string {
	return "thumbnails"
}

// NewModule creates a new thumbnail module. blurHashUpdater may be nil to skip BlurHash storage.
func NewModule(cfg *config.Manager, blurHashUpdater BlurHashUpdater) *Module {
	log := logger.New("thumbnails")
	currentConfig := cfg.Get()

	// Use configured queue size with minimum of 100
	queueSize := currentConfig.Thumbnails.QueueSize
	if queueSize < 100 {
		queueSize = 100
	}

	m := &Module{
		log:             log,
		config:          cfg,
		thumbnailDir:    currentConfig.Directories.Thumbnails,
		jobHeap:         jobHeap{},
		jobCap:          queueSize,
		healthy:         false,
		healthMsg:       "", // Empty message to suppress warning before Start() is called
		blurHashUpdater: blurHashUpdater,
	}
	m.jobCond = sync.NewCond(&m.jobMu)
	return m
}

// Start initializes the thumbnail module
func (m *Module) Start(ctx context.Context) error {
	m.log.Info("Starting thumbnail module...")

	// Ensure thumbnail directory exists
	if err := os.MkdirAll(m.thumbnailDir, 0755); err != nil {
		m.log.Error("Failed to create thumbnail directory: %v", err)
		return err
	}
	m.log.Info("Thumbnail directory: %s", m.thumbnailDir)

	// Check for ffmpeg
	ffmpegPath, err := helpers.FindBinary("ffmpeg")
	if err != nil {
		m.log.Error("╔═══════════════════════════════════════════════════════════════╗")
		m.log.Error("║ CRITICAL: FFmpeg NOT FOUND - Thumbnails DISABLED              ║")
		m.log.Error("║ Install FFmpeg to enable thumbnail generation                 ║")
		m.log.Error("╚═══════════════════════════════════════════════════════════════╝")
		return fmt.Errorf("ffmpeg not found - thumbnail generation disabled")
	}
	m.ffmpegPath = ffmpegPath
	m.log.Info("✓ FFmpeg found: %s", ffmpegPath)

	// Check for ffprobe
	ffprobePath, err := helpers.FindBinary("ffprobe")
	if err != nil {
		m.log.Warn("ffprobe not found - will use default timestamps")
	} else {
		m.log.Info("✓ FFprobe found: %s", ffprobePath)
	}
	m.ffprobePath = ffprobePath

	// Start worker pool using a background context so workers are not
	// cancelled when the short-lived module-startup context expires.
	workerCtx, cancel := context.WithCancel(context.Background())
	m.ctx = workerCtx
	m.cancel = cancel

	// Use configured worker count with minimum of 2
	cfg := m.config.Get()
	workerCount := cfg.Thumbnails.WorkerCount
	if workerCount < 2 {
		workerCount = 2
	}

	// Get queue size for logging
	queueSize := cfg.Thumbnails.QueueSize
	if queueSize < 100 {
		queueSize = 100
	}

	m.log.Info("Starting %d thumbnail worker(s) with queue size %d...", workerCount, queueSize)

	for i := 0; i < workerCount; i++ {
		m.wg.Add(1)
		go m.worker(i)
	}

	// Start a background goroutine to evict inFlight entries that have been
	// stuck for more than 5 minutes.  This handles the case where a worker
	// exits mid-job (e.g. during shutdown) and never calls inFlight.Delete,
	// which would otherwise permanently block future thumbnail generation for
	// the affected file.
	m.wg.Add(1)
	go m.evictStaleInFlight(workerCtx)

	m.healthMu.Lock()
	m.healthy = true
	m.healthMsg = fmt.Sprintf("Running with %d workers, queue size %d", workerCount, queueSize)
	m.healthMu.Unlock()

	m.log.Info("✓ Thumbnail module started successfully")
	return nil
}

// evictStaleInFlight scans the inFlight map every minute and removes entries
// that have been pending for more than 5 minutes.  Stale entries arise when a
// worker goroutine exits unexpectedly without completing its job (e.g. context
// cancelled during a long ffmpeg run) and the deferred Delete never ran.
func (m *Module) evictStaleInFlight(ctx context.Context) {
	defer m.wg.Done()
	const staleDuration = 5 * time.Minute
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			cutoff := time.Now().Add(-staleDuration)
			m.inFlight.Range(func(key, value any) bool {
				if t, ok := value.(time.Time); ok && t.Before(cutoff) {
					m.inFlight.Delete(key)
					m.log.Warn("Evicted stale inFlight thumbnail entry: %v (queued %v ago)", key, time.Since(t))
				}
				return true
			})
		}
	}
}

// Stop shuts down the module
func (m *Module) Stop(ctx context.Context) error {
	m.log.Info("Stopping thumbnail module...")

	if m.cancel != nil {
		m.cancel()
	}
	// Wake workers blocked in dequeue so they see ctx.Done()
	m.jobCond.Broadcast()

	// Wait for workers with timeout
	done := make(chan struct{})
	go func() {
		m.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		m.log.Info("All workers stopped")
	case <-time.After(5 * time.Second):
		m.log.Warn("Workers did not stop gracefully")
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

	status := models.StatusHealthy
	if !m.healthy {
		status = models.StatusUnhealthy
	}

	return models.HealthStatus{
		Status:  status,
		Message: m.healthMsg,
	}
}

// enqueue adds a job to the priority queue. highPriority=true for user-triggered requests.
// Returns true if queued, false if queue full.
func (m *Module) enqueue(job *ThumbnailJob, highPriority bool) bool {
	m.jobMu.Lock()
	defer m.jobMu.Unlock()
	if len(m.jobHeap) >= m.jobCap {
		return false
	}
	pri := 1 // low (background)
	if highPriority {
		pri = 0 // high (user viewing)
	}
	heap.Push(&m.jobHeap, &priorityJob{job: job, priority: pri})
	m.jobCond.Signal()
	return true
}

// tryEnqueueThumbnailJob claims outputPath via inFlight, increments pending, and enqueues the job.
// Returns (queued, ok): ok is true if the item was skipped (already in-flight) or queued; ok is false when the queue is full (caller should break).
func (m *Module) tryEnqueueThumbnailJob(job *ThumbnailJob, outputPath string, highPriority bool) (queued bool, ok bool) {
	if _, loaded := m.inFlight.LoadOrStore(outputPath, time.Now()); loaded {
		return false, true // already in flight, skip
	}
	m.statsMu.Lock()
	m.stats.Pending++
	m.statsMu.Unlock()
	if m.enqueue(job, highPriority) {
		return true, true
	}
	m.inFlight.Delete(outputPath)
	m.statsMu.Lock()
	m.stats.Pending--
	m.statsMu.Unlock()
	return false, false // queue full
}

// previewTimeRange returns start/end offsets and usable duration (skip first and last 5%).
func previewTimeRange(duration float64) (startOffset, endOffset, usableDuration float64) {
	startOffset = duration * 0.05
	endOffset = duration * 0.95
	usableDuration = endOffset - startOffset
	return startOffset, endOffset, usableDuration
}

// queueMainPreviewThumbnail queues the main (index 0) preview thumbnail if it does not exist on disk.
func (m *Module) queueMainPreviewThumbnail(opts *queueMainPreviewThumbnailOpts) {
	if _, err := os.Stat(opts.MainPath); err == nil {
		return
	}
	cfg := m.config.Get()
	job := &ThumbnailJob{
		MediaPath:  opts.MediaPath,
		OutputPath: opts.MainPath,
		Width:      cfg.Thumbnails.Width,
		Height:     cfg.Thumbnails.Height,
		Timestamp:  opts.Timestamp,
		IsAudio:    false,
	}
	queued, ok := m.tryEnqueueThumbnailJob(job, opts.MainPath, opts.HighPriority)
	if queued {
		m.log.Debug("Queued main thumbnail for: %s", opts.MediaPath)
	} else if !ok {
		m.log.Debug("Job queue full, skipping main thumbnail for: %s", opts.MediaPath)
	}
}

// dequeue blocks until a job is available or ctx is cancelled. Returns nil when ctx is done.
func (m *Module) dequeue(ctx context.Context) *ThumbnailJob {
	m.jobMu.Lock()
	defer m.jobMu.Unlock()
	for len(m.jobHeap) == 0 {
		if ctx.Err() != nil {
			return nil
		}
		m.jobCond.Wait()
	}
	pj := heap.Pop(&m.jobHeap).(*priorityJob)
	return pj.job
}

// worker processes thumbnail generation jobs from the priority queue
func (m *Module) worker(id int) {
	defer m.wg.Done()
	m.log.Debug("Worker %d started", id)

	for {
		select {
		case <-m.ctx.Done():
			m.log.Debug("Worker %d stopping", id)
			return
		default:
		}
		job := m.dequeue(m.ctx)
		if job == nil {
			return
		}

		m.log.Info("Worker %d: Generating thumbnail for %s", id, job.MediaPath)

		m.statsMu.Lock()
		m.stats.Pending--
		m.statsMu.Unlock()

		if err := m.generateThumbnail(job); err != nil {
			m.log.Error("Worker %d: Failed to generate thumbnail for %s: %v", id, job.MediaPath, err)
			m.statsMu.Lock()
			m.stats.Failed++
			m.statsMu.Unlock()
		} else {
			m.log.Info("Worker %d: ✓ Thumbnail generated: %s", id, job.OutputPath)
			m.statsMu.Lock()
			m.stats.Generated++
			m.statsMu.Unlock()
		}
		m.inFlight.Delete(job.OutputPath)
	}
}

// GenerateThumbnail queues async thumbnail generation (generates all preview thumbnails).
// highPriority=true for user-triggered (HTTP, hover); false for background scan.
func (m *Module) GenerateThumbnail(mediaPath string, mediaID string, isAudio bool, highPriority bool) (string, error) {
	return m.GenerateThumbnailRequest(&ThumbnailRequest{
		MediaPath:    mediaPath,
		MediaID:      mediaID,
		IsAudio:      isAudio,
		HighPriority: highPriority,
	})
}

// GenerateThumbnailRequest queues thumbnail generation using a ThumbnailRequest (reduces primitive obsession at API boundary).
func (m *Module) GenerateThumbnailRequest(req *ThumbnailRequest) (string, error) {
	return m.generateThumbnailFromRequest(&generateThumbnailRequest{
		MediaPath:    req.MediaPath,
		MediaID:      req.MediaID,
		IsAudio:      req.IsAudio,
		HighPriority: req.HighPriority,
	})
}

func (m *Module) generateThumbnailFromRequest(req *generateThumbnailRequest) (string, error) {
	if m.ffmpegPath == "" {
		return "", fmt.Errorf("ffmpeg not available")
	}

	cfg := m.config.Get()
	outputPath := m.getThumbnailPath(req.MediaID)

	if !req.IsAudio {
		return m.generatePreviewThumbnailsFromRequest(&generatePreviewThumbnailsRequest{
			MediaPath:    req.MediaPath,
			MediaID:      req.MediaID,
			HighPriority: req.HighPriority,
		})
	}

	// For audio: single waveform thumbnail — skip if already on disk.
	if _, err := os.Stat(outputPath); err == nil {
		m.log.Debug("Thumbnail already exists: %s", outputPath)
		return outputPath, nil
	}

	// For audio, just generate one waveform.
	// Guard against duplicate queuing: if another caller already queued this
	// output path, skip silently and return ErrThumbnailPending.
	if _, loaded := m.inFlight.LoadOrStore(outputPath, time.Now()); loaded {
		return outputPath, ErrThumbnailPending
	}

	job := &ThumbnailJob{
		MediaPath:  req.MediaPath,
		OutputPath: outputPath,
		Width:      cfg.Thumbnails.Width,
		Height:     cfg.Thumbnails.Height,
		Timestamp:  float64(cfg.Thumbnails.VideoInterval),
		IsAudio:    req.IsAudio,
	}

	// Increment pending count
	m.statsMu.Lock()
	m.stats.Pending++
	m.statsMu.Unlock()

	if m.enqueue(job, req.HighPriority) {
		m.log.Debug("Queued thumbnail generation for: %s (priority=%v)", req.MediaPath, req.HighPriority)
		return outputPath, ErrThumbnailPending
	}
	m.inFlight.Delete(outputPath)
	m.statsMu.Lock()
	m.stats.Pending--
	m.statsMu.Unlock()
	m.log.Warn("Job queue full, generating thumbnail synchronously: %s", req.MediaPath)
	return outputPath, m.generateThumbnail(job)
}

// GeneratePreviewThumbnails generates multiple thumbnails at different timestamps for hover preview.
// highPriority=true when user is viewing (hover); false for background scan.
func (m *Module) GeneratePreviewThumbnails(mediaPath string, mediaID string, highPriority bool) (string, error) {
	return m.GeneratePreviewThumbnailsRequest(&PreviewThumbnailsRequest{
		MediaPath:    mediaPath,
		MediaID:      mediaID,
		HighPriority: highPriority,
	})
}

// GeneratePreviewThumbnailsRequest generates preview thumbnails using a PreviewThumbnailsRequest.
func (m *Module) GeneratePreviewThumbnailsRequest(req *PreviewThumbnailsRequest) (string, error) {
	return m.generatePreviewThumbnailsFromRequest(&generatePreviewThumbnailsRequest{
		MediaPath:    req.MediaPath,
		MediaID:      req.MediaID,
		HighPriority: req.HighPriority,
	})
}

func (m *Module) generatePreviewThumbnailsFromRequest(req *generatePreviewThumbnailsRequest) (string, error) {
	if m.ffmpegPath == "" {
		return "", fmt.Errorf("ffmpeg not available")
	}
	previewCount, duration := m.getPreviewConfig(req.MediaPath)
	if m.HasAllPreviewThumbnails(req.MediaID) {
		m.log.Debug("All preview thumbnails already exist for: %s", req.MediaPath)
		return m.getThumbnailPath(req.MediaID), nil
	}
	if duration <= 0 {
		duration = 600.0
	}
	startOffset, _, usableDuration := previewTimeRange(duration)
	mainPath := m.getThumbnailPath(req.MediaID)
	m.queueMainPreviewThumbnail(&queueMainPreviewThumbnailOpts{
		MediaPath:     req.MediaPath,
		MainPath:      mainPath,
		Timestamp:     startOffset + usableDuration/2,
		HighPriority:  req.HighPriority,
	})
	m.queuePreviewThumbnailsLoop(&queuePreviewThumbnailsLoopOpts{
		MediaPath:      req.MediaPath,
		MediaID:        req.MediaID,
		PreviewCount:   previewCount,
		StartOffset:    startOffset,
		UsableDuration: usableDuration,
		HighPriority:   req.HighPriority,
	})
	return "", ErrThumbnailPending
}

// getPreviewConfig returns PreviewCount (min 1) and media duration for preview generation.
func (m *Module) getPreviewConfig(mediaPath string) (previewCount int, duration float64) {
	cfg := m.config.Get()
	previewCount = cfg.Thumbnails.PreviewCount
	if previewCount < 1 {
		previewCount = 10
	}
	duration, _ = m.getMediaDuration(mediaPath)
	return previewCount, duration
}

// queuePreviewThumbnailsLoop queues per-index preview thumbnail jobs; logs and stops if queue is full.
func (m *Module) queuePreviewThumbnailsLoop(opts *queuePreviewThumbnailsLoopOpts) {
	for i := 0; i < opts.PreviewCount; i++ {
		outputPath := filepath.Join(m.thumbnailDir, fmt.Sprintf("%s_preview_%d.jpg", opts.MediaID, i))
		if _, err := os.Stat(outputPath); err == nil {
			continue
		}
		timestamp := previewTimestamp(opts.PreviewCount, i, opts.StartOffset, opts.UsableDuration)
		job := m.buildPreviewJob(&buildPreviewJobOpts{MediaPath: opts.MediaPath, OutputPath: outputPath, Timestamp: timestamp})
		queued, ok := m.tryEnqueueThumbnailJob(job, outputPath, opts.HighPriority)
		if queued {
			m.log.Debug("Queued preview thumbnail %d/%d for: %s (timestamp: %.2fs)", i+1, opts.PreviewCount, opts.MediaPath, timestamp)
			continue
		}
		if !ok {
			c := m.config.Get()
			m.log.Warn("Job queue full (%d jobs), skipped %d remaining preview thumbnails for: %s - Consider increasing Thumbnails.QueueSize (current: %d) or WorkerCount (current: %d)",
				c.Thumbnails.QueueSize, opts.PreviewCount-i, opts.MediaPath, c.Thumbnails.QueueSize, c.Thumbnails.WorkerCount)
			break
		}
	}
}

// previewTimestamp returns the timestamp for preview index i (previewCount >= 1).
func previewTimestamp(previewCount, i int, startOffset, usableDuration float64) float64 {
	if previewCount == 1 {
		return startOffset + usableDuration/2
	}
	return startOffset + usableDuration*float64(i)/float64(previewCount-1)
}

// buildPreviewJob creates a ThumbnailJob for a preview at the given path and timestamp.
func (m *Module) buildPreviewJob(opts *buildPreviewJobOpts) *ThumbnailJob {
	cfg := m.config.Get()
	return &ThumbnailJob{
		MediaPath:  opts.MediaPath,
		OutputPath: opts.OutputPath,
		Width:      cfg.Thumbnails.Width,
		Height:     cfg.Thumbnails.Height,
		Timestamp:  opts.Timestamp,
		IsAudio:    false,
	}
}

// GenerateThumbnailSync generates a thumbnail synchronously.
// mediaPath is the filesystem path (for ffmpeg), mediaID is the stable UUID (for naming).
func (m *Module) GenerateThumbnailSync(mediaPath string, mediaID string, isAudio bool) (string, error) {
	return m.generateThumbnailSyncFromRequest(&generateThumbnailSyncRequest{
		MediaPath: mediaPath,
		MediaID:   mediaID,
		IsAudio:   isAudio,
	})
}

func (m *Module) generateThumbnailSyncFromRequest(req *generateThumbnailSyncRequest) (string, error) {
	if m.ffmpegPath == "" {
		m.log.Error("Cannot generate thumbnail - FFmpeg not available")
		return "", fmt.Errorf("ffmpeg not available")
	}

	cfg := m.config.Get()
	outputPath := m.getThumbnailPath(req.MediaID)

	// Check if already exists
	if _, err := os.Stat(outputPath); err == nil {
		m.log.Debug("Thumbnail already exists: %s", outputPath)
		return outputPath, nil
	}

	job := &ThumbnailJob{
		MediaPath:  req.MediaPath,
		OutputPath: outputPath,
		Width:      cfg.Thumbnails.Width,
		Height:     cfg.Thumbnails.Height,
		Timestamp:  float64(cfg.Thumbnails.VideoInterval),
		IsAudio:    req.IsAudio,
	}

	m.log.Info("Generating thumbnail synchronously for: %s", req.MediaPath)
	if err := m.generateThumbnail(job); err != nil {
		m.log.Error("Failed to generate thumbnail for %s: %v", req.MediaPath, err)
		return "", err
	}

	m.log.Info("✓ Thumbnail generated successfully: %s", outputPath)
	return outputPath, nil
}

// generateThumbnail performs the actual thumbnail generation
func (m *Module) generateThumbnail(job *ThumbnailJob) error {
	// Ensure output directory exists
	if err := os.MkdirAll(filepath.Dir(job.OutputPath), 0755); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	if job.IsAudio {
		return m.generateAudioThumbnail(job)
	}
	return m.generateVideoThumbnail(job)
}

// resolveVideoTimestamp returns the timestamp to use for frame extraction (from job or derived from duration).
func (m *Module) resolveVideoTimestamp(job *ThumbnailJob) float64 {
	timestamp := job.Timestamp
	if timestamp > 0 {
		return timestamp
	}
	duration := 60.0
	if d, err := m.getMediaDuration(job.MediaPath); err == nil {
		duration = d
	}
	timestamp = duration * 0.1
	if timestamp < 1 {
		timestamp = 1
	}
	if timestamp > duration-1 {
		timestamp = duration / 2
	}
	return timestamp
}

// addFileSizeToStats adds the file size at path to module stats if the file exists.
func (m *Module) addFileSizeToStats(path string) {
	info, err := os.Stat(path)
	if err != nil {
		return
	}
	m.statsMu.Lock()
	m.stats.TotalSize += info.Size()
	m.statsMu.Unlock()
	m.log.Debug("Thumbnail size: %d bytes", info.Size())
}

// tryGenerateWebPVariant generates a WebP variant for the main JPEG and updates stats on success.
func (m *Module) tryGenerateWebPVariant(job *ThumbnailJob, timestamp float64) {
	webpPath := m.getThumbnailPathWebp(job.OutputPath)
	if err := m.generateWebPFromVideo(&webPFromVideoOpts{job.MediaPath, webpPath, job.Width, job.Height, timestamp}); err != nil {
		m.log.Warn("WebP thumbnail generation failed (JPEG served): %v", err)
		return
	}
	m.addFileSizeToStats(webpPath)
}

// generateResponsiveThumbnailsIfMain generates 160/320/640 WebP variants for srcset; no-op for _preview_ thumbnails.
func (m *Module) generateResponsiveThumbnailsIfMain(job *ThumbnailJob, timestamp float64) {
	if strings.Contains(filepath.Base(job.OutputPath), "_preview_") {
		return
	}
	mediaID := strings.TrimSuffix(filepath.Base(job.OutputPath), ".jpg")
	for _, v := range responsiveVariants {
		h := v.Width * 9 / 16
		outPath := filepath.Join(m.thumbnailDir, mediaID+v.Suffix+".webp")
		if err := m.generateWebPFromVideo(&webPFromVideoOpts{job.MediaPath, outPath, v.Width, h, timestamp}); err != nil {
			m.log.Debug("Responsive thumbnail %dw failed: %v", v.Width, err)
		}
	}
}

// tryUpdateBlurHashForThumbnail computes and stores BlurHash for the main thumbnail (LQIP); no-op if no updater or _preview_.
func (m *Module) tryUpdateBlurHashForThumbnail(job *ThumbnailJob) {
	if m.blurHashUpdater == nil || strings.Contains(filepath.Base(job.OutputPath), "_preview_") {
		return
	}
	hash, err := m.computeBlurHash(job.OutputPath)
	if err != nil || hash == "" {
		return
	}
	bgCtx, bgCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer bgCancel()
	if err := m.blurHashUpdater.UpdateBlurHash(bgCtx, job.MediaPath, hash); err != nil {
		m.log.Warn("Failed to store BlurHash: %v", err)
	}
}

// generateVideoThumbnail extracts a frame from video using ffmpeg-go
func (m *Module) generateVideoThumbnail(job *ThumbnailJob) error {
	m.log.Info("Extracting video frame from: %s", job.MediaPath)
	timestamp := m.resolveVideoTimestamp(job)
	m.log.Debug("Using timestamp: %.2f seconds", timestamp)

	// format=yuv420p ensures 8-bit output before JPEG encoding;
	// without it, 10-bit HDR/HEVC/AV1 sources fail with "codec not supported" errors.
	scaleFilter := fmt.Sprintf("scale=%d:%d:force_original_aspect_ratio=decrease,pad=%d:%d:(ow-iw)/2:(oh-ih)/2,format=yuv420p",
		job.Width, job.Height, job.Width, job.Height)
	stream := ffmpeg.Input(job.MediaPath, ffmpeg.KwArgs{"ss": fmt.Sprintf("%.2f", timestamp)}).
		Output(job.OutputPath, ffmpeg.KwArgs{
			"vframes": "1",
			"vf":      scaleFilter,
			"q:v":     "2",
		}).OverWriteOutput().SetFfmpegPath(m.ffmpegPath)
	cmd := stream.Compile()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmdWithContext := exec.CommandContext(ctx, cmd.Path, cmd.Args[1:]...)
	cmdWithContext.Env = cmd.Env
	cmdWithContext.Dir = cmd.Dir

	output, err := cmdWithContext.CombinedOutput()
	if err != nil {
		m.log.Error("FFmpeg failed: %v", err)
		m.log.Error("FFmpeg output: %s", string(output))
		return fmt.Errorf("ffmpeg failed: %w", err)
	}
	if _, err := os.Stat(job.OutputPath); os.IsNotExist(err) {
		return fmt.Errorf("thumbnail file not created")
	}

	m.addFileSizeToStats(job.OutputPath)
	m.tryGenerateWebPVariant(job, timestamp)
	m.generateResponsiveThumbnailsIfMain(job, timestamp)
	m.tryUpdateBlurHashForThumbnail(job)
	return nil
}

// computeBlurHash reads a JPEG and returns its BlurHash string (4x3 components)
func (m *Module) computeBlurHash(jpgPath string) (string, error) {
	f, err := os.Open(jpgPath)
	if err != nil {
		return "", err
	}
	defer f.Close()

	img, err := jpeg.Decode(f)
	if err != nil {
		return "", err
	}
	return blurhash.Encode(4, 3, img)
}

// webPFromVideoOpts holds parameters for generating a WebP frame from video.
type webPFromVideoOpts struct {
	mediaPath   string
	outputPath  string
	width       int
	height      int
	timestamp   float64
}

// generateWebPFromVideo extracts a frame and encodes as WebP
func (m *Module) generateWebPFromVideo(opts *webPFromVideoOpts) error {
	scaleFilter := fmt.Sprintf("scale=%d:%d:force_original_aspect_ratio=decrease,pad=%d:%d:(ow-iw)/2:(oh-ih)/2,format=yuv420p",
		opts.width, opts.height, opts.width, opts.height)

	stream := ffmpeg.Input(opts.mediaPath, ffmpeg.KwArgs{"ss": fmt.Sprintf("%.2f", opts.timestamp)}).
		Output(opts.outputPath, ffmpeg.KwArgs{
			"vframes": "1",
			"vf":      scaleFilter,
			"c:v":     "libwebp",
			"q:v":     "80",
		}).OverWriteOutput().SetFfmpegPath(m.ffmpegPath)

	cmd := stream.Compile()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cmdWithContext := exec.CommandContext(ctx, cmd.Path, cmd.Args[1:]...)
	cmdWithContext.Env = cmd.Env
	cmdWithContext.Dir = cmd.Dir

	output, err := cmdWithContext.CombinedOutput()
	if err != nil {
		m.log.Debug("FFmpeg WebP failed: %v, output: %s", err, string(output))
		return fmt.Errorf("ffmpeg webp: %w", err)
	}
	return nil
}

// generateAudioThumbnail creates waveform for audio using ffmpeg-go
func (m *Module) generateAudioThumbnail(job *ThumbnailJob) error {
	m.log.Info("Generating audio waveform for: %s", job.MediaPath)

	// Build ffmpeg pipeline using ffmpeg-go
	waveformFilter := fmt.Sprintf("showwavespic=s=%dx%d:colors=#0080ff", job.Width, job.Height)

	stream := ffmpeg.Input(job.MediaPath).
		Output(job.OutputPath, ffmpeg.KwArgs{
			"filter_complex": waveformFilter,
			"frames:v":       "1",
		}).OverWriteOutput().SetFfmpegPath(m.ffmpegPath)

	// Compile to command
	cmd := stream.Compile()

	// Apply context for timeout
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cmdWithContext := exec.CommandContext(ctx, cmd.Path, cmd.Args[1:]...)
	cmdWithContext.Env = cmd.Env
	cmdWithContext.Dir = cmd.Dir

	output, err := cmdWithContext.CombinedOutput()
	if err != nil {
		m.log.Error("FFmpeg waveform failed: %v", err)
		m.log.Error("FFmpeg output: %s", string(output))
		return fmt.Errorf("ffmpeg waveform failed: %w", err)
	}

	if err := m.verifyAndPostProcessAudioThumbnail(job); err != nil {
		return err
	}
	return nil
}

// verifyAndPostProcessAudioThumbnail verifies the waveform file exists, generates WebP variant, and updates BlurHash.
func (m *Module) verifyAndPostProcessAudioThumbnail(job *ThumbnailJob) error {
	if _, err := os.Stat(job.OutputPath); os.IsNotExist(err) {
		return fmt.Errorf("waveform file not created")
	}
	webpPath := m.getThumbnailPathWebp(job.OutputPath)
	if err := m.generateWebPFromAudio(&webPFromAudioOpts{MediaPath: job.MediaPath, OutputPath: webpPath, Width: job.Width, Height: job.Height}); err != nil {
		m.log.Warn("WebP waveform generation failed (JPEG served): %v", err)
	}
	m.updateBlurHashForAudioThumbnail(job)
	return nil
}

// updateBlurHashForAudioThumbnail computes and stores BlurHash for the main audio waveform thumbnail.
func (m *Module) updateBlurHashForAudioThumbnail(job *ThumbnailJob) {
	if m.blurHashUpdater == nil || strings.Contains(filepath.Base(job.OutputPath), "_preview_") {
		return
	}
	hash, err := m.computeBlurHash(job.OutputPath)
	if err != nil || hash == "" {
		return
	}
	bgCtx, bgCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer bgCancel()
	if err := m.blurHashUpdater.UpdateBlurHash(bgCtx, job.MediaPath, hash); err != nil {
		m.log.Warn("Failed to store BlurHash: %v", err)
	}
}

// generateWebPFromAudio creates waveform as WebP
func (m *Module) generateWebPFromAudio(opts *webPFromAudioOpts) error {
	waveformFilter := fmt.Sprintf("showwavespic=s=%dx%d:colors=#0080ff", opts.Width, opts.Height)

	stream := ffmpeg.Input(opts.MediaPath).
		Output(opts.OutputPath, ffmpeg.KwArgs{
			"filter_complex": waveformFilter,
			"frames:v":       "1",
			"c:v":            "libwebp",
			"q:v":            "80",
		}).OverWriteOutput().SetFfmpegPath(m.ffmpegPath)

	cmd := stream.Compile()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cmdWithContext := exec.CommandContext(ctx, cmd.Path, cmd.Args[1:]...)
	cmdWithContext.Env = cmd.Env
	cmdWithContext.Dir = cmd.Dir

	_, err := cmdWithContext.CombinedOutput()
	return err
}

// getThumbnailPath generates the output path for a thumbnail (index 0 for main thumbnail).
// mediaID is the stable UUID used as the filename base.
func (m *Module) getThumbnailPath(mediaID string) string {
	return m.getThumbnailPathByIndex(mediaID, 0)
}

// getThumbnailPathWebp returns the WebP path for a given JPEG path.
func (m *Module) getThumbnailPathWebp(jpgPath string) string {
	return strings.TrimSuffix(jpgPath, ".jpg") + ".webp"
}

// getThumbnailPathByIndex generates the output path for a specific thumbnail index.
// mediaID is the stable UUID; the on-disk filename is {uuid}.jpg or {uuid}_preview_N.jpg.
func (m *Module) getThumbnailPathByIndex(mediaID string, index int) string {
	if index == 0 {
		return filepath.Join(m.thumbnailDir, mediaID+".jpg")
	}
	filename := fmt.Sprintf("%s_preview_%d.jpg", mediaID, index-1)
	return filepath.Join(m.thumbnailDir, filename)
}

// GetThumbnailPath returns the thumbnail path for a media ID (public version)
func (m *Module) GetThumbnailPath(mediaID string) string {
	return m.getThumbnailPath(mediaID)
}

// GetThumbnailFilePath returns the absolute file path for a media ID
func (m *Module) GetThumbnailFilePath(mediaID string) string {
	return m.getThumbnailPath(mediaID)
}

// HasThumbnail checks if a thumbnail exists for a media ID
func (m *Module) HasThumbnail(mediaID string) bool {
	path := m.getThumbnailPath(mediaID)
	_, err := os.Stat(path)
	return err == nil
}

// HasWebPThumbnail checks if a WebP thumbnail exists for a media ID
func (m *Module) HasWebPThumbnail(mediaID string) bool {
	jpgPath := m.getThumbnailPath(mediaID)
	webpPath := m.getThumbnailPathWebp(jpgPath)
	_, err := os.Stat(webpPath)
	return err == nil
}

// GetThumbnailFilePathWebp returns the absolute file path for WebP variant, or empty if not found
func (m *Module) GetThumbnailFilePathWebp(mediaID string) string {
	jpgPath := m.getThumbnailPath(mediaID)
	webpPath := m.getThumbnailPathWebp(jpgPath)
	if _, err := os.Stat(webpPath); err == nil {
		return webpPath
	}
	return ""
}

// GetThumbnailFilePathForSize returns the path for a responsive size (160, 320, 640).
// Responsive sizes are stored as WebP only (-sm.webp, -md.webp, -lg.webp).
// Returns empty if width not in (160, 320, 640) or file does not exist.
func (m *Module) GetThumbnailFilePathForSize(mediaID string, width int) string {
	var suffix string
	for _, v := range responsiveVariants {
		if v.Width == width {
			suffix = v.Suffix
			break
		}
	}
	if suffix == "" {
		return ""
	}
	// Responsive sizes are WebP-only (-sm.webp, -md.webp, -lg.webp)
	path := filepath.Join(m.thumbnailDir, mediaID+suffix+".webp")
	if _, err := os.Stat(path); err == nil {
		return path
	}
	return ""
}

// HasAllPreviewThumbnails checks if all preview thumbnails exist for a media ID
func (m *Module) HasAllPreviewThumbnails(mediaID string) bool {
	cfg := m.config.Get()

	// Check main thumbnail
	mainPath := filepath.Join(m.thumbnailDir, mediaID+".jpg")
	if _, err := os.Stat(mainPath); err != nil {
		return false
	}

	// Check all preview thumbnails
	for i := 0; i < cfg.Thumbnails.PreviewCount; i++ {
		filename := fmt.Sprintf("%s_preview_%d.jpg", mediaID, i)
		path := filepath.Join(m.thumbnailDir, filename)
		if _, err := os.Stat(path); err != nil {
			return false
		}
	}
	return true
}

// GetThumbnailURL returns the URL path for a thumbnail given the media's stable ID.
// Uses the ID-based endpoint so the handler can resolve the media file and enforce
// mature-content checks on every access. The stable ID is stored in the DB and
// survives file renames/moves (see media/discovery.go createMediaItem).
func (m *Module) GetThumbnailURL(mediaID string) string {
	return "/thumbnail?id=" + mediaID
}

// GetThumbnailDir returns the thumbnail directory path
func (m *Module) GetThumbnailDir() string {
	return m.thumbnailDir
}

// GetPlaceholderPath returns path to static placeholder images
func (m *Module) GetPlaceholderPath(placeholderType string) (string, error) {
	var filename string
	switch placeholderType {
	case "audio_placeholder":
		filename = "audio_placeholder.jpg"
	case "censored":
		filename = "censored_placeholder.jpg"
	default:
		filename = "placeholder.jpg"
	}

	placeholderPath := filepath.Join(m.thumbnailDir, filename)

	// Check if exists
	if _, err := os.Stat(placeholderPath); err == nil {
		return placeholderPath, nil
	}

	// Generate if missing
	if err := m.generateStaticPlaceholder(placeholderPath, placeholderType); err != nil {
		return "", err
	}

	return placeholderPath, nil
}

// generateStaticPlaceholder creates static placeholder images
func (m *Module) generateStaticPlaceholder(outputPath, placeholderType string) error {
	cfg := m.config.Get()
	img := image.NewRGBA(image.Rect(0, 0, cfg.Thumbnails.Width, cfg.Thumbnails.Height))

	var bgColor color.RGBA
	if placeholderType == "censored" {
		bgColor = color.RGBA{R: 80, G: 20, B: 20, A: 255} // Dark red
	} else {
		bgColor = color.RGBA{R: 40, G: 40, B: 50, A: 255} // Dark gray
	}

	// Fill image
	for y := 0; y < cfg.Thumbnails.Height; y++ {
		for x := 0; x < cfg.Thumbnails.Width; x++ {
			img.Set(x, y, bgColor)
		}
	}

	return m.writePlaceholderImage(outputPath, img)
}

// writePlaceholderImage writes an RGBA image to path as JPEG (for static placeholders).
func (m *Module) writePlaceholderImage(outputPath string, img image.Image) error {
	file, err := os.Create(outputPath)
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := file.Close(); closeErr != nil {
			m.log.Warn("Failed to close thumbnail file: %v", closeErr)
		}
	}()

	if err := jpeg.Encode(file, img, &jpeg.Options{Quality: 80}); err != nil {
		if removeErr := os.Remove(outputPath); removeErr != nil {
			m.log.Warn("Failed to remove corrupted thumbnail %s: %v", outputPath, removeErr)
		}
		return fmt.Errorf("failed to encode thumbnail: %w", err)
	}
	return nil
}

// getMediaDuration uses ffmpeg-go Probe to get duration
func (m *Module) getMediaDuration(path string) (float64, error) {
	// Try ffmpeg-go Probe first, using the explicit ffprobe path when available
	// so this works under systemd (which strips PATH to a minimal set).
	var probeJSON string
	var err error
	const probeTimeout = 15 * time.Second
	if m.ffprobePath != "" {
		probeJSON, err = ffmpeg.ProbeWithTimeout(path, probeTimeout, ffmpeg.KwArgs{"cmd": m.ffprobePath})
	} else {
		probeJSON, err = ffmpeg.ProbeWithTimeout(path, probeTimeout, nil)
	}
	if err == nil {
		duration := m.parseProbeDuration(probeJSON)
		if duration > 0 {
			return duration, nil
		}
	}

	// Fallback to raw ffprobe if available
	if m.ffprobePath == "" {
		return 0, fmt.Errorf("ffprobe not available and ffmpeg-go probe failed: %w", err)
	}

	m.log.Debug("ffmpeg-go probe failed, trying raw ffprobe: %v", err)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, m.ffprobePath,
		"-v", "error",
		"-show_entries", "format=duration",
		"-of", "default=noprint_wrappers=1:nokey=1",
		path,
	)
	output, err := cmd.Output()
	if err != nil {
		return 0, err
	}

	var duration float64
	if _, err := fmt.Sscanf(string(output), "%f", &duration); err != nil {
		return 0, err
	}

	return duration, nil
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

// GetStats returns current statistics
func (m *Module) GetStats() Stats {
	m.statsMu.RLock()
	defer m.statsMu.RUnlock()
	return m.stats
}

// getPreviewDuration returns the media duration in seconds, or 60.0 as fallback.
func (m *Module) getPreviewDuration(mediaPath string) float64 {
	duration := 60.0
	if m.ffprobePath != "" {
		if d, err := m.getMediaDuration(mediaPath); err == nil {
			duration = d
		}
	}
	return duration
}

// previewURLForIndex returns the preview URL for the i-th frame (existing file or generated), or "" if unavailable.
func (m *Module) previewURLForIndex(opts *buildPreviewURLListOpts, i int) string {
	startOffset := opts.Duration * 0.05
	endOffset := opts.Duration * 0.95
	interval := (endOffset - startOffset) / float64(opts.Count)
	timestamp := startOffset + (float64(i) * interval)
	previewFilename := fmt.Sprintf("%s_preview_%d.jpg", opts.MediaID, i)
	previewPath := filepath.Join(m.thumbnailDir, previewFilename)
	previewURL := "/thumbnails/" + previewFilename

	if _, err := os.Stat(previewPath); err == nil {
		return previewURL
	}
	if opts.Cfg.Thumbnails.GenerateOnAccess {
		previewOpts := &tryGeneratePreviewOpts{MediaPath: opts.MediaPath, PreviewPath: previewPath, PreviewURL: previewURL, Timestamp: timestamp}
		return m.tryGeneratePreview(previewOpts)
	}
	return ""
}

// buildPreviewURLList fills and returns the list of preview URLs for the given media.
func (m *Module) buildPreviewURLList(opts *buildPreviewURLListOpts) []string {
	urls := make([]string, 0, opts.Count)
	for i := 0; i < opts.Count; i++ {
		if u := m.previewURLForIndex(opts, i); u != "" {
			urls = append(urls, u)
		}
	}
	return urls
}

// tryGeneratePreview queues or runs preview generation and returns the URL if the preview is/will be available.
func (m *Module) tryGeneratePreview(opts *tryGeneratePreviewOpts) string {
	if _, loaded := m.inFlight.LoadOrStore(opts.PreviewPath, time.Now()); loaded {
		return opts.PreviewURL
	}

	cfg := m.config.Get()
	job := &ThumbnailJob{
		MediaPath:  opts.MediaPath,
		OutputPath: opts.PreviewPath,
		Width:      cfg.Thumbnails.Width,
		Height:     cfg.Thumbnails.Height,
		Timestamp:  opts.Timestamp,
		IsAudio:    false,
	}

	m.statsMu.Lock()
	m.stats.Pending++
	m.statsMu.Unlock()

	if m.enqueue(job, true) {
		m.log.Debug("Queued preview thumbnail generation: %s (frame at %.1fs)", filepath.Base(opts.PreviewPath), opts.Timestamp)
		return opts.PreviewURL
	}

	m.inFlight.Delete(opts.PreviewPath)
	m.statsMu.Lock()
	m.stats.Pending--
	m.statsMu.Unlock()
	if err := m.generateThumbnail(job); err != nil {
		return ""
	}
	return opts.PreviewURL
}

// GetPreviewURLs returns preview thumbnail URLs for a media file.
// mediaPath is the filesystem path (for ffmpeg), mediaID is the stable UUID (for naming).
func (m *Module) GetPreviewURLs(mediaPath string, mediaID string, count int) []string {
	return m.getPreviewURLsFromRequest(&getPreviewURLsRequest{
		MediaPath: mediaPath,
		MediaID:   mediaID,
		Count:     count,
	})
}

func (m *Module) getPreviewURLsFromRequest(req *getPreviewURLsRequest) []string {
	if m.ffmpegPath == "" {
		return []string{}
	}
	cfg := m.config.Get()
	if !cfg.Thumbnails.Enabled || req.Count <= 0 {
		return []string{}
	}
	count := req.Count
	if count == 0 {
		count = cfg.Thumbnails.PreviewCount
	}
	duration := m.getPreviewDuration(req.MediaPath)
	if duration < 10 {
		return []string{}
	}
	return m.buildPreviewURLList(&buildPreviewURLListOpts{
		MediaPath: req.MediaPath,
		MediaID:   req.MediaID,
		Count:     count,
		Duration:  duration,
		Cfg:       cfg,
	})
}
