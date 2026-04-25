package config

import (
	"fmt"
	"os"
	"strings"
	"time"
)

// Validate validates the current configuration and returns any blocking errors.
func (m *Manager) Validate() []error {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.validateLocked()
}

// validateLocked runs all sub-validators without acquiring the lock.
// Called by both Validate() (which holds RLock) and the private validate()
// called during Load() (which holds the write lock). This eliminates the
// former M-04 drift where the two paths had different coverage.
func (m *Manager) validateLocked() []error {
	var errors []error //nolint:prealloc // capacity unknown at compile time
	errors = append(errors, m.validateServer()...)
	errors = append(errors, m.validateStreaming()...)
	m.validateAdmin()
	errors = append(errors, m.validateSecurity()...)
	errors = append(errors, m.validateHLS()...)
	errors = append(errors, m.validateDatabase()...)
	errors = append(errors, m.validateAuth()...)
	errors = append(errors, m.validateThumbnails()...)
	errors = append(errors, m.validateAnalytics()...)
	m.validateReceiver()
	errors = append(errors, m.validateUploads()...)
	errors = append(errors, m.validateBackup()...)
	m.validateHuggingFace()
	errors = append(errors, m.validateExtractor()...)
	errors = append(errors, m.validateCrawler()...)
	errors = append(errors, m.validateStorage()...)
	errors = append(errors, m.validateClaude()...)
	m.warnCORS()
	return errors
}

func (m *Manager) validateServer() []error {
	var errs []error //nolint:prealloc // capacity unknown at compile time
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
	} else if _, err := os.Stat(m.config.Server.CertFile); err != nil {
		errs = append(errs, fmt.Errorf("cert_file not readable: %w", err))
	}
	if m.config.Server.KeyFile == "" {
		errs = append(errs, fmt.Errorf("HTTPS enabled but no key_file specified"))
	} else if _, err := os.Stat(m.config.Server.KeyFile); err != nil {
		errs = append(errs, fmt.Errorf("key_file not readable: %w", err))
	}
	return errs
}

func (m *Manager) validateStreaming() []error {
	if m.config.Streaming.DefaultChunkSize < 1024 {
		return []error{fmt.Errorf("chunk_size too small: %d", m.config.Streaming.DefaultChunkSize)}
	}
	return nil
}

func (m *Manager) validateAdmin() {
	if !m.config.Admin.Enabled {
		return
	}
	if m.config.Admin.Username == "" {
		m.log.Warn("admin enabled but no username specified — admin login will fail until ADMIN_USERNAME is set")
	}
	if m.config.Admin.PasswordHash == "" {
		m.log.Warn("admin enabled but no password hash — admin login will fail until ADMIN_PASSWORD_HASH is set")
	}
}

func (m *Manager) validateSecurity() []error {
	sec := m.config.Security
	if !sec.RateLimitEnabled {
		return nil
	}
	var errs []error
	if sec.RateLimitRequests < 1 {
		errs = append(errs, fmt.Errorf("rate_limit_requests must be positive"))
	}
	if sec.BurstLimit < 1 {
		errs = append(errs, fmt.Errorf("burst_limit must be positive when rate limiting is enabled"))
	}
	if sec.BurstWindow <= 0 {
		errs = append(errs, fmt.Errorf("burst_window must be positive when rate limiting is enabled"))
	}
	if sec.BanDuration <= 0 {
		errs = append(errs, fmt.Errorf("ban_duration must be positive when rate limiting is enabled"))
	}
	if sec.ViolationsForBan < 1 {
		errs = append(errs, fmt.Errorf("violations_for_ban must be positive when rate limiting is enabled"))
	}
	for _, dangerous := range []string{"default-src *", "script-src *", "unsafe-eval"} {
		if strings.Contains(sec.CSPPolicy, dangerous) {
			m.log.Warn("SECURITY: CSP policy contains dangerous directive %q — browser XSS protections may be weakened", dangerous)
		}
	}
	return errs
}

func (m *Manager) validateHLS() []error {
	if !m.config.HLS.Enabled {
		return nil
	}
	var errs []error
	hls := m.config.HLS
	if hls.SegmentDuration < 1 || hls.SegmentDuration > 60 {
		errs = append(errs, fmt.Errorf("hls segment_duration must be between 1 and 60 seconds, got: %d", hls.SegmentDuration))
	}
	// FND-1071: enforce positive bounds on numeric runtime knobs.
	if hls.PlaylistLength < 1 {
		errs = append(errs, fmt.Errorf("hls playlist_length must be at least 1, got: %d", hls.PlaylistLength))
	}
	if hls.ConcurrentLimit < 1 {
		errs = append(errs, fmt.Errorf("hls concurrent_limit must be at least 1, got: %d", hls.ConcurrentLimit))
	}
	if hls.ProbeTimeout <= 0 {
		errs = append(errs, fmt.Errorf("hls probe_timeout must be positive, got: %v", hls.ProbeTimeout))
	}
	// FND-1072: cleanup intervals must have a sane lower bound to avoid tight loops.
	if hls.CleanupEnabled {
		if hls.CleanupInterval < time.Minute {
			errs = append(errs, fmt.Errorf("hls cleanup_interval must be at least 1 minute when cleanup is enabled, got: %v", hls.CleanupInterval))
		}
		if hls.RetentionMinutes < 1 {
			errs = append(errs, fmt.Errorf("hls retention_minutes must be at least 1 when cleanup is enabled, got: %d", hls.RetentionMinutes))
		}
	}
	if hls.StaleLockThreshold < time.Minute {
		errs = append(errs, fmt.Errorf("hls stale_lock_threshold must be at least 1 minute, got: %v", hls.StaleLockThreshold))
	}
	return errs
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
	if m.config.Database.Username == "" {
		errs = append(errs, fmt.Errorf("database username is required"))
	}
	return errs
}

