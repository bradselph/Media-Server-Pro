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

type categorizedItemRow struct {
	Path            string    `gorm:"column:path;primaryKey"`
	ID              string    `gorm:"column:id"`
	Name            string    `gorm:"column:name"`
	Category        string    `gorm:"column:category"`
	Confidence      float64   `gorm:"column:confidence"`
	DetectedTitle   *string   `gorm:"column:detected_title"`
	DetectedYear    *int      `gorm:"column:detected_year"`
	DetectedSeason  *int      `gorm:"column:detected_season"`
	DetectedEpisode *int      `gorm:"column:detected_episode"`
	DetectedShow    *string   `gorm:"column:detected_show"`
	DetectedArtist  *string   `gorm:"column:detected_artist"`
	DetectedAlbum   *string   `gorm:"column:detected_album"`
	CategorizedAt   time.Time `gorm:"column:categorized_at"`
	ManualOverride  bool      `gorm:"column:manual_override"`
}

func (categorizedItemRow) TableName() string { return "categorized_items" }

type CategorizedItemRepository struct {
	db *gorm.DB
}

func NewCategorizedItemRepository(db *gorm.DB) repositories.CategorizedItemRepository {
	return &CategorizedItemRepository{db: db}
}

func (r *CategorizedItemRepository) Upsert(ctx context.Context, item *repositories.CategorizedItemRecord) error {
	row := r.recordToRow(item)
	if err := r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "path"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"id", "name", "category", "confidence",
			"detected_title", "detected_year", "detected_season", "detected_episode",
			"detected_show", "detected_artist", "detected_album",
			"categorized_at", "manual_override",
		}),
	}).Create(&row).Error; err != nil {
		return fmt.Errorf("failed to upsert categorized item: %w", err)
	}
	return nil
}

func (r *CategorizedItemRepository) Get(ctx context.Context, path string) (*repositories.CategorizedItemRecord, error) {
	var row categorizedItemRow
	if err := r.db.WithContext(ctx).Where("path = ?", path).First(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get categorized item: %w", err)
	}
	return r.rowToRecord(&row), nil
}

func (r *CategorizedItemRepository) Delete(ctx context.Context, path string) error {
	if err := r.db.WithContext(ctx).Where("path = ?", path).Delete(&categorizedItemRow{}).Error; err != nil {
		return fmt.Errorf("failed to delete categorized item: %w", err)
	}
	return nil
}

func (r *CategorizedItemRepository) List(ctx context.Context) ([]*repositories.CategorizedItemRecord, error) {
	var rows []categorizedItemRow
	if err := r.db.WithContext(ctx).Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("failed to list categorized items: %w", err)
	}
	records := make([]*repositories.CategorizedItemRecord, len(rows))
	for i := range rows {
		records[i] = r.rowToRecord(&rows[i])
	}
	return records, nil
}

func (r *CategorizedItemRepository) recordToRow(rec *repositories.CategorizedItemRecord) categorizedItemRow {
	row := categorizedItemRow{
		Path:           rec.Path,
		ID:             rec.ID,
		Name:           rec.Name,
		Category:       rec.Category,
		Confidence:     rec.Confidence,
		CategorizedAt:  rec.CategorizedAt,
		ManualOverride: rec.ManualOverride,
	}
	if rec.DetectedTitle != "" {
		row.DetectedTitle = &rec.DetectedTitle
	}
	if rec.DetectedYear != 0 {
		row.DetectedYear = &rec.DetectedYear
	}
	if rec.DetectedSeason != 0 {
		row.DetectedSeason = &rec.DetectedSeason
	}
	if rec.DetectedEpisode != 0 {
		row.DetectedEpisode = &rec.DetectedEpisode
	}
	if rec.DetectedShow != "" {
		row.DetectedShow = &rec.DetectedShow
	}
	if rec.DetectedArtist != "" {
		row.DetectedArtist = &rec.DetectedArtist
	}
	if rec.DetectedAlbum != "" {
		row.DetectedAlbum = &rec.DetectedAlbum
	}
	return row
}

func (r *CategorizedItemRepository) rowToRecord(row *categorizedItemRow) *repositories.CategorizedItemRecord {
	rec := &repositories.CategorizedItemRecord{
		Path:           row.Path,
		ID:             row.ID,
		Name:           row.Name,
		Category:       row.Category,
		Confidence:     row.Confidence,
		CategorizedAt:  row.CategorizedAt,
		ManualOverride: row.ManualOverride,
	}
	if row.DetectedTitle != nil {
		rec.DetectedTitle = *row.DetectedTitle
	}
	if row.DetectedYear != nil {
		rec.DetectedYear = *row.DetectedYear
	}
	if row.DetectedSeason != nil {
		rec.DetectedSeason = *row.DetectedSeason
	}
	if row.DetectedEpisode != nil {
		rec.DetectedEpisode = *row.DetectedEpisode
	}
	if row.DetectedShow != nil {
		rec.DetectedShow = *row.DetectedShow
	}
	if row.DetectedArtist != nil {
		rec.DetectedArtist = *row.DetectedArtist
	}
	if row.DetectedAlbum != nil {
		rec.DetectedAlbum = *row.DetectedAlbum
	}
	return rec
}
