package hls

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
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

// transcode performs the actual transcoding
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
	for i, quality := range qualitiesToTranscode {
		runParams.CurrentQuality = i + 1
		if err := m.transcodeQuality(ctx, job, quality, runParams); err != nil {
			return
		}
	}

	// Use qualitiesToTranscode (not job.Qualities) so the master playlist only
	// advertises qualities that were actually transcoded. With lazy transcode
	// enabled, job.Qualities is the full list but only the first quality exists
	// on disk — listing all would cause 404s for any non-first quality variant.
	if err := m.generateMasterPlaylist(&generateMasterPlaylistParams{OutputDir: job.OutputDir, Variants: qualitiesToTranscode}); err != nil {
		m.updateJobStatus(&updateJobStatusParams{JobID: job.ID, Status: models.HLSStatusFailed, ErrorMsg: fmt.Sprintf("Failed to create master playlist: %v", err), Progress: 0})
		return
	}

	m.finalizeJobCompleted(job)
	m.log.Info("HLS generation completed for job %s", job.ID)
}

func (m *Module) acquireTranscodeSem(ctx context.Context, job *models.HLSJob) bool {
	// Spin-wait with context check — the dynamic semaphore doesn't support channel-based select.
	for {
		if m.tryAcquireTranscode() {
			return true
		}
		select {
		case <-ctx.Done():
			m.updateJobStatus(&updateJobStatusParams{JobID: job.ID, Status: models.HLSStatusCanceled, ErrorMsg: "Context canceled", Progress: 0})
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
	delete(m.jobCancels, job.ID)
	delete(m.jobDone, job.ID)
	m.jobsMu.Unlock()
	if err := m.saveJobs(); err != nil {
		m.log.Warn("Failed to save job state after completion: %v", err)
	}
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

	var stderrBuf bytes.Buffer
	stderrPipe, err := cmdWithContext.StderrPipe()
	if err != nil {
		m.updateJobStatus(&updateJobStatusParams{JobID: job.ID, Status: models.HLSStatusFailed, ErrorMsg: fmt.Sprintf("Failed to create stderr pipe: %v", err), Progress: 0})
		return err
	}
	if err := cmdWithContext.Start(); err != nil {
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
// Keyframes are placed at segment boundaries via force_key_frames (frame-rate independent).
func (m *Module) buildFFmpegTranscodeCmd(ctx context.Context, paths *transcodePaths, profile *config.HLSQuality) *exec.Cmd {
	cfg := m.config.Get()
	// Resolve S3 keys to presigned URLs so ffmpeg can fetch the source over HTTPS.
	mediaInput := m.resolveMediaInputPath(ctx, paths.MediaPath)
	stream := ffmpeg.Input(mediaInput)
	stream = stream.Output(paths.PlaylistPath,
		ffmpeg.KwArgs{
			"c:v":              "libx264",
			"preset":           "fast",
			"vf":               fmt.Sprintf("scale=%d:%d", profile.Width, profile.Height),
			"b:v":              fmt.Sprintf("%dk", profile.Bitrate/1000),
			"maxrate":          fmt.Sprintf("%dk", profile.Bitrate/1000),
			"bufsize":          fmt.Sprintf("%dk", profile.Bitrate*2/1000),
			"force_key_frames": fmt.Sprintf("expr:gte(t,n_forced*%d)", cfg.HLS.SegmentDuration),
			"sc_threshold":     "0",

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
		},
	).OverWriteOutput().SetFfmpegPath(m.ffmpegPath)

	compiled := stream.Compile()
	cmd := exec.CommandContext(ctx, compiled.Path, compiled.Args[1:]...) //nolint:gosec // G204: compiled.Path is the validated ffmpeg binary path
	cmd.Env = compiled.Env
	cmd.Dir = compiled.Dir
	return cmd
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

	// Acquire dynamic semaphore with context awareness.
	for !m.tryAcquireTranscode() {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(250 * time.Millisecond):
		}
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

	totalDuration := m.getMediaDuration(ctx, job.MediaPath)
	run := &qualityRunParams{JobID: job.ID, TotalQualities: 1, CurrentQuality: 1, TotalDuration: totalDuration}
	return m.transcodeQuality(ctx, job, quality, run)
}

// monitorProgress monitors ffmpeg progress output and parses time= for progress tracking.
func (m *Module) monitorProgress(jobID string, stderr io.Reader, run *qualityRunParams) {
	scanner := bufio.NewScanner(stderr)
	for scanner.Scan() {
		line := scanner.Text()
		if idx := strings.Index(line, "time="); idx >= 0 {
			m.handleProgressUpdate(jobID, line[idx+5:], run)
		}
	}
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

func calculateVariantProgress(currentSecs, totalDuration float64) float64 {
	if currentSecs <= 0 {
		return 0
	}
	var pct, maxPct float64
	if totalDuration > 0 {
		pct = currentSecs / totalDuration
		maxPct = 0.99
	} else {
		pct = 0.5 + (currentSecs/7200.0)*0.45
		maxPct = 0.95
	}
	return math.Min(pct, maxPct)
}

func parseFFmpegTime(timeStr string) float64 {
	parts := strings.Split(timeStr, ":")
	if len(parts) == 3 {
		h, _ := strconv.ParseFloat(parts[0], 64)
		m, _ := strconv.ParseFloat(parts[1], 64)
		s, _ := strconv.ParseFloat(parts[2], 64)
		return h*3600 + m*60 + s
	}
	s, _ := strconv.ParseFloat(timeStr, 64)
	return s
}
