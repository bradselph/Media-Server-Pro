package mysql

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"media-server-pro/internal/repositories"
)

type autodiscoveryRow struct {
	OriginalPath  string  `gorm:"column:original_path;primaryKey"`
	SuggestedName *string `gorm:"column:suggested_name"`
	SuggestedPath *string `gorm:"column:suggested_path"`
	Type          string  `gorm:"column:type"`
	Confidence    float64 `gorm:"column:confidence"`
	Metadata      string  `gorm:"column:metadata;type:json"`
}

func (autodiscoveryRow) TableName() string { return "autodiscovery_suggestions" }

type AutoDiscoverySuggestionRepository struct {
	db *gorm.DB
}

func NewAutoDiscoverySuggestionRepository(db *gorm.DB) repositories.AutoDiscoverySuggestionRepository {
	return &AutoDiscoverySuggestionRepository{db: db}
}

func (r *AutoDiscoverySuggestionRepository) Save(ctx context.Context, suggestion *repositories.AutoDiscoveryRecord) error {
	metaJSON, err := json.Marshal(suggestion.Metadata)
	if err != nil {
		return fmt.Errorf("failed to marshal metadata: %w", err)
	}
	row := autodiscoveryRow{
		OriginalPath: suggestion.OriginalPath,
		Type:         suggestion.Type,
		Confidence:   suggestion.Confidence,
		Metadata:     string(metaJSON),
	}
	if suggestion.SuggestedName != "" {
		row.SuggestedName = &suggestion.SuggestedName
	}
	if suggestion.SuggestedPath != "" {
		row.SuggestedPath = &suggestion.SuggestedPath
	}
	if err := r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "original_path"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"suggested_name", "suggested_path", "type", "confidence", "metadata",
		}),
	}).Create(&row).Error; err != nil {
		return fmt.Errorf("failed to save autodiscovery suggestion: %w", err)
	}
	return nil
}

func (r *AutoDiscoverySuggestionRepository) Get(ctx context.Context, originalPath string) (*repositories.AutoDiscoveryRecord, error) {
	var row autodiscoveryRow
	if err := r.db.WithContext(ctx).Where("original_path = ?", originalPath).First(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil //nolint:nilnil // callers check rec == nil explicitly
		}
		return nil, fmt.Errorf("failed to get autodiscovery suggestion: %w", err)
	}
	rec, err := r.rowToRecord(&row)
	if err != nil {
		return nil, fmt.Errorf("failed to decode autodiscovery suggestion %q metadata: %w", originalPath, err)
	}
	return rec, nil
}

func (r *AutoDiscoverySuggestionRepository) Delete(ctx context.Context, originalPath string) error {
	result := r.db.WithContext(ctx).Where("original_path = ?", originalPath).Delete(&autodiscoveryRow{})
	if result.Error != nil {
		return fmt.Errorf("failed to delete autodiscovery suggestion: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("autodiscovery suggestion not found: %s", originalPath)
	}
	return nil
}

func (r *AutoDiscoverySuggestionRepository) List(ctx context.Context) ([]*repositories.AutoDiscoveryRecord, error) {
	var rows []autodiscoveryRow
	if err := r.db.WithContext(ctx).Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("failed to list autodiscovery suggestions: %w", err)
	}
	records := make([]*repositories.AutoDiscoveryRecord, len(rows))
	for i := range rows {
		rec, err := r.rowToRecord(&rows[i])
		if err != nil {
			return nil, fmt.Errorf("failed to decode autodiscovery suggestion %q metadata: %w", rows[i].OriginalPath, err)
		}
		records[i] = rec
	}
	return records, nil
}

func (r *AutoDiscoverySuggestionRepository) DeleteAll(ctx context.Context) error {
	if err := r.db.WithContext(ctx).Session(&gorm.Session{AllowGlobalUpdate: true}).Delete(&autodiscoveryRow{}).Error; err != nil {
		return fmt.Errorf("failed to delete all autodiscovery suggestions: %w", err)
	}
	return nil
}

func (r *AutoDiscoverySuggestionRepository) rowToRecord(row *autodiscoveryRow) (*repositories.AutoDiscoveryRecord, error) {
	rec := &repositories.AutoDiscoveryRecord{
		OriginalPath: row.OriginalPath,
		Type:         row.Type,
		Confidence:   row.Confidence,
	}
	if row.SuggestedName != nil {
		rec.SuggestedName = *row.SuggestedName
	}
	if row.SuggestedPath != nil {
		rec.SuggestedPath = *row.SuggestedPath
	}
	if row.Metadata == "" {
		rec.Metadata = make(map[string]string)
	} else if err := json.Unmarshal([]byte(row.Metadata), &rec.Metadata); err != nil {
		return nil, err
	}
	if rec.Metadata == nil {
		rec.Metadata = make(map[string]string)
	}
	return rec, nil
}
