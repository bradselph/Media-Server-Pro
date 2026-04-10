package config

import (
	"os"
	"path/filepath"
	"testing"
)

const testConfigFilename = "config.json"

// ---------------------------------------------------------------------------
// parseEnvLine
// ---------------------------------------------------------------------------

func TestParseEnvLine_Simple(t *testing.T) {
	key, val := parseEnvLine("FOO=bar")
	if key != "FOO" || val != "bar" {
		t.Errorf("parseEnvLine(%q) = (%q, %q), want (FOO, bar)", "FOO=bar", key, val)
	}
}

func TestParseEnvLine_DoubleQuoted(t *testing.T) {
	key, val := parseEnvLine(`KEY="hello world"`)
	if key != "KEY" || val != "hello world" {
		t.Errorf("parseEnvLine = (%q, %q), want (KEY, hello world)", key, val)
	}
}

func TestParseEnvLine_SingleQuoted(t *testing.T) {
	key, val := parseEnvLine(`KEY='hello world'`)
	if key != "KEY" || val != "hello world" {
		t.Errorf("parseEnvLine = (%q, %q), want (KEY, hello world)", key, val)
	}
}

func TestParseEnvLine_InlineComment(t *testing.T) {
	key, val := parseEnvLine("KEY=value # this is a comment")
	if key != "KEY" || val != "value" {
		t.Errorf("parseEnvLine = (%q, %q), want (KEY, value)", key, val)
	}
}

func TestParseEnvLine_EmptyValue(t *testing.T) {
	key, val := parseEnvLine("KEY=")
	if key != "KEY" || val != "" {
		t.Errorf("parseEnvLine = (%q, %q), want (KEY, empty)", key, val)
	}
}

func TestParseEnvLine_NoEquals(t *testing.T) {
	key, val := parseEnvLine("NOEQUALS")
	if key != "" || val != "" {
		t.Errorf("parseEnvLine = (%q, %q), want (empty, empty)", key, val)
	}
}

func TestParseEnvLine_WhitespaceAround(t *testing.T) {
	key, _ := parseEnvLine("  KEY = value  ")
	// Key should be trimmed
	if key == "" {
		t.Error("key should not be empty")
	}
}

func TestParseEnvLine_ValueWithEquals(t *testing.T) {
	key, val := parseEnvLine("DSN=postgres://user:pass@host/db?sslmode=disable")
	if key != "DSN" {
		t.Errorf("key = %q, want DSN", key)
	}
	if val == "" {
		t.Error("value should contain the full connection string")
	}
}

// ---------------------------------------------------------------------------
// CreateDirectories
// ---------------------------------------------------------------------------

func TestCreateDirectories(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, testConfigFilename)
	mgr := NewManager(cfgPath)

	// Set directories to temp paths
	cfg := mgr.Get()
	cfg.Directories.Videos = filepath.Join(dir, "videos")
	cfg.Directories.Uploads = filepath.Join(dir, "uploads")
	cfg.Directories.HLSCache = filepath.Join(dir, "hls")
	cfg.Directories.Thumbnails = filepath.Join(dir, "thumbs")
	mgr.Update(func(c *Config) {
		c.Directories = cfg.Directories
	})

	if err := mgr.CreateDirectories(); err != nil {
		t.Fatalf("CreateDirectories: %v", err)
	}

	// Verify directories were created
	for _, path := range []string{
		filepath.Join(dir, "videos"),
		filepath.Join(dir, "uploads"),
		filepath.Join(dir, "hls"),
		filepath.Join(dir, "thumbs"),
	} {
		fi, err := os.Stat(path)
		if err != nil {
			t.Errorf("directory %q not created: %v", path, err)
			continue
		}
		if !fi.IsDir() {
			t.Errorf("%q should be a directory", path)
		}
	}
}

// ---------------------------------------------------------------------------
// SetValuesBatch
// ---------------------------------------------------------------------------

func TestSetValuesBatch_SingleValue(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, testConfigFilename)
	mgr := NewManager(cfgPath)

	err := mgr.SetValuesBatch(map[string]interface{}{
		"server.port": 9090,
	})
	if err != nil {
		t.Fatalf("SetValuesBatch: %v", err)
	}
	cfg := mgr.Get()
	if cfg.Server.Port != 9090 {
		t.Errorf("Server.Port = %d, want 9090", cfg.Server.Port)
	}
}

func TestSetValuesBatch_MultipleValues(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, testConfigFilename)
	mgr := NewManager(cfgPath)

	err := mgr.SetValuesBatch(map[string]interface{}{
		"server.port":   8888,
		"server.host":   "0.0.0.0",
		"logging.level": "debug",
	})
	if err != nil {
		t.Fatalf("SetValuesBatch: %v", err)
	}
	cfg := mgr.Get()
	if cfg.Server.Port != 8888 {
		t.Errorf("Server.Port = %d", cfg.Server.Port)
	}
	if cfg.Server.Host != "0.0.0.0" {
		t.Errorf("Server.Host = %q", cfg.Server.Host)
	}
}

func TestSetValuesBatch_InvalidPath(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, testConfigFilename)
	mgr := NewManager(cfgPath)

	err := mgr.SetValuesBatch(map[string]interface{}{
		"nonexistent.field.path": "value",
	})
	if err == nil {
		t.Error("expected error for invalid field path")
	}
}

// ---------------------------------------------------------------------------
// getCopy — deep copy verification
// ---------------------------------------------------------------------------

func TestGetCopy_SliceIsolation(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, testConfigFilename)
	mgr := NewManager(cfgPath)

	mgr.Update(func(c *Config) {
		c.Security.CORSOrigins = []string{"http://a.com", "http://b.com"}
	})

	cfg1 := mgr.Get()
	cfg2 := mgr.Get()

	// Mutate the first copy
	cfg1.Security.CORSOrigins[0] = "MUTATED"

	// Second copy should be unaffected
	if cfg2.Security.CORSOrigins[0] != "http://a.com" {
		t.Error("getCopy should produce independent slice copies")
	}
}

// ---------------------------------------------------------------------------
// syncFeatureToggles — extended
// ---------------------------------------------------------------------------

func TestSyncFeatureToggles_AllEnabled(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, testConfigFilename)
	mgr := NewManager(cfgPath)

	mgr.Update(func(c *Config) {
		c.Features.EnableHLS = true
		c.Features.EnableThumbnails = true
		c.Features.EnableAnalytics = true
		c.Features.EnableUploads = true
	})
	cfg := mgr.Get()
	if !cfg.Features.EnableHLS {
		t.Error("HLS should be enabled")
	}
	if !cfg.Features.EnableUploads {
		t.Error("Uploads should be enabled")
	}
}

// ---------------------------------------------------------------------------
// normalizeFieldName — extended
// ---------------------------------------------------------------------------

func TestNormalizeFieldName_Extended(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"enable_hls", "enablehls"},
		{"HLS", "hls"},
		{"a_b_c", "abc"},
	}
	for _, tc := range tests {
		got := normalizeFieldName(tc.input)
		if got != tc.want {
			t.Errorf("normalizeFieldName(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}
