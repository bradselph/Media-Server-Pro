package handlers

import (
	"encoding/xml"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"media-server-pro/internal/media"
	"media-server-pro/pkg/middleware"
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

// Sitemaps must stay under 50k URLs / 50MB per file. We cap at 25k as a
// safety margin; if/when a library exceeds that, split into a sitemap index.
const sitemapMaxURLs = 25000
const sitemapCacheTTL = 1 * time.Hour

var (
	sitemapCacheMu sync.Mutex
	sitemapCache   []byte
	sitemapCacheAt time.Time
)

// GetSitemap returns an XML sitemap of public, indexable URLs: the home
// page, top-level discovery routes, and one entry per non-mature-restricted
// media item under /player?id=...
//
// Public endpoint — no auth required so search-engine crawlers can fetch it.
// Mature items are still listed (the project is adult-content-only); the
// site's age gate is enforced separately in the SPA.
func (h *Handler) GetSitemap(c *gin.Context) {
	sitemapCacheMu.Lock()
	if sitemapCache != nil && time.Since(sitemapCacheAt) < sitemapCacheTTL {
		body := sitemapCache
		sitemapCacheMu.Unlock()
		c.Header(headerCacheControl, "public, max-age=3600")
		c.Data(http.StatusOK, "application/xml; charset=utf-8", body)
		return
	}
	sitemapCacheMu.Unlock()

	baseURL := seoBaseURL(c)

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

	items := h.media.ListMedia(media.Filter{SortBy: "date_added", SortDesc: true})
	for _, item := range items {
		if len(urls) >= sitemapMaxURLs {
			break
		}
		entry := sitemapURLEntry{
			Loc:        fmt.Sprintf("%s/player?id=%s", baseURL, item.ID),
			LastMod:    item.DateAdded.UTC().Format("2006-01-02"),
			ChangeFreq: "weekly",
			Priority:   "0.6",
		}
		if item.ThumbnailURL != "" {
			entry.Image = &sitemapImage{Loc: absoluteURL(baseURL, item.ThumbnailURL)}
		}
		urls = append(urls, entry)
	}

	urlset := sitemapURLSet{
		XMLNS:    "http://www.sitemaps.org/schemas/sitemap/0.9",
		XMLNSImg: "http://www.google.com/schemas/sitemap-image/1.1",
		URLs:     urls,
	}

	data, err := xml.MarshalIndent(urlset, "", "  ")
	if err != nil {
		writeError(c, http.StatusInternalServerError, "Failed to generate sitemap")
		return
	}
	rendered := append([]byte(xml.Header), data...)

	sitemapCacheMu.Lock()
	sitemapCache = rendered
	sitemapCacheAt = time.Now()
	sitemapCacheMu.Unlock()

	c.Header(headerCacheControl, "public, max-age=3600")
	c.Data(http.StatusOK, "application/xml; charset=utf-8", rendered)
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
