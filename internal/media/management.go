// Package media - management.go handles media file operations (rename, delete, modify).
package media

import (
	"context"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"media-server-pro/internal/config"
	"media-server-pro/pkg/helpers"
	"media-server-pro/pkg/models"
	"media-server-pro/pkg/storage"
)

var (
	// Dangerous filename patterns
	dangerousPatterns = regexp.MustCompile(`[<>:"|?*\x00-\x1f]`)
)

const (
	errSourceNotFound    = "source file not found: %w"
	errDestExists        = "destination file already exists: %s"
	fmtCreateMediaFailed = "failed to create media item for %s"
)

// storeResult holds the backend and relative key path for a media file.
type storeResult struct {
	store   storage.Backend
	relPath string // path relative to the backend root/prefix
}

// keyPrefixer is implemented by remote backends that expose their S3 key prefix.
type keyPrefixer interface {
	KeyPrefix() string
}

// storeFor resolves which backend owns the given path and returns the backend
// plus the relative path within that backend.
//
// For local backends the path must be an absolute filesystem path under the
// configured directory. For remote backends (S3/B2) the path is the S3 key
// stored in the media index (e.g. "videos/foo.mp4"); storeFor matches it
// against the backend's key prefix so that management operations correctly
// route to the right store.
//
// If no store matches, storeResult.store is nil and callers fall back to
// direct os.* operations.
func (m *Module) storeFor(p string) storeResult {
	cfg := m.config.Get()

	type entry struct {
		dir   string
		store storage.Backend
	}

	candidates := []entry{
		{cfg.Directories.Videos, m.videoStore},
		{cfg.Directories.Music, m.musicStore},
		{cfg.Directories.Uploads, m.uploadStore},
	}

	for _, c := range candidates {
		if c.store == nil || c.dir == "" {
			continue
		}

		if !c.store.IsLocal() {
			// Remote backend (S3/B2): match against the backend's key prefix.
			// The path stored in the media index is already an S3 key like
			// "videos/foo.mp4", so check whether it begins with the prefix.
			if kp, ok := c.store.(keyPrefixer); ok {
				prefix := kp.KeyPrefix()
				if prefix != "" && strings.HasPrefix(p, prefix) {
					return storeResult{
						store:   c.store,
						relPath: strings.TrimPrefix(p, prefix),
					}
				}
			}
			continue
		}

		// Local backend: match against the absolute filesystem directory.
		absDir, err := filepath.Abs(c.dir)
		if err != nil {
			continue
		}
		sep := string(filepath.Separator)
		if !strings.HasPrefix(p, absDir+sep) && p != absDir {
			continue
		}
		rel, err := filepath.Rel(absDir, p)
		if err != nil {
			continue
		}
		return storeResult{store: c.store, relPath: filepath.ToSlash(rel)}
	}

	return storeResult{relPath: p}
}

// ResolveForFFmpeg converts a stored media path to a form that ffmpeg can read.
// For absolute local paths it returns the path unchanged.
// For remote S3 paths it returns a short-lived presigned GET URL.
// Implements the MediaInputResolver interface consumed by the hls and thumbnails modules.
func (m *Module) ResolveForFFmpeg(ctx context.Context, mediaPath string) (string, error) {
	if filepath.IsAbs(mediaPath) {
		return mediaPath, nil // absolute local path — ffmpeg reads directly
	}
	sr := m.storeFor(mediaPath)
	if sr.store == nil || sr.store.IsLocal() {
		return mediaPath, nil // local or unrecognized — pass through
	}
	presigner, ok := sr.store.(storage.PresignURLer)
	if !ok {
		return mediaPath, nil // remote but no presign support
	}
	// 12-hour TTL ensures presigned URLs survive long HLS transcodes of 4K content.
	return presigner.PresignGetURL(ctx, sr.relPath, 12*time.Hour)
}

// crossStoreMove copies a file from srcStore/srcRel to dstStore/dstRel, then
// deletes the source. Used when moving a file between backends that share the
// same bucket but use different key prefixes (e.g. videos/ → music/).
func crossStoreMove(ctx context.Context, srcStore storage.Backend, srcRel string, dstStore storage.Backend, dstRel string) error {
	r, err := srcStore.Open(ctx, srcRel)
	if err != nil {
		return fmt.Errorf("open source: %w", err)
	}
	defer func() { _ = r.Close() }()

	if _, err := dstStore.Create(ctx, dstRel, r); err != nil {
		return fmt.Errorf("write destination: %w", err)
	}

	return srcStore.Remove(ctx, srcRel)
}

