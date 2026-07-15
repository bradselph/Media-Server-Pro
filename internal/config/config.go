// Package config provides configuration management for the media server.
// It supports JSON files, environment variables (.env), and hot-reloading.
package config

import (
	"encoding/json"
	"fmt"
	"maps"
	"net"
	"os"
	"sync"
	"time"

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

// Load loads configuration from file and merges with defaults.
//
// Precedence (see env_overrides.go for the infra/tunable split):
//   - Infrastructure/secret env vars (paths, bind, DB/storage creds, log level,
//     updater branch, admin bootstrap) ALWAYS apply, on every load. They are
//     owned by the deployment environment, not the admin UI.
//   - Tunable settings (HLS, security, features, UI, etc.) are owned by
//     config.json / the admin UI. Tunable env vars only seed a brand-new
//     config.json, or run once during the EnvSeedMigrated upgrade. After that,
//     a value changed in the UI is authoritative and is never reverted by env.
func (m *Manager) Load() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.log.Info("Loading configuration...")

	// Start with defaults
	m.config = DefaultConfig()

	// Load .env file if it exists (populates the process environment that the
	// *EnvOverrides readers below consult).
	envPath := m.findEnvFile()
	if envPath != "" {
		m.log.Info("Loading environment from %s", envPath)
		if err := m.loadEnvFile(envPath); err != nil {
			m.log.Warn("Failed to load .env file: %v", err)
		}
	}

	// Determine whether a config file already exists. If it is missing, check
	// for a .bak file left by a crash between the rename steps in save() — if
	// found, restore it (counts as "exists") rather than silently creating a
	// fresh default config.
	configExists := true
	if _, err := os.Stat(m.configPath); os.IsNotExist(err) {
		bakPath := m.configPath + ".bak"
		if _, bakErr := os.Stat(bakPath); bakErr == nil {
			m.log.Warn("config.json missing but %s exists (crash recovery) — restoring from backup", bakPath)
			if renameErr := os.Rename(bakPath, m.configPath); renameErr != nil {
				return fmt.Errorf("crash recovery: failed to restore %s to %s: %w", bakPath, m.configPath, renameErr)
			}
		} else {
			configExists = false
		}
	}

	needsSave := false

	if configExists {
		m.log.Info("Loading configuration from %s", m.configPath)
		data, err := os.ReadFile(m.configPath)
		if err != nil {
			return fmt.Errorf("failed to read config file: %w", err)
		}
		if err := json.Unmarshal(data, m.config); err != nil {
			return fmt.Errorf("failed to parse config file: %w", err)
		}

		// Infrastructure/secret env vars always win; tunables stay as saved.
		m.applyInfraEnvOverrides()

		// One-shot upgrade: bake the current env-driven tunable values into
		// config.json so effective behavior does not change on the first load
		// after the upgrade, then let config.json own tunables from then on.
		if !m.config.EnvSeedMigrated {
			m.applyTunableEnvOverrides()
			m.config.EnvSeedMigrated = true
			needsSave = true
			m.log.Info("Config migration: tunable settings are now owned by config.json / the admin UI (environment variables seed only)")
		}
	} else {
		// Fresh install: seed the whole config from environment + defaults and
		// persist it. config.json is authoritative for tunables from here on.
		m.log.Info("Configuration file not found, seeding from environment and defaults")
		m.applyEnvOverrides()
		m.config.EnvSeedMigrated = true
		needsSave = true
	}

	m.resolveAbsolutePaths()
	m.syncFeatureToggles()
	// One-shot migrations must persist their Migrated flags, otherwise they
	// re-run on every restart until some unrelated config save lands them.
	if m.migrateHLSQualityEnabled() {
		needsSave = true
	}
	if m.migrateHLSCleanupEnabled() {
		needsSave = true
	}
	if m.migrateStreamingBufferSize() {
		needsSave = true
	}
	if m.migrateHLSConcurrentLimitAuto() {
		needsSave = true
	}
	if m.migrateThumbnailWorkerCountAuto() {
		needsSave = true
	}
	m.normalizeHLSScalars()
	if err := m.validate(); err != nil {
		return err
	}

	if needsSave {
		if err := m.save(); err != nil {
			return err
		}
	}

	m.log.Info("Configuration loaded successfully")
	return nil
}

