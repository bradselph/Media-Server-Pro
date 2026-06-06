package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

const (
	testTmpConfigPath = "/tmp/test-config.json"
	testServerPortKey = "server.port"
)

// ---------------------------------------------------------------------------
// DefaultConfig
// ---------------------------------------------------------------------------

func TestDefaultConfig_ServerPort(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.Server.Port != 8080 {
		t.Errorf("default port = %d, want 8080", cfg.Server.Port)
	}
}

func TestDefaultConfig_DatabaseEnabled(t *testing.T) {
	cfg := DefaultConfig()
	if !cfg.Database.Enabled {
		t.Error("database should be enabled by default")
	}
}

func TestDefaultConfig_DatabaseHost(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.Database.Host != "localhost" {
		t.Errorf("default db host = %q, want localhost", cfg.Database.Host)
	}
	if cfg.Database.Port != 3306 {
		t.Errorf("default db port = %d, want 3306", cfg.Database.Port)
	}
}

func TestDefaultConfig_AuthEnabled(t *testing.T) {
	cfg := DefaultConfig()
	if !cfg.Auth.Enabled {
		t.Error("auth should be enabled by default")
	}
	if cfg.Auth.SessionTimeout != 7*24*time.Hour {
		t.Errorf("default session timeout = %v, want 168h", cfg.Auth.SessionTimeout)
	}
}

func TestDefaultConfig_HLSQualityProfiles(t *testing.T) {
	cfg := DefaultConfig()
	if len(cfg.HLS.QualityProfiles) != 4 {
		t.Errorf("default HLS quality profiles count = %d, want 4", len(cfg.HLS.QualityProfiles))
	}
	names := make(map[string]bool)
	for _, q := range cfg.HLS.QualityProfiles {
		names[q.Name] = true
	}
	for _, want := range []string{"1080p", "720p", "480p", "360p"} {
		if !names[want] {
			t.Errorf("missing default HLS quality profile %q", want)
		}
	}
}

func TestDefaultConfig_Features(t *testing.T) {
	cfg := DefaultConfig()
	if !cfg.Features.EnableHLS {
		t.Error("HLS should be enabled by default")
	}
	if !cfg.Features.EnableAnalytics {
		t.Error("Analytics should be enabled by default")
	}
	if !cfg.Features.EnablePlaylists {
		t.Error("Playlists should be enabled by default")
	}
	if !cfg.Features.EnableUserAuth {
		t.Error("UserAuth should be enabled by default")
	}
	if !cfg.Features.EnableAdminPanel {
		t.Error("AdminPanel should be enabled by default")
	}
	if cfg.Features.EnableReceiver {
		t.Error("Receiver should be disabled by default")
	}
	if cfg.Features.EnableExtractor {
		t.Error("Extractor should be disabled by default")
	}
}

func TestDefaultConfig_StreamingChunkSize(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.Streaming.DefaultChunkSize != 1024*1024 {
		t.Errorf("default chunk size = %d, want 1MB", cfg.Streaming.DefaultChunkSize)
	}
}

func TestDefaultConfig_Security(t *testing.T) {
	cfg := DefaultConfig()
	if !cfg.Security.RateLimitEnabled {
		t.Error("rate limiting should be enabled by default")
	}
	if cfg.Security.CORSEnabled {
		t.Error("CORS should be disabled by default")
	}
}

func TestDefaultConfig_Thumbnails(t *testing.T) {
	cfg := DefaultConfig()
	if !cfg.Thumbnails.Enabled {
		t.Error("thumbnails should be enabled by default")
	}
	if cfg.Thumbnails.Width != 320 || cfg.Thumbnails.Height != 180 {
		t.Errorf("default thumbnail size = %dx%d, want 320x180", cfg.Thumbnails.Width, cfg.Thumbnails.Height)
	}
}

func TestDefaultConfig_Uploads(t *testing.T) {
	cfg := DefaultConfig()
	if !cfg.Uploads.Enabled {
		t.Error("uploads should be enabled by default")
	}
	if len(cfg.Uploads.AllowedExtensions) == 0 {
		t.Error("uploads should have default allowed extensions")
	}
}

