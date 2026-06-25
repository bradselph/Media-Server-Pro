package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// The HLS concurrent_limit and thumbnails worker_count knobs default to 0 =
// "auto" (scale with the host CPU/GPU at runtime). These tests cover the
// one-shot migrations that flip the legacy fixed defaults to auto, and confirm
// that 0 is a valid persisted value.

func TestMigrateHLSConcurrentLimitAuto(t *testing.T) {
	t.Run("flips legacy default 2 to 0 once", func(t *testing.T) {
		m := newTestManager()
		m.config.HLS.ConcurrentLimit = 2
		m.config.HLS.ConcurrentLimitMigrated = false

		if !m.migrateHLSConcurrentLimitAuto() {
			t.Fatal("expected migration to run on first pass")
		}
		if m.config.HLS.ConcurrentLimit != 0 {
			t.Errorf("ConcurrentLimit = %d, want 0 (auto)", m.config.HLS.ConcurrentLimit)
		}
		if !m.config.HLS.ConcurrentLimitMigrated {
			t.Error("ConcurrentLimitMigrated should be set after the migration")
		}
		// Idempotent: a second pass must not run again.
		if m.migrateHLSConcurrentLimitAuto() {
			t.Error("migration ran twice; should be guarded by the Migrated flag")
		}
	})

	t.Run("preserves a non-default explicit value", func(t *testing.T) {
		m := newTestManager()
		m.config.HLS.ConcurrentLimit = 6
		m.config.HLS.ConcurrentLimitMigrated = false

		m.migrateHLSConcurrentLimitAuto()
		if m.config.HLS.ConcurrentLimit != 6 {
			t.Errorf("ConcurrentLimit = %d, want 6 (explicit value preserved)", m.config.HLS.ConcurrentLimit)
		}
	})

	t.Run("no-op when already migrated", func(t *testing.T) {
		m := newTestManager()
		m.config.HLS.ConcurrentLimit = 2
		m.config.HLS.ConcurrentLimitMigrated = true

		if m.migrateHLSConcurrentLimitAuto() {
			t.Error("migration should not run when already migrated")
		}
		if m.config.HLS.ConcurrentLimit != 2 {
			t.Errorf("ConcurrentLimit = %d, want 2 (untouched once migrated)", m.config.HLS.ConcurrentLimit)
		}
	})
}

func TestMigrateThumbnailWorkerCountAuto(t *testing.T) {
	t.Run("flips legacy default 4 to 0 once", func(t *testing.T) {
		m := newTestManager()
		m.config.Thumbnails.WorkerCount = 4
		m.config.Thumbnails.WorkerCountMigrated = false

		if !m.migrateThumbnailWorkerCountAuto() {
			t.Fatal("expected migration to run on first pass")
		}
		if m.config.Thumbnails.WorkerCount != 0 {
			t.Errorf("WorkerCount = %d, want 0 (auto)", m.config.Thumbnails.WorkerCount)
		}
		if !m.config.Thumbnails.WorkerCountMigrated {
			t.Error("WorkerCountMigrated should be set after the migration")
		}
		if m.migrateThumbnailWorkerCountAuto() {
			t.Error("migration ran twice; should be guarded by the Migrated flag")
		}
	})

	t.Run("preserves a non-default explicit value", func(t *testing.T) {
		m := newTestManager()
		m.config.Thumbnails.WorkerCount = 12
		m.config.Thumbnails.WorkerCountMigrated = false

		m.migrateThumbnailWorkerCountAuto()
		if m.config.Thumbnails.WorkerCount != 12 {
			t.Errorf("WorkerCount = %d, want 12 (explicit value preserved)", m.config.Thumbnails.WorkerCount)
		}
	})
}

// TestAutoConcurrencyValuesPassValidation guards against a regression where the
// validator rejected 0 (it previously required >= 1). A default config must
// load and validate with both knobs at their new auto sentinel.
func TestAutoConcurrencyValuesPassValidation(t *testing.T) {
	m := newTestManager()
	m.config.HLS.ConcurrentLimit = 0
	m.config.Thumbnails.WorkerCount = 0
	for _, err := range m.Validate() {
		t.Errorf("unexpected validation error with auto (0) knobs: %v", err)
	}
}

// TestLoad_MigratesLegacyDefaultsToAuto exercises the full Load() path: a legacy
// config that predates the auto sentinel (concurrent_limit 2, worker_count 4,
// already env-migrated) is upgraded to auto and the change persists.
func TestLoad_MigratesLegacyDefaultsToAuto(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, testConfigFilename)

	legacy := DefaultConfig()
	legacy.EnvSeedMigrated = true // not a fresh install; skip env seeding
	legacy.HLS.ConcurrentLimit = 2
	legacy.HLS.ConcurrentLimitMigrated = false
	legacy.Thumbnails.WorkerCount = 4
	legacy.Thumbnails.WorkerCountMigrated = false
	data, _ := json.MarshalIndent(legacy, "", "  ")
	if err := os.WriteFile(cfgPath, data, 0o600); err != nil {
		t.Fatal(err)
	}

	m := NewManager(cfgPath)
	if err := m.Load(); err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if got := m.Get().HLS.ConcurrentLimit; got != 0 {
		t.Errorf("HLS.ConcurrentLimit = %d, want 0 (auto after migration)", got)
	}
	if got := m.Get().Thumbnails.WorkerCount; got != 0 {
		t.Errorf("Thumbnails.WorkerCount = %d, want 0 (auto after migration)", got)
	}

	// Reload: the migration must not re-run, and the auto value must persist.
	m2 := NewManager(cfgPath)
	if err := m2.Load(); err != nil {
		t.Fatalf("second Load() error: %v", err)
	}
	if got := m2.Get().HLS.ConcurrentLimit; got != 0 {
		t.Errorf("after reload HLS.ConcurrentLimit = %d, want 0", got)
	}
	if !m2.Get().HLS.ConcurrentLimitMigrated || !m2.Get().Thumbnails.WorkerCountMigrated {
		t.Error("migration flags should be persisted after first load")
	}
}
