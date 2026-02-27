// Package upload handles file uploads with validation and security.
package upload

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"media-server-pro/internal/config"
	"media-server-pro/internal/logger"
	"media-server-pro/pkg/helpers"
	"media-server-pro/pkg/models"
)

var (
	// Allowed video extensions
	videoExtensions = map[string]bool{
		".mp4": true, ".mkv": true, ".avi": true, ".mov": true, ".wmv": true,
		".flv": true, ".webm": true, ".m4v": true, ".mpg": true, ".mpeg": true,
		".3gp": true, ".ts": true, ".m2ts": true, ".vob": true, ".ogv": true,
	}

	// Allowed audio extensions
	audioExtensions = map[string]bool{
		".mp3": true, ".wav": true, ".flac": true, ".aac": true, ".ogg": true,
		".m4a": true, ".wma": true, ".aiff": true, ".alac": true, ".opus": true,
	}

	// Dangerous patterns in filenames
	dangerousPatterns = regexp.MustCompile(`[<>:"|?*\x00-\x1f]`)
)

// Module handles file uploads
type Module struct {
	config        *config.Manager
	log           *logger.Logger
	activeUploads map[string]*Progress
	mu            sync.RWMutex
	healthy       bool
	healthMsg     string
	healthMu      sync.RWMutex
	uploadDir     string
}

// Progress tracks upload progress.
type Progress struct {
	ID          string     `json:"id"`
	Filename    string     `json:"filename"`
	Size        int64      `json:"size"`
	Uploaded    int64      `json:"uploaded"`
	Progress    float64    `json:"progress"`
	Status      string     `json:"status"`
	StartedAt   time.Time  `json:"started_at"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`
	Error       string     `json:"error,omitempty"`
	UserID      string     `json:"user_id"`
	DestPath    string     `json:"dest_path,omitempty"`
}

// Result contains the result of an upload.
type Result struct {
	Success   bool   `json:"success"`
	Filename  string `json:"filename"`
	Path      string `json:"path"`
	Size      int64  `json:"size"`
	MediaType string `json:"media_type"`
	Error     string `json:"error,omitempty"`
}

// NewModule creates a new upload module
func NewModule(cfg *config.Manager) *Module {
	return &Module{
		config:        cfg,
		log:           logger.New("upload"),
		activeUploads: make(map[string]*Progress),
		uploadDir:     cfg.Get().Directories.Uploads,
	}
}

// Name returns the module name
func (m *Module) Name() string {
	return "upload"
}

// Start initializes the upload module
func (m *Module) Start(_ context.Context) error {
	m.log.Info("Starting upload module...")

	// Ensure upload directory exists
	if err := os.MkdirAll(m.uploadDir, 0755); err != nil {
		m.log.Error("Failed to create upload directory: %v", err)
		m.healthMu.Lock()
		m.healthy = false
		m.healthMsg = fmt.Sprintf("Directory error: %v", err)
		m.healthMu.Unlock()
		return err
	}

	m.healthMu.Lock()
	m.healthy = true
	m.healthMsg = "Running"
	m.healthMu.Unlock()
	m.log.Info("Upload module started")
	return nil
}

// Stop gracefully stops the module
func (m *Module) Stop(_ context.Context) error {
	m.log.Info("Stopping upload module...")
	m.healthMu.Lock()
	m.healthy = false
	m.healthMsg = "Stopped"
	m.healthMu.Unlock()
	return nil
}

// Health returns the module health status
func (m *Module) Health() models.HealthStatus {
	m.healthMu.RLock()
	defer m.healthMu.RUnlock()
	return models.HealthStatus{
		Name:      m.Name(),
		Status:    helpers.StatusString(m.healthy),
		Message:   m.healthMsg,
		CheckedAt: time.Now(),
	}
}

