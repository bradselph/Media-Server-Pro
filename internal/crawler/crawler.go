// Package crawler discovers M3U8 streams on target sites by crawling page
// links and probing each for HLS playlists. Discovered streams are stored
// for admin review; approved items are added to the extractor for proxy playback.
package crawler

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"media-server-pro/internal/config"
	"media-server-pro/internal/database"
	"media-server-pro/internal/extractor"
	"media-server-pro/internal/logger"
	"media-server-pro/internal/repositories"
	mysqlrepo "media-server-pro/internal/repositories/mysql"
	"media-server-pro/pkg/helpers"
	"media-server-pro/pkg/models"
)

// Module handles stream discovery by crawling target sites.
type Module struct {
	config    *config.Manager
	log       *logger.Logger
	dbModule  *database.Module
	extractor *extractor.Module

	targetRepo    repositories.CrawlerTargetRepository
	discoveryRepo repositories.CrawlerDiscoveryRepository

	httpClient *http.Client
	browser    *browserDetector

	crawlMu   sync.RWMutex
	crawling  bool
	healthMu  sync.RWMutex
	healthy   bool
	healthMsg string
}

// CrawlTarget is the public representation of a crawl target.
type CrawlTarget struct {
	ID          string     `json:"id"`
	Name        string     `json:"name"`
	URL         string     `json:"url"`
	Site        string     `json:"site"`
	Enabled     bool       `json:"enabled"`
	LastCrawled *time.Time `json:"last_crawled,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
}

// Discovery is the public representation of a discovered stream.
type Discovery struct {
	ID           string     `json:"id"`
	TargetID     string     `json:"target_id"`
	PageURL      string     `json:"page_url"`
	Title        string     `json:"title"`
	StreamURL    string     `json:"stream_url"`
	StreamType   string     `json:"stream_type"`
	Quality      int        `json:"quality"`
	Status       string     `json:"status"` // "pending", "added", "ignored"
	ReviewedBy   string     `json:"reviewed_by,omitempty"`
	ReviewedAt   *time.Time `json:"reviewed_at,omitempty"`
	DiscoveredAt time.Time  `json:"discovered_at"`
}

// Stats holds crawler statistics.
type Stats struct {
	TotalTargets       int  `json:"total_targets"`
	EnabledTargets     int  `json:"enabled_targets"`
	TotalDiscoveries   int  `json:"total_discoveries"`
	PendingDiscoveries int  `json:"pending_discoveries"`
	Crawling           bool `json:"crawling"`
}

// NewModule creates a new crawler module.
func NewModule(cfg *config.Manager, dbModule *database.Module, extractorModule *extractor.Module) *Module {
	log := logger.New("crawler")
	crawlCfg := cfg.Get().Crawler
	crawlTimeout := crawlCfg.CrawlTimeout
	if crawlTimeout <= 0 {
		crawlTimeout = 60 * time.Second
	}
	var bd *browserDetector
	if crawlCfg.BrowserEnabled {
		bd = newBrowserDetector(log, crawlTimeout)
		if bd.available() {
			log.Info("Browser detection enabled (Chrome: %s)", bd.chromeBin)
		} else {
			log.Warn("No Chrome/Chromium found — browser detection disabled, falling back to HTML-only mode")
		}
	} else {
		log.Info("Browser detection disabled by config")
	}
	return &Module{
		config:    cfg,
		log:       log,
		dbModule:  dbModule,
		extractor: extractorModule,
		browser:   bd,
		httpClient: &http.Client{
			Transport: helpers.SafeHTTPTransport(),
			Timeout:   30 * time.Second,
			CheckRedirect: func(_ *http.Request, via []*http.Request) error {
				if len(via) >= 10 {
					return fmt.Errorf("too many redirects")
				}
				return nil
			},
		},
	}
}

func (m *Module) Name() string { return "crawler" }

func (m *Module) Start(_ context.Context) error {
	m.log.Info("Starting crawler module...")

	cfg := m.config.Get()
	if !cfg.Crawler.Enabled {
		m.log.Info("Crawler is disabled")
		m.setHealth(true, "Disabled")
		return nil
	}

	m.targetRepo = mysqlrepo.NewCrawlerTargetRepository(m.dbModule.GORM())
	m.discoveryRepo = mysqlrepo.NewCrawlerDiscoveryRepository(m.dbModule.GORM())

	m.setHealth(true, "Running")
	m.log.Info("Crawler module started")
	return nil
}

func (m *Module) Stop(_ context.Context) error {
	m.log.Info("Stopping crawler module...")
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

var errDisabled = fmt.Errorf("crawler is disabled")

// --- Target Management ---

// AddTarget adds a new crawl target.
func (m *Module) AddTarget(name, rawURL string) (*CrawlTarget, error) {
	if m.targetRepo == nil {
		return nil, errDisabled
	}
	u, err := url.Parse(rawURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return nil, fmt.Errorf("invalid URL: must be HTTP/HTTPS")
	}

	id := generateTargetID(rawURL)
	site := u.Hostname()
	if name == "" {
		name = site
	}

	now := time.Now()
	rec := &repositories.CrawlerTargetRecord{
		ID:        id,
		Name:      name,
		URL:       rawURL,
		Site:      site,
		Enabled:   true,
		CreatedAt: now,
		UpdatedAt: now,
	}

	if err := m.targetRepo.Upsert(context.Background(), rec); err != nil {
		return nil, fmt.Errorf("failed to save target: %w", err)
	}

	m.log.Info("Added crawler target: %s -> %s", name, rawURL)
	return recordToTarget(rec), nil
}

// RemoveTarget removes a crawl target and its discoveries.
func (m *Module) RemoveTarget(id string) error {
	if m.targetRepo == nil {
		return errDisabled
	}
	return m.targetRepo.Delete(context.Background(), id)
}

// GetTargets returns all crawl targets.
func (m *Module) GetTargets() ([]*CrawlTarget, error) {
	if m.targetRepo == nil {
		return nil, errDisabled
	}
	records, err := m.targetRepo.List(context.Background())
	if err != nil {
		return nil, err
	}
	targets := make([]*CrawlTarget, len(records))
	for i, rec := range records {
		targets[i] = recordToTarget(rec)
	}
	return targets, nil
}

// ToggleTarget enables/disables a target.
func (m *Module) ToggleTarget(id string, enabled bool) error {
	if m.targetRepo == nil {
		return errDisabled
	}
	rec, err := m.targetRepo.Get(context.Background(), id)
	if err != nil {
		return err
	}
	if rec == nil {
		return fmt.Errorf("target not found: %s", id)
	}
	rec.Enabled = enabled
	rec.UpdatedAt = time.Now()
	return m.targetRepo.Upsert(context.Background(), rec)
}

// --- Crawling ---

// Regex patterns for extracting links and M3U8 URLs from HTML
var (
	hrefRegex  = regexp.MustCompile(`href=["']([^"']+)["']`)
	m3u8Regex  = regexp.MustCompile(`https?://[^\s"'<>]+\.m3u8[^\s"'<>]*`)
	titleRegex = regexp.MustCompile(`<title[^>]*>([^<]+)</title>`)
	// Patterns that indicate a page link is likely a video/content page
	contentPathPatterns = []*regexp.Regexp{
		regexp.MustCompile(`(?i)/view_video`), // PornHub
		regexp.MustCompile(`(?i)/video\d+`),   // XVideos
		regexp.MustCompile(`(?i)/watch`),      // YouTube, YouPorn
		regexp.MustCompile(`(?i)/embed`),
		regexp.MustCompile(`(?i)/video/`),
		regexp.MustCompile(`(?i)/v/`),
		regexp.MustCompile(`(?i)/videos/`),
	}
	// Patterns to skip (non-content pages)
	skipPathPatterns = []*regexp.Regexp{
		regexp.MustCompile(`(?i)^/(categories|tags|channels|pornstars|users|search|login|signup|upload|about|contact|dmca|terms|privacy|2257)`),
		regexp.MustCompile(`(?i)\.(jpg|jpeg|png|gif|css|js|ico|svg|woff|ttf)$`),
	}
)

