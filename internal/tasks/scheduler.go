// Package tasks provides background task scheduling and execution.
// It handles periodic tasks like cleanup, maintenance, and auto-discovery.
package tasks

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"media-server-pro/internal/config"
	"media-server-pro/internal/logger"
	"media-server-pro/pkg/helpers"
	"media-server-pro/pkg/models"
)

const errTaskNotFoundFmt = "task not found: %s"

// TaskFunc is a function that performs a task
type TaskFunc func(ctx context.Context) error

// TaskRegistration holds parameters for registering a scheduled task.
type TaskRegistration struct {
	ID          string
	Name        string
	Description string
	Schedule    time.Duration
	Func        TaskFunc
}

// Task represents a scheduled task
type Task struct {
	ID          string
	Name        string
	Description string
	Schedule    time.Duration
	Timeout     time.Duration // 0 means use default (Schedule - 10s, min 30s)
	Func        TaskFunc
	LastRun     time.Time
	NextRun     time.Time
	Running     bool
	LastError   error
	Enabled     bool
	loopRunning bool               // tracks if runTaskLoop goroutine is active
	reschedule  chan time.Duration // signals runTaskLoop to recreate its ticker

	// stopRunning cancels the context of the currently executing task function.
	// It is set by executeTask and cleared when execution finishes.
	stopRunning context.CancelFunc
	stopMu      sync.Mutex // protects stopRunning
}

// defaultStartupDelay is how long all tasks wait before their first execution.
// Kept short so tasks run "initially" soon after startup, then at their intervals.
// Prevents all tasks from firing the instant the scheduler starts.
const defaultStartupDelay = 10 * time.Second

// Module implements task scheduling
type Module struct {
	config       *config.Manager
	log          *logger.Logger
	tasks        map[string]*Task
	mu           sync.RWMutex
	ctx          context.Context
	cancel       context.CancelFunc
	done         <-chan struct{}
	wg           sync.WaitGroup
	startupDelay time.Duration // how long to wait before the first task execution
	healthy      bool
	healthMsg    string
	healthMu     sync.RWMutex
}

// NewModule creates a new tasks module
func NewModule(cfg *config.Manager) *Module {
	return &Module{
		config: cfg,
		log:    logger.New("tasks"),
		tasks:  make(map[string]*Task),
	}
}

// Name returns the module name
func (m *Module) Name() string {
	return "tasks"
}

// Start initializes the task scheduler
func (m *Module) Start(_ context.Context) error {
	m.log.Info("Starting task scheduler...")

	// Apply a startup delay so that tasks don't all fire the instant the
	// scheduler starts.  This prevents a flood of DB queries while the
	// remaining modules (scanner, thumbnails, …) are still initializing.
	m.startupDelay = defaultStartupDelay

	// Use a background context for task goroutines so they aren't canceled
	// when the server's module-startup context completes.
	taskCtx, cancel := context.WithCancel(context.Background()) //nolint:gosec // G118: cancel stored in m.cancel, called by Stop()
	m.ctx = taskCtx
	m.cancel = cancel
	m.done = taskCtx.Done()

	// Start all enabled tasks.
	// Use a write lock so we can set loopRunning=true atomically before
	// spawning goroutines.  This prevents EnableTask from racing Start and
	// launching a duplicate runTaskLoop for the same task.
	m.mu.Lock()
	for _, task := range m.tasks {
		if task.Enabled {
			task.loopRunning = true
			m.wg.Add(1)
			go m.runTaskLoop(taskCtx, task)
		}
	}
	m.mu.Unlock()

	m.healthMu.Lock()
	m.healthy = true
	m.healthMsg = fmt.Sprintf("Running (%d tasks, startup delay: %v)", len(m.tasks), m.startupDelay)
	m.healthMu.Unlock()
	m.log.Info("Task scheduler started with %d tasks (first run in %v)", len(m.tasks), m.startupDelay)
	return nil
}

