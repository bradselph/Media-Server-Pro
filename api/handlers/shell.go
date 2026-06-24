package handlers

import (
	"encoding/json"
	"fmt"
	"html"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"media-server-pro/internal/media"
	"media-server-pro/pkg/models"
	"media-server-pro/web"
)

// EnrichSPAShell builds server-rendered SEO additions for the indexable SPA
// routes (home, browse, categories, and per-item player pages). The Nuxt
// frontend is a pure client-side SPA (ssr:false), so the served index.html
// contains no content — a crawler that does not run JavaScript indexes nothing,
// and even a JS-capable archiver (e.g. the Wayback Machine) renders blank
// because the content comes from /api/... XHR calls it never captured.
//
// This injects a real <title>, description, OpenGraph/Twitter meta, JSON-LD,
// and a <noscript> content + link-graph fallback so crawlers (Googlebot,
// social-card scrapers, the Internet Archive's Heritrix) get real HTML without
// executing the bundle or replaying API calls. It mirrors the client-side
// useSeoMeta/JSON-LD in player.vue so live browsers and crawlers agree.
//
// Wired into web.RegisterStaticRoutes via the router. Returns the zero
// web.ShellMeta for non-indexable routes, leaving the shell untouched.
func (h *Handler) EnrichSPAShell(c *gin.Context) web.ShellMeta {
	path := c.Request.URL.Path
	switch path {
	case "/player":
		return h.shellMetaForPlayer(c)
	case "/":
		return h.shellMetaForDiscovery(c,
			"Media Server Pro",
			"Stream and browse the full media library on Media Server Pro.")
	case "/browse":
		return h.shellMetaForDiscovery(c,
			"Browse — Media Server Pro",
			"Browse every title in the Media Server Pro library, newest first.")
	case "/categories":
		return h.shellMetaForDiscovery(c,
			"Categories — Media Server Pro",
			"Explore the Media Server Pro library by category.")
	}
	// Category detail pages (/categories/<id>) become their own indexable, topical
	// landing pages with a category-specific title and description.
	if rest, ok := strings.CutPrefix(path, "/categories/"); ok {
		return h.shellMetaForCategory(c, rest)
	}
	return web.ShellMeta{}
}

// shellMetaForCategory renders category-specific SEO for /categories/<id>. Falls
// back to the generic categories meta when the id is empty or no longer resolves
// to a category (deleted, or a malformed path segment).
func (h *Handler) shellMetaForCategory(c *gin.Context, id string) web.ShellMeta {
	id = strings.Trim(strings.TrimSpace(id), "/")
	if id != "" {
		if name := h.categoryNamesByIDs(c.Request.Context(), []string{id})[id]; name != "" {
			return h.shellMetaForDiscovery(c,
				name+" — Media Server Pro",
				"Browse "+name+" videos and media on Media Server Pro.")
		}
	}
	return h.shellMetaForDiscovery(c,
		"Categories — Media Server Pro",
		"Explore the Media Server Pro library by category.")
}

// shellMetaForPlayer renders per-item SEO for /player?id=<id>. Mirrors the
// client-side useSeoMeta + VideoObject/AudioObject JSON-LD in player.vue.
func (h *Handler) shellMetaForPlayer(c *gin.Context) web.ShellMeta {
	id := strings.TrimSpace(c.Query("id"))
	if id == "" {
		return web.ShellMeta{}
	}
	item, err := h.media.GetMediaByID(id)
	if err != nil || item == nil {
		return web.ShellMeta{}
	}

	base := seoBaseURL(c)
	title := shellMediaTitle(item)
	desc := shellMediaDescription(item, title)
	canonical := fmt.Sprintf("%s/player?id=%s", base, url.QueryEscape(item.ID))
	thumb := absoluteURL(base, item.ThumbnailURL)

	ogType, ldType := "video.other", "VideoObject"
	if item.Type == models.MediaTypeAudio {
		ogType, ldType = "music.song", "AudioObject"
	}

	var head strings.Builder
	writeMetaProperty(&head, "og:type", ogType)
	writeMetaProperty(&head, "og:title", title)
	writeMetaProperty(&head, "og:description", desc)
	writeMetaProperty(&head, "og:url", canonical)
	writeMetaName(&head, "twitter:title", title)
	writeMetaName(&head, "twitter:description", desc)
	if thumb != "" {
		writeMetaProperty(&head, "og:image", thumb)
		writeMetaName(&head, "twitter:card", "summary_large_image")
		writeMetaName(&head, "twitter:image", thumb)
	} else {
		writeMetaName(&head, "twitter:card", "summary")
	}
	head.WriteString(`<link rel="canonical" href="` + html.EscapeString(canonical) + `">`)
	// JSON-LD takes the RAW (un-HTML-escaped) title/desc — JSON encoding handles
	// quoting and playerJSONLD escapes '<' so a crafted title can't break out.
	head.WriteString(playerJSONLD(ldType, title, desc, canonical, item, thumb))

	var ns strings.Builder
	ns.WriteString(`<h1>` + html.EscapeString(title) + `</h1>`)
	if thumb != "" {
		ns.WriteString(`<img src="` + html.EscapeString(thumb) + `" alt="` + html.EscapeString(title) + `" width="480">`)
	}
	if desc != "" {
		ns.WriteString(`<p>` + html.EscapeString(desc) + `</p>`)
	}
	ns.WriteString(`<p><a href="` + html.EscapeString(canonical) + `">` + html.EscapeString(title) + `</a></p>`)

	return web.ShellMeta{
		Title:       html.EscapeString(title),
		Description: html.EscapeString(desc),
		Head:        head.String(),
		NoScript:    ns.String(),
	}
}

