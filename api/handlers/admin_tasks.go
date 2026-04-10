package handlers

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

// AdminListTasks returns scheduled tasks
func (h *Handler) AdminListTasks(c *gin.Context) {
	if h.tasks == nil {
		writeError(c, http.StatusServiceUnavailable, "Tasks module not available")
		return
	}
	taskList := h.tasks.ListTasks()
	writeSuccess(c, taskList)
}

// AdminRunTask runs a task immediately
func (h *Handler) AdminRunTask(c *gin.Context) {
	if h.tasks == nil {
		writeError(c, http.StatusServiceUnavailable, "Tasks module not available")
		return
	}
	taskID := c.Param("id")

	if err := h.tasks.RunNow(taskID); err != nil {
		if strings.Contains(err.Error(), "not found") {
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
		writeError(c, http.StatusServiceUnavailable, "Tasks module not available")
		return
	}
	taskID := c.Param("id")

	if err := h.tasks.EnableTask(taskID); err != nil {
		writeError(c, http.StatusNotFound, "Task not found")
		return
	}

	writeSuccess(c, map[string]string{"message": "Task enabled"})
}

// AdminDisableTask disables a background task
func (h *Handler) AdminDisableTask(c *gin.Context) {
	if h.tasks == nil {
		writeError(c, http.StatusServiceUnavailable, "Tasks module not available")
		return
	}
	taskID := c.Param("id")

	if err := h.tasks.DisableTask(taskID); err != nil {
		writeError(c, http.StatusNotFound, "Task not found")
		return
	}

	writeSuccess(c, map[string]string{"message": "Task disabled"})
}

// AdminStopTask force-cancels a running task without disabling future runs
func (h *Handler) AdminStopTask(c *gin.Context) {
	if h.tasks == nil {
		writeError(c, http.StatusServiceUnavailable, "Tasks module not available")
		return
	}
	taskID := c.Param("id")

	if err := h.tasks.StopTask(taskID); err != nil {
		switch {
		case strings.Contains(err.Error(), "not found"):
			writeError(c, http.StatusNotFound, err.Error())
		case strings.Contains(err.Error(), "not currently running"):
			writeError(c, http.StatusConflict, err.Error())
		default:
			writeError(c, http.StatusBadRequest, err.Error())
		}
		return
	}

	writeSuccess(c, map[string]string{"message": "Task stopped"})
}
