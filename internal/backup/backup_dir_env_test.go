package backup

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"media-server-pro/internal/config"
)

// ---------------------------------------------------------------------------
// resolveBackupDir
//
// Regression coverage for R02: the installer/setup.sh "Backups dir" prompt
// writes BACKUP_DIR into the generated .env, but the backup module used to
// ignore it and always hardcode <Directories.Data>/backups. These tests pin
// the fix: BACKUP_DIR (when set) wins, and the historical default is kept
// unchanged when it is unset.
// ---------------------------------------------------------------------------

func TestResolveBackupDir_DefaultFallsBackToDataSubdir(t *testing.T) {
	dataDir := filepath.Join(t.TempDir(), "data")
	got := resolveBackupDir(dataDir)
	want := filepath.Join(dataDir, "backups")
	if got != want {
		t.Errorf("resolveBackupDir(%q) = %q, want %q", dataDir, got, want)
	}
}

func TestResolveBackupDir_EnvOverrideAbsolute(t *testing.T) {
	dataDir := filepath.Join(t.TempDir(), "data")
	override := filepath.Join(t.TempDir(), "offsite-backups")
	t.Setenv(backupDirEnvVar, override)

	got := resolveBackupDir(dataDir)
	if got != override {
		t.Errorf("resolveBackupDir(%q) with %s=%q = %q, want %q", dataDir, backupDirEnvVar, override, got, override)
	}
}

func TestResolveBackupDir_EnvOverrideRelativeIsMadeAbsolute(t *testing.T) {
	dataDir := filepath.Join(t.TempDir(), "data")
	t.Setenv(backupDirEnvVar, "./relative-backups")

	got := resolveBackupDir(dataDir)
	if !filepath.IsAbs(got) {
		t.Fatalf("resolveBackupDir(%q) = %q, want an absolute path", dataDir, got)
	}
	if got == filepath.Join(dataDir, "backups") {
		t.Errorf("resolveBackupDir(%q) fell back to the default instead of honoring %s", dataDir, backupDirEnvVar)
	}
}

func TestResolveBackupDir_EnvWhitespaceOnlyFallsBackToDefault(t *testing.T) {
	dataDir := filepath.Join(t.TempDir(), "data")
	t.Setenv(backupDirEnvVar, "   ")

	got := resolveBackupDir(dataDir)
	want := filepath.Join(dataDir, "backups")
	if got != want {
		t.Errorf("resolveBackupDir(%q) with blank %s = %q, want default %q", dataDir, backupDirEnvVar, got, want)
	}
}

// ---------------------------------------------------------------------------
// NewModule wiring
// ---------------------------------------------------------------------------

func TestNewModule_DefaultBackupDirUnchanged(t *testing.T) {
	cfg := config.NewManager(filepath.Join(t.TempDir(), "config.json"))
	m := NewModule(cfg, nil)

	want := filepath.Join(cfg.Get().Directories.Data, "backups")
	if m.backupDir != want {
		t.Errorf("NewModule().backupDir = %q, want unchanged default %q", m.backupDir, want)
	}
}

func TestNewModule_HonorsBackupDirEnvOverride(t *testing.T) {
	override := filepath.Join(t.TempDir(), "offsite-backups")
	t.Setenv(backupDirEnvVar, override)

	cfg := config.NewManager(filepath.Join(t.TempDir(), "config.json"))
	m := NewModule(cfg, nil)

	if m.backupDir != override {
		t.Errorf("NewModule().backupDir = %q, want %q (from %s)", m.backupDir, override, backupDirEnvVar)
	}
}

// TestResolveBackupPath_UsesConfiguredBackupDir pins that path resolution used
// by ListBackups (indirectly, via the same backupDir), RestoreBackup, and
// DeleteBackup all derive from the single m.backupDir field set at
// construction — so an env override applies consistently everywhere, not just
// to newly created backups.
func TestResolveBackupPath_UsesConfiguredBackupDir(t *testing.T) {
	custom := filepath.Join(t.TempDir(), "custom-backups")
	m := &Module{backupDir: custom}

	got, err := m.resolveBackupPath("backup_123")
	if err != nil {
		t.Fatalf("resolveBackupPath returned unexpected error: %v", err)
	}
	want := filepath.Join(custom, "backup_123.zip")
	if got != want {
		t.Errorf("resolveBackupPath() = %q, want %q", got, want)
	}
}

// ---------------------------------------------------------------------------
// BackupDir
// ---------------------------------------------------------------------------

func TestModule_BackupDir_ReturnsResolvedDir(t *testing.T) {
	custom := filepath.Join(t.TempDir(), "custom-backups")
	m := &Module{backupDir: custom}

	if got := m.BackupDir(); got != custom {
		t.Errorf("BackupDir() = %q, want %q", got, custom)
	}
}

// ---------------------------------------------------------------------------
// legacyBackupOrphanWarning
//
// Regression coverage for the R02 follow-up: once BACKUP_DIR redirects
// storage, archives left behind at the historical <dataDir>/backups location
// (e.g. from before BACKUP_DIR was set) must not go unnoticed.
// ---------------------------------------------------------------------------

func TestLegacyBackupOrphanWarning_NoWarningWhenDirsMatch(t *testing.T) {
	dataDir := t.TempDir()
	backupDir := legacyBackupDir(dataDir)

	if got := legacyBackupOrphanWarning(dataDir, backupDir); got != "" {
		t.Errorf("legacyBackupOrphanWarning() = %q, want empty (backupDir is the legacy default)", got)
	}
}

func TestLegacyBackupOrphanWarning_NoWarningWhenLegacyDirEmpty(t *testing.T) {
	dataDir := t.TempDir()
	backupDir := filepath.Join(t.TempDir(), "offsite-backups")

	if got := legacyBackupOrphanWarning(dataDir, backupDir); got != "" {
		t.Errorf("legacyBackupOrphanWarning() = %q, want empty (legacy dir has no archives)", got)
	}
}

func TestLegacyBackupOrphanWarning_WarnsWhenLegacyDirHasArchives(t *testing.T) {
	dataDir := t.TempDir()
	legacy := legacyBackupDir(dataDir)
	if err := os.MkdirAll(legacy, 0o750); err != nil {
		t.Fatalf("failed to create legacy dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(legacy, "backup_20240101_000000.zip"), []byte("x"), 0o640); err != nil {
		t.Fatalf("failed to write fake archive: %v", err)
	}
	backupDir := filepath.Join(t.TempDir(), "offsite-backups")

	got := legacyBackupOrphanWarning(dataDir, backupDir)
	if got == "" {
		t.Fatal("legacyBackupOrphanWarning() = \"\", want a non-empty warning")
	}
	if !strings.Contains(got, legacy) || !strings.Contains(got, backupDir) {
		t.Errorf("legacyBackupOrphanWarning() = %q, want it to mention both %q and %q", got, legacy, backupDir)
	}
}

func TestLegacyBackupOrphanWarning_IgnoresNonZipFiles(t *testing.T) {
	dataDir := t.TempDir()
	legacy := legacyBackupDir(dataDir)
	if err := os.MkdirAll(legacy, 0o750); err != nil {
		t.Fatalf("failed to create legacy dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(legacy, ".gitkeep"), []byte(""), 0o640); err != nil {
		t.Fatalf("failed to write placeholder file: %v", err)
	}
	backupDir := filepath.Join(t.TempDir(), "offsite-backups")

	if got := legacyBackupOrphanWarning(dataDir, backupDir); got != "" {
		t.Errorf("legacyBackupOrphanWarning() = %q, want empty (no .zip archives present)", got)
	}
}
