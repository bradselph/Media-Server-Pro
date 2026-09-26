package config

import "testing"

// ---------------------------------------------------------------------------
// C23: hls.enabled=true with zero enabled quality profiles must be rejected —
// otherwise GenerateHLS resolves an empty ladder and produces a "completed"
// job whose master.m3u8 has no variants at all (see internal/hls.GenerateHLS's
// companion defense-in-depth check for the same condition).
// ---------------------------------------------------------------------------

func TestValidate_HLS_NoEnabledQualityProfiles_Errors(t *testing.T) {
	m := newTestManager()
	m.config.HLS.Enabled = true
	for i := range m.config.HLS.QualityProfiles {
		m.config.HLS.QualityProfiles[i].Enabled = false
	}
	errs := m.validateHLS()
	if len(errs) == 0 {
		t.Fatal("expected an error when HLS is enabled with zero enabled quality profiles")
	}
}

func TestValidate_HLS_NoQualityProfilesAtAll_Errors(t *testing.T) {
	m := newTestManager()
	m.config.HLS.Enabled = true
	m.config.HLS.QualityProfiles = nil
	errs := m.validateHLS()
	if len(errs) == 0 {
		t.Fatal("expected an error when HLS is enabled with no quality profiles configured at all")
	}
}

func TestValidate_HLS_DisabledWithNoEnabledProfiles_NoError(t *testing.T) {
	m := newTestManager()
	m.config.HLS.Enabled = false
	for i := range m.config.HLS.QualityProfiles {
		m.config.HLS.QualityProfiles[i].Enabled = false
	}
	errs := m.validateHLS()
	if len(errs) > 0 {
		t.Errorf("no error expected when HLS is disabled, even with zero enabled profiles: %v", errs)
	}
}

func TestValidate_HLS_AtLeastOneEnabledProfile_NoError(t *testing.T) {
	m := newTestManager()
	m.config.HLS.Enabled = true
	// Leave the default quality profiles (all enabled) in place.
	errs := m.validateHLS()
	for _, e := range errs {
		t.Errorf("unexpected validation error with default (all-enabled) quality profiles: %v", e)
	}
}

// TestValidate_DefaultConfig_QualityLadderIsValid guards against this check
// ever regressing the shipped default config (all four default profiles are
// enabled — see defaultHLSConfig).
func TestValidate_DefaultConfig_QualityLadderIsValid(t *testing.T) {
	m := newTestManager()
	errs := m.Validate()
	for _, e := range errs {
		t.Errorf("unexpected validation error on default config: %v", e)
	}
}
