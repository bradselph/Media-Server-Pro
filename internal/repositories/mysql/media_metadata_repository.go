package mysql

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"media-server-pro/internal/repositories"
)

const errQueryMediaMetadata = "failed to query media metadata: %w"

// escapeLike escapes SQL LIKE meta-characters (% and _) and backslash so the value
// can be safely used in a LIKE pattern with ESCAPE '\\'.
func escapeLike(s string) string {
	s = strings.ReplaceAll(s, "\\", "\\\\")
	s = strings.ReplaceAll(s, "%", "\\%")
	s = strings.ReplaceAll(s, "_", "\\_")
	return s
}

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
	BlurHash           string     `gorm:"column:blur_hash"`
	Duration           float64    `gorm:"column:duration"`
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
	Duration  float64   `gorm:"column:duration"`
	Progress  float64   `gorm:"column:progress"`
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
			BlurHash:           metadata.BlurHash,
			Duration:           metadata.Duration,
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
				"duration":       gorm.Expr("IF(VALUES(duration) > 0, VALUES(duration), media_metadata.duration)"),
				// Only write stable_id when it's not already set
				"stable_id": gorm.Expr("IF(media_metadata.stable_id IS NULL OR media_metadata.stable_id = '', VALUES(stable_id), media_metadata.stable_id)"),
				// Only write fingerprint when it's not already set
				"content_fingerprint": gorm.Expr("IF(media_metadata.content_fingerprint IS NULL OR media_metadata.content_fingerprint = '', VALUES(content_fingerprint), media_metadata.content_fingerprint)"),
			}),
		}).Create(&row).Error; err != nil {
			return fmt.Errorf("failed to upsert media metadata: %w", err)
		}

		// Replace tags: delete old, insert new
		if err := tx.Where(sqlPathEq, path).Delete(&mediaTagRow{}).Error; err != nil {
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
	if err := r.db.WithContext(ctx).Where(sqlPathEq, path).First(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("media metadata not found: %s", path)
		}
		return nil, fmt.Errorf(errQueryMediaMetadata, err)
	}

	metadata := r.rowToMetadata(&row)

	// Get tags
	var tags []mediaTagRow
	if err := r.db.WithContext(ctx).Where(sqlPathEq, path).Find(&tags).Error; err != nil {
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
	result := r.db.WithContext(ctx).Where(sqlPathEq, path).Delete(&mediaMetadataRow{})
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
		return nil, fmt.Errorf(errQueryMediaMetadata, err)
	}

	results := make(map[string]*repositories.MediaMetadata, len(rows))
	for i := range rows {
		metadata := r.rowToMetadata(&rows[i])
		metadata.Tags = []string{} // populated below
		results[rows[i].Path] = metadata
	}

	// Batch-load tags only for the paths we have (WHERE path IN), avoiding loading
	// the entire media_tags table for large libraries.
	if len(results) > 0 {
		paths := make([]string, 0, len(results))
		for p := range results {
			paths = append(paths, p)
		}
		var tags []mediaTagRow
		if err := r.db.WithContext(ctx).Where("path IN ?", paths).Find(&tags).Error; err != nil {
			return nil, fmt.Errorf("failed to load media tags: %w", err)
		}
		for _, t := range tags {
			if meta, ok := results[t.Path]; ok {
				meta.Tags = append(meta.Tags, t.Tag)
			}
		}
	}

	return results, nil
}

