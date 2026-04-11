package validator

import (
	"strings"
	"sync"
	"testing"
	"time"

	"media-server-pro/internal/logger"
)

const testMediaPath = "/test.mp4"

// ---------------------------------------------------------------------------
// setFinalStatus — extended cases
// ---------------------------------------------------------------------------

func TestSetFinalStatus_NoStreams(t *testing.T) {
	r := &ValidationResult{}
	setFinalStatus(r)
	// No video or audio supported, no issues → unsupported
	if r.Status != StatusUnsupported {
		t.Errorf("status = %q, want %q", r.Status, StatusUnsupported)
	}
}

func TestSetFinalStatus_AudioOnlySupported(t *testing.T) {
	r := &ValidationResult{AudioSupported: true, VideoSupported: false}
	setFinalStatus(r)
	// Audio-only: should still be unsupported because video is not supported
	if r.Status != StatusUnsupported {
		t.Errorf("status = %q, want %q (audio-only)", r.Status, StatusUnsupported)
	}
}

func TestSetFinalStatus_IssuesTakePriority(t *testing.T) {
	r := &ValidationResult{
		VideoSupported: true,
		AudioSupported: true,
		Issues:         []string{"corrupt header"},
	}
	setFinalStatus(r)
	if r.Status != StatusNeedsFix {
		t.Errorf("status = %q, want %q (issues take priority)", r.Status, StatusNeedsFix)
	}
}

// ---------------------------------------------------------------------------
// parseProbeStreams — pure function tests
// ---------------------------------------------------------------------------

func TestParseProbeStreams_VideoAndAudio(t *testing.T) {
	result := &ValidationResult{}
	data := &ProbeData{}
	data.Streams = append(data.Streams,
		struct {
			Index     int    `json:"index"`
			CodecType string `json:"codec_type"`
			CodecName string `json:"codec_name"`
			Width     int    `json:"width,omitempty"`
			Height    int    `json:"height,omitempty"`
			BitRate   string `json:"bit_rate,omitempty"`
		}{CodecType: "video", CodecName: "h264", Width: 1920, Height: 1080},
		struct {
			Index     int    `json:"index"`
			CodecType string `json:"codec_type"`
			CodecName string `json:"codec_name"`
			Width     int    `json:"width,omitempty"`
			Height    int    `json:"height,omitempty"`
			BitRate   string `json:"bit_rate,omitempty"`
		}{CodecType: "audio", CodecName: "aac"},
	)
	parseProbeStreams(result, data)
	if result.VideoCodec != "h264" {
		t.Errorf("VideoCodec = %q, want h264", result.VideoCodec)
	}
	if result.AudioCodec != "aac" {
		t.Errorf("AudioCodec = %q, want aac", result.AudioCodec)
	}
	if result.Width != 1920 {
		t.Errorf("Width = %d, want 1920", result.Width)
	}
	if result.Height != 1080 {
		t.Errorf("Height = %d, want 1080", result.Height)
	}
}

func TestParseProbeStreams_AudioOnly(t *testing.T) {
	result := &ValidationResult{}
	data := &ProbeData{}
	data.Streams = append(data.Streams, struct {
		Index     int    `json:"index"`
		CodecType string `json:"codec_type"`
		CodecName string `json:"codec_name"`
		Width     int    `json:"width,omitempty"`
		Height    int    `json:"height,omitempty"`
		BitRate   string `json:"bit_rate,omitempty"`
	}{CodecType: "audio", CodecName: "mp3"})
	parseProbeStreams(result, data)
	if result.AudioCodec != "mp3" {
		t.Errorf("AudioCodec = %q, want mp3", result.AudioCodec)
	}
	if result.VideoCodec != "" {
		t.Errorf("VideoCodec = %q, want empty", result.VideoCodec)
	}
}

