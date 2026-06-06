package mysql

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"

	"media-server-pro/internal/repositories"
)

type savedSearchRow struct {
	ID         string    `gorm:"column:id;primaryKey"`
	UserID     string    `gorm:"column:user_id"`
	Name       string    `gorm:"column:name"`
	Query      string    `gorm:"column:query"`
	Tags       string    `gorm:"column:tags"`
	TagMode    string    `gorm:"column:tag_mode"`
	MediaType  string    `gorm:"column:media_type"`
	CreatedAt  time.Time `gorm:"column:created_at;autoCreateTime"`
	LastSeenAt time.Time `gorm:"column:last_seen_at"`
}

func (savedSearchRow) TableName() string { return "saved_searches" }

// SavedSearchRepository implements repositories.SavedSearchRepository using GORM.
type SavedSearchRepository struct {
	db *gorm.DB
}

// NewSavedSearchRepository creates a new GORM-based saved-search repository.
func NewSavedSearchRepository(db *gorm.DB) repositories.SavedSearchRepository {
	if db == nil {
		panic("NewSavedSearchRepository: db is nil")
	}
	return &SavedSearchRepository{db: db}
}

func (r *SavedSearchRepository) Create(ctx context.Context, rec *repositories.SavedSearchRecord) error {
	row := savedSearchRow{
		ID:         rec.ID,
		UserID:     rec.UserID,
		Name:       rec.Name,
		Query:      rec.Query,
		Tags:       strings.Join(rec.Tags, ","),
		TagMode:    rec.TagMode,
		MediaType:  rec.MediaType,
		LastSeenAt: rec.LastSeenAt,
	}
	if err := r.db.WithContext(ctx).Create(&row).Error; err != nil {
		return fmt.Errorf("create saved search: %w", err)
	}
	return nil
}

func (r *SavedSearchRepository) Delete(ctx context.Context, id, userID string) error {
	result := r.db.WithContext(ctx).
		Where("id = ? AND user_id = ?", id, userID).
		Delete(&savedSearchRow{})
	if result.Error != nil {
		return fmt.Errorf("delete saved search: %w", result.Error)
	}
	return nil
}

func (r *SavedSearchRepository) List(ctx context.Context, userID string) ([]*repositories.SavedSearchRecord, error) {
	var rows []savedSearchRow
	if err := r.db.WithContext(ctx).
		Where("user_id = ?", userID).
		Order("created_at DESC").
		Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("list saved searches: %w", err)
	}
	out := make([]*repositories.SavedSearchRecord, len(rows))
	for i, row := range rows {
		out[i] = rowToRecord(row)
	}
	return out, nil
}

func (r *SavedSearchRepository) Get(ctx context.Context, id, userID string) (*repositories.SavedSearchRecord, error) {
	var row savedSearchRow
	err := r.db.WithContext(ctx).
		Where("id = ? AND user_id = ?", id, userID).
		First(&row).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil //nolint:nilnil // callers check rec == nil explicitly
		}
		return nil, fmt.Errorf("get saved search: %w", err)
	}
	return rowToRecord(row), nil
}

func (r *SavedSearchRepository) UpdateLastSeen(ctx context.Context, id, userID string, seenAt time.Time) error {
	result := r.db.WithContext(ctx).
		Model(&savedSearchRow{}).
		Where("id = ? AND user_id = ?", id, userID).
		Update("last_seen_at", seenAt)
	if result.Error != nil {
		return fmt.Errorf("update last seen: %w", result.Error)
	}
	return nil
}

func rowToRecord(row savedSearchRow) *repositories.SavedSearchRecord {
	var tags []string
	if row.Tags != "" {
		for t := range strings.SplitSeq(row.Tags, ",") {
			if t = strings.TrimSpace(t); t != "" {
				tags = append(tags, t)
			}
		}
	}
	return &repositories.SavedSearchRecord{
		ID:         row.ID,
		UserID:     row.UserID,
		Name:       row.Name,
		Query:      row.Query,
		Tags:       tags,
		TagMode:    row.TagMode,
		MediaType:  row.MediaType,
		CreatedAt:  row.CreatedAt,
		LastSeenAt: row.LastSeenAt,
	}
}