func TestDefaultConfig_MatureScanner(t *testing.T) {
	cfg := DefaultConfig()
	if !cfg.MatureScanner.Enabled {
		t.Error("mature scanner should be enabled by default")
	}
	if cfg.MatureScanner.HighConfidenceThreshold == 0 {
		t.Error("high confidence threshold should be non-zero")
	}
}

func TestDefaultConfig_UI(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.UI.ItemsPerPage != 48 {
		t.Errorf("default items per page = %d, want 48", cfg.UI.ItemsPerPage)
	}
}

func TestDefaultConfig_Backup(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.Backup.RetentionCount != 10 {
		t.Errorf("default backup retention = %d, want 10", cfg.Backup.RetentionCount)
	}
}

func TestDefaultConfig_Directories(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.Directories.Videos != "./videos" {
		t.Errorf("default videos dir = %q", cfg.Directories.Videos)
	}
	if cfg.Directories.Music != "./music" {
		t.Errorf("default music dir = %q", cfg.Directories.Music)
	}
}

// ---------------------------------------------------------------------------
// Manager — NewManager, Load, Get
// ---------------------------------------------------------------------------

func TestNewManager_DefaultConfig(t *testing.T) {
	m := NewManager("/nonexistent/config.json")
	cfg := m.Get()
	if cfg == nil {
		t.Fatal("Get() returned nil before Load()")
	}
	if cfg.Server.Port != 8080 {
		t.Errorf("pre-load port = %d, want default 8080", cfg.Server.Port)
	}
}

func TestManager_Load_CreatesConfigIfMissing(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, testConfigFilename)

	m := NewManager(cfgPath)
	if err := m.Load(); err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	// Should have created the config file
	if _, err := os.Stat(cfgPath); os.IsNotExist(err) {
		t.Error("Load() should create config file when missing")
	}
}

func TestManager_Load_FromFile(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, testConfigFilename)

	// Write a custom config
	custom := DefaultConfig()
	custom.Server.Port = 9999
	data, _ := json.MarshalIndent(custom, "", "  ")
	os.WriteFile(cfgPath, data, 0o600)

	m := NewManager(cfgPath)
	if err := m.Load(); err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	cfg := m.Get()
	if cfg.Server.Port != 9999 {
		t.Errorf("port = %d, want 9999 from file", cfg.Server.Port)
	}
}

// TestManager_Load_TunableEnvSeedsOnly_InfraAlwaysApplies locks in the
// precedence guarantee: for an already-migrated config.json, tunable env vars
// (e.g. HLS_CONCURRENT_LIMIT) must NOT override the saved value, while
// infrastructure env vars (e.g. SERVER_PORT) must still apply on every load.
func TestManager_Load_TunableEnvSeedsOnly_InfraAlwaysApplies(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, testConfigFilename)

	custom := DefaultConfig()
	custom.EnvSeedMigrated = true // simulate a config saved after the upgrade
	custom.HLS.ConcurrentLimit = 5
	custom.Server.Port = 8080
	data, _ := json.MarshalIndent(custom, "", "  ")
	if err := os.WriteFile(cfgPath, data, 0o600); err != nil {
		t.Fatal(err)
	}

	t.Setenv("HLS_CONCURRENT_LIMIT", "2") // tunable: must be ignored
	t.Setenv("SERVER_PORT", "9999")       // infra: must apply

	m := NewManager(cfgPath)
	if err := m.Load(); err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	cfg := m.Get()
	if cfg.HLS.ConcurrentLimit != 5 {
		t.Errorf("HLS.ConcurrentLimit = %d, want 5 (config.json must win over tunable env)", cfg.HLS.ConcurrentLimit)
	}
	if cfg.Server.Port != 9999 {
		t.Errorf("Server.Port = %d, want 9999 (infra env must win)", cfg.Server.Port)
	}
}

