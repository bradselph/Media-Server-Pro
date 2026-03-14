// Package validator provides media file validation and codec checking using FFprobe.
package validator

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"media-server-pro/internal/config"
	"media-server-pro/internal/database"
	"media-server-pro/internal/logger"
	"media-server-pro/internal/repositories"
	mysqlrepo "media-server-pro/internal/repositories/mysql"
	"media-server-pro/pkg/helpers"
	"media-server-pro/pkg/models"

	ffmpeg "github.com/u2takey/ffmpeg-go"
)

// Supported video codecs
var supportedVideoCodecs = map[string]bool{
	"h264": true, "hevc": true, "h265": true,
	"vp8": true, "vp9": true, "av1": true,
	"mpeg4": true, "mpeg2video": true, "mpeg1video": true,
	"wmv3": true, "vc1": true,
	"theora": true, "mjpeg": true,
}

// Supported audio codecs
var supportedAudioCodecs = map[string]bool{
	"aac": true, "mp3": true, "opus": true, "vorbis": true,
	"flac": true, "alac": true, "ac3": true, "eac3": true,
	"dts": true, "truehd": true,
	"pcm_s16le": true, "pcm_s24le": true, "pcm_s32le": true,
	"pcm_f32le": true, "pcm_f64le": true,
	"wmav2": true, "wmalossless": true,
}

// ValidationStatus represents the status of a validation
type ValidationStatus string

const (
	StatusPending     ValidationStatus = "pending"
	StatusValidated   ValidationStatus = "validated"
	StatusNeedsFix    ValidationStatus = "needs_fix"
	StatusFixed       ValidationStatus = "fixed"
	StatusFailed      ValidationStatus = "failed"
	StatusUnsupported ValidationStatus = "unsupported"
)

// ValidationResult holds the result of validating a media file
type ValidationResult struct {
	Path           string           `json:"-"`
	Status         ValidationStatus `json:"status"`
	ValidatedAt    time.Time        `json:"validated_at"`
	Duration       float64          `json:"duration"`
	VideoCodec     string           `json:"video_codec,omitempty"`
	AudioCodec     string           `json:"audio_codec,omitempty"`
	Width          int              `json:"width,omitempty"`
	Height         int              `json:"height,omitempty"`
	Bitrate        int64            `json:"bitrate,omitempty"`
	Container      string           `json:"container,omitempty"`
	Issues         []string         `json:"issues,omitempty"`
	FixedPath      string           `json:"-"`
	Error          string           `json:"error,omitempty"`
	VideoSupported bool             `json:"video_supported"`
	AudioSupported bool             `json:"audio_supported"`
}

// Module handles media validation
type Module struct {
	config      *config.Manager
	log         *logger.Logger
	dbModule    *database.Module
	repo        repositories.ValidationResultRepository
	results     map[string]*ValidationResult
	mu          sync.RWMutex
	fixing      map[string]bool // tracks paths currently being fixed
	fixingMu    sync.Mutex
	ffprobePath string
	ffmpegPath  string
	healthy     bool
	healthMsg   string
	healthMu    sync.RWMutex
}

// NewModule creates a new validator module
func NewModule(cfg *config.Manager, dbModule *database.Module) *Module {
	return &Module{
		config:   cfg,
		log:      logger.New("validator"),
		dbModule: dbModule,
		results:  make(map[string]*ValidationResult),
		fixing:   make(map[string]bool),
	}
}

// Name returns the module name
func (m *Module) Name() string {
	return "validator"
}