// CrawlTarget triggers a crawl for a specific target.
func (m *Module) CrawlTarget(ctx context.Context, targetID string) (int, error) {
	if m.targetRepo == nil {
		return 0, errDisabled
	}
	m.crawlMu.Lock()
	if m.crawling {
		m.crawlMu.Unlock()
		return 0, fmt.Errorf("a crawl is already in progress")
	}
	m.crawling = true
	m.crawlMu.Unlock()

	defer func() {
		m.crawlMu.Lock()
		m.crawling = false
		m.crawlMu.Unlock()
	}()

	target, err := m.targetRepo.Get(ctx, targetID)
	if err != nil {
		return 0, err
	}
	if target == nil {
		return 0, fmt.Errorf("target not found: %s", targetID)
	}

	return m.doCrawl(ctx, target)
}

func (m *Module) doCrawl(ctx context.Context, target *repositories.CrawlerTargetRecord) (int, error) {
	cfg := m.config.Get()
	maxPages := cfg.Crawler.MaxPages
	if maxPages <= 0 {
		maxPages = 20
	}

	m.log.Info("Starting crawl of %s (%s), max %d pages", target.Name, target.URL, maxPages)

	targetURL, err := url.Parse(target.URL)
	if err != nil {
		return 0, fmt.Errorf("invalid target URL: %w", err)
	}

	pageBody, err := m.fetchPage(ctx, target.URL)
	if err != nil {
		return 0, fmt.Errorf("failed to fetch target page: %w", err)
	}

	contentLinks := m.extractContentLinks(pageBody, targetURL)
	m.log.Info("Found %d content links on %s", len(contentLinks), target.URL)

	if len(contentLinks) > maxPages {
		contentLinks = contentLinks[:maxPages]
	}

	newCount := 0
	for i, link := range contentLinks {
		if err := ctx.Err(); err != nil {
			return newCount, fmt.Errorf("crawl canceled: %w", err)
		}
		m.log.Debug("Probing [%d/%d]: %s", i+1, len(contentLinks), link)

		streams, title := m.probeForStreams(ctx, link)
		if len(streams) == 0 {
			continue
		}

		if title == "" {
			title = extractTitleFromURL(link)
		}

		for _, stream := range streams {
			if stream.Type != "m3u8" {
				continue
			}

			streamURL := stream.URL

			exists, err := m.discoveryRepo.ExistsByStreamURL(ctx, streamURL)
			if err != nil {
				m.log.Warn("Failed to check discovery existence: %v", err)
				continue
			}
			if exists {
				continue
			}

			id := generateDiscoveryID(streamURL)
			rec := &repositories.CrawlerDiscoveryRecord{
				ID:              id,
				TargetID:        target.ID,
				PageURL:         link,
				Title:           title,
				StreamURL:       streamURL,
				StreamType:      "hls",
				Quality:         stream.Quality,
				DetectionMethod: stream.DetectionMethod,
				Status:          "pending",
				DiscoveredAt:    time.Now(),
			}

			if err := m.discoveryRepo.Create(ctx, rec); err != nil {
				m.log.Warn("Failed to store discovery: %v", err)
				continue
			}
			newCount++
			m.log.Info("Discovered M3U8 [%s]: %s -> %s", stream.DetectionMethod, title, streamURL)
			break
		}
	}

	now := time.Now()
	if err := m.targetRepo.UpdateLastCrawled(ctx, target.ID, now); err != nil {
		m.log.Warn("Failed to update last crawled: %v", err)
	}

	m.log.Info("Crawl of %s complete: %d new M3U8 streams found", target.Name, newCount)
	return newCount, nil
}

