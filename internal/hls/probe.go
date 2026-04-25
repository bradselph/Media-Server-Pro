package hls

import (
	"context"
	"encoding/json"
	"os/exec"
	"path/filepath"
	"strconv"
	"time"

	ffmpeg "github.com/u2takey/ffmpeg-go"
)

// resolveMediaInputPath converts a stored media path to a form ffmpeg can read.
// Absolute local paths are returned unchanged. S3 object keys are resolved to a
// short-lived presigned HTTPS URL via m.mediaInputResolver when one is set.
// Falls back to the original path on error so ffmpeg produces a clear error message.
func (m *Module) resolveMediaInputPath(ctx context.Context, mediaPath string) string {
	if m.mediaInputResolver == nil || filepath.IsAbs(mediaPath) {
		return mediaPath
	}
	url, err := m.mediaInputResolver.ResolveForFFmpeg(ctx, mediaPath)
	if err != nil {
		m.log.Warn("failed to resolve media input path %q: %v", mediaPath, err)
		return mediaPath
	}
	return url
}

// probeTimeout returns the configured probe deadline with a safe fallback.
func (m *Module) probeTimeout() time.Duration {
	if d := m.config.Get().HLS.ProbeTimeout; d > 0 {
		return d
	}
	return 30 * time.Second
}

// getMediaDuration uses ffprobe to get media duration in seconds. Prefers the context-aware
// exec path when ffprobePath is set so ctx cancellation (e.g. shutdown) is honored.
func (m *Module) getMediaDuration(ctx context.Context, mediaPath string) float64 {
	if mediaPath == "" {
		return 0
	}
	mediaPath = m.resolveMediaInputPath(ctx, mediaPath)
	if m.ffprobePath == "" && m.ffmpegPath == "" {
		return 0
	}

	if m.ffprobePath != "" {
		probeCtx, cancel := context.WithTimeout(ctx, m.probeTimeout())
		defer cancel()
		cmd := exec.CommandContext(probeCtx, m.ffprobePath, //nolint:gosec // G204: ffprobePath validated at startup
			"-v", "quiet",
			"-print_format", "json",
			"-show_format",
			mediaPath,
		)
		output, err := cmd.Output()
		if err == nil {
			if d := m.parseProbeDuration(string(output)); d > 0 {
				return d
			}
		} else {
			m.log.Debug("Failed to probe media duration: %v", err)
		}
	}
	// Fallback when ffprobePath is unset or context path failed (no ctx cancellation in fallback)
	probeJSON, err := ffmpeg.ProbeWithTimeout(mediaPath, m.probeTimeout(), nil)
	if err != nil {
		return 0
	}
	return m.parseProbeDuration(probeJSON)
}

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

// getSourceHeight probes the source media file and returns the video stream height in pixels.
func (m *Module) getSourceHeight(ctx context.Context, mediaPath string) int {
	if mediaPath == "" {
		return 0
	}
	mediaPath = m.resolveMediaInputPath(ctx, mediaPath)
	if m.ffmpegPath == "" && m.ffprobePath == "" {
		return 0
	}

	// Try context-aware ffprobe first so the probe is cancellable on shutdown.
	// Fall back to the library probe (no context) only when ffprobePath is absent.
	if m.ffprobePath != "" {
		probeCtx, cancel := context.WithTimeout(ctx, m.probeTimeout())
		defer cancel()

		cmd := exec.CommandContext(probeCtx, m.ffprobePath, //nolint:gosec // G204: ffprobePath validated at startup
			"-v", "quiet",
			"-print_format", "json",
			"-show_streams",
			"-select_streams", "v:0",
			mediaPath,
		)
		output, err := cmd.Output()
		if err == nil {
			if h := m.parseProbeHeight(string(output)); h > 0 {
				return h
			}
		} else {
			m.log.Debug("ffprobe stream info failed for %s: %v", filepath.Base(mediaPath), err)
		}
	}

	// ffprobePath not configured — fall back to library probe (no context).
	probeJSON, err := ffmpeg.ProbeWithTimeout(mediaPath, m.probeTimeout(), nil)
	if err == nil {
		return m.parseProbeHeight(probeJSON)
	}
	return 0
}

func (m *Module) parseProbeHeight(probeJSON string) int {
	var probe struct {
		Streams []struct {
			Height    int    `json:"height"`
			CodecType string `json:"codec_type"`
		} `json:"streams"`
	}
	if err := json.Unmarshal([]byte(probeJSON), &probe); err != nil {
		return 0
	}
	for _, s := range probe.Streams {
		if s.Height > 0 {
			return s.Height
		}
	}
	return 0
}
