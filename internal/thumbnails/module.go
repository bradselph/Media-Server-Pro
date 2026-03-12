package thumbnails

import (
	"context"
	"fmt"
	"os"
	"sync"
	"time"

	"media-server-pro/internal/config"
	"media-server-pro/internal/logger"
	"media-server-pro/pkg/helpers"
	"media-server-pro/pkg/models"
)

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
func (m *Module) Start(_ context.Context) error {
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
func (m *Module) Stop(_ context.Context) error {
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
		Name:      m.Name(),
		Status:    status,
		Message:   m.healthMsg,
		CheckedAt: time.Now(),
	}
}
