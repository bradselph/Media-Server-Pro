// Package config provides configuration management for the media server.
// It supports JSON files, environment variables (.env), and hot-reloading.
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"

	"media-server-pro/internal/logger"
)

const errConfigPathNotFoundFmt = "config path not found: %s"

// Manager manages configuration loading, saving, and watching
type Manager struct {
	mu         sync.RWMutex
	config     *Config
	configPath string
	log        *logger.Logger
	watchers   []func(*Config)
}

// NewManager creates a new configuration manager
func NewManager(configPath string) *Manager {
	return &Manager{
		config:     DefaultConfig(),
		configPath: configPath,
		log:        logger.New("config"),
		watchers:   make([]func(*Config), 0),
	}
}

// Load loads configuration from file and merges with defaults
// Loading order: defaults -> .env file -> config.json -> environment overrides
func (m *Manager) Load() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.log.Info("Loading configuration...")

	// Start with defaults
	m.config = DefaultConfig()

	// Load .env file if it exists
	envPath := m.findEnvFile()
	if envPath != "" {
		m.log.Info("Loading environment from %s", envPath)
		if err := m.loadEnvFile(envPath); err != nil {
			m.log.Warn("Failed to load .env file: %v", err)
		}
	}

	// Check if config file exists
	if _, err := os.Stat(m.configPath); os.IsNotExist(err) {
		m.log.Info("Configuration file not found, creating with current settings")
		return m.save()
	}

	// Read config file
	m.log.Info("Loading configuration from %s", m.configPath)
	data, err := os.ReadFile(m.configPath)
	if err != nil {
		return fmt.Errorf("failed to read config file: %w", err)
	}

	if err := json.Unmarshal(data, m.config); err != nil {
		return fmt.Errorf("failed to parse config file: %w", err)
	}

	m.applyEnvOverrides()
	m.resolveAbsolutePaths()
	m.syncFeatureToggles()
	m.migrateHLSQualityEnabled()

	m.log.Info("Configuration loaded successfully")
	return nil
}

// syncFeatureToggles makes feature toggles the master: when true, module is enabled;
// when false, module is disabled. This overrides module-level Enabled in config.json.
//
// NOTE: The following feature flags do NOT have corresponding module-level Enabled
// fields (there is no PlaylistConfig, SuggestionsConfig, AutoDiscoveryConfig, or
// DuplicateDetectionConfig with an Enabled bool). They only take effect at startup
// when checked in cmd/server/main.go to decide whether to register the module:
//   - EnablePlaylists
//   - EnableSuggestions
//   - EnableAutoDiscovery
//   - EnableDuplicateDetection
func (m *Manager) syncFeatureToggles() {
	syncToggle := func(enabled bool, target *bool) {
		*target = enabled
	}
	f := &m.config.Features
	cfg := m.config
	syncToggle(f.EnableHLS, &cfg.HLS.Enabled)
	syncToggle(f.EnableAnalytics, &cfg.Analytics.Enabled)
	syncToggle(f.EnableRemoteMedia, &cfg.RemoteMedia.Enabled)
	syncToggle(f.EnableReceiver, &cfg.Receiver.Enabled)
	syncToggle(f.EnableExtractor, &cfg.Extractor.Enabled)
	syncToggle(f.EnableCrawler, &cfg.Crawler.Enabled)
	syncToggle(f.EnableMatureScanner, &cfg.MatureScanner.Enabled)
	syncToggle(f.EnableHuggingFace, &cfg.HuggingFace.Enabled)
	syncToggle(f.EnableThumbnails, &cfg.Thumbnails.Enabled)
	syncToggle(f.EnableUploads, &cfg.Uploads.Enabled)
	syncToggle(f.EnableUserAuth, &cfg.Auth.Enabled)
	syncToggle(f.EnableAdminPanel, &cfg.Admin.Enabled)
	syncToggle(f.EnableDownloader, &cfg.Downloader.Enabled)
}

// migrateHLSQualityEnabled sets Enabled=true for HLS quality profiles that
// were saved before the Enabled field was added. Without this, existing configs
// would have all profiles disabled (Go zero-value for bool = false).
func (m *Manager) migrateHLSQualityEnabled() {
	profiles := m.config.HLS.QualityProfiles
	if len(profiles) == 0 {
		return
	}
	// If ANY profile has Enabled=true, the config is already migrated.
	for _, p := range profiles {
		if p.Enabled {
			return
		}
	}
	// All profiles are Enabled=false — this is a pre-migration config. Enable all.
	for i := range profiles {
		profiles[i].Enabled = true
	}
	m.log.Info("Migrated %d HLS quality profiles to include enabled flag", len(profiles))
}

