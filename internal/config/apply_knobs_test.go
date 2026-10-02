package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// writeSeededConfig writes a config.json that looks like production: already
// seeded, with both one-shot env migrations done, so Load ignores seed-only
// env vars and only ApplyDeployKnobs can change them.
func writeSeededConfig(t *testing.T, mutate func(*Config)) string {
	t.Helper()
	cfg := DefaultConfig()
	cfg.EnvSeedMigrated = true
	cfg.InfraOwnershipMigrated = true
	if mutate != nil {
		mutate(cfg)
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), testConfigFilename)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	// An (empty) record means knob tracking is already established, so the
	// apply step acts on changes instead of doing the first-run baseline.
	if err := writeKnobState(KnobStatePath(path), map[string]string{}); err != nil {
		t.Fatal(err)
	}
	return path
}

func loadConfig(t *testing.T, path string) *Config {
	t.Helper()
	m := NewManager(path)
	if err := m.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	return m.Get()
}

func TestKnownEnvKeys_Classification(t *testing.T) {
	byName := make(map[string]EnvKeyInfo)
	for _, k := range KnownEnvKeys() {
		if _, dup := byName[k.Name]; dup {
			t.Fatalf("%s reported twice", k.Name)
		}
		byName[k.Name] = k
	}
	for name, want := range map[string]string{
		"DATABASE_HOST":                EnvClassAlways,
		"VIDEOS_DIR":                   EnvClassAlways,
		"S3_BUCKET":                    EnvClassAlways,
		"ADMIN_PASSWORD_HASH":          EnvClassAlways,
		"SERVER_PORT":                  EnvClassSeed,
		"LOG_LEVEL":                    EnvClassSeed,
		"HLS_CONCURRENT_LIMIT":         EnvClassSeed,
		"RATE_LIMIT_REQUESTS":          EnvClassSeed,
		"FEATURE_HUB":                  EnvClassSeed,
		"SERVER_MEMORY_LIMIT_PERCENT":  EnvClassSeed,
		"SECURITY_TRUSTED_PROXY_CIDRS": EnvClassSeed,
	} {
		got, ok := byName[name]
		if !ok {
			t.Errorf("%s not reported", name)
			continue
		}
		if got.Class != want {
			t.Errorf("%s class = %s, want %s", name, got.Class, want)
		}
	}
	if a := byName["HLS_CONCURRENT_LIMIT"].Aliases; !slices.Contains(a, "HLS_MAX_CONCURRENT_JOBS") {
		t.Errorf("HLS_CONCURRENT_LIMIT aliases = %v, want HLS_MAX_CONCURRENT_JOBS", a)
	}
}

// KnownEnvKeys must observe the overrides without applying anything, and must
// restore the real environment afterwards.
func TestKnownEnvKeys_RestoresProcessEnv(t *testing.T) {
	t.Setenv("HLS_CONCURRENT_LIMIT", "7")
	_ = KnownEnvKeys()
	if got := envGetStr("HLS_CONCURRENT_LIMIT"); got != "7" {
		t.Fatalf("after KnownEnvKeys envGetStr = %q, want the process value 7", got)
	}
}

func TestApplyDeployKnobs_AppliesListedSeedKnobsOnly(t *testing.T) {
	path := writeSeededConfig(t, nil)
	t.Setenv("HLS_CONCURRENT_LIMIT", "6")
	t.Setenv("LOG_LEVEL", "debug")
	t.Setenv("RATE_LIMIT_REQUESTS", "777")
	t.Setenv("DATABASE_NAME", "msp_test") // always class: applied by Load, nothing to persist
	t.Setenv("GOGC", "150")               // process env
	t.Setenv("NOT_A_REAL_KNOB", "x")      // unknown
	t.Setenv("HLS_AUTO_GENERATE", "true") // seed-only, present but NOT listed: must not apply
	t.Setenv("HLS_SEGMENT_DURATION", "")  // listed but empty

	m := NewManager(path)
	res, err := m.ApplyDeployKnobs(ApplyKnobsOptions{Keys: []string{
		"HLS_CONCURRENT_LIMIT", "LOG_LEVEL", "RATE_LIMIT_REQUESTS", "DATABASE_NAME",
		"GOGC", "NOT_A_REAL_KNOB", "HLS_SEGMENT_DURATION", " LOG_LEVEL ", "bad-name",
	}})
	if err != nil {
		t.Fatalf("ApplyDeployKnobs: %v", err)
	}
	if want := []string{"HLS_CONCURRENT_LIMIT", "LOG_LEVEL", "RATE_LIMIT_REQUESTS"}; !slices.Equal(res.Applied, want) {
		t.Errorf("Applied = %v, want %v", res.Applied, want)
	}
	if !slices.Equal(res.Always, []string{"DATABASE_NAME"}) || !slices.Equal(res.Process, []string{"GOGC"}) ||
		!slices.Equal(res.Unknown, []string{"NOT_A_REAL_KNOB"}) || !slices.Equal(res.Empty, []string{"HLS_SEGMENT_DURATION"}) {
		t.Errorf("classification: always=%v process=%v unknown=%v empty=%v", res.Always, res.Process, res.Unknown, res.Empty)
	}
	if !res.Saved {
		t.Error("expected config.json to be rewritten")
	}

	cfg := loadConfig(t, path)
	if cfg.HLS.ConcurrentLimit != 6 || cfg.Logging.Level != "debug" || cfg.Security.RateLimitRequests != 777 {
		t.Errorf("applied values not persisted: concurrent=%d level=%q rate=%d", cfg.HLS.ConcurrentLimit, cfg.Logging.Level, cfg.Security.RateLimitRequests)
	}
	if cfg.HLS.AutoGenerate {
		t.Error("HLS_AUTO_GENERATE was in the environment but not listed — it must not be applied")
	}

	record, err := os.ReadFile(KnobStatePath(path))
	if err != nil {
		t.Fatalf("knob record not written: %v", err)
	}
	for _, secret := range []string{"777", "debug"} {
		if strings.Contains(string(record), secret) {
			t.Errorf("knob record contains a raw value %q; it must store hashes only", secret)
		}
	}
}

