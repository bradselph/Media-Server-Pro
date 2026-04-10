package tasks

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// executeTask — panic recovery
// ---------------------------------------------------------------------------

func TestExecuteTask_PanicRecovery(t *testing.T) {
	m := newTestModule(t)
	m.Start(context.Background())
	m.mu.Lock()
	m.startupDelay = 0
	m.mu.Unlock()
	defer m.Stop(context.Background())

	var ranCount int32
	m.RegisterTask(TaskRegistration{
		ID:       "panic-task",
		Name:     "Panic Task",
		Schedule: 1 * time.Hour,
		Func: func(_ context.Context) error {
			atomic.AddInt32(&ranCount, 1)
			panic("test panic")
		},
	})
	// Wait for the initial run that triggers the panic
	time.Sleep(200 * time.Millisecond)

	info, err := m.GetTask("panic-task")
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	// Task should not be stuck in "running" state after panic
	if info.Running {
		t.Error("task should not be stuck running after panic")
	}
	// Last error should mention panic
	if info.LastError == "" {
		t.Error("LastError should be set after panic")
	}
}

// ---------------------------------------------------------------------------
// RunNow with error — task should still be runnable
// ---------------------------------------------------------------------------

func TestRunNow_AfterError_StillRunnable(t *testing.T) {
	m := newTestModule(t)
	m.Start(context.Background())
	m.mu.Lock()
	m.startupDelay = 0
	m.mu.Unlock()
	defer m.Stop(context.Background())

	var count int32
	m.RegisterTask(TaskRegistration{
		ID:       "retry-test",
		Name:     "Retry Test",
		Schedule: 1 * time.Hour,
		Func: func(_ context.Context) error {
			n := atomic.AddInt32(&count, 1)
			if n == 1 {
				return context.DeadlineExceeded
			}
			return nil
		},
	})
	// Wait for first run (should error)
	time.Sleep(100 * time.Millisecond)

	// RunNow should work after error
	if err := m.RunNow("retry-test"); err != nil {
		t.Fatalf("RunNow after error: %v", err)
	}
	time.Sleep(100 * time.Millisecond)
	if atomic.LoadInt32(&count) < 2 {
		t.Error("task should have run again after RunNow")
	}
}

// ---------------------------------------------------------------------------
// computeTaskTimeout — additional edge cases
// ---------------------------------------------------------------------------

func TestComputeTaskTimeout_ZeroSchedule(t *testing.T) {
	task := &Task{Schedule: 0, Timeout: 0}
	got := computeTaskTimeout(task)
	// Should return minTimeout (30s) since schedule-10s < 30s
	if got < 0 {
		t.Errorf("zero schedule should produce non-negative timeout, got %v", got)
	}
}

func TestComputeTaskTimeout_ExplicitOverridesSchedule(t *testing.T) {
	task := &Task{Schedule: 1 * time.Hour, Timeout: 10 * time.Second}
	got := computeTaskTimeout(task)
	if got != 10*time.Second {
		t.Errorf("explicit timeout should be used, got %v", got)
	}
}

// ---------------------------------------------------------------------------
// RegisterTask after Start — should auto-schedule
// ---------------------------------------------------------------------------

func TestRegisterTask_AfterStart(t *testing.T) {
	m := newTestModule(t)
	m.Start(context.Background())
	m.mu.Lock()
	m.startupDelay = 0
	m.mu.Unlock()
	defer m.Stop(context.Background())

	var ran int32
	m.RegisterTask(TaskRegistration{
		ID:       "late-register",
		Name:     "Late Register",
		Schedule: 1 * time.Hour,
		Func: func(_ context.Context) error {
			atomic.AddInt32(&ran, 1)
			return nil
		},
	})
	time.Sleep(200 * time.Millisecond)
	if atomic.LoadInt32(&ran) < 1 {
		t.Error("task registered after Start should run once immediately")
	}
}

// ---------------------------------------------------------------------------
// ListTasks — ordering and fields
// ---------------------------------------------------------------------------

func TestListTasks_Fields(t *testing.T) {
	m := newTestModule(t)
	m.RegisterTask(TaskRegistration{
		ID:          "field-test",
		Name:        "Field Test",
		Description: "Tests field mapping",
		Schedule:    2 * time.Hour,
		Func:        func(_ context.Context) error { return nil },
	})
	tasks := m.ListTasks()
	if len(tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(tasks))
	}
	info := tasks[0]
	if info.ID != "field-test" {
		t.Errorf("ID = %q", info.ID)
	}
	if info.Name != "Field Test" {
		t.Errorf("Name = %q", info.Name)
	}
	if info.Description != "Tests field mapping" {
		t.Errorf("Description = %q", info.Description)
	}
	if !info.Enabled {
		t.Error("task should be enabled by default")
	}
}