// Stop gracefully stops all scheduled tasks
func (m *Module) Stop(ctx context.Context) error {
	m.log.Info("Stopping task scheduler...")

	m.cancel()

	// Wait for all tasks to finish with timeout
	done := make(chan struct{})
	go func() {
		m.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		m.log.Info("All tasks stopped gracefully")
	case <-ctx.Done():
		m.log.Warn("Task shutdown timed out")
	}

	m.healthMu.Lock()
	m.healthy = false
	m.healthMsg = "Stopped"
	m.healthMu.Unlock()
	return nil
}

// Health returns the module health status
func (m *Module) Health() models.HealthStatus {
	m.healthMu.RLock()
	defer m.healthMu.RUnlock()
	return models.HealthStatus{
		Name:      m.Name(),
		Status:    helpers.StatusString(m.healthy),
		Message:   m.healthMsg,
		CheckedAt: time.Now(),
	}
}

// RegisterTask registers a new scheduled task. If the scheduler has already been
// started (m.ctx set), the task's runTaskLoop is started immediately so it is not idle.
func (m *Module) RegisterTask(opts TaskRegistration) {
	m.mu.Lock()
	defer m.mu.Unlock()

	task := &Task{
		ID:          opts.ID,
		Name:        opts.Name,
		Description: opts.Description,
		Schedule:    opts.Schedule,
		Func:        opts.Func,
		NextRun:     time.Now().Add(opts.Schedule),
		Enabled:     true,
		reschedule:  make(chan time.Duration, 1),
	}

	m.tasks[opts.ID] = task
	m.log.Info("Registered task: %s (schedule: %v)", opts.Name, opts.Schedule)

	if m.ctx != nil && task.Enabled && !task.loopRunning {
		task.loopRunning = true
		m.wg.Add(1)
		go m.runTaskLoop(m.ctx, task)
	}
}

// waitForStartupDelay waits for the configured startup delay unless ctx is canceled.
// Returns false if context was canceled during the wait, true otherwise.
func (m *Module) waitForStartupDelay(ctx context.Context, task *Task) bool {
	m.mu.RLock()
	delay := m.startupDelay
	m.mu.RUnlock()
	if delay == 0 {
		return true
	}
	m.log.Debug("Task %s: waiting %v before first run", task.Name, delay)
	select {
	case <-ctx.Done():
		return false
	case <-time.After(delay):
		return true
	}
}

// tryRunScheduledTask runs the task on a schedule tick if it is still enabled.
// Returns false if the task was disabled (caller should stop the loop), true otherwise.
func (m *Module) tryRunScheduledTask(ctx context.Context, task *Task) bool {
	m.mu.RLock()
	enabled := task.Enabled
	m.mu.RUnlock()
	if !enabled {
		return false
	}
	m.executeTask(ctx, task)
	return true
}

// runTaskLoop runs a task on its schedule
func (m *Module) runTaskLoop(ctx context.Context, task *Task) {
	defer m.wg.Done()
	defer func() {
		m.mu.Lock()
		// If the task was re-enabled while this goroutine was in its shutdown path
		// (EnableTask saw loopRunning=true and set Enabled=true, then we arrived here
		// committed to exiting), self-restart so the task is never stuck "enabled"
		// without a running loop.
		if task.Enabled && ctx.Err() == nil {
			m.wg.Add(1)
			go m.runTaskLoop(ctx, task)
		} else {
			task.loopRunning = false
		}
		m.mu.Unlock()
	}()

	if !m.waitForStartupDelay(ctx, task) {
		return
	}

	ticker := time.NewTicker(task.Schedule)
	defer ticker.Stop()

	// Check if the task was disabled during the startup delay before the initial run.
	m.mu.RLock()
	enabled := task.Enabled
	m.mu.RUnlock()
	if !enabled {
		m.log.Debug("Task %s: skipping initial run (disabled during startup delay)", task.Name)
	} else {
		m.log.Debug("Task %s: initial run", task.Name)
		m.executeTask(ctx, task)
	}

	for {
		select {
		case <-ctx.Done():
			return
		case newSchedule := <-task.reschedule:
			ticker.Stop()
			select {
			case <-ticker.C:
			default:
			}
			ticker = time.NewTicker(newSchedule)
		case <-ticker.C:
			if !m.tryRunScheduledTask(ctx, task) {
				return
			}
		}
	}
}

