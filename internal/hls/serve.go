package hls

import (
	"bytes"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"media-server-pro/pkg/models"
)

const headerCacheControl = "Cache-Control"

// headerXAccelBuffering disables response buffering on nginx (or any
// X-Accel-capable proxy) in front of the server, matching the direct-play
// headers in internal/streaming, so segments reach the player as they are read.
const headerXAccelBuffering = "X-Accel-Buffering"

// ErrNotReady is returned by ServeMasterPlaylist when the HLS job exists but
// transcoding has not yet completed. Callers should respond with 503 (not 404)
// so HLS-aware clients know to retry.
var ErrNotReady = errors.New("HLS job not yet ready")

// ensureVariantPlaylistExists ensures the variant playlist exists. In lazy
// transcode mode, when the playlist is missing it dispatches (or joins) a
// background on-demand transcode of that quality and returns ErrNotReady
// immediately — it never blocks the caller for the encode (X01). The caller
// (ServeVariantPlaylist) maps ErrNotReady to a 503 so the player retries
// shortly; by the time it does, either the background encode has finished
// (the fast os.Stat path below succeeds) or it is still running (another 503).
func (m *Module) ensureVariantPlaylistExists(job *models.HLSJob, quality string) (string, error) {
	// Reject quality values that contain path traversal components. The router
	// splits on '/' so a literal slash cannot appear, but a single ".." is
	// enough to escape the job directory. This mirrors the guard in ServeSegment.
	if strings.Contains(quality, "..") || strings.ContainsAny(quality, "/\\") {
		return "", fmt.Errorf("%w: invalid quality value %q", os.ErrNotExist, quality)
	}
	playlistPath := filepath.Join(job.OutputDir, quality, "playlist.m3u8")
	if _, err := os.Stat(playlistPath); err == nil {
		return playlistPath, nil
	}

	cfg := m.config.Get()
	if !cfg.HLS.LazyTranscode {
		if job.Status != models.HLSStatusCompleted {
			// Still transcoding the ladder in order (see transcode()): this
			// quality just hasn't been reached yet. 503 tells the player to
			// retry shortly instead of treating it as a permanent 404 — the same
			// signal ServeMasterPlaylist gives before the job is even Available.
			return "", fmt.Errorf("%w: quality %s not yet transcoded", ErrNotReady, quality)
		}
		return "", fmt.Errorf("%w: variant playlist %s", os.ErrNotExist, quality)
	}

	// Never block this request for the encode: kick off (or join, via
	// triggerLazyTranscode's TryLock-based dedup) a background, low-priority
	// transcode of this quality and tell the caller to retry shortly.
	m.triggerLazyTranscode(job, quality)
	return "", fmt.Errorf("%w: quality %s is being transcoded on demand", ErrNotReady, quality)
}

// rewritePlaylistLines rewrites non-comment, non-empty (URI) lines: prefixed
// with baseURL (absolute CDN URLs; "" keeps them relative) and, when version is
// set, tagged with a v=<version> query parameter. Segment names containing path
// traversal components ("..") or newline characters are skipped — they should
// never appear in FFmpeg-generated manifests.
func rewritePlaylistLines(data []byte, baseURL, version string) []byte {
	var buf bytes.Buffer
	for line := range strings.SplitSeq(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed != "" && !strings.HasPrefix(trimmed, "#") {
			if strings.Contains(trimmed, "..") || strings.ContainsAny(trimmed, "\r\n") {
				// Malformed segment — skip rather than forward to the player.
				continue
			}
			line = baseURL + trimmed
			if version != "" {
				sep := "?"
				if strings.Contains(trimmed, "?") {
					sep = "&"
				}
				line += sep + "v=" + version
			}
		}
		buf.WriteString(line)
		buf.WriteString("\n")
	}
	return buf.Bytes()
}

// servePlaylistOpts holds parameters for serving a playlist (direct or CDN-rewritten).
type servePlaylistOpts struct {
	path       string
	cdnBase    string
	urlPath    string
	corsOrigin string // value for Access-Control-Allow-Origin header
	// version, when set, is appended to every URI as ?v=<version> — see
	// variantPlaylistVersion.
	version string
}

