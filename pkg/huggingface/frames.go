// Package huggingface provides frame extraction for visual classification.
package huggingface

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"media-server-pro/pkg/helpers"
)

// imageExtensions are file extensions treated as single-frame images (no extraction).
var imageExtensions = map[string]bool{
	".jpg": true, ".jpeg": true, ".png": true, ".webp": true, ".bmp": true, ".gif": true,
}

// ExtractFrames extracts up to count evenly-spaced frames from a video file using ffmpeg.
// For image files (e.g. .jpg, .png), returns a slice containing the original path only.
// tempDir is used to write frame images; caller is responsible for cleanup.
// Returns paths to extracted frame image files, or an error if ffmpeg/ffprobe fail.
func ExtractFrames(ctx context.Context, videoPath string, count int, tempDir string) ([]string, error) {
	if count <= 0 {
		count = 1
	}

	ext := strings.ToLower(filepath.Ext(videoPath))
	if imageExtensions[ext] {
		if _, err := os.Stat(videoPath); err != nil {
			return nil, err
		}
		return []string{videoPath}, nil
	}

	ffprobePath, err := helpers.FindBinary("ffprobe")
	if err != nil {
		return nil, fmt.Errorf("ffprobe not found: %w", err)
	}
	ffmpegPath, err := helpers.FindBinary("ffmpeg")
	if err != nil {
		return nil, fmt.Errorf("ffmpeg not found: %w", err)
	}

	duration, err := getDuration(ctx, ffprobePath, videoPath)
	if err != nil {
		return nil, fmt.Errorf("ffprobe duration: %w", err)
	}
	if duration <= 0 {
		return nil, fmt.Errorf("invalid duration %.2f for %s", duration, videoPath)
	}

	// Evenly-spaced timestamps (avoid 0 and duration to skip black frames)
	step := duration / float64(count+1)
	var timestamps []float64
	for i := 1; i <= count; i++ {
		timestamps = append(timestamps, step*float64(i))
	}

	if err := os.MkdirAll(tempDir, 0755); err != nil {
		return nil, err
	}

	baseName := filepath.Base(videoPath)
	baseName = strings.TrimSuffix(baseName, filepath.Ext(baseName))
	// Sanitize for use as filename prefix
	baseName = strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' {
			return r
		}
		return '_'
	}, baseName)
	if baseName == "" {
		baseName = "frame"
	}

	var paths []string
	for i, ts := range timestamps {
		outPath := filepath.Join(tempDir, fmt.Sprintf("%s_%d_%d.jpg", baseName, time.Now().UnixNano(), i))
		if err := extractOneFrame(ctx, ffmpegPath, videoPath, ts, outPath); err != nil {
			_ = os.Remove(outPath)
			return nil, fmt.Errorf("extract frame at %.2fs: %w", ts, err)
		}
		paths = append(paths, outPath)
	}
	return paths, nil
}

func getDuration(ctx context.Context, ffprobePath, input string) (float64, error) {
	// -v error -show_entries format=duration -of json
	cmd := exec.CommandContext(ctx, ffprobePath,
		"-v", "error",
		"-show_entries", "format=duration",
		"-of", "json",
		input,
	)
	out, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok && len(exitErr.Stderr) > 0 {
			return 0, fmt.Errorf("%s: %w", string(exitErr.Stderr), err)
		}
		return 0, err
	}
	var result struct {
		Format struct {
			Duration string `json:"duration"`
		} `json:"format"`
	}
	if err := json.Unmarshal(out, &result); err != nil {
		return 0, err
	}
	var d float64
	if _, err := fmt.Sscanf(result.Format.Duration, "%f", &d); err != nil {
		return 0, fmt.Errorf("parse duration %q: %w", result.Format.Duration, err)
	}
	return d, nil
}

func extractOneFrame(ctx context.Context, ffmpegPath, input string, timestamp float64, outputPath string) error {
	// -ss before -i for fast seek; -frames:v 1 to get one frame; -q:v 2 for good quality
	cmd := exec.CommandContext(ctx, ffmpegPath,
		"-y",
		"-ss", fmt.Sprintf("%.2f", timestamp),
		"-i", input,
		"-frames:v", "1",
		"-q:v", "2",
		outputPath,
	)
	_, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok && len(exitErr.Stderr) > 0 {
			return fmt.Errorf("%s: %w", string(exitErr.Stderr), err)
		}
		return err
	}
	return nil
}