// computeTaskTimeout returns the timeout for a task run. Uses task.Timeout if set;
// otherwise Schedule minus 10s, with min 30s and for short intervals cap at 80% of schedule.
func computeTaskTimeout(task *Task) time.Duration {
	if task.Timeout > 0 {
		return task.Timeout
	}
	timeout := task.Schedule - 10*time.Second
	minTimeout := 30 * time.Second
	maxTimeout := time.Duration(float64(task.Schedule) * 0.8)
	if maxTimeout > 0 && maxTimeout < minTimeout {
		return maxTimeout
	}
	if timeout < minTimeout {
		return minTimeout
	}
	return timeout
}

// recordTaskResult updates task.LastError and logs the outcome of a task run.
func (m *Module) recordTaskResult(task *Task, err error, start time.Time) {
	m.mu.Lock()
	task.LastError = err
	m.mu.Unlock()
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			m.log.Info("Task %s canceled: %v", task.Name, err)
		} else {
			m.log.Error("Task %s failed: %v", task.Name, err)
		}
		return
	}
	m.log.Debug("Task %s completed in %v", task.Name, time.Since(start))
}

// executeTask runs a single task execution with panic recovery.
func (m *Module) executeTask(ctx context.Context, task *Task) {
	m.mu.Lock()
	if task.Running {
		m.mu.Unlock()
		m.log.Debug("Task %s already running, skipping", task.Name)
		return
	}
	task.Running = true
	m.mu.Unlock()

	defer func() {
		m.mu.Lock()
		task.Running = false
		task.LastRun = time.Now()
		task.NextRun = time.Now().Add(task.Schedule)
		m.mu.Unlock()
	}()

	m.log.Debug("Executing task: %s", task.Name)
	start := time.Now()
	timeout := computeTaskTimeout(task)
	ctx, cancel := context.WithTimeout(ctx, timeout)

	task.stopMu.Lock()
	task.stopRunning = cancel
	task.stopMu.Unlock()

	defer func() {
		cancel()
		task.stopMu.Lock()
		task.stopRunning = nil
		task.stopMu.Unlock()
	}()

	// Recover from panics so a single misbehaving task doesn't permanently
	// mark itself as Running=true and block future executions.
	var err error
	func() {
		defer func() {
			if r := recover(); r != nil {
				err = fmt.Errorf("task panicked: %v", r)
				m.log.Error("Task %s panicked: %v", task.Name, r)
			}
		}()
		err = task.Func(ctx)
	}()
	m.recordTaskResult(task, err, start)
}

// RunNow triggers immediate execution of a task. The goroutine is tracked by m.wg
// so Stop() waits for it to complete.
func (m *Module) RunNow(taskID string) error {
	m.mu.RLock()
	task, exists := m.tasks[taskID]
	ctx := m.ctx
	m.mu.RUnlock()

	if !exists {
		return fmt.Errorf(errTaskNotFoundFmt, taskID)
	}

	if ctx == nil {
		return fmt.Errorf("task scheduler not started")
	}

	if ctx.Err() != nil {
		return fmt.Errorf("task scheduler is stopping")
	}

	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		m.executeTask(ctx, task)
	}()
	return nil
}

// EnableTask enables a task
func (m *Module) EnableTask(taskID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	task, exists := m.tasks[taskID]
	if !exists {
		return fmt.Errorf(errTaskNotFoundFmt, taskID)
	}

	// Only start a new goroutine if task is not enabled and no loop is running
	if !task.Enabled && !task.loopRunning {
		task.Enabled = true
		task.loopRunning = true
		m.wg.Add(1)
		go m.runTaskLoop(m.ctx, task)
		m.log.Info("Enabled task: %s", task.Name)
	} else if !task.Enabled && task.loopRunning {
		// Loop is still running but task was disabled - just re-enable it
		task.Enabled = true
		m.log.Info("Re-enabled task: %s", task.Name)
	}

	return nil
}

