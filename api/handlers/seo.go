package handlers

import (
	"encoding/xml"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"media-server-pro/internal/media"
	"media-server-pro/pkg/middleware"
	"media-server-pro/pkg/models"
)

// urlset / urlEntry / sitemapImage model the sitemaps.org schema, with the
// image-sitemap extension so Google can pick up thumbnails alongside the
// player URL.
type sitemapURLSet struct {
	XMLName  xml.Name          `xml:"urlset"`
	XMLNS    string            `xml:"xmlns,attr"`
	XMLNSImg string            `xml:"xmlns:image,attr,omitempty"`
	URLs     []sitemapURLEntry `xml:"url"`
}

type sitemapURLEntry struct {
	Loc        string        `xml:"loc"`
	LastMod    string        `xml:"lastmod,omitempty"`
	ChangeFreq string        `xml:"changefreq,omitempty"`
	Priority   string        `xml:"priority,omitempty"`
	Image      *sitemapImage `xml:"image:image,omitempty"`
}

type sitemapImage struct {
	Loc string `xml:"image:loc"`
}

// Sitemaps must stay under 50k URLs / 50MB per file. Each file holds at most
// sitemapMaxURLs (a safety margin); a site with more URLs is split into
// numbered parts (/sitemaps/<n>.xml) listed by a sitemap index served at
// /sitemap.xml, so a large library is crawlable in full rather than cut off.
const (
	sitemapMaxURLs  = 25000
	sitemapMaxParts = 40 // 1M URLs — far beyond any real library; bounds memory
	sitemapCacheTTL = 1 * time.Hour
	// sitemapCacheHosts bounds the per-host cache: the Host header is
	// client-controlled, so an unbounded map keyed by it would grow on demand.
	sitemapCacheHosts = 8
)

type sitemapIndex struct {
	XMLName  xml.Name          `xml:"sitemapindex"`
	XMLNS    string            `xml:"xmlns,attr"`
	Sitemaps []sitemapIndexRef `xml:"sitemap"`
}

type sitemapIndexRef struct {
	Loc     string `xml:"loc"`
	LastMod string `xml:"lastmod,omitempty"`
}

// renderedSitemap is the sitemap for one base URL: a single urlset (index is
// nil and parts has one entry), or an index plus the parts it lists.
type renderedSitemap struct {
	index []byte
	parts [][]byte
	at    time.Time
}

// The cache is keyed by base URL: every <loc> is absolute, and a single shared
// entry let whichever Host a request carried first decide the URLs served to
// every crawler for the next hour.
var (
	sitemapCacheMu sync.Mutex
	sitemapCache   = map[string]*renderedSitemap{}
)

// GetSitemap returns an XML sitemap of public, indexable URLs: the home
// page, top-level discovery routes, and one entry per media item under
// /player?id=... — or, past sitemapMaxURLs, a sitemap index of the parts.
//
// Public endpoint — no auth required so search-engine crawlers can fetch it.
// Mature items are still listed (the project is adult-content-only); the
// site's age gate is enforced separately in the SPA. Their thumbnails are not
// (see appendSitemapMediaEntries).
func (h *Handler) GetSitemap(c *gin.Context) {
	sm, err := h.sitemapFor(c)
	if err != nil {
		writeError(c, http.StatusInternalServerError, "Failed to generate sitemap")
		return
	}
	body := sm.index
	if body == nil {
		body = sm.parts[0]
	}
	c.Header(headerCacheControl, "public, max-age=3600")
	c.Data(http.StatusOK, "application/xml; charset=utf-8", body)
}

// GetSitemapPart serves one numbered part (/sitemaps/<n>.xml) of a sitemap
// that GetSitemap split into an index.
func (h *Handler) GetSitemapPart(c *gin.Context) {
	n, err := strconv.Atoi(strings.TrimSuffix(c.Param("part"), ".xml"))
	if err != nil || n < 1 {
		writeError(c, http.StatusNotFound, "Sitemap not found")
		return
	}
	sm, err := h.sitemapFor(c)
	if err != nil {
		writeError(c, http.StatusInternalServerError, "Failed to generate sitemap")
		return
	}
	if n > len(sm.parts) {
		writeError(c, http.StatusNotFound, "Sitemap not found")
		return
	}
	c.Header(headerCacheControl, "public, max-age=3600")
	c.Data(http.StatusOK, "application/xml; charset=utf-8", sm.parts[n-1])
}

