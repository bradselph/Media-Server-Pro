package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"media-server-pro/internal/upload"
)

// UploadMedia handles media file upload
func (h *Handler) UploadMedia(c *gin.Context) {
	if !h.requireUpload(c) {
		return
	}
	session := getSession(c)
	if session == nil {
		writeError(c, http.StatusUnauthorized, errNotAuthenticated)
		return
	}

	user := getUser(c)
	if user == nil {
		writeError(c, http.StatusUnauthorized, errNotAuthenticated)
		return
	}

	if !user.Permissions.CanUpload {
		writeError(c, http.StatusForbidden, "Upload not allowed for your account")
		return
	}

	cfg := h.media.GetConfig()

	if !cfg.Uploads.Enabled {
		writeError(c, http.StatusForbidden, "Uploads are disabled")
		return
	}

	if cfg.Uploads.MaxFileSize > 0 {
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, cfg.Uploads.MaxFileSize)
	}

	if err := c.Request.ParseMultipartForm(32 << 20); err != nil {
		writeError(c, http.StatusBadRequest, "Failed to parse upload form")
		return
	}
	defer func() {
		if c.Request.MultipartForm != nil {
			if err := c.Request.MultipartForm.RemoveAll(); err != nil {
				h.log.Warn("Failed to clean multipart form: %v", err)
			}
		}
	}()

	fileHeaders := c.Request.MultipartForm.File["files"]
	if len(fileHeaders) == 0 {
		fileHeaders = c.Request.MultipartForm.File["file"]
	}
	if len(fileHeaders) == 0 {
		writeError(c, http.StatusBadRequest, "No files provided")
		return
	}

	userType := h.getUserType(cfg, user)
	if userType != nil && userType.StorageQuota > 0 {
		var totalIncoming int64
		for _, fh := range fileHeaders {
			totalIncoming += fh.Size
		}
		if user.StorageUsed+totalIncoming > userType.StorageQuota {
			writeError(c, http.StatusForbidden, "Storage quota exceeded")
			return
		}
	}

	category := c.Request.FormValue("category")

	type uploadedEntry struct {
		UploadID string `json:"upload_id"`
		Filename string `json:"filename"`
		Size     int64  `json:"size"`
	}
	type errorEntry struct {
		Filename string `json:"filename"`
		Error    string `json:"error"`
	}

	uploaded := make([]uploadedEntry, 0, len(fileHeaders))
	uploadErrors := make([]errorEntry, 0)
	var totalAdded int64

	for _, fh := range fileHeaders {
		result, err := h.upload.ProcessFileHeader(fh, upload.UploadScope{UserID: session.UserID, Category: category})
		if err != nil {
			h.log.Error("Upload failed for %s: %v", fh.Filename, err)
			uploadErrors = append(uploadErrors, errorEntry{Filename: fh.Filename, Error: "Upload failed"})
			continue
		}
		if result == nil {
			h.log.Error("Upload returned nil result for %s", fh.Filename)
			uploadErrors = append(uploadErrors, errorEntry{Filename: fh.Filename, Error: "Upload failed"})
			continue
		}
		uploaded = append(uploaded, uploadedEntry{UploadID: string(result.UploadID), Filename: result.Filename, Size: result.Size})
		totalAdded += result.Size

		if cfg.Uploads.ScanForMature && result.Path != "" && h.scanner != nil {
			if scanResult := h.scanner.ScanFile(result.Path); scanResult != nil && scanResult.IsMature && cfg.MatureScanner.AutoFlag {
				if _, err := h.media.GetMedia(result.Path); err == nil {
					updates := map[string]interface{}{"is_mature": true}
					if len(scanResult.Reasons) > 0 {
						updates["mature_reason"] = scanResult.Reasons[0]
					}
					if err := h.media.UpdateMetadata(result.Path, updates); err != nil {
						h.log.Error("Failed to flag uploaded file as mature: %v", err)
					}
				}
			}
		}
	}

	if totalAdded > 0 && user.ID != "admin" {
		if err := h.auth.UpdateUser(c.Request.Context(), user.Username, map[string]interface{}{
			"storage_used": user.StorageUsed + totalAdded,
		}); err != nil {
			h.log.Error("Failed to update user storage: %v", err)
		}
	}

	writeSuccess(c, map[string]interface{}{
		"uploaded": uploaded,
		"errors":   uploadErrors,
	})
}

// GetUploadProgress returns upload progress
func (h *Handler) GetUploadProgress(c *gin.Context) {
	if !h.requireUpload(c) {
		return
	}
	uploadID := upload.UploadID(c.Param("id"))

	progress, ok := h.upload.GetProgress(uploadID)
	if !ok {
		writeError(c, http.StatusNotFound, "Upload not found")
		return
	}

	writeSuccess(c, progress)
}
