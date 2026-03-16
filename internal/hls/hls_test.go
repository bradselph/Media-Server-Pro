package hls

import (
	"testing"
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