// sitemapFor returns the cached sitemap for the request's base URL, building
// it when missing or older than sitemapCacheTTL.
func (h *Handler) sitemapFor(c *gin.Context) (*renderedSitemap, error) {
	baseURL := seoBaseURL(c)
	sitemapCacheMu.Lock()
	if sm := sitemapCache[baseURL]; sm != nil && time.Since(sm.at) < sitemapCacheTTL {
		sitemapCacheMu.Unlock()
		return sm, nil
	}
	sitemapCacheMu.Unlock()

	sm, err := renderSitemap(baseURL, h.sitemapURLs(c, baseURL), sitemapMaxURLs)
	if err != nil {
		return nil, err
	}

	sitemapCacheMu.Lock()
	if _, ok := sitemapCache[baseURL]; !ok && len(sitemapCache) >= sitemapCacheHosts {
		oldest := ""
		for k, v := range sitemapCache {
			if oldest == "" || v.at.Before(sitemapCache[oldest].at) {
				oldest = k
			}
		}
		delete(sitemapCache, oldest)
	}
	sitemapCache[baseURL] = sm
	sitemapCacheMu.Unlock()
	return sm, nil
}

// renderSitemap renders urls as one urlset when they fit in a single file of
// perFile URLs, and otherwise as consecutive parts of perFile URLs plus an
// index listing <baseURL>/sitemaps/<n>.xml for each.
func renderSitemap(baseURL string, urls []sitemapURLEntry, perFile int) (*renderedSitemap, error) {
	sm := &renderedSitemap{at: time.Now()}
	for start := 0; start == 0 || start < len(urls); start += perFile {
		data, err := xml.MarshalIndent(sitemapURLSet{
			XMLNS:    "http://www.sitemaps.org/schemas/sitemap/0.9",
			XMLNSImg: "http://www.google.com/schemas/sitemap-image/1.1",
			URLs:     urls[start:min(start+perFile, len(urls))],
		}, "", "  ")
		if err != nil {
			return nil, err
		}
		sm.parts = append(sm.parts, append([]byte(xml.Header), data...))
	}
	if len(sm.parts) == 1 {
		return sm, nil
	}
	now := time.Now().UTC().Format("2006-01-02")
	idx := sitemapIndex{XMLNS: "http://www.sitemaps.org/schemas/sitemap/0.9"}
	for i := range sm.parts {
		idx.Sitemaps = append(idx.Sitemaps, sitemapIndexRef{
			Loc:     fmt.Sprintf("%s/sitemaps/%d.xml", baseURL, i+1),
			LastMod: now,
		})
	}
	data, err := xml.MarshalIndent(idx, "", "  ")
	if err != nil {
		return nil, err
	}
	sm.index = append([]byte(xml.Header), data...)
	return sm, nil
}

// sitemapURLs collects every URL the sitemap lists.
func (h *Handler) sitemapURLs(c *gin.Context, baseURL string) []sitemapURLEntry {
	// Static, always-present routes that should be indexed.
	now := time.Now().UTC().Format("2006-01-02")
	urls := []sitemapURLEntry{
		{Loc: baseURL + "/", LastMod: now, ChangeFreq: "hourly", Priority: "1.0"},
		{Loc: baseURL + "/browse", LastMod: now, ChangeFreq: "daily", Priority: "0.8"},
		{Loc: baseURL + "/categories", LastMod: now, ChangeFreq: "daily", Priority: "0.7"},
		{Loc: baseURL + "/privacy", ChangeFreq: "monthly", Priority: "0.2"},
		{Loc: baseURL + "/terms", ChangeFreq: "monthly", Priority: "0.2"},
		{Loc: baseURL + "/2257", ChangeFreq: "monthly", Priority: "0.2"},
		{Loc: baseURL + "/dmca", ChangeFreq: "monthly", Priority: "0.2"},
	}

	// Category detail pages — the most topically relevant landing pages for
	// organic search ("watch <tag> videos"). One entry per curated category.
	if gdb := h.database.GORM(); gdb != nil {
		var cats []struct{ ID string }
		if err := gdb.WithContext(c.Request.Context()).
			Model(&models.MediaCategory{}).Select("id").Scan(&cats).Error; err == nil {
			for _, cat := range cats {
				if len(urls) >= sitemapMaxURLs*sitemapMaxParts {
					break
				}
				urls = append(urls, sitemapURLEntry{
					Loc:        baseURL + "/categories/" + cat.ID,
					ChangeFreq: "weekly",
					Priority:   "0.7",
				})
			}
		}
	}

	items := h.media.ListMedia(media.Filter{SortBy: "date_added", SortDesc: true})
	return appendSitemapMediaEntries(urls, baseURL, items)
}

