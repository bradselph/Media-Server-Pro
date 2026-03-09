// Package thumbnails provides thumbnail generation for media files
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

	// Responsive thumbnail widths (16:9: 160x90, 320x180, 640x360)
	responsiveWidths  = []int{160, 320, 640}
	responsiveSuffixes = map[int]string{160: "-sm", 320: "-md", 640: "-lg"}
)

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

// GenerateThumbnail queues async thumbnail generation (generates all preview thumbnails)
// GenerateThumbnail generates a thumbnail for a media file.
// highPriority=true for user-triggered (HTTP, hover); false for background scan.
func (m *Module) GenerateThumbnail(mediaPath string, mediaID string, isAudio bool, highPriority bool) (string, error) {
	if m.ffmpegPath == "" {
		return "", fmt.Errorf("ffmpeg not available")
	}

	cfg := m.config.Get()
	outputPath := m.getThumbnailPath(mediaID)

	if !isAudio {
		return m.GeneratePreviewThumbnails(mediaPath, mediaID, highPriority)
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
		MediaPath:  mediaPath,
		OutputPath: outputPath,
		Width:      cfg.Thumbnails.Width,
		Height:     cfg.Thumbnails.Height,
		Timestamp:  float64(cfg.Thumbnails.VideoInterval),
		IsAudio:    isAudio,
	}

	// Increment pending count
	m.statsMu.Lock()
	m.stats.Pending++
	m.statsMu.Unlock()

	if m.enqueue(job, highPriority) {
		m.log.Debug("Queued thumbnail generation for: %s (priority=%v)", mediaPath, highPriority)
		return outputPath, ErrThumbnailPending
	}
	m.inFlight.Delete(outputPath)
	m.statsMu.Lock()
	m.stats.Pending--
	m.statsMu.Unlock()
	m.log.Warn("Job queue full, generating thumbnail synchronously: %s", mediaPath)
	return outputPath, m.generateThumbnail(job)
}

// GeneratePreviewThumbnails generates multiple thumbnails at different timestamps for hover preview.
// highPriority=true when user is viewing (hover); false for background scan.
func (m *Module) GeneratePreviewThumbnails(mediaPath string, mediaID string, highPriority bool) (string, error) {
	if m.ffmpegPath == "" {
		return "", fmt.Errorf("ffmpeg not available")
	}

	cfg := m.config.Get()
	previewCount := cfg.Thumbnails.PreviewCount
	if previewCount < 1 {
		previewCount = 10 // Default to 10 if not configured
	}

	// Check if all previews already exist
	if m.HasAllPreviewThumbnails(mediaID) {
		m.log.Debug("All preview thumbnails already exist for: %s", mediaPath)
		return m.getThumbnailPath(mediaID), nil
	}

	// Get video duration to calculate timestamps
	duration, err := m.getMediaDuration(mediaPath)
	if err != nil {
		duration = 600.0 // Default to 10 minutes if we can't get duration
	}

	// Generate thumbnails at evenly spaced intervals
	// Skip first and last 5% to avoid black frames/credits
	startOffset := duration * 0.05
	endOffset := duration * 0.95
	usableDuration := endOffset - startOffset

	// Generate main thumbnail first (index 0)
	mainPath := m.getThumbnailPath(mediaID)
	if _, err := os.Stat(mainPath); os.IsNotExist(err) {
		// Only queue if not already in-flight
		if _, loaded := m.inFlight.LoadOrStore(mainPath, time.Now()); !loaded {
			mainTimestamp := startOffset + (usableDuration / 2) // Middle of video for main thumbnail
			mainJob := &ThumbnailJob{
				MediaPath:  mediaPath,
				OutputPath: mainPath,
				Width:      cfg.Thumbnails.Width,
				Height:     cfg.Thumbnails.Height,
				Timestamp:  mainTimestamp,
				IsAudio:    false,
			}

			m.statsMu.Lock()
			m.stats.Pending++
			m.statsMu.Unlock()

			if m.enqueue(mainJob, highPriority) {
				m.log.Debug("Queued main thumbnail for: %s", mediaPath)
			} else {
				m.inFlight.Delete(mainPath)
				m.statsMu.Lock()
				m.stats.Pending--
				m.statsMu.Unlock()
				m.log.Debug("Job queue full, skipping main thumbnail for: %s", mediaPath)
			}
		}
	}

	// Generate preview thumbnails (index 1+, stored as {uuid}_preview_N.jpg)
previewLoop:
	for i := 0; i < previewCount; i++ {
		filename := fmt.Sprintf("%s_preview_%d.jpg", mediaID, i)
		outputPath := filepath.Join(m.thumbnailDir, filename)

		// Skip if this specific thumbnail already exists on disk or in-flight
		if _, err := os.Stat(outputPath); err == nil {
			m.log.Debug("Preview thumbnail %d already exists: %s", i, outputPath)
			continue
		}
		if _, loaded := m.inFlight.LoadOrStore(outputPath, time.Now()); loaded {
			continue
		}

		// Calculate timestamp for this preview
		var timestamp float64
		if previewCount == 1 {
			timestamp = startOffset + (usableDuration / 2)
		} else {
			timestamp = startOffset + (usableDuration * float64(i) / float64(previewCount-1))
		}

		job := &ThumbnailJob{
			MediaPath:  mediaPath,
			OutputPath: outputPath,
			Width:      cfg.Thumbnails.Width,
			Height:     cfg.Thumbnails.Height,
			Timestamp:  timestamp,
			IsAudio:    false,
		}

		// Increment pending count
		m.statsMu.Lock()
		m.stats.Pending++
		m.statsMu.Unlock()

		if m.enqueue(job, highPriority) {
			m.log.Debug("Queued preview thumbnail %d/%d for: %s (timestamp: %.2fs)", i+1, previewCount, mediaPath, timestamp)
		} else {
			m.inFlight.Delete(outputPath)
			m.statsMu.Lock()
			m.stats.Pending--
			m.statsMu.Unlock()
			cfg := m.config.Get()
			m.log.Warn("Job queue full (%d jobs), skipped %d remaining preview thumbnails for: %s - Consider increasing Thumbnails.QueueSize (current: %d) or WorkerCount (current: %d)",
				cfg.Thumbnails.QueueSize, previewCount-i, mediaPath, cfg.Thumbnails.QueueSize, cfg.Thumbnails.WorkerCount)
			break previewLoop
		}
	}

	return "", ErrThumbnailPending
}

