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
func (h *Handler) parseUploadFormAndGetFiles(c *gin.Context, cfg *config.Config) ([]*multipart.FileHeader, func(), bool) {
	if cfg.Uploads.MaxFileSize > 0 {
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, cfg.Uploads.MaxFileSize)
	}
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
	// Sum client-reported sizes as an upper bound. The actual bytes written may be
	// less (truncated by MaxBytesReader), but client-reported 0 is treated as the
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

	uploaded := make([]uploadedEntry, 0, len(fileHeaders))
	uploadErrors := make([]errorEntry, 0)
	var totalAdded int64
	// registeredPaths tracks each successfully registered file path so we can
	// delete them if the post-upload quota check fails.
	var registeredPaths []string

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

		// Register the file in the media index immediately so it's visible in the
		// library without waiting for the next scheduled scan. For remote-store
		// uploads the path is a storage key (not a local path), so we use the
		// size-aware variant to skip os.Stat which would fail on remote keys.
		if result.Path != "" {
			var regErr error
			if _, statErr := os.Stat(result.Path); statErr == nil {
				// Local file — use standard path which runs os.Stat internally.
				regErr = h.media.RegisterUploadedFile(result.Path)
			} else {
				// Remote-store key — provide size from upload result.
				regErr = h.media.RegisterUploadedFileWithSize(result.Path, result.Size, time.Now())
			}
			if regErr != nil {
				h.log.Warn("Failed to register uploaded file in library: %v", regErr)
			} else {
				registeredPaths = append(registeredPaths, result.Path)
			}
		}

		// Mature flagging now works because RegisterUploadedFile added the file to
		// the index above, so GetMedia will find it.
		if cfg.Uploads.ScanForMature && result.Path != "" && h.scanner != nil {
			if scanResult := h.scanner.ScanFile(result.Path); scanResult != nil && scanResult.IsMature && cfg.MatureScanner.AutoFlag {
				updates := map[string]any{"is_mature": true}
				if len(scanResult.Reasons) > 0 {
					updates["mature_reason"] = scanResult.Reasons[0]
				}
				if err := h.media.UpdateMetadata(result.Path, updates); err != nil {
					h.log.Error("Failed to flag uploaded file as mature: %v", err)
				}
			}
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
				// Roll back: delete all files that were just registered.
				ctx := c.Request.Context()
				for _, p := range registeredPaths {
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
		}
	}

	writeSuccess(c, map[string]any{
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
