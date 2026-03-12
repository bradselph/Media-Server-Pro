package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// AdminListTasks returns scheduled tasks
func (h *Handler) AdminListTasks(c *gin.Context) {
	taskList := h.tasks.ListTasks()
	writeSuccess(c, taskList)
}

// AdminRunTask runs a task immediately
func (h *Handler) AdminRunTask(c *gin.Context) {
	taskID := c.Param("id")

	if err := h.tasks.RunNow(taskID); err != nil {
		writeError(c, http.StatusNotFound, "Task not found")
		return
	}

	writeSuccess(c, map[string]string{"message": "Task started"})
}

// AdminEnableTask enables a background task
func (h *Handler) AdminEnableTask(c *gin.Context) {
	taskID := c.Param("id")

	if err := h.tasks.EnableTask(taskID); err != nil {
		writeError(c, http.StatusNotFound, "Task not found")
		return
	}

	writeSuccess(c, map[string]string{"message": "Task enabled"})
}

// AdminDisableTask disables a background task
func (h *Handler) AdminDisableTask(c *gin.Context) {
	taskID := c.Param("id")

	if err := h.tasks.DisableTask(taskID); err != nil {
		writeError(c, http.StatusNotFound, "Task not found")
		return
	}

	writeSuccess(c, map[string]string{"message": "Task disabled"})
}

// AdminStopTask force-cancels a running task without disabling future runs
func (h *Handler) AdminStopTask(c *gin.Context) {
	taskID := c.Param("id")

	if err := h.tasks.StopTask(taskID); err != nil {
		writeError(c, http.StatusBadRequest, "Cannot stop task")
		return
	}

	writeSuccess(c, map[string]string{"message": "Task stopped"})
}
