package thumbnails

import (
	"context"
	"fmt"
	"image/jpeg"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/buckket/go-blurhash"
	ffmpeg "github.com/u2takey/ffmpeg-go"
)

// jpegQuality converts the config quality (1-100, higher=better) to ffmpeg's
// -q:v scale (2-31, lower=better). Returns "2" at quality=100, "31" at quality=1.
func jpegQuality(configQuality int) string {
	if configQuality <= 0 || configQuality > 100 {
		configQuality = 80
	}
	q := 2 + (100-configQuality)*29/99
	return fmt.Sprintf("%d", q)
}

// webpQuality returns the config quality directly (1-100) for WebP's -q:v flag,
// which maps linearly to compression quality.
func webpQuality(configQuality int) string {
	if configQuality <= 0 || configQuality > 100 {
		configQuality = 80
	}
	return fmt.Sprintf("%d", configQuality)
}

// generateThumbnail performs the actual thumbnail generation
func (m *Module) generateThumbnail(job *ThumbnailJob) error {
	// Ensure output directory exists
	if err := os.MkdirAll(filepath.Dir(job.OutputPath), 0o755); err != nil { //nolint:gosec // G301: thumbnail dirs need world-read for serving
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	// Resolve S3 keys to presigned URLs once so all ffmpeg calls in this job use
	// the same URL. MediaPath is kept as the DB identifier for BlurHash updates etc.
	if job.FFmpegInput == "" {
		job.FFmpegInput = m.resolveMediaInputPath(job.MediaPath)
	}

	if job.IsAudio {
		return m.generateAudioThumbnail(job)
	}
	return m.generateVideoThumbnail(job)
}

// resolveVideoTimestamp returns the timestamp to use for frame extraction (from job or derived from duration).
func (m *Module) resolveVideoTimestamp(job *ThumbnailJob) float64 {
	timestamp := job.Timestamp
	if timestamp > 0 {
		return timestamp
	}
	duration := 60.0
	if d, err := m.getMediaDuration(job.MediaPath); err == nil {
		duration = d
	}
	timestamp = duration * 0.1
	if timestamp < 1 {
		timestamp = 1
	}
	if timestamp > duration-1 {
		timestamp = duration / 2
	}
	return timestamp
}

// isPreviewThumbnail reports whether the thumbnail path is for a preview (skip responsive/BlurHash).
func isPreviewThumbnail(outputPath string) bool {
	return strings.Contains(filepath.Base(outputPath), "_preview_")
}

// addFileSizeToStats adds the file size at path to module stats if the file exists.
func (m *Module) addFileSizeToStats(path string) {
	info, err := os.Stat(path)
	if err != nil {
		return
	}
	m.statsMu.Lock()
	m.stats.TotalSize += info.Size()
	m.statsMu.Unlock()
	m.log.Debug("Thumbnail size: %d bytes", info.Size())
}

// tryGenerateWebPVariant generates a WebP variant for the main JPEG and updates stats on success.
func (m *Module) tryGenerateWebPVariant(job *ThumbnailJob, timestamp float64) {
	webpPath := m.getThumbnailPathWebp(job.OutputPath)
	if err := m.generateWebPFromVideo(&webPFromVideoOpts{job.FFmpegInput, webpPath, job.Width, job.Height, timestamp}); err != nil {
		m.log.Warn("WebP thumbnail generation failed (JPEG served): %v", err)
		return
	}
	m.addFileSizeToStats(webpPath)
}

// generateResponsiveThumbnailsIfMain generates 160/320/640 WebP variants for srcset; no-op for _preview_ thumbnails.
func (m *Module) generateResponsiveThumbnailsIfMain(job *ThumbnailJob, timestamp float64) {
	if isPreviewThumbnail(job.OutputPath) {
		return
	}
	mediaID := strings.TrimSuffix(filepath.Base(job.OutputPath), ".jpg")
	for _, v := range responsiveVariants {
		h := v.Width * 9 / 16
		outPath := filepath.Join(m.thumbnailDir, mediaID+v.Suffix+".webp")
		if err := m.generateWebPFromVideo(&webPFromVideoOpts{job.FFmpegInput, outPath, v.Width, h, timestamp}); err != nil {
			m.log.Debug("Responsive thumbnail %dw failed: %v", v.Width, err)
		}
	}
}

// tryUpdateBlurHashForThumbnail computes and stores BlurHash for the main thumbnail (LQIP); no-op if no updater or _preview_.
func (m *Module) tryUpdateBlurHashForThumbnail(job *ThumbnailJob) {
	if m.blurHashUpdater == nil || isPreviewThumbnail(job.OutputPath) {
		return
	}
	m.updateBlurHashFromThumbnail(job.OutputPath, job.MediaPath)
}

// generateVideoThumbnail extracts a frame from video using ffmpeg-go
func (m *Module) generateVideoThumbnail(job *ThumbnailJob) error {
	m.log.Info("Extracting video frame from: %s", job.MediaPath)
	timestamp := m.resolveVideoTimestamp(job)
	m.log.Debug("Using timestamp: %.2f seconds", timestamp)

	// format=yuv420p ensures 8-bit output before JPEG encoding;
	// without it, 10-bit HDR/HEVC/AV1 sources fail with "codec not supported" errors.
	cfg := m.config.Get()
	scaleFilter := fmt.Sprintf("scale=%d:%d:force_original_aspect_ratio=decrease,pad=%d:%d:(ow-iw)/2:(oh-ih)/2,format=yuv420p",
		job.Width, job.Height, job.Width, job.Height)
	stream := ffmpeg.Input(job.FFmpegInput, ffmpeg.KwArgs{"ss": fmt.Sprintf("%.2f", timestamp)}).
		Output(job.OutputPath, ffmpeg.KwArgs{
			"vframes": "1",
			"vf":      scaleFilter,
			"q:v":     jpegQuality(cfg.Thumbnails.Quality),
		}).OverWriteOutput().SetFfmpegPath(m.ffmpegPath)
	cmd := stream.Compile()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmdWithContext := exec.CommandContext(ctx, cmd.Path, cmd.Args[1:]...) //nolint:gosec // G204: cmd.Path is from ffmpeg library, not user input
	cmdWithContext.Env = cmd.Env
	cmdWithContext.Dir = cmd.Dir

	output, err := cmdWithContext.CombinedOutput()
	if err != nil {
		// Clean up any partial output file left behind on failure (e.g. timeout)
		_ = os.Remove(job.OutputPath)
		m.log.Error("FFmpeg failed: %v", err)
		m.log.Error("FFmpeg output: %s", string(output))
		return fmt.Errorf("ffmpeg failed: %w", err)
	}
	if _, err := os.Stat(job.OutputPath); os.IsNotExist(err) {
		return fmt.Errorf("thumbnail file not created")
	}

	m.addFileSizeToStats(job.OutputPath)
	m.tryGenerateWebPVariant(job, timestamp)
	m.generateResponsiveThumbnailsIfMain(job, timestamp)
	m.tryUpdateBlurHashForThumbnail(job)
	return nil
}

// computeBlurHash reads a JPEG and returns its BlurHash string (4x3 components)
func (m *Module) computeBlurHash(jpgPath string) (string, error) {
	f, err := os.Open(jpgPath)
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()

	img, err := jpeg.Decode(f)
	if err != nil {
		return "", err
	}
	return blurhash.Encode(4, 3, img)
}

// updateBlurHashFromThumbnail computes BlurHash from the JPEG at outputPath and stores it for mediaPath.
// Caller must ensure m.blurHashUpdater != nil and path is not a preview thumbnail.
func (m *Module) updateBlurHashFromThumbnail(outputPath, mediaPath string) {
	hash, err := m.computeBlurHash(outputPath)
	if err != nil || hash == "" {
		return
	}
	bgCtx, bgCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer bgCancel()
	if err := m.blurHashUpdater.UpdateBlurHash(bgCtx, mediaPath, hash); err != nil {
		m.log.Warn("Failed to store BlurHash: %v", err)
	}
}

// webPFromVideoOpts holds parameters for generating a WebP frame from video.
type webPFromVideoOpts struct {
	mediaPath  string
	outputPath string
	width      int
	height     int
	timestamp  float64
}

// generateWebPFromVideo extracts a frame and encodes as WebP
func (m *Module) generateWebPFromVideo(opts *webPFromVideoOpts) error {
	cfg := m.config.Get()
	scaleFilter := fmt.Sprintf("scale=%d:%d:force_original_aspect_ratio=decrease,pad=%d:%d:(ow-iw)/2:(oh-ih)/2,format=yuv420p",
		opts.width, opts.height, opts.width, opts.height)

	stream := ffmpeg.Input(opts.mediaPath, ffmpeg.KwArgs{"ss": fmt.Sprintf("%.2f", opts.timestamp)}).
		Output(opts.outputPath, ffmpeg.KwArgs{
			"vframes": "1",
			"vf":      scaleFilter,
			"c:v":     "libwebp",
			"q:v":     webpQuality(cfg.Thumbnails.Quality),
		}).OverWriteOutput().SetFfmpegPath(m.ffmpegPath)

	cmd := stream.Compile()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cmdWithContext := exec.CommandContext(ctx, cmd.Path, cmd.Args[1:]...) //nolint:gosec // G204: cmd.Path is from ffmpeg library, not user input
	cmdWithContext.Env = cmd.Env
	cmdWithContext.Dir = cmd.Dir

	output, err := cmdWithContext.CombinedOutput()
	if err != nil {
		// Clean up any partial output file left behind on failure
		_ = os.Remove(opts.outputPath)
		m.log.Debug("FFmpeg WebP failed: %v, output: %s", err, string(output))
		return fmt.Errorf("ffmpeg webp: %w", err)
	}
	return nil
}

// generateAudioThumbnail creates waveform for audio using ffmpeg-go
func (m *Module) generateAudioThumbnail(job *ThumbnailJob) error {
	m.log.Info("Generating audio waveform for: %s", job.MediaPath)

	// Build ffmpeg pipeline using ffmpeg-go
	waveformFilter := fmt.Sprintf("showwavespic=s=%dx%d:colors=#0080ff", job.Width, job.Height)

	stream := ffmpeg.Input(job.FFmpegInput).
		Output(job.OutputPath, ffmpeg.KwArgs{
			"filter_complex": waveformFilter,
			"frames:v":       "1",
		}).OverWriteOutput().SetFfmpegPath(m.ffmpegPath)

	// Compile to command
	cmd := stream.Compile()

	// Apply context for timeout
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cmdWithContext := exec.CommandContext(ctx, cmd.Path, cmd.Args[1:]...) //nolint:gosec // G204: cmd.Path is from ffmpeg library, not user input
	cmdWithContext.Env = cmd.Env
	cmdWithContext.Dir = cmd.Dir

	output, err := cmdWithContext.CombinedOutput()
	if err != nil {
		m.log.Error("FFmpeg waveform failed: %v", err)
		m.log.Error("FFmpeg output: %s", string(output))
		return fmt.Errorf("ffmpeg waveform failed: %w", err)
	}

	m.addFileSizeToStats(job.OutputPath)
	return m.verifyAndPostProcessAudioThumbnail(job)
}

// verifyAndPostProcessAudioThumbnail verifies the waveform file exists, generates WebP variant, and updates BlurHash.
func (m *Module) verifyAndPostProcessAudioThumbnail(job *ThumbnailJob) error {
	if _, err := os.Stat(job.OutputPath); os.IsNotExist(err) {
		return fmt.Errorf("waveform file not created")
	}
	webpPath := m.getThumbnailPathWebp(job.OutputPath)
	if err := m.generateWebPFromAudio(&webPFromAudioOpts{MediaPath: job.FFmpegInput, OutputPath: webpPath, Width: job.Width, Height: job.Height}); err != nil {
		m.log.Warn("WebP waveform generation failed (JPEG served): %v", err)
	}
	m.updateBlurHashForAudioThumbnail(job)
	return nil
}

// updateBlurHashForAudioThumbnail computes and stores BlurHash for the main audio waveform thumbnail.
func (m *Module) updateBlurHashForAudioThumbnail(job *ThumbnailJob) {
	m.tryUpdateBlurHashForThumbnail(job)
}

// generateWebPFromAudio creates waveform as WebP
func (m *Module) generateWebPFromAudio(opts *webPFromAudioOpts) error {
	cfg := m.config.Get()
	waveformFilter := fmt.Sprintf("showwavespic=s=%dx%d:colors=#0080ff", opts.Width, opts.Height)

	stream := ffmpeg.Input(opts.MediaPath).
		Output(opts.OutputPath, ffmpeg.KwArgs{
			"filter_complex": waveformFilter,
			"frames:v":       "1",
			"c:v":            "libwebp",
			"q:v":            webpQuality(cfg.Thumbnails.Quality),
		}).OverWriteOutput().SetFfmpegPath(m.ffmpegPath)

	cmd := stream.Compile()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cmdWithContext := exec.CommandContext(ctx, cmd.Path, cmd.Args[1:]...) //nolint:gosec // G204: cmd.Path is from ffmpeg library, not user input
	cmdWithContext.Env = cmd.Env
	cmdWithContext.Dir = cmd.Dir

	_, err := cmdWithContext.CombinedOutput()
	return err
}
