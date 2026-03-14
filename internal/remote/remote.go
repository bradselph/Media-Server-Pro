// Package remote provides remote media source management and streaming.
// It handles fetching media from remote HTTP sources with authentication support.
// Periodic synchronization is managed internally via syncTicker (started in Start()
// when RemoteMediaConfig.SyncInterval > 0). Remote media items are accessible via
// the admin API (/api/admin/remote/media) and are stored separately from local media.
package remote

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"media-server-pro/internal/config"
	"media-server-pro/internal/database"
	"media-server-pro/internal/logger"
	"media-server-pro/internal/repositories"
	mysqlrepo "media-server-pro/internal/repositories/mysql"
	"media-server-pro/pkg/helpers"
	"media-server-pro/pkg/models"
)

const (
	errSourceNotFoundFmt = "source not found: %s"
	errCloseResponseFmt  = "Failed to close response body: %v"
	headerUserAgent      = "User-Agent"
	userAgentValue       = "MediaServerPro/4.0"
)

// allowedRemoteHeaders is the set of response headers forwarded from the remote
// origin to the client. Only media-relevant headers are allowed to avoid leaking
// server identity or infrastructure details (Server, X-Powered-By, etc.).
var allowedRemoteHeaders = map[string]bool{
	"Content-Type":        true,
	"Content-Length":      true,
	"Content-Range":       true,
	"Content-Disposition": true,
	"Accept-Ranges":       true,
	"Last-Modified":       true,
	"Etag":                true,
	"Cache-Control":       true,
}

// Module handles remote media sources
type Module struct {
	config     *config.Manager
	log        *logger.Logger
	dbModule   *database.Module
	repo       repositories.RemoteCacheRepository
	httpClient *http.Client
	sources    map[string]*SourceState
	mediaCache map[string]*CachedMedia
	mu         sync.RWMutex
	healthy    bool
	healthMsg  string
	healthMu   sync.RWMutex
	cacheDir   string
	syncTicker *time.Ticker
	syncDone   chan struct{}
	cacheSem   chan struct{} // bounds concurrent background cache downloads
}

// SourceState tracks the state of a remote source.
type SourceState struct {
	Source     config.RemoteSource `json:"source"`
	Status     string              `json:"status"`
	LastSync   time.Time           `json:"last_sync"`
	MediaCount int                 `json:"media_count"`
	Error      string              `json:"error,omitempty"`
	Media      []*MediaItem        `json:"media,omitempty"`
}

// MediaItem represents a media item from a remote source.
type MediaItem struct {
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	Path        string            `json:"-"`
	URL         string            `json:"url"`
	SourceName  string            `json:"source_name"`
	Size        int64             `json:"size"`
	ContentType string            `json:"content_type"`
	Duration    float64           `json:"duration,omitempty"`
	Metadata    map[string]string `json:"metadata,omitempty"`
	CachedAt    *time.Time        `json:"cached_at,omitempty"`
}

// CachedMedia represents cached remote media
type CachedMedia struct {
	RemoteURL   string    `json:"remote_url"`
	LocalPath   string    `json:"-"`
	Size        int64     `json:"size"`
	ContentType string    `json:"content_type"`
	CachedAt    time.Time `json:"cached_at"`
	LastAccess  time.Time `json:"last_access"`
	Hits        int       `json:"hits"`
}

// NewModule creates a new remote media module
func NewModule(cfg *config.Manager, dbModule *database.Module) *Module {
	transport := helpers.SafeHTTPTransport()
	return &Module{
		config:   cfg,
		log:      logger.New("remote"),
		dbModule: dbModule,
		httpClient: &http.Client{
			Transport: transport,
			Timeout:   30 * time.Second,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				if len(via) >= 5 {
					return fmt.Errorf("too many redirects")
				}
				// SSRF: SafeHTTPTransport's DialContext blocks private IPs when connecting.
				// Also validate redirect URL explicitly so hostnames that resolve to
				// private IPs are rejected before the connection attempt.
				if err := validateURL(req.URL.String()); err != nil {
					return fmt.Errorf("redirect blocked: %w", err)
				}
				return nil
			},
		},
		sources:    make(map[string]*SourceState),
		mediaCache: make(map[string]*CachedMedia),
		cacheDir:   filepath.Join(cfg.Get().Directories.Data, "remote_cache"),
		syncDone:   make(chan struct{}),
		cacheSem:   make(chan struct{}, 4), // max 4 concurrent background cache downloads
	}
}

