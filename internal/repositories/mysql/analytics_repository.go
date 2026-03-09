// Package mysql provides MySQL/GORM implementations of repositories
package mysql

import (
	"context"

	"media-server-pro/internal/repositories"
	"media-server-pro/pkg/models"

	"gorm.io/gorm"
)

// AnalyticsRepository implements repositories.AnalyticsRepository using GORM
type AnalyticsRepository struct {
	db *gorm.DB
}

// NewAnalyticsRepository creates a new GORM-based analytics repository
func NewAnalyticsRepository(db *gorm.DB) repositories.AnalyticsRepository {
	return &AnalyticsRepository{db: db}
}

// Create stores a new analytics event
func (r *AnalyticsRepository) Create(ctx context.Context, event *models.AnalyticsEvent) error {
	return r.db.WithContext(ctx).Create(event).Error
}

// List retrieves analytics events with optional filtering
func (r *AnalyticsRepository) List(ctx context.Context, filter repositories.AnalyticsFilter) ([]*models.AnalyticsEvent, error) {
	var events []*models.AnalyticsEvent
	query := r.db.WithContext(ctx).Model(&models.AnalyticsEvent{})

	if filter.Type != "" {
		query = query.Where("type = ?", filter.Type)
	}
	if filter.MediaID != "" {
		query = query.Where("media_id = ?", filter.MediaID)
	}
	if filter.UserID != "" {
		query = query.Where("user_id = ?", filter.UserID)
	}
	if filter.StartDate != "" {
		query = query.Where("timestamp >= ?", filter.StartDate)
	}
	if filter.EndDate != "" {
		query = query.Where("timestamp <= ?", filter.EndDate)
	}

	if filter.Limit > 0 {
		query = query.Limit(filter.Limit)
	}
	if filter.Offset > 0 {
		query = query.Offset(filter.Offset)
	}

	query = query.Order("timestamp DESC")

	if err := query.Find(&events).Error; err != nil {
		return nil, err
	}

	return events, nil
}

// GetByMediaID retrieves all events for a specific media item
func (r *AnalyticsRepository) GetByMediaID(ctx context.Context, mediaID string) ([]*models.AnalyticsEvent, error) {
	var events []*models.AnalyticsEvent
	err := r.db.WithContext(ctx).
		Where("media_id = ?", mediaID).
		Order("timestamp DESC").
		Find(&events).Error
	return events, err
}

// GetByUserID retrieves all events for a specific user.
// TODO: Never called. Backend routes GET /api/analytics/events/by-type and by-media exist; profile or admin "events by user" could use this. Either add an API/handler that returns events by user and call GetByUserID, or remove if not needed.
func (r *AnalyticsRepository) GetByUserID(ctx context.Context, userID string) ([]*models.AnalyticsEvent, error) {
	var events []*models.AnalyticsEvent
	err := r.db.WithContext(ctx).
		Where("user_id = ?", userID).
		Order("timestamp DESC").
		Find(&events).Error
	return events, err
}

// DeleteOlderThan deletes events older than the specified timestamp
func (r *AnalyticsRepository) DeleteOlderThan(ctx context.Context, before string) error {
	return r.db.WithContext(ctx).
		Where("timestamp < ?", before).
		Delete(&models.AnalyticsEvent{}).Error
}

// CountByType returns event counts grouped by event type using a single SQL GROUP BY query.
// Avoids the full in-memory table scan previously used by GetEventTypeCounts and GetEventStats.
func (r *AnalyticsRepository) CountByType(ctx context.Context) (map[string]int, error) {
	type row struct {
		Type  string `gorm:"column:type"`
		Count int    `gorm:"column:cnt"`
	}
	var rows []row
	if err := r.db.WithContext(ctx).
		Model(&models.AnalyticsEvent{}).
		Select("type, COUNT(*) AS cnt").
		Group("type").
		Scan(&rows).Error; err != nil {
		return nil, err
	}
	result := make(map[string]int, len(rows))
	for _, row := range rows {
		result[row.Type] = row.Count
	}
	return result, nil
}

// Count returns the number of events matching the filter
func (r *AnalyticsRepository) Count(ctx context.Context, filter repositories.AnalyticsFilter) (int64, error) {
	var count int64
	query := r.db.WithContext(ctx).Model(&models.AnalyticsEvent{})

	if filter.Type != "" {
		query = query.Where("type = ?", filter.Type)
	}
	if filter.MediaID != "" {
		query = query.Where("media_id = ?", filter.MediaID)
	}
	if filter.UserID != "" {
		query = query.Where("user_id = ?", filter.UserID)
	}
	if filter.StartDate != "" {
		query = query.Where("timestamp >= ?", filter.StartDate)
	}
	if filter.EndDate != "" {
		query = query.Where("timestamp <= ?", filter.EndDate)
	}

	err := query.Count(&count).Error
	return count, err
}
