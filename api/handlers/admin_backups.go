package handlers

import (
	"encoding/json"
	"errors"
	"net/http"
	"regexp"

	"github.com/gin-gonic/gin"

	"media-server-pro/internal/analytics"
	"media-server-pro/internal/backup"
	"media-server-pro/internal/repositories"
)

// validBackupID matches alphanumeric strings, hyphens, underscores, and dots.
// Dots are required because backup IDs embed a fractional-second timestamp
// (backup_20060102_150405.000000000). No path separators are allowed, and
// resolveBackupPath additionally enforces pathWithinBase, so dots are safe.
var validBackupID = regexp.MustCompile(`^[a-zA-Z0-9_.-]+$`)

// ListBackupsV2 lists backups using the backup module
func (h *Handler) ListBackupsV2(c *gin.Context) {
	if !h.requireBackup(c) {
		return
	}
	backups, err := h.backup.ListBackups()
	if err != nil {
		h.log.Error("%v", err)
		writeError(c, http.StatusInternalServerError, errInternalServer)
		return
	}
	writeSuccess(c, backups)
}

// CreateBackupV2 creates a backup using the backup module
func (h *Handler) CreateBackupV2(c *gin.Context) {
	if !h.requireBackup(c) {
		return
	}
	var req struct {
		Description string `json:"description"`
		BackupType  string `json:"backup_type"`
	}
	if err := json.NewDecoder(c.Request.Body).Decode(&req); err != nil {
		writeError(c, http.StatusBadRequest, errInvalidRequest)
		return
	}

	if req.BackupType == "" {
		req.BackupType = "full"
	}

	backupInfo, err := h.backup.CreateBackup(backup.CreateBackupOptions{
		Description: req.Description,
		Type:        req.BackupType,
		Version:     h.buildInfo.Version,
	})
	if err != nil {
		h.log.Error("%v", err)
		writeError(c, http.StatusInternalServerError, errInternalServer)
		return
	}

	h.trackServerEvent(c, analytics.EventBackupCreate, map[string]any{
		"backup_type": req.BackupType,
		"description": req.Description,
	})
	writeSuccess(c, backupInfo)
}

// RestoreBackup restores from a backup (v2 API - by ID path param). Runs synchronously.
func (h *Handler) RestoreBackup(c *gin.Context) {
	if !h.requireBackup(c) {
		return
	}
	backupID := c.Param("id")
	if !validBackupID.MatchString(backupID) {
		writeError(c, http.StatusBadRequest, "Invalid backup ID format")
		return
	}

	if err := h.backup.RestoreBackup(backupID); err != nil {
		h.log.Error("%v", err)
		writeError(c, http.StatusInternalServerError, errInternalServer)
		return
	}

	h.trackServerEvent(c, analytics.EventBackupRestore, map[string]any{
		"backup_id": backupID,
	})
	writeSuccess(c, map[string]string{"message": "Backup restored"})
}

// DeleteBackup deletes a backup
func (h *Handler) DeleteBackup(c *gin.Context) {
	if !h.requireBackup(c) {
		return
	}
	backupID := c.Param("id")
	if !validBackupID.MatchString(backupID) {
		writeError(c, http.StatusBadRequest, "Invalid backup ID format")
		return
	}

	if err := h.backup.DeleteBackup(backupID); err != nil {
		if errors.Is(err, repositories.ErrBackupManifestNotFound) {
			writeError(c, http.StatusNotFound, "Backup not found")
			return
		}
		h.log.Error("%v", err)
		writeError(c, http.StatusInternalServerError, errInternalServer)
		return
	}

	h.trackServerEvent(c, analytics.EventBackupDelete, map[string]any{
		"backup_id": backupID,
	})
	writeSuccess(c, nil)
}
