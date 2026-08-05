package hub

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"path"
	"slices"
	"strings"
	"sync"
	"time"
)

// Byte proxying for server-side Hub playback.
//
// Everything here exists to keep one invariant: the provider's origin only ever
// sees this server. That means (a) the browser is only ever handed URLs on this
// server, never the resolved CDN URL, and (b) outbound requests are built from
// scratch so no viewer-identifying header can ride along. Both properties are
// load-bearing — leaking the signed URL to the client would let the client fetch
// it directly and re-expose their IP, defeating the feature entirely.

const (
	mimeHLS       = "application/vnd.apple.mpegurl"
	mimeTS        = "video/mp2t"
	mimeMP4       = "video/mp4"
	mimeBinary    = "application/octet-stream"
	hdrContentTyp = "Content-Type"
	hdrCacheCtl   = "Cache-Control"

	// masterCacheKey suffixes the resolve id for the parsed master playlist.
	masterCacheKey = ":master"

	// playlistTTL bounds how long a parsed playlist is trusted. Segment URLs in
	// it are signed and rotate, so an entry older than this is treated as a miss
	// and the player is nudged to refetch the playlist.
	playlistTTL = 5 * time.Minute

	// maxPlaylistBytes bounds a playlist read. Playlists are text and small; a
	// multi-megabyte one is a malformed or hostile response.
	maxPlaylistBytes = 8 << 20
)

// cachedPlaylist is a parsed playlist: either the variant list from a master, or
// the asset list from a media playlist.
type cachedPlaylist struct {
	variants  []playlistVariant
	assets    []playlistAsset
	fetchedAt time.Time
}

// playlistVariant is one rendition from a master playlist.
type playlistVariant struct {
	originalURL string
}

// playlistAsset is any URI referenced by a media playlist — a segment, an
// encryption key, or an fMP4 initialisation segment. They share one namespace so
// every URI form gets rewritten; a URI left unrewritten would be fetched by the
// browser directly and leak the viewer's IP.
type playlistAsset struct {
	originalURL string
	// name is the opaque, server-assigned filename handed to the player. Assets
	// are looked up by exact name in this table and never used to build a URL,
	// so a crafted name cannot escape to an arbitrary upstream.
	name string
}

// StreamInfo is the capability answer for one embed: enough for the frontend to
// choose a player, and deliberately nothing more. The upstream URL is omitted.
type StreamInfo struct {
	Available bool   `json:"available"`
	Kind      string `json:"kind,omitempty"`
	Quality   string `json:"quality,omitempty"`
	Reason    string `json:"reason,omitempty"`
}

// CheckPlayback resolves an embed and reports whether it can be played through
// the server, without exposing the resolved URL.
func (m *Module) CheckPlayback(ctx context.Context, embedID string) (*StreamInfo, error) {
	rs, err := m.ResolveStream(ctx, embedID)
	if err != nil {
		return nil, err
	}
	return &StreamInfo{Available: true, Kind: string(rs.Kind), Quality: rs.Quality}, nil
}

// ─── HLS ─────────────────────────────────────────────────────────────────────

// ProxyHLSMaster serves the master playlist with every rendition URL rewritten
// to point back at this server.
func (m *Module) ProxyHLSMaster(w http.ResponseWriter, r *http.Request, embedID string) error {
	rs, err := m.ResolveStream(r.Context(), embedID)
	if err != nil {
		return err
	}
	if rs.Kind != StreamHLS {
		return fmt.Errorf("hub: embed %s is not an HLS stream", embedID)
	}

	body, finalURL, err := m.fetchPlaylist(r.Context(), embedID, rs.URL)
	if err != nil {
		return err
	}

	rewritten, variants := rewriteMasterPlaylist(body, finalURL, embedID)
	m.playlistCache.Store(embedID+masterCacheKey, &cachedPlaylist{
		variants: variants, fetchedAt: time.Now(),
	})
	m.writePlaylist(w, r, rewritten)
	return nil
}

