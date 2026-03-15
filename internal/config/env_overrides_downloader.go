package config

import "time"

func (m *Manager) applyDownloaderEnvOverrides() {
	if val, ok := envGetBool("DOWNLOADER_ENABLED"); ok {
		m.config.Downloader.Enabled = val
	}
	if val := envGetStr("DOWNLOADER_URL"); val != "" {
		m.config.Downloader.URL = val
	}
	if val := envGetStr("DOWNLOADER_DOWNLOADS_DIR"); val != "" {
		m.config.Downloader.DownloadsDir = val
	}
	if val := envGetStr("DOWNLOADER_IMPORT_DIR"); val != "" {
		m.config.Downloader.ImportDir = val
	}
	if val, ok := envGetDuration(time.Second, "DOWNLOADER_HEALTH_INTERVAL_SECONDS"); ok {
		m.config.Downloader.HealthInterval = val
	}
	if val, ok := envGetDuration(time.Second, "DOWNLOADER_REQUEST_TIMEOUT_SECONDS"); ok {
		m.config.Downloader.RequestTimeout = val
	}
}
