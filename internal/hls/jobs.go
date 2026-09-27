package hls

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"media-server-pro/pkg/models"
)

// estimatedHLSJobDuration is used to estimate the start time for HLS jobs
// that already existed on disk but don't have a recorded start time.
const estimatedHLSJobDuration = 1 * time.Hour

// copyHLSJob returns a deep copy of the job so callers cannot mutate shared state.
func copyHLSJob(j *models.HLSJob) *models.HLSJob {
	if j == nil {
		return nil
	}
	c := *j
	c.Qualities = append([]string(nil), j.Qualities...)
	if j.CompletedAt != nil {
		c.CompletedAt = new(*j.CompletedAt)
	}
	if j.LastAccessedAt != nil {
		c.LastAccessedAt = new(*j.LastAccessedAt)
	}
	return &c
}

// createOrReuseHLSJobParams holds arguments for creating or reusing an HLS job.
type createOrReuseHLSJobParams struct {
	Ctx       context.Context
	JobID     string
	MediaPath string
	OutputDir string
	Qualities []string
	// HighPriority marks this as a live, user-triggered request — see
	// GenerateHLSParams.HighPriority.
	HighPriority bool
}

// updateJobStatusParams holds arguments for updating an HLS job's status.
type updateJobStatusParams struct {
	JobID    string
	Status   models.HLSStatus
	ErrorMsg string
	Progress float64
}

// discoverQualitiesParams holds arguments for reading discovered qualities from disk.
type discoverQualitiesParams struct {
	OutputDir string
	JobID     string
}

// qualityCheckParams holds arguments for validating a single quality on disk.
type qualityCheckParams struct {
	OutputDir string
	Quality   string
}

// validateExistingHLSParams holds arguments for validating existing HLS content on disk.
type validateExistingHLSParams struct {
	OutputDir string
}

// createOrReuseHLSJobLocked creates or reuses an HLS job; caller must hold m.jobsMu.
func (m *Module) createOrReuseHLSJobLocked(p *createOrReuseHLSJobParams) (*models.HLSJob, error) {
	if existing, done, err := m.existingJobOrRetryErrorLocked(p); err != nil {
		return existing, err
	} else if done {
		return existing, nil
	}
	if job, ok := m.tryReuseExistingHLSOnDiskLocked(p); ok {
		return job, nil
	}
	return m.enqueueNewHLSJobLocked(p)
}

// existingJobOrRetryErrorLocked returns (existing job, true, nil) to return as-is, (nil, true, err) to fail, or (nil, false, nil) to continue; caller holds m.jobsMu.
func (m *Module) existingJobOrRetryErrorLocked(p *createOrReuseHLSJobParams) (*models.HLSJob, bool, error) {
	existing, ok := m.jobs[p.JobID]
	if !ok {
		return nil, false, nil
	}
	switch existing.Status {
	case models.HLSStatusCompleted, models.HLSStatusRunning:
		return existing, true, nil
	case models.HLSStatusPending:
		// Job is already queued with an active goroutine. Return it directly to
		// avoid spawning a second goroutine that would overwrite jobCancels/jobDone
		// and race against the original transcoding to the same output directory.
		// If this caller is high priority (e.g. a viewer's GenerateHLS request hit
		// a job a background pregen cycle already queued at low priority), promote
		// it so its spin loop starts contending for a slot as high priority.
		if p.HighPriority {
			m.upgradeJobPriority(existing.ID)
		}
		return existing, true, nil
	case models.HLSStatusFailed:
		if existing.FailCount >= m.maxFailures() {
			return existing, true, fmt.Errorf("HLS generation for %s has failed %d times and will not be retried automatically", p.MediaPath, existing.FailCount)
		}
	}
	return nil, false, nil
}

