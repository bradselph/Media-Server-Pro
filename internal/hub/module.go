// Package hub implements the BETA "Hub" feature: a browsable, age-gated catalog
// of external video embeds imported from a large pipe-delimited CSV into the
// hub_embeds table. It is fully optional and non-interfering — when the feature
// is disabled the module is never constructed (see cmd/server/main.go), and even
// if Start is invoked directly it no-ops without touching the database.
package hub

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"

	"media-server-pro/internal/config"
	"media-server-pro/internal/database"
	"media-server-pro/internal/logger"
	"media-server-pro/internal/repositories"
	mysqlrepo "media-server-pro/internal/repositories/mysql"
	"media-server-pro/pkg/helpers"
	"media-server-pro/pkg/models"
)

// embedBaseURL is the provider embed URL prefix. Every catalog row is a
// pornhub.com/embed/<id> iframe; the id is stored and the URL reconstructed here
// so the client renders one canonical, sanitized iframe src.
const embedBaseURL = "https://www.pornhub.com/embed/"

// maxPreviewURLs bounds how many hover/scrub preview frames the API returns per
// item, keeping list payloads reasonable.
const maxPreviewURLs = 20

// Embed is the API-facing representation of one catalog entry.
type Embed struct {
	EmbedID     string   `json:"embed_id"`
	EmbedURL    string   `json:"embed_url"`
	Title       string   `json:"title"`
	Pornstar    string   `json:"pornstar"`
	Duration    int      `json:"duration_secs"`
	Views       int64    `json:"views"`
	RatingUp    int      `json:"rating_up"`
	RatingDown  int      `json:"rating_down"`
	Tags        []string `json:"tags"`
	Categories  []string `json:"categories"`
	ThumbURL    string   `json:"thumb_url"`
	PreviewURLs []string `json:"preview_urls"`
	// IsMature is always true for Hub content; surfaced so the frontend reuses the
	// exact same blur/age-gate treatment as local mature media.
	IsMature bool `json:"is_mature"`
}

// Filter narrows a catalog query.
type Filter struct {
	Search   string
	Category string
	Tag      string
	// SortBy is "views" | "duration" | "rating" | "title" | "newest" ("" is
	// treated as newest). Unknown values fall back to newest rather than
	// erroring, so an old client cannot break the listing.
	SortBy string
}

// ImportState is the observable state of the CSV import job.
type ImportState struct {
	Running  bool   `json:"running"`
	Phase    string `json:"phase,omitempty"` // "downloading" | "importing" | ""
	Source   string `json:"source,omitempty"`
	RowsRead int64  `json:"rows_read"`
	// Inserted is the number of rows written. For a first (append-only) import it
	// is the new-row count; for an upsert re-import (see Upsert) it is the driver's
	// affected-row count (insert = 1, refresh = 2, no-op = 0) and so is NOT a
	// pure new-row count — the UI labels it "written" rather than "inserted" then.
	Inserted int64 `json:"inserted"`
	// Upsert is true when this run refreshes existing rows in place (re-import into
	// a populated catalog) rather than INSERT IGNORE. Lets the UI label Inserted
	// correctly and signals that the run added new rows AND updated changed ones.
	Upsert     bool      `json:"upsert"`
	TotalRows  int64     `json:"total_rows"`
	Path       string    `json:"path,omitempty"`
	Error      string    `json:"error,omitempty"`
	StartedAt  time.Time `json:"started_at,omitempty"`
	FinishedAt time.Time `json:"finished_at,omitempty"`
}

// Module is the Hub feature's lifecycle + query surface.
type Module struct {
	config   *config.Manager
	log      *logger.Logger
	dbModule *database.Module
	repo     repositories.HubEmbedRepository

	healthy   bool
	healthMsg string
	healthMu  sync.RWMutex

	importMu     sync.Mutex
	importState  ImportState
	importCancel context.CancelFunc

	// categories facet cache (computed from a sample; refreshed lazily).
	catMu       sync.Mutex
	catCache    []string
	catCachedAt time.Time

	// ─── server-side playback (resolve.go / stream.go / images.go) ───────────
	// httpClient reaches the provider and its CDN. It deliberately carries no
	// client-level Timeout: a media response is streamed for the length of the
	// video, and a whole-request deadline would sever playback mid-stream. Setup
	// is still bounded by the transport, and cancellation rides on the request
	// context so a viewer closing the tab tears the upstream fetch down.
	httpClient *http.Client
	// detector is an optional external resolver (the downloader service).
	detector   StreamDetector
	detectorMu sync.RWMutex
	// resolveCache maps embed id -> *ResolvedStream.
	resolveCache sync.Map
	// resolveGroup coalesces concurrent resolutions of the same embed.
	resolveGroup singleflight.Group
	// resolveSem bounds how many distinct embeds resolve at once.
	resolveSem chan struct{}
	// playlistCache maps "<embed>:master" / "<embed>:<rendition>" -> *cachedPlaylist.
	playlistCache sync.Map
	// imageURLs remembers catalog artwork URLs so serving an image does not need
	// a database round trip per request.
	imageURLs  map[string]imageURLs
	imageURLMu sync.RWMutex
	// imageCache holds fetched artwork bytes, shared across viewers.
	imageCache *imageCache
}

