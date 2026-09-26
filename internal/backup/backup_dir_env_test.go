package backup

import (
	"path/filepath"
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