// Start initializes the validator module
func (m *Module) Start(_ context.Context) error {
	m.log.Info("Starting media validator module...")

	m.repo = mysqlrepo.NewValidationResultRepository(m.dbModule.GORM())

	// Check for ffprobe
	ffprobePath, err := helpers.FindBinary("ffprobe")
	if err != nil {
		m.log.Warn("ffprobe not found, validation disabled")
		m.healthMu.Lock()
		m.healthy = false
		m.healthMsg = "ffprobe not found"
		m.healthMu.Unlock()
		return nil // Don't fail - validation is optional
	}
	m.ffprobePath = ffprobePath

	// Check for ffmpeg (for fixing)
	ffmpegPath, err := helpers.FindBinary("ffmpeg")
	if err != nil {
		m.log.Warn("ffmpeg not found, auto-fix disabled")
	}
	m.ffmpegPath = ffmpegPath

	// Load existing results
	if err := m.loadResults(); err != nil {
		m.log.Warn("Failed to load validation results: %v", err)
	}

	m.healthMu.Lock()
	m.healthy = true
	m.healthMsg = "Running"
	m.healthMu.Unlock()
	m.log.Info("Media validator started")
	return nil
}

// Stop gracefully stops the module
func (m *Module) Stop(_ context.Context) error {
	m.log.Info("Stopping media validator module...")

	// Save results
	if err := m.saveResults(); err != nil {
		m.log.Error("Failed to save validation results: %v", err)
	}

	m.healthMu.Lock()
	m.healthy = false
	m.healthMsg = "Stopped"
	m.healthMu.Unlock()
	return nil
}

// Health returns the module health status
func (m *Module) Health() models.HealthStatus {
	m.healthMu.RLock()
	defer m.healthMu.RUnlock()
	return models.HealthStatus{
		Name:      m.Name(),
		Status:    helpers.StatusString(m.healthy),
		Message:   m.healthMsg,
		CheckedAt: time.Now(),
	}
}

// getCachedResult returns a recent validation result for path if one exists (within 7 days).
func (m *Module) getCachedResult(path string) (*ValidationResult, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result, ok := m.results[path]
	if !ok || time.Since(result.ValidatedAt) >= 7*24*time.Hour {
		return nil, false
	}
	return result, true
}

// setFinalStatus sets result.Status based on issues and codec support.
func setFinalStatus(result *ValidationResult) {
	if len(result.Issues) > 0 {
		result.Status = StatusNeedsFix
	} else if result.VideoSupported && result.AudioSupported {
		result.Status = StatusValidated
	} else {
		result.Status = StatusUnsupported
	}
}

// ValidateFile validates a single media file
func (m *Module) ValidateFile(path string) (*ValidationResult, error) {
	if m.ffprobePath == "" {
		return nil, fmt.Errorf("ffprobe not available")
	}
	if cached, ok := m.getCachedResult(path); ok {
		return cached, nil
	}

	result := &ValidationResult{
		Path:        path,
		Status:      StatusPending,
		ValidatedAt: time.Now(),
	}

	probeData, err := m.probeFile(path)
	if err != nil {
		result.Status = StatusFailed
		result.Error = err.Error()
		m.storeResult(result)
		return result, err
	}

	m.parseProbeData(result, probeData)
	m.checkCodecSupport(result)
	setFinalStatus(result)

	m.storeResult(result)
	m.log.Debug("Validated %s: status=%s, codec=%s/%s", path, result.Status, result.VideoCodec, result.AudioCodec)
	return result, nil
}

