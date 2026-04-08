package handlers

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"media-server-pro/internal/admin"
	"media-server-pro/internal/config"
	"media-server-pro/internal/updater"
	"media-server-pro/pkg/models"
)

// CheckForUpdates checks GitHub for new versions
func (h *Handler) CheckForUpdates(c *gin.Context) {
	if !h.requireUpdater(c) {
		return
	}
	result, err := h.updater.CheckForUpdates()
	if err != nil {
		if result != nil {
			writeSuccess(c, result)
			return
		}
		h.log.Error("%v", err)
		writeError(c, http.StatusInternalServerError, "Internal server error")
		return
	}

	writeSuccess(c, result)
}

// GetUpdateStatus returns the last update check result.
func (h *Handler) GetUpdateStatus(c *gin.Context) {
	if !h.requireUpdater(c) {
		return
	}
	result := h.updater.GetLastCheck()
	if result == nil {
		version := h.updater.GetVersion()
		currentVersion, _ := version["version"].(string)
		writeSuccess(c, map[string]interface{}{
			"current_version":  currentVersion,
			"latest_version":   "",
			"update_available": false,
			"checked_at":       nil,
		})
		return
	}

	writeSuccess(c, result)
}

// ApplyUpdate downloads and installs an update
func (h *Handler) ApplyUpdate(c *gin.Context) {
	if !h.requireUpdater(c) {
		return
	}
	if h.updater.IsUpdateRunning() {
		writeError(c, http.StatusConflict, "A binary update is already in progress")
		return
	}

	status, err := h.updater.ApplyUpdate(c.Request.Context())
	if err != nil {
		h.log.Error("ApplyUpdate: %v", err)
		writeError(c, http.StatusInternalServerError, err.Error())
		return
	}

	h.logAdminActionResult(c, &adminLogResultParams{
		Action: "apply_update", Target: status.Stage, Success: status.Error == "",
	})
	writeSuccess(c, status)
}

// ApplySourceUpdate starts an async source build. The updater claims the build
// atomically inside SourceUpdate, so duplicate requests get "already in progress".
func (h *Handler) ApplySourceUpdate(c *gin.Context) {
	if !h.requireUpdater(c) {
		return
	}
	if h.updater.IsBuildRunning() {
		writeError(c, http.StatusConflict, "A source build is already in progress")
		return
	}
	clientIP := c.ClientIP()
	actorID, actorName := "admin", "admin"
	if session := getSession(c); session != nil {
		actorID = session.UserID
		actorName = session.Username
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
		defer cancel()
		status, err := h.updater.SourceUpdate(ctx)
		if err != nil {
			h.log.Error("Source update failed: %v", err)
			return
		}
		if h.admin != nil {
			h.admin.LogAction(context.Background(), &admin.AuditLogParams{
				UserID: actorID, Username: actorName, Action: "apply_source_update",
				Resource: status.Stage, Details: nil, IPAddress: clientIP, Success: status.Error == "",
			})
		}
	}()
	initial := h.updater.GetActiveBuildStatus()
	if initial == nil {
		initial = &updater.UpdateStatus{InProgress: true, Stage: "starting", Progress: 0, StartedAt: time.Now()}
	}
	c.JSON(http.StatusAccepted, models.APIResponse{Success: true, Data: initial})
}

// GetSourceUpdateProgress returns the live progress of a running source build.
func (h *Handler) GetSourceUpdateProgress(c *gin.Context) {
	if !h.requireUpdater(c) {
		return
	}
	status := h.updater.GetActiveBuildStatus()
	if status == nil {
		writeSuccess(c, map[string]interface{}{
			"in_progress": false,
			"stage":       "",
			"progress":    0,
		})
		return
	}
	writeSuccess(c, status)
}

// CheckForSourceUpdates fetches remote git refs and reports whether new commits are available.
func (h *Handler) CheckForSourceUpdates(c *gin.Context) {
	if !h.requireUpdater(c) {
		return
	}
	hasUpdates, remoteHash, err := h.updater.CheckForSourceUpdates(c.Request.Context())
	if err != nil {
		h.log.Error("Source update check failed: %v", err)
		writeError(c, http.StatusInternalServerError, "Internal server error")
		return
	}
	writeSuccess(c, map[string]interface{}{
		"updates_available": hasUpdates,
		"remote_commit":     remoteHash,
	})
}

// GetUpdateConfig returns the current updater configuration.
func (h *Handler) GetUpdateConfig(c *gin.Context) {
	cfg := h.config.Get()
	method := cfg.Updater.UpdateMethod
	if method == "" {
		method = "source"
	}
	branch := cfg.Updater.Branch
	if branch == "" {
		branch = "main"
	}
	writeSuccess(c, map[string]interface{}{
		"update_method": method,
		"branch":        branch,
	})
}

// SetUpdateConfig updates the updater configuration (method, branch).
func (h *Handler) SetUpdateConfig(c *gin.Context) {
	var req struct {
		UpdateMethod string `json:"update_method"`
		Branch       string `json:"branch"`
	}
	if !BindJSON(c, &req, "") {
		return
	}

	if req.UpdateMethod != "" && req.UpdateMethod != "source" && req.UpdateMethod != "binary" {
		writeError(c, http.StatusBadRequest, "update_method must be \"source\" or \"binary\"")
		return
	}

	// Validate branch name against git ref naming rules to prevent command injection
	if req.Branch != "" {
		for _, bad := range []string{"..", " ", "~", "^", ":", "\\", "*", "?", "[", "@{"} {
			if strings.Contains(req.Branch, bad) {
				writeError(c, http.StatusBadRequest, "Invalid branch name")
				return
			}
		}
	}

	if err := h.config.Update(func(cfg *config.Config) {
		if req.UpdateMethod != "" {
			cfg.Updater.UpdateMethod = req.UpdateMethod
		}
		if req.Branch != "" {
			cfg.Updater.Branch = req.Branch
		}
	}); err != nil {
		h.log.Error("Failed to update updater config: %v", err)
		writeError(c, http.StatusInternalServerError, "Failed to save configuration")
		return
	}

	h.logAdminAction(c, &adminLogActionParams{Action: "update_updater_config", Target: "updater_settings",
		Details: map[string]interface{}{"update_method": req.UpdateMethod, "branch": req.Branch}})

	cfg := h.config.Get()
	writeSuccess(c, map[string]interface{}{
		"update_method": cfg.Updater.UpdateMethod,
		"branch":        cfg.Updater.Branch,
	})
}
