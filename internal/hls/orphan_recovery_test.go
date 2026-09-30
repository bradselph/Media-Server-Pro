package hls

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"media-server-pro/internal/config"
	"media-server-pro/pkg/models"
)

// ---------------------------------------------------------------------------
// Regression tests for HLS jobs that were accepted but never completed:
//   - Pending jobs left without a worker goroutine (orphans) after a restart
//     were returned as-is by GenerateHLS/CheckOrGenerateHLS, so "Generate HLS"
//     reported success while nothing ever ran them.
//   - The hourly stale-lock sweep killed healthy encodes that ran longer than
//     StaleLockThreshold because the lock timestamp is never refreshed.
//   - Stop() marked in-flight work Canceled, so every restart discarded it.
// ---------------------------------------------------------------------------

func newRecoveryTestModule(t *testing.T) *Module {
	t.Helper()
	m := newTestModuleWithConfig(t)
	m.repo = &stubHLSRepo{}
	m.jobs = map[string]*models.HLSJob{}
	m.jobCancels = map[string]context.CancelFunc{}
	m.jobDone = map[string]chan struct{}{}
	m.cacheDir = t.TempDir()
	return m
}

func TestExistingJobOrRetryErrorLocked_OrphanedPendingIsRequeued(t *testing.T) {
	m := newRecoveryTestModule(t)
	m.jobs["job1"] = &models.HLSJob{ID: "job1", Status: models.HLSStatusPending} // no jobCancels entry

	existing, done, err := m.existingJobOrRetryErrorLocked(&createOrReuseHLSJobParams{JobID: "job1"})
	if err != nil || done || existing != nil {
		t.Fatalf("orphaned pending job = (%v, %v, %v); want fall-through (nil, false, nil) so it is re-queued", existing, done, err)
	}
}

func TestExistingJobOrRetryErrorLocked_LivePendingIsReused(t *testing.T) {
	m := newRecoveryTestModule(t)
	m.jobs["job1"] = &models.HLSJob{ID: "job1", Status: models.HLSStatusPending}
	m.jobCancels["job1"] = func() {}

	existing, done, err := m.existingJobOrRetryErrorLocked(&createOrReuseHLSJobParams{JobID: "job1"})
	if err != nil || !done || existing == nil {
		t.Fatalf("live pending job = (%v, %v, %v); want it reused as-is", existing, done, err)
	}
}

func TestExistingJobOrRetryErrorLocked_CompletedWithMissingMasterRegenerates(t *testing.T) {
	m := newRecoveryTestModule(t)
	m.jobs["job1"] = &models.HLSJob{ID: "job1", Status: models.HLSStatusCompleted, OutputDir: t.TempDir()}

	if _, done, _ := m.existingJobOrRetryErrorLocked(&createOrReuseHLSJobParams{JobID: "job1"}); done {
		t.Fatal("completed job whose master.m3u8 is gone must fall through to regeneration")
	}
}

func TestExistingJobOrRetryErrorLocked_ResetFailuresBypassesBreaker(t *testing.T) {
	m := newRecoveryTestModule(t)
	m.jobs["job1"] = &models.HLSJob{ID: "job1", Status: models.HLSStatusFailed, FailCount: 99}

	if _, _, err := m.existingJobOrRetryErrorLocked(&createOrReuseHLSJobParams{JobID: "job1"}); err == nil {
		t.Fatal("automatic retry past maxFailures should be refused")
	}
	if _, done, err := m.existingJobOrRetryErrorLocked(&createOrReuseHLSJobParams{JobID: "job1", ResetFailures: true}); err != nil || done {
		t.Fatalf("explicit ResetFailures retry = (done=%v, err=%v); want fall-through to re-queue", done, err)
	}
}

func TestTryResolveExistingJob_Classification(t *testing.T) {
	m := newRecoveryTestModule(t)
	m.jobs["live"] = &models.HLSJob{ID: "live", Status: models.HLSStatusRunning}
	m.jobCancels["live"] = func() {}
	m.jobs["orphan"] = &models.HLSJob{ID: "orphan", Status: models.HLSStatusPending}
	m.jobs["failed-retry"] = &models.HLSJob{ID: "failed-retry", Status: models.HLSStatusFailed, FailCount: 1}
	m.jobs["failed-final"] = &models.HLSJob{ID: "failed-final", Status: models.HLSStatusFailed, FailCount: 99}
	m.jobs["canceled-empty"] = &models.HLSJob{ID: "canceled-empty", Status: models.HLSStatusCanceled}
	m.jobs["canceled-playable"] = &models.HLSJob{ID: "canceled-playable", Status: models.HLSStatusCanceled, Available: true}

	for id, want := range map[string]existingJobState{
		"live":              existingJobUsable,
		"orphan":            existingJobOrphaned,
		"failed-retry":      existingJobRetryable,
		"failed-final":      existingJobUsable,
		"canceled-empty":    existingJobRetryable,
		"canceled-playable": existingJobUsable,
		"missing":           existingJobNone,
	} {
		if _, got := m.tryResolveExistingJob(id); got != want {
			t.Errorf("tryResolveExistingJob(%q) = %v, want %v", id, got, want)
		}
	}
}