// ProxyHLSVariant serves one rendition's media playlist with all of its asset
// URIs rewritten.
func (m *Module) ProxyHLSVariant(w http.ResponseWriter, r *http.Request, embedID string, qualityIdx int) error {
	variantURL, err := m.variantURL(r.Context(), embedID, qualityIdx)
	if err != nil {
		return err
	}
	body, finalURL, err := m.fetchPlaylist(r.Context(), embedID, variantURL)
	if err != nil {
		return err
	}
	rewritten, assets := rewriteMediaPlaylist(body, finalURL, embedID, qualityIdx)
	m.playlistCache.Store(fmt.Sprintf("%s:%d", embedID, qualityIdx), &cachedPlaylist{
		assets: assets, fetchedAt: time.Now(),
	})
	m.writePlaylist(w, r, rewritten)
	return nil
}

// variantURL returns the upstream URL for a rendition index, refetching the
// master playlist if the cached copy is missing or stale.
func (m *Module) variantURL(ctx context.Context, embedID string, qualityIdx int) (string, error) {
	master := m.cachedPlaylist(embedID + masterCacheKey)
	if master == nil {
		rs, err := m.ResolveStream(ctx, embedID)
		if err != nil {
			return "", err
		}
		body, finalURL, fErr := m.fetchPlaylist(ctx, embedID, rs.URL)
		if fErr != nil {
			return "", fErr
		}
		_, variants := rewriteMasterPlaylist(body, finalURL, embedID)
		master = &cachedPlaylist{variants: variants, fetchedAt: time.Now()}
		m.playlistCache.Store(embedID+masterCacheKey, master)
	}
	if qualityIdx < 0 || qualityIdx >= len(master.variants) {
		return "", fmt.Errorf("hub: unknown rendition %d for %s", qualityIdx, embedID)
	}
	return master.variants[qualityIdx].originalURL, nil
}

// ProxyHLSAsset serves one segment, encryption key, or init segment.
//
// Unknown or stale assets return 404 rather than an error: an HLS player treats
// 404 on a segment as "refetch the playlist" (which re-resolves and repopulates
// this table), but treats a 5xx as a fatal stream error.
func (m *Module) ProxyHLSAsset(w http.ResponseWriter, r *http.Request, embedID string, qualityIdx int, name string) error {
	cached := m.cachedPlaylist(fmt.Sprintf("%s:%d", embedID, qualityIdx))
	if cached == nil {
		http.NotFound(w, r)
		return nil
	}
	idx := slices.IndexFunc(cached.assets, func(a playlistAsset) bool { return a.name == name })
	if idx < 0 {
		http.NotFound(w, r)
		return nil
	}
	return m.proxyBytes(w, r, embedID, cached.assets[idx].originalURL, contentTypeFor(name))
}

// ─── progressive MP4 ─────────────────────────────────────────────────────────

// ProxyMP4 streams a progressive file, forwarding byte ranges so the player can
// seek.
func (m *Module) ProxyMP4(w http.ResponseWriter, r *http.Request, embedID string) error {
	rs, err := m.ResolveStream(r.Context(), embedID)
	if err != nil {
		return err
	}
	return m.proxyBytes(w, r, embedID, rs.URL, mimeMP4)
}

// ─── upstream fetching ───────────────────────────────────────────────────────

// fetchPlaylist reads a playlist, re-resolving once if the CDN rejects the URL.
// A rejection is the authoritative signal that a signed URL expired — the TTL is
// only a safety net.
func (m *Module) fetchPlaylist(ctx context.Context, embedID, rawURL string) (body, finalURL string, err error) {
	body, finalURL, err = m.fetchPlaylistOnce(ctx, rawURL)
	if err == nil || !isExpiredUpstream(err) {
		return body, finalURL, err
	}
	m.log.Debug("Hub: upstream rejected playlist for %s, re-resolving", embedID)
	m.invalidateResolve(embedID)
	rs, rErr := m.ResolveStream(ctx, embedID)
	if rErr != nil {
		return "", "", rErr
	}
	return m.fetchPlaylistOnce(ctx, rs.URL)
}