// ListFiltered returns metadata matching the given filter with DB-level
// pagination. Returns matching rows and the total count before LIMIT/OFFSET.
func (r *MediaMetadataRepository) ListFiltered(ctx context.Context, filter repositories.MediaFilter) ([]*repositories.MediaMetadata, int64, error) {
	query := r.db.WithContext(ctx).Model(&mediaMetadataRow{})

	// Apply filters
	if filter.Category != "" {
		query = query.Where("category = ?", filter.Category)
	}
	if filter.IsMature != nil {
		query = query.Where("is_mature = ?", *filter.IsMature)
	}
	if filter.Search != "" {
		// Split into words so "blonde sassy" matches items containing both words
		// anywhere in path or category (AND logic: every word must appear).
		for _, word := range strings.Fields(filter.Search) {
			like := "%" + escapeLike(word) + "%"
			query = query.Where("(path LIKE ? ESCAPE '\\\\' OR category LIKE ? ESCAPE '\\\\')", like, like)
		}
	}
	switch filter.Type {
	case "video":
		query = query.Where("LOWER(path) REGEXP ?", `\.(mp4|mkv|avi|mov|wmv|flv|webm|m4v|mpg|mpeg|3gp|ts|m2ts|vob|ogv)$`)
	case "audio":
		query = query.Where("LOWER(path) REGEXP ?", `\.(mp3|wav|flac|aac|ogg|m4a|wma|aiff|alac|opus|ape|mka)$`)
	}
	if len(filter.Tags) > 0 {
		// Subquery: paths that have at least one of the given tags (OR)
		query = query.Where("path IN (SELECT path FROM media_tags WHERE tag IN ?)", filter.Tags)
	}

	// Count total matches before pagination
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, fmt.Errorf("failed to count media metadata: %w", err)
	}

	// Apply sorting
	switch filter.SortBy {
	case "views":
		if filter.SortDesc {
			query = query.Order("views DESC")
		} else {
			query = query.Order("views ASC")
		}
	case "date_added":
		if filter.SortDesc {
			query = query.Order("date_added DESC")
		} else {
			query = query.Order("date_added ASC")
		}
	default:
		if filter.SortDesc {
			query = query.Order("path DESC")
		} else {
			query = query.Order("path ASC")
		}
	}

	// Apply pagination. Always enforce an upper limit: Limit=0 from a caller
	// should not pull the entire table into memory (OOM risk on large libraries).
	const defaultMediaMetadataLimit = 1000
	limit := filter.Limit
	if limit <= 0 {
		limit = defaultMediaMetadataLimit
	}
	query = query.Limit(limit)
	if filter.Offset > 0 {
		query = query.Offset(filter.Offset)
	}

	var rows []mediaMetadataRow
	if err := query.Find(&rows).Error; err != nil {
		return nil, 0, fmt.Errorf(errQueryMediaMetadata, err)
	}

	// Convert rows and batch-load tags
	results := make([]*repositories.MediaMetadata, len(rows))
	paths := make([]string, len(rows))
	for i := range rows {
		results[i] = r.rowToMetadata(&rows[i])
		results[i].Tags = []string{}
		paths[i] = rows[i].Path
	}

	if len(paths) > 0 {
		var tags []mediaTagRow
		if err := r.db.WithContext(ctx).Where("path IN ?", paths).Find(&tags).Error; err != nil {
			return nil, 0, fmt.Errorf("failed to load tags: %w", err)
		}
		tagMap := make(map[string][]string)
		for _, t := range tags {
			tagMap[t.Path] = append(tagMap[t.Path], t.Tag)
		}
		for _, m := range results {
			if t, ok := tagMap[m.Path]; ok {
				m.Tags = t
			}
		}
	}

	return results, total, nil
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
	// If no row existed, the media hasn't been cataloged yet — skip silently.
	// The next full scan will create the row with a proper stable_id.
	return nil
}

// UpdatePlaybackPosition updates the playback position, total duration, and
// progress fraction for a user. All three values must be supplied together so
// the row stays internally consistent; pass 0 for duration and progress when
// clearing the position.
func (r *MediaMetadataRepository) UpdatePlaybackPosition(ctx context.Context, path, userID string, position, duration, progress float64) error {
	return r.db.WithContext(ctx).Exec(`
		INSERT INTO playback_positions (path, user_id, position, duration, progress, updated_at)
		VALUES (?, ?, ?, ?, ?, NOW())
		ON DUPLICATE KEY UPDATE
			position  = VALUES(position),
			duration  = VALUES(duration),
			progress  = VALUES(progress),
			updated_at = VALUES(updated_at)
	`, path, userID, position, duration, progress).Error
}

