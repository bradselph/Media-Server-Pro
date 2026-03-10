package handlers

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"media-server-pro/internal/admin"
	"media-server-pro/internal/auth"
	"media-server-pro/internal/config"
	"media-server-pro/internal/updater"
	"media-server-pro/internal/upload"
	"media-server-pro/pkg/helpers"
	"media-server-pro/pkg/models"
)

// AdminGetStats returns admin statistics.
func (h *Handler) AdminGetStats(c *gin.Context) {
	if !h.requireAdmin(c) {
		return
	}
	adminStats := h.admin.GetServerStats()
	mediaStats := h.media.GetStats()
	streamStats := h.streaming.GetStats()
	var hlsRunning, hlsCompleted int
	if h.hls != nil {
		hlsStats := h.hls.GetStats()
		hlsRunning = hlsStats.RunningJobs
		hlsCompleted = hlsStats.CompletedJobs
	}

	totalUsers := len(h.auth.ListUsers(c.Request.Context()))

	var totalViews int
	if h.analytics != nil {
		totalViews = h.analytics.GetStats().TotalViews
	}

	var diskTotal, diskFree uint64
	cfg := h.media.GetConfig()
	if cfg.Directories.Videos != "" {
		if du, err := helpers.GetDiskUsage(cfg.Directories.Videos); err == nil {
			diskTotal = du.Total
			diskFree = du.Available
		}
	}
	var diskUsed uint64
	if diskTotal > diskFree {
		diskUsed = diskTotal - diskFree
	}

	writeSuccess(c, map[string]interface{}{
		"total_videos":       mediaStats.VideoCount,
		"total_audio":        mediaStats.AudioCount,
		"active_sessions":    streamStats.ActiveStreams,
		"total_users":        totalUsers,
		"disk_usage":         diskUsed,
		"disk_total":         diskTotal,
		"disk_free":          diskFree,
		"hls_jobs_running":   hlsRunning,
		"hls_jobs_completed": hlsCompleted,
		"server_uptime":      int64(adminStats.Uptime.Seconds()),
		"total_views":        totalViews,
	})
}

// AdminGetSystemInfo returns system information shaped for the frontend SystemInfo type.
func (h *Handler) AdminGetSystemInfo(c *gin.Context) {
	if !h.requireAdmin(c) {
		return
	}
	info := h.admin.GetSystemInfo()
	uptimeSecs := h.admin.GetUptimeSecs()

	type healthier interface {
		Health() models.HealthStatus
	}
	// Build module list, skipping any that are nil (optional modules that failed to init).
	// NOTE: A typed nil pointer (e.g. (*analytics.Module)(nil)) stored in an interface
	// is NOT nil at the interface level — we must use reflect to detect it.
	allModules := []healthier{
		h.security, h.database, h.auth, h.media, h.streaming, h.hls,
		h.analytics, h.playlist, h.admin, h.tasks, h.upload, h.scanner,
		h.thumbnails, h.validator, h.backup, h.autodiscovery, h.suggestions,
		h.categorizer, h.updater, h.remote, h.receiver,
		h.extractor, h.crawler, h.duplicates,
	}
	type moduleHealthItem struct {
		Name      string `json:"name"`
		Status    string `json:"status"`
		Message   string `json:"message,omitempty"`
		LastCheck string `json:"last_check,omitempty"`
	}
	moduleHealths := make([]moduleHealthItem, 0, len(allModules))
	for _, p := range allModules {
		if p == nil || reflect.ValueOf(p).IsNil() {
			continue
		}
		hs := p.Health()
		moduleHealths = append(moduleHealths, moduleHealthItem{
			Name:      hs.Name,
			Status:    hs.Status,
			Message:   hs.Message,
			LastCheck: hs.CheckedAt.Format(time.RFC3339),
		})
	}

	writeSuccess(c, map[string]interface{}{
		"version":      h.version,
		"build_date":   h.buildDate,
		"os":           runtime.GOOS,
		"arch":         runtime.GOARCH,
		"go_version":   info.GoVersion,
		"cpu_count":    info.NumCPU,
		"memory_used":  info.MemAlloc,
		"memory_total": info.MemTotal,
		"uptime":       uptimeSecs,
		"modules":      moduleHealths,
	})
}

