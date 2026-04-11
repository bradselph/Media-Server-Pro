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
//   - category — filter by media category (e.g. "Movies", "TV Shows")
//   - type     — filter by media type ("video" or "audio")
//   - limit    — number of items (1–50, default 20)
//
// The endpoint is accessible to all authenticated users so RSS clients that
// pass the session cookie (same-origin tooling, Inoreader with cookie auth, etc.)
// can subscribe to library updates.
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

	filter := media.Filter{
		Category: c.Query("category"),
		Type:     models.MediaType(c.Query("type")),
		SortBy:   "date_added",
		SortDesc: true,
	}

	allItems := h.media.ListMedia(filter)

	// Filter out mature content for users who are not authorized to view it.
	canViewMature := h.canViewMatureContent(c)
	items := allItems[:0]
	for _, item := range allItems {
		if !item.IsMature || canViewMature {
			items = append(items, item)
		}
	}

	// Truncate to requested limit
	if len(items) > limit {
		items = items[:limit]
	}

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
	if filter.Category != "" {
		feedTitle = fmt.Sprintf("Media Server — %s", filter.Category)
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
		summary := fmt.Sprintf("%s — %s", typeStr, item.Category)
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

	c.Header(headerCacheControl, "public, max-age=300")
	c.Data(http.StatusOK, "application/atom+xml; charset=utf-8", append([]byte(xml.Header), data...))
}