// TestManager_Load_OneShotMigrationBakesEnv verifies the EnvSeedMigrated
// upgrade: on the first load of a legacy config (flag false) the env-driven
// tunable value is baked into config.json, and on subsequent loads env no
// longer overrides it.
func TestManager_Load_OneShotMigrationBakesEnv(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, testConfigFilename)

	legacy := DefaultConfig()
	legacy.EnvSeedMigrated = false // legacy config predating the upgrade
	legacy.HLS.ConcurrentLimit = 2
	data, _ := json.MarshalIndent(legacy, "", "  ")
	if err := os.WriteFile(cfgPath, data, 0o600); err != nil {
		t.Fatal(err)
	}

	t.Setenv("HLS_CONCURRENT_LIMIT", "7")
	m := NewManager(cfgPath)
	if err := m.Load(); err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if got := m.Get().HLS.ConcurrentLimit; got != 7 {
		t.Errorf("first load ConcurrentLimit = %d, want 7 (env baked in once)", got)
	}

	// After migration the flag is persisted; a different env value must be ignored.
	t.Setenv("HLS_CONCURRENT_LIMIT", "3")
	m2 := NewManager(cfgPath)
	if err := m2.Load(); err != nil {
		t.Fatalf("second Load() error: %v", err)
	}
	if got := m2.Get().HLS.ConcurrentLimit; got != 7 {
		t.Errorf("second load ConcurrentLimit = %d, want 7 (env must not override after migration)", got)
	}
}

func TestManager_Get_ReturnsCopy(t *testing.T) {
	m := NewManager(testTmpConfigPath)
	c1 := m.Get()
	c2 := m.Get()
	c1.Server.Port = 1234
	if c2.Server.Port == 1234 {
		t.Error("Get() should return independent copies")
	}
}

func TestManager_Get_CopiesSlices(t *testing.T) {
	m := NewManager(testTmpConfigPath)
	c1 := m.Get()
	c1.Security.IPWhitelist = append(c1.Security.IPWhitelist, "10.0.0.1")
	c2 := m.Get()
	if len(c2.Security.IPWhitelist) != 0 {
		t.Error("slice mutation on copy should not affect original")
	}
}

// ---------------------------------------------------------------------------
// Manager — Save and Update
// ---------------------------------------------------------------------------

func TestManager_Save(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, testConfigFilename)
	m := NewManager(cfgPath)
	if err := m.Save(); err != nil {
		t.Fatalf("Save() error: %v", err)
	}
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("ReadFile error: %v", err)
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}
	if cfg.Server.Port != 8080 {
		t.Errorf("saved port = %d, want 8080", cfg.Server.Port)
	}
}

func TestManager_Update(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, testConfigFilename)
	m := NewManager(cfgPath)

	err := m.Update(func(c *Config) {
		c.Server.Port = 7777
	})
	if err != nil {
		t.Fatalf("Update() error: %v", err)
	}
	if m.Get().Server.Port != 7777 {
		t.Errorf("port after Update = %d, want 7777", m.Get().Server.Port)
	}
	// Verify persisted
	data, _ := os.ReadFile(cfgPath)
	var cfg Config
	json.Unmarshal(data, &cfg)
	if cfg.Server.Port != 7777 {
		t.Errorf("persisted port = %d, want 7777", cfg.Server.Port)
	}
}

func TestManager_OnChange(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, testConfigFilename)
	m := NewManager(cfgPath)

	called := make(chan int, 1)
	m.OnChange(func(c *Config) {
		called <- c.Server.Port
	})
	m.Update(func(c *Config) {
		c.Server.Port = 5555
	})
	select {
	case port := <-called:
		if port != 5555 {
			t.Errorf("watcher got port=%d, want 5555", port)
		}
	case <-time.After(2 * time.Second):
		t.Error("OnChange watcher not called within 2s")
	}
}

// ---------------------------------------------------------------------------
// Accessors — GetValue, SetValue
// ---------------------------------------------------------------------------

