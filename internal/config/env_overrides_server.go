package config

import "time"

func (m *Manager) applyServerEnvOverrides() {
	if val := envGetStr("SERVER_HOST", "MEDIA_SERVER_HOST"); val != "" {
		m.config.Server.Host = val
		m.log.Debug("Applied env override: Server.Host = %s", val)
	}
	if val, ok := envGetInt("SERVER_PORT", "MEDIA_SERVER_PORT"); ok {
		m.config.Server.Port = val
		m.log.Debug("Applied env override: Server.Port = %d", val)
	}
	if val, ok := envGetBool("SERVER_ENABLE_HTTPS", "MEDIA_SERVER_ENABLE_HTTPS"); ok {
		m.config.Server.EnableHTTPS = val
	}
	if val := envGetStr("SERVER_CERT_FILE"); val != "" {
		m.config.Server.CertFile = val
	}
	if val := envGetStr("SERVER_KEY_FILE"); val != "" {
		m.config.Server.KeyFile = val
	}
	m.applyServerTimeoutOverrides()
}

func (m *Manager) applyServerTimeoutOverrides() {
	if val, ok := envGetDuration(time.Second, "SERVER_READ_TIMEOUT"); ok {
		m.config.Server.ReadTimeout = val
	}
	if val, ok := envGetDuration(time.Second, "SERVER_WRITE_TIMEOUT"); ok {
		m.config.Server.WriteTimeout = val
	}
	if val, ok := envGetDuration(time.Second, "SERVER_IDLE_TIMEOUT"); ok {
		m.config.Server.IdleTimeout = val
	}
	if val, ok := envGetDuration(time.Second, "SERVER_SHUTDOWN_TIMEOUT"); ok {
		m.config.Server.ShutdownTimeout = val
	}
	if val, ok := envGetInt("SERVER_MAX_HEADER_BYTES"); ok {
		m.config.Server.MaxHeaderBytes = val
	}
}