func TestReleaseJobWorkerLocked_OnlyRemovesOwnRegistration(t *testing.T) {
	m := newRecoveryTestModule(t)
	oldDone := make(chan struct{})
	newDone := make(chan struct{})
	// A newer goroutine has re-registered the same job ID.
	m.jobDone["job1"] = newDone
	m.jobCancels["job1"] = func() {}

	m.releaseJobWorkerLocked("job1", oldDone)
	if !m.hasLiveWorkerLocked("job1") {
		t.Fatal("an exiting old goroutine must not unregister the newer goroutine's worker")
	}
	m.releaseJobWorkerLocked("job1", newDone)
	if m.hasLiveWorkerLocked("job1") {
		t.Fatal("the owning goroutine should unregister its worker on exit")
	}
}

func TestActiveJobCountAndPregenChecks_IgnoreOrphans(t *testing.T) {
	m := newRecoveryTestModule(t)
	m.jobs["live"] = &models.HLSJob{ID: "live", Status: models.HLSStatusPending}
	m.jobCancels["live"] = func() {}
	m.jobs["orphan"] = &models.HLSJob{ID: "orphan", Status: models.HLSStatusPending}
	m.jobs["blocked"] = &models.HLSJob{ID: "blocked", Status: models.HLSStatusFailed, FailCount: 99}
	m.jobs["retry"] = &models.HLSJob{ID: "retry", Status: models.HLSStatusFailed, FailCount: 1}

	if got := m.ActiveJobCount(); got != 1 {
		t.Errorf("ActiveJobCount = %d, want 1 (orphans hold no slot and must not starve pregen)", got)
	}
	if !m.IsJobActive("live") || m.IsJobActive("orphan") {
		t.Error("IsJobActive should be true only for the job with a live worker")
	}
	for id, want := range map[string]bool{"live": false, "orphan": true, "blocked": false, "retry": true, "new": true} {
		if got := m.NeedsPregen(id); got != want {
			t.Errorf("NeedsPregen(%q) = %v, want %v", id, got, want)
		}
	}
}

