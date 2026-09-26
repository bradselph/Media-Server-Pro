package hls

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"maps"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"media-server-pro/internal/config"
	"media-server-pro/pkg/models"

	ffmpeg "github.com/u2takey/ffmpeg-go"
)

// qualityRunParams holds progress-tracking parameters for a single quality transcode run.
type qualityRunParams struct {
	JobID          string
	TotalQualities int
	CurrentQuality int
	TotalDuration  float64
}

// transcodePaths holds paths for a single variant transcode (output dir and segment pattern).
type transcodePaths struct {
	MediaPath      string
	PlaylistPath   string
	SegmentPattern string
}

// transcodeErrorContext holds context for reporting a failed transcode (job, quality, paths, stderr).
type transcodeErrorContext struct {
	JobID      string
	Quality    string
	VariantDir string
	StderrStr  string
}

// maxStderrTailBytes bounds the ffmpeg stderr tail kept for error reporting.
// handleTranscodeWaitError only ever logs the last ~1000 chars, and
// isTranscodeCancelled only substring-matches ffmpeg's "Exiting normally,
// received signal" line near the end of output — so there's no need to retain
// an entire transcode's worth of stderr, which (once monitorProgress splits
// on every '\r'-refreshed progress line) can otherwise grow unbounded over a
// long encode.
const maxStderrTailBytes = 64 * 1024

// stderrTailBuffer is an io.Writer that retains only the most recently
// written maxStderrTailBytes, discarding older bytes as new ones arrive. It
// is used to tee ffmpeg's stderr for error reporting without buffering the
// entire (potentially unbounded) stream in memory.
type stderrTailBuffer struct {
	buf []byte
}

func (t *stderrTailBuffer) Write(p []byte) (int, error) {
	t.buf = append(t.buf, p...)
	if excess := len(t.buf) - maxStderrTailBytes; excess > 0 {
		n := copy(t.buf, t.buf[excess:])
		t.buf = t.buf[:n]
	}
	return len(p), nil
}

func (t *stderrTailBuffer) String() string {
	return string(t.buf)
}

// transcode performs the actual transcoding. Qualities are encoded
// sequentially in ladder order; after EACH one finishes, master.m3u8 is
// atomically (re)published (see publishMasterPlaylist) to list every quality
// done so far, and the job is flagged Available (see markJobPlayable) the
// instant the FIRST quality finishes. This is what makes the stream playable
// well before the whole ladder completes, instead of only once every
// configured quality has been transcoded.
func (m *Module) transcode(ctx context.Context, job *models.HLSJob) {
	if !m.acquireTranscodeSem(ctx, job) {
		return
	}
	defer m.releaseTranscode()

	m.updateJobStatus(&updateJobStatusParams{JobID: job.ID, Status: models.HLSStatusRunning, Progress: 0})
	if err := m.createLock(job.ID, job.MediaPath); err != nil {
		m.log.Warn("Failed to create lock file: %v", err)
	}
	defer m.removeLock(job.ID)

	totalDuration := m.getMediaDuration(ctx, job.MediaPath)
	if totalDuration > 0 {
		m.log.Debug("Media duration for %s: %.1fs", job.ID, totalDuration)
	}

	qualitiesToTranscode := m.resolveQualitiesToTranscode(job)
	runParams := &qualityRunParams{
		JobID: job.ID, TotalQualities: len(qualitiesToTranscode), TotalDuration: totalDuration,
	}

	// completed accumulates qualities in ladder order as each one finishes, so
	// publishMasterPlaylist always (re)writes master.m3u8 with exactly the set
	// that is genuinely on disk right now — never more (C01's invariant: the
	// master must only advertise variants whose playlist actually exists;
	// lazy-transcode mode, which intentionally advertises ahead of what's on
	// disk, is unaffected — see resolveQualitiesToTranscode/H4).
	completed := make([]string, 0, len(qualitiesToTranscode))
	for i, quality := range qualitiesToTranscode {
		runParams.CurrentQuality = i + 1
		if err := m.transcodeQuality(ctx, job, quality, runParams); err != nil {
			m.finalizeAfterQualityFailure(ctx, job, quality, completed)
			return
		}
		completed = append(completed, quality)
		if !m.publishMasterPlaylist(job, completed) {
			return
		}
		if len(completed) == 1 {
			m.markJobPlayable(job)
		}
	}

	m.finalizeJobCompleted(job)
	m.log.Info("HLS generation completed for job %s", job.ID)
}

