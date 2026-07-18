package config

// applyEnvOverrides applies every environment override (infrastructure +
// tunable). It is used only when seeding a brand-new config.json or during the
// one-shot EnvSeedMigrated upgrade. On normal loads of an existing config only
// applyInfraEnvOverrides runs — see Manager.Load.
func (m *Manager) applyEnvOverrides() {
	m.applyInfraEnvOverrides()
	m.applySeededInfraEnvOverrides()
	m.applyTunableEnvOverrides()
}

// applySeededInfraEnvOverrides applies the UI-editable infra sections (server,
// logging, updater). These SEED config.json (fresh install / one-shot migration)
// but are then owned by config.json / the admin UI — an admin change (e.g. the
// updater branch) persists across restarts instead of being re-clobbered by the
// env var. Guarded by InfraOwnershipMigrated in Load (see the comment there).
func (m *Manager) applySeededInfraEnvOverrides() {
	m.applyServerEnvOverrides()
	m.applyLoggingEnvOverrides()
	m.applyUpdaterEnvOverrides()
}

// applyInfraEnvOverrides applies environment variables that must always reflect
// the deployment environment, on every load. These are owned by the
// orchestration layer (Docker/systemd/deploy.sh), not the admin UI:
//   - server bind address, port, TLS, timeouts
//   - storage directories (Docker forces /data/* paths)
//   - log level
//   - database connection + credentials
//   - object-storage (S3) endpoint + credentials
//   - updater branch / method (deploy.sh controls which branch ships)
//   - admin bootstrap username / password (operator credential reset)
// applyInfraEnvOverrides applies env vars that ALWAYS win on every load — data
// paths and credentials owned by the orchestration layer, not the admin UI.
// (server/logging/updater moved to applySeededInfraEnvOverrides so the admin UI
// can own them.)
func (m *Manager) applyInfraEnvOverrides() {
	m.applyDirectoryEnvOverrides()
	m.applyDatabaseEnvOverrides()
	m.applyStorageEnvOverrides()
	m.applyAdminEnvOverrides()
}

// applyTunableEnvOverrides applies environment variables for runtime-tunable
// settings. These are applied ONLY when seeding a fresh config.json (or once,
// during the EnvSeedMigrated upgrade). Once config.json exists the admin UI /
// config.json is authoritative for these settings and the env vars are ignored,
// so a value changed in the UI is no longer silently reverted on restart.
func (m *Manager) applyTunableEnvOverrides() {
	m.applyAuthEnvOverrides()
	m.applySecurityEnvOverrides()
	m.applyStreamingEnvOverrides()
	m.applyDownloadEnvOverrides()
	m.applyHLSEnvOverrides()
	m.applyThumbnailsEnvOverrides()
	m.applyAnalyticsEnvOverrides()
	m.applyUploadsEnvOverrides()
	m.applyFeatureEnvOverrides()
	m.applyBackupEnvOverrides()
	m.applyMatureScannerEnvOverrides()
	m.applyHuggingFaceEnvOverrides()
	m.applyRemoteMediaEnvOverrides()
	m.applyReceiverEnvOverrides()
	m.applyFollowerEnvOverrides()
	m.applyExtractorEnvOverrides()
	m.applyAgeGateEnvOverrides()
	m.applyDownloaderEnvOverrides()
	m.applyUIEnvOverrides()
	m.applyHubEnvOverrides()
}

// applyHubEnvOverrides applies environment overrides for the BETA Hub feature.
// The enable flag is handled by applyFeatureEnvOverrides (FEATURE_HUB); this
// covers the catalog CSV path and page size for easy Docker configuration.
func (m *Manager) applyHubEnvOverrides() {
	if val := envGetStr("HUB_CSV_PATH"); val != "" {
		m.config.Hub.CSVPath = val
	}
	if val := envGetStr("HUB_SOURCE_URL"); val != "" {
		m.config.Hub.SourceURL = val
	}
	if val := envGetStr("HUB_WORK_DIR"); val != "" {
		m.config.Hub.WorkDir = val
	}
	if val, found := envGetBool("HUB_AUTO_IMPORT"); found {
		m.config.Hub.AutoImport = val
	}
	if val, ok := envGetInt("HUB_PAGE_SIZE"); ok && val > 0 {
		m.config.Hub.PageSize = val
	}
}
