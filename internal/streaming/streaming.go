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
	"net/http"
	"os"
	"path/filepath"
	"slices"
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
	ErrFileNotFound        = errors.New("file not found")
	ErrInvalidRange        = errors.New("invalid range")
	ErrFileTooLarge        = errors.New("file too large")
	ErrStreamLimitExceeded = errors.New("stream limit exceeded")
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

// keepaliveInterval refreshes an active session's LastUpdate well within
// staleSessionTimeout so the stale-session sweep never evicts a stream that is
// still being served. Long single transfers flush stats only once at the end,
// and opaque proxied streams never touch LastUpdate at all, so without this a
// stream longer than staleSessionTimeout would be wrongly evicted mid-transfer.
const keepaliveInterval = staleSessionTimeout / 3

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
	// onSessionStart and onSessionEnd are optional analytics hooks set by the
	// caller (main.go) to bridge streaming events into the analytics module
	// without creating a streaming → analytics import dependency. Both are
	// called outside any streaming-internal lock so the callback is free to
	// do its own DB I/O.
	onSessionStart func(*models.StreamSession)
	onSessionEnd   func(*models.StreamSession)

	// hookCh is a bounded queue draining session-lifecycle analytics hooks through a
	// small fixed worker pool, instead of spawning an unbounded `go` per Stream()
	// call (which fires per Range request, not per logical session). This bounds
	// goroutine churn and provides backpressure if the analytics DB is briefly slow.
	hookCh       chan func()
	hookStop     chan struct{}
	hookStopOnce sync.Once
	hookWG       sync.WaitGroup
}

// Streaming analytics-hook worker pool sizing.
const (
	streamHookQueueSize = 256
	streamHookWorkers   = 2
)

// SetSessionHooks installs callbacks invoked when a stream session starts
// or ends. Pass nil for either hook to leave it unset. Calling this on a
// running module is safe but only affects sessions started after the call.
func (m *Module) SetSessionHooks(onStart, onEnd func(*models.StreamSession)) {
	m.onSessionStart = onStart
	m.onSessionEnd = onEnd
}

// StreamStats holds streaming statistics. TotalStreams and TotalBytesSent reset on
// server restart (not persisted). Protected by statsMu.
type StreamStats struct {
	TotalStreams   int64 `json:"total_streams"`
	ActiveStreams  int   `json:"active_streams"`
	TotalBytesSent int64 `json:"total_bytes_sent"`
	PeakConcurrent int   `json:"peak_concurrent"`
}

// maxPooledBufferSize is a sane upper bound on the pooled I/O buffer computed in
// NewModule, so a pathological chunk-size config value can't blow up per-buffer
// memory use (the pool holds one of these per concurrently-streaming goroutine).
const maxPooledBufferSize = 64 * 1024 * 1024 // 64MB

