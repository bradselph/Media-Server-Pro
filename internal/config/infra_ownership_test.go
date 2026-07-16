package config

import (
	"path/filepath"
	"testing"
)

// TestInfraOwnership_UpdaterBranchPersists locks in the fix for "admin changes a
// setting, it reverts on restart": after the InfraOwnershipMigrated one-shot,
// server/logging/updater are config.json-owned, so an admin-set updater branch
// survives a restart even when UPDATER_BRANCH is still set in the environment.
func TestInfraOwnership_UpdaterBranchPersists(t *testing.T) {
	t.Setenv("UPDATER_BRANCH", "main")
	path := filepath.Join(t.TempDir(), "config.json")

	// Fresh load seeds from env + defaults and marks the migration done.
	m := NewManager(path)
	if err := m.Load(); err != nil {
		t.Fatalf("initial load: %v", err)
	}
	if !m.Get().InfraOwnershipMigrated {
		t.Fatal("InfraOwnershipMigrated should be true after a fresh seed")
	}
	if got := m.Get().Updater.Branch; got != "main" {
		t.Fatalf("seeded branch = %q, want main (from UPDATER_BRANCH)", got)
	}

	// Admin changes the branch through the UI (persisted to config.json).
	if err := m.SetValuesBatch(map[string]any{"updater": map[string]any{"branch": "development"}}); err != nil {
		t.Fatalf("set branch: %v", err)
	}
	if got := m.Get().Updater.Branch; got != "development" {
		t.Fatalf("after set, branch = %q, want development", got)
	}

	// Restart: UPDATER_BRANCH=main is STILL set, but config.json now owns the
	// branch, so the admin's choice must persist (not revert to main).
	m2 := NewManager(path)
	if err := m2.Load(); err != nil {
		t.Fatalf("reload: %v", err)
	}
	if got := m2.Get().Updater.Branch; got != "development" {
		t.Errorf("after restart branch = %q, want development — admin edit must persist over UPDATER_BRANCH=main", got)
	}
}

// TestInfraOwnership_MigrationBakesEnv verifies that an EXISTING install (config
// without the flag) bakes the current env value once so effective behavior is
// unchanged on the migration load, then owns it thereafter.
func TestInfraOwnership_MigrationBakesEnv(t *testing.T) {
	t.Setenv("UPDATER_BRANCH", "release")
	path := filepath.Join(t.TempDir(), "config.json")

	// Simulate a pre-migration config.json: a saved branch, flag absent (false).
	m0 := NewManager(path)
	if err := m0.Load(); err != nil { // first load also migrates+saves
		t.Fatalf("seed: %v", err)
	}
	// Force the pre-migration state by clearing the flag and saving a stale branch.
	if err := m0.SetValuesBatch(map[string]any{"updater": map[string]any{"branch": "stale"}}); err != nil {
		t.Fatalf("set stale: %v", err)
	}
	m0.mu.Lock()
	m0.config.InfraOwnershipMigrated = false
	m0.mu.Unlock()
	if err := m0.Save(); err != nil {
		t.Fatalf("save pre-migration: %v", err)
	}

	// Migration load: bakes UPDATER_BRANCH=release over the stale value (effective
	// behavior == env, as before the upgrade), and marks the migration done.
	m := NewManager(path)
	if err := m.Load(); err != nil {
		t.Fatalf("migration load: %v", err)
	}
	if !m.Get().InfraOwnershipMigrated {
		t.Fatal("migration should set InfraOwnershipMigrated")
	}
	if got := m.Get().Updater.Branch; got != "release" {
		t.Errorf("migration branch = %q, want release (env baked so behavior is unchanged)", got)
	}
}
