// Package streaming handles media streaming with HTTP range request support.
// It provides adaptive streaming, chunked delivery, and mobile optimization.
package streaming

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
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
	ErrFileNotFound = errors.New("file not found")
	ErrInvalidRange = errors.New("invalid range")
	ErrFileTooLarge = errors.New("file too large")
)

const (
	headerContentLength      = "Content-Length"
	headerContentRange       = "Content-Range"
	headerContentDisposition = "Content-Disposition"
	errCloseFileFmt          = "failed to close file: %v"
	errStatFile              = "failed to stat file: %w"
	errOpenFile              = "failed to open file: %w"
	errReadFile              = "read error: %w"
	errClientDisconnected    = "Client disconnected during stream: %v"
	errSeekFile              = "failed to seek: %w"
)

// staleSessionTimeout is the maximum age of an active session's LastUpdate before
// it is considered stale and evicted by the cleanup sweep.
const staleSessionTimeout = 30 * time.Minute

// Module implements media streaming
type Module struct {
	config         *config.Manager
	log            *logger.Logger
	store          storage.Backend // optional; when nil, falls back to direct os.Open
	activeSessions map[string]*models.StreamSession
	sessionMu      sync.RWMutex
	healthy        bool
	healthMsg      string
	healthMu       sync.RWMutex
	stats          StreamStats
	statsMu        sync.RWMutex
	bufferPool     *sync.Pool
	cleanupTicker  *time.Ticker
	cleanupDone    chan struct{}
}

// StreamStats holds streaming statistics. TotalStreams and TotalBytesSent reset on
// server restart (not persisted). Protected by statsMu.
type StreamStats struct {
	TotalStreams   int64 `json:"total_streams"`
	ActiveStreams  int   `json:"active_streams"`
	TotalBytesSent int64 `json:"total_bytes_sent"`
	PeakConcurrent int   `json:"peak_concurrent"`
}

// NewModule creates a new streaming module
func NewModule(cfg *config.Manager) *Module {
	return &Module{
		config:         cfg,
		log:            logger.New("streaming"),
		activeSessions: make(map[string]*models.StreamSession),
		bufferPool: &sync.Pool{
			New: func() any {
				// Create 1MB buffers for the pool (reasonable size for streaming)
				return make([]byte, 1024*1024)
			},
		},
	}
}

// SetStore sets the storage backend for file I/O. When set, the module
// uses the backend instead of direct os.Open calls. This enables S3 support.
func (m *Module) SetStore(s storage.Backend) {
	m.store = s
}

// storeRelPath strips the backend's key prefix from an absolute S3 key so that
// m.store's methods (which add the prefix internally) receive only the relative
// component. For example "videos/foo.mp4" with prefix "videos/" → "foo.mp4".
// For local backends (no KeyPrefix), the path is returned unchanged.
func (m *Module) storeRelPath(p string) string {
	type keyPrefixer interface{ KeyPrefix() string }
	if kp, ok := m.store.(keyPrefixer); ok {
		return strings.TrimPrefix(p, kp.KeyPrefix())
	}
	return p
}

// Name returns the module name
func (m *Module) Name() string {
	return "streaming"
}

// Start initializes the streaming module
func (m *Module) Start(_ context.Context) error {
	m.log.Info("Starting streaming module...")
	m.healthMu.Lock()
	m.healthy = true
	m.healthMsg = "Running"
	m.healthMu.Unlock()

	// Periodic cleanup of stale sessions (e.g. from panicked handlers or abandoned streams).
	m.cleanupDone = make(chan struct{})
	m.cleanupTicker = time.NewTicker(5 * time.Minute)
	go m.sessionCleanupLoop()

	m.log.Info("Streaming module started")
	return nil
}

// Stop gracefully stops the module. Active stream sessions are not waited on;
// they are left to finish or close on their own; we log the count for visibility.
func (m *Module) Stop(_ context.Context) error {
	m.log.Info("Stopping streaming module...")
	if m.cleanupTicker != nil {
		m.cleanupTicker.Stop()
		close(m.cleanupDone)
	}
	m.healthMu.Lock()
	m.healthy = false
	m.healthMsg = "Stopped"
	m.healthMu.Unlock()
	m.sessionMu.Lock()
	activeCount := len(m.activeSessions)
	m.sessionMu.Unlock()
	if activeCount > 0 {
		m.log.Info("Leaving %d active stream session(s) to finish or close", activeCount)
	}
	return nil
}

