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

type backupManifestRow struct {
	ID          string    `gorm:"column:id;primaryKey"`
	Filename    string    `gorm:"column:filename"`
	CreatedAt   time.Time `gorm:"column:created_at"`
	Size        int64     `gorm:"column:size"`
	Type        *string   `gorm:"column:type"`
	Description *string   `gorm:"column:description"`
	Files       string    `gorm:"column:files;type:json"`
	Errors      string    `gorm:"column:errors;type:json"`
	Version     *string   `gorm:"column:version"`
}

func (backupManifestRow) TableName() string { return "backup_manifests" }

type BackupManifestRepository struct {
	db *gorm.DB
}

func NewBackupManifestRepository(db *gorm.DB) repositories.BackupManifestRepository {
	return &BackupManifestRepository{db: db}
}

func (r *BackupManifestRepository) Save(ctx context.Context, manifest *repositories.BackupManifestRecord) error {
	if manifest == nil {
		return fmt.Errorf("manifest must not be nil")
	}
	filesJSON, err := json.Marshal(manifest.Files)
	if err != nil {
		return fmt.Errorf("failed to marshal backup manifest files: %w", err)
	}
	errorsJSON, err := json.Marshal(manifest.Errors)
	if err != nil {
		return fmt.Errorf("failed to marshal backup manifest errors: %w", err)
	}
	row := backupManifestRow{
		ID:        manifest.ID,
		Filename:  manifest.Filename,
		CreatedAt: manifest.CreatedAt,
		Size:      manifest.Size,
		Files:     string(filesJSON),
		Errors:    string(errorsJSON),
	}
	if manifest.Type != "" {
		row.Type = &manifest.Type
	}
	if manifest.Description != "" {
		row.Description = &manifest.Description
	}
	if manifest.Version != "" {
		row.Version = &manifest.Version
	}
	if err := r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "id"}},
		DoUpdates: clause.AssignmentColumns([]string{"filename", "created_at", "size", "type", "description", "files", "errors", "version"}),
	}).Create(&row).Error; err != nil {
		return fmt.Errorf("failed to save backup manifest: %w", err)
	}
	return nil
}

func (r *BackupManifestRepository) Get(ctx context.Context, id string) (*repositories.BackupManifestRecord, error) {
	var row backupManifestRow
	if err := r.db.WithContext(ctx).Where("id = ?", id).First(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil //nolint:nilnil // callers check rec == nil explicitly
		}
		return nil, fmt.Errorf("failed to get backup manifest: %w", err)
	}
	rec, err := r.rowToRecord(&row)
	if err != nil {
		return nil, fmt.Errorf("failed to decode backup manifest %q JSON payload: %w", id, err)
	}
	return rec, nil
}

func (r *BackupManifestRepository) Delete(ctx context.Context, id string) error {
	result := r.db.WithContext(ctx).Where("id = ?", id).Delete(&backupManifestRow{})
	if result.Error != nil {
		return fmt.Errorf("failed to delete backup manifest: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("backup manifest not found: %s", id)
	}
	return nil
}

func (r *BackupManifestRepository) List(ctx context.Context) ([]*repositories.BackupManifestRecord, error) {
	var rows []backupManifestRow
	if err := r.db.WithContext(ctx).Order("created_at DESC").Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("failed to list backup manifests: %w", err)
	}
	records := make([]*repositories.BackupManifestRecord, len(rows))
	for i := range rows {
		rec, err := r.rowToRecord(&rows[i])
		if err != nil {
			return nil, fmt.Errorf("failed to decode backup manifest %q JSON payload: %w", rows[i].ID, err)
		}
		records[i] = rec
	}
	return records, nil
}

func (r *BackupManifestRepository) rowToRecord(row *backupManifestRow) (*repositories.BackupManifestRecord, error) {
	rec := &repositories.BackupManifestRecord{
		ID:        row.ID,
		Filename:  row.Filename,
		CreatedAt: row.CreatedAt,
		Size:      row.Size,
	}
	if row.Type != nil {
		rec.Type = *row.Type
	}
	if row.Description != nil {
		rec.Description = *row.Description
	}
	if row.Version != nil {
		rec.Version = *row.Version
	}
	if err := json.Unmarshal([]byte(row.Files), &rec.Files); err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(row.Errors), &rec.Errors); err != nil {
		return nil, err
	}
	if rec.Files == nil {
		rec.Files = []string{}
	}
	if rec.Errors == nil {
		rec.Errors = []string{}
	}
	return rec, nil
}