// Save saves the current configuration to file
func (m *Manager) Save() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.save()
}

// save writes config to disk using a crash-safe rename strategy.
// On Windows, atomic rename-over-existing is not supported, so we:
//  1. Write to .tmp
//  2. Rename existing config to .bak (preserving a fallback)
//  3. Rename .tmp to config
//  4. Remove .bak
//
// A crash between steps 2-3 leaves a .bak file that can be recovered manually.
func (m *Manager) save() error {
	data, err := json.MarshalIndent(m.config, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}
	tempPath := m.configPath + ".tmp"
	bakPath := m.configPath + ".bak"
	if err := os.WriteFile(tempPath, data, 0600); err != nil {
		return fmt.Errorf("failed to write temp config: %w", err)
	}
	// Rename existing config to .bak (safe — original preserved as backup)
	if _, err := os.Stat(m.configPath); err == nil {
		_ = os.Remove(bakPath) // remove stale backup if any
		if err := os.Rename(m.configPath, bakPath); err != nil {
			_ = os.Remove(tempPath)
			return fmt.Errorf("failed to backup old config: %w", err)
		}
	}
	// Rename .tmp to config
	if err := os.Rename(tempPath, m.configPath); err != nil {
		// Attempt to restore backup
		_ = os.Rename(bakPath, m.configPath)
		_ = os.Remove(tempPath)
		return fmt.Errorf("failed to rename config: %w", err)
	}
	// Clean up backup
	_ = os.Remove(bakPath)
	m.log.Info("Configuration saved to %s", m.configPath)
	return nil
}

func (m *Manager) rollbackFromJSON(originalJSON []byte, saveErr error) {
	var rollbackConfig Config
	if err := json.Unmarshal(originalJSON, &rollbackConfig); err == nil {
		m.config = &rollbackConfig
		m.log.Warn("Config save failed, rolled back in-memory changes: %v", saveErr)
	}
}

// Get returns a copy of the current configuration
func (m *Manager) Get() *Config {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.getCopy()
}

func (m *Manager) getCopy() *Config {
	cp := *m.config
	cp.Security.IPWhitelist = append([]string(nil), m.config.Security.IPWhitelist...)
	cp.Security.IPBlacklist = append([]string(nil), m.config.Security.IPBlacklist...)
	cp.Security.CORSOrigins = append([]string(nil), m.config.Security.CORSOrigins...)
	cp.Uploads.AllowedExtensions = append([]string(nil), m.config.Uploads.AllowedExtensions...)
	cp.HLS.QualityProfiles = append([]HLSQuality(nil), m.config.HLS.QualityProfiles...)
	cp.Auth.UserTypes = append([]UserType(nil), m.config.Auth.UserTypes...)
	cp.RemoteMedia.Sources = append([]RemoteSource(nil), m.config.RemoteMedia.Sources...)
	cp.MatureScanner.HighConfidenceKeywords = append([]string(nil), m.config.MatureScanner.HighConfidenceKeywords...)
	cp.MatureScanner.MediumConfidenceKeywords = append([]string(nil), m.config.MatureScanner.MediumConfidenceKeywords...)
	cp.AgeGate.BypassIPs = append([]string(nil), m.config.AgeGate.BypassIPs...)
	cp.Receiver.APIKeys = append([]string(nil), m.config.Receiver.APIKeys...)
	return &cp
}

// Update updates the configuration and notifies watchers
func (m *Manager) Update(updater func(*Config)) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	originalJSON, err := json.Marshal(m.config)
	if err != nil {
		return fmt.Errorf("failed to marshal original config for backup: %w", err)
	}
	updater(m.config)
	if err := m.save(); err != nil {
		m.rollbackFromJSON(originalJSON, err)
		return err
	}
	cfg := m.getCopy()
	watchers := make([]func(*Config), len(m.watchers))
	copy(watchers, m.watchers)
	for _, watcher := range watchers {
		w := watcher
		go func() {
			defer func() {
				if r := recover(); r != nil {
					m.log.Error("Config watcher panic recovered: %v", r)
				}
			}()
			w(cfg)
		}()
	}
	return nil
}

// OnChange registers a callback for configuration changes
func (m *Manager) OnChange(callback func(*Config)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.watchers = append(m.watchers, callback)
}
