package hls

import (
	"context"
	"os"
	"path/filepath"
	"time"

	"media-server-pro/pkg/models"
)

// isJobRunningOrPending returns true if the job exists and is running or pending.
func isJobRunningOrPending(job *models.HLSJob, exists bool) bool {
	if !exists || job == nil {
		return false
	}
	return job.Status == models.HLSStatusRunning || job.Status == models.HLSStatusPending
}

// CleanInactiveJobs removes HLS content that hasn't been accessed recently
func (m *Module) CleanInactiveJobs(inactiveThreshold time.Duration) int {
	m.log.Debug("Cleaning inactive HLS jobs (threshold: %v)", inactiveThreshold)
	removed := 0
	cutoff := time.Now().Add(-inactiveThreshold)

	entries, err := os.ReadDir(m.cacheDir)
	if err != nil {
		m.log.Error(errReadCacheDirFmt, err)
		return 0
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if m.cleanInactiveJob(entry, cutoff) {
			removed++
		}
	}

	if removed > 0 {
		m.log.Info("Cleaned %d inactive HLS jobs", removed)
	}

	return removed
}

// getEffectiveLastAccess returns the last access time for a job, falling back to persisted LastAccessedAt and then directory modification time.
func (m *Module) getEffectiveLastAccess(jobID string, entry os.DirEntry) time.Time {
	if t, ok := m.GetLastAccess(jobID); ok {
		return t
	}
	m.jobsMu.RLock()
	job, exists := m.jobs[jobID]
	m.jobsMu.RUnlock()
	if exists && job.LastAccessedAt != nil {
		return *job.LastAccessedAt
	}
	info, err := entry.Info()
	if err != nil {
		return time.Time{}
	}
	return info.ModTime()
}

// cleanInactiveJob checks a single job directory and removes it if inactive. Returns true if the job was removed.
func (m *Module) cleanInactiveJob(entry os.DirEntry, cutoff time.Time) bool {
	jobID := entry.Name()
	lastAccess := m.getEffectiveLastAccess(jobID, entry)
	if lastAccess.IsZero() {
		return false
	}
	if !lastAccess.Before(cutoff) {
		return false
	}

	m.jobsMu.RLock()
	job, exists := m.jobs[jobID]
	m.jobsMu.RUnlock()

	if isJobRunningOrPending(job, exists) {
		return false
	}

	// M-14: Re-read lastAccess immediately before the write lock to narrow the stale-read
	// window. RecordAccess updates accessTracker.lastAccess under its own lock independently
	// of jobsMu, so a fresh access may have arrived since the initial read above.
	if t, ok := m.GetLastAccess(jobID); ok && !t.Before(cutoff) {
		return false
	}

	// Re-check under write lock to avoid TOCTOU: job could have started between RLock and removal.
	// Delete the map entry while still holding the lock so GetJobStatus cannot return
	// a job whose files have already been removed (the previous pattern released the lock,
	// removed files, then reacquired — leaving a window where the entry was in the map
	// but the directory was gone).
	m.jobsMu.Lock()
	job, exists = m.jobs[jobID]
	if isJobRunningOrPending(job, exists) {
		m.jobsMu.Unlock()
		return false
	}
	// M-15: RecordAccess debounce updates job.LastAccessedAt under jobsMu — re-check here
	// so a fresh debounced access that arrived between our read and this write lock is not missed.
	if exists && job != nil && job.LastAccessedAt != nil && !job.LastAccessedAt.Before(cutoff) {
		m.jobsMu.Unlock()
		return false
	}
	delete(m.jobs, jobID)
	m.jobsMu.Unlock()

	// Files and DB cleanup happen after the map entry is removed. If RemoveAll
	// fails we log and return false, but the in-memory entry is already gone —
	// on restart loadJobs will reload from DB and validateExistingHLS will handle
	// the missing files.
	path := filepath.Join(m.cacheDir, jobID)
	if err := os.RemoveAll(path); err != nil {
		m.log.Warn("Failed to remove inactive HLS job %s: %v", jobID, err)
		return false
	}

	m.accessTracker.mu.Lock()
	delete(m.accessTracker.lastAccess, jobID)
	m.accessTracker.mu.Unlock()

	if m.repo != nil {
		if err := m.repo.Delete(context.Background(), jobID); err != nil {
			m.log.Warn("Failed to delete inactive HLS job %s from DB: %v", jobID, err)
		}
	}

	m.cleanQualityLocks(jobID)
	m.log.Debug("Removed inactive HLS job: %s (last access: %v)", jobID, lastAccess)
	return true
}
