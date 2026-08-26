package hub

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"media-server-pro/pkg/helpers"
)

// Server-side playback resolution.
//
// A catalog row stores only a provider embed id, so the browser normally loads
// the provider's iframe directly and the provider sees the viewer's IP. Where
// the provider blocks a region that means the embed does not play at all. This
// file turns an embed id into a concrete, playable media URL *on the server* so
// internal/hub/stream.go can fetch the bytes from here instead.
//
// Resolution is deliberately pluggable: the provider can change its page markup
// at any time, and different deployments have different tooling available. The
// chain is configured by name (Hub.ProxyResolvers) and the first resolver to
// return a stream wins.

const (
	// providerPageURL is the canonical watch page for an embed id. It is what a
	// yt-dlp-style detector expects to be handed.
	providerPageURL = "https://www.pornhub.com/view_video.php?viewkey="
	// providerReferer is sent on every upstream fetch. CDNs for this provider
	// commonly reject hotlinked media without a matching Referer.
	providerReferer = "https://www.pornhub.com/"
	// providerUserAgent is a fixed, ordinary browser UA. It is a constant and is
	// never derived from the viewer's own request — see stream.go.
	providerUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 " +
		"(KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"

	// maxPageBytes bounds how much of a provider page is read before giving up,
	// so a hostile or broken response cannot exhaust memory.
	maxPageBytes = 4 << 20 // 4 MiB
	// resolveTimeout bounds a single resolution attempt end to end. This is a
	// short metadata operation, unlike the byte-streaming fetches in stream.go
	// which must never carry a client-level deadline.
	resolveTimeout = 25 * time.Second
)

// Resolution failure modes. Callers distinguish these to decide between a hard
// HTTP error and a soft "not available, keep showing the iframe" response.
var (
	// ErrNotInCatalog means the embed id is not in hub_embeds. This is the
	// open-proxy guard: nothing is ever fetched for an id we did not import.
	ErrNotInCatalog = errors.New("hub: embed is not in the catalog")
	// ErrNoResolver means no configured resolver was usable (e.g. the only
	// configured resolver is the sidecar and it is offline).
	ErrNoResolver = errors.New("hub: no stream resolver available")
	// ErrNoStream means the resolvers ran but found no playable media — most
	// often because the provider blocked this server or removed the video.
	ErrNoStream = errors.New("hub: no playable stream found")
)

// StreamKind is the transport of a resolved stream. It decides whether the
// frontend attaches hls.js or points a <video> element straight at the proxy.
type StreamKind string

const (
	// StreamHLS is an adaptive master playlist (.m3u8).
	StreamHLS StreamKind = "hls"
	// StreamMP4 is a progressive file served with byte ranges.
	StreamMP4 StreamKind = "mp4"
)

// ResolvedStream is a playable upstream URL plus everything needed to fetch it.
// It is cached in memory only and is never sent to the browser: handing the
// signed CDN URL to the client would let the client fetch it directly and
// re-expose their IP, defeating the entire feature.
type ResolvedStream struct {
	Kind StreamKind
	URL  string
	// Quality is a human label ("1080") when the resolver reported one.
	Quality string
	// ResolvedBy names the resolver that produced this, for diagnostics.
	ResolvedBy string
	// ResolvedAt is when this entry was produced; expiry is judged against the
	// live Hub.ProxyCacheTTL so changing the knob takes effect immediately.
	ResolvedAt time.Time
}

// DetectedStream is one candidate media URL reported by an external detector.
// It mirrors the shape a yt-dlp-backed service returns without binding this
// package to any particular one.
type DetectedStream struct {
	URL        string
	Type       string
	Quality    string
	Resolution string
	Size       int64
	IsAd       bool
}

// StreamDetector is an external service that can resolve a watch-page URL into
// candidate media streams. It is declared here, rather than importing the
// downloader package, so the Hub carries no compile-time dependency on that
// feature and the chain stays unit-testable with a fake.
type StreamDetector interface {
	// DetectorReady reports whether the service is configured and reachable.
	DetectorReady() bool
	// DetectStreams resolves pageURL into candidate streams.
	DetectStreams(ctx context.Context, pageURL string) ([]DetectedStream, error)
}

