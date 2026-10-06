package handlers

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"media-server-pro/pkg/models"
)

// Link-preview bots (Discord, iMessage, X, Telegram, WhatsApp, Slack, ...) and
// search crawlers fetch shared URLs without a session; the preview tags the
// server renders for them are shown to everyone the link is shared with.
// These tests pin that a mature item's preview is neutral and image-free,
// while the text search engines index stays real (adult-content-only site).

const (
	matureTitle = "Explicit Title XYZ"
	matureDesc  = "explicit description text"
	matureThumb = "/thumbnail?id=mature-1"
)

func matureItem() *models.MediaItem {
	return &models.MediaItem{
		ID:           "mature-1",
		Name:         "explicit_file_name.mp4",
		Type:         models.MediaTypeVideo,
		IsMature:     true,
		ThumbnailURL: matureThumb,
		Metadata:     map[string]string{"title": matureTitle, "description": matureDesc},
		DateAdded:    time.Now(),
		Duration:     600,
		Views:        42,
	}
}

// previewTagContent returns the content of every OpenGraph/Twitter tag in head
// — what link-preview cards are built from.
func previewTagContent(head string) []string {
	re := regexp.MustCompile(`<meta (?:property="og:[^"]*"|name="twitter:[^"]*") content="([^"]*)">`)
	var out []string
	for _, m := range re.FindAllStringSubmatch(head, -1) {
		out = append(out, m[1])
	}
	return out
}

func TestPlayerShellMeta_MaturePreviewIsNeutralAndImageFree(t *testing.T) {
	m := playerShellMeta("https://example.com", matureItem())

	// Preview cards: neutral copy only.
	tags := previewTagContent(m.Head)
	if len(tags) == 0 {
		t.Fatalf("mature shell has no preview tags (apps would fall back to <title>):\n%s", m.Head)
	}
	for _, content := range tags {
		for _, leak := range []string{matureTitle, matureDesc, "explicit_file_name", "explicit file name"} {
			if strings.Contains(content, leak) {
				t.Errorf("preview tag content %q leaks %q", content, leak)
			}
		}
	}
	// No image anywhere, no large-image card, no structured data.
	all := m.Title + m.Description + m.Head + m.NoScript
	for _, leak := range []string{matureThumb, "thumbnail", "og:image", "twitter:image", "summary_large_image", "ld+json", "<img"} {
		if strings.Contains(all, leak) {
			t.Errorf("mature shell carries %q:\n%s", leak, all)
		}
	}
	for _, want := range []string{`property="og:title" content="` + matureShellTitle + `"`, `name="twitter:card" content="summary"`, `rel="canonical" href="https://example.com/player?id=mature-1"`} {
		if !strings.Contains(m.Head, want) {
			t.Errorf("mature shell head missing %q:\n%s", want, m.Head)
		}
	}
}

// The site is adult-content-only and its pages are meant to be found: the
// document title/description (what search engines index) stay real, and the
// page is not noindexed.
func TestPlayerShellMeta_MatureItemStaysIndexable(t *testing.T) {
	m := playerShellMeta("https://example.com", matureItem())
	if m.Title != matureTitle {
		t.Errorf("<title> = %q, want the real title %q", m.Title, matureTitle)
	}
	if !strings.Contains(m.Description, matureDesc) {
		t.Errorf("meta description = %q, want the real description", m.Description)
	}
	if strings.Contains(m.Head, "noindex") {
		t.Errorf("mature player pages must stay indexable:\n%s", m.Head)
	}
	if !strings.Contains(m.NoScript, matureTitle) {
		t.Errorf("noscript fallback should carry the title for JS-less crawlers:\n%s", m.NoScript)
	}
}

func TestPlayerShellMeta_NonMatureItemKeepsRichPreview(t *testing.T) {
	item := matureItem()
	item.IsMature = false
	m := playerShellMeta("https://example.com", item)
	for _, want := range []string{matureTitle, `property="og:image" content="https://example.com/thumbnail?id=mature-1"`, "summary_large_image", "ld+json"} {
		if !strings.Contains(m.Title+m.Head, want) {
			t.Errorf("non-mature shell missing %q:\n%s", want, m.Head)
		}
	}
	if strings.Contains(m.Head, "og=1") {
		t.Error("og:image must not carry the removed og=1 marker")
	}
	if strings.Contains(m.Head, "noindex") {
		t.Error("non-mature item pages must stay indexable")
	}
}