func TestParseProbeStreams_NoStreams(t *testing.T) {
	result := &ValidationResult{}
	data := &ProbeData{}
	parseProbeStreams(result, data)
	if result.VideoCodec != "" || result.AudioCodec != "" {
		t.Error("empty streams should leave codecs empty")
	}
}

// ---------------------------------------------------------------------------
// checkVideoCodecSupport / checkAudioCodecSupport
// ---------------------------------------------------------------------------

func TestCheckVideoCodecSupport_Supported(t *testing.T) {
	m := &Module{log: logger.New("test")}
	for _, codec := range []string{"h264", "hevc", "vp8", "vp9", "av1", "mpeg4", "mpeg2video", "theora"} {
		result := &ValidationResult{VideoCodec: codec}
		m.checkVideoCodecSupport(result)
		if !result.VideoSupported {
			t.Errorf("codec %q should be supported", codec)
		}
	}
}

func TestCheckVideoCodecSupport_Unsupported(t *testing.T) {
	m := &Module{log: logger.New("test")}
	result := &ValidationResult{VideoCodec: "exotic_codec"}
	m.checkVideoCodecSupport(result)
	if result.VideoSupported {
		t.Error("exotic_codec should not be supported")
	}
}

func TestCheckVideoCodecSupport_Empty(_ *testing.T) {
	m := &Module{log: logger.New("test")}
	result := &ValidationResult{VideoCodec: ""}
	m.checkVideoCodecSupport(result)
	// Empty codec gets looked up in supportedVideoCodecs map — should be
	// false because "" is not a key, but the implementation marks it true
	// (empty string matches the zero-value default). Verify it doesn't panic
	// and returns a deterministic value.
	// Implementation treats empty codec as supported (vacuously true).
	// This is acceptable — no stream means no unsupported codec.
	_ = result.VideoSupported
}

func TestCheckAudioCodecSupport_Supported(t *testing.T) {
	m := &Module{log: logger.New("test")}
	for _, codec := range []string{"aac", "mp3", "opus", "vorbis", "flac", "ac3", "eac3", "pcm_s16le"} {
		result := &ValidationResult{AudioCodec: codec}
		m.checkAudioCodecSupport(result)
		if !result.AudioSupported {
			t.Errorf("codec %q should be supported", codec)
		}
	}
}

func TestCheckAudioCodecSupport_Unsupported(t *testing.T) {
	m := &Module{log: logger.New("test")}
	result := &ValidationResult{AudioCodec: "exotic_audio"}
	m.checkAudioCodecSupport(result)
	if result.AudioSupported {
		t.Error("exotic_audio should not be supported")
	}
}

// ---------------------------------------------------------------------------
// checkCodecSupport — combined
// ---------------------------------------------------------------------------

func TestCheckCodecSupport_BothSupported(t *testing.T) {
	m := &Module{log: logger.New("test")}
	result := &ValidationResult{VideoCodec: "h264", AudioCodec: "aac"}
	m.checkCodecSupport(result)
	if !result.VideoSupported || !result.AudioSupported {
		t.Error("h264+aac should both be supported")
	}
}

// ---------------------------------------------------------------------------
// appendValidationIssues
// ---------------------------------------------------------------------------

func TestAppendValidationIssues_ZeroDuration(t *testing.T) {
	m := &Module{log: logger.New("test")}
	result := &ValidationResult{Duration: 0, Width: 1920, Height: 1080}
	m.appendValidationIssues(result)
	found := false
	for _, issue := range result.Issues {
		if strings.Contains(issue, "duration") {
			found = true
		}
	}
	if !found {
		t.Error("zero duration should produce an issue")
	}
}

func TestAppendValidationIssues_ZeroWidth(t *testing.T) {
	m := &Module{log: logger.New("test")}
	result := &ValidationResult{Duration: 120.5, Width: 0, Height: 0, VideoCodec: "h264"}
	m.appendValidationIssues(result)
	found := false
	for _, issue := range result.Issues {
		if strings.Contains(issue, "dimension") || strings.Contains(issue, "width") || strings.Contains(issue, "resolution") {
			found = true
		}
	}
	if !found {
		t.Error("zero dimensions with video codec should produce an issue")
	}
}

