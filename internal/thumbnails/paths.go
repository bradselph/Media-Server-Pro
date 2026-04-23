package thumbnails

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// getThumbnailPath generates the output path for a thumbnail (index 0 for main thumbnail).
// mediaID is the stable UUID used as the filename base.
func (m *Module) getThumbnailPath(mediaID MediaID) string {
	return m.getThumbnailPathByIndex(mediaID, 0)
}

// getThumbnailPathWebp returns the WebP path for a given JPEG path.
func (m *Module) getThumbnailPathWebp(jpgPath string) string {
	return strings.TrimSuffix(jpgPath, ".jpg") + ".webp"
}

// getThumbnailPathByIndex generates the output path for a specific thumbnail index.
// mediaID is the stable UUID; the on-disk filename is {uuid}.jpg or {uuid}_preview_N.jpg.
func (m *Module) getThumbnailPathByIndex(mediaID MediaID, index int) string {
	if mediaID == "" {
		return ""
	}
	s := string(mediaID)
	if index == 0 {
		return filepath.Join(m.thumbnailDir, s+".jpg")
	}
	filename := fmt.Sprintf("%s_preview_%d.jpg", s, index-1)
	return filepath.Join(m.thumbnailDir, filename)
}

// GetThumbnailPath returns the thumbnail path for a media ID (public version)
func (m *Module) GetThumbnailPath(mediaID MediaID) string {
	return m.getThumbnailPath(mediaID)
}

// GetThumbnailFilePath returns the absolute file path for a media ID
func (m *Module) GetThumbnailFilePath(mediaID MediaID) string {
	return m.getThumbnailPath(mediaID)
}

// HasThumbnail checks if a valid (non-empty) thumbnail exists for a media ID
func (m *Module) HasThumbnail(mediaID MediaID) bool {
	if mediaID == "" {
		return false
	}
	return isValidThumbnailFile(m.getThumbnailPath(mediaID))
}

// HasWebPThumbnail checks if a WebP thumbnail exists for a media ID
func (m *Module) HasWebPThumbnail(mediaID MediaID) bool {
	if mediaID == "" {
		return false
	}
	jpgPath := m.getThumbnailPath(mediaID)
	webpPath := m.getThumbnailPathWebp(jpgPath)
	_, err := os.Stat(webpPath)
	return err == nil
}

// GetThumbnailFilePathWebp returns the absolute file path for WebP variant, or empty if not found
func (m *Module) GetThumbnailFilePathWebp(mediaID MediaID) string {
	if mediaID == "" {
		return ""
	}
	jpgPath := m.getThumbnailPath(mediaID)
	webpPath := m.getThumbnailPathWebp(jpgPath)
	if _, err := os.Stat(webpPath); err == nil {
		return webpPath
	}
	return ""
}

// GetThumbnailFilePathForSize returns the path for a responsive size (160, 320, 640).
// Responsive sizes are stored as WebP only (-sm.webp, -md.webp, -lg.webp).
// Returns empty if width not in (160, 320, 640) or file does not exist.
func (m *Module) GetThumbnailFilePathForSize(mediaID MediaID, width int) string {
	if mediaID == "" {
		return ""
	}
	var suffix string
	for _, v := range responsiveVariants {
		if v.Width == width {
			suffix = v.Suffix
			break
		}
	}
	if suffix == "" {
		return ""
	}
	s := string(mediaID)
	path := filepath.Join(m.thumbnailDir, s+suffix+".webp")
	if _, err := os.Stat(path); err == nil {
		return path
	}
	return ""
}

// HasAllPreviewThumbnails checks if all preview thumbnails exist for a media ID.
// Files must be non-empty (0-byte files from failed generation are treated as missing).
func (m *Module) HasAllPreviewThumbnails(mediaID MediaID) bool {
	if mediaID == "" {
		return false
	}
	cfg := m.config.Get()
	s := string(mediaID)

	// Check main thumbnail
	mainPath := filepath.Join(m.thumbnailDir, s+".jpg")
	if !isValidThumbnailFile(mainPath) {
		return false
	}

	// Check all preview thumbnails
	for i := 0; i < cfg.Thumbnails.PreviewCount; i++ {
		filename := fmt.Sprintf("%s_preview_%d.jpg", s, i)
		path := filepath.Join(m.thumbnailDir, filename)
		if !isValidThumbnailFile(path) {
			return false
		}
	}
	return true
}

// isValidThumbnailFile returns true if the file exists and is non-empty.
func isValidThumbnailFile(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return info.Size() > 0
}

// GetThumbnailURL returns the URL path for a thumbnail given the media's stable ID.
// Uses the ID-based endpoint so the handler can resolve the media file and enforce
// mature-content checks on every access. The stable ID is stored in the DB and
// survives file renames/moves (see media/discovery.go createMediaItem).
func (m *Module) GetThumbnailURL(mediaID MediaID) string {
	return "/thumbnail?id=" + string(mediaID)
}

// GetThumbnailDir returns the thumbnail directory path
func (m *Module) GetThumbnailDir() string {
	return m.thumbnailDir
}
