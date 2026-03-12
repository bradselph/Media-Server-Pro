package config

func (m *Manager) applyFeatureEnvOverrides() {
	m.applyFeatureFlagsPart1()
	m.applyFeatureFlagsPart2()
}

// setFeatureBool applies a bool from env vars to dst if any of keys are set.
func setFeatureBool(dst *bool, keys ...string) {
	if val, ok := envGetBool(keys...); ok {
		*dst = val
	}
}

func (m *Manager) applyFeatureFlagsPart1() {
	f := &m.config.Features
	setFeatureBool(&f.EnableHLS, "FEATURE_HLS", "FEATURES_HLS")
	setFeatureBool(&f.EnableAnalytics, "FEATURE_ANALYTICS", "FEATURES_ANALYTICS")
	setFeatureBool(&f.EnablePlaylists, "FEATURE_PLAYLISTS", "FEATURES_PLAYLISTS")
	setFeatureBool(&f.EnableUploads, "FEATURE_UPLOADS", "FEATURES_UPLOADS")
	setFeatureBool(&f.EnableThumbnails, "FEATURE_THUMBNAILS", "FEATURES_THUMBNAILS")
	setFeatureBool(&f.EnableMatureScanner, "FEATURE_MATURE_SCANNER", "FEATURES_MATURE_SCANNER")
	setFeatureBool(&f.EnableRemoteMedia, "FEATURE_REMOTE_MEDIA", "FEATURES_REMOTE_MEDIA")
}

func (m *Manager) applyFeatureFlagsPart2() {
	f := &m.config.Features
	setFeatureBool(&f.EnableUserAuth, "FEATURE_USER_AUTH", "FEATURES_USER_AUTH")
	setFeatureBool(&f.EnableAdminPanel, "FEATURE_ADMIN_PANEL", "FEATURES_ADMIN", "FEATURES_ADMIN_PANEL")
	setFeatureBool(&f.EnableSuggestions, "FEATURE_SUGGESTIONS", "FEATURES_SUGGESTIONS")
	setFeatureBool(&f.EnableAutoDiscovery, "FEATURE_AUTO_DISCOVERY", "FEATURES_AUTO_DISCOVERY")
	setFeatureBool(&f.EnableReceiver, "FEATURE_RECEIVER", "FEATURES_RECEIVER")
	setFeatureBool(&f.EnableExtractor, "FEATURE_EXTRACTOR", "FEATURES_EXTRACTOR")
	setFeatureBool(&f.EnableCrawler, "FEATURE_CRAWLER", "FEATURES_CRAWLER")
	setFeatureBool(&f.EnableDuplicateDetection, "FEATURE_DUPLICATE_DETECTION", "FEATURES_DUPLICATE_DETECTION")
	setFeatureBool(&f.EnableHuggingFace, "FEATURE_HUGGINGFACE", "FEATURES_ENABLE_HUGGINGFACE", "FEATURES_HUGGINGFACE")
}

func (m *Manager) applyBackupEnvOverrides() {
	if val, ok := envGetInt("BACKUP_RETENTION_COUNT"); ok {
		m.config.Backup.RetentionCount = val
	}
}