// Name returns the module name
func (m *Module) Name() string {
	return "remote"
}

// Start initializes the remote media module
func (m *Module) Start(_ context.Context) error {
	m.log.Info("Starting remote media module...")

	m.repo = mysqlrepo.NewRemoteCacheRepository(m.dbModule.GORM())

	cfg := m.config.Get()
	if !cfg.RemoteMedia.Enabled {
		m.log.Info("Remote media is disabled")
		m.healthMu.Lock()
		m.healthy = true
		m.healthMsg = "Disabled"
		m.healthMu.Unlock()
		return nil
	}

	// Create cache directory
	if cfg.RemoteMedia.CacheEnabled {
		if err := os.MkdirAll(m.cacheDir, 0755); err != nil {
			m.log.Warn("Failed to create cache directory: %v", err)
		}
	}

	// Load cache index
	m.loadCacheIndex()

	// Initialize sources
	for _, source := range cfg.RemoteMedia.Sources {
		if source.Enabled {
			m.sources[source.Name] = &SourceState{
				Source: source,
				Status: "pending",
			}
		}
	}

	// Start sync goroutine
	if cfg.RemoteMedia.SyncInterval > 0 {
		m.syncTicker = time.NewTicker(cfg.RemoteMedia.SyncInterval)
		go m.syncLoop()
	}

	// Initial sync
	go m.syncAllSources()

	m.healthMu.Lock()
	m.healthy = true
	m.healthMsg = fmt.Sprintf("Running with %d sources", len(m.sources))
	m.healthMu.Unlock()
	m.log.Info("Remote media module started")
	return nil
}

// Stop gracefully stops the module
func (m *Module) Stop(_ context.Context) error {
	m.log.Info("Stopping remote media module...")

	if m.syncTicker != nil {
		m.syncTicker.Stop()
	}
	close(m.syncDone)

	// Save cache index
	m.saveCacheIndex()

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

// syncLoop periodically syncs all sources
func (m *Module) syncLoop() {
	for {
		select {
		case <-m.syncTicker.C:
			m.syncAllSources()
		case <-m.syncDone:
			return
		}
	}
}

// syncAllSources syncs media from all configured sources
func (m *Module) syncAllSources() {
	m.mu.RLock()
	sources := make([]*SourceState, 0, len(m.sources))
	for _, s := range m.sources {
		sources = append(sources, s)
	}
	m.mu.RUnlock()

	for _, source := range sources {
		if err := m.syncSource(source.Source.Name); err != nil {
			m.log.Error("Failed to sync source %s: %v", source.Source.Name, err)
		}
	}
}

// syncSource syncs media from a specific source
func (m *Module) syncSource(sourceName string) error {
	m.mu.Lock()
	state, exists := m.sources[sourceName]
	if !exists {
		m.mu.Unlock()
		return fmt.Errorf(errSourceNotFoundFmt, sourceName)
	}

	state.Status = "syncing"
	m.mu.Unlock()

	m.log.Info("Syncing remote source: %s", sourceName)

	// Try to discover media from source
	media, err := m.discoverMedia(state.Source)

	m.mu.Lock()
	defer m.mu.Unlock()

	if err != nil {
		state.Status = "error"
		state.Error = err.Error()
		return err
	}

	state.Media = media
	state.MediaCount = len(media)
	state.LastSync = time.Now()
	state.Status = "synced"
	state.Error = ""

	m.log.Info("Synced %d items from source: %s", len(media), sourceName)
	return nil
}

// discoverMedia discovers media files from a remote source
func (m *Module) discoverMedia(source config.RemoteSource) ([]*MediaItem, error) {
	// Validate URL against SSRF before making the request
	if err := validateURL(source.URL); err != nil {
		return nil, fmt.Errorf("SSRF check failed for source %s: %w", source.Name, err)
	}

	// Create request
	req, err := http.NewRequest("GET", source.URL, nil)
	if err != nil {
		return nil, err
	}

	// Add authentication
	if source.Username != "" && source.Password != "" {
		req.SetBasicAuth(source.Username, source.Password)
	}

	req.Header.Set(headerUserAgent, userAgentValue)
	req.Header.Set("Accept", "application/json, text/html, */*")

	resp, err := m.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			m.log.Warn(errCloseResponseFmt, err)
		}
	}()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d from source", resp.StatusCode)
	}

	// Try to parse as JSON first (if source provides an API)
	// Supports both array format [{"id":...},...] and wrapper format {"items":[...],"data":[...]}
	contentType := resp.Header.Get("Content-Type")
	if strings.Contains(contentType, "application/json") {
		// First, decode into raw JSON to detect structure
		var raw json.RawMessage
		if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
			return nil, fmt.Errorf("failed to parse JSON response: %w", err)
		}

		// Try to unmarshal as array first
		var items []*MediaItem
		if err := json.Unmarshal(raw, &items); err == nil {
			for _, item := range items {
				item.SourceName = source.Name
			}
			return items, nil
		}

		// Try common wrapper formats: {"items": [...]} or {"data": [...]}
		var wrapper map[string]json.RawMessage
		if err := json.Unmarshal(raw, &wrapper); err == nil {
			for _, key := range []string{"items", "data", "results", "media"} {
				if arrayData, ok := wrapper[key]; ok {
					if err := json.Unmarshal(arrayData, &items); err == nil {
						for _, item := range items {
							item.SourceName = source.Name
						}
						return items, nil
					}
				}
			}
		}

		return nil, fmt.Errorf("unsupported JSON format (expected array or {items/data/results/media:[...]} wrapper)")
	}

	// For non-JSON sources, create a single item for the URL
	item := &MediaItem{
		ID:         generateID(source.URL),
		Name:       filepath.Base(source.URL),
		Path:       source.URL,
		URL:        source.URL,
		SourceName: source.Name,
	}

	// Try to get size from Content-Length
	if cl := resp.Header.Get("Content-Length"); cl != "" {
		if size, err := strconv.ParseInt(cl, 10, 64); err == nil {
			item.Size = size
		}
	}

	item.ContentType = contentType

	return []*MediaItem{item}, nil
}

