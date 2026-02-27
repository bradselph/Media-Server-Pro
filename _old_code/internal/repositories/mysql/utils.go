package mysql

import (
	"database/sql"
	"fmt"
	"time"
)

// parseTimeToMySQL parses an RFC3339 datetime string and returns a time.Time for MySQL
func parseTimeToMySQL(timeStr string) (time.Time, error) {
	if timeStr == "" {
		return time.Now(), nil
	}

	// Try parsing as RFC3339 (with timezone)
	t, err := time.Parse(time.RFC3339, timeStr)
	if err == nil {
		return t, nil
	}

	// Try parsing as MySQL datetime format
	t, err = time.Parse("2006-01-02 15:04:05", timeStr)
	if err == nil {
		return t, nil
	}

	// If all parsing fails, return error
	return time.Time{}, fmt.Errorf("failed to parse datetime: %s", timeStr)
}

// nullString converts a string to sql.NullString
func nullString(s string) sql.NullString {
	if s == "" {
		return sql.NullString{Valid: false}
	}
	return sql.NullString{String: s, Valid: true}
}