// Resolver turns an embed id into a playable stream.
type Resolver interface {
	// Name is the identifier used in Hub.ProxyResolvers.
	Name() string
	// Available reports whether this resolver can be attempted right now.
	Available() bool
	// Resolve returns a playable stream, or an error if it could not find one.
	Resolve(ctx context.Context, embedID string) (*ResolvedStream, error)
}

// SetStreamDetector wires an external detector (the downloader service) into the
// "sidecar" resolver. Safe to call with nil, which simply leaves that resolver
// unavailable and falls the chain through to the next one.
func (m *Module) SetStreamDetector(d StreamDetector) {
	m.detectorMu.Lock()
	m.detector = d
	m.detectorMu.Unlock()
}

func (m *Module) streamDetector() StreamDetector {
	m.detectorMu.RLock()
	defer m.detectorMu.RUnlock()
	return m.detector
}

// imageProxyEnabled reports whether catalog artwork should be served through the
// server. Independent of proxyEnabled — broken thumbnails are worth fixing even
// when video proxying is off.
func (m *Module) imageProxyEnabled() bool {
	cfg := m.config.Get()
	return cfg.Hub.Enabled && cfg.Hub.ProxyImages
}

// resolveChain builds the ordered resolver list from config. Unknown names are
// skipped so the knob can name a resolver this build does not have without
// breaking playback.
func (m *Module) resolveChain() []Resolver {
	names := m.config.Get().Hub.ProxyResolvers
	if len(names) == 0 {
		names = []string{"sidecar", "page"}
	}
	out := make([]Resolver, 0, len(names))
	for _, raw := range names {
		switch strings.ToLower(strings.TrimSpace(raw)) {
		case "sidecar", "downloader", "detect":
			out = append(out, &sidecarResolver{m: m})
		case "page", "embed", "native":
			out = append(out, &pageResolver{m: m})
		default:
			// Unknown resolver name — ignored by design.
		}
	}
	return out
}

// ResolveStream returns a playable stream for embedID, using the cache when it
// is still fresh. Concurrent callers for the same id share a single resolution.
func (m *Module) ResolveStream(ctx context.Context, embedID string) (*ResolvedStream, error) {
	if rs := m.cachedResolve(embedID); rs != nil {
		return rs, nil
	}
	// Coalesce concurrent misses for the same id so a burst of segment requests
	// after an expiry cannot fan out into N identical upstream resolutions.
	v, err, _ := m.resolveGroup.Do(embedID, func() (any, error) {
		// Re-check under the singleflight leader: a racing caller may have
		// populated the cache between our miss and acquiring the slot.
		if rs := m.cachedResolve(embedID); rs != nil {
			return rs, nil
		}
		// Detach from the leader's request context. Every caller coalesced onto
		// this slot receives whatever it returns, so honouring one viewer's
		// cancellation would fail resolution for the others too — a viewer
		// closing their tab must not knock everyone else back to the iframe.
		// doResolve applies its own resolveTimeout, so this cannot hang.
		rs, rErr := m.doResolve(context.WithoutCancel(ctx), embedID)
		if rErr != nil {
			return nil, rErr
		}
		m.resolveCache.Store(embedID, rs)
		return rs, nil
	})
	if err != nil {
		return nil, err
	}
	rs, ok := v.(*ResolvedStream)
	if !ok || rs == nil {
		return nil, ErrNoStream
	}
	return rs, nil
}

// cachedResolve returns a cached entry when it is still within the configured
// TTL, else nil. Expiry is evaluated against the live config so shortening the
// TTL takes effect without a restart.
func (m *Module) cachedResolve(embedID string) *ResolvedStream {
	v, ok := m.resolveCache.Load(embedID)
	if !ok {
		return nil
	}
	rs, ok := v.(*ResolvedStream)
	if !ok || rs == nil {
		m.resolveCache.Delete(embedID)
		return nil
	}
	ttl := m.config.Get().Hub.ProxyCacheTTL
	if ttl <= 0 {
		ttl = 30 * time.Minute
	}
	if time.Since(rs.ResolvedAt) > ttl {
		m.resolveCache.Delete(embedID)
		return nil
	}
	return rs
}

