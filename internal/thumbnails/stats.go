package thumbnails

import "os"

// GetStats returns current statistics
func (m *Module) GetStats() Stats {
	m.statsMu.RLock()
	defer m.statsMu.RUnlock()
	return m.stats
}

// scanExistingThumbnails walks the thumbnail directory on startup and
// initialises Generated and TotalSize so the stats reflect what is
// actually on disk, not just what was created in the current session.
func (m *Module) scanExistingThumbnails() {
	entries, err := os.ReadDir(m.thumbnailDir)
	if err != nil {
		m.log.Warn("Could not scan thumbnail directory for stats: %v", err)
		return
	}

	var count int64
	var totalSize int64
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		info, err := entry.Info()
		if err != nil || info.Size() == 0 {
			continue
		}
		count++
		totalSize += info.Size()
	}

	m.statsMu.Lock()
	m.stats.Generated = count
	m.stats.TotalSize = totalSize
	m.statsMu.Unlock()

	m.log.Info("Scanned existing thumbnails: %d files, %.1f MB", count, float64(totalSize)/(1024*1024))
}