// publishMasterPlaylist atomically (re)writes master.m3u8 to advertise every
// quality in completed (see generateMasterPlaylist). Called by transcode()
// after each quality finishes so the stream becomes playable incrementally
// (see markJobPlayable) instead of only once the whole ladder is done.
// Returns false when the job has already been finalized (Failed or
// Completed) as a result of a publish failure, telling the caller's loop to
// stop; true means the caller should proceed to the next quality.
func (m *Module) publishMasterPlaylist(job *models.HLSJob, completed []string) bool {
	err := m.generateMasterPlaylist(&generateMasterPlaylistParams{OutputDir: job.OutputDir, Variants: completed})
	if err == nil {
		return true
	}

	failedQuality := completed[len(completed)-1]
	variantDir := filepath.Join(job.OutputDir, failedQuality)
	if removeErr := os.RemoveAll(variantDir); removeErr != nil {
		m.log.Warn("Failed to clean up variant dir %s after master playlist failure: %v", variantDir, removeErr)
	}

	if len(completed) == 1 {
		// Nothing was ever published (this was the first quality) — same
		// unrecoverable-loss outcome as failing to write the master playlist
		// used to be for the whole job.
		m.updateJobStatus(&updateJobStatusParams{JobID: job.ID, Status: models.HLSStatusFailed, ErrorMsg: fmt.Sprintf("Failed to create master playlist: %v", err), Progress: 0})
		return false
	}

	// A later quality's own encode succeeded, but publishing the updated master
	// failed. generateMasterPlaylist writes to a temp file and renames it into
	// place, so the previous, already-published master — listing
	// completed[:len(completed)-1], already playable — is untouched on disk.
	// There's no reason to tear down an in-progress viewer's stream over a
	// transient publish error, so finalize with that last-known-good subset
	// instead of failing the whole job.
	m.log.Warn("Failed to publish updated master playlist for job %s after quality %s: %v; finalizing with the last published subset", job.ID, failedQuality, err)
	m.finalizeJobCompletedWithSubset(job, completed[:len(completed)-1])
	return false
}

// finalizeAfterQualityFailure decides how to conclude a job when
// transcodeQuality has already returned an error for `quality` (and has
// already recorded Failed/Canceled status and cleaned up its own partial
// output — see handleTranscodeWaitError). completed is every quality that
// published successfully before this failure, in ladder order.
//
//   - First quality (completed empty): nothing usable exists yet. Leave the
//     Failed/Canceled status transcodeQuality already recorded, exactly as
//     before this feature.
//   - Later quality, canceled (server shutdown or an explicit CancelJob call):
//     keep the job exactly as transcodeQuality left it — Canceled, but still
//     Available and playable from the earlier subset. Do not relabel it
//     Completed: master.m3u8 already only lists `completed`, so there is
//     nothing to fix up, and DeleteJob/cleanup still remove everything
//     eventually.
//   - Later quality, genuinely failed (not canceled): the earlier subset is
//     already playable and is already the only thing master.m3u8 advertises,
//     so finalize the job as Completed with that subset instead of leaving it
//     stuck Failed.
func (m *Module) finalizeAfterQualityFailure(ctx context.Context, job *models.HLSJob, quality string, completed []string) {
	if len(completed) == 0 {
		return
	}
	if ctx.Err() != nil || m.stopping.Load() {
		m.log.Warn("HLS job %s canceled after %d/%d qualities completed; keeping the completed subset usable", job.ID, len(completed), len(job.Qualities))
		return
	}
	m.log.Warn("HLS quality %s failed for job %s after %d earlier quality/qualities succeeded; finalizing with the completed subset", quality, job.ID, len(completed))
	m.finalizeJobCompletedWithSubset(job, completed)
}

