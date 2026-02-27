// Package media - management.go handles media file operations (rename, delete, modify).
package media

import (
	"context"
	"crypto/md5"
	"encoding/hex"
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

// RenameMedia renames a media file
func (m *Module) RenameMedia(oldPath, newName string) (string, error) {
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

	// Update in-memory indexes under mu
	m.mu.Lock()
	if item, exists := m.media[oldPath]; exists {
		oldID := item.ID
		item.Path = newPath
		item.Name = newName
		hash := md5.Sum([]byte(newPath))
		item.ID = hex.EncodeToString(hash[:])
		delete(m.media, oldPath)
		m.media[newPath] = item
		// Update ID index
		delete(m.mediaByID, oldID)
		m.mediaByID[item.ID] = item
	}
	m.mu.Unlock()

	// Update metadata key under metaMu
	m.metaMu.Lock()
	if meta, exists := m.metadata[oldPath]; exists {
		delete(m.metadata, oldPath)
		m.metadata[newPath] = meta
	}
	m.metaMu.Unlock()

	m.log.Info("Renamed media: %s -> %s", oldPath, newPath)

	// Save only the renamed item (not all 261) to avoid long blocking writes
	if err := m.saveMetadataItem(newPath); err != nil {
		m.log.Error("Failed to save metadata after rename: %v", err)
	}

	return newPath, nil
}

// MoveMedia moves a media file to a new directory
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

	// Update in-memory indexes under mu
	m.mu.Lock()
	if item, exists := m.media[oldPath]; exists {
		oldID := item.ID
		item.Path = newPath
		hash := md5.Sum([]byte(newPath))
		item.ID = hex.EncodeToString(hash[:])
		delete(m.media, oldPath)
		m.media[newPath] = item
		// Update ID index
		delete(m.mediaByID, oldID)
		m.mediaByID[item.ID] = item

		// Re-detect category based on new location
		item.Category = m.detectCategory(newPath)
	}
	m.mu.Unlock()

	// Update metadata key under metaMu
	m.metaMu.Lock()
	if meta, exists := m.metadata[oldPath]; exists {
		delete(m.metadata, oldPath)
		m.metadata[newPath] = meta
	}
	m.metaMu.Unlock()

	m.log.Info("Moved media: %s -> %s", oldPath, newPath)

	if err := m.saveMetadataItem(newPath); err != nil {
		m.log.Error("Failed to save metadata after move: %v", err)
	}

	return newPath, nil
}

// DeleteMedia removes a media file from the filesystem
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

	// Remove from in-memory indexes under mu
	m.mu.Lock()
	if item, exists := m.media[path]; exists {
		delete(m.mediaByID, item.ID)
	}
	delete(m.media, path)
	m.mu.Unlock()

	// Remove from metadata under metaMu
	m.metaMu.Lock()
	delete(m.metadata, path)
	m.metaMu.Unlock()

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

// RemoveMedia removes a media entry from the index without deleting the file
// This is used for cleanup when files have already been deleted externally
// Lock ordering: metaMu first, then mu (consistent with UpdateMetadata, AddTag, etc.)
func (m *Module) RemoveMedia(path string) error {
	// Remove from metadata first (acquire metaMu before mu to maintain lock ordering)
	m.metaMu.Lock()
	delete(m.metadata, path)
	m.metaMu.Unlock()

	// Remove from media cache and ID index
	m.mu.Lock()
	item, exists := m.media[path]
	if !exists {
		m.mu.Unlock()
		return fmt.Errorf("media not found in index: %s", path)
	}
	delete(m.mediaByID, item.ID)
	delete(m.media, path)

	// Increment version to signal change
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
// Lock ordering: metaMu is released before acquiring mu to prevent deadlock.
func (m *Module) UpdateMetadata(path string, updates map[string]interface{}) error {
	m.metaMu.Lock()

	meta, exists := m.metadata[path]
	if !exists {
		meta = &Metadata{
			PlaybackPos: make(map[string]float64),
			CustomMeta:  make(map[string]string),
		}
		m.metadata[path] = meta
	}

	applyMetadataUpdates(meta, updates)
	m.metaMu.Unlock()

	// Sync to media item under mu only (metaMu already released to avoid deadlock)
	m.mu.Lock()
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

// AddTag adds a tag to a media file
// Lock ordering: metaMu first, then mu (consistent with UpdateMetadata)
func (m *Module) AddTag(path, tag string) error {
	// Acquire metaMu first for consistent lock ordering
	m.metaMu.Lock()

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
			m.metaMu.Unlock()
			return nil // Already has tag
		}
	}

	meta.Tags = append(meta.Tags, tag)
	newTags := make([]string, len(meta.Tags))
	copy(newTags, meta.Tags)
	m.metaMu.Unlock()

	// Sync tags to the in-memory MediaItem (acquire mu after releasing metaMu)
	m.mu.Lock()
	if item, exists := m.media[path]; exists {
		item.Tags = newTags
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
// Lock ordering: metaMu first, then mu (consistent with UpdateMetadata)
func (m *Module) RemoveTag(path, tag string) error {
	// Acquire metaMu first for consistent lock ordering
	m.metaMu.Lock()

	meta, exists := m.metadata[path]
	if !exists {
		m.metaMu.Unlock()
		return nil
	}

	var newTags []string
	for _, t := range meta.Tags {
		if t != tag {
			newTags = append(newTags, t)
		}
	}
	meta.Tags = newTags

	tagsCopy := make([]string, len(meta.Tags))
	copy(tagsCopy, meta.Tags)
	m.metaMu.Unlock()

	// Sync tags to the in-memory MediaItem (acquire mu after releasing metaMu)
	m.mu.Lock()
	if item, exists := m.media[path]; exists {
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
func (m *Module) GetMediaLog() *logger.Logger {
	return m.log
}
