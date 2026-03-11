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

	m.log.Info("Configuration loaded successfully")
	return nil
}

func (m *Manager) syncFeatureToggles() {
	disableIfOff := func(enabled bool, target *bool) {
		if !enabled {
			*target = false
		}
	}
	f := &m.config.Features
	cfg := m.config
	disableIfOff(f.EnableHLS, &cfg.HLS.Enabled)
	disableIfOff(f.EnableAnalytics, &cfg.Analytics.Enabled)
	disableIfOff(f.EnableRemoteMedia, &cfg.RemoteMedia.Enabled)
	disableIfOff(f.EnableReceiver, &cfg.Receiver.Enabled)
	disableIfOff(f.EnableExtractor, &cfg.Extractor.Enabled)
	disableIfOff(f.EnableCrawler, &cfg.Crawler.Enabled)
	disableIfOff(f.EnableMatureScanner, &cfg.MatureScanner.Enabled)
	disableIfOff(f.EnableHuggingFace, &cfg.HuggingFace.Enabled)
	disableIfOff(f.EnableThumbnails, &cfg.Thumbnails.Enabled)
	disableIfOff(f.EnableUploads, &cfg.Uploads.Enabled)
	disableIfOff(f.EnableUserAuth, &cfg.Auth.Enabled)
	disableIfOff(f.EnableAdminPanel, &cfg.Admin.Enabled)
}

// Save saves the current configuration to file
func (m *Manager) Save() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.save()
}

func (m *Manager) save() error {
	data, err := json.MarshalIndent(m.config, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}
	tempPath := m.configPath + ".tmp"
	if err := os.WriteFile(tempPath, data, 0600); err != nil {
		return fmt.Errorf("failed to write temp config: %w", err)
	}
	if _, err := os.Stat(m.configPath); err == nil {
		if err := os.Remove(m.configPath); err != nil {
			return fmt.Errorf("failed to remove old config: %w", err)
		}
	}
	if err := os.Rename(tempPath, m.configPath); err != nil {
		_ = os.Remove(tempPath)
		return fmt.Errorf("failed to rename config: %w", err)
	}
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
