package hls

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"media-server-pro/pkg/models"
)

// LockFile represents an HLS generation lock
type LockFile struct {
	JobID     string    `json:"job_id"`
	MediaPath string    `json:"media_path"`
	StartedAt time.Time `json:"started_at"`
	PID       int       `json:"pid"`
}

// createLock creates a lock file for a job
func (m *Module) createLock(jobID, mediaPath string) error {
	lock := LockFile{
		JobID:     jobID,
		MediaPath: mediaPath,
		StartedAt: time.Now(),
		PID:       os.Getpid(),
	}

	data, err := json.Marshal(lock)
	if err != nil {
		return err
	}

	lockPath := filepath.Join(m.cacheDir, jobID, ".lock")
	return os.WriteFile(lockPath, data, 0o600)
}

// removeLock removes a lock file
func (m *Module) removeLock(jobID string) {
	lockPath := filepath.Join(m.cacheDir, jobID, ".lock")
	if err := os.Remove(lockPath); err != nil && !os.IsNotExist(err) {
		m.log.Warn("Failed to remove lock file %s: %v", lockPath, err)
	}
}

// checkLock checks if a lock file exists and if it's stale
func (m *Module) checkLock(jobID string) (exists, stale bool, lock *LockFile) {
	lockPath := filepath.Join(m.cacheDir, jobID, ".lock")
	data, err := os.ReadFile(lockPath)
	if err != nil {
		return false, false, nil
	}

	lock = &LockFile{}
	if json.Unmarshal(data, lock) != nil {
		return true, true, nil // Corrupted lock is stale
	}

	// Check if lock is stale. The threshold is configurable (default 2h) so
	// operators running long 4K encodes can raise it without touching code.
	staleThreshold := m.config.Get().HLS.StaleLockThreshold
	if staleThreshold <= 0 {
		staleThreshold = 2 * time.Hour
	}
	if time.Since(lock.StartedAt) > staleThreshold {
		return true, true, lock
	}

	return true, false, lock
}

// handleStaleLock processes a single stale lock: logs, removes the lock file, and updates job status.
func (m *Module) handleStaleLock(jobID string, lock *LockFile) {
	m.log.Warn("Found stale lock for job %s (started: %v)", jobID, lock.StartedAt)
	m.removeLock(jobID)
	m.jobsMu.Lock()
	if job, ok := m.jobs[jobID]; ok && job.Status == models.HLSStatusRunning {
		job.Status = models.HLSStatusFailed
		job.Error = "Job timed out (stale lock)"
	}
	m.jobsMu.Unlock()
}

// processEntryForStaleLocks checks one cache dir entry for a stale lock and cleans it if found. Returns true if a lock was removed.
func (m *Module) processEntryForStaleLocks(entry os.DirEntry) bool {
	if !entry.IsDir() {
		return false
	}
	jobID := entry.Name()
	exists, stale, lock := m.checkLock(jobID)
	shouldSkip := !exists || !stale || lock == nil
	if shouldSkip {
		return false
	}
	m.handleStaleLock(jobID, lock)
	return true
}

// CleanStaleLocks finds and removes stale lock files
func (m *Module) CleanStaleLocks() int {
	m.log.Debug("Checking for stale HLS locks...")
	removed := 0

	entries, err := os.ReadDir(m.cacheDir)
	if err != nil {
		m.log.Error(errReadCacheDirFmt, err)
		return 0
	}

	for _, entry := range entries {
		if m.processEntryForStaleLocks(entry) {
			removed++
		}
	}

	if removed > 0 {
		m.log.Info("Cleaned %d stale HLS locks", removed)
	}

	return removed
}

// removeStartupLockForEntry removes the lock file for one cache dir entry and resets job status. Returns true if a lock was removed.
func (m *Module) removeStartupLockForEntry(entry os.DirEntry) bool {
	if !entry.IsDir() {
		return false
	}
	jobID := entry.Name()
	lockPath := filepath.Join(m.cacheDir, jobID, ".lock")
	if _, err := os.Stat(lockPath); err != nil {
		return false
	}
	m.log.Info("Removing leftover lock for job %s", jobID)
	if err := os.Remove(lockPath); err != nil {
		m.log.Warn("Failed to remove lock %s: %v", lockPath, err)
		return false
	}
	m.jobsMu.Lock()
	if job, ok := m.jobs[jobID]; ok && job.Status == models.HLSStatusRunning {
		job.Status = models.HLSStatusPending
		job.Error = ""
	}
	m.jobsMu.Unlock()
	return true
}

// cleanLocksOnStartup removes ALL lock files unconditionally.
// At startup no transcoding is active, so every lock is leftover from a previous run.
func (m *Module) cleanLocksOnStartup() int {
	entries, err := os.ReadDir(m.cacheDir)
	if err != nil {
		return 0
	}
	removed := 0
	for _, entry := range entries {
		if m.removeStartupLockForEntry(entry) {
			removed++
		}
	}
	return removed
}
