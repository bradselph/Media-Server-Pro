package streaming

import (
	"path/filepath"
	"testing"

	"media-server-pro/internal/config"
)

// Regression tests for C14: the pooled I/O buffer must be at least as large as
// the largest configured chunk size (Default/Max/Mobile), or streaming.go's
// effectiveChunkSize := min(len(buf), chunkSize) silently caps every configured
// chunk size down to buffer_size with no error or log.

func newConfigManager(t *testing.T) *config.Manager {
	t.Helper()
	dir := t.TempDir()
	return config.NewManager(filepath.Join(dir, "config.json"))
}

func poolBufLen(m *Module) int {
	buf := m.bufferPool.Get().([]byte) //nolint:errcheck // test-only, pool invariant guaranteed by NewModule
	m.bufferPool.Put(buf)
	return len(buf)
}

func TestNewModule_BufferSizedToLargestConfiguredChunkSize(t *testing.T) {
	cfg := newConfigManager(t)
	// buffer_size (1MB default) would otherwise silently cap max_chunk_size (10MB
	// default) — raise max_chunk_size well above buffer_size to prove the pool
	// grows to match instead of truncating it.
	const wantMax = 20 * 1024 * 1024
	if err := cfg.Update(func(c *config.Config) {
		c.Streaming.BufferSize = 1024 * 1024
		c.Streaming.MaxChunkSize = wantMax
		c.Streaming.DefaultChunkSize = 1024 * 1024
		c.Streaming.MobileChunkSize = 512 * 1024
	}); err != nil {
		t.Fatalf("Update: %v", err)
	}

	m := NewModule(cfg)
	if got := poolBufLen(m); got < wantMax {
		t.Errorf("pooled buffer size = %d, want >= %d (max_chunk_size)", got, wantMax)
	}
}

func TestNewModule_BufferSizeFloorsToDefaultWhenUnset(t *testing.T) {
	cfg := newConfigManager(t)
	if err := cfg.Update(func(c *config.Config) {
		c.Streaming.BufferSize = 0
		c.Streaming.DefaultChunkSize = 1024 * 1024
		c.Streaming.MaxChunkSize = 512 * 1024
		c.Streaming.MobileChunkSize = 256 * 1024
	}); err != nil {
		t.Fatalf("Update: %v", err)
	}

	m := NewModule(cfg)
	if got := poolBufLen(m); got < 1024*1024 {
		t.Errorf("pooled buffer size = %d, want >= 1MB default floor", got)
	}
}

func TestNewModule_BufferSizeHasSaneUpperBound(t *testing.T) {
	cfg := newConfigManager(t)
	if err := cfg.Update(func(c *config.Config) {
		c.Streaming.BufferSize = 1024 * 1024
		c.Streaming.MaxChunkSize = 1024 * 1024 * 1024 // pathological 1GB
		c.Streaming.DefaultChunkSize = 1024 * 1024
		c.Streaming.MobileChunkSize = 512 * 1024
	}); err != nil {
		t.Fatalf("Update: %v", err)
	}

	m := NewModule(cfg)
	if got := poolBufLen(m); got > maxPooledBufferSize {
		t.Errorf("pooled buffer size = %d, must be capped at maxPooledBufferSize (%d)", got, maxPooledBufferSize)
	}
}
