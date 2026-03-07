package mysql

import (
	"context"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"

	"media-server-pro/internal/repositories"
)

type receiverDuplicateRow struct {
	ID           string  `gorm:"column:id;primaryKey"`
	Fingerprint  string  `gorm:"column:fingerprint"`
	ItemAID      string  `gorm:"column:item_a_id"`
	ItemASlaveID string  `gorm:"column:item_a_slave_id"`
	ItemAName    string  `gorm:"column:item_a_name"`
	ItemBID      string  `gorm:"column:item_b_id"`
	ItemBSlaveID string  `gorm:"column:item_b_slave_id"`
	ItemBName    string  `gorm:"column:item_b_name"`
	Status       string  `gorm:"column:status"`
	ResolvedBy   string  `gorm:"column:resolved_by"`
	ResolvedAt   *string `gorm:"column:resolved_at"`
	DetectedAt   string  `gorm:"column:detected_at"`
}

func (receiverDuplicateRow) TableName() string { return "receiver_duplicates" }

// ReceiverDuplicateRepository implements repositories.ReceiverDuplicateRepository.
type ReceiverDuplicateRepository struct {
	db *gorm.DB
}

// NewReceiverDuplicateRepository creates a new receiver duplicate repository.
func NewReceiverDuplicateRepository(db *gorm.DB) repositories.ReceiverDuplicateRepository {
	return &ReceiverDuplicateRepository{db: db}
}

func (r *ReceiverDuplicateRepository) Create(ctx context.Context, dup *repositories.ReceiverDuplicateRecord) error {
	row := receiverDuplicateRow{
		ID:           dup.ID,
		Fingerprint:  dup.Fingerprint,
		ItemAID:      dup.ItemAID,
		ItemASlaveID: dup.ItemASlaveID,
		ItemAName:    dup.ItemAName,
		ItemBID:      dup.ItemBID,
		ItemBSlaveID: dup.ItemBSlaveID,
		ItemBName:    dup.ItemBName,
		Status:       dup.Status,
		DetectedAt:   dup.DetectedAt.Format("2006-01-02 15:04:05"),
	}
	if err := r.db.WithContext(ctx).Create(&row).Error; err != nil {
		return fmt.Errorf("failed to create duplicate record: %w", err)
	}
	return nil
}

func (r *ReceiverDuplicateRepository) Get(ctx context.Context, id string) (*repositories.ReceiverDuplicateRecord, error) {
	var row receiverDuplicateRow
	if err := r.db.WithContext(ctx).Where("id = ?", id).First(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get duplicate record: %w", err)
	}
	return r.rowToRecord(&row), nil
}

func (r *ReceiverDuplicateRepository) List(ctx context.Context) ([]*repositories.ReceiverDuplicateRecord, error) {
	var rows []receiverDuplicateRow
	if err := r.db.WithContext(ctx).Order("detected_at DESC").Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("failed to list duplicates: %w", err)
	}
	return r.rowsToRecords(rows), nil
}

func (r *ReceiverDuplicateRepository) ListPending(ctx context.Context) ([]*repositories.ReceiverDuplicateRecord, error) {
	var rows []receiverDuplicateRow
	if err := r.db.WithContext(ctx).Where("status = ?", "pending").Order("detected_at DESC").Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("failed to list pending duplicates: %w", err)
	}
	return r.rowsToRecords(rows), nil
}

// ExistsByPair checks whether a duplicate record already exists for the given pair (either ordering).
func (r *ReceiverDuplicateRepository) ExistsByPair(ctx context.Context, itemAID, itemBID string) (bool, error) {
	var count int64
	err := r.db.WithContext(ctx).Model(&receiverDuplicateRow{}).
		Where("(item_a_id = ? AND item_b_id = ?) OR (item_a_id = ? AND item_b_id = ?)",
			itemAID, itemBID, itemBID, itemAID).
		Count(&count).Error
	if err != nil {
		return false, fmt.Errorf("failed to check pair existence: %w", err)
	}
	return count > 0, nil
}