// NewModule creates a new streaming module
func NewModule(cfg *config.Manager) *Module {
	// Buffer size is read once at module construction (streaming is not a
	// hot-reload config section; changes require restart). The pooled buffer must
	// be at least as large as the largest configured chunk size, or
	// getChunkSize's MaxChunkSize/DefaultChunkSize/MobileChunkSize settings would
	// be silently truncated to BufferSize on every read (effectiveChunkSize in
	// streamFromReader/streamContentSeeker/streamContent/writeChunkedData is
	// min(len(buf), chunkSize)).
	streamingCfg := cfg.Get().Streaming
	bufSize := streamingCfg.BufferSize
	if bufSize <= 0 {
		bufSize = 1024 * 1024
	}
	for _, chunkSize := range []int64{
		streamingCfg.DefaultChunkSize,
		streamingCfg.MaxChunkSize,
		streamingCfg.MobileChunkSize,
	} {
		if chunkSize > int64(bufSize) {
			bufSize = int(chunkSize)
		}
	}
	if bufSize > maxPooledBufferSize {
		bufSize = maxPooledBufferSize
	}
	return &Module{
		config:         cfg,
		log:            logger.New("streaming"),
		activeSessions: make(map[string]*models.StreamSession),
		bufferPool: &sync.Pool{
			New: func() any {
				return make([]byte, bufSize)
			},
		},
		hookCh:   make(chan func(), streamHookQueueSize),
		hookStop: make(chan struct{}),
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

	// Stale-session eviction runs as a registered task under the central
	// scheduler (see cmd/server/main.go → "streaming-session-cleanup").
	// The module no longer owns its own ticker, so admins can re-schedule
	// or disable eviction from the System Ops panel.

	// Start the bounded analytics-hook worker pool.
	for range streamHookWorkers {
		m.hookWG.Add(1)
		go m.hookWorker()
	}

	m.log.Info("Streaming module started")
	return nil
}

// hookWorker drains queued session-lifecycle hooks. On stop it drains any already
// queued hooks before exiting so a graceful shutdown doesn't drop accepted events.
func (m *Module) hookWorker() {
	defer m.hookWG.Done()
	for {
		select {
		case fn := <-m.hookCh:
			fn()
		case <-m.hookStop:
			for {
				select {
				case fn := <-m.hookCh:
					fn()
				default:
					return
				}
			}
		}
	}
}

// stopHookWorkers signals the hook workers to drain and exit, then waits. Safe to
// call multiple times (Stop may be invoked more than once).
func (m *Module) stopHookWorkers() {
	m.hookStopOnce.Do(func() {
		close(m.hookStop)
		m.hookWG.Wait()
	})
}

// enqueueHook queues a session-lifecycle analytics hook for the worker pool. If the
// queue is full it drops the event (with a warning) rather than blocking the hot
// streaming path or spawning an unbounded goroutine — analytics hooks are best-effort.
func (m *Module) enqueueHook(name string, hook func(*models.StreamSession), snap *models.StreamSession) {
	job := func() { m.runSessionHook(name, hook, snap) }
	select {
	case m.hookCh <- job:
	default:
		m.log.Warn("Streaming %s hook queue full; dropping analytics event", name)
	}
}

// Stop gracefully stops the module. Active stream sessions are not waited on;
// they are left to finish or close on their own; we log the count for visibility.
func (m *Module) Stop(_ context.Context) error {
	m.log.Info("Stopping streaming module...")
	m.healthMu.Lock()
	m.healthy = false
	m.healthMsg = "Stopped"
	m.healthMu.Unlock()

	// Stop the analytics-hook workers, draining any queued hooks first.
	m.stopHookWorkers()

	m.sessionMu.Lock()
	activeCount := len(m.activeSessions)
	m.sessionMu.Unlock()
	if activeCount > 0 {
		m.log.Info("Leaving %d active stream session(s) to finish or close", activeCount)
	}
	return nil
}

// EvictStaleSessions removes sessions that have not been updated within
// staleSessionTimeout. The scheduler drives this on a registered cadence
// (see cmd/server/main.go → "streaming-session-cleanup"); call it directly
// only from tests or shutdown paths.
func (m *Module) EvictStaleSessions() int {
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
	return evicted
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
	MaxStreams  int // per-UserID concurrent-stream cap, enforced atomically in startSession; 0 = unlimited
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

	// Get file size and mtime via stat (S3 uses backend; local always uses os.Stat
	// directly to avoid cross-root errors when videoStore serves music paths).
	var fileSize int64
	var modTime time.Time
	if m.store != nil && !m.store.IsLocal() {
		info, err := m.store.Stat(ctx, m.storeRelPath(req.Path))
		if err != nil {
			if errors.Is(err, storage.ErrNotFound) {
				return ErrFileNotFound
			}
			return fmt.Errorf(errStatFile, err)
		}
		fileSize = info.Size
		modTime = info.ModTime
	} else {
		fi, err := os.Stat(req.Path)
		if err != nil {
			if os.IsNotExist(err) {
				return ErrFileNotFound
			}
			return fmt.Errorf(errStatFile, err)
		}
		fileSize = fi.Size()
		modTime = fi.ModTime()
	}

	// Determine content type
	contentType := m.getContentType(req.Path)

	// Get chunk size based on quality and device
	chunkSize := m.getChunkSize(req.Quality, req.UserAgent)

	// Validators let the browser revalidate/resume a previously fetched byte range
	// (e.g. after SPA navigation away and back) instead of always refetching from
	// byte 0. hasModTime is false for storage backends whose Stat doesn't report a
	// mtime, in which case validators are skipped entirely.
	etag, hasModTime := computeValidators(fileSize, modTime)

	// If-Range: only serve the requested range when the client's cached copy still
	// matches the current validator; otherwise fall back to serving the full file
	// with 200 instead of splicing a range onto content that has since changed.
	rangeHeader := req.RangeHeader
	if rangeHeader != "" && !ifRangeSatisfied(r.Header.Get("If-Range"), etag, modTime, hasModTime) {
		rangeHeader = ""
	}

	// If-None-Match / If-Modified-Since revalidation applies to plain (non-range)
	// GETs — a Range request is a seek/resume, not a full-response cache check.
	if req.RangeHeader == "" && notModified(r, etag, modTime, hasModTime) {
		m.writeNotModified(w, etag, modTime)
		return nil
	}

	// Parse range header
	start, end, err := m.parseRange(rangeHeader, fileSize)
	if err != nil {
		w.Header().Set(headerContentRange, fmt.Sprintf("bytes */%d", fileSize))
		w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
		return nil
	}

	// Track session. startSession enforces req.MaxStreams atomically under the same
	// lock that inserts the session and returns nil when the cap is already reached,
	// closing the check-then-act race a separate CanStartStream pre-check leaves open.
	// The cap counts distinct media per user/IP, not raw session rows, so ordinary
	// seeking (which recreates the underlying HTTP connection and thus this session)
	// never trips a low cap by itself — see countUserStreamsLocked.
	session := m.startSession(req, start)
	if session == nil {
		return ErrStreamLimitExceeded
	}
	defer m.endSession(session.ID)
	// Keep the session fresh during long transfers (stats are otherwise flushed
	// only once, after the whole range is written) so the sweep can't evict it.
	defer m.startSessionKeepalive(session.ID)()

	// Set response headers
	isRangeRequest := rangeHeader != ""
	m.setHeaders(w, contentType, fileSize, start, end, isRangeRequest, etag, modTime)

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

// getContentType returns the MIME type for a file. Delegates to the shared
// helpers.MediaContentType so master and slave (internal/follower)
// advertise identical content types — keeping the curated map in one place.
func (m *Module) getContentType(path string) string {
	return helpers.MediaContentType(path)
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
	return slices.ContainsFunc(mobileIndicators, func(indicator string) bool {
		return strings.Contains(ua, indicator)
	})
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

// computeValidators derives a weak ETag from size+mtime for use as an HTTP
// validator. The ETag is weak (RFC 7232's "W/" prefix) because chunked delivery
// across different storage backends doesn't guarantee byte-for-byte
// reproducibility — only size+mtime identity is guaranteed, and that's exactly
// what a resume/cache revalidation needs. It is stable across requests as long as
// the file itself hasn't changed. Returns ("", false) when modTime is unknown
// (e.g. a storage backend whose Stat doesn't populate ModTime), so callers can
// skip validators entirely instead of emitting a bogus Unix-epoch mtime.
func computeValidators(size int64, modTime time.Time) (etag string, hasModTime bool) {
	if modTime.IsZero() {
		return "", false
	}
	return fmt.Sprintf(`W/"%x-%x"`, size, modTime.UnixNano()), true
}

// setValidatorHeaders writes ETag/Last-Modified when available; a no-op for
// whichever is unset (e.g. hasModTime was false when the caller computed etag).
func setValidatorHeaders(w http.ResponseWriter, etag string, modTime time.Time) {
	if etag != "" {
		w.Header().Set("ETag", etag)
	}
	if !modTime.IsZero() {
		w.Header().Set("Last-Modified", modTime.UTC().Format(http.TimeFormat))
	}
}

// etagMatch reports whether any entry in a comma-separated If-None-Match/If-Range
// header value matches etag, honoring the "*" wildcard and ignoring weak ("W/")
// prefixes on either side, per RFC 7232's weak-comparison rules.
func etagMatch(header, etag string) bool {
	if etag == "" {
		return false
	}
	header = strings.TrimSpace(header)
	if header == "" {
		return false
	}
	if header == "*" {
		return true
	}
	want := strings.TrimPrefix(etag, "W/")
	for _, part := range strings.Split(header, ",") {
		if strings.TrimPrefix(strings.TrimSpace(part), "W/") == want {
			return true
		}
	}
	return false
}

// ifRangeSatisfied reports whether an If-Range precondition permits serving the
// requested range. Per RFC 7233 §3.2: a missing header always permits the range;
// an HTTP-date value permits it only if the file has not been modified since that
// date; any other value is compared as an ETag (exact match only — a weak
// validator never satisfies If-Range for byte-range purposes, but since our ETag
// is itself weak we accept a match here rather than never honoring If-Range at
// all, which matches this module's "best-effort validators" scope).
func ifRangeSatisfied(header, etag string, modTime time.Time, hasModTime bool) bool {
	if header == "" {
		return true
	}
	if t, err := http.ParseTime(header); err == nil {
		return hasModTime && !modTime.Truncate(time.Second).After(t)
	}
	return etagMatch(header, etag)
}

// notModified evaluates If-None-Match (preferred when present) or
// If-Modified-Since against the current validators, per RFC 7232 §6. Only
// meaningful for plain (non-range) GETs — callers must not apply this to a
// request carrying a Range header, where a 206/200 is always the correct
// response shape.
func notModified(r *http.Request, etag string, modTime time.Time, hasModTime bool) bool {
	if inm := r.Header.Get("If-None-Match"); inm != "" {
		return etagMatch(inm, etag)
	}
	if ims := r.Header.Get("If-Modified-Since"); ims != "" && hasModTime {
		if t, err := http.ParseTime(ims); err == nil {
			return !modTime.Truncate(time.Second).After(t)
		}
	}
	return false
}

// writeNotModified sends a 304 response carrying the current validators. Per RFC
// 7232 §4.1 a 304 must include the headers that would have accompanied a 200
// (ETag/Last-Modified/Cache-Control here) and must not include a body.
func (m *Module) writeNotModified(w http.ResponseWriter, etag string, modTime time.Time) {
	setValidatorHeaders(w, etag, modTime)
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusNotModified)
}

// setHeaders sets the appropriate HTTP headers for streaming
func (m *Module) setHeaders(w http.ResponseWriter, contentType string, fileSize, start, end int64, isRange bool, etag string, modTime time.Time) {
	cfg := m.config.Get()

	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Accept-Ranges", "bytes")
	w.Header().Set(headerContentLength, strconv.FormatInt(end-start+1, 10))
	if isRange {
		w.Header().Set(headerContentRange, fmt.Sprintf("bytes %d-%d/%d", start, end, fileSize))
	}

	setValidatorHeaders(w, etag, modTime)

	if cfg.Streaming.KeepAliveEnabled {
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("Keep-Alive", fmt.Sprintf("timeout=%d", int(cfg.Streaming.KeepAliveTimeout.Seconds())))
	}

	// Cache headers for partial content. no-cache still lets the browser reuse a
	// cached response after a successful revalidation against ETag/Last-Modified
	// above — it only forces that revalidation round-trip on every fetch.
	w.Header().Set("Cache-Control", "no-cache")

	// Disable response buffering on nginx (or any X-Accel-capable proxy) in front
	// of the app so chunked media bytes reach the client as they're flushed
	// instead of sitting in a proxy buffer. Mirrors api/handlers/analytics.go.
	w.Header().Set("X-Accel-Buffering", "no")
}

// streamFromReader streams content from an io.Reader (e.g., S3 ranged GET response)
// to the response writer. Used when the backend supports efficient range reads.
//
// NOTE: streamFromReader, streamContentSeeker, and streamContent deliberately
// share an identical inner loop (read → write → accumulate → flush) but differ
// in seek handling and byte accounting. Keeping them separate avoids an
// unverified must-seek-first precondition and an extra call per chunk that a
// shared helper would introduce on this hot path — do not dedup.
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

// countUserStreamsLocked returns the number of distinct media currently counted
// against userID's concurrent-stream cap, and reports whether matchMediaID (when
// non-empty) already has an active session for this user. Callers must hold
// sessionMu (read or write lock).
//
// Sessions are deduped by MediaID because a single logical playback can produce
// many rows in m.activeSessions: every HTTP Range request (including the ones
// ordinary seeking/scrubbing generates by aborting and recreating the <video>
// element's connection) starts its own session, and the old row isn't reaped
// until its next Write() fails — see Stream()/streamContent. Charging the cap
// once per Range request instead of once per distinct video is exactly the bug
// this dedup closes.
//
// Sessions with no MediaID (currently only TrackProxyStream's receiver-proxy
// sessions, which have no media identity to dedupe against) are never coalesced
// together — doing so would let a single user hold unlimited proxy streams under
// one counted slot.
func (m *Module) countUserStreamsLocked(userID, matchMediaID string) (distinct int, alreadyStreaming bool) {
	seenMedia := make(map[string]struct{})
	anonymous := 0
	for _, s := range m.activeSessions {
		if s.UserID != userID {
			continue
		}
		if s.MediaID == "" {
			anonymous++
			continue
		}
		if matchMediaID != "" && s.MediaID == matchMediaID {
			alreadyStreaming = true
		}
		seenMedia[s.MediaID] = struct{}{}
	}
	return len(seenMedia) + anonymous, alreadyStreaming
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
	// Enforce the per-user cap inside the same critical section as the insert so
	// concurrent requests can't both pass a separate pre-check and over-fill. The
	// cap counts DISTINCT media (see countUserStreamsLocked), and a request for
	// media the user is already streaming always bypasses it: ordinary seeking
	// aborts the in-flight HTTP connection and opens a new one for the same
	// video, which would otherwise look like a second concurrent stream until the
	// old session's next failed Write() reaps it.
	if req.MaxStreams > 0 {
		distinct, alreadyStreaming := m.countUserStreamsLocked(req.UserID, mediaID)
		if !alreadyStreaming && distinct >= req.MaxStreams {
			m.sessionMu.Unlock()
			return nil
		}
	}
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
	// Fire-and-forget analytics hook outside the lock so analytics DB I/O
	// can never block the hot streaming path.
	if hook := m.onSessionStart; hook != nil {
		// Pass a copy so the analytics layer can't accidentally mutate the
		// session struct that the streaming module is still tracking.
		m.enqueueHook("onSessionStart", hook, new(*session))
	}
	return session
}

// runSessionHook invokes a session-lifecycle hook with panic recovery so a
// faulty analytics callback can't kill the per-session goroutine silently.
func (m *Module) runSessionHook(name string, hook func(*models.StreamSession), snap *models.StreamSession) {
	defer func() {
		if r := recover(); r != nil {
			m.log.Error("Streaming %s hook panicked: %v", name, r)
		}
	}()
	hook(snap)
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
	if exists {
		if hook := m.onSessionEnd; hook != nil {
			m.enqueueHook("onSessionEnd", hook, new(*session))
		}
	}
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

// touchSession refreshes an active session's LastUpdate only (no stats flush),
// so a long-running-but-active session doesn't look stale to the cleanup sweep.
func (m *Module) touchSession(sessionID string) {
	m.sessionMu.Lock()
	if session, exists := m.activeSessions[sessionID]; exists {
		session.LastUpdate = time.Now()
	}
	m.sessionMu.Unlock()
}

// startSessionKeepalive periodically touches the session until the returned stop
// func is called (idempotent). Tie stop to the request lifetime with defer; the
// ticker first fires after keepaliveInterval, so short streams pay ~nothing.
func (m *Module) startSessionKeepalive(sessionID string) (stop func()) {
	done := make(chan struct{})
	go func() {
		t := time.NewTicker(keepaliveInterval)
		defer t.Stop()
		for {
			select {
			case <-done:
				return
			case <-t.C:
				m.touchSession(sessionID)
			}
		}
	}()
	var once sync.Once
	return func() { once.Do(func() { close(done) }) }
}

// GetActiveSessions returns snapshots of all active streaming sessions.
// Copies each struct before returning so callers cannot race with
// updateSessionStats, which mutates BytesSent/LastUpdate under sessionMu.Lock.
func (m *Module) GetActiveSessions() []*models.StreamSession {
	m.sessionMu.RLock()
	defer m.sessionMu.RUnlock()

	sessions := make([]*models.StreamSession, 0, len(m.activeSessions))
	for _, session := range m.activeSessions {
		sessions = append(sessions, new(*session))
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

// GetActiveStreamCount returns the number of distinct media a user is currently
// streaming (see countUserStreamsLocked) — not the raw number of session rows,
// since one logical playback can hold multiple rows across seeks.
func (m *Module) GetActiveStreamCount(userID string) int {
	m.sessionMu.RLock()
	defer m.sessionMu.RUnlock()

	distinct, _ := m.countUserStreamsLocked(userID, "")
	return distinct
}

// CanStartStream checks if a user can start a new stream. It has no way to know
// which media the caller is about to request, so it cannot exempt "already
// streaming this exact media" the way startSession/CanStartStreamForMedia do —
// prefer CanStartStreamForMedia when the target mediaID is known before checking.
func (m *Module) CanStartStream(userID string, maxStreams int) bool {
	if maxStreams <= 0 {
		return true
	}
	return m.GetActiveStreamCount(userID) < maxStreams
}

// CanStartStreamForMedia is like CanStartStream but never counts a stream against
// the cap when the user already has an active session for the exact same media,
// so a pre-flight check performed before a Range request for media already in
// progress (e.g. a seek) never rejects it merely because the cap is otherwise
// full. maxStreams<=0 disables the cap.
func (m *Module) CanStartStreamForMedia(userID, mediaID string, maxStreams int) bool {
	if maxStreams <= 0 {
		return true
	}
	m.sessionMu.RLock()
	defer m.sessionMu.RUnlock()
	distinct, alreadyStreaming := m.countUserStreamsLocked(userID, mediaID)
	return alreadyStreaming || distinct < maxStreams
}

// TrackProxyStream atomically enforces the per-user/per-IP concurrent-stream cap and,
// when under it, registers a proxy stream (e.g. receiver-sourced) so it counts toward
// the limit. It returns ok=false (with a nil release) without registering when the cap
// is already reached; maxStreams<=0 disables the cap. Folding the count check and the
// insert into one critical section closes the check-then-act race a separate
// CanStartStream + TrackProxyStream pair leaves open. The caller must invoke the
// returned release func when the stream ends.
func (m *Module) TrackProxyStream(userID string, maxStreams int) (release func(), ok bool) {
	return m.TrackProxyStreamForMedia(userID, "", maxStreams)
}

// TrackProxyStreamForMedia is TrackProxyStream with a mediaID so repeated proxy
// requests for the same receiver-sourced media dedupe against the cap exactly
// like the local direct-play path (see countUserStreamsLocked). Pass "" for
// mediaID when the caller has no media identity to dedupe against; such sessions
// are counted individually and never coalesced with each other.
func (m *Module) TrackProxyStreamForMedia(userID, mediaID string, maxStreams int) (release func(), ok bool) {
	session := &models.StreamSession{
		ID:         generateSessionID("proxy"),
		MediaID:    mediaID,
		UserID:     userID,
		StartedAt:  time.Now(),
		LastUpdate: time.Now(),
	}
	m.sessionMu.Lock()
	if maxStreams > 0 {
		distinct, alreadyStreaming := m.countUserStreamsLocked(userID, mediaID)
		if !alreadyStreaming && distinct >= maxStreams {
			m.sessionMu.Unlock()
			return nil, false
		}
	}
	m.activeSessions[session.ID] = session
	m.sessionMu.Unlock()
	// Proxied streams never touch LastUpdate (no updateSessionStats runs for them),
	// so keep the session fresh until release() or the sweep would evict any relay
	// longer than staleSessionTimeout mid-stream.
	stopKeepalive := m.startSessionKeepalive(session.ID)
	return func() {
		stopKeepalive()
		m.endSession(session.ID)
	}, true
}

// Download handles a file download request with range support and chunked streaming
func (m *Module) Download(w http.ResponseWriter, r *http.Request, path string) error {
	m.log.Debug("Download request for %s", path)

	ctx := r.Context()

	var fileSize int64
	var modTime time.Time
	if m.store != nil && !m.store.IsLocal() {
		info, err := m.store.Stat(ctx, m.storeRelPath(path))
		if err != nil {
			if errors.Is(err, storage.ErrNotFound) {
				return ErrFileNotFound
			}
			return fmt.Errorf(errStatFile, err)
		}
		fileSize = info.Size
		modTime = info.ModTime
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
		modTime = fi.ModTime()
	}

	if err := m.validateDownloadFileSize(fileSize); err != nil {
		return err
	}

	filename := filepath.Base(path)
	contentType := m.getContentType(path)

	// Validators mirror Stream()'s: they let a resumed/retried download revalidate
	// or resume against a previously fetched range instead of always restarting.
	etag, hasModTime := computeValidators(fileSize, modTime)

	// Parse range header for resume support
	origRangeHeader := r.Header.Get("Range")
	rangeHeader := origRangeHeader
	if rangeHeader != "" && !ifRangeSatisfied(r.Header.Get("If-Range"), etag, modTime, hasModTime) {
		rangeHeader = ""
	}

	// If-None-Match / If-Modified-Since revalidation applies to plain (non-range)
	// GETs, same rationale as Stream().
	if origRangeHeader == "" && notModified(r, etag, modTime, hasModTime) {
		m.writeNotModified(w, etag, modTime)
		return nil
	}

	start, end, err := m.parseRange(rangeHeader, fileSize)
	if err != nil {
		w.Header().Set(headerContentRange, fmt.Sprintf("bytes */%d", fileSize))
		w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
		return nil
	}

	m.setDownloadHeaders(w, filename, contentType, rangeHeader, fileSize, start, end, etag, modTime)

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
func (m *Module) setDownloadHeaders(w http.ResponseWriter, filename, contentType, rangeHeader string, fileSize, start, end int64, etag string, modTime time.Time) {
	w.Header().Set("Content-Type", contentType)
	w.Header().Set(headerContentDisposition, helpers.SafeContentDispositionFilename(filename))
	w.Header().Set("Accept-Ranges", "bytes")
	setValidatorHeaders(w, etag, modTime)
	// no-cache still lets the browser/download-manager reuse a cached response
	// after a successful revalidation against ETag/Last-Modified above.
	w.Header().Set("Cache-Control", "no-cache")
	// Disable proxy response buffering, same rationale as setHeaders.
	w.Header().Set("X-Accel-Buffering", "no")

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
