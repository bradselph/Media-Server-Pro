package config

func (m *Manager) applyDirectoryEnvOverrides() {
	m.applyDirectoryVideoMusicOverrides()
	m.applyDirectoryOtherOverrides()
}

func (m *Manager) applyDirectoryVideoMusicOverrides() {
	if val := envGetStr("VIDEOS_DIR", "MEDIA_SERVER_VIDEOS_DIR"); val != "" {
		m.config.Directories.Videos = val
	}
	if val := envGetStr("MUSIC_DIR", "MEDIA_SERVER_MUSIC_DIR"); val != "" {
		m.config.Directories.Music = val
	}
	if val := envGetStr("THUMBNAILS_DIR"); val != "" {
		m.config.Directories.Thumbnails = val
	}
	if val := envGetStr("PLAYLISTS_DIR"); val != "" {
		m.config.Directories.Playlists = val
	}
	if val := envGetStr("UPLOADS_DIR"); val != "" {
		m.config.Directories.Uploads = val
	}
}

func (m *Manager) applyDirectoryOtherOverrides() {
	if val := envGetStr("ANALYTICS_DIR"); val != "" {
		m.config.Directories.Analytics = val
	}
	if val := envGetStr("HLS_CACHE_DIR"); val != "" {
		m.config.Directories.HLSCache = val
	}
	if val := envGetStr("DATA_DIR", "MEDIA_SERVER_DATA_DIR"); val != "" {
		m.config.Directories.Data = val
	}
	if val := envGetStr("LOGS_DIR"); val != "" {
		m.config.Directories.Logs = val
	}
	if val := envGetStr("TEMP_DIR"); val != "" {
		m.config.Directories.Temp = val
	}
}
