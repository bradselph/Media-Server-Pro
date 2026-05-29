package downloader

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"media-server-pro/internal/logger"
	"media-server-pro/pkg/helpers"
)

// ImportableFile represents a completed download ready for import.
type ImportableFile struct {
	Name     string `json:"name"`
	Size     int64  `json:"size"`
	Modified int64  `json:"modified"`
	IsAudio  bool   `json:"isAudio"`
}

// ImportDestination is a library location a downloaded file can be moved into.
// Key is a stable identifier the import endpoint validates against this same
// enumerated set, so a caller can never write to an arbitrary path.
type ImportDestination struct {
	Key       string `json:"key"`   // "videos" | "music" | "uploads" | "<root>/<subdir>"
	Label     string `json:"label"` // human-friendly, e.g. "Videos / hidrive"
	Path      string `json:"path"`  // absolute destination directory
	IsDefault bool   `json:"isDefault"`
	Writable  bool   `json:"writable"` // false = a read-only mount (e.g. HiDrive --read-only); importing here would fail
}

// probeWritable creates and immediately removes a temp file in dir (which must
// exist) to test whether the filesystem accepts writes. os.CreateTemp gives each
// probe a unique name so concurrent callers don't collide. This is how a
// read-only mount — a HiDrive WebDAV share mounted --read-only — is told apart
// from a writable one: os.ReadDir succeeds on both, but only a writable
// filesystem accepts the probe.
func probeWritable(dir string) bool {
	f, err := os.CreateTemp(dir, ".msp-wtest-*")
	if err != nil {
		return false
	}
	name := f.Name()
	_ = f.Close()
	_ = os.Remove(name)
	return true
}

// isDirWritable reports whether dir accepts writes. A path that does not exist
// yet is treated as writable: ImportFile MkdirAll's the destination, so flagging
// a not-yet-created (but configured) dir as read-only would wrongly block valid
// imports. Used to set the per-destination Writable flag for the picker.
func isDirWritable(dir string) bool {
	info, err := os.Stat(dir)
	if err != nil {
		return true // doesn't exist yet — ImportFile will create it and surface any real error
	}
	if !info.IsDir() {
		return false
	}
	return probeWritable(dir)
}

// destinationWritable reports whether a file can ultimately be written under dir,
// accounting for not-yet-created sub-folders. ImportFile MkdirAll's dir, so the
// real test is whether the nearest EXISTING ancestor accepts writes — e.g. a new
// sub-folder under a read-only HiDrive mount is not writable even though the leaf
// path doesn't exist yet. Used for the import pre-flight check.
func destinationWritable(dir string) bool {
	for {
		info, err := os.Stat(dir)
		if err == nil {
			return info.IsDir() && probeWritable(dir)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return false // walked to the filesystem root without finding an existing dir
		}
		dir = parent
	}
}

// rootLabels maps a root key to its display name.
var rootLabels = map[string]string{
	"videos":  "Videos",
	"music":   "Music",
	"uploads": "Uploads",
}

// ListDestinations enumerates valid import targets: each configured library root
// (videos/music/uploads) plus its immediate sub-directories. Sub-dirs let an
// operator drop a download into an existing folder — including a HiDrive WebDAV
// mount grafted under the videos root (which appears as e.g. "videos/hidrive").
// Roots with an empty configured path are skipped. defaultDir, when it matches a
// listed path, flags that entry as the pre-selected default; if it is set but
// matches nothing, it is prepended as an explicit "default" entry.
func ListDestinations(videosDir, musicDir, uploadsDir, defaultDir string) []ImportDestination {
	log := logger.New("downloader-import")
	roots := []struct{ key, dir string }{
		{"videos", videosDir},
		{"music", musicDir},
		{"uploads", uploadsDir},
	}

	var dests []ImportDestination
	for _, r := range roots {
		if r.dir == "" {
			continue
		}
		dests = append(dests, ImportDestination{Key: r.key, Label: rootLabels[r.key], Path: r.dir, Writable: isDirWritable(r.dir)})

		entries, err := os.ReadDir(r.dir)
		if err != nil {
			// Root not yet created or unreadable (permissions, stalled/offline
			// mount). Offer the root but log so a missing sub-dir — e.g. a HiDrive
			// mount that went offline — is diagnosable instead of silently absent.
			log.Warn("Cannot enumerate sub-directories of %s root %q: %v (offering the root only)", r.key, r.dir, err)
			continue
		}
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			name := e.Name()
			sub := filepath.Join(r.dir, name)
			dests = append(dests, ImportDestination{
				Key:      r.key + "/" + name,
				Label:    rootLabels[r.key] + " / " + name,
				Path:     sub,
				Writable: isDirWritable(sub),
			})
		}
	}

	// Flag the default. defaultDir is an absolute path (Downloader.ImportDir or
	// Directories.Uploads); match it against the enumerated paths.
	if defaultDir != "" {
		matched := false
		for i := range dests {
			if sameDir(dests[i].Path, defaultDir) {
				dests[i].IsDefault = true
				matched = true
				break
			}
		}
		if !matched {
			dests = append([]ImportDestination{{
				Key: "default", Label: "Default", Path: defaultDir, IsDefault: true, Writable: isDirWritable(defaultDir),
			}}, dests...)
		}
	}

	return dests
}

