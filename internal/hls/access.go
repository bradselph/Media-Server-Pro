package hls

import (
	"sync"
	"time"

	"media-server-pro/pkg/models"
)

// AccessTracker tracks last access time for HLS jobs
type AccessTracker struct {
	lastAccess map[string]time.Time
	mu         sync.RWMutex
}

// RecordAccess records an access to an HLS job and persists the timestamp
// so that access times survive restarts. The DB write happens outside jobsMu
// to avoid serializing concurrent segment requests on database round-trips.
func (m *Module) RecordAccess(jobID string) {
	now := time.Now()
	m.accessTracker.mu.Lock()
	m.accessTracker.lastAccess[jobID] = now
	m.accessTracker.mu.Unlock()

	var jobCopy *models.HLSJob
	m.jobsMu.Lock()
	job, exists := m.jobs[jobID]
	if exists {
		job.LastAccessedAt = &now
		jobCopy = copyHLSJob(job)
	}
	m.jobsMu.Unlock()

	if jobCopy != nil {
		m.saveJob(jobCopy)
	}
}

// GetLastAccess returns the last access time for a job
func (m *Module) GetLastAccess(jobID string) (time.Time, bool) {
	m.accessTracker.mu.RLock()
	defer m.accessTracker.mu.RUnlock()
	t, ok := m.accessTracker.lastAccess[jobID]
	return t, ok
}