// RenameMedia renames a media file. Validates oldPath is within allowed directories.
func (m *Module) RenameMedia(oldPath, newName string) (string, error) {
	if err := m.validatePath(oldPath); err != nil {
		return "", err
	}

	// Sanitize new name
	newName, err := sanitizeFilename(newName)
	if err != nil {
		return "", err
	}

	ctx := context.Background()
	sr := m.storeFor(oldPath)

	var newPath string

	if sr.store != nil && !sr.store.IsLocal() {
		// Remote backend (S3/B2): use key-based rename.
		newRelPath := path.Join(path.Dir(sr.relPath), newName)

		if _, err := sr.store.Stat(ctx, sr.relPath); err != nil {
			return "", fmt.Errorf(errSourceNotFound, err)
		}
		if _, err := sr.store.Stat(ctx, newRelPath); err == nil {
			return "", fmt.Errorf(errDestExists, newName)
		}
		if err := sr.store.Rename(ctx, sr.relPath, newRelPath); err != nil {
			return "", fmt.Errorf("failed to rename: %w", err)
		}
		// Use AbsPath so the index key is the canonical S3 key (forward-slash,
		// correct prefix) rather than a filepath.Join result which uses OS separators.
		newPath = sr.store.AbsPath(newRelPath)
	} else {
		// Local filesystem (original behavior).
		if _, err := os.Stat(oldPath); err != nil {
			return "", fmt.Errorf(errSourceNotFound, err)
		}
		newPath = filepath.Join(filepath.Dir(oldPath), newName)
		if _, err := os.Stat(newPath); err == nil {
			return "", fmt.Errorf(errDestExists, newName)
		}
		if err := os.Rename(oldPath, newPath); err != nil {
			return "", fmt.Errorf("failed to rename: %w", err)
		}
	}

	// Update in-memory indexes (media + metadata share mu). mediaByID key is by item.ID;
	// the same *models.MediaItem is kept so ID lookups still work. fingerprintIndex is
	// updated so the fingerprint maps to the new path.
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
		if meta.ContentFingerprint != "" {
			m.fingerprintIndex[meta.ContentFingerprint] = newPath
		}
	}
	m.mu.Unlock()

	m.log.Info("Renamed media: %s -> %s", oldPath, newPath)

	// Save only the renamed item (not all 261) to avoid long blocking writes
	if err := m.saveMetadataItem(newPath); err != nil {
		m.log.Error("Failed to save metadata after rename: %v", err)
		return newPath, fmt.Errorf("file renamed but metadata save failed: %w", err)
	}

	return newPath, nil
}

// MoveMedia moves a media file to a new directory.
// fingerprintIndex is updated so the fingerprint maps to the new path.
func (m *Module) MoveMedia(oldPath, newDir string) (string, error) {
	if err := m.validatePath(oldPath); err != nil {
		return "", err
	}

	// Validate new directory (must be within allowed dirs).
	newDir, err := validateDirectory(newDir, m.config.Get())
	if err != nil {
		return "", err
	}

	// For local backends, filename comes from the filesystem path.
	// For remote backends, oldPath is an S3 key so path.Base is correct.
	filename := path.Base(oldPath)
	newPath := filepath.Join(newDir, filename) // used for local; overridden for remote below

	ctx := context.Background()
	srcSR := m.storeFor(oldPath)
	dstSR := m.storeFor(newPath) // newPath is local dir + filename → matches local store

	if srcSR.store != nil && !srcSR.store.IsLocal() {
		// Remote backend: check source exists.
		if _, err := srcSR.store.Stat(ctx, srcSR.relPath); err != nil {
			return "", fmt.Errorf(errSourceNotFound, err)
		}

		// dstSR must be non-nil; the destination directory was validated against
		// known local dirs which map 1-to-1 with a configured backend.
		if dstSR.store == nil {
			return "", fmt.Errorf("destination directory has no configured storage backend")
		}
		if _, err := dstSR.store.Stat(ctx, dstSR.relPath); err == nil {
			return "", fmt.Errorf(errDestExists, newPath)
		}

		if srcSR.store == dstSR.store {
			// Same backend (same prefix): use Rename (server-side copy + delete).
			if err := srcSR.store.Rename(ctx, srcSR.relPath, dstSR.relPath); err != nil {
				return "", fmt.Errorf("failed to move: %w", err)
			}
		} else {
			// Different backends / prefixes: stream-copy then delete source.
			if err := crossStoreMove(ctx, srcSR.store, srcSR.relPath, dstSR.store, dstSR.relPath); err != nil {
				return "", fmt.Errorf("failed to move across stores: %w", err)
			}
		}

		// Override newPath with the canonical S3 key so the index stays consistent.
		newPath = dstSR.store.AbsPath(dstSR.relPath)
	} else {
		// Local filesystem (original behavior).
		if _, err := os.Stat(oldPath); err != nil {
			return "", fmt.Errorf(errSourceNotFound, err)
		}
		if err := os.MkdirAll(newDir, 0o755); err != nil { //nolint:gosec // G301: media directories need world-read for serving
			return "", fmt.Errorf("failed to create directory: %w", err)
		}
		if _, err := os.Stat(newPath); err == nil {
			return "", fmt.Errorf(errDestExists, newPath)
		}
		if err := os.Rename(oldPath, newPath); err != nil {
			return "", fmt.Errorf("failed to move: %w", err)
		}
	}

	// Update in-memory indexes (media + metadata share mu); keep fingerprintIndex in sync
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
		if meta.ContentFingerprint != "" {
			m.fingerprintIndex[meta.ContentFingerprint] = newPath
		}
	}
	m.mu.Unlock()

	m.log.Info("Moved media: %s -> %s", oldPath, newPath)

	if err := m.saveMetadataItem(newPath); err != nil {
		m.log.Error("Failed to save metadata after move: %v", err)
		return newPath, fmt.Errorf("file moved but metadata save failed: %w", err)
	}

	return newPath, nil
}

