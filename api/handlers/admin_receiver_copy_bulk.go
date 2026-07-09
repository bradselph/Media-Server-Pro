package handlers

import (
	"context"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"media-server-pro/internal/receiver"
)

// receiverBulkCopyMaxIDs bounds how many items one bulk request may queue so a
// buggy client cannot enqueue an effectively unbounded amount of transfer work.
const receiverBulkCopyMaxIDs = 500

// Terminal per-item outcomes of a bulk copy.
const (
	receiverCopyStatusCopied  = "copied"
	receiverCopyStatusSkipped = "skipped"
	receiverCopyStatusFailed  = "failed"
)

// receiverBulkCopyResult is the terminal outcome of one item in a bulk copy.
type receiverBulkCopyResult struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Status string `json:"status"` // copied | skipped | failed
	Detail string `json:"detail,omitempty"`
}

// receiverBulkCopyStatus is the wire shape of a bulk-copy job snapshot,
// returned by all three /api/admin/receiver/copy-bulk methods.
type receiverBulkCopyStatus struct {
	Running    bool                     `json:"running"`
	Canceled   bool                     `json:"canceled"`
	Total      int                      `json:"total"`
	Done       int                      `json:"done"`
	Copied     int                      `json:"copied"`
	Skipped    int                      `json:"skipped"`
	Failed     int                      `json:"failed"`
	Current    string                   `json:"current,omitempty"`
	StartedAt  *time.Time               `json:"started_at,omitempty"`
	FinishedAt *time.Time               `json:"finished_at,omitempty"`
	Results    []receiverBulkCopyResult `json:"results"`
}

// receiverBulkCopyJob tracks one background bulk copy-to-library run. All
// mutable fields are guarded by mu; snapshot() returns a copy that is safe to
// marshal while the job keeps running.
type receiverBulkCopyJob struct {
	mu         sync.Mutex
	cancel     context.CancelFunc
	running    bool
	canceled   bool
	total      int
	done       int
	copied     int
	skipped    int
	failed     int
	current    string // display name of the item being transferred right now
	results    []receiverBulkCopyResult
	startedAt  time.Time
	finishedAt time.Time
}

func newReceiverBulkCopyJob(total int, cancel context.CancelFunc) *receiverBulkCopyJob {
	return &receiverBulkCopyJob{
		cancel:    cancel,
		running:   true,
		total:     total,
		results:   make([]receiverBulkCopyResult, 0, total),
		startedAt: time.Now(),
	}
}

func (j *receiverBulkCopyJob) isRunning() bool {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.running
}

func (j *receiverBulkCopyJob) setCurrent(name string) {
	j.mu.Lock()
	j.current = name
	j.mu.Unlock()
}

// record appends one item's terminal outcome and bumps the matching counter.
func (j *receiverBulkCopyJob) record(res receiverBulkCopyResult) {
	j.mu.Lock()
	j.results = append(j.results, res)
	j.done++
	switch res.Status {
	case receiverCopyStatusCopied:
		j.copied++
	case receiverCopyStatusSkipped:
		j.skipped++
	default:
		j.failed++
	}
	j.mu.Unlock()
}

// markCanceled flags the job as canceled (the worker also observes ctx and
// stops); safe to call at any time, including after completion.
func (j *receiverBulkCopyJob) markCanceled() {
	j.mu.Lock()
	if j.running {
		j.canceled = true
	}
	j.mu.Unlock()
	j.cancel()
}

// finish marks the job as done. canceled is sticky — a job canceled just as
// its last item completed still reports canceled=true.
func (j *receiverBulkCopyJob) finish(canceled bool) {
	j.mu.Lock()
	j.running = false
	j.canceled = j.canceled || canceled
	j.current = ""
	j.finishedAt = time.Now()
	j.mu.Unlock()
}

func (j *receiverBulkCopyJob) snapshot() receiverBulkCopyStatus {
	j.mu.Lock()
	defer j.mu.Unlock()
	// make (not append-to-nil) so an empty snapshot marshals as [] rather than
	// null — the frontend types declare results as a required array.
	results := make([]receiverBulkCopyResult, len(j.results))
	copy(results, j.results)
	st := receiverBulkCopyStatus{
		Running:  j.running,
		Canceled: j.canceled,
		Total:    j.total,
		Done:     j.done,
		Copied:   j.copied,
		Skipped:  j.skipped,
		Failed:   j.failed,
		Current:  j.current,
		Results:  results,
	}
	if !j.startedAt.IsZero() {
		t := j.startedAt
		st.StartedAt = &t
	}
	if !j.finishedAt.IsZero() {
		t := j.finishedAt
		st.FinishedAt = &t
	}
	return st
}