func TestManager_GetValue(t *testing.T) {
	m := NewManager(testTmpConfigPath)
	v, err := m.GetValue(testServerPortKey)
	if err != nil {
		t.Fatalf("GetValue error: %v", err)
	}
	port, ok := v.(int)
	if !ok {
		t.Fatalf("GetValue returned %T, want int", v)
	}
	if port != 8080 {
		t.Errorf("GetValue(server.port) = %d, want 8080", port)
	}
}

func TestManager_GetValue_InvalidPath(t *testing.T) {
	m := NewManager(testTmpConfigPath)
	_, err := m.GetValue("nonexistent.field")
	if err == nil {
		t.Error("GetValue with invalid path should return error")
	}
}

func TestManager_SetValue(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, testConfigFilename)
	m := NewManager(cfgPath)
	if err := m.SetValue(testServerPortKey, 4444); err != nil {
		t.Fatalf("SetValue error: %v", err)
	}
	v, _ := m.GetValue(testServerPortKey)
	if v.(int) != 4444 {
		t.Errorf("after SetValue, port = %d, want 4444", v.(int))
	}
}

func TestManager_GetValue_CaseInsensitive(t *testing.T) {
	m := NewManager(testTmpConfigPath)
	// "Server.Port" and "server.port" should both work via normalizeFieldName
	v1, err1 := m.GetValue("Server.Port")
	v2, err2 := m.GetValue(testServerPortKey)
	if err1 != nil || err2 != nil {
		t.Fatalf("errors: %v, %v", err1, err2)
	}
	if v1.(int) != v2.(int) {
		t.Errorf("case insensitive mismatch: %v vs %v", v1, v2)
	}
}

// ---------------------------------------------------------------------------
// normalizeFieldName
// ---------------------------------------------------------------------------

