package handlers

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"media-server-pro/internal/downloader"
	"media-server-pro/internal/playlist"
	"media-server-pro/pkg/helpers"
)

// The admin "download a playlist's Hub items into the library" job. It mirrors
// the manual flow — download each hub:<embed_id> item's source URL with the
// downloader, then move (import) the resulting file into the library — but runs
// automatically over a whole playlist as a single, cancelable background job.
// Mirrors the receiverBulkCopyJob pattern (admin_receiver_copy_bulk.go).

const (
	hubPlaylistImportMaxItems = 500

	hubImportStatusImported = "imported"
	hubImportStatusSkipped  = "skipped"
	hubImportStatusFailed   = "failed"

	// Synthetic client id — the downloader routes WebSocket progress by clientId
	// but does not require it to map to a live socket (this job polls instead).
	hubImportClientID = "msp-hub-playlist-import"

	// Per-item safety cap: a single video download+settle can't exceed this.
	hubImportPerItemTimeout = 30 * time.Minute
	// Poll cadence for the downloader queue / importable dir.
	hubImportPollInterval = 3 * time.Second
	// Once a download leaves the queue, wait this long for its output file to
	// appear (ListImportable skips files <30s old) before declaring it failed.
	hubImportPostQueueGrace = 40 * time.Second
)

// hubPlaylistImportResult is one item's terminal outcome.
type hubPlaylistImportResult struct {
	EmbedID string `json:"embed_id"`
	Title   string `json:"title"`
	Status  string `json:"status"` // imported | skipped | failed
	Detail  string `json:"detail,omitempty"`
}

// hubPlaylistImportStatus is the wire shape returned by all three endpoints.
type hubPlaylistImportStatus struct {
	Running      bool                      `json:"running"`
	Canceled     bool                      `json:"canceled"`
	PlaylistID   string                    `json:"playlist_id,omitempty"`
	PlaylistName string                    `json:"playlist_name,omitempty"`
	Total        int                       `json:"total"`
	Done         int                       `json:"done"`
	Imported     int                       `json:"imported"`
	Skipped      int                       `json:"skipped"`
	Failed       int                       `json:"failed"`
	Current      string                    `json:"current,omitempty"`
	StartedAt    *time.Time                `json:"started_at,omitempty"`
	FinishedAt   *time.Time                `json:"finished_at,omitempty"`
	Results      []hubPlaylistImportResult `json:"results"`
}

// hubPlaylistImportJob tracks one background run. Mutable fields are guarded by mu.
type hubPlaylistImportJob struct {
	mu           sync.Mutex
	cancel       context.CancelFunc
	running      bool
	canceled     bool
	playlistID   string
	playlistName string
	total        int
	done         int
	imported     int
	skipped      int
	failed       int
	current      string
	results      []hubPlaylistImportResult
	startedAt    time.Time
	finishedAt   time.Time
}

func newHubPlaylistImportJob(playlistID, playlistName string, total int, cancel context.CancelFunc) *hubPlaylistImportJob {
	return &hubPlaylistImportJob{
		cancel:       cancel,
		running:      true,
		playlistID:   playlistID,
		playlistName: playlistName,
		total:        total,
		results:      make([]hubPlaylistImportResult, 0, total),
		startedAt:    time.Now(),
	}
}

func (j *hubPlaylistImportJob) isRunning() bool {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.running
}

func (j *hubPlaylistImportJob) setCurrent(name string) {
	j.mu.Lock()
	j.current = name
	j.mu.Unlock()
}

func (j *hubPlaylistImportJob) record(res hubPlaylistImportResult) {
	j.mu.Lock()
	j.results = append(j.results, res)
	j.done++
	switch res.Status {
	case hubImportStatusImported:
		j.imported++
	case hubImportStatusSkipped:
		j.skipped++
	default:
		j.failed++
	}
	j.mu.Unlock()
}

func (j *hubPlaylistImportJob) markCanceled() {
	j.mu.Lock()
	if j.running {
		j.canceled = true
	}
	j.mu.Unlock()
	j.cancel()
}

func (j *hubPlaylistImportJob) finish(canceled bool) {
	j.mu.Lock()
	j.running = false
	j.canceled = j.canceled || canceled
	j.current = ""
	j.finishedAt = time.Now()
	j.mu.Unlock()
}