// tryReuseExistingHLSOnDiskLocked reuses valid HLS on disk if present; caller
// holds m.jobsMu. Returns (job, true) when reused. The reused job's Qualities
// is the actual validated subset on disk (not p.Qualities, the originally
// requested ladder) — a job interrupted mid-ladder (server crash between two
// qualities) leaves a master.m3u8 listing fewer qualities than requested, and
// that subset is exactly what's reusable; see validateExistingHLS.
func (m *Module) tryReuseExistingHLSOnDiskLocked(p *createOrReuseHLSJobParams) (*models.HLSJob, bool) {
	valid := m.validateExistingHLS(&validateExistingHLSParams{OutputDir: p.OutputDir})
	if len(valid) == 0 {
		return nil, false
	}
	m.log.Info("Found existing valid HLS content for %s, reusing files (%d of %d requested qualities present)", p.JobID, len(valid), len(p.Qualities))
	now := time.Now()
	job := &models.HLSJob{
		ID:          p.JobID,
		MediaPath:   p.MediaPath,
		OutputDir:   p.OutputDir,
		Status:      models.HLSStatusCompleted,
		Progress:    100,
		Qualities:   valid,
		Available:   true,
		HLSUrl:      hlsURLForJob(p.JobID),
		StartedAt:   now.Add(-estimatedHLSJobDuration),
		CompletedAt: &now,
	}
	m.jobs[p.JobID] = job
	// Use saveJob (single-row, no mutex) instead of saveJobs (acquires jobsMu.RLock).
	// saveJobs would deadlock here because the caller already holds jobsMu as a write lock.
	_ = m.saveJob(job)
	return job, true
}

// enqueueNewHLSJobLocked cleans output dir, creates job, and starts transcode goroutine; caller holds m.jobsMu.
func (m *Module) enqueueNewHLSJobLocked(p *createOrReuseHLSJobParams) (*models.HLSJob, error) {
	if _, err := os.Stat(p.OutputDir); err == nil {
		m.log.Warn("Output directory exists but HLS validation failed, cleaning up before regeneration: %s", p.OutputDir)
		if err := os.RemoveAll(p.OutputDir); err != nil {
			// Stale segments mixed with a fresh transcode produce a broken playlist
			// (orphan .ts files, mismatched byte ranges). Fail loud instead of
			// silently regenerating into a corrupted directory.
			return nil, fmt.Errorf("failed to clean up corrupted HLS directory %s: %w", p.OutputDir, err)
		}
	}
	if err := os.MkdirAll(p.OutputDir, 0o755); err != nil { //nolint:gosec // G301: HLS output dirs need world-read for serving
		return nil, fmt.Errorf("failed to create output directory: %w", err)
	}
	// Carry the prior job's consecutive-failure count forward: pregen retry
	// cycles re-create the job under the same ID, and resetting FailCount to 0
	// here meant it never climbed past 1, so the maxFailures circuit breaker in
	// existingJobOrRetryErrorLocked could never trip on an untranscodable file.
	prevFailCount := 0
	if prev, ok := m.jobs[p.JobID]; ok {
		prevFailCount = prev.FailCount
	}
	job := &models.HLSJob{
		ID:        p.JobID,
		MediaPath: p.MediaPath,
		OutputDir: p.OutputDir,
		Status:    models.HLSStatusPending,
		Progress:  0,
		Qualities: p.Qualities,
		StartedAt: time.Now(),
		FailCount: prevFailCount,
	}
	jobCtx, jobCancel := context.WithCancel(context.Background()) //nolint:gosec // cancel stored in m.jobCancels for external cancellation
	doneCh := make(chan struct{})
	// Register the job's transcode priority before its goroutine can possibly
	// call acquireTranscodeSem, so isJobHighPriority never misses on a race.
	m.setJobPriority(p.JobID, p.HighPriority)
	m.jobs[p.JobID] = job
	m.jobCancels[p.JobID] = jobCancel
	m.jobDone[p.JobID] = doneCh
	m.activeJobs.Add(1)
	go func() {
		defer close(doneCh)
		defer m.activeJobs.Done()
		// Release the per-job context regardless of exit path (success, failure,
		// or panic). finalizeJobCompleted handles the success path explicitly,
		// but failure/panic paths inside transcode() never reach that code and
		// would otherwise leak the cancel func until module Stop. cancel() is
		// idempotent so the success-path call is harmless.
		defer func() {
			m.jobsMu.Lock()
			if cancel, ok := m.jobCancels[p.JobID]; ok {
				cancel()
				delete(m.jobCancels, p.JobID)
			}
			m.jobsMu.Unlock()
		}()
		defer func() {
			if r := recover(); r != nil {
				m.log.Error("Panic in HLS transcode for job %s: %v\n%s", p.JobID, r, debug.Stack())
				m.updateJobStatus(&updateJobStatusParams{JobID: p.JobID, Status: models.HLSStatusFailed, ErrorMsg: fmt.Sprintf("Internal error: %v", r), Progress: 0})
			}
		}()
		m.transcode(jobCtx, job)
	}()
	m.log.Info("Started HLS generation for %s (job: %s)", p.MediaPath, p.JobID)
	return job, nil
}