// markJobPlayable flips a job's Available flag and stamps HLSUrl once its
// first quality has finished transcoding and been published to master.m3u8
// (see publishMasterPlaylist). From this point the stream is playable even
// though Status stays "running" while later qualities keep transcoding —
// applyHLSCompletionFields (api/handlers/hls.go) is what surfaces this to
// /api/hls/check and /api/hls/status.
func (m *Module) markJobPlayable(job *models.HLSJob) {
	m.jobsMu.Lock()
	job.Available = true
	job.HLSUrl = hlsURLForJob(job.ID)
	jobCopy := copyHLSJob(job)
	m.jobsMu.Unlock()
	if err := m.saveJob(jobCopy); err != nil {
		m.log.Warn("Failed to persist HLS job %s availability after first quality completed: %v", job.ID, err)
	}
}

func (m *Module) acquireTranscodeSem(ctx context.Context, job *models.HLSJob) bool {
	// Re-read job.ID's tracked priority on every tick (rather than capturing it
	// once) so a job queued at low priority (background pre-generation) that
	// gets upgraded mid-spin by upgradeJobPriority immediately starts
	// contending for a slot as high priority.
	if m.waitForTranscodeSlot(ctx, func() bool { return m.isJobHighPriority(job.ID) }) {
		return true
	}
	m.updateJobStatus(&updateJobStatusParams{JobID: job.ID, Status: models.HLSStatusCanceled, ErrorMsg: "Context canceled", Progress: 0})
	return false
}

// waitForTranscodeSlot spin-waits (the dynamic semaphore doesn't support
// channel-based select) until a transcode slot is available or ctx is done.
// priorityFn is re-evaluated on every tick, not just once, so a caller whose
// priority is upgraded while it's already spinning (see upgradeJobPriority)
// picks that up immediately. While priorityFn reports high priority, the
// caller is registered on m.waitingHigh for the entire remaining spin (not
// just the tick that observed it) so low-priority acquisitions yield to it —
// see tryAcquireTranscode.
func (m *Module) waitForTranscodeSlot(ctx context.Context, priorityFn func() bool) bool {
	markedHigh := false
	defer func() {
		if markedHigh {
			m.waitingHigh.Add(-1)
		}
	}()
	for {
		highPriority := priorityFn()
		if highPriority && !markedHigh {
			m.waitingHigh.Add(1)
			markedHigh = true
		}
		if m.tryAcquireTranscode(highPriority) {
			return true
		}
		select {
		case <-ctx.Done():
			return false
		case <-time.After(250 * time.Millisecond):
			// retry
		}
	}
}

func (m *Module) resolveQualitiesToTranscode(job *models.HLSJob) []string {
	cfg := m.config.Get()
	qualities := job.Qualities
	if cfg.HLS.LazyTranscode && len(qualities) > 1 {
		qualities = qualities[:1]
		m.log.Info("Lazy transcode: only generating %s upfront for job %s", qualities[0], job.ID)
	}
	return qualities
}