// fetchPage fetches a page's HTML body.
func (m *Module) fetchPage(ctx context.Context, rawURL string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, http.NoBody)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")

	resp, err := m.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 5*1024*1024)) // 5MB limit
	if err != nil {
		return "", err
	}

	return string(body), nil
}

// extractContentLinks finds content page links from HTML.
func (m *Module) extractContentLinks(html string, baseURL *url.URL) []string {
	seen := make(map[string]bool)
	var links []string

	matches := hrefRegex.FindAllStringSubmatch(html, -1)
	for _, match := range matches {
		if len(match) < 2 {
			continue
		}
		href := match[1]

		// Resolve relative URLs
		resolved := resolveHref(href, baseURL)
		if resolved == "" {
			continue
		}

		u, err := url.Parse(resolved)
		if err != nil {
			continue
		}

		// Only follow same-host links. Use an exact match or dot-boundary suffix
		// to prevent "evil-example.com" from matching "example.com".
		baseHost := strings.TrimPrefix(baseURL.Hostname(), "www.")
		host := u.Hostname()
		if host != baseHost && !strings.HasSuffix(host, "."+baseHost) {
			continue
		}

		path := u.Path
		if path == "" || path == "/" {
			continue
		}

		// Skip non-content paths
		skip := false
		for _, pat := range skipPathPatterns {
			if pat.MatchString(path) {
				skip = true
				break
			}
		}
		if skip {
			continue
		}

		// Check if it matches content patterns
		isContent := false
		for _, pat := range contentPathPatterns {
			if pat.MatchString(path) {
				isContent = true
				break
			}
		}
		if !isContent {
			continue
		}

		// Normalize: remove fragment, keep query
		u.Fragment = ""
		normalized := u.String()

		if seen[normalized] {
			continue
		}
		seen[normalized] = true
		links = append(links, normalized)
	}

	return links
}

