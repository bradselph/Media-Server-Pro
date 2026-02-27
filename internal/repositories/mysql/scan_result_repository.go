package mysql

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"media-server-pro/internal/logger"
	"media-server-pro/internal/repositories"
)

// ScanResultRepository implements MySQL storage for scan results
type ScanResultRepository struct {
	db  *sql.DB
	log *logger.Logger
}

// NewScanResultRepository creates a new MySQL scan result repository
func NewScanResultRepository(db *sql.DB) repositories.ScanResultRepository {
	return &ScanResultRepository{
		db:  db,
		log: logger.New("scan-result-repo"),
	}
}

// Save stores or updates a scan result
func (r *ScanResultRepository) Save(ctx context.Context, result *repositories.ScanResult) error {
	// Begin transaction for atomic save of result + reasons
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() {
		if err := tx.Rollback(); err != nil && !errors.Is(err, sql.ErrTxDone) {
			r.log.Warn("Failed to rollback transaction: %v", err)
		}
	}()

	// Convert RFC3339 datetime to MySQL format
	scannedAt, err := parseTimeToMySQL(result.ScannedAt)
	if err != nil {
		r.log.Warn("Failed to parse scanned_at datetime, using NOW(): %v", err)
		scannedAt = time.Now()
	}

	// Parse ReviewedAt if present
	var reviewedAtValue sql.NullTime
	if result.ReviewedAt != "" {
		reviewedAt, err := parseTimeToMySQL(result.ReviewedAt)
		if err != nil {
			r.log.Warn("Failed to parse reviewed_at datetime: %v", err)
		} else {
			reviewedAtValue = sql.NullTime{Time: reviewedAt, Valid: true}
		}
	}

	// Upsert scan result
	_, err = tx.ExecContext(ctx, `
		INSERT INTO scan_results
			(path, is_mature, confidence, auto_flagged, needs_review, scanned_at,
			 reviewed_by, reviewed_at, review_decision)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON DUPLICATE KEY UPDATE
			is_mature = VALUES(is_mature),
			confidence = VALUES(confidence),
			auto_flagged = VALUES(auto_flagged),
			needs_review = VALUES(needs_review),
			scanned_at = VALUES(scanned_at),
			reviewed_by = VALUES(reviewed_by),
			reviewed_at = VALUES(reviewed_at),
			review_decision = VALUES(review_decision)
	`,
		result.Path,
		result.IsMature,
		result.Confidence,
		result.AutoFlagged,
		result.NeedsReview,
		scannedAt,
		nullString(result.ReviewedBy),
		reviewedAtValue,
		nullString(result.ReviewDecision),
	)
	if err != nil {
		return fmt.Errorf("failed to save scan result: %w", err)
	}

	// Delete old reasons and insert new ones
	_, err = tx.ExecContext(ctx, "DELETE FROM scan_reasons WHERE path = ?", result.Path)
	if err != nil {
		return fmt.Errorf("failed to delete old reasons: %w", err)
	}

	// Insert new reasons
	if len(result.Reasons) > 0 {
		for _, reason := range result.Reasons {
			_, err = tx.ExecContext(ctx,
				"INSERT INTO scan_reasons (path, reason) VALUES (?, ?)",
				result.Path, reason)
			if err != nil {
				return fmt.Errorf("failed to insert reason: %w", err)
			}
		}
	}

	// Commit transaction
	if err = tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	r.log.Debug("Saved scan result for: %s (mature: %v, confidence: %.2f)",
		result.Path, result.IsMature, result.Confidence)
	return nil
}