// sessionCleanupLoop periodically evicts stale sessions whose LastUpdate exceeds
// staleSessionTimeout. This prevents memory leaks from abandoned or panicked streams.
func (m *Module) sessionCleanupLoop() {
	for {
		select {
		case <-m.cleanupTicker.C:
			m.evictStaleSessions()
		case <-m.cleanupDone:
			return
		}
	}
}

// evictStaleSessions removes sessions that have not been updated within staleSessionTimeout.
func (m *Module) evictStaleSessions() {
	now := time.Now()
	m.sessionMu.Lock()
	evicted := 0
	for id, session := range m.activeSessions {
		if now.Sub(session.LastUpdate) > staleSessionTimeout {
			delete(m.activeSessions, id)
			evicted++
		}
	}
	m.sessionMu.Unlock()
	if evicted > 0 {
		m.log.Info("Evicted %d stale stream session(s)", evicted)
	}
}

// Health returns the module health status
func (m *Module) Health() models.HealthStatus {
	m.healthMu.RLock()
	healthy := m.healthy
	msg := m.healthMsg
	m.healthMu.RUnlock()
	return models.HealthStatus{
		Name:      m.Name(),
		Status:    helpers.StatusString(healthy),
		Message:   msg,
		CheckedAt: time.Now(),
	}
}

// StreamRequest holds parameters for a stream request
type StreamRequest struct {
	Path        string // filesystem path for file I/O
	MediaID     string // stable UUID for API responses (player links, admin streams); prefer over Path when exposing to clients
	Quality     string
	UserID      string
	SessionID   string
	IPAddress   string
	UserAgent   string
	RangeHeader string
}

// StreamResponse holds stream response data
type StreamResponse struct {
	File        *os.File
	ContentType string
	FileSize    int64
	Start       int64
	End         int64
	ChunkSize   int64
	StatusCode  int
}

// Stream handles a streaming request
func (m *Module) Stream(w http.ResponseWriter, r *http.Request, req StreamRequest) error {
	m.log.Debug("Stream request for %s from %s", req.Path, req.IPAddress)

	// Use request context so S3 operations are canceled when the client disconnects.
	ctx := r.Context()

	// Get file size via stat (S3 uses backend; local always uses os.Stat directly
	// to avoid cross-root errors when videoStore serves music paths).
	var fileSize int64
	if m.store != nil && !m.store.IsLocal() {
		info, err := m.store.Stat(ctx, m.storeRelPath(req.Path))
		if err != nil {
			if errors.Is(err, storage.ErrNotFound) {
				return ErrFileNotFound
			}
			return fmt.Errorf(errStatFile, err)
		}
		fileSize = info.Size
	} else {
		fi, err := os.Stat(req.Path)
		if err != nil {
			if os.IsNotExist(err) {
				return ErrFileNotFound
			}
			return fmt.Errorf(errStatFile, err)
		}
		fileSize = fi.Size()
	}

	// Determine content type
	contentType := m.getContentType(req.Path)

	// Get chunk size based on quality and device
	chunkSize := m.getChunkSize(req.Quality, req.UserAgent)

	// Parse range header
	start, end, err := m.parseRange(req.RangeHeader, fileSize)
	if err != nil {
		w.Header().Set(headerContentRange, fmt.Sprintf("bytes */%d", fileSize))
		w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
		return nil
	}

	// Track session
	session := m.startSession(req, start)
	defer m.endSession(session.ID)

	// Set response headers
	isRangeRequest := req.RangeHeader != ""
	m.setHeaders(w, contentType, fileSize, start, end, isRangeRequest)

	// Determine status code
	if isRangeRequest {
		w.WriteHeader(http.StatusPartialContent)
	} else {
		w.WriteHeader(http.StatusOK)
	}

	// For remote S3 backends, route through the backend (with prefix stripping).
	// Local backends fall through to os.Open below to handle cross-root paths
	// (e.g. videoStore root cannot serve music directory paths).
	if m.store != nil && !m.store.IsLocal() {
		relPath := m.storeRelPath(req.Path)
		if ro, ok := m.store.(storage.RangeOpener); ok {
			reader, openErr := ro.OpenRange(ctx, relPath, start, end)
			if openErr != nil {
				return fmt.Errorf("failed to open range: %w", openErr)
			}
			defer func() { _ = reader.Close() }()
			return m.streamFromReader(w, reader, end-start+1, chunkSize, session)
		}
		// Fallback: open full file from storage backend
		f, openErr := m.store.Open(ctx, relPath)
		if openErr != nil {
			if errors.Is(openErr, storage.ErrNotFound) {
				return ErrFileNotFound
			}
			return fmt.Errorf(errOpenFile, openErr)
		}
		defer func() { _ = f.Close() }()
		return m.streamContentSeeker(w, f, start, end, chunkSize, session)
	}

	// Legacy: direct filesystem
	file, err := os.Open(req.Path)
	if err != nil {
		if os.IsNotExist(err) {
			return ErrFileNotFound
		}
		return fmt.Errorf(errOpenFile, err)
	}
	defer func() {
		if err := file.Close(); err != nil {
			m.log.Warn(errCloseFileFmt, err)
		}
	}()
	return m.streamContent(w, file, start, end, chunkSize, session)
}