// probeForStreams attempts browser-based detection first (clicking play buttons,
// intercepting network requests), then falls back to simple HTML regex matching.
// This is the core fix: modern video sites load streams via JavaScript after
// user interaction, so plain HTML fetching misses them entirely.
func (m *Module) probeForStreams(ctx context.Context, pageURL string) (streams []detectedStream, title string) {
	if m.browser != nil && m.browser.available() {
		result, err := m.browser.probe(ctx, pageURL)
		if err != nil {
			m.log.Warn("Browser probe failed for %s: %v — falling back to HTML", pageURL, err)
		} else if len(result.Streams) > 0 {
			return result.Streams, result.Title
		}
		m.log.Debug("Browser found no streams on %s, trying HTML fallback", pageURL)
	}

	var m3u8s []string
	m3u8s, title = m.probeForM3U8HTML(ctx, pageURL)
	if len(m3u8s) == 0 {
		return nil, title
	}
	streams = make([]detectedStream, len(m3u8s))
	for i, u := range m3u8s {
		streams[i] = detectedStream{
			URL:             u,
			Type:            "m3u8",
			DetectionMethod: "html-regex",
		}
	}
	return streams, title
}

// probeForM3U8HTML is the original HTML-only prober (kept as fallback).
func (m *Module) probeForM3U8HTML(ctx context.Context, pageURL string) (urls []string, title string) {
	body, err := m.fetchPage(ctx, pageURL)
	if err != nil {
		m.log.Debug("Failed to fetch %s: %v", pageURL, err)
		return nil, ""
	}

	// Extract title
	if matches := titleRegex.FindStringSubmatch(body); len(matches) >= 2 {
		title = strings.TrimSpace(matches[1])
		// Clean up common title suffixes
		for _, suffix := range []string{" - Pornhub.com", " - XVIDEOS.COM", " - YouPorn", " - YouTube"} {
			title = strings.TrimSuffix(title, suffix)
		}
	}

	// Find M3U8 URLs in the page source
	m3u8s := m3u8Regex.FindAllString(body, -1)

	// Deduplicate and clean
	seen := make(map[string]bool)
	for _, u := range m3u8s {
		// Unescape common HTML entities
		u = strings.ReplaceAll(u, "&amp;", "&")
		u = strings.ReplaceAll(u, "\\u0026", "&")
		u = strings.ReplaceAll(u, "\\/", "/")

		if seen[u] {
			continue
		}
		seen[u] = true
		urls = append(urls, u)
	}

	return urls, title
}

// --- Discovery Review ---

// GetDiscoveries returns discoveries, optionally filtered by status.
func (m *Module) GetDiscoveries(status string) ([]*Discovery, error) {
	if m.discoveryRepo == nil {
		return nil, errDisabled
	}
	var records []*repositories.CrawlerDiscoveryRecord
	var err error

	if status == "pending" {
		records, err = m.discoveryRepo.ListPending(context.Background())
	} else {
		records, err = m.discoveryRepo.List(context.Background())
	}
	if err != nil {
		return nil, err
	}

	discoveries := make([]*Discovery, len(records))
	for i, rec := range records {
		discoveries[i] = recordToDiscovery(rec)
	}
	return discoveries, nil
}

// ApproveDiscovery marks a discovery as "added" and adds it to the extractor.
func (m *Module) ApproveDiscovery(id, reviewedBy string) (*Discovery, error) {
	if m.discoveryRepo == nil {
		return nil, errDisabled
	}
	disc, err := m.discoveryRepo.Get(context.Background(), id)
	if err != nil {
		return nil, err
	}
	if disc == nil {
		return nil, fmt.Errorf("discovery not found: %s", id)
	}
	if disc.Status != "pending" {
		return nil, fmt.Errorf("discovery already reviewed: %s", disc.Status)
	}

	// Add to extractor module
	if m.extractor != nil {
		if _, err := m.extractor.AddItem(disc.StreamURL, disc.Title, reviewedBy); err != nil {
			return nil, fmt.Errorf("failed to add to extractor: %w", err)
		}
	}

	// Mark as added
	if err := m.discoveryRepo.UpdateStatus(context.Background(), id, "added", reviewedBy); err != nil {
		return nil, err
	}

	disc.Status = "added"
	disc.ReviewedBy = reviewedBy
	disc.ReviewedAt = new(time.Now())

	m.log.Info("Approved discovery: %s -> %s", disc.Title, disc.StreamURL)
	return recordToDiscovery(disc), nil
}

