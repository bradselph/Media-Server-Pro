// Package helpers provides common utility functions for the media server
package helpers

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// FindBinary locates an executable by name. It first delegates to exec.LookPath
// which honours the process PATH. If that fails (common under systemd, which
// strips PATH to a minimal safe set), it falls back to the standard filesystem
// locations where package managers place binaries on Linux/macOS.
//
// Returns the full path on success, or an error that lists all locations tried.
func FindBinary(name string) (string, error) {
	// Fast path: honour the process PATH first (works for non-systemd environments
	// and any custom PATH set in the EnvironmentFile).
	if path, err := exec.LookPath(name); err == nil {
		return path, nil
	}

	// Fallback: standard filesystem locations used by apt, homebrew, snap, and
	// manual installs. Ordered from most to least common on a typical VPS.
	candidates := []string{
		"/usr/bin/" + name,
		"/usr/local/bin/" + name,
		"/bin/" + name,
		"/usr/sbin/" + name,
		"/usr/local/sbin/" + name,
		"/snap/bin/" + name,
	}
	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
	}
	return "", fmt.Errorf("%q not found in PATH or standard locations (%s)",
		name, strings.Join(candidates, ", "))
}

// StatusString returns "healthy" or "unhealthy" based on the given boolean.
// This is used by all modules to report their health status consistently.
func StatusString(healthy bool) string {
	if healthy {
		return "healthy"
	}
	return "unhealthy"
}

// mediaExts is the canonical set of recognized media file extensions, initialized once.
var mediaExts = map[string]bool{
	// Video
	".mp4": true, ".mkv": true, ".avi": true, ".mov": true, ".wmv": true,
	".flv": true, ".webm": true, ".m4v": true, ".mpg": true, ".mpeg": true,
	".3gp": true, ".ts": true, ".m2ts": true, ".vob": true, ".ogv": true,
	// Audio
	".mp3": true, ".wav": true, ".flac": true, ".aac": true, ".ogg": true,
	".m4a": true, ".opus": true, ".wma": true, ".alac": true, ".ape": true,
	".aiff": true, ".mka": true,
}

// IsMediaExtension checks if a file extension (with leading dot, e.g. ".mp4")
// belongs to a known media format. This is the canonical check used across modules.
func IsMediaExtension(ext string) bool {
	return mediaExts[strings.ToLower(ext)]
}

// audioExts is the canonical set of recognized audio-only file extensions.
var audioExts = map[string]bool{
	".mp3": true, ".wav": true, ".flac": true, ".aac": true, ".ogg": true,
	".m4a": true, ".opus": true, ".wma": true, ".alac": true, ".ape": true,
	".aiff": true, ".mka": true,
}

// IsAudioExtension checks if a file extension (with leading dot, e.g. ".mp3")
// belongs to a known audio-only format (excludes video containers).
func IsAudioExtension(ext string) bool {
	return audioExts[strings.ToLower(ext)]
}
