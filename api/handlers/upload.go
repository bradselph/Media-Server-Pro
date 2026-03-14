package handlers

import (
	"mime/multipart"
	"net/http"

	"github.com/gin-gonic/gin"

	"media-server-pro/internal/config"
	"media-server-pro/internal/upload"
	"media-server-pro/pkg/models"
)

// requireUploadSessionAndConfig ensures upload module, session, CanUpload, and uploads enabled.
// Returns (session, user, cfg, true) or (nil, nil, nil, false) after writing an error.
func (h *Handler) requireUploadSessionAndConfig(c *gin.Context) (session *models.Session, user *models.User, cfg *config.Config, ok bool) {
	if !h.requireUpload(c) {
		return nil, nil, nil, false
	}
	session = getSession(c)
	if session == nil {
		writeError(c, http.StatusUnauthorized, errNotAuthenticated)
		return nil, nil, nil, false
	}
	user = getUser(c)
	if user == nil {
		writeError(c, http.StatusUnauthorized, errNotAuthenticated)
		return nil, nil, nil, false
	}
	if !user.Permissions.CanUpload {
		writeError(c, http.StatusForbidden, "Upload not allowed for your account")
		return nil, nil, nil, false
	}
	cfg = h.media.GetConfig()
	if !cfg.Uploads.Enabled {
		writeError(c, http.StatusForbidden, "Uploads are disabled")
		return nil, nil, nil, false
	}
	return session, user, cfg, true
}

// parseUploadFormAndGetFiles parses the multipart form and returns file headers. Caller must call the returned cleanup.
func (h *Handler) parseUploadFormAndGetFiles(c *gin.Context, cfg *config.Config) ([]*multipart.FileHeader, func(), bool) {
	if cfg.Uploads.MaxFileSize > 0 {
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, cfg.Uploads.MaxFileSize)
	}
	if err := c.Request.ParseMultipartForm(32 << 20); err != nil {
		writeError(c, http.StatusBadRequest, "Failed to parse upload form")
		return nil, func() {}, false
	}
	cleanup := func() {
		if c.Request.MultipartForm != nil {
			if err := c.Request.MultipartForm.RemoveAll(); err != nil {
				h.log.Warn("Failed to clean multipart form: %v", err)
			}
		}
	}
	fileHeaders := c.Request.MultipartForm.File["files"]
	if len(fileHeaders) == 0 {
		fileHeaders = c.Request.MultipartForm.File["file"]
	}
	if len(fileHeaders) == 0 {
		writeError(c, http.StatusBadRequest, "No files provided")
		return nil, cleanup, false
	}
	return fileHeaders, cleanup, true
}

// checkUploadStorageQuota returns false and writes an error if user would exceed quota. Otherwise true.
func (h *Handler) checkUploadStorageQuota(c *gin.Context, cfg *config.Config, user *models.User, fileHeaders []*multipart.FileHeader) bool {
	userType := h.getUserType(cfg, user)
	if userType == nil || userType.StorageQuota <= 0 {
		return true
	}
	var totalIncoming int64
	for _, fh := range fileHeaders {
		totalIncoming += fh.Size
	}
	if user.StorageUsed+totalIncoming <= userType.StorageQuota {
		return true
	}
	writeError(c, http.StatusForbidden, "Storage quota exceeded")
	return false
}

// UploadMedia handles media file upload
func (h *Handler) UploadMedia(c *gin.Context) {
	session, user, cfg, ok := h.requireUploadSessionAndConfig(c)
	if !ok {
		return
	}
	fileHeaders, cleanup, ok := h.parseUploadFormAndGetFiles(c, cfg)
	defer cleanup()
	if !ok {
		return
	}
	if !h.checkUploadStorageQuota(c, cfg, user, fileHeaders) {
		return
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
		if err := h.auth.AddStorageUsed(c.Request.Context(), user.ID, totalAdded); err != nil {
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
