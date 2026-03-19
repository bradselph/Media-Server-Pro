package mysql

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"media-server-pro/internal/repositories"
)

type suggestionProfileRow struct {
	UserID          string    `gorm:"column:user_id;primaryKey"`
	CategoryScores  string    `gorm:"column:category_scores;type:json"`
	TypePreferences string    `gorm:"column:type_preferences;type:json"`
	TotalViews      int       `gorm:"column:total_views"`
	TotalWatchTime  float64   `gorm:"column:total_watch_time"`
	LastUpdated     time.Time `gorm:"column:last_updated"`
}

func (suggestionProfileRow) TableName() string { return "suggestion_profiles" }

type viewHistoryRow struct {
	UserID      string     `gorm:"column:user_id;primaryKey"`
	MediaPath   string     `gorm:"column:media_path;primaryKey"`
	Category    string     `gorm:"column:category"`
	MediaType   string     `gorm:"column:media_type"`
	ViewCount   int        `gorm:"column:view_count"`
	TotalTime   float64    `gorm:"column:total_time"`
	LastViewed  time.Time  `gorm:"column:last_viewed"`
	CompletedAt *time.Time `gorm:"column:completed_at"`
	Rating      float64    `gorm:"column:rating"`
}

func (viewHistoryRow) TableName() string { return "suggestion_view_history" }

type SuggestionProfileRepository struct {
	db *gorm.DB
}

func NewSuggestionProfileRepository(db *gorm.DB) repositories.SuggestionProfileRepository {
	return &SuggestionProfileRepository{db: db}
}

func (r *SuggestionProfileRepository) SaveProfile(ctx context.Context, profile *repositories.SuggestionProfileRecord) error {
	catJSON, err := json.Marshal(profile.CategoryScores)
	if err != nil {
		return fmt.Errorf("failed to marshal category_scores: %w", err)
	}
	typeJSON, err := json.Marshal(profile.TypePreferences)
	if err != nil {
		return fmt.Errorf("failed to marshal type_preferences: %w", err)
	}
	row := suggestionProfileRow{
		UserID:          profile.UserID,
		CategoryScores:  string(catJSON),
		TypePreferences: string(typeJSON),
		TotalViews:      profile.TotalViews,
		TotalWatchTime:  profile.TotalWatchTime,
		LastUpdated:     profile.LastUpdated,
	}
	if err := r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "user_id"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"category_scores", "type_preferences", "total_views",
			"total_watch_time", "last_updated",
		}),
	}).Create(&row).Error; err != nil {
		return fmt.Errorf("failed to save suggestion profile: %w", err)
	}
	return nil
}

func (r *SuggestionProfileRepository) GetProfile(ctx context.Context, userID string) (*repositories.SuggestionProfileRecord, error) {
	var row suggestionProfileRow
	if err := r.db.WithContext(ctx).Where("user_id = ?", userID).First(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get suggestion profile: %w", err)
	}
	rec := &repositories.SuggestionProfileRecord{
		UserID:         row.UserID,
		TotalViews:     row.TotalViews,
		TotalWatchTime: row.TotalWatchTime,
		LastUpdated:    row.LastUpdated,
	}
	if row.CategoryScores != "" {
		if err := json.Unmarshal([]byte(row.CategoryScores), &rec.CategoryScores); err != nil {
			return nil, fmt.Errorf("failed to unmarshal category_scores for user %s: %w", row.UserID, err)
		}
	}
	if row.TypePreferences != "" {
		if err := json.Unmarshal([]byte(row.TypePreferences), &rec.TypePreferences); err != nil {
			return nil, fmt.Errorf("failed to unmarshal type_preferences for user %s: %w", row.UserID, err)
		}
	}
	if rec.CategoryScores == nil {
		rec.CategoryScores = make(map[string]float64)
	}
	if rec.TypePreferences == nil {
		rec.TypePreferences = make(map[string]float64)
	}
	return rec, nil
}