// shellMetaForDiscovery renders SEO for the home/browse/categories landing
// routes: route-specific meta plus a <noscript> heading, intro, navigation, and
// a cached link list of recent items so JS-less crawlers get content and a
// crawlable link graph into individual player pages.
func (h *Handler) shellMetaForDiscovery(c *gin.Context, title, desc string) web.ShellMeta {
	base := seoBaseURL(c)

	var head strings.Builder
	writeMetaProperty(&head, "og:type", "website")
	writeMetaProperty(&head, "og:title", title)
	writeMetaProperty(&head, "og:description", desc)
	writeMetaProperty(&head, "og:url", base+c.Request.URL.Path)
	writeMetaName(&head, "twitter:card", "summary")
	writeMetaName(&head, "twitter:title", title)
	writeMetaName(&head, "twitter:description", desc)

	var ns strings.Builder
	ns.WriteString(`<h1>` + html.EscapeString(title) + `</h1>`)
	ns.WriteString(`<p>` + html.EscapeString(desc) + `</p>`)
	ns.WriteString(`<p><a href="/browse">Browse all</a> · <a href="/categories">Categories</a> · <a href="/sitemap.xml">Sitemap</a></p>`)
	ns.WriteString(h.discoveryLinks())

	return web.ShellMeta{
		Title:       html.EscapeString(title),
		Description: html.EscapeString(desc),
		Head:        head.String(),
		NoScript:    ns.String(),
	}
}

const (
	shellDiscoveryLimit = 100
	shellDiscoveryTTL   = 10 * time.Minute
)

var (
	shellDiscoveryMu   sync.Mutex
	shellDiscoveryHTML string
	shellDiscoveryAt   time.Time
	shellDiscoveryHas  bool
)

// discoveryLinks renders a cached <ul> of links to the most recent media items
// for the <noscript> fallback. The full canonical list lives in /sitemap.xml;
// this is capped at shellDiscoveryLimit and cached for shellDiscoveryTTL so a
// landing-page hit never deep-copies the whole catalog. Uses ListMediaPage so
// only the page (not every matching item) is copied.
func (h *Handler) discoveryLinks() string {
	shellDiscoveryMu.Lock()
	if shellDiscoveryHas && time.Since(shellDiscoveryAt) < shellDiscoveryTTL {
		out := shellDiscoveryHTML
		shellDiscoveryMu.Unlock()
		return out
	}
	shellDiscoveryMu.Unlock()

	items, total, _ := h.media.ListMediaPage(
		media.Filter{SortBy: "date_added", SortDesc: true},
		shellDiscoveryLimit, 0)

	var b strings.Builder
	b.WriteString(`<ul>`)
	for _, item := range items {
		b.WriteString(`<li><a href="/player?id=` + url.QueryEscape(item.ID) + `">` +
			html.EscapeString(shellMediaTitle(item)) + `</a></li>`)
	}
	b.WriteString(`</ul>`)
	if total > len(items) {
		fmt.Fprintf(&b,
			`<p>Showing %d of %d items — see <a href="/sitemap.xml">the sitemap</a> for the full list.</p>`,
			len(items), total)
	}
	out := b.String()

	shellDiscoveryMu.Lock()
	shellDiscoveryHTML = out
	shellDiscoveryAt = time.Now()
	shellDiscoveryHas = true
	shellDiscoveryMu.Unlock()
	return out
}

// writeMetaProperty / writeMetaName append a single meta tag with an
// HTML-escaped content attribute. The property/name argument is always a
// constant literal at the call sites, so only content needs escaping.
func writeMetaProperty(b *strings.Builder, prop, content string) {
	if content == "" {
		return
	}
	b.WriteString(`<meta property="` + prop + `" content="` + html.EscapeString(content) + `">`)
}

