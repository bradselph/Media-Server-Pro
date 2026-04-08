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

type validationResultRow struct {
	Path           string    `gorm:"column:path;primaryKey"`
	Status         string    `gorm:"column:status"`
	ValidatedAt    time.Time `gorm:"column:validated_at"`
	Duration       float64   `gorm:"column:duration"`
	VideoCodec     *string   `gorm:"column:video_codec"`
	AudioCodec     *string   `gorm:"column:audio_codec"`
	Width          int       `gorm:"column:width"`
	Height         int       `gorm:"column:height"`
	Bitrate        int64     `gorm:"column:bitrate"`
	Container      *string   `gorm:"column:container"`
	Issues         string    `gorm:"column:issues;type:json"`
	Error          *string   `gorm:"column:error_message"`
	VideoSupported bool      `gorm:"column:video_supported"`
	AudioSupported bool      `gorm:"column:audio_supported"`
}

func (validationResultRow) TableName() string { return "validation_results" }

type ValidationResultRepository struct {
	db *gorm.DB
}

func NewValidationResultRepository(db *gorm.DB) repositories.ValidationResultRepository {
	return &ValidationResultRepository{db: db}
}

func (r *ValidationResultRepository) Upsert(ctx context.Context, result *repositories.ValidationResultRecord) error {
	row, err := r.recordToRow(result)
	if err != nil {
		return err
	}
	if err := r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "path"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"status", "validated_at", "duration", "video_codec", "audio_codec",
			"width", "height", "bitrate", "container", "issues",
			"error_message", "video_supported", "audio_supported",
		}),
	}).Create(&row).Error; err != nil {
		return fmt.Errorf("failed to upsert validation result: %w", err)
	}
	return nil
}

func (r *ValidationResultRepository) Get(ctx context.Context, path string) (*repositories.ValidationResultRecord, error) {
	var row validationResultRow
	if err := r.db.WithContext(ctx).Where("path = ?", path).First(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get validation result: %w", err)
	}
	return r.rowToRecord(&row), nil
}

func (r *ValidationResultRepository) Delete(ctx context.Context, path string) error {
	result := r.db.WithContext(ctx).Where("path = ?", path).Delete(&validationResultRow{})
	if result.Error != nil {
		return fmt.Errorf("failed to delete validation result: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("validation result not found: %s", path)
	}
	return nil
}

func (r *ValidationResultRepository) List(ctx context.Context) ([]*repositories.ValidationResultRecord, error) {
	var rows []validationResultRow
	if err := r.db.WithContext(ctx).Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("failed to list validation results: %w", err)
	}
	records := make([]*repositories.ValidationResultRecord, len(rows))
	for i := range rows {
		records[i] = r.rowToRecord(&rows[i])
	}
	return records, nil
}

func (r *ValidationResultRepository) recordToRow(rec *repositories.ValidationResultRecord) (validationResultRow, error) {
	issuesJSON, err := json.Marshal(rec.Issues)
	if err != nil {
		return validationResultRow{}, fmt.Errorf("failed to marshal validation issues: %w", err)
	}
	row := validationResultRow{
		Path:           rec.Path,
		Status:         rec.Status,
		ValidatedAt:    rec.ValidatedAt,
		Duration:       rec.Duration,
		Width:          rec.Width,
		Height:         rec.Height,
		Bitrate:        rec.Bitrate,
		Issues:         string(issuesJSON),
		VideoSupported: rec.VideoSupported,
		AudioSupported: rec.AudioSupported,
	}
	if rec.VideoCodec != "" {
		row.VideoCodec = &rec.VideoCodec
	}
	if rec.AudioCodec != "" {
		row.AudioCodec = &rec.AudioCodec
	}
	if rec.Container != "" {
		row.Container = &rec.Container
	}
	if rec.Error != "" {
		row.Error = &rec.Error
	}
	return row, nil
}

func (r *ValidationResultRepository) rowToRecord(row *validationResultRow) *repositories.ValidationResultRecord {
	rec := &repositories.ValidationResultRecord{
		Path:           row.Path,
		Status:         row.Status,
		ValidatedAt:    row.ValidatedAt,
		Duration:       row.Duration,
		Width:          row.Width,
		Height:         row.Height,
		Bitrate:        row.Bitrate,
		VideoSupported: row.VideoSupported,
		AudioSupported: row.AudioSupported,
	}
	if row.VideoCodec != nil {
		rec.VideoCodec = *row.VideoCodec
	}
	if row.AudioCodec != nil {
		rec.AudioCodec = *row.AudioCodec
	}
	if row.Container != nil {
		rec.Container = *row.Container
	}
	if row.Error != nil {
		rec.Error = *row.Error
	}
	_ = json.Unmarshal([]byte(row.Issues), &rec.Issues)
	if rec.Issues == nil {
		rec.Issues = []string{}
	}
	return rec
}