func (m *Module) finalizeJobCompleted(job *models.HLSJob) {
	m.jobsMu.Lock()
	job.Status = models.HLSStatusCompleted
	job.Progress = 100
	job.CompletedAt = new(time.Now())
	if cancel, ok := m.jobCancels[job.ID]; ok {
		cancel()
		delete(m.jobCancels, job.ID)
	}
	// Do NOT delete from jobDone here. finalizeJobCompleted runs inside transcode(),
	// which is called from the goroutine body. The goroutine's "defer close(doneCh)"
	// hasn't fired yet at this point. Deleting jobDone[id] before close(doneCh)
	// creates a window where DeleteJob sees no doneCh, skips its wait, and races
	// os.RemoveAll against the goroutine's own deferred cleanup (e.g. removeLock).
	// The entry is cleaned up by DeleteJob (when explicitly deleted) or by
	// cleanInactiveJob (when evicted by the inactive-jobs cleanup pass).
	//
	// Snapshot the just-completed job under the lock and persist only that single
	// row after releasing it. Previously this called saveJobs(), which re-persisted
	// EVERY job in memory on every completion — O(N) DB writes per completion that
	// grew to O(N^2) over a server's lifetime since jobs are never auto-pruned. The
	// deep copy also keeps the DB write off the hot jobsMu path (a slow write no
	// longer stalls other job readers/writers) while staying race-free.
	jobCopy := copyHLSJob(job)
	m.jobsMu.Unlock()
	if err := m.saveJob(jobCopy); err != nil {
		m.log.Warn("Failed to save job state after completion: %v", err)
	}
}

// finalizeJobCompletedWithSubset finalizes job as Completed via
// finalizeJobCompleted, first truncating job.Qualities to actualQualities —
// the subset that is genuinely servable (what master.m3u8 actually lists) —
// so API responses (job.Qualities is returned as-is by GetHLSStatus/
// CheckHLSAvailability) never advertise more qualities than actually exist on
// disk. Used when a later quality's transcode, or publishing the updated
// master playlist, fails partway through the ladder (see
// finalizeAfterQualityFailure/publishMasterPlaylist); the normal end-of-ladder
// success path calls finalizeJobCompleted directly since job.Qualities is
// already exactly what was transcoded in that case.
func (m *Module) finalizeJobCompletedWithSubset(job *models.HLSJob, actualQualities []string) {
	m.jobsMu.Lock()
	job.Qualities = actualQualities
	m.jobsMu.Unlock()
	m.finalizeJobCompleted(job)
}

// transcodeQuality transcodes a single quality variant for a job.
func (m *Module) transcodeQuality(ctx context.Context, job *models.HLSJob, quality string, run *qualityRunParams) error {
	profile := m.getQualityProfile(quality)
	if profile == nil {
		m.log.Warn("Unknown quality profile: %s", quality)
		return fmt.Errorf("unknown quality profile: %s", quality)
	}

	variantDir, playlistPath, segmentPattern, err := m.prepareVariantDir(job, quality)
	if err != nil {
		m.updateJobStatus(&updateJobStatusParams{JobID: job.ID, Status: models.HLSStatusFailed, ErrorMsg: err.Error(), Progress: 0})
		return err
	}

	m.log.Info("Generating HLS variant %s (%dx%d @ %dkbps)", quality, profile.Width, profile.Height, profile.Bitrate/1000)

	paths := &transcodePaths{MediaPath: job.MediaPath, PlaylistPath: playlistPath, SegmentPattern: segmentPattern}
	cmdWithContext := m.buildFFmpegTranscodeCmd(ctx, paths, profile)

	var stderrBuf stderrTailBuffer
	stderrPipe, err := cmdWithContext.StderrPipe()
	if err != nil {
		m.updateJobStatus(&updateJobStatusParams{JobID: job.ID, Status: models.HLSStatusFailed, ErrorMsg: fmt.Sprintf("Failed to create stderr pipe: %v", err), Progress: 0})
		return err
	}
	if err := cmdWithContext.Start(); err != nil {
		_ = stderrPipe.Close()
		m.updateJobStatus(&updateJobStatusParams{JobID: job.ID, Status: models.HLSStatusFailed, ErrorMsg: fmt.Sprintf("Failed to start ffmpeg: %v", err), Progress: 0})
		return err
	}

	progressDone := make(chan struct{})
	go func() {
		defer close(progressDone)
		m.monitorProgress(job.ID, io.TeeReader(stderrPipe, &stderrBuf), run)
	}()

	waitErr := cmdWithContext.Wait()
	<-progressDone

	if waitErr != nil {
		errCtx := &transcodeErrorContext{JobID: job.ID, Quality: quality, VariantDir: variantDir, StderrStr: stderrBuf.String()}
		return m.handleTranscodeWaitError(ctx, errCtx, waitErr)
	}

	if _, err := os.Stat(playlistPath); err != nil {
		m.log.Error("Playlist not created for quality %s: %v", quality, err)
		m.updateJobStatus(&updateJobStatusParams{JobID: job.ID, Status: models.HLSStatusFailed, ErrorMsg: fmt.Sprintf("Playlist not created for %s", quality), Progress: 0})
		return fmt.Errorf("playlist not created for %s", quality)
	}

	m.log.Info("Successfully generated HLS variant %s", quality)
	return nil
}

