package handlers

import (
	"fmt"
	"strings"
	"testing"
)

func TestRenderSitemap_SingleFileWhenItFits(t *testing.T) {
	urls := []sitemapURLEntry{{Loc: "https://example.com/"}, {Loc: "https://example.com/browse"}}
	sm, err := renderSitemap("https://example.com", urls, 5)
	if err != nil {
		t.Fatal(err)
	}
	if sm.index != nil || len(sm.parts) != 1 {
		t.Fatalf("got index=%v parts=%d, want a single urlset", sm.index != nil, len(sm.parts))
	}
	body := string(sm.parts[0])
	for _, want := range []string{"<urlset", "<loc>https://example.com/</loc>", "<loc>https://example.com/browse</loc>"} {
		if !strings.Contains(body, want) {
			t.Errorf("sitemap missing %q:\n%s", want, body)
		}
	}
}

// Past one file's worth of URLs the sitemap used to be cut off; now every URL
// lands in exactly one numbered part and /sitemap.xml becomes their index.
func TestRenderSitemap_SplitsIntoIndexedParts(t *testing.T) {
	var urls []sitemapURLEntry
	for i := range 5 {
		urls = append(urls, sitemapURLEntry{Loc: fmt.Sprintf("https://example.com/p%d", i)})
	}
	sm, err := renderSitemap("https://example.com", urls, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(sm.parts) != 3 || sm.index == nil {
		t.Fatalf("got %d parts (index=%v), want 3 parts and an index", len(sm.parts), sm.index != nil)
	}
	index := string(sm.index)
	if !strings.Contains(index, "<sitemapindex") {
		t.Errorf("index is not a sitemapindex:\n%s", index)
	}
	for n := 1; n <= 3; n++ {
		if want := fmt.Sprintf("<loc>https://example.com/sitemaps/%d.xml</loc>", n); !strings.Contains(index, want) {
			t.Errorf("index missing %q:\n%s", want, index)
		}
	}
	for i := range 5 {
		loc := fmt.Sprintf("<loc>https://example.com/p%d</loc>", i)
		found := 0
		for _, part := range sm.parts {
			found += strings.Count(string(part), loc)
		}
		if found != 1 {
			t.Errorf("%s appears in %d parts, want exactly 1", loc, found)
		}
	}
}

func TestRenderSitemap_EmptyStillRendersOneFile(t *testing.T) {
	sm, err := renderSitemap("https://example.com", nil, 2)
	if err != nil {
		t.Fatal(err)
	}
	if sm.index != nil || len(sm.parts) != 1 || !strings.Contains(string(sm.parts[0]), "<urlset") {
		t.Fatalf("empty sitemap should be one (empty) urlset, got index=%v parts=%d", sm.index != nil, len(sm.parts))
	}
}

func TestWebsiteJSONLD(t *testing.T) {
	out := websiteJSONLD("https://example.com", "Media <Server> Pro")
	for _, want := range []string{`<script type="application/ld+json">`, `"@type":"WebSite"`, `"url":"https://example.com/"`} {
		if !strings.Contains(out, want) {
			t.Errorf("WebSite JSON-LD missing %q: %s", want, out)
		}
	}
	if strings.Contains(strings.TrimSuffix(strings.TrimPrefix(out, `<script type="application/ld+json">`), `</script>`), "<") {
		t.Errorf("a '<' in the name must be escaped so it can't close the script block: %s", out)
	}
}
