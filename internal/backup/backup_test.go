package backup

import (
	"os"
	"path/filepath"
	"testing"
)

// ---------------------------------------------------------------------------
// defaultBackupVersion
// ---------------------------------------------------------------------------

func TestDefaultBackupVersion_NonEmpty(t *testing.T) {
	got := defaultBackupVersion("4.1.0")
	if got != "4.1.0" {
		t.Errorf("defaultBackupVersion = %q, want %q", got, "4.1.0")
	}
}

func TestDefaultBackupVersion_Empty(t *testing.T) {
	got := defaultBackupVersion("")
	if got != defaultVersionFallback {
		t.Errorf("defaultBackupVersion = %q, want %q", got, defaultVersionFallback)
	}
}

// ---------------------------------------------------------------------------
// pathWithinBase
// ---------------------------------------------------------------------------

func TestPathWithinBase_Inside(t *testing.T) {
	base := filepath.Join(os.TempDir(), "backups")
	path := filepath.Join(base, "file.zip")
	if !pathWithinBase(pathScopeArgs{path: path, base: base}) {
		t.Error("path should be within base")
	}
}

func TestPathWithinBase_Equal(t *testing.T) {
	base := filepath.Join(os.TempDir(), "backups")
	if !pathWithinBase(pathScopeArgs{path: base, base: base}) {
		t.Error("path equal to base should be within")
	}
}

func TestPathWithinBase_Outside(t *testing.T) {
	base := filepath.Join(os.TempDir(), "backups")
	path := filepath.Join(os.TempDir(), "other", "file.zip")
	if pathWithinBase(pathScopeArgs{path: path, base: base}) {
		t.Error("path should NOT be within base")
	}
}

func TestPathWithinBase_Traversal(t *testing.T) {
	base := filepath.Join(os.TempDir(), "backups")
	path := filepath.Join(base, "..", "etc", "passwd")
	if pathWithinBase(pathScopeArgs{path: path, base: base}) {
		t.Error("path traversal should NOT be within base")
	}
}

// ---------------------------------------------------------------------------
// Module basics
// ---------------------------------------------------------------------------

func TestModuleName(t *testing.T) {
	m := &Module{}
	if m.Name() != "backup" {
		t.Errorf("Name() = %q, want %q", m.Name(), "backup")
	}
}

func TestModuleHealth_Default(t *testing.T) {
	m := &Module{}
	h := m.Health()
	if h.Name != "backup" {
		t.Errorf("Health().Name = %q", h.Name)
	}
}
