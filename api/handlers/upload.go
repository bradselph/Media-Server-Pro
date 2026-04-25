package handlers

import (
	"mime/multipart"
	"net/http"
	"os"
	"time"

	"github.com/gin-gonic/gin"

	"media-server-pro/internal/config"
	"media-server-pro/internal/upload"
	"media-server-pro/pkg/models"
)

// requireUploadSessionAndConfig ensures the upload module and config allow this request.
// When cfg.Uploads.RequireAuth is true, an authenticated session with CanUpload is required.
// When false, anonymous uploads are permitted (subject to storage quota checks being skipped).
// Returns (session, user, cfg, true) or (nil, nil, nil, false) after writing an error.
func (h *Handler) requireUploadSessionAndConfig(c *gin.Context) (session *models.Session, user *models.User, cfg *config.Config, ok bool) {
	if !h.requireUpload(c) {
		return nil, nil, nil, false
	}
	cfg = h.media.GetConfig()
	if !cfg.Uploads.Enabled {
		writeError(c, http.StatusForbidden, "Uploads are disabled")
		return nil, nil, nil, false
	}
	session = getSession(c)
	if cfg.Uploads.RequireAuth && session == nil {
		writeError(c, http.StatusUnauthorized, errNotAuthenticated)
		return nil, nil, nil, false
	}
	if session != nil {
		user = getUser(c)
		if user == nil {
			writeError(c, http.StatusUnauthorized, errNotAuthenticated)
			return nil, nil, nil, false
		}
		if !user.Permissions.CanUpload {
			writeError(c, http.StatusForbidden, "Upload not allowed for your account")
			return nil, nil, nil, false
		}
	}
	return session, user, cfg, true
}

// parseUploadFormAndGetFiles parses the multipart form and returns file headers. Caller must call the returned cleanup.
func (h *Handler) parseUploadFormAndGetFiles(c *gin.Context, _ *config.Config) ([]*multipart.FileHeader, func(), bool) {
	// No MaxBytesReader here: per-file size is enforced by io.LimitReader inside ProcessFileHeader.
	// Applying MaxBytesReader(MaxFileSize) at the body level incorrectly rejects any upload
	// where multipart framing pushes the total body past the per-file limit.
	if err := c.Request.ParseMultipartForm(32 << 20); err != nil {
		writeError(c, http.StatusBadRequest, "Failed to parse upload form")
		return nil, func() { /* no cleanup needed after parse failure */ }, false
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
// Re-reads storage_used from the DB to minimize the race window between concurrent uploads.
// user may be nil for anonymous uploads (RequireAuth=false), in which case no quota applies.
func (h *Handler) checkUploadStorageQuota(c *gin.Context, cfg *config.Config, user *models.User, fileHeaders []*multipart.FileHeader) bool {
	if user == nil {
		return true // anonymous uploads have no quota
	}
	userType := h.getUserType(cfg, user)
	if userType == nil || userType.StorageQuota <= 0 {
		return true
	}
	// Sum client-reported sizes as an upper bound. Client-reported 0 is treated as the
	// configured max file size to prevent quota bypass via a zero-sized header.
	maxFileSize := cfg.Uploads.MaxFileSize
	if maxFileSize <= 0 {
		maxFileSize = 10 << 30 // 10 GB safety cap when unconfigured
	}
	var totalIncoming int64
	for _, fh := range fileHeaders {
		if fh.Size > 0 {
			totalIncoming += fh.Size
		} else {
			// Client sent Size=0 — assume worst case to prevent bypass.
			totalIncoming += maxFileSize
		}
	}
	// Re-read authoritative storage from the auth module to narrow the concurrent-upload race.
	freshUsed := user.StorageUsed
	if freshUser, err := h.auth.GetUser(c.Request.Context(), user.Username); err == nil && freshUser != nil {
		freshUsed = freshUser.StorageUsed
	}
	if freshUsed+totalIncoming <= userType.StorageQuota {
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
	type postEntry struct {
		path    string
		size    int64
		isLocal bool
	}

	uploaded := make([]uploadedEntry, 0, len(fileHeaders))
	uploadErrors := make([]errorEntry, 0)
	var totalAdded int64
	// uploadedPaths tracks every path that was physically written (local or remote)
	// so the quota rollback can delete them all.
	var uploadedPaths []string
	var postProcess []postEntry

	for _, fh := range fileHeaders {
		userID := ""
		if session != nil {
			userID = session.UserID
		}
		result, err := h.upload.ProcessFileHeader(fh, upload.UploadScope{UserID: userID, Category: category})
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

		if result.Path != "" {
			uploadedPaths = append(uploadedPaths, result.Path)
			_, statErr := os.Stat(result.Path)
			postProcess = append(postProcess, postEntry{path: result.Path, size: result.Size, isLocal: statErr == nil})
		}
	}

	// Post-upload quota check using actual bytes written.
	// Skipped for anonymous uploads (user == nil) and the admin account.
	if totalAdded > 0 && user != nil && user.ID != "admin" {
		userType := h.getUserType(cfg, user)
		if userType != nil && userType.StorageQuota > 0 {
			// Re-read authoritative storage to account for concurrent uploads.
			freshUsed := user.StorageUsed
			if freshUser, err := h.auth.GetUser(c.Request.Context(), user.Username); err == nil && freshUser != nil {
				freshUsed = freshUser.StorageUsed
			}
			if freshUsed+totalAdded > userType.StorageQuota {
				ctx := c.Request.Context()
				for _, p := range uploadedPaths {
					if delErr := h.media.DeleteMedia(ctx, p); delErr != nil {
						h.log.Error("Failed to delete overquota upload %s: %v", p, delErr)
					}
				}
				h.log.Warn("Quota exceeded after upload for user %s: used=%d actual=%d quota=%d — files rolled back",
					user.Username, freshUsed, totalAdded, userType.StorageQuota)
				writeError(c, http.StatusForbidden, "Storage quota exceeded")
				return
			}
		}
		if err := h.auth.AddStorageUsed(c.Request.Context(), user.ID, totalAdded); err != nil {
			h.log.Error("Failed to update user storage: %v", err)
			writeError(c, http.StatusInternalServerError, "Upload succeeded but storage quota could not be updated")
			return
		}
	}

	writeSuccess(c, map[string]any{
		"uploaded": uploaded,
		"errors":   uploadErrors,
	})

	// Media-index registration and mature scanning run after the response is sent
	// so that ffprobe and the content classifier don't block the HTTP round-trip.
	scanForMature := cfg.Uploads.ScanForMature
	autoFlag := cfg.MatureScanner.AutoFlag
	for _, entry := range postProcess {
		go func() { //nolint:gosec // G118: background context intentional; request is already complete
			var regErr error
			if entry.isLocal {
				regErr = h.media.RegisterUploadedFile(entry.path)
			} else {
				regErr = h.media.RegisterUploadedFileWithSize(entry.path, entry.size, time.Now())
			}
			if regErr != nil {
				h.log.Warn("Failed to register uploaded file in library: %v", regErr)
				return
			}
			if scanForMature && h.scanner != nil {
				if scanResult := h.scanner.ScanFile(entry.path); scanResult != nil && scanResult.IsMature && autoFlag {
					updates := map[string]any{"is_mature": true}
					if len(scanResult.Reasons) > 0 {
						updates["mature_reason"] = scanResult.Reasons[0]
					}
					if err := h.media.UpdateMetadata(entry.path, updates); err != nil {
						h.log.Error("Failed to flag uploaded file as mature: %v", err)
					}
				}
			}
		}()
	}
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