// updateJobStatus updates a job's status.
// Transitioning to HLSStatusFailed automatically increments the job's FailCount.
func (m *Module) updateJobStatus(params *updateJobStatusParams) {
	m.jobsMu.Lock()

	job, ok := m.jobs[params.JobID]
	if !ok {
		m.jobsMu.Unlock()
		return
	}

	if params.Status == models.HLSStatusFailed {
		job.FailCount++
	}
	job.Status = params.Status
	if params.ErrorMsg != "" {
		job.Error = params.ErrorMsg
	}
	job.Progress = params.Progress

	// Persist terminal failures so the incremented FailCount survives a restart.
	// Otherwise a crash right after a failure reloads the old FailCount and the
	// job is retried indefinitely instead of stopping at maxFailures. Snapshot the
	// job under the lock and write it AFTER releasing jobsMu so a slow DB round trip
	// does not stall every other jobsMu holder (progress updates, ListJobs, handler
	// status reads, RecordAccess).
	var toPersist *models.HLSJob
	if params.Status == models.HLSStatusFailed {
		toPersist = copyHLSJob(job)
	}
	m.jobsMu.Unlock()

	if toPersist != nil {
		_ = m.saveJob(toPersist)
	}
}

// GetJobStatus returns a copy of the job status to avoid data races with the transcode goroutine.
func (m *Module) GetJobStatus(jobID string) (*models.HLSJob, error) {
	m.jobsMu.RLock()
	defer m.jobsMu.RUnlock()

	job, ok := m.jobs[jobID]
	if !ok {
		return nil, fmt.Errorf(errJobNotFoundFmt, jobID)
	}
	return copyHLSJob(job), nil
}

// GetJobByMediaPath returns a copy of the job for a media file by its path.
func (m *Module) GetJobByMediaPath(mediaPath string) (*models.HLSJob, error) {
	m.jobsMu.RLock()
	defer m.jobsMu.RUnlock()
	for _, job := range m.jobs {
		if job.MediaPath == mediaPath {
			return copyHLSJob(job), nil
		}
	}
	return nil, fmt.Errorf("HLS job not found for path: %s", mediaPath)
}

// HasHLS checks if playable HLS content exists for a media file (with disk
// verification). Available (not Status=="completed") is the gate: a job
// becomes playable as soon as its first quality finishes — see
// markJobPlayable — so this reports true for a job that is still Running the
// rest of its ladder, not only a fully Completed one. Callers that previously
// relied on this meaning "fully done" (e.g. the pre-generation sweep, via
// HasHLSByID below) now treat "already playable" as "don't re-queue", which is
// the same outcome in practice since createOrReuseHLSJobLocked already
// no-ops a duplicate GenerateHLS call for a Running job.
func (m *Module) HasHLS(mediaPath string) bool {
	job, err := m.GetJobByMediaPath(mediaPath)
	if err != nil {
		return false
	}
	if !job.Available {
		return false
	}
	masterPath := filepath.Join(job.OutputDir, masterPlaylistName)
	_, statErr := os.Stat(masterPath)
	return statErr == nil
}

// HasHLSByID checks playable HLS content (see HasHLS's Available note above)
// for a media item by its stable ID, which is also the HLS job ID (see
// GenerateHLS: jobID := params.MediaID). This is an O(1) map lookup, unlike
// HasHLS(path), which linearly scans every job to match MediaPath. Callers
// with the media ID in hand (e.g. the HLS pre-generation sweep over the whole
// catalog) should prefer this to avoid an O(items x jobs) scan every cycle.
func (m *Module) HasHLSByID(mediaID string) bool {
	m.jobsMu.RLock()
	job, ok := m.jobs[mediaID]
	if !ok || !job.Available {
		m.jobsMu.RUnlock()
		return false
	}
	outputDir := job.OutputDir
	m.jobsMu.RUnlock()

	// Verify on disk outside the lock (don't hold jobsMu across a stat syscall).
	masterPath := filepath.Join(outputDir, masterPlaylistName)
	_, statErr := os.Stat(masterPath)
	return statErr == nil
}