// probeFile runs ffprobe on a file using ffmpeg-go
// TODO: Unlike thumbnails/probe.go which uses m.ffprobePath with ffmpeg.ProbeWithTimeout,
// this function calls ffmpeg.Probe(path) without a timeout and without passing the
// explicit ffprobe path. This means: (1) it relies on ffprobe being in PATH, which may
// fail under systemd; and (2) it has no timeout, so a corrupted file could hang
// ffprobe indefinitely. Should use ffmpeg.ProbeWithTimeout with m.ffprobePath like the
// thumbnails module does.
func (m *Module) probeFile(path string) (*ProbeData, error) {
	// Try ffmpeg-go Probe first
	probeJSON, probeErr := ffmpeg.Probe(path)
	if probeErr == nil {
		var data ProbeData
		if parseErr := json.Unmarshal([]byte(probeJSON), &data); parseErr == nil {
			return &data, nil
		} else {
			m.log.Debug("ffmpeg-go probe parsing failed, trying raw ffprobe: %v", parseErr)
		}
	} else {
		m.log.Debug("ffmpeg-go probe failed, trying raw ffprobe: %v", probeErr)
	}

	// Fallback to raw ffprobe if ffmpeg-go fails
	if m.ffprobePath == "" {
		return nil, fmt.Errorf("ffprobe not available and ffmpeg-go probe failed: %w", probeErr)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	args := []string{
		"-v", "quiet",
		"-print_format", "json",
		"-show_format",
		"-show_streams",
		path,
	}

	cmd := exec.CommandContext(ctx, m.ffprobePath, args...)
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("ffprobe failed: %w", err)
	}

	var data ProbeData
	if err := json.Unmarshal(output, &data); err != nil {
		return nil, fmt.Errorf("failed to parse ffprobe output: %w", err)
	}

	return &data, nil
}

// ProbeData holds ffprobe output
type ProbeData struct {
	Format struct {
		Filename   string `json:"filename"`
		FormatName string `json:"format_name"`
		Duration   string `json:"duration"`
		BitRate    string `json:"bit_rate"`
		Size       string `json:"size"`
	} `json:"format"`
	Streams []struct {
		Index     int    `json:"index"`
		CodecType string `json:"codec_type"`
		CodecName string `json:"codec_name"`
		Width     int    `json:"width,omitempty"`
		Height    int    `json:"height,omitempty"`
		BitRate   string `json:"bit_rate,omitempty"`
	} `json:"streams"`
}

// parseProbeData extracts relevant info from probe data
func (m *Module) parseProbeData(result *ValidationResult, data *ProbeData) {
	result.Container = data.Format.FormatName
	m.parseFormatFields(result, data)
	parseProbeStreams(result, data)
}

// parseFormatNumber parses s with parseFn; returns (zero, false) on empty or parse error and logs the error.
func parseFormatNumber[T any](s string, log *logger.Logger, fieldName string, parseFn func(string) (T, error)) (T, bool) {
	var zero T
	if s == "" {
		return zero, false
	}
	v, err := parseFn(s)
	if err != nil {
		log.Debug("Failed to parse %s %q: %v", fieldName, s, err)
		return zero, false
	}
	return v, true
}

// parseFormatFloat parses a string to float64; logs and returns false on failure or empty.
func parseFormatFloat(s string, log *logger.Logger, fieldName string) (float64, bool) {
	return parseFormatNumber(s, log, fieldName, func(s string) (float64, error) { return strconv.ParseFloat(s, 64) })
}

// parseFormatInt parses a string to int64; logs and returns false on failure or empty.
func parseFormatInt(s string, log *logger.Logger, fieldName string) (int64, bool) {
	return parseFormatNumber(s, log, fieldName, func(s string) (int64, error) { return strconv.ParseInt(s, 10, 64) })
}

// parseFormatFields parses duration and bitrate from the probe format section.
func (m *Module) parseFormatFields(result *ValidationResult, data *ProbeData) {
	if v, ok := parseFormatFloat(data.Format.Duration, m.log, "duration"); ok {
		result.Duration = v
	}
	if v, ok := parseFormatInt(data.Format.BitRate, m.log, "bitrate"); ok {
		result.Bitrate = v
	}
}

// parseProbeStreams extracts video and audio codec information from probe streams.
func parseProbeStreams(result *ValidationResult, data *ProbeData) {
	for _, stream := range data.Streams {
		switch stream.CodecType {
		case "video":
			if result.VideoCodec == "" {
				result.VideoCodec = stream.CodecName
				result.Width = stream.Width
				result.Height = stream.Height
			}
		case "audio":
			if result.AudioCodec == "" {
				result.AudioCodec = stream.CodecName
			}
		}
	}
}

// checkCodecSupport checks if codecs are supported
func (m *Module) checkCodecSupport(result *ValidationResult) {
	m.checkVideoCodecSupport(result)
	m.checkAudioCodecSupport(result)
	m.appendValidationIssues(result)
}