// GetSources returns all configured remote sources with their status
func (m *Module) GetSources() []*SourceState {
	m.mu.RLock()
	defer m.mu.RUnlock()

	sources := make([]*SourceState, 0, len(m.sources))
	for _, s := range m.sources {
		sources = append(sources, s)
	}
	return sources
}

// GetSourceMedia returns media from a specific source; triggers syncSource if cache is empty (concurrent callers may serialize).
func (m *Module) GetSourceMedia(sourceName string) ([]*MediaItem, error) {
	m.mu.RLock()
	state, exists := m.sources[sourceName]
	empty := exists && len(state.Media) == 0
	m.mu.RUnlock()

	if !exists {
		return nil, fmt.Errorf(errSourceNotFoundFmt, sourceName)
	}

	// Trigger live sync if cache is empty (server just started or first add)
	if empty {
		if err := m.syncSource(sourceName); err != nil {
			m.log.Warn("Live sync for %s failed: %v", sourceName, err)
		}
	}

	m.mu.RLock()
	defer m.mu.RUnlock()
	state, exists = m.sources[sourceName]
	if !exists {
		return nil, fmt.Errorf(errSourceNotFoundFmt, sourceName)
	}
	return state.Media, nil
}

// GetAllRemoteMedia returns all remote media from all sources
func (m *Module) GetAllRemoteMedia() []*MediaItem {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var all []*MediaItem
	for _, state := range m.sources {
		all = append(all, state.Media...)
	}
	return all
}

// StreamRemote streams a remote media file
func (m *Module) StreamRemote(w http.ResponseWriter, r *http.Request, remoteURL string, sourceName string) error {
	// Validate URL against SSRF before streaming
	if err := validateURL(remoteURL); err != nil {
		return fmt.Errorf("SSRF check failed: %w", err)
	}

	m.log.Debug("Streaming remote: %s from %s", remoteURL, sourceName)

	// Get source config for auth
	m.mu.RLock()
	state, exists := m.sources[sourceName]
	m.mu.RUnlock()

	var source config.RemoteSource
	if exists {
		source = state.Source
	}

	// Check cache first
	cfg := m.config.Get()
	if cfg.RemoteMedia.CacheEnabled {
		if cached := m.getCachedMedia(remoteURL); cached != nil {
			m.log.Debug("Serving from cache: %s", cached.LocalPath)
			http.ServeFile(w, r, cached.LocalPath)
			return nil
		}
	}

	// Create request to remote
	req, err := http.NewRequestWithContext(r.Context(), "GET", remoteURL, nil)
	if err != nil {
		return err
	}

	// Add authentication if available
	if source.Username != "" && source.Password != "" {
		req.SetBasicAuth(source.Username, source.Password)
	}

	// Forward range header for seeking
	if rangeHeader := r.Header.Get("Range"); rangeHeader != "" {
		req.Header.Set("Range", rangeHeader)
	}

	req.Header.Set(headerUserAgent, userAgentValue)

	resp, err := m.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to connect to remote: %w", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			m.log.Warn(errCloseResponseFmt, err)
		}
	}()

	// Copy only allowed response headers (allowlist to avoid leaking Server, X-Powered-By, etc.)
	for key, values := range resp.Header {
		if !allowedRemoteHeaders[http.CanonicalHeaderKey(key)] {
			continue
		}
		for _, value := range values {
			w.Header().Add(key, value)
		}
	}

	// Set status code
	w.WriteHeader(resp.StatusCode)

	// Stream the content
	_, err = io.Copy(w, resp.Body)
	return err
}

