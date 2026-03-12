// Package media - management.go handles media file operations (rename, delete, modify).
package media

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"media-server-pro/internal/config"
	"media-server-pro/internal/logger"
)

var (
	// Dangerous filename patterns
	dangerousPatterns = regexp.MustCompile(`[<>:"|?*\x00-\x1f]`)
)

// RenameMedia renames a media file. Validates oldPath is within allowed directories.
func (m *Module) RenameMedia(oldPath, newName string) (string, error) {
	if err := m.validatePath(oldPath); err != nil {
		return "", err
	}
	// Validate old path exists (no lock needed for stat)
	if _, err := os.Stat(oldPath); err != nil {
		return "", fmt.Errorf("source file not found: %w", err)
	}

	// Sanitize new name
	newName, err := sanitizeFilename(newName)
	if err != nil {
		return "", err
	}

	// Construct new path in same directory
	dir := filepath.Dir(oldPath)
	newPath := filepath.Join(dir, newName)

	// Check if destination already exists
	if _, err := os.Stat(newPath); err == nil {
		return "", fmt.Errorf("destination file already exists: %s", newName)
	}

	// Perform rename
	if err := os.Rename(oldPath, newPath); err != nil {
		return "", fmt.Errorf("failed to rename: %w", err)
	}

	// TODO: Bug - the mediaByID secondary index is not updated during rename.
	// The old entry in mediaByID still points to the same *models.MediaItem
	// (whose Path is now updated), so ID lookups still work, but the key
	// association relies on pointer identity. This is fragile — if the item
	// is ever replaced (e.g. during a scan), the stale mediaByID entry breaks.
	// Also, the fingerprintIndex is not updated — the old path remains in the
	// fingerprint map. On next scan, this could cause the renamed file to be
	// misidentified as a "moved" file from itself.
	// Update in-memory indexes (media + metadata share mu)
	m.mu.Lock()
	if item, exists := m.media[oldPath]; exists {
		item.Path = newPath
		item.Name = newName
		delete(m.media, oldPath)
		m.media[newPath] = item
	}
	if meta, exists := m.metadata[oldPath]; exists {
		delete(m.metadata, oldPath)
		m.metadata[newPath] = meta
	}
	m.mu.Unlock()

	m.log.Info("Renamed media: %s -> %s", oldPath, newPath)

	// Save only the renamed item (not all 261) to avoid long blocking writes
	if err := m.saveMetadataItem(newPath); err != nil {
		m.log.Error("Failed to save metadata after rename: %v", err)
	}

	return newPath, nil
}

// MoveMedia moves a media file to a new directory
// TODO: Bug - same fingerprintIndex issue as RenameMedia: the old path remains
// in the fingerprint map after the move. Also, MoveMedia does not validate that
// oldPath is within allowed directories (only validates newDir). A file outside
// allowed dirs could be moved into them.
func (m *Module) MoveMedia(oldPath, newDir string) (string, error) {
	// Validate old path exists (no lock needed for stat)
	if _, err := os.Stat(oldPath); err != nil {
		return "", fmt.Errorf("source file not found: %w", err)
	}

	// Validate new directory
	newDir, err := validateDirectory(newDir, m.config.Get())
	if err != nil {
		return "", err
	}

	// Ensure destination directory exists
	if err := os.MkdirAll(newDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create directory: %w", err)
	}

	// Construct new path
	filename := filepath.Base(oldPath)
	newPath := filepath.Join(newDir, filename)

	// Check if destination already exists
	if _, err := os.Stat(newPath); err == nil {
		return "", fmt.Errorf("destination file already exists: %s", newPath)
	}

	// Perform move
	if err := os.Rename(oldPath, newPath); err != nil {
		return "", fmt.Errorf("failed to move: %w", err)
	}

	// Update in-memory indexes (media + metadata share mu)
	m.mu.Lock()
	if item, exists := m.media[oldPath]; exists {
		item.Path = newPath
		delete(m.media, oldPath)
		m.media[newPath] = item
		item.Category = m.detectCategory(newPath)
	}
	if meta, exists := m.metadata[oldPath]; exists {
		delete(m.metadata, oldPath)
		m.metadata[newPath] = meta
	}
	m.mu.Unlock()

	m.log.Info("Moved media: %s -> %s", oldPath, newPath)

	if err := m.saveMetadataItem(newPath); err != nil {
		m.log.Error("Failed to save metadata after move: %v", err)
	}

	return newPath, nil
}

