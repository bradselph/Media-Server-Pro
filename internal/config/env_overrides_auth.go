package config

import (
	"time"

	"golang.org/x/crypto/bcrypt"
)

func (m *Manager) applyAuthEnvOverrides() {
	if val, ok := envGetBool("AUTH_ENABLED", "MEDIA_SERVER_ENABLE_AUTH"); ok {
		m.config.Auth.Enabled = val
	}
	if val, ok := envGetBool("AUTH_ALLOW_GUESTS"); ok {
		m.config.Auth.AllowGuests = val
	}
	if val, ok := envGetDuration(time.Hour, "AUTH_SESSION_TIMEOUT_HOURS"); ok {
		m.config.Auth.SessionTimeout = val
	}
	if val, ok := envGetInt("AUTH_MAX_LOGIN_ATTEMPTS"); ok {
		m.config.Auth.MaxLoginAttempts = val
	}
	if val, ok := envGetDuration(time.Minute, "AUTH_LOCKOUT_DURATION_MINUTES"); ok {
		m.config.Auth.LockoutDuration = val
	}
	if val, ok := envGetBool("AUTH_SECURE_COOKIES"); ok {
		m.config.Auth.SecureCookies = val
	}
	if val := envGetStr("AUTH_DEFAULT_USER_TYPE"); val != "" {
		m.config.Auth.DefaultUserType = val
	}
}

func (m *Manager) applyAdminEnvOverrides() {
	if val, ok := envGetBool("ADMIN_ENABLED"); ok {
		m.config.Admin.Enabled = val
	}
	if val := envGetStr("ADMIN_USERNAME", "MEDIA_SERVER_ADMIN_USER"); val != "" {
		m.config.Admin.Username = val
	}
	m.applyAdminPasswordOverride()
	if val, ok := envGetDuration(time.Hour, "ADMIN_SESSION_TIMEOUT_HOURS"); ok {
		m.config.Admin.SessionTimeout = val
	}
}

func (m *Manager) applyAdminPasswordOverride() {
	if val := envGetStr("ADMIN_PASSWORD_HASH", "MEDIA_SERVER_ADMIN_PASSWORD_HASH"); val != "" {
		m.config.Admin.PasswordHash = val
		return
	}
	if val := envGetStr("ADMIN_PASSWORD"); val != "" {
		hash, err := bcrypt.GenerateFromPassword([]byte(val), bcrypt.DefaultCost)
		if err != nil {
			m.log.Error("Failed to hash ADMIN_PASSWORD: %v", err)
		} else {
			m.config.Admin.PasswordHash = string(hash)
			m.log.Info("Admin password set from ADMIN_PASSWORD environment variable")
		}
	}
}
