package thumbnails

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

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
