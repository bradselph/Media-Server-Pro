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

// --- Crawler Target Repository ---

type crawlerTargetRow struct {
	ID          string  `gorm:"column:id;primaryKey"`
	Name        string  `gorm:"column:name"`
	URL         string  `gorm:"column:url"`
	Site        string  `gorm:"column:site"`
	Enabled     bool    `gorm:"column:enabled"`
	LastCrawled *string `gorm:"column:last_crawled"`
	CreatedAt   string  `gorm:"column:created_at"`
	UpdatedAt   string  `gorm:"column:updated_at"`
}

func (crawlerTargetRow) TableName() string { return "crawler_targets" }

type CrawlerTargetRepository struct {
	db *gorm.DB
}

func NewCrawlerTargetRepository(db *gorm.DB) repositories.CrawlerTargetRepository {
	return &CrawlerTargetRepository{db: db}
}

func (r *CrawlerTargetRepository) Upsert(ctx context.Context, target *repositories.CrawlerTargetRecord) error {
	row := r.recordToRow(target)
	if err := r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "id"}},
		DoUpdates: clause.AssignmentColumns([]string{"name", "url", "site", "enabled", "updated_at"}),
	}).Create(&row).Error; err != nil {
		return fmt.Errorf("failed to upsert crawler target: %w", err)
	}
	return nil
}

func (r *CrawlerTargetRepository) Get(ctx context.Context, id string) (*repositories.CrawlerTargetRecord, error) {
	var row crawlerTargetRow
	if err := r.db.WithContext(ctx).Where("id = ?", id).First(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get crawler target: %w", err)
	}
	return r.rowToRecord(&row), nil
}

func (r *CrawlerTargetRepository) Delete(ctx context.Context, id string) error {
	if err := r.db.WithContext(ctx).Where("id = ?", id).Delete(&crawlerTargetRow{}).Error; err != nil {
		return fmt.Errorf("failed to delete crawler target: %w", err)
	}
	return nil
}

func (r *CrawlerTargetRepository) List(ctx context.Context) ([]*repositories.CrawlerTargetRecord, error) {
	var rows []crawlerTargetRow
	if err := r.db.WithContext(ctx).Order("created_at DESC").Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("failed to list crawler targets: %w", err)
	}
	records := make([]*repositories.CrawlerTargetRecord, len(rows))
	for i := range rows {
		records[i] = r.rowToRecord(&rows[i])
	}
	return records, nil
}

func (r *CrawlerTargetRepository) UpdateLastCrawled(ctx context.Context, id string, crawledAt time.Time) error {
	ts := crawledAt.Format("2006-01-02 15:04:05")
	if err := r.db.WithContext(ctx).Model(&crawlerTargetRow{}).Where("id = ?", id).
		Update("last_crawled", ts).Error; err != nil {
		return fmt.Errorf("failed to update last crawled: %w", err)
	}
	return nil
}

func (r *CrawlerTargetRepository) recordToRow(rec *repositories.CrawlerTargetRecord) crawlerTargetRow {
	row := crawlerTargetRow{
		ID:        rec.ID,
		Name:      rec.Name,
		URL:       rec.URL,
		Site:      rec.Site,
		Enabled:   rec.Enabled,
		CreatedAt: rec.CreatedAt.Format("2006-01-02 15:04:05"),
		UpdatedAt: rec.UpdatedAt.Format("2006-01-02 15:04:05"),
	}
	if rec.LastCrawled != nil {
		s := rec.LastCrawled.Format("2006-01-02 15:04:05")
		row.LastCrawled = &s
	}
	return row
}

func (r *CrawlerTargetRepository) rowToRecord(row *crawlerTargetRow) *repositories.CrawlerTargetRecord {
	rec := &repositories.CrawlerTargetRecord{
		ID:      row.ID,
		Name:    row.Name,
		URL:     row.URL,
		Site:    row.Site,
		Enabled: row.Enabled,
	}
	if t, err := parseTime(row.CreatedAt); err == nil {
		rec.CreatedAt = t
	}
	if t, err := parseTime(row.UpdatedAt); err == nil {
		rec.UpdatedAt = t
	}
	if row.LastCrawled != nil {
		if t, err := parseTime(*row.LastCrawled); err == nil {
			rec.LastCrawled = &t
		}
	}
	return rec
}

// --- Crawler Discovery Repository ---

type crawlerDiscoveryRow struct {
	ID              string  `gorm:"column:id;primaryKey"`
	TargetID        string  `gorm:"column:target_id"`
	PageURL         string  `gorm:"column:page_url"`
	Title           string  `gorm:"column:title"`
	StreamURL       string  `gorm:"column:stream_url"`
	StreamType      string  `gorm:"column:stream_type"`
	Quality         int     `gorm:"column:quality"`
	DetectionMethod string  `gorm:"column:detection_method"`
	Status          string  `gorm:"column:status"`
	ReviewedBy      string  `gorm:"column:reviewed_by"`
	ReviewedAt      *string `gorm:"column:reviewed_at"`
	DiscoveredAt    string  `gorm:"column:discovered_at"`
}

func (crawlerDiscoveryRow) TableName() string { return "crawler_discoveries" }

type CrawlerDiscoveryRepository struct {
	db *gorm.DB
}

func NewCrawlerDiscoveryRepository(db *gorm.DB) repositories.CrawlerDiscoveryRepository {
	return &CrawlerDiscoveryRepository{db: db}
}