// ProcessFileHeader validates and saves a single uploaded file identified by its
// multipart file header. userID is used for the per-user subdirectory; category
// is an optional subdirectory name within the user directory.
func (m *Module) ProcessFileHeader(fh *multipart.FileHeader, userID, category string) (*Result, error) {
	// Validate filename
	filename, err := m.sanitizeFilename(fh.Filename)
	if err != nil {
		return nil, err
	}

	// Validate extension
	ext := strings.ToLower(filepath.Ext(filename))
	if !m.isAllowedExtension(ext) {
		return nil, fmt.Errorf("file type not allowed: %s", ext)
	}

	// Determine media type
	mediaType := "unknown"
	if videoExtensions[ext] {
		mediaType = "video"
	} else if audioExtensions[ext] {
		mediaType = "audio"
	}

	// Build destination directory
	destDir := filepath.Join(m.uploadDir, userID)
	if category != "" {
		destDir = filepath.Join(m.uploadDir, userID, m.sanitizeCategory(category))
	}

	if err := os.MkdirAll(destDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create directory: %w", err)
	}

	// Open the uploaded file
	file, err := fh.Open()
	if err != nil {
		return nil, fmt.Errorf("failed to open file: %w", err)
	}
	defer func() {
		if err := file.Close(); err != nil {
			m.log.Warn("Failed to close uploaded file: %v", err)
		}
	}()

	// Generate upload ID and create progress tracker
	uploadID := m.generateUploadID()
	progress := &Progress{
		ID:        uploadID,
		Filename:  filename,
		Size:      fh.Size,
		Status:    "uploading",
		StartedAt: time.Now(),
		UserID:    userID,
	}

	m.mu.Lock()
	m.activeUploads[uploadID] = progress
	m.mu.Unlock()

	defer func() {
		m.mu.Lock()
		delete(m.activeUploads, uploadID)
		m.mu.Unlock()
	}()

	// Atomically find unique filename and create temp file
	destPath, destFile, err := m.createUniqueUploadFile(destDir, filename)
	if err != nil {
		return nil, fmt.Errorf("failed to create unique file: %w", err)
	}
	progress.DestPath = destPath
	tempPath := destPath + ".tmp"

	// Copy with progress tracking
	written, err := m.copyWithProgress(destFile, file, progress)

	// Close destFile explicitly before the rename. On Windows, renaming an
	// open file fails with "The process cannot access the file because it is
	// being used by another process". The defer-based close runs after the
	// function returns — too late. We close here, then fall through to the
	// rename. The double-close from the defer below is safe: Close on an
	// already-closed *os.File returns an error which is silently ignored.
	if closeErr := destFile.Close(); closeErr != nil {
		m.log.Warn("Failed to close temporary file %s: %v", tempPath, closeErr)
	}

	if err != nil {
		if removeErr := os.Remove(tempPath); removeErr != nil {
			m.log.Warn("Failed to remove temporary file %s: %v", tempPath, removeErr)
		}
		progress.Status = "failed"
		progress.Error = err.Error()
		return nil, fmt.Errorf("upload failed: %w", err)
	}

	// Atomic rename (file must be closed first, especially on Windows)
	if err := os.Rename(tempPath, destPath); err != nil {
		if removeErr := os.Remove(tempPath); removeErr != nil {
			m.log.Warn("Failed to remove temporary file %s: %v", tempPath, removeErr)
		}
		return nil, fmt.Errorf("failed to finalize upload: %w", err)
	}

	progress.Status = "completed"
	completedAt := time.Now()
	progress.CompletedAt = &completedAt
	progress.Progress = 100

	m.log.Info("Upload complete: %s (%d bytes) by user %s", filename, written, userID)

	return &Result{
		Success:   true,
		Filename:  filepath.Base(destPath),
		Path:      destPath,
		Size:      written,
		MediaType: mediaType,
	}, nil
}

// HandleUpload processes a multipart file upload (legacy single-file path).
// Prefer using ProcessFileHeader for multi-file support.
func (m *Module) HandleUpload(w http.ResponseWriter, r *http.Request, userID string) (*Result, error) {
	cfg := m.config.Get()

	if !cfg.Uploads.Enabled {
		return nil, fmt.Errorf("uploads are disabled")
	}

	if cfg.Uploads.MaxFileSize > 0 {
		r.Body = http.MaxBytesReader(w, r.Body, cfg.Uploads.MaxFileSize)
	}

	if err := r.ParseMultipartForm(32 << 20); err != nil {
		return nil, fmt.Errorf("failed to parse form: %w", err)
	}
	defer func() {
		if err := r.MultipartForm.RemoveAll(); err != nil {
			m.log.Warn("Failed to clean up multipart form: %v", err)
		}
	}()

	_, header, err := r.FormFile("file")
	if err != nil {
		return nil, fmt.Errorf("failed to get file: %w", err)
	}

	return m.ProcessFileHeader(header, userID, r.FormValue("category"))
}

// sanitizeFilename validates and cleans a filename
func (m *Module) sanitizeFilename(filename string) (string, error) {
	// Get base name only
	filename = filepath.Base(filename)

	// Check for empty filename
	if filename == "" || filename == "." || filename == ".." {
		return "", fmt.Errorf("invalid filename")
	}

	// Check for hidden files
	if strings.HasPrefix(filename, ".") {
		return "", fmt.Errorf("hidden files not allowed")
	}

	// Check for path traversal
	if strings.Contains(filename, "..") || strings.Contains(filename, "/") || strings.Contains(filename, "\\") {
		return "", fmt.Errorf("path traversal detected")
	}

	// Check for dangerous characters
	if dangerousPatterns.MatchString(filename) {
		return "", fmt.Errorf("filename contains invalid characters")
	}

	// Limit filename length
	if len(filename) > 255 {
		ext := filepath.Ext(filename)
		base := filename[:255-len(ext)]
		filename = base + ext
	}

	return filename, nil
}

// sanitizeCategory cleans a category name
func (m *Module) sanitizeCategory(category string) string {
	// Remove any path separators and dangerous chars
	category = filepath.Base(category)
	category = dangerousPatterns.ReplaceAllString(category, "_")
	category = strings.ReplaceAll(category, "..", "")

	if category == "" || category == "." {
		return "uncategorized"
	}
	return category
}