// DeleteMedia removes a media file from the filesystem
// TODO: Bug - fingerprintIndex is not cleaned up when a file is deleted. The
// stale fingerprint entry will cause createMediaItem on next scan to incorrectly
// detect the deleted path as the "old path" of a moved file if a new file with
// the same fingerprint appears. Delete the fingerprint from m.fingerprintIndex
// using the metadata's ContentFingerprint value.
func (m *Module) DeleteMedia(ctx context.Context, path string) error {
	// Validate path exists and is within allowed directories (no lock needed)
	if _, err := os.Stat(path); err != nil {
		return fmt.Errorf("file not found: %w", err)
	}
	if err := m.validatePath(path); err != nil {
		return err
	}

	// Delete the file
	if err := os.Remove(path); err != nil {
		return fmt.Errorf("failed to delete: %w", err)
	}

	// Remove from in-memory indexes (media + metadata share mu)
	m.mu.Lock()
	if item, exists := m.media[path]; exists {
		delete(m.mediaByID, item.ID)
	}
	delete(m.media, path)
	delete(m.metadata, path)
	m.mu.Unlock()

	m.log.Info("Deleted media: %s", path)
	// Item was deleted — remove from DB too (not just the in-memory map)
	if m.metadataRepo != nil {
		ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		if err := m.metadataRepo.Delete(ctx, path); err != nil {
			m.log.Warn("Failed to delete metadata from DB for %s: %v", path, err)
		}
	}

	return nil
}

// RemoveMedia removes a media entry from the index without deleting the file.
// This is used for cleanup when files have already been deleted externally.
// TODO: Bug - same fingerprintIndex issue as DeleteMedia: the stale fingerprint
// entry is not removed. Also, RenameMedia and MoveMedia do not call m.version++
// but RemoveMedia does — the version bump behavior is inconsistent. All mutation
// methods should bump the version for cache invalidation.
func (m *Module) RemoveMedia(path string) error {
	m.mu.Lock()
	delete(m.metadata, path)
	item, exists := m.media[path]
	if !exists {
		m.mu.Unlock()
		return fmt.Errorf("media not found in index: %s", path)
	}
	delete(m.mediaByID, item.ID)
	delete(m.media, path)
	m.version++
	m.mu.Unlock()

	m.log.Debug("Removed media from index: %s", path)

	// Remove from DB (best-effort, async)
	if m.metadataRepo != nil {
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := m.metadataRepo.Delete(ctx, path); err != nil {
				m.log.Warn("Failed to delete metadata from DB for %s: %v", path, err)
			}
		}()
	}

	return nil
}

// UpdateMetadata updates metadata for a media file.
func (m *Module) UpdateMetadata(path string, updates map[string]interface{}) error {
	m.mu.Lock()

	meta, exists := m.metadata[path]
	if !exists {
		meta = &Metadata{
			PlaybackPos: make(map[string]float64),
			CustomMeta:  make(map[string]string),
		}
		m.metadata[path] = meta
	}

	applyMetadataUpdates(meta, updates)
	m.syncMediaItem(path, updates)
	m.mu.Unlock()

	m.log.Debug("Updated metadata for: %s", path)

	go func() {
		if err := m.saveMetadataItem(path); err != nil {
			m.log.Warn("Failed to save metadata after update: %v", err)
		}
	}()

	return nil
}

