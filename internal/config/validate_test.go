package config

import (
	"testing"
)

// ---------------------------------------------------------------------------
// Validation — individual sections
// ---------------------------------------------------------------------------

func newTestManager() *Manager {
	return NewManager("/tmp/validate-test-config.json")
}

func TestValidate_DefaultConfig_HasDatabaseError(t *testing.T) {
	// Default config has database enabled with password="" which is valid;
	// but the overall config should be valid with defaults
	m := newTestManager()
	errs := m.Validate()
	// With defaults, database host/name/port are set, so no errors expected
	for _, e := range errs {
		t.Errorf("unexpected validation error: %v", e)
	}
}

func TestValidate_InvalidServerPort(t *testing.T) {
	m := newTestManager()
	m.config.Server.Port = 0
	errs := m.Validate()
	found := false
	for _, e := range errs {
		if e.Error() == "invalid server port: 0" {
			found = true
		}
	}
	if !found {
		t.Error("expected 'invalid server port' error for port 0")
	}
}

func TestValidate_ServerPort_HighBound(t *testing.T) {
	m := newTestManager()
	m.config.Server.Port = 65536
	errs := m.Validate()
	found := false
	for _, e := range errs {
		if e != nil {
			found = true
		}
	}
	if !found {
		t.Error("expected error for port 65536")
	}
}

func TestValidate_ValidServerPort(t *testing.T) {
	m := newTestManager()
	m.config.Server.Port = 8080
	errs := m.validateServer()
	if len(errs) > 0 {
		t.Errorf("unexpected errors for valid port 8080: %v", errs)
	}
}

func TestValidate_HTTPS_NoCert(t *testing.T) {
	m := newTestManager()
	m.config.Server.EnableHTTPS = true
	m.config.Server.CertFile = ""
	m.config.Server.KeyFile = ""
	errs := m.validateServerHTTPS()
	if len(errs) != 2 {
		t.Errorf("expected 2 errors for HTTPS without cert/key, got %d", len(errs))
	}
}

func TestValidate_HTTPS_Disabled(t *testing.T) {
	m := newTestManager()
	m.config.Server.EnableHTTPS = false
	errs := m.validateServerHTTPS()
	if len(errs) != 0 {
		t.Errorf("expected no errors when HTTPS disabled, got %d", len(errs))
	}
}

func TestValidate_StreamingChunkSize_TooSmall(t *testing.T) {
	m := newTestManager()
	m.config.Streaming.DefaultChunkSize = 512
	errs := m.validateStreaming()
	if len(errs) == 0 {
		t.Error("expected error for chunk size < 1024")
	}
}

func TestValidate_StreamingChunkSize_Valid(t *testing.T) {
	m := newTestManager()
	m.config.Streaming.DefaultChunkSize = 1024
	errs := m.validateStreaming()
	if len(errs) > 0 {
		t.Errorf("unexpected error for valid chunk size: %v", errs)
	}
}

func TestValidate_Security_RateLimit(t *testing.T) {
	m := newTestManager()
	m.config.Security.RateLimitEnabled = true
	m.config.Security.RateLimitRequests = 0
	errs := m.validateSecurity()
	if len(errs) == 0 {
		t.Error("expected error for rate_limit_requests=0 when enabled")
	}
}

func TestValidate_Security_RateLimit_Disabled(t *testing.T) {
	m := newTestManager()
	m.config.Security.RateLimitEnabled = false
	m.config.Security.RateLimitRequests = 0
	errs := m.validateSecurity()
	if len(errs) > 0 {
		t.Errorf("no error expected when rate limit disabled: %v", errs)
	}
}

func TestValidate_HLS_SegmentDuration_Invalid(t *testing.T) {
	m := newTestManager()
	m.config.HLS.Enabled = true
	m.config.HLS.SegmentDuration = 0
	errs := m.validateHLS()
	if len(errs) == 0 {
		t.Error("expected error for HLS segment duration 0")
	}
}

func TestValidate_HLS_SegmentDuration_TooLarge(t *testing.T) {
	m := newTestManager()
	m.config.HLS.Enabled = true
	m.config.HLS.SegmentDuration = 61
	errs := m.validateHLS()
	if len(errs) == 0 {
		t.Error("expected error for HLS segment duration 61")
	}
}

