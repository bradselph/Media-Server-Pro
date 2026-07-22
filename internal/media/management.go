// Package media - management.go handles media file operations (rename, delete, modify).
package media

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"media-server-pro/internal/config"
	"media-server-pro/internal/repositories"
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
		_ = dstStore.Remove(ctx, dstRel)
		return fmt.Errorf("write destination: %w", err)
	}

	if err := srcStore.Remove(ctx, srcRel); err != nil {
		// Roll back the destination copy so it does not become an orphaned
		// object that leaks remote storage and is never indexed.
		_ = dstStore.Remove(ctx, dstRel)
		return fmt.Errorf("remove source after copy: %w", err)
	}
	return nil
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
	m.reindexPath(oldPath, newPath, newName)

	m.log.Info("Renamed media: %s -> %s", oldPath, newPath)

	// Upsert the new path row FIRST so a failure here leaves the old row intact —
	// otherwise a delete-then-failed-insert loses every user-assigned tag,
	// is_mature flag and custom field (newPath would be re-scanned as a fresh
	// item on restart). Only once the new row is safely persisted do we delete
	// the old one; a delete failure then leaves a benign ghost row that the
	// in-memory m.media guard filters out of listings until the next scan prunes it.
	m.persistPathChange(oldPath, newPath)

	return newPath, nil
}

// ReindexMovedFile updates the in-memory media/metadata/fingerprint indexes and
// DB rows to reflect a file that has ALREADY been moved on disk from oldPath to
// newPath by an external actor (e.g. the autodiscovery "apply suggestion" flow,
// which performs its own os.Rename). It does no filesystem I/O and is a no-op
// when oldPath isn't indexed. Mirrors the index-update half of RenameMedia.
func (m *Module) ReindexMovedFile(oldPath, newPath string) {
	if oldPath == "" || newPath == "" || oldPath == newPath {
		return
	}
	newName := filepath.Base(newPath)

	m.reindexPath(oldPath, newPath, newName)

	m.log.Info("Reindexed moved media: %s -> %s", oldPath, newPath)

	// Upsert the new path row FIRST (see RenameMedia): a failed insert after the
	// old row was already deleted would lose all metadata for the moved file.
	m.persistPathChange(oldPath, newPath)
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

	// Update in-memory indexes (media + metadata share mu); keep fingerprintIndex in sync.
	// A move no longer re-derives the category from the path — categorisation is
	// driven solely by admin-curated MediaCategory membership, which is keyed by
	// the stable media ID and is unaffected by a path change.
	m.reindexPath(oldPath, newPath, "")

	m.log.Info("Moved media: %s -> %s", oldPath, newPath)

	// Upsert the new path row FIRST (see RenameMedia): a failed insert after the
	// old row was already deleted would lose all metadata for the moved file.
	m.persistPathChange(oldPath, newPath)

	return newPath, nil
}

