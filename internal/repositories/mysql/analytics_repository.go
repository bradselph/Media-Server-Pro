// Package mysql provides MySQL/GORM implementations of repositories
package mysql

import (
	"context"

	"media-server-pro/internal/repositories"
	"media-server-pro/pkg/models"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const sqlOrderTimestampDesc = "timestamp DESC"

// AnalyticsRepository implements repositories.AnalyticsRepository using GORM
type AnalyticsRepository struct {
	db *gorm.DB
}

// NewAnalyticsRepository creates a new GORM-based analytics repository
func NewAnalyticsRepository(db *gorm.DB) repositories.AnalyticsRepository {
	if db == nil {
		panic("analytics repository: db is nil")
	}
	return &AnalyticsRepository{db: db}
}

// Create stores a new analytics event
func (r *AnalyticsRepository) Create(ctx context.Context, event *models.AnalyticsEvent) error {
	return r.db.WithContext(ctx).Create(event).Error
}

// listWhereSpec describes a single optional WHERE clause for List filtering.
type listWhereSpec struct {
	ok     bool
	clause string
	value  interface{}
}

// applyListFilter applies optional filter conditions to the query via a single loop to keep cyclomatic complexity low.
func (r *AnalyticsRepository) applyListFilter(query *gorm.DB, filter repositories.AnalyticsFilter) *gorm.DB {
	specs := []listWhereSpec{
		{filter.Type != "", "type = ?", filter.Type},
		{filter.MediaID != "", "media_id = ?", filter.MediaID},
		{filter.UserID != "", "user_id = ?", filter.UserID},
		{filter.StartDate != "", "timestamp >= ?", filter.StartDate},
		{filter.EndDate != "", "timestamp <= ?", filter.EndDate},
	}
	for _, s := range specs {
		if s.ok {
			query = query.Where(s.clause, s.value)
		}
	}
	return query
}

// List retrieves analytics events with optional filtering
func (r *AnalyticsRepository) List(ctx context.Context, filter repositories.AnalyticsFilter) ([]*models.AnalyticsEvent, error) {
	var events []*models.AnalyticsEvent
	query := r.db.WithContext(ctx).Model(&models.AnalyticsEvent{})
	query = r.applyListFilter(query, filter)
	limit := filter.Limit
	if limit <= 0 || limit > defaultAnalyticsQueryLimit {
		limit = defaultAnalyticsQueryLimit
	}
	query = query.Limit(limit)
	if filter.Offset > 0 {
		query = query.Offset(filter.Offset)
	}
	query = query.Order(sqlOrderTimestampDesc)
	if err := query.Find(&events).Error; err != nil {
		return nil, err
	}
	return events, nil
}

// defaultAnalyticsQueryLimit caps GetByMediaID/GetByUserID result size to avoid unbounded queries.
const defaultAnalyticsQueryLimit = 10000

// GetByMediaID retrieves events for a specific media item (capped by defaultAnalyticsQueryLimit).
func (r *AnalyticsRepository) GetByMediaID(ctx context.Context, mediaID string) ([]*models.AnalyticsEvent, error) {
	var events []*models.AnalyticsEvent
	err := r.db.WithContext(ctx).
		Where("media_id = ?", mediaID).
		Order(sqlOrderTimestampDesc).
		Limit(defaultAnalyticsQueryLimit).
		Find(&events).Error
	return events, err
}

// GetByUserID retrieves events for a specific user (capped by defaultAnalyticsQueryLimit).
func (r *AnalyticsRepository) GetByUserID(ctx context.Context, userID string) ([]*models.AnalyticsEvent, error) {
	var events []*models.AnalyticsEvent
	err := r.db.WithContext(ctx).
		Where("user_id = ?", userID).
		Order(sqlOrderTimestampDesc).
		Limit(defaultAnalyticsQueryLimit).
		Find(&events).Error
	return events, err
}

// DeleteOlderThan deletes events older than the specified timestamp
func (r *AnalyticsRepository) DeleteOlderThan(ctx context.Context, before string) error {
	return r.db.WithContext(ctx).
		Where("timestamp < ?", before).
		Delete(&models.AnalyticsEvent{}).Error
}

// DeleteByMediaID deletes all analytics events for the given media ID.
func (r *AnalyticsRepository) DeleteByMediaID(ctx context.Context, mediaID string) error {
	return r.db.WithContext(ctx).
		Where("media_id = ?", mediaID).
		Delete(&models.AnalyticsEvent{}).Error
}

// CountByType returns event counts grouped by event type using a single SQL GROUP BY query.
// Avoids the full in-memory table scan previously used by GetEventTypeCounts and GetEventStats.
func (r *AnalyticsRepository) CountByType(ctx context.Context) (map[string]int64, error) {
	type row struct {
		Type  string `gorm:"column:type"`
		Count int64  `gorm:"column:cnt"`
	}
	var rows []row
	if err := r.db.WithContext(ctx).
		Model(&models.AnalyticsEvent{}).
		Select("type, COUNT(*) AS cnt").
		Group("type").
		Scan(&rows).Error; err != nil {
		return nil, err
	}
	result := make(map[string]int64, len(rows))
	for _, row := range rows {
		result[row.Type] = row.Count
	}
	return result, nil
}

// Count returns the number of events matching the filter
func (r *AnalyticsRepository) Count(ctx context.Context, filter repositories.AnalyticsFilter) (int64, error) {
	var count int64
	query := r.db.WithContext(ctx).Model(&models.AnalyticsEvent{})
	query = r.applyListFilter(query, filter)
	err := query.Count(&count).Error
	return count, err
}

// UpsertDailyStats writes (or replaces) the daily aggregate row keyed by date.
// Uses INSERT ... ON DUPLICATE KEY UPDATE so concurrent writers can't lose data
// to a race between read-modify-write cycles. TopMedia is gorm:"-" and skipped.
func (r *AnalyticsRepository) UpsertDailyStats(ctx context.Context, stats *models.DailyStats) error {
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "date"}},
		UpdateAll: true,
	}).Create(stats).Error
}

// ListDailyStatsBetween returns persisted daily aggregates ordered by date asc.
// Both bounds are inclusive ("YYYY-MM-DD"); empty bounds disable that side.
func (r *AnalyticsRepository) ListDailyStatsBetween(ctx context.Context, startDate, endDate string) ([]*models.DailyStats, error) {
	var rows []*models.DailyStats
	query := r.db.WithContext(ctx).Model(&models.DailyStats{})
	if startDate != "" {
		query = query.Where("date >= ?", startDate)
	}
	if endDate != "" {
		query = query.Where("date <= ?", endDate)
	}
	if err := query.Order("date ASC").Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

// DeleteDailyStatsOlderThan removes daily_stats rows whose date is strictly
// before beforeDate ("YYYY-MM-DD"). Called from the cleanup loop to honor the
// retention policy applied to raw events.
func (r *AnalyticsRepository) DeleteDailyStatsOlderThan(ctx context.Context, beforeDate string) error {
	return r.db.WithContext(ctx).
		Where("date < ?", beforeDate).
		Delete(&models.DailyStats{}).Error
}