func (m *Module) fetchPlaylistOnce(ctx context.Context, rawURL string) (body, finalURL string, err error) {
	if !isSafeUpstreamURL(rawURL) {
		return "", "", fmt.Errorf("playlist URL rejected: %s", rawURL)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, http.NoBody)
	if err != nil {
		return "", "", err
	}
	applyUpstreamHeaders(req, "")
	resp, err := m.httpClient.Do(req)
	if err != nil {
		return "", "", err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return "", "", &upstreamError{status: resp.StatusCode, url: rawURL}
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxPlaylistBytes))
	if err != nil {
		return "", "", err
	}
	return string(raw), resp.Request.URL.String(), nil
}

// upstreamError carries an upstream status so callers can tell an expired URL
// apart from a transport failure.
type upstreamError struct {
	status int
	url    string
}

func (e *upstreamError) Error() string {
	return fmt.Sprintf("upstream returned %d for %s", e.status, e.url)
}

// isExpiredUpstream reports whether an error means "this URL is no longer valid"
// as opposed to a transient network problem.
func isExpiredUpstream(err error) bool {
	var ue *upstreamError
	if !asUpstream(err, &ue) {
		return false
	}
	return ue.status == http.StatusForbidden ||
		ue.status == http.StatusNotFound ||
		ue.status == http.StatusGone ||
		ue.status == http.StatusUnauthorized
}

// asUpstream is a tiny errors.As wrapper kept local so the hot path avoids
// pulling reflection into every call site.
func asUpstream(err error, target **upstreamError) bool {
	for err != nil {
		if ue, ok := err.(*upstreamError); ok { //nolint:errorlint // direct match is the only wrap shape produced here
			*target = ue
			return true
		}
		u, ok := err.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		err = u.Unwrap()
	}
	return false
}

// proxyBytes streams an upstream resource to the client.
//
// Only Range is carried over from the inbound request; the outbound request is
// otherwise built from scratch (see applyUpstreamHeaders), which is what
// guarantees no viewer-identifying header reaches the provider. Response headers
// are copied through a strict allow-list so upstream infrastructure headers are
// not echoed to the browser.
func (m *Module) proxyBytes(w http.ResponseWriter, r *http.Request, embedID, targetURL, fallbackType string) error {
	resp, err := m.openUpstream(r.Context(), r, targetURL)
	if err != nil {
		return err
	}
	// A rejected URL mid-playback means the signature expired: re-resolve once
	// and retry before giving up, so a long video does not simply die.
	if isRetryableStatus(resp.StatusCode) {
		_ = resp.Body.Close()
		m.invalidateResolve(embedID)
		rs, rErr := m.ResolveStream(r.Context(), embedID)
		if rErr != nil {
			return rErr
		}
		if resp, err = m.openUpstream(r.Context(), r, rs.URL); err != nil {
			return err
		}
	}
	defer func() { _ = resp.Body.Close() }()

	copyAllowedHeaders(w.Header(), resp.Header)
	if w.Header().Get(hdrContentTyp) == "" {
		w.Header().Set(hdrContentTyp, fallbackType)
	}
	// Media bytes are immutable for the life of a signed URL but the URL itself
	// rotates, so let the browser reuse them briefly without pinning them.
	if w.Header().Get(hdrCacheCtl) == "" {
		w.Header().Set(hdrCacheCtl, "private, max-age=60")
	}
	if origin := m.corsOrigin(r); origin != "" {
		w.Header().Set("Access-Control-Allow-Origin", origin)
	}

	w.WriteHeader(resp.StatusCode)
	if _, err := io.Copy(w, resp.Body); err != nil {
		// The client going away mid-stream is normal (seek, tab close) and must
		// not be logged as a server fault.
		if r.Context().Err() != nil {
			return nil
		}
		return fmt.Errorf("hub: stream copy failed: %w", err)
	}
	return nil
}