// AdminListUsers returns all users
func (h *Handler) AdminListUsers(c *gin.Context) {
	users := h.auth.ListUsers(c.Request.Context())
	writeSuccess(c, users)
}

// AdminCreateUser creates a user
func (h *Handler) AdminCreateUser(c *gin.Context) {
	var req struct {
		Username string          `json:"username"`
		Password string          `json:"password"`
		Email    string          `json:"email"`
		Type     string          `json:"type"`
		Role     models.UserRole `json:"role"`
	}
	if c.ShouldBindJSON(&req) != nil {
		writeError(c, http.StatusBadRequest, errInvalidRequest)
		return
	}

	req.Username = strings.TrimSpace(req.Username)
	if len(req.Username) < 3 || len(req.Username) > 64 {
		writeError(c, http.StatusBadRequest, "Username must be between 3 and 64 characters")
		return
	}
	for _, ch := range req.Username {
		if (ch < 'a' || ch > 'z') && (ch < 'A' || ch > 'Z') && (ch < '0' || ch > '9') && ch != '_' && ch != '-' {
			writeError(c, http.StatusBadRequest, "Username may only contain letters, numbers, underscores, and hyphens")
			return
		}
	}
	if len(req.Password) < 8 {
		writeError(c, http.StatusBadRequest, "Password must be at least 8 characters")
		return
	}

	if req.Type == "" {
		req.Type = "standard"
	}
	user, err := h.auth.CreateUser(c.Request.Context(), auth.CreateUserParams{
		Username: req.Username,
		Password: req.Password,
		Email:    req.Email,
		UserType: req.Type,
		Role:     req.Role,
	})
	if err != nil {
		if errors.Is(err, auth.ErrUserExists) {
			writeError(c, http.StatusConflict, "Username is already taken")
			return
		}
		h.log.Error("Failed to create user %s: %v", req.Username, err)
		writeError(c, http.StatusInternalServerError, "Internal server error")
		return
	}

	h.logAdminAction(c, "admin", "admin", "create_user", req.Username, nil)

	writeSuccess(c, user)
}

// AdminGetUser returns a single user's details
func (h *Handler) AdminGetUser(c *gin.Context) {
	username := c.Param("username")

	user, err := h.auth.GetUser(c.Request.Context(), username)
	if err != nil {
		writeError(c, http.StatusNotFound, errUserNotFound)
		return
	}

	writeSuccess(c, user)
}