// reindexPath re-keys the in-memory media/metadata/fingerprint indexes from
// oldPath to newPath under m.mu, renaming the item when newName is non-empty,
// and bumps the catalog version. A no-op when oldPath isn't indexed.
func (m *Module) reindexPath(oldPath, newPath, newName string) {
	m.mu.Lock()
	if item, exists := m.media[oldPath]; exists {
		item.Path = newPath
		if newName != "" {
			item.Name = newName
		}
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
	m.version++ // catalog mutated — poll-based consumers must see a bump
	m.mu.Unlock()
}

// persistPathChange upserts the newPath metadata row FIRST (so a failure leaves
// the old row intact rather than losing all metadata) and only then deletes the
// old row. A failed delete leaves a benign ghost row that the in-memory guard
// filters out until the next scan prunes it.
func (m *Module) persistPathChange(oldPath, newPath string) {
	if err := m.saveMetadataItem(newPath); err != nil {
		// The new-path upsert failed: do NOT delete the old row. Keeping it
		// preserves the item's stable ID, tags, is_mature flag and view count —
		// fingerprint-based move detection re-keys them onto newPath on the next
		// scan. Deleting here (the previous behavior, contradicting this
		// function's own contract) would strand the moved file with zero DB rows,
		// so a restart before the next full save loses all its metadata and a
		// mature item silently reverts to non-mature.
		m.log.Error("Failed to save metadata after path change of %s to %s; keeping old row as fallback: %v", oldPath, newPath, err)
		return
	}
	if m.metadataRepo != nil {
		dbCtx, dbCancel := context.WithTimeout(context.Background(), 8*time.Second)
		if err := m.metadataRepo.Delete(dbCtx, oldPath); err != nil {
			m.log.Warn("Failed to delete old metadata row after path change of %s: %v", oldPath, err)
		}
		dbCancel()
	}
}

// deleteMediaFile removes the backing object idempotently. Treating an already
// absent object as success lets callers safely retry an interrupted delete.
func deleteMediaFile(ctx context.Context, sr storeResult, filePath string) error {
	if sr.store != nil && !sr.store.IsLocal() {
		if err := sr.store.Remove(ctx, sr.relPath); err != nil && !errors.Is(err, storage.ErrNotFound) {
			return fmt.Errorf("failed to delete: %w", err)
		}
		return nil
	}
	if err := os.Remove(filePath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to delete: %w", err)
	}
	return nil
}

// removeCachedMediaIfUnchanged publishes a completed delete only when neither
// the item nor metadata pointer was replaced while storage/DB work ran. This
// prevents an upload that reuses the same path from being removed by an older
// delete operation. Caller must hold saveMu.
func (m *Module) removeCachedMediaIfUnchanged(mediaPath string, expectedItem *models.MediaItem, expectedMeta *Metadata) bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.media[mediaPath] != expectedItem || m.metadata[mediaPath] != expectedMeta {
		return false
	}
	if expectedItem == nil && expectedMeta == nil {
		return false
	}
	if expectedMeta != nil && expectedMeta.ContentFingerprint != "" && m.fingerprintIndex[expectedMeta.ContentFingerprint] == mediaPath {
		delete(m.fingerprintIndex, expectedMeta.ContentFingerprint)
	}
	if expectedItem != nil && m.mediaByID[expectedItem.ID] == expectedItem {
		delete(m.mediaByID, expectedItem.ID)
	}
	delete(m.media, mediaPath)
	delete(m.metadata, mediaPath)
	m.version++
	return true
}

// DeleteMedia removes a media file from the filesystem and from in-memory indexes
// (including fingerprintIndex so the next scan does not treat a new file with the
// same fingerprint as a move from the deleted path).
func (m *Module) DeleteMedia(ctx context.Context, filePath string) error {
	if err := m.validatePath(filePath); err != nil {
		return err
	}
	m.saveMu.Lock()
	defer m.saveMu.Unlock()

	sr := m.storeFor(filePath)
	m.mu.RLock()
	savedMeta := m.metadata[filePath]
	savedMetaSnapshot := cloneMetadata(savedMeta)
	savedItem := m.media[filePath]
	if savedMetaSnapshot == nil && savedItem != nil {
		savedMetaSnapshot = metadataFromItem(savedItem)
	}
	m.mu.RUnlock()

	// Delete persistent metadata before the irreversible storage operation. A DB
	// failure therefore leaves the file and both caches untouched and immediately
	// retryable through the same media ID.
	if m.metadataRepo != nil {
		dbCtx, dbCancel := context.WithTimeout(ctx, 5*time.Second)
		err := m.metadataRepo.Delete(dbCtx, filePath)
		dbCancel()
		if err != nil && !errors.Is(err, repositories.ErrMetadataNotFound) {
			return fmt.Errorf("failed to delete metadata for %s: %w", filePath, err)
		}
	}

	if err := deleteMediaFile(ctx, sr, filePath); err != nil {
		// Best-effort compensation keeps a storage failure from leaving a live
		// file/cache with no metadata row. The cache remains authoritative even if
		// compensation fails, so the next bulk metadata save can retry the upsert.
		var restoreErr error
		if m.metadataRepo != nil && savedMetaSnapshot != nil {
			restoreCtx, restoreCancel := context.WithTimeout(context.Background(), 5*time.Second)
			restoreErr = m.metadataRepo.Upsert(restoreCtx, filePath, m.convertInternalToRepo(filePath, savedMetaSnapshot))
			restoreCancel()
			if restoreErr != nil {
				m.log.Warn("Failed to restore metadata after storage delete failure for %s: %v", filePath, restoreErr)
			}
		}
		return errors.Join(err, restoreErr)
	}

	if !m.removeCachedMediaIfUnchanged(filePath, savedItem, savedMeta) {
		m.log.Debug("DeleteMedia: cache entry for %s changed during delete; preserving replacement", filePath)
	}
	m.log.Info("Deleted media: %s", filePath)
	return nil
}

