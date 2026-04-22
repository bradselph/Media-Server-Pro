package hls

import (
	"context"
	"os"
	"testing"

	"media-server-pro/internal/config"
	"media-server-pro/internal/logger"
)

// ---------------------------------------------------------------------------
// parseFFmpegTime
// ---------------------------------------------------------------------------

func TestParseFFmpegTime_HoursMinsSecs(t *testing.T) {
	got := parseFFmpegTime("01:30:45.50")
	want := 1*3600.0 + 30*60.0 + 45.5
	if got < want-0.01 || got > want+0.01 {
		t.Errorf("parseFFmpegTime = %f, want %f", got, want)
	}
}

func TestParseFFmpegTime_ZeroTime(t *testing.T) {
	got := parseFFmpegTime("00:00:00.00")
	if got != 0 {
		t.Errorf("parseFFmpegTime = %f, want 0", got)
	}
}

func TestParseFFmpegTime_SecsOnly(t *testing.T) {
	got := parseFFmpegTime("45.5")
	if got < 45.4 || got > 45.6 {
		t.Errorf("parseFFmpegTime = %f, want ~45.5", got)
	}
}

func TestParseFFmpegTime_Invalid(t *testing.T) {
	got := parseFFmpegTime("invalid")
	if got != 0 {
		t.Errorf("parseFFmpegTime = %f, want 0", got)
	}
}

// ---------------------------------------------------------------------------
// calculateVariantProgress
// ---------------------------------------------------------------------------

func TestCalculateVariantProgress_ZeroCurrent(t *testing.T) {
	got := calculateVariantProgress(0, 100)
	if got != 0 {
		t.Errorf("calculateVariantProgress(0,100) = %f, want 0", got)
	}
}

func TestCalculateVariantProgress_NegativeCurrent(t *testing.T) {
	got := calculateVariantProgress(-1, 100)
	if got != 0 {
		t.Errorf("calculateVariantProgress(-1,100) = %f, want 0", got)
	}
}

func TestCalculateVariantProgress_HalfDone(t *testing.T) {
	got := calculateVariantProgress(50, 100)
	if got < 0.49 || got > 0.51 {
		t.Errorf("calculateVariantProgress(50,100) = %f, want ~0.5", got)
	}
}

func TestCalculateVariantProgress_Capped(t *testing.T) {
	got := calculateVariantProgress(200, 100)
	if got > 1.0 {
		t.Errorf("calculateVariantProgress should be capped at or below 1.0: %f", got)
	}
	if got < 0.9 {
		t.Errorf("calculateVariantProgress for 200/100 should be near cap: %f", got)
	}
}

func TestCalculateVariantProgress_ZeroDuration(t *testing.T) {
	got := calculateVariantProgress(10, 0)
	if got > 1.0 {
		t.Errorf("calculateVariantProgress with zero duration should cap: %f", got)
	}
}

// ---------------------------------------------------------------------------
// Module basics
// ---------------------------------------------------------------------------

func TestModuleName(t *testing.T) {
	m := &Module{}
	if m.Name() != "hls" {
		t.Errorf("Name() = %q, want %q", m.Name(), "hls")
	}
}

// ---------------------------------------------------------------------------
// Nil guard regression tests (FND-0511 through FND-0517)
// ---------------------------------------------------------------------------
// These tests verify that functions gracefully handle nil parameters instead of panicking.

func TestGenerateHLS_NilParams_ReturnsError(t *testing.T) {
	// FND-0511: GenerateHLS should return an error (not panic) when params is nil
	m := &Module{log: logger.New("test")}
	ctx := context.Background()

	job, err := m.GenerateHLS(ctx, nil)

	if job != nil {
		t.Errorf("GenerateHLS with nil params should return nil job, got %v", job)
	}
	if err == nil {
		t.Error("GenerateHLS with nil params should return an error, got nil")
	}
	if err.Error() != "GenerateHLSParams cannot be nil" {
		t.Errorf("GenerateHLS error message = %q, want %q", err.Error(), "GenerateHLSParams cannot be nil")
	}
}

