package config

func (m *Manager) applyThumbnailsEnvOverrides() {
	m.applyThumbnailsFeatureOverrides()
	m.applyThumbnailsDimensionOverrides()
	m.applyThumbnailsIntervalOverrides()
	m.applyThumbnailsWorkerOverrides()
}

func (m *Manager) applyThumbnailsFeatureOverrides() {
	if val, ok := envGetBool("THUMBNAILS_ENABLED"); ok {
		m.config.Thumbnails.Enabled = val
	}
	if val, ok := envGetBool("THUMBNAILS_AUTO_GENERATE"); ok {
		m.config.Thumbnails.AutoGenerate = val
	}
	if val, ok := envGetBool("THUMBNAILS_GENERATE_ON_ACCESS"); ok {
		m.config.Thumbnails.GenerateOnAccess = val
	}
}

func (m *Manager) applyThumbnailsDimensionOverrides() {
	if val, ok := envGetInt("THUMBNAILS_WIDTH"); ok {
		m.config.Thumbnails.Width = val
	}
	if val, ok := envGetInt("THUMBNAILS_HEIGHT"); ok {
		m.config.Thumbnails.Height = val
	}
	if val, ok := envGetInt("THUMBNAILS_QUALITY"); ok {
		m.config.Thumbnails.Quality = val
	}
}

func (m *Manager) applyThumbnailsIntervalOverrides() {
	if val, ok := envGetInt("THUMBNAILS_VIDEO_INTERVAL"); ok {
		m.config.Thumbnails.VideoInterval = val
	}
	if val, ok := envGetInt("THUMBNAILS_PREVIEW_COUNT"); ok {
		m.config.Thumbnails.PreviewCount = val
	}
}

func (m *Manager) applyThumbnailsWorkerOverrides() {
	if val, ok := envGetInt("THUMBNAILS_QUEUE_SIZE"); ok {
		m.config.Thumbnails.QueueSize = val
	}
	if val, ok := envGetInt("THUMBNAILS_WORKER_COUNT"); ok {
		m.config.Thumbnails.WorkerCount = val
	}
}