// isAllowedExtension checks if extension is allowed
func (m *Module) isAllowedExtension(ext string) bool {
	cfg := m.config.Get()

	// Check against configured allowed extensions
	for _, allowed := range cfg.Uploads.AllowedExtensions {
		if strings.EqualFold(ext, allowed) {
			return true
		}
	}

	// Fall back to built-in lists
	return videoExtensions[ext] || audioExtensions[ext]
}

// createUniqueUploadFile atomically finds a unique filename and creates a temporary file for upload.
// Uses O_CREATE|O_EXCL to prevent TOCTOU race conditions between uniqueness check and file creation.
// Returns the final destination path, the opened temp file handle, and any error.
func (m *Module) createUniqueUploadFile(destDir, filename string) (string, *os.File, error) {
	ext := filepath.Ext(filename)
	base := strings.TrimSuffix(filename, ext)

	// Try original filename first
	destPath := filepath.Join(destDir, filename)
	tempPath := destPath + ".tmp"

	// O_CREATE|O_EXCL ensures atomic check-and-create (fails if file exists)
	file, err := os.OpenFile(tempPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
	if err == nil {
		return destPath, file, nil
	}
	if !os.IsExist(err) {
		return "", nil, err
	}

	// Try numbered variants
	for i := 1; i < 1000; i++ {
		destPath = filepath.Join(destDir, fmt.Sprintf("%s_%d%s", base, i, ext))
		tempPath = destPath + ".tmp"

		file, err = os.OpenFile(tempPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
		if err == nil {
			return destPath, file, nil
		}
		if !os.IsExist(err) {
			return "", nil, err
		}
	}

	// Fallback: use nanosecond timestamp for guaranteed uniqueness
	destPath = filepath.Join(destDir, fmt.Sprintf("%s_%d%s", base, time.Now().UnixNano(), ext))
	tempPath = destPath + ".tmp"

	file, err = os.OpenFile(tempPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
	if err != nil {
		return "", nil, fmt.Errorf("failed to create unique file even with timestamp: %w", err)
	}

	return destPath, file, nil
}

// copyWithProgress copies data while updating progress
func (m *Module) copyWithProgress(dst io.Writer, src multipart.File, progress *Progress) (int64, error) {
	buf := make([]byte, 32*1024) // 32KB buffer
	var written int64

	for {
		nr, readErr := src.Read(buf)
		if nr > 0 {
			n, err := m.writeChunkAndTrack(dst, buf[:nr], progress, written)
			written += n
			if err != nil {
				return written, err
			}
			if nr != int(n) {
				return written, io.ErrShortWrite
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return written, readErr
		}
	}

	return written, nil
}

// writeChunkAndTrack writes a chunk to dst and updates the upload progress tracker.
// It returns the number of bytes written and any write error.
func (m *Module) writeChunkAndTrack(dst io.Writer, chunk []byte, progress *Progress, prevWritten int64) (int64, error) {
	nw, writeErr := dst.Write(chunk)
	if nw > 0 {
		totalWritten := prevWritten + int64(nw)
		m.mu.Lock()
		progress.Uploaded = totalWritten
		if progress.Size > 0 {
			progress.Progress = float64(totalWritten) / float64(progress.Size) * 100
		}
		m.mu.Unlock()
	}
	return int64(nw), writeErr
}

// generateUploadID creates a unique upload ID
func (m *Module) generateUploadID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		// Fallback to timestamp-based ID if crypto/rand fails
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}

// GetProgress returns progress for an upload
func (m *Module) GetProgress(uploadID string) (*Progress, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	progress, ok := m.activeUploads[uploadID]
	return progress, ok
}

// GetActiveUploads returns all active uploads
func (m *Module) GetActiveUploads() []*Progress {
	m.mu.RLock()
	defer m.mu.RUnlock()

	uploads := make([]*Progress, 0, len(m.activeUploads))
	for _, u := range m.activeUploads {
		uploads = append(uploads, u)
	}
	return uploads
}

// GetUserStorageUsed calculates storage used by a specific user by walking
// only their per-user upload subdirectory (uploads/{userID}/).
func (m *Module) GetUserStorageUsed(userID string) (int64, error) {
	userDir := filepath.Join(m.uploadDir, userID)
	if _, err := os.Stat(userDir); os.IsNotExist(err) {
		return 0, nil
	}

	var total int64
	err := filepath.Walk(userDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // Continue on error
		}
		if !info.IsDir() {
			total += info.Size()
		}
		return nil
	})

	return total, err
}

// CheckQuota checks if user has storage quota available
func (m *Module) CheckQuota(userID string, fileSize int64, quota int64) (bool, error) {
	if quota <= 0 {
		return true, nil // No quota limit
	}

	used, err := m.GetUserStorageUsed(userID)
	if err != nil {
		return false, err
	}

	return used+fileSize <= quota, nil
}