// invalidateResolve drops the cached stream for embedID along with any cached
// playlists derived from it. Called when the CDN rejects a previously good URL,
// which is the authoritative signal that a signed URL has expired.
func (m *Module) invalidateResolve(embedID string) {
	m.resolveCache.Delete(embedID)
	prefix := embedID + ":"
	m.playlistCache.Range(func(key, _ any) bool {
		if k, ok := key.(string); ok && strings.HasPrefix(k, prefix) {
			m.playlistCache.Delete(key)
		}
		return true
	})
}

// doResolve runs the catalog guard, then the resolver chain. It is only ever
// called through ResolveStream's singleflight.
func (m *Module) doResolve(ctx context.Context, embedID string) (*ResolvedStream, error) {
	repo := m.ready()
	if repo == nil {
		return nil, ErrNoResolver
	}
	// Open-proxy guard: resolution only ever proceeds for an id that is already
	// in our own imported catalog, so a caller cannot steer the server at a URL
	// of their choosing.
	rec, err := repo.GetByEmbedID(ctx, embedID)
	if err != nil {
		return nil, fmt.Errorf("hub: catalog lookup failed: %w", err)
	}
	if rec == nil {
		return nil, ErrNotInCatalog
	}

	// Bound how many distinct embeds resolve at once. Non-blocking: a full queue
	// reports back immediately rather than piling up goroutines on a slow
	// upstream. A nil channel means the Module was assembled without NewModule
	// (only tests do that), in which case there is simply no bound to take.
	if m.resolveSem != nil {
		select {
		case m.resolveSem <- struct{}{}:
			defer func() { <-m.resolveSem }()
		default:
			return nil, errors.New("hub: too many resolutions in progress, try again")
		}
	}

	ctx, cancel := context.WithTimeout(ctx, resolveTimeout)
	defer cancel()

	chain := m.resolveChain()
	if len(chain) == 0 {
		return nil, ErrNoResolver
	}
	attempted := false
	var lastErr error
	for _, r := range chain {
		if !r.Available() {
			continue
		}
		attempted = true
		rs, rErr := r.Resolve(ctx, embedID)
		if rErr != nil {
			lastErr = fmt.Errorf("%s: %w", r.Name(), rErr)
			m.log.Debug("Hub: resolver %s failed for %s: %v", r.Name(), embedID, rErr)
			continue
		}
		if rs != nil && rs.URL != "" {
			m.log.Info("Hub: resolved %s via %s (%s %s)", embedID, r.Name(), rs.Kind, rs.Quality)
			return rs, nil
		}
	}
	if !attempted {
		return nil, ErrNoResolver
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, ErrNoStream
}

// ─── sidecar resolver ────────────────────────────────────────────────────────

// sidecarResolver delegates to an external detector service. Preferred when
// available: such a service typically wraps yt-dlp and may route through its own
// proxy pool, so it survives provider page changes that break direct parsing.
type sidecarResolver struct{ m *Module }

func (r *sidecarResolver) Name() string { return "sidecar" }

func (r *sidecarResolver) Available() bool {
	d := r.m.streamDetector()
	return d != nil && d.DetectorReady()
}

func (r *sidecarResolver) Resolve(ctx context.Context, embedID string) (*ResolvedStream, error) {
	d := r.m.streamDetector()
	if d == nil {
		return nil, ErrNoResolver
	}
	pageURL := providerPageURL + embedID
	if err := helpers.ValidateURLForSSRF(pageURL); err != nil {
		return nil, fmt.Errorf("page URL rejected: %w", err)
	}
	streams, err := d.DetectStreams(ctx, pageURL)
	if err != nil {
		return nil, err
	}
	return pickStream(streams, r.Name())
}

// pickStream chooses the best candidate: adaptive HLS when offered (it lets the
// player adapt without us transcoding), else the largest progressive file.
//
// The detector's Type strings are not a stable contract, so classification also
// sniffs the URL rather than trusting Type alone.
func pickStream(streams []DetectedStream, resolvedBy string) (*ResolvedStream, error) {
	var bestHLS, bestMP4 *DetectedStream
	for i := range streams {
		s := &streams[i]
		if s.IsAd || strings.TrimSpace(s.URL) == "" {
			continue
		}
		// A detector is a third party; treat its output like any other untrusted
		// URL. The connect-time guard in SafeDialContext is the real enforcement
		// (see isSafeUpstreamURL) — this just refuses the obvious cases early.
		if !isSafeUpstreamURL(s.URL) {
			continue
		}
		if looksLikeHLS(s.Type, s.URL) {
			if bestHLS == nil || qualityRank(s) > qualityRank(bestHLS) {
				bestHLS = s
			}
			continue
		}
		if bestMP4 == nil || qualityRank(s) > qualityRank(bestMP4) {
			bestMP4 = s
		}
	}
	switch {
	case bestHLS != nil:
		return &ResolvedStream{
			Kind: StreamHLS, URL: bestHLS.URL, Quality: bestHLS.Quality,
			ResolvedBy: resolvedBy, ResolvedAt: time.Now(),
		}, nil
	case bestMP4 != nil:
		return &ResolvedStream{
			Kind: StreamMP4, URL: bestMP4.URL, Quality: bestMP4.Quality,
			ResolvedBy: resolvedBy, ResolvedAt: time.Now(),
		}, nil
	default:
		return nil, ErrNoStream
	}
}

// looksLikeHLS classifies a candidate by declared type first, then by URL shape.
func looksLikeHLS(mediaType, rawURL string) bool {
	t := strings.ToLower(mediaType)
	if strings.Contains(t, "hls") || strings.Contains(t, "m3u8") || strings.Contains(t, "mpegurl") {
		return true
	}
	u := strings.ToLower(rawURL)
	if i := strings.IndexByte(u, '?'); i >= 0 {
		u = u[:i]
	}
	return strings.HasSuffix(u, ".m3u8")
}

// qualityRank scores a candidate so "best" is comparable across resolvers that
// report quality differently (height, resolution string, or byte size).
func qualityRank(s *DetectedStream) int64 {
	if n := parseLeadingInt(s.Quality); n > 0 {
		return n
	}
	if n := parseLeadingInt(s.Resolution); n > 0 {
		return n
	}
	return s.Size
}

// parseLeadingInt pulls the first run of digits out of s ("1080p" -> 1080,
// "1920x1080" -> 1920). Returns 0 when there are none.
func parseLeadingInt(s string) int64 {
	start := -1
	for i := 0; i < len(s); i++ {
		if s[i] >= '0' && s[i] <= '9' {
			if start < 0 {
				start = i
			}
			continue
		}
		if start >= 0 {
			break
		}
	}
	if start < 0 {
		return 0
	}
	end := start
	for end < len(s) && s[end] >= '0' && s[end] <= '9' {
		end++
	}
	n, err := strconv.ParseInt(s[start:end], 10, 64)
	if err != nil {
		return 0
	}
	return n
}

// ─── page resolver ───────────────────────────────────────────────────────────

// pageResolver fetches the provider's own embed page and reads the media
// definitions the page's player uses. It needs no extra services, which makes it
// the fallback when no detector is deployed — but it parses markup we do not
// control, so it is intentionally second in the default chain.
type pageResolver struct{ m *Module }

func (r *pageResolver) Name() string { return "page" }

// Available is always true: it needs nothing but outbound HTTP.
func (r *pageResolver) Available() bool { return true }

// Resolve tries each candidate page in turn and uses the first that yields a
// stream.
//
// Order matters. The watch page is first because it is the page that actually
// carries the player configuration: /embed/<id> is a thin shell whose player is
// bootstrapped separately, so it frequently contains no flashvars_ object at all
// and parsing it fails with ErrNoStream. Resolving against the embed page alone
// is why this resolver could not stand in for the sidecar on a deployment with no
// downloader service — the fallback silently never produced a stream.
//
// The embed page is still tried second: it costs one extra request only on a path
// that was about to fail outright, and the two pages have historically swapped
// which one carries the definitions.
func (r *pageResolver) Resolve(ctx context.Context, embedID string) (*ResolvedStream, error) {
	candidates := []string{providerPageURL + embedID, embedBaseURL + embedID}
	var lastErr error
	for i, pageURL := range candidates {
		// Share the chain's deadline out across the attempts still to come, so a
		// first candidate that hangs cannot consume the whole budget and leave the
		// fallback with no time to run. Without this, adding a second candidate
		// could turn a formerly-working single fetch into a timeout.
		attemptCtx, cancel := pageAttemptContext(ctx, len(candidates)-i)
		rs, err := r.resolveFromPage(attemptCtx, pageURL)
		cancel()
		if err == nil {
			return rs, nil
		}
		lastErr = err
		r.m.log.Debug("Hub: page resolver found no stream at %s: %v", pageURL, err)
	}
	return nil, lastErr
}

// pageAttemptContext gives one candidate an equal share of the time left on
// ctx's deadline. With no deadline it returns ctx unchanged, so the caller's own
// cancellation still applies.
func pageAttemptContext(ctx context.Context, attemptsLeft int) (context.Context, context.CancelFunc) {
	deadline, ok := ctx.Deadline()
	if !ok || attemptsLeft <= 1 {
		return context.WithCancel(ctx)
	}
	remaining := time.Until(deadline)
	if remaining <= 0 {
		return context.WithCancel(ctx)
	}
	return context.WithTimeout(ctx, remaining/time.Duration(attemptsLeft))
}

// resolveFromPage parses one candidate page into a playable stream.
func (r *pageResolver) resolveFromPage(ctx context.Context, pageURL string) (*ResolvedStream, error) {
	if err := helpers.ValidateURLForSSRF(pageURL); err != nil {
		return nil, fmt.Errorf("embed URL rejected: %w", err)
	}
	body, err := r.m.fetchText(ctx, pageURL, "")
	if err != nil {
		return nil, err
	}
	defs, err := parseMediaDefinitions(body)
	if err != nil {
		return nil, err
	}

	streams := make([]DetectedStream, 0, len(defs))
	for _, d := range defs {
		if d.VideoURL == "" {
			continue
		}
		// A definition whose quality is a list is an index, not a stream: its
		// URL returns the real per-quality list as JSON. Follow it once.
		if d.QualityIsList {
			nested, nErr := r.followQualityIndex(ctx, pageURL, d.VideoURL)
			if nErr != nil {
				r.m.log.Debug("Hub: quality index fetch failed: %v", nErr)
				continue
			}
			streams = append(streams, nested...)
			continue
		}
		streams = append(streams, DetectedStream{
			URL:     absoluteURL(pageURL, d.VideoURL),
			Type:    d.Format,
			Quality: d.Quality,
		})
	}
	if len(streams) == 0 {
		return nil, ErrNoStream
	}
	return pickStream(streams, r.Name())
}

// followQualityIndex resolves an indirection entry into concrete streams.
func (r *pageResolver) followQualityIndex(ctx context.Context, pageURL, indexURL string) ([]DetectedStream, error) {
	target := absoluteURL(pageURL, indexURL)
	if err := helpers.ValidateURLForSSRF(target); err != nil {
		return nil, fmt.Errorf("quality index URL rejected: %w", err)
	}
	body, err := r.m.fetchText(ctx, target, pageURL)
	if err != nil {
		return nil, err
	}
	var entries []struct {
		Format   string `json:"format"`
		Quality  any    `json:"quality"`
		VideoURL string `json:"videoUrl"`
	}
	if err := json.Unmarshal([]byte(body), &entries); err != nil {
		return nil, fmt.Errorf("parse quality index: %w", err)
	}
	out := make([]DetectedStream, 0, len(entries))
	for _, e := range entries {
		if e.VideoURL == "" {
			continue
		}
		q, _ := e.Quality.(string)
		out = append(out, DetectedStream{
			URL:     absoluteURL(target, e.VideoURL),
			Type:    e.Format,
			Quality: q,
		})
	}
	return out, nil
}

// mediaDefinition is one entry of the player's mediaDefinitions array.
type mediaDefinition struct {
	Format   string
	Quality  string
	VideoURL string
	// QualityIsList marks the indirection entry described in Resolve.
	QualityIsList bool
}

// parseMediaDefinitions extracts the player configuration object embedded in the
// page and returns its media definitions.
//
// The page assigns a JSON object to a `flashvars_<id>` variable. We locate that
// assignment and brace-match the object rather than regex it, so nested braces
// inside string values cannot truncate the capture.
func parseMediaDefinitions(page string) ([]mediaDefinition, error) {
	obj, err := extractFlashvarsObject(page)
	if err != nil {
		return nil, err
	}
	var parsed struct {
		MediaDefinitions []struct {
			Format   string          `json:"format"`
			Quality  json.RawMessage `json:"quality"`
			VideoURL string          `json:"videoUrl"`
		} `json:"mediaDefinitions"`
	}
	if err := json.Unmarshal([]byte(obj), &parsed); err != nil {
		return nil, fmt.Errorf("parse player config: %w", err)
	}
	if len(parsed.MediaDefinitions) == 0 {
		return nil, ErrNoStream
	}
	out := make([]mediaDefinition, 0, len(parsed.MediaDefinitions))
	for _, d := range parsed.MediaDefinitions {
		md := mediaDefinition{Format: d.Format, VideoURL: d.VideoURL}
		q := strings.TrimSpace(string(d.Quality))
		switch {
		case strings.HasPrefix(q, "["):
			md.QualityIsList = true
		case strings.HasPrefix(q, `"`):
			var s string
			if json.Unmarshal(d.Quality, &s) == nil {
				md.Quality = s
			}
		default:
			md.Quality = strings.Trim(q, `"`)
		}
		out = append(out, md)
	}
	return out, nil
}

// extractFlashvarsObject finds the first `flashvars_… = { … }` assignment and
// returns the object literal, brace-matched with string awareness.
func extractFlashvarsObject(page string) (string, error) {
	idx := strings.Index(page, "flashvars_")
	if idx < 0 {
		return "", ErrNoStream
	}
	eq := strings.IndexByte(page[idx:], '=')
	if eq < 0 {
		return "", ErrNoStream
	}
	start := idx + eq + 1
	for start < len(page) && (page[start] == ' ' || page[start] == '\t' || page[start] == '\n' || page[start] == '\r') {
		start++
	}
	if start >= len(page) || page[start] != '{' {
		return "", ErrNoStream
	}
	depth := 0
	inStr := false
	escaped := false
	for i := start; i < len(page); i++ {
		c := page[i]
		switch {
		case escaped:
			escaped = false
		case c == '\\' && inStr:
			escaped = true
		case c == '"':
			inStr = !inStr
		case inStr:
			// Braces inside string values must not affect depth.
		case c == '{':
			depth++
		case c == '}':
			depth--
			if depth == 0 {
				return page[start : i+1], nil
			}
		}
	}
	return "", ErrNoStream
}

// fetchText performs a bounded GET and returns the body as a string. Used for
// page/metadata reads only — media bytes stream through stream.go instead.
func (m *Module) fetchText(ctx context.Context, rawURL, referer string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, http.NoBody)
	if err != nil {
		return "", err
	}
	applyUpstreamHeaders(req, referer)
	resp, err := m.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("upstream returned %s for %s", resp.Status, rawURL)
	}
	// +1 so the cap is detectable: silently truncating the page would make the
	// extractors below fail in confusing ways (or, worse, match a partial URL)
	// instead of reporting that the page was too large to parse.
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxPageBytes+1))
	if err != nil {
		return "", err
	}
	if int64(len(body)) > maxPageBytes {
		return "", fmt.Errorf("page exceeds %d bytes: %s", int64(maxPageBytes), rawURL)
	}
	return string(body), nil
}

// applyUpstreamHeaders sets the fixed identity used for every outbound request.
//
// This is the privacy boundary of the whole feature: the request is built fresh
// and only these constants are ever set on it, so no viewer-identifying header
// (X-Forwarded-For, Forwarded, X-Real-IP, their User-Agent, their cookies) can
// reach the provider. Callers must never copy headers from the inbound request
// other than Range.
func applyUpstreamHeaders(req *http.Request, referer string) {
	req.Header.Set("User-Agent", providerUserAgent)
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	if referer == "" {
		referer = providerReferer
	}
	req.Header.Set("Referer", referer)
	req.Header.Set("Origin", strings.TrimSuffix(providerReferer, "/"))
}