func TestValidate_HLS_Disabled(t *testing.T) {
	m := newTestManager()
	m.config.HLS.Enabled = false
	m.config.HLS.SegmentDuration = 0
	errs := m.validateHLS()
	if len(errs) > 0 {
		t.Errorf("no error expected when HLS disabled: %v", errs)
	}
}

func TestValidate_Database_Disabled(t *testing.T) {
	m := newTestManager()
	m.config.Database.Enabled = false
	errs := m.validateDatabase()
	if len(errs) == 0 {
		t.Error("expected error when database disabled")
	}
}

func TestValidate_Database_MissingHost(t *testing.T) {
	m := newTestManager()
	m.config.Database.Host = ""
	errs := m.validateDatabase()
	found := false
	for _, e := range errs {
		if e.Error() == "database host is required" {
			found = true
		}
	}
	if !found {
		t.Error("expected 'database host is required' error")
	}
}

func TestValidate_Database_MissingName(t *testing.T) {
	m := newTestManager()
	m.config.Database.Name = ""
	errs := m.validateDatabase()
	found := false
	for _, e := range errs {
		if e.Error() == "database name is required" {
			found = true
		}
	}
	if !found {
		t.Error("expected 'database name is required' error")
	}
}

func TestValidate_Database_InvalidPort(t *testing.T) {
	m := newTestManager()
	m.config.Database.Port = 0
	errs := m.validateDatabase()
	found := false
	for _, e := range errs {
		if e != nil {
			found = true
		}
	}
	if !found {
		t.Error("expected error for invalid database port 0")
	}
}

func TestValidate_Auth_NegativeLockout(t *testing.T) {
	m := newTestManager()
	m.config.Auth.Enabled = true
	m.config.Auth.LockoutDuration = -1
	errs := m.validateAuth()
	if len(errs) == 0 {
		t.Error("expected error for negative lockout duration")
	}
}

func TestValidate_Auth_NegativeMaxAttempts(t *testing.T) {
	m := newTestManager()
	m.config.Auth.Enabled = true
	m.config.Auth.MaxLoginAttempts = -1
	errs := m.validateAuth()
	if len(errs) == 0 {
		t.Error("expected error for negative max login attempts")
	}
}

func TestValidate_Auth_Disabled(t *testing.T) {
	m := newTestManager()
	m.config.Auth.Enabled = false
	m.config.Auth.MaxLoginAttempts = -99
	errs := m.validateAuth()
	if len(errs) > 0 {
		t.Errorf("no error expected when auth disabled: %v", errs)
	}
}

func TestValidate_Thumbnails_InvalidSize(t *testing.T) {
	m := newTestManager()
	m.config.Thumbnails.Enabled = true
	m.config.Thumbnails.Width = 0
	m.config.Thumbnails.Height = 0
	errs := m.validateThumbnails()
	if len(errs) == 0 {
		t.Error("expected error for zero thumbnail dimensions")
	}
}

func TestValidate_Thumbnails_NegativeWorkers(t *testing.T) {
	m := newTestManager()
	m.config.Thumbnails.Enabled = true
	m.config.Thumbnails.Width = 320
	m.config.Thumbnails.Height = 180
	m.config.Thumbnails.WorkerCount = -1
	errs := m.validateThumbnails()
	if len(errs) == 0 {
		t.Error("expected error for negative worker count")
	}
}

func TestValidate_Analytics_NegativeRetention(t *testing.T) {
	m := newTestManager()
	m.config.Analytics.Enabled = true
	m.config.Analytics.RetentionDays = -1
	errs := m.validateAnalytics()
	if len(errs) == 0 {
		t.Error("expected error for negative retention days")
	}
}

func TestValidate_Uploads_ZeroFileSize(t *testing.T) {
	m := newTestManager()
	m.config.Uploads.Enabled = true
	m.config.Uploads.MaxFileSize = 0
	errs := m.validateUploads()
	if len(errs) == 0 {
		t.Error("expected error for zero max file size when uploads enabled")
	}
}

func TestValidate_Backup_NegativeRetention(t *testing.T) {
	m := newTestManager()
	m.config.Backup.RetentionCount = -1
	errs := m.validateBackup()
	if len(errs) == 0 {
		t.Error("expected error for negative backup retention count")
	}
}

func TestValidate_Backup_Valid(t *testing.T) {
	m := newTestManager()
	m.config.Backup.RetentionCount = 5
	errs := m.validateBackup()
	if len(errs) > 0 {
		t.Errorf("unexpected error: %v", errs)
	}
}
