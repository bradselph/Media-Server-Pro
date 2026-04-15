package admin

import (
	"testing"

	"media-server-pro/internal/config"
)

const errFmtEnabled = "enabled = %v"

// ---------------------------------------------------------------------------
// buildConfig*Map — all remaining helpers
// ---------------------------------------------------------------------------

func TestBuildConfigHLSMap(t *testing.T) {
	cfg := &config.Config{}
	cfg.HLS.QualityProfiles = []config.HLSQuality{
		{Name: "720p", Width: 1280, Height: 720, Bitrate: 2500, AudioBitrate: 128, Enabled: true},
	}
	m := buildConfigHLSMap(cfg, nil)
	if m == nil {
		t.Fatal("nil map")
	}
	profiles, ok := m["quality_profiles"].([]map[string]any)
	if !ok {
		t.Fatal("quality_profiles should be []map[string]any")
	}
	if len(profiles) != 1 {
		t.Errorf("expected 1 profile, got %d", len(profiles))
	}
	if profiles[0]["name"] != "720p" {
		t.Errorf("profile name = %v", profiles[0]["name"])
	}
}

func TestBuildConfigThumbnailsMap(t *testing.T) {
	cfg := &config.Config{}
	cfg.Thumbnails.Width = 320
	cfg.Thumbnails.Height = 180
	cfg.Thumbnails.Quality = 80
	m := buildConfigThumbnailsMap(cfg, nil)
	if m == nil {
		t.Fatal("nil map")
	}
	if m["width"] != 320 {
		t.Errorf("width = %v, want 320", m["width"])
	}
	if m["height"] != 180 {
		t.Errorf("height = %v, want 180", m["height"])
	}
	if m["quality"] != 80 {
		t.Errorf("quality = %v, want 80", m["quality"])
	}
}

func TestBuildConfigAnalyticsMap(t *testing.T) {
	cfg := &config.Config{}
	cfg.Features.EnableAnalytics = true
	cfg.Analytics.TrackPlayback = true
	m := buildConfigAnalyticsMap(cfg, nil)
	if m["enabled"] != true {
		t.Errorf("enabled = %v, want true", m["enabled"])
	}
	if m["track_playback"] != true {
		t.Errorf("track_playback = %v, want true", m["track_playback"])
	}
}

func TestBuildConfigMatureScannerMap(t *testing.T) {
	cfg := &config.Config{}
	cfg.MatureScanner.Enabled = true
	cfg.MatureScanner.AutoFlag = true
	cfg.MatureScanner.HighConfidenceThreshold = 0.9
	m := buildConfigMatureScannerMap(cfg, nil)
	if m["enabled"] != true {
		t.Errorf(errFmtEnabled, m["enabled"])
	}
	if m["auto_flag"] != true {
		t.Errorf("auto_flag = %v", m["auto_flag"])
	}
}

func TestBuildConfigHuggingFaceMap(t *testing.T) {
	cfg := &config.Config{}
	cfg.HuggingFace.APIKey = "secret-key"
	m := buildConfigHuggingFaceMap(cfg, nil)
	if m["api_key_set"] != true {
		t.Error("api_key_set should be true when key is set")
	}
	// Key itself should not be in the map
	if _, exists := m["api_key"]; exists {
		t.Error("raw api_key should not be exposed")
	}
}

func TestBuildConfigHuggingFaceMap_NoKey(t *testing.T) {
	cfg := &config.Config{}
	m := buildConfigHuggingFaceMap(cfg, nil)
	if m["api_key_set"] != false {
		t.Error("api_key_set should be false when key is empty")
	}
}

func TestBuildConfigStreamingMap(t *testing.T) {
	cfg := &config.Config{}
	cfg.Streaming.RequireAuth = true
	cfg.Streaming.MobileOptimization = true
	cfg.Streaming.UnauthStreamLimit = 5
	m := buildConfigStreamingMap(cfg, nil)
	if m["require_auth"] != true {
		t.Errorf("require_auth = %v", m["require_auth"])
	}
	if m["unauth_stream_limit"] != 5 {
		t.Errorf("unauth_stream_limit = %v", m["unauth_stream_limit"])
	}
}

func TestBuildConfigDownloadMap(t *testing.T) {
	cfg := &config.Config{}
	cfg.Download.Enabled = true
	cfg.Download.RequireAuth = true
	cfg.Download.ChunkSizeKB = 512
	m := buildConfigDownloadMap(cfg, nil)
	if m["enabled"] != true {
		t.Errorf(errFmtEnabled, m["enabled"])
	}
	if m["chunk_size_kb"] != 512 {
		t.Errorf("chunk_size_kb = %v", m["chunk_size_kb"])
	}
}

func TestBuildConfigLoggingMap(t *testing.T) {
	cfg := &config.Config{}
	cfg.Logging.Level = "debug"
	m := buildConfigLoggingMap(cfg, nil)
	if m["level"] != "debug" {
		t.Errorf("level = %v", m["level"])
	}
}