// hlsCORSOrigin computes the correct Access-Control-Allow-Origin header value for
// HLS responses based on the server CORS configuration and the incoming Origin header.
//
// HLS content must be accessible to all media players (browser, mobile, CDN) so it
// always sets some ACAO header. When the operator has configured specific allowed
// origins we reflect a matching origin (or omit the header for non-matching requests).
// When no specific origins are configured, we fall back to "*".
func (m *Module) hlsCORSOrigin(r *http.Request) string {
	cfg := m.config.Get()
	if !cfg.Security.CORSEnabled || len(cfg.Security.CORSOrigins) == 0 {
		return "*"
	}
	if slices.Contains(cfg.Security.CORSOrigins, "*") {
		return "*"
	}
	// Operator has configured specific origins — reflect a matching one.
	requestOrigin := r.Header.Get("Origin")
	if requestOrigin == "" {
		return "*" // direct player request without Origin header; allow it
	}
	for _, allowed := range cfg.Security.CORSOrigins {
		if strings.EqualFold(allowed, requestOrigin) {
			return requestOrigin
		}
	}
	// Origin is set but not in the allow-list; return empty so the CORS header is
	// omitted. Browsers will block the cross-origin request; native players that
	// send an Origin header unexpectedly are blocked too, which is the safer default.
	return ""
}

// servePlaylist writes the playlist to w, rewriting URLs for CDN if cdnBase is set.
// When cdnBase is empty, the file is read and written with explicit headers so
// Content-Type and Cache-Control are not overwritten by http.ServeFile.
func servePlaylist(w http.ResponseWriter, _ *http.Request, opts servePlaylistOpts) error {
	data, err := os.ReadFile(opts.path)
	if err != nil {
		return fmt.Errorf("failed to read playlist: %w", err)
	}
	w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
	w.Header().Set(headerXAccelBuffering, "no")
	// Omit the CORS header when empty: hlsCORSOrigin returns "" only to DENY a
	// configured-but-non-matching Origin. Defaulting to "*" would turn that deny
	// into allow-all and defeat the operator's CORS allow-list.
	if opts.corsOrigin != "" {
		w.Header().Set("Access-Control-Allow-Origin", opts.corsOrigin)
	}
	if opts.cdnBase == "" {
		w.Header().Set(headerCacheControl, "no-cache")
		if opts.version != "" {
			data = rewritePlaylistLines(data, "", opts.version)
		}
		if _, err := w.Write(data); err != nil {
			return fmt.Errorf("failed to write playlist: %w", err)
		}
	} else {
		rewritten := rewritePlaylistLines(data, opts.cdnBase+"/hls/"+opts.urlPath+"/", opts.version)
		w.Header().Set(headerCacheControl, "public, max-age=60")
		if _, err := w.Write(rewritten); err != nil {
			return fmt.Errorf("failed to write rewritten playlist: %w", err)
		}
	}
	return nil
}

// ServeMasterPlaylist serves the master HLS playlist.
// When CDNBaseURL is configured, variant paths are rewritten to absolute CDN URLs.
func (m *Module) ServeMasterPlaylist(w http.ResponseWriter, r *http.Request, jobID string) error {
	job, err := m.GetJobStatus(jobID)
	if err != nil {
		return err
	}

	// Available (not Status=="completed") is the servability gate: a job
	// becomes playable as soon as its first quality finishes — see
	// markJobPlayable — and stays servable afterward even if it later ends up
	// Canceled (e.g. a server shutdown after already becoming playable — see
	// finalizeAfterQualityFailure) as long as master.m3u8 still lists a real,
	// completed subset.
	if !job.Available {
		return fmt.Errorf("%w: status=%s", ErrNotReady, job.Status)
	}

	masterPath := filepath.Join(job.OutputDir, masterPlaylistName)
	cfg := m.config.Get()
	return servePlaylist(w, r, servePlaylistOpts{
		path:       masterPath,
		cdnBase:    cfg.HLS.CDNBaseURL,
		urlPath:    jobID,
		corsOrigin: m.hlsCORSOrigin(r),
	})
}

// VariantPlaylistParams holds job ID and quality for variant playlist requests.
type VariantPlaylistParams struct {
	JobID   string
	Quality string
}