// ListJobs returns copies of all HLS jobs to avoid data races with transcode goroutines.
func (m *Module) ListJobs() []*models.HLSJob {
	m.jobsMu.RLock()
	defer m.jobsMu.RUnlock()

	jobs := make([]*models.HLSJob, 0, len(m.jobs))
	for _, job := range m.jobs {
		jobs = append(jobs, copyHLSJob(job))
	}
	return jobs
}

// CancelJob cancels a running job and kills the ffmpeg process.
func (m *Module) CancelJob(jobID string) error {
	m.jobsMu.Lock()

	job, ok := m.jobs[jobID]
	if !ok {
		m.jobsMu.Unlock()
		return fmt.Errorf(errJobNotFoundFmt, jobID)
	}

	var toPersist *models.HLSJob
	if job.Status == models.HLSStatusRunning || job.Status == models.HLSStatusPending {
		job.Status = models.HLSStatusCanceled
		if cancel, ok := m.jobCancels[jobID]; ok {
			cancel()
			delete(m.jobCancels, jobID)
		}
		toPersist = copyHLSJob(job)
	}
	m.jobsMu.Unlock()

	// If the Canceled status fails to persist, loadJobs() reloads the job as
	// Running on restart and resetRunningToPending re-queues it — silently
	// re-transcoding a job the user canceled. Surface the failure to the operator.
	// Persist after releasing jobsMu so the DB write doesn't serialize other holders.
	if toPersist != nil {
		if err := m.saveJob(toPersist); err != nil {
			m.log.Warn("CancelJob: failed to persist Canceled status for job %s; it may re-transcode on restart: %v", jobID, err)
		}
	}

	return nil
}

// DeleteJob cancels a running job, removes its files, and deletes the DB record.
// The in-memory entry is only removed after the DB delete succeeds so that a
// server restart re-loads the job rather than leaving a DB orphan.
func (m *Module) DeleteJob(jobID string) error {
	m.jobsMu.Lock()
	job, ok := m.jobs[jobID]
	if !ok {
		m.jobsMu.Unlock()
		return fmt.Errorf(errJobNotFoundFmt, jobID)
	}
	// Cancel running transcode so ffmpeg stops before we remove OutputDir.
	// Grab the done channel while holding the lock, then release before waiting.
	// Do NOT delete m.jobDone[jobID] yet — a concurrent DeleteJob call arriving
	// between this Unlock and the <-doneCh wait would observe jobDone absent,
	// skip the wait, and race os.RemoveAll with the still-running goroutine.
	var doneCh chan struct{}
	if cancel, ok := m.jobCancels[jobID]; ok {
		cancel()
		delete(m.jobCancels, jobID)
	}
	if ch, ok := m.jobDone[jobID]; ok {
		doneCh = ch
		// deletion happens after wait — see below
	}
	outputDir := job.OutputDir
	m.jobsMu.Unlock()

	// Wait for the transcode goroutine to exit so it is no longer writing segment
	// files before we remove the output directory.
	if doneCh != nil {
		<-doneCh
		// Now safe to remove from the map — the goroutine has exited.
		m.jobsMu.Lock()
		delete(m.jobDone, jobID)
		m.jobsMu.Unlock()
	}

	// Drain any lazy transcodes running in HTTP handler goroutines for this job.
	// These hold m.activeJobs but have no jobDone entry, so the <-doneCh wait above
	// does not cover them; without this an on-demand transcode could still be
	// writing segments when os.RemoveAll runs below. Cancel first (killing ffmpeg)
	// so the wait returns promptly instead of blocking on a full on-demand encode
	// that runs under the HTTP request context this call cannot otherwise stop.
	m.cancelLazyTranscodes(jobID)
	if rawWg, ok := m.lazyWg.Load(jobID); ok {
		rawWg.(*sync.WaitGroup).Wait()
		m.lazyWg.Delete(jobID)
	}
	m.lazyCancels.Delete(jobID)

	// Filesystem cleanup (best-effort; warn only).
	if err := os.RemoveAll(outputDir); err != nil {
		m.log.Warn("Failed to remove HLS directory: %v", err)
	}

	// DB delete must succeed before we remove the in-memory entry.
	// On failure the in-memory map still has the record, which is consistent
	// with what loadJobs() would restore on restart.
	if m.repo != nil {
		if err := m.repo.Delete(context.Background(), jobID); err != nil {
			m.log.Warn("Failed to delete HLS job %s from DB: %v", jobID, err)
			return fmt.Errorf("failed to delete HLS job from database: %w", err)
		}
	}

	m.jobsMu.Lock()
	delete(m.jobs, jobID)
	m.jobsMu.Unlock()

	// Drop access-tracker entries for the deleted job so neither debounce map leaks
	// for the life of the process (respects the accessTracker.mu < jobsMu ordering:
	// jobsMu is already released here, so the two locks are never held together).
	m.accessTracker.mu.Lock()
	delete(m.accessTracker.lastAccess, jobID)
	delete(m.accessTracker.lastSaved, jobID)
	m.accessTracker.mu.Unlock()

	m.cleanQualityLocks(jobID)
	m.log.Info("Deleted HLS job %s", jobID)
	return nil
}

