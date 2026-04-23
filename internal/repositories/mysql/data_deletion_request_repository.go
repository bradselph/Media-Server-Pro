package mysql

import (
	"context"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"

	"media-server-pro/internal/repositories"
)

type dataDeletionRequestRow struct {
	ID         string     `gorm:"column:id;primaryKey"`
	UserID     string     `gorm:"column:user_id"`
	Username   string     `gorm:"column:username"`
	Email      string     `gorm:"column:email"`
	Reason     string     `gorm:"column:reason"`
	Status     string     `gorm:"column:status"`
	CreatedAt  time.Time  `gorm:"column:created_at;autoCreateTime"`
	ReviewedAt *time.Time `gorm:"column:reviewed_at"`
	ReviewedBy string     `gorm:"column:reviewed_by"`
	AdminNotes string     `gorm:"column:admin_notes"`
}

func (dataDeletionRequestRow) TableName() string { return "data_deletion_requests" }

// DataDeletionRequestRepositoryImpl implements repositories.DataDeletionRequestRepository using GORM.
type DataDeletionRequestRepositoryImpl struct {
	db *gorm.DB
}

// NewDataDeletionRequestRepository creates a new GORM-based data deletion request repository.
func NewDataDeletionRequestRepository(db *gorm.DB) repositories.DataDeletionRequestRepository {
	if db == nil {
		panic("NewDataDeletionRequestRepository: db is nil")
	}
	return &DataDeletionRequestRepositoryImpl{db: db}
}

func (r *DataDeletionRequestRepositoryImpl) Create(ctx context.Context, req *repositories.DataDeletionRequestRecord) error {
	row := dataDeletionRequestRow{
		ID:       req.ID,
		UserID:   req.UserID,
		Username: req.Username,
		Email:    req.Email,
		Reason:   req.Reason,
		Status:   req.Status,
	}
	if err := r.db.WithContext(ctx).Create(&row).Error; err != nil {
		return fmt.Errorf("create data deletion request: %w", err)
	}
	return nil
}

func (r *DataDeletionRequestRepositoryImpl) Get(ctx context.Context, id string) (*repositories.DataDeletionRequestRecord, error) {
	var row dataDeletionRequestRow
	if err := r.db.WithContext(ctx).Where("id = ?", id).First(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil //nolint:nilnil // callers check rec == nil explicitly
		}
		return nil, fmt.Errorf("get data deletion request: %w", err)
	}
	return rowToDataDeletionRequestRecord(&row), nil
}

func (r *DataDeletionRequestRepositoryImpl) ListByStatus(ctx context.Context, status string) ([]*repositories.DataDeletionRequestRecord, error) {
	var rows []dataDeletionRequestRow
	q := r.db.WithContext(ctx).Order("created_at DESC")
	if status != "" {
		q = q.Where("status = ?", status)
	}
	if err := q.Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("list data deletion requests: %w", err)
	}
	out := make([]*repositories.DataDeletionRequestRecord, len(rows))
	for i, row := range rows {
		out[i] = rowToDataDeletionRequestRecord(&row)
	}
	return out, nil
}

func (r *DataDeletionRequestRepositoryImpl) CountPendingByUser(ctx context.Context, userID string) (int64, error) {
	var count int64
	if err := r.db.WithContext(ctx).Model(&dataDeletionRequestRow{}).
		Where("user_id = ? AND status = 'pending'", userID).
		Count(&count).Error; err != nil {
		return 0, fmt.Errorf("count pending deletion requests: %w", err)
	}
	return count, nil
}

func (r *DataDeletionRequestRepositoryImpl) UpdateStatus(ctx context.Context, id, status, reviewedBy, adminNotes string) error {
	result := r.db.WithContext(ctx).Model(&dataDeletionRequestRow{}).
		Where("id = ?", id).
		Updates(map[string]any{
			"status":      status,
			"reviewed_at": func() *time.Time { t := time.Now().UTC(); return &t }(),
			"reviewed_by": reviewedBy,
			"admin_notes": adminNotes,
		})
	if result.Error != nil {
		return fmt.Errorf("update data deletion request status: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("data deletion request %s not found", id)
	}
	return nil
}

func rowToDataDeletionRequestRecord(row *dataDeletionRequestRow) *repositories.DataDeletionRequestRecord {
	if row == nil {
		return nil
	}
	return &repositories.DataDeletionRequestRecord{
		ID:         row.ID,
		UserID:     row.UserID,
		Username:   row.Username,
		Email:      row.Email,
		Reason:     row.Reason,
		Status:     row.Status,
		CreatedAt:  row.CreatedAt,
		ReviewedAt: row.ReviewedAt,
		ReviewedBy: row.ReviewedBy,
		AdminNotes: row.AdminNotes,
	}
}
