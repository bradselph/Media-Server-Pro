package database

import (
	"context"
	"fmt"
	"regexp"
)

const (
	sqlTimestampNullDefault  = "TIMESTAMP NULL DEFAULT NULL"
	sqlBooleanNotNullDefault = "BOOLEAN NOT NULL DEFAULT TRUE"
)

// tableDefs holds CREATE TABLE SQL for ensureSchema. Package-level to avoid string-heavy function arguments.
var tableDefs = []struct {
	name string
	sql  string
}{
	{"users", `
		CREATE TABLE IF NOT EXISTS users (
			id            VARCHAR(255) PRIMARY KEY,
			username      VARCHAR(255) UNIQUE NOT NULL,
			password_hash TEXT         NOT NULL,
			salt          VARCHAR(255) NOT NULL DEFAULT '',
			email         VARCHAR(255),
			role          ENUM('admin','viewer') NOT NULL DEFAULT 'viewer',
			type          VARCHAR(50)  NOT NULL DEFAULT 'standard',
			enabled       BOOLEAN      NOT NULL DEFAULT TRUE,
			created_at    TIMESTAMP    DEFAULT CURRENT_TIMESTAMP,
			last_login    TIMESTAMP    NULL,
			storage_used  BIGINT       NOT NULL DEFAULT 0,
			active_streams INT         NOT NULL DEFAULT 0,
			watch_history JSON,
			metadata      JSON,
			INDEX idx_username (username),
			INDEX idx_email   (email)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`},
	{"user_permissions", `
		CREATE TABLE IF NOT EXISTS user_permissions (
			user_id              VARCHAR(255) PRIMARY KEY,
			can_stream           BOOLEAN NOT NULL DEFAULT TRUE,
			can_download         BOOLEAN NOT NULL DEFAULT FALSE,
			can_upload           BOOLEAN NOT NULL DEFAULT FALSE,
			can_delete           BOOLEAN NOT NULL DEFAULT FALSE,
			can_manage           BOOLEAN NOT NULL DEFAULT FALSE,
			can_view_mature      BOOLEAN NOT NULL DEFAULT FALSE,
			can_create_playlists BOOLEAN NOT NULL DEFAULT TRUE,
			FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`},
	{"user_preferences", `
		CREATE TABLE IF NOT EXISTS user_preferences (
			user_id               VARCHAR(255) PRIMARY KEY,
			theme                 VARCHAR(50)  DEFAULT 'dark',
			view_mode             VARCHAR(50)  DEFAULT 'grid',
			default_quality       VARCHAR(50)  DEFAULT 'auto',
			auto_play             BOOLEAN      DEFAULT FALSE,
			playback_speed        FLOAT        DEFAULT 1.0,
			volume                FLOAT        DEFAULT 1.0,
			show_mature           BOOLEAN      DEFAULT FALSE,
			mature_preference_set BOOLEAN      DEFAULT FALSE,
			language              VARCHAR(10)  DEFAULT 'en',
			subtitle_lang         VARCHAR(10)  DEFAULT 'en',
			equalizer_preset      VARCHAR(100),
			resume_playback       BOOLEAN      DEFAULT TRUE,
			show_analytics        BOOLEAN      DEFAULT TRUE,
			items_per_page        INT          DEFAULT 20,
			sort_by               VARCHAR(50)  DEFAULT 'date_added',
			sort_order            VARCHAR(10)  DEFAULT 'desc',
			filter_category       VARCHAR(100),
			filter_media_type     VARCHAR(50),
			custom_eq_presets     JSON,
			FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`},
	{"sessions", `
		CREATE TABLE IF NOT EXISTS sessions (
			id            VARCHAR(255) PRIMARY KEY,
			user_id       VARCHAR(255) NOT NULL,
			username      VARCHAR(255) NOT NULL,
			role          ENUM('admin','viewer') NOT NULL,
			created_at    TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			expires_at    TIMESTAMP NOT NULL,
			last_activity TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
			ip_address    VARCHAR(45),
			user_agent    TEXT,
			INDEX idx_user_id   (user_id),
			INDEX idx_expires_at (expires_at)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`},
	{"media_metadata", `
		CREATE TABLE IF NOT EXISTS media_metadata (
			path           VARCHAR(500) PRIMARY KEY,
			views          INT       DEFAULT 0,
			last_played    TIMESTAMP NULL,
			date_added     TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			is_mature      BOOLEAN   DEFAULT FALSE,
			mature_score   FLOAT     DEFAULT 0.0,
			category       VARCHAR(255),
			probe_mod_time TIMESTAMP NULL DEFAULT NULL,
			INDEX idx_category  (category),
			INDEX idx_date_added (date_added),
			INDEX idx_views     (views)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`},
	{"media_tags", `
		CREATE TABLE IF NOT EXISTS media_tags (
			path VARCHAR(500),
			tag  VARCHAR(255),
			PRIMARY KEY (path, tag),
			INDEX idx_tag (tag),
			FOREIGN KEY (path) REFERENCES media_metadata(path) ON DELETE CASCADE
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`},
	{"playback_positions", `
		CREATE TABLE IF NOT EXISTS playback_positions (
			path       VARCHAR(500),
			user_id    VARCHAR(255),
			position   FLOAT     NOT NULL,
			updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
			PRIMARY KEY (path, user_id),
			FOREIGN KEY (path)    REFERENCES media_metadata(path) ON DELETE CASCADE,
			FOREIGN KEY (user_id) REFERENCES users(id)            ON DELETE CASCADE
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`},
	{"playlists", `
		CREATE TABLE IF NOT EXISTS playlists (
			id          VARCHAR(255) PRIMARY KEY,
			user_id     VARCHAR(255) NOT NULL,
			name        VARCHAR(255) NOT NULL,
			description TEXT,
			created_at  TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			modified_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
			is_public   BOOLEAN   DEFAULT FALSE,
			cover_image VARCHAR(1024),
			INDEX idx_user_id (user_id),
			FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`},
	{"playlist_items", `
		CREATE TABLE IF NOT EXISTS playlist_items (
			playlist_id VARCHAR(255),
			media_path  VARCHAR(500),
			title       VARCHAR(255),
			position    INT       NOT NULL,
			added_at    TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			PRIMARY KEY (playlist_id, media_path),
			INDEX idx_position (playlist_id, position),
			FOREIGN KEY (playlist_id) REFERENCES playlists(id) ON DELETE CASCADE
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`},
	{"analytics_events", `
		CREATE TABLE IF NOT EXISTS analytics_events (
			id         VARCHAR(255) PRIMARY KEY,
			type       VARCHAR(100) NOT NULL,
			media_id   VARCHAR(255),
			user_id    VARCHAR(255),
			session_id VARCHAR(255),
			ip_address VARCHAR(45),
			user_agent TEXT,
			timestamp  TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			data       JSON,
			INDEX idx_type      (type),
			INDEX idx_media_id  (media_id),
			INDEX idx_user_id   (user_id),
			INDEX idx_timestamp (timestamp)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`},
	{"audit_log", `
		CREATE TABLE IF NOT EXISTS audit_log (
			id         VARCHAR(255) PRIMARY KEY,
			timestamp  TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			user_id    VARCHAR(255),
			username   VARCHAR(255) NOT NULL,
			action     VARCHAR(255) NOT NULL,
			resource   VARCHAR(1024),
			details    JSON,
			ip_address VARCHAR(45),
			success    BOOLEAN NOT NULL,
			INDEX idx_timestamp (timestamp),
			INDEX idx_user_id   (user_id),
			INDEX idx_action    (action)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`},
	{"scan_results", `
		CREATE TABLE IF NOT EXISTS scan_results (
			path            VARCHAR(500) PRIMARY KEY,
			is_mature       BOOLEAN   DEFAULT FALSE,
			confidence      FLOAT     DEFAULT 0.0,
			auto_flagged    BOOLEAN   DEFAULT FALSE,
			needs_review    BOOLEAN   DEFAULT FALSE,
			scanned_at      TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			reviewed_by     VARCHAR(255) NULL,
			reviewed_at     TIMESTAMP    NULL,
			review_decision VARCHAR(50)  NULL,
			INDEX idx_needs_review (needs_review),
			INDEX idx_scanned_at   (scanned_at)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`},
	{"scan_reasons", `
		CREATE TABLE IF NOT EXISTS scan_reasons (
			path   VARCHAR(500),
			reason TEXT,
			PRIMARY KEY (path, reason(191)),
			FOREIGN KEY (path) REFERENCES scan_results(path) ON DELETE CASCADE
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`},
	{"categorized_items", `
		CREATE TABLE IF NOT EXISTS categorized_items (
			path            VARCHAR(500) PRIMARY KEY,
			id              VARCHAR(64)  NOT NULL,
			name            VARCHAR(255) NOT NULL,
			category        VARCHAR(50)  NOT NULL DEFAULT 'uncategorized',
			confidence      FLOAT        DEFAULT 0.0,
			detected_title  VARCHAR(255),
			detected_year   INT,
			detected_season INT,
			detected_episode INT,
			detected_show   VARCHAR(255),
			detected_artist VARCHAR(255),
			detected_album  VARCHAR(255),
			categorized_at  TIMESTAMP    DEFAULT CURRENT_TIMESTAMP,
			manual_override BOOLEAN      DEFAULT FALSE,
			INDEX idx_category (category),
			INDEX idx_id       (id)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`},
	{"hls_jobs", `
		CREATE TABLE IF NOT EXISTS hls_jobs (
			id               VARCHAR(64)  PRIMARY KEY,
			media_path       VARCHAR(500) NOT NULL,
			output_dir       VARCHAR(500),
			status           VARCHAR(50)  NOT NULL DEFAULT 'pending',
			progress         FLOAT        DEFAULT 0.0,
			qualities        JSON,
			started_at       TIMESTAMP    DEFAULT CURRENT_TIMESTAMP,
			completed_at     TIMESTAMP    NULL,
			last_accessed_at TIMESTAMP    NULL,
			error_message    TEXT,
			fail_count       INT          DEFAULT 0,
			hls_url          VARCHAR(1024),
			available        BOOLEAN      DEFAULT FALSE,
			INDEX idx_media_path (media_path),
			INDEX idx_status     (status)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`},
	{"validation_results", `
		CREATE TABLE IF NOT EXISTS validation_results (
			path            VARCHAR(500) PRIMARY KEY,
			status          VARCHAR(50)  NOT NULL DEFAULT 'pending',
			validated_at    TIMESTAMP    DEFAULT CURRENT_TIMESTAMP,
			duration        FLOAT        DEFAULT 0.0,
			video_codec     VARCHAR(100),
			audio_codec     VARCHAR(100),
			width           INT          DEFAULT 0,
			height          INT          DEFAULT 0,
			bitrate         BIGINT       DEFAULT 0,
			container       VARCHAR(100),
			issues          JSON,
			error_message   TEXT,
			video_supported BOOLEAN      DEFAULT FALSE,
			audio_supported BOOLEAN      DEFAULT FALSE
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`},
	{"suggestion_profiles", `
		CREATE TABLE IF NOT EXISTS suggestion_profiles (
			user_id          VARCHAR(255) PRIMARY KEY,
			category_scores  JSON,
			type_preferences JSON,
			total_views      INT     DEFAULT 0,
			total_watch_time FLOAT   DEFAULT 0.0,
			last_updated     TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
			FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`},
	{"suggestion_view_history", `
		CREATE TABLE IF NOT EXISTS suggestion_view_history (
			user_id      VARCHAR(255),
			media_path   VARCHAR(500),
			category     VARCHAR(100),
			media_type   VARCHAR(50),
			view_count   INT       DEFAULT 0,
			total_time   FLOAT     DEFAULT 0.0,
			last_viewed  TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			completed_at TIMESTAMP NULL,
			rating       FLOAT     DEFAULT 0.0,
			PRIMARY KEY (user_id, media_path(255)),
			FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`},
	{"autodiscovery_suggestions", `
		CREATE TABLE IF NOT EXISTS autodiscovery_suggestions (
			original_path  VARCHAR(500) PRIMARY KEY,
			suggested_name VARCHAR(500),
			suggested_path VARCHAR(500),
			type           VARCHAR(50),
			confidence     FLOAT   DEFAULT 0.0,
			metadata       JSON
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`},
	{"ip_list_config", `
		CREATE TABLE IF NOT EXISTS ip_list_config (
			list_type VARCHAR(20)  PRIMARY KEY,
			name      VARCHAR(100) NOT NULL,
			enabled   BOOLEAN      DEFAULT FALSE
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`},
	{"ip_list_entries", `
		CREATE TABLE IF NOT EXISTS ip_list_entries (
			list_type  VARCHAR(20),
			ip_value   VARCHAR(100),
			comment    TEXT,
			added_at   TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			added_by   VARCHAR(255),
			expires_at TIMESTAMP NULL,
			PRIMARY KEY (list_type, ip_value),
			FOREIGN KEY (list_type) REFERENCES ip_list_config(list_type) ON DELETE CASCADE
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`},
	{"remote_cache_entries", `
		CREATE TABLE IF NOT EXISTS remote_cache_entries (
			remote_url   VARCHAR(500) PRIMARY KEY,
			local_path   VARCHAR(500),
			file_size    BIGINT    DEFAULT 0,
			content_type VARCHAR(100),
			cached_at    TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			last_access  TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			hits         INT       DEFAULT 0
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`},
	{"backup_manifests", `
		CREATE TABLE IF NOT EXISTS backup_manifests (
			id          VARCHAR(255) PRIMARY KEY,
			filename    VARCHAR(500) NOT NULL,
			created_at  TIMESTAMP    DEFAULT CURRENT_TIMESTAMP,
			size        BIGINT       DEFAULT 0,
			type        VARCHAR(50),
			description TEXT,
			files       JSON,
			errors      JSON,
			version     VARCHAR(50)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`},
	{"receiver_slaves", `
		CREATE TABLE IF NOT EXISTS receiver_slaves (
			id          VARCHAR(255) PRIMARY KEY,
			name        VARCHAR(255) NOT NULL,
			base_url    VARCHAR(500) NOT NULL,
			status      VARCHAR(50)  NOT NULL DEFAULT 'offline',
			media_count INT          DEFAULT 0,
			last_seen   TIMESTAMP    DEFAULT CURRENT_TIMESTAMP,
			created_at  TIMESTAMP    DEFAULT CURRENT_TIMESTAMP,
			INDEX idx_receiver_slaves_status (status)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`},
	{"receiver_media", `
		CREATE TABLE IF NOT EXISTS receiver_media (
			id           VARCHAR(255) PRIMARY KEY,
			slave_id     VARCHAR(255) NOT NULL,
			remote_path  VARCHAR(500) NOT NULL,
			name         VARCHAR(500) NOT NULL,
			media_type   VARCHAR(50),
			file_size    BIGINT       DEFAULT 0,
			duration     DOUBLE       DEFAULT 0,
			content_type VARCHAR(100),
			width        INT          DEFAULT 0,
			height       INT          DEFAULT 0,
			updated_at   TIMESTAMP    DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
			INDEX idx_receiver_media_slave (slave_id),
			INDEX idx_receiver_media_name (name(191))
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`},
	{"extractor_items", `
		CREATE TABLE IF NOT EXISTS extractor_items (
			id               VARCHAR(255) PRIMARY KEY,
			source_url       VARCHAR(2048) NOT NULL,
			title            VARCHAR(500)  NOT NULL,
			stream_url       VARCHAR(2048) NOT NULL,
			stream_type      VARCHAR(20)   NOT NULL DEFAULT 'hls',
			content_type     VARCHAR(100),
			quality          VARCHAR(50),
			width            INT           DEFAULT 0,
			height           INT           DEFAULT 0,
			duration         DOUBLE        DEFAULT 0,
			site             VARCHAR(255),
			detection_method VARCHAR(50),
			status           VARCHAR(50)   NOT NULL DEFAULT 'active',
			error_message    TEXT,
			added_by         VARCHAR(255),
			resolved_at      TIMESTAMP     DEFAULT CURRENT_TIMESTAMP,
			expires_at       TIMESTAMP     NULL,
			created_at       TIMESTAMP     DEFAULT CURRENT_TIMESTAMP,
			updated_at       TIMESTAMP     DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
			INDEX idx_extractor_status (status),
			INDEX idx_extractor_site (site),
			INDEX idx_extractor_title (title(191))
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`},
	{"crawler_targets", `
		CREATE TABLE IF NOT EXISTS crawler_targets (
			id           VARCHAR(255) PRIMARY KEY,
			name         VARCHAR(255) NOT NULL,
			url          VARCHAR(2048) NOT NULL,
			site         VARCHAR(255),
			enabled      TINYINT(1) NOT NULL DEFAULT 1,
			last_crawled TIMESTAMP  NULL,
			created_at   TIMESTAMP  DEFAULT CURRENT_TIMESTAMP,
			updated_at   TIMESTAMP  DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
			INDEX idx_crawler_target_enabled (enabled),
			INDEX idx_crawler_target_site (site)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`},
	{"crawler_discoveries", `
		CREATE TABLE IF NOT EXISTS crawler_discoveries (
			id               VARCHAR(255) PRIMARY KEY,
			target_id        VARCHAR(255) NOT NULL,
			page_url         VARCHAR(2048) NOT NULL,
			title            VARCHAR(500)  NOT NULL,
			stream_url       VARCHAR(2048) NOT NULL,
			stream_type      VARCHAR(20)   NOT NULL DEFAULT 'hls',
			quality          INT DEFAULT 0,
			detection_method VARCHAR(50),
			status           VARCHAR(50)   NOT NULL DEFAULT 'pending',
			reviewed_by      VARCHAR(255),
			reviewed_at      TIMESTAMP     NULL,
			discovered_at    TIMESTAMP     DEFAULT CURRENT_TIMESTAMP,
			INDEX idx_crawler_disc_target (target_id),
			INDEX idx_crawler_disc_status (status),
			FOREIGN KEY (target_id) REFERENCES crawler_targets(id) ON DELETE CASCADE
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`},
	{"receiver_duplicates", `
		CREATE TABLE IF NOT EXISTS receiver_duplicates (
			id              VARCHAR(255) PRIMARY KEY,
			fingerprint     VARCHAR(64)  NOT NULL,
			item_a_id       VARCHAR(255) NOT NULL,
			item_a_slave_id VARCHAR(255) NOT NULL,
			item_a_name     VARCHAR(500) NOT NULL,
			item_b_id       VARCHAR(255) NOT NULL,
			item_b_slave_id VARCHAR(255) NOT NULL,
			item_b_name     VARCHAR(500) NOT NULL,
			status          VARCHAR(50)  NOT NULL DEFAULT 'pending',
			resolved_by     VARCHAR(255),
			resolved_at     TIMESTAMP    NULL,
			detected_at     TIMESTAMP    DEFAULT CURRENT_TIMESTAMP,
			INDEX idx_dup_fingerprint (fingerprint),
			INDEX idx_dup_status (status)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`},
	{"user_favorites", `
		CREATE TABLE IF NOT EXISTS user_favorites (
			id         VARCHAR(255) PRIMARY KEY,
			user_id    VARCHAR(255) NOT NULL,
			media_id   VARCHAR(255) NOT NULL,
			media_path VARCHAR(500) NOT NULL,
			added_at   TIMESTAMP    DEFAULT CURRENT_TIMESTAMP,
			UNIQUE KEY uniq_fav_user_media (user_id, media_id),
			INDEX idx_fav_user (user_id)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`},
	{"media_chapters", `
		CREATE TABLE IF NOT EXISTS media_chapters (
			id         VARCHAR(36)  PRIMARY KEY,
			media_id   VARCHAR(255) NOT NULL,
			start_time FLOAT        NOT NULL DEFAULT 0,
			end_time   FLOAT        NULL,
			label      VARCHAR(255) NOT NULL,
			created_at TIMESTAMP    DEFAULT CURRENT_TIMESTAMP,
			INDEX idx_chapter_media (media_id)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`},
	{"user_api_tokens", `
		CREATE TABLE IF NOT EXISTS user_api_tokens (
			id           VARCHAR(255) PRIMARY KEY,
			user_id      VARCHAR(255) NOT NULL,
			name         VARCHAR(255) NOT NULL,
			token_hash   VARCHAR(64)  NOT NULL,
			last_used_at TIMESTAMP    NULL,
			created_at   TIMESTAMP    DEFAULT CURRENT_TIMESTAMP,
			UNIQUE KEY uniq_api_token_hash (token_hash),
			INDEX idx_api_token_user (user_id)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`},
	{"data_deletion_requests", `
		CREATE TABLE IF NOT EXISTS data_deletion_requests (
			id          VARCHAR(255) PRIMARY KEY,
			user_id     VARCHAR(255) NOT NULL,
			username    VARCHAR(255) NOT NULL,
			email       VARCHAR(255),
			reason      TEXT,
			status      VARCHAR(50)  NOT NULL DEFAULT 'pending',
			created_at  TIMESTAMP    DEFAULT CURRENT_TIMESTAMP,
			reviewed_at TIMESTAMP    NULL,
			reviewed_by VARCHAR(255),
			admin_notes TEXT,
			INDEX idx_ddr_user_id  (user_id),
			INDEX idx_ddr_status   (status),
			INDEX idx_ddr_created  (created_at)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`},
		{"smart_playlists", `
			CREATE TABLE IF NOT EXISTS smart_playlists (
				id          VARCHAR(36)  PRIMARY KEY,
				name        VARCHAR(255) NOT NULL,
				description TEXT,
				user_id     VARCHAR(255) NOT NULL,
				rules       TEXT         NOT NULL,
				created_at  TIMESTAMP    DEFAULT CURRENT_TIMESTAMP,
				updated_at  TIMESTAMP    DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
				INDEX idx_smart_playlist_user (user_id)
			) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`},
		{"auto_tag_rules", `
			CREATE TABLE IF NOT EXISTS auto_tag_rules (
				id         VARCHAR(36)  PRIMARY KEY,
				name       VARCHAR(255) NOT NULL,
				pattern    VARCHAR(500) NOT NULL,
				tags       TEXT         NOT NULL,
				priority   INT          NOT NULL DEFAULT 0,
				enabled    TINYINT(1)   NOT NULL DEFAULT 1,
				created_at TIMESTAMP    DEFAULT CURRENT_TIMESTAMP,
				updated_at TIMESTAMP    DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
				INDEX idx_auto_tag_rules_priority (priority)
			) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`},
		{"media_collections", `
			CREATE TABLE IF NOT EXISTS media_collections (
				id             VARCHAR(36)  PRIMARY KEY,
				name           VARCHAR(255) NOT NULL,
				description    TEXT,
				cover_media_id VARCHAR(36),
				created_at     TIMESTAMP    DEFAULT CURRENT_TIMESTAMP,
				updated_at     TIMESTAMP    DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
				INDEX idx_media_collections_name (name)
			) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`},
		{"media_collection_items", `
			CREATE TABLE IF NOT EXISTS media_collection_items (
				collection_id VARCHAR(36) NOT NULL,
				media_id      VARCHAR(36) NOT NULL,
				position      INT         NOT NULL DEFAULT 0,
				added_at      TIMESTAMP   DEFAULT CURRENT_TIMESTAMP,
				PRIMARY KEY (collection_id, media_id),
				INDEX idx_collection_items_media (media_id),
				INDEX idx_collection_items_position (collection_id, position)
			) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`},
	}