// openUpstream issues the outbound GET, forwarding only Range.
func (m *Module) openUpstream(ctx context.Context, r *http.Request, targetURL string) (*http.Response, error) {
	if !isSafeUpstreamURL(targetURL) {
		return nil, fmt.Errorf("stream URL rejected: %s", targetURL)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, targetURL, http.NoBody)
	if err != nil {
		return nil, err
	}
	applyUpstreamHeaders(req, "")
	if rng := r.Header.Get("Range"); rng != "" {
		req.Header.Set("Range", rng)
	}
	resp, err := m.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("hub: upstream fetch failed: %w", err)
	}
	return resp, nil
}

func isRetryableStatus(code int) bool {
	return code == http.StatusForbidden || code == http.StatusUnauthorized || code == http.StatusGone
}

// allowedProxyHeaders is the exact set echoed from upstream to the browser.
// Anything else (Server, Set-Cookie, CDN edge/debug headers) is dropped.
var allowedProxyHeaders = map[string]bool{
	"Content-Type":   true,
	"Content-Length": true,
	"Content-Range":  true,
	"Accept-Ranges":  true,
	"Last-Modified":  true,
	"Etag":           true,
	"Cache-Control":  true,
}

func copyAllowedHeaders(dst, src http.Header) {
	for key, values := range src {
		if !allowedProxyHeaders[http.CanonicalHeaderKey(key)] {
			continue
		}
		for _, v := range values {
			dst.Add(key, v)
		}
	}
}

// writePlaylist emits a rewritten playlist. Playlists are never cached by the
// browser: their asset URLs rotate with the signed upstream URL.
func (m *Module) writePlaylist(w http.ResponseWriter, r *http.Request, body string) {
	w.Header().Set(hdrContentTyp, mimeHLS)
	w.Header().Set(hdrCacheCtl, "no-store")
	if origin := m.corsOrigin(r); origin != "" {
		w.Header().Set("Access-Control-Allow-Origin", origin)
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(body))
}

// corsOrigin mirrors how the other media proxies in this codebase answer CORS,
// so a deployment serving the SPA from another origin behaves consistently.
func (m *Module) corsOrigin(r *http.Request) string {
	cfg := m.config.Get()
	if !cfg.Security.CORSEnabled || len(cfg.Security.CORSOrigins) == 0 {
		return "*"
	}
	if slices.Contains(cfg.Security.CORSOrigins, "*") {
		return "*"
	}
	origin := r.Header.Get("Origin")
	if origin == "" {
		return "*"
	}
	if slices.ContainsFunc(cfg.Security.CORSOrigins, func(a string) bool { return strings.EqualFold(a, origin) }) {
		return origin
	}
	return ""
}

// cachedPlaylist returns a parsed playlist when it is still fresh, else nil and
// evicts the stale entry so the next request repopulates it.
func (m *Module) cachedPlaylist(key string) *cachedPlaylist {
	v, ok := m.playlistCache.Load(key)
	if !ok {
		return nil
	}
	cp, ok := v.(*cachedPlaylist)
	if !ok || cp == nil {
		m.playlistCache.Delete(key)
		return nil
	}
	if time.Since(cp.fetchedAt) > playlistTTL {
		m.playlistCache.Delete(key)
		return nil
	}
	return cp
}

// ─── playlist rewriting ──────────────────────────────────────────────────────

