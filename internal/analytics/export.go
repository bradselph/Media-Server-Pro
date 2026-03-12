package analytics

import (
	"context"
	"encoding/csv"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"media-server-pro/internal/repositories"
)

// ExportCSV exports analytics data to CSV.
// TODO: Bug — the analytics directory (m.config.Get().Directories.Analytics) may not exist;
// os.Create will fail if it hasn't been created. The config paths module creates directories
// but only at startup. Also, like admin.ExportAuditLog, exported files accumulate indefinitely.
func (m *Module) ExportCSV(ctx context.Context, startDate, endDate time.Time) (string, error) {
	events, err := m.eventRepo.List(ctx, repositories.AnalyticsFilter{
		StartDate: startDate.Format(time.RFC3339),
		EndDate:   endDate.Format(time.RFC3339),
	})
	if err != nil {
		return "", fmt.Errorf("failed to fetch events: %w", err)
	}

	filename := filepath.Join(m.config.Get().Directories.Analytics, fmt.Sprintf("export_%s.csv", time.Now().Format("20060102_150405")))
	file, err := os.Create(filename)
	if err != nil {
		return "", fmt.Errorf("failed to create export file: %w", err)
	}
	defer func() {
		if err := file.Close(); err != nil {
			m.log.Warn("Failed to close CSV export file: %v", err)
		}
	}()

	writer := csv.NewWriter(file)
	defer writer.Flush()

	rows := [][]string{{"Timestamp", "Type", "MediaID", "UserID", "SessionID", "IPAddress"}}
	for _, event := range events {
		rows = append(rows, []string{
			event.Timestamp.Format(time.RFC3339),
			event.Type,
			event.MediaID,
			event.UserID,
			event.SessionID,
			event.IPAddress,
		})
	}
	if err := writer.WriteAll(rows); err != nil {
		return "", err
	}

	m.log.Info("Exported analytics to %s", filename)
	return filename, nil
}