// mediaContentTypes maps common media extensions to their MIME types.
// Parsed once at package init; supplemented by mime.TypeByExtension for unknown extensions.
var mediaContentTypes = map[string]string{
	".mp4":  "video/mp4",
	".webm": "video/webm",
	".mkv":  "video/x-matroska",
	".avi":  "video/x-msvideo",
	".mov":  "video/quicktime",
	".wmv":  "video/x-ms-wmv",
	".flv":  "video/x-flv",
	".m4v":  "video/x-m4v",
	".ts":   "video/mp2t",
	".mp3":  "audio/mpeg",
	".wav":  "audio/wav",
	".flac": "audio/flac",
	".aac":  "audio/aac",
	".ogg":  "audio/ogg",
	".m4a":  "audio/mp4",
	".opus": "audio/opus",
}

// getContentType returns the MIME type for a file
func (m *Module) getContentType(path string) string {
	ext := strings.ToLower(filepath.Ext(path))

	if ct, ok := mediaContentTypes[ext]; ok {
		return ct
	}

	// Fallback to mime package
	ct := mime.TypeByExtension(ext)
	if ct != "" {
		return ct
	}

	return "application/octet-stream"
}

// getChunkSize returns appropriate chunk size based on quality and device.
// Always returns at least 64 KB to prevent zero-length reads when config values
// are missing or explicitly set to zero.
func (m *Module) getChunkSize(quality, userAgent string) int64 {
	const minChunkSize int64 = 64 * 1024

	cfg := m.config.Get()

	var size int64
	isMobile := m.isMobileDevice(userAgent)
	if isMobile && cfg.Streaming.MobileOptimization {
		size = cfg.Streaming.MobileChunkSize
	} else {
		switch quality {
		case "1080p", "high":
			size = cfg.Streaming.MaxChunkSize
		case "480p", "360p", "low":
			size = cfg.Streaming.MobileChunkSize
		default:
			size = cfg.Streaming.DefaultChunkSize
		}
	}

	if size < minChunkSize {
		size = minChunkSize
	}
	return size
}

// isMobileDevice checks if the user agent indicates a mobile device
func (m *Module) isMobileDevice(userAgent string) bool {
	ua := strings.ToLower(userAgent)
	mobileIndicators := []string{
		"mobile", "android", "iphone", "ipad", "ipod",
		"blackberry", "windows phone", "opera mini", "opera mobi",
	}
	for _, indicator := range mobileIndicators {
		if strings.Contains(ua, indicator) {
			return true
		}
	}
	return false
}

// generateSessionID creates a unique session ID using crypto/rand to avoid collisions.
func generateSessionID(prefix string) string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		// Fallback to timestamp if crypto/rand fails
		return fmt.Sprintf("%s-%d", prefix, time.Now().UnixNano())
	}
	return fmt.Sprintf("%s-%s", prefix, hex.EncodeToString(b))
}