func (m *Module) checkVideoCodecSupport(result *ValidationResult) {
	if result.VideoCodec == "" {
		result.VideoSupported = true // No video is fine for audio files
		return
	}
	result.VideoSupported = supportedVideoCodecs[strings.ToLower(result.VideoCodec)]
	if !result.VideoSupported {
		result.Issues = append(result.Issues, fmt.Sprintf("Unsupported video codec: %s", result.VideoCodec))
	}
}

func (m *Module) checkAudioCodecSupport(result *ValidationResult) {
	if result.AudioCodec == "" {
		result.AudioSupported = true // No audio is fine for some video files
		return
	}
	result.AudioSupported = supportedAudioCodecs[strings.ToLower(result.AudioCodec)]
	if !result.AudioSupported {
		result.Issues = append(result.Issues, fmt.Sprintf("Unsupported audio codec: %s", result.AudioCodec))
	}
}

func (m *Module) appendValidationIssues(result *ValidationResult) {
	if result.Duration <= 0 {
		result.Issues = append(result.Issues, "Could not determine duration")
	}
	if result.VideoCodec != "" && result.Width <= 0 {
		result.Issues = append(result.Issues, "Could not determine video dimensions")
	}
}

// storeResult saves a validation result and persists to the database immediately.
func (m *Module) storeResult(result *ValidationResult) {
	m.mu.Lock()
	m.results[result.Path] = result
	m.mu.Unlock()

	// Persist immediately to prevent data loss on crash
	rec := m.resultToRecord(result)
	if err := m.repo.Upsert(context.Background(), rec); err != nil {
		m.log.Error("Failed to save validation result: %v", err)
	}
}

// FixFile attempts to transcode a file to a supported format
func (m *Module) FixFile(path string) (*ValidationResult, error) {
	if m.ffmpegPath == "" {
		return nil, fmt.Errorf("ffmpeg not available")
	}

	// Prevent duplicate fix operations for the same path
	m.fixingMu.Lock()
	if m.fixing[path] {
		m.fixingMu.Unlock()
		return nil, fmt.Errorf("fix already in progress for: %s", path)
	}
	m.fixing[path] = true
	m.fixingMu.Unlock()
	defer func() {
		m.fixingMu.Lock()
		delete(m.fixing, path)
		m.fixingMu.Unlock()
	}()

	m.mu.RLock()
	result, exists := m.results[path]
	m.mu.RUnlock()

	if !exists {
		// Validate first
		var err error
		result, err = m.ValidateFile(path)
		if err != nil {
			return nil, err
		}
	}

	if result.Status != StatusNeedsFix {
		return result, nil // No fix needed
	}

	// Generate output path
	dir := filepath.Dir(path)
	base := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	outputPath := filepath.Join(dir, base+"_fixed.mp4")

	m.log.Info("Transcoding %s to %s", path, outputPath)

	// Build ffmpeg pipeline using ffmpeg-go
	stream := ffmpeg.Input(path).
		Output(outputPath, ffmpeg.KwArgs{
			"c:v":      "libx264",
			"preset":   "fast",
			"crf":      "23",
			"c:a":      "aac",
			"b:a":      "128k",
			"movflags": "+faststart",
		}).OverWriteOutput().SetFfmpegPath(m.ffmpegPath)

	// Compile to command
	cmd := stream.Compile()

	// Apply context for timeout
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	cmdWithContext := exec.CommandContext(ctx, cmd.Path, cmd.Args[1:]...)
	cmdWithContext.Env = cmd.Env
	cmdWithContext.Dir = cmd.Dir

	output, err := cmdWithContext.CombinedOutput()
	if err != nil {
		m.log.Error("FFmpeg fix failed: %s", string(output))
		return nil, fmt.Errorf("transcoding failed: %w", err)
	}

	// Re-fetch under write lock (result from earlier RLock may be stale)
	m.mu.Lock()
	if r := m.results[path]; r != nil {
		r.Status = StatusFixed
		r.FixedPath = outputPath
		result = r
	}
	m.mu.Unlock()

	m.log.Info("Fixed media file: %s -> %s", path, outputPath)
	return result, nil
}