// DisableTask disables a task and cancels any currently running execution.
func (m *Module) DisableTask(taskID string) error {
	m.mu.Lock()
	task, exists := m.tasks[taskID]
	if !exists {
		m.mu.Unlock()
		return fmt.Errorf(errTaskNotFoundFmt, taskID)
	}
	task.Enabled = false
	m.mu.Unlock()

	// Cancel the running execution so it stops promptly.
	task.stopMu.Lock()
	if task.stopRunning != nil {
		task.stopRunning()
	}
	task.stopMu.Unlock()

	m.log.Info("Disabled task: %s", task.Name)
	return nil
}

// StopTask cancels the currently running execution of a task without disabling it.
// The task will resume on its next scheduled interval.
func (m *Module) StopTask(taskID string) error {
	m.mu.RLock()
	task, exists := m.tasks[taskID]
	m.mu.RUnlock()

	if !exists {
		return fmt.Errorf(errTaskNotFoundFmt, taskID)
	}

	task.stopMu.Lock()
	cancel := task.stopRunning
	task.stopMu.Unlock()

	if cancel == nil {
		return fmt.Errorf("task %s is not currently running", taskID)
	}

	cancel()
	m.log.Info("Force-stopped task: %s", task.Name)
	return nil
}

// ListTasks returns all registered tasks
func (m *Module) ListTasks() []TaskInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()

	tasks := make([]TaskInfo, 0, len(m.tasks))
	for _, task := range m.tasks {
		var lastErr string
		if task.LastError != nil {
			lastErr = task.LastError.Error()
		}
		tasks = append(tasks, TaskInfo{
			ID:          task.ID,
			Name:        task.Name,
			Description: task.Description,
			Schedule:    task.Schedule.String(),
			LastRun:     task.LastRun,
			NextRun:     task.NextRun,
			Running:     task.Running,
			LastError:   lastErr,
			Enabled:     task.Enabled,
		})
	}

	return tasks
}

// TaskInfo holds task information for API responses
type TaskInfo struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Schedule    string    `json:"schedule"`
	LastRun     time.Time `json:"last_run"`
	NextRun     time.Time `json:"next_run"`
	Running     bool      `json:"running"`
	LastError   string    `json:"last_error,omitempty"`
	Enabled     bool      `json:"enabled"`
}

// GetTask returns information about a specific task
func (m *Module) GetTask(taskID string) (*TaskInfo, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	task, exists := m.tasks[taskID]
	if !exists {
		return nil, fmt.Errorf(errTaskNotFoundFmt, taskID)
	}

	var lastErr string
	if task.LastError != nil {
		lastErr = task.LastError.Error()
	}

	return &TaskInfo{
		ID:          task.ID,
		Name:        task.Name,
		Description: task.Description,
		Schedule:    task.Schedule.String(),
		LastRun:     task.LastRun,
		NextRun:     task.NextRun,
		Running:     task.Running,
		LastError:   lastErr,
		Enabled:     task.Enabled,
	}, nil
}

// UpdateSchedule changes a task's schedule and signals the running goroutine
// to recreate its ticker with the new interval.
func (m *Module) UpdateSchedule(taskID string, schedule time.Duration) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	task, exists := m.tasks[taskID]
	if !exists {
		return fmt.Errorf(errTaskNotFoundFmt, taskID)
	}

	task.Schedule = schedule
	task.NextRun = time.Now().Add(schedule)

	// Signal the running goroutine to recreate its ticker with the new schedule
	select {
	case task.reschedule <- schedule:
	default:
		// Channel full; goroutine will pick up the new schedule on next receive
	}

	m.log.Info("Updated schedule for task %s: %v", task.Name, schedule)

	return nil
}

// GetRunningCount returns the number of currently running tasks
func (m *Module) GetRunningCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()

	count := 0
	for _, task := range m.tasks {
		if task.Running {
			count++
		}
	}
	return count
}