// DeleteAllPlaybackPositionsByUser removes all playback positions for a user.
func (r *MediaMetadataRepository) DeleteAllPlaybackPositionsByUser(ctx context.Context, userID string) error {
	return r.db.WithContext(ctx).Where("user_id = ?", userID).Delete(&playbackPositionRow{}).Error
}

// DeletePlaybackPositionsByPath deletes all playback positions for a given media path.
func (r *MediaMetadataRepository) DeletePlaybackPositionsByPath(ctx context.Context, path string) error {
	return r.db.WithContext(ctx).Where("path = ?", path).Delete(&playbackPositionRow{}).Error
}

// BatchGetPlaybackPositions retrieves playback positions for multiple paths for a user.
// Returns a map of path → position; paths with no stored position are omitted.
func (r *MediaMetadataRepository) BatchGetPlaybackPositions(ctx context.Context, paths []string, userID string) (map[string]float64, error) {
	if len(paths) == 0 {
		return map[string]float64{}, nil
	}
	var rows []playbackPositionRow
	err := r.db.WithContext(ctx).
		Where("user_id = ? AND path IN ?", userID, paths).
		Find(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("failed to batch-query playback positions: %w", err)
	}
	result := make(map[string]float64, len(rows))
	for _, row := range rows {
		result[row.Path] = row.Position
	}
	return result, nil
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
		metadata.LastPlayed = new(row.LastPlayed.Format(time.RFC3339))
	}
	if row.ProbeModTime != nil {
		metadata.ProbeModTime = new(*row.ProbeModTime)
	}
	metadata.BlurHash = row.BlurHash
	metadata.Duration = row.Duration

	return metadata
}

// UpdateBlurHash updates the BlurHash for a metadata row by path
func (r *MediaMetadataRepository) UpdateBlurHash(ctx context.Context, path, blurHash string) error {
	if r.db == nil {
		return fmt.Errorf("database not available")
	}
	result := r.db.WithContext(ctx).Model(&mediaMetadataRow{}).
		Where(sqlPathEq, path).
		Update("blur_hash", blurHash)
	if result.Error != nil {
		return fmt.Errorf("failed to update blur_hash: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("metadata not found for path %q", path)
	}
	return nil
}

// GetPathByStableID returns the file path for the given stable ID.
// Returns ("", nil) when no matching row exists.
func (r *MediaMetadataRepository) GetPathByStableID(ctx context.Context, stableID string) (string, error) {
	var row mediaMetadataRow
	err := r.db.WithContext(ctx).
		Select("path").
		Where("stable_id = ?", stableID).
		First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return "", repositories.ErrPathNotFound
	}
	if err != nil {
		return "", fmt.Errorf("failed to get path by stable_id: %w", err)
	}
	return row.Path, nil
}

// ListDuplicateCandidates returns only rows that have both a non-empty
// content_fingerprint and stable_id.  Tags are not loaded — callers that need
// only fingerprint/stableID/path (e.g. the duplicate-detection scan) avoid
// the cost of fetching the full table and the extra tag batch query.
func (r *MediaMetadataRepository) ListDuplicateCandidates(ctx context.Context) (map[string]*repositories.MediaMetadata, error) {
	var rows []mediaMetadataRow
	if err := r.db.WithContext(ctx).
		Where("content_fingerprint != '' AND stable_id != ''").
		Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("failed to query duplicate candidates: %w", err)
	}
	results := make(map[string]*repositories.MediaMetadata, len(rows))
	for i := range rows {
		metadata := r.rowToMetadata(&rows[i])
		metadata.Tags = []string{}
		results[rows[i].Path] = metadata
	}
	return results, nil
}
