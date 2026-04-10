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

// mediaExtensions matches the download-move.sh script's supported media types.
var mediaExtensions = map[string]bool{
	".mp4": true, ".mkv": true, ".webm": true, ".mov": true, ".avi": true,
	".flv": true, ".wmv": true, ".m4v": true, ".mpg": true, ".mpeg": true,
	".mp3": true, ".m4a": true, ".opus": true, ".ogg": true, ".flac": true,
	".wav": true, ".aac": true,
}

// ImportableFile represents a completed download ready for import.
type ImportableFile struct {
	Name     string `json:"name"`
	Size     int64  `json:"size"`
	Modified int64  `json:"modified"`
	IsAudio  bool   `json:"isAudio"`
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
		if !mediaExtensions[ext] {
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
