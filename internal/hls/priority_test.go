package hls

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"media-server-pro/pkg/models"
)

// ---------------------------------------------------------------------------
// tryAcquireTranscode priority semantics (H2 / C00: on-demand playback must
// not queue behind background pre-generation; C07: pregen must use spare
// capacity instead of skipping the whole cycle).
// ---------------------------------------------------------------------------

// TestTryAcquireTranscode_ReservedSlotForHighPriority verifies that when
// limit > 1, low-priority (background pre-generation) callers are capped at
// limit-1 so a high-priority (viewer) caller always has a free slot.
func TestTryAcquireTranscode_ReservedSlotForHighPriority(t *testing.T) {
	m := newConcurrencyModule(t, 3)

	if !m.tryAcquireTranscode(false) {
		t.Fatal("expected first low-priority acquire to succeed")
	}
	if !m.tryAcquireTranscode(false) {
		t.Fatal("expected second low-priority acquire to succeed (limit-1 = 2)")
	}
	if m.tryAcquireTranscode(false) {
		t.Fatal("third low-priority acquire should be capped at limit-1, leaving a slot reserved for viewers")
	}
	if !m.tryAcquireTranscode(true) {
		t.Fatal("high-priority acquire should still get the reserved slot")
	}
}

// TestTryAcquireTranscode_HighPriorityUsesFullLimit verifies high-priority
// callers may use every slot up to EffectiveConcurrentLimit(), not just the
// slots left over after the low-priority cap.
func TestTryAcquireTranscode_HighPriorityUsesFullLimit(t *testing.T) {
	m := newConcurrencyModule(t, 2)

	if !m.tryAcquireTranscode(true) {
		t.Fatal("expected first high-priority acquire to succeed")
	}
	if !m.tryAcquireTranscode(true) {
		t.Fatal("expected second high-priority acquire to succeed (full limit = 2)")
	}
	if m.tryAcquireTranscode(true) {
		t.Fatal("third high-priority acquire should fail once the full limit is reached")
	}
}

// TestTryAcquireTranscode_LowBlockedWhileHighWaiting verifies a low-priority
// acquire fails outright while any high-priority caller is registered as
// spinning for a slot (waitingHigh > 0) — even at limit == 1, where there is
// no spare slot to reserve — so a viewer always wins the next release instead
// of racing a background job for it.
func TestTryAcquireTranscode_LowBlockedWhileHighWaiting(t *testing.T) {
	m := newConcurrencyModule(t, 1)

	m.waitingHigh.Add(1)
	defer m.waitingHigh.Add(-1)

	if m.tryAcquireTranscode(false) {
		t.Fatal("low-priority acquire should fail while a high-priority waiter is spinning, even at limit==1")
	}
	if !m.tryAcquireTranscode(true) {
		t.Fatal("high-priority acquire should still succeed")
	}
}

// TestTryAcquireTranscode_LowPriorityCapacityAtLimitOne verifies that when
// limit == 1 and no high-priority caller is waiting, a low-priority job may
// still use the single slot — reserving a slot would disable pre-generation
// outright, which is not the intent.
func TestTryAcquireTranscode_LowPriorityCapacityAtLimitOne(t *testing.T) {
	m := newConcurrencyModule(t, 1)

	if !m.tryAcquireTranscode(false) {
		t.Fatal("low-priority acquire should succeed at limit==1 when no high-priority waiter is spinning")
	}
}

// ---------------------------------------------------------------------------
// Per-job priority tracking and upgrade (C00: a job a background pregen cycle
// already queued at low priority must be promoted when a viewer requests it).
// ---------------------------------------------------------------------------

func TestJobPriority_DefaultsLowWhenUnregistered(t *testing.T) {
	m := &Module{}
	if m.isJobHighPriority("unknown") {
		t.Error("an unregistered job should default to low priority")
	}
}

func TestJobPriority_SetAndRead(t *testing.T) {
	m := &Module{}
	m.setJobPriority("job-low", false)
	if m.isJobHighPriority("job-low") {
		t.Error("job-low should read back as low priority")
	}
	m.setJobPriority("job-high", true)
	if !m.isJobHighPriority("job-high") {
		t.Error("job-high should read back as high priority")
	}
}

func TestUpgradeJobPriority_PromotesRegisteredJob(t *testing.T) {
	m := &Module{}
	m.setJobPriority("job1", false)
	m.upgradeJobPriority("job1")
	if !m.isJobHighPriority("job1") {
		t.Error("upgradeJobPriority should promote a registered low-priority job to high")
	}
}

func TestUpgradeJobPriority_RegistersUnknownJobAsHigh(t *testing.T) {
	// Defensive: if upgrade somehow races ahead of registration, it should
	// still end up high rather than silently doing nothing.
	m := &Module{}
	m.upgradeJobPriority("ghost")
	if !m.isJobHighPriority("ghost") {
		t.Error("upgradeJobPriority should register an unknown job as high priority")
	}
}