func (m *Module) prepareVariantDir(job *models.HLSJob, quality string) (variantDir, playlistPath, segmentPattern string, err error) {
	variantDir = filepath.Join(job.OutputDir, quality)
	if err = os.MkdirAll(variantDir, 0o755); err != nil { //nolint:gosec // G301: HLS variant dirs need world-read for serving
		return "", "", "", fmt.Errorf("failed to create variant dir: %w", err)
	}
	playlistPath = filepath.Join(variantDir, "playlist.m3u8")
	segmentPattern = filepath.Join(variantDir, "segment_%04d.ts")
	return variantDir, playlistPath, segmentPattern, nil
}

// buildFFmpegTranscodeCmd builds the ffmpeg command for HLS transcoding.
// The video encoder (software libx264 or a hardware encoder resolved at
// startup) is chosen by buildVideoEncodeArgs. Keyframes are placed at segment
// boundaries via force_key_frames (frame-rate independent).
func (m *Module) buildFFmpegTranscodeCmd(ctx context.Context, paths *transcodePaths, profile *config.HLSQuality) *exec.Cmd {
	cfg := m.config.Get()
	// Resolve S3 keys to presigned URLs so ffmpeg can fetch the source over HTTPS.
	mediaInput := m.resolveMediaInputPath(ctx, paths.MediaPath)

	inputArgs, videoArgs := m.buildVideoEncodeArgs(profile, cfg.HLS.SegmentDuration)

	outArgs := ffmpeg.KwArgs{
		"c:a": "aac",
		"b:a": fmt.Sprintf("%dk", profile.AudioBitrate/1000),
		"ac":  "2",

		"f":                    "hls",
		"hls_time":             strconv.Itoa(cfg.HLS.SegmentDuration),
		"hls_playlist_type":    "vod",
		"hls_segment_type":     "mpegts",
		"hls_list_size":        "0",
		"hls_segment_filename": paths.SegmentPattern,
		"hls_flags":            "independent_segments",
	}
	maps.Copy(outArgs, videoArgs)

	var stream *ffmpeg.Stream
	if len(inputArgs) > 0 {
		stream = ffmpeg.Input(mediaInput, inputArgs)
	} else {
		stream = ffmpeg.Input(mediaInput)
	}
	stream = stream.Output(paths.PlaylistPath, outArgs).OverWriteOutput().SetFfmpegPath(m.ffmpegPath)

	compiled := stream.Compile()
	cmd := exec.CommandContext(ctx, compiled.Path, compiled.Args[1:]...) //nolint:gosec // G204: compiled.Path is the validated ffmpeg binary path
	cmd.Env = compiled.Env
	cmd.Dir = compiled.Dir
	return cmd
}

