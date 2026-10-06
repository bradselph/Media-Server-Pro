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

// Lock ordering invariant for the HLS package:
//   accessTracker.mu  <  jobsMu  <  transMu
// RecordAccess acquires accessTracker.mu then jobsMu. No code path acquires
// them in reverse order. transMu (dynamic semaphore) is independent.

// AccessTracker tracks last access time for HLS jobs
type AccessTracker struct {
	lastAccess map[string]time.Time
	lastSaved  map[string]time.Time // last time we persisted each job to DB
	saving     map[string]bool      // jobs with a background persist in flight (lazily allocated)
	mu         sync.RWMutex
}

// RecordAccess records an access to an HLS job. The in-memory timestamp is
// always updated (used by the inactive-job cleaner) but the DB write is
// debounced to at most once per accessSaveInterval per job.
func (m *Module) RecordAccess(jobID string) {
	now := time.Now()

	// Always update the in-memory timestamp
	t := m.accessTracker
	t.mu.Lock()
	t.lastAccess[jobID] = now
	lastSave := t.lastSaved[jobID]
	// At most one background persist per job at a time: while the database is
	// unreachable a save can hang for its whole timeout, and a new one every
	// accessSaveInterval would otherwise pile up behind it.
	needsSave := now.Sub(lastSave) >= accessSaveInterval && !t.saving[jobID]
	if needsSave {
		t.lastSaved[jobID] = now
		if t.saving == nil {
			t.saving = make(map[string]bool)
		}
		t.saving[jobID] = true
	}
	t.mu.Unlock()

	if !needsSave {
		return
	}

	// Debounced: persist to DB at most every accessSaveInterval — and off the
	// request path. RecordAccess runs on every playlist/segment request, so a
	// synchronous save made whichever segment crossed the 30s boundary wait on
	// a database round trip (or a whole connect timeout while the database was
	// unreachable): a periodic playback stall for every viewer of the job. The
	// job is snapshotted inside the goroutine so the row written carries the
	// latest status rather than a copy taken before a concurrent update.
	go func() {
		defer func() {
			t.mu.Lock()
			delete(t.saving, jobID)
			t.mu.Unlock()
		}()
		var jobCopy *models.HLSJob
		m.jobsMu.Lock()
		job, exists := m.jobs[jobID]
		if exists {
			job.LastAccessedAt = &now
			jobCopy = copyHLSJob(job)
		}
		m.jobsMu.Unlock()

		if jobCopy != nil {
			_ = m.saveJob(jobCopy)
		}
	}()
}

// GetLastAccess returns the last access time for a job
func (m *Module) GetLastAccess(jobID string) (time.Time, bool) {
	m.accessTracker.mu.RLock()
	defer m.accessTracker.mu.RUnlock()
	t, ok := m.accessTracker.lastAccess[jobID]
	return t, ok
}