// ProxyRemoteWithCache streams and optionally caches the remote file.
// On a cache hit the file is served directly from disk. On a cache miss the
// response is streamed from the remote origin while the file is downloaded to
// the local cache directory in the background, so subsequent requests hit disk.
func (m *Module) ProxyRemoteWithCache(w http.ResponseWriter, r *http.Request, remoteURL, sourceName string) error {
	cfg := m.config.Get()

	// If caching disabled, just stream
	if !cfg.RemoteMedia.CacheEnabled {
		return m.StreamRemote(w, r, remoteURL, sourceName)
	}

	// Check cache
	cached := m.getCachedMedia(remoteURL)
	if cached != nil {
		m.mu.Lock()
		cached.LastAccess = time.Now()
		cached.Hits++
		m.mu.Unlock()

		http.ServeFile(w, r, cached.LocalPath)
		return nil
	}

	// Cache miss: stream to the client immediately, then cache in the background so
	// subsequent requests are served from disk. Bounded by cacheSem to prevent goroutine exhaustion.
	select {
	case m.cacheSem <- struct{}{}:
		go func() {
			defer func() { <-m.cacheSem }()
			if _, err := m.CacheMedia(remoteURL, sourceName); err != nil {
				m.log.Debug("Background cache failed for %s: %v", remoteURL, err)
			}
		}()
	default:
		m.log.Debug("Background cache skipped for %s: too many concurrent downloads", remoteURL)
	}
	return m.StreamRemote(w, r, remoteURL, sourceName)
}

// getCachedMedia returns cached media if available and not expired.
func (m *Module) getCachedMedia(remoteURL string) *CachedMedia {
	m.mu.RLock()
	cached, exists := m.mediaCache[remoteURL]
	if !exists {
		m.mu.RUnlock()
		return nil
	}

	// Verify file still exists
	if _, err := os.Stat(cached.LocalPath); os.IsNotExist(err) {
		m.mu.RUnlock()
		return nil
	}

	// Check TTL expiry based on last access time
	cfg := m.config.Get()
	if cfg.RemoteMedia.CacheTTL > 0 && time.Since(cached.LastAccess) > cfg.RemoteMedia.CacheTTL {
		m.mu.RUnlock()
		// Lazily evict expired entry — re-check under write lock to avoid races
		m.mu.Lock()
		if entry, ok := m.mediaCache[remoteURL]; ok && time.Since(entry.LastAccess) > cfg.RemoteMedia.CacheTTL {
			if err := os.Remove(entry.LocalPath); err != nil && !os.IsNotExist(err) {
				m.log.Warn("Failed to remove expired cache file %s: %v", entry.LocalPath, err)
			}
			delete(m.mediaCache, remoteURL)
		}
		m.mu.Unlock()
		return nil
	}

	m.mu.RUnlock()
	return cached
}