func (m *Module) loadJobs() error {
	jobs, err := m.repo.List(context.Background())
	if err != nil {
		return err
	}

	m.jobsMu.Lock()
	defer m.jobsMu.Unlock()

	for _, job := range jobs {
		// Reset stale Running → Pending so that jobs beyond the resume concurrency
		// limit (resumeInterruptedJobs honors cfg.HLS.ConcurrentLimit) are left in
		// a retryable Pending state rather than permanently stuck as "Running". On
		// shutdown, saveJobs serializes the in-memory state: if a Running job was
		// never resumed its goroutine never started, so it would be reloaded as
		// Running on every restart without ever making progress.
		if job.Status == models.HLSStatusRunning {
			job.Status = models.HLSStatusPending
		}
		m.jobs[job.ID] = job
	}
	return nil
}

func (m *Module) saveJobs() error {
	m.jobsMu.RLock()
	jobs := make([]*models.HLSJob, 0, len(m.jobs))
	for _, j := range m.jobs {
		jobs = append(jobs, copyHLSJob(j))
	}
	m.jobsMu.RUnlock()

	ctx := context.Background()
	var lastErr error
	for _, job := range jobs {
		if err := m.repo.Save(ctx, job); err != nil {
			m.log.Warn("Failed to save HLS job %s: %v", job.ID, err)
			lastErr = err
		}
	}
	return lastErr
}

// saveJob persists a single job to the database. It logs and returns any error
// so callers that must react to a failed persist (e.g. CancelJob, whose Canceled
// status would otherwise be lost on restart and re-transcode) can do so; the
// best-effort call sites discard the result with `_ =`.
func (m *Module) saveJob(job *models.HLSJob) error {
	if err := m.repo.Save(context.Background(), job); err != nil {
		m.log.Error("Failed to persist HLS job %s: %v", job.ID, err)
		return err
	}
	return nil
}

func skipDirEntry(entry os.DirEntry) bool {
	if !entry.IsDir() {
		return true
	}
	name := entry.Name()
	return name == "." || name == ".."
}

