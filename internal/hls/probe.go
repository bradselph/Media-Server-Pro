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

// getMediaDuration uses ffmpeg-go's Probe to get media duration in seconds.
func (m *Module) getMediaDuration(ctx context.Context, mediaPath string) float64 {
	if m.ffprobePath == "" && m.ffmpegPath == "" {
		return 0
	}

	probeJSON, err := ffmpeg.ProbeWithTimeout(mediaPath, 15*time.Second, nil)
	if err == nil {
		duration := m.parseProbeDuration(probeJSON)
		if duration > 0 {
			return duration
		}
	}
	m.log.Debug("ffmpeg-go probe failed, trying raw ffprobe: %v", err)

	if m.ffprobePath == "" {
		return 0
	}

	probeCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	cmd := exec.CommandContext(probeCtx, m.ffprobePath,
		"-v", "quiet",
		"-print_format", "json",
		"-show_format",
		mediaPath,
	)
	output, err := cmd.Output()
	if err != nil {
		m.log.Debug("Failed to probe media duration: %v", err)
		return 0
	}

	return m.parseProbeDuration(string(output))
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
	if m.ffmpegPath == "" && m.ffprobePath == "" {
		return 0
	}

	probeJSON, err := ffmpeg.ProbeWithTimeout(mediaPath, 15*time.Second, nil)
	if err == nil {
		if h := m.parseProbeHeight(probeJSON); h > 0 {
			return h
		}
	}

	if m.ffprobePath == "" {
		return 0
	}

	probeCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	cmd := exec.CommandContext(probeCtx, m.ffprobePath,
		"-v", "quiet",
		"-print_format", "json",
		"-show_streams",
		"-select_streams", "v:0",
		mediaPath,
	)
	output, err := cmd.Output()
	if err != nil {
		m.log.Debug("ffprobe stream info failed for %s: %v", filepath.Base(mediaPath), err)
		return 0
	}

	return m.parseProbeHeight(string(output))
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
