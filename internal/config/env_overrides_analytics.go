package config

import "time"

func (m *Manager) applyAnalyticsEnvOverrides() {
	if val, ok := envGetBool("ANALYTICS_ENABLED"); ok {
		m.config.Analytics.Enabled = val
	}
	if val, ok := envGetInt("ANALYTICS_RETENTION_DAYS"); ok {
		m.config.Analytics.RetentionDays = val
	}
	if val, ok := envGetDuration(time.Minute, "ANALYTICS_SESSION_TIMEOUT_MINUTES"); ok {
		m.config.Analytics.SessionTimeout = val
	}
	if val, ok := envGetDuration(time.Minute, "ANALYTICS_CLEANUP_INTERVAL_MINUTES"); ok {
		m.config.Analytics.CleanupInterval = val
	}
	if val, ok := envGetBool("ANALYTICS_TRACK_PLAYBACK"); ok {
		m.config.Analytics.TrackPlayback = val
	}
	if val, ok := envGetBool("ANALYTICS_TRACK_VIEWS"); ok {
		m.config.Analytics.TrackViews = val
	}
	if val, ok := envGetDuration(time.Minute, "ANALYTICS_VIEW_COOLDOWN_MINUTES"); ok {
		m.config.Analytics.ViewCooldown = val
	}
	if val, ok := envGetInt("ANALYTICS_MAX_RECONSTRUCT_EVENTS"); ok {
		m.config.Analytics.MaxReconstructEvents = val
	}
}