// applyMetadataUpdates applies a set of key-value updates to a Metadata struct.
func applyMetadataUpdates(meta *Metadata, updates map[string]interface{}) {
	for key, value := range updates {
		applyMetadataField(meta, key, value)
	}
}

// applyMetadataField applies a single metadata field update.
func applyMetadataField(meta *Metadata, key string, value interface{}) {
	switch key {
	case "tags":
		if tags, ok := value.([]string); ok {
			meta.Tags = tags
		}
	case "is_mature":
		if isMature, ok := value.(bool); ok {
			meta.IsMature = isMature
		}
	case "mature_score":
		if score, ok := value.(float64); ok {
			meta.MatureScore = score
		}
	case "category":
		if cat, ok := value.(string); ok {
			meta.Category = cat
		}
	case "views":
		if views, ok := value.(float64); ok {
			meta.Views = int(views)
		} else if views, ok := value.(int); ok {
			meta.Views = views
		}
	default:
		if strVal, ok := value.(string); ok {
			if meta.CustomMeta == nil {
				meta.CustomMeta = make(map[string]string)
			}
			meta.CustomMeta[key] = strVal
		}
	}
}

// syncMediaItem synchronizes relevant metadata updates to the in-memory media item.
// Must be called with m.mu held.
func (m *Module) syncMediaItem(path string, updates map[string]interface{}) {
	item, exists := m.media[path]
	if !exists {
		return
	}
	if tags, ok := updates["tags"].([]string); ok {
		item.Tags = tags
	}
	if isMature, ok := updates["is_mature"].(bool); ok {
		item.IsMature = isMature
	}
	if category, ok := updates["category"].(string); ok {
		item.Category = category
	}
	if views, ok := updates["views"].(float64); ok {
		item.Views = int(views)
	} else if views, ok := updates["views"].(int); ok {
		item.Views = views
	}
}

// SetTags sets tags for a media file
func (m *Module) SetTags(path string, tags []string) error {
	return m.UpdateMetadata(path, map[string]interface{}{"tags": tags})
}

// UpdateTags merges new tags with existing tags for a media file (deduplicated, case-insensitive).
// TODO: Race condition - reads current tags under RLock, then releases the lock,
// then calls SetTags (which calls UpdateMetadata, which acquires a write Lock).
// Between the RLock release and the write Lock acquisition, another goroutine could
// modify the tags (e.g. AddTag or RemoveTag), and those changes would be lost because
// SetTags replaces the entire tag list with the snapshot taken before the lock release.
func (m *Module) UpdateTags(path string, tags []string) error {
	var current []string
	m.mu.RLock()
	if meta, ok := m.metadata[path]; ok && meta != nil {
		current = make([]string, len(meta.Tags))
		copy(current, meta.Tags)
	}
	m.mu.RUnlock()
	seen := make(map[string]bool)
	for _, t := range current {
		seen[strings.ToLower(t)] = true
	}
	for _, t := range tags {
		if t == "" {
			continue
		}
		key := strings.ToLower(t)
		if !seen[key] {
			seen[key] = true
			current = append(current, t)
		}
	}
	return m.SetTags(path, current)
}

// AddTag adds a tag to a media file
func (m *Module) AddTag(path, tag string) error {
	m.mu.Lock()

	meta, exists := m.metadata[path]
	if !exists {
		meta = &Metadata{
			PlaybackPos: make(map[string]float64),
			CustomMeta:  make(map[string]string),
			Tags:        []string{},
		}
		m.metadata[path] = meta
	}

	// Check if tag already exists
	for _, t := range meta.Tags {
		if t == tag {
			m.mu.Unlock()
			return nil // Already has tag
		}
	}

	meta.Tags = append(meta.Tags, tag)
	if item, exists := m.media[path]; exists {
		tagsCopy := make([]string, len(meta.Tags))
		copy(tagsCopy, meta.Tags)
		item.Tags = tagsCopy
	}
	m.mu.Unlock()

	go func() {
		if err := m.saveMetadataItem(path); err != nil {
			m.log.Warn("Failed to save metadata after adding tag: %v", err)
		}
	}()

	return nil
}

