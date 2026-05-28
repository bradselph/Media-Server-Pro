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
		dests = append(dests, ImportDestination{Key: r.key, Label: rootLabels[r.key], Path: r.dir})

		entries, err := os.ReadDir(r.dir)
		if err != nil {
			continue // root not yet created or unreadable — just offer the root
		}
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			name := e.Name()
			dests = append(dests, ImportDestination{
				Key:   r.key + "/" + name,
				Label: rootLabels[r.key] + " / " + name,
				Path:  filepath.Join(r.dir, name),
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
				Key: "default", Label: "Default", Path: defaultDir, IsDefault: true,
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
	return "", fmt.Errorf("unknown or unavailable destination %q", key)
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

	// Handle filename collision
	destPath = filepath.Join(destDir, filename)
	if _, err := os.Stat(destPath); err == nil {
		ext := filepath.Ext(filename)
		base := strings.TrimSuffix(filename, ext)
		timestamp := time.Now().Format("20060102_150405")
		destPath = filepath.Join(destDir, base+"_"+timestamp+ext)
	}

	if deleteSource {
		// Try rename first (instant on same filesystem)
		if err := os.Rename(srcPath, destPath); err == nil {
			return destPath, true, nil
		}
		// Fallback: copy + delete
		if err := copyFile(srcPath, destPath); err != nil {
			return "", false, fmt.Errorf("copy file: %w", err)
		}
		if removeErr := os.Remove(srcPath); removeErr != nil {
			log := logger.New("downloader-import")
			log.Warn("Import succeeded but could not remove source %s: %v", srcPath, removeErr)
			return destPath, false, nil
		}
		return destPath, true, nil
	}

	// Copy only
	if err := copyFile(srcPath, destPath); err != nil {
		return "", false, fmt.Errorf("copy file: %w", err)
	}
	return destPath, false, nil
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