func (j *hubPlaylistImportJob) snapshot() hubPlaylistImportStatus {
	j.mu.Lock()
	defer j.mu.Unlock()
	results := make([]hubPlaylistImportResult, len(j.results))
	copy(results, j.results)
	st := hubPlaylistImportStatus{
		Running:      j.running,
		Canceled:     j.canceled,
		PlaylistID:   j.playlistID,
		PlaylistName: j.playlistName,
		Total:        j.total,
		Done:         j.done,
		Imported:     j.imported,
		Skipped:      j.skipped,
		Failed:       j.failed,
		Current:      j.current,
		Results:      results,
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

// hubImportItem is one queued unit of work.
type hubImportItem struct {
	embedID string
	title   string
}

// AdminHubPlaylistImportStart starts a background job that downloads every Hub
// item in the given playlist via the downloader and imports each into the
// library. Sequential (one at a time) so the snapshot-diff correlation on the
// shared downloads dir is unambiguous. One job runs at a time, globally.
//
// POST /api/admin/hub/playlist-import  {"playlist_id","destination","subfolder","relay_id"}
func (h *Handler) AdminHubPlaylistImportStart(c *gin.Context) {
	if !h.requireHub(c) {
		return
	}
	if !h.checkDownloaderEnabled(c) {
		return
	}
	if h.playlist == nil {
		writeError(c, http.StatusServiceUnavailable, "Playlists feature is not available")
		return
	}
	if !h.downloader.IsOnline() {
		writeError(c, http.StatusServiceUnavailable, msgDownloaderOffline)
		return
	}

	var req struct {
		PlaylistID  string `json:"playlist_id"`
		Destination string `json:"destination"`
		Subfolder   string `json:"subfolder"`
		RelayID     string `json:"relay_id"`
	}
	if !BindJSON(c, &req, errInvalidRequest) {
		return
	}
	if req.PlaylistID == "" {
		writeError(c, http.StatusBadRequest, "playlist_id is required")
		return
	}

	// Admin can import ANY user's playlist — use GetPlaylist (not GetPlaylistForUser).
	pl, err := h.playlist.GetPlaylist(playlist.PlaylistID(req.PlaylistID))
	if err != nil || pl == nil {
		writeError(c, http.StatusNotFound, msgPlaylistNotFound)
		return
	}

	items := make([]hubImportItem, 0, len(pl.Items))
	for _, it := range pl.Items {
		if embedID, ok := strings.CutPrefix(it.MediaID, hubItemPrefix); ok && embedID != "" {
			items = append(items, hubImportItem{embedID: embedID, title: it.Title})
		}
	}
	if len(items) == 0 {
		writeError(c, http.StatusBadRequest, "Playlist has no Hub items to import")
		return
	}
	if len(items) > hubPlaylistImportMaxItems {
		items = items[:hubPlaylistImportMaxItems]
	}

	h.hubPlaylistMu.Lock()
	if h.hubPlaylistJob != nil && h.hubPlaylistJob.isRunning() {
		h.hubPlaylistMu.Unlock()
		writeError(c, http.StatusConflict, "a playlist import is already running")
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	job := newHubPlaylistImportJob(pl.ID, pl.Name, len(items), cancel)
	h.hubPlaylistJob = job
	h.hubPlaylistMu.Unlock()

	go h.runHubPlaylistImport(ctx, job, items, req.Destination, req.Subfolder, req.RelayID)

	h.logAdminAction(c, &adminLogActionParams{Action: "hub_playlist_import", Target: "hub", Details: map[string]any{"playlist_id": pl.ID, "count": len(items)}})
	writeSuccess(c, job.snapshot())
}

// AdminHubPlaylistImportStatus returns the latest job's progress (idle when none).
// GET /api/admin/hub/playlist-import/status
func (h *Handler) AdminHubPlaylistImportStatus(c *gin.Context) {
	if !h.requireHub(c) {
		return
	}
	h.hubPlaylistMu.Lock()
	job := h.hubPlaylistJob
	h.hubPlaylistMu.Unlock()
	if job == nil {
		writeSuccess(c, hubPlaylistImportStatus{Results: []hubPlaylistImportResult{}})
		return
	}
	writeSuccess(c, job.snapshot())
}

// AdminHubPlaylistImportCancel stops the running job (imported items stay).
// DELETE /api/admin/hub/playlist-import
func (h *Handler) AdminHubPlaylistImportCancel(c *gin.Context) {
	if !h.requireHub(c) {
		return
	}
	h.hubPlaylistMu.Lock()
	job := h.hubPlaylistJob
	h.hubPlaylistMu.Unlock()
	if job == nil || !job.isRunning() {
		writeError(c, http.StatusConflict, "no playlist import is running")
		return
	}
	job.markCanceled()
	writeSuccess(c, job.snapshot())
}

// runHubPlaylistImport is the worker: downloads+imports each hub item sequentially.
func (h *Handler) runHubPlaylistImport(ctx context.Context, job *hubPlaylistImportJob, items []hubImportItem, destination, subfolder, relayID string) {
	defer job.cancel()
	// This goroutine runs outside gin.Recovery(); contain a panic so the process
	// survives and the job can't stay "running" forever (mirrors the receiver job).
	defer func() {
		if r := recover(); r != nil {
			job.finish(false)
			h.log.Error("Hub playlist import panicked: %v", r)
		}
	}()

	importedAny := false
	for _, item := range items {
		if ctx.Err() != nil {
			break
		}
		res := h.downloadAndImportHubItem(ctx, job, item, destination, subfolder, relayID)
		if res.Status == hubImportStatusImported {
			importedAny = true
		}
		job.record(res)
	}

	// Trigger exactly one media rescan at the end (per-item triggerScan is off) so
	// the freshly imported files enter the catalog, matching the manual flow's
	// single post-import scan.
	if importedAny && ctx.Err() == nil && h.media != nil {
		if err := h.media.Scan(); err != nil {
			h.log.Warn("Hub playlist import: post-import rescan failed: %v", err)
		}
	}

	job.finish(ctx.Err() != nil)
	st := job.snapshot()
	h.log.Info("Hub playlist import finished: %d imported, %d skipped, %d failed of %d (canceled=%v)",
		st.Imported, st.Skipped, st.Failed, st.Total, st.Canceled)
}

// downloadAndImportHubItem downloads one hub item's source and imports the result.
// It follows the manual admin flow: start a server download, wait for it to
// finish, then move the produced file into the library.
func (h *Handler) downloadAndImportHubItem(ctx context.Context, job *hubPlaylistImportJob, item hubImportItem, destination, subfolder, relayID string) hubPlaylistImportResult {
	res := hubPlaylistImportResult{EmbedID: item.embedID, Title: item.title}
	if res.Title == "" {
		res.Title = item.embedID
	}
	job.setCurrent(res.Title)
	defer job.setCurrent("")

	// Canonical pornhub video page (the embed id is the viewkey); yt-dlp resolves it.
	url := "https://www.pornhub.com/view_video.php?viewkey=" + item.embedID
	if err := helpers.ValidateURLForSSRF(url); err != nil {
		res.Status = hubImportStatusFailed
		res.Detail = "invalid source URL"
		return res
	}

	// Snapshot the importable set BEFORE this download so the file it produces is
	// identifiable as the one new name afterwards.
	beforeNames := h.hubImportableNames()

	resp, err := h.downloader.GetClient().Download(downloader.DownloadParams{
		URL:          url,
		Title:        item.title,
		SaveLocation: "server",
		ClientID:     hubImportClientID,
		RelayID:      relayID,
	}, "")
	if err != nil || resp == nil || !resp.Success {
		res.Status = hubImportStatusFailed
		if err != nil {
			res.Detail = "download start failed: " + err.Error()
		} else if resp != nil && resp.Message != "" {
			res.Detail = "download rejected: " + resp.Message
		} else {
			res.Detail = "download start failed"
		}
		return res
	}

	newFile, waitErr := h.waitForHubDownload(ctx, resp.DownloadID, beforeNames)
	if waitErr != "" {
		res.Status = hubImportStatusFailed
		res.Detail = waitErr
		return res
	}

	// Import exactly like the manual flow: copy into the library, delete the
	// source, but DON'T rescan per item (batched into one rescan at the end).
	if _, _, err := h.downloader.Import(newFile, destination, subfolder, true, false); err != nil {
		res.Status = hubImportStatusFailed
		res.Detail = "import failed: " + err.Error()
		return res
	}
	res.Status = hubImportStatusImported
	return res
}

// hubImportableNames returns the current set of importable file names.
func (h *Handler) hubImportableNames() map[string]bool {
	files, _ := h.downloader.ListImportable()
	names := make(map[string]bool, len(files))
	for _, f := range files {
		names[f.Name] = true
	}
	return names
}

// waitForHubDownload waits for a started download to produce a new importable
// file. Returns (newFileName, "") on success or ("", reason) on failure/timeout/
// cancel. Success = a name not in beforeNames appears. Fast-fail = the download
// leaves the queue and no new file appears within hubImportPostQueueGrace.
func (h *Handler) waitForHubDownload(ctx context.Context, downloadID string, beforeNames map[string]bool) (string, string) {
	deadline := time.Now().Add(hubImportPerItemTimeout)
	var leftQueueAt time.Time
	for {
		if ctx.Err() != nil {
			return "", "canceled"
		}
		if time.Now().After(deadline) {
			return "", "timed out waiting for the download to finish"
		}

		// Success: a new importable file appeared.
		if files, err := h.downloader.ListImportable(); err == nil {
			for i := range files {
				if !beforeNames[files[i].Name] {
					return files[i].Name, ""
				}
			}
		}

		// Fast-fail: once the job has left the downloader queue (Active+Queued)
		// and stayed gone past the settle grace with no output file, it failed.
		if q, err := h.downloader.GetClient().Queue(); err == nil {
			if hubDownloadInQueue(q, downloadID) {
				leftQueueAt = time.Time{}
			} else if leftQueueAt.IsZero() {
				leftQueueAt = time.Now()
			} else if time.Since(leftQueueAt) > hubImportPostQueueGrace {
				return "", "download produced no file (source may be geo-blocked, removed, or unsupported)"
			}
		}

		select {
		case <-ctx.Done():
			return "", "canceled"
		case <-time.After(hubImportPollInterval):
		}
	}
}

// hubDownloadInQueue reports whether the download id is still active or queued.
func hubDownloadInQueue(q *downloader.QueueResponse, downloadID string) bool {
	if q == nil {
		return false
	}
	for _, a := range q.Active {
		if a.DownloadID == downloadID {
			return true
		}
	}
	for _, qi := range q.Queued {
		if qi.DownloadID == downloadID {
			return true
		}
	}
	return false
}
