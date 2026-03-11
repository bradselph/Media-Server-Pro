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

// parseTime attempts to parse a timestamp string in common formats.
func parseTime(s string) (time.Time, error) {
	for _, layout := range []string{
		"2006-01-02 15:04:05",
		time.RFC3339,
		"2006-01-02T15:04:05Z",
	} {
		if t, err := time.Parse(layout, s); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("unable to parse time: %s", s)
}

// parseTimeDefault returns the parsed time or zero value on error (for required time fields).
func parseTimeDefault(s string) time.Time {
	t, _ := parseTime(s)
	return t
}

// parseOptionalTime returns the parsed time or nil on error (for optional time fields).
func parseOptionalTime(s *string) *time.Time {
	if s == nil {
		return nil
	}
	t, err := parseTime(*s)
	if err != nil {
		return nil
	}
	return &t
}

type remoteCacheRow struct {
	RemoteURL   string `gorm:"column:remote_url;primaryKey"`
	LocalPath   string `gorm:"column:local_path"`
	Size        int64  `gorm:"column:file_size"`
	ContentType string `gorm:"column:content_type"`
	CachedAt    string `gorm:"column:cached_at"`
	LastAccess  string `gorm:"column:last_access"`
	Hits        int    `gorm:"column:hits"`
}

func (remoteCacheRow) TableName() string { return "remote_cache_entries" }

type RemoteCacheRepository struct {
	db *gorm.DB
}

func NewRemoteCacheRepository(db *gorm.DB) repositories.RemoteCacheRepository {
	return &RemoteCacheRepository{db: db}
}

func (r *RemoteCacheRepository) Save(ctx context.Context, entry *repositories.RemoteCacheRecord) error {
	row := remoteCacheRow{
		RemoteURL:   entry.RemoteURL,
		LocalPath:   entry.LocalPath,
		Size:        entry.Size,
		ContentType: entry.ContentType,
		CachedAt:    entry.CachedAt.Format("2006-01-02 15:04:05"),
		LastAccess:  entry.LastAccess.Format("2006-01-02 15:04:05"),
		Hits:        entry.Hits,
	}
	if err := r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "remote_url"}},
		DoUpdates: clause.AssignmentColumns([]string{"local_path", "file_size", "content_type", "cached_at", "last_access", "hits"}),
	}).Create(&row).Error; err != nil {
		return fmt.Errorf("failed to save remote cache entry: %w", err)
	}
	return nil
}

func (r *RemoteCacheRepository) Get(ctx context.Context, remoteURL string) (*repositories.RemoteCacheRecord, error) {
	var row remoteCacheRow
	if err := r.db.WithContext(ctx).Where("remote_url = ?", remoteURL).First(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get remote cache entry: %w", err)
	}
	return r.rowToRecord(&row), nil
}

func (r *RemoteCacheRepository) Delete(ctx context.Context, remoteURL string) error {
	if err := r.db.WithContext(ctx).Where("remote_url = ?", remoteURL).Delete(&remoteCacheRow{}).Error; err != nil {
		return fmt.Errorf("failed to delete remote cache entry: %w", err)
	}
	return nil
}

func (r *RemoteCacheRepository) List(ctx context.Context) ([]*repositories.RemoteCacheRecord, error) {
	var rows []remoteCacheRow
	if err := r.db.WithContext(ctx).Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("failed to list remote cache entries: %w", err)
	}
	records := make([]*repositories.RemoteCacheRecord, len(rows))
	for i := range rows {
		records[i] = r.rowToRecord(&rows[i])
	}
	return records, nil
}

func (r *RemoteCacheRepository) rowToRecord(row *remoteCacheRow) *repositories.RemoteCacheRecord {
	rec := &repositories.RemoteCacheRecord{
		RemoteURL:   row.RemoteURL,
		LocalPath:   row.LocalPath,
		Size:        row.Size,
		ContentType: row.ContentType,
		Hits:        row.Hits,
	}
	// Parse timestamps — use zero time on failure
	if t, err := parseTime(row.CachedAt); err == nil {
		rec.CachedAt = t
	}
	if t, err := parseTime(row.LastAccess); err == nil {
		rec.LastAccess = t
	}
	return rec
}