func (r *SuggestionProfileRepository) DeleteProfile(ctx context.Context, userID string) error {
	if err := r.db.WithContext(ctx).Where("user_id = ?", userID).Delete(&suggestionProfileRow{}).Error; err != nil {
		return fmt.Errorf("failed to delete suggestion profile: %w", err)
	}
	return nil
}

func (r *SuggestionProfileRepository) ListProfiles(ctx context.Context) ([]*repositories.SuggestionProfileRecord, error) {
	var rows []suggestionProfileRow
	if err := r.db.WithContext(ctx).Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("failed to list suggestion profiles: %w", err)
	}
	records := make([]*repositories.SuggestionProfileRecord, len(rows))
	for i := range rows {
		rec := &repositories.SuggestionProfileRecord{
			UserID:         rows[i].UserID,
			TotalViews:     rows[i].TotalViews,
			TotalWatchTime: rows[i].TotalWatchTime,
			LastUpdated:    rows[i].LastUpdated,
		}
		if rows[i].CategoryScores != "" {
			if err := json.Unmarshal([]byte(rows[i].CategoryScores), &rec.CategoryScores); err != nil {
				return nil, fmt.Errorf("failed to unmarshal category_scores for user %s: %w", rows[i].UserID, err)
			}
		}
		if rows[i].TypePreferences != "" {
			if err := json.Unmarshal([]byte(rows[i].TypePreferences), &rec.TypePreferences); err != nil {
				return nil, fmt.Errorf("failed to unmarshal type_preferences for user %s: %w", rows[i].UserID, err)
			}
		}
		if rec.CategoryScores == nil {
			rec.CategoryScores = make(map[string]float64)
		}
		if rec.TypePreferences == nil {
			rec.TypePreferences = make(map[string]float64)
		}
		records[i] = rec
	}
	return records, nil
}

func (r *SuggestionProfileRepository) SaveViewHistory(ctx context.Context, userID string, entry *repositories.ViewHistoryRecord) error {
	row := viewHistoryRow{
		UserID:      userID,
		MediaPath:   entry.MediaPath,
		Category:    entry.Category,
		MediaType:   entry.MediaType,
		ViewCount:   entry.ViewCount,
		TotalTime:   entry.TotalTime,
		LastViewed:  entry.LastViewed,
		CompletedAt: entry.CompletedAt,
		Rating:      entry.Rating,
	}
	if err := r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "user_id"}, {Name: "media_path"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"category", "media_type", "view_count", "total_time",
			"last_viewed", "completed_at", "rating",
		}),
	}).Create(&row).Error; err != nil {
		return fmt.Errorf("failed to save view history: %w", err)
	}
	return nil
}

func (r *SuggestionProfileRepository) GetViewHistory(ctx context.Context, userID string) ([]*repositories.ViewHistoryRecord, error) {
	var rows []viewHistoryRow
	if err := r.db.WithContext(ctx).Where("user_id = ?", userID).Order("last_viewed DESC").Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("failed to get view history: %w", err)
	}
	records := make([]*repositories.ViewHistoryRecord, len(rows))
	for i := range rows {
		records[i] = &repositories.ViewHistoryRecord{
			UserID:      rows[i].UserID,
			MediaPath:   rows[i].MediaPath,
			Category:    rows[i].Category,
			MediaType:   rows[i].MediaType,
			ViewCount:   rows[i].ViewCount,
			TotalTime:   rows[i].TotalTime,
			LastViewed:  rows[i].LastViewed,
			CompletedAt: rows[i].CompletedAt,
			Rating:      rows[i].Rating,
		}
	}
	return records, nil
}

func (r *SuggestionProfileRepository) DeleteViewHistory(ctx context.Context, userID string) error {
	if err := r.db.WithContext(ctx).Where("user_id = ?", userID).Delete(&viewHistoryRow{}).Error; err != nil {
		return fmt.Errorf("failed to delete view history: %w", err)
	}
	return nil
}
