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
	path    string
	cdnBase string
	urlPath string
}

// servePlaylist writes the playlist to w, rewriting URLs for CDN if cdnBase is set.
// TODO: Bug - when cdnBase is empty, Content-Type and Cache-Control headers are
// set before calling http.ServeFile, but ServeFile will overwrite Content-Type
// based on file extension detection and may add its own Cache-Control. The
// explicit header sets are effectively overridden. Either use http.ServeContent
// with a bytes.Reader for consistent header control, or remove the redundant
// header sets.
func servePlaylist(w http.ResponseWriter, r *http.Request, opts servePlaylistOpts) error {
	if opts.cdnBase == "" {
		w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Access-Control-Allow-Origin", "*")
		http.ServeFile(w, r, opts.path)
		return nil
	}

	data, err := os.ReadFile(opts.path)
	if err != nil {
		return fmt.Errorf("failed to read playlist: %w", err)
	}
	rewritten := rewritePlaylistLines(data, opts.cdnBase+"/hls/"+opts.urlPath+"/")
	w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
	w.Header().Set("Cache-Control", "public, max-age=60")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Write(rewritten)
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
	return servePlaylist(w, r, servePlaylistOpts{path: masterPath, cdnBase: cfg.HLS.CDNBaseURL, urlPath: jobID})
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
	opts := servePlaylistOpts{path: playlistPath, cdnBase: cfg.HLS.CDNBaseURL, urlPath: p.JobID + "/" + p.Quality}
	return servePlaylist(w, r, opts)
}

// SegmentParams holds job ID, quality, and segment name for segment requests.
type SegmentParams struct {
	JobID   string
	Quality string
	Segment string
}

// ServeSegment serves an HLS segment.
// TODO: Bug - p.Quality and p.Segment are used directly in filepath.Join without
// sanitization. A malicious quality or segment value like "../../etc/passwd"
// could escape the job output directory via path traversal. Validate that the
// resulting segmentPath is actually within job.OutputDir, or reject values
// containing ".." or path separators.
func (m *Module) ServeSegment(w http.ResponseWriter, r *http.Request, p SegmentParams) error {
	job, err := m.GetJobStatus(p.JobID)
	if err != nil {
		return err
	}

	segmentPath := filepath.Join(job.OutputDir, p.Quality, p.Segment)
	if _, err := os.Stat(segmentPath); err != nil {
		return fmt.Errorf("segment not found: %s", p.Segment)
	}

	w.Header().Set("Content-Type", "video/mp2t")
	w.Header().Set("Cache-Control", "public, max-age=31536000")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	http.ServeFile(w, r, segmentPath)
	return nil
}
