// Package upload handles file uploads with validation and security.
package upload

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"media-server-pro/internal/config"
	"media-server-pro/internal/logger"
	"media-server-pro/pkg/helpers"
	"media-server-pro/pkg/models"
	"media-server-pro/pkg/storage"
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

	// Dangerous patterns in filenames (include backslash for Windows path traversal)
	dangerousPatterns = regexp.MustCompile(`[<>:"|?*\\\x00-\x1f]`)
)

// UploadStatus represents the state of an upload (replaces primitive string).
type UploadStatus string

const (
	UploadStatusUploading UploadStatus = "uploading"
	UploadStatusCompleted UploadStatus = "completed"
	UploadStatusFailed    UploadStatus = "failed"
)

// MediaType represents the kind of media (replaces primitive string).
type MediaType string

const (
	MediaTypeVideo   MediaType = "video"
	MediaTypeAudio   MediaType = "audio"
	MediaTypeUnknown MediaType = "unknown"
)

// UploadID uniquely identifies an upload session (replaces primitive string).
type UploadID string

// Module handles file uploads
type Module struct {
	config        *config.Manager
	log           *logger.Logger
	store         storage.Backend // optional storage backend for upload I/O
	activeUploads map[UploadID]*Progress
	mu            sync.RWMutex
	healthy       bool
	healthMsg     string
	healthMu      sync.RWMutex
	uploadDir     string
	done          chan struct{}
	doneOnce      sync.Once
}

// SetStore sets the storage backend for upload I/O.
func (m *Module) SetStore(s storage.Backend) {
	m.store = s
}

// Progress tracks upload progress.
type Progress struct {
	ID          UploadID     `json:"id"`
	Filename    string       `json:"filename"`
	Size        int64        `json:"size"`
	Uploaded    int64        `json:"uploaded"`
	Progress    float64      `json:"progress"`
	Status      UploadStatus `json:"status"`
	StartedAt   time.Time    `json:"started_at"`
	CompletedAt *time.Time   `json:"completed_at,omitempty"`
	Error       string       `json:"error,omitempty"`
	UserID      string       `json:"user_id"`
	DestPath    string       `json:"-"`
}

// Result contains the result of an upload.
type Result struct {
	UploadID  UploadID  `json:"upload_id"`
	Success   bool      `json:"success"`
	Filename  string    `json:"filename"`
	Path      string    `json:"path"`
	Size      int64     `json:"size"`
	MediaType MediaType `json:"media_type"`
	Error     string    `json:"error,omitempty"`
}

// UploadScope identifies the user and optional category for an upload.
type UploadScope struct {
	UserID   string
	Category string
}

// PreparedUpload holds the validated filename, media type, and destination dir from validation.
type PreparedUpload struct {
	Filename  string
	MediaType MediaType
	DestDir   string
}

// ProgressRegistration holds parameters for registering upload progress.
type ProgressRegistration struct {
	UploadID UploadID
	Filename string
	UserID   string
	Size     int64
}

// NewModule creates a new upload module
func NewModule(cfg *config.Manager) *Module {
	return &Module{
		config:        cfg,
		log:           logger.New("upload"),
		activeUploads: make(map[UploadID]*Progress),
		uploadDir:     cfg.Get().Directories.Uploads,
		done:          make(chan struct{}),
	}
}

// Name returns the module name
func (m *Module) Name() string {
	return "upload"
}

