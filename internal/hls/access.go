package hls

import (
	"sync"
	"time"
)

// AccessTracker tracks last access time for HLS jobs
type AccessTracker struct {
	lastAccess map[string]time.Time
	mu         sync.RWMutex
}

// RecordAccess records an access to an HLS job and persists the timestamp
// so that access times survive restarts.
// TODO: Race condition - job.LastAccessedAt is modified without holding jobsMu
// write lock. The RLock only prevents the map from being modified but does not
// prevent concurrent writes to the job struct itself. Two concurrent RecordAccess
// calls could race on job.LastAccessedAt. Also, saveJob is called on every access
// which could be a performance issue under high traffic; consider debouncing the
// DB write (e.g. only persist if last save was >30s ago).
func (m *Module) RecordAccess(jobID string) {
	now := time.Now()
	m.accessTracker.mu.Lock()
	m.accessTracker.lastAccess[jobID] = now
	m.accessTracker.mu.Unlock()

	m.jobsMu.RLock()
	job, exists := m.jobs[jobID]
	m.jobsMu.RUnlock()
	if exists {
		job.LastAccessedAt = &now
		m.saveJob(job)
	}
}

// GetLastAccess returns the last access time for a job
func (m *Module) GetLastAccess(jobID string) (time.Time, bool) {
	m.accessTracker.mu.RLock()
	defer m.accessTracker.mu.RUnlock()
	t, ok := m.accessTracker.lastAccess[jobID]
	return t, ok
}
