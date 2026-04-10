package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
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
	cfgPath := filepath.Join(dir, "config.json")

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
	cfgPath := filepath.Join(dir, "config.json")

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

func TestManager_Get_ReturnsCopy(t *testing.T) {
	m := NewManager("/tmp/test-config.json")
	c1 := m.Get()
	c2 := m.Get()
	c1.Server.Port = 1234
	if c2.Server.Port == 1234 {
		t.Error("Get() should return independent copies")
	}
}

func TestManager_Get_CopiesSlices(t *testing.T) {
	m := NewManager("/tmp/test-config.json")
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
	cfgPath := filepath.Join(dir, "config.json")
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
	cfgPath := filepath.Join(dir, "config.json")
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
	cfgPath := filepath.Join(dir, "config.json")
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
	m := NewManager("/tmp/test-config.json")
	v, err := m.GetValue("server.port")
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
	m := NewManager("/tmp/test-config.json")
	_, err := m.GetValue("nonexistent.field")
	if err == nil {
		t.Error("GetValue with invalid path should return error")
	}
}

func TestManager_SetValue(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.json")
	m := NewManager(cfgPath)
	if err := m.SetValue("server.port", 4444); err != nil {
		t.Fatalf("SetValue error: %v", err)
	}
	v, _ := m.GetValue("server.port")
	if v.(int) != 4444 {
		t.Errorf("after SetValue, port = %d, want 4444", v.(int))
	}
}

func TestManager_GetValue_CaseInsensitive(t *testing.T) {
	m := NewManager("/tmp/test-config.json")
	// "Server.Port" and "server.port" should both work via normalizeFieldName
	v1, err1 := m.GetValue("Server.Port")
	v2, err2 := m.GetValue("server.port")
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
	cfgPath := filepath.Join(dir, "config.json")

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
