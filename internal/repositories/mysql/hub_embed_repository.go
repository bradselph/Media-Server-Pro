package mysql

import (
	"context"
	"fmt"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"media-server-pro/internal/repositories"
)

// hubEmbedRow is the GORM row model for the hub_embeds table (BETA Hub feature).
type hubEmbedRow struct {
	ID           uint64    `gorm:"column:id;primaryKey"`
	EmbedID      string    `gorm:"column:embed_id"`
	Title        string    `gorm:"column:title"`
	Pornstar     string    `gorm:"column:pornstar"`
	DurationSecs int       `gorm:"column:duration_secs"`
	Views        int64     `gorm:"column:views"`
	RatingUp     int       `gorm:"column:rating_up"`
	RatingDown   int       `gorm:"column:rating_down"`
	Tags         string    `gorm:"column:tags"`
	Categories   string    `gorm:"column:categories"`
	ThumbURL     string    `gorm:"column:thumb_url"`
	PreviewURLs  string    `gorm:"column:preview_urls"`
	CreatedAt    time.Time `gorm:"column:created_at;autoCreateTime"`
}

func (hubEmbedRow) TableName() string { return "hub_embeds" }

// HubEmbedRepository is the MySQL implementation of repositories.HubEmbedRepository.
type HubEmbedRepository struct {
	db *gorm.DB
}

// NewHubEmbedRepository constructs the repository over the given GORM handle.
func NewHubEmbedRepository(db *gorm.DB) repositories.HubEmbedRepository {
	if db == nil {
		panic("NewHubEmbedRepository: db is nil")
	}
	return &HubEmbedRepository{db: db}
}

// BatchInsert idempotently inserts embeds using INSERT IGNORE (OnConflict DoNothing
// on the embed_id unique key), returning the number of rows actually inserted.
func (r *HubEmbedRepository) BatchInsert(ctx context.Context, embeds []*repositories.HubEmbedRecord) (int64, error) {
	if len(embeds) == 0 {
		return 0, nil
	}
	rows := make([]hubEmbedRow, len(embeds))
	for i, e := range embeds {
		rows[i] = hubRecordToRow(e)
	}
	// CreateInBatches keeps each INSERT statement bounded so a MEDIUMTEXT column
	// can't blow past max_allowed_packet on a large chunk.
	const batchSize = 1000
	result := r.db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).CreateInBatches(rows, batchSize)
	if result.Error != nil {
		return 0, fmt.Errorf("failed to batch insert hub embeds: %w", result.Error)
	}
	return result.RowsAffected, nil
}

// List returns a page ordered by sort plus the total row count.
func (r *HubEmbedRepository) List(ctx context.Context, offset, limit int, sort string) ([]*repositories.HubEmbedRecord, int64, error) {
	limit, offset = hubClampPage(limit, offset)
	q := r.db.WithContext(ctx).Model(&hubEmbedRow{})
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, fmt.Errorf("count hub embeds: %w", err)
	}
	var rows []hubEmbedRow
	if err := q.Order(hubSortOrder(sort)).Limit(limit).Offset(offset).Find(&rows).Error; err != nil {
		return nil, 0, fmt.Errorf("list hub embeds: %w", err)
	}
	return hubRowsToRecords(rows), total, nil
}

// Search filters by a full-text query and/or category/tag. Short queries (below
// InnoDB's ft_min_word_len) fall back to an indexed title-prefix LIKE so partial
// single-word searches still return something rather than an empty FULLTEXT set.
func (r *HubEmbedRepository) Search(ctx context.Context, query string, filter repositories.HubEmbedFilter, offset, limit int) ([]*repositories.HubEmbedRecord, int64, error) {
	limit, offset = hubClampPage(limit, offset)
	q := r.db.WithContext(ctx).Model(&hubEmbedRow{})
	if query != "" {
		if len(query) >= 3 {
			q = q.Where("MATCH(title, tags) AGAINST(? IN NATURAL LANGUAGE MODE)", query)
		} else {
			q = q.Where("title LIKE ?", query+"%")
		}
	}
	if filter.Category != "" {
		q = q.Where("categories LIKE ?", "%"+filter.Category+"%")
	}
	if filter.Tag != "" {
		q = q.Where("tags LIKE ?", "%"+filter.Tag+"%")
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, fmt.Errorf("count hub embed search: %w", err)
	}
	var rows []hubEmbedRow
	if err := q.Order("views DESC").Limit(limit).Offset(offset).Find(&rows).Error; err != nil {
		return nil, 0, fmt.Errorf("search hub embeds: %w", err)
	}
	return hubRowsToRecords(rows), total, nil
}