// buildVideoEncodeArgs returns the ffmpeg input options and video-encode output
// options for the resolved encoder. Software libx264 is the default; a hardware
// encoder (resolved once at startup in detectHWEncoder) is used when available.
//
// All paths target the same per-segment keyframe alignment and bitrate ceiling
// so the HLS output is interchangeable regardless of which encoder produced it.
func (m *Module) buildVideoEncodeArgs(profile *config.HLSQuality, segmentDuration int) (inputArgs, videoArgs ffmpeg.KwArgs) {
	bv := fmt.Sprintf("%dk", profile.Bitrate/1000)
	bufsize := fmt.Sprintf("%dk", profile.Bitrate*2/1000)
	forceKey := fmt.Sprintf("expr:gte(t,n_forced*%d)", segmentDuration)
	scale := fmt.Sprintf("scale=%d:%d", profile.Width, profile.Height)

	switch m.hwEncoder {
	case "h264_nvenc", "h264_qsv", "h264_videotoolbox":
		// These encoders accept CPU-decoded frames, so CPU scaling is fine.
		// Preset names differ across ffmpeg builds, so we omit preset and rely
		// on the bitrate ceiling for portability.
		return nil, ffmpeg.KwArgs{
			"c:v":              m.hwEncoder,
			"vf":               scale,
			"b:v":              bv,
			"maxrate":          bv,
			"bufsize":          bufsize,
			"force_key_frames": forceKey,
		}
	case "h264_vaapi":
		// VAAPI scales on the GPU after uploading NV12 frames; the render
		// device is passed as an input option.
		in := ffmpeg.KwArgs{}
		if m.hwDevice != "" {
			in["vaapi_device"] = m.hwDevice
		}
		return in, ffmpeg.KwArgs{
			"c:v":              "h264_vaapi",
			"vf":               fmt.Sprintf("format=nv12,hwupload,scale_vaapi=%d:%d", profile.Width, profile.Height),
			"b:v":              bv,
			"maxrate":          bv,
			"bufsize":          bufsize,
			"force_key_frames": forceKey,
		}
	default: // software libx264
		return nil, ffmpeg.KwArgs{
			"c:v":              "libx264",
			"preset":           "fast",
			"vf":               scale,
			"b:v":              bv,
			"maxrate":          bv,
			"bufsize":          bufsize,
			"force_key_frames": forceKey,
			"sc_threshold":     "0",
		}
	}
}

func (m *Module) handleTranscodeWaitError(ctx context.Context, errCtx *transcodeErrorContext, waitErr error) error {
	if m.isTranscodeCancelled(ctx, errCtx.StderrStr) {
		m.log.Info("HLS transcoding canceled for job %s quality %s", errCtx.JobID, errCtx.Quality)
		m.updateJobStatus(&updateJobStatusParams{JobID: errCtx.JobID, Status: models.HLSStatusCanceled, ErrorMsg: "Transcoding canceled", Progress: 0})
		return waitErr
	}
	if errOutput := strings.TrimSpace(errCtx.StderrStr); errOutput != "" {
		if len(errOutput) > 1000 {
			errOutput = "...(truncated)\n" + errOutput[len(errOutput)-1000:]
		}
		m.log.Error("ffmpeg stderr for job %s quality %s:\n%s", errCtx.JobID, errCtx.Quality, errOutput)
	}
	m.log.Warn("Transcoding failed for job %s quality %s, cleaning up partial output", errCtx.JobID, errCtx.Quality)
	if removeErr := os.RemoveAll(errCtx.VariantDir); removeErr != nil {
		m.log.Error("Failed to clean up partial HLS variant at %s: %v", errCtx.VariantDir, removeErr)
	}
	m.updateJobStatus(&updateJobStatusParams{JobID: errCtx.JobID, Status: models.HLSStatusFailed, ErrorMsg: fmt.Sprintf("Transcoding failed for %s: %v", errCtx.Quality, waitErr), Progress: 0})
	return waitErr
}

func (m *Module) isTranscodeCancelled(ctx context.Context, stderrStr string) bool {
	signalKilled := strings.Contains(stderrStr, "Exiting normally, received signal")
	return ctx.Err() != nil || m.stopping.Load() || signalKilled
}