// DeleteMedia removes a media file from the filesystem and from in-memory indexes
// (including fingerprintIndex so the next scan does not treat a new file with the
// same fingerprint as a move from the deleted path).
func (m *Module) DeleteMedia(ctx context.Context, filePath string) error {
	if err := m.validatePath(filePath); err != nil {
		return err
	}

	sr := m.storeFor(filePath)

	if sr.store != nil && !sr.store.IsLocal() {
		// Remote backend (S3/B2).
		if _, err := sr.store.Stat(ctx, sr.relPath); err != nil {
			return fmt.Errorf("file not found: %w", err)
		}
		if err := sr.store.Remove(ctx, sr.relPath); err != nil {
			return fmt.Errorf("failed to delete: %w", err)
		}
	} else {
		// Local filesystem (original behavior).
		if _, err := os.Stat(filePath); err != nil {
			return fmt.Errorf("file not found: %w", err)
		}
		if err := os.Remove(filePath); err != nil {
			return fmt.Errorf("failed to delete: %w", err)
		}
	}

	// Remove from in-memory indexes (media + metadata + fingerprintIndex)
	m.mu.Lock()
	if meta, exists := m.metadata[filePath]; exists && meta.ContentFingerprint != "" {
		delete(m.fingerprintIndex, meta.ContentFingerprint)
	}
	if item, exists := m.media[filePath]; exists {
		delete(m.mediaByID, item.ID)
	}
	delete(m.media, filePath)
	delete(m.metadata, filePath)
	m.version++
	m.mu.Unlock()

	m.log.Info("Deleted media: %s", filePath)
	// Item was deleted — remove from DB too (not just the in-memory map)
	if m.metadataRepo != nil {
		dbCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		if err := m.metadataRepo.Delete(dbCtx, filePath); err != nil {
			m.log.Warn("Failed to delete metadata from DB for %s: %v", filePath, err)
		}
	}

	return nil
}

// RemoveMedia removes a media entry from the index without deleting the file.
// This is used for cleanup when files have already been deleted externally.
// fingerprintIndex is updated so the fingerprint is no longer associated with this path.
func (m *Module) RemoveMedia(mediaPath string) error {
	m.mu.Lock()
	meta := m.metadata[mediaPath]
	if meta != nil && meta.ContentFingerprint != "" {
		delete(m.fingerprintIndex, meta.ContentFingerprint)
	}
	delete(m.metadata, mediaPath)
	item, exists := m.media[mediaPath]
	if !exists {
		m.mu.Unlock()
		return fmt.Errorf("media not found in index: %s", mediaPath)
	}
	delete(m.mediaByID, item.ID)
	delete(m.media, mediaPath)
	m.version++
	m.mu.Unlock()

	m.log.Debug("Removed media from index: %s", mediaPath)

	// Remove from DB (best-effort, async)
	if m.metadataRepo != nil {
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := m.metadataRepo.Delete(ctx, mediaPath); err != nil {
				m.log.Warn("Failed to delete metadata from DB for %s: %v", mediaPath, err)
			}
		}()
	}

	return nil
}