// GetByEmbedID returns a single embed by its natural key, or (nil, nil) if absent.
func (r *HubEmbedRepository) GetByEmbedID(ctx context.Context, embedID string) (*repositories.HubEmbedRecord, error) {
	var row hubEmbedRow
	err := r.db.WithContext(ctx).Where("embed_id = ?", embedID).First(&row).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, fmt.Errorf("get hub embed: %w", err)
	}
	rec := hubRowToRecord(row)
	return &rec, nil
}

// GetByEmbedIDs returns all embeds whose embed_id is in the given set.
func (r *HubEmbedRepository) GetByEmbedIDs(ctx context.Context, embedIDs []string) ([]*repositories.HubEmbedRecord, error) {
	if len(embedIDs) == 0 {
		return nil, nil
	}
	var rows []hubEmbedRow
	if err := r.db.WithContext(ctx).Where("embed_id IN ?", embedIDs).Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("get hub embeds batch: %w", err)
	}
	return hubRowsToRecords(rows), nil
}

// CountAll returns the total number of imported rows.
func (r *HubEmbedRepository) CountAll(ctx context.Context) (int64, error) {
	var count int64
	err := r.db.WithContext(ctx).Model(&hubEmbedRow{}).Count(&count).Error
	return count, err
}

// CategorySamples returns the raw ';'-joined category strings from the most-viewed
// rows so the caller can build a facet list without a full-table DISTINCT scan.
func (r *HubEmbedRepository) CategorySamples(ctx context.Context, limit int) ([]string, error) {
	if limit <= 0 || limit > 20000 {
		limit = 5000
	}
	var cats []string
	err := r.db.WithContext(ctx).Model(&hubEmbedRow{}).
		Where("categories <> ''").
		Order("views DESC").
		Limit(limit).
		Pluck("categories", &cats).Error
	if err != nil {
		return nil, fmt.Errorf("sample hub categories: %w", err)
	}
	return cats, nil
}

// DeleteAll truncates the catalog table.
func (r *HubEmbedRepository) DeleteAll(ctx context.Context) error {
	return r.db.WithContext(ctx).Exec("TRUNCATE TABLE hub_embeds").Error
}

// hubClampPage bounds limit/offset to safe ranges.
func hubClampPage(limit, offset int) (int, int) {
	if limit <= 0 || limit > 200 {
		limit = 60
	}
	if offset < 0 {
		offset = 0
	}
	return limit, offset
}

func hubSortOrder(sort string) string {
	switch sort {
	case "views":
		return "views DESC"
	case "duration":
		return "duration_secs DESC"
	case "title":
		return "title ASC"
	default:
		return "id DESC"
	}
}

func hubRecordToRow(rec *repositories.HubEmbedRecord) hubEmbedRow {
	// ID and CreatedAt intentionally omitted: id is AUTO_INCREMENT and created_at
	// is populated by the DB default / autoCreateTime.
	return hubEmbedRow{
		EmbedID:      rec.EmbedID,
		Title:        rec.Title,
		Pornstar:     rec.Pornstar,
		DurationSecs: rec.DurationSecs,
		Views:        rec.Views,
		RatingUp:     rec.RatingUp,
		RatingDown:   rec.RatingDown,
		Tags:         rec.Tags,
		Categories:   rec.Categories,
		ThumbURL:     rec.ThumbURL,
		PreviewURLs:  rec.PreviewURLs,
	}
}

func hubRowToRecord(row hubEmbedRow) repositories.HubEmbedRecord {
	return repositories.HubEmbedRecord{
		ID:           row.ID,
		EmbedID:      row.EmbedID,
		Title:        row.Title,
		Pornstar:     row.Pornstar,
		DurationSecs: row.DurationSecs,
		Views:        row.Views,
		RatingUp:     row.RatingUp,
		RatingDown:   row.RatingDown,
		Tags:         row.Tags,
		Categories:   row.Categories,
		ThumbURL:     row.ThumbURL,
		PreviewURLs:  row.PreviewURLs,
		CreatedAt:    row.CreatedAt,
	}
}

func hubRowsToRecords(rows []hubEmbedRow) []*repositories.HubEmbedRecord {
	out := make([]*repositories.HubEmbedRecord, len(rows))
	for i := range rows {
		rec := hubRowToRecord(rows[i])
		out[i] = &rec
	}
	return out
}
