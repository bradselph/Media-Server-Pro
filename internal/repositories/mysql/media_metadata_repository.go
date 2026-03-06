package mysql

import (
	"context"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"media-server-pro/internal/repositories"
)

// mediaMetadataRow maps to the media_metadata table.
type mediaMetadataRow struct {
	Path               string     `gorm:"column:path;primaryKey"`
	StableID           string     `gorm:"column:stable_id"`
	ContentFingerprint string     `gorm:"column:content_fingerprint"`
	Views              int        `gorm:"column:views"`
	LastPlayed         *time.Time `gorm:"column:last_played"`
	DateAdded          time.Time  `gorm:"column:date_added"`
	IsMature           bool       `gorm:"column:is_mature"`
	MatureScore        float64    `gorm:"column:mature_score"`
	Category           string     `gorm:"column:category"`
	ProbeModTime       *time.Time `gorm:"column:probe_mod_time"`
}

func (mediaMetadataRow) TableName() string { return "media_metadata" }

// mediaTagRow maps to the media_tags table.
type mediaTagRow struct {
	Path string `gorm:"column:path;primaryKey"`
	Tag  string `gorm:"column:tag;primaryKey"`
}

func (mediaTagRow) TableName() string { return "media_tags" }

// playbackPositionRow maps to the playback_positions table.
type playbackPositionRow struct {
	Path      string    `gorm:"column:path;primaryKey"`
	UserID    string    `gorm:"column:user_id;primaryKey"`
	Position  float64   `gorm:"column:position"`
	UpdatedAt time.Time `gorm:"column:updated_at"`
}

func (playbackPositionRow) TableName() string { return "playback_positions" }

// MediaMetadataRepository implements MySQL storage for media metadata using GORM
type MediaMetadataRepository struct {
	db *gorm.DB
}

// NewMediaMetadataRepository creates a new GORM-backed media metadata repository
func NewMediaMetadataRepository(db *gorm.DB) repositories.MediaMetadataRepository {
	return &MediaMetadataRepository{db: db}
}

// Upsert inserts or updates media metadata
func (r *MediaMetadataRepository) Upsert(ctx context.Context, path string, metadata *repositories.MediaMetadata) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Parse LastPlayed
		var lastPlayed *time.Time
		if metadata.LastPlayed != nil {
			if t, err := time.Parse(time.RFC3339, *metadata.LastPlayed); err == nil {
				lastPlayed = &t
			}
		}

		// Parse DateAdded
		dateAdded, err := time.Parse(time.RFC3339, metadata.DateAdded)
		if err != nil {
			dateAdded = time.Now()
		}

		// Handle ProbeModTime — nil or zero → nil
		var probeModTime *time.Time
		if metadata.ProbeModTime != nil && !metadata.ProbeModTime.IsZero() {
			probeModTime = metadata.ProbeModTime
		}

		row := mediaMetadataRow{
			Path:               path,
			StableID:           metadata.StableID,
			ContentFingerprint: metadata.ContentFingerprint,
			Views:              metadata.Views,
			LastPlayed:         lastPlayed,
			DateAdded:          dateAdded,
			IsMature:           metadata.IsMature,
			MatureScore:        metadata.MatureScore,
			Category:           metadata.Category,
			ProbeModTime:       probeModTime,
		}

		// On conflict: always update operational fields but only set stable_id
		// if the existing row doesn't already have one (preserve existing UUIDs).
		if err := tx.Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "path"}},
			DoUpdates: clause.Assignments(map[string]interface{}{
				// MySQL uses VALUES(col) to reference the incoming row in ON DUPLICATE KEY UPDATE.
				"views":          gorm.Expr("VALUES(views)"),
				"last_played":    gorm.Expr("VALUES(last_played)"),
				"is_mature":      gorm.Expr("VALUES(is_mature)"),
				"mature_score":   gorm.Expr("VALUES(mature_score)"),
				"category":       gorm.Expr("VALUES(category)"),
				"probe_mod_time": gorm.Expr("VALUES(probe_mod_time)"),
				// Only write stable_id when it's not already set
				"stable_id": gorm.Expr("IF(media_metadata.stable_id IS NULL OR media_metadata.stable_id = '', VALUES(stable_id), media_metadata.stable_id)"),
				// Only write fingerprint when it's not already set
				"content_fingerprint": gorm.Expr("IF(media_metadata.content_fingerprint IS NULL OR media_metadata.content_fingerprint = '', VALUES(content_fingerprint), media_metadata.content_fingerprint)"),
			}),
		}).Create(&row).Error; err != nil {
			return fmt.Errorf("failed to upsert media metadata: %w", err)
		}

		// Replace tags: delete old, insert new
		if err := tx.Where("path = ?", path).Delete(&mediaTagRow{}).Error; err != nil {
			return fmt.Errorf("failed to delete old tags: %w", err)
		}

		if len(metadata.Tags) > 0 {
			tags := make([]mediaTagRow, len(metadata.Tags))
			for i, tag := range metadata.Tags {
				tags[i] = mediaTagRow{Path: path, Tag: tag}
			}
			if err := tx.Create(&tags).Error; err != nil {
				return fmt.Errorf("failed to insert tags: %w", err)
			}
		}

		return nil
	})
}

