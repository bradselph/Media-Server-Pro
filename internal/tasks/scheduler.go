// Package tasks provides background task scheduling and execution.
// It handles periodic tasks like cleanup, maintenance, and auto-discovery.
package tasks

import (
	"context"
	"errors"
	"fmt"
	"hash/fnv"
	"sync"
	"time"

	"media-server-pro/internal/config"
	"media-server-pro/internal/logger"
	"media-server-pro/pkg/helpers"
	"media-server-pro/pkg/models"
)

// Sentinel errors for typed error checking by callers.
var (
	ErrTaskNotFound   = errors.New("task not found")
	ErrTaskNotRunning = errors.New("task not currently running")
)

// MinScheduleSecs is the lower bound (in seconds) for an admin-supplied task
// schedule. Anything faster than this is treated as a misconfiguration; tasks
// that genuinely need sub-minute cadence belong in their own ticker loop, not
// the general scheduler. Enforced both by the admin API and by the boot-time
// override path (a persisted/restored config must not bypass the floor).
const MinScheduleSecs = 60

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
	started      bool          // guarded by mu; admissions require started && !stopping
	stopping     bool          // guarded by mu; closes the Add-vs-Wait shutdown race
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
	// Use a background context for task goroutines so they aren't canceled
	// when the server's module-startup context completes.
	taskCtx, cancel := context.WithCancel(context.Background()) //nolint:gosec // G118: cancel stored in m.cancel, called by Stop()
	// Start all enabled tasks.
	// Use a write lock so we can set loopRunning=true atomically before
	// spawning goroutines.  This prevents EnableTask from racing Start and
	// launching a duplicate runTaskLoop for the same task.
	m.mu.Lock()
	if m.started || m.stopping {
		m.mu.Unlock()
		cancel()
		return fmt.Errorf("task scheduler already started or stopping")
	}
	m.startupDelay = defaultStartupDelay
	m.ctx = taskCtx
	m.cancel = cancel
	m.done = taskCtx.Done()
	m.started = true
	for _, task := range m.tasks {
		if task.Enabled {
			task.loopRunning = true
			m.wg.Add(1)
			go m.runTaskLoop(taskCtx, task)
		}
	}
	taskCount := len(m.tasks)
	m.mu.Unlock()

	m.healthMu.Lock()
	m.healthy = true
	m.healthMsg = fmt.Sprintf("Running (%d tasks, startup delay: %v)", taskCount, m.startupDelay)
	m.healthMu.Unlock()
	m.log.Info("Task scheduler started with %d tasks (first run in %v)", taskCount, m.startupDelay)
	return nil
}

