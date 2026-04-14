package thumbnails

import (
	"errors"
	"fmt"
	"io"
	"os"
	"time"
)

const errFFmpegNotAvailable = "ffmpeg not available"

// GenerateThumbnailRequest queues thumbnail generation (generates all preview thumbnails).
// Use highPriority=true for user-triggered (HTTP, hover); false for background scan.
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
		return "", fmt.Errorf(errFFmpegNotAvailable)
	}

	cfg := m.config.Get()
	outputPath := m.getThumbnailPath(MediaID(req.MediaID))

	if !req.IsAudio {
		return m.generatePreviewThumbnailsFromRequest(&generatePreviewThumbnailsRequest{
			MediaPath:    req.MediaPath,
			MediaID:      req.MediaID,
			HighPriority: req.HighPriority,
		})
	}

	// For audio: single waveform thumbnail — skip if already on disk and valid.
	if isValidThumbnailFile(outputPath) {
		m.log.Debug("Thumbnail already exists: %s", outputPath)
		return outputPath, nil
	}
	// Remove 0-byte corrupt file so ffmpeg can overwrite
	_ = os.Remove(outputPath)

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

// GeneratePreviewThumbnailsRequest generates multiple thumbnails at different timestamps for hover preview.
// Use highPriority=true when user is viewing (hover); false for background scan.
func (m *Module) GeneratePreviewThumbnailsRequest(req *PreviewThumbnailsRequest) (string, error) {
	return m.generatePreviewThumbnailsFromRequest(&generatePreviewThumbnailsRequest{
		MediaPath:    req.MediaPath,
		MediaID:      req.MediaID,
		HighPriority: req.HighPriority,
	})
}

func (m *Module) generatePreviewThumbnailsFromRequest(req *generatePreviewThumbnailsRequest) (string, error) {
	if m.ffmpegPath == "" {
		return "", fmt.Errorf(errFFmpegNotAvailable)
	}
	previewCount, duration := m.getPreviewConfig(req.MediaPath)
	if m.HasAllPreviewThumbnails(MediaID(req.MediaID)) {
		m.log.Debug("All preview thumbnails already exist for: %s", req.MediaPath)
		return m.getThumbnailPath(MediaID(req.MediaID)), nil
	}
	if duration <= 0 {
		duration = 600.0
	}
	startOffset, _, usableDuration := previewTimeRange(duration)
	mainPath := m.getThumbnailPath(MediaID(req.MediaID))
	m.queueMainPreviewThumbnail(&queueMainPreviewThumbnailOpts{
		MediaPath:    req.MediaPath,
		MainPath:     mainPath,
		Timestamp:    startOffset + usableDuration/2,
		HighPriority: req.HighPriority,
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

// GenerateThumbnailSyncRequest generates a thumbnail synchronously.
// mediaPath is the filesystem path (for ffmpeg), mediaID is the stable UUID (for naming).
func (m *Module) GenerateThumbnailSyncRequest(req *ThumbnailSyncRequest) (string, error) {
	return m.generateThumbnailSyncFromRequest(req)
}

// QueueThumbnailIfMissing implements media.ThumbnailQueuer.
// It queues a low-priority background thumbnail job if one doesn't already exist.
func (m *Module) QueueThumbnailIfMissing(mediaPath, mediaID string, isAudio bool) {
	if m.HasThumbnail(MediaID(mediaID)) {
		return
	}
	_, err := m.GenerateThumbnailRequest(&ThumbnailRequest{
		MediaPath:    mediaPath,
		MediaID:      mediaID,
		IsAudio:      isAudio,
		HighPriority: false,
	})
	if err != nil && !errors.Is(err, ErrThumbnailPending) {
		m.log.Debug("Background thumbnail queue failed for %s: %v", mediaPath, err)
	}
}

// SaveCustomThumbnail replaces the thumbnail for mediaID with the image data from r.
// Any existing WebP variant and preview frames are deleted so they don't serve stale data.
// The image is saved as a JPEG regardless of source format; callers are responsible for
// ensuring r contains valid image data (JPEG, PNG, or WebP).
func (m *Module) SaveCustomThumbnail(mediaID string, r io.Reader) error {
	destPath := m.getThumbnailPath(MediaID(mediaID))
	if err := os.MkdirAll(m.thumbnailDir, 0o755); err != nil { //nolint:gosec // G301
		return fmt.Errorf("creating thumbnail dir: %w", err)
	}
	tmp := destPath + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return fmt.Errorf("creating temp file: %w", err)
	}
	if _, err := io.Copy(f, r); err != nil {
		f.Close()
		os.Remove(tmp)
		return fmt.Errorf("writing thumbnail data: %w", err)
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("closing temp file: %w", err)
	}
	// Atomic replace
	if err := os.Rename(tmp, destPath); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("replacing thumbnail: %w", err)
	}
	// Remove stale WebP and preview frames so browser gets fresh content
	_ = os.Remove(m.getThumbnailPathWebp(destPath))
	for i := 1; i <= 20; i++ {
		p := m.getThumbnailPathByIndex(MediaID(mediaID), i)
		if _, err := os.Stat(p); os.IsNotExist(err) {
			break
		}
		_ = os.Remove(p)
		_ = os.Remove(m.getThumbnailPathWebp(p))
	}
	m.log.Info("Custom thumbnail saved for media %s → %s", mediaID, destPath)
	return nil
}

func (m *Module) generateThumbnailSyncFromRequest(req *ThumbnailSyncRequest) (string, error) {
	if m.ffmpegPath == "" {
		m.log.Error("Cannot generate thumbnail - FFmpeg not available")
		return "", fmt.Errorf(errFFmpegNotAvailable)
	}

	cfg := m.config.Get()
	outputPath := m.getThumbnailPath(MediaID(req.MediaID))

	// Check if already exists and is valid
	if isValidThumbnailFile(outputPath) {
		m.log.Debug("Thumbnail already exists: %s", outputPath)
		return outputPath, nil
	}
	_ = os.Remove(outputPath) // remove 0-byte corrupt file if present

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