// parseRange parses the Range header and returns start and end positions.
// Supports both standard byte ranges (bytes=0-499) and suffix-byte-ranges (bytes=-500).
func (m *Module) parseRange(rangeHeader string, fileSize int64) (start, end int64, err error) {
	if rangeHeader == "" {
		return 0, fileSize - 1, nil
	}

	// Parse "bytes=start-end"
	if !strings.HasPrefix(rangeHeader, "bytes=") {
		return 0, 0, ErrInvalidRange
	}

	rangeSpec := strings.TrimPrefix(rangeHeader, "bytes=")
	parts := strings.Split(rangeSpec, "-")

	if len(parts) != 2 {
		return 0, 0, ErrInvalidRange
	}

	if parts[0] != "" {
		start, err = strconv.ParseInt(parts[0], 10, 64)
		if err != nil {
			return 0, 0, ErrInvalidRange
		}
	} else if parts[1] != "" {
		// Suffix-byte-range-spec: bytes=-500 (last 500 bytes)
		suffixLength, parseErr := strconv.ParseInt(parts[1], 10, 64)
		if parseErr != nil || suffixLength <= 0 {
			return 0, 0, ErrInvalidRange
		}
		if suffixLength >= fileSize {
			start = 0
		} else {
			start = fileSize - suffixLength
		}
		end = fileSize - 1
		return start, end, nil
	}

	if parts[1] != "" {
		end, err = strconv.ParseInt(parts[1], 10, 64)
		if err != nil {
			return 0, 0, ErrInvalidRange
		}
	} else {
		end = fileSize - 1
	}

	// Validate range
	if start < 0 || end >= fileSize || start > end {
		return 0, 0, ErrInvalidRange
	}

	return start, end, nil
}

// setHeaders sets the appropriate HTTP headers for streaming
func (m *Module) setHeaders(w http.ResponseWriter, contentType string, fileSize, start, end int64, isRange bool) {
	cfg := m.config.Get()

	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Accept-Ranges", "bytes")
	w.Header().Set(headerContentLength, strconv.FormatInt(end-start+1, 10))
	if isRange {
		w.Header().Set(headerContentRange, fmt.Sprintf("bytes %d-%d/%d", start, end, fileSize))
	}

	if cfg.Streaming.KeepAliveEnabled {
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("Keep-Alive", fmt.Sprintf("timeout=%d", int(cfg.Streaming.KeepAliveTimeout.Seconds())))
	}

	// Cache headers for partial content
	w.Header().Set("Cache-Control", "no-cache")
}

// streamFromReader streams content from an io.Reader (e.g., S3 ranged GET response)
// to the response writer. Used when the backend supports efficient range reads.
func (m *Module) streamFromReader(w http.ResponseWriter, reader io.Reader, totalBytes, chunkSize int64, session *models.StreamSession) error {
	bufInterface := m.bufferPool.Get()
	buf := bufInterface.([]byte) //nolint:errcheck // pool invariant: only []byte stored
	defer m.bufferPool.Put(bufInterface)

	effectiveChunkSize := min(int64(len(buf)), chunkSize)

	remaining := totalBytes
	var accumulatedBytes int64

	for remaining > 0 {
		toRead := min(effectiveChunkSize, remaining)

		n, err := reader.Read(buf[:toRead])
		if err != nil && !errors.Is(err, io.EOF) {
			if accumulatedBytes > 0 {
				m.updateSessionStats(session.ID, accumulatedBytes)
			}
			return fmt.Errorf(errReadFile, err)
		}
		if n == 0 {
			break
		}

		chunk := buf[:n]
		totalWritten := 0
		for totalWritten < n {
			written, err := w.Write(chunk[totalWritten:])
			if err != nil {
				if accumulatedBytes > 0 {
					m.updateSessionStats(session.ID, accumulatedBytes)
				}
				m.log.Debug(errClientDisconnected, err)
				return nil
			}
			totalWritten += written
		}

		remaining -= int64(totalWritten)
		accumulatedBytes += int64(totalWritten)

		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
	}

	if accumulatedBytes > 0 {
		m.updateSessionStats(session.ID, accumulatedBytes)
	}
	return nil
}