// validate checks configuration for obviously incorrect values.
// Called from Load() while the write lock is already held. It runs the same
// sub-validators as Validate() but without acquiring the lock again (which
// would deadlock). Additional startup-only warnings are also emitted here.
func (m *Manager) validate() error {
	// Warn about invalid CIDR entries.
	for _, cidr := range m.config.Security.TrustedProxyCIDRs {
		if _, _, err := net.ParseCIDR(cidr); err != nil {
			m.log.Warn("Invalid CIDR in security.trusted_proxy_cidrs (will be ignored): %q: %v", cidr, err)
		}
	}

	// Warn about negative HLS concurrent limit (0 = auto is valid).
	if m.config.HLS.Enabled && m.config.HLS.ConcurrentLimit < 0 {
		m.log.Warn("HLS concurrent_limit is negative (%d); will be treated as auto", m.config.HLS.ConcurrentLimit)
	}

	// Run the same sub-validators as Validate() (lock already held — do NOT
	// call Validate() which would try to acquire the lock and deadlock).
	if errs := m.validateLocked(); len(errs) > 0 {
		return errs[0]
	}
	return nil
}

// syncFeatureToggles makes feature toggles the master: when true, module is enabled;
// when false, module is disabled. This overrides module-level Enabled in config.json.
//
// NOTE: The following feature flags do NOT have corresponding module-level Enabled
// fields (there is no PlaylistConfig, SuggestionsConfig, AutoDiscoveryConfig, or
// DuplicateDetectionConfig with an Enabled bool). Their modules are always
// constructed; the flags are enforced hot-reloadably at request time by the
// require*/checkFeatureEnabled handler guards, and at tick time inside the
// related background tasks (media-scan, duplicate-scan):
//   - EnablePlaylists
//   - EnableSuggestions
//   - EnableAutoDiscovery (also gates module construction in cmd/server/main.go)
//   - EnableDuplicateDetection
func (m *Manager) syncFeatureToggles() {
	f := &m.config.Features
	cfg := m.config
	cfg.HLS.Enabled = f.EnableHLS
	cfg.Analytics.Enabled = f.EnableAnalytics
	cfg.RemoteMedia.Enabled = f.EnableRemoteMedia
	cfg.Receiver.Enabled = f.EnableReceiver
	cfg.Extractor.Enabled = f.EnableExtractor
	cfg.Crawler.Enabled = f.EnableCrawler
	cfg.MatureScanner.Enabled = f.EnableMatureScanner
	cfg.Hub.Enabled = f.EnableHub
	cfg.HuggingFace.Enabled = f.EnableHuggingFace
	cfg.Thumbnails.Enabled = f.EnableThumbnails
	cfg.Uploads.Enabled = f.EnableUploads
	cfg.Auth.Enabled = f.EnableUserAuth
	cfg.Admin.Enabled = f.EnableAdminPanel
	cfg.Downloader.Enabled = f.EnableDownloader
}

// migrateHLSQualityEnabled sets Enabled=true for HLS quality profiles that
// were saved before the Enabled field was added. Without this, existing configs
// would have all profiles disabled (Go zero-value for bool = false).
// The migration is idempotent: once QualityProfilesMigrated is true it will not
// fire again, so a user who later deliberately disables all profiles will not
// have them silently re-enabled on the next restart.
// The bool return reports whether this load performed the migration (and the
// flag therefore needs to be persisted).
func (m *Manager) migrateHLSQualityEnabled() bool {
	if m.config.HLS.QualityProfilesMigrated {
		return false
	}
	profiles := m.config.HLS.QualityProfiles
	if len(profiles) == 0 {
		m.config.HLS.QualityProfilesMigrated = true
		return true
	}
	// If ANY profile has Enabled=true the config was already written after the
	// Enabled field existed — mark migrated and leave profiles unchanged.
	for _, p := range profiles {
		if p.Enabled {
			m.config.HLS.QualityProfilesMigrated = true
			return true
		}
	}
	// All profiles are Enabled=false and the migration flag is unset — this is a
	// pre-migration config. Enable all profiles and mark the migration done.
	for i := range profiles {
		profiles[i].Enabled = true
	}
	m.config.HLS.QualityProfilesMigrated = true
	m.log.Info("Migrated %d HLS quality profiles to include enabled flag", len(profiles))
	return true
}