func (m *Manager) validateStorage() []error {
	if m.config.Storage.Backend != "s3" {
		return nil
	}
	var errs []error
	s3 := m.config.Storage.S3
	if s3.Endpoint == "" {
		errs = append(errs, fmt.Errorf("storage.s3.endpoint is required when backend is s3"))
	}
	if s3.Bucket == "" {
		errs = append(errs, fmt.Errorf("storage.s3.bucket is required when backend is s3"))
	}
	if s3.AccessKeyID == "" {
		errs = append(errs, fmt.Errorf("storage.s3.access_key_id is required when backend is s3"))
	}
	if s3.SecretAccessKey == "" {
		errs = append(errs, fmt.Errorf("storage.s3.secret_access_key is required when backend is s3"))
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
	if m.config.Auth.LockoutDuration == 0 {
		m.log.Warn("auth lockout_duration is 0 — lockout expires immediately, brute-force protection is ineffective")
	}
	if m.config.Auth.DefaultUserType != "" {
		found := false
		for _, ut := range m.config.Auth.UserTypes {
			if ut.Name == m.config.Auth.DefaultUserType {
				found = true
				break
			}
		}
		if !found {
			errs = append(errs, fmt.Errorf("auth default_user_type %q does not match any configured UserType", m.config.Auth.DefaultUserType))
		}
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

func (m *Manager) validateReceiver() {
	if !m.config.Receiver.Enabled {
		return
	}
	if len(m.config.Receiver.APIKeys) == 0 {
		m.log.Warn("receiver enabled but no api_keys configured — slave connections will be rejected")
	}
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

func (m *Manager) validateHuggingFace() {
	if !m.config.HuggingFace.Enabled {
		return
	}
	if m.config.HuggingFace.APIKey == "" {
		m.log.Warn("huggingface enabled but api_key is empty — classification requests will fail")
	}
	if strings.TrimSpace(m.config.HuggingFace.Model) == "" {
		m.log.Warn("huggingface enabled but model is empty — set HUGGINGFACE_MODEL to an image-classification or image-to-text model (e.g. Falconsai/nsfw_image_detection)")
	}
}

func (m *Manager) validateExtractor() []error {
	if !m.config.Extractor.Enabled {
		return nil
	}
	var errs []error
	if m.config.Extractor.MaxItems < 0 {
		errs = append(errs, fmt.Errorf("extractor max_items cannot be negative"))
	}
	if m.config.Extractor.ProxyTimeout < 0 {
		errs = append(errs, fmt.Errorf("extractor proxy_timeout cannot be negative"))
	}
	return errs
}

func (m *Manager) validateCrawler() []error {
	if !m.config.Crawler.Enabled {
		return nil
	}
	var errs []error
	if m.config.Crawler.MaxPages < 0 {
		errs = append(errs, fmt.Errorf("crawler max_pages cannot be negative"))
	}
	if m.config.Crawler.CrawlTimeout < 0 {
		errs = append(errs, fmt.Errorf("crawler crawl_timeout cannot be negative"))
	}
	return errs
}

func (m *Manager) validateClaude() []error {
	if !m.config.Claude.Enabled {
		return nil
	}
	var errs []error
	switch m.config.Claude.Mode {
	case "advisory", "interactive", "autonomous":
		// valid
	default:
		errs = append(errs, fmt.Errorf("claude.mode must be one of advisory/interactive/autonomous, got: %q", m.config.Claude.Mode))
	}
	if m.config.Claude.MaxTokens < 1 {
		errs = append(errs, fmt.Errorf("claude.max_tokens must be positive"))
	}
	if m.config.Claude.RequestTimeout <= 0 {
		errs = append(errs, fmt.Errorf("claude.request_timeout must be positive"))
	}
	if m.config.Claude.MaxToolCallsPerTurn < 1 {
		errs = append(errs, fmt.Errorf("claude.max_tool_calls_per_turn must be positive"))
	}
	if m.config.Claude.HistoryRetentionDays < 0 {
		errs = append(errs, fmt.Errorf("claude.history_retention_days cannot be negative"))
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