// The core guarantee: an unchanged knob never reverts an admin-UI edit, a
// changed knob is applied, and Force re-asserts everything.
func TestApplyDeployKnobs_OnlyChangedKnobsOverrideAdminEdits(t *testing.T) {
	path := writeSeededConfig(t, nil)
	t.Setenv("HLS_CONCURRENT_LIMIT", "6")
	keys := []string{"HLS_CONCURRENT_LIMIT"}
	if _, err := NewManager(path).ApplyDeployKnobs(ApplyKnobsOptions{Keys: keys}); err != nil {
		t.Fatal(err)
	}

	// Admin changes the setting in the UI.
	admin := NewManager(path)
	if err := admin.Load(); err != nil {
		t.Fatal(err)
	}
	if err := admin.Update(func(c *Config) { c.HLS.ConcurrentLimit = 3 }); err != nil {
		t.Fatal(err)
	}

	// Redeploy with the same .deploy.env value: the admin edit must survive.
	res, err := NewManager(path).ApplyDeployKnobs(ApplyKnobsOptions{Keys: keys})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(res.Unchanged, keys) || len(res.Applied) != 0 {
		t.Errorf("unchanged knob: applied=%v unchanged=%v", res.Applied, res.Unchanged)
	}
	if got := loadConfig(t, path).HLS.ConcurrentLimit; got != 3 {
		t.Fatalf("admin edit reverted by an unchanged knob: limit = %d, want 3", got)
	}

	// Operator changes the knob: it wins again.
	t.Setenv("HLS_CONCURRENT_LIMIT", "8")
	if _, err := NewManager(path).ApplyDeployKnobs(ApplyKnobsOptions{Keys: keys}); err != nil {
		t.Fatal(err)
	}
	if got := loadConfig(t, path).HLS.ConcurrentLimit; got != 8 {
		t.Fatalf("changed knob not applied: limit = %d, want 8", got)
	}

	// Force re-asserts an unchanged knob over a newer admin edit.
	admin2 := NewManager(path)
	if err := admin2.Load(); err != nil {
		t.Fatal(err)
	}
	if err := admin2.Update(func(c *Config) { c.HLS.ConcurrentLimit = 2 }); err != nil {
		t.Fatal(err)
	}
	if _, err := NewManager(path).ApplyDeployKnobs(ApplyKnobsOptions{Keys: keys, Force: true}); err != nil {
		t.Fatal(err)
	}
	if got := loadConfig(t, path).HLS.ConcurrentLimit; got != 8 {
		t.Fatalf("Force did not re-apply: limit = %d, want 8", got)
	}
}

func TestApplyDeployKnobs_InvalidValueLeavesConfigAndRecordUntouched(t *testing.T) {
	path := writeSeededConfig(t, nil)
	loadConfig(t, path) // settle one-shot migrations, as a normal start would
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	// Parses fine but fails validation (chunk size must be >= 1024 bytes).
	// (Out-of-range HLS scalars are repaired to defaults by normalizeHLSScalars
	// instead, exactly as Load does.)
	t.Setenv("STREAMING_CHUNK_SIZE", "100")
	res, err := NewManager(path).ApplyDeployKnobs(ApplyKnobsOptions{Keys: []string{"STREAMING_CHUNK_SIZE"}})
	if err == nil {
		t.Fatal("expected a validation error")
	}
	if res == nil || res.Saved {
		t.Fatalf("result = %+v, want not saved", res)
	}
	after, _ := os.ReadFile(path)
	if string(before) != string(after) {
		t.Error("config.json changed despite failed validation")
	}
	if st, _, _ := readKnobState(KnobStatePath(path)); len(st) != 0 {
		t.Error("knob record updated despite failed apply — the next deploy would not retry")
	}
}

