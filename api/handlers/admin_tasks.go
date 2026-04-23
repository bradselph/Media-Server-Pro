package handlers

import (
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"media-server-pro/internal/tasks"
)

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

	writeSuccess(c, map[string]string{"message": "Task disabled"})
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

	writeSuccess(c, map[string]string{"message": "Task stopped"})
}