func (r *CrawlerDiscoveryRepository) Create(ctx context.Context, disc *repositories.CrawlerDiscoveryRecord) error {
	row := r.recordToRow(disc)
	if err := r.db.WithContext(ctx).Create(&row).Error; err != nil {
		return fmt.Errorf("failed to create crawler discovery: %w", err)
	}
	return nil
}

func (r *CrawlerDiscoveryRepository) Get(ctx context.Context, id string) (*repositories.CrawlerDiscoveryRecord, error) {
	var row crawlerDiscoveryRow
	if err := r.db.WithContext(ctx).Where("id = ?", id).First(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get crawler discovery: %w", err)
	}
	return r.rowToRecord(&row), nil
}

func (r *CrawlerDiscoveryRepository) Delete(ctx context.Context, id string) error {
	if err := r.db.WithContext(ctx).Where("id = ?", id).Delete(&crawlerDiscoveryRow{}).Error; err != nil {
		return fmt.Errorf("failed to delete crawler discovery: %w", err)
	}
	return nil
}

func (r *CrawlerDiscoveryRepository) List(ctx context.Context) ([]*repositories.CrawlerDiscoveryRecord, error) {
	var rows []crawlerDiscoveryRow
	if err := r.db.WithContext(ctx).Order("discovered_at DESC").Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("failed to list crawler discoveries: %w", err)
	}
	records := make([]*repositories.CrawlerDiscoveryRecord, len(rows))
	for i := range rows {
		records[i] = r.rowToRecord(&rows[i])
	}
	return records, nil
}

func (r *CrawlerDiscoveryRepository) ListByTarget(ctx context.Context, targetID string) ([]*repositories.CrawlerDiscoveryRecord, error) {
	var rows []crawlerDiscoveryRow
	if err := r.db.WithContext(ctx).Where("target_id = ?", targetID).
		Order("discovered_at DESC").Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("failed to list crawler discoveries by target: %w", err)
	}
	records := make([]*repositories.CrawlerDiscoveryRecord, len(rows))
	for i := range rows {
		records[i] = r.rowToRecord(&rows[i])
	}
	return records, nil
}

func (r *CrawlerDiscoveryRepository) ListPending(ctx context.Context) ([]*repositories.CrawlerDiscoveryRecord, error) {
	var rows []crawlerDiscoveryRow
	if err := r.db.WithContext(ctx).Where("status = ?", "pending").
		Order("discovered_at DESC").Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("failed to list pending crawler discoveries: %w", err)
	}
	records := make([]*repositories.CrawlerDiscoveryRecord, len(rows))
	for i := range rows {
		records[i] = r.rowToRecord(&rows[i])
	}
	return records, nil
}

func (r *CrawlerDiscoveryRepository) UpdateStatus(ctx context.Context, id, status, reviewedBy string) error {
	updates := map[string]interface{}{
		"status":      status,
		"reviewed_by": reviewedBy,
		"reviewed_at": time.Now().Format("2006-01-02 15:04:05"),
	}
	if err := r.db.WithContext(ctx).Model(&crawlerDiscoveryRow{}).Where("id = ?", id).
		Updates(updates).Error; err != nil {
		return fmt.Errorf("failed to update crawler discovery status: %w", err)
	}
	return nil
}

func (r *CrawlerDiscoveryRepository) ExistsByStreamURL(ctx context.Context, streamURL string) (bool, error) {
	var count int64
	if err := r.db.WithContext(ctx).Model(&crawlerDiscoveryRow{}).
		Where("stream_url = ?", streamURL).Count(&count).Error; err != nil {
		return false, fmt.Errorf("failed to check crawler discovery existence: %w", err)
	}
	return count > 0, nil
}

func (r *CrawlerDiscoveryRepository) recordToRow(rec *repositories.CrawlerDiscoveryRecord) crawlerDiscoveryRow {
	row := crawlerDiscoveryRow{
		ID:              rec.ID,
		TargetID:        rec.TargetID,
		PageURL:         rec.PageURL,
		Title:           rec.Title,
		StreamURL:       rec.StreamURL,
		StreamType:      rec.StreamType,
		Quality:         rec.Quality,
		DetectionMethod: rec.DetectionMethod,
		Status:          rec.Status,
		ReviewedBy:      rec.ReviewedBy,
		DiscoveredAt:    rec.DiscoveredAt.Format("2006-01-02 15:04:05"),
	}
	if rec.ReviewedAt != nil {
		s := rec.ReviewedAt.Format("2006-01-02 15:04:05")
		row.ReviewedAt = &s
	}
	return row
}

func (r *CrawlerDiscoveryRepository) rowToRecord(row *crawlerDiscoveryRow) *repositories.CrawlerDiscoveryRecord {
	rec := &repositories.CrawlerDiscoveryRecord{
		ID:              row.ID,
		TargetID:        row.TargetID,
		PageURL:         row.PageURL,
		Title:           row.Title,
		StreamURL:       row.StreamURL,
		StreamType:      row.StreamType,
		Quality:         row.Quality,
		DetectionMethod: row.DetectionMethod,
		Status:          row.Status,
		ReviewedBy:      row.ReviewedBy,
		DiscoveredAt:    parseTimeDefault(row.DiscoveredAt),
		ReviewedAt:      parseOptionalTime(row.ReviewedAt),
	}
	return rec
}