// UpdateMetadata updates metadata for a media file.
func (m *Module) UpdateMetadata(mediaPath string, updates map[string]any) error {
	m.mu.Lock()

	meta, exists := m.metadata[mediaPath]
	if !exists {
		meta = &Metadata{
			PlaybackPos: make(map[string]float64),
			CustomMeta:  make(map[string]string),
		}
		m.metadata[mediaPath] = meta
	}

	applyMetadataUpdates(meta, updates)
	m.syncMediaItem(mediaPath, updates)
	m.mu.Unlock()

	m.log.Debug("Updated metadata for: %s", mediaPath)

	if err := m.saveMetadataItem(mediaPath); err != nil {
		m.log.Error("Failed to save metadata after update: %v", err)
		return fmt.Errorf("metadata updated in memory but DB save failed: %w", err)
	}

	return nil
}

// applyMetadataUpdates applies a set of key-value updates to a Metadata struct.
func applyMetadataUpdates(meta *Metadata, updates map[string]any) {
	for key, value := range updates {
		applyMetadataField(meta, key, value)
	}
}

// applyMetadataField applies a single metadata field update.
func applyMetadataField(meta *Metadata, key string, value any) {
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
		switch views := value.(type) {
		case float64:
			meta.Views = int(views)
		case int:
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
func (m *Module) syncMediaItem(mediaPath string, updates map[string]any) {
	item, exists := m.media[mediaPath]
	if !exists {
		return
	}
	if tags, ok := updates["tags"].([]string); ok {
		item.Tags = tags
	}
	if isMature, ok := updates["is_mature"].(bool); ok {
		item.IsMature = isMature
	}
	if score, ok := updates["mature_score"].(float64); ok {
		item.MatureScore = score
	}
	if category, ok := updates["category"].(string); ok {
		item.Category = category
	}
	switch views := updates["views"].(type) {
	case float64:
		item.Views = int(views)
	case int:
		item.Views = views
	}
}

// SetTags sets tags for a media file
func (m *Module) SetTags(mediaPath string, tags []string) error {
	return m.UpdateMetadata(mediaPath, map[string]any{"tags": tags})
}

// UpdateTags merges new tags with existing tags for a media file (deduplicated, case-insensitive).
// The merge and write happen atomically under a single write lock to prevent lost updates.
func (m *Module) UpdateTags(mediaPath string, tags []string) error {
	m.mu.Lock()

	meta, exists := m.metadata[mediaPath]
	if !exists {
		meta = &Metadata{
			PlaybackPos: make(map[string]float64),
			CustomMeta:  make(map[string]string),
			Tags:        []string{},
		}
		m.metadata[mediaPath] = meta
	}

	seen := make(map[string]bool)
	for _, t := range meta.Tags {
		seen[strings.ToLower(t)] = true
	}
	merged := append([]string(nil), meta.Tags...)
	for _, t := range tags {
		if t == "" {
			continue
		}
		key := strings.ToLower(t)
		if !seen[key] {
			seen[key] = true
			merged = append(merged, t)
		}
	}

	meta.Tags = merged
	if item, ok := m.media[mediaPath]; ok {
		tagsCopy := make([]string, len(merged))
		copy(tagsCopy, merged)
		item.Tags = tagsCopy
	}
	m.mu.Unlock()

	m.log.Debug("Updated tags for: %s", mediaPath)

	if err := m.saveMetadataItem(mediaPath); err != nil {
		m.log.Error("Failed to save metadata after tag update: %v", err)
		return fmt.Errorf("tags updated in memory but DB save failed: %w", err)
	}

	return nil
}

// AddTag adds a tag to a media file
func (m *Module) AddTag(mediaPath, tag string) error {
	m.mu.Lock()

	meta, exists := m.metadata[mediaPath]
	if !exists {
		meta = &Metadata{
			PlaybackPos: make(map[string]float64),
			CustomMeta:  make(map[string]string),
			Tags:        []string{},
		}
		m.metadata[mediaPath] = meta
	}

	// Check if tag already exists
	if slices.Contains(meta.Tags, tag) {
		m.mu.Unlock()
		return nil
	}

	meta.Tags = append(meta.Tags, tag)
	if item, exists := m.media[mediaPath]; exists {
		tagsCopy := make([]string, len(meta.Tags))
		copy(tagsCopy, meta.Tags)
		item.Tags = tagsCopy
	}
	m.mu.Unlock()

	if err := m.saveMetadataItem(mediaPath); err != nil {
		m.log.Error("Failed to save metadata after adding tag: %v", err)
		return fmt.Errorf("tag added in memory but DB save failed: %w", err)
	}

	return nil
}

// RemoveTag removes a tag from a media file
func (m *Module) RemoveTag(mediaPath, tag string) error {
	m.mu.Lock()

	meta, exists := m.metadata[mediaPath]
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

	if item, exists := m.media[mediaPath]; exists {
		tagsCopy := make([]string, len(newTags))
		copy(tagsCopy, newTags)
		item.Tags = tagsCopy
	}
	m.mu.Unlock()

	if err := m.saveMetadataItem(mediaPath); err != nil {
		m.log.Error("Failed to save metadata after removing tag: %v", err)
		return fmt.Errorf("tag removed in memory but DB save failed: %w", err)
	}

	return nil
}

// validatePath ensures the path is managed by one of the configured stores
// (local directories or remote backend key prefixes).
func (m *Module) validatePath(filePath string) error {
	// storeFor checks both local directory prefixes and remote key prefixes.
	// If it returns a non-nil store OR a relPath that was successfully resolved,
	// we know the path is within a managed partition.
	sr := m.storeFor(filePath)
	if sr.store != nil {
		// Matched a configured store (local or remote).
		return nil
	}

	// Fall back to the filesystem-only check for the case where stores
	// haven't been set yet (e.g., during early startup).
	cfg := m.config.Get()
	absPath, err := filepath.Abs(filePath)
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

	return fmt.Errorf("path not in allowed directory: %s", filePath)
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

// RegisterUploadedFile indexes a single newly-uploaded file so it appears in
// the media library immediately without waiting for the next scheduled scan.
// It creates a MediaItem, adds it to the in-memory index, runs ffprobe for
// metadata extraction, and persists the metadata to the database.
// RegisterUploadedFile indexes a newly-uploaded local file by path.
// For remote-store uploads where os.Stat would fail, use RegisterUploadedFileWithSize instead.
func (m *Module) RegisterUploadedFile(mediaPath string) error {
	info, err := os.Stat(mediaPath)
	if err != nil {
		return fmt.Errorf("stat uploaded file: %w", err)
	}

	ext := strings.ToLower(filepath.Ext(mediaPath))
	var mediaType models.MediaType
	switch {
	case videoExtensions[ext]:
		mediaType = models.MediaTypeVideo
	case helpers.IsAudioExtension(ext):
		mediaType = models.MediaTypeAudio
	default:
		mediaType = models.MediaTypeUnknown
	}

	item := m.createMediaItem(mediaPath, info, mediaType)
	if item == nil {
		return fmt.Errorf(fmtCreateMediaFailed, mediaPath)
	}
	return m.finalizeRegisteredItem(mediaPath, item)
}

// RegisterUploadedFileWithSize indexes a newly-uploaded file using caller-supplied size metadata,
// skipping os.Stat. Use this for remote-store (S3/B2) uploads where the path is a storage key.
func (m *Module) RegisterUploadedFileWithSize(mediaPath string, size int64, modTime time.Time) error {
	ext := strings.ToLower(filepath.Ext(mediaPath))
	var mediaType models.MediaType
	switch {
	case videoExtensions[ext]:
		mediaType = models.MediaTypeVideo
	case helpers.IsAudioExtension(ext):
		mediaType = models.MediaTypeAudio
	default:
		mediaType = models.MediaTypeUnknown
	}

	// Build item using the storage-oriented helper which accepts struct fields
	// directly — no os.FileInfo required.
	item := m.createMediaItemFromStorageInfo(mediaPath, storage.FileInfo{
		Name:    filepath.Base(mediaPath),
		Size:    size,
		ModTime: modTime,
	}, mediaType)
	if item == nil {
		return fmt.Errorf(fmtCreateMediaFailed, mediaPath)
	}
	return m.finalizeRegisteredItem(mediaPath, item)
}

func (m *Module) finalizeRegisteredItem(mediaPath string, item *models.MediaItem) error {
	if item == nil {
		return fmt.Errorf(fmtCreateMediaFailed, mediaPath)
	}

	// Extract metadata (duration, codec, etc.) synchronously so the item is
	// fully populated before it becomes visible to API consumers.
	if m.ffprobeAvail {
		m.extractMetadata(item)
	}

	m.mu.Lock()
	m.media[mediaPath] = item
	m.mediaByID[item.ID] = item
	m.version++
	m.mu.Unlock()

	// Persist to DB.
	if err := m.saveMetadataItem(mediaPath); err != nil {
		m.log.Error("Failed to persist metadata for uploaded file %s: %v", mediaPath, err)
		return fmt.Errorf("file indexed but metadata save failed: %w", err)
	}

	m.log.Info("Registered uploaded file: %s (id: %s)", mediaPath, item.ID)
	return nil
}