// Stop gracefully stops all scheduled tasks
func (m *Module) Stop(ctx context.Context) error {
	m.log.Info("Stopping task scheduler...")

	// Close task admission under the same lock used by RegisterTask, RunNow,
	// and EnableTask. Every positive WaitGroup Add therefore happens either
	// before this transition or after a future Start, never concurrently with
	// the zero-counter Wait below.
	m.mu.Lock()
	if !m.started || m.cancel == nil {
		m.mu.Unlock()
		return nil
	}
	m.stopping = true
	cancel := m.cancel
	m.mu.Unlock()
	cancel()

	// Wait for all tasks to finish with timeout
	done := make(chan struct{})
	go func() {
		m.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		m.log.Info("All tasks stopped gracefully")
		m.mu.Lock()
		m.started = false
		m.stopping = false
		m.ctx = nil
		m.cancel = nil
		m.done = nil
		m.mu.Unlock()
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
	if opts.Func == nil {
		m.log.Error("cannot register task %s: Func is nil", opts.ID)
		return
	}
	if opts.Schedule <= 0 {
		m.log.Error("cannot register task %s: schedule must be positive, got %v", opts.ID, opts.Schedule)
		return
	}

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

	if m.started && !m.stopping && m.ctx != nil && task.Enabled && !task.loopRunning {
		task.loopRunning = true
		m.wg.Add(1)
		go m.runTaskLoop(m.ctx, task)
	}
}

// taskJitter returns a deterministic per-task startup offset derived from the task
// ID, bounded to a tenth of the task's own schedule (capped at 5m). Adding it to the
// shared startup delay decorrelates tasks that share the same interval so they don't
// fire in permanent lock-step — spreading the recurring CPU/disk spike of many tasks
// firing at the same instant over time. It is deterministic (a hash of the ID, not
// random) so NextRun estimates stay stable and tests remain reproducible, and it only
// offsets the initial delay, never the ticker period (which would change how often a
// task runs).
func taskJitter(task *Task) time.Duration {
	bound := task.Schedule / 10
	if bound > 5*time.Minute {
		bound = 5 * time.Minute
	}
	if bound <= 0 {
		return 0
	}
	h := fnv.New64a()
	_, _ = h.Write([]byte(task.ID))
	return time.Duration(h.Sum64() % uint64(bound))
}

// waitForStartupDelay waits for the configured startup delay unless ctx is canceled.
// Returns false if context was canceled during the wait, true otherwise.
func (m *Module) waitForStartupDelay(ctx context.Context, task *Task) bool {
	m.mu.RLock()
	delay := m.startupDelay
	// Only stagger when a startup delay is configured; a zero delay means "run
	// immediately" (used by tests) and must stay immediate.
	if delay > 0 {
		delay += taskJitter(task)
	}
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
	return m.executeTask(ctx, task, true)
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

	m.mu.RLock()
	initialSchedule := task.Schedule
	m.mu.RUnlock()
	ticker := time.NewTicker(initialSchedule)
	defer ticker.Stop()

	// The enabled check and execution claim are atomic, so DisableTask cannot
	// slip through the gap and miss canceling a just-started run.
	if !m.executeTask(ctx, task, true) {
		m.log.Debug("Task %s: skipping initial run (disabled during startup delay)", task.Name)
	} else {
		m.log.Debug("Task %s: initial run", task.Name)
	}

	for {
		select {
		case <-ctx.Done():
			return
		case <-task.reschedule:
			ticker.Stop()
			select {
			case <-ticker.C:
			default:
			}
			// Read the authoritative schedule rather than the value carried by the
			// channel: UpdateSchedule always writes task.Schedule under m.mu, but its
			// buffered (size-1) send is dropped when reschedules arrive in quick
			// succession, which would otherwise leave the ticker on a stale interval.
			m.mu.RLock()
			sched := task.Schedule
			m.mu.RUnlock()
			ticker = time.NewTicker(sched)
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

// executeTask runs a single task execution with panic recovery. When
// requireEnabled is true, the enabled check and installation of stopRunning
// happen under the same scheduler lock, closing the DisableTask race window.
// It returns false only when a scheduled task is disabled.
func (m *Module) executeTask(ctx context.Context, task *Task, requireEnabled bool) bool {
	m.mu.Lock()
	if requireEnabled && !task.Enabled {
		m.mu.Unlock()
		return false
	}
	if task.Running {
		m.mu.Unlock()
		m.log.Debug("Task %s already running, skipping", task.Name)
		return true
	}
	timeout := computeTaskTimeout(task)
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	task.stopMu.Lock()
	task.stopRunning = cancel
	task.stopMu.Unlock()
	task.Running = true
	m.mu.Unlock()

	defer func() {
		cancel()
		task.stopMu.Lock()
		task.stopRunning = nil
		task.stopMu.Unlock()
		m.mu.Lock()
		task.Running = false
		task.LastRun = time.Now()
		task.NextRun = time.Now().Add(task.Schedule)
		m.mu.Unlock()
	}()

	m.log.Debug("Executing task: %s", task.Name)
	start := time.Now()

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
		err = task.Func(runCtx)
	}()
	m.recordTaskResult(task, err, start)
	return true
}

// RunNow triggers immediate execution of a task. The goroutine is tracked by m.wg
// so Stop() waits for it to complete.
func (m *Module) RunNow(taskID string) error {
	m.mu.Lock()
	task, exists := m.tasks[taskID]
	ctx := m.ctx
	if !exists {
		m.mu.Unlock()
		return fmt.Errorf("%w: %s", ErrTaskNotFound, taskID)
	}
	if !m.started || ctx == nil {
		m.mu.Unlock()
		return fmt.Errorf("task scheduler not started")
	}
	if m.stopping || ctx.Err() != nil {
		m.mu.Unlock()
		return fmt.Errorf("task scheduler is stopping")
	}
	// Add while admission is still protected by m.mu; Stop takes the same lock
	// before beginning Wait, which is the ordering sync.WaitGroup requires.
	m.wg.Add(1)
	m.mu.Unlock()
	go func() {
		defer m.wg.Done()
		// Re-check ctx after the goroutine starts — the scheduler may have been
		// stopped between the RLock release above and this goroutine being scheduled.
		// Tasks that don't honor context cancellation would otherwise run to
		// completion after shutdown.
		if ctx.Err() != nil {
			return
		}
		m.executeTask(ctx, task, false)
	}()
	return nil
}

// EnableTask enables a task
func (m *Module) EnableTask(taskID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	task, exists := m.tasks[taskID]
	if !exists {
		return fmt.Errorf("%w: %s", ErrTaskNotFound, taskID)
	}

	// Only start a new goroutine if task is not enabled and no loop is running
	if !task.Enabled && !task.loopRunning {
		task.Enabled = true
		if m.started && !m.stopping && m.ctx != nil && m.ctx.Err() == nil {
			task.loopRunning = true
			m.wg.Add(1)
			go m.runTaskLoop(m.ctx, task)
		}
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
		return fmt.Errorf("%w: %s", ErrTaskNotFound, taskID)
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
		return fmt.Errorf("%w: %s", ErrTaskNotFound, taskID)
	}

	task.stopMu.Lock()
	cancel := task.stopRunning
	task.stopMu.Unlock()

	if cancel == nil {
		return fmt.Errorf("%w: %s", ErrTaskNotRunning, taskID)
	}

	cancel()
	m.log.Info("Force-stopped task: %s", task.Name)
	return nil
}

// taskInfo builds the API view of a scheduled task. Callers must hold m.mu.
func taskInfo(task *Task) TaskInfo {
	var lastErr string
	if task.LastError != nil {
		lastErr = task.LastError.Error()
	}
	return TaskInfo{
		ID:          task.ID,
		Name:        task.Name,
		Description: task.Description,
		Schedule:    task.Schedule.String(),
		LastRun:     task.LastRun,
		NextRun:     task.NextRun,
		Running:     task.Running,
		LastError:   lastErr,
		Enabled:     task.Enabled,
	}
}

// ListTasks returns all registered tasks
func (m *Module) ListTasks() []TaskInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()

	tasks := make([]TaskInfo, 0, len(m.tasks))
	for _, task := range m.tasks {
		tasks = append(tasks, taskInfo(task))
	}

	return tasks
}

// TaskInfo holds task information for API responses
type TaskInfo struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Schedule    string `json:"schedule"`
	// omitzero: a never-run task has zero-value times, which would otherwise
	// serialize as the truthy string "0001-01-01T00:00:00Z" and defeat the
	// SPA's `value ? format(value) : '—'` guards.
	LastRun   time.Time `json:"last_run,omitzero"`
	NextRun   time.Time `json:"next_run,omitzero"`
	Running   bool      `json:"running"`
	LastError string    `json:"last_error,omitempty"`
	Enabled   bool      `json:"enabled"`
}

// GetTask returns information about a specific task
func (m *Module) GetTask(taskID string) (*TaskInfo, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	task, exists := m.tasks[taskID]
	if !exists {
		return nil, fmt.Errorf("%w: %s", ErrTaskNotFound, taskID)
	}

	return new(taskInfo(task)), nil
}

// UpdateSchedule changes a task's schedule and signals the running goroutine
// to recreate its ticker with the new interval.
func (m *Module) UpdateSchedule(taskID string, schedule time.Duration) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	task, exists := m.tasks[taskID]
	if !exists {
		return fmt.Errorf("%w: %s", ErrTaskNotFound, taskID)
	}
	if schedule <= 0 {
		return fmt.Errorf("schedule must be positive, got %v", schedule)
	}

	task.Schedule = schedule
	task.NextRun = time.Now().Add(schedule)

	// Signal the running goroutine to recreate its ticker with the new schedule
	select {
	case task.reschedule <- schedule:
	default:
		// Channel full or goroutine has exited; schedule is persisted in task.Schedule
		// so any future goroutine restart will use the updated value.
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