// ensureSchema idempotently creates all required tables and columns.
// Safe to call on every startup:
//   - Fresh database: creates everything from scratch
//   - Existing database: skips existing tables/columns, adds any that are missing
//
// No migration tracking files or version numbers — just checks information_schema.
func (m *Module) ensureSchema(ctx context.Context) error {
	m.log.Info("Ensuring database schema...")
	if err := m.createTables(ctx); err != nil {
		return err
	}
	if err := m.ensureSchemaColumns(ctx); err != nil {
		return err
	}
	if err := m.migratePlaylistItemsPK(ctx); err != nil {
		return err
	}
	if err := m.ensureSchemaIndexes(ctx); err != nil {
		return err
	}
	if err := m.ensureSchemaForeignKeys(ctx); err != nil {
		return err
	}
	if err := m.backfillMediaDuration(ctx); err != nil {
		// Non-fatal: backfill failure should not block startup.
		m.log.Warn("Duration backfill from validation_results failed (non-fatal): %v", err)
	}
	m.log.Info("Database schema is up to date")
	return nil
}

// backfillMediaDuration copies duration values from validation_results into media_metadata
// for all rows where media_metadata.duration is still 0 but validation_results.duration > 0.
// This is a one-pass operation; rows already populated are skipped by the WHERE clause.
func (m *Module) backfillMediaDuration(ctx context.Context) error {
	result, err := m.sqlDB.ExecContext(ctx, `
		UPDATE media_metadata mm
		JOIN validation_results vr ON mm.path = vr.path
		SET mm.duration = vr.duration
		WHERE mm.duration = 0 AND vr.duration > 0
	`)
	if err != nil {
		return fmt.Errorf("backfill duration: %w", err)
	}
	rows, _ := result.RowsAffected()
	if rows > 0 {
		m.log.Info("Backfilled duration for %d media items from validation_results", rows)
	}
	return nil
}