// AdminUpdateUser updates a user's details
func (h *Handler) AdminUpdateUser(c *gin.Context) {
	username := c.Param("username")

	var req struct {
		Role        string                 `json:"role"`
		Enabled     *bool                  `json:"enabled"`
		Email       string                 `json:"email"`
		Permissions map[string]interface{} `json:"permissions"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		writeError(c, http.StatusBadRequest, errInvalidRequest)
		return
	}
	updates := map[string]interface{}{}
	if req.Role != "" {
		updates["role"] = req.Role
	}
	if req.Enabled != nil {
		updates["enabled"] = *req.Enabled
	}
	if req.Email != "" {
		updates["email"] = req.Email
	}
	if req.Permissions != nil {
		updates["permissions"] = req.Permissions
	}

	if err := h.auth.UpdateUser(c.Request.Context(), username, updates); err != nil {
		h.log.Error("%v", err)
		writeError(c, http.StatusInternalServerError, "Internal server error")
		return
	}

	h.logAdminAction(c, "admin", "admin", "update_user", username, updates)

	user, err := h.auth.GetUser(c.Request.Context(), username)
	if err != nil {
		h.log.Error("Failed to fetch updated user %s: %v", username, err)
		writeSuccess(c, map[string]string{"message": "User updated"})
		return
	}
	writeSuccess(c, user)
}

// AdminDeleteUser deletes a user
func (h *Handler) AdminDeleteUser(c *gin.Context) {
	username := c.Param("username")

	// Prevent admin from deleting their own account
	if sess := getSession(c); sess != nil && sess.Username == username {
		writeError(c, http.StatusForbidden, "Cannot delete your own account")
		return
	}

	if err := h.auth.DeleteUser(c.Request.Context(), username); err != nil {
		writeError(c, http.StatusNotFound, "User not found")
		return
	}

	h.logAdminAction(c, "admin", "admin", "delete_user", username, nil)
	writeSuccess(c, nil)
}

// AdminChangePassword changes a user's password (admin action)
func (h *Handler) AdminChangePassword(c *gin.Context) {
	username := c.Param("username")

	var req struct {
		NewPassword string `json:"new_password"`
	}
	if c.ShouldBindJSON(&req) != nil {
		writeError(c, http.StatusBadRequest, errInvalidRequest)
		return
	}

	if req.NewPassword == "" {
		writeError(c, http.StatusBadRequest, "New password required")
		return
	}

	if len(req.NewPassword) < 8 {
		writeError(c, http.StatusBadRequest, "New password must be at least 8 characters")
		return
	}

	if err := h.auth.SetPassword(c.Request.Context(), username, req.NewPassword); err != nil {
		h.log.Error("%v", err)
		writeError(c, http.StatusInternalServerError, "Internal server error")
		return
	}

	h.logAdminAction(c, "admin", "admin", "change_password", username, nil)
	writeSuccess(c, map[string]string{"status": "password_changed"})
}

// AdminChangeOwnPassword lets an admin change the admin account password directly
func (h *Handler) AdminChangeOwnPassword(c *gin.Context) {
	var req struct {
		CurrentPassword string `json:"current_password"`
		NewPassword     string `json:"new_password"`
	}
	if c.ShouldBindJSON(&req) != nil {
		writeError(c, http.StatusBadRequest, errInvalidRequest)
		return
	}

	if req.CurrentPassword == "" || req.NewPassword == "" {
		writeError(c, http.StatusBadRequest, "Current and new password required")
		return
	}

	if len(req.NewPassword) < 8 {
		writeError(c, http.StatusBadRequest, "New password must be at least 8 characters")
		return
	}

	if err := h.auth.ChangeAdminPassword(c.Request.Context(), req.CurrentPassword, req.NewPassword); err != nil {
		writeError(c, http.StatusUnauthorized, "Current password is incorrect")
		return
	}

	h.logAdminAction(c, "admin", "admin", "change_admin_password", "", nil)
	writeSuccess(c, map[string]string{"status": "password_changed"})
}

// AdminBulkUsers performs a bulk action (delete, enable, disable) on multiple users.
func (h *Handler) AdminBulkUsers(c *gin.Context) {
	var req struct {
		Usernames []string `json:"usernames"`
		Action    string   `json:"action"`
	}
	if c.ShouldBindJSON(&req) != nil {
		writeError(c, http.StatusBadRequest, errInvalidRequest)
		return
	}
	if len(req.Usernames) == 0 {
		writeError(c, http.StatusBadRequest, "usernames must not be empty")
		return
	}
	if len(req.Usernames) > 200 {
		writeError(c, http.StatusBadRequest, "too many usernames (max 200)")
		return
	}
	if req.Action != "delete" && req.Action != "enable" && req.Action != "disable" {
		writeError(c, http.StatusBadRequest, `action must be "delete", "enable", or "disable"`)
		return
	}

	var successCount, failedCount int
	errs := make([]string, 0)

	for _, username := range req.Usernames {
		if username == "" || username == "admin" {
			continue
		}
		var opErr error
		switch req.Action {
		case "delete":
			opErr = h.auth.DeleteUser(c.Request.Context(), username)
			if opErr == nil {
				h.logAdminAction(c, "admin", "admin", "bulk_delete_user", username, nil)
			}
		case "enable":
			opErr = h.auth.UpdateUser(c.Request.Context(), username, map[string]interface{}{"enabled": true})
			if opErr == nil {
				h.logAdminAction(c, "admin", "admin", "bulk_enable_user", username, nil)
			}
		case "disable":
			opErr = h.auth.UpdateUser(c.Request.Context(), username, map[string]interface{}{"enabled": false})
			if opErr == nil {
				h.logAdminAction(c, "admin", "admin", "bulk_disable_user", username, nil)
			}
		}
		if opErr != nil {
			h.log.Error("bulk %s user %s: %v", req.Action, username, opErr)
			failedCount++
			errs = append(errs, fmt.Sprintf("%s: %v", username, opErr))
		} else {
			successCount++
		}
	}

	writeSuccess(c, map[string]interface{}{
		"success": successCount,
		"failed":  failedCount,
		"errors":  errs,
	})
}

// AdminGetAuditLog returns audit log, optionally filtered by user_id query param.
func (h *Handler) AdminGetAuditLog(c *gin.Context) {
	if !h.requireAdmin(c) {
		return
	}
	limit := 100
	offset := 0
	if l, err := strconv.Atoi(c.Query("limit")); err == nil && l > 0 && l <= 1000 {
		limit = l
	}
	if o, err := strconv.Atoi(c.Query("offset")); err == nil && o >= 0 {
		offset = o
	}
	userID := strings.TrimSpace(c.Query("user_id"))

	log := h.admin.GetAuditLog(c.Request.Context(), limit, offset, userID)
	writeSuccess(c, log)
}

// AdminExportAuditLog exports the audit log as a CSV file download
func (h *Handler) AdminExportAuditLog(c *gin.Context) {
	if !h.requireAdmin(c) {
		return
	}
	filename, err := h.admin.ExportAuditLog(c.Request.Context())
	if err != nil {
		h.log.Error("%v", err)
		writeError(c, http.StatusInternalServerError, "Internal server error")
		return
	}

	c.Header(headerContentDisposition, fmt.Sprintf("attachment; filename=%q", filepath.Base(filename)))
	c.Header(headerContentType, "text/csv")
	http.ServeFile(c.Writer, c.Request, filename)
}

// GetServerLogs reads recent entries from the server log files.
func (h *Handler) GetServerLogs(c *gin.Context) {
	limit := 200
	if l, err := strconv.Atoi(c.Query("limit")); err == nil && l > 0 && l <= 2000 {
		limit = l
	}

	cfg := h.media.GetConfig()
	logsDir := cfg.Directories.Logs
	if logsDir == "" {
		logsDir = "logs"
	}

	entries, err := os.ReadDir(logsDir)
	if err != nil {
		writeSuccess(c, []interface{}{})
		return
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Name() > entries[j].Name()
	})

	var logLines []map[string]interface{}

	const maxLogFiles = 50
	filesProcessed := 0
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".log") {
			continue
		}

		filePath := filepath.Join(logsDir, entry.Name())
		lines, readErr := readLastNLines(filePath, limit-len(logLines))
		if readErr != nil {
			h.log.Debug("Failed to read log file %s: %v", filePath, readErr)
			continue
		}

		for _, line := range lines {
			logEntry := parseLogLine(line)
			logLines = append(logLines, logEntry)
		}

		filesProcessed++
		if len(logLines) >= limit || filesProcessed >= maxLogFiles {
			break
		}
	}

	for i, j := 0, len(logLines)-1; i < j; i, j = i+1, j-1 {
		logLines[i], logLines[j] = logLines[j], logLines[i]
	}

	levelFilter := strings.ToLower(c.Query("level"))
	moduleFilter := strings.ToLower(c.Query("module"))
	if levelFilter != "" || moduleFilter != "" {
		filtered := logLines[:0]
		for _, entry := range logLines {
			if levelFilter != "" {
				entryLevel, _ := entry["level"].(string)
				if strings.ToLower(entryLevel) != levelFilter {
					continue
				}
			}
			if moduleFilter != "" {
				entryModule, _ := entry["module"].(string)
				if !strings.Contains(strings.ToLower(entryModule), moduleFilter) {
					continue
				}
			}
			filtered = append(filtered, entry)
		}
		logLines = filtered
	}

	writeSuccess(c, logLines)
}

// readLastNLines reads the last N lines from a file
func readLastNLines(filePath string, n int) ([]string, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = file.Close()
	}()

	var lines []string
	sc := bufio.NewScanner(file)
	for sc.Scan() {
		lines = append(lines, sc.Text())
	}

	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return lines, sc.Err()
}

// parseLogLine parses a server log line into a structured entry
func parseLogLine(line string) map[string]interface{} {
	entry := map[string]interface{}{
		"raw":       line,
		"timestamp": "",
		"level":     "info",
		"module":    "",
		"message":   line,
	}

	if len(line) > 25 && line[0] == '[' {
		if idx := strings.Index(line[1:], "]"); idx > 0 {
			entry["timestamp"] = line[1 : idx+1]
			rest := strings.TrimSpace(line[idx+2:])

			if len(rest) > 0 && rest[0] == '[' {
				if idx2 := strings.Index(rest[1:], "]"); idx2 > 0 {
					level := strings.TrimSpace(rest[1 : idx2+1])
					entry["level"] = strings.ToLower(level)
					rest = strings.TrimSpace(rest[idx2+2:])
				}
			}

			if len(rest) > 0 && rest[0] == '[' {
				if idx3 := strings.Index(rest[1:], "]"); idx3 > 0 {
					entry["module"] = rest[1 : idx3+1]
					rest = strings.TrimSpace(rest[idx3+2:])
				}
			}

			if len(rest) > 0 && rest[0] == '[' {
				if idx4 := strings.Index(rest[1:], "]"); idx4 > 0 {
					rest = strings.TrimSpace(rest[idx4+2:])
				}
			}

			entry["message"] = rest
		}
	}

	return entry
}

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

// AdminGetConfig returns the current configuration
func (h *Handler) AdminGetConfig(c *gin.Context) {
	if !h.requireAdmin(c) {
		return
	}
	cfg := h.admin.GetConfigMap()
	writeSuccess(c, cfg)
}

// AdminUpdateConfig updates the configuration
func (h *Handler) AdminUpdateConfig(c *gin.Context) {
	if !h.requireAdmin(c) {
		return
	}
	var updates map[string]interface{}
	if json.NewDecoder(c.Request.Body).Decode(&updates) != nil {
		writeError(c, http.StatusBadRequest, errInvalidRequest)
		return
	}

	if err := h.admin.UpdateConfig(updates); err != nil {
		h.log.Error("%v", err)
		writeError(c, http.StatusInternalServerError, "Internal server error")
		return
	}

	// Apply runtime config changes to in-memory modules
	if h.security != nil {
		updatedCfg := h.media.GetConfig()
		h.security.SetWhitelistEnabled(updatedCfg.Security.EnableIPWhitelist)
		h.security.SetBlacklistEnabled(updatedCfg.Security.EnableIPBlacklist)
	}

	h.logAdminAction(c, "admin", "admin", "update_config", "configuration", updates)
	writeSuccess(c, h.admin.GetConfigMap())
}

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

	h.logAdminActionResult(c, "admin", "admin", "apply_update", status.Stage, nil, status.Error == "")
	writeSuccess(c, status)
}

// ApplySourceUpdate starts an async source build.
func (h *Handler) ApplySourceUpdate(c *gin.Context) {
	if !h.requireUpdater(c) {
		return
	}
	if h.updater.IsBuildRunning() {
		writeError(c, http.StatusConflict, "A source build is already in progress")
		return
	}
	clientIP := c.ClientIP()
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
				UserID: "admin", Username: "admin", Action: "apply_source_update",
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
	if err := c.ShouldBindJSON(&req); err != nil {
		writeError(c, http.StatusBadRequest, errInvalidRequest)
		return
	}

	if req.UpdateMethod != "" && req.UpdateMethod != "source" && req.UpdateMethod != "binary" {
		writeError(c, http.StatusBadRequest, "update_method must be \"source\" or \"binary\"")
		return
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

	h.logAdminAction(c, "admin", "admin", "update_updater_config", "updater_settings",
		map[string]interface{}{"update_method": req.UpdateMethod, "branch": req.Branch})

	cfg := h.config.Get()
	writeSuccess(c, map[string]interface{}{
		"update_method": cfg.Updater.UpdateMethod,
		"branch":        cfg.Updater.Branch,
	})
}

// RestartServer initiates a graceful server restart via self-exec.
func (h *Handler) RestartServer(c *gin.Context) {
	h.log.Warn("Server restart requested by admin")
	h.logAdminAction(c, "admin", "admin", "restart_server", "initiated", nil)

	writeSuccess(c, map[string]interface{}{
		"message": "Server restart initiated. The server will restart in a few seconds.",
		"status":  "restarting",
	})

	go func() {
		time.Sleep(1 * time.Second)

		if os.Getenv("INVOCATION_ID") != "" {
			// Under systemd: exit with code 1 so Restart=on-failure triggers a restart.
			// os.Exit(0) is a clean exit that systemd does NOT restart.
			h.log.Info("Running under systemd — exiting with code 1 for service manager restart")
			os.Exit(1)
			return
		}

		h.log.Info("Initiating server restart via self-exec...")

		exe, err := os.Executable()
		if err != nil {
			h.log.Error("Failed to resolve executable path for restart: %v — falling back to exit", err)
			os.Exit(0)
			return
		}

		exe, err = filepath.EvalSymlinks(exe)
		if err != nil {
			h.log.Error("Failed to evaluate symlinks for restart: %v — falling back to exit", err)
			os.Exit(0)
			return
		}

		cmd := exec.Command(exe, os.Args[1:]...)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		setCmdRestartAttrs(cmd) // detach child from parent session (platform-specific)

		if err := cmd.Start(); err != nil {
			h.log.Error("Failed to start replacement process: %v — falling back to exit", err)
			os.Exit(1)
			return
		}

		h.log.Info("Replacement process started (PID %d), exiting current instance", cmd.Process.Pid)
		os.Exit(0)
	}()
}

// ShutdownServer initiates a graceful server shutdown
func (h *Handler) ShutdownServer(c *gin.Context) {
	h.log.Warn("Server shutdown requested by admin")
	h.logAdminAction(c, "admin", "admin", "shutdown_server", "initiated", nil)

	writeSuccess(c, map[string]interface{}{
		"message": "Server shutdown initiated. The server will shut down in a few seconds.",
		"status":  "shutting_down",
	})

	go func() {
		time.Sleep(1 * time.Second)
		h.log.Info("Initiating server shutdown...")
		os.Exit(0)
	}()
}

// AdminGetActiveStreams returns the list of active streaming sessions.
func (h *Handler) AdminGetActiveStreams(c *gin.Context) {
	sessions := h.streaming.GetActiveSessions()
	if sessions == nil {
		sessions = []*models.StreamSession{}
	}
	writeSuccess(c, sessions)
}

// AdminGetActiveUploads returns the list of in-progress uploads.
func (h *Handler) AdminGetActiveUploads(c *gin.Context) {
	if h.upload == nil {
		writeSuccess(c, []*upload.Progress{})
		return
	}
	uploads := h.upload.GetActiveUploads()
	if uploads == nil {
		uploads = []*upload.Progress{}
	}
	writeSuccess(c, uploads)
}

// AdminGetUserSessions returns active sessions for a specific user.
func (h *Handler) AdminGetUserSessions(c *gin.Context) {
	username := c.Param("username")
	sessions := h.auth.GetActiveSessions(username)
	if sessions == nil {
		sessions = []*models.Session{}
	}
	writeSuccess(c, sessions)
}