func TestBuildConfigAgeGateMap(t *testing.T) {
	cfg := &config.Config{}
	cfg.AgeGate.Enabled = true
	cfg.AgeGate.CookieName = "age_verified"
	cfg.AgeGate.CookieMaxAge = 86400
	m := buildConfigAgeGateMap(cfg, nil)
	if m["enabled"] != true {
		t.Errorf(errFmtEnabled, m["enabled"])
	}
	if m["cookie_name"] != "age_verified" {
		t.Errorf("cookie_name = %v", m["cookie_name"])
	}
}

func TestBuildConfigUploadsMap(t *testing.T) {
	cfg := &config.Config{}
	cfg.Uploads.MaxFileSize = 1073741824
	cfg.Uploads.AllowedExtensions = []string{".mp4", ".mkv"}
	m := buildConfigUploadsMap(cfg, nil)
	if m["max_file_size"] != int64(1073741824) {
		t.Errorf("max_file_size = %v", m["max_file_size"])
	}
}

func TestBuildConfigUIMap(t *testing.T) {
	cfg := &config.Config{}
	cfg.UI.ItemsPerPage = 24
	cfg.UI.MobileItemsPerPage = 12
	m := buildConfigUIMap(cfg, nil)
	if m["items_per_page"] != 24 {
		t.Errorf("items_per_page = %v", m["items_per_page"])
	}
}

func TestBuildConfigDownloaderMap(t *testing.T) {
	cfg := &config.Config{}
	cfg.Downloader.Enabled = true
	cfg.Downloader.URL = "http://localhost:8080"
	m := buildConfigDownloaderMap(cfg, nil)
	if m["enabled"] != true {
		t.Errorf(errFmtEnabled, m["enabled"])
	}
	if m["url"] != "http://localhost:8080" {
		t.Errorf("url = %v", m["url"])
	}
}

func TestBuildConfigStorageMap(t *testing.T) {
	cfg := &config.Config{}
	cfg.Storage.Backend = "s3"
	cfg.Storage.S3.Endpoint = "s3.example.com"
	cfg.Storage.S3.AccessKeyID = "AKID"
	cfg.Storage.S3.SecretAccessKey = "SECRET"
	m := buildConfigStorageMap(cfg, nil)
	if m["backend"] != "s3" {
		t.Errorf("backend = %v", m["backend"])
	}
	s3map, ok := m["s3"].(map[string]any)
	if !ok {
		t.Fatal("s3 should be a map")
	}
	if s3map["access_key_set"] != true {
		t.Error("access_key_set should be true")
	}
	if s3map["secret_key_set"] != true {
		t.Error("secret_key_set should be true")
	}
}

func TestBuildConfigStorageMap_NoKeys(t *testing.T) {
	cfg := &config.Config{}
	cfg.Storage.Backend = "local"
	m := buildConfigStorageMap(cfg, nil)
	s3map := m["s3"].(map[string]any)
	if s3map["access_key_set"] != false {
		t.Error("access_key_set should be false")
	}
}

func TestBuildConfigBackupMap(t *testing.T) {
	cfg := &config.Config{}
	cfg.Backup.RetentionCount = 5
	m := buildConfigBackupMap(cfg, nil)
	if m["retention_count"] != 5 {
		t.Errorf("retention_count = %v", m["retention_count"])
	}
}

func TestBuildConfigUpdaterMap(t *testing.T) {
	cfg := &config.Config{}
	cfg.Updater.UpdateMethod = "git"
	cfg.Updater.Branch = "main"
	m := buildConfigUpdaterMap(cfg, nil)
	if m["update_method"] != "git" {
		t.Errorf("update_method = %v", m["update_method"])
	}
	if m["branch"] != "main" {
		t.Errorf("branch = %v", m["branch"])
	}
}

func TestBuildConfigRemoteMediaMap(t *testing.T) {
	cfg := &config.Config{}
	cfg.RemoteMedia.Enabled = true
	cfg.RemoteMedia.CacheEnabled = true
	m := buildConfigRemoteMediaMap(cfg, nil)
	if m["enabled"] != true {
		t.Errorf(errFmtEnabled, m["enabled"])
	}
}

func TestBuildConfigCrawlerMap(t *testing.T) {
	cfg := &config.Config{}
	cfg.Crawler.Enabled = true
	cfg.Crawler.BrowserEnabled = true
	cfg.Crawler.MaxPages = 50
	m := buildConfigCrawlerMap(cfg, nil)
	if m["enabled"] != true {
		t.Errorf(errFmtEnabled, m["enabled"])
	}
	if m["max_pages"] != 50 {
		t.Errorf("max_pages = %v", m["max_pages"])
	}
}

func TestBuildConfigExtractorMap(t *testing.T) {
	cfg := &config.Config{}
	cfg.Extractor.Enabled = true
	cfg.Extractor.MaxItems = 100
	m := buildConfigExtractorMap(cfg, nil)
	if m["enabled"] != true {
		t.Errorf(errFmtEnabled, m["enabled"])
	}
	if m["max_items"] != 100 {
		t.Errorf("max_items = %v", m["max_items"])
	}
}