// ServeVariantPlaylist serves a variant HLS playlist.
// In lazy transcode mode, if the requested quality hasn't been transcoded yet,
// a background on-demand transcode is dispatched and this returns ErrNotReady
// (503) immediately — see ensureVariantPlaylistExists/triggerLazyTranscode.
func (m *Module) ServeVariantPlaylist(w http.ResponseWriter, r *http.Request, p VariantPlaylistParams) error {
	job, err := m.GetJobStatus(p.JobID)
	if err != nil {
		return err
	}

	playlistPath, err := m.ensureVariantPlaylistExists(job, p.Quality)
	if err != nil {
		return err
	}

	cfg := m.config.Get()
	opts := servePlaylistOpts{
		path:       playlistPath,
		cdnBase:    cfg.HLS.CDNBaseURL,
		urlPath:    p.JobID + "/" + p.Quality,
		corsOrigin: m.hlsCORSOrigin(r),
		version:    variantPlaylistVersion(playlistPath),
	}
	return servePlaylist(w, r, opts)
}

// variantPlaylistVersion identifies one encode of a variant: its playlist's
// modification time, which changes whenever the quality is (re)encoded.
//
// Segment URLs are otherwise stable across encodes — the job ID is the media
// ID and ffmpeg numbers segments from zero — while ServeSegment lets clients
// cache them for a year. Without a version, a viewer who watched before the
// HLS was regenerated (new segment duration or encoder settings, replaced
// source) would splice year-cached old segments into the new playlist: wrong
// timing, decode errors, stalls. Returns "" when the playlist can't be
// stat'ed, which leaves the URIs untouched.
func variantPlaylistVersion(playlistPath string) string {
	fi, err := os.Stat(playlistPath)
	if err != nil {
		return ""
	}
	return strconv.FormatInt(fi.ModTime().UnixNano(), 36)
}

// SegmentParams holds job ID, quality, and segment name for segment requests.
type SegmentParams struct {
	JobID   string
	Quality string
	Segment string
}

// ServeSegment serves an HLS segment. Path traversal is prevented by rejecting
// ".." / slash components in the path parameters and then confirming the resolved
// segment path stays under the quality directory via strings.HasPrefix.
func (m *Module) ServeSegment(w http.ResponseWriter, r *http.Request, p SegmentParams) error {
	job, err := m.GetJobStatus(p.JobID)
	if err != nil {
		return err
	}

	// Reject traversal components before joining: a single ".." in :quality
	// collapses job.OutputDir to the cache root and would defeat the prefix
	// check below (mirrors the guard in ensureVariantPlaylistExists).
	// These (and the containment/existence checks below) wrap os.ErrNotExist
	// so the handler answers 404 — a request for a segment that isn't there —
	// instead of logging a server error and returning 500 for each of them.
	if strings.Contains(p.Quality, "..") || strings.ContainsAny(p.Quality, "/\\") {
		return fmt.Errorf("%w: invalid quality value %q", os.ErrNotExist, p.Quality)
	}
	if strings.Contains(p.Segment, "..") || strings.ContainsAny(p.Segment, "/\\") {
		return fmt.Errorf("%w: invalid segment name %q", os.ErrNotExist, p.Segment)
	}

	// Reject a quality whose own playlist.m3u8 doesn't exist yet: ffmpeg only
	// writes it once that quality's whole encode finishes (hls_playlist_type=
	// vod in buildFFmpegTranscodeCmd), so a missing playlist here means the
	// variant dir is still mid-encode and must not be served even if some
	// segments already exist on disk — mirrors the completed-variant gate in
	// ensureVariantPlaylistExists.
	if _, err := os.Stat(filepath.Join(job.OutputDir, p.Quality, "playlist.m3u8")); err != nil {
		return fmt.Errorf("%w: quality %s not yet ready", ErrNotReady, p.Quality)
	}

	segmentPath := filepath.Join(job.OutputDir, p.Quality, p.Segment)
	cleanOut := filepath.Clean(job.OutputDir)
	cleanSeg := filepath.Clean(segmentPath)
	qualityDir := filepath.Join(cleanOut, filepath.Clean(p.Quality))
	if !strings.HasPrefix(cleanSeg, qualityDir+string(filepath.Separator)) {
		return fmt.Errorf("%w: segment path outside quality directory", os.ErrNotExist)
	}
	if _, err := os.Stat(segmentPath); err != nil {
		return fmt.Errorf("%w: segment %s", os.ErrNotExist, p.Segment)
	}

	w.Header().Set("Content-Type", "video/mp2t")
	w.Header().Set(headerXAccelBuffering, "no")
	w.Header().Set(headerCacheControl, "public, max-age=31536000")
	if origin := m.hlsCORSOrigin(r); origin != "" {
		w.Header().Set("Access-Control-Allow-Origin", origin)
	}
	http.ServeFile(w, r, segmentPath)
	return nil
}