// appendSitemapMediaEntries adds one /player entry per item, up to
// sitemapMaxURLs*sitemapMaxParts in total, with an image-sitemap entry for its
// thumbnail unless the item is mature: a mature thumbnail is gated (crawlers
// get the censored placeholder), and submitting it to image search would be
// wrong either way.
func appendSitemapMediaEntries(urls []sitemapURLEntry, baseURL string, items []*models.MediaItem) []sitemapURLEntry {
	for _, item := range items {
		if len(urls) >= sitemapMaxURLs*sitemapMaxParts {
			break
		}
		entry := sitemapURLEntry{
			Loc:        fmt.Sprintf("%s/player?id=%s", baseURL, item.ID),
			LastMod:    item.DateAdded.UTC().Format("2006-01-02"),
			ChangeFreq: "weekly",
			Priority:   "0.6",
		}
		if item.ThumbnailURL != "" && !item.IsMature {
			entry.Image = &sitemapImage{Loc: absoluteURL(baseURL, item.ThumbnailURL)}
		}
		urls = append(urls, entry)
	}
	return urls
}

// GetRobotsTxt returns a robots.txt that allows crawling everything except
// authenticated, transient, or non-indexable paths (search results, the
// admin panel, the API surface, raw media streams). Points crawlers at the
// sitemap so they can find player pages without depending on internal links.
func (h *Handler) GetRobotsTxt(c *gin.Context) {
	baseURL := seoBaseURL(c)
	body := strings.Join([]string{
		"User-agent: *",
		"Disallow: /admin",
		"Disallow: /admin-login",
		"Disallow: /api/",
		"Disallow: /hls/",
		"Disallow: /media",
		"Disallow: /download",
		"Disallow: /ws/",
		"Disallow: /search",
		"Disallow: /profile",
		"Disallow: /upload",
		"Disallow: /favorites",
		"Disallow: /history",
		"",
		"Sitemap: " + baseURL + "/sitemap.xml",
		"",
	}, "\n")
	c.Header(headerCacheControl, "public, max-age=86400")
	c.Data(http.StatusOK, "text/plain; charset=utf-8", []byte(body))
}

// seoBaseURL returns the canonical scheme+host for the current request,
// honoring X-Forwarded-Proto / Cf-Visitor only when they come from a
// trusted proxy. Mirrors the logic in feed.go so SEO URLs are consistent
// with the RSS feed's self-link.
func seoBaseURL(c *gin.Context) string {
	scheme := "http"
	if c.Request.TLS != nil {
		scheme = "https"
	} else {
		remoteIP, _, splitErr := net.SplitHostPort(c.Request.RemoteAddr)
		if splitErr != nil {
			remoteIP = c.Request.RemoteAddr
		}
		if middleware.IsTrustedProxy(remoteIP) &&
			(c.GetHeader("X-Forwarded-Proto") == "https" ||
				strings.Contains(c.GetHeader("Cf-Visitor"), `"scheme":"https"`)) {
			scheme = "https"
		}
	}
	return fmt.Sprintf("%s://%s", scheme, c.Request.Host)
}

// absoluteURL turns a same-origin relative path into a fully-qualified URL.
// Leaves already-absolute URLs untouched.
func absoluteURL(baseURL, ref string) string {
	if ref == "" {
		return ""
	}
	if strings.HasPrefix(ref, "http://") || strings.HasPrefix(ref, "https://") {
		return ref
	}
	if !strings.HasPrefix(ref, "/") {
		ref = "/" + ref
	}
	return baseURL + ref
}
