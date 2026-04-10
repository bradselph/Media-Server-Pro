package auth

import (
	"errors"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// hashToken
// ---------------------------------------------------------------------------

func TestHashToken_Deterministic(t *testing.T) {
	h1 := hashToken("my-secret-token")
	h2 := hashToken("my-secret-token")
	if h1 != h2 {
		t.Error("hashToken should be deterministic")
	}
}

func TestHashToken_DifferentInputs(t *testing.T) {
	h1 := hashToken("token-a")
	h2 := hashToken("token-b")
	if h1 == h2 {
		t.Error("different inputs should produce different hashes")
	}
}

func TestHashToken_Length(t *testing.T) {
	h := hashToken("test")
	if len(h) != 64 {
		t.Errorf("SHA-256 hex should be 64 chars, got %d", len(h))
	}
}

func TestHashToken_Empty(t *testing.T) {
	h := hashToken("")
	if h == "" {
		t.Error("hash of empty string should not be empty")
	}
	if len(h) != 64 {
		t.Errorf("length = %d, want 64", len(h))
	}
}

// ---------------------------------------------------------------------------
// IsSessionError
// ---------------------------------------------------------------------------

func TestIsSessionError_SessionNotFound(t *testing.T) {
	if !IsSessionError(ErrSessionNotFound) {
		t.Error("ErrSessionNotFound should be a session error")
	}
}

func TestIsSessionError_SessionExpired(t *testing.T) {
	if !IsSessionError(ErrSessionExpired) {
		t.Error("ErrSessionExpired should be a session error")
	}
}

func TestIsSessionError_OtherError(t *testing.T) {
	if IsSessionError(errors.New("database timeout")) {
		t.Error("random error should not be a session error")
	}
}

func TestIsSessionError_Nil(t *testing.T) {
	if IsSessionError(nil) {
		t.Error("nil error should not be a session error")
	}
}

func TestIsSessionError_Wrapped(t *testing.T) {
	wrapped := errors.Join(ErrSessionNotFound, errors.New("context"))
	if !IsSessionError(wrapped) {
		t.Error("wrapped ErrSessionNotFound should still be a session error")
	}
}

func TestIsSessionError_AccountDisabled(t *testing.T) {
	// ErrAccountDisabled should clear stale cookies — it must be a session error.
	if !IsSessionError(ErrAccountDisabled) {
		t.Error("ErrAccountDisabled should be a session error so stale cookies get cleared")
	}
}

func TestIsSessionError_AccountLocked(t *testing.T) {
	// ErrAccountLocked is rate-limit related, not a permanent session rejection.
	if IsSessionError(ErrAccountLocked) {
		t.Error("ErrAccountLocked should NOT be a session error — it is transient")
	}
}

// ---------------------------------------------------------------------------
// deleteExpiredFromMap
// ---------------------------------------------------------------------------

func TestDeleteExpiredFromMap_RemovesExpired(t *testing.T) {
	now := time.Now()
	m := map[string]time.Time{
		"expired":  now.Add(-1 * time.Hour),
		"valid":    now.Add(1 * time.Hour),
		"also_exp": now.Add(-5 * time.Minute),
	}
	n := deleteExpiredFromMap(m, now, func(t time.Time) time.Time { return t })
	if n != 2 {
		t.Errorf("deleted %d, want 2", n)
	}
	if len(m) != 1 {
		t.Errorf("remaining = %d, want 1", len(m))
	}
	if _, ok := m["valid"]; !ok {
		t.Error("valid entry should remain")
	}
}

func TestDeleteExpiredFromMap_NoneExpired(t *testing.T) {
	now := time.Now()
	m := map[string]time.Time{
		"a": now.Add(1 * time.Hour),
		"b": now.Add(2 * time.Hour),
	}
	n := deleteExpiredFromMap(m, now, func(t time.Time) time.Time { return t })
	if n != 0 {
		t.Errorf("deleted %d, want 0", n)
	}
}

func TestDeleteExpiredFromMap_EmptyMap(t *testing.T) {
	m := map[string]time.Time{}
	n := deleteExpiredFromMap(m, time.Now(), func(t time.Time) time.Time { return t })
	if n != 0 {
		t.Errorf("deleted %d from empty map", n)
	}
}

// ---------------------------------------------------------------------------
// generateRandomPassword
// ---------------------------------------------------------------------------

func TestGenerateRandomPassword_Length(t *testing.T) {
	pwd, err := generateRandomPassword(32)
	if err != nil {
		t.Fatalf("generateRandomPassword: %v", err)
	}
	if len(pwd) != 32 {
		t.Errorf("length = %d, want 32", len(pwd))
	}
}

func TestGenerateRandomPassword_Unique(t *testing.T) {
	p1, _ := generateRandomPassword(32)
	p2, _ := generateRandomPassword(32)
	if p1 == p2 {
		t.Error("two passwords should be different")
	}
}

// ---------------------------------------------------------------------------
// defaultUserPreferences
// ---------------------------------------------------------------------------

func TestDefaultUserPreferences(t *testing.T) {
	prefs := defaultUserPreferences()
	if prefs.Theme != "dark" {
		t.Errorf("Theme = %q, want dark", prefs.Theme)
	}
	if prefs.ViewMode != "grid" {
		t.Errorf("ViewMode = %q, want grid", prefs.ViewMode)
	}
	if prefs.PlaybackSpeed != 1.0 {
		t.Errorf("PlaybackSpeed = %f, want 1.0", prefs.PlaybackSpeed)
	}
	if prefs.Volume != 1.0 {
		t.Errorf("Volume = %f, want 1.0", prefs.Volume)
	}
	if !prefs.ResumePlayback {
		t.Error("ResumePlayback should be true")
	}
	if prefs.Language != "en" {
		t.Errorf("Language = %q, want en", prefs.Language)
	}
}

// ---------------------------------------------------------------------------
// adminPermissions
// ---------------------------------------------------------------------------

func TestAdminPermissions(t *testing.T) {
	perms := adminPermissions()
	if !perms.CanStream {
		t.Error("CanStream should be true")
	}
	if !perms.CanDownload {
		t.Error("CanDownload should be true")
	}
	if !perms.CanUpload {
		t.Error("CanUpload should be true")
	}
	if !perms.CanDelete {
		t.Error("CanDelete should be true")
	}
	if !perms.CanManage {
		t.Error("CanManage should be true")
	}
	if !perms.CanViewMature {
		t.Error("CanViewMature should be true")
	}
	if !perms.CanCreatePlaylists {
		t.Error("CanCreatePlaylists should be true")
	}
}