// Get retrieves a scan result by path
func (r *ScanResultRepository) Get(ctx context.Context, path string) (*repositories.ScanResult, error) {
	result := &repositories.ScanResult{Path: path}

	var reviewedBy, reviewDecision sql.NullString
	var scannedAt time.Time
	var reviewedAt sql.NullTime

	err := r.db.QueryRowContext(ctx, `
		SELECT is_mature, confidence, auto_flagged, needs_review, scanned_at,
		       reviewed_by, reviewed_at, review_decision
		FROM scan_results
		WHERE path = ?
	`, path).Scan(
		&result.IsMature,
		&result.Confidence,
		&result.AutoFlagged,
		&result.NeedsReview,
		&scannedAt,
		&reviewedBy,
		&reviewedAt,
		&reviewDecision,
	)

	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("scan result not found: %s", path)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to query scan result: %w", err)
	}

	// Format TIMESTAMP fields as RFC3339 strings
	result.ScannedAt = scannedAt.Format(time.RFC3339)

	// Set nullable fields
	if reviewedBy.Valid {
		result.ReviewedBy = reviewedBy.String
	}
	if reviewedAt.Valid {
		result.ReviewedAt = reviewedAt.Time.Format(time.RFC3339)
	}
	if reviewDecision.Valid {
		result.ReviewDecision = reviewDecision.String
	}

	// Get reasons
	rows, err := r.db.QueryContext(ctx,
		"SELECT reason FROM scan_reasons WHERE path = ?", path)
	if err != nil {
		return nil, fmt.Errorf("failed to query reasons: %w", err)
	}
	defer func() {
		if err := rows.Close(); err != nil {
			r.log.Warn("Failed to close rows: %v", err)
		}
	}()

	result.Reasons = []string{}
	for rows.Next() {
		var reason string
		if err := rows.Scan(&reason); err != nil {
			return nil, fmt.Errorf("failed to scan reason: %w", err)
		}
		result.Reasons = append(result.Reasons, reason)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to iterate reason rows: %w", err)
	}

	return result, nil
}

// GetPendingReview retrieves all scan results that need review
func (r *ScanResultRepository) GetPendingReview(ctx context.Context) ([]*repositories.ScanResult, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT path, is_mature, confidence, auto_flagged, needs_review, scanned_at
		FROM scan_results
		WHERE needs_review = TRUE
		ORDER BY scanned_at DESC
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to query pending reviews: %w", err)
	}
	defer func() {
		if err := rows.Close(); err != nil {
			r.log.Warn("Failed to close rows: %v", err)
		}
	}()

	var results []*repositories.ScanResult
	for rows.Next() {
		result := &repositories.ScanResult{}
		var scannedAt time.Time
		err := rows.Scan(
			&result.Path,
			&result.IsMature,
			&result.Confidence,
			&result.AutoFlagged,
			&result.NeedsReview,
			&scannedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan row: %w", err)
		}
		result.ScannedAt = scannedAt.Format(time.RFC3339)

		// Get reasons for this result
		reasonRows, err := r.db.QueryContext(ctx,
			"SELECT reason FROM scan_reasons WHERE path = ?", result.Path)
		if err != nil {
			return nil, fmt.Errorf("failed to query reasons: %w", err)
		}

		reasons, err := r.scanReasons(reasonRows)
		if err != nil {
			return nil, err
		}
		result.Reasons = reasons

		results = append(results, result)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to iterate scan result rows: %w", err)
	}

	return results, nil
}

// MarkReviewed updates a scan result with review information
func (r *ScanResultRepository) MarkReviewed(ctx context.Context, path, reviewedBy, decision string) error {
	reviewedAt := time.Now() // Pass time.Time directly for MySQL

	result, err := r.db.ExecContext(ctx, `
		UPDATE scan_results
		SET needs_review = FALSE,
		    reviewed_by = ?,
		    reviewed_at = ?,
		    review_decision = ?
		WHERE path = ?
	`, reviewedBy, reviewedAt, decision, path)

	if err != nil {
		return fmt.Errorf("failed to mark as reviewed: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rowsAffected == 0 {
		return fmt.Errorf("scan result not found: %s", path)
	}

	r.log.Info("Marked scan result as reviewed: %s (decision: %s)", path, decision)
	return nil
}

// scanReasons reads all reason strings from the provided rows, closing them properly.
func (r *ScanResultRepository) scanReasons(rows *sql.Rows) ([]string, error) {
	defer func() {
		if err := rows.Close(); err != nil {
			r.log.Warn("Failed to close reason rows: %v", err)
		}
	}()

	var reasons []string
	for rows.Next() {
		var reason string
		if err := rows.Scan(&reason); err != nil {
			return nil, fmt.Errorf("failed to scan reason: %w", err)
		}
		reasons = append(reasons, reason)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to iterate reason rows: %w", err)
	}
	return reasons, nil
}