// migrateHLSCleanupEnabled is a one-shot upgrade migration. Before the
// hls-inactive-cleanup scheduled task existed, HLS.CleanupEnabled was
// shipped as true-by-default but read by nothing, so existing installs
// have it persisted as true even though no admin explicitly chose
// auto-eviction. The product rule (memory: "HLS cache must NEVER be
// automatically deleted") requires that we force every legacy config to
// the safer "off" state on first load; admins who genuinely want cleanup
// can flip it back on now that the toggle actually controls behavior.
//
// The migration runs at most once per config file: CleanupMigrated is set
// after the first pass so any later admin choice (true or false) is
// preserved on subsequent restarts.
// The bool return reports whether this load performed the migration (and the
// flag therefore needs to be persisted).
func (m *Manager) migrateHLSCleanupEnabled() bool {
	if m.config.HLS.CleanupMigrated {
		return false
	}
	if m.config.HLS.CleanupEnabled {
		m.log.Info("Migrating legacy HLS cleanup default: forcing CleanupEnabled=false (admin can re-enable in System Settings)")
		m.config.HLS.CleanupEnabled = false
	}
	m.config.HLS.CleanupMigrated = true
	return true
}

// migrateStreamingBufferSize is a one-shot upgrade migration. Before the
// streaming buffer pool read Streaming.BufferSize, the field was shipped as
// 32KB-by-default but read by nothing (the pool hardcoded 1MB), so existing
// installs have 32768 persisted even though no admin explicitly chose it.
// Honoring that stale value would silently shrink streaming buffers 32x, so
// the legacy default is upgraded once to the 1MB the server actually used.
// Admins who genuinely want 32KB can set it again now that the field works.
// The bool return reports whether this load performed the migration (and the
// flag therefore needs to be persisted).
func (m *Manager) migrateStreamingBufferSize() bool {
	if m.config.Streaming.BufferSizeMigrated {
		return false
	}
	if m.config.Streaming.BufferSize == 32*1024 {
		m.log.Info("Migrating legacy streaming buffer_size default: 32KB (never honored) -> 1MB (actual prior behavior)")
		m.config.Streaming.BufferSize = 1024 * 1024
	}
	m.config.Streaming.BufferSizeMigrated = true
	return true
}

// migrateHLSConcurrentLimitAuto is a one-shot upgrade migration. Earlier builds
// shipped hls.concurrent_limit with a fixed default of 2, so existing installs
// have 2 persisted even though no admin deliberately chose it. Now that 0 means
// "auto" (scale transcode concurrency to the host CPU/GPU), flip that legacy
// default to 0 exactly once so upgraded servers use their hardware. Any other
// value — including an admin who later sets 2 again — is preserved, and the
// flag is set after the first pass so the migration never re-runs.
// The bool return reports whether this load performed the migration (and the
// flag therefore needs to be persisted).
func (m *Manager) migrateHLSConcurrentLimitAuto() bool {
	if m.config.HLS.ConcurrentLimitMigrated {
		return false
	}
	if m.config.HLS.ConcurrentLimit == 2 {
		m.log.Info("Migrating legacy HLS concurrent_limit default 2 -> 0 (auto: scale with CPU/GPU)")
		m.config.HLS.ConcurrentLimit = 0
	}
	m.config.HLS.ConcurrentLimitMigrated = true
	return true
}

// migrateThumbnailWorkerCountAuto is a one-shot upgrade migration. Earlier
// builds shipped thumbnails.worker_count with a fixed default of 4, so existing
// installs have 4 persisted even though no admin deliberately chose it. Now that
// 0 means "auto" (scale the worker pool to the host CPU), flip that legacy
// default to 0 exactly once. Any other value is preserved, and the flag is set
// after the first pass so the migration never re-runs.
// The bool return reports whether this load performed the migration (and the
// flag therefore needs to be persisted).
func (m *Manager) migrateThumbnailWorkerCountAuto() bool {
	if m.config.Thumbnails.WorkerCountMigrated {
		return false
	}
	if m.config.Thumbnails.WorkerCount == 4 {
		m.log.Info("Migrating legacy thumbnails worker_count default 4 -> 0 (auto: scale with CPU)")
		m.config.Thumbnails.WorkerCount = 0
	}
	m.config.Thumbnails.WorkerCountMigrated = true
	return true
}