// GenerateThumbnailSync generates a thumbnail synchronously.
// mediaPath is the filesystem path (for ffmpeg), mediaID is the stable UUID (for naming).
func (m *Module) GenerateThumbnailSync(mediaPath string, mediaID string, isAudio bool) (string, error) {
	if m.ffmpegPath == "" {
		m.log.Error("Cannot generate thumbnail - FFmpeg not available")
		return "", fmt.Errorf("ffmpeg not available")
	}

	cfg := m.config.Get()
	outputPath := m.getThumbnailPath(mediaID)

	// Check if already exists
	if _, err := os.Stat(outputPath); err == nil {
		m.log.Debug("Thumbnail already exists: %s", outputPath)
		return outputPath, nil
	}

	job := &ThumbnailJob{
		MediaPath:  mediaPath,
		OutputPath: outputPath,
		Width:      cfg.Thumbnails.Width,
		Height:     cfg.Thumbnails.Height,
		Timestamp:  float64(cfg.Thumbnails.VideoInterval),
		IsAudio:    isAudio,
	}

	m.log.Info("Generating thumbnail synchronously for: %s", mediaPath)
	if err := m.generateThumbnail(job); err != nil {
		m.log.Error("Failed to generate thumbnail for %s: %v", mediaPath, err)
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

// generateVideoThumbnail extracts a frame from video using ffmpeg-go
func (m *Module) generateVideoThumbnail(job *ThumbnailJob) error {
	m.log.Info("Extracting video frame from: %s", job.MediaPath)

	// Use timestamp from job, or calculate default
	timestamp := job.Timestamp
	if timestamp <= 0 {
		// Get video duration
		duration := 60.0 // Default
		if d, err := m.getMediaDuration(job.MediaPath); err == nil {
			duration = d
		}

		// Pick timestamp (10% into video)
		timestamp = duration * 0.1
		if timestamp < 1 {
			timestamp = 1
		}
		if timestamp > duration-1 {
			timestamp = duration / 2
		}
	}

	m.log.Debug("Using timestamp: %.2f seconds", timestamp)

	// Build ffmpeg pipeline using ffmpeg-go
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
		m.log.Error("FFmpeg failed: %v", err)
		m.log.Error("FFmpeg output: %s", string(output))
		return fmt.Errorf("ffmpeg failed: %w", err)
	}

	// Verify file was created
	if _, err := os.Stat(job.OutputPath); os.IsNotExist(err) {
		return fmt.Errorf("thumbnail file not created")
	}

	// Update stats
	if info, err := os.Stat(job.OutputPath); err == nil {
		m.statsMu.Lock()
		m.stats.TotalSize += info.Size()
		m.statsMu.Unlock()
		m.log.Debug("Thumbnail size: %d bytes", info.Size())
	}

	// Generate WebP variant (30–50% smaller) for clients that accept it
	webpPath := m.getThumbnailPathWebp(job.OutputPath)
	if err := m.generateWebPFromVideo(job.MediaPath, webpPath, job.Width, job.Height, timestamp); err != nil {
		m.log.Warn("WebP thumbnail generation failed (JPEG served): %v", err)
		// Non-fatal: JPEG is already available
	} else if info, err := os.Stat(webpPath); err == nil {
		m.statsMu.Lock()
		m.stats.TotalSize += info.Size()
		m.statsMu.Unlock()
		m.log.Debug("WebP thumbnail size: %d bytes", info.Size())
	}

	// Generate responsive sizes (160, 320, 640) for srcset — main thumbnail only
	if !strings.Contains(filepath.Base(job.OutputPath), "_preview_") {
		mediaID := strings.TrimSuffix(filepath.Base(job.OutputPath), ".jpg")
		for _, w := range responsiveWidths {
			h := w * 9 / 16
			suffix := responsiveSuffixes[w]
			outPath := filepath.Join(m.thumbnailDir, mediaID+suffix+".webp")
			if err := m.generateWebPFromVideo(job.MediaPath, outPath, w, h, timestamp); err != nil {
				m.log.Debug("Responsive thumbnail %dw failed: %v", w, err)
			}
		}
	}

	// Compute and store BlurHash for main thumbnail only (LQIP placeholders)
	if m.blurHashUpdater != nil && !strings.Contains(filepath.Base(job.OutputPath), "_preview_") {
		if hash, err := m.computeBlurHash(job.OutputPath); err == nil && hash != "" {
			bgCtx, bgCancel := context.WithTimeout(context.Background(), 5*time.Second)
			if err := m.blurHashUpdater.UpdateBlurHash(bgCtx, job.MediaPath, hash); err != nil {
				m.log.Warn("Failed to store BlurHash: %v", err)
			}
			bgCancel()
		}
	}

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

// generateWebPFromVideo extracts a frame and encodes as WebP
func (m *Module) generateWebPFromVideo(mediaPath, outputPath string, width, height int, timestamp float64) error {
	scaleFilter := fmt.Sprintf("scale=%d:%d:force_original_aspect_ratio=decrease,pad=%d:%d:(ow-iw)/2:(oh-ih)/2,format=yuv420p",
		width, height, width, height)

	stream := ffmpeg.Input(mediaPath, ffmpeg.KwArgs{"ss": fmt.Sprintf("%.2f", timestamp)}).
		Output(outputPath, ffmpeg.KwArgs{
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

	// Verify file was created
	if _, err := os.Stat(job.OutputPath); os.IsNotExist(err) {
		return fmt.Errorf("waveform file not created")
	}

	// Generate WebP variant for audio waveforms
	webpPath := m.getThumbnailPathWebp(job.OutputPath)
	if err := m.generateWebPFromAudio(job.MediaPath, webpPath, job.Width, job.Height); err != nil {
		m.log.Warn("WebP waveform generation failed (JPEG served): %v", err)
	}

	// Compute and store BlurHash for audio waveforms (main thumbnail only)
	if m.blurHashUpdater != nil && !strings.Contains(filepath.Base(job.OutputPath), "_preview_") {
		if hash, err := m.computeBlurHash(job.OutputPath); err == nil && hash != "" {
			bgCtx, bgCancel := context.WithTimeout(context.Background(), 5*time.Second)
			if err := m.blurHashUpdater.UpdateBlurHash(bgCtx, job.MediaPath, hash); err != nil {
				m.log.Warn("Failed to store BlurHash: %v", err)
			}
			bgCancel()
		}
	}
	return nil
}

// generateWebPFromAudio creates waveform as WebP
func (m *Module) generateWebPFromAudio(mediaPath, outputPath string, width, height int) error {
	waveformFilter := fmt.Sprintf("showwavespic=s=%dx%d:colors=#0080ff", width, height)

	stream := ffmpeg.Input(mediaPath).
		Output(outputPath, ffmpeg.KwArgs{
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
	suffix, ok := responsiveSuffixes[width]
	if !ok {
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
	switch placeholderType {
	case "censored":
		bgColor = color.RGBA{R: 80, G: 20, B: 20, A: 255} // Dark red
	default:
		bgColor = color.RGBA{R: 40, G: 40, B: 50, A: 255} // Dark gray
	}

	// Fill image
	for y := 0; y < cfg.Thumbnails.Height; y++ {
		for x := 0; x < cfg.Thumbnails.Width; x++ {
			img.Set(x, y, bgColor)
		}
	}

	file, err := os.Create(outputPath)
	if err != nil {
		return err
	}
	defer func() {
		if err := file.Close(); err != nil {
			m.log.Warn("Failed to close thumbnail file: %v", err)
		}
	}()

	if err := jpeg.Encode(file, img, &jpeg.Options{Quality: 80}); err != nil {
		// Remove corrupted file if encoding failed
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

// GetPreviewURLs returns preview thumbnail URLs for a media file
// GetPreviewURLs returns preview thumbnail URLs for a media file.
// mediaPath is the filesystem path (for ffmpeg), mediaID is the stable UUID (for naming).
func (m *Module) GetPreviewURLs(mediaPath string, mediaID string, count int) []string {
	// Only generate previews for videos
	if m.ffmpegPath == "" {
		return []string{}
	}

	// Get configuration
	cfg := m.config.Get()
	if !cfg.Thumbnails.Enabled || count <= 0 {
		return []string{}
	}

	// Use config default if count not specified
	if count == 0 {
		count = cfg.Thumbnails.PreviewCount
	}

	// Get video duration
	duration := 60.0 // Default fallback
	if m.ffprobePath != "" {
		if d, err := m.getMediaDuration(mediaPath); err == nil {
			duration = d
		}
	}

	// Don't generate previews for very short videos
	if duration < 10 {
		return []string{}
	}

	urls := make([]string, 0, count)

	// Calculate evenly distributed timestamps
	// Skip first 5% and last 5% to avoid black frames
	startOffset := duration * 0.05
	endOffset := duration * 0.95
	interval := (endOffset - startOffset) / float64(count)

	for i := 0; i < count; i++ {
		timestamp := startOffset + (float64(i) * interval)
		previewFilename := fmt.Sprintf("%s_preview_%d.jpg", mediaID, i)
		previewPath := filepath.Join(m.thumbnailDir, previewFilename)
		previewURL := "/thumbnails/" + previewFilename

		// Check if preview already exists
		if _, err := os.Stat(previewPath); err == nil {
			urls = append(urls, previewURL)
			continue
		}

		// Generate preview thumbnail if GenerateOnAccess is enabled
		if cfg.Thumbnails.GenerateOnAccess {
			// Guard against duplicate queuing for the same output path.
			if _, loaded := m.inFlight.LoadOrStore(previewPath, time.Now()); loaded {
				// Already queued by another concurrent request; return URL optimistically.
				urls = append(urls, previewURL)
				continue
			}

			job := &ThumbnailJob{
				MediaPath:  mediaPath,
				OutputPath: previewPath,
				Width:      cfg.Thumbnails.Width,
				Height:     cfg.Thumbnails.Height,
				Timestamp:  timestamp,
				IsAudio:    false,
			}

			m.statsMu.Lock()
			m.stats.Pending++
			m.statsMu.Unlock()

			// Try async generation (high priority — user is viewing)
			if m.enqueue(job, true) {
				m.log.Debug("Queued preview thumbnail generation: %s (frame at %.1fs)", previewFilename, timestamp)
				urls = append(urls, previewURL)
			} else {
				m.inFlight.Delete(previewPath)
				m.statsMu.Lock()
				m.stats.Pending--
				m.statsMu.Unlock()
				if err := m.generateThumbnail(job); err == nil {
					urls = append(urls, previewURL)
				}
			}
		}
	}

	return urls
}
