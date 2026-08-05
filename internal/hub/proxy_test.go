package hub

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"media-server-pro/internal/config"
	"media-server-pro/internal/repositories"
)

// newProxyRequest builds a recorder plus a GET request for the proxy handlers.
func newProxyRequest(t *testing.T, target string) (*httptest.ResponseRecorder, *http.Request) {
	t.Helper()
	return httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, target, http.NoBody)
}

// Tests for server-side Hub playback.
//
// The property most of these protect is the one the whole feature rests on: the
// browser must never be handed a provider URL, and the server must never fetch a
// URL the caller chose. Everything else is recoverable; those two are not.

// ─── test doubles ────────────────────────────────────────────────────────────

// catalogRepo is a fake catalog holding a fixed set of embed ids.
type catalogRepo struct {
	fakeHubRepo
	byID map[string]*repositories.HubEmbedRecord
}

func (c *catalogRepo) GetByEmbedID(_ context.Context, embedID string) (*repositories.HubEmbedRecord, error) {
	return c.byID[embedID], nil
}

// stubDetector is a fake external resolver.
type stubDetector struct {
	ready   bool
	streams []DetectedStream
	err     error
	calls   int
	lastURL string
}

func (s *stubDetector) DetectorReady() bool { return s.ready }

func (s *stubDetector) DetectStreams(_ context.Context, pageURL string) ([]DetectedStream, error) {
	s.calls++
	s.lastURL = pageURL
	return s.streams, s.err
}

// proxyTestModule builds a Module with the Hub enabled and one catalog entry.
func proxyTestModule(t *testing.T, embedIDs ...string) *Module {
	t.Helper()
	cfg := config.NewManager(filepath.Join(t.TempDir(), "config.json"))
	if err := cfg.Update(func(c *config.Config) {
		// Hub.Enabled is derived from the feature flag on every load
		// (internal/config/config.go), so setting it directly would not stick.
		c.Features.EnableHub = true
		c.Hub.ProxyEnabled = true
		c.Hub.ProxyResolvers = []string{"sidecar"}
	}); err != nil {
		t.Fatalf("configure hub: %v", err)
	}
	repo := &catalogRepo{byID: map[string]*repositories.HubEmbedRecord{}}
	for _, id := range embedIDs {
		repo.byID[id] = &repositories.HubEmbedRecord{EmbedID: id, Title: id}
	}
	m := NewModule(cfg, nil)
	m.repo = repo
	return m
}

// ─── catalog guard ───────────────────────────────────────────────────────────

// An id that is not in our own catalog must be rejected before anything reaches
// the network. This is what stops the proxy endpoints becoming an open relay.
func TestResolveStream_RejectsEmbedNotInCatalog(t *testing.T) {
	m := proxyTestModule(t, "known")
	det := &stubDetector{ready: true, streams: []DetectedStream{{URL: "https://cdn.example.com/a.m3u8", Type: "hls"}}}
	m.SetStreamDetector(det)

	_, err := m.ResolveStream(context.Background(), "not-in-catalog")
	if !errors.Is(err, ErrNotInCatalog) {
		t.Fatalf("want ErrNotInCatalog, got %v", err)
	}
	if det.calls != 0 {
		t.Fatalf("resolver was called %d times for an unknown embed; it must not be reached", det.calls)
	}
}

// With no usable resolver the caller gets a distinct error so the API can answer
// "not available" and leave the iframe in place, rather than reporting a fault.
func TestResolveStream_NoResolverAvailable(t *testing.T) {
	m := proxyTestModule(t, "known")
	m.SetStreamDetector(&stubDetector{ready: false})

	if _, err := m.ResolveStream(context.Background(), "known"); !errors.Is(err, ErrNoResolver) {
		t.Fatalf("want ErrNoResolver, got %v", err)
	}
}

// A second call inside the TTL must not hit the resolver again.
func TestResolveStream_CachesResult(t *testing.T) {
	m := proxyTestModule(t, "known")
	det := &stubDetector{ready: true, streams: []DetectedStream{{URL: "https://cdn.example.com/a.m3u8", Type: "hls"}}}
	m.SetStreamDetector(det)

	for i := range 3 {
		if _, err := m.ResolveStream(context.Background(), "known"); err != nil {
			t.Fatalf("resolve %d: %v", i, err)
		}
	}
	if det.calls != 1 {
		t.Fatalf("resolver called %d times, want 1 (results should be cached)", det.calls)
	}

	// Invalidation is what recovers from an expired signed URL.
	m.invalidateResolve("known")
	if _, err := m.ResolveStream(context.Background(), "known"); err != nil {
		t.Fatalf("resolve after invalidate: %v", err)
	}
	if det.calls != 2 {
		t.Fatalf("resolver called %d times after invalidation, want 2", det.calls)
	}
}

