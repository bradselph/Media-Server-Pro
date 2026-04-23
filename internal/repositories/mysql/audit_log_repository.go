// Package mysql provides MySQL/GORM implementations of repositories
package mysql

import (
	"context"

	"media-server-pro/internal/repositories"
	"media-server-pro/pkg/models"

	"gorm.io/gorm"
)

// AuditLogRepository implements repositories.AuditLogRepository using GORM
type AuditLogRepository struct {
	db *gorm.DB
}

// NewAuditLogRepository creates a new GORM-based audit log repository
func NewAuditLogRepository(db *gorm.DB) repositories.AuditLogRepository {
	if db == nil {
		panic("NewAuditLogRepository: db is nil")
	}
	return &AuditLogRepository{db: db}
}

// Create stores a new audit log entry
func (r *AuditLogRepository) Create(ctx context.Context, entry *models.AuditLogEntry) error {
	return r.db.WithContext(ctx).Create(entry).Error
}

// List retrieves audit log entries with optional filtering
func (r *AuditLogRepository) List(ctx context.Context, filter repositories.AuditLogFilter) ([]*models.AuditLogEntry, error) {
	var entries []*models.AuditLogEntry
	query := r.db.WithContext(ctx).Model(&models.AuditLogEntry{})

	if filter.UserID != "" {
		query = query.Where("user_id = ?", filter.UserID)
	}
	if filter.Action != "" {
		query = query.Where("action = ?", filter.Action)
	}
	if filter.Resource != "" {
		query = query.Where("resource = ?", filter.Resource)
	}
	if filter.Success != nil {
		query = query.Where("success = ?", *filter.Success)
	}
	if filter.StartDate != "" {
		query = query.Where("timestamp >= ?", filter.StartDate)
	}
	if filter.EndDate != "" {
		query = query.Where("timestamp <= ?", filter.EndDate)
	}

	limit := filter.Limit
	if limit <= 0 || limit > 100000 {
		limit = 100000 // cap to avoid OOM; ExportAuditLog uses same ceiling
	}
	query = query.Limit(limit)
	if filter.Offset > 0 {
		query = query.Offset(filter.Offset)
	}

	query = query.Order("timestamp DESC")

	if err := query.Find(&entries).Error; err != nil {
		return nil, err
	}

	return entries, nil
}

// getByUserMaxLimit is the hard cap applied when limit <= 0 to prevent unbounded queries.
const getByUserMaxLimit = 1000

// GetByUser retrieves audit log entries for a specific user
func (r *AuditLogRepository) GetByUser(ctx context.Context, userID string, limit int) ([]*models.AuditLogEntry, error) {
	var entries []*models.AuditLogEntry
	if limit <= 0 || limit > getByUserMaxLimit {
		limit = getByUserMaxLimit
	}
	err := r.db.WithContext(ctx).
		Where("user_id = ?", userID).
		Order("timestamp DESC").
		Limit(limit).
		Find(&entries).Error
	return entries, err
}

// DeleteOlderThan deletes log entries older than the specified timestamp
func (r *AuditLogRepository) DeleteOlderThan(ctx context.Context, before string) error {
	return r.db.WithContext(ctx).
		Where("timestamp < ?", before).
		Delete(&models.AuditLogEntry{}).Error
}