// lazyTranscodeQuality transcodes a single quality on-demand with per-quality locking.
// M-16: semaphore is acquired BEFORE the per-quality mutex to prevent deadlock.
// Holding qMu while blocking on the semaphore could deadlock when all slots are
// occupied by goroutines that are also waiting to acquire qMu for the same quality.
func (m *Module) lazyTranscodeQuality(ctx context.Context, job *models.HLSJob, quality string) error {
	playlistPath := filepath.Join(job.OutputDir, quality, "playlist.m3u8")

	// Fast path: avoid semaphore contention if already done (racy read, re-checked under lock below).
	if _, err := os.Stat(playlistPath); err == nil {
		return nil
	}

	// Register this in-flight lazy transcode on the per-job WaitGroup BEFORE the
	// semaphore wait, so DeleteJob/cleanInactiveJob drain queued-and-running lazy
	// transcodes (which hold no jobDone entry) before os.RemoveAll deletes the
	// output directory out from under an active ffmpeg write.
	rawWg, _ := m.lazyWg.LoadOrStore(job.ID, new(sync.WaitGroup))
	lWg := rawWg.(*sync.WaitGroup)
	lWg.Add(1)
	defer lWg.Done()

	// Derive a context DeleteJob/cleanInactiveJob can cancel (via cancelLazyTranscodes)
	// so a delete aborts this on-demand encode — killing ffmpeg — instead of blocking
	// the lazyWg drain until it finishes on its own. Register the cancel under a unique
	// key so concurrent transcodes of the same job (different qualities) each get their
	// own entry.
	lazyCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	rawSet, _ := m.lazyCancels.LoadOrStore(job.ID, &sync.Map{})
	cancelSet := rawSet.(*sync.Map)
	cancelKey := new(int)
	cancelSet.Store(cancelKey, cancel)
	defer cancelSet.Delete(cancelKey)

	// Acquire dynamic semaphore with context awareness. Lazy transcodes are
	// always a live, user-triggered request (a viewer's player asked for a
	// quality on demand), so this always spins as high priority.
	if !m.waitForTranscodeSlot(lazyCtx, func() bool { return true }) {
		return lazyCtx.Err()
	}
	defer m.releaseTranscode()

	m.activeJobs.Add(1)
	defer m.activeJobs.Done()

	lockKey := job.ID + "/" + quality
	mu, _ := m.qualityLocks.LoadOrStore(lockKey, &sync.Mutex{})
	qMu, ok := mu.(*sync.Mutex)
	if !ok {
		return fmt.Errorf("internal lock type error for key %s", lockKey)
	}
	qMu.Lock()
	defer qMu.Unlock()

	// Re-check under lock — another goroutine may have completed while we waited for semaphore.
	if _, err := os.Stat(playlistPath); err == nil {
		return nil
	}

	m.log.Info("On-demand lazy transcode of quality %s for job %s", quality, job.ID)

	totalDuration := m.getMediaDuration(lazyCtx, job.MediaPath)
	run := &qualityRunParams{JobID: job.ID, TotalQualities: 1, CurrentQuality: 1, TotalDuration: totalDuration}
	return m.transcodeQuality(lazyCtx, job, quality, run)
}

// scanFFmpegLines is like bufio.ScanLines but also splits on a bare '\r',
// matching ffmpeg's periodic "frame=... time=..." stats, which it refreshes
// with '\r' (no trailing '\n') rather than emitting a full new line. '\r\n'
// is still treated as a single terminator.
func scanFFmpegLines(data []byte, atEOF bool) (advance int, token []byte, err error) {
	if atEOF && len(data) == 0 {
		return 0, nil, nil
	}
	if i := bytes.IndexAny(data, "\r\n"); i >= 0 {
		if data[i] == '\r' && i+1 < len(data) && data[i+1] == '\n' {
			return i + 2, data[:i], nil
		}
		return i + 1, data[:i], nil
	}
	if atEOF {
		return len(data), data, nil
	}
	return 0, nil, nil
}