// Get retrieves media metadata by path
func (r *MediaMetadataRepository) Get(ctx context.Context, path string) (*repositories.MediaMetadata, error) {
	var row mediaMetadataRow
	if err := r.db.WithContext(ctx).Where("path = ?", path).First(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("media metadata not found: %s", path)
		}
		return nil, fmt.Errorf("failed to query media metadata: %w", err)
	}

	metadata := r.rowToMetadata(&row)

	// Get tags
	var tags []mediaTagRow
	if err := r.db.WithContext(ctx).Where("path = ?", path).Find(&tags).Error; err != nil {
		return nil, fmt.Errorf("failed to query tags: %w", err)
	}
	metadata.Tags = make([]string, len(tags))
	for i, t := range tags {
		metadata.Tags[i] = t.Tag
	}

	return metadata, nil
}

// Delete removes media metadata
func (r *MediaMetadataRepository) Delete(ctx context.Context, path string) error {
	result := r.db.WithContext(ctx).Where("path = ?", path).Delete(&mediaMetadataRow{})
	if result.Error != nil {
		return fmt.Errorf("failed to delete media metadata: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("media metadata not found: %s", path)
	}
	return nil
}

// List retrieves all media metadata.
// Uses two bulk queries (metadata + all tags) to avoid N+1 round trips.
func (r *MediaMetadataRepository) List(ctx context.Context) (map[string]*repositories.MediaMetadata, error) {
	var rows []mediaMetadataRow
	if err := r.db.WithContext(ctx).Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("failed to query media metadata: %w", err)
	}

	results := make(map[string]*repositories.MediaMetadata, len(rows))
	for i := range rows {
		metadata := r.rowToMetadata(&rows[i])
		metadata.Tags = []string{} // populated below
		results[rows[i].Path] = metadata
	}

	// Batch-load all tags in a single query
	if len(results) > 0 {
		var allTags []mediaTagRow
		if err := r.db.WithContext(ctx).Find(&allTags).Error; err == nil {
			for _, t := range allTags {
				if meta, ok := results[t.Path]; ok {
					meta.Tags = append(meta.Tags, t.Tag)
				}
			}
		}
	}

	return results, nil
}

// IncrementViews increments the view count for a media item.
// Only updates existing rows to avoid creating metadata entries without a stable_id.
func (r *MediaMetadataRepository) IncrementViews(ctx context.Context, path string) error {
	result := r.db.WithContext(ctx).Exec(`
		UPDATE media_metadata SET views = views + 1, last_played = NOW() WHERE path = ?
	`, path)
	if result.Error != nil {
		return result.Error
	}
	// If no row existed, the media hasn't been catalogued yet — skip silently.
	// The next full scan will create the row with a proper stable_id.
	return nil
}

// UpdatePlaybackPosition updates the playback position for a user
func (r *MediaMetadataRepository) UpdatePlaybackPosition(ctx context.Context, path, userID string, position float64) error {
	return r.db.WithContext(ctx).Exec(`
		INSERT INTO playback_positions (path, user_id, position, updated_at)
		VALUES (?, ?, ?, NOW())
		ON DUPLICATE KEY UPDATE position = VALUES(position), updated_at = VALUES(updated_at)
	`, path, userID, position).Error
}

// GetPlaybackPosition retrieves the playback position for a user
func (r *MediaMetadataRepository) GetPlaybackPosition(ctx context.Context, path, userID string) (float64, error) {
	var row playbackPositionRow
	err := r.db.WithContext(ctx).
		Where("path = ? AND user_id = ?", path, userID).
		First(&row).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return 0, nil // No position stored — this is expected
		}
		return 0, fmt.Errorf("failed to query playback position: %w", err)
	}
	return row.Position, nil
}

// rowToMetadata converts a GORM row to a repository MediaMetadata struct.
func (r *MediaMetadataRepository) rowToMetadata(row *mediaMetadataRow) *repositories.MediaMetadata {
	metadata := &repositories.MediaMetadata{
		Path:               row.Path,
		StableID:           row.StableID,
		ContentFingerprint: row.ContentFingerprint,
		Views:              row.Views,
		DateAdded:          row.DateAdded.Format(time.RFC3339),
		IsMature:           row.IsMature,
		MatureScore:        row.MatureScore,
		Category:           row.Category,
	}

	if row.LastPlayed != nil {
		formatted := row.LastPlayed.Format(time.RFC3339)
		metadata.LastPlayed = &formatted
	}
	if row.ProbeModTime != nil {
		t := *row.ProbeModTime
		metadata.ProbeModTime = &t
	}

	return metadata
}
