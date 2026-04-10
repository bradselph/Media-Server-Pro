// Package helpers provides common utility functions for the media server
package helpers

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// FindBinary locates an executable by name. It first delegates to exec.LookPath
// which honors the process PATH. If that fails (common under systemd, which
// strips PATH to a minimal safe set), it falls back to the standard filesystem
// locations where package managers place binaries on Linux/macOS.
//
// Returns the full path on success, or an error that lists all locations tried.
func FindBinary(name string) (string, error) {
	// Fast path: honor the process PATH first (works for non-systemd environments
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

// mediaType tags each extension as video or audio.
type mediaType int

const (
	mediaVideo mediaType = iota
	mediaAudio
)

// mediaExtTypes is the single source of truth for all recognized media extensions.
// To add a format, add it here — IsMediaExtension and IsAudioExtension derive automatically.
var mediaExtTypes = map[string]mediaType{
	// Video
	".mp4": mediaVideo, ".mkv": mediaVideo, ".avi": mediaVideo, ".mov": mediaVideo, ".wmv": mediaVideo,
	".flv": mediaVideo, ".webm": mediaVideo, ".m4v": mediaVideo, ".mpg": mediaVideo, ".mpeg": mediaVideo,
	".3gp": mediaVideo, ".ts": mediaVideo, ".m2ts": mediaVideo, ".vob": mediaVideo, ".ogv": mediaVideo,
	// Audio
	".mp3": mediaAudio, ".wav": mediaAudio, ".flac": mediaAudio, ".aac": mediaAudio, ".ogg": mediaAudio,
	".m4a": mediaAudio, ".opus": mediaAudio, ".wma": mediaAudio, ".alac": mediaAudio, ".ape": mediaAudio,
	".aiff": mediaAudio, ".mka": mediaAudio,
}

// mediaExts and audioExts are derived from mediaExtTypes at init so they
// cannot drift out of sync.
var (
	mediaExts = make(map[string]bool, len(mediaExtTypes))
	audioExts = make(map[string]bool)
)

func init() {
	for ext, mt := range mediaExtTypes {
		mediaExts[ext] = true
		if mt == mediaAudio {
			audioExts[ext] = true
		}
	}
}

// IsMediaExtension checks if a file extension (with leading dot, e.g. ".mp4")
// belongs to a known media format. This is the canonical check used across modules.
func IsMediaExtension(ext string) bool {
	return mediaExts[strings.ToLower(ext)]
}

// IsAudioExtension checks if a file extension (with leading dot, e.g. ".mp3")
// belongs to a known audio-only format (excludes video containers).
func IsAudioExtension(ext string) bool {
	return audioExts[strings.ToLower(ext)]
}

// AllowedProxyHeaders is the canonical set of HTTP response headers that may be
// forwarded from an upstream origin (remote source, slave node) to the client.
// Only media-relevant headers are included to avoid leaking server identity or
// infrastructure details (Server, X-Powered-By, etc.).
var AllowedProxyHeaders = map[string]bool{
	"Content-Type":        true,
	"Content-Length":      true,
	"Content-Range":       true,
	"Content-Disposition": true,
	"Accept-Ranges":       true,
	"Last-Modified":       true,
	"Etag":                true,
	"Cache-Control":       true,
}
