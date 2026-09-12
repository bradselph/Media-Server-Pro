package config

import (
	"strings"
	"time"
)

func (m *Manager) applyDatabaseEnvOverrides() {
	m.applyDatabaseConnectionOverrides()
	m.applyDatabasePoolOverrides()
	m.applyDatabaseTimeoutOverrides()
	m.applyDatabaseRetryOverrides()
	m.applyDatabaseHeartbeatOverrides()
	if val := envGetStr("DATABASE_TLS_MODE"); val != "" {
		m.config.Database.TLSMode = val
	}
	if val, ok := envGetDuration(time.Millisecond, "DATABASE_SLOW_QUERY_THRESHOLD_MS"); ok {
		m.config.Database.SlowQueryThreshold = val
	}
}

// applyDatabaseHeartbeatOverrides wires the liveness heartbeat and its recovery
// action. These belong with the other infra overrides (not the tunable set)
// because the recovery command is a property of how the host was deployed, and
// must not be editable from the admin UI: it runs a shell command as the server
// user.
func (m *Manager) applyDatabaseHeartbeatOverrides() {
	if val, ok := envGetBool("DATABASE_HEARTBEAT_ENABLED"); ok {
		m.config.Database.HeartbeatEnabled = val
	}
	if val, ok := envGetDurationString("DATABASE_HEARTBEAT_INTERVAL"); ok {
		m.config.Database.HeartbeatInterval = val
	} else if val, ok := envGetDuration(time.Second, "DATABASE_HEARTBEAT_INTERVAL_SECONDS"); ok {
		m.config.Database.HeartbeatInterval = val
	}
	if val, ok := envGetInt("DATABASE_HEARTBEAT_THRESHOLD"); ok {
		m.config.Database.HeartbeatThreshold = val
	}

	if val, ok := envGetBool("DATABASE_RECOVERY_ENABLED"); ok {
		m.config.Database.RecoveryEnabled = val
	}
	if val := envGetStr("DATABASE_RECOVERY_COMMAND"); val != "" {
		m.config.Database.RecoveryCommand = val
	}
	if val := envGetStr("DATABASE_RECOVERY_ARGS"); val != "" {
		m.config.Database.RecoveryArgs = strings.Fields(val)
	}
	if val, ok := envGetDurationString("DATABASE_RECOVERY_COOLDOWN"); ok {
		m.config.Database.RecoveryCooldown = val
	} else if val, ok := envGetDuration(time.Minute, "DATABASE_RECOVERY_COOLDOWN_MINUTES"); ok {
		m.config.Database.RecoveryCooldown = val
	}
	if val, ok := envGetInt("DATABASE_RECOVERY_MAX_ATTEMPTS"); ok {
		m.config.Database.RecoveryMaxAttempts = val
	}
}

func (m *Manager) applyDatabaseConnectionOverrides() {
	if val, ok := envGetBool("DATABASE_ENABLED"); ok {
		m.config.Database.Enabled = val
	}
	if val := envGetStr("DATABASE_HOST"); val != "" {
		m.config.Database.Host = val
	}
	if val, ok := envGetInt("DATABASE_PORT"); ok {
		m.config.Database.Port = val
	}
	if val := envGetStr("DATABASE_NAME"); val != "" {
		m.config.Database.Name = val
	}
	if val := envGetStr("DATABASE_USERNAME"); val != "" {
		m.config.Database.Username = val
	}
	if val := envGetStr("DATABASE_PASSWORD"); val != "" {
		m.config.Database.Password = val
		m.log.Info("Database password set from DATABASE_PASSWORD environment variable")
	}
}

func (m *Manager) applyDatabasePoolOverrides() {
	if val, ok := envGetInt("DATABASE_MAX_OPEN_CONNS"); ok {
		m.config.Database.MaxOpenConns = val
	}
	if val, ok := envGetInt("DATABASE_MAX_IDLE_CONNS"); ok {
		m.config.Database.MaxIdleConns = val
	}
	if val, ok := envGetDurationString("DATABASE_CONN_MAX_LIFETIME"); ok {
		m.config.Database.ConnMaxLifetime = val
	} else if val, ok := envGetDuration(time.Hour, "DATABASE_CONN_MAX_LIFETIME_HOURS"); ok {
		m.config.Database.ConnMaxLifetime = val
	}
}

func (m *Manager) applyDatabaseTimeoutOverrides() {
	if val, ok := envGetDurationString("DATABASE_TIMEOUT"); ok {
		m.config.Database.Timeout = val
	} else if val, ok := envGetDuration(time.Second, "DATABASE_TIMEOUT_SECONDS"); ok {
		m.config.Database.Timeout = val
	}
}

func (m *Manager) applyDatabaseRetryOverrides() {
	if val, ok := envGetInt("DATABASE_MAX_RETRIES"); ok {
		m.config.Database.MaxRetries = val
	}
	if val, ok := envGetDurationString("DATABASE_RETRY_INTERVAL"); ok {
		m.config.Database.RetryInterval = val
	} else if val, ok := envGetDuration(time.Second, "DATABASE_RETRY_INTERVAL_SECONDS"); ok {
		m.config.Database.RetryInterval = val
	}
}
