package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/gin-gonic/gin"
)

// ListBackupsV2 lists backups using the backup module
func (h *Handler) ListBackupsV2(c *gin.Context) {
	if !h.requireBackup(c) {
		return
	}
	backups, err := h.backup.ListBackups()
	if err != nil {
		h.log.Error("%v", err)
		writeError(c, http.StatusInternalServerError, "Internal server error")
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

	backupInfo, err := h.backup.CreateBackup(req.Description, req.BackupType)
	if err != nil {
		h.log.Error("%v", err)
		writeError(c, http.StatusInternalServerError, "Internal server error")
		return
	}

	writeSuccess(c, backupInfo)
}

// RestoreBackup restores from a backup (v2 API - by ID path param)
func (h *Handler) RestoreBackup(c *gin.Context) {
	if !h.requireBackup(c) {
		return
	}
	backupID := c.Param("id")

	if err := h.backup.RestoreBackup(backupID); err != nil {
		h.log.Error("%v", err)
		writeError(c, http.StatusInternalServerError, "Internal server error")
		return
	}

	writeSuccess(c, map[string]string{"message": "Backup restored"})
}

// DeleteBackup deletes a backup
func (h *Handler) DeleteBackup(c *gin.Context) {
	if !h.requireBackup(c) {
		return
	}
	backupID := c.Param("id")

	if err := h.backup.DeleteBackup(backupID); err != nil {
		h.log.Error("%v", err)
		writeError(c, http.StatusInternalServerError, "Internal server error")
		return
	}

	writeSuccess(c, nil)
}
