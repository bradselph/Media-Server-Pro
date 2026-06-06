package validator

import (
	"encoding/json"
	"testing"
	"time"

	"media-server-pro/internal/logger"
)

// probeDataFromJSON builds a ProbeData exactly the way probeFile does at runtime
// (json.Unmarshal of ffprobe's -print_format json output), so these tests
// exercise the real parse path without invoking ffprobe.
func probeDataFromJSON(t *testing.T, raw string) *ProbeData {
	t.Helper()
	var data ProbeData
	if err := json.Unmarshal([]byte(raw), &data); err != nil {
		t.Fatalf("unmarshal probe json: %v", err)
	}
	return &data
}

func TestParseProbeData_FullPipeline(t *testing.T) {
	m := &Module{log: logger.New("test")}
	data := probeDataFromJSON(t, `{
		"format": {"filename":"/v.mp4","format_name":"mov,mp4,m4a","duration":"123.45","bit_rate":"8000000","size":"123456"},
		"streams": [
			{"index":0,"codec_type":"video","codec_name":"h264","width":1920,"height":1080,"bit_rate":"7000000"},
			{"index":1,"codec_type":"audio","codec_name":"aac"}
		]
	}`)
	result := &ValidationResult{Path: "/v.mp4"}
	m.parseProbeData(result, data)

	if result.Container != "mov,mp4,m4a" {
		t.Errorf("Container = %q, want mov,mp4,m4a", result.Container)
	}
	if result.Duration != 123.45 {
		t.Errorf("Duration = %v, want 123.45", result.Duration)
	}
	if result.Bitrate != 8000000 {
		t.Errorf("Bitrate = %d, want 8000000", result.Bitrate)
	}
	if result.VideoCodec != "h264" || result.Width != 1920 || result.Height != 1080 {
		t.Errorf("video = %q %dx%d, want h264 1920x1080", result.VideoCodec, result.Width, result.Height)
	}
	if result.AudioCodec != "aac" {
		t.Errorf("AudioCodec = %q, want aac", result.AudioCodec)
	}
}

func TestParseFormatFields_EmptyAndInvalidLeaveZero(t *testing.T) {
	m := &Module{log: logger.New("test")}
	// Empty duration and a non-numeric bitrate must leave the result fields at
	// their zero values (the helpers return ok=false and the assignment is skipped).
	data := probeDataFromJSON(t, `{"format":{"duration":"","bit_rate":"not-a-number"}}`)
	result := &ValidationResult{Duration: -1, Bitrate: -1}
	m.parseFormatFields(result, data)
	if result.Duration != -1 {
		t.Errorf("Duration = %v, want unchanged (-1) when source empty", result.Duration)
	}
	if result.Bitrate != -1 {
		t.Errorf("Bitrate = %v, want unchanged (-1) when source invalid", result.Bitrate)
	}
}

func TestResultToRecord_MapsEveryField(t *testing.T) {
	m := &Module{log: logger.New("test")}
	now := time.Now()
	result := &ValidationResult{
		Path:           "/v.mp4",
		Status:         StatusValidated,
		ValidatedAt:    now,
		Duration:       12.5,
		VideoCodec:     "h264",
		AudioCodec:     "aac",
		Width:          1280,
		Height:         720,
		Bitrate:        5000,
		Container:      "mp4",
		Issues:         []string{"some issue"},
		Error:          "boom",
		VideoSupported: true,
		AudioSupported: false,
	}
	rec := m.resultToRecord(result)

	switch {
	case rec.Path != result.Path:
		t.Errorf("Path = %q, want %q", rec.Path, result.Path)
	case rec.Status != string(result.Status):
		t.Errorf("Status = %q, want %q", rec.Status, string(result.Status))
	case !rec.ValidatedAt.Equal(now):
		t.Errorf("ValidatedAt = %v, want %v", rec.ValidatedAt, now)
	case rec.Duration != result.Duration:
		t.Errorf("Duration = %v, want %v", rec.Duration, result.Duration)
	case rec.VideoCodec != result.VideoCodec:
		t.Errorf("VideoCodec = %q, want %q", rec.VideoCodec, result.VideoCodec)
	case rec.AudioCodec != result.AudioCodec:
		t.Errorf("AudioCodec = %q, want %q", rec.AudioCodec, result.AudioCodec)
	case rec.Width != result.Width || rec.Height != result.Height:
		t.Errorf("dims = %dx%d, want %dx%d", rec.Width, rec.Height, result.Width, result.Height)
	case rec.Bitrate != result.Bitrate:
		t.Errorf("Bitrate = %d, want %d", rec.Bitrate, result.Bitrate)
	case rec.Container != result.Container:
		t.Errorf("Container = %q, want %q", rec.Container, result.Container)
	case len(rec.Issues) != 1 || rec.Issues[0] != "some issue":
		t.Errorf("Issues = %v, want [some issue]", rec.Issues)
	case rec.Error != result.Error:
		t.Errorf("Error = %q, want %q", rec.Error, result.Error)
	case rec.VideoSupported != result.VideoSupported:
		t.Errorf("VideoSupported = %v, want %v", rec.VideoSupported, result.VideoSupported)
	case rec.AudioSupported != result.AudioSupported:
		t.Errorf("AudioSupported = %v, want %v", rec.AudioSupported, result.AudioSupported)
	}
}