// shouldValidateFile checks whether a file at the given path is a media file
// that has not been recently validated. Used for on-demand validation via admin API;
// can be used by a future scheduled task to validate library files in bulk.
func (m *Module) shouldValidateFile(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	if !helpers.IsMediaExtension(ext) {
		return false
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	if existing, ok := m.results[path]; ok {
		if time.Since(existing.ValidatedAt) < 7*24*time.Hour {
			return false
		}
	}
	return true
}

// GetResult returns the validation result for a file
func (m *Module) GetResult(path string) (*ValidationResult, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result, ok := m.results[path]
	return result, ok
}

// GetStats returns validation statistics
func (m *Module) GetStats() Stats {
	m.mu.RLock()
	defer m.mu.RUnlock()

	stats := Stats{}
	for _, result := range m.results {
		stats.Total++
		switch result.Status {
		case StatusValidated:
			stats.Validated++
		case StatusNeedsFix:
			stats.NeedsFix++
		case StatusFixed:
			stats.Fixed++
		case StatusFailed:
			stats.Failed++
		case StatusUnsupported:
			stats.Unsupported++
		}
	}
	return stats
}

// Stats holds validation statistics.
type Stats struct {
	Total       int `json:"total"`
	Validated   int `json:"validated"`
	NeedsFix    int `json:"needs_fix"`
	Fixed       int `json:"fixed"`
	Failed      int `json:"failed"`
	Unsupported int `json:"unsupported"`
}

// ClearResult removes a validation result
func (m *Module) ClearResult(path string) {
	m.mu.Lock()
	delete(m.results, path)
	m.mu.Unlock()

	if m.repo != nil {
		if err := m.repo.Delete(context.Background(), path); err != nil {
			m.log.Warn("ClearResult: failed to delete from DB for %s: %v", path, err)
		}
	}
}

// Persistence — reads/writes via MySQL repository

func (m *Module) loadResults() error {
	records, err := m.repo.List(context.Background())
	if err != nil {
		return err
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	for _, rec := range records {
		result := &ValidationResult{
			Path:           rec.Path,
			Status:         ValidationStatus(rec.Status),
			ValidatedAt:    rec.ValidatedAt,
			Duration:       rec.Duration,
			VideoCodec:     rec.VideoCodec,
			AudioCodec:     rec.AudioCodec,
			Width:          rec.Width,
			Height:         rec.Height,
			Bitrate:        rec.Bitrate,
			Container:      rec.Container,
			Issues:         rec.Issues,
			Error:          rec.Error,
			VideoSupported: rec.VideoSupported,
			AudioSupported: rec.AudioSupported,
		}
		m.results[rec.Path] = result
	}
	return nil
}

func (m *Module) saveResults() error {
	m.mu.RLock()
	defer m.mu.RUnlock()

	ctx := context.Background()
	for _, result := range m.results {
		if err := m.repo.Upsert(ctx, m.resultToRecord(result)); err != nil {
			return err
		}
	}
	return nil
}

func (m *Module) resultToRecord(result *ValidationResult) *repositories.ValidationResultRecord {
	return &repositories.ValidationResultRecord{
		Path:           result.Path,
		Status:         string(result.Status),
		ValidatedAt:    result.ValidatedAt,
		Duration:       result.Duration,
		VideoCodec:     result.VideoCodec,
		AudioCodec:     result.AudioCodec,
		Width:          result.Width,
		Height:         result.Height,
		Bitrate:        result.Bitrate,
		Container:      result.Container,
		Issues:         result.Issues,
		Error:          result.Error,
		VideoSupported: result.VideoSupported,
		AudioSupported: result.AudioSupported,
	}
}
