package mysql

import (
	"context"
	"errors"
	"fmt"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"media-server-pro/internal/repositories"
)

type extractorItemRow struct {
	ID              string  `gorm:"column:id;primaryKey"`
	SourceURL       string  `gorm:"column:source_url"`
	Title           string  `gorm:"column:title"`
	StreamURL       string  `gorm:"column:stream_url"`
	StreamType      string  `gorm:"column:stream_type"`
	ContentType     string  `gorm:"column:content_type"`
	Quality         string  `gorm:"column:quality"`
	Width           int     `gorm:"column:width"`
	Height          int     `gorm:"column:height"`
	Duration        float64 `gorm:"column:duration"`
	Site            string  `gorm:"column:site"`
	DetectionMethod string  `gorm:"column:detection_method"`
	Status          string  `gorm:"column:status"`
	ErrorMessage    string  `gorm:"column:error_message"`
	AddedBy         string  `gorm:"column:added_by"`
	ResolvedAt      string  `gorm:"column:resolved_at"`
	ExpiresAt       *string `gorm:"column:expires_at"`
	CreatedAt       string  `gorm:"column:created_at"`
	UpdatedAt       string  `gorm:"column:updated_at"`
}

func (extractorItemRow) TableName() string { return "extractor_items" }

// ExtractorItemRepository implements repositories.ExtractorItemRepository using GORM.
type ExtractorItemRepository struct {
	db *gorm.DB
}

// NewExtractorItemRepository creates a new ExtractorItemRepository.
func NewExtractorItemRepository(db *gorm.DB) repositories.ExtractorItemRepository {
	return &ExtractorItemRepository{db: db}
}

// TODO: Bug — Upsert's OnConflict DoUpdates list does not include "updated_at", so
// re-upserting an existing item preserves the original updated_at timestamp. The
// "added_by" and "created_at" columns are also excluded from updates (intentionally,
// as they should be immutable), but "updated_at" should be refreshed on conflict.
func (r *ExtractorItemRepository) Upsert(ctx context.Context, item *repositories.ExtractorItemRecord) error {
	row := r.recordToRow(item)
	if err := r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "id"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"source_url", "title", "stream_url", "stream_type", "content_type",
			"quality", "width", "height", "duration", "site", "detection_method",
			"status", "error_message", "resolved_at", "expires_at",
		}),
	}).Create(&row).Error; err != nil {
		return fmt.Errorf("failed to upsert extractor item: %w", err)
	}
	return nil
}

func (r *ExtractorItemRepository) Get(ctx context.Context, id string) (*repositories.ExtractorItemRecord, error) {
	var row extractorItemRow
	if err := r.db.WithContext(ctx).Where("id = ?", id).First(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get extractor item: %w", err)
	}
	return r.rowToRecord(&row), nil
}

func (r *ExtractorItemRepository) Delete(ctx context.Context, id string) error {
	if err := r.db.WithContext(ctx).Where("id = ?", id).Delete(&extractorItemRow{}).Error; err != nil {
		return fmt.Errorf("failed to delete extractor item: %w", err)
	}
	return nil
}

func (r *ExtractorItemRepository) List(ctx context.Context) ([]*repositories.ExtractorItemRecord, error) {
	var rows []extractorItemRow
	if err := r.db.WithContext(ctx).Order("created_at DESC").Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("failed to list extractor items: %w", err)
	}
	records := make([]*repositories.ExtractorItemRecord, len(rows))
	for i := range rows {
		records[i] = r.rowToRecord(&rows[i])
	}
	return records, nil
}

func (r *ExtractorItemRepository) ListActive(ctx context.Context) ([]*repositories.ExtractorItemRecord, error) {
	var rows []extractorItemRow
	if err := r.db.WithContext(ctx).Where("status = ?", "active").Order("created_at DESC").Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("failed to list active extractor items: %w", err)
	}
	records := make([]*repositories.ExtractorItemRecord, len(rows))
	for i := range rows {
		records[i] = r.rowToRecord(&rows[i])
	}
	return records, nil
}

// TODO: Bug — UpdateStatus does not update the "updated_at" column. When the status
// changes (e.g. "active" -> "expired"), the updated_at timestamp still reflects the
// original creation/upsert time. Add "updated_at" to the updates map with the current
// timestamp. Same issue exists in CrawlerDiscoveryRepository.UpdateStatus and
// ReceiverDuplicateRepository.UpdateStatus (which does update resolved_at but not
// a general updated_at).
func (r *ExtractorItemRepository) UpdateStatus(ctx context.Context, id, status, errorMsg string) error {
	updates := map[string]interface{}{
		"status":        status,
		"error_message": errorMsg,
	}
	if err := r.db.WithContext(ctx).Model(&extractorItemRow{}).Where("id = ?", id).Updates(updates).Error; err != nil {
		return fmt.Errorf("failed to update extractor item status: %w", err)
	}
	return nil
}

func (r *ExtractorItemRepository) recordToRow(rec *repositories.ExtractorItemRecord) extractorItemRow {
	row := extractorItemRow{
		ID:              rec.ID,
		SourceURL:       rec.SourceURL,
		Title:           rec.Title,
		StreamURL:       rec.StreamURL,
		StreamType:      rec.StreamType,
		ContentType:     rec.ContentType,
		Quality:         rec.Quality,
		Width:           rec.Width,
		Height:          rec.Height,
		Duration:        rec.Duration,
		Site:            rec.Site,
		DetectionMethod: rec.DetectionMethod,
		Status:          rec.Status,
		ErrorMessage:    rec.ErrorMessage,
		AddedBy:         rec.AddedBy,
		ResolvedAt:      rec.ResolvedAt.Format("2006-01-02 15:04:05"),
		CreatedAt:       rec.CreatedAt.Format("2006-01-02 15:04:05"),
		UpdatedAt:       rec.UpdatedAt.Format("2006-01-02 15:04:05"),
	}
	if rec.ExpiresAt != nil {
		s := rec.ExpiresAt.Format("2006-01-02 15:04:05")
		row.ExpiresAt = &s
	}
	return row
}

func (r *ExtractorItemRepository) rowToRecord(row *extractorItemRow) *repositories.ExtractorItemRecord {
	rec := &repositories.ExtractorItemRecord{
		ID:              row.ID,
		SourceURL:       row.SourceURL,
		Title:           row.Title,
		StreamURL:       row.StreamURL,
		StreamType:      row.StreamType,
		ContentType:     row.ContentType,
		Quality:         row.Quality,
		Width:           row.Width,
		Height:          row.Height,
		Duration:        row.Duration,
		Site:            row.Site,
		DetectionMethod: row.DetectionMethod,
		Status:          row.Status,
		ErrorMessage:    row.ErrorMessage,
		AddedBy:         row.AddedBy,
	}
	if t, err := parseTime(row.ResolvedAt); err == nil {
		rec.ResolvedAt = t
	}
	if row.ExpiresAt != nil {
		if t, err := parseTime(*row.ExpiresAt); err == nil {
			rec.ExpiresAt = &t
		}
	}
	if t, err := parseTime(row.CreatedAt); err == nil {
		rec.CreatedAt = t
	}
	if t, err := parseTime(row.UpdatedAt); err == nil {
		rec.UpdatedAt = t
	}
	return rec
}