// createTables runs CREATE TABLE IF NOT EXISTS for all schema tables.
// Path columns in composite PKs are VARCHAR(500) to keep utf8mb4 keys under InnoDB 3072-byte limit.
func (m *Module) createTables(ctx context.Context) error {
	tables := m.tableDefinitions()
	for _, t := range tables {
		if _, err := m.sqlDB.ExecContext(ctx, t.sql); err != nil {
			return fmt.Errorf("failed to create table %s: %w", t.name, err)
		}
		m.log.Debug("Table OK: %s", t.name)
	}
	return nil
}

// tableDefinitions returns the list of table names and CREATE TABLE SQL (from package-level tableDefs).
func (m *Module) tableDefinitions() []struct {
	name string
	sql  string
} {
	return tableDefs
}

// ensureSchemaColumns adds any missing columns to existing tables (checked via information_schema).
func (m *Module) ensureSchemaColumns(ctx context.Context) error {
	columns := []struct {
		table  string
		column string
		def    string
	}{
		{"users", "watch_history", "JSON"},
		{"users", "metadata", "JSON"},
		{"users", "previous_last_login", sqlTimestampNullDefault},
		{"user_preferences", "custom_eq_presets", "JSON"},
		{"user_preferences", "show_continue_watching", sqlBooleanNotNullDefault},
		{"user_preferences", "show_recommended", sqlBooleanNotNullDefault},
		{"user_preferences", "show_trending", sqlBooleanNotNullDefault},
		{"user_preferences", "skip_interval", "INT NOT NULL DEFAULT 10"},
		{"user_preferences", "shuffle_enabled", "BOOLEAN NOT NULL DEFAULT FALSE"},
		{"user_preferences", "show_buffer_bar", "BOOLEAN NOT NULL DEFAULT TRUE"},
		{"user_preferences", "download_prompt", "BOOLEAN NOT NULL DEFAULT TRUE"},
		{"analytics_events", "data", "JSON NULL"},
		{"media_metadata", "duration", "DOUBLE NOT NULL DEFAULT 0"},
		{"media_metadata", "probe_mod_time", sqlTimestampNullDefault},
		{"media_metadata", "stable_id", "VARCHAR(36) NULL"},
		{"media_metadata", "content_fingerprint", "VARCHAR(64) NULL"},
		{"media_metadata", "blur_hash", "VARCHAR(100) NULL"},
		{"receiver_media", "content_fingerprint", "VARCHAR(64) NULL"},
		{"hls_jobs", "last_accessed_at", "TIMESTAMP NULL"},
		{"user_api_tokens", "expires_at", sqlTimestampNullDefault},
		// PlaylistItem schema alignment: GORM model expects id and media_id columns
		{"playlist_items", "id", "VARCHAR(255) NOT NULL DEFAULT '' FIRST"},
		{"playlist_items", "media_id", "VARCHAR(255) NOT NULL DEFAULT '' AFTER playlist_id"},
	}
	for _, col := range columns {
		if err := m.ensureColumn(ctx, col.table, col.column, col.def); err != nil {
			return err
		}
	}
	return nil
}