// TestExistingJobOrRetryErrorLocked_UpgradesReusedPendingJob verifies that
// GenerateHLS/CheckOrGenerateHLS reusing an existing Pending job created at
// low priority (e.g. by hls-pregenerate) promotes it to high priority when
// the new caller is high priority (e.g. a viewer's request).
func TestExistingJobOrRetryErrorLocked_UpgradesReusedPendingJob(t *testing.T) {
	m := &Module{jobs: map[string]*models.HLSJob{
		"job1": {ID: "job1", Status: models.HLSStatusPending},
	}}
	m.setJobPriority("job1", false)

	existing, done, err := m.existingJobOrRetryErrorLocked(&createOrReuseHLSJobParams{JobID: "job1", HighPriority: true})
	if err != nil || !done || existing == nil {
		t.Fatalf("existingJobOrRetryErrorLocked = (%v, %v, %v), want a reused pending job", existing, done, err)
	}
	if !m.isJobHighPriority("job1") {
		t.Error("a high-priority caller reusing a pending low-priority job should upgrade it")
	}
}

// TestExistingJobOrRetryErrorLocked_LowPriorityDoesNotUpgrade verifies job
// reuse never demotes/promotes priority when the new caller is itself low
// priority (e.g. a second pregen cycle re-scanning the same pending item).
func TestExistingJobOrRetryErrorLocked_LowPriorityDoesNotUpgrade(t *testing.T) {
	m := &Module{jobs: map[string]*models.HLSJob{
		"job1": {ID: "job1", Status: models.HLSStatusPending},
	}}
	m.setJobPriority("job1", false)

	if _, _, err := m.existingJobOrRetryErrorLocked(&createOrReuseHLSJobParams{JobID: "job1", HighPriority: false}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m.isJobHighPriority("job1") {
		t.Error("a low-priority caller reusing a pending job should not upgrade it")
	}
}

// ---------------------------------------------------------------------------
// acquireTranscodeSem: mid-spin upgrade and no-deadlock regression.
// ---------------------------------------------------------------------------

// TestAcquireTranscodeSem_UpgradeUnblocksLowPriorityJob simulates the C00
// scenario end-to-end: a low-priority (pregen) job is already spinning for
// the sole transcode slot, held by another job. Upgrading the low-priority
// job's tracked priority mid-spin (as CheckOrGenerateHLS does when a viewer
// requests the same item) lets it acquire the slot the instant it frees.
func TestAcquireTranscodeSem_UpgradeUnblocksLowPriorityJob(t *testing.T) {
	m := newConcurrencyModule(t, 1) // limit == 1: no spare slot to reserve
	lowJob := &models.HLSJob{ID: "low", Status: models.HLSStatusPending}
	m.jobs = map[string]*models.HLSJob{"low": lowJob}
	m.setJobPriority("low", false)

	// Occupy the only slot (simulates another job already running).
	if !m.tryAcquireTranscode(true) {
		t.Fatal("setup: expected to acquire the only slot")
	}

	acquired := make(chan bool, 1)
	go func() {
		acquired <- m.acquireTranscodeSem(context.Background(), lowJob)
	}()

	// Give the low-priority goroutine time to start spinning (blocked: it's
	// capped below the low-priority capacity while the slot is held).
	time.Sleep(50 * time.Millisecond)
	select {
	case <-acquired:
		t.Fatal("low-priority job should not have acquired the sole slot while it's held")
	default:
	}

	// A viewer now requests the same item: upgrade it in place, exactly as
	// CheckOrGenerateHLS does for an existing Pending job.
	m.upgradeJobPriority("low")

	// Release the slot; the newly-upgraded job should win it on its next tick.
	m.releaseTranscode()

	select {
	case ok := <-acquired:
		if !ok {
			t.Fatal("acquireTranscodeSem returned false after being upgraded to high priority")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("acquireTranscodeSem did not acquire the slot after being upgraded to high priority")
	}
	m.releaseTranscode()
}

// TestAcquireTranscodeSem_NoDeadlockUnderConcurrency is a lightweight
// regression check that mixed high/low priority acquisitions never deadlock
// and every job eventually completes, exercising both the reserved-slot cap
// and the waitingHigh yield together under real goroutine contention.
func TestAcquireTranscodeSem_NoDeadlockUnderConcurrency(t *testing.T) {
	m := newConcurrencyModule(t, 2)
	m.jobs = map[string]*models.HLSJob{}
	m.jobCancels = map[string]context.CancelFunc{}
	m.jobDone = map[string]chan struct{}{}

	const n = 6 // 3 high priority, 3 low priority
	var wg sync.WaitGroup
	for i := range n {
		id := fmt.Sprintf("job%d", i)
		job := &models.HLSJob{ID: id, Status: models.HLSStatusPending}
		m.jobsMu.Lock()
		m.jobs[id] = job
		m.jobsMu.Unlock()
		m.setJobPriority(id, i%2 == 0)

		wg.Add(1)
		go func(job *models.HLSJob) {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if !m.acquireTranscodeSem(ctx, job) {
				t.Errorf("acquireTranscodeSem for %s failed/timed out", job.ID)
				return
			}
			time.Sleep(5 * time.Millisecond)
			m.releaseTranscode()
		}(job)
	}

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(8 * time.Second):
		t.Fatal("deadlock: not all goroutines completed")
	}
}