// streamContentSeeker streams from an io.ReadSeeker (storage.ReadSeekCloser).
// Same as streamContent but accepts io.ReadSeeker instead of *os.File.
func (m *Module) streamContentSeeker(w http.ResponseWriter, reader io.ReadSeeker, start, end, chunkSize int64, session *models.StreamSession) error {
	if _, err := reader.Seek(start, io.SeekStart); err != nil {
		return fmt.Errorf(errSeekFile, err)
	}

	bufInterface := m.bufferPool.Get()
	buf := bufInterface.([]byte) //nolint:errcheck // pool invariant: only []byte stored
	defer m.bufferPool.Put(bufInterface)

	effectiveChunkSize := min(int64(len(buf)), chunkSize)

	remaining := end - start + 1
	var accumulatedBytes int64

	for remaining > 0 {
		toRead := min(effectiveChunkSize, remaining)

		n, err := reader.Read(buf[:toRead])
		if err != nil && !errors.Is(err, io.EOF) {
			if accumulatedBytes > 0 {
				m.updateSessionStats(session.ID, accumulatedBytes)
			}
			return fmt.Errorf(errReadFile, err)
		}
		if n == 0 {
			break
		}

		chunk := buf[:n]
		totalWritten := 0
		for totalWritten < n {
			written, err := w.Write(chunk[totalWritten:])
			if err != nil {
				if accumulatedBytes > 0 {
					m.updateSessionStats(session.ID, accumulatedBytes)
				}
				m.log.Debug(errClientDisconnected, err)
				return nil
			}
			totalWritten += written
		}

		remaining -= int64(totalWritten)
		accumulatedBytes += int64(totalWritten)

		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
	}

	if accumulatedBytes > 0 {
		m.updateSessionStats(session.ID, accumulatedBytes)
	}
	return nil
}

// streamContent streams file content to the response writer using buffer pool.
// Handles short writes by retrying remaining bytes until all data is sent.
// Stats are accumulated locally and flushed once after the loop completes
// to avoid acquiring mutexes on every chunk.
func (m *Module) streamContent(w http.ResponseWriter, file *os.File, start, end, chunkSize int64, session *models.StreamSession) error {
	// Seek to start position
	if _, err := file.Seek(start, io.SeekStart); err != nil {
		return fmt.Errorf(errSeekFile, err)
	}

	// Get buffer from pool to prevent memory exhaustion
	bufInterface := m.bufferPool.Get()
	buf := bufInterface.([]byte) //nolint:errcheck // pool invariant: only []byte stored
	defer m.bufferPool.Put(bufInterface)

	// Use pool buffer size (1MB) to prevent excessive memory usage
	effectiveChunkSize := min(int64(len(buf)), chunkSize)

	remaining := end - start + 1
	var accumulatedBytes int64

	for remaining > 0 {
		toRead := min(effectiveChunkSize, remaining)

		n, err := file.Read(buf[:toRead])
		if err != nil && !errors.Is(err, io.EOF) {
			// Flush accumulated stats before returning on error
			if accumulatedBytes > 0 {
				m.updateSessionStats(session.ID, accumulatedBytes)
			}
			return fmt.Errorf(errReadFile, err)
		}
		if n == 0 {
			break
		}

		// Handle short writes by looping until all bytes are written
		chunk := buf[:n]
		totalWritten := 0
		for totalWritten < n {
			written, err := w.Write(chunk[totalWritten:])
			if err != nil {
				// Client disconnected — flush accumulated stats before returning
				if accumulatedBytes > 0 {
					m.updateSessionStats(session.ID, accumulatedBytes)
				}
				m.log.Debug(errClientDisconnected, err)
				return nil
			}
			totalWritten += written
		}

		remaining -= int64(totalWritten)
		accumulatedBytes += int64(totalWritten)

		// Flush if supported
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
	}

	// Flush all accumulated stats in a single call
	if accumulatedBytes > 0 {
		m.updateSessionStats(session.ID, accumulatedBytes)
	}

	return nil
}

