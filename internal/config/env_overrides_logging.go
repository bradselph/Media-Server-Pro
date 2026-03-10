package config

func (m *Manager) applyLoggingEnvOverrides() {
	if val := envGetStr("LOG_LEVEL", "MEDIA_SERVER_LOG_LEVEL"); val != "" {
		m.config.Logging.Level = val
	}
	if val := envGetStr("LOG_FORMAT"); val != "" {
		m.config.Logging.Format = val
	}
	if val, ok := envGetBool("LOG_FILE_ENABLED"); ok {
		m.config.Logging.FileEnabled = val
	}
	if val, ok := envGetBool("LOG_COLOR_ENABLED"); ok {
		m.config.Logging.ColorEnabled = val
	}
	if val, ok := envGetBool("LOG_FILE_ROTATION"); ok {
		m.config.Logging.FileRotation = val
	}
	if val, ok := envGetInt64("LOG_MAX_FILE_SIZE"); ok {
		m.config.Logging.MaxFileSize = val
	}
	if val, ok := envGetInt("LOG_MAX_BACKUPS"); ok {
		m.config.Logging.MaxBackups = val
	}
}