// ResolveDestination validates key against the enumerated destination set and
// returns its absolute directory. Because the key must exactly match an entry
// ListDestinations produced, no path-traversal input can reach the filesystem.
func ResolveDestination(key, videosDir, musicDir, uploadsDir, defaultDir string) (string, error) {
	for _, d := range ListDestinations(videosDir, musicDir, uploadsDir, defaultDir) {
		if d.Key == key {
			return d.Path, nil
		}
	}
	return "", fmt.Errorf("%w: %q", ErrUnknownDestination, key)
}

// sanitizeSubfolder validates an optional new sub-folder name for an import. It
// must be a single path segment — no separators, no "."/".."/control bytes — so
// it can never escape the chosen destination root. An empty/whitespace input
// returns ("", nil), meaning "no sub-folder". The cleaned name is returned for
// the caller to join onto the destination directory.
func sanitizeSubfolder(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", nil
	}
	if strings.ContainsAny(name, `/\`) || name != filepath.Base(name) {
		return "", fmt.Errorf("%w: must be a single name, not a path", ErrInvalidSubfolder)
	}
	if name == "." || name == ".." {
		return "", fmt.Errorf("%w: %q", ErrInvalidSubfolder, name)
	}
	for _, r := range name {
		if r < 0x20 {
			return "", fmt.Errorf("%w: contains control characters", ErrInvalidSubfolder)
		}
	}
	return name, nil
}

// sameDir reports whether two directory paths refer to the same location after
// cleaning. Best-effort: falls back to a cleaned-string compare when Abs fails.
func sameDir(a, b string) bool {
	ca, err := filepath.Abs(a)
	if err != nil {
		ca = filepath.Clean(a)
	}
	cb, err := filepath.Abs(b)
	if err != nil {
		cb = filepath.Clean(b)
	}
	return ca == cb
}

// ListImportableFiles scans the downloads directory and returns files that
// are completed and ready for import. Applies the same skip logic as
// dev-tools/download-move.sh.
func ListImportableFiles(downloadsDir string) ([]ImportableFile, error) {
	log := logger.New("downloader-import")

	entries, err := os.ReadDir(downloadsDir)
	if err != nil {
		return nil, fmt.Errorf("read downloads dir: %w", err)
	}

	now := time.Now()
	var files []ImportableFile

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		name := entry.Name()

		// 1. Skip partial downloads (.part = yt-dlp in-progress)
		if strings.HasSuffix(name, ".part") {
			continue
		}

		// 2. Skip temp files (.tmp = HLS concatenation)
		if strings.HasSuffix(name, ".tmp") {
			continue
		}

		// 3. Skip remux intermediates (_remux in name)
		if strings.Contains(name, "_remux") {
			continue
		}

		// 4. Skip non-media extensions
		ext := strings.ToLower(filepath.Ext(name))
		if !helpers.IsMediaExtension(ext) {
			continue
		}

		info, err := entry.Info()
		if err != nil {
			log.Debug("Skip %s: stat error: %v", name, err)
			continue
		}

		// 5. Skip zero-byte files
		if info.Size() == 0 {
			continue
		}

		// 6. Skip files modified within last 30 seconds
		if now.Sub(info.ModTime()) < 30*time.Second {
			continue
		}

		files = append(files, ImportableFile{
			Name:     name,
			Size:     info.Size(),
			Modified: info.ModTime().Unix(),
			IsAudio:  helpers.IsAudioExtension(ext),
		})
	}

	return files, nil
}

// ImportFile moves (or copies) a file from srcDir to destDir.
// Returns the destination path and whether the source was deleted (when deleteSource was true).
// If a file with the same name exists, appends a timestamp to avoid collision.
func ImportFile(srcDir, destDir, filename string, deleteSource bool) (destPath string, sourceDeleted bool, err error) {
	// Sanitize filename to prevent path traversal — strip directory components
	filename = filepath.Base(filename)
	if filename == "." || filename == ".." || filename == string(filepath.Separator) {
		return "", false, fmt.Errorf("invalid filename")
	}

	srcPath := filepath.Join(srcDir, filename)

	// Defense-in-depth: verify resolved path is within srcDir
	absSrc, err := filepath.Abs(srcPath)
	if err != nil {
		return "", false, fmt.Errorf("resolve source path: %w", err)
	}
	absSrcDir, err := filepath.Abs(srcDir)
	if err != nil {
		return "", false, fmt.Errorf("resolve source dir: %w", err)
	}
	if !strings.HasPrefix(absSrc, absSrcDir+string(filepath.Separator)) {
		return "", false, fmt.Errorf("filename escapes source directory")
	}

	// Verify source exists
	if _, err := os.Stat(srcPath); err != nil {
		return "", false, fmt.Errorf("source file not found: %w", err)
	}

	// Ensure destination directory exists
	if err := os.MkdirAll(destDir, 0o755); err != nil { //nolint:gosec // G301: media dest dirs need world-read for serving
		return "", false, fmt.Errorf("create dest dir: %w", err)
	}

	// Atomically claim a unique destination name. The O_CREATE|O_EXCL placeholder
	// closes the TOCTOU window: a plain stat-then-create lets two concurrent
	// imports of the same filename both resolve to the same path, and the second
	// os.Create (O_TRUNC) would silently truncate the first's data. With an
	// exclusive claim, the loser of the race gets EEXIST and falls through to the
	// next numbered variant. We then rename-over or copy-into the placeholder.
	destPath, err = claimDestPath(destDir, filename)
	if err != nil {
		return "", false, fmt.Errorf("claim destination: %w", err)
	}

	if deleteSource {
		// Try rename first (instant on same filesystem) — atomically replaces the
		// placeholder we just claimed.
		if err := os.Rename(srcPath, destPath); err == nil {
			return destPath, true, nil
		}
		// Cross-device (e.g. importing onto a WebDAV/FUSE mount): copy into the
		// claimed placeholder, then delete the source.
		if err := copyFile(srcPath, destPath); err != nil {
			_ = os.Remove(destPath) // drop the placeholder so a failed import leaves nothing behind
			return "", false, fmt.Errorf("copy file: %w", err)
		}
		if removeErr := os.Remove(srcPath); removeErr != nil {
			log := logger.New("downloader-import")
			log.Warn("Import succeeded but could not remove source %s: %v", srcPath, removeErr)
			return destPath, false, nil
		}
		return destPath, true, nil
	}

	// Copy only — into the claimed placeholder.
	if err := copyFile(srcPath, destPath); err != nil {
		_ = os.Remove(destPath)
		return "", false, fmt.Errorf("copy file: %w", err)
	}
	return destPath, false, nil
}

// claimDestPath atomically reserves a destination filename in destDir, returning
// the claimed absolute path. It tries the original name first, then base_1.ext,
// base_2.ext, … using O_CREATE|O_EXCL so each name is claimed by exactly one
// caller even under concurrent imports. The returned path points at a 0-byte
// placeholder owned by the caller, to be rename-over'd or copied-into.
func claimDestPath(destDir, filename string) (string, error) {
	ext := filepath.Ext(filename)
	base := strings.TrimSuffix(filename, ext)
	const maxAttempts = 1000
	for i := range maxAttempts {
		candidate := filepath.Join(destDir, filename)
		if i > 0 {
			candidate = filepath.Join(destDir, fmt.Sprintf("%s_%d%s", base, i, ext))
		}
		f, err := os.OpenFile(candidate, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if err == nil {
			_ = f.Close()
			return candidate, nil
		}
		if !os.IsExist(err) {
			return "", err // EROFS, ENOSPC, permission, etc. — surface the real cause
		}
	}
	return "", fmt.Errorf("no free destination name for %q after %d attempts", filename, maxAttempts)
}

func copyFile(src, dst string) (retErr error) {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer func() {
		if cerr := out.Close(); cerr != nil && retErr == nil {
			retErr = cerr
		}
	}()

	if _, err = io.Copy(out, in); err != nil {
		// Clean up partial copy
		_ = out.Close()
		_ = os.Remove(dst)
		return err
	}
	return nil
}
