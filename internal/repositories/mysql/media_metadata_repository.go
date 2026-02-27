package mysql

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"media-server-pro/internal/logger"
	"media-server-pro/internal/repositories"
)

// MediaMetadataRepository implements MySQL storage for media metadata
type MediaMetadataRepository struct {
	db  *sql.DB
	log *logger.Logger
}

// NewMediaMetadataRepository creates a new MySQL media metadata repository
func NewMediaMetadataRepository(db *sql.DB) repositories.MediaMetadataRepository {
	return &MediaMetadataRepository{
		db:  db,
		log: logger.New("media-metadata-repo"),
	}
}

// Upsert inserts or updates media metadata
func (r *MediaMetadataRepository) Upsert(ctx context.Context, path string, metadata *repositories.MediaMetadata) error {
	// Begin transaction for atomic save of metadata + tags
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() {
		if err := tx.Rollback(); err != nil && err != sql.ErrTxDone {
			r.log.Warn("Failed to rollback transaction: %v", err)
		}
	}()

	// Handle nullable LastPlayed field - parse RFC3339 string to time.Time for TIMESTAMP column
	var lastPlayedValue sql.NullTime
	if metadata.LastPlayed != nil {
		if t, err := time.Parse(time.RFC3339, *metadata.LastPlayed); err == nil {
			lastPlayedValue = sql.NullTime{Time: t, Valid: true}
		}
	}

	// Convert DateAdded from RFC3339 to time.Time for MySQL
	dateAdded, err := parseTimeToMySQL(metadata.DateAdded)
	if err != nil {
		r.log.Warn("Failed to parse date_added, using NOW(): %v", err)
		dateAdded = time.Now()
	}

	// Nullable probe_mod_time
	var probeModTime sql.NullTime
	if metadata.ProbeModTime != nil && !metadata.ProbeModTime.IsZero() {
		probeModTime = sql.NullTime{Time: *metadata.ProbeModTime, Valid: true}
	}

	// Upsert metadata (without tags column)
	_, err = tx.ExecContext(ctx, `
		INSERT INTO media_metadata
			(path, views, last_played, date_added, is_mature, mature_score, category, probe_mod_time)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON DUPLICATE KEY UPDATE
			views = VALUES(views),
			last_played = VALUES(last_played),
			is_mature = VALUES(is_mature),
			mature_score = VALUES(mature_score),
			category = VALUES(category),
			probe_mod_time = VALUES(probe_mod_time)
	`,
		path,
		metadata.Views,
		lastPlayedValue,
		dateAdded,
		metadata.IsMature,
		metadata.MatureScore,
		metadata.Category,
		probeModTime,
	)

	if err != nil {
		return fmt.Errorf("failed to upsert media metadata: %w", err)
	}

	// Delete old tags and insert new ones
	_, err = tx.ExecContext(ctx, "DELETE FROM media_tags WHERE path = ?", path)
	if err != nil {
		return fmt.Errorf("failed to delete old tags: %w", err)
	}

	// Insert new tags
	if len(metadata.Tags) > 0 {
		for _, tag := range metadata.Tags {
			_, err = tx.ExecContext(ctx,
				"INSERT INTO media_tags (path, tag) VALUES (?, ?)",
				path, tag)
			if err != nil {
				return fmt.Errorf("failed to insert tag: %w", err)
			}
		}
	}

	// Commit transaction
	if err = tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	r.log.Debug("Upserted media metadata for: %s", path)
	return nil
}

// Get retrieves media metadata by path
func (r *MediaMetadataRepository) Get(ctx context.Context, path string) (*repositories.MediaMetadata, error) {
	metadata := &repositories.MediaMetadata{Path: path}

	var lastPlayed sql.NullTime
	var dateAdded time.Time

	var probeModTime sql.NullTime
	err := r.db.QueryRowContext(ctx, `
		SELECT views, last_played, date_added, is_mature, mature_score, category, probe_mod_time
		FROM media_metadata
		WHERE path = ?
	`, path).Scan(
		&metadata.Views,
		&lastPlayed,
		&dateAdded,
		&metadata.IsMature,
		&metadata.MatureScore,
		&metadata.Category,
		&probeModTime,
	)

	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("media metadata not found: %s", path)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to query media metadata: %w", err)
	}

	// Format date_added as RFC3339 for consistent cross-layer handling
	metadata.DateAdded = dateAdded.Format(time.RFC3339)

	// Set nullable fields
	if lastPlayed.Valid {
		formatted := lastPlayed.Time.Format(time.RFC3339)
		metadata.LastPlayed = &formatted
	}
	if probeModTime.Valid {
		t := probeModTime.Time
		metadata.ProbeModTime = &t
	}

	// Get tags from media_tags table
	rows, err := r.db.QueryContext(ctx,
		"SELECT tag FROM media_tags WHERE path = ?", path)
	if err != nil {
		return nil, fmt.Errorf("failed to query tags: %w", err)
	}
	defer func() {
		if err := rows.Close(); err != nil {
			r.log.Warn("Failed to close rows: %v", err)
		}
	}()

	metadata.Tags = []string{}
	for rows.Next() {
		var tag string
		if err := rows.Scan(&tag); err != nil {
			return nil, fmt.Errorf("failed to scan tag: %w", err)
		}
		metadata.Tags = append(metadata.Tags, tag)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to iterate tag rows: %w", err)
	}

	return metadata, nil
}

