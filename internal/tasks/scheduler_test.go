package tasks

import (
	"context"
	"fmt"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"media-server-pro/internal/config"
)

func newTestModule(t *testing.T) *Module {
	t.Helper()
	dir := t.TempDir()
	cfg := config.NewManager(filepath.Join(dir, "config.json"))
	return NewModule(cfg)
}

// ---------------------------------------------------------------------------
// Module lifecycle
// ---------------------------------------------------------------------------

func TestModule_Name(t *testing.T) {
	m := newTestModule(t)
	if m.Name() != "tasks" {
		t.Errorf("Name() = %q, want tasks", m.Name())
	}
}

func TestModule_StartStop(t *testing.T) {
	m := newTestModule(t)
	if err := m.Start(context.Background()); err != nil {
		t.Fatalf("Start() error: %v", err)
	}
	h := m.Health()
	if h.Status != "healthy" {
		t.Errorf("after Start, status = %q, want healthy", h.Status)
	}
	if err := m.Stop(context.Background()); err != nil {
		t.Fatalf("Stop() error: %v", err)
	}
	h = m.Health()
	if h.Status != "unhealthy" {
		t.Errorf("after Stop, status = %q, want unhealthy", h.Status)
	}
}

// ---------------------------------------------------------------------------
// RegisterTask
// ---------------------------------------------------------------------------

func TestRegisterTask(t *testing.T) {
	m := newTestModule(t)
	m.RegisterTask(TaskRegistration{
		ID:          "test-task",
		Name:        "Test Task",
		Description: "A test task",
		Schedule:    1 * time.Hour,
		Func:        func(_ context.Context) error { return nil },
	})
	tasks := m.ListTasks()
	if len(tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(tasks))
	}
	if tasks[0].ID != "test-task" {
		t.Errorf("task ID = %q, want test-task", tasks[0].ID)
	}
	if !tasks[0].Enabled {
		t.Error("task should be enabled by default")
	}
}

func TestRegisterTask_Multiple(t *testing.T) {
	m := newTestModule(t)
	for i := 0; i < 5; i++ {
		m.RegisterTask(TaskRegistration{
			ID:       fmt.Sprintf("task-%d", i),
			Name:     fmt.Sprintf("Task %d", i),
			Schedule: 1 * time.Hour,
			Func:     func(_ context.Context) error { return nil },
		})
	}
	tasks := m.ListTasks()
	if len(tasks) != 5 {
		t.Errorf("expected 5 tasks, got %d", len(tasks))
	}
}

// ---------------------------------------------------------------------------
// GetTask
// ---------------------------------------------------------------------------

func TestGetTask(t *testing.T) {
	m := newTestModule(t)
	m.RegisterTask(TaskRegistration{
		ID:          "my-task",
		Name:        "My Task",
		Description: "Does things",
		Schedule:    30 * time.Minute,
		Func:        func(_ context.Context) error { return nil },
	})
	info, err := m.GetTask("my-task")
	if err != nil {
		t.Fatalf("GetTask error: %v", err)
	}
	if info.Name != "My Task" {
		t.Errorf("task name = %q", info.Name)
	}
	if info.Schedule != (30 * time.Minute).String() {
		t.Errorf("task schedule = %q", info.Schedule)
	}
}

func TestGetTask_NotFound(t *testing.T) {
	m := newTestModule(t)
	_, err := m.GetTask("nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent task")
	}
}

// ---------------------------------------------------------------------------
// EnableTask / DisableTask
// ---------------------------------------------------------------------------

func TestDisableTask(t *testing.T) {
	m := newTestModule(t)
	m.Start(context.Background())
	defer m.Stop(context.Background())

	m.RegisterTask(TaskRegistration{
		ID:       "disable-test",
		Name:     "Disable Test",
		Schedule: 1 * time.Hour,
		Func:     func(_ context.Context) error { return nil },
	})
	if err := m.DisableTask("disable-test"); err != nil {
		t.Fatalf("DisableTask error: %v", err)
	}
	info, _ := m.GetTask("disable-test")
	if info.Enabled {
		t.Error("task should be disabled after DisableTask")
	}
}

func TestEnableTask(t *testing.T) {
	m := newTestModule(t)
	m.Start(context.Background())
	defer m.Stop(context.Background())

	m.RegisterTask(TaskRegistration{
		ID:       "enable-test",
		Name:     "Enable Test",
		Schedule: 1 * time.Hour,
		Func:     func(_ context.Context) error { return nil },
	})
	m.DisableTask("enable-test")
	// Wait for loop to notice and stop
	time.Sleep(50 * time.Millisecond)

	if err := m.EnableTask("enable-test"); err != nil {
		t.Fatalf("EnableTask error: %v", err)
	}
	info, _ := m.GetTask("enable-test")
	if !info.Enabled {
		t.Error("task should be enabled after EnableTask")
	}
}

func TestDisableTask_NotFound(t *testing.T) {
	m := newTestModule(t)
	m.Start(context.Background())
	defer m.Stop(context.Background())

	err := m.DisableTask("nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent task")
	}
}

func TestEnableTask_NotFound(t *testing.T) {
	m := newTestModule(t)
	m.Start(context.Background())
	defer m.Stop(context.Background())

	err := m.EnableTask("nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent task")
	}
}