// startSession creates and tracks a new streaming session
func (m *Module) startSession(req StreamRequest, position int64) *models.StreamSession {
	mediaID := req.MediaID
	if mediaID == "" {
		mediaID = req.Path // fallback for callers that don't pass MediaID (internal path; avoid exposing in API)
	}
	session := &models.StreamSession{
		ID:         generateSessionID(req.SessionID),
		MediaID:    mediaID,
		UserID:     req.UserID,
		IPAddress:  req.IPAddress,
		Quality:    req.Quality,
		Position:   float64(position),
		StartedAt:  time.Now(),
		LastUpdate: time.Now(),
	}

	m.sessionMu.Lock()
	m.activeSessions[session.ID] = session
	activeCount := len(m.activeSessions)
	// Acquire statsMu while still holding sessionMu so that activeCount is still
	// accurate when we compare it to PeakConcurrent. Releasing sessionMu first
	// creates a TOCTOU window where another goroutine can insert its own session
	// and read the same activeCount, causing both to record the same peak and
	// miss the true concurrent maximum. Consistent with sessionMu→statsMu order
	// established in GetStats and updateSessionStats.
	m.statsMu.Lock()
	m.stats.TotalStreams++
	if activeCount > m.stats.PeakConcurrent {
		m.stats.PeakConcurrent = activeCount
	}
	m.statsMu.Unlock()
	m.sessionMu.Unlock()

	m.log.Debug("Started stream session %s for %s", session.ID, req.Path)
	return session
}

// endSession removes a streaming session
func (m *Module) endSession(sessionID string) {
	m.sessionMu.Lock()
	session, exists := m.activeSessions[sessionID]
	if exists {
		delete(m.activeSessions, sessionID)
		m.log.Debug("Ended stream session %s (bytes: %d)", sessionID, session.BytesSent)
	}
	m.sessionMu.Unlock()
}

// updateSessionStats updates session statistics
func (m *Module) updateSessionStats(sessionID string, bytes int64) {
	m.sessionMu.Lock()
	if session, exists := m.activeSessions[sessionID]; exists {
		session.BytesSent += bytes
		session.LastUpdate = time.Now()
	}
	m.sessionMu.Unlock()

	m.statsMu.Lock()
	m.stats.TotalBytesSent += bytes
	m.statsMu.Unlock()
}

// GetActiveSessions returns all active streaming sessions
func (m *Module) GetActiveSessions() []*models.StreamSession {
	m.sessionMu.RLock()
	defer m.sessionMu.RUnlock()

	sessions := make([]*models.StreamSession, 0, len(m.activeSessions))
	for _, session := range m.activeSessions {
		sessions = append(sessions, session)
	}
	return sessions
}

// GetStats returns streaming statistics.
// Lock ordering: sessionMu -> statsMu (consistent with endSession).
func (m *Module) GetStats() StreamStats {
	m.sessionMu.RLock()
	activeStreams := len(m.activeSessions)
	m.sessionMu.RUnlock()

	m.statsMu.RLock()
	stats := m.stats
	m.statsMu.RUnlock()

	stats.ActiveStreams = activeStreams
	return stats
}

// GetActiveStreamCount returns the number of active streams for a user
func (m *Module) GetActiveStreamCount(userID string) int {
	m.sessionMu.RLock()
	defer m.sessionMu.RUnlock()

	count := 0
	for _, session := range m.activeSessions {
		if session.UserID == userID {
			count++
		}
	}
	return count
}

// CanStartStream checks if a user can start a new stream
func (m *Module) CanStartStream(userID string, maxStreams int) bool {
	if maxStreams <= 0 {
		return true
	}
	return m.GetActiveStreamCount(userID) < maxStreams
}

// TrackProxyStream registers a proxy stream (e.g. receiver-sourced) so it counts toward
// the per-user/per-IP limit. Caller must invoke the returned release func when the stream ends.
func (m *Module) TrackProxyStream(userID string) (release func()) {
	session := &models.StreamSession{
		ID:         generateSessionID("proxy"),
		UserID:     userID,
		StartedAt:  time.Now(),
		LastUpdate: time.Now(),
	}
	m.sessionMu.Lock()
	m.activeSessions[session.ID] = session
	m.sessionMu.Unlock()
	return func() {
		m.endSession(session.ID)
	}
}

