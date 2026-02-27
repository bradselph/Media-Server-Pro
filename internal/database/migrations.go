package database

import (
	"context"
	"fmt"
)

// ensureSchema idempotently creates all required tables and columns.
// Safe to call on every startup:
//   - Fresh database: creates everything from scratch
//   - Existing database: skips existing tables/columns, adds any that are missing
//
// No migration tracking files or version numbers — just checks information_schema.
func (m *Module) ensureSchema(ctx context.Context) error {
	m.log.Info("Ensuring database schema...")

	// ── Step 1: Create tables (all columns included) ─────────────────────────
	// CREATE TABLE IF NOT EXISTS is a no-op when the table already exists.
	// Path columns in composite PKs are VARCHAR(500) — keeps utf8mb4 composite
	// keys under the 3072-byte InnoDB limit: (500+255)*4 = 3020 bytes.
	tables := []struct {
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

		// No FK on user_id — admin sessions use an ID that isn't in the users table.
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

		// Composite PK: (500+255)*4 = 3020 bytes — under the 3072-byte InnoDB limit.
		{"media_tags", `
			CREATE TABLE IF NOT EXISTS media_tags (
				path VARCHAR(500),
				tag  VARCHAR(255),
				PRIMARY KEY (path, tag),
				INDEX idx_tag (tag),
				FOREIGN KEY (path) REFERENCES media_metadata(path) ON DELETE CASCADE
			) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`},

		// Composite PK: (500+255)*4 = 3020 bytes.
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

		// Composite PK: (255+500)*4 = 3020 bytes.
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

		// Composite PK: (500+191)*4 = 2764 bytes.
		{"scan_reasons", `
			CREATE TABLE IF NOT EXISTS scan_reasons (
				path   VARCHAR(500),
				reason TEXT,
				PRIMARY KEY (path, reason(191)),
				FOREIGN KEY (path) REFERENCES scan_results(path) ON DELETE CASCADE
			) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`},
	}

	for _, t := range tables {
		if _, err := m.sqlDB.ExecContext(ctx, t.sql); err != nil {
			return fmt.Errorf("failed to create table %s: %w", t.name, err)
		}
		m.log.Debug("Table OK: %s", t.name)
	}

	// ── Step 2: Remove legacy FK on sessions.user_id if it exists ────────────
	// Older databases may have been created with a FK that blocks admin sessions.
	if err := m.dropConstraintIfExists(ctx, "sessions", "sessions_ibfk_1", "FOREIGN KEY"); err != nil {
		return fmt.Errorf("failed to remove sessions FK: %w", err)
	}

	// ── Step 3: Ensure all columns exist (handles pre-existing tables) ────────
	// Each entry is checked via information_schema and only ALTER'd if missing.
	columns := []struct {
		table  string
		column string
		def    string
	}{
		{"users", "watch_history", "JSON"},
		{"users", "metadata", "JSON"},
		{"user_preferences", "custom_eq_presets", "JSON"},
		{"analytics_events", "data", "JSON NULL"},
		{"media_metadata", "probe_mod_time", "TIMESTAMP NULL DEFAULT NULL"},
	}

	for _, col := range columns {
		if err := m.ensureColumn(ctx, col.table, col.column, col.def); err != nil {
			return err
		}
	}

	// ── Step 4: Ensure indexes exist ─────────────────────────────────────────
	indexes := []struct {
		table string
		index string
		sql   string
	}{
		{"analytics_events", "idx_timestamp",
			"ALTER TABLE analytics_events ADD INDEX idx_timestamp (timestamp)"},
	}

	for _, idx := range indexes {
		if err := m.ensureIndex(ctx, idx.table, idx.index, idx.sql); err != nil {
			return err
		}
	}

	m.log.Info("Database schema is up to date")
	return nil
}

// ensureColumn adds a column to a table if it doesn't already exist.
func (m *Module) ensureColumn(ctx context.Context, table, column, def string) error {
	var exists bool
	err := m.sqlDB.QueryRowContext(ctx, `
		SELECT COUNT(*) > 0
		FROM information_schema.COLUMNS
		WHERE TABLE_SCHEMA = DATABASE()
		  AND TABLE_NAME   = ?
		  AND COLUMN_NAME  = ?
	`, table, column).Scan(&exists)
	if err != nil {
		return fmt.Errorf("check column %s.%s: %w", table, column, err)
	}
	if exists {
		return nil
	}
	m.log.Info("Adding missing column %s.%s", table, column)
	_, err = m.sqlDB.ExecContext(ctx,
		fmt.Sprintf("ALTER TABLE `%s` ADD COLUMN `%s` %s", table, column, def))
	if err != nil {
		return fmt.Errorf("add column %s.%s: %w", table, column, err)
	}
	return nil
}

// ensureIndex adds an index if it doesn't already exist.
func (m *Module) ensureIndex(ctx context.Context, table, index, alterSQL string) error {
	var exists bool
	err := m.sqlDB.QueryRowContext(ctx, `
		SELECT COUNT(*) > 0
		FROM information_schema.STATISTICS
		WHERE TABLE_SCHEMA = DATABASE()
		  AND TABLE_NAME   = ?
		  AND INDEX_NAME   = ?
	`, table, index).Scan(&exists)
	if err != nil {
		return fmt.Errorf("check index %s.%s: %w", table, index, err)
	}
	if exists {
		return nil
	}
	m.log.Info("Adding missing index %s on %s", index, table)
	if _, err = m.sqlDB.ExecContext(ctx, alterSQL); err != nil {
		return fmt.Errorf("add index %s.%s: %w", table, index, err)
	}
	return nil
}

// dropConstraintIfExists drops a named constraint from a table if it exists.
func (m *Module) dropConstraintIfExists(ctx context.Context, table, constraint, constraintType string) error {
	var count int
	err := m.sqlDB.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM information_schema.TABLE_CONSTRAINTS
		WHERE TABLE_SCHEMA    = DATABASE()
		  AND TABLE_NAME      = ?
		  AND CONSTRAINT_NAME = ?
		  AND CONSTRAINT_TYPE = ?
	`, table, constraint, constraintType).Scan(&count)
	if err != nil {
		return fmt.Errorf("check constraint %s.%s: %w", table, constraint, err)
	}
	if count == 0 {
		return nil
	}
	m.log.Info("Dropping legacy constraint %s from %s", constraint, table)
	_, err = m.sqlDB.ExecContext(ctx,
		fmt.Sprintf("ALTER TABLE `%s` DROP FOREIGN KEY `%s`", table, constraint))
	return err
}