func TestNormalizeFieldName(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"Port", "port"},
		{"max_open_conns", "maxopenconns"},
		{"CDNBaseURL", "cdnbaseurl"},
		{"hls_cdn_base_url", "hlscdnbaseurl"},
	}
	for _, tc := range tests {
		got := normalizeFieldName(tc.input)
		if got != tc.want {
			t.Errorf("normalizeFieldName(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

// ---------------------------------------------------------------------------
// syncFeatureToggles
// ---------------------------------------------------------------------------

func TestSyncFeatureToggles(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, testConfigFilename)

	// Write config with HLS feature disabled but module enabled
	cfg := DefaultConfig()
	cfg.Features.EnableHLS = false
	cfg.HLS.Enabled = true
	cfg.Features.EnableAnalytics = false
	cfg.Analytics.Enabled = true
	data, _ := json.MarshalIndent(cfg, "", "  ")
	os.WriteFile(cfgPath, data, 0o600)

	m := NewManager(cfgPath)
	if err := m.Load(); err != nil {
		t.Fatalf("Load error: %v", err)
	}
	result := m.Get()
	if result.HLS.Enabled {
		t.Error("HLS.Enabled should be false after syncFeatureToggles (feature disabled)")
	}
	if result.Analytics.Enabled {
		t.Error("Analytics.Enabled should be false after syncFeatureToggles (feature disabled)")
	}
}

// ---------------------------------------------------------------------------
// TasksConfig / TaskOverride
// ---------------------------------------------------------------------------

func TestTasksConfig_DefaultHasNoOverrides(t *testing.T) {
	cfg := DefaultConfig()
	if len(cfg.Tasks.Overrides) != 0 {
		t.Errorf("default Tasks.Overrides should be empty, got %d entries", len(cfg.Tasks.Overrides))
	}
}

func TestTasksConfig_HLSCleanupOffByDefault(t *testing.T) {
	// Memory rule: HLS cache must NEVER be auto-deleted without an explicit
	// admin action. Defaults must respect this.
	cfg := DefaultConfig()
	if cfg.HLS.CleanupEnabled {
		t.Error("HLS.CleanupEnabled must default to false; admin opt-in only")
	}
}

func TestMigrateHLSCleanupEnabled_LegacyConfigForcedOff(t *testing.T) {
	// Simulate a legacy config: CleanupEnabled=true (old default), no
	// migration flag. Migration must flip it to false and mark migrated.
	cfgPath := filepath.Join(t.TempDir(), "config.json")
	raw := `{"hls":{"cleanup_enabled":true}}`
	if err := os.WriteFile(cfgPath, []byte(raw), 0o600); err != nil {
		t.Fatalf("seed config: %v", err)
	}
	m := NewManager(cfgPath)
	if err := m.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if m.Get().HLS.CleanupEnabled {
		t.Error("legacy CleanupEnabled=true should have been forced off")
	}
	if !m.Get().HLS.CleanupMigrated {
		t.Error("CleanupMigrated flag should be set after migration")
	}
}

func TestDefaultAdminConfig_AuditLogRetention(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.Admin.AuditLogRetentionDays != 90 {
		t.Errorf("default audit_log_retention_days = %d, want 90", cfg.Admin.AuditLogRetentionDays)
	}
}

func TestMigrateHLSCleanupEnabled_RespectsExplicitOptIn(t *testing.T) {
	// If the migration has already run and the admin then explicitly turned
	// CleanupEnabled back on, a subsequent reload must NOT flip it back off.
	cfgPath := filepath.Join(t.TempDir(), "config.json")
	raw := `{"hls":{"cleanup_enabled":true,"cleanup_migrated":true}}`
	if err := os.WriteFile(cfgPath, []byte(raw), 0o600); err != nil {
		t.Fatalf("seed config: %v", err)
	}
	m := NewManager(cfgPath)
	if err := m.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !m.Get().HLS.CleanupEnabled {
		t.Error("post-migration explicit CleanupEnabled=true must be preserved")
	}
}

func TestTaskOverride_RoundTripJSON(t *testing.T) {
	// Pointer-typed fields must round-trip through JSON so the absent-vs-false
	// distinction survives a save/load cycle.
	in := TaskOverride{Enabled: new(false), ScheduleSecs: new(1800)}

	blob, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var out TaskOverride
	if err := json.Unmarshal(blob, &out); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if out.Enabled == nil || *out.Enabled != false {
		t.Errorf("Enabled lost across round-trip: got %v", out.Enabled)
	}
	if out.ScheduleSecs == nil || *out.ScheduleSecs != 1800 {
		t.Errorf("ScheduleSecs lost across round-trip: got %v", out.ScheduleSecs)
	}
}

func TestTaskOverride_NilFieldsOmitted(t *testing.T) {
	// A TaskOverride with no fields set must marshal to "{}" — important so
	// the persisted config doesn't pin every task to false/0 just because the
	// admin touched one.
	blob, err := json.Marshal(TaskOverride{})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if string(blob) != "{}" {
		t.Errorf("zero TaskOverride should marshal to {}, got %s", blob)
	}
}

func TestTasksConfig_UpdatePersistsOverrides(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "config.json")
	m := NewManager(cfgPath)
	// Seed with defaults so the file exists.
	if err := m.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}
	enabled := false
	secs := 7200
	if err := m.Update(func(c *Config) {
		if c.Tasks.Overrides == nil {
			c.Tasks.Overrides = make(map[string]TaskOverride)
		}
		c.Tasks.Overrides["hls-inactive-cleanup"] = TaskOverride{
			Enabled:      &enabled,
			ScheduleSecs: &secs,
		}
	}); err != nil {
		t.Fatalf("Update: %v", err)
	}

	// Reload from disk and verify the override survived.
	m2 := NewManager(cfgPath)
	if err := m2.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	over, ok := m2.Get().Tasks.Overrides["hls-inactive-cleanup"]
	if !ok {
		t.Fatal("override not persisted across save/load")
	}
	if over.Enabled == nil || *over.Enabled {
		t.Errorf("Enabled override not persisted; got %v", over.Enabled)
	}
	if over.ScheduleSecs == nil || *over.ScheduleSecs != 7200 {
		t.Errorf("ScheduleSecs override not persisted; got %v", over.ScheduleSecs)
	}
}
