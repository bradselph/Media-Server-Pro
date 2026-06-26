package thumbnails

import "testing"

func TestEffectiveThumbnailWorkers(t *testing.T) {
	// An explicit (> 0) value is authoritative and returned verbatim.
	if got := effectiveThumbnailWorkers(3); got != 3 {
		t.Errorf("explicit 3 -> %d, want 3", got)
	}
	if got := effectiveThumbnailWorkers(50); got != 50 {
		t.Errorf("explicit 50 -> %d, want 50", got)
	}
	// 0 = auto: derived from CPU and clamped to [4, 16].
	if got := effectiveThumbnailWorkers(0); got < 4 || got > 16 {
		t.Errorf("auto -> %d, want within [4,16]", got)
	}
}
