package handlers

import (
	"context"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"

	"media-server-pro/internal/receiver"
	"media-server-pro/pkg/models"
)

// receiverCopyMaxBytes bounds how many bytes a single "copy to local library"
// operation will pull from a peer when the catalog does not declare a size, so a
// broken or malicious slave cannot stream unbounded data onto local disk.
const receiverCopyMaxBytes = 100 << 30 // 100 GiB

// AdminReceiverCopyMedia copies a federated (peer/slave) media item into the
// local library, turning a proxied reference into a real local file that
// survives the peer disconnecting. It pulls the bytes from the slave (WebSocket
// push, HTTP fallback), writes them under the configured media directory, and
// registers the file exactly like an upload — so afterwards it behaves as
// ordinary local media (editable metadata, HLS, hover previews, etc.).
//
// POST /api/admin/receiver/media/:id/copy
func (h *Handler) AdminReceiverCopyMedia(c *gin.Context) {
	id, ok := RequireParamID(c, "id")
	if !ok {
		return
	}
	if h.receiver == nil {
		writeError(c, http.StatusServiceUnavailable, "peer connections are not enabled")
		return
	}

	item := h.receiver.GetMediaItem(id)
	if item == nil {
		writeError(c, http.StatusNotFound, "federated media not found")
		return
	}

	// If a local copy already exists (same content fingerprint), don't download
	// it again — the unified listing already hides the federated duplicate.
	if item.ContentFingerprint != "" && h.media.HasFingerprint(item.ContentFingerprint) {
		writeError(c, http.StatusConflict, "this item already exists in the local library")
		return
	}

	// One copy of a given content at a time — a double-click or a concurrent
	// bulk job must not download the same bytes twice in parallel (the
	// fingerprint guard above can't see a copy that hasn't finished
	// registering yet).
	guardKey := receiverCopyGuardKey(id, item)
	if !h.beginReceiverCopy(guardKey) {
		writeError(c, http.StatusConflict, "this item is already being copied")
		return
	}
	defer h.endReceiverCopy(guardKey)

	destDir, isAudio, errMsg := h.receiverCopyDestDir(item)
	if errMsg != "" {
		writeError(c, http.StatusInternalServerError, errMsg)
		return
	}
	if err := os.MkdirAll(destDir, 0o750); err != nil {
		h.log.Error("copy federated media: mkdir %s: %v", destDir, err)
		writeError(c, http.StatusInternalServerError, errInternalServer)
		return
	}

	destPath, err := h.streamReceiverItemToDir(c.Request.Context(), id, item, destDir)
	if err != nil {
		h.log.Error("copy federated media %s: %v", id, err)
		writeError(c, http.StatusBadGateway, "failed to copy media from peer")
		return
	}

	// Register the copied file like an upload: mints/reuses a stable ID, runs
	// ffprobe, updates the in-memory index, and upserts the media_metadata row.
	if err := h.media.RegisterUploadedFile(destPath); err != nil {
		// Registration failed. RegisterUploadedFile inserts into the in-memory
		// index BEFORE persisting the metadata row, so a failed DB save leaves a
		// ghost entry pointing at a file we're about to delete. Roll the index
		// back too, then remove the orphaned file so a later scan doesn't pick up
		// an unindexed copy.
		_ = h.media.RemoveMedia(destPath)
		_ = os.Remove(destPath)
		h.log.Error("copy federated media %s: register %s: %v", id, destPath, err)
		writeError(c, http.StatusInternalServerError, "copied the file but failed to index it")
		return
	}

	// No explicit fingerprint step needed: RegisterUploadedFile fingerprints
	// the freshly-written bytes synchronously (createMediaItem), and both
	// servers compute fingerprints the same way — so the copy is recognized
	// as the peer item's content (already_local, re-copy 409) immediately.

	newItem, _ := h.media.GetMedia(destPath)
	h.applyReceiverCopyMetadata(destPath, item)
	if newItem != nil && h.thumbnails != nil {
		h.thumbnails.QueueThumbnailIfMissing(destPath, newItem.ID, isAudio)
		// Re-read so the response reflects any tag/mature updates just applied.
		if refreshed, gErr := h.media.GetMedia(destPath); gErr == nil && refreshed != nil {
			newItem = refreshed
		}
	}

	h.trackServerEvent(c, "receiver_media_copy", map[string]any{
		"source_id": id,
		"slave_id":  item.SlaveID,
		"path":      destPath,
	})

	writeSuccess(c, gin.H{
		"message": "Copied to local library",
		"item":    newItem,
	})
}

// receiverCopyDestDir picks the destination directory for a copied federated
// item based on its media type, returning the directory, whether it is audio,
// and an error message (empty on success). Video → Directories.Videos, audio →
// Directories.Music, everything else → Directories.Uploads (the catch-all
// whitelisted root), so the copy always lands somewhere the media module manages.
func (h *Handler) receiverCopyDestDir(item *receiver.MediaItem) (dir string, isAudio bool, errMsg string) {
	dirs := h.config.Get().Directories
	switch models.MediaType(item.MediaType) {
	case models.MediaTypeAudio:
		if dirs.Music == "" {
			return "", true, "music directory is not configured"
		}
		return dirs.Music, true, ""
	case models.MediaTypeVideo:
		if dirs.Videos == "" {
			return "", false, "video directory is not configured"
		}
		return dirs.Videos, false, ""
	default:
		if dirs.Uploads == "" {
			return "", false, "uploads directory is not configured"
		}
		return dirs.Uploads, false, ""
	}
}