// rewriteMasterPlaylist replaces every rendition URI with a local one.
//
// A resolved URL is not guaranteed to be a master playlist — some sources hand
// back a media playlist directly. When no rendition is found the playlist is
// reported as a single rendition pointing at itself, so the variant route can
// serve it unchanged.
func rewriteMasterPlaylist(body, baseURL, embedID string) (string, []playlistVariant) {
	var (
		variants  []playlistVariant
		out       strings.Builder
		expectURI bool
	)
	scanner := bufio.NewScanner(strings.NewReader(body))
	scanner.Buffer(make([]byte, 0, 64*1024), maxPlaylistBytes)
	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)

		// Alternate renditions (audio/subtitle tracks) carry their playlist in a
		// URI attribute. Missing these would leave the audio track loading
		// straight from the provider.
		if strings.HasPrefix(trimmed, "#EXT-X-MEDIA:") {
			rewritten, uri := rewriteURIAttr(trimmed, baseURL, func(string) string {
				idx := len(variants)
				variants = append(variants, playlistVariant{})
				return fmt.Sprintf("/hub/proxy/%s/%d/playlist.m3u8", url.PathEscape(embedID), idx)
			})
			if uri != "" {
				variants[len(variants)-1] = playlistVariant{originalURL: uri}
			}
			out.WriteString(rewritten)
			out.WriteString("\n")
			continue
		}

		if strings.HasPrefix(trimmed, "#EXT-X-STREAM-INF:") {
			expectURI = true
			out.WriteString(line)
			out.WriteString("\n")
			continue
		}

		if expectURI && trimmed != "" && !strings.HasPrefix(trimmed, "#") {
			expectURI = false
			abs := absoluteURL(baseURL, trimmed)
			if !isSafeUpstreamURL(abs) {
				continue
			}
			idx := len(variants)
			variants = append(variants, playlistVariant{originalURL: abs})
			fmt.Fprintf(&out, "/hub/proxy/%s/%d/playlist.m3u8\n", url.PathEscape(embedID), idx)
			continue
		}

		out.WriteString(line)
		out.WriteString("\n")
	}

	if len(variants) == 0 {
		variants = append(variants, playlistVariant{originalURL: baseURL})
	}
	return out.String(), variants
}

// rewriteMediaPlaylist replaces every asset URI — segments, encryption keys and
// fMP4 init segments — with a local one.
//
// Every URI form matters: an unrewritten key or init-segment URI would be
// fetched by the browser directly, which both leaks the viewer's IP and fails
// for exactly the blocked viewers this feature exists to serve.
func rewriteMediaPlaylist(body, baseURL, embedID string, qualityIdx int) (string, []playlistAsset) {
	var (
		assets []playlistAsset
		out    strings.Builder
	)
	local := func(kind, original, hintURL string) string {
		name := fmt.Sprintf("%s%d%s", kind, len(assets), urlExt(hintURL))
		assets = append(assets, playlistAsset{originalURL: original, name: name})
		return fmt.Sprintf("/hub/proxy/%s/%d/%s", url.PathEscape(embedID), qualityIdx, name)
	}

	scanner := bufio.NewScanner(strings.NewReader(body))
	scanner.Buffer(make([]byte, 0, 64*1024), maxPlaylistBytes)
	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)

		switch {
		case trimmed == "":
			out.WriteString("\n")

		case strings.HasPrefix(trimmed, "#EXT-X-KEY:"), strings.HasPrefix(trimmed, "#EXT-X-SESSION-KEY:"):
			// METHOD=NONE has no URI; rewriteURIAttr leaves such lines alone.
			rewritten, _ := rewriteURIAttr(trimmed, baseURL, func(abs string) string {
				// Keys have no meaningful extension; name them uniformly.
				return local("key", abs, "k.bin")
			})
			out.WriteString(rewritten)
			out.WriteString("\n")

		case strings.HasPrefix(trimmed, "#EXT-X-MAP:"):
			rewritten, _ := rewriteURIAttr(trimmed, baseURL, func(abs string) string {
				return local("map", abs, abs)
			})
			out.WriteString(rewritten)
			out.WriteString("\n")

		case strings.HasPrefix(trimmed, "#"):
			out.WriteString(line)
			out.WriteString("\n")

		default:
			abs := absoluteURL(baseURL, trimmed)
			if !isSafeUpstreamURL(abs) {
				continue
			}
			out.WriteString(local("seg", abs, abs))
			out.WriteString("\n")
		}
	}
	return out.String(), assets
}

