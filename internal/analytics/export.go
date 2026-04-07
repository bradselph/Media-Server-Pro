package analytics

import (
	"context"
	"encoding/csv"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"time"

	"media-server-pro/internal/repositories"
)

// ExportCSV exports analytics data to CSV.
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

	succeeded := false
	defer func() {
		if err := file.Close(); err != nil {
			m.log.Warn("Failed to close CSV export file: %v", err)
		}
		if !succeeded {
			if removeErr := os.Remove(filename); removeErr != nil && !os.IsNotExist(removeErr) {
				m.log.Warn("Failed to remove partial export file %s: %v", filename, removeErr)
			}
		}
	}()

	writer := csv.NewWriter(file)

	rows := [][]string{{"Timestamp", "Type", "MediaID", "UserID", "SessionID", "IPMasked"}}
	for _, event := range events {
		rows = append(rows, []string{
			event.Timestamp.Format(time.RFC3339),
			event.Type,
			event.MediaID,
			event.UserID,
			event.SessionID,
			maskIP(event.IPAddress),
		})
	}
	if err := writer.WriteAll(rows); err != nil {
		return "", err
	}

	succeeded = true
	m.log.Info("Exported analytics to %s", filename)
	return filename, nil
}

// maskIP pseudonymizes an IP address for GDPR compliance.
// IPv4: last octet replaced with 0 (e.g. 192.168.1.0).
// IPv6: last 8 bytes zeroed (keeps /64 prefix).
// Non-parseable values are replaced with "masked".
func maskIP(ip string) string {
	parsed := net.ParseIP(ip)
	if parsed == nil {
		return "masked"
	}
	if v4 := parsed.To4(); v4 != nil {
		v4[3] = 0
		return v4.String()
	}
	v6 := parsed.To16()
	for i := 8; i < 16; i++ {
		v6[i] = 0
	}
	return v6.String()
}
