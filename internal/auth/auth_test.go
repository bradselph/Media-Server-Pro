package auth

import (
	"testing"
)

// ---------------------------------------------------------------------------
// Error sentinels
// ---------------------------------------------------------------------------

func TestErrorSentinels_NonNil(t *testing.T) {
	errs := map[string]error{
		"ErrInvalidCredentials":    ErrInvalidCredentials,
		"ErrAccountDisabled":       ErrAccountDisabled,
		"ErrAccountLocked":         ErrAccountLocked,
		"ErrSessionExpired":        ErrSessionExpired,
		"ErrAdminWrongPassword":    ErrAdminWrongPassword,
		"ErrNotAdminUsername":      ErrNotAdminUsername,
		"ErrCannotDemoteLastAdmin": ErrCannotDemoteLastAdmin,
	}
	for name, err := range errs {
		if err == nil {
			t.Errorf("%s should not be nil", name)
		}
	}
}

func TestErrorSentinels_Distinct(t *testing.T) {
	errs := []error{
		ErrInvalidCredentials,
		ErrAccountDisabled,
		ErrAccountLocked,
		ErrSessionExpired,
		ErrAdminWrongPassword,
		ErrNotAdminUsername,
		ErrCannotDemoteLastAdmin,
	}
	seen := make(map[string]bool)
	for _, err := range errs {
		msg := err.Error()
		if seen[msg] {
			t.Errorf("duplicate error message: %q", msg)
		}
		seen[msg] = true
	}
}

// ---------------------------------------------------------------------------
// generateID
// ---------------------------------------------------------------------------

func TestGenerateID_NonEmpty(t *testing.T) {
	id := generateID()
	if id == "" {
		t.Error("generateID should return non-empty string")
	}
}

func TestGenerateID_Unique(t *testing.T) {
	id1 := generateID()
	id2 := generateID()
	if id1 == id2 {
		t.Error("two generated IDs should be different")
	}
}

func TestGenerateID_Length(t *testing.T) {
	id := generateID()
	// 16 bytes hex encoded = 32 chars
	if len(id) != 32 {
		t.Errorf("generateID length = %d, want 32", len(id))
	}
}

// ---------------------------------------------------------------------------
// generateSessionID
// ---------------------------------------------------------------------------

func TestGenerateSessionID_NonEmpty(t *testing.T) {
	sid := generateSessionID()
	if sid == "" {
		t.Error("generateSessionID should return non-empty string")
	}
}

func TestGenerateSessionID_Unique(t *testing.T) {
	s1 := generateSessionID()
	s2 := generateSessionID()
	if s1 == s2 {
		t.Error("two session IDs should be different")
	}
}

// ---------------------------------------------------------------------------
// generateSalt
// ---------------------------------------------------------------------------

func TestGenerateSalt_NonEmpty(t *testing.T) {
	salt := generateSalt()
	if salt == "" {
		t.Error("generateSalt should return non-empty string")
	}
}

func TestGenerateSalt_Unique(t *testing.T) {
	s1 := generateSalt()
	s2 := generateSalt()
	if s1 == s2 {
		t.Error("two salts should be different")
	}
}

// ---------------------------------------------------------------------------
// Module basics
// ---------------------------------------------------------------------------

func TestModuleName(t *testing.T) {
	m := &Module{}
	if m.Name() != "auth" {
		t.Errorf("Name() = %q, want %q", m.Name(), "auth")
	}
}
