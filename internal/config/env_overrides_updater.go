package config

import (
	"time"
)

func (m *Manager) applyUpdaterEnvOverrides() {
	if val := envGetStr("UPDATER_APP_DIR"); val != "" {
		m.config.Updater.AppDir = val
	}
	if val := envGetStr("UPDATER_DEPLOY_KEY_PATH"); val != "" {
		m.config.Updater.DeployKeyPath = val
	}
	if val := envGetStr("UPDATER_GITHUB_TOKEN"); val != "" {
		m.config.Updater.GitHubToken = val
	}
	if val := envGetStr("UPDATER_GITHUB_USERNAME"); val != "" {
		m.config.Updater.GitHubUsername = val
	}
	if val := envGetStr("UPDATER_BRANCH"); val != "" {
		m.config.Updater.Branch = val
	}
	if val := envGetStr("UPDATER_METHOD"); val != "" {
		m.config.Updater.UpdateMethod = val
	}
}

func (m *Manager) applyAgeGateEnvOverrides() {
	if val, ok := envGetBool("AGE_GATE_ENABLED"); ok {
		m.config.AgeGate.Enabled = val
	}
	if val := envGetStr("AGE_GATE_BYPASS_IPS"); val != "" {
		m.config.AgeGate.BypassIPs = splitTrimmed(val)
	}
	if val, ok := envGetDuration(time.Hour, "AGE_GATE_IP_VERIFY_TTL_HOURS"); ok {
		m.config.AgeGate.IPVerifyTTL = val
	}
	if val := envGetStr("AGE_GATE_COOKIE_NAME"); val != "" {
		m.config.AgeGate.CookieName = val
	}
	if val, ok := envGetInt("AGE_GATE_COOKIE_MAX_AGE"); ok {
		m.config.AgeGate.CookieMaxAge = val
	}
}