// receiverCopyGuardKey returns the in-flight guard key for a copy: the content
// fingerprint when known — the same content can be cataloged under different
// federated IDs (e.g. two slaves sharing a file), and both must not download
// concurrently — otherwise the federated ID.
func receiverCopyGuardKey(id string, item *receiver.MediaItem) string {
	if item != nil && item.ContentFingerprint != "" {
		return "fp:" + item.ContentFingerprint
	}
	return "id:" + id
}

// beginReceiverCopy claims the per-content copy slot. Returns false when
// another copy (single or bulk) of the same content is already in flight —
// the fingerprint duplicate check cannot see a copy that hasn't finished
// registering yet, so this closes the double-download window.
func (h *Handler) beginReceiverCopy(key string) bool {
	_, loaded := h.receiverCopyBusy.LoadOrStore(key, struct{}{})
	return !loaded
}

func (h *Handler) endReceiverCopy(key string) {
	h.receiverCopyBusy.Delete(key)
}

// AdminReceiverBulkCopyStart queues a background job that copies the given
// federated media items into the local library one at a time (sequential on
// purpose — the transfers share the peer's uplink). Items whose content already
// exists locally are skipped, so the admin can select liberally. Progress is
// polled via GET on the same path; DELETE cancels.
//
// POST /api/admin/receiver/copy-bulk  {"ids": ["...", ...]}
func (h *Handler) AdminReceiverBulkCopyStart(c *gin.Context) {
	if !h.checkReceiverEnabled(c) {
		return
	}
	// Bound the body before decoding — 64 KiB comfortably fits the 500-ID cap
	// (same pattern as AdminBulkUsers).
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 64*1024)
	var req struct {
		IDs []string `json:"ids"`
	}
	if !BindJSON(c, &req, errInvalidRequest) {
		return
	}

	// Dedupe while preserving the admin's selection order.
	seen := make(map[string]struct{}, len(req.IDs))
	ids := make([]string, 0, len(req.IDs))
	for _, id := range req.IDs {
		if id == "" {
			continue
		}
		if _, dup := seen[id]; dup {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		writeError(c, http.StatusBadRequest, "ids is required")
		return
	}
	if len(ids) > receiverBulkCopyMaxIDs {
		writeError(c, http.StatusBadRequest, "too many items in one bulk copy (max 500)")
		return
	}

	h.receiverBulkMu.Lock()
	if h.receiverBulkJob != nil && h.receiverBulkJob.isRunning() {
		h.receiverBulkMu.Unlock()
		writeError(c, http.StatusConflict, "a bulk copy is already running")
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	job := newReceiverBulkCopyJob(len(ids), cancel)
	h.receiverBulkJob = job
	h.receiverBulkMu.Unlock()

	go h.runReceiverBulkCopy(ctx, job, ids)

	h.trackServerEvent(c, "receiver_media_copy_bulk", map[string]any{"count": len(ids)})
	writeSuccess(c, job.snapshot())
}

// AdminReceiverBulkCopyStatus returns the latest bulk-copy job's progress, or
// an idle zero-status when no job has run since startup.
// GET /api/admin/receiver/copy-bulk
func (h *Handler) AdminReceiverBulkCopyStatus(c *gin.Context) {
	h.receiverBulkMu.Lock()
	job := h.receiverBulkJob
	h.receiverBulkMu.Unlock()
	if job == nil {
		writeSuccess(c, receiverBulkCopyStatus{Results: []receiverBulkCopyResult{}})
		return
	}
	writeSuccess(c, job.snapshot())
}

// AdminReceiverBulkCopyCancel stops the running bulk-copy job. The in-flight
// item's transfer is aborted; completed items stay in the library.
// DELETE /api/admin/receiver/copy-bulk
func (h *Handler) AdminReceiverBulkCopyCancel(c *gin.Context) {
	h.receiverBulkMu.Lock()
	job := h.receiverBulkJob
	h.receiverBulkMu.Unlock()
	if job == nil || !job.isRunning() {
		writeError(c, http.StatusConflict, "no bulk copy is running")
		return
	}
	job.markCanceled()
	writeSuccess(c, job.snapshot())
}

// runReceiverBulkCopy is the bulk job worker: copies each item sequentially,
// recording a terminal result per attempted item. Cancellation stops before
// the next item (and aborts the in-flight transfer via ctx).
func (h *Handler) runReceiverBulkCopy(ctx context.Context, job *receiverBulkCopyJob, ids []string) {
	defer job.cancel()
	// This goroutine runs outside gin.Recovery(), so a panic anywhere in the
	// copy chain (peer fetch, probe, indexing) would kill the whole process.
	// Contain it and finish the job so it can't stay "running" forever and
	// block future bulk copies. Same pattern as admin_classify.go's jobs.
	defer func() {
		if r := recover(); r != nil {
			job.finish(false)
			h.log.Error("Receiver bulk copy panicked: %v", r)
		}
	}()
	for _, id := range ids {
		if ctx.Err() != nil {
			break
		}
		job.record(h.copyOneReceiverItem(ctx, job, id))
	}
	job.finish(ctx.Err() != nil)
	st := job.snapshot()
	h.log.Info("Receiver bulk copy finished: %d copied, %d skipped, %d failed of %d (canceled=%v)",
		st.Copied, st.Skipped, st.Failed, st.Total, st.Canceled)
}

// copyOneReceiverItem copies a single federated item into the local library and
// returns its terminal result. Mirrors AdminReceiverCopyMedia's flow, with
// per-item errors reported as data instead of HTTP statuses.
func (h *Handler) copyOneReceiverItem(ctx context.Context, job *receiverBulkCopyJob, id string) receiverBulkCopyResult {
	res := receiverBulkCopyResult{ID: id, Name: id}
	item := h.receiver.GetMediaItem(id)
	if item == nil {
		res.Status = receiverCopyStatusFailed
		res.Detail = "no longer available from any peer"
		return res
	}
	res.Name = item.Name

	if item.ContentFingerprint != "" && h.media.HasFingerprint(item.ContentFingerprint) {
		res.Status = receiverCopyStatusSkipped
		res.Detail = "already in the local library"
		return res
	}
	guardKey := receiverCopyGuardKey(id, item)
	if !h.beginReceiverCopy(guardKey) {
		res.Status = receiverCopyStatusSkipped
		res.Detail = "another copy of this content is already in progress"
		return res
	}
	defer h.endReceiverCopy(guardKey)

	job.setCurrent(item.Name)
	defer job.setCurrent("")

	destDir, isAudio, errMsg := h.receiverCopyDestDir(item)
	if errMsg != "" {
		res.Status = receiverCopyStatusFailed
		res.Detail = errMsg
		return res
	}
	if err := os.MkdirAll(destDir, 0o750); err != nil {
		h.log.Error("bulk copy federated media: mkdir %s: %v", destDir, err)
		res.Status = receiverCopyStatusFailed
		res.Detail = "could not create the destination directory"
		return res
	}

	// Per-item child context: releases this item's reader-watchdog goroutine in
	// streamReceiverItemToDir as soon as the item finishes, instead of holding
	// one goroutine per item alive until the whole batch ends.
	itemCtx, itemCancel := context.WithCancel(ctx)
	defer itemCancel()
	destPath, err := h.streamReceiverItemToDir(itemCtx, id, item, destDir)
	if err != nil {
		h.log.Error("bulk copy federated media %s: %v", id, err)
		res.Status = receiverCopyStatusFailed
		if ctx.Err() != nil {
			res.Detail = "canceled mid-transfer"
		} else {
			res.Detail = "transfer from peer failed"
		}
		return res
	}

	if err := h.media.RegisterUploadedFile(destPath); err != nil {
		// Same contract as the single-copy path: never leave an unindexed file
		// behind for a later scan to pick up as a mystery item.
		_ = os.Remove(destPath)
		h.log.Error("bulk copy federated media %s: register %s: %v", id, destPath, err)
		res.Status = receiverCopyStatusFailed
		res.Detail = "copied the file but failed to index it"
		return res
	}

	// RegisterUploadedFile fingerprinted the freshly-written bytes, so from
	// here the item is recognized as already_local and further copies skip.
	h.applyReceiverCopyMetadata(destPath, item)
	if newItem, gErr := h.media.GetMedia(destPath); gErr == nil && newItem != nil && h.thumbnails != nil {
		h.thumbnails.QueueThumbnailIfMissing(destPath, newItem.ID, isAudio)
	}

	res.Status = receiverCopyStatusCopied
	return res
}
