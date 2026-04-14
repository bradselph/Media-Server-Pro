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

// scanResultRow maps to the scan_results table.
type scanResultRow struct {
	Path           string     `gorm:"column:path;primaryKey"`
	IsMature       bool       `gorm:"column:is_mature"`
	Confidence     float64    `gorm:"column:confidence"`
	AutoFlagged    bool       `gorm:"column:auto_flagged"`
	NeedsReview    bool       `gorm:"column:needs_review"`
	ScannedAt      time.Time  `gorm:"column:scanned_at"`
	ReviewedBy     *string    `gorm:"column:reviewed_by"`
	ReviewedAt     *time.Time `gorm:"column:reviewed_at"`
	ReviewDecision *string    `gorm:"column:review_decision"`
}

func (scanResultRow) TableName() string { return "scan_results" }

// scanReasonRow maps to the scan_reasons table.
type scanReasonRow struct {
	Path   string `gorm:"column:path;primaryKey"`
	Reason string `gorm:"column:reason;primaryKey"`
}

func (scanReasonRow) TableName() string { return "scan_reasons" }

// ScanResultRepository implements MySQL storage for scan results using GORM
type ScanResultRepository struct {
	db *gorm.DB
}

// NewScanResultRepository creates a new GORM-backed scan result repository
func NewScanResultRepository(db *gorm.DB) repositories.ScanResultRepository {
	return &ScanResultRepository{db: db}
}

// Save stores or updates a scan result
func (r *ScanResultRepository) Save(ctx context.Context, result *repositories.ScanResult) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Parse scanned_at
		scannedAt, err := time.Parse(time.RFC3339, result.ScannedAt)
		if err != nil {
			scannedAt = time.Now()
		}

		// Parse reviewed_at
		var reviewedAt *time.Time
		if result.ReviewedAt != "" {
			if t, err := time.Parse(time.RFC3339, result.ReviewedAt); err == nil {
				reviewedAt = &t
			}
		}

		row := scanResultRow{
			Path:           result.Path,
			IsMature:       result.IsMature,
			Confidence:     result.Confidence,
			AutoFlagged:    result.AutoFlagged,
			NeedsReview:    result.NeedsReview,
			ScannedAt:      scannedAt,
			ReviewedBy:     nilIfEmpty(result.ReviewedBy),
			ReviewedAt:     reviewedAt,
			ReviewDecision: nilIfEmpty(result.ReviewDecision),
		}

		if err := tx.Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "path"}},
			DoUpdates: clause.AssignmentColumns([]string{
				"is_mature", "confidence", "auto_flagged", "needs_review",
				"scanned_at", "reviewed_by", "reviewed_at", "review_decision",
			}),
		}).Create(&row).Error; err != nil {
			return fmt.Errorf("failed to save scan result: %w", err)
		}

		// Replace reasons: delete old, insert new
		if err := tx.Where(sqlPathEq, result.Path).Delete(&scanReasonRow{}).Error; err != nil {
			return fmt.Errorf("failed to delete old reasons: %w", err)
		}

		if len(result.Reasons) > 0 {
			reasons := make([]scanReasonRow, len(result.Reasons))
			for i, reason := range result.Reasons {
				reasons[i] = scanReasonRow{Path: result.Path, Reason: reason}
			}
			if err := tx.Create(&reasons).Error; err != nil {
				return fmt.Errorf("failed to insert reasons: %w", err)
			}
		}

		return nil
	})
}

// Get retrieves a scan result by path. Returns (nil, ErrScanResultNotFound) when no record exists.
func (r *ScanResultRepository) Get(ctx context.Context, path string) (*repositories.ScanResult, error) {
	var row scanResultRow
	if err := r.db.WithContext(ctx).Where(sqlPathEq, path).First(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, repositories.ErrScanResultNotFound
		}
		return nil, fmt.Errorf("failed to query scan result: %w", err)
	}

	result := r.rowToResult(&row)

	// Get reasons
	var reasons []scanReasonRow
	if err := r.db.WithContext(ctx).Where(sqlPathEq, path).Find(&reasons).Error; err != nil {
		return nil, fmt.Errorf("failed to query reasons: %w", err)
	}
	result.Reasons = make([]string, len(reasons))
	for i, rr := range reasons {
		result.Reasons[i] = rr.Reason
	}

	return result, nil
}

// GetPendingReview retrieves all scan results that need review.
// Uses a batch WHERE IN query for reasons to avoid N+1.
func (r *ScanResultRepository) GetPendingReview(ctx context.Context) ([]*repositories.ScanResult, error) {
	var rows []scanResultRow
	if err := r.db.WithContext(ctx).
		Where("needs_review = ?", true).
		Order("scanned_at DESC").
		Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("failed to query pending reviews: %w", err)
	}

	if len(rows) == 0 {
		return nil, nil
	}

	// Collect paths for batch reason loading
	paths := make([]string, len(rows))
	resultMap := make(map[string]*repositories.ScanResult, len(rows))
	results := make([]*repositories.ScanResult, len(rows))

	for i := range rows {
		result := r.rowToResult(&rows[i])
		result.Reasons = []string{}
		paths[i] = rows[i].Path
		resultMap[rows[i].Path] = result
		results[i] = result
	}

	// Batch load all reasons with WHERE IN (fixes N+1)
	var allReasons []scanReasonRow
	if err := r.db.WithContext(ctx).Where("path IN ?", paths).Find(&allReasons).Error; err != nil {
		return nil, fmt.Errorf("failed to load scan reasons: %w", err)
	}
	for _, rr := range allReasons {
		if result, ok := resultMap[rr.Path]; ok {
			result.Reasons = append(result.Reasons, rr.Reason)
		}
	}

	return results, nil
}

// MarkReviewed updates a scan result with review information
func (r *ScanResultRepository) MarkReviewed(ctx context.Context, path, reviewedBy, decision string) error {
	now := time.Now()
	result := r.db.WithContext(ctx).
		Model(&scanResultRow{}).
		Where(sqlPathEq, path).
		Updates(map[string]interface{}{
			"needs_review":    false,
			"reviewed_by":     reviewedBy,
			"reviewed_at":     now,
			"review_decision": decision,
		})

	if result.Error != nil {
		return fmt.Errorf("failed to mark as reviewed: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("scan result not found: %s", path)
	}
	return nil
}

// Delete removes a scan result by file path.
func (r *ScanResultRepository) Delete(ctx context.Context, path string) error {
	if err := r.db.WithContext(ctx).Where(sqlPathEq, path).Delete(&scanResultRow{}).Error; err != nil {
		return fmt.Errorf("failed to delete scan result: %w", err)
	}
	return nil
}

// rowToResult converts a GORM row to a repository ScanResult.
func (r *ScanResultRepository) rowToResult(row *scanResultRow) *repositories.ScanResult {
	result := &repositories.ScanResult{
		Path:        row.Path,
		IsMature:    row.IsMature,
		Confidence:  row.Confidence,
		AutoFlagged: row.AutoFlagged,
		NeedsReview: row.NeedsReview,
		ScannedAt:   row.ScannedAt.Format(time.RFC3339),
	}

	if row.ReviewedBy != nil {
		result.ReviewedBy = *row.ReviewedBy
	}
	if row.ReviewedAt != nil {
		result.ReviewedAt = row.ReviewedAt.Format(time.RFC3339)
	}
	if row.ReviewDecision != nil {
		result.ReviewDecision = *row.ReviewDecision
	}

	return result
}

// nilIfEmpty returns nil for empty strings, or a pointer to the string.
func nilIfEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