func TestApplyDeployKnobs_DryRunWritesNothing(t *testing.T) {
	path := writeSeededConfig(t, nil)
	loadConfig(t, path) // settle one-shot migrations, as a normal start would
	before, _ := os.ReadFile(path)
	t.Setenv("LOG_LEVEL", "warn")
	res, err := NewManager(path).ApplyDeployKnobs(ApplyKnobsOptions{Keys: []string{"LOG_LEVEL"}, DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, c := range res.Changes {
		if c.Path == "logging.level" && c.New == `"warn"` {
			found = true
		}
	}
	if !found {
		t.Errorf("dry run should report logging.level -> warn, got %+v", res.Changes)
	}
	after, _ := os.ReadFile(path)
	if string(before) != string(after) || res.Saved {
		t.Error("dry run wrote config.json")
	}
	if st, _, _ := readKnobState(KnobStatePath(path)); len(st) != 0 {
		t.Error("dry run wrote the knob record")
	}
}

func TestApplyDeployKnobs_RedactsSecretsInChanges(t *testing.T) {
	path := writeSeededConfig(t, nil)
	t.Setenv("HUGGINGFACE_API_KEY", "hf_supersecret")
	res, err := NewManager(path).ApplyDeployKnobs(ApplyKnobsOptions{Keys: []string{"HUGGINGFACE_API_KEY"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Changes) == 0 {
		t.Fatal("expected a change")
	}
	for _, c := range res.Changes {
		if strings.Contains(c.Old+c.New, "supersecret") {
			t.Errorf("secret leaked in change report: %+v", c)
		}
	}
}

func TestApplyDeployKnobs_FeatureToggleWinsLikeLoad(t *testing.T) {
	path := writeSeededConfig(t, nil)
	t.Setenv("FEATURE_UPLOADS", "false")
	if _, err := NewManager(path).ApplyDeployKnobs(ApplyKnobsOptions{Keys: []string{"FEATURE_UPLOADS"}}); err != nil {
		t.Fatal(err)
	}
	cfg := loadConfig(t, path)
	if cfg.Features.EnableUploads || cfg.Uploads.Enabled {
		t.Errorf("FEATURE_UPLOADS=false not applied through syncFeatureToggles: feature=%v module=%v", cfg.Features.EnableUploads, cfg.Uploads.Enabled)
	}
}

func TestHLSQualities_TogglesInsteadOfDeleting(t *testing.T) {
	path := writeSeededConfig(t, nil)
	t.Setenv("HLS_QUALITIES", "720p, 480p")
	if _, err := NewManager(path).ApplyDeployKnobs(ApplyKnobsOptions{Keys: []string{"HLS_QUALITIES"}}); err != nil {
		t.Fatal(err)
	}
	cfg := loadConfig(t, path)
	if len(cfg.HLS.QualityProfiles) != 4 {
		t.Fatalf("profiles = %d, want all 4 kept", len(cfg.HLS.QualityProfiles))
	}
	for _, p := range cfg.HLS.QualityProfiles {
		want := p.Name == "720p" || p.Name == "480p"
		if p.Enabled != want {
			t.Errorf("%s enabled = %v, want %v", p.Name, p.Enabled, want)
		}
	}

	// A value naming no profile must not disable everything.
	t.Setenv("HLS_QUALITIES", "4k")
	if _, err := NewManager(path).ApplyDeployKnobs(ApplyKnobsOptions{Keys: []string{"HLS_QUALITIES"}}); err != nil {
		t.Fatal(err)
	}
	enabled := 0
	for _, p := range loadConfig(t, path).HLS.QualityProfiles {
		if p.Enabled {
			enabled++
		}
	}
	if enabled != 2 {
		t.Errorf("unknown profile name changed the ladder: %d enabled, want 2", enabled)
	}
}

func TestHLSConcurrentLimitZeroMeansAuto(t *testing.T) {
	path := writeSeededConfig(t, func(c *Config) { c.HLS.ConcurrentLimit = 5 })
	t.Setenv("HLS_CONCURRENT_LIMIT", "0")
	if _, err := NewManager(path).ApplyDeployKnobs(ApplyKnobsOptions{Keys: []string{"HLS_CONCURRENT_LIMIT"}}); err != nil {
		t.Fatal(err)
	}
	if got := loadConfig(t, path).HLS.ConcurrentLimit; got != 0 {
		t.Errorf("HLS_CONCURRENT_LIMIT=0 should select auto (0), got %d", got)
	}
}

func TestNewEnvVars_MemoryProxyHeaderTimeout(t *testing.T) {
	path := writeSeededConfig(t, nil)
	t.Setenv("SERVER_MEMORY_LIMIT_PERCENT", "85")
	t.Setenv("SECURITY_TRUSTED_PROXY_CIDRS", "10.0.0.0/8, bogus, 192.168.1.0/24")
	t.Setenv("SERVER_READ_HEADER_TIMEOUT", "20")
	keys := []string{"SERVER_MEMORY_LIMIT_PERCENT", "SECURITY_TRUSTED_PROXY_CIDRS", "SERVER_READ_HEADER_TIMEOUT"}
	if _, err := NewManager(path).ApplyDeployKnobs(ApplyKnobsOptions{Keys: keys}); err != nil {
		t.Fatal(err)
	}
	cfg := loadConfig(t, path)
	if cfg.Server.MemoryLimitPercent != 85 {
		t.Errorf("memory limit = %d, want 85", cfg.Server.MemoryLimitPercent)
	}
	if !slices.Equal(cfg.Security.TrustedProxyCIDRs, []string{"10.0.0.0/8", "192.168.1.0/24"}) {
		t.Errorf("trusted proxies = %v (invalid entries must be dropped)", cfg.Security.TrustedProxyCIDRs)
	}
	if cfg.Server.ReadHeaderTimeout.Seconds() != 20 {
		t.Errorf("read header timeout = %v, want 20s", cfg.Server.ReadHeaderTimeout)
	}

	// Out-of-range percent is ignored, not clamped silently.
	t.Setenv("SERVER_MEMORY_LIMIT_PERCENT", "99")
	if _, err := NewManager(path).ApplyDeployKnobs(ApplyKnobsOptions{Keys: keys[:1]}); err != nil {
		t.Fatal(err)
	}
	if got := loadConfig(t, path).Server.MemoryLimitPercent; got != 85 {
		t.Errorf("out-of-range percent applied: %d", got)
	}
}

// The first run on an install (no record yet) must not apply anything: it
// records the baseline and reports where .deploy.env differs from config.json,
// so upgrading never reverts admin-UI edits behind the operator's back.
func TestApplyDeployKnobs_FirstRunRecordsBaselineWithoutApplying(t *testing.T) {
	path := writeSeededConfig(t, func(c *Config) { c.HLS.ConcurrentLimit = 4 })
	if err := os.Remove(KnobStatePath(path)); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HLS_CONCURRENT_LIMIT", "6")
	keys := []string{"HLS_CONCURRENT_LIMIT"}

	res, err := NewManager(path).ApplyDeployKnobs(ApplyKnobsOptions{Keys: keys})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(res.Baselined, keys) || len(res.Applied) != 0 || res.Saved {
		t.Fatalf("baseline run: baselined=%v applied=%v saved=%v", res.Baselined, res.Applied, res.Saved)
	}
	if len(res.Changes) != 1 || res.Changes[0].Path != "hls.concurrent_limit" || res.Changes[0].Old != "4" || res.Changes[0].New != "6" {
		t.Errorf("baseline should report the pending difference, got %+v", res.Changes)
	}
	if got := loadConfig(t, path).HLS.ConcurrentLimit; got != 4 {
		t.Fatalf("baseline run changed config.json: limit = %d, want 4", got)
	}

	// Same value next deploy: unchanged, still not applied.
	res, err = NewManager(path).ApplyDeployKnobs(ApplyKnobsOptions{Keys: keys})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(res.Unchanged, keys) {
		t.Errorf("after baseline the same value should be unchanged, got %+v", res)
	}

	// The operator applies the difference deliberately.
	if _, err := NewManager(path).ApplyDeployKnobs(ApplyKnobsOptions{Keys: keys, Force: true}); err != nil {
		t.Fatal(err)
	}
	if got := loadConfig(t, path).HLS.ConcurrentLimit; got != 6 {
		t.Errorf("forced apply after baseline: limit = %d, want 6", got)
	}
}

func TestApplyDeployKnobs_BaselineDryRunWritesNothing(t *testing.T) {
	path := writeSeededConfig(t, nil)
	if err := os.Remove(KnobStatePath(path)); err != nil {
		t.Fatal(err)
	}
	t.Setenv("LOG_LEVEL", "warn")
	if _, err := NewManager(path).ApplyDeployKnobs(ApplyKnobsOptions{Keys: []string{"LOG_LEVEL"}, DryRun: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(KnobStatePath(path)); !os.IsNotExist(err) {
		t.Error("dry run created the knob record")
	}
}
