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

type favoriteRow struct {
	ID        string    `gorm:"column:id;primaryKey"`
	UserID    string    `gorm:"column:user_id"`
	MediaID   string    `gorm:"column:media_id"`
	MediaPath string    `gorm:"column:media_path"`
	AddedAt   time.Time `gorm:"column:added_at;autoCreateTime"`
}

func (favoriteRow) TableName() string { return "user_favorites" }

// FavoritesRepository implements repositories.FavoriteRepository using GORM.
type FavoritesRepository struct {
	db *gorm.DB
}

// NewFavoritesRepository creates a new GORM-based favorites repository.
func NewFavoritesRepository(db *gorm.DB) repositories.FavoriteRepository {
	return &FavoritesRepository{db: db}
}

func (r *FavoritesRepository) Add(ctx context.Context, rec *repositories.FavoriteRecord) error {
	row := favoriteRow{
		ID:        rec.ID,
		UserID:    rec.UserID,
		MediaID:   rec.MediaID,
		MediaPath: rec.MediaPath,
	}
	if err := r.db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&row).Error; err != nil {
		return fmt.Errorf("add favorite: %w", err)
	}
	return nil
}

func (r *FavoritesRepository) Remove(ctx context.Context, userID, mediaID string) error {
	result := r.db.WithContext(ctx).
		Where("user_id = ? AND media_id = ?", userID, mediaID).
		Delete(&favoriteRow{})
	if result.Error != nil {
		return fmt.Errorf("remove favorite: %w", result.Error)
	}
	return nil
}

func (r *FavoritesRepository) List(ctx context.Context, userID string) ([]*repositories.FavoriteRecord, error) {
	var rows []favoriteRow
	if err := r.db.WithContext(ctx).Where("user_id = ?", userID).Order("added_at DESC").Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("list favorites: %w", err)
	}
	out := make([]*repositories.FavoriteRecord, len(rows))
	for i, row := range rows {
		out[i] = &repositories.FavoriteRecord{
			ID:        row.ID,
			UserID:    row.UserID,
			MediaID:   row.MediaID,
			MediaPath: row.MediaPath,
			AddedAt:   row.AddedAt,
		}
	}
	return out, nil
}

func (r *FavoritesRepository) Exists(ctx context.Context, userID, mediaID string) (bool, error) {
	var count int64
	err := r.db.WithContext(ctx).Model(&favoriteRow{}).
		Where("user_id = ? AND media_id = ?", userID, mediaID).
		Count(&count).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return false, fmt.Errorf("check favorite exists: %w", err)
	}
	return count > 0, nil
}
