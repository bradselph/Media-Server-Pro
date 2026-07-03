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

const sqlUserIDEq = "user_id = ?"

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
	if profile == nil {
		return fmt.Errorf("profile cannot be nil")
	}
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
	if err := r.db.WithContext(ctx).Where(sqlUserIDEq, userID).First(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil //nolint:nilnil // callers check rec == nil explicitly
		}
		return nil, fmt.Errorf("failed to get suggestion profile: %w", err)
	}
	return rowToSuggestionProfileRecord(&row)
}

func (r *SuggestionProfileRepository) DeleteProfile(ctx context.Context, userID string) error {
	result := r.db.WithContext(ctx).Where(sqlUserIDEq, userID).Delete(&suggestionProfileRow{})
	if result.Error != nil {
		return fmt.Errorf("failed to delete suggestion profile: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return repositories.ErrSuggestionProfileNotFound
	}
	return nil
}

// rowToSuggestionProfileRecord builds a domain record from a DB row, decoding
// the two JSON score maps and defaulting them to non-nil empty maps.
func rowToSuggestionProfileRecord(row *suggestionProfileRow) (*repositories.SuggestionProfileRecord, error) {
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

// buildViewHistoryRow maps a domain ViewHistoryRecord to its DB row for userID.
func buildViewHistoryRow(userID string, e *repositories.ViewHistoryRecord) viewHistoryRow {
	return viewHistoryRow{
		UserID:      userID,
		MediaPath:   e.MediaPath,
		Category:    e.Category,
		MediaType:   e.MediaType,
		ViewCount:   e.ViewCount,
		TotalTime:   e.TotalTime,
		LastViewed:  e.LastViewed,
		CompletedAt: e.CompletedAt,
		Rating:      e.Rating,
	}
}

func (r *SuggestionProfileRepository) ListProfiles(ctx context.Context) ([]*repositories.SuggestionProfileRecord, error) {
	var rows []suggestionProfileRow
	if err := r.db.WithContext(ctx).Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("failed to list suggestion profiles: %w", err)
	}
	records := make([]*repositories.SuggestionProfileRecord, len(rows))
	for i := range rows {
		rec, err := rowToSuggestionProfileRecord(&rows[i])
		if err != nil {
			return nil, err
		}
		records[i] = rec
	}
	return records, nil
}

func (r *SuggestionProfileRepository) SaveViewHistory(ctx context.Context, userID string, entry *repositories.ViewHistoryRecord) error {
	if entry == nil {
		return fmt.Errorf("entry cannot be nil")
	}
	row := buildViewHistoryRow(userID, entry)
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

func (r *SuggestionProfileRepository) BatchSaveViewHistory(ctx context.Context, userID string, entries []*repositories.ViewHistoryRecord) error {
	if len(entries) == 0 {
		return nil
	}
	rows := make([]viewHistoryRow, len(entries))
	for i, e := range entries {
		if e == nil {
			return fmt.Errorf("nil entry at index %d", i)
		}
		rows[i] = buildViewHistoryRow(userID, e)
	}
	if err := r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "user_id"}, {Name: "media_path"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"category", "media_type", "view_count", "total_time",
			"last_viewed", "completed_at", "rating",
		}),
	}).CreateInBatches(rows, 100).Error; err != nil {
		return fmt.Errorf("failed to batch save view history: %w", err)
	}
	return nil
}

func (r *SuggestionProfileRepository) GetViewHistory(ctx context.Context, userID string) ([]*repositories.ViewHistoryRecord, error) {
	var rows []viewHistoryRow
	if err := r.db.WithContext(ctx).Where(sqlUserIDEq, userID).Order("last_viewed DESC").Find(&rows).Error; err != nil {
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
	result := r.db.WithContext(ctx).Where(sqlUserIDEq, userID).Delete(&viewHistoryRow{})
	if result.Error != nil {
		return fmt.Errorf("failed to delete view history: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return repositories.ErrViewHistoryNotFound
	}
	return nil
}

func (r *SuggestionProfileRepository) DeleteViewHistoryByMediaPath(ctx context.Context, mediaPath string) error {
	result := r.db.WithContext(ctx).Where("media_path = ?", mediaPath).Delete(&viewHistoryRow{})
	if result.Error != nil {
		return fmt.Errorf("failed to delete view history by media path: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return repositories.ErrViewHistoryNotFound
	}
	return nil
}

// RenameViewHistoryMediaPath re-keys every user's view-history rows from
// oldPath to newPath after a media file rename. Rows whose owner already has
// an entry for newPath are deleted first — re-keying them would violate the
// (user_id, media_path) composite primary key; the existing newPath row wins.
func (r *SuggestionProfileRepository) RenameViewHistoryMediaPath(ctx context.Context, oldPath, newPath string) error {
	// Wrap the collision-delete and the re-key update in one transaction: if the
	// delete commits but the update fails (timeout/DB error), the colliding rows
	// would be lost without the rename completing, leaving view history corrupt.
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec(
			`DELETE vh_old FROM suggestion_view_history vh_old
			 JOIN suggestion_view_history vh_new
			   ON vh_new.user_id = vh_old.user_id AND vh_new.media_path = ?
			 WHERE vh_old.media_path = ?`, newPath, oldPath).Error; err != nil {
			return fmt.Errorf("failed to drop colliding view history rows: %w", err)
		}
		if err := tx.Model(&viewHistoryRow{}).
			Where("media_path = ?", oldPath).
			Update("media_path", newPath).Error; err != nil {
			return fmt.Errorf("failed to re-key view history media path: %w", err)
		}
		return nil
	})
}