// getDiscoveredQualitiesLocked reads the master playlist in outputDir and returns quality names if all variant files exist; caller holds m.jobsMu.
func (m *Module) getDiscoveredQualitiesLocked(p *discoverQualitiesParams) ([]string, bool) {
	masterPath := filepath.Join(p.OutputDir, masterPlaylistName)
	if _, err := os.Stat(masterPath); err != nil {
		m.log.Debug("Skipping job %s: no master playlist found", p.JobID)
		return nil, false
	}
	masterData, err := os.ReadFile(masterPath)
	if err != nil {
		m.log.Debug("Skipping job %s: failed to read master playlist: %v", p.JobID, err)
		return nil, false
	}
	variants := m.parseVariantStreams(string(masterData))
	if len(variants) == 0 {
		m.log.Debug("Skipping job %s: no variants in master playlist", p.JobID)
		return nil, false
	}
	qualities := make([]string, 0, len(variants))
	for _, variantPath := range variants {
		qualityName := filepath.Dir(variantPath)
		qualities = append(qualities, qualityName)
		fullVariantPath := filepath.Join(p.OutputDir, variantPath)
		if _, err := os.Stat(fullVariantPath); err != nil {
			m.log.Debug("Variant %s missing for job %s", variantPath, p.JobID)
			return nil, false
		}
	}
	return qualities, true
}

// tryDiscoverJobFromEntryLocked attempts to discover one HLS job from a cache dir entry; caller holds m.jobsMu. Returns true if a job was registered.
func (m *Module) tryDiscoverJobFromEntryLocked(entry os.DirEntry) bool {
	if skipDirEntry(entry) {
		return false
	}
	jobID := entry.Name()
	if existing, ok := m.jobs[jobID]; ok && existing.Status == models.HLSStatusCompleted {
		return false
	}
	outputDir := filepath.Join(m.cacheDir, jobID)
	qualities, ok := m.getDiscoveredQualitiesLocked(&discoverQualitiesParams{OutputDir: outputDir, JobID: jobID})
	if !ok {
		return false
	}
	// A job that never got past its first quality before a crash/restart has
	// its master.m3u8 (if any) reflect that; a job that got further has a
	// master.m3u8 listing a subset (see transcode()'s progressive
	// publishMasterPlaylist) — either way, `qualities` above is already
	// exactly what's genuinely usable. Remove any other variant directory
	// that isn't in that set and never finished before the interruption.
	m.removeLeftoverVariantDirs(outputDir, qualities)
	info, err := entry.Info()
	if err != nil {
		m.log.Warn("Failed to stat HLS dir %s: %v", entry.Name(), err)
		return false
	}
	completedTime := info.ModTime()
	mediaPath := m.findMediaPathForJob(outputDir)
	job := &models.HLSJob{
		ID:          jobID,
		MediaPath:   mediaPath,
		OutputDir:   outputDir,
		Status:      models.HLSStatusCompleted,
		Progress:    100,
		Qualities:   qualities,
		Available:   true,
		HLSUrl:      hlsURLForJob(jobID),
		StartedAt:   completedTime.Add(-estimatedHLSJobDuration),
		CompletedAt: &completedTime,
	}
	m.jobs[jobID] = job
	m.log.Debug("Discovered existing HLS job: %s (qualities: %v)", jobID, qualities)
	return true
}

// removeLeftoverVariantDirs removes every immediate subdirectory of outputDir
// that is not one of completedQualities and has no playlist.m3u8 of its own.
// buildFFmpegTranscodeCmd uses hls_playlist_type=vod, so ffmpeg only writes a
// quality's playlist.m3u8 once that quality's whole encode finishes — a
// leftover directory with segments but no playlist.m3u8 can only be one whose
// encode was interrupted (server crash/restart) before it published (see
// transcode()'s publishMasterPlaylist). Such a directory is never referenced
// by master.m3u8 and would otherwise sit as orphaned disk usage indefinitely,
// or collide with a later lazy/on-demand re-transcode of the same quality
// (prepareVariantDir does not clean an existing directory before reusing it).
func (m *Module) removeLeftoverVariantDirs(outputDir string, completedQualities []string) {
	entries, err := os.ReadDir(outputDir)
	if err != nil {
		return
	}
	completed := make(map[string]bool, len(completedQualities))
	for _, q := range completedQualities {
		completed[q] = true
	}
	for _, entry := range entries {
		if !entry.IsDir() || completed[entry.Name()] {
			continue
		}
		variantDir := filepath.Join(outputDir, entry.Name())
		if _, statErr := os.Stat(filepath.Join(variantDir, "playlist.m3u8")); statErr == nil {
			// Has its own valid playlist but wasn't listed in master.m3u8 — leave
			// it alone rather than guess; this should not happen in practice since
			// publishMasterPlaylist lists a quality immediately once its playlist
			// exists.
			continue
		}
		m.log.Info("Removing leftover in-progress HLS variant dir (interrupted before completion): %s", variantDir)
		if removeErr := os.RemoveAll(variantDir); removeErr != nil {
			m.log.Warn("Failed to remove leftover HLS variant dir %s: %v", variantDir, removeErr)
		}
	}
}

