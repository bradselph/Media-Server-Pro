package hub

import (
	"container/list"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"media-server-pro/internal/repositories"
	"media-server-pro/pkg/helpers"
)

// Server-side catalog artwork.
//
// Thumbnails and hover previews are provider CDN URLs. Left alone they load in
// the viewer's browser, which both reports every browsing user's IP to the
// provider and renders a grid of broken images for anyone the provider blocks —
// the same root cause as embeds that will not play. Routing them through the
// server fixes both, and unlike video it is cheap: the images are small, highly
// cacheable, and shared across users.

const (
	// imageKindThumb is the single grid/card thumbnail for an embed.
	imageKindThumb = "t"
	// imageKindPreview is one frame of the hover scrub strip.
	imageKindPreview = "p"

	// maxImageBytes bounds one image read. Catalog artwork is tens of KB; far
	// beyond that means a wrong or hostile response.
	maxImageBytes = 8 << 20

	// imageFetchTimeout bounds a single artwork fetch. Unlike video, an image is
	// a short bounded read, so a deadline is appropriate here.
	imageFetchTimeout = 20 * time.Second

	// imageURLTTL bounds how long an embed's artwork URLs are remembered. They
	// only change on re-import, so this mainly caps memory growth.
	imageURLTTL = 30 * time.Minute
	// imageURLCacheMax caps how many embeds' URL sets are held at once.
	imageURLCacheMax = 20000

	// imageBrowserCache is what the browser is told. Artwork for a given embed is
	// stable, so a long cache is what keeps proxying cheap: after the first view
	// the server is not asked again.
	imageBrowserCache = "public, max-age=604800"
)

// imageURLs is the artwork for one catalog entry.
type imageURLs struct {
	thumb    string
	previews []string
	storedAt time.Time
}

// ProxiedThumbURL returns the local URL for an embed's thumbnail.
func ProxiedThumbURL(embedID string) string {
	return "/hub/img/" + url.PathEscape(embedID) + "/" + imageKindThumb
}

// ProxiedPreviewURL returns the local URL for one hover-preview frame.
func ProxiedPreviewURL(embedID string, idx int) string {
	return "/hub/img/" + url.PathEscape(embedID) + "/" + imageKindPreview + "/" + strconv.Itoa(idx)
}

// rememberImageURLs caches a record's artwork URLs so serving its images does
// not need a database round trip per image. Catalog pages are fetched right
// before their images are, so this is warm by the time the browser asks.
func (m *Module) rememberImageURLs(rec *repositories.HubEmbedRecord) {
	if rec == nil || rec.EmbedID == "" {
		return
	}
	m.imageURLMu.Lock()
	defer m.imageURLMu.Unlock()
	// Cheap bound: the catalog has millions of rows, so drop everything rather
	// than grow without limit. Entries are trivially rebuilt from the database.
	// The nil case covers a Module assembled without NewModule (tests do this),
	// where a bare assignment would panic.
	if m.imageURLs == nil || len(m.imageURLs) >= imageURLCacheMax {
		m.imageURLs = make(map[string]imageURLs, imageURLCacheMax/2)
	}
	m.imageURLs[rec.EmbedID] = imageURLs{
		thumb:    rec.ThumbURL,
		previews: splitList(rec.PreviewURLs),
		storedAt: time.Now(),
	}
}

// lookupImageURLs returns an embed's artwork, consulting the cache first and
// falling back to the catalog. The database is the only source — a URL is never
// accepted from the caller, which is what stops this becoming an open proxy.
func (m *Module) lookupImageURLs(ctx context.Context, embedID string) (*imageURLs, error) {
	m.imageURLMu.RLock()
	cached, ok := m.imageURLs[embedID]
	m.imageURLMu.RUnlock()
	if ok && time.Since(cached.storedAt) < imageURLTTL {
		return &cached, nil
	}

	repo := m.ready()
	if repo == nil {
		return nil, ErrNotInCatalog
	}
	rec, err := repo.GetByEmbedID(ctx, embedID)
	if err != nil {
		return nil, fmt.Errorf("hub: catalog lookup failed: %w", err)
	}
	if rec == nil {
		return nil, ErrNotInCatalog
	}
	m.rememberImageURLs(rec)
	return &imageURLs{thumb: rec.ThumbURL, previews: splitList(rec.PreviewURLs)}, nil
}

