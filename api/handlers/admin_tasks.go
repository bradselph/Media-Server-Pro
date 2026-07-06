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
// general scheduler. Shared with the boot-time override clamp (cmd/server) so
// both paths enforce the same floor.
const minTaskScheduleSecs = tasks.MinScheduleSecs

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
	h.setTaskEnabled(c, true)
}

// AdminDisableTask disables a background task
func (h *Handler) AdminDisableTask(c *gin.Context) {
	h.setTaskEnabled(c, false)
}

// setTaskEnabled enables or disables a task and persists the override so the
// state survives a restart. Shared body of AdminEnableTask/AdminDisableTask.
func (h *Handler) setTaskEnabled(c *gin.Context, enabled bool) {
	if h.tasks == nil {
		writeError(c, http.StatusServiceUnavailable, msgTasksNotAvailable)
		return
	}
	taskID := strings.TrimSpace(c.Param("id"))
	if taskID == "" {
		writeError(c, http.StatusBadRequest, "Task ID is required")
		return
	}

	action := "enabled"
	event := analytics.EventAdminTaskEnable
	toggle := h.tasks.EnableTask
	if !enabled {
		action = "disabled"
		event = analytics.EventAdminTaskDisable
		toggle = h.tasks.DisableTask
	}
	if err := toggle(taskID); err != nil {
		if errors.Is(err, tasks.ErrTaskNotFound) {
			writeError(c, http.StatusNotFound, "Task not found")
		} else {
			writeError(c, http.StatusInternalServerError, err.Error())
		}
		return
	}
	if err := persistTaskOverride(h.config, taskID, func(o *config.TaskOverride) {
		o.Enabled = &enabled
	}); err != nil {
		h.log.Error("Failed to persist %s override for task %s: %v", action, taskID, err)
		writeError(c, http.StatusInternalServerError, "Task "+action+" but failed to persist configuration")
		return
	}

	h.trackServerEvent(c, event, map[string]any{"task_id": taskID})
	writeSuccess(c, map[string]string{"message": "Task " + action})
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
		o.ScheduleSecs = new(req.ScheduleSecs)
	}); err != nil {
		h.log.Error("Failed to persist schedule override for task %s: %v", taskID, err)
		writeError(c, http.StatusInternalServerError, "Task schedule updated but failed to persist configuration")
		return
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