// ensureSchemaIndexes adds any missing indexes (checked via information_schema).
func (m *Module) ensureSchemaIndexes(ctx context.Context) error {
	indexes := []struct {
		table string
		index string
		sql   string
	}{
		{"analytics_events", "idx_timestamp",
			"ALTER TABLE analytics_events ADD INDEX idx_timestamp (timestamp)"},
		{"media_metadata", "idx_stable_id",
			"ALTER TABLE media_metadata ADD UNIQUE INDEX idx_stable_id (stable_id)"},
		{"media_metadata", "idx_content_fingerprint",
			"ALTER TABLE media_metadata ADD INDEX idx_content_fingerprint (content_fingerprint)"},
		{"receiver_media", "idx_receiver_media_fingerprint",
			"ALTER TABLE receiver_media ADD INDEX idx_receiver_media_fingerprint (content_fingerprint)"},
		// Prevent duplicate items in the same playlist after PK migration from
		// composite (playlist_id, media_path) to single-column (id).
		{"playlist_items", "uniq_playlist_media",
			"ALTER TABLE playlist_items ADD UNIQUE INDEX uniq_playlist_media (playlist_id, media_id)"},
	}
	for _, idx := range indexes {
		if err := m.ensureIndex(ctx, idx.table, idx.index, idx.sql); err != nil {
			return err
		}
	}
	return nil
}

