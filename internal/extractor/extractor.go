// Package extractor provides HLS stream proxy capabilities.
// It accepts M3U8 playlist URLs and proxies HLS streams to users
// as if they were native media items in the library.
package extractor

import (
	"bufio"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
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

// ErrNotFound is returned when an extractor item does not exist.
var ErrNotFound = errors.New("extractor item not found")

// playlistCacheTTL is the maximum age of a cached HLS playlist entry.
// After this duration the entry is treated as a cache miss and the upstream
// playlist is re-fetched so stale variant/segment URLs are refreshed.
const playlistCacheTTL = 5 * time.Minute

// Module handles HLS stream proxying for external M3U8 URLs.
type Module struct {
	config     *config.Manager
	log        *logger.Logger
	dbModule   *database.Module
	repo       repositories.ExtractorItemRepository
	httpClient *http.Client

	mu    sync.RWMutex
	items map[string]*ExtractedItem // keyed by item ID

	// HLS playlist cache: maps "itemID:qualityIdx" -> parsed playlist info
	playlistCache sync.Map

	healthMu  sync.RWMutex
	healthy   bool
	healthMsg string
}

// ExtractedItem is the in-memory representation of a proxied stream.
type ExtractedItem struct {
	ID           string    `json:"id"`
	Title        string    `json:"title"`
	StreamURL    string    `json:"stream_url"` // M3U8 playlist URL
	AddedBy      string    `json:"added_by"`
	Status       string    `json:"status"` // "active" or "error"
	ErrorMessage string    `json:"error_message,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
}

// cachedPlaylist stores parsed HLS playlist data.
type cachedPlaylist struct {
	variants  []playlistVariant // for master playlists
	segments  []playlistSegment // for variant playlists
	baseURL   string
	fetchedAt time.Time
}

type playlistVariant struct {
	originalURL string
	info        string // the full #EXT-X-STREAM-INF line
}

type playlistSegment struct {
	originalURL string
	filename    string
}

// Stats holds extractor statistics.
type Stats struct {
	TotalItems  int `json:"total_items"`
	ActiveItems int `json:"active_items"`
	ErrorItems  int `json:"error_items"`
}

// NewModule creates a new extractor module.
func NewModule(cfg *config.Manager, dbModule *database.Module) *Module {
	return &Module{
		config:   cfg,
		log:      logger.New("extractor"),
		dbModule: dbModule,
		httpClient: &http.Client{
			Transport: helpers.SafeHTTPTransport(),
			Timeout:   30 * time.Second,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				if len(via) >= 10 {
					return fmt.Errorf("too many redirects")
				}
				return nil
			},
		},
		items: make(map[string]*ExtractedItem),
	}
}

func (m *Module) Name() string { return "extractor" }

func (m *Module) Start(_ context.Context) error {
	m.log.Info("Starting extractor module...")

	cfg := m.config.Get()
	if !cfg.Extractor.Enabled {
		m.log.Info("Extractor is disabled")
		m.setHealth(true, "Disabled")
		return nil
	}

	m.repo = mysqlrepo.NewExtractorItemRepository(m.dbModule.GORM())

	// Load items from DB
	records, err := m.repo.List(context.Background())
	if err != nil {
		m.log.Warn("Failed to load extractor items from DB: %v", err)
	} else {
		m.mu.Lock()
		for _, rec := range records {
			m.items[rec.ID] = recordToItem(rec)
		}
		m.mu.Unlock()
		m.log.Info("Loaded %d extractor items from DB", len(records))
	}

	m.setHealth(true, fmt.Sprintf("Running with %d items", len(m.items)))
	m.log.Info("Extractor module started")
	return nil
}

func (m *Module) Stop(_ context.Context) error {
	m.log.Info("Stopping extractor module...")
	// Clear playlist cache so stale entries do not hold references to upstream URLs.
	m.playlistCache.Range(func(key, _ interface{}) bool {
		m.playlistCache.Delete(key)
		return true
	})
	m.setHealth(false, "Stopped")
	return nil
}

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

func (m *Module) setHealth(healthy bool, msg string) {
	m.healthMu.Lock()
	m.healthy = healthy
	m.healthMsg = msg
	m.healthMu.Unlock()
}

// --- Public API ---

// AddItem adds an M3U8 stream URL to the library.
func (m *Module) AddItem(streamURL, title, addedBy string) (*ExtractedItem, error) {
	cfg := m.config.Get()

	// Check max items limit
	m.mu.RLock()
	count := len(m.items)
	m.mu.RUnlock()
	if cfg.Extractor.MaxItems > 0 && count >= cfg.Extractor.MaxItems {
		return nil, fmt.Errorf("maximum extracted items limit reached (%d)", cfg.Extractor.MaxItems)
	}

	// Validate that the URL looks like an M3U8 playlist
	u, err := url.Parse(streamURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return nil, fmt.Errorf("invalid URL: must be an HTTP/HTTPS URL")
	}

	// Generate deterministic ID from stream URL
	id := generateID(streamURL)

	if title == "" {
		title = path.Base(u.Path)
		if title == "" || title == "/" || title == "." {
			title = u.Host
		}
	}

	now := time.Now()
	item := &ExtractedItem{
		ID:        id,
		Title:     title,
		StreamURL: streamURL,
		Status:    "active",
		AddedBy:   addedBy,
		CreatedAt: now,
	}

	// SSRF: validate URL before fetching (SafeHTTPTransport also blocks private IPs at connect time)
	if err := helpers.ValidateURLForSSRF(streamURL); err != nil {
		item.Status = "error"
		item.ErrorMessage = fmt.Sprintf("Invalid URL: %v", err)
		m.log.Warn("M3U8 URL rejected: %s — %v", streamURL, err)
		return item, nil
	}
	// Verify the M3U8 URL is reachable (outside the lock — can be slow)
	if _, _, err := m.fetchURL(context.Background(), streamURL); err != nil {
		item.Status = "error"
		item.ErrorMessage = fmt.Sprintf("Failed to fetch M3U8: %v", err)
		m.log.Warn("M3U8 URL unreachable: %s — %v", streamURL, err)
	}

	// Acquire the write lock to enforce the max-items limit and add the item
	// atomically.  Checking the count under RLock and adding under Lock would
	// be a TOCTOU race: two concurrent callers could both pass the check and
	// both add, exceeding the configured limit.
	m.mu.Lock()
	if cfg.Extractor.MaxItems > 0 && len(m.items) >= cfg.Extractor.MaxItems {
		// Don't count an update to an existing item against the limit.
		if _, exists := m.items[id]; !exists {
			m.mu.Unlock()
			return nil, fmt.Errorf("maximum extracted items limit reached (%d)", cfg.Extractor.MaxItems)
		}
	}
	m.items[id] = item
	m.mu.Unlock()

	// Save to DB
	if m.repo != nil {
		rec := itemToRecord(item)
		if err := m.repo.Upsert(context.Background(), rec); err != nil {
			m.log.Warn("Failed to save extractor item to DB: %v", err)
		}
	}

	m.log.Info("Added extractor item: %s -> %s", item.Title, item.StreamURL)
	return item, nil
}

// RemoveItem removes a proxied stream from the library.
func (m *Module) RemoveItem(id string) error {
	m.mu.Lock()
	_, existed := m.items[id]
	delete(m.items, id)
	m.mu.Unlock()
	if !existed {
		return ErrNotFound
	}

	if m.repo != nil {
		if err := m.repo.Delete(context.Background(), id); err != nil {
			m.log.Warn("Failed to delete extractor item from DB: %v", err)
		}
	}

	// Clear any cached playlists for this item (master + all variant qualities)
	prefix := id + ":"
	m.playlistCache.Range(func(key, _ interface{}) bool {
		if k, ok := key.(string); ok && len(k) >= len(prefix) && k[:len(prefix)] == prefix {
			m.playlistCache.Delete(key)
		}
		return true
	})

	return nil
}

// GetItem returns a proxied stream by ID.
func (m *Module) GetItem(id string) *ExtractedItem {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.items[id]
}

// GetAllItems returns all proxied streams.
func (m *Module) GetAllItems() []*ExtractedItem {
	m.mu.RLock()
	defer m.mu.RUnlock()

	items := make([]*ExtractedItem, 0, len(m.items))
	for _, item := range m.items {
		items = append(items, item)
	}
	return items
}

// GetStats returns extractor statistics.
func (m *Module) GetStats() Stats {
	m.mu.RLock()
	defer m.mu.RUnlock()

	stats := Stats{
		TotalItems: len(m.items),
	}

	for _, item := range m.items {
		switch item.Status {
		case "active":
			stats.ActiveItems++
		case "error":
			stats.ErrorItems++
		}
	}

	return stats
}

// --- HLS Proxy ---

// ProxyHLSMaster fetches the upstream master M3U8 playlist and rewrites variant
// URLs to route through MSP's HLS proxy endpoints.
func (m *Module) ProxyHLSMaster(w http.ResponseWriter, r *http.Request, itemID string) error {
	item := m.GetItem(itemID)
	if item == nil {
		return fmt.Errorf("item not found: %s", itemID)
	}
	if item.Status != "active" {
		return fmt.Errorf("item is not active: %s", item.Status)
	}

	// Fetch the upstream master playlist
	playlistBody, playlistURL, err := m.fetchURL(r.Context(), item.StreamURL)
	if err != nil {
		return fmt.Errorf("failed to fetch master playlist: %w", err)
	}

	baseURL := resolveBaseURL(playlistURL)

	// Parse and rewrite the master playlist
	rewritten, variants := m.rewriteMasterPlaylist(playlistBody, baseURL, itemID)

	// Cache the variant mapping
	m.playlistCache.Store(itemID+":master", &cachedPlaylist{
		variants:  variants,
		baseURL:   baseURL,
		fetchedAt: time.Now(),
	})

	w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(rewritten))
	return nil
}

// ProxyHLSVariant fetches an upstream variant playlist and rewrites segment URLs.
func (m *Module) ProxyHLSVariant(w http.ResponseWriter, r *http.Request, itemID string, qualityIdx int) error {
	// Look up the variant URL from the cached master.
	// Treat the entry as a miss if it is older than playlistCacheTTL so that
	// rotated CDN variant URLs are refreshed automatically.
	cached, ok := m.playlistCache.Load(itemID + ":master")
	if ok {
		if cp, _ := cached.(*cachedPlaylist); cp != nil && time.Since(cp.fetchedAt) > playlistCacheTTL {
			ok = false
		}
	}
	if !ok {
		// Try to re-fetch the master first
		item := m.GetItem(itemID)
		if item == nil {
			return fmt.Errorf("item not found: %s", itemID)
		}
		playlistBody, playlistURL, err := m.fetchURL(r.Context(), item.StreamURL)
		if err != nil {
			return fmt.Errorf("failed to fetch master for variant lookup: %w", err)
		}
		baseURL := resolveBaseURL(playlistURL)
		_, variants := m.rewriteMasterPlaylist(playlistBody, baseURL, itemID)
		cp := &cachedPlaylist{variants: variants, baseURL: baseURL, fetchedAt: time.Now()}
		m.playlistCache.Store(itemID+":master", cp)
		cached = cp
	}

	master := cached.(*cachedPlaylist)
	if qualityIdx < 0 || qualityIdx >= len(master.variants) {
		// It's possible this is a media playlist directly, not a master.
		return m.proxyMediaPlaylist(r.Context(), w, r, itemID, qualityIdx)
	}

	variantURL := master.variants[qualityIdx].originalURL

	// Fetch the variant playlist
	playlistBody, playlistURL, err := m.fetchURL(r.Context(), variantURL)
	if err != nil {
		return fmt.Errorf("failed to fetch variant playlist: %w", err)
	}

	baseURL := resolveBaseURL(playlistURL)

	// Rewrite segment URLs
	rewritten, segments := m.rewriteVariantPlaylist(playlistBody, baseURL, itemID, qualityIdx)

	// Cache segments
	m.playlistCache.Store(fmt.Sprintf("%s:%d", itemID, qualityIdx), &cachedPlaylist{
		segments:  segments,
		baseURL:   baseURL,
		fetchedAt: time.Now(),
	})

	w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(rewritten))
	return nil
}

// ProxyHLSSegment proxies a single HLS .ts segment from the upstream CDN.
func (m *Module) ProxyHLSSegment(w http.ResponseWriter, r *http.Request, itemID string, qualityIdx int, segment string) error {
	cacheKey := fmt.Sprintf("%s:%d", itemID, qualityIdx)
	cached, ok := m.playlistCache.Load(cacheKey)
	if ok {
		// A stale segment cache means the variant playlist has not been
		// re-fetched recently.  Return 404 so the HLS client re-requests
		// the variant playlist (which will refresh the segment list) before
		// asking for segments again.
		if cp, _ := cached.(*cachedPlaylist); cp != nil && time.Since(cp.fetchedAt) > playlistCacheTTL {
			ok = false
		}
	}
	if !ok {
		return fmt.Errorf("segment cache not found for %s quality %d", itemID, qualityIdx)
	}

	playlist := cached.(*cachedPlaylist)

	// Find the segment URL
	var segmentURL string
	for _, seg := range playlist.segments {
		if seg.filename == segment {
			segmentURL = seg.originalURL
			break
		}
	}

	if segmentURL == "" {
		// Try resolving relative to the playlist base URL
		segmentURL = resolveURL(playlist.baseURL, segment)
	}

	// Proxy the segment
	return m.proxyStream(w, r, segmentURL, "video/MP2T")
}

// --- Internal helpers ---

func (m *Module) proxyMediaPlaylist(ctx context.Context, w http.ResponseWriter, _ *http.Request, itemID string, qualityIdx int) error {
	item := m.GetItem(itemID)
	if item == nil {
		return fmt.Errorf("item not found: %s", itemID)
	}

	playlistBody, playlistURL, err := m.fetchURL(ctx, item.StreamURL)
	if err != nil {
		return fmt.Errorf("failed to fetch media playlist: %w", err)
	}

	baseURL := resolveBaseURL(playlistURL)
	rewritten, segments := m.rewriteVariantPlaylist(playlistBody, baseURL, itemID, qualityIdx)

	m.playlistCache.Store(fmt.Sprintf("%s:%d", itemID, qualityIdx), &cachedPlaylist{
		segments:  segments,
		baseURL:   baseURL,
		fetchedAt: time.Now(),
	})

	w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(rewritten))
	return nil
}

func (m *Module) proxyStream(w http.ResponseWriter, r *http.Request, targetURL, contentType string) error {
	cfg := m.config.Get()

	req, err := http.NewRequestWithContext(r.Context(), "GET", targetURL, nil)
	if err != nil {
		return fmt.Errorf("failed to create proxy request: %w", err)
	}

	// Forward range header for seeking
	if rangeHeader := r.Header.Get("Range"); rangeHeader != "" {
		req.Header.Set("Range", rangeHeader)
	}

	req.Header.Set("User-Agent", "MediaServerPro/4.0")

	proxyTimeout := cfg.Extractor.ProxyTimeout
	if proxyTimeout <= 0 {
		proxyTimeout = 30 * time.Second
	}
	client := &http.Client{Transport: m.httpClient.Transport, Timeout: proxyTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("proxy request failed: %w", err)
	}
	defer resp.Body.Close()

	// Copy only media-relevant headers (allowlist avoids leaking CDN/server infra).
	allowedProxyHeaders := map[string]bool{
		"Content-Type": true, "Content-Length": true, "Content-Range": true,
		"Content-Disposition": true, "Accept-Ranges": true,
		"Last-Modified": true, "Etag": true, "Cache-Control": true,
	}
	for key, values := range resp.Header {
		if allowedProxyHeaders[http.CanonicalHeaderKey(key)] {
			for _, value := range values {
				w.Header().Add(key, value)
			}
		}
	}

	// Ensure content type is set
	if w.Header().Get("Content-Type") == "" {
		w.Header().Set("Content-Type", contentType)
	}
	w.Header().Set("Access-Control-Allow-Origin", "*")

	w.WriteHeader(resp.StatusCode)
	if _, err := io.Copy(w, resp.Body); err != nil {
		return fmt.Errorf("proxy copy failed: %w", err)
	}
	return nil
}

func (m *Module) fetchURL(ctx context.Context, rawURL string) (string, string, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", rawURL, nil)
	if err != nil {
		return "", "", err
	}
	req.Header.Set("User-Agent", "MediaServerPro/4.0")

	resp, err := m.httpClient.Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("HTTP %d fetching %s", resp.StatusCode, rawURL)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 10*1024*1024)) // 10MB limit
	if err != nil {
		return "", "", err
	}

	// Use the final URL after redirects
	finalURL := resp.Request.URL.String()
	return string(body), finalURL, nil
}

func (m *Module) rewriteMasterPlaylist(body, baseURL, itemID string) (string, []playlistVariant) {
	var variants []playlistVariant
	var result strings.Builder
	qualityIdx := 0

	scanner := bufio.NewScanner(strings.NewReader(body))
	prevLineIsStreamInf := false
	prevStreamInf := ""

	for scanner.Scan() {
		line := scanner.Text()

		if strings.HasPrefix(line, "#EXT-X-STREAM-INF:") {
			prevLineIsStreamInf = true
			prevStreamInf = line
			result.WriteString(line)
			result.WriteString("\n")
			continue
		}

		if prevLineIsStreamInf {
			prevLineIsStreamInf = false
			variantURL := resolveURL(baseURL, strings.TrimSpace(line))
			variants = append(variants, playlistVariant{
				originalURL: variantURL,
				info:        prevStreamInf,
			})
			result.WriteString(fmt.Sprintf("/extractor/hls/%s/%d/playlist.m3u8\n", itemID, qualityIdx))
			qualityIdx++
			continue
		}

		result.WriteString(line)
		result.WriteString("\n")
	}

	// If no variants were found, this might be a media playlist, not a master.
	// Return as-is with a single "variant" pointing to itself.
	if len(variants) == 0 {
		item := m.GetItem(itemID)
		if item != nil {
			variants = append(variants, playlistVariant{
				originalURL: item.StreamURL,
			})
		}
	}

	return result.String(), variants
}

func (m *Module) rewriteVariantPlaylist(body, baseURL, itemID string, qualityIdx int) (string, []playlistSegment) {
	var segments []playlistSegment
	var result strings.Builder

	scanner := bufio.NewScanner(strings.NewReader(body))
	for scanner.Scan() {
		line := scanner.Text()

		if strings.TrimSpace(line) == "" {
			result.WriteString("\n")
			continue
		}

		if strings.HasPrefix(line, "#") {
			result.WriteString(line)
			result.WriteString("\n")
			continue
		}

		// This is a segment URI line
		segmentURL := resolveURL(baseURL, strings.TrimSpace(line))
		filename := extractSegmentFilename(line)

		segments = append(segments, playlistSegment{
			originalURL: segmentURL,
			filename:    filename,
		})

		result.WriteString(fmt.Sprintf("/extractor/hls/%s/%d/%s\n", itemID, qualityIdx, url.PathEscape(filename)))
	}

	return result.String(), segments
}

// --- Utility functions ---

func generateID(streamURL string) string {
	h := sha256.Sum256([]byte(streamURL))
	return fmt.Sprintf("ext_%x", h[:12])
}

func resolveBaseURL(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}
	u.Path = path.Dir(u.Path) + "/"
	u.RawQuery = ""
	u.Fragment = ""
	return u.String()
}

func resolveURL(baseURL, ref string) string {
	if strings.HasPrefix(ref, "http://") || strings.HasPrefix(ref, "https://") {
		return ref
	}
	base, err := url.Parse(baseURL)
	if err != nil {
		return ref
	}
	refURL, err := url.Parse(ref)
	if err != nil {
		return ref
	}
	return base.ResolveReference(refURL).String()
}

func extractSegmentFilename(uri string) string {
	u, err := url.Parse(uri)
	if err != nil {
		return path.Base(uri)
	}
	name := path.Base(u.Path)
	if name == "" || name == "/" || name == "." {
		h := sha256.Sum256([]byte(uri))
		return fmt.Sprintf("seg_%x.ts", h[:8])
	}
	return name
}

func recordToItem(rec *repositories.ExtractorItemRecord) *ExtractedItem {
	return &ExtractedItem{
		ID:           rec.ID,
		Title:        rec.Title,
		StreamURL:    rec.StreamURL,
		Status:       rec.Status,
		ErrorMessage: rec.ErrorMessage,
		AddedBy:      rec.AddedBy,
		CreatedAt:    rec.CreatedAt,
	}
}

func itemToRecord(item *ExtractedItem) *repositories.ExtractorItemRecord {
	return &repositories.ExtractorItemRecord{
		ID:           item.ID,
		SourceURL:    item.StreamURL, // source_url = stream_url (the M3U8 URL itself)
		Title:        item.Title,
		StreamURL:    item.StreamURL,
		StreamType:   "hls",
		Status:       item.Status,
		ErrorMessage: item.ErrorMessage,
		AddedBy:      item.AddedBy,
		ResolvedAt:   item.CreatedAt,
		CreatedAt:    item.CreatedAt,
		UpdatedAt:    time.Now(),
	}
}