// rewriteURIAttr replaces the URI="…" attribute of an HLS tag using replace,
// which receives the absolute upstream URL and returns the local path. Returns
// the rewritten line and the absolute upstream URL (empty when there was none).
func rewriteURIAttr(line, baseURL string, replace func(abs string) string) (rewritten, upstreamURL string) {
	const marker = `URI="`
	i := strings.Index(line, marker)
	if i < 0 {
		return line, ""
	}
	start := i + len(marker)
	end := strings.IndexByte(line[start:], '"')
	if end < 0 {
		return line, ""
	}
	raw := line[start : start+end]
	abs := absoluteURL(baseURL, raw)
	if raw == "" || !isSafeUpstreamURL(abs) {
		return line, ""
	}
	return line[:start] + replace(abs) + line[start+end:], abs
}

// isSafeUpstreamURL is a cheap structural check on a URL we are about to fetch.
//
// It deliberately does NOT resolve DNS. A media playlist carries hundreds of
// segment URIs, so a lookup per URI would add hundreds of resolutions to every
// playlist rewrite — and worse, a momentary DNS failure would make the check
// reject good segments, silently dropping them from the playlist we hand the
// player. That fails as a broken video rather than a slow one.
//
// The authoritative guard is the transport: helpers.SafeHTTPTransport dials via
// helpers.SafeDialContext, which resolves and refuses private, loopback and
// link-local addresses at connect time, on every hop including redirects. That
// is strictly stronger than a one-off pre-check, because it also closes the DNS
// rebinding window a pre-check leaves open.
func isSafeUpstreamURL(raw string) bool {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return false
	}
	host := strings.ToLower(u.Hostname())
	if host == "" || host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return false
	}
	// Reject literal addresses that name our own network outright, so a malformed
	// playlist cannot even queue such a request.
	if ip := net.ParseIP(host); ip != nil {
		return ip.IsGlobalUnicast() && !ip.IsPrivate() && !ip.IsLoopback() && !ip.IsLinkLocalUnicast()
	}
	return true
}

// absoluteURL resolves ref against base, leaving already-absolute URLs alone.
func absoluteURL(base, ref string) string {
	ref = strings.TrimSpace(ref)
	if strings.HasPrefix(ref, "http://") || strings.HasPrefix(ref, "https://") {
		return ref
	}
	b, err := url.Parse(base)
	if err != nil {
		return ref
	}
	r, err := url.Parse(ref)
	if err != nil {
		return ref
	}
	return b.ResolveReference(r).String()
}

// urlExt returns the file extension of a URL's path, ignoring any query string.
//
// Taking path.Ext of the whole URL would fold the query into the extension —
// and CDN media URLs are signed, so they essentially always carry one. That
// produced asset names like "seg0.ts?e=1699&h=abc", which broke both the
// generated playlist path and the name lookup that serves it.
func urlExt(raw string) string {
	if u, err := url.Parse(raw); err == nil {
		return path.Ext(u.Path)
	}
	if i := strings.IndexAny(raw, "?#"); i >= 0 {
		raw = raw[:i]
	}
	return path.Ext(raw)
}

// contentTypeFor maps a server-assigned asset name to a media type.
//
// Names are generated by rewriteMediaPlaylist, so both the kind prefix and the
// extension set are closed. The prefix is the fallback because plenty of CDN
// segment URLs have no extension at all.
func contentTypeFor(name string) string {
	switch strings.ToLower(path.Ext(name)) {
	case ".ts":
		return mimeTS
	case ".m4s", ".mp4", ".m4v":
		return mimeMP4
	case ".m4a", ".aac":
		return "audio/aac"
	case ".vtt":
		return "text/vtt"
	}
	switch {
	case strings.HasPrefix(name, "key"):
		return mimeBinary
	case strings.HasPrefix(name, "map"):
		return mimeMP4
	case strings.HasPrefix(name, "seg"):
		return mimeTS
	default:
		return mimeBinary
	}
}

// clearProxyCaches drops all resolution and playlist state. Called on shutdown
// so no resolved upstream URL outlives the module.
func (m *Module) clearProxyCaches() {
	clearSyncMap(&m.resolveCache)
	clearSyncMap(&m.playlistCache)
}

func clearSyncMap(sm *sync.Map) {
	sm.Range(func(key, _ any) bool {
		sm.Delete(key)
		return true
	})
}
