package config

import (
	"fmt"
	"time"
)

// TODO: Incomplete — Validate only checks 6 of 21 config sections. Many sections with
// critical constraints have no validation at all: Auth (negative LockoutDuration, zero
// MaxLoginAttempts), Thumbnails (zero or negative Width/Height/WorkerCount), Analytics
// (negative RetentionDays), Receiver (empty APIKeys when enabled), Uploads (zero
// MaxFileSize), Backup (zero or negative RetentionCount), and HuggingFace (enabled
// with empty APIKey). Invalid values in these sections pass silently and cause runtime
// failures. Should add validation for each section that has constraints.
//
// Validate validates the current configuration
func (m *Manager) Validate() []error {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var errors []error
	errors = append(errors, m.validateServer()...)
	errors = append(errors, m.validateStreaming()...)
	errors = append(errors, m.validateAdmin()...)
	errors = append(errors, m.validateSecurity()...)
	errors = append(errors, m.validateHLS()...)
	errors = append(errors, m.validateDatabase()...)
	m.warnCORS()
	return errors
}

func (m *Manager) validateServer() []error {
	var errs []error
	errs = append(errs, m.validateServerPort()...)
	m.validateServerTimeouts()
	errs = append(errs, m.validateServerHTTPS()...)
	return errs
}

func (m *Manager) validateServerPort() []error {
	if m.config.Server.Port >= 1 && m.config.Server.Port <= 65535 {
		return nil
	}
	return []error{fmt.Errorf("invalid server port: %d", m.config.Server.Port)}
}

func (m *Manager) validateServerTimeouts() {
	for _, t := range []struct {
		name string
		d    time.Duration
	}{
		{"ReadTimeout", m.config.Server.ReadTimeout},
		{"WriteTimeout", m.config.Server.WriteTimeout},
		{"IdleTimeout", m.config.Server.IdleTimeout},
	} {
		if t.d <= 0 {
			m.log.Warn("%s is %v, timeouts disabled - may cause resource exhaustion", t.name, t.d)
		}
	}
}

func (m *Manager) validateServerHTTPS() []error {
	if !m.config.Server.EnableHTTPS {
		return nil
	}
	var errs []error
	if m.config.Server.CertFile == "" {
		errs = append(errs, fmt.Errorf("HTTPS enabled but no cert_file specified"))
	}
	if m.config.Server.KeyFile == "" {
		errs = append(errs, fmt.Errorf("HTTPS enabled but no key_file specified"))
	}
	return errs
}

func (m *Manager) validateStreaming() []error {
	if m.config.Streaming.DefaultChunkSize < 1024 {
		return []error{fmt.Errorf("chunk_size too small: %d", m.config.Streaming.DefaultChunkSize)}
	}
	return nil
}

func (m *Manager) validateAdmin() []error {
	if !m.config.Admin.Enabled {
		return nil
	}
	if m.config.Admin.Username == "" {
		m.log.Warn("admin enabled but no username specified — admin login will fail until ADMIN_USERNAME is set")
	}
	if m.config.Admin.PasswordHash == "" {
		m.log.Warn("admin enabled but no password hash — admin login will fail until ADMIN_PASSWORD_HASH is set")
	}
	return nil
}

func (m *Manager) validateSecurity() []error {
	if m.config.Security.RateLimitEnabled && m.config.Security.RateLimitRequests < 1 {
		return []error{fmt.Errorf("rate_limit_requests must be positive")}
	}
	return nil
}

func (m *Manager) validateHLS() []error {
	if !m.config.HLS.Enabled {
		return nil
	}
	if m.config.HLS.SegmentDuration < 1 || m.config.HLS.SegmentDuration > 60 {
		return []error{fmt.Errorf("hls segment_duration must be between 1 and 60 seconds, got: %d", m.config.HLS.SegmentDuration)}
	}
	return nil
}

func (m *Manager) validateDatabase() []error {
	if !m.config.Database.Enabled {
		return []error{fmt.Errorf("database must be enabled: set DATABASE_ENABLED=true")}
	}
	var errs []error
	if m.config.Database.Host == "" {
		errs = append(errs, fmt.Errorf("database host is required"))
	}
	if m.config.Database.Port < 1 || m.config.Database.Port > 65535 {
		errs = append(errs, fmt.Errorf("invalid database port: %d", m.config.Database.Port))
	}
	if m.config.Database.Name == "" {
		errs = append(errs, fmt.Errorf("database name is required"))
	}
	return errs
}

func (m *Manager) warnCORS() {
	if !m.config.Security.CORSEnabled || !m.config.Auth.Enabled {
		return
	}
	for _, origin := range m.config.Security.CORSOrigins {
		if origin == "*" {
			m.log.Warn("SECURITY: Wildcard CORS origin (*) is active with authentication enabled.")
			return
		}
	}
}