// IgnoreDiscovery marks a discovery as "ignored".
func (m *Module) IgnoreDiscovery(id, reviewedBy string) error {
	if m.discoveryRepo == nil {
		return errDisabled
	}
	disc, err := m.discoveryRepo.Get(context.Background(), id)
	if err != nil {
		return err
	}
	if disc == nil {
		return fmt.Errorf("discovery not found: %s", id)
	}
	if disc.Status != "pending" {
		return fmt.Errorf("discovery already reviewed: %s", disc.Status)
	}

	return m.discoveryRepo.UpdateStatus(context.Background(), id, "ignored", reviewedBy)
}

// DeleteDiscovery removes a discovery permanently.
func (m *Module) DeleteDiscovery(id string) error {
	if m.discoveryRepo == nil {
		return errDisabled
	}
	return m.discoveryRepo.Delete(context.Background(), id)
}

// --- Stats ---

// GetStats returns crawler statistics (loads all targets and discoveries to compute counts).
func (m *Module) GetStats() Stats {
	stats := Stats{}

	m.crawlMu.RLock()
	stats.Crawling = m.crawling
	m.crawlMu.RUnlock()

	if m.targetRepo != nil {
		targets, err := m.targetRepo.List(context.Background())
		if err != nil {
			m.log.Warn("GetStats: failed to list targets: %v", err)
		} else {
			stats.TotalTargets = len(targets)
			for _, t := range targets {
				if t.Enabled {
					stats.EnabledTargets++
				}
			}
		}
	}

	if m.discoveryRepo == nil {
		return stats
	}
	discoveries, err := m.discoveryRepo.List(context.Background())
	if err != nil {
		m.log.Warn("GetStats: failed to list discoveries: %v", err)
	} else {
		stats.TotalDiscoveries = len(discoveries)
		for _, d := range discoveries {
			if d.Status == "pending" {
				stats.PendingDiscoveries++
			}
		}
	}

	return stats
}

// IsCrawling returns whether a crawl is currently in progress.
func (m *Module) IsCrawling() bool {
	m.crawlMu.RLock()
	defer m.crawlMu.RUnlock()
	return m.crawling
}

// --- Utilities ---

func generateTargetID(rawURL string) string {
	h := sha256.Sum256([]byte(rawURL))
	return fmt.Sprintf("ct_%x", h[:12])
}

func generateDiscoveryID(streamURL string) string {
	h := sha256.Sum256([]byte(streamURL))
	return fmt.Sprintf("cd_%x", h[:12])
}

func resolveHref(href string, base *url.URL) string {
	if strings.HasPrefix(href, "http://") || strings.HasPrefix(href, "https://") {
		return href
	}
	if strings.HasPrefix(href, "//") {
		return base.Scheme + ":" + href
	}
	if strings.HasPrefix(href, "/") {
		return base.Scheme + "://" + base.Host + href
	}
	if strings.HasPrefix(href, "javascript:") || strings.HasPrefix(href, "mailto:") || strings.HasPrefix(href, "#") {
		return ""
	}
	ref, err := url.Parse(href)
	if err != nil {
		return ""
	}
	return base.ResolveReference(ref).String()
}

func extractTitleFromURL(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "Untitled"
	}
	// Try to get a meaningful name from the URL path
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) > 0 {
		last := parts[len(parts)-1]
		// Replace common separators with spaces
		last = strings.ReplaceAll(last, "-", " ")
		last = strings.ReplaceAll(last, "_", " ")
		if last != "" {
			return last
		}
	}
	return "Untitled"
}

func recordToTarget(rec *repositories.CrawlerTargetRecord) *CrawlTarget {
	return &CrawlTarget{
		ID:          rec.ID,
		Name:        rec.Name,
		URL:         rec.URL,
		Site:        rec.Site,
		Enabled:     rec.Enabled,
		LastCrawled: rec.LastCrawled,
		CreatedAt:   rec.CreatedAt,
	}
}

func recordToDiscovery(rec *repositories.CrawlerDiscoveryRecord) *Discovery {
	return &Discovery{
		ID:           rec.ID,
		TargetID:     rec.TargetID,
		PageURL:      rec.PageURL,
		Title:        rec.Title,
		StreamURL:    rec.StreamURL,
		StreamType:   rec.StreamType,
		Quality:      rec.Quality,
		Status:       rec.Status,
		ReviewedBy:   rec.ReviewedBy,
		ReviewedAt:   rec.ReviewedAt,
		DiscoveredAt: rec.DiscoveredAt,
	}
}