// discoverExistingJobs scans the cache directory and creates job entries for existing HLS content.
func (m *Module) discoverExistingJobs() int {
	entries, err := os.ReadDir(m.cacheDir)
	if err != nil {
		m.log.Debug("Failed to read HLS cache directory during discovery: %v", err)
		return 0
	}
	discovered := 0
	m.jobsMu.Lock()
	defer m.jobsMu.Unlock()
	for _, entry := range entries {
		if m.tryDiscoverJobFromEntryLocked(entry) {
			discovered++
		}
	}
	return discovered
}

// findMediaPathForJob attempts to determine the original media path for a job.
// First checks the .lock file (present while a job is running). For completed
// jobs whose lock file has been removed, falls back to the DB record.
func (m *Module) findMediaPathForJob(outputDir string) string {
	lockPath := filepath.Join(outputDir, ".lock")
	data, err := os.ReadFile(lockPath)
	if err == nil {
		var lock LockFile
		if json.Unmarshal(data, &lock) == nil && lock.MediaPath != "" {
			return lock.MediaPath
		}
	}
	// Lock file absent (job completed and lock removed) — try the DB.
	if m.repo != nil {
		jobID := filepath.Base(outputDir)
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if job, dbErr := m.repo.Get(ctx, jobID); dbErr == nil && job != nil && job.MediaPath != "" {
			return job.MediaPath
		}
	}
	return ""
}

// validateQualityOnDisk checks that a single quality has a valid variant playlist and segment files on disk.
func (m *Module) validateQualityOnDisk(p *qualityCheckParams) bool {
	variantPlaylistPath := filepath.Join(p.OutputDir, p.Quality, "playlist.m3u8")
	variantData, err := os.ReadFile(variantPlaylistPath)
	if err != nil {
		m.log.Debug("Variant playlist missing for quality %s", p.Quality)
		return false
	}
	if !strings.Contains(string(variantData), ".ts") {
		m.log.Debug("Variant playlist for %s has no segments", p.Quality)
		return false
	}
	variantDir := filepath.Join(p.OutputDir, p.Quality)
	entries, err := os.ReadDir(variantDir)
	if err != nil {
		return false
	}
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".ts") {
			return true
		}
	}
	m.log.Debug("No segment files found for quality %s", p.Quality)
	return false
}

// validateExistingHLS returns the qualities listed in outputDir's master.m3u8
// that are genuinely valid and complete on disk (playlist + at least one
// segment — see validateQualityOnDisk), or nil if master.m3u8 is missing/
// empty or none of its listed variants validate.
//
// A job interrupted mid-ladder (server crash/restart between two qualities —
// see transcode()'s progressive publishMasterPlaylist) leaves a master.m3u8
// that lists fewer qualities than were originally requested; that is expected
// and valid, not a validation failure, so this does not require every
// originally-requested quality to be present. Callers get back whatever
// subset is actually usable, mirroring getDiscoveredQualitiesLocked (used by
// discovery at startup) so on-demand reuse (tryReuseExistingHLSOnDiskLocked)
// and restart discovery treat a partial ladder the same way.
func (m *Module) validateExistingHLS(p *validateExistingHLSParams) []string {
	masterPath := filepath.Join(p.OutputDir, masterPlaylistName)
	masterData, err := os.ReadFile(masterPath)
	if err != nil {
		return nil
	}

	existingVariants := m.parseVariantStreams(string(masterData))
	if len(existingVariants) == 0 {
		return nil
	}

	valid := make([]string, 0, len(existingVariants))
	for _, variantPath := range existingVariants {
		qualityName := filepath.Dir(variantPath)
		if m.validateQualityOnDisk(&qualityCheckParams{OutputDir: p.OutputDir, Quality: qualityName}) {
			valid = append(valid, qualityName)
		}
	}
	return valid
}