// NewModule constructs the Hub module. Following the autodiscovery pattern, this
// always returns a non-nil *Module; the "do nothing when disabled" behavior lives
// in the caller's feature gate (main.go constructs+registers this only when
// Features.EnableHub is set).
func NewModule(cfg *config.Manager, dbModule *database.Module) *Module {
	hc := cfg.Get().Hub
	resolves := hc.ProxyMaxConcurrentResolves
	if resolves <= 0 {
		resolves = 4
	}
	cacheBytes := int64(hc.ProxyImageCacheMB) * 1024 * 1024
	return &Module{
		config:    cfg,
		log:       logger.New("hub"),
		dbModule:  dbModule,
		healthMsg: "Not started",
		httpClient: &http.Client{
			Transport: helpers.SafeHTTPTransport(),
			// No Timeout — see the field comment on Module.httpClient.
			CheckRedirect: func(_ *http.Request, via []*http.Request) error {
				if len(via) >= 10 {
					return fmt.Errorf("too many redirects")
				}
				return nil
			},
		},
		resolveSem: make(chan struct{}, resolves),
		imageURLs:  make(map[string]imageURLs),
		imageCache: newImageCache(cacheBytes),
	}
}

// Name returns the module name.
func (m *Module) Name() string { return "hub" }

// Start initializes the repository. It is a no-op when the feature is disabled
// (defense-in-depth: main.go already avoids constructing/registering us then).
func (m *Module) Start(_ context.Context) error {
	if !m.config.Get().Hub.Enabled {
		m.log.Info("Hub module disabled, skipping start")
		m.setHealth(false, "Disabled")
		return nil
	}
	if m.dbModule == nil || m.dbModule.GORM() == nil {
		m.log.Warn("Hub module: database unavailable, catalog will be inaccessible")
		m.setHealth(false, "Database unavailable")
		return nil
	}
	m.repo = mysqlrepo.NewHubEmbedRepository(m.dbModule.GORM())
	m.setHealth(true, "Running")
	m.log.Info("Hub module started (BETA)")

	// Auto-bootstrap the catalog on a fresh deployment when configured, so the
	// whole flow is knob-driven (enable + source URL + auto-import => it just
	// happens). Runs in the background and only when the table is empty.
	if m.config.Get().Hub.AutoImport {
		go m.maybeAutoImport()
	}
	return nil
}

// Stop cancels any in-flight import and marks the module unhealthy. No DB writes.
func (m *Module) Stop(_ context.Context) error {
	m.importMu.Lock()
	if m.importCancel != nil {
		m.importCancel()
	}
	m.importMu.Unlock()
	// Drop every cached upstream URL and artwork blob so no resolved provider
	// URL outlives the module.
	m.clearProxyCaches()
	m.imageCache.clear()
	m.imageURLMu.Lock()
	m.imageURLs = make(map[string]imageURLs)
	m.imageURLMu.Unlock()
	m.setHealth(false, "Stopped")
	return nil
}

// Health reports the module health for the admin dashboard.
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

func (m *Module) setHealth(ok bool, msg string) {
	m.healthMu.Lock()
	m.healthy = ok
	m.healthMsg = msg
	m.healthMu.Unlock()
}

// ready returns the repo if the feature is enabled and initialized, else nil.
func (m *Module) ready() repositories.HubEmbedRepository {
	if !m.config.Get().Hub.Enabled {
		return nil
	}
	return m.repo
}

// GetEmbeds returns a page of catalog entries plus the total match count.
func (m *Module) GetEmbeds(ctx context.Context, filter Filter, limit, offset int) ([]*Embed, int64, error) {
	repo := m.ready()
	if repo == nil {
		return nil, 0, nil
	}
	var (
		recs  []*repositories.HubEmbedRecord
		total int64
		err   error
	)
	if filter.Search != "" || filter.Category != "" || filter.Tag != "" {
		recs, total, err = repo.Search(ctx, filter.Search, repositories.HubEmbedFilter{
			Category: filter.Category,
			Tag:      filter.Tag,
			SortBy:   filter.SortBy,
		}, offset, limit)
	} else {
		recs, total, err = repo.List(ctx, offset, limit, filter.SortBy)
	}
	if err != nil {
		return nil, 0, err
	}
	out := make([]*Embed, len(recs))
	for i, r := range recs {
		out[i] = m.toEmbed(r)
	}
	return out, total, nil
}

