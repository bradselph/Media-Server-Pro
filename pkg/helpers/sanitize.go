package helpers

import (
	"fmt"
	"html"
	"strings"
)

// SanitizeString sanitizes a string for safe storage and HTML rendering
// by escaping HTML entities and removing potentially dangerous characters
func SanitizeString(s string) string {
	// Remove null bytes first (before escape) to avoid corruption with adjacent HTML-escape chars
	s = strings.ReplaceAll(s, "\x00", "")
	// Escape HTML entities to prevent XSS
	s = html.EscapeString(s)
	return s
}

// SanitizeMap sanitizes all string values in a map for safe storage and rendering
func SanitizeMap(m map[string]string) map[string]string {
	if m == nil {
		return nil
	}
	sanitized := make(map[string]string, len(m))
	for k, v := range m {
		// Sanitize both keys and values to be extra safe
		sanitized[SanitizeString(k)] = SanitizeString(v)
	}
	return sanitized
}

// isValidMetadataKeyChar returns true if the rune is allowed in a metadata key
// (alphanumeric, underscore, hyphen, period).
func isValidMetadataKeyChar(ch rune) bool {
	return (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') ||
		(ch >= '0' && ch <= '9') || ch == '_' || ch == '-' || ch == '.'
}

// allMetadataKeyCharsValid returns true if every rune in s is allowed in a metadata key.
func allMetadataKeyCharsValid(s string) bool {
	for _, ch := range s {
		if !isValidMetadataKeyChar(ch) {
			return false
		}
	}
	return true
}

// ValidateMetadataKey checks if a metadata key is valid
// Keys should be non-empty, alphanumeric with underscores/hyphens
func ValidateMetadataKey(key string) bool {
	if key == "" || len(key) > 100 {
		return false
	}
	return allMetadataKeyCharsValid(key)
}

// ValidateMetadataValue checks if a metadata value is within acceptable limits
func ValidateMetadataValue(value string) bool {
	// Limit metadata values to 10KB to prevent abuse
	return len(value) <= 10240
}

// SafeContentDispositionFilename returns a Content-Disposition attachment header value
// with the filename sanitized to prevent header injection. Quotes, backslashes, newlines,
// and control characters (< 0x20) are stripped.
func SafeContentDispositionFilename(filename string) string {
	var b strings.Builder
	for _, r := range filename {
		if r == '"' || r == '\\' || r == '\n' || r == '\r' || r == ';' || r < 0x20 {
			continue
		}
		b.WriteRune(r)
	}
	return fmt.Sprintf("attachment; filename=%q", b.String())
}
