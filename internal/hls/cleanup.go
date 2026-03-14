package hls

import (
	"context"
	"os"
	"path/filepath"
	"time"

	"media-server-pro/pkg/models"
)

// cleanupLoop periodically removes old HLS segments
func (m *Module) cleanupLoop() {
	for {
		select {
		case <-m.cleanupTicker.C:
			m.cleanupOldSegments()
		case <-m.cleanupDone:
			return
		}
	}
}

// cleanupOldSegments removes HLS segments older than retention period.
func (m *Module) cleanupOldSegments() {
	cfg := m.config.Get()
	retention := time.Duration(cfg.HLS.RetentionMinutes) * time.Minute
	if retention <= 0 {
		return
	}
	cutoff := time.Now().Add(-retention)

	m.log.Debug("Cleaning up HLS segments older than %v", retention)

	entries, err := os.ReadDir(m.cacheDir)
	if err != nil {
		m.log.Error(errReadCacheDirFmt, err)
		return
	}

	removed := 0
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		jobID := entry.Name()
		m.jobsMu.RLock()
		job, exists := m.jobs[jobID]
		m.jobsMu.RUnlock()
		if !m.shouldCleanupSegmentDir(entry, job, exists, cutoff) {
			continue
		}
		if m.removeSegmentDirAndState(jobID) {
			removed++
		}
	}

	if removed > 0 {
		m.log.Info("Cleaned up %d old HLS directories", removed)
	}
}

// isJobRunningOrPending returns true if the job exists and is running or pending.
func isJobRunningOrPending(job *models.HLSJob, exists bool) bool {
	if !exists || job == nil {
		return false
	}
	return job.Status == models.HLSStatusRunning || job.Status == models.HLSStatusPending
}

// shouldCleanupSegmentDir reports whether the segment dir is eligible for cleanup (old enough, not running/pending).
func (m *Module) shouldCleanupSegmentDir(entry os.DirEntry, job *models.HLSJob, exists bool, cutoff time.Time) bool {
	if isJobRunningOrPending(job, exists) {
		return false
	}
	lastActivity := m.getCleanupLastAccess(entry.Name(), job, entry)
	return !lastActivity.IsZero() && lastActivity.Before(cutoff)
}

// removeSegmentDirAndState deletes the segment directory, its in-memory state,
// and the persisted DB record. Returns true if removed successfully.
func (m *Module) removeSegmentDirAndState(jobID string) bool {
	path := filepath.Join(m.cacheDir, jobID)
	if err := os.RemoveAll(path); err != nil {
		m.log.Warn("Failed to remove HLS directory %s: %v", path, err)
		return false
	}
	m.jobsMu.Lock()
	delete(m.jobs, jobID)
	m.jobsMu.Unlock()
	m.accessTracker.mu.Lock()
	delete(m.accessTracker.lastAccess, jobID)
	m.accessTracker.mu.Unlock()
	if m.repo != nil {
		if err := m.repo.Delete(context.Background(), jobID); err != nil {
			m.log.Warn("Failed to delete HLS job %s from DB during cleanup: %v", jobID, err)
		}
	}
	return true
}

// getCleanupLastAccess returns the best available last-access time for a job.
func (m *Module) getCleanupLastAccess(jobID string, job *models.HLSJob, entry os.DirEntry) time.Time {
	if t, ok := m.GetLastAccess(jobID); ok {
		return t
	}
	if job != nil && job.LastAccessedAt != nil {
		return *job.LastAccessedAt
	}
	info, err := entry.Info()
	if err != nil {
		return time.Time{}
	}
	return info.ModTime()
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

	path := filepath.Join(m.cacheDir, jobID)
	if err := os.RemoveAll(path); err != nil {
		m.log.Warn("Failed to remove inactive HLS job %s: %v", jobID, err)
		return false
	}

	m.jobsMu.Lock()
	delete(m.jobs, jobID)
	m.jobsMu.Unlock()

	m.accessTracker.mu.Lock()
	delete(m.accessTracker.lastAccess, jobID)
	m.accessTracker.mu.Unlock()

	if m.repo != nil {
		if err := m.repo.Delete(context.Background(), jobID); err != nil {
			m.log.Warn("Failed to delete inactive HLS job %s from DB: %v", jobID, err)
		}
	}

	m.log.Debug("Removed inactive HLS job: %s (last access: %v)", jobID, lastAccess)
	return true
}
