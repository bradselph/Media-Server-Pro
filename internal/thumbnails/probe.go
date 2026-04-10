package thumbnails

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"strconv"
	"time"

	ffmpeg "github.com/u2takey/ffmpeg-go"
)

// resolveMediaInputPath converts a stored media path to a form ffmpeg can read.
// Absolute local paths are returned unchanged. S3 object keys are resolved to a
// short-lived presigned HTTPS URL via m.mediaInputResolver when one is set.
// Uses context.Background() because callers lack a request context.
func (m *Module) resolveMediaInputPath(mediaPath string) string {
	if m.mediaInputResolver == nil || filepath.IsAbs(mediaPath) {
		return mediaPath
	}
	url, err := m.mediaInputResolver.ResolveForFFmpeg(context.Background(), mediaPath)
	if err != nil {
		m.log.Warn("failed to resolve media input path %q: %v", mediaPath, err)
		return mediaPath
	}
	return url
}

// getMediaDuration uses ffmpeg-go Probe to get duration
func (m *Module) getMediaDuration(path string) (float64, error) {
	path = m.resolveMediaInputPath(path)
	// Try ffmpeg-go Probe first, using the explicit ffprobe path when available
	// so this works under systemd (which strips PATH to a minimal set).
	var probeJSON string
	var err error
	const probeTimeout = 15 * time.Second
	if m.ffprobePath != "" {
		probeJSON, err = ffmpeg.ProbeWithTimeout(path, probeTimeout, ffmpeg.KwArgs{"cmd": m.ffprobePath})
	} else {
		probeJSON, err = ffmpeg.ProbeWithTimeout(path, probeTimeout, nil)
	}
	if err == nil {
		duration := m.parseProbeDuration(probeJSON)
		if duration > 0 {
			return duration, nil
		}
	}

	// Fallback to raw ffprobe if available
	if m.ffprobePath == "" {
		return 0, fmt.Errorf("ffprobe not available and ffmpeg-go probe failed: %w", err)
	}

	m.log.Debug("ffmpeg-go probe failed, trying raw ffprobe: %v", err)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, m.ffprobePath, //nolint:gosec // G204: ffprobePath validated at startup
		"-v", "error",
		"-show_entries", "format=duration",
		"-of", "default=noprint_wrappers=1:nokey=1",
		path,
	)
	output, err := cmd.Output()
	if err != nil {
		return 0, err
	}

	var duration float64
	if _, err := fmt.Sscanf(string(output), "%f", &duration); err != nil {
		return 0, err
	}

	return duration, nil
}

// parseProbeDuration extracts duration from ffprobe JSON output
func (m *Module) parseProbeDuration(probeJSON string) float64 {
	var probe struct {
		Format struct {
			Duration string `json:"duration"`
		} `json:"format"`
	}
	if err := json.Unmarshal([]byte(probeJSON), &probe); err != nil {
		m.log.Debug("Failed to parse ffprobe JSON output: %v", err)
		return 0
	}
	duration, err := strconv.ParseFloat(probe.Format.Duration, 64)
	if err != nil {
		return 0
	}
	return duration
}
