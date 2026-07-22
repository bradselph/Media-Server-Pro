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

const (
	errNonexistentTask = "expected error for nonexistent task"
	taskDisableTest    = "disable-test"
	taskEnableTest     = "enable-test"
	taskScheduleTest   = "schedule-test"
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
	for i := range 5 {
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
		t.Error(errNonexistentTask)
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
		ID:       taskDisableTest,
		Name:     "Disable Test",
		Schedule: 1 * time.Hour,
		Func:     func(_ context.Context) error { return nil },
	})
	if err := m.DisableTask(taskDisableTest); err != nil {
		t.Fatalf("DisableTask error: %v", err)
	}
	info, _ := m.GetTask(taskDisableTest)
	if info.Enabled {
		t.Error("task should be disabled after DisableTask")
	}
}

func TestScheduledExecutionDoesNotRunDisabledTask(t *testing.T) {
	m := newTestModule(t)
	var ran atomic.Int32
	task := &Task{
		ID:       "disabled-claim",
		Name:     "Disabled Claim",
		Schedule: time.Hour,
		Enabled:  false,
		Func: func(context.Context) error {
			ran.Add(1)
			return nil
		},
	}
	if m.executeTask(context.Background(), task, true) {
		t.Fatal("scheduled execution should report disabled")
	}
	if ran.Load() != 0 {
		t.Fatal("disabled scheduled task executed")
	}
}

func TestDisableTaskCancelsClaimedExecution(t *testing.T) {
	m := newTestModule(t)
	started := make(chan struct{})
	finished := make(chan struct{})
	task := &Task{
		ID:       "disable-claimed",
		Name:     "Disable Claimed",
		Schedule: time.Hour,
		Enabled:  true,
		Func: func(ctx context.Context) error {
			close(started)
			<-ctx.Done()
			return ctx.Err()
		},
	}
	m.tasks[task.ID] = task
	go func() {
		m.executeTask(context.Background(), task, true)
		close(finished)
	}()
	<-started
	if err := m.DisableTask(task.ID); err != nil {
		t.Fatalf("DisableTask() error: %v", err)
	}
	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("claimed task was not canceled by DisableTask")
	}
	if ran, _ := m.GetTask(task.ID); ran.Running {
		t.Fatal("task remained marked running after cancellation")
	}
}

func TestEnableTask(t *testing.T) {
	m := newTestModule(t)
	m.Start(context.Background())
	defer m.Stop(context.Background())

	m.RegisterTask(TaskRegistration{
		ID:       taskEnableTest,
		Name:     "Enable Test",
		Schedule: 1 * time.Hour,
		Func:     func(_ context.Context) error { return nil },
	})
	m.DisableTask(taskEnableTest)
	// Wait for loop to notice and stop
	time.Sleep(50 * time.Millisecond)

	if err := m.EnableTask(taskEnableTest); err != nil {
		t.Fatalf("EnableTask error: %v", err)
	}
	info, _ := m.GetTask(taskEnableTest)
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
		t.Error(errNonexistentTask)
	}
}

func TestEnableTask_NotFound(t *testing.T) {
	m := newTestModule(t)
	m.Start(context.Background())
	defer m.Stop(context.Background())

	err := m.EnableTask("nonexistent")
	if err == nil {
		t.Error(errNonexistentTask)
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

	var ran atomic.Int32
	m.RegisterTask(TaskRegistration{
		ID:       "run-now-test",
		Name:     "Run Now Test",
		Schedule: 1 * time.Hour,
		Func: func(_ context.Context) error {
			ran.Add(1)
			return nil
		},
	})
	// Wait for initial run from registration
	time.Sleep(100 * time.Millisecond)
	before := ran.Load()

	if err := m.RunNow("run-now-test"); err != nil {
		t.Fatalf("RunNow error: %v", err)
	}
	time.Sleep(100 * time.Millisecond)
	after := ran.Load()
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
		t.Error(errNonexistentTask)
	}
}

func TestRunNowRejectedOnceStopBegins(t *testing.T) {
	m := newTestModule(t)
	release := make(chan struct{})
	started := make(chan struct{})
	m.RegisterTask(TaskRegistration{
		ID:       "stop-admission",
		Name:     "Stop Admission",
		Schedule: time.Hour,
		Func: func(context.Context) error {
			select {
			case <-started:
			default:
				close(started)
			}
			<-release
			return nil
		},
	})
	if err := m.Start(context.Background()); err != nil {
		t.Fatalf("Start() error: %v", err)
	}
	if err := m.RunNow("stop-admission"); err != nil {
		t.Fatalf("initial RunNow() error: %v", err)
	}
	<-started
	stopDone := make(chan error, 1)
	go func() {
		stopCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		stopDone <- m.Stop(stopCtx)
	}()
	deadline := time.Now().Add(time.Second)
	for {
		m.mu.RLock()
		stopping := m.stopping
		m.mu.RUnlock()
		if stopping {
			break
		}
		if time.Now().After(deadline) {
			close(release)
			t.Fatal("Stop did not close task admission")
		}
		time.Sleep(time.Millisecond)
	}
	if err := m.RunNow("stop-admission"); err == nil {
		close(release)
		t.Fatal("RunNow succeeded after Stop began")
	}
	close(release)
	if err := <-stopDone; err != nil {
		t.Fatalf("Stop() error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// UpdateSchedule
// ---------------------------------------------------------------------------

func TestUpdateSchedule(t *testing.T) {
	m := newTestModule(t)
	m.RegisterTask(TaskRegistration{
		ID:       taskScheduleTest,
		Name:     "Schedule Test",
		Schedule: 1 * time.Hour,
		Func:     func(_ context.Context) error { return nil },
	})
	if err := m.UpdateSchedule(taskScheduleTest, 5*time.Minute); err != nil {
		t.Fatalf("UpdateSchedule error: %v", err)
	}
	info, _ := m.GetTask(taskScheduleTest)
	if info.Schedule != (5 * time.Minute).String() {
		t.Errorf("schedule = %q, want 5m0s", info.Schedule)
	}
}

func TestUpdateSchedule_NotFound(t *testing.T) {
	m := newTestModule(t)
	err := m.UpdateSchedule("nonexistent", 5*time.Minute)
	if err == nil {
		t.Error(errNonexistentTask)
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
		t.Error(errNonexistentTask)
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
