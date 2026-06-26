package hls

import (
	"path/filepath"
	"testing"

	"media-server-pro/internal/config"
	"media-server-pro/internal/logger"
)

// newConcurrencyModule builds a minimal HLS module backed by a real config
// manager. limit == 0 leaves the default (auto) in place; a positive limit is
// applied as an explicit admin value.
func newConcurrencyModule(t *testing.T, limit int) *Module {
	t.Helper()
	mgr := config.NewManager(filepath.Join(t.TempDir(), "config.json"))
	if limit != 0 {
		if err := mgr.Update(func(c *config.Config) { c.HLS.ConcurrentLimit = limit }); err != nil {
			t.Fatalf("Update: %v", err)
		}
	}
	return &Module{config: mgr, log: logger.New("test")}
}

func TestEffectiveConcurrentLimit(t *testing.T) {
	t.Run("explicit value wins", func(t *testing.T) {
		m := newConcurrencyModule(t, 5)
		if got := m.EffectiveConcurrentLimit(); got != 5 {
			t.Errorf("got %d, want 5 (explicit admin value)", got)
		}
	})

	t.Run("auto software stays within [2,8]", func(t *testing.T) {
		m := newConcurrencyModule(t, 0) // default 0 = auto, no hardware encoder
		if got := m.EffectiveConcurrentLimit(); got < 2 || got > 8 {
			t.Errorf("auto software got %d, want within [2,8]", got)
		}
	})

	t.Run("auto with hardware encoder stays at 2", func(t *testing.T) {
		m := newConcurrencyModule(t, 0)
		m.hwEncoder = "h264_nvenc" // GPU-session bound, not CPU bound
		if got := m.EffectiveConcurrentLimit(); got != 2 {
			t.Errorf("auto hardware got %d, want 2", got)
		}
	})
}