// CacheMedia downloads and caches a remote media file
func (m *Module) CacheMedia(remoteURL, sourceName string) (*CachedMedia, error) {
	// Validate URL against SSRF before downloading
	if err := validateURL(remoteURL); err != nil {
		return nil, fmt.Errorf("SSRF check failed: %w", err)
	}

	m.log.Info("Caching remote media: %s", remoteURL)

	// Get source config
	m.mu.RLock()
	state, exists := m.sources[sourceName]
	m.mu.RUnlock()

	var source config.RemoteSource
	if exists {
		source = state.Source
	}

	// Create request with context so it can be cancelled (e.g. on shutdown).
	req, err := http.NewRequestWithContext(context.Background(), "GET", remoteURL, nil)
	if err != nil {
		return nil, err
	}

	if source.Username != "" && source.Password != "" {
		req.SetBasicAuth(source.Username, source.Password)
	}

	req.Header.Set(headerUserAgent, userAgentValue)

	resp, err := m.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			m.log.Warn(errCloseResponseFmt, err)
		}
	}()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	// Cap single-file download size to avoid unbounded disk use (default 2GB).
	const maxSingleCacheFile = 2 * 1024 * 1024 * 1024
	if resp.ContentLength > 0 && resp.ContentLength > maxSingleCacheFile {
		return nil, fmt.Errorf("remote file too large: %d bytes (max %d)", resp.ContentLength, maxSingleCacheFile)
	}

	// Generate cache filename
	filename := generateCacheFilename(remoteURL)
	localPath := filepath.Join(m.cacheDir, filename)

	// Create cache file
	file, err := os.Create(localPath)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err := file.Close(); err != nil {
			m.log.Warn("Failed to close cache file: %v", err)
		}
	}()

	// Copy content (limited to maxSingleCacheFile to avoid unbounded disk use)
	size, err := io.Copy(file, io.LimitReader(resp.Body, maxSingleCacheFile))
	if err != nil {
		if removeErr := os.Remove(localPath); removeErr != nil {
			m.log.Warn("Failed to remove incomplete cache file %s: %v", localPath, removeErr)
		}
		return nil, err
	}

	cached := &CachedMedia{
		RemoteURL:   remoteURL,
		LocalPath:   localPath,
		Size:        size,
		ContentType: resp.Header.Get("Content-Type"),
		CachedAt:    time.Now(),
		LastAccess:  time.Now(),
	}

	m.mu.Lock()
	m.mediaCache[remoteURL] = cached
	m.mu.Unlock()

	// Trigger eviction if cache exceeds size limit or has expired entries
	m.CleanCache()

	m.log.Info("Cached %d bytes: %s", size, localPath)
	return cached, nil
}

// SyncSource triggers an immediate sync for a named source.
// This is the exported wrapper around the unexported syncSource method,
// intended for use by HTTP handlers.
func (m *Module) SyncSource(name string) error {
	return m.syncSource(name)
}

// AddSource adds a new remote source to the in-memory map.
// The CreateRemoteSource handler calls this and also persists the source to
// config via config.Manager.Update(), so runtime additions survive restarts.
func (m *Module) AddSource(source config.RemoteSource) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.sources[source.Name]; exists {
		return fmt.Errorf("source already exists: %s", source.Name)
	}

	m.sources[source.Name] = &SourceState{
		Source: source,
		Status: "pending",
	}

	return nil
}

// RemoveSource removes a remote source
func (m *Module) RemoveSource(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.sources[name]; !exists {
		return fmt.Errorf(errSourceNotFoundFmt, name)
	}

	delete(m.sources, name)
	return nil
}

// GetStats returns remote media statistics
func (m *Module) GetStats() Stats {
	m.mu.RLock()
	defer m.mu.RUnlock()

	stats := Stats{
		SourceCount:     len(m.sources),
		CachedItemCount: len(m.mediaCache),
		Sources:         make([]SourceStats, 0, len(m.sources)),
	}

	var totalMedia int
	var totalCacheSize int64

	for name, state := range m.sources {
		stats.Sources = append(stats.Sources, SourceStats{
			Name:       name,
			Status:     state.Status,
			MediaCount: state.MediaCount,
			LastSync:   state.LastSync,
			Error:      state.Error,
		})
		totalMedia += state.MediaCount
	}

	for _, cached := range m.mediaCache {
		totalCacheSize += cached.Size
	}

	stats.TotalMediaCount = totalMedia
	stats.CacheSize = totalCacheSize

	return stats
}

// Stats holds remote media statistics.
type Stats struct {
	SourceCount     int           `json:"source_count"`
	TotalMediaCount int           `json:"total_media_count"`
	CachedItemCount int           `json:"cached_item_count"`
	CacheSize       int64         `json:"cache_size"`
	Sources         []SourceStats `json:"sources"`
}

// SourceStats holds stats for a single source
type SourceStats struct {
	Name       string    `json:"name"`
	Status     string    `json:"status"`
	MediaCount int       `json:"media_count"`
	LastSync   time.Time `json:"last_sync"`
	Error      string    `json:"error,omitempty"`
}

// Cache management

