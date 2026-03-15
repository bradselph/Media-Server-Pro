package mysql

import (
	"context"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"media-server-pro/internal/repositories"
)

// --- Slave Repository ---

// receiverSlaveRow maps DB columns (LastSeen/CreatedAt as strings for compatibility).
type receiverSlaveRow struct {
	ID         string `gorm:"column:id;primaryKey"`
	Name       string `gorm:"column:name"`
	BaseURL    string `gorm:"column:base_url"`
	Status     string `gorm:"column:status"`
	MediaCount int    `gorm:"column:media_count"`
	LastSeen   string `gorm:"column:last_seen"`
	CreatedAt  string `gorm:"column:created_at"`
}

func (receiverSlaveRow) TableName() string { return "receiver_slaves" }

// ReceiverSlaveRepository implements repositories.ReceiverSlaveRepository.
type ReceiverSlaveRepository struct {
	db *gorm.DB
}

// NewReceiverSlaveRepository creates a new receiver slave repository.
func NewReceiverSlaveRepository(db *gorm.DB) repositories.ReceiverSlaveRepository {
	return &ReceiverSlaveRepository{db: db}
}

func (r *ReceiverSlaveRepository) Upsert(ctx context.Context, slave *repositories.ReceiverSlaveRecord) error {
	row := receiverSlaveRow{
		ID:         slave.ID,
		Name:       slave.Name,
		BaseURL:    slave.BaseURL,
		Status:     slave.Status,
		MediaCount: slave.MediaCount,
		LastSeen:   slave.LastSeen.Format("2006-01-02 15:04:05"),
		CreatedAt:  slave.CreatedAt.Format("2006-01-02 15:04:05"),
	}
	if err := r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "id"}},
		DoUpdates: clause.AssignmentColumns([]string{"name", "base_url", "status", "media_count", "last_seen"}),
	}).Create(&row).Error; err != nil {
		return fmt.Errorf("failed to upsert slave record: %w", err)
	}
	return nil
}

func (r *ReceiverSlaveRepository) Get(ctx context.Context, slaveID string) (*repositories.ReceiverSlaveRecord, error) {
	var row receiverSlaveRow
	if err := r.db.WithContext(ctx).Where("id = ?", slaveID).First(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get slave record: %w", err)
	}
	return r.rowToSlaveRecord(&row), nil
}

func (r *ReceiverSlaveRepository) Delete(ctx context.Context, slaveID string) error {
	result := r.db.WithContext(ctx).Where("id = ?", slaveID).Delete(&receiverSlaveRow{})
	if result.Error != nil {
		return fmt.Errorf("failed to delete slave record: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("slave record not found: %s", slaveID)
	}
	return nil
}

func (r *ReceiverSlaveRepository) List(ctx context.Context) ([]*repositories.ReceiverSlaveRecord, error) {
	var rows []receiverSlaveRow
	if err := r.db.WithContext(ctx).Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("failed to list slave records: %w", err)
	}
	records := make([]*repositories.ReceiverSlaveRecord, len(rows))
	for i := range rows {
		records[i] = r.rowToSlaveRecord(&rows[i])
	}
	return records, nil
}

func (r *ReceiverSlaveRepository) rowToSlaveRecord(row *receiverSlaveRow) *repositories.ReceiverSlaveRecord {
	rec := &repositories.ReceiverSlaveRecord{
		ID:         row.ID,
		Name:       row.Name,
		BaseURL:    row.BaseURL,
		Status:     row.Status,
		MediaCount: row.MediaCount,
	}
	if t, err := parseTime(row.LastSeen); err == nil {
		rec.LastSeen = t
	}
	if t, err := parseTime(row.CreatedAt); err == nil {
		rec.CreatedAt = t
	}
	return rec
}

// --- Media Repository ---

type receiverMediaRow struct {
	ID                 string  `gorm:"column:id;primaryKey"`
	SlaveID            string  `gorm:"column:slave_id"`
	RemotePath         string  `gorm:"column:remote_path"`
	Name               string  `gorm:"column:name"`
	MediaType          string  `gorm:"column:media_type"`
	Size               int64   `gorm:"column:file_size"`
	Duration           float64 `gorm:"column:duration"`
	ContentType        string  `gorm:"column:content_type"`
	ContentFingerprint string  `gorm:"column:content_fingerprint"`
	Width              int     `gorm:"column:width"`
	Height             int     `gorm:"column:height"`
	UpdatedAt          string  `gorm:"column:updated_at"`
}

func (receiverMediaRow) TableName() string { return "receiver_media" }

// ReceiverMediaRepository implements repositories.ReceiverMediaRepository.
type ReceiverMediaRepository struct {
	db *gorm.DB
}

// NewReceiverMediaRepository creates a new receiver media repository.
func NewReceiverMediaRepository(db *gorm.DB) repositories.ReceiverMediaRepository {
	return &ReceiverMediaRepository{db: db}
}

