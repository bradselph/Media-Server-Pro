package config

import "time"

func (m *Manager) applyUIEnvOverrides() {
	if val, ok := envGetInt("UI_ITEMS_PER_PAGE"); ok {
		m.config.UI.ItemsPerPage = val
	}
	if val, ok := envGetInt("UI_MOBILE_ITEMS_PER_PAGE"); ok {
		m.config.UI.MobileItemsPerPage = val
	}
	if val, ok := envGetInt("UI_MOBILE_GRID_COLUMNS"); ok {
		m.config.UI.MobileGridColumns = val
	}
	if val, ok := envGetInt("UI_FEED_MAX_ITEMS"); ok {
		m.config.UI.FeedMaxItems = val
	}
	if val, ok := envGetInt("UI_FEED_DEFAULT_ITEMS"); ok {
		m.config.UI.FeedDefaultItems = val
	}
}

func (m *Manager) applyServerEnvOverrides() {
	if val := envGetStr("SERVER_HOST", "MEDIA_SERVER_HOST"); val != "" {
		m.config.Server.Host = val
		m.log.Debug("Applied env override: Server.Host = %s", val)
	}
	if val, ok := envGetInt("SERVER_PORT", "MEDIA_SERVER_PORT"); ok {
		if val < 1 || val > 65535 {
			m.log.Warn("SERVER_PORT value %d is out of range [1, 65535], ignoring", val)
		} else {
			m.config.Server.Port = val
			m.log.Debug("Applied env override: Server.Port = %d", val)
		}
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