// streamReceiverItemToDir pulls the federated item's bytes from the peer into a
// temp file under destDir, then atomically moves it to a unique final name so a
// concurrent library scan never sees a partially-written media file. Returns the
// final path. ctx cancellation (admin disconnect on the single-copy path, job
// cancel on the bulk path) aborts the transfer.
func (h *Handler) streamReceiverItemToDir(ctx context.Context, id string, item *receiver.MediaItem, destDir string) (string, error) {
	reader, _, err := h.receiver.FetchMedia(ctx, id)
	if err != nil {
		return "", fmt.Errorf("fetch from peer: %w", err)
	}
	defer func() { _ = reader.Close() }()
	// Unblock a blocked read if ctx is canceled mid-copy; Close is idempotent so
	// the deferred Close above stays safe.
	go func() {
		<-ctx.Done()
		_ = reader.Close()
	}()

	tmp, err := os.CreateTemp(destDir, "receiver-copy-*.part")
	if err != nil {
		return "", fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := tmp.Name()
	// Clean up the temp file on any failure path; on success it is renamed away
	// and this Remove becomes a harmless no-op.
	success := false
	defer func() {
		_ = tmp.Close()
		if !success {
			_ = os.Remove(tmpPath)
		}
	}()

	limit := item.Size
	if limit <= 0 {
		limit = receiverCopyMaxBytes
	}
	written, err := io.Copy(tmp, io.LimitReader(reader, limit))
	if err != nil {
		return "", fmt.Errorf("copy stream: %w", err)
	}
	if item.Size > 0 && written < item.Size {
		return "", fmt.Errorf("short read from peer: got %d of %d bytes", written, item.Size)
	}
	if syncErr := tmp.Sync(); syncErr != nil {
		return "", fmt.Errorf("flush temp file: %w", syncErr)
	}
	if closeErr := tmp.Close(); closeErr != nil {
		return "", fmt.Errorf("close temp file: %w", closeErr)
	}

	base, ext := receiverCopyName(item)
	finalPath, err := moveToUniqueName(tmpPath, destDir, base, ext)
	if err != nil {
		return "", fmt.Errorf("finalize file: %w", err)
	}
	success = true
	return finalPath, nil
}

// applyReceiverCopyMetadata carries the peer's tags and mature flag onto the
// freshly-registered local item. Category is intentionally not carried over —
// per-item categories are retired in favor of curated MediaCategory membership.
func (h *Handler) applyReceiverCopyMetadata(destPath string, item *receiver.MediaItem) {
	updates := make(map[string]any)
	if len(item.Tags) > 0 {
		updates["tags"] = item.Tags
	}
	if item.IsMature {
		updates["is_mature"] = true
	}
	if len(updates) == 0 {
		return
	}
	if err := h.media.UpdateMetadata(destPath, updates); err != nil {
		h.log.Warn("copy federated media: carry-over metadata for %s: %v", destPath, err)
	}
}

// receiverCopyName derives a safe base filename (no extension) and extension for
// a copied federated item, from its display name, remote path, or content type.
func receiverCopyName(item *receiver.MediaItem) (base, ext string) {
	ext = strings.ToLower(filepath.Ext(item.Path))
	if ext == "" || len(ext) > 6 {
		if exts, _ := mime.ExtensionsByType(item.ContentType); len(exts) > 0 {
			ext = exts[0]
		}
	}
	if ext == "" {
		switch models.MediaType(item.MediaType) {
		case models.MediaTypeAudio:
			ext = ".mp3"
		case models.MediaTypeVideo:
			ext = ".mp4"
		default:
			ext = ".bin"
		}
	}

	base = item.Name
	if strings.TrimSpace(base) == "" {
		base = strings.TrimSuffix(filepath.Base(item.Path), filepath.Ext(item.Path))
	}
	base = sanitizeReceiverFilename(base)
	// Avoid "Movie.mp4.mp4" when the display name already ends in the extension.
	if strings.EqualFold(filepath.Ext(base), ext) {
		base = strings.TrimSuffix(base, filepath.Ext(base))
	}
	if base == "" {
		base = "media"
	}
	return base, ext
}

// sanitizeReceiverFilename strips path separators, control characters, and
// filesystem-reserved characters from a peer-supplied name so it is safe to use
// as a local filename, and bounds its length on a rune boundary.
func sanitizeReceiverFilename(name string) string {
	name = strings.Map(func(r rune) rune {
		switch {
		case r == '/' || r == '\\' || r == 0:
			return '_'
		case r < 0x20:
			return '_'
		case strings.ContainsRune(`<>:"|?*`, r):
			return '_'
		default:
			return r
		}
	}, name)
	name = strings.Trim(name, " .")
	if runes := []rune(name); len(runes) > 150 {
		name = strings.TrimRight(string(runes[:150]), " .")
	}
	return name
}

// moveToUniqueName renames tmpPath into destDir under base+ext, appending
// _1, _2, … on collision. It checks for existence before renaming (os.Rename
// onto an existing file fails on Windows) and retries on a lost race.
func moveToUniqueName(tmpPath, destDir, base, ext string) (string, error) {
	for i := range 1000 {
		name := base + ext
		if i > 0 {
			name = fmt.Sprintf("%s_%d%s", base, i, ext)
		}
		final := filepath.Join(destDir, name)
		if _, err := os.Stat(final); err == nil {
			continue // already exists — try the next suffix
		}
		if err := os.Rename(tmpPath, final); err != nil {
			// Lost the race (created between Stat and Rename) or Windows
			// rename-onto-existing — try the next suffix if it now exists.
			if _, statErr := os.Stat(final); statErr == nil {
				continue
			}
			return "", err
		}
		return final, nil
	}
	return "", fmt.Errorf("could not find a free filename in %s", destDir)
}