// writeLock writes a .lock file for jobID that is far older than any stale threshold.
func writeOldLock(t *testing.T, m *Module, jobID string) string {
	t.Helper()
	dir := filepath.Join(m.cacheDir, jobID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(LockFile{JobID: jobID, MediaPath: "/m.mp4", StartedAt: time.Now().Add(-48 * time.Hour), PID: os.Getpid()})
	lockPath := filepath.Join(dir, ".lock")
	if err := os.WriteFile(lockPath, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return lockPath
}

func TestCleanStaleLocks_SparesLiveLongRunningEncode(t *testing.T) {
	m := newRecoveryTestModule(t)
	canceled := false
	m.jobs["long"] = &models.HLSJob{ID: "long", Status: models.HLSStatusRunning}
	m.jobCancels["long"] = func() { canceled = true }
	lockPath := writeOldLock(t, m, "long")

	if removed := m.CleanStaleLocks(); removed != 0 {
		t.Errorf("CleanStaleLocks removed %d locks; a live encode's lock must be left alone", removed)
	}
	if canceled {
		t.Fatal("a healthy long-running encode must not be canceled by the stale-lock sweep")
	}
	if got := m.jobs["long"].Status; got != models.HLSStatusRunning {
		t.Errorf("status = %s, want running", got)
	}
	if _, err := os.Stat(lockPath); err != nil {
		t.Errorf("lock file should still exist: %v", err)
	}
}

func TestCleanStaleLocks_StillCleansDeadJobs(t *testing.T) {
	m := newRecoveryTestModule(t)
	m.jobs["dead"] = &models.HLSJob{ID: "dead", Status: models.HLSStatusRunning} // no worker
	lockPath := writeOldLock(t, m, "dead")

	if removed := m.CleanStaleLocks(); removed != 1 {
		t.Errorf("CleanStaleLocks removed %d, want 1", removed)
	}
	if got := m.jobs["dead"].Status; got != models.HLSStatusFailed {
		t.Errorf("status = %s, want failed", got)
	}
	if _, err := os.Stat(lockPath); !os.IsNotExist(err) {
		t.Errorf("stale lock should be removed, stat err = %v", err)
	}
}

func TestWatchFFmpegStall(t *testing.T) {
	t.Run("fires when output stops growing", func(t *testing.T) {
		fired := make(chan struct{})
		go watchFFmpegStall(make(chan struct{}), func() int64 { return 42 }, 50*time.Millisecond, func() { close(fired) })
		select {
		case <-fired:
		case <-time.After(2 * time.Second):
			t.Fatal("watchdog did not fire for an encode whose output stopped growing")
		}
	})

	t.Run("does not fire while output keeps growing", func(t *testing.T) {
		var size atomic.Int64
		done := make(chan struct{})
		fired := make(chan struct{}, 1)
		go watchFFmpegStall(done, size.Load, 100*time.Millisecond, func() { fired <- struct{}{} })
		deadline := time.After(500 * time.Millisecond)
	loop:
		for {
			select {
			case <-fired:
				t.Fatal("watchdog fired although output was growing")
			case <-deadline:
				break loop
			case <-time.After(10 * time.Millisecond):
				size.Add(1024)
			}
		}
		close(done)
	})

	t.Run("dirBytes sums segment files", func(t *testing.T) {
		dir := t.TempDir()
		if got := dirBytes(dir); got != 0 {
			t.Fatalf("empty dir = %d, want 0", got)
		}
		if err := os.WriteFile(filepath.Join(dir, "segment_0000.ts"), make([]byte, 100), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "segment_0001.ts"), make([]byte, 50), 0o644); err != nil {
			t.Fatal(err)
		}
		if got := dirBytes(dir); got != 150 {
			t.Errorf("dirBytes = %d, want 150", got)
		}
		if got := dirBytes(filepath.Join(dir, "missing")); got != 0 {
			t.Errorf("missing dir = %d, want 0", got)
		}
	})
}

func TestInterruptedStatus(t *testing.T) {
	m := newRecoveryTestModule(t)
	if status, _ := m.interruptedStatus("x"); status != models.HLSStatusCanceled {
		t.Errorf("explicit cancel -> %s, want canceled", status)
	}
	m.stopping.Store(true)
	if status, _ := m.interruptedStatus("x"); status != models.HLSStatusPending {
		t.Errorf("shutdown interruption -> %s, want pending (resumable)", status)
	}
}

func TestStop_ParksRunningJobsAsPendingForResume(t *testing.T) {
	m := newRecoveryTestModule(t)
	m.jobs["run"] = &models.HLSJob{ID: "run", Status: models.HLSStatusRunning}
	if err := m.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	job := m.jobs["run"]
	if job.Status != models.HLSStatusPending {
		t.Fatalf("status after Stop = %s, want pending", job.Status)
	}
	if !m.shouldResumeJob(job) {
		t.Error("a job interrupted by shutdown must be resumed on the next start")
	}
	if _, err := m.GenerateHLS(context.Background(), &GenerateHLSParams{MediaPath: "v.mp4", MediaID: "x"}); err == nil {
		t.Error("GenerateHLS during shutdown should refuse instead of spawning an orphaned goroutine")
	}
}

func TestResumeInterruptedJobs_ResumesAllNotJustConcurrentLimit(t *testing.T) {
	m := newRecoveryTestModule(t)
	if err := m.config.Update(func(c *config.Config) { c.HLS.ConcurrentLimit = 1 }); err != nil {
		t.Fatal(err)
	}
	const n = 4
	for i := range n {
		id := string(rune('a' + i))
		// Relative path skips the os.Stat existence check; no qualities means
		// the transcode loop finalizes immediately without invoking ffmpeg.
		m.jobs[id] = &models.HLSJob{ID: id, Status: models.HLSStatusPending, MediaPath: "v.mp4", OutputDir: filepath.Join(m.cacheDir, id)}
	}

	if resumed := m.resumeInterruptedJobs(); resumed != n {
		t.Fatalf("resumed %d jobs, want all %d (limit=1 must not leave the rest orphaned)", resumed, n)
	}
	m.activeJobs.Wait()
	for id, job := range m.jobs {
		if job.Status != models.HLSStatusCompleted {
			t.Errorf("job %s status = %s, want completed", id, job.Status)
		}
	}
}

func TestRunRefillLoop_RunsRefillAfterJobFinishes(t *testing.T) {
	m := newRecoveryTestModule(t)
	m.jobFinished = make(chan struct{}, 1)
	ran := make(chan struct{}, 4)
	m.SetBackgroundRefill(func(context.Context) { ran <- struct{}{} })

	// A burst of completions coalesces into one refill (signaled before the
	// loop starts so none is consumed mid-burst).
	m.notifyJobFinished()
	m.notifyJobFinished()
	m.notifyJobFinished()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go m.runRefillLoop(ctx)

	select {
	case <-ran:
	case <-time.After(refillDebounce + 2*time.Second):
		t.Fatal("background refill did not run after a job finished")
	}
	select {
	case <-ran:
		t.Fatal("a burst of completions should trigger a single refill")
	case <-time.After(refillDebounce + 500*time.Millisecond):
	}
}