// ProxyImage streams one piece of catalog artwork.
//
// kind selects the thumbnail or a hover-preview frame; idx picks the frame.
// Failures are reported as 404 so a missing image degrades to the frontend's own
// placeholder rather than a console full of 502s.
func (m *Module) ProxyImage(w http.ResponseWriter, r *http.Request, embedID, kind string, idx int) error {
	urls, err := m.lookupImageURLs(r.Context(), embedID)
	if err != nil {
		http.NotFound(w, r)
		return nil //nolint:nilerr // a missing catalog row is a 404, not a server fault
	}

	target := urls.thumb
	if kind == imageKindPreview {
		if idx < 0 || idx >= len(urls.previews) {
			http.NotFound(w, r)
			return nil
		}
		target = urls.previews[idx]
	}
	if strings.TrimSpace(target) == "" {
		http.NotFound(w, r)
		return nil
	}

	if blob, ctype, ok := m.imageCache.get(target); ok {
		writeImage(w, ctype, blob)
		return nil
	}

	blob, ctype, err := m.fetchImage(r.Context(), target)
	if err != nil {
		m.log.Debug("Hub: artwork fetch failed for %s: %v", embedID, err)
		http.NotFound(w, r)
		return nil //nolint:nilerr // upstream artwork failure degrades to a placeholder
	}
	m.imageCache.put(target, blob, ctype)
	writeImage(w, ctype, blob)
	return nil
}

func writeImage(w http.ResponseWriter, contentType string, blob []byte) {
	w.Header().Set(hdrContentTyp, contentType)
	w.Header().Set(hdrCacheCtl, imageBrowserCache)
	w.Header().Set("Content-Length", strconv.Itoa(len(blob)))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(blob)
}

// fetchImage reads one image from the provider CDN. Like every other outbound
// request here it is built from scratch, so nothing about the viewer travels
// upstream.
func (m *Module) fetchImage(ctx context.Context, rawURL string) (blob []byte, contentType string, err error) {
	if vErr := helpers.ValidateURLForSSRF(rawURL); vErr != nil {
		return nil, "", fmt.Errorf("image URL rejected: %w", vErr)
	}
	ctx, cancel := context.WithTimeout(ctx, imageFetchTimeout)
	defer cancel()

	// rawURL is never caller-supplied: it comes from our own catalog row, has just
	// been SSRF-validated, and is dialed through SafeDialContext, which refuses
	// private and loopback addresses at connect time on every redirect hop.
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, http.NoBody) //nolint:gosec // URL is catalog-sourced and validated above
	if err != nil {
		return nil, "", err
	}
	applyUpstreamHeaders(req, "")
	req.Header.Set("Accept", "image/avif,image/webp,image/jpeg,image/png,*/*;q=0.8")

	resp, err := m.httpClient.Do(req) //nolint:gosec // see above: validated, catalog-sourced URL over a guarded transport
	if err != nil {
		return nil, "", err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, "", &upstreamError{status: resp.StatusCode, url: rawURL}
	}

	blob, err = io.ReadAll(io.LimitReader(resp.Body, maxImageBytes))
	if err != nil {
		return nil, "", err
	}
	contentType = resp.Header.Get("Content-Type")
	if !strings.HasPrefix(contentType, "image/") {
		// Never echo an upstream-declared type we do not trust: a provider error
		// page served as text/html would otherwise be handed to an <img> tag.
		contentType = "image/jpeg"
	}
	return blob, contentType, nil
}

// ─── image byte cache ────────────────────────────────────────────────────────

// imageCache is a byte-bounded LRU of fetched artwork, shared across users. It
// is what keeps proxying affordable: popular catalog pages are served from
// memory instead of refetched per viewer.
type imageCache struct {
	mu      sync.Mutex
	maxSize int64
	size    int64
	order   *list.List // front = most recently used
	items   map[string]*list.Element
}

type imageEntry struct {
	key         string
	blob        []byte
	contentType string
}

func newImageCache(maxBytes int64) *imageCache {
	return &imageCache{
		maxSize: maxBytes,
		order:   list.New(),
		items:   make(map[string]*list.Element),
	}
}

func (c *imageCache) get(key string) (blob []byte, contentType string, ok bool) {
	if c == nil || c.maxSize <= 0 {
		return nil, "", false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	el, found := c.items[key]
	if !found {
		return nil, "", false
	}
	c.order.MoveToFront(el)
	e, _ := el.Value.(*imageEntry)
	if e == nil {
		return nil, "", false
	}
	return e.blob, e.contentType, true
}

func (c *imageCache) put(key string, blob []byte, contentType string) {
	if c == nil || c.maxSize <= 0 || len(blob) == 0 {
		return
	}
	// A single oversized image must not evict everything else.
	if int64(len(blob)) > c.maxSize/4 {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if el, found := c.items[key]; found {
		c.order.MoveToFront(el)
		return
	}
	el := c.order.PushFront(&imageEntry{key: key, blob: blob, contentType: contentType})
	c.items[key] = el
	c.size += int64(len(blob))
	for c.size > c.maxSize {
		back := c.order.Back()
		if back == nil {
			break
		}
		c.order.Remove(back)
		if e, _ := back.Value.(*imageEntry); e != nil {
			delete(c.items, e.key)
			c.size -= int64(len(e.blob))
		}
	}
}

func (c *imageCache) clear() {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.order.Init()
	c.items = make(map[string]*list.Element)
	c.size = 0
}