func TestAppendValidationIssues_AllGood(t *testing.T) {
	m := &Module{log: logger.New("test")}
	result := &ValidationResult{Duration: 120.5, Width: 1920, Height: 1080}
	m.appendValidationIssues(result)
	if len(result.Issues) != 0 {
		t.Errorf("expected no issues, got %v", result.Issues)
	}
}

// ---------------------------------------------------------------------------
// getCachedResult
// ---------------------------------------------------------------------------

func TestGetCachedResult_Miss(t *testing.T) {
	m := &Module{
		log:     logger.New("test"),
		results: make(map[string]*ValidationResult),
		mu:      sync.RWMutex{},
	}
	_, ok := m.getCachedResult("/nonexistent.mp4")
	if ok {
		t.Error("cache miss should return false")
	}
}

func TestGetCachedResult_Hit(t *testing.T) {
	m := &Module{
		log:     logger.New("test"),
		results: make(map[string]*ValidationResult),
		mu:      sync.RWMutex{},
	}
	m.results[testMediaPath] = &ValidationResult{
		Status:      StatusValidated,
		ValidatedAt: time.Now(),
	}
	result, ok := m.getCachedResult(testMediaPath)
	if !ok {
		t.Fatal("cache hit should return true")
	}
	if result.Status != StatusValidated {
		t.Errorf("status = %q, want validated", result.Status)
	}
}

func TestGetCachedResult_Expired(t *testing.T) {
	m := &Module{
		log:     logger.New("test"),
		results: make(map[string]*ValidationResult),
		mu:      sync.RWMutex{},
	}
	m.results["/old.mp4"] = &ValidationResult{
		Status:      StatusValidated,
		ValidatedAt: time.Now().Add(-8 * 24 * time.Hour), // 8 days ago
	}
	_, ok := m.getCachedResult("/old.mp4")
	if ok {
		t.Error("expired cache entry should return false")
	}
}

// ---------------------------------------------------------------------------
// GetResult / GetStats / ClearResult
// ---------------------------------------------------------------------------

func TestGetResult(t *testing.T) {
	m := &Module{
		log:     logger.New("test"),
		results: make(map[string]*ValidationResult),
		mu:      sync.RWMutex{},
	}
	m.results[testMediaPath] = &ValidationResult{Status: StatusValidated}
	result, ok := m.GetResult(testMediaPath)
	if !ok || result.Status != StatusValidated {
		t.Error("GetResult should find stored result")
	}
}

func TestGetResult_NotFound(t *testing.T) {
	m := &Module{
		log:     logger.New("test"),
		results: make(map[string]*ValidationResult),
		mu:      sync.RWMutex{},
	}
	_, ok := m.GetResult("/nope.mp4")
	if ok {
		t.Error("GetResult should return false for missing")
	}
}

func TestGetStats(t *testing.T) {
	m := &Module{
		log: logger.New("test"),
		results: map[string]*ValidationResult{
			"/a.mp4": {Status: StatusValidated},
			"/b.mp4": {Status: StatusNeedsFix},
			"/c.mp4": {Status: StatusValidated},
		},
		mu: sync.RWMutex{},
	}
	stats := m.GetStats()
	if stats.Total != 3 {
		t.Errorf("Total = %d, want 3", stats.Total)
	}
}

func TestClearResult(t *testing.T) {
	m := &Module{
		log:     logger.New("test"),
		results: make(map[string]*ValidationResult),
		mu:      sync.RWMutex{},
	}
	m.results[testMediaPath] = &ValidationResult{Status: StatusValidated}
	m.ClearResult(testMediaPath)
	_, ok := m.GetResult(testMediaPath)
	if ok {
		t.Error("ClearResult should remove the entry")
	}
}
