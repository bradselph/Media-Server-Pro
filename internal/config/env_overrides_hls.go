package config

import (
	"strings"
	"time"
)

func (m *Manager) applyHLSEnvOverrides() {
	m.applyHLSBaseOverridesCore()
	m.applyHLSCleanupOverrides()
	// 0 = auto (scale with CPU / GPU sessions, see hls.EffectiveConcurrentLimit).
	if val, ok := envGetInt("HLS_CONCURRENT_LIMIT", "HLS_MAX_CONCURRENT_JOBS"); ok && val >= 0 {
		m.config.HLS.ConcurrentLimit = val
	}
	m.applyHLSQualityOverrides()
	m.applyHLSOptionsOverrides()
}

func (m *Manager) applyHLSBaseOverridesCore() {
	if val, ok := envGetBool("HLS_ENABLED"); ok {
		m.config.HLS.Enabled = val
	}
	if val, ok := envGetInt("HLS_SEGMENT_DURATION"); ok && val >= 1 {
		m.config.HLS.SegmentDuration = val
	}
	if val, ok := envGetInt("HLS_PLAYLIST_LENGTH"); ok && val >= 1 {
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
	if val, ok := envGetDuration(time.Minute, "HLS_CLEANUP_INTERVAL_MINUTES"); ok && val >= time.Minute {
		m.config.HLS.CleanupInterval = val
	}
	if val, ok := envGetInt("HLS_RETENTION_MINUTES"); ok && val >= 1 {
		m.config.HLS.RetentionMinutes = val
	}
}

// applyHLSQualityOverrides enables exactly the quality profiles named in
// HLS_QUALITIES (comma-separated) and disables the rest. Profiles are toggled,
// not removed, so a deploy that narrows the ladder can be reversed later from
// the admin UI or a new value. A value naming no existing profile is ignored
// rather than disabling every profile.
func (m *Manager) applyHLSQualityOverrides() {
	raw := envGetStr("HLS_QUALITIES")
	if raw == "" {
		return
	}
	nameSet := make(map[string]bool)
	for name := range strings.SplitSeq(raw, ",") {
		nameSet[strings.TrimSpace(name)] = true
	}
	matched := false
	for _, p := range m.config.HLS.QualityProfiles {
		if nameSet[p.Name] {
			matched = true
			break
		}
	}
	if !matched {
		m.log.Warn("HLS_QUALITIES %q names no configured quality profile, ignoring", raw)
		return
	}
	for i := range m.config.HLS.QualityProfiles {
		m.config.HLS.QualityProfiles[i].Enabled = nameSet[m.config.HLS.QualityProfiles[i].Name]
	}
}

func (m *Manager) applyHLSOptionsOverrides() {
	if val := envGetStr("HLS_CDN_BASE_URL"); val != "" {
		m.config.HLS.CDNBaseURL = strings.TrimRight(val, "/")
	}
	if val := envGetStr("HLS_HARDWARE_ACCEL"); val != "" {
		m.config.HLS.HardwareAccel = strings.ToLower(strings.TrimSpace(val))
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