// Start initializes the upload module
func (m *Module) Start(_ context.Context) error {
	m.log.Info("Starting upload module...")

	// Ensure upload directory exists (local backends only; S3/B2 have no directories).
	if m.store == nil || m.store.IsLocal() {
		if err := os.MkdirAll(m.uploadDir, 0o750); err != nil {
			m.log.Error("Failed to create upload directory: %v", err)
			m.healthMu.Lock()
			m.healthy = false
			m.healthMsg = fmt.Sprintf("Directory error: %v", err)
			m.healthMu.Unlock()
			return err
		}
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
	m.doneOnce.Do(func() { close(m.done) })
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
// multipart file header. scope identifies the user and optional category for the upload.
func (m *Module) ProcessFileHeader(fh *multipart.FileHeader, scope UploadScope) (*Result, error) {
	prepared, err := m.validateAndPrepareUpload(fh, scope)
	if err != nil {
		return nil, err
	}

	file, err := fh.Open()
	if err != nil {
		return nil, fmt.Errorf("failed to open file: %w", err)
	}
	defer func() {
		if closeErr := file.Close(); closeErr != nil {
			m.log.Warn("Failed to close uploaded file: %v", closeErr)
		}
	}()

	// Validate content type via magic bytes to prevent uploading disguised files (e.g. HTML as .mp4).
	sniffBuf := make([]byte, 512)
	n, _ := file.Read(sniffBuf)
	if n > 0 {
		detectedType := http.DetectContentType(sniffBuf[:n])
		if !m.isContentTypeAllowed(detectedType, prepared.MediaType) {
			return nil, fmt.Errorf("file content does not match extension (detected %s)", detectedType)
		}
	}
	// Seek back to the start so the full file is written to disk.
	if _, seekErr := file.Seek(0, io.SeekStart); seekErr != nil {
		return nil, fmt.Errorf("failed to reset file reader: %w", seekErr)
	}

	// Enforce the actual byte limit regardless of the client-supplied fh.Size.
	// io.LimitReader caps reads at maxFileSize+1; if written > maxFileSize after
	// the copy we know the stream was larger than fh.Size claimed.
	maxFileSize := m.config.Get().Uploads.MaxFileSize
	var reader io.Reader = file
	if maxFileSize > 0 {
		reader = io.LimitReader(file, maxFileSize+1)
	}

	uploadID := m.generateUploadID()
	progress := m.registerUploadProgress(ProgressRegistration{
		UploadID: uploadID,
		Filename: prepared.Filename,
		UserID:   scope.UserID,
		Size:     fh.Size,
	})
	defer m.scheduleUnregisterUpload(uploadID, 5*time.Minute)

	var destPath string
	var written int64

	if m.store != nil && !m.store.IsLocal() {
		// Remote backend (S3/B2): stream directly via the storage backend.
		// No temp-file/rename needed — object writes in S3 are atomic.
		destPath, written, err = m.uploadToRemoteStore(context.Background(), reader, prepared, progress)
	} else {
		// Local filesystem: use the existing temp-file-then-rename pattern.
		var destFile *os.File
		tempPath := ""
		destPath, destFile, err = m.createUniqueUploadFile(prepared.DestDir, prepared.Filename)
		if err != nil {
			return nil, fmt.Errorf("failed to create unique file: %w", err)
		}
		progress.DestPath = destPath
		tempPath = destPath + ".tmp"
		written, err = m.copyAndRenameUpload(reader, destFile, copyPaths{tempPath, destPath}, progress)
	}

	if err != nil {
		progress.Status = UploadStatusFailed
		progress.Error = err.Error()
		return nil, fmt.Errorf("upload failed: %w", err)
	}

	// LimitReader stops at maxFileSize+1, so hitting that exact count means the
	// actual file is larger. Clean up and reject.
	if maxFileSize > 0 && written > maxFileSize {
		if destPath != "" {
			_ = os.Remove(destPath)
		}
		progress.Status = UploadStatusFailed
		progress.Error = "file exceeds size limit"
		return nil, fmt.Errorf("file size exceeds maximum of %d bytes", maxFileSize)
	}

	progress.Status = UploadStatusCompleted
	progress.CompletedAt = new(time.Now())
	progress.Progress = 100
	m.log.Info("Upload complete: %s (%d bytes) by user %s", prepared.Filename, written, scope.UserID)

	return &Result{
		UploadID:  uploadID,
		Success:   true,
		Filename:  filepath.Base(destPath),
		Path:      destPath,
		Size:      written,
		MediaType: prepared.MediaType,
	}, nil
}

// uploadToRemoteStore streams the uploaded file to the configured remote backend
// (S3/B2).  A progress-tracking reader wraps the source so that the UI sees
// live byte counts even though we never touch the local filesystem.
func (m *Module) uploadToRemoteStore(ctx context.Context, file io.Reader, prepared *PreparedUpload, progress *Progress) (string, int64, error) {
	// Compute the relative key path within the upload backend.
	// prepared.DestDir is an absolute local path like /uploads/userID[/category].
	// Strip the upload root prefix to get the relative part.
	absRoot, err := filepath.Abs(m.uploadDir)
	if err != nil {
		return "", 0, fmt.Errorf("resolve upload root: %w", err)
	}
	relDir, err := filepath.Rel(absRoot, prepared.DestDir)
	if err != nil {
		return "", 0, fmt.Errorf("compute relative dest dir: %w", err)
	}
	relDir = filepath.ToSlash(relDir)
	// Guard against ".." segments that could escape the upload prefix (e.g. if
	// the config is reloaded and uploadDir becomes stale relative to DestDir).
	if strings.HasPrefix(relDir, "..") {
		return "", 0, fmt.Errorf("upload destination escapes upload root (got %q)", relDir)
	}

	// Find a unique key within the backend.
	relPath := m.uniqueRemoteKey(ctx, relDir, prepared.Filename)
	progress.DestPath = m.store.AbsPath(relPath)

	// Wrap the reader with a progress counter that uses its own dedicated mutex
	// so that it does not contend with the module's global RWMutex on every
	// read call (which would block concurrent GetActiveUploads/GetProgress calls
	// for the full duration of every S3 chunk write).
	pr := &progressReader{src: file, progress: progress}
	written, err := m.store.Create(ctx, relPath, pr)
	if err != nil {
		return "", 0, fmt.Errorf("store write: %w", err)
	}

	return m.store.AbsPath(relPath), written, nil
}

// uniqueRemoteKey finds an available object key within the remote backend,
// appending _1, _2, … suffixes when the preferred key is already taken.
func (m *Module) uniqueRemoteKey(ctx context.Context, dir, filename string) string {
	ext := filepath.Ext(filename)
	base := strings.TrimSuffix(filename, ext)

	try := func(name string) string {
		if dir == "" || dir == "." {
			return name
		}
		return path.Join(dir, name)
	}

	relPath := try(filename)
	if _, err := m.store.Stat(ctx, relPath); err != nil {
		return relPath // key is free
	}

	for i := 1; i < 1000; i++ {
		relPath = try(fmt.Sprintf("%s_%d%s", base, i, ext))
		if _, err := m.store.Stat(ctx, relPath); err != nil {
			return relPath
		}
	}

	// Timestamp fallback for guaranteed uniqueness.
	return try(fmt.Sprintf("%s_%d%s", base, time.Now().UnixNano(), ext))
}

// progressReader wraps an io.Reader and updates an upload Progress as bytes
// are consumed.  Used for remote backend uploads where we never touch an
// os.File.  It uses a dedicated per-reader mutex so that progress updates do
// not contend with the module's global RWMutex on every chunk read.  Readers
// of Progress (GetProgress, GetActiveUploads) may observe a value lagging by
// at most one chunk, which is acceptable for a progress indicator.
type progressReader struct {
	src      io.Reader
	progress *Progress
	mu       sync.Mutex // per-reader lock, not the module's global RWMutex
}

func (pr *progressReader) Read(p []byte) (int, error) {
	n, err := pr.src.Read(p)
	if n > 0 {
		pr.mu.Lock()
		pr.progress.Uploaded += int64(n)
		if pr.progress.Size > 0 {
			pr.progress.Progress = float64(pr.progress.Uploaded) / float64(pr.progress.Size) * 100
		}
		pr.mu.Unlock()
	}
	return n, err
}

// validateAndPrepareUpload checks size/filename/extension, resolves media type and destination directory.
func (m *Module) validateAndPrepareUpload(fh *multipart.FileHeader, scope UploadScope) (*PreparedUpload, error) {
	if err := validateUploadSize(m.config.Get(), fh.Size); err != nil {
		return nil, err
	}
	filename, err := m.sanitizeFilename(fh.Filename)
	if err != nil {
		return nil, err
	}
	ext := strings.ToLower(filepath.Ext(filename))
	if !m.isAllowedExtension(ext) {
		return nil, fmt.Errorf("file type not allowed: %s", ext)
	}
	destDir, err := m.buildUploadDestDir(scope)
	if err != nil {
		return nil, err
	}
	// MkdirAll is only needed for local backends; remote backends (S3/B2) have no real directories.
	if m.store == nil || m.store.IsLocal() {
		if err := os.MkdirAll(destDir, 0755); err != nil {
			return nil, fmt.Errorf("failed to create directory: %w", err)
		}
	}
	return &PreparedUpload{
		Filename:  filename,
		MediaType: resolveMediaType(ext),
		DestDir:   destDir,
	}, nil
}

// validateUploadSize returns an error if size exceeds the configured maximum.
func validateUploadSize(cfg *config.Config, size int64) error {
	if cfg.Uploads.MaxFileSize > 0 && size > cfg.Uploads.MaxFileSize {
		return fmt.Errorf("file size %d bytes exceeds maximum allowed size of %d bytes", size, cfg.Uploads.MaxFileSize)
	}
	return nil
}

// resolveMediaType returns the media type for the given file extension.
func resolveMediaType(ext string) MediaType {
	if videoExtensions[ext] {
		return MediaTypeVideo
	}
	if audioExtensions[ext] {
		return MediaTypeAudio
	}
	return MediaTypeUnknown
}

// buildUploadDestDir validates scope and returns the destination directory path for the upload.
func (m *Module) buildUploadDestDir(scope UploadScope) (string, error) {
	safeUserID := filepath.Base(scope.UserID)
	if isEmptyOrSpecialFilename(safeUserID) {
		return "", fmt.Errorf("invalid user ID")
	}
	destDir := filepath.Join(m.uploadDir, safeUserID)
	if scope.Category != "" {
		destDir = filepath.Join(m.uploadDir, safeUserID, m.sanitizeCategory(scope.Category))
	}
	return destDir, nil
}

// registerUploadProgress creates a Progress, stores it in activeUploads, and returns it.
func (m *Module) registerUploadProgress(params ProgressRegistration) *Progress {
	progress := &Progress{
		ID:        params.UploadID,
		Filename:  params.Filename,
		Size:      params.Size,
		Status:    UploadStatusUploading,
		StartedAt: time.Now(),
		UserID:    params.UserID,
	}
	m.mu.Lock()
	m.activeUploads[params.UploadID] = progress
	m.mu.Unlock()
	return progress
}

// scheduleUnregisterUpload removes the upload from activeUploads after the given duration.
// Exits on shutdown so goroutines don't leak.
func (m *Module) scheduleUnregisterUpload(uploadID UploadID, after time.Duration) {
	go func() {
		select {
		case <-time.After(after):
			m.mu.Lock()
			delete(m.activeUploads, uploadID)
			m.mu.Unlock()
		case <-m.done:
			return
		}
	}()
}

// copyPaths holds temp and final paths for an upload.
type copyPaths struct {
	tempPath, destPath string
}

// copyAndRenameUpload copies src to destFile with progress, closes destFile, then renames paths.tempPath to paths.destPath.
func (m *Module) copyAndRenameUpload(src io.Reader, destFile *os.File, paths copyPaths, progress *Progress) (int64, error) {
	written, err := m.copyWithProgress(destFile, src, progress)
	if closeErr := destFile.Close(); closeErr != nil {
		m.log.Warn("Failed to close temporary file %s: %v", paths.tempPath, closeErr)
	}
	if err != nil {
		_ = os.Remove(paths.tempPath)
		return 0, err
	}
	if err := os.Rename(paths.tempPath, paths.destPath); err != nil {
		_ = os.Remove(paths.tempPath)
		return written, fmt.Errorf("failed to finalize upload: %w", err)
	}
	return written, nil
}

// HandleUpload processes a multipart file upload (legacy single-file path).
// Prefer using ProcessFileHeader for multi-file support.
// w is used only for MaxBytesReader; the caller must write the response.
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

	return m.ProcessFileHeader(header, UploadScope{UserID: userID, Category: r.FormValue("category")})
}

// containsPathTraversal returns true if s contains path traversal or separator characters.
func containsPathTraversal(s string) bool {
	for _, substr := range []string{"..", "/", "\\"} {
		if strings.Contains(s, substr) {
			return true
		}
	}
	return false
}

// isEmptyOrSpecialFilename returns true for empty, "." or ".." filenames.
func isEmptyOrSpecialFilename(s string) bool {
	switch s {
	case "", ".", "..":
		return true
	default:
		return false
	}
}

// sanitizeFilename validates and cleans a filename
func (m *Module) sanitizeFilename(filename string) (string, error) {
	// Get base name only
	filename = filepath.Base(filename)

	// Check for empty filename
	if isEmptyOrSpecialFilename(filename) {
		return "", fmt.Errorf("invalid filename")
	}

	// Check for hidden files
	if strings.HasPrefix(filename, ".") {
		return "", fmt.Errorf("hidden files not allowed")
	}

	// Check for path traversal
	if containsPathTraversal(filename) {
		return "", fmt.Errorf("path traversal detected")
	}

	// Check for dangerous characters
	if dangerousPatterns.MatchString(filename) {
		return "", fmt.Errorf("filename contains invalid characters")
	}

	// Limit filename length
	if len(filename) > 255 {
		ext := filepath.Ext(filename)
		if len(ext) > 254 {
			ext = ext[:254]
		}
		maxBase := 255 - len(ext)
		if maxBase < 1 {
			maxBase = 1
		}
		filename = filename[:maxBase] + ext
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

// isContentTypeAllowed checks that the detected MIME type is compatible with the expected media type.
// This prevents uploading disguised files (e.g. an HTML file renamed to .mp4).
func (m *Module) isContentTypeAllowed(detected string, expected MediaType) bool {
	// application/octet-stream is the fallback for unknown binary — always allow since
	// many media formats aren't recognized by http.DetectContentType.
	if detected == "application/octet-stream" {
		return true
	}
	switch expected {
	case MediaTypeVideo:
		return strings.HasPrefix(detected, "video/") || strings.HasPrefix(detected, "audio/") || detected == "application/octet-stream"
	case MediaTypeAudio:
		return strings.HasPrefix(detected, "audio/") || detected == "application/ogg" || detected == "application/octet-stream"
	default:
		// Unknown media type — reject HTML/JS/XML which are the dangerous ones.
		return !strings.HasPrefix(detected, "text/html") && !strings.HasPrefix(detected, "text/xml") &&
			!strings.HasPrefix(detected, "application/javascript")
	}
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
	file, err := os.OpenFile(tempPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
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

		file, err = os.OpenFile(tempPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
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

	file, err = os.OpenFile(tempPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return "", nil, fmt.Errorf("failed to create unique file even with timestamp: %w", err)
	}

	return destPath, file, nil
}

// copyWithProgress copies data while updating progress
func (m *Module) copyWithProgress(dst io.Writer, src io.Reader, progress *Progress) (int64, error) {
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
		if errors.Is(readErr, io.EOF) {
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
func (m *Module) generateUploadID() UploadID {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		// Fallback to timestamp-based ID if crypto/rand fails
		return UploadID(fmt.Sprintf("%d", time.Now().UnixNano()))
	}
	return UploadID(hex.EncodeToString(b))
}

// GetProgress returns progress for an upload
func (m *Module) GetProgress(uploadID UploadID) (*Progress, bool) {
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
	safeUserID := filepath.Base(userID)

	if m.store != nil && !m.store.IsLocal() {
		// Remote backend: walk the user's key prefix.
		ctx := context.Background()
		var total int64
		err := m.store.Walk(ctx, safeUserID, func(_ string, fi storage.FileInfo, _ error) error {
			if !fi.IsDir {
				total += fi.Size
			}
			return nil
		})
		return total, err
	}

	// Local filesystem.
	userDir := filepath.Join(m.uploadDir, safeUserID)
	if _, err := os.Stat(userDir); os.IsNotExist(err) {
		return 0, nil
	}

	var total int64
	err := filepath.Walk(userDir, func(_ string, info os.FileInfo, err error) error {
		if err != nil {
			return nil //nolint:nilerr // skip unreadable entries; continue walking
		}
		if !info.IsDir() {
			total += info.Size()
		}
		return nil
	})

	return total, err
}

// CheckQuota checks if user has storage quota available
func (m *Module) CheckQuota(userID string, fileSize, quota int64) (bool, error) {
	if quota <= 0 {
		return true, nil // No quota limit
	}

	used, err := m.GetUserStorageUsed(userID)
	if err != nil {
		return false, err
	}

	return used+fileSize <= quota, nil
}
