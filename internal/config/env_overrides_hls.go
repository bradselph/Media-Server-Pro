package config

import (
	"strings"
	"time"
)

func (m *Manager) applyHLSEnvOverrides() {
	m.applyHLSBaseOverrides()
	m.applyHLSQualityOverrides()
	m.applyHLSOptionsOverrides()
}

func (m *Manager) applyHLSBaseOverrides() {
	m.applyHLSBaseOverridesCore()
	m.applyHLSCleanupOverrides()
	m.applyHLSConcurrencyOverrides()
}

func (m *Manager) applyHLSBaseOverridesCore() {
	if val, ok := envGetBool("HLS_ENABLED"); ok {
		m.config.HLS.Enabled = val
	}
	if val, ok := envGetInt("HLS_SEGMENT_DURATION"); ok {
		m.config.HLS.SegmentDuration = val
	}
	if val, ok := envGetInt("HLS_PLAYLIST_LENGTH"); ok {
		m.config.HLS.PlaylistLength = val
	}
	if val, ok := envGetBool("HLS_AUTO_GENERATE"); ok {
		m.config.HLS.AutoGenerate = val
	}
}

func (m *Manager) applyHLSCleanupOverrides() {
	if val, ok := envGetBool("HLS_CLEANUP_ENABLED"); ok {
		m.config.HLS.CleanupEnabled = val
	}
	if val, ok := envGetDuration(time.Minute, "HLS_CLEANUP_INTERVAL_MINUTES"); ok {
		m.config.HLS.CleanupInterval = val
	}
	if val, ok := envGetInt("HLS_RETENTION_MINUTES"); ok {
		m.config.HLS.RetentionMinutes = val
	}
}

func (m *Manager) applyHLSConcurrencyOverrides() {
	if val, ok := envGetInt("HLS_CONCURRENT_LIMIT", "HLS_MAX_CONCURRENT_JOBS"); ok {
		m.config.HLS.ConcurrentLimit = val
	}
}

func (m *Manager) applyHLSQualityOverrides() {
	raw := envGetStr("HLS_QUALITIES")
	if raw == "" {
		return
	}
	nameSet := make(map[string]bool)
	for _, name := range strings.Split(raw, ",") {
		nameSet[strings.TrimSpace(name)] = true
	}
	filtered := make([]HLSQuality, 0, len(m.config.HLS.QualityProfiles))
	for _, p := range m.config.HLS.QualityProfiles {
		if nameSet[p.Name] {
			filtered = append(filtered, p)
		}
	}
	if len(filtered) > 0 {
		m.config.HLS.QualityProfiles = filtered
	}
}

func (m *Manager) applyHLSOptionsOverrides() {
	if val := envGetStr("HLS_CDN_BASE_URL"); val != "" {
		m.config.HLS.CDNBaseURL = strings.TrimRight(val, "/")
	}
	if val, ok := envGetBool("HLS_LAZY_TRANSCODE"); ok {
		m.config.HLS.LazyTranscode = val
	}
	if val, ok := envGetInt("HLS_MAX_CONSECUTIVE_FAILURES"); ok {
		m.config.HLS.MaxConsecutiveFailures = val
	}
	if val, ok := envGetDuration(time.Second, "HLS_PROBE_TIMEOUT_SECONDS"); ok {
		m.config.HLS.ProbeTimeout = val
	}
	if val, ok := envGetInt("HLS_PRE_GENERATE_INTERVAL_HOURS"); ok {
		m.config.HLS.PreGenerateIntervalHours = val
	}
	if val, ok := envGetDuration(time.Hour, "HLS_STALE_LOCK_THRESHOLD_HOURS"); ok {
		m.config.HLS.StaleLockThreshold = val
	}
}
