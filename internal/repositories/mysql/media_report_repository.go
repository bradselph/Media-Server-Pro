package mysql

import (
	"context"
	"fmt"
	"time"

	"gorm.io/gorm"

	"media-server-pro/internal/repositories"
)

type mediaReportRow struct {
	ID         string     `gorm:"column:id;primaryKey;size:64"`
	MediaID    string     `gorm:"column:media_id;size:255;index"`
	ReporterID string     `gorm:"column:reporter_id;size:255;index"`
	Reason     string     `gorm:"column:reason;size:64"`
	Notes      string     `gorm:"column:notes;type:text"`
	Status     string     `gorm:"column:status;size:32;index;default:open"`
	CreatedAt  time.Time  `gorm:"column:created_at;autoCreateTime"`
	ResolvedAt *time.Time `gorm:"column:resolved_at"`
	ResolvedBy string     `gorm:"column:resolved_by;size:255"`
	IPAddress  string     `gorm:"column:ip_address;size:45"`
}

func (mediaReportRow) TableName() string { return "media_reports" }

// MediaReportRepository implements repositories.MediaReportRepository via GORM.
type MediaReportRepository struct {
	db *gorm.DB
}

// NewMediaReportRepository constructs the repository.
func NewMediaReportRepository(db *gorm.DB) repositories.MediaReportRepository {
	if db == nil {
		panic("NewMediaReportRepository: db is nil")
	}
	return &MediaReportRepository{db: db}
}

func (r *MediaReportRepository) Create(ctx context.Context, rec *repositories.MediaReportRecord) error {
	row := mediaReportRow{
		ID:         rec.ID,
		MediaID:    rec.MediaID,
		ReporterID: rec.ReporterID,
		Reason:     rec.Reason,
		Notes:      rec.Notes,
		Status:     rec.Status,
		IPAddress:  rec.IPAddress,
	}
	if row.Status == "" {
		row.Status = "open"
	}
	if err := r.db.WithContext(ctx).Create(&row).Error; err != nil {
		return fmt.Errorf("create media report: %w", err)
	}
	rec.CreatedAt = row.CreatedAt
	rec.Status = row.Status
	return nil
}

func (r *MediaReportRepository) List(ctx context.Context, status string, limit, offset int) ([]*repositories.MediaReportRecord, error) {
	if limit <= 0 {
		limit = 50
	}
	if limit > 500 {
		limit = 500
	}
	if offset < 0 {
		offset = 0
	}
	q := r.db.WithContext(ctx).Model(&mediaReportRow{}).Order("created_at DESC").Limit(limit).Offset(offset)
	if status != "" {
		q = q.Where("status = ?", status)
	}
	var rows []mediaReportRow
	if err := q.Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("list media reports: %w", err)
	}
	out := make([]*repositories.MediaReportRecord, len(rows))
	for i, row := range rows {
		out[i] = mediaReportRowToRecord(row)
	}
	return out, nil
}

func (r *MediaReportRepository) UpdateStatus(ctx context.Context, id, status, resolvedBy string) error {
	updates := map[string]any{"status": status, "resolved_by": resolvedBy}
	if status == "resolved" || status == "dismissed" {
		updates["resolved_at"] = new(time.Now().UTC())
	}
	result := r.db.WithContext(ctx).
		Model(&mediaReportRow{}).
		Where("id = ?", id).
		Updates(updates)
	if result.Error != nil {
		return fmt.Errorf("update media report status: %w", result.Error)
	}
	return nil
}

func (r *MediaReportRepository) CountByStatus(ctx context.Context, status string) (int64, error) {
	var count int64
	q := r.db.WithContext(ctx).Model(&mediaReportRow{})
	if status != "" {
		q = q.Where("status = ?", status)
	}
	if err := q.Count(&count).Error; err != nil {
		return 0, fmt.Errorf("count media reports: %w", err)
	}
	return count, nil
}

func mediaReportRowToRecord(row mediaReportRow) *repositories.MediaReportRecord {
	return &repositories.MediaReportRecord{
		ID:         row.ID,
		MediaID:    row.MediaID,
		ReporterID: row.ReporterID,
		Reason:     row.Reason,
		Notes:      row.Notes,
		Status:     row.Status,
		CreatedAt:  row.CreatedAt,
		ResolvedAt: row.ResolvedAt,
		ResolvedBy: row.ResolvedBy,
		IPAddress:  row.IPAddress,
	}
}