// Download handles a file download request with range support and chunked streaming
func (m *Module) Download(w http.ResponseWriter, r *http.Request, path string) error {
	m.log.Debug("Download request for %s", path)

	ctx := r.Context()

	var fileSize int64
	if m.store != nil && !m.store.IsLocal() {
		info, err := m.store.Stat(ctx, m.storeRelPath(path))
		if err != nil {
			if errors.Is(err, storage.ErrNotFound) {
				return ErrFileNotFound
			}
			return fmt.Errorf(errStatFile, err)
		}
		fileSize = info.Size
	} else {
		// Use os.Stat (not open+stat+close) to avoid opening the file twice.
		fi, err := os.Stat(path)
		if err != nil {
			if os.IsNotExist(err) {
				return ErrFileNotFound
			}
			return fmt.Errorf(errStatFile, err)
		}
		fileSize = fi.Size()
	}

	if err := m.validateDownloadFileSize(fileSize); err != nil {
		return err
	}

	filename := filepath.Base(path)
	contentType := m.getContentType(path)

	// Parse range header for resume support
	rangeHeader := r.Header.Get("Range")
	start, end, err := m.parseRange(rangeHeader, fileSize)
	if err != nil {
		w.Header().Set(headerContentRange, fmt.Sprintf("bytes */%d", fileSize))
		w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
		return nil
	}

	m.setDownloadHeaders(w, filename, contentType, rangeHeader, fileSize, start, end)

	// Remote S3 backends: route through the backend with prefix stripping.
	if m.store != nil && !m.store.IsLocal() {
		relPath := m.storeRelPath(path)
		if ro, ok := m.store.(storage.RangeOpener); ok {
			reader, openErr := ro.OpenRange(ctx, relPath, start, end)
			if openErr != nil {
				return fmt.Errorf("failed to open range for download: %w", openErr)
			}
			defer func() { _ = reader.Close() }()
			_, copyErr := io.Copy(w, reader)
			return copyErr
		}
		// Fallback: open full file from storage backend
		f, openErr := m.store.Open(ctx, relPath)
		if openErr != nil {
			return fmt.Errorf(errOpenFile, openErr)
		}
		defer func() { _ = f.Close() }()
		if start > 0 {
			if _, seekErr := f.Seek(start, io.SeekStart); seekErr != nil {
				return fmt.Errorf(errSeekFile, seekErr)
			}
		}
		_, copyErr := io.CopyN(w, f, end-start+1)
		if copyErr != nil && !errors.Is(copyErr, io.EOF) {
			return copyErr
		}
		return nil
	}

	// Legacy: direct filesystem
	file, _, err := m.openFileForDownload(path)
	if err != nil {
		return err
	}
	defer func() {
		if err := file.Close(); err != nil {
			m.log.Warn(errCloseFileFmt, err)
		}
	}()

	return m.streamFileChunked(w, file, filename, start, end)
}

// openFileForDownload opens a file and returns it along with its size.
func (m *Module) openFileForDownload(path string) (*os.File, int64, error) {
	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, 0, ErrFileNotFound
		}
		return nil, 0, fmt.Errorf(errOpenFile, err)
	}
	fileInfo, err := file.Stat()
	if err != nil {
		// Best-effort close on error
		_ = file.Close()
		return nil, 0, fmt.Errorf(errStatFile, err)
	}
	return file, fileInfo.Size(), nil
}

// validateDownloadFileSize checks whether the file exceeds the configured size limit.
func (m *Module) validateDownloadFileSize(fileSize int64) error {
	cfg := m.config.Get()
	if cfg.Security.MaxFileSizeMB > 0 {
		maxBytes := int64(cfg.Security.MaxFileSizeMB) * 1024 * 1024
		if fileSize > maxBytes {
			return fmt.Errorf("%w: %d bytes (limit: %d MB)", ErrFileTooLarge, fileSize, cfg.Security.MaxFileSizeMB)
		}
	}
	return nil
}

// setDownloadHeaders sets HTTP response headers and writes the status code for a download.
func (m *Module) setDownloadHeaders(w http.ResponseWriter, filename, contentType, rangeHeader string, fileSize, start, end int64) {
	w.Header().Set("Content-Type", contentType)
	w.Header().Set(headerContentDisposition, helpers.SafeContentDispositionFilename(filename))
	w.Header().Set("Accept-Ranges", "bytes")
	w.Header().Set("Cache-Control", "no-cache")

	// Handle range request: per RFC 7233, send 206 whenever Range was present
	if rangeHeader != "" {
		contentLen := end - start + 1
		w.Header().Set(headerContentLength, strconv.FormatInt(contentLen, 10))
		w.Header().Set(headerContentRange, fmt.Sprintf("bytes %d-%d/%d", start, end, fileSize))
		w.WriteHeader(http.StatusPartialContent)
		if start != 0 || end != fileSize-1 {
			m.log.Info("Resume download: bytes %d-%d/%d for %s", start, end, fileSize, filename)
		}
	} else {
		w.Header().Set(headerContentLength, strconv.FormatInt(fileSize, 10))
		w.WriteHeader(http.StatusOK)
	}
}

