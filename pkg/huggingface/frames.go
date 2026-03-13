// Package huggingface provides frame extraction for visual classification.
package huggingface

import (
	"context"
	"encoding/json"
	"errors"
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

var videoExtensions = map[string]bool{
	".mp4": true, ".mkv": true, ".avi": true, ".mov": true, ".wmv": true, ".flv": true,
	".webm": true, ".m4v": true, ".mpeg": true, ".mpg": true,
	".3gp": true, ".ts": true, ".m2ts": true, ".vob": true, ".ogv": true,
}

const maxBaseNameLen = 200

// tryImageFrames returns (paths, true, nil) for valid image paths, (nil, true, err) on image stat error,
// and (nil, false, nil) when ext is not an image (caller should continue with video path).
func tryImageFrames(videoPath, ext string) ([]string, bool, error) {
	if !imageExtensions[ext] {
		return nil, false, nil
	}
	if _, err := os.Stat(videoPath); err != nil {
		return nil, true, err
	}
	return []string{videoPath}, true, nil
}

func checkVideoExtension(ext string) error {
	if !videoExtensions[ext] {
		return fmt.Errorf("unsupported file type for classification: %s (use video or image)", ext)
	}
	return nil
}

func findFFmpegBinaries() (ffprobePath, ffmpegPath string, err error) {
	ffprobePath, err = helpers.FindBinary("ffprobe")
	if err != nil {
		return "", "", fmt.Errorf("ffprobe not found: %w", err)
	}
	ffmpegPath, err = helpers.FindBinary("ffmpeg")
	if err != nil {
		return "", "", fmt.Errorf("ffmpeg not found: %w", err)
	}
	return ffprobePath, ffmpegPath, nil
}

func computeTimestamps(duration float64, count int) []float64 {
	step := duration / float64(count+1)
	timestamps := make([]float64, 0, count)
	for i := 1; i <= count; i++ {
		timestamps = append(timestamps, step*float64(i))
	}
	return timestamps
}

// sanitizeBaseNameRune keeps alphanumerics, hyphen, and underscore; replaces other runes with '_'.
func sanitizeBaseNameRune(r rune) rune {
	switch {
	case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		return r
	case r == '-', r == '_':
		return r
	default:
		return '_'
	}
}

func sanitizeBaseName(videoPath string) string {
	baseName := filepath.Base(videoPath)
	baseName = strings.TrimSuffix(baseName, filepath.Ext(baseName))
	baseName = strings.Map(sanitizeBaseNameRune, baseName)
	if baseName == "" {
		baseName = "frame"
	}
	if len(baseName) > maxBaseNameLen {
		baseName = baseName[:maxBaseNameLen]
	}
	return baseName
}

// extractFrameParams holds arguments for extracting a single frame (avoids excess function args).
type extractFrameParams struct {
	ffmpegPath string
	input      string
	timestamp  float64
	outputPath string
}

// extractFramesAtTimestampsParams holds arguments for batch frame extraction (avoids excess function args).
type extractFramesAtTimestampsParams struct {
	ffmpegPath string
	videoPath  string
	baseName   string
	timestamps []float64
	tempDir    string
}

func extractFramesAtTimestamps(ctx context.Context, p extractFramesAtTimestampsParams) ([]string, error) {
	paths := make([]string, 0, len(p.timestamps))
	for i, ts := range p.timestamps {
		outPath := filepath.Join(p.tempDir, fmt.Sprintf("%s_%d_%d.jpg", p.baseName, time.Now().UnixNano(), i))
		oneParams := extractFrameParams{ffmpegPath: p.ffmpegPath, input: p.videoPath, timestamp: ts, outputPath: outPath}
		if err := extractOneFrame(ctx, oneParams); err != nil {
			_ = os.Remove(outPath)
			// Clean up all previously extracted frames on failure
			for _, prev := range paths {
				_ = os.Remove(prev)
			}
			return nil, fmt.Errorf("extract frame at %.2fs: %w", ts, err)
		}
		paths = append(paths, outPath)
	}
	return paths, nil
}

// ExtractFramesOptions holds arguments for ExtractFrames to avoid string-heavy function signature.
type ExtractFramesOptions struct {
	VideoPath string
	Count     int
	TempDir   string
}

// ExtractFrames extracts up to count evenly-spaced frames from a video file using ffmpeg.
// For image files (e.g. .jpg, .png), returns a slice containing the original path only.
// tempDir is used to write frame images; caller is responsible for cleanup.
// Returns paths to extracted frame image files, or an error if ffmpeg/ffprobe fail.
func ExtractFrames(ctx context.Context, opts ExtractFramesOptions) ([]string, error) {
	count := opts.Count
	if count <= 0 {
		count = 1
	}
	videoPath := opts.VideoPath
	tempDir := opts.TempDir
	ext := strings.ToLower(filepath.Ext(videoPath))
	if paths, ok, err := tryImageFrames(videoPath, ext); ok {
		return paths, err
	}
	if err := checkVideoExtension(ext); err != nil {
		return nil, err
	}
	ffprobePath, ffmpegPath, err := findFFmpegBinaries()
	if err != nil {
		return nil, err
	}
	duration, err := getDuration(ctx, getDurationParams{ffprobePath: ffprobePath, input: videoPath})
	if err != nil {
		return nil, fmt.Errorf("ffprobe duration: %w", err)
	}
	if duration <= 0 {
		return nil, fmt.Errorf("invalid duration %.2f for %s", duration, videoPath)
	}
	timestamps := computeTimestamps(duration, count)
	if err := os.MkdirAll(tempDir, 0755); err != nil {
		return nil, err
	}
	baseName := sanitizeBaseName(videoPath)
	return extractFramesAtTimestamps(ctx, extractFramesAtTimestampsParams{
		ffmpegPath: ffmpegPath,
		videoPath:  videoPath,
		baseName:   baseName,
		timestamps: timestamps,
		tempDir:    tempDir,
	})
}

type getDurationParams struct {
	ffprobePath string
	input       string
}

func getDuration(ctx context.Context, p getDurationParams) (float64, error) {
	// -v error -show_entries format=duration -of json
	cmd := exec.CommandContext(ctx, p.ffprobePath,
		"-v", "error",
		"-show_entries", "format=duration",
		"-of", "json",
		p.input,
	)
	out, err := cmd.Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && len(exitErr.Stderr) > 0 {
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

func extractOneFrame(ctx context.Context, p extractFrameParams) error {
	// -ss before -i for fast seek; -frames:v 1 to get one frame; -q:v 2 for good quality
	args := []string{
		"-y",
		"-ss", fmt.Sprintf("%.2f", p.timestamp),
		"-i", p.input,
		"-frames:v", "1",
		"-q:v", "2",
		p.outputPath,
	}
	cmd := exec.CommandContext(ctx, p.ffmpegPath, args...)
	_, err := cmd.Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && len(exitErr.Stderr) > 0 {
			return fmt.Errorf("%s: %w", string(exitErr.Stderr), err)
		}
		return err
	}
	return nil
}
