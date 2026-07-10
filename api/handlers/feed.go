package handlers

import (
	"encoding/xml"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"media-server-pro/internal/media"
	"media-server-pro/pkg/middleware"
	"media-server-pro/pkg/models"
)

const feedMaxItems = 50
const feedDefaultItems = 20

// atomFeed is the top-level Atom feed element.
type atomFeed struct {
	XMLName xml.Name    `xml:"feed"`
	XMLNS   string      `xml:"xmlns,attr"`
	Title   string      `xml:"title"`
	ID      string      `xml:"id"`
	Updated string      `xml:"updated"`
	Link    []atomLink  `xml:"link"`
	Entries []atomEntry `xml:"entry"`
}

type atomLink struct {
	Rel  string `xml:"rel,attr,omitempty"`
	Type string `xml:"type,attr,omitempty"`
	Href string `xml:"href,attr"`
}

type atomEntry struct {
	Title   string      `xml:"title"`
	ID      string      `xml:"id"`
	Updated string      `xml:"updated"`
	Link    atomLink    `xml:"link"`
	Summary atomSummary `xml:"summary"`
}

type atomSummary struct {
	Type    string `xml:"type,attr"`
	Content string `xml:",chardata"`
}

// GetRSSFeed returns an Atom feed of recently-added media items.
// Optional query params:
//   - category — filter by curated category (MediaCategory.id)
//   - type     — filter by media type ("video" or "audio")
//   - limit    — number of items (1–50, default 20)
//
// The endpoint is accessible to all authenticated users so RSS clients that
// pass the session cookie (same-origin tooling, Inoreader with cookie auth, etc.)
// can subscribe to library updates.
const feedCacheTTL = 2 * time.Minute

func (h *Handler) GetRSSFeed(c *gin.Context) {
	uiCfg := h.config.Get().UI
	maxItems := uiCfg.FeedMaxItems
	if maxItems <= 0 {
		maxItems = feedMaxItems
	}
	defaultItems := uiCfg.FeedDefaultItems
	if defaultItems <= 0 {
		defaultItems = feedDefaultItems
	}
	limit := defaultItems
	if l, err := strconv.Atoi(c.Query("limit")); err == nil && l > 0 {
		if l > maxItems {
			l = maxItems
		}
		limit = l
	}

	canViewMature := h.canViewMatureContent(c)

	// Build a stable cache key from request parameters.
	cacheKey := fmt.Sprintf("%s|%s|%d|%v", c.Query("category"), c.Query("type"), limit, canViewMature)

	h.feedCacheMu.Lock()
	if h.feedCache == nil {
		h.feedCache = make(map[string]feedCacheEntry)
	}
	if entry, ok := h.feedCache[cacheKey]; ok && time.Now().Before(entry.expires) {
		h.feedCacheMu.Unlock()
		c.Header(headerCacheControl, "public, max-age=300")
		c.Data(http.StatusOK, "application/atom+xml; charset=utf-8", entry.data)
		return
	}
	h.feedCacheMu.Unlock()

	filter := media.Filter{
		Type:     models.MediaType(c.Query("type")),
		SortBy:   "date_added",
		SortDesc: true,
	}
	// Push mature exclusion into the filter (rather than post-filtering the full
	// result) so the bounded top-N fetch below returns `limit` non-mature items
	// directly instead of dropping some after the fact.
	if !canViewMature {
		filter.IsMature = new(false)
	}
	// ?category=<MediaCategory.id> restricts the feed to a curated category.
	categoryName := ""
	if catID := c.Query("category"); catID != "" && catID != "all" {
		filter.CategoryID = catID
		members, err := h.media.GetCategoryMemberIDs(c.Request.Context(), catID)
		if err != nil || members == nil {
			members = map[string]bool{}
		}
		filter.CategoryIDSet = members
		if gdb := h.database.GORM(); gdb != nil {
			var cat models.MediaCategory
			if gdb.WithContext(c.Request.Context()).Select("name").First(&cat, "id = ?", catID).Error == nil {
				categoryName = cat.Name
			}
		}
	}

	// Bounded top-N: only the top `limit` items are ever deep-copied/sorted, rather
	// than materializing the entire matched catalog just to keep the first page.
	// Mature exclusion is already applied via filter.IsMature above.
	items := h.mergedMediaTopN(filter, limit) // include federated media in the feed

	// Derive a canonical base URL for self-links.
	// Only trust proxy headers (X-Forwarded-Proto, Cf-Visitor) from trusted proxies.
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
	baseURL := fmt.Sprintf("%s://%s", scheme, c.Request.Host)

	// Build feed title
	feedTitle := "Media Server — Latest Media"
	if categoryName != "" {
		feedTitle = fmt.Sprintf("Media Server — %s", categoryName)
	} else if filter.Type != "" {
		typeStr := string(filter.Type)
		if typeStr != "" {
			typeStr = strings.ToUpper(typeStr[:1]) + typeStr[1:]
		}
		feedTitle = fmt.Sprintf("Media Server — %s", typeStr)
	}

	updated := time.Now().UTC().Format(time.RFC3339)
	if len(items) > 0 {
		updated = items[0].DateAdded.UTC().Format(time.RFC3339)
	}

	feedID := baseURL + "/api/feed"
	if c.Request.URL.RawQuery != "" {
		feedID += "?" + c.Request.URL.RawQuery
	}

	feed := atomFeed{
		XMLNS:   "http://www.w3.org/2005/Atom",
		Title:   feedTitle,
		ID:      feedID,
		Updated: updated,
		Link: []atomLink{
			{Rel: "self", Type: "application/atom+xml", Href: feedID},
			{Rel: "alternate", Type: "text/html", Href: baseURL},
		},
	}

	for _, item := range items {
		typeStr := string(item.Type)
		if typeStr != "" {
			typeStr = strings.ToUpper(typeStr[:1]) + typeStr[1:]
		}
		summary := typeStr
		if item.Duration > 0 {
			mins := int(item.Duration) / 60
			secs := int(item.Duration) % 60
			summary += fmt.Sprintf(" (%d:%02d)", mins, secs)
		}

		feed.Entries = append(feed.Entries, atomEntry{
			Title:   item.Name,
			ID:      fmt.Sprintf("%s/player?id=%s", baseURL, item.ID),
			Updated: item.DateAdded.UTC().Format(time.RFC3339),
			Link:    atomLink{Rel: "alternate", Type: "text/html", Href: fmt.Sprintf("%s/player?id=%s", baseURL, item.ID)},
			Summary: atomSummary{Type: "text", Content: summary},
		})
	}

	data, err := xml.MarshalIndent(feed, "", "  ")
	if err != nil {
		writeError(c, http.StatusInternalServerError, "Failed to generate feed")
		return
	}

	rendered := append([]byte(xml.Header), data...)

	h.feedCacheMu.Lock()
	if h.feedCache == nil {
		h.feedCache = make(map[string]feedCacheEntry)
	}
	h.feedCache[cacheKey] = feedCacheEntry{data: rendered, updated: updated, expires: time.Now().Add(feedCacheTTL)}
	h.feedCacheMu.Unlock()

	c.Header(headerCacheControl, "public, max-age=300")
	c.Data(http.StatusOK, "application/atom+xml; charset=utf-8", rendered)
}