func (r *ReceiverMediaRepository) UpsertBatch(ctx context.Context, slaveID string, items []*repositories.ReceiverMediaRecord) error {
	if len(items) == 0 {
		return nil
	}

	rows := make([]receiverMediaRow, len(items))
	for i, item := range items {
		rows[i] = receiverMediaRow{
			ID:                 item.ID,
			SlaveID:            slaveID,
			RemotePath:         item.RemotePath,
			Name:               item.Name,
			MediaType:          item.MediaType,
			Size:               item.Size,
			Duration:           item.Duration,
			ContentType:        item.ContentType,
			ContentFingerprint: item.ContentFingerprint,
			Width:              item.Width,
			Height:             item.Height,
			UpdatedAt:          time.Now().Format("2006-01-02 15:04:05"),
		}
	}

	// Batch upsert in chunks of 100 inside a transaction so partial failure rolls back all batches.
	const batchSize = 100
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for start := 0; start < len(rows); start += batchSize {
			end := start + batchSize
			if end > len(rows) {
				end = len(rows)
			}
			batch := rows[start:end]
			if err := tx.Clauses(clause.OnConflict{
				Columns: []clause.Column{{Name: "id"}},
				DoUpdates: clause.AssignmentColumns([]string{
					"remote_path", "name", "media_type", "file_size", "duration",
					"content_type", "content_fingerprint", "width", "height", "updated_at",
				}),
			}).Create(&batch).Error; err != nil {
				return fmt.Errorf("failed to upsert media batch: %w", err)
			}
		}
		return nil
	})
}

func (r *ReceiverMediaRepository) Get(ctx context.Context, id string) (*repositories.ReceiverMediaRecord, error) {
	var row receiverMediaRow
	if err := r.db.WithContext(ctx).Where("id = ?", id).First(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get receiver media record: %w", err)
	}
	return r.rowToMediaRecord(&row), nil
}

func (r *ReceiverMediaRepository) ListBySlave(ctx context.Context, slaveID string) ([]*repositories.ReceiverMediaRecord, error) {
	var rows []receiverMediaRow
	if err := r.db.WithContext(ctx).Where("slave_id = ?", slaveID).Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("failed to list media by slave: %w", err)
	}
	return r.rowsToMediaRecords(rows), nil
}

func (r *ReceiverMediaRepository) ListAll(ctx context.Context) ([]*repositories.ReceiverMediaRecord, error) {
	var rows []receiverMediaRow
	if err := r.db.WithContext(ctx).Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("failed to list all receiver media: %w", err)
	}
	return r.rowsToMediaRecords(rows), nil
}

func (r *ReceiverMediaRepository) DeleteBySlave(ctx context.Context, slaveID string) error {
	if err := r.db.WithContext(ctx).Where("slave_id = ?", slaveID).Delete(&receiverMediaRow{}).Error; err != nil {
		return fmt.Errorf("failed to delete media by slave: %w", err)
	}
	return nil
}

func (r *ReceiverMediaRepository) DeleteByID(ctx context.Context, id string) error {
	result := r.db.WithContext(ctx).Where("id = ?", id).Delete(&receiverMediaRow{})
	if result.Error != nil {
		return fmt.Errorf("failed to delete receiver media record: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("receiver media record not found: %s", id)
	}
	return nil
}

func (r *ReceiverMediaRepository) Search(ctx context.Context, query string) ([]*repositories.ReceiverMediaRecord, error) {
	var rows []receiverMediaRow
	pattern := "%" + escapeLike(query) + "%"
	if err := r.db.WithContext(ctx).Where("name LIKE ? ESCAPE '\\\\'", pattern).Limit(100).Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("failed to search receiver media: %w", err)
	}
	return r.rowsToMediaRecords(rows), nil
}

func (r *ReceiverMediaRepository) rowToMediaRecord(row *receiverMediaRow) *repositories.ReceiverMediaRecord {
	rec := &repositories.ReceiverMediaRecord{
		ID:                 row.ID,
		SlaveID:            row.SlaveID,
		RemotePath:         row.RemotePath,
		Name:               row.Name,
		MediaType:          row.MediaType,
		Size:               row.Size,
		Duration:           row.Duration,
		ContentType:        row.ContentType,
		ContentFingerprint: row.ContentFingerprint,
		Width:              row.Width,
		Height:             row.Height,
	}
	if t, err := parseTime(row.UpdatedAt); err == nil {
		rec.UpdatedAt = t
	}
	return rec
}

func (r *ReceiverMediaRepository) rowsToMediaRecords(rows []receiverMediaRow) []*repositories.ReceiverMediaRecord {
	records := make([]*repositories.ReceiverMediaRecord, len(rows))
	for i := range rows {
		records[i] = r.rowToMediaRecord(&rows[i])
	}
	return records
}
