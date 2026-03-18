package database

import (
	"strings"
	"testing"

	"media-server-pro/internal/config"
)

// ---------------------------------------------------------------------------
// buildDSN
// ---------------------------------------------------------------------------

func TestBuildDSN_Basic(t *testing.T) {
	dsn := buildDSN(config.DatabaseConfig{
		Host:     "localhost",
		Port:     3306,
		Username: "root",
		Password: "secret",
		Name:     "testdb",
	})
	if !strings.Contains(dsn, "root") {
		t.Errorf("DSN should contain username: %s", dsn)
	}
	if !strings.Contains(dsn, "testdb") {
		t.Errorf("DSN should contain db name: %s", dsn)
	}
	if !strings.Contains(dsn, "localhost:3306") {
		t.Errorf("DSN should contain host:port: %s", dsn)
	}
	if !strings.Contains(dsn, "charset=utf8mb4") {
		t.Errorf("DSN should contain charset: %s", dsn)
	}
	if !strings.Contains(dsn, "parseTime=true") {
		t.Errorf("DSN should contain parseTime: %s", dsn)
	}
}

func TestBuildDSN_SpecialCharsInPassword(t *testing.T) {
	dsn := buildDSN(config.DatabaseConfig{
		Host:     "db.example.com",
		Port:     3307,
		Username: "admin",
		Password: "p@ss:w/rd",
		Name:     "media",
	})
	// FormatDSN should URL-encode special characters
	if dsn == "" {
		t.Error("DSN should not be empty")
	}
}

func TestBuildDSN_TLSMode(t *testing.T) {
	dsn := buildDSN(config.DatabaseConfig{
		Host:     "localhost",
		Port:     3306,
		Username: "root",
		Name:     "testdb",
		TLSMode:  "preferred",
	})
	if !strings.Contains(dsn, "tls=preferred") {
		t.Errorf("DSN should contain TLS mode: %s", dsn)
	}
}

func TestBuildDSN_NoTLS(t *testing.T) {
	dsn := buildDSN(config.DatabaseConfig{
		Host:     "localhost",
		Port:     3306,
		Username: "root",
		Name:     "testdb",
		TLSMode:  "",
	})
	if !strings.Contains(dsn, "tls=false") {
		t.Errorf("DSN should default to tls=false: %s", dsn)
	}
}

func TestBuildDSN_MultiStatementsFalse(t *testing.T) {
	dsn := buildDSN(config.DatabaseConfig{
		Host:     "localhost",
		Port:     3306,
		Username: "root",
		Name:     "testdb",
	})
	if !strings.Contains(dsn, "multiStatements=false") {
		t.Errorf("DSN should disable multi-statements: %s", dsn)
	}
}

// ---------------------------------------------------------------------------
// safeDSNString
// ---------------------------------------------------------------------------

func TestSafeDSNString_MasksPassword(t *testing.T) {
	safe := safeDSNString(config.DatabaseConfig{
		Host:     "db.example.com",
		Port:     3306,
		Username: "admin",
		Password: "supersecret",
		Name:     "media",
	})
	if strings.Contains(safe, "supersecret") {
		t.Error("safe DSN should NOT contain the password")
	}
	if !strings.Contains(safe, "***") {
		t.Error("safe DSN should contain masked password (***)")
	}
	if !strings.Contains(safe, "admin") {
		t.Error("safe DSN should contain username")
	}
	if !strings.Contains(safe, "db.example.com:3306") {
		t.Errorf("safe DSN should contain host:port: %s", safe)
	}
	if !strings.Contains(safe, "media") {
		t.Errorf("safe DSN should contain db name: %s", safe)
	}
}

// ---------------------------------------------------------------------------
// Module basics
// ---------------------------------------------------------------------------

func TestModuleName(t *testing.T) {
	m := &Module{}
	if m.Name() != "database" {
		t.Errorf("Name() = %q, want %q", m.Name(), "database")
	}
}

func TestModuleHealth_NotStarted(t *testing.T) {
	m := &Module{}
	h := m.Health()
	if h.Name != "database" {
		t.Errorf("Health().Name = %q", h.Name)
	}
	if h.Status != "unhealthy" {
		t.Errorf("unstarted module should be unhealthy: %q", h.Status)
	}
}

func TestSetHealth(t *testing.T) {
	m := &Module{}
	m.setHealth(true, "Connected")
	h := m.Health()
	if h.Status != "healthy" {
		t.Errorf("status = %q, want healthy", h.Status)
	}
	if h.Message != "Connected" {
		t.Errorf("message = %q, want Connected", h.Message)
	}
}

func TestIsConnected_NotStarted(t *testing.T) {
	m := &Module{}
	if m.IsConnected() {
		t.Error("should not be connected when not started")
	}
}

func TestGORM_Nil(t *testing.T) {
	m := &Module{}
	if m.GORM() != nil {
		t.Error("GORM() should return nil when not started")
	}
}

func TestDB_Nil(t *testing.T) {
	m := &Module{}
	if m.DB() != nil {
		t.Error("DB() should return nil when not started")
	}
}
