package hls

import (
	"sync"
	"time"

	"media-server-pro/pkg/models"
)

// accessSaveInterval is the minimum time between DB writes for the same job's
// last-accessed timestamp. In-memory updates happen on every segment request
// but database persistence is debounced to avoid serializing all concurrent
// segment requests behind a DB round-trip.
const accessSaveInterval = 30 * time.Second

// AccessTracker tracks last access time for HLS jobs
type AccessTracker struct {
	lastAccess map[string]time.Time
	lastSaved  map[string]time.Time // last time we persisted each job to DB
	mu         sync.RWMutex
}

// RecordAccess records an access to an HLS job. The in-memory timestamp is
// always updated (used by the inactive-job cleaner) but the DB write is
// debounced to at most once per accessSaveInterval per job.
func (m *Module) RecordAccess(jobID string) {
	now := time.Now()

	// Always update the in-memory timestamp
	m.accessTracker.mu.Lock()
	m.accessTracker.lastAccess[jobID] = now
	lastSave := m.accessTracker.lastSaved[jobID]
	needsSave := now.Sub(lastSave) >= accessSaveInterval
	if needsSave {
		m.accessTracker.lastSaved[jobID] = now
	}
	m.accessTracker.mu.Unlock()

	if !needsSave {
		return
	}

	// Debounced: persist to DB at most every accessSaveInterval
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
