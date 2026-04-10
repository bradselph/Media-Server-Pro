package updater

import (
	"path/filepath"
	"testing"

	"media-server-pro/internal/config"
)

const testConfigFile = "config.json"

// ---------------------------------------------------------------------------
// GitHub constants
// ---------------------------------------------------------------------------

func TestGitHubConstants(t *testing.T) {
	if GitHubOwner == "" {
		t.Error("GitHubOwner should not be empty")
	}
	if GitHubRepo == "" {
		t.Error("GitHubRepo should not be empty")
	}
	if GitHubAPI == "" {
		t.Error("GitHubAPI should not be empty")
	}
	if GitHubOwner != "bradselph" {
		t.Errorf("GitHubOwner = %q, want bradselph", GitHubOwner)
	}
	if GitHubRepo != "Media-Server-Pro" {
		t.Errorf("GitHubRepo = %q, want Media-Server-Pro", GitHubRepo)
	}
}

// ---------------------------------------------------------------------------
// Module basics
// ---------------------------------------------------------------------------

func TestModuleName(t *testing.T) {
	cfg := config.NewManager(filepath.Join(t.TempDir(), testConfigFile))
	m := NewModule(cfg, "4.0.0")
	if m.Name() != "updater" {
		t.Errorf("Name() = %q, want %q", m.Name(), "updater")
	}
}

func TestModuleHealth_Default(t *testing.T) {
	cfg := config.NewManager(filepath.Join(t.TempDir(), testConfigFile))
	m := NewModule(cfg, "4.0.0")
	h := m.Health()
	if h.Name != "updater" {
		t.Errorf("Health().Name = %q", h.Name)
	}
}

func TestNewModule_Version(t *testing.T) {
	cfg := config.NewManager(filepath.Join(t.TempDir(), testConfigFile))
	m := NewModule(cfg, "1.2.3")
	if m.currentVersion != "1.2.3" {
		t.Errorf("currentVersion = %q, want 1.2.3", m.currentVersion)
	}
}