// schemaObjectSpec holds parameters for ensureSchemaObject to avoid string-heavy function arguments.
type schemaObjectSpec struct {
	table       string
	name        string
	checkQuery  string
	checkArgs   []interface{}
	apply       func() error
	logMsg      string
	errCheckFmt string
	errApplyFmt string
}

// ensureSchemaObject checks existence via checkQuery (one bool), then if missing runs apply() and logs logMsg.
func (m *Module) ensureSchemaObject(ctx context.Context, spec schemaObjectSpec) error {
	var exists bool
	if err := m.sqlDB.QueryRowContext(ctx, spec.checkQuery, spec.checkArgs...).Scan(&exists); err != nil {
		return fmt.Errorf(spec.errCheckFmt, spec.table, spec.name, err)
	}
	if exists {
		return nil
	}
	m.log.Info("%s", spec.logMsg)
	if err := spec.apply(); err != nil {
		return fmt.Errorf(spec.errApplyFmt, spec.table, spec.name, err)
	}
	return nil
}

// ensureSchemaObjectWithKind builds a schemaObjectSpec with standard error formats and runs ensureSchemaObject.
func (m *Module) ensureSchemaObjectWithKind(ctx context.Context, kind, table, name, checkQuery string, checkArgs []interface{}, apply func() error, logMsg string) error {
	return m.ensureSchemaObject(ctx, schemaObjectSpec{
		table:       table,
		name:        name,
		checkQuery:  checkQuery,
		checkArgs:   checkArgs,
		apply:       apply,
		logMsg:      logMsg,
		errCheckFmt: "check " + kind + " %s.%s: %w",
		errApplyFmt: "add " + kind + " %s.%s: %w",
	})
}

