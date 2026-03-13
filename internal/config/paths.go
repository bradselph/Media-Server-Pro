package config

import (
	"fmt"
	"os"
	"path/filepath"
)

func (m *Manager) resolveAbsolutePaths() {
	resolvePath := func(path string, name string) string {
		if path == "" {
			return path
		}
		if filepath.IsAbs(path) {
			return path
		}
		absPath, err := filepath.Abs(path)
		if err != nil {
			m.log.Warn("Failed to resolve absolute path for %s (%s): %v", name, path, err)
			return path
		}
		m.log.Debug("Resolved %s: %s -> %s", name, path, absPath)
		return absPath
	}

	m.config.Directories.Videos = resolvePath(m.config.Directories.Videos, "videos directory")
	m.config.Directories.Music = resolvePath(m.config.Directories.Music, "music directory")
	m.config.Directories.Thumbnails = resolvePath(m.config.Directories.Thumbnails, "thumbnails directory")
	m.config.Directories.Playlists = resolvePath(m.config.Directories.Playlists, "playlists directory")
	m.config.Directories.Uploads = resolvePath(m.config.Directories.Uploads, "uploads directory")
	m.config.Directories.Analytics = resolvePath(m.config.Directories.Analytics, "analytics directory")
	m.config.Directories.HLSCache = resolvePath(m.config.Directories.HLSCache, "HLS cache directory")
	m.config.Directories.Data = resolvePath(m.config.Directories.Data, "data directory")
	m.config.Directories.Logs = resolvePath(m.config.Directories.Logs, "logs directory")
	m.config.Directories.Temp = resolvePath(m.config.Directories.Temp, "temp directory")

	if m.config.Server.CertFile != "" {
		m.config.Server.CertFile = resolvePath(m.config.Server.CertFile, "cert file")
	}
	if m.config.Server.KeyFile != "" {
		m.config.Server.KeyFile = resolvePath(m.config.Server.KeyFile, "key file")
	}
}

// CreateDirectories ensures all configured directories exist.
// Paths are typically already absolute after Load/resolveAbsolutePaths; we only call
// filepath.Abs when the path is relative so behavior is correct if CreateDirectories
// runs without a prior Load.
func (m *Manager) CreateDirectories() error {
	m.mu.RLock()
	defer m.mu.RUnlock()

	dirs := []string{
		m.config.Directories.Videos,
		m.config.Directories.Music,
		m.config.Directories.Thumbnails,
		m.config.Directories.Playlists,
		m.config.Directories.Uploads,
		m.config.Directories.Analytics,
		m.config.Directories.HLSCache,
		m.config.Directories.Data,
		m.config.Directories.Logs,
		m.config.Directories.Temp,
	}

	for _, dir := range dirs {
		absDir := dir
		if !filepath.IsAbs(dir) {
			var err error
			absDir, err = filepath.Abs(dir)
			if err != nil {
				return fmt.Errorf("failed to resolve path %s: %w", dir, err)
			}
		}
		if err := os.MkdirAll(absDir, 0755); err != nil {
			return fmt.Errorf("failed to create directory %s: %w", absDir, err)
		}
		info, err := os.Stat(absDir)
		if err != nil {
			return fmt.Errorf("failed to stat directory %s: %w", absDir, err)
		}
		if !info.IsDir() {
			return fmt.Errorf("path exists but is not a directory: %s", absDir)
		}
	}
	return nil
}
