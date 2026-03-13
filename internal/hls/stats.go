package hls

import (
	"os"
	"path/filepath"

	"media-server-pro/pkg/models"
)

// Stats holds HLS module statistics
type Stats struct {
	TotalJobs     int    `json:"total_jobs"`
	RunningJobs   int    `json:"running_jobs"`
	CompletedJobs int    `json:"completed_jobs"`
	FailedJobs    int    `json:"failed_jobs"`
	PendingJobs   int    `json:"pending_jobs"`
	CacheSize     int64  `json:"cache_size_bytes"`
	CacheDir      string `json:"-"`
}

// GetStats returns HLS module statistics. Cache size is computed after releasing
// the job lock so filepath.Walk does not block job mutations.
func (m *Module) GetStats() Stats {
	m.jobsMu.RLock()
	stats := Stats{
		TotalJobs: len(m.jobs),
		CacheDir:  m.cacheDir,
	}
	for _, job := range m.jobs {
		switch job.Status {
		case models.HLSStatusCompleted:
			stats.CompletedJobs++
		case models.HLSStatusRunning:
			stats.RunningJobs++
		case models.HLSStatusFailed:
			stats.FailedJobs++
		case models.HLSStatusPending:
			stats.PendingJobs++
		}
	}
	m.jobsMu.RUnlock()

	stats.CacheSize = m.calculateCacheSize()
	return stats
}

// calculateCacheSize walks the cache directory to sum file sizes. Called without holding jobsMu.
func (m *Module) calculateCacheSize() int64 {
	var size int64

	if err := filepath.Walk(m.cacheDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if !info.IsDir() {
			size += info.Size()
		}
		return nil
	}); err != nil {
		m.log.Warn("Failed to calculate cache size: %v", err)
	}

	return size
}
