package analytics

import (
	"testing"
)

// ---------------------------------------------------------------------------
// completionRateFromCounts
// ---------------------------------------------------------------------------

func TestCompletionRateFromCounts_Normal(t *testing.T) {
	rate := completionRateFromCounts(3, 10)
	if rate < 0.29 || rate > 0.31 {
		t.Errorf("completionRateFromCounts(3,10) = %f, want ~0.3", rate)
	}
}

func TestCompletionRateFromCounts_ZeroPlaybacks(t *testing.T) {
	rate := completionRateFromCounts(5, 0)
	if rate != 0 {
		t.Errorf("completionRateFromCounts(5,0) = %f, want 0", rate)
	}
}

func TestCompletionRateFromCounts_NegativePlaybacks(t *testing.T) {
	rate := completionRateFromCounts(5, -1)
	if rate != 0 {
		t.Errorf("completionRateFromCounts(5,-1) = %f, want 0", rate)
	}
}

func TestCompletionRateFromCounts_AllCompleted(t *testing.T) {
	rate := completionRateFromCounts(10, 10)
	if rate != 1.0 {
		t.Errorf("completionRateFromCounts(10,10) = %f, want 1.0", rate)
	}
}

func TestCompletionRateFromCounts_ZeroCompletions(t *testing.T) {
	rate := completionRateFromCounts(0, 10)
	if rate != 0 {
		t.Errorf("completionRateFromCounts(0,10) = %f, want 0", rate)
	}
}

// ---------------------------------------------------------------------------
// Module basics
// ---------------------------------------------------------------------------

func TestModuleName(t *testing.T) {
	m := &Module{}
	if m.Name() != "analytics" {
		t.Errorf("Name() = %q, want %q", m.Name(), "analytics")
	}
}
