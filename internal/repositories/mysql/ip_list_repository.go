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

type ipListConfigRow struct {
	ListType string `gorm:"column:list_type;primaryKey"`
	Name     string `gorm:"column:name"`
	Enabled  bool   `gorm:"column:enabled"`
}

func (ipListConfigRow) TableName() string { return "ip_list_config" }

type ipListEntryRow struct {
	ListType  string     `gorm:"column:list_type;primaryKey"`
	IPValue   string     `gorm:"column:ip_value;primaryKey"`
	Comment   *string    `gorm:"column:comment"`
	AddedAt   time.Time  `gorm:"column:added_at"`
	AddedBy   *string    `gorm:"column:added_by"`
	ExpiresAt *time.Time `gorm:"column:expires_at"`
}

func (ipListEntryRow) TableName() string { return "ip_list_entries" }

type IPListRepository struct {
	db *gorm.DB
}

func NewIPListRepository(db *gorm.DB) repositories.IPListRepository {
	return &IPListRepository{db: db}
}

func (r *IPListRepository) SaveListConfig(ctx context.Context, listType string, name string, enabled bool) error {
	row := ipListConfigRow{
		ListType: listType,
		Name:     name,
		Enabled:  enabled,
	}
	if err := r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "list_type"}},
		DoUpdates: clause.AssignmentColumns([]string{"name", "enabled"}),
	}).Create(&row).Error; err != nil {
		return fmt.Errorf("failed to save IP list config: %w", err)
	}
	return nil
}

func (r *IPListRepository) GetListConfig(ctx context.Context, listType string) (string, bool, error) {
	var row ipListConfigRow
	if err := r.db.WithContext(ctx).Where("list_type = ?", listType).First(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return "", false, nil
		}
		return "", false, fmt.Errorf("failed to get IP list config: %w", err)
	}
	return row.Name, row.Enabled, nil
}

func (r *IPListRepository) SaveEntries(ctx context.Context, listType string, entries []*repositories.IPEntryRecord) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Delete existing entries for this list type
		if err := tx.Where("list_type = ?", listType).Delete(&ipListEntryRow{}).Error; err != nil {
			return fmt.Errorf("failed to clear IP list entries: %w", err)
		}
		if len(entries) == 0 {
			return nil
		}
		rows := make([]ipListEntryRow, len(entries))
		for i, e := range entries {
			rows[i] = ipListEntryRow{
				ListType:  listType,
				IPValue:   e.Value,
				AddedAt:   e.AddedAt,
				ExpiresAt: e.ExpiresAt,
			}
			if e.Comment != "" {
				rows[i].Comment = &e.Comment
			}
			if e.AddedBy != "" {
				rows[i].AddedBy = &e.AddedBy
			}
		}
		if err := tx.Create(&rows).Error; err != nil {
			return fmt.Errorf("failed to save IP list entries: %w", err)
		}
		return nil
	})
}

func (r *IPListRepository) GetEntries(ctx context.Context, listType string) ([]*repositories.IPEntryRecord, error) {
	var rows []ipListEntryRow
	if err := r.db.WithContext(ctx).Where("list_type = ?", listType).Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("failed to get IP list entries: %w", err)
	}
	records := make([]*repositories.IPEntryRecord, len(rows))
	for i := range rows {
		rec := &repositories.IPEntryRecord{
			ListType:  rows[i].ListType,
			Value:     rows[i].IPValue,
			AddedAt:   rows[i].AddedAt,
			ExpiresAt: rows[i].ExpiresAt,
		}
		if rows[i].Comment != nil {
			rec.Comment = *rows[i].Comment
		}
		if rows[i].AddedBy != nil {
			rec.AddedBy = *rows[i].AddedBy
		}
		records[i] = rec
	}
	return records, nil
}

func (r *IPListRepository) AddEntry(ctx context.Context, listType string, entry *repositories.IPEntryRecord) error {
	row := ipListEntryRow{
		ListType:  listType,
		IPValue:   entry.Value,
		AddedAt:   entry.AddedAt,
		ExpiresAt: entry.ExpiresAt,
	}
	if entry.Comment != "" {
		row.Comment = &entry.Comment
	}
	if entry.AddedBy != "" {
		row.AddedBy = &entry.AddedBy
	}
	if err := r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "list_type"}, {Name: "ip_value"}},
		DoUpdates: clause.AssignmentColumns([]string{"comment", "added_at", "added_by", "expires_at"}),
	}).Create(&row).Error; err != nil {
		return fmt.Errorf("failed to add IP list entry: %w", err)
	}
	return nil
}

func (r *IPListRepository) RemoveEntry(ctx context.Context, listType string, ipValue string) error {
	if err := r.db.WithContext(ctx).Where("list_type = ? AND ip_value = ?", listType, ipValue).Delete(&ipListEntryRow{}).Error; err != nil {
		return fmt.Errorf("failed to remove IP list entry: %w", err)
	}
	return nil
}

// TODO: Silent failure — SetEnabled does not check RowsAffected. If the listType
// does not exist in ip_list_config, the update silently succeeds with 0 rows. The
// caller will think the list was toggled but nothing was actually changed. Consider
// checking RowsAffected == 0 and returning an error, or auto-creating the config
// row via Upsert.
func (r *IPListRepository) SetEnabled(ctx context.Context, listType string, enabled bool) error {
	if err := r.db.WithContext(ctx).Model(&ipListConfigRow{}).Where("list_type = ?", listType).Update("enabled", enabled).Error; err != nil {
		return fmt.Errorf("failed to set IP list enabled: %w", err)
	}
	return nil
}
