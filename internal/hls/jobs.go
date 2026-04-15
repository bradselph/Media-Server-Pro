package hls

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"time"

	"media-server-pro/pkg/models"
)

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
	OutputDir          string
	RequestedQualities []string
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
	case models.HLSStatusFailed:
		if existing.FailCount >= m.maxFailures() {
			return existing, true, fmt.Errorf("HLS generation for %s has failed %d times and will not be retried automatically", p.MediaPath, existing.FailCount)
		}
	}
	return nil, false, nil
}

// tryReuseExistingHLSOnDiskLocked reuses valid HLS on disk if present; caller holds m.jobsMu. Returns (job, true) when reused.
func (m *Module) tryReuseExistingHLSOnDiskLocked(p *createOrReuseHLSJobParams) (*models.HLSJob, bool) {
	if !m.validateExistingHLS(&validateExistingHLSParams{OutputDir: p.OutputDir, RequestedQualities: p.Qualities}) {
		return nil, false
	}
	m.log.Info("Found existing valid HLS content for %s, reusing files", p.JobID)
	now := time.Now()
	job := &models.HLSJob{
		ID:          p.JobID,
		MediaPath:   p.MediaPath,
		OutputDir:   p.OutputDir,
		Status:      models.HLSStatusCompleted,
		Progress:    100,
		Qualities:   p.Qualities,
		StartedAt:   now.Add(-1 * time.Hour),
		CompletedAt: &now,
	}
	m.jobs[p.JobID] = job
	if err := m.saveJobs(); err != nil {
		m.log.Warn("Failed to save job state after discovering existing HLS: %v", err)
	}
	return job, true
}

// enqueueNewHLSJobLocked cleans output dir, creates job, and starts transcode goroutine; caller holds m.jobsMu.
func (m *Module) enqueueNewHLSJobLocked(p *createOrReuseHLSJobParams) (*models.HLSJob, error) {
	if _, err := os.Stat(p.OutputDir); err == nil {
		m.log.Warn("Output directory exists but HLS validation failed, cleaning up before regeneration: %s", p.OutputDir)
		if err := os.RemoveAll(p.OutputDir); err != nil {
			m.log.Error("Failed to clean up corrupted HLS directory: %v", err)
		}
	}
	if err := os.MkdirAll(p.OutputDir, 0o755); err != nil { //nolint:gosec // G301: HLS output dirs need world-read for serving
		return nil, fmt.Errorf("failed to create output directory: %w", err)
	}
	job := &models.HLSJob{
		ID:        p.JobID,
		MediaPath: p.MediaPath,
		OutputDir: p.OutputDir,
		Status:    models.HLSStatusPending,
		Progress:  0,
		Qualities: p.Qualities,
		StartedAt: time.Now(),
	}
	jobCtx, jobCancel := context.WithCancel(context.Background()) //nolint:gosec // cancel stored in m.jobCancels for external cancellation
	doneCh := make(chan struct{})
	m.jobs[p.JobID] = job
	m.jobCancels[p.JobID] = jobCancel
	m.jobDone[p.JobID] = doneCh
	m.activeJobs.Add(1)
	go func() {
		defer close(doneCh)
		defer m.activeJobs.Done()
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
	defer m.jobsMu.Unlock()

	job, ok := m.jobs[params.JobID]
	if !ok {
		return
	}

	if params.Status == models.HLSStatusFailed {
		job.FailCount++
	}
	job.Status = params.Status
	if params.ErrorMsg != "" {
		job.Error = params.ErrorMsg
	}
	if params.Progress > 0 {
		job.Progress = params.Progress
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

// HasHLS checks if completed HLS content exists for a media file (with disk verification)
func (m *Module) HasHLS(mediaPath string) bool {
	job, err := m.GetJobByMediaPath(mediaPath)
	if err != nil {
		return false
	}
	if job.Status != models.HLSStatusCompleted {
		return false
	}
	masterPath := filepath.Join(job.OutputDir, masterPlaylistName)
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
	defer m.jobsMu.Unlock()

	job, ok := m.jobs[jobID]
	if !ok {
		return fmt.Errorf(errJobNotFoundFmt, jobID)
	}

	if job.Status == models.HLSStatusRunning || job.Status == models.HLSStatusPending {
		job.Status = models.HLSStatusCanceled
		if cancel, ok := m.jobCancels[jobID]; ok {
			cancel()
			delete(m.jobCancels, jobID)
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
	var doneCh chan struct{}
	if cancel, ok := m.jobCancels[jobID]; ok {
		cancel()
		delete(m.jobCancels, jobID)
	}
	if ch, ok := m.jobDone[jobID]; ok {
		doneCh = ch
		delete(m.jobDone, jobID)
	}
	outputDir := job.OutputDir
	m.jobsMu.Unlock()

	// Wait for the transcode goroutine to exit so it is no longer writing segment
	// files before we remove the output directory.
	if doneCh != nil {
		<-doneCh
	}

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

// saveJob persists a single job to the database.
func (m *Module) saveJob(job *models.HLSJob) {
	if err := m.repo.Save(context.Background(), job); err != nil {
		m.log.Error("Failed to persist HLS job %s: %v", job.ID, err)
	}
}

// SaveJobs persists all in-memory HLS jobs to the database. Exposed for the
// hls-pregenerate background task and admin tooling.
func (m *Module) SaveJobs() error {
	return m.saveJobs()
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
		StartedAt:   completedTime.Add(-1 * time.Hour),
		CompletedAt: &completedTime,
	}
	m.jobs[jobID] = job
	m.log.Debug("Discovered existing HLS job: %s (qualities: %v)", jobID, qualities)
	return true
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

// validateExistingHLS checks if valid HLS content exists on disk for the given output directory and qualities.
func (m *Module) validateExistingHLS(p *validateExistingHLSParams) bool {
	masterPath := filepath.Join(p.OutputDir, masterPlaylistName)
	masterData, err := os.ReadFile(masterPath)
	if err != nil {
		return false
	}

	existingVariants := m.parseVariantStreams(string(masterData))
	if len(existingVariants) == 0 {
		return false
	}

	existingQualities := make(map[string]bool)
	for _, variantPath := range existingVariants {
		qualityName := filepath.Dir(variantPath)
		existingQualities[qualityName] = true
	}

	for _, quality := range p.RequestedQualities {
		if !existingQualities[quality] {
			m.log.Debug("Requested quality %s not found in existing HLS content", quality)
			return false
		}
		if !m.validateQualityOnDisk(&qualityCheckParams{OutputDir: p.OutputDir, Quality: quality}) {
			return false
		}
	}

	return true
}