// normalizeHLSScalars repairs HLS numeric fields that were persisted as zero
// or negative values by older builds (or hand-edited configs). The validator
// rejects these values outright; without this step, an existing install that
// upgrades into a stricter validator can't start until someone edits the
// config file by hand. Each field is restored to the same default the
// initializer would have set.
//
// Only fields the validator actually rejects are repaired here. Optional
// fields that may legitimately be zero (e.g. CDN base URL) are left alone.
func (m *Manager) normalizeHLSScalars() {
	if !m.config.HLS.Enabled {
		return
	}
	hls := &m.config.HLS
	defaults := defaultHLSConfig()
	type fix struct {
		field   string
		repair  func()
		broken  bool
		toValue any
	}
	fixes := []fix{
		{
			field:   "segment_duration",
			broken:  hls.SegmentDuration < 1 || hls.SegmentDuration > 60,
			repair:  func() { hls.SegmentDuration = defaults.SegmentDuration },
			toValue: defaults.SegmentDuration,
		},
		{
			field:   "playlist_length",
			broken:  hls.PlaylistLength < 1,
			repair:  func() { hls.PlaylistLength = defaults.PlaylistLength },
			toValue: defaults.PlaylistLength,
		},
		{
			// 0 is a legitimate value (auto: scale with CPU/GPU at runtime); only
			// a negative value is broken and gets restored to the default (auto).
			field:   "concurrent_limit",
			broken:  hls.ConcurrentLimit < 0,
			repair:  func() { hls.ConcurrentLimit = defaults.ConcurrentLimit },
			toValue: defaults.ConcurrentLimit,
		},
		{
			field:   "probe_timeout",
			broken:  hls.ProbeTimeout <= 0,
			repair:  func() { hls.ProbeTimeout = defaults.ProbeTimeout },
			toValue: defaults.ProbeTimeout,
		},
		{
			field:   "stale_lock_threshold",
			broken:  hls.StaleLockThreshold < time.Minute,
			repair:  func() { hls.StaleLockThreshold = defaults.StaleLockThreshold },
			toValue: defaults.StaleLockThreshold,
		},
	}
	if hls.CleanupEnabled {
		fixes = append(fixes,
			fix{
				field:   "cleanup_interval",
				broken:  hls.CleanupInterval < time.Minute,
				repair:  func() { hls.CleanupInterval = defaults.CleanupInterval },
				toValue: defaults.CleanupInterval,
			},
			fix{
				field:   "retention_minutes",
				broken:  hls.RetentionMinutes < 1,
				repair:  func() { hls.RetentionMinutes = defaults.RetentionMinutes },
				toValue: defaults.RetentionMinutes,
			},
		)
	}
	for _, f := range fixes {
		if f.broken {
			f.repair()
			m.log.Warn("hls.%s was invalid; restored default: %v", f.field, f.toValue)
		}
	}

	// Normalize hardware_accel to a known value so the encoder selector never
	// has to guess. Unknown values fall back to "auto" (probe + software
	// fallback) rather than blocking startup.
	switch hls.HardwareAccel {
	case "", "auto", "none", "nvenc", "qsv", "vaapi", "videotoolbox":
		if hls.HardwareAccel == "" {
			hls.HardwareAccel = "auto"
		}
	default:
		m.log.Warn("hls.hardware_accel %q is not recognized; falling back to \"auto\"", hls.HardwareAccel)
		hls.HardwareAccel = "auto"
	}
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
	if err := os.WriteFile(tempPath, data, 0o600); err != nil {
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
	cp.Security.TrustedProxyCIDRs = append([]string(nil), m.config.Security.TrustedProxyCIDRs...)
	if m.config.Storage.S3.Prefixes != nil {
		cp.Storage.S3.Prefixes = maps.Clone(m.config.Storage.S3.Prefixes)
	}
	// Tasks.Overrides is a map and must be cloned too, or a Get() snapshot aliases
	// the live map: callers holding the snapshot (e.g. cmd/server/main.go's
	// cfg.Get().Tasks.Overrides[...] reads) both observe later mutations and race
	// with a concurrent Update writer (concurrent map read/write -> panic).
	if m.config.Tasks.Overrides != nil {
		cp.Tasks.Overrides = maps.Clone(m.config.Tasks.Overrides)
	}
	return &cp
}

// Update updates the configuration and notifies watchers.
// Watchers are called synchronously after the lock is released so that they
// receive configs in order and can safely call m.Get() without deadlocking.
func (m *Manager) Update(updater func(*Config)) error {
	m.mu.Lock()

	originalJSON, err := json.Marshal(m.config)
	if err != nil {
		m.mu.Unlock()
		return fmt.Errorf("failed to marshal original config for backup: %w", err)
	}
	updater(m.config)
	// Sync feature toggles BEFORE validation so that module-level Enabled fields
	// reflect the new config. This matches the ordering in Load().
	m.syncFeatureToggles()
	if err := m.validate(); err != nil {
		m.rollbackFromJSON(originalJSON, err)
		m.mu.Unlock()
		return fmt.Errorf("config validation failed: %w", err)
	}
	if err := m.save(); err != nil {
		m.rollbackFromJSON(originalJSON, err)
		m.mu.Unlock()
		return err
	}
	cfg := m.getCopy()
	watchers := make([]func(*Config), len(m.watchers))
	copy(watchers, m.watchers)
	m.mu.Unlock()

	for _, w := range watchers {
		func() {
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