func (m *Module) loadCacheIndex() {
	records, err := m.repo.List(context.Background())
	if err != nil {
		m.log.Warn("Failed to load cache index from DB: %v", err)
		return
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	for _, rec := range records {
		m.mediaCache[rec.RemoteURL] = &CachedMedia{
			RemoteURL:   rec.RemoteURL,
			LocalPath:   rec.LocalPath,
			Size:        rec.Size,
			ContentType: rec.ContentType,
			CachedAt:    rec.CachedAt,
			LastAccess:  rec.LastAccess,
			Hits:        rec.Hits,
		}
	}
}

func (m *Module) saveCacheIndex() {
	m.mu.RLock()
	defer m.mu.RUnlock()

	ctx := context.Background()
	for _, cached := range m.mediaCache {
		rec := &repositories.RemoteCacheRecord{
			RemoteURL:   cached.RemoteURL,
			LocalPath:   cached.LocalPath,
			Size:        cached.Size,
			ContentType: cached.ContentType,
			CachedAt:    cached.CachedAt,
			LastAccess:  cached.LastAccess,
			Hits:        cached.Hits,
		}
		if err := m.repo.Save(ctx, rec); err != nil {
			m.log.Warn("Failed to save cache entry: %v", err)
		}
	}
}

// cacheEntry pairs a URL key with its cached media for sorting.
type cacheEntry struct {
	url    string
	cached *CachedMedia
}

// CleanCache removes expired (TTL) and oversized (LRU) cache entries.
func (m *Module) CleanCache() int {
	cfg := m.config.Get()
	maxSize := cfg.RemoteMedia.CacheSize
	ttl := cfg.RemoteMedia.CacheTTL

	m.mu.Lock()
	defer m.mu.Unlock()

	removed := 0

	// Phase 1: evict TTL-expired entries
	if ttl > 0 {
		now := time.Now()
		for mediaURL, cached := range m.mediaCache {
			if now.Sub(cached.LastAccess) > ttl {
				if err := os.Remove(cached.LocalPath); err != nil && !os.IsNotExist(err) {
					m.log.Warn("Failed to remove expired cache file %s: %v", cached.LocalPath, err)
				}
				delete(m.mediaCache, mediaURL)
				removed++
			}
		}
	}

	// Phase 2: LRU eviction if over size limit (skip when maxSize <= 0 = no limit).
	var currentSize int64
	for _, cached := range m.mediaCache {
		currentSize += cached.Size
	}
	if maxSize <= 0 || currentSize <= maxSize {
		if removed > 0 {
			m.log.Info("Cleaned %d expired cache items", removed)
		}
		return removed
	}

	// Sort remaining entries by LastAccess (oldest first)
	entries := make([]cacheEntry, 0, len(m.mediaCache))
	for mediaURL, cached := range m.mediaCache {
		entries = append(entries, cacheEntry{url: mediaURL, cached: cached})
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].cached.LastAccess.Before(entries[j].cached.LastAccess)
	})

	for _, entry := range entries {
		if currentSize <= maxSize {
			break
		}
		if err := os.Remove(entry.cached.LocalPath); err != nil && !os.IsNotExist(err) {
			m.log.Warn("Failed to remove cached file %s: %v", entry.cached.LocalPath, err)
		}
		currentSize -= entry.cached.Size
		delete(m.mediaCache, entry.url)
		removed++
	}

	m.log.Info("Cleaned %d cached items (TTL + LRU)", removed)
	return removed
}

// validateURL checks that the given URL uses http/https and does not point to
// a private, loopback, or link-local address. This prevents SSRF when the
// remote module fetches user-configured source URLs.
func validateURL(rawURL string) error {
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}

	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("unsupported URL scheme: %s", u.Scheme)
	}

	host := u.Hostname()
	if host == "" {
		return fmt.Errorf("URL has no host")
	}

	// Resolve hostname to IPs and check each one
	ips, err := net.LookupIP(host)
	if err != nil {
		// If DNS lookup fails, try parsing as literal IP
		ip := net.ParseIP(host)
		if ip == nil {
			return fmt.Errorf("cannot resolve host: %s", host)
		}
		ips = []net.IP{ip}
	}

	for _, ip := range ips {
		if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified() {
			return fmt.Errorf("URL resolves to private/loopback address: %s", ip)
		}
	}

	return nil
}

// Helper functions

// generateID returns a collision-resistant 16-char hex ID from the input (e.g. URL).
func generateID(input string) string {
	sum := sha256.Sum256([]byte(input))
	return hex.EncodeToString(sum[:8])
}

func generateCacheFilename(remoteURL string) string {
	u, err := url.Parse(remoteURL)
	if err != nil {
		return generateID(remoteURL)
	}

	name := filepath.Base(u.Path)
	if name == "" || name == "/" {
		name = generateID(remoteURL)
	}

	return fmt.Sprintf("%s_%s", generateID(remoteURL), name)
}