// The resolver is handed the canonical watch page for the embed id.
func TestResolveStream_UsesCanonicalPageURL(t *testing.T) {
	m := proxyTestModule(t, "abc123")
	det := &stubDetector{ready: true, streams: []DetectedStream{{URL: "https://cdn.example.com/a.m3u8", Type: "hls"}}}
	m.SetStreamDetector(det)

	if _, err := m.ResolveStream(context.Background(), "abc123"); err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if !strings.HasSuffix(det.lastURL, "abc123") || !strings.HasPrefix(det.lastURL, "https://") {
		t.Fatalf("unexpected page URL handed to resolver: %q", det.lastURL)
	}
}

// ─── stream selection ────────────────────────────────────────────────────────

func TestPickStream(t *testing.T) {
	tests := []struct {
		name     string
		streams  []DetectedStream
		wantKind StreamKind
		wantURL  string
		wantErr  bool
	}{
		{
			name: "prefers HLS over MP4 so the player can adapt",
			streams: []DetectedStream{
				{URL: "https://cdn.example.com/720.mp4", Type: "mp4", Quality: "720"},
				{URL: "https://cdn.example.com/master.m3u8", Type: "hls"},
			},
			wantKind: StreamHLS,
			wantURL:  "https://cdn.example.com/master.m3u8",
		},
		{
			name: "classifies by URL when the type string is unhelpful",
			streams: []DetectedStream{
				{URL: "https://cdn.example.com/master.m3u8?sig=x", Type: "video"},
			},
			wantKind: StreamHLS,
			wantURL:  "https://cdn.example.com/master.m3u8?sig=x",
		},
		{
			name: "skips advertisements",
			streams: []DetectedStream{
				{URL: "https://ads.example.com/ad.m3u8", Type: "hls", IsAd: true},
				{URL: "https://cdn.example.com/720.mp4", Type: "mp4", Quality: "720"},
			},
			wantKind: StreamMP4,
			wantURL:  "https://cdn.example.com/720.mp4",
		},
		{
			name: "takes the highest quality among progressive files",
			streams: []DetectedStream{
				{URL: "https://cdn.example.com/480.mp4", Type: "mp4", Quality: "480p"},
				{URL: "https://cdn.example.com/1080.mp4", Type: "mp4", Quality: "1080p"},
				{URL: "https://cdn.example.com/720.mp4", Type: "mp4", Quality: "720p"},
			},
			wantKind: StreamMP4,
			wantURL:  "https://cdn.example.com/1080.mp4",
		},
		{
			name:    "no candidates is an error, not a nil stream",
			streams: []DetectedStream{},
			wantErr: true,
		},
		{
			name:    "entries with no URL are not playable",
			streams: []DetectedStream{{URL: "", Type: "hls"}},
			wantErr: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := pickStream(tc.streams, "test")
			if tc.wantErr {
				if err == nil {
					t.Fatalf("want error, got %+v", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("pickStream: %v", err)
			}
			if got.Kind != tc.wantKind {
				t.Errorf("kind = %q, want %q", got.Kind, tc.wantKind)
			}
			if got.URL != tc.wantURL {
				t.Errorf("url = %q, want %q", got.URL, tc.wantURL)
			}
		})
	}
}

// A resolver is a third party, so anything it returns pointing at our own
// network is refused exactly like caller-supplied input would be.
func TestPickStream_RejectsInternalTargets(t *testing.T) {
	_, err := pickStream([]DetectedStream{
		{URL: "http://127.0.0.1:8080/admin", Type: "mp4"},
		{URL: "http://localhost/secrets.m3u8", Type: "hls"},
	}, "test")
	if err == nil {
		t.Fatal("expected internal addresses to be rejected")
	}
}

// ─── playlist rewriting ──────────────────────────────────────────────────────

const masterPlaylist = `#EXTM3U
#EXT-X-MEDIA:TYPE=AUDIO,GROUP-ID="aud",NAME="English",URI="https://cdn.example.com/audio/eng.m3u8"
#EXT-X-STREAM-INF:BANDWIDTH=800000,RESOLUTION=640x360
360/index.m3u8
#EXT-X-STREAM-INF:BANDWIDTH=3000000,RESOLUTION=1920x1080
https://cdn.example.com/1080/index.m3u8
`

const mediaPlaylist = `#EXTM3U
#EXT-X-TARGETDURATION:6
#EXT-X-KEY:METHOD=AES-128,URI="https://cdn.example.com/key.bin",IV=0x0
#EXT-X-MAP:URI="https://cdn.example.com/init.mp4"
#EXTINF:6.0,
seg1.ts
#EXTINF:6.0,
https://cdn.example.com/abs/seg2.ts
`

// Every rendition URI in a master playlist must be replaced, including the
// alternate audio rendition — an unrewritten one is fetched by the browser
// directly, which is exactly what this feature exists to prevent.
func TestRewriteMasterPlaylist_ReplacesEveryURI(t *testing.T) {
	out, variants := rewriteMasterPlaylist(masterPlaylist, "https://cdn.example.com/base/master.m3u8", "vid1")

	if strings.Contains(out, "cdn.example.com") {
		t.Fatalf("upstream host leaked into the master playlist served to the browser:\n%s", out)
	}
	if len(variants) != 3 {
		t.Fatalf("captured %d renditions, want 3 (audio + two video)", len(variants))
	}
	for _, v := range variants {
		if !strings.HasPrefix(v.originalURL, "https://cdn.example.com/") {
			t.Errorf("rendition URL not resolved to absolute upstream: %q", v.originalURL)
		}
	}
	// The stream tags themselves must survive so the player still sees the ladder.
	if strings.Count(out, "#EXT-X-STREAM-INF:") != 2 {
		t.Errorf("stream tags were not preserved:\n%s", out)
	}
	if !strings.Contains(out, "/hub/proxy/vid1/") {
		t.Errorf("renditions were not pointed back at this server:\n%s", out)
	}
}

// Segments, encryption keys and fMP4 init segments must all be rewritten. A key
// or init URI left alone both leaks the viewer's IP and fails for the blocked
// viewers this feature serves.
func TestRewriteMediaPlaylist_ReplacesSegmentsKeysAndInit(t *testing.T) {
	out, assets := rewriteMediaPlaylist(mediaPlaylist, "https://cdn.example.com/base/index.m3u8", "vid1", 0)

	if strings.Contains(out, "cdn.example.com") {
		t.Fatalf("upstream host leaked into the media playlist served to the browser:\n%s", out)
	}
	if len(assets) != 4 {
		t.Fatalf("captured %d assets, want 4 (key + init + two segments)", len(assets))
	}

	var haveKey, haveMap, segments int
	for _, a := range assets {
		switch {
		case strings.HasPrefix(a.name, "key"):
			haveKey++
		case strings.HasPrefix(a.name, "map"):
			haveMap++
		case strings.HasPrefix(a.name, "seg"):
			segments++
		}
	}
	if haveKey != 1 || haveMap != 1 || segments != 2 {
		t.Errorf("asset mix = key:%d map:%d seg:%d, want 1/1/2", haveKey, haveMap, segments)
	}
	// A relative segment must resolve against the playlist it came from.
	if !strings.Contains(assets[2].originalURL, "/base/seg1.ts") {
		t.Errorf("relative segment resolved to %q", assets[2].originalURL)
	}
	// Tags carrying the rewritten URIs must otherwise survive intact.
	if !strings.Contains(out, "METHOD=AES-128") || !strings.Contains(out, "IV=0x0") {
		t.Errorf("key tag attributes were damaged by rewriting:\n%s", out)
	}
}

// Real CDN media URLs are signed, so they carry a query string. Deriving the
// asset extension from the raw URL folded that query into the name
// ("seg0.ts?e=1699&h=abc"), which broke both the playlist path handed to the
// player and the exact-name lookup that serves it.
func TestRewriteMediaPlaylist_HandlesSignedURLs(t *testing.T) {
	signed := `#EXTM3U
#EXT-X-KEY:METHOD=AES-128,URI="https://cdn.example.com/key?token=abc"
#EXT-X-MAP:URI="https://cdn.example.com/init.mp4?validfrom=1&sig=xyz"
#EXTINF:6.0,
seg1.ts?e=1699999999&h=deadbeef
#EXTINF:6.0,
https://cdn.example.com/seg/00012?token=xyz
`
	out, assets := rewriteMediaPlaylist(signed, "https://cdn.example.com/base/index.m3u8", "vid1", 0)

	if len(assets) != 4 {
		t.Fatalf("captured %d assets, want 4", len(assets))
	}
	for _, a := range assets {
		if strings.ContainsAny(a.name, "?&#/") {
			t.Errorf("asset name %q carries query characters; it must be a clean path segment", a.name)
		}
		// The query must survive on the upstream URL — it is the signature.
		if strings.Contains(a.originalURL, "token=") || strings.Contains(a.originalURL, "sig=") ||
			strings.Contains(a.originalURL, "h=deadbeef") {
			continue
		}
		t.Errorf("upstream URL lost its signature: %q", a.originalURL)
	}
	if strings.Contains(out, "cdn.example.com") {
		t.Errorf("upstream host leaked into the playlist:\n%s", out)
	}

	// Extensions must still be recovered so segments get a sane content type.
	if got := contentTypeFor(assets[2].name); got != mimeTS {
		t.Errorf("content type for %q = %q, want %q", assets[2].name, got, mimeTS)
	}
	// An extensionless segment falls back on its kind rather than octet-stream.
	if got := contentTypeFor(assets[3].name); got != mimeTS {
		t.Errorf("content type for extensionless %q = %q, want %q", assets[3].name, got, mimeTS)
	}
	// The init segment is fMP4.
	if got := contentTypeFor(assets[1].name); got != mimeMP4 {
		t.Errorf("content type for %q = %q, want %q", assets[1].name, got, mimeMP4)
	}
}

func TestURLExt(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"https://cdn.example.com/init.mp4", ".mp4"},
		{"https://cdn.example.com/init.mp4?validfrom=1&sig=abc", ".mp4"},
		{"https://cdn.example.com/seg1.ts?e=1699999999&h=deadbeef", ".ts"},
		{"https://cdn.example.com/seg/00012?token=xyz", ""},
		{"https://cdn.example.com/seg#frag", ""},
	} {
		if got := urlExt(tc.in); got != tc.want {
			t.Errorf("urlExt(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// Asset names are server-assigned and looked up in a table, so a crafted name
// cannot be turned into a request for something else.
func TestProxyHLSAsset_UnknownNameIsNotFetched(t *testing.T) {
	m := proxyTestModule(t, "vid1")
	_, assets := rewriteMediaPlaylist(mediaPlaylist, "https://cdn.example.com/base/index.m3u8", "vid1", 0)

	for _, a := range assets {
		if strings.ContainsAny(a.name, "/\\") || strings.Contains(a.name, "..") {
			t.Fatalf("asset name %q is not a safe single path segment", a.name)
		}
	}
	// Nothing cached for this rendition: an asset request must 404 (which makes
	// the player refetch the playlist) rather than reach upstream.
	rec, req := newProxyRequest(t, "/hub/proxy/vid1/0/../../etc/passwd")
	if err := m.ProxyHLSAsset(rec, req, "vid1", 0, "../../etc/passwd"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rec.Code != 404 {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

// ─── player config parsing ───────────────────────────────────────────────────

func TestExtractFlashvarsObject(t *testing.T) {
	// A brace inside a string value must not end the object early — this is why
	// the extractor brace-matches with string awareness instead of using a regex.
	page := `<script>var flashvars_12345 = {"title":"a } trap","mediaDefinitions":[]};</script>`
	obj, err := extractFlashvarsObject(page)
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	if !strings.HasSuffix(obj, "}") || !strings.Contains(obj, "mediaDefinitions") {
		t.Fatalf("object truncated at the brace inside a string: %q", obj)
	}

	if _, err := extractFlashvarsObject("<html>no player config here</html>"); !errors.Is(err, ErrNoStream) {
		t.Fatalf("want ErrNoStream for a page with no config, got %v", err)
	}
}

func TestParseMediaDefinitions(t *testing.T) {
	page := `var flashvars_1 = {"mediaDefinitions":[
		{"format":"hls","quality":["1080","720"],"videoUrl":"https://cdn.example.com/index"},
		{"format":"mp4","quality":"720","videoUrl":"https://cdn.example.com/720.mp4"}
	]};`
	defs, err := parseMediaDefinitions(page)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(defs) != 2 {
		t.Fatalf("got %d definitions, want 2", len(defs))
	}
	// A list-valued quality marks the indirection entry, which must be followed
	// rather than treated as a playable URL.
	if !defs[0].QualityIsList {
		t.Error("entry with a quality list was not marked as an index")
	}
	if defs[1].QualityIsList || defs[1].Quality != "720" {
		t.Errorf("concrete entry parsed as %+v", defs[1])
	}
}

func TestParseLeadingInt(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want int64
	}{
		{"1080p", 1080},
		{"1920x1080", 1920},
		{"720", 720},
		{"", 0},
		{"auto", 0},
	} {
		if got := parseLeadingInt(tc.in); got != tc.want {
			t.Errorf("parseLeadingInt(%q) = %d, want %d", tc.in, got, tc.want)
		}
	}
}

// ─── image proxying ──────────────────────────────────────────────────────────

// With artwork proxying on, the API must hand out local URLs only: a provider
// URL in the payload would be fetched by the browser and defeat the point.
func TestToEmbed_ProxiesArtworkURLs(t *testing.T) {
	m := proxyTestModule(t, "vid1")
	if err := m.config.Update(func(c *config.Config) { c.Hub.ProxyImages = true }); err != nil {
		t.Fatalf("enable image proxy: %v", err)
	}
	rec := &repositories.HubEmbedRecord{
		EmbedID:     "vid1",
		ThumbURL:    "https://cdn.example.com/thumb.jpg",
		PreviewURLs: "https://cdn.example.com/p1.jpg;https://cdn.example.com/p2.jpg",
	}

	got := m.toEmbed(rec)
	if strings.Contains(got.ThumbURL, "cdn.example.com") {
		t.Errorf("thumbnail still points at the provider: %q", got.ThumbURL)
	}
	for _, p := range got.PreviewURLs {
		if strings.Contains(p, "cdn.example.com") {
			t.Errorf("preview still points at the provider: %q", p)
		}
	}
	// The iframe fallback still needs the provider's own embed URL.
	if !strings.Contains(got.EmbedURL, "pornhub.com") {
		t.Errorf("embed URL should remain the provider's iframe: %q", got.EmbedURL)
	}
}

// With proxying off the payload is unchanged, so the switch is genuinely inert.
func TestToEmbed_LeavesArtworkAloneWhenDisabled(t *testing.T) {
	m := proxyTestModule(t, "vid1")
	if err := m.config.Update(func(c *config.Config) { c.Hub.ProxyImages = false }); err != nil {
		t.Fatalf("disable image proxy: %v", err)
	}
	rec := &repositories.HubEmbedRecord{EmbedID: "vid1", ThumbURL: "https://cdn.example.com/thumb.jpg"}

	if got := m.toEmbed(rec); got.ThumbURL != rec.ThumbURL {
		t.Errorf("thumbnail = %q, want it untouched (%q)", got.ThumbURL, rec.ThumbURL)
	}
}

func TestImageCache_EvictsOldestPastBudget(t *testing.T) {
	c := newImageCache(100)
	c.put("a", make([]byte, 20), "image/jpeg")
	c.put("b", make([]byte, 20), "image/jpeg")
	if _, _, ok := c.get("a"); !ok {
		t.Fatal("entry a should still be cached")
	}
	// Touching "a" makes "b" the eviction candidate.
	c.put("c", make([]byte, 20), "image/jpeg")
	c.put("d", make([]byte, 20), "image/jpeg")
	c.put("e", make([]byte, 20), "image/jpeg")
	c.put("f", make([]byte, 20), "image/jpeg")
	if _, _, ok := c.get("b"); ok {
		t.Error("entry b should have been evicted once the budget was exceeded")
	}

	// A single oversized image must not be able to flush the whole cache.
	c2 := newImageCache(100)
	c2.put("small", make([]byte, 10), "image/jpeg")
	c2.put("huge", make([]byte, 90), "image/jpeg")
	if _, _, ok := c2.get("small"); !ok {
		t.Error("an oversized entry evicted the rest of the cache")
	}
	if _, _, ok := c2.get("huge"); ok {
		t.Error("an oversized entry should not be cached at all")
	}
}

// A zero-size cache disables caching without any nil handling at call sites.
func TestImageCache_DisabledIsSafe(t *testing.T) {
	c := newImageCache(0)
	c.put("a", []byte("x"), "image/jpeg")
	if _, _, ok := c.get("a"); ok {
		t.Error("a zero-budget cache should never report a hit")
	}
	var nilCache *imageCache
	nilCache.put("a", []byte("x"), "image/jpeg")
	if _, _, ok := nilCache.get("a"); ok {
		t.Error("a nil cache should never report a hit")
	}
	nilCache.clear()
}