// GetEmbedByID returns a single entry by its provider embed id, or nil if absent.
func (m *Module) GetEmbedByID(ctx context.Context, embedID string) (*Embed, error) {
	repo := m.ready()
	if repo == nil {
		return nil, nil
	}
	rec, err := repo.GetByEmbedID(ctx, embedID)
	if err != nil || rec == nil {
		return nil, err
	}
	return m.toEmbed(rec), nil
}

// GetEmbedsByIDs returns the embeds for the given ids in one query, skipping ids
// with no match (order not guaranteed — callers key by embed_id). Returns empty
// when the feature is disabled or ids is empty.
func (m *Module) GetEmbedsByIDs(ctx context.Context, embedIDs []string) ([]*Embed, error) {
	repo := m.ready()
	if repo == nil || len(embedIDs) == 0 {
		return []*Embed{}, nil
	}
	recs, err := repo.GetByEmbedIDs(ctx, embedIDs)
	if err != nil {
		return nil, err
	}
	out := make([]*Embed, len(recs))
	for i, r := range recs {
		out[i] = m.toEmbed(r)
	}
	return out, nil
}

// ListCategories returns a de-duplicated facet list built from a bounded sample
// of the most-viewed rows, ordered by frequency. Cached for 10 minutes.
func (m *Module) ListCategories(ctx context.Context) ([]string, error) {
	repo := m.ready()
	if repo == nil {
		return []string{}, nil
	}
	m.catMu.Lock()
	defer m.catMu.Unlock()
	if m.catCache != nil && time.Since(m.catCachedAt) < 10*time.Minute {
		return m.catCache, nil
	}
	samples, err := repo.CategorySamples(ctx, 5000)
	if err != nil {
		return nil, err
	}
	freq := make(map[string]int)
	for _, s := range samples {
		for _, c := range splitList(s) {
			freq[c]++
		}
	}
	cats := make([]string, 0, len(freq))
	for c := range freq {
		cats = append(cats, c)
	}
	sort.Slice(cats, func(a, b int) bool {
		if freq[cats[a]] != freq[cats[b]] {
			return freq[cats[a]] > freq[cats[b]]
		}
		return cats[a] < cats[b]
	})
	if len(cats) > 60 {
		cats = cats[:60]
	}
	m.catCache = cats
	m.catCachedAt = time.Now()
	return cats, nil
}

// CountAll returns the current number of imported rows (0 when disabled).
func (m *Module) CountAll(ctx context.Context) (int64, error) {
	repo := m.ready()
	if repo == nil {
		return 0, nil
	}
	return repo.CountAll(ctx)
}

// ClearAll truncates the catalog. Only meaningful when enabled.
func (m *Module) ClearAll(ctx context.Context) error {
	repo := m.ready()
	if repo == nil {
		return nil
	}
	m.catMu.Lock()
	m.catCache = nil
	m.catMu.Unlock()
	return repo.DeleteAll(ctx)
}

// toEmbed converts a stored record to the API representation.
//
// When artwork proxying is on, the returned URLs point at this server rather
// than the provider CDN. That is deliberate and load-bearing: handing the CDN
// URL to the browser would have it fetched directly, which both reports the
// viewer's IP to the provider and shows a broken image to anyone the provider
// blocks. EmbedURL is left as the provider's own iframe URL because that remains
// the fallback player.
func (m *Module) toEmbed(r *repositories.HubEmbedRecord) *Embed {
	previews := splitList(r.PreviewURLs)
	if len(previews) > maxPreviewURLs {
		previews = previews[:maxPreviewURLs]
	}
	thumb := r.ThumbURL
	if m.imageProxyEnabled() {
		// Remember the real URLs now, while we hold the record: the image
		// requests that follow this page load then need no database lookup.
		m.rememberImageURLs(r)
		thumb = ProxiedThumbURL(r.EmbedID)
		// Truncation above keeps a prefix, so these indices still line up with
		// the full list recorded above.
		for i := range previews {
			previews[i] = ProxiedPreviewURL(r.EmbedID, i)
		}
	}
	return &Embed{
		EmbedID:     r.EmbedID,
		EmbedURL:    embedBaseURL + r.EmbedID,
		Title:       r.Title,
		Pornstar:    r.Pornstar,
		Duration:    r.DurationSecs,
		Views:       r.Views,
		RatingUp:    r.RatingUp,
		RatingDown:  r.RatingDown,
		Tags:        splitList(r.Tags),
		Categories:  splitList(r.Categories),
		ThumbURL:    thumb,
		PreviewURLs: previews,
		IsMature:    true,
	}
}

// splitList splits a ';'-joined field into a trimmed, non-empty slice.
func splitList(s string) []string {
	if s == "" {
		return []string{}
	}
	parts := strings.Split(s, ";")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if t := strings.TrimSpace(p); t != "" {
			out = append(out, t)
		}
	}
	return out
}
