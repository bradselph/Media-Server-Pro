package config

import (
	"time"
)

func (m *Manager) applySecurityEnvOverrides() {
	m.applySecurityRateLimitOverrides()
	m.applySecurityCORSOverrides()
	m.applySecurityCSPOverrides()
	m.applySecurityIPOverrides()
	m.applySecurityUploadOverrides()
}

func (m *Manager) applySecurityRateLimitOverrides() {
	m.applySecurityRateLimitCoreOverrides()
	m.applySecurityBurstOverrides()
	m.applySecurityBanOverrides()
	m.applySecurityAuthRateOverrides()
}

func (m *Manager) applySecurityRateLimitCoreOverrides() {
	if val, ok := envGetBool("RATE_LIMIT_ENABLED"); ok {
		m.config.Security.RateLimitEnabled = val
	}
	if val, ok := envGetInt("RATE_LIMIT_REQUESTS"); ok {
		m.config.Security.RateLimitRequests = val
	}
	if val, ok := envGetDuration(time.Second, "RATE_LIMIT_WINDOW_SECONDS"); ok {
		m.config.Security.RateLimitWindow = val
	}
}

func (m *Manager) applySecurityBurstOverrides() {
	if val, ok := envGetInt("SECURITY_BURST_LIMIT"); ok {
		m.config.Security.BurstLimit = val
	}
	if val, ok := envGetDuration(time.Second, "SECURITY_BURST_WINDOW_SECONDS"); ok {
		m.config.Security.BurstWindow = val
	}
}

func (m *Manager) applySecurityBanOverrides() {
	if val, ok := envGetInt("SECURITY_VIOLATIONS_FOR_BAN"); ok {
		m.config.Security.ViolationsForBan = val
	}
	if val, ok := envGetDuration(time.Minute, "BAN_DURATION_MINUTES"); ok {
		m.config.Security.BanDuration = val
	}
}

func (m *Manager) applySecurityAuthRateOverrides() {
	if val, ok := envGetInt("AUTH_RATE_LIMIT"); ok {
		m.config.Security.AuthRateLimit = val
	}
	if val, ok := envGetInt("AUTH_BURST_LIMIT"); ok {
		m.config.Security.AuthBurstLimit = val
	}
}

func (m *Manager) applySecurityCORSOverrides() {
	if val, ok := envGetBool("CORS_ENABLED"); ok {
		m.config.Security.CORSEnabled = val
	}
	if val := envGetStr("CORS_ORIGINS"); val != "" {
		m.config.Security.CORSOrigins = splitTrimmed(val, ",")
	}
}

func (m *Manager) applySecurityCSPOverrides() {
	if val, ok := envGetInt("HSTS_MAX_AGE"); ok {
		m.config.Security.HSTSMaxAge = val
	}
	if val := envGetStr("CSP_POLICY"); val != "" {
		m.config.Security.CSPPolicy = val
	}
}

func (m *Manager) applySecurityIPOverrides() {
	if val, ok := envGetBool("SECURITY_ENABLE_IP_WHITELIST"); ok {
		m.config.Security.EnableIPWhitelist = val
	}
	if val := envGetStr("SECURITY_IP_WHITELIST"); val != "" {
		m.config.Security.IPWhitelist = splitTrimmed(val, ",")
	}
	if val, ok := envGetBool("SECURITY_ENABLE_IP_BLACKLIST"); ok {
		m.config.Security.EnableIPBlacklist = val
	}
	if val := envGetStr("SECURITY_IP_BLACKLIST"); val != "" {
		m.config.Security.IPBlacklist = splitTrimmed(val, ",")
	}
}

func (m *Manager) applySecurityUploadOverrides() {
	if val, ok := envGetInt("SECURITY_MAX_FILE_SIZE_MB"); ok {
		m.config.Security.MaxFileSizeMB = val
	}
}
