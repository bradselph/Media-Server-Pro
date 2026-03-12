package config

import (
	"fmt"
	"time"
)

// Validate validates the current configuration and returns any blocking errors.
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
	errors = append(errors, m.validateAuth()...)
	errors = append(errors, m.validateThumbnails()...)
	errors = append(errors, m.validateAnalytics()...)
	errors = append(errors, m.validateReceiver()...)
	errors = append(errors, m.validateUploads()...)
	errors = append(errors, m.validateBackup()...)
	errors = append(errors, m.validateHuggingFace()...)
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

func (m *Manager) validateAuth() []error {
	if !m.config.Auth.Enabled {
		return nil
	}
	var errs []error
	if m.config.Auth.LockoutDuration < 0 {
		errs = append(errs, fmt.Errorf("auth lockout_duration cannot be negative"))
	}
	if m.config.Auth.MaxLoginAttempts < 0 {
		errs = append(errs, fmt.Errorf("auth max_login_attempts cannot be negative"))
	}
	if m.config.Auth.MaxLoginAttempts == 0 {
		m.log.Warn("auth max_login_attempts is 0 — lockout will not trigger")
	}
	return errs
}

func (m *Manager) validateThumbnails() []error {
	if !m.config.Thumbnails.Enabled {
		return nil
	}
	var errs []error
	if m.config.Thumbnails.Width < 1 || m.config.Thumbnails.Height < 1 {
		errs = append(errs, fmt.Errorf("thumbnails width and height must be positive when enabled, got %dx%d", m.config.Thumbnails.Width, m.config.Thumbnails.Height))
	}
	if m.config.Thumbnails.WorkerCount < 0 {
		errs = append(errs, fmt.Errorf("thumbnails worker_count cannot be negative"))
	}
	return errs
}

func (m *Manager) validateAnalytics() []error {
	if !m.config.Analytics.Enabled {
		return nil
	}
	if m.config.Analytics.RetentionDays < 0 {
		return []error{fmt.Errorf("analytics retention_days cannot be negative")}
	}
	return nil
}

func (m *Manager) validateReceiver() []error {
	if !m.config.Receiver.Enabled {
		return nil
	}
	if len(m.config.Receiver.APIKeys) == 0 {
		m.log.Warn("receiver enabled but no api_keys configured — slave connections will be rejected")
	}
	return nil
}

func (m *Manager) validateUploads() []error {
	if !m.config.Uploads.Enabled {
		return nil
	}
	if m.config.Uploads.MaxFileSize < 1 {
		return []error{fmt.Errorf("uploads max_file_size must be positive when uploads enabled")}
	}
	return nil
}

func (m *Manager) validateBackup() []error {
	if m.config.Backup.RetentionCount < 0 {
		return []error{fmt.Errorf("backup retention_count cannot be negative")}
	}
	return nil
}

func (m *Manager) validateHuggingFace() []error {
	if !m.config.HuggingFace.Enabled {
		return nil
	}
	if m.config.HuggingFace.APIKey == "" {
		m.log.Warn("huggingface enabled but api_key is empty — classification requests will fail")
	}
	return nil
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