func TestCheckOrGenerateHLS_NilParams_ReturnsError(t *testing.T) {
	// FND-0512: CheckOrGenerateHLS should return an error (not panic) when params is nil
	m := &Module{log: logger.New("test")}
	ctx := context.Background()

	job, err := m.CheckOrGenerateHLS(ctx, nil)

	if job != nil {
		t.Errorf("CheckOrGenerateHLS with nil params should return nil job, got %v", job)
	}
	if err == nil {
		t.Error("CheckOrGenerateHLS with nil params should return an error, got nil")
	}
	if err.Error() != "CheckOrGenerateHLSParams cannot be nil" {
		t.Errorf("CheckOrGenerateHLS error message = %q, want %q", err.Error(), "CheckOrGenerateHLSParams cannot be nil")
	}
}

func TestFilterQualitiesBySourceHeight_NilParams_ReturnsNil(t *testing.T) {
	// FND-0513: filterQualitiesBySourceHeight should return nil (not panic) when p is nil
	m := &Module{log: logger.New("test")}
	ctx := context.Background()

	result := m.filterQualitiesBySourceHeight(ctx, nil)

	if result != nil {
		t.Errorf("filterQualitiesBySourceHeight with nil params should return nil, got %v", result)
	}
}

func TestResolveHLSQualities_NilParams_ReturnsNil(t *testing.T) {
	// FND-0514: resolveHLSQualities should return nil (not panic) when p is nil
	m := &Module{log: logger.New("test")}
	ctx := context.Background()

	result := m.resolveHLSQualities(ctx, nil)

	if result != nil {
		t.Errorf("resolveHLSQualities with nil params should return nil, got %v", result)
	}
}

func TestWritePlaylistLine_NilOpts_ReturnsError(t *testing.T) {
	// FND-0515: writePlaylistLine should return an error (not panic) when opts is nil
	m := &Module{log: logger.New("test")}

	err := m.writePlaylistLine(nil, func() error { return nil })

	if err == nil {
		t.Error("writePlaylistLine with nil opts should return an error, got nil")
	}
	if err.Error() != "writePlaylistLineOpts cannot be nil" {
		t.Errorf("writePlaylistLine error message = %q, want %q", err.Error(), "writePlaylistLineOpts cannot be nil")
	}
}

func TestWriteVariantEntry_NilOpts_ReturnsError(t *testing.T) {
	// FND-0516 (part 1): writeVariantEntry should return an error (not panic) when opts is nil
	m := &Module{log: logger.New("test")}
	profile := &config.HLSQuality{Name: "720p"}
	tmpFile, err := os.CreateTemp("", "test-*.m3u8")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())
	defer tmpFile.Close()

	result := m.writeVariantEntry(tmpFile, nil, profile)

	if result == nil {
		t.Error("writeVariantEntry with nil opts should return an error, got nil")
	}
	if result.Error() != "writeVariantEntryOpts cannot be nil" {
		t.Errorf("writeVariantEntry error message = %q, want %q", result.Error(), "writeVariantEntryOpts cannot be nil")
	}
}

func TestWriteVariantEntry_NilProfile_ReturnsError(t *testing.T) {
	// FND-0516 (part 2): writeVariantEntry should return an error (not panic) when profile is nil
	m := &Module{log: logger.New("test")}
	opts := &writeVariantEntryOpts{MasterPath: "/tmp/master.m3u8", Variant: "720p"}
	tmpFile, err := os.CreateTemp("", "test-*.m3u8")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())
	defer tmpFile.Close()

	result := m.writeVariantEntry(tmpFile, opts, nil)

	if result == nil {
		t.Error("writeVariantEntry with nil profile should return an error, got nil")
	}
	if result.Error() != "HLSQuality profile cannot be nil" {
		t.Errorf("writeVariantEntry error message = %q, want %q", result.Error(), "HLSQuality profile cannot be nil")
	}
}

func TestGenerateMasterPlaylist_NilParams_ReturnsError(t *testing.T) {
	// FND-0517: generateMasterPlaylist should return an error (not panic) when p is nil
	m := &Module{log: logger.New("test")}

	err := m.generateMasterPlaylist(nil)

	if err == nil {
		t.Error("generateMasterPlaylist with nil params should return an error, got nil")
	}
	if err.Error() != "generateMasterPlaylistParams cannot be nil" {
		t.Errorf("generateMasterPlaylist error message = %q, want %q", err.Error(), "generateMasterPlaylistParams cannot be nil")
	}
}
