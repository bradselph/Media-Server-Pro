package validator

import (
	"testing"

	"media-server-pro/internal/logger"
)

const testStatusFmt = "status = %q, want %q"

// ---------------------------------------------------------------------------
// Status constants
// ---------------------------------------------------------------------------

func TestStatusConstants(t *testing.T) {
	statuses := []ValidationStatus{StatusPending, StatusValidated, StatusNeedsFix, StatusFixed, StatusFailed, StatusUnsupported}
	seen := make(map[ValidationStatus]bool)
	for _, s := range statuses {
		if s == "" {
			t.Error("status constant should not be empty")
		}
		if seen[s] {
			t.Errorf("duplicate status constant: %s", s)
		}
		seen[s] = true
	}
}

// ---------------------------------------------------------------------------
// parseFormatFloat
// ---------------------------------------------------------------------------

func TestParseFormatFloat_Valid(t *testing.T) {
	log := logger.New("test")
	v, ok := parseFormatFloat("3.14", log, "test")
	if !ok {
		t.Error("should parse valid float")
	}
	if v < 3.13 || v > 3.15 {
		t.Errorf("parseFormatFloat = %f, want ~3.14", v)
	}
}

func TestParseFormatFloat_Empty(t *testing.T) {
	log := logger.New("test")
	_, ok := parseFormatFloat("", log, "test")
	if ok {
		t.Error("should return false for empty string")
	}
}

func TestParseFormatFloat_Invalid(t *testing.T) {
	log := logger.New("test")
	_, ok := parseFormatFloat("notanumber", log, "test")
	if ok {
		t.Error("should return false for invalid string")
	}
}

func TestParseFormatFloat_Integer(t *testing.T) {
	log := logger.New("test")
	v, ok := parseFormatFloat("42", log, "test")
	if !ok {
		t.Error("should parse integer as float")
	}
	if v != 42.0 {
		t.Errorf("parseFormatFloat = %f, want 42.0", v)
	}
}

// ---------------------------------------------------------------------------
// parseFormatInt
// ---------------------------------------------------------------------------

func TestParseFormatInt_Valid(t *testing.T) {
	log := logger.New("test")
	v, ok := parseFormatInt("12345", log, "test")
	if !ok {
		t.Error("should parse valid int")
	}
	if v != 12345 {
		t.Errorf("parseFormatInt = %d, want 12345", v)
	}
}

func TestParseFormatInt_Empty(t *testing.T) {
	log := logger.New("test")
	_, ok := parseFormatInt("", log, "test")
	if ok {
		t.Error("should return false for empty string")
	}
}

func TestParseFormatInt_Invalid(t *testing.T) {
	log := logger.New("test")
	_, ok := parseFormatInt("abc", log, "test")
	if ok {
		t.Error("should return false for invalid string")
	}
}

func TestParseFormatInt_Negative(t *testing.T) {
	log := logger.New("test")
	v, ok := parseFormatInt("-100", log, "test")
	if !ok {
		t.Error("should parse negative int")
	}
	if v != -100 {
		t.Errorf("parseFormatInt = %d, want -100", v)
	}
}

// ---------------------------------------------------------------------------
// setFinalStatus
// ---------------------------------------------------------------------------

func TestSetFinalStatus_WithIssues(t *testing.T) {
	r := &ValidationResult{Issues: []string{"codec unsupported"}}
	setFinalStatus(r)
	if r.Status != StatusNeedsFix {
		t.Errorf(testStatusFmt, r.Status, StatusNeedsFix)
	}
}

func TestSetFinalStatus_AllSupported(t *testing.T) {
	r := &ValidationResult{VideoSupported: true, AudioSupported: true}
	setFinalStatus(r)
	if r.Status != StatusValidated {
		t.Errorf(testStatusFmt, r.Status, StatusValidated)
	}
}

func TestSetFinalStatus_UnsupportedCodec(t *testing.T) {
	r := &ValidationResult{VideoSupported: false, AudioSupported: true}
	setFinalStatus(r)
	if r.Status != StatusUnsupported {
		t.Errorf(testStatusFmt, r.Status, StatusUnsupported)
	}
}

// ---------------------------------------------------------------------------
// Module basics
// ---------------------------------------------------------------------------

func TestModuleName(t *testing.T) {
	m := &Module{}
	if m.Name() != "validator" {
		t.Errorf("Name() = %q, want %q", m.Name(), "validator")
	}
}