// RemoveTag removes a tag from a media file
func (m *Module) RemoveTag(path, tag string) error {
	m.mu.Lock()

	meta, exists := m.metadata[path]
	if !exists {
		m.mu.Unlock()
		return nil
	}

	var newTags []string
	for _, t := range meta.Tags {
		if t != tag {
			newTags = append(newTags, t)
		}
	}
	meta.Tags = newTags

	if item, exists := m.media[path]; exists {
		tagsCopy := make([]string, len(newTags))
		copy(tagsCopy, newTags)
		item.Tags = tagsCopy
	}
	m.mu.Unlock()

	go func() {
		if err := m.saveMetadataItem(path); err != nil {
			m.log.Warn("Failed to save metadata after removing tag: %v", err)
		}
	}()

	return nil
}

// validatePath ensures the path is within allowed directories
func (m *Module) validatePath(path string) error {
	cfg := m.config.Get()
	absPath, err := filepath.Abs(path)
	if err != nil {
		return err
	}

	allowedDirs := []string{
		cfg.Directories.Videos,
		cfg.Directories.Music,
		cfg.Directories.Uploads,
	}

	for _, dir := range allowedDirs {
		if dir == "" {
			continue
		}
		absDir, err := filepath.Abs(dir)
		if err != nil {
			continue
		}
		// Append separator to prevent prefix bypass (/allowed_dir_extra matching /allowed_dir)
		if strings.HasPrefix(absPath, absDir+string(os.PathSeparator)) || absPath == absDir {
			return nil
		}
	}

	return fmt.Errorf("path not in allowed directory: %s", path)
}

// sanitizeFilename cleans and validates a filename
func sanitizeFilename(name string) (string, error) {
	// Get base name only (no directory components)
	name = filepath.Base(name)

	// Check for empty or dot names
	if name == "" || name == "." || name == ".." {
		return "", fmt.Errorf("invalid filename")
	}

	// Check for hidden files
	if strings.HasPrefix(name, ".") {
		return "", fmt.Errorf("hidden files not allowed")
	}

	// Check for path traversal
	if strings.Contains(name, "..") || strings.Contains(name, "/") || strings.Contains(name, "\\") {
		return "", fmt.Errorf("path traversal detected")
	}

	// Check for dangerous characters
	if dangerousPatterns.MatchString(name) {
		return "", fmt.Errorf("filename contains invalid characters")
	}

	// Limit filename length
	if len(name) > 255 {
		ext := filepath.Ext(name)
		if len(ext) >= 255 {
			return "", fmt.Errorf("extension too long")
		}
		base := name[:255-len(ext)]
		name = base + ext
	}

	return name, nil
}

// validateDirectory ensures the directory is allowed
func validateDirectory(dir string, cfg *config.Config) (string, error) {
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}

	// Check for path traversal
	if strings.Contains(dir, "..") {
		return "", fmt.Errorf("path traversal detected")
	}

	allowedDirs := []string{
		cfg.Directories.Videos,
		cfg.Directories.Music,
		cfg.Directories.Uploads,
	}

	for _, allowed := range allowedDirs {
		absAllowed, err := filepath.Abs(allowed)
		if err != nil {
			continue
		}
		// Append separator to prevent prefix bypass (/allowed_dir_extra matching /allowed_dir)
		if strings.HasPrefix(absDir, absAllowed+string(os.PathSeparator)) || absDir == absAllowed {
			return absDir, nil
		}
	}

	return "", fmt.Errorf("directory not allowed: %s", dir)
}

// GetMediaLog returns a logger for media operations
// TODO: Redundant code - this getter exposes the internal logger, which breaks
// encapsulation. External callers should use their own logger. This method is
// never called anywhere in the codebase (confirmed via grep). Remove it.
func (m *Module) GetMediaLog() *logger.Logger {
	return m.log
}