// ---------------------------------------------------------------------------
// RunNow
// ---------------------------------------------------------------------------

func TestRunNow(t *testing.T) {
	m := newTestModule(t)
	m.startupDelay = 0
	m.Start(context.Background())
	defer m.Stop(context.Background())

	var ran int32
	m.RegisterTask(TaskRegistration{
		ID:       "run-now-test",
		Name:     "Run Now Test",
		Schedule: 1 * time.Hour,
		Func: func(_ context.Context) error {
			atomic.AddInt32(&ran, 1)
			return nil
		},
	})
	// Wait for initial run from registration
	time.Sleep(100 * time.Millisecond)
	before := atomic.LoadInt32(&ran)

	if err := m.RunNow("run-now-test"); err != nil {
		t.Fatalf("RunNow error: %v", err)
	}
	time.Sleep(100 * time.Millisecond)
	after := atomic.LoadInt32(&ran)
	if after <= before {
		t.Error("task should have run after RunNow")
	}
}

func TestRunNow_NotFound(t *testing.T) {
	m := newTestModule(t)
	m.Start(context.Background())
	defer m.Stop(context.Background())

	err := m.RunNow("nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent task")
	}
}

// ---------------------------------------------------------------------------
// UpdateSchedule
// ---------------------------------------------------------------------------

func TestUpdateSchedule(t *testing.T) {
	m := newTestModule(t)
	m.RegisterTask(TaskRegistration{
		ID:       "schedule-test",
		Name:     "Schedule Test",
		Schedule: 1 * time.Hour,
		Func:     func(_ context.Context) error { return nil },
	})
	if err := m.UpdateSchedule("schedule-test", 5*time.Minute); err != nil {
		t.Fatalf("UpdateSchedule error: %v", err)
	}
	info, _ := m.GetTask("schedule-test")
	if info.Schedule != (5 * time.Minute).String() {
		t.Errorf("schedule = %q, want 5m0s", info.Schedule)
	}
}

func TestUpdateSchedule_NotFound(t *testing.T) {
	m := newTestModule(t)
	err := m.UpdateSchedule("nonexistent", 5*time.Minute)
	if err == nil {
		t.Error("expected error for nonexistent task")
	}
}

// ---------------------------------------------------------------------------
// GetRunningCount
// ---------------------------------------------------------------------------

func TestGetRunningCount(t *testing.T) {
	m := newTestModule(t)
	if m.GetRunningCount() != 0 {
		t.Error("expected 0 running tasks initially")
	}
}

// ---------------------------------------------------------------------------
// computeTaskTimeout
// ---------------------------------------------------------------------------

func TestComputeTaskTimeout(t *testing.T) {
	tests := []struct {
		name     string
		schedule time.Duration
		timeout  time.Duration
		want     time.Duration
	}{
		{"explicit timeout", 1 * time.Hour, 5 * time.Minute, 5 * time.Minute},
		{"default from 1h schedule", 1 * time.Hour, 0, 59*time.Minute + 50*time.Second},
		{"default from 5m schedule", 5 * time.Minute, 0, 4*time.Minute + 50*time.Second},
		{"minimum 30s", 35 * time.Second, 0, 28 * time.Second}, // 80% of 35s = 28s < 30s min
		{"very short schedule", 10 * time.Second, 0, 8 * time.Second},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			task := &Task{Schedule: tc.schedule, Timeout: tc.timeout}
			got := computeTaskTimeout(task)
			if got != tc.want {
				t.Errorf("computeTaskTimeout() = %v, want %v", got, tc.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Task with error recording
// ---------------------------------------------------------------------------

func TestRecordTaskResult_Error(t *testing.T) {
	m := newTestModule(t)
	m.Start(context.Background())
	// Override startup delay after Start (which sets it to 45s)
	m.mu.Lock()
	m.startupDelay = 0
	m.mu.Unlock()
	defer m.Stop(context.Background())

	taskErr := fmt.Errorf("something broke")
	m.RegisterTask(TaskRegistration{
		ID:       "error-test",
		Name:     "Error Test",
		Schedule: 1 * time.Hour,
		Func: func(_ context.Context) error {
			return taskErr
		},
	})
	// Wait for registration to trigger first run
	time.Sleep(100 * time.Millisecond)
	info, _ := m.GetTask("error-test")
	if info.LastError != "something broke" {
		t.Errorf("LastError = %q, want 'something broke'", info.LastError)
	}
}

// ---------------------------------------------------------------------------
// StopTask
// ---------------------------------------------------------------------------

func TestStopTask_NotFound(t *testing.T) {
	m := newTestModule(t)
	m.Start(context.Background())
	defer m.Stop(context.Background())
	err := m.StopTask("nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent task")
	}
}

func TestStopTask_NotRunning(t *testing.T) {
	m := newTestModule(t)
	m.Start(context.Background())
	defer m.Stop(context.Background())

	m.RegisterTask(TaskRegistration{
		ID:       "stop-test",
		Name:     "Stop Test",
		Schedule: 1 * time.Hour,
		Func:     func(_ context.Context) error { return nil },
	})
	// Let the initial run finish
	time.Sleep(100 * time.Millisecond)

	err := m.StopTask("stop-test")
	if err == nil {
		t.Error("expected error for non-running task")
	}
}
