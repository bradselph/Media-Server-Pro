package thumbnails

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// thumbnailFilePattern matches thumbnail filenames:
//   - {uuid}.jpg            — main thumbnail
//   - {uuid}.webp           — main WebP variant
//   - {uuid}-sm.webp        — responsive 160w
//   - {uuid}-md.webp        — responsive 320w
//   - {uuid}-lg.webp        — responsive 640w
//   - {uuid}_preview_N.jpg  — preview thumbnail at index N
var thumbnailFilePattern = regexp.MustCompile(
	`^([0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12})` +
		`(?:_preview_(\d+))?` +
		`(?:-(sm|md|lg))?` +
		`\.(jpg|webp)$`)

// Cleanup scans the thumbnail directory and removes:
//   - Orphans: thumbnails for media IDs that no longer exist
//   - Excess: preview thumbnails beyond the configured PreviewCount
//   - Corrupt: 0-byte files that block regeneration
//
// Returns a summary of what was removed. Requires SetMediaIDProvider to have been called.
func (m *Module) Cleanup() (*CleanupResult, error) {
	if m.mediaIDProvider == nil {
		return nil, fmt.Errorf("no media ID provider configured")
	}

	validIDs := m.mediaIDProvider.GetAllMediaIDs()
	cfg := m.config.Get()
	previewCount := cfg.Thumbnails.PreviewCount
	if previewCount < 1 {
		previewCount = 10
	}

	entries, err := os.ReadDir(m.thumbnailDir)
	if err != nil {
		return nil, fmt.Errorf("reading thumbnail directory: %w", err)
	}

	result := &CleanupResult{}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()

		// Skip placeholder files
		if strings.HasPrefix(name, "placeholder") || strings.HasPrefix(name, "audio_placeholder") || strings.HasPrefix(name, "censored_placeholder") {
			continue
		}

		matches := thumbnailFilePattern.FindStringSubmatch(name)
		if matches == nil {
			// Not a thumbnail file we manage — leave it alone
			continue
		}

		mediaID := matches[1]
		previewIndexStr := matches[2] // "" for non-preview files

		fullPath := filepath.Join(m.thumbnailDir, name)

		// Check for corrupt (0-byte) files
		info, err := entry.Info()
		if err != nil {
			continue
		}
		if info.Size() == 0 {
			if err := os.Remove(fullPath); err == nil {
				result.CorruptRemoved++
				m.log.Debug("Removed corrupt 0-byte thumbnail: %s", name)
			}
			continue
		}

		// Check for orphans (media no longer exists)
		if !validIDs[mediaID] {
			if err := os.Remove(fullPath); err == nil {
				result.OrphansRemoved++
				result.BytesFreed += info.Size()
				m.log.Debug("Removed orphan thumbnail: %s", name)
			}
			continue
		}

		// Check for excess previews (index >= configured PreviewCount)
		if previewIndexStr != "" {
			idx, err := strconv.Atoi(previewIndexStr)
			if err == nil && idx >= previewCount {
				if err := os.Remove(fullPath); err == nil {
					result.ExcessRemoved++
					result.BytesFreed += info.Size()
					m.log.Debug("Removed excess preview thumbnail: %s (index %d >= configured %d)", name, idx, previewCount)
				}
			}
		}
	}

	// Update cumulative stats
	m.statsMu.Lock()
	m.stats.OrphansRemoved += int64(result.OrphansRemoved)
	m.stats.ExcessRemoved += int64(result.ExcessRemoved)
	m.stats.CorruptRemoved += int64(result.CorruptRemoved)
	m.stats.LastCleanup = time.Now()
	m.statsMu.Unlock()

	return result, nil
}

func formatBytes(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "KMG"[exp])
}