// validIdent matches MySQL identifier characters: alphanumeric and underscore.
// Used to prevent SQL injection if table/column/index names ever come from external input.
var validIdent = regexp.MustCompile(`^[a-zA-Z0-9_]+$`)

// ensureColumn adds a column to a table if it doesn't already exist.
func (m *Module) ensureColumn(ctx context.Context, table, column, def string) error {
	if !validIdent.MatchString(table) || !validIdent.MatchString(column) {
		return fmt.Errorf("invalid table or column name: %q.%q", table, column)
	}
	return m.ensureSchemaObjectWithKind(ctx, "column", table, column,
		`SELECT COUNT(*) > 0 FROM information_schema.COLUMNS WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = ? AND COLUMN_NAME = ?`,
		[]interface{}{table, column},
		func() error {
			_, err := m.sqlDB.ExecContext(ctx, fmt.Sprintf("ALTER TABLE `%s` ADD COLUMN `%s` %s", table, column, def))
			return err
		},
		fmt.Sprintf("Adding missing column %s.%s", table, column),
	)
}

// ensureIndex adds an index if it doesn't already exist.
func (m *Module) ensureIndex(ctx context.Context, table, index, alterSQL string) error {
	if !validIdent.MatchString(table) || !validIdent.MatchString(index) {
		return fmt.Errorf("invalid table or index name: %q.%q", table, index)
	}
	return m.ensureSchemaObjectWithKind(ctx, "index", table, index,
		`SELECT COUNT(*) > 0 FROM information_schema.STATISTICS WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = ? AND INDEX_NAME = ?`,
		[]interface{}{table, index},
		func() error {
			_, err := m.sqlDB.ExecContext(ctx, alterSQL)
			return err
		},
		fmt.Sprintf("Adding missing index %s on %s", index, table),
	)
}

