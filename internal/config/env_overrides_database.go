package config

import "time"

func (m *Manager) applyDatabaseEnvOverrides() {
	m.applyDatabaseConnectionOverrides()
	m.applyDatabasePoolOverrides()
	m.applyDatabaseTimeoutOverrides()
	m.applyDatabaseRetryOverrides()
	m.applyDatabaseTLSOverride()
	m.applyDatabaseSlowQueryOverride()
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

func (m *Manager) applyDatabaseTLSOverride() {
	if val := envGetStr("DATABASE_TLS_MODE"); val != "" {
		m.config.Database.TLSMode = val
	}
}

func (m *Manager) applyDatabaseSlowQueryOverride() {
	if val, ok := envGetDuration(time.Millisecond, "DATABASE_SLOW_QUERY_THRESHOLD_MS"); ok {
		m.config.Database.SlowQueryThreshold = val
	}
}
