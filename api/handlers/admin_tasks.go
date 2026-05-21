package handlers

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"media-server-pro/internal/analytics"
	"media-server-pro/internal/config"
	"media-server-pro/internal/tasks"
)

// minTaskScheduleSecs is the lower bound for an admin-supplied task schedule.
// Anything faster than 60s is treated as a misconfiguration — tasks that
// genuinely need sub-minute cadence belong in their own ticker loop, not the
// general scheduler.
const minTaskScheduleSecs = 60

// persistTaskOverride merges an admin task tweak into Tasks.Overrides so the
// change survives a restart. mutate runs under the config lock; pass nil
// fields to leave them unchanged.
func persistTaskOverride(cfg *config.Manager, taskID string, mutate func(*config.TaskOverride)) error {
	return cfg.Update(func(c *config.Config) {
		if c.Tasks.Overrides == nil {
			c.Tasks.Overrides = make(map[string]config.TaskOverride)
		}
		entry := c.Tasks.Overrides[taskID]
		mutate(&entry)
		c.Tasks.Overrides[taskID] = entry
	})
}

const (
	msgTasksNotAvailable = "Tasks module not available"
)

// AdminListTasks returns scheduled tasks
func (h *Handler) AdminListTasks(c *gin.Context) {
	if h.tasks == nil {
		writeError(c, http.StatusServiceUnavailable, msgTasksNotAvailable)
		return
	}
	taskList := h.tasks.ListTasks()
	writeSuccess(c, taskList)
}

// AdminRunTask runs a task immediately
func (h *Handler) AdminRunTask(c *gin.Context) {
	if h.tasks == nil {
		writeError(c, http.StatusServiceUnavailable, msgTasksNotAvailable)
		return
	}
	taskID := strings.TrimSpace(c.Param("id"))
	if taskID == "" {
		writeError(c, http.StatusBadRequest, "Task ID is required")
		return
	}

	if err := h.tasks.RunNow(taskID); err != nil {
		if errors.Is(err, tasks.ErrTaskNotFound) {
			writeError(c, http.StatusNotFound, err.Error())
		} else {
			writeError(c, http.StatusServiceUnavailable, err.Error())
		}
		return
	}

	h.trackServerEvent(c, analytics.EventAdminTaskRun, map[string]any{"task_id": taskID})
	writeSuccess(c, map[string]string{"message": "Task started"})
}

// AdminEnableTask enables a background task
func (h *Handler) AdminEnableTask(c *gin.Context) {
	if h.tasks == nil {
		writeError(c, http.StatusServiceUnavailable, msgTasksNotAvailable)
		return
	}
	taskID := strings.TrimSpace(c.Param("id"))
	if taskID == "" {
		writeError(c, http.StatusBadRequest, "Task ID is required")
		return
	}

	if err := h.tasks.EnableTask(taskID); err != nil {
		if errors.Is(err, tasks.ErrTaskNotFound) {
			writeError(c, http.StatusNotFound, "Task not found")
		} else {
			writeError(c, http.StatusInternalServerError, err.Error())
		}
		return
	}
	if err := persistTaskOverride(h.config, taskID, func(o *config.TaskOverride) {
		enabled := true
		o.Enabled = &enabled
	}); err != nil {
		h.log.Warn("Failed to persist enabled override for task %s: %v", taskID, err)
	}

	h.trackServerEvent(c, analytics.EventAdminTaskEnable, map[string]any{"task_id": taskID})
	writeSuccess(c, map[string]string{"message": "Task enabled"})
}

// AdminDisableTask disables a background task
func (h *Handler) AdminDisableTask(c *gin.Context) {
	if h.tasks == nil {
		writeError(c, http.StatusServiceUnavailable, msgTasksNotAvailable)
		return
	}
	taskID := strings.TrimSpace(c.Param("id"))
	if taskID == "" {
		writeError(c, http.StatusBadRequest, "Task ID is required")
		return
	}

	if err := h.tasks.DisableTask(taskID); err != nil {
		if errors.Is(err, tasks.ErrTaskNotFound) {
			writeError(c, http.StatusNotFound, "Task not found")
		} else {
			writeError(c, http.StatusInternalServerError, err.Error())
		}
		return
	}
	if err := persistTaskOverride(h.config, taskID, func(o *config.TaskOverride) {
		disabled := false
		o.Enabled = &disabled
	}); err != nil {
		h.log.Warn("Failed to persist disabled override for task %s: %v", taskID, err)
	}

	h.trackServerEvent(c, analytics.EventAdminTaskDisable, map[string]any{"task_id": taskID})
	writeSuccess(c, map[string]string{"message": "Task disabled"})
}

// AdminUpdateTaskSchedule changes the schedule of a registered background task
// and persists the override so the new cadence survives a restart.
//
// POST /admin/tasks/:id/schedule
// Body: { "schedule_secs": 3600 }
func (h *Handler) AdminUpdateTaskSchedule(c *gin.Context) {
	if h.tasks == nil {
		writeError(c, http.StatusServiceUnavailable, msgTasksNotAvailable)
		return
	}
	taskID := strings.TrimSpace(c.Param("id"))
	if taskID == "" {
		writeError(c, http.StatusBadRequest, "Task ID is required")
		return
	}

	var req struct {
		ScheduleSecs int `json:"schedule_secs"`
	}
	if !BindJSON(c, &req, "") {
		return
	}
	if req.ScheduleSecs < minTaskScheduleSecs {
		writeError(c, http.StatusBadRequest,
			"schedule_secs must be at least 60 seconds")
		return
	}

	schedule := time.Duration(req.ScheduleSecs) * time.Second
	if err := h.tasks.UpdateSchedule(taskID, schedule); err != nil {
		if errors.Is(err, tasks.ErrTaskNotFound) {
			writeError(c, http.StatusNotFound, "Task not found")
		} else {
			writeError(c, http.StatusBadRequest, err.Error())
		}
		return
	}

	if err := persistTaskOverride(h.config, taskID, func(o *config.TaskOverride) {
		secs := req.ScheduleSecs
		o.ScheduleSecs = &secs
	}); err != nil {
		h.log.Warn("Failed to persist schedule override for task %s: %v", taskID, err)
	}

	h.trackServerEvent(c, analytics.EventAdminTaskSchedule, map[string]any{
		"task_id":       taskID,
		"schedule_secs": req.ScheduleSecs,
	})
	writeSuccess(c, map[string]any{
		"message":       "Task schedule updated",
		"schedule_secs": req.ScheduleSecs,
	})
}

// AdminStopTask force-cancels a running task without disabling future runs
func (h *Handler) AdminStopTask(c *gin.Context) {
	if h.tasks == nil {
		writeError(c, http.StatusServiceUnavailable, msgTasksNotAvailable)
		return
	}
	taskID := strings.TrimSpace(c.Param("id"))
	if taskID == "" {
		writeError(c, http.StatusBadRequest, "Task ID is required")
		return
	}

	if err := h.tasks.StopTask(taskID); err != nil {
		switch {
		case errors.Is(err, tasks.ErrTaskNotFound):
			writeError(c, http.StatusNotFound, err.Error())
		case errors.Is(err, tasks.ErrTaskNotRunning):
			writeError(c, http.StatusConflict, err.Error())
		default:
			writeError(c, http.StatusBadRequest, err.Error())
		}
		return
	}

	h.trackServerEvent(c, analytics.EventAdminTaskStop, map[string]any{"task_id": taskID})
	writeSuccess(c, map[string]string{"message": "Task stopped"})
}