// ensureSchemaForeignKeys adds missing FK constraints for existing databases.
// Orphaned rows are cleaned first so that the FK addition cannot fail due to
// referential violations on data that pre-dates the constraint.
func (m *Module) ensureSchemaForeignKeys(ctx context.Context) error {
	type fkSpec struct {
		table, constraint, cleanupSQL, alterSQL string
	}
	fks := []fkSpec{
		{
			table:      "suggestion_profiles",
			constraint: "fk_suggestion_profiles_user",
			cleanupSQL: "DELETE FROM suggestion_profiles WHERE user_id NOT IN (SELECT id FROM users)",
			alterSQL:   "ALTER TABLE suggestion_profiles ADD CONSTRAINT fk_suggestion_profiles_user FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE",
		},
		{
			table:      "suggestion_view_history",
			constraint: "fk_suggestion_view_history_user",
			cleanupSQL: "DELETE FROM suggestion_view_history WHERE user_id NOT IN (SELECT id FROM users)",
			alterSQL:   "ALTER TABLE suggestion_view_history ADD CONSTRAINT fk_suggestion_view_history_user FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE",
		},
		{
			table:      "receiver_media",
			constraint: "fk_receiver_media_slave",
			cleanupSQL: "DELETE FROM receiver_media WHERE slave_id NOT IN (SELECT id FROM receiver_slaves)",
			alterSQL:   "ALTER TABLE receiver_media ADD CONSTRAINT fk_receiver_media_slave FOREIGN KEY (slave_id) REFERENCES receiver_slaves(id) ON DELETE CASCADE",
		},
		{
			table:      "user_favorites",
			constraint: "fk_user_favorites_user",
			cleanupSQL: "DELETE FROM user_favorites WHERE user_id NOT IN (SELECT id FROM users)",
			alterSQL:   "ALTER TABLE user_favorites ADD CONSTRAINT fk_user_favorites_user FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE",
		},
		{
			table:      "user_api_tokens",
			constraint: "fk_user_api_tokens_user",
			cleanupSQL: "DELETE FROM user_api_tokens WHERE user_id NOT IN (SELECT id FROM users)",
			alterSQL:   "ALTER TABLE user_api_tokens ADD CONSTRAINT fk_user_api_tokens_user FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE",
		},
		{
			table:      "hls_jobs",
			constraint: "fk_hls_jobs_media",
			cleanupSQL: "DELETE FROM hls_jobs WHERE media_path NOT IN (SELECT path FROM media_metadata)",
			alterSQL:   "ALTER TABLE hls_jobs ADD CONSTRAINT fk_hls_jobs_media FOREIGN KEY (media_path) REFERENCES media_metadata(path) ON DELETE CASCADE",
		},
		{
			table:      "playlist_items",
			constraint: "fk_playlist_items_media",
			cleanupSQL: "DELETE FROM playlist_items WHERE media_path != '' AND media_path NOT IN (SELECT path FROM media_metadata)",
			alterSQL:   "ALTER TABLE playlist_items ADD CONSTRAINT fk_playlist_items_media FOREIGN KEY (media_path) REFERENCES media_metadata(path) ON DELETE CASCADE",
		},
		{
			table:      "user_favorites",
			constraint: "fk_user_favorites_media",
			cleanupSQL: "DELETE FROM user_favorites WHERE media_path NOT IN (SELECT path FROM media_metadata)",
			alterSQL:   "ALTER TABLE user_favorites ADD CONSTRAINT fk_user_favorites_media FOREIGN KEY (media_path) REFERENCES media_metadata(path) ON DELETE CASCADE",
		},
	}
	for _, fk := range fks {
		if err := m.ensureForeignKey(ctx, fk.table, fk.constraint, fk.cleanupSQL, fk.alterSQL); err != nil {
			return err
		}
	}
	return nil
}

