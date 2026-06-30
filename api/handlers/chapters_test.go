package handlers

import (
	"math"
	"testing"
)

// TestValidateChapterTime locks in the bounds check that keeps forged chapter
// timestamps (NaN/Inf, negative, or absurdly large) out of the database, where
// they would break start_time sorting and the player timeline.
func TestValidateChapterTime(t *testing.T) {
	cases := []struct {
		name string
		v    float64
		want bool
	}{
		{"zero", 0, true},
		{"mid-range", 3600, true},
		{"max boundary", chapterMaxTimeSeconds, true},
		{"just over max", chapterMaxTimeSeconds + 1, false},
		{"negative", -1, false},
		{"NaN", math.NaN(), false},
		{"positive infinity", math.Inf(1), false},
		{"negative infinity", math.Inf(-1), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := validateChapterTime(tc.v); got != tc.want {
				t.Errorf("validateChapterTime(%v) = %v, want %v", tc.v, got, tc.want)
			}
		})
	}
}