// ExistsResolvedRemoval reports whether any record with the given fingerprint was
// previously resolved via remove_a or remove_b.  Used to suppress re-detection of
// receiver items that get re-pushed by a slave after being removed.
func (r *ReceiverDuplicateRepository) ExistsResolvedRemoval(ctx context.Context, fingerprint string) (bool, error) {
	var count int64
	err := r.db.WithContext(ctx).Model(&receiverDuplicateRow{}).
		Where("fingerprint = ? AND status IN ('remove_a', 'remove_b')", fingerprint).
		Count(&count).Error
	if err != nil {
		return false, fmt.Errorf("failed to check resolved removal: %w", err)
	}
	return count > 0, nil
}

func (r *ReceiverDuplicateRepository) UpdateStatus(ctx context.Context, id, status, resolvedBy string) error {
	updates := map[string]interface{}{
		"status":      status,
		"resolved_by": resolvedBy,
		"resolved_at": time.Now().Format("2006-01-02 15:04:05"),
	}
	if err := r.db.WithContext(ctx).Model(&receiverDuplicateRow{}).Where("id = ?", id).Updates(updates).Error; err != nil {
		return fmt.Errorf("failed to update duplicate status: %w", err)
	}
	return nil
}

// UpdateStatusForItem marks all pending duplicate records that reference the given item ID
// (on either side) with the given status.  Used to cascade resolution when an item is removed.
func (r *ReceiverDuplicateRepository) UpdateStatusForItem(ctx context.Context, itemID, status, resolvedBy string) error {
	updates := map[string]interface{}{
		"status":      status,
		"resolved_by": resolvedBy,
		"resolved_at": time.Now().Format("2006-01-02 15:04:05"),
	}
	if err := r.db.WithContext(ctx).Model(&receiverDuplicateRow{}).
		Where("(item_a_id = ? OR item_b_id = ?) AND status = 'pending'", itemID, itemID).
		Updates(updates).Error; err != nil {
		return fmt.Errorf("failed to cascade duplicate status for item %s: %w", itemID, err)
	}
	return nil
}

func (r *ReceiverDuplicateRepository) CountPending(ctx context.Context) (int64, error) {
	var count int64
	if err := r.db.WithContext(ctx).Model(&receiverDuplicateRow{}).Where("status = ?", "pending").Count(&count).Error; err != nil {
		return 0, fmt.Errorf("failed to count pending duplicates: %w", err)
	}
	return count, nil
}

// DeleteForItem removes all duplicate records that reference the given item ID (either side).
func (r *ReceiverDuplicateRepository) DeleteForItem(ctx context.Context, itemID string) error {
	if err := r.db.WithContext(ctx).
		Where("item_a_id = ? OR item_b_id = ?", itemID, itemID).
		Delete(&receiverDuplicateRow{}).Error; err != nil {
		return fmt.Errorf("failed to delete duplicates for item: %w", err)
	}
	return nil
}

func (r *ReceiverDuplicateRepository) rowToRecord(row *receiverDuplicateRow) *repositories.ReceiverDuplicateRecord {
	rec := &repositories.ReceiverDuplicateRecord{
		ID:           row.ID,
		Fingerprint:  row.Fingerprint,
		ItemAID:      row.ItemAID,
		ItemASlaveID: row.ItemASlaveID,
		ItemAName:    row.ItemAName,
		ItemBID:      row.ItemBID,
		ItemBSlaveID: row.ItemBSlaveID,
		ItemBName:    row.ItemBName,
		Status:       row.Status,
		ResolvedBy:   row.ResolvedBy,
	}
	if t, err := parseTime(row.DetectedAt); err == nil {
		rec.DetectedAt = t
	}
	if row.ResolvedAt != nil {
		if t, err := parseTime(*row.ResolvedAt); err == nil {
			rec.ResolvedAt = &t
		}
	}
	return rec
}

func (r *ReceiverDuplicateRepository) rowsToRecords(rows []receiverDuplicateRow) []*repositories.ReceiverDuplicateRecord {
	records := make([]*repositories.ReceiverDuplicateRecord, len(rows))
	for i := range rows {
		records[i] = r.rowToRecord(&rows[i])
	}
	return records
}