// ensureForeignKey checks whether a named FK constraint exists. If not, it first
// removes any orphaned rows (to avoid constraint-violation failures) then adds the FK.
func (m *Module) ensureForeignKey(ctx context.Context, table, constraint, cleanupSQL, alterSQL string) error {
	if !validIdent.MatchString(table) || !validIdent.MatchString(constraint) {
		return fmt.Errorf("invalid table or constraint name: %q.%q", table, constraint)
	}
	var exists bool
	if err := m.sqlDB.QueryRowContext(ctx, `
		SELECT COUNT(*) > 0 FROM information_schema.TABLE_CONSTRAINTS
		WHERE TABLE_SCHEMA    = DATABASE()
		  AND TABLE_NAME      = ?
		  AND CONSTRAINT_NAME = ?
		  AND CONSTRAINT_TYPE = 'FOREIGN KEY'
	`, table, constraint).Scan(&exists); err != nil {
		return fmt.Errorf("check FK %s.%s: %w", table, constraint, err)
	}
	if exists {
		return nil
	}
	// Purge orphaned rows so the FK addition cannot fail with a constraint violation.
	if cleanupSQL != "" {
		if result, err := m.sqlDB.ExecContext(ctx, cleanupSQL); err != nil {
			m.log.Warn("FK pre-cleanup failed for %s.%s: %v", table, constraint, err)
		} else if n, _ := result.RowsAffected(); n > 0 {
			m.log.Info("Removed %d orphaned rows from %s before adding FK %s", n, table, constraint)
		}
	}
	m.log.Info("Adding missing FK %s on %s", constraint, table)
	if _, err := m.sqlDB.ExecContext(ctx, alterSQL); err != nil {
		return fmt.Errorf("add FK %s.%s: %w", table, constraint, err)
	}
	return nil
}

// migratePlaylistItemsPK migrates playlist_items from composite PK (playlist_id, media_path)
// to single-column PK (id). This aligns the DB schema with the GORM model which expects
// an id column as primary key and uses id for RemoveItem/UpdateItem queries.
func (m *Module) migratePlaylistItemsPK(ctx context.Context) error {
	// Check if the id column is already the primary key
	var pkColumn string
	err := m.sqlDB.QueryRowContext(ctx, `
		SELECT COLUMN_NAME FROM information_schema.KEY_COLUMN_USAGE
		WHERE TABLE_SCHEMA = DATABASE()
		  AND TABLE_NAME = 'playlist_items'
		  AND CONSTRAINT_NAME = 'PRIMARY'
		ORDER BY ORDINAL_POSITION LIMIT 1
	`).Scan(&pkColumn)
	if err != nil {
		// Table might not exist yet (fresh install) — skip
		return nil
	}
	if pkColumn == "id" {
		return nil // Already migrated
	}

	m.log.Info("Migrating playlist_items PK from composite to id column")

	// Wrap both steps in a transaction. Note: MySQL DDL statements (ALTER TABLE)
	// cause an implicit commit regardless of the active transaction, so the
	// UPDATE step cannot be atomically rolled back if ALTER TABLE fails. In
	// practice this is safe because the UUID population is idempotent — a
	// failed or interrupted migration can be retried; rows with existing UUIDs
	// are skipped by the WHERE clause.
	tx, err := m.sqlDB.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin playlist_items PK migration tx: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	// Populate any empty id values with UUIDs
	_, err = tx.ExecContext(ctx, `UPDATE playlist_items SET id = UUID() WHERE id = '' OR id IS NULL`)
	if err != nil {
		return fmt.Errorf("populate playlist_items IDs: %w", err)
	}

	// Drop old composite PK and add new single-column PK
	_, err = tx.ExecContext(ctx, `ALTER TABLE playlist_items DROP PRIMARY KEY, ADD PRIMARY KEY (id)`)
	if err != nil {
		return fmt.Errorf("migrate playlist_items PK: %w", err)
	}

	if err = tx.Commit(); err != nil {
		return fmt.Errorf("commit playlist_items PK migration: %w", err)
	}

	m.log.Info("playlist_items PK migration complete")
	return nil
}
