package mysql

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

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

func (r *ExtractorItemRepository) Upsert(ctx context.Context, item *repositories.ExtractorItemRecord) error {
	row := r.recordToRow(item)
	if err := r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "id"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"source_url", "title", "stream_url", "stream_type", "content_type",
			"quality", "width", "height", "duration", "site", "detection_method",
			"status", "error_message", "resolved_at", "expires_at", "updated_at",
		}),
	}).Create(&row).Error; err != nil {
		return fmt.Errorf("failed to upsert extractor item: %w", err)
	}
	return nil
}

func (r *ExtractorItemRepository) Get(ctx context.Context, id string) (*repositories.ExtractorItemRecord, error) {
	var row extractorItemRow
	if err := r.db.WithContext(ctx).Where(sqlIDEq, id).First(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil //nolint:nilnil // callers check rec == nil explicitly
		}
		return nil, fmt.Errorf("failed to get extractor item: %w", err)
	}
	return r.rowToRecord(&row), nil
}

func (r *ExtractorItemRepository) Delete(ctx context.Context, id string) error {
	result := r.db.WithContext(ctx).Where(sqlIDEq, id).Delete(&extractorItemRow{})
	if result.Error != nil {
		return fmt.Errorf("failed to delete extractor item: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("extractor item not found: %s", id)
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

func (r *ExtractorItemRepository) UpdateStatus(ctx context.Context, id, status, errorMsg string) error {
	updates := map[string]interface{}{
		"status":        status,
		"error_message": errorMsg,
		"updated_at":    time.Now().Format(sqlTimeFormat),
	}
	result := r.db.WithContext(ctx).Model(&extractorItemRow{}).Where(sqlIDEq, id).Updates(updates)
	if result.Error != nil {
		return fmt.Errorf("failed to update extractor item status: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("extractor item not found: %s", id)
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
		ResolvedAt:      rec.ResolvedAt.Format(sqlTimeFormat),
		CreatedAt:       rec.CreatedAt.Format(sqlTimeFormat),
		UpdatedAt:       rec.UpdatedAt.Format(sqlTimeFormat),
	}
	if rec.ExpiresAt != nil {
		s := rec.ExpiresAt.Format(sqlTimeFormat)
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
	} else if row.ResolvedAt != "" {
		log.Printf("[extractor_item_repository] corrupt resolved_at for %s: %v", row.ID, err)
	}
	if row.ExpiresAt != nil {
		if t, err := parseTime(*row.ExpiresAt); err == nil {
			rec.ExpiresAt = &t
		} else {
			log.Printf("[extractor_item_repository] corrupt expires_at for %s: %v", row.ID, err)
		}
	}
	if t, err := parseTime(row.CreatedAt); err == nil {
		rec.CreatedAt = t
	} else if row.CreatedAt != "" {
		log.Printf("[extractor_item_repository] corrupt created_at for %s: %v", row.ID, err)
	}
	if t, err := parseTime(row.UpdatedAt); err == nil {
		rec.UpdatedAt = t
	} else if row.UpdatedAt != "" {
		log.Printf("[extractor_item_repository] corrupt updated_at for %s: %v", row.ID, err)
	}
	return rec
}