// streamFileChunked seeks to the start position and streams file content in chunks.
func (m *Module) streamFileChunked(w http.ResponseWriter, file *os.File, filename string, start, end int64) error {
	if start > 0 {
		if _, err := file.Seek(start, io.SeekStart); err != nil {
			return fmt.Errorf(errSeekFile, err)
		}
	}

	chunkSize := m.getDownloadChunkSize()
	totalBytes := end - start + 1
	bytesSent, err := m.writeChunkedData(w, file, filename, totalBytes, chunkSize)
	if err != nil {
		m.log.Debug("Chunked transfer error for %s: %v", filename, err)
		return err
	}

	if bytesSent == totalBytes {
		m.log.Info("Download completed: %s (%d bytes)", filename, bytesSent)
	} else {
		m.log.Warn("Download incomplete: %s (%d/%d bytes)", filename, bytesSent, totalBytes)
	}

	return nil
}

// getDownloadChunkSize returns the configured download chunk size in bytes.
func (m *Module) getDownloadChunkSize() int {
	cfg := m.config.Get()
	chunkSize := cfg.Download.ChunkSizeKB * 1024
	if chunkSize <= 0 {
		chunkSize = 512 * 1024 // Default 512KB
	}
	return chunkSize
}

// writeChunkedData writes file content to the response writer in chunks.
// It returns the number of bytes sent and any error that interrupted the transfer.
// Handles short writes by retrying until the chunk is fully written or an error occurs.
func (m *Module) writeChunkedData(w http.ResponseWriter, file *os.File, filename string, totalBytes int64, chunkSize int) (int64, error) {
	// Reuse a buffer from the pool to reduce GC pressure under concurrent downloads.
	bufInterface := m.bufferPool.Get()
	buf := bufInterface.([]byte) //nolint:errcheck // pool invariant: only []byte stored
	defer m.bufferPool.Put(bufInterface)

	// Use the smaller of pool buffer size and requested chunk size
	effectiveChunkSize := min(len(buf), chunkSize)

	remaining := totalBytes
	bytesSent := int64(0)

	for remaining > 0 {
		toRead := min(effectiveChunkSize, int(remaining))

		n, err := file.Read(buf[:toRead])
		if err != nil && !errors.Is(err, io.EOF) {
			m.log.Debug("Download read error for %s: %v", filename, err)
			return bytesSent, err
		}
		if n == 0 {
			break
		}

		chunk := buf[:n]
		for len(chunk) > 0 {
			written, err := w.Write(chunk)
			if err != nil {
				m.log.Debug("Client disconnected during download (sent %d/%d bytes): %v", bytesSent, totalBytes, err)
				return bytesSent, err
			}
			chunk = chunk[written:]
			bytesSent += int64(written)
		}
		remaining -= int64(n)

		// Flush to client
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
	}

	return bytesSent, nil
}

// ServeStatic serves a static file, routing to the storage backend when remote.
func (m *Module) ServeStatic(w http.ResponseWriter, r *http.Request, path string) error {
	if m.store != nil && !m.store.IsLocal() {
		relPath := m.storeRelPath(path)
		rc, err := m.store.Open(r.Context(), relPath)
		if err != nil {
			if errors.Is(err, storage.ErrNotFound) {
				return ErrFileNotFound
			}
			return err
		}
		defer func() { _ = rc.Close() }()
		fi, err := m.store.Stat(r.Context(), relPath)
		if err != nil {
			return err
		}
		http.ServeContent(w, r, fi.Name, fi.ModTime, rc)
		return nil
	}

	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return ErrFileNotFound
		}
		return err
	}
	defer func() {
		if closeErr := file.Close(); closeErr != nil {
			m.log.Warn(errCloseFileFmt, closeErr)
		}
	}()

	fileInfo, err := file.Stat()
	if err != nil {
		return err
	}

	// Use http.ServeContent for proper caching and range support
	http.ServeContent(w, r, fileInfo.Name(), fileInfo.ModTime(), file)
	return nil
}
