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
