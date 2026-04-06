package hls

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"media-server-pro/pkg/models"
)

// ensureVariantPlaylistExists ensures the variant playlist exists, performing
// lazy transcode if enabled when the playlist is missing.
func (m *Module) ensureVariantPlaylistExists(ctx context.Context, job *models.HLSJob, quality string) (string, error) {
	playlistPath := filepath.Join(job.OutputDir, quality, "playlist.m3u8")
	if _, err := os.Stat(playlistPath); err == nil {
		return playlistPath, nil
	}

	cfg := m.config.Get()
	if !cfg.HLS.LazyTranscode {
		return "", fmt.Errorf("variant playlist not found: %s", quality)
	}

	if err := m.lazyTranscodeQuality(ctx, job, quality); err != nil {
		return "", fmt.Errorf("on-demand transcode failed for %s: %w", quality, err)
	}

	if _, err := os.Stat(playlistPath); err != nil {
		return "", fmt.Errorf("variant playlist not found after on-demand transcode: %s", quality)
	}
	return playlistPath, nil
}

// rewritePlaylistLines rewrites non-comment, non-empty lines to absolute CDN URLs.
func rewritePlaylistLines(data []byte, baseURL string) []byte {
	var buf bytes.Buffer
	for _, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed != "" && !strings.HasPrefix(trimmed, "#") {
			line = baseURL + trimmed
		}
		buf.WriteString(line)
		buf.WriteString("\n")
	}
	return buf.Bytes()
}

// servePlaylistOpts holds parameters for serving a playlist (direct or CDN-rewritten).
type servePlaylistOpts struct {
	path        string
	cdnBase     string
	urlPath     string
	corsOrigin  string // value for Access-Control-Allow-Origin header
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
	for _, o := range cfg.Security.CORSOrigins {
		if o == "*" {
			return "*"
		}
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
	corsOrigin := opts.corsOrigin
	if corsOrigin == "" {
		corsOrigin = "*"
	}
	w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
	w.Header().Set("Access-Control-Allow-Origin", corsOrigin)
	if opts.cdnBase == "" {
		w.Header().Set("Cache-Control", "no-cache")
		if _, err := w.Write(data); err != nil {
			return fmt.Errorf("failed to write playlist: %w", err)
		}
	} else {
		rewritten := rewritePlaylistLines(data, opts.cdnBase+"/hls/"+opts.urlPath+"/")
		w.Header().Set("Cache-Control", "public, max-age=60")
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

	if job.Status != models.HLSStatusCompleted {
		return fmt.Errorf("HLS not ready, status: %s", job.Status)
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
// In lazy transcode mode, if the requested quality hasn't been transcoded yet, it will be transcoded on-demand.
func (m *Module) ServeVariantPlaylist(w http.ResponseWriter, r *http.Request, p VariantPlaylistParams) error {
	job, err := m.GetJobStatus(p.JobID)
	if err != nil {
		return err
	}

	playlistPath, err := m.ensureVariantPlaylistExists(r.Context(), job, p.Quality)
	if err != nil {
		return err
	}

	cfg := m.config.Get()
	opts := servePlaylistOpts{
		path:       playlistPath,
		cdnBase:    cfg.HLS.CDNBaseURL,
		urlPath:    p.JobID + "/" + p.Quality,
		corsOrigin: m.hlsCORSOrigin(r),
	}
	return servePlaylist(w, r, opts)
}

// SegmentParams holds job ID, quality, and segment name for segment requests.
type SegmentParams struct {
	JobID   string
	Quality string
	Segment string
}

// ServeSegment serves an HLS segment. Path traversal is prevented by validating
// that the resolved segment path lies under job.OutputDir via filepath.Rel.
func (m *Module) ServeSegment(w http.ResponseWriter, r *http.Request, p SegmentParams) error {
	job, err := m.GetJobStatus(p.JobID)
	if err != nil {
		return err
	}

	segmentPath := filepath.Join(job.OutputDir, p.Quality, p.Segment)
	cleanOut := filepath.Clean(job.OutputDir)
	cleanSeg := filepath.Clean(segmentPath)
	rel, relErr := filepath.Rel(cleanOut, cleanSeg)
	if relErr != nil || strings.HasPrefix(rel, "..") {
		return fmt.Errorf("segment path outside job directory")
	}
	if _, err := os.Stat(segmentPath); err != nil {
		return fmt.Errorf("segment not found: %s", p.Segment)
	}

	w.Header().Set("Content-Type", "video/mp2t")
	w.Header().Set("Cache-Control", "public, max-age=31536000")
	w.Header().Set("Access-Control-Allow-Origin", m.hlsCORSOrigin(r))
	http.ServeFile(w, r, segmentPath)
	return nil
}
