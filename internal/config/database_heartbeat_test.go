package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// preHeartbeatConfigJSON is a config.json as written by a build from before the
// heartbeat knobs existed: a complete "database" object with none of the
// heartbeat or recovery keys.
const preHeartbeatConfigJSON = `{
  "env_seed_migrated": true,
  "infra_ownership_migrated": true,
  "database": {
    "enabled": true,
    "host": "127.0.0.1",
    "port": 3306,
    "name": "mediaserver",
    "username": "mediaserver",
    "password": "secret",
    "max_open_conns": 25,
    "max_idle_conns": 10,
    "conn_max_lifetime": 3600000000000,
    "timeout": 10000000000,
    "max_retries": 3,
    "retry_interval": 2000000000,
    "tls_mode": "false",
    "slow_query_threshold": 500000000
  }
}`

// Load starts from DefaultConfig() and unmarshals the saved file over it, so
// keys absent from an older config.json must keep their defaults. If that ever
// changes (e.g. the section becomes a pointer, or loading switches to a
// replace-the-struct decode), every existing deployment would silently come up
// with heartbeat_enabled=false and heartbeat_interval=0 — a disabled heartbeat
// and a config that no longer passes validation.
func TestLoad_PreHeartbeatConfigKeepsHeartbeatDefaults(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(cfgPath, []byte(preHeartbeatConfigJSON), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	m := NewManager(cfgPath)
	if err := m.Load(); err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	db := m.Get().Database

	// Saved values must still win.
	if db.Name != "mediaserver" {
		t.Errorf("Database.Name = %q, want the saved %q", db.Name, "mediaserver")
	}

	defaults := defaultDatabaseConfig()
	if !db.HeartbeatEnabled {
		t.Error("HeartbeatEnabled = false after loading a pre-heartbeat config.json; " +
			"upgrading deployments would come up with no heartbeat at all")
	}
	if db.HeartbeatInterval != defaults.HeartbeatInterval {
		t.Errorf("HeartbeatInterval = %v, want the default %v", db.HeartbeatInterval, defaults.HeartbeatInterval)
	}
	if db.HeartbeatThreshold != defaults.HeartbeatThreshold {
		t.Errorf("HeartbeatThreshold = %d, want the default %d", db.HeartbeatThreshold, defaults.HeartbeatThreshold)
	}
	// Recovery restarts the stack, so an upgrade must never switch it on by itself.
	if db.RecoveryEnabled {
		t.Error("RecoveryEnabled = true after loading a pre-heartbeat config.json; " +
			"an upgrade must not start auto-restarting a host on its own")
	}
	if db.RecoveryCooldown != defaults.RecoveryCooldown {
		t.Errorf("RecoveryCooldown = %v, want the default %v", db.RecoveryCooldown, defaults.RecoveryCooldown)
	}
}

// The heartbeat/recovery knobs live in the infra override set, so they must
// apply on every load rather than only seeding a fresh config.json. An operator
// disabling runaway recovery in .env has to take effect on the next restart.
func TestLoad_HeartbeatEnvOverridesApplyToExistingConfig(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(cfgPath, []byte(preHeartbeatConfigJSON), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	t.Setenv("DATABASE_HEARTBEAT_INTERVAL", "45s")
	t.Setenv("DATABASE_HEARTBEAT_THRESHOLD", "9")
	t.Setenv("DATABASE_RECOVERY_ENABLED", "true")
	t.Setenv("DATABASE_RECOVERY_COOLDOWN", "20m")
	t.Setenv("DATABASE_RECOVERY_MAX_ATTEMPTS", "7")
	t.Setenv("DATABASE_RECOVERY_COMMAND", "/opt/msp/deploy.sh")

	m := NewManager(cfgPath)
	if err := m.Load(); err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	db := m.Get().Database

	if db.HeartbeatInterval != 45*time.Second {
		t.Errorf("HeartbeatInterval = %v, want 45s from env", db.HeartbeatInterval)
	}
	if db.HeartbeatThreshold != 9 {
		t.Errorf("HeartbeatThreshold = %d, want 9 from env", db.HeartbeatThreshold)
	}
	if !db.RecoveryEnabled {
		t.Error("RecoveryEnabled = false, want true from env")
	}
	if db.RecoveryCooldown != 20*time.Minute {
		t.Errorf("RecoveryCooldown = %v, want 20m from env", db.RecoveryCooldown)
	}
	if db.RecoveryMaxAttempts != 7 {
		t.Errorf("RecoveryMaxAttempts = %d, want 7 from env", db.RecoveryMaxAttempts)
	}
	if db.RecoveryCommand != "/opt/msp/deploy.sh" {
		t.Errorf("RecoveryCommand = %q, want %q from env", db.RecoveryCommand, "/opt/msp/deploy.sh")
	}
}

func TestValidateDatabaseHeartbeat_RejectsUnsafeValues(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*DatabaseConfig)
		wantErr bool
	}{
		{"defaults are valid", func(*DatabaseConfig) {}, false},
		{"sub-second interval", func(d *DatabaseConfig) { d.HeartbeatInterval = 500 * time.Millisecond }, true},
		{"zero interval", func(d *DatabaseConfig) { d.HeartbeatInterval = 0 }, true},
		{"zero threshold", func(d *DatabaseConfig) { d.HeartbeatThreshold = 0 }, true},
		{"negative threshold", func(d *DatabaseConfig) { d.HeartbeatThreshold = -1 }, true},
		// A threshold of 1 restarts on a single dropped ping, but that is a
		// deliberate choice an operator is allowed to make.
		{"threshold of one", func(d *DatabaseConfig) { d.HeartbeatThreshold = 1 }, false},
		// Recovery guards are only meaningful when recovery is on.
		{"short cooldown with recovery on", func(d *DatabaseConfig) {
			d.RecoveryEnabled = true
			d.RecoveryCooldown = 10 * time.Second
		}, true},
		{"short cooldown with recovery off", func(d *DatabaseConfig) {
			d.RecoveryEnabled = false
			d.RecoveryCooldown = 10 * time.Second
		}, false},
		{"negative max attempts", func(d *DatabaseConfig) {
			d.RecoveryEnabled = true
			d.RecoveryMaxAttempts = -1
		}, true},
		// Disabling the heartbeat must not surface its knobs as errors.
		{"disabled heartbeat skips its own checks", func(d *DatabaseConfig) {
			d.HeartbeatEnabled = false
			d.HeartbeatInterval = 0
			d.HeartbeatThreshold = 0
		}, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m := &Manager{config: DefaultConfig()}
			tc.mutate(&m.config.Database)

			errs := m.validateDatabaseHeartbeat()
			if tc.wantErr && len(errs) == 0 {
				t.Error("expected a validation error, got none")
			}
			if !tc.wantErr && len(errs) > 0 {
				t.Errorf("expected no validation error, got: %v", errs)
			}
		})
	}
}
