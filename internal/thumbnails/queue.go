package thumbnails

import (
	"container/heap"
	"context"
	"os"
	"time"
)

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
