package config

import (
	"net"
	"net/url"
	"strings"
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
	if val, ok := envGetDuration(time.Second, "RATE_LIMIT_WINDOW_SECONDS"); ok && val > 0 {
		m.config.Security.RateLimitWindow = val
	}
}

func (m *Manager) applySecurityBurstOverrides() {
	if val, ok := envGetInt("SECURITY_BURST_LIMIT"); ok {
		m.config.Security.BurstLimit = val
	}
	if val, ok := envGetDuration(time.Second, "SECURITY_BURST_WINDOW_SECONDS"); ok && val > 0 {
		m.config.Security.BurstWindow = val
	}
}

func (m *Manager) applySecurityBanOverrides() {
	if val, ok := envGetInt("SECURITY_VIOLATIONS_FOR_BAN"); ok {
		m.config.Security.ViolationsForBan = val
	}
	if val, ok := envGetDuration(time.Minute, "BAN_DURATION_MINUTES"); ok && val > 0 {
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
		origins := splitTrimmed(val)
		var valid []string
		for _, o := range origins {
			if u, err := url.Parse(o); err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != "" {
				valid = append(valid, o)
			} else {
				// Log invalid origin so operator is aware of misconfiguration
				if m.log != nil {
					m.log.Warn("CORS_ORIGINS: skipping invalid origin %q (must be http:// or https:// URL)", o)
				}
			}
		}
		if len(valid) > 0 {
			m.config.Security.CORSOrigins = valid
		}
		if len(valid) < len(origins) && m.log != nil {
			m.log.Warn("CORS_ORIGINS: %d invalid origins filtered out of %d total", len(origins)-len(valid), len(origins))
		}
	}
}

func (m *Manager) applySecurityCSPOverrides() {
	if val, ok := envGetBool("CSP_ENABLED"); ok {
		m.config.Security.CSPEnabled = val
	}
	if val, ok := envGetBool("HSTS_ENABLED"); ok {
		m.config.Security.HSTSEnabled = val
	}
	if val, ok := envGetInt("HSTS_MAX_AGE"); ok && val >= 0 {
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
		m.config.Security.IPWhitelist = filterValidIPs(splitTrimmed(val))
	}
	if val, ok := envGetBool("SECURITY_ENABLE_IP_BLACKLIST"); ok {
		m.config.Security.EnableIPBlacklist = val
	}
	if val := envGetStr("SECURITY_IP_BLACKLIST"); val != "" {
		m.config.Security.IPBlacklist = filterValidIPs(splitTrimmed(val))
	}
}

func (m *Manager) applySecurityUploadOverrides() {
	if val, ok := envGetInt("SECURITY_MAX_FILE_SIZE_MB"); ok && val > 0 {
		m.config.Security.MaxFileSizeMB = val
	}
}

func filterValidIPs(entries []string) []string {
	valid := make([]string, 0, len(entries))
	for _, e := range entries {
		if strings.Contains(e, "/") {
			if _, _, err := net.ParseCIDR(e); err == nil {
				valid = append(valid, e)
			}
		} else if net.ParseIP(e) != nil {
			valid = append(valid, e)
		}
	}
	return valid
}