// RemoveMedia removes a media entry from the index without deleting the file.
// This is used for cleanup when files have already been deleted externally.
// fingerprintIndex is updated so the fingerprint is no longer associated with this path.
func (m *Module) RemoveMedia(mediaPath string) error {
	m.saveMu.Lock()
	defer m.saveMu.Unlock()
	m.mu.RLock()
	item := m.media[mediaPath]
	meta := m.metadata[mediaPath]
	m.mu.RUnlock()

	// Remove from DB synchronously. Single-row DELETEs are fast and the
	// goroutine-per-call pattern caused DB connection pool exhaustion during
	// bulk cleanup (10 000 items → 10 000 concurrent goroutines).
	if m.metadataRepo != nil {
		removeCtx, removeCancel := context.WithTimeout(context.Background(), 5*time.Second)
		if err := m.metadataRepo.Delete(removeCtx, mediaPath); err != nil && !errors.Is(err, repositories.ErrMetadataNotFound) {
			removeCancel()
			return fmt.Errorf("failed to remove metadata for %s: %w", mediaPath, err)
		}
		removeCancel()
	}

	// Publish the delete only after persistence succeeds. The pointer guard
	// preserves any item concurrently registered at the same path. When no cache
	// entry exists, the DB delete above still repairs a ghost row and succeeds
	// idempotently.
	if !m.removeCachedMediaIfUnchanged(mediaPath, item, meta) && (item != nil || meta != nil) {
		m.log.Debug("RemoveMedia: cache entry for %s changed during delete; preserving replacement", mediaPath)
	}
	m.log.Debug("Removed media from index: %s", mediaPath)

	return nil
}

// UpdateMetadata updates metadata for a media file.
func (m *Module) UpdateMetadata(mediaPath string, updates map[string]any) error {
	err := m.updateMetadataPersisted(mediaPath, func(meta *Metadata) {
		for key, value := range updates {
			applyMetadataField(meta, key, value)
		}
	})
	if err != nil {
		m.log.Error("Failed to save metadata after update: %v", err)
		return fmt.Errorf("failed to update metadata: %w", err)
	}
	m.log.Debug("Updated metadata for: %s", mediaPath)
	return nil
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
	case "mature_reason":
		// Written by the post-upload mature scanner; routes the reason string
		// to the structured MatureReasons field rather than CustomMeta.
		if s, ok := value.(string); ok && s != "" {
			meta.MatureReasons = append(meta.MatureReasons, s)
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

// SetTags sets tags for a media file
func (m *Module) SetTags(mediaPath string, tags []string) error {
	return m.UpdateMetadata(mediaPath, map[string]any{"tags": tags})
}

// UpdateTags merges new tags with existing tags for a media file (deduplicated, case-insensitive).
// The merge and write happen atomically under a single write lock to prevent lost updates.
func (m *Module) UpdateTags(mediaPath string, tags []string) error {
	err := m.updateMetadataPersisted(mediaPath, func(meta *Metadata) {
		seen := make(map[string]bool)
		for _, tag := range meta.Tags {
			seen[strings.ToLower(tag)] = true
		}
		merged := append([]string(nil), meta.Tags...)
		for _, tag := range tags {
			if tag == "" {
				continue
			}
			key := strings.ToLower(tag)
			if !seen[key] {
				seen[key] = true
				merged = append(merged, tag)
			}
		}
		meta.Tags = merged
	})
	if err != nil {
		m.log.Error("Failed to save metadata after tag update: %v", err)
		return fmt.Errorf("failed to update tags: %w", err)
	}
	m.log.Debug("Updated tags for: %s", mediaPath)
	return nil
}

// AddTag adds a tag to a media file
func (m *Module) AddTag(mediaPath, tag string) error {
	err := m.updateMetadataPersisted(mediaPath, func(meta *Metadata) {
		if !slices.Contains(meta.Tags, tag) {
			meta.Tags = append(meta.Tags, tag)
		}
	})
	if err != nil {
		m.log.Error("Failed to save metadata after adding tag: %v", err)
		return fmt.Errorf("failed to add tag: %w", err)
	}
	return nil
}

// RemoveTag removes a tag from a media file
func (m *Module) RemoveTag(mediaPath, tag string) error {
	err := m.updateMetadataPersisted(mediaPath, func(meta *Metadata) {
		newTags := make([]string, 0, len(meta.Tags))
		for _, existing := range meta.Tags {
			if existing != tag {
				newTags = append(newTags, existing)
			}
		}
		meta.Tags = newTags
	})
	if err != nil {
		m.log.Error("Failed to save metadata after removing tag: %v", err)
		return fmt.Errorf("failed to remove tag: %w", err)
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

	item := m.createMediaItem(mediaPath, info, mediaType, m.config.Get().Directories.Thumbnails)
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
