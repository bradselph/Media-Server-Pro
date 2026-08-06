package mysql

import (
	"context"
	"fmt"
	"sync"
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

// hubTotalTTL bounds how long the unfiltered row count is reused. The catalog
// only changes on import or clear, both of which invalidate the cache outright,
// so this is a backstop for writes that bypass this process.
const hubTotalTTL = 5 * time.Minute

// HubEmbedRepository is the MySQL implementation of repositories.HubEmbedRepository.
type HubEmbedRepository struct {
	db *gorm.DB

	// Unfiltered COUNT(*) cache. InnoDB has no O(1) row count, so on a catalog of
	// millions of rows the count behind every browse page costs more than the
	// page of rows itself. The mutex is held across the query on purpose: it
	// collapses a burst of concurrent cold reads into one count.
	totalMu    sync.Mutex
	totalValue int64
	totalAt    time.Time
	totalValid bool

	// Filtered reads. Kept as two caches rather than one because the entries have
	// wildly different sizes: a count is an int64 and a page is up to 200 records
	// carrying preview URLs, so a shared entry bound would either starve the
	// counts or blow the memory budget on pages. See hub_embed_cache.go.
	countCache *hubResultCache
	pageCache  *hubResultCache

	// Presence of the categories FULLTEXT index, re-checked periodically. See
	// hub_embed_search.go.
	ftIndexState
}

// NewHubEmbedRepository constructs the repository over the given GORM handle.
func NewHubEmbedRepository(db *gorm.DB) repositories.HubEmbedRepository {
	if db == nil {
		panic("NewHubEmbedRepository: db is nil")
	}
	return &HubEmbedRepository{
		db:         db,
		countCache: newHubResultCache(hubCountCacheMax, hubFilterTTL),
		pageCache:  newHubResultCache(hubPageCacheMax, hubFilterTTL),
	}
}

// cachedTotal returns the unfiltered row count, refreshing it past the TTL.
func (r *HubEmbedRepository) cachedTotal(ctx context.Context) (int64, error) {
	r.totalMu.Lock()
	defer r.totalMu.Unlock()
	if r.totalValid && time.Since(r.totalAt) < hubTotalTTL {
		return r.totalValue, nil
	}
	var total int64
	if err := r.db.WithContext(ctx).Model(&hubEmbedRow{}).Count(&total).Error; err != nil {
		return 0, fmt.Errorf("count hub embeds: %w", err)
	}
	r.storeTotal(total)
	return total, nil
}

func (r *HubEmbedRepository) storeTotal(total int64) {
	r.totalValue = total
	r.totalAt = time.Now()
	r.totalValid = true
}

// invalidateTotal drops every cached read after a write.
//
// An import or a clear can change any row, so per-key invalidation would be
// guesswork; dropping everything is correct and cheap, because writes happen at
// import time rather than in the browse path.
func (r *HubEmbedRepository) invalidateTotal() {
	r.totalMu.Lock()
	r.totalValid = false
	r.totalMu.Unlock()
	if r.countCache != nil {
		r.countCache.clear()
	}
	if r.pageCache != nil {
		r.pageCache.clear()
	}
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
	r.invalidateTotal()
	return result.RowsAffected, nil
}

// hubUpsertColumns are the mutable content columns refreshed by BatchUpsert on an
// embed_id conflict. Deliberately excludes id (AUTO_INCREMENT primary key) and
// created_at (the original import time) so a re-import preserves both — only the
// catalog data that can change between snapshots is updated.
var hubUpsertColumns = []string{
	"title", "pornstar", "duration_secs", "views",
	"rating_up", "rating_down", "tags", "categories",
	"thumb_url", "preview_urls",
}

// BatchUpsert inserts new embeds and refreshes existing ones in one
// INSERT ... ON DUPLICATE KEY UPDATE per batch. This is the re-import path: an
// updated catalog snapshot adds its new rows and updates any changed rows in
// place, so there is never a duplicate and never a destructive TRUNCATE+full
// reinsert to "update a few new rows". Rows whose columns are identical to what's
// stored are not rewritten (MySQL treats an ON DUPLICATE KEY UPDATE with no value
// change as a no-op). Returns the driver's affected-row count (insert = 1, real
// update = 2, unchanged = 0).
//
// On MySQL the UPDATE clause fires for ANY unique/PK collision — the statement has
// no conflict-target syntax. hub_embeds has exactly one non-PK unique key
// (embed_id) and BatchUpsert never sets id (AUTO_INCREMENT), so the only reachable
// conflict today is on embed_id. The clause.OnConflict.Columns below is INERT on
// MySQL (it only scopes the target on Postgres); it is kept for dialect
// portability and to document intent. If a new unique index is ever added to
// hub_embeds, revisit this — the update would fire for that collision too.
func (r *HubEmbedRepository) BatchUpsert(ctx context.Context, embeds []*repositories.HubEmbedRecord) (int64, error) {
	if len(embeds) == 0 {
		return 0, nil
	}
	rows := make([]hubEmbedRow, len(embeds))
	for i, e := range embeds {
		rows[i] = hubRecordToRow(e)
	}
	const batchSize = 1000
	result := r.db.WithContext(ctx).Clauses(clause.OnConflict{
		// Inert on MySQL (Postgres-only conflict target); see the note above.
		Columns:   []clause.Column{{Name: "embed_id"}},
		DoUpdates: clause.AssignmentColumns(hubUpsertColumns),
	}).CreateInBatches(rows, batchSize)
	if result.Error != nil {
		return 0, fmt.Errorf("failed to batch upsert hub embeds: %w", result.Error)
	}
	r.invalidateTotal()
	return result.RowsAffected, nil
}

// List returns a page ordered by sort plus the total row count.
//
// The count comes from cachedTotal rather than a fresh COUNT(*): the unfiltered
// total is identical for every page and every viewer, and recomputing it per
// request dominated the cost of browsing a large catalog.
func (r *HubEmbedRepository) List(ctx context.Context, offset, limit int, sort string) ([]*repositories.HubEmbedRecord, int64, error) {
	limit, offset = hubClampPage(limit, offset)
	total, err := r.cachedTotal(ctx)
	if err != nil {
		return nil, 0, err
	}
	// Cached too. Each sort has a covering (sortkey, id) index so page 1 is fast,
	// but LIMIT/OFFSET still walks the index from the start — a viewer deep in the
	// catalog pays for every row they skipped. Caching makes paging back and forth
	// through a range free.
	records, err := hubMemo(r.pageCache, "list\x00"+hubPageKey("", sort, offset, limit),
		func() ([]*repositories.HubEmbedRecord, error) {
			var rows []hubEmbedRow
			if fErr := r.db.WithContext(ctx).Model(&hubEmbedRow{}).
				Order(hubSortOrder(sort)).Limit(limit).Offset(offset).Find(&rows).Error; fErr != nil {
				return nil, fmt.Errorf("list hub embeds: %w", fErr)
			}
			return hubRowsToRecords(rows), nil
		})
	if err != nil {
		return nil, 0, err
	}
	return records, total, nil
}

// Search filters by a full-text query and/or category/tag. Short queries (below
// InnoDB's ft_min_word_len) fall back to an indexed title-prefix LIKE so partial
// single-word searches still return something rather than an empty FULLTEXT set.
// Both the count and the page are served from cache when possible. They are
// cached separately because the count depends only on the predicate: it is the
// same for every page and every sort of a given filter, so paging through a
// category or flipping its sort order reuses one count instead of recomputing a
// full-table aggregate each time. See hub_embed_cache.go for why this matters so
// much more on the filtered path than the unfiltered one.
func (r *HubEmbedRepository) Search(ctx context.Context, query string, filter repositories.HubEmbedFilter, offset, limit int) ([]*repositories.HubEmbedRecord, int64, error) {
	limit, offset = hubClampPage(limit, offset)

	// Resolved once per call rather than inside applyFilter, so the count and the
	// page are always built with the same plan and a single cached lookup serves
	// both.
	var ftInfo categoryIndexInfo
	if filter.Category != "" {
		ftInfo = r.categoryIndexReady(ctx)
	}

	// applyFilter rebuilds the predicate per query. A single *gorm.DB cannot be
	// shared between the Count and the Find here: the two now run on independent
	// cache paths and may not both execute.
	applyFilter := func() *gorm.DB {
		q := r.db.WithContext(ctx).Model(&hubEmbedRow{})
		if query != "" {
			if len(query) >= 3 {
				q = q.Where("MATCH(title, tags) AGAINST(? IN NATURAL LANGUAGE MODE)", query)
			} else {
				q = q.Where("title LIKE ?", query+"%")
			}
		}
		// Categories and tags are stored as ';'-joined lists, so wrap the column in
		// sentinel delimiters and match the exact facet value between them. This
		// avoids the substring over-match a bare LIKE '%val%' would cause (e.g. the
		// facet "Teen" matching a stored "Teenager"). The leading-wildcard pattern
		// is already a scan either way, so there is no added index cost.
		if filter.Category != "" {
			// Narrow with the FULLTEXT index first where we safely can. This does
			// not change which rows come back — the exact LIKE below still decides
			// that — it only stops MySQL from reading the whole table to find them.
			if ftInfo.usable {
				if expr, ok := hubCategoryMatchExpr(filter.Category, ftInfo.minTokenSize); ok {
					q = q.Where("MATCH(categories) AGAINST(? IN BOOLEAN MODE)", expr)
				}
			}
			q = q.Where("CONCAT(';', categories, ';') LIKE ? ESCAPE '\\\\'", "%;"+escapeLike(filter.Category)+";%")
		}
		if filter.Tag != "" {
			q = q.Where("CONCAT(';', tags, ';') LIKE ? ESCAPE '\\\\'", "%;"+escapeLike(filter.Tag)+";%")
		}
		return q
	}

	filterKey := hubFilterKey(query, filter.Category, filter.Tag)
	total, err := hubMemo(r.countCache, "count\x00"+filterKey, func() (int64, error) {
		var n int64
		if cErr := applyFilter().Count(&n).Error; cErr != nil {
			return 0, fmt.Errorf("count hub embed search: %w", cErr)
		}
		return n, nil
	})
	if err != nil {
		return nil, 0, err
	}

	records, err := hubMemo(r.pageCache, "search\x00"+hubPageKey(filterKey, filter.SortBy, offset, limit),
		func() ([]*repositories.HubEmbedRecord, error) {
			var rows []hubEmbedRow
			// Honor the caller's sort in the filtered path too — previously this was
			// pinned to views DESC, so picking Longest/Title/Newest while a search or
			// category filter was active silently reverted to most-viewed.
			if fErr := applyFilter().Order(hubSortOrder(filter.SortBy)).
				Limit(limit).Offset(offset).Find(&rows).Error; fErr != nil {
				return nil, fmt.Errorf("search hub embeds: %w", fErr)
			}
			return hubRowsToRecords(rows), nil
		})
	if err != nil {
		return nil, 0, err
	}
	return records, total, nil
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
// CountAll returns the exact current row count.
//
// Deliberately uncached: the auto-import bootstrap decides whether the catalog
// is empty from this, and acting on a stale zero would re-import a populated
// catalog. It does refresh the cache that List reads.
func (r *HubEmbedRepository) CountAll(ctx context.Context) (int64, error) {
	var count int64
	if err := r.db.WithContext(ctx).Model(&hubEmbedRow{}).Count(&count).Error; err != nil {
		return 0, err
	}
	r.totalMu.Lock()
	r.storeTotal(count)
	r.totalMu.Unlock()
	return count, nil
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
	if err := r.db.WithContext(ctx).Exec("TRUNCATE TABLE hub_embeds").Error; err != nil {
		return err
	}
	r.invalidateTotal()
	return nil
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

// hubSortOrder maps a sort key to an ORDER BY clause.
//
// Every clause ends in the unique id column. That tiebreaker is not cosmetic: a
// huge share of catalog rows share a view count or duration, and without a
// unique final key MySQL is free to order equal rows differently between
// queries — so paging through results silently shows some items twice and skips
// others. Each indexed sort has a matching (key, id) index; see the hub_embeds
// entries in internal/database/migrations.go.
func hubSortOrder(sort string) string {
	switch sort {
	case "views":
		return "views DESC, id DESC"
	case "duration":
		return "duration_secs DESC, id DESC"
	case "rating":
		// rating_score is a generated column: a vote-count-smoothed up/down ratio,
		// so a lone 1-0 item cannot outrank a heavily-rated one.
		return "rating_score DESC, id DESC"
	case "title":
		// No usable index: title is only prefix-indexed, which MySQL cannot use to
		// avoid a sort. Kept because it is cheap for filtered result sets.
		return "title ASC, id ASC"
	default:
		// "newest" and any unknown value: most recently imported first.
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