func TestDiscoveryListHTML_TextLinksOnly(t *testing.T) {
	safe := &models.MediaItem{ID: "safe-1", Name: "Safe Title", Metadata: map[string]string{}}
	out := discoveryListHTML([]*models.MediaItem{safe, matureItem()}, 2)
	for _, want := range []string{"Safe Title", matureTitle, "/player?id=mature-1"} {
		if !strings.Contains(out, want) {
			t.Errorf("discovery list missing %q: %s", want, out)
		}
	}
	if strings.Contains(out, "<img") || strings.Contains(out, "thumbnail") {
		t.Errorf("discovery list must not carry images: %s", out)
	}
}

// Mature player pages are listed (adult-content-only site), but their gated
// thumbnails are never submitted to image search.
func TestAppendSitemapMediaEntries_ListsMatureItemsWithoutImages(t *testing.T) {
	safe := &models.MediaItem{ID: "safe-1", ThumbnailURL: "/thumbnail?id=safe-1", DateAdded: time.Now()}
	urls := appendSitemapMediaEntries(nil, "https://example.com", []*models.MediaItem{matureItem(), safe})
	if len(urls) != 2 {
		t.Fatalf("sitemap entries = %+v, want both items", urls)
	}
	if !strings.HasSuffix(urls[0].Loc, "id=mature-1") || urls[0].Image != nil {
		t.Errorf("mature entry = %+v, want its page without an image", urls[0])
	}
	if urls[1].Image == nil || urls[1].Image.Loc != "https://example.com/thumbnail?id=safe-1" {
		t.Errorf("safe item image = %+v", urls[1].Image)
	}
}

// The thumbnail handlers once honoured ?og=1 to serve real mature thumbnails
// to link-preview bots — and to anyone who appended it. Guard against any
// handler reading such a bypass parameter again.
func TestNoMatureGateBypassQueryParam(t *testing.T) {
	bypass := regexp.MustCompile(`\.Query\("og"\)|isOGImageRequest`)
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		if loc := bypass.FindIndex(src); loc != nil {
			t.Errorf("%s reads an og bypass parameter (offset %d): mature thumbnails must be gated for every request", f, loc[0])
		}
	}
}

// A shared /categories/<id> link must not show mature titles to a viewer who
// may not see mature content (the listing carries each member's name).
func TestWithoutMatureCategoryItems(t *testing.T) {
	items := []categoryItemResponse{
		{MediaID: "safe", MediaName: "Safe"},
		{MediaID: "local-mature", MediaName: "Explicit Local"},
		{MediaID: "fed-mature", MediaName: "Explicit Federated"},
		{MediaID: "fed-safe", MediaName: "Federated Safe"},
	}
	got := withoutMatureCategoryItems(items,
		map[string]bool{"local-mature": true},
		func(id string) bool { return id == "fed-mature" })
	if len(got) != 2 || got[0].MediaID != "safe" || got[1].MediaID != "fed-safe" {
		t.Fatalf("got %+v, want only the safe local and federated items", got)
	}
}

// A public playlist opened from a shared link must not list mature or hub
// items for a viewer who may not see mature content — and stripping them for
// one caller must not alter the playlist other callers see.
func TestWithoutMaturePlaylistItems(t *testing.T) {
	items := []models.PlaylistItem{
		{MediaID: "safe", Title: "Safe"},
		{MediaID: "mature", Title: "Explicit"},
		{MediaID: hubItemPrefix + "abc", Title: "Hub Explicit"},
		{MediaID: "safe-2", Title: "Safe 2"},
	}
	got := withoutMaturePlaylistItems(items, func(id string) bool { return id == "mature" })
	if len(got) != 2 || got[0].MediaID != "safe" || got[1].MediaID != "safe-2" {
		t.Fatalf("got %+v, want only the two safe items", got)
	}
	if items[1].MediaID != "mature" || items[2].MediaID != hubItemPrefix+"abc" {
		t.Errorf("input slice was modified in place: %+v", items)
	}
}