// monitorProgress monitors ffmpeg progress output and parses time= for progress tracking.
// ffmpeg refreshes its periodic "frame=... time=..." stats line using a bare
// '\r' rather than a trailing '\n'; the default bufio.ScanLines split only
// breaks on '\n', which would otherwise coalesce every refresh for the whole
// encode into one giant token that never surfaces a progress update and can
// exceed bufio.MaxScanTokenSize. scanFFmpegLines treats '\r', '\n', and
// '\r\n' as line terminators so each refresh becomes its own token.
func (m *Module) monitorProgress(jobID string, stderr io.Reader, run *qualityRunParams) {
	scanner := bufio.NewScanner(stderr)
	scanner.Split(scanFFmpegLines)
	for scanner.Scan() {
		line := scanner.Text()
		if _, after, ok := strings.Cut(line, "time="); ok {
			m.handleProgressUpdate(jobID, after, run)
		}
	}
	if err := scanner.Err(); err != nil {
		m.log.Warn("Progress monitoring error for job %s: %v", jobID, err)
	}
	// Drain any remainder to EOF even if scanning above stopped early (e.g. a
	// scanner error), so ffmpeg can never block writing to a full stderr pipe
	// with nothing left reading from it.
	_, _ = io.Copy(io.Discard, stderr)
}

// handleProgressUpdate processes a single ffmpeg progress line and updates job progress.
func (m *Module) handleProgressUpdate(jobID, rawTimeStr string, run *qualityRunParams) {
	timeStr := rawTimeStr
	if spaceIdx := strings.IndexAny(timeStr, " \t"); spaceIdx > 0 {
		timeStr = timeStr[:spaceIdx]
	}
	currentSecs := parseFFmpegTime(timeStr)
	baseProgress := float64(run.CurrentQuality-1) / float64(run.TotalQualities) * 100
	qualityProgress := 100.0 / float64(run.TotalQualities)
	variantPct := calculateVariantProgress(currentSecs, run.TotalDuration)
	m.updateJobStatus(&updateJobStatusParams{JobID: jobID, Status: models.HLSStatusRunning, Progress: baseProgress + qualityProgress*variantPct})
}

// unknownDurationAssumedSecs is the heuristic assumed total duration used to
// estimate progress when the real media duration is unknown (e.g. ffprobe
// failed). Progress rises smoothly from 0% toward maxPct as currentSecs
// approaches this assumed ceiling, rather than jumping to a mid-point on the
// very first sample.
const unknownDurationAssumedSecs = 7200.0 // assume up to 2 hours

func calculateVariantProgress(currentSecs, totalDuration float64) float64 {
	if currentSecs <= 0 {
		return 0
	}
	var pct, maxPct float64
	if totalDuration > 0 {
		pct = currentSecs / totalDuration
		maxPct = 0.99
	} else {
		pct = currentSecs / unknownDurationAssumedSecs
		maxPct = 0.95
	}
	return math.Min(pct, maxPct)
}

func parseFFmpegTime(timeStr string) float64 {
	parts := strings.Split(timeStr, ":")
	if len(parts) == 3 {
		h, errH := strconv.ParseFloat(parts[0], 64)
		m, errM := strconv.ParseFloat(parts[1], 64)
		s, errS := strconv.ParseFloat(parts[2], 64)
		// On a malformed ffmpeg time string, return 0 rather than a partial sum from
		// silently-zeroed components, which would make progress jump or run backward.
		if errH != nil || errM != nil || errS != nil {
			return 0
		}
		return h*3600 + m*60 + s
	}
	s, err := strconv.ParseFloat(timeStr, 64)
	if err != nil {
		return 0
	}
	return s
}