// Delete removes media metadata
func (r *MediaMetadataRepository) Delete(ctx context.Context, path string) error {
	result, err := r.db.ExecContext(ctx, "DELETE FROM media_metadata WHERE path = ?", path)
	if err != nil {
		return fmt.Errorf("failed to delete media metadata: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rowsAffected == 0 {
		return fmt.Errorf("media metadata not found: %s", path)
	}

	r.log.Debug("Deleted media metadata for: %s", path)
	return nil
}

// List retrieves all media metadata.
// Uses two bulk queries (metadata + all tags) instead of N+1 queries to avoid
// the per-row round-trip cost on remote MySQL instances.
func (r *MediaMetadataRepository) List(ctx context.Context) (map[string]*repositories.MediaMetadata, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT path, views, last_played, date_added, is_mature, mature_score, category, probe_mod_time
		FROM media_metadata
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to query media metadata: %w", err)
	}
	defer func() {
		if err := rows.Close(); err != nil {
			r.log.Warn("Failed to close rows: %v", err)
		}
	}()

	results := make(map[string]*repositories.MediaMetadata)
	for rows.Next() {
		metadata := &repositories.MediaMetadata{}
		var lastPlayed sql.NullTime
		var dateAdded time.Time
		var probeModTime sql.NullTime

		err := rows.Scan(
			&metadata.Path,
			&metadata.Views,
			&lastPlayed,
			&dateAdded,
			&metadata.IsMature,
			&metadata.MatureScore,
			&metadata.Category,
			&probeModTime,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan row: %w", err)
		}

		// Format date_added as RFC3339 for consistent cross-layer handling
		metadata.DateAdded = dateAdded.Format(time.RFC3339)

		// Set nullable fields
		if lastPlayed.Valid {
			formatted := lastPlayed.Time.Format(time.RFC3339)
			metadata.LastPlayed = &formatted
		}
		if probeModTime.Valid {
			t := probeModTime.Time
			metadata.ProbeModTime = &t
		}

		metadata.Tags = []string{} // initialised empty; populated by batch tag query below
		results[metadata.Path] = metadata
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to iterate media metadata rows: %w", err)
	}

	// Batch-load all tags in a single query to eliminate N+1 round trips.
	// With 261 rows on a remote MySQL this previously cost ~52 s (261 × ~200 ms).
	// Two queries total, regardless of row count.
	if len(results) > 0 {
		tagRows, err := r.db.QueryContext(ctx, "SELECT path, tag FROM media_tags")
		if err != nil {
			r.log.Warn("Failed to batch-load tags, tags will be empty: %v", err)
		} else {
			defer func() {
				if err := tagRows.Close(); err != nil {
					r.log.Warn("Failed to close tag rows: %v", err)
				}
			}()
			for tagRows.Next() {
				var path, tag string
				if err := tagRows.Scan(&path, &tag); err != nil {
					r.log.Warn("Failed to scan tag row: %v", err)
					continue
				}
				if meta, ok := results[path]; ok {
					meta.Tags = append(meta.Tags, tag)
				}
			}
			if err := tagRows.Err(); err != nil {
				r.log.Warn("Failed to iterate tag rows: %v", err)
			}
		}
	}

	return results, nil
}

// IncrementViews increments the view count for a media item
func (r *MediaMetadataRepository) IncrementViews(ctx context.Context, path string) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO media_metadata (path, views, date_added)
		VALUES (?, 1, NOW())
		ON DUPLICATE KEY UPDATE views = views + 1, last_played = NOW()
	`, path)

	if err != nil {
		return fmt.Errorf("failed to increment views: %w", err)
	}

	r.log.Debug("Incremented views for: %s", path)
	return nil
}

// UpdatePlaybackPosition updates the playback position for a user
func (r *MediaMetadataRepository) UpdatePlaybackPosition(ctx context.Context, path, userID string, position float64) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO playback_positions (path, user_id, position, updated_at)
		VALUES (?, ?, ?, NOW())
		ON DUPLICATE KEY UPDATE position = VALUES(position), updated_at = VALUES(updated_at)
	`, path, userID, position)

	if err != nil {
		return fmt.Errorf("failed to update playback position: %w", err)
	}

	r.log.Debug("Updated playback position for %s (user: %s, position: %.2f)", path, userID, position)
	return nil
}

// GetPlaybackPosition retrieves the playback position for a user
func (r *MediaMetadataRepository) GetPlaybackPosition(ctx context.Context, path, userID string) (float64, error) {
	var position float64

	err := r.db.QueryRowContext(ctx, `
		SELECT position
		FROM playback_positions
		WHERE path = ? AND user_id = ?
	`, path, userID).Scan(&position)

	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil // No position stored, return 0
	}
	if err != nil {
		return 0, fmt.Errorf("failed to query playback position: %w", err)
	}

	return position, nil
}
