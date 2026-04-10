package config

func (m *Manager) applyUploadsEnvOverrides() {
	if val, ok := envGetBool("UPLOADS_ENABLED"); ok {
		m.config.Uploads.Enabled = val
	}
	if val, ok := envGetInt64("UPLOADS_MAX_FILE_SIZE"); ok {
		m.config.Uploads.MaxFileSize = val
	}
	if val := envGetStr("UPLOADS_ALLOWED_EXTENSIONS"); val != "" {
		m.config.Uploads.AllowedExtensions = splitTrimmed(val)
	}
	if val, ok := envGetBool("UPLOADS_REQUIRE_AUTH"); ok {
		m.config.Uploads.RequireAuth = val
	}
	if val, ok := envGetBool("UPLOADS_SCAN_FOR_MATURE"); ok {
		m.config.Uploads.ScanForMature = val
	}
}
