package admin

import (
	"testing"

	"media-server-pro/internal/config"
)

// ---------------------------------------------------------------------------
// Module basics
// ---------------------------------------------------------------------------

func TestModuleName(t *testing.T) {
	m := &Module{}
	if m.Name() != "admin" {
		t.Errorf("Name() = %q, want %q", m.Name(), "admin")
	}
}

func TestModuleHealth_Default(t *testing.T) {
	m := &Module{}
	h := m.Health()
	if h.Name != "admin" {
		t.Errorf("Health().Name = %q", h.Name)
	}
}

// ---------------------------------------------------------------------------
// AuditLogParams
// ---------------------------------------------------------------------------

func TestAuditLogParams_Fields(t *testing.T) {
	p := AuditLogParams{
		Action:   "user.create",
		UserID:   "user-1",
		Username: "admin",
		Resource: "users",
		Success:  true,
	}
	if p.Action != "user.create" {
		t.Errorf("Action = %q", p.Action)
	}
	if p.UserID != "user-1" {
		t.Errorf("UserID = %q", p.UserID)
	}
	if p.Username != "admin" {
		t.Errorf("Username = %q", p.Username)
	}
	if p.Resource != "users" {
		t.Errorf("Resource = %q", p.Resource)
	}
	if !p.Success {
		t.Error("Success should be true")
	}
}

// ---------------------------------------------------------------------------
// SystemInfo
// ---------------------------------------------------------------------------

func TestSystemInfo_Fields(t *testing.T) {
	si := SystemInfo{}
	if si.GoVersion != "" {
		t.Error("zero SystemInfo GoVersion should be empty")
	}
}

// ---------------------------------------------------------------------------
// buildConfig* map builders
// ---------------------------------------------------------------------------

func TestBuildConfigServerMap(t *testing.T) {
	cfg := &config.Config{}
	cfg.Server.Port = 8080
	cfg.Server.Host = "0.0.0.0"
	m := buildConfigServerMap(cfg, nil)
	if m == nil {
		t.Fatal("buildConfigServerMap returned nil")
	}
	if m["port"] != cfg.Server.Port {
		t.Errorf("port = %v, want %d", m["port"], cfg.Server.Port)
	}
}

func TestBuildConfigFeaturesMap(t *testing.T) {
	cfg := &config.Config{}
	m := buildConfigFeaturesMap(cfg, nil)
	if m == nil {
		t.Fatal("buildConfigFeaturesMap returned nil")
	}
}

func TestBuildConfigSecurityMap(t *testing.T) {
	cfg := &config.Config{}
	m := buildConfigSecurityMap(cfg, nil)
	if m == nil {
		t.Fatal("buildConfigSecurityMap returned nil")
	}
}

func TestBuildConfigDatabaseMap(t *testing.T) {
	cfg := &config.Config{}
	cfg.Database.Host = "db.example.com"
	m := buildConfigDatabaseMap(cfg, nil)
	if m == nil {
		t.Fatal("buildConfigDatabaseMap returned nil")
	}
	if m["host"] != "db.example.com" {
		t.Errorf("host = %v", m["host"])
	}
}