func writeMetaName(b *strings.Builder, name, content string) {
	if content == "" {
		return
	}
	b.WriteString(`<meta name="` + name + `" content="` + html.EscapeString(content) + `">`)
}

// playerJSONLD builds a schema.org VideoObject/AudioObject block. name and
// description are passed raw: json.Marshal handles quoting, and every '<' is
// replaced with its JSON unicode escape so a crafted title cannot terminate the
// <script type="application/ld+json"> block early (mirrors utils/jsonld.ts).
func playerJSONLD(ldType, title, desc, canonical string, item *models.MediaItem, thumb string) string {
	ld := map[string]any{
		"@context":    "https://schema.org",
		"@type":       ldType,
		"name":        title,
		"description": desc,
		"uploadDate":  item.DateAdded.UTC().Format(time.RFC3339),
		"duration":    toISO8601Duration(item.Duration),
	}
	// The watch-page URL helps search engines bind the rich result to a landing
	// page. (We can't expose a public contentUrl/embedUrl — media is age-gated.)
	if canonical != "" {
		ld["url"] = canonical
	}
	if thumb != "" {
		ld["thumbnailUrl"] = thumb
	}
	// Surface view count as a watch interaction so popular items can earn richer
	// SERP treatment.
	if item.Views > 0 {
		ld["interactionStatistic"] = map[string]any{
			"@type":                "InteractionCounter",
			"interactionType":      "https://schema.org/WatchAction",
			"userInteractionCount": item.Views,
		}
	}
	b, err := json.Marshal(ld)
	if err != nil {
		return ""
	}
	safe := strings.ReplaceAll(string(b), "<", "\\u003c")
	return `<script type="application/ld+json">` + safe + `</script>`
}

// shellMediaTitle resolves a human-readable title for SEO. It is intentionally
// lighter than the frontend's getDisplayTitle (utils/mediaTitle.ts): prefer an
// explicit metadata title, else the stored Name with a light filename cleanup.
// Crawlers only need a readable label, not the full noise-token normalisation.
func shellMediaTitle(item *models.MediaItem) string {
	if t := strings.TrimSpace(item.Metadata["title"]); t != "" {
		return t
	}
	if n := strings.TrimSpace(item.Name); n != "" {
		return cleanupShellTitle(n)
	}
	return "Media"
}

var shellMediaExts = []string{
	".mp4", ".mkv", ".webm", ".mov", ".avi", ".m4v",
	".mp3", ".m4a", ".flac", ".wav", ".opus", ".ogg",
}

// cleanupShellTitle strips a trailing known media extension and turns
// underscores into spaces. Deliberately conservative — it leaves hyphens and
// casing alone so genuine titles are not mangled.
func cleanupShellTitle(s string) string {
	low := strings.ToLower(s)
	for _, ext := range shellMediaExts {
		if strings.HasSuffix(low, ext) {
			s = s[:len(s)-len(ext)]
			break
		}
	}
	s = strings.ReplaceAll(s, "_", " ")
	return strings.Join(strings.Fields(s), " ")
}

// shellMediaDescription prefers a stored description, falling back to a concise
// generated sentence. Truncated to keep the meta description within the ~300
// characters search engines display.
func shellMediaDescription(item *models.MediaItem, title string) string {
	if d := strings.TrimSpace(item.Metadata["description"]); d != "" {
		return truncateRunes(d, 300)
	}
	kind := "media"
	switch item.Type {
	case models.MediaTypeVideo:
		kind = "video"
	case models.MediaTypeAudio:
		kind = "audio"
	}
	if cat := strings.TrimSpace(item.Category); cat != "" {
		return fmt.Sprintf("Watch %s — %s in %s on Media Server Pro.", title, kind, cat)
	}
	return fmt.Sprintf("Watch %s — %s on Media Server Pro.", title, kind)
}

// truncateRunes trims s to at most n runes (never splitting a multibyte
// sequence), appending an ellipsis when it shortens the string.
func truncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return strings.TrimSpace(string(r[:n])) + "…"
}

// toISO8601Duration formats a duration in seconds as the schema.org-required
// ISO-8601 form (e.g. "PT1H2M3S"). Mirrors player.vue's toISODuration.
func toISO8601Duration(seconds float64) string {
	s := int(seconds)
	if s <= 0 {
		return "PT0S"
	}
	hrs, mins, secs := s/3600, (s%3600)/60, s%60
	var b strings.Builder
	b.WriteString("PT")
	if hrs > 0 {
		fmt.Fprintf(&b, "%dH", hrs)
	}
	if mins > 0 {
		fmt.Fprintf(&b, "%dM", mins)
	}
	if secs > 0 || (hrs == 0 && mins == 0) {
		fmt.Fprintf(&b, "%dS", secs)
	}
	return b.String()
}
