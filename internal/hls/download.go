package hls

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"media-server-pro/pkg/models"
)

// VariantDownloadPath validates that a completed HLS variant exists on disk for
// the given media ID and quality, returning that variant's playlist path. It has
// no side effects and returns an error when HLS/ffmpeg is unavailable or the
// variant isn't ready — letting the caller fall back to the original file.
//
// The job ID equals the media ID (see GenerateHLS), so quality is the only
// untrusted segment; it is rejected if it contains path-traversal components.
func (m *Module) VariantDownloadPath(mediaID, quality string) (string, error) {
	if quality == "" {
		return "", fmt.Errorf("quality is required")
	}
	if strings.Contains(quality, "..") || strings.ContainsAny(quality, `/\`) {
		return "", fmt.Errorf("invalid quality value: %q", quality)
	}
	if m.ffmpegPath == "" {
		return "", fmt.Errorf("ffmpeg unavailable for variant download")
	}
	job, err := m.GetJobStatus(mediaID)
	if err != nil {
		return "", err
	}
	if job.Status != models.HLSStatusCompleted {
		return "", fmt.Errorf("HLS not ready for media %s (status %s)", mediaID, job.Status)
	}
	playlistPath := filepath.Join(job.OutputDir, quality, "playlist.m3u8")
	if _, statErr := os.Stat(playlistPath); statErr != nil {
		return "", fmt.Errorf("variant %q not available: %w", quality, statErr)
	}
	return playlistPath, nil
}

// StreamVariantMP4 remuxes a variant HLS playlist into a fragmented MP4 (stream
// copy, no re-encode) and writes it to w. The caller is expected to have already
// set the response headers. Cancelling ctx (e.g. client disconnect) kills the
// ffmpeg process. RecordAccess is called so a freshly-downloaded variant is not
// immediately evicted by the inactive-jobs cleanup.
func (m *Module) StreamVariantMP4(ctx context.Context, mediaID, playlistPath string, w io.Writer) error {
	if m.ffmpegPath == "" {
		return fmt.Errorf("ffmpeg unavailable")
	}
	m.RecordAccess(mediaID)
	args := []string{
		"-loglevel", "error",
		"-allowed_extensions", "ALL",
		"-i", playlistPath,
		"-c", "copy",
		"-movflags", "frag_keyframe+empty_moov+default_base_moof",
		"-f", "mp4",
		"pipe:1",
	}
	cmd := exec.CommandContext(ctx, m.ffmpegPath, args...) //nolint:gosec // G204: ffmpegPath is the validated binary path; args are fixed flags plus a server-resolved playlist path
	cmd.Stdout = w
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	// Bound the post-cancel wait: when the client disconnects, ctx cancellation
	// kills ffmpeg, but the stdout-copy goroutine can block writing to the dead
	// connection. WaitDelay forces Run() to return instead of hanging on it.
	cmd.WaitDelay = 5 * time.Second
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("ffmpeg remux failed: %w (%s)", err, strings.TrimSpace(stderr.String()))
	}
	return nil
}
