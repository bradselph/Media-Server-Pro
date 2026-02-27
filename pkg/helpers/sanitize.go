package helpers

import (
	"html"
	"strings"
)

// SanitizeString sanitizes a string for safe storage and HTML rendering
// by escaping HTML entities and removing potentially dangerous characters
func SanitizeString(s string) string {
	// Escape HTML entities to prevent XSS
	s = html.EscapeString(s)
	// Remove null bytes which can cause issues in storage
	s = strings.ReplaceAll(s, "\x00", "")
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

// ValidateMetadataKey checks if a metadata key is valid
// Keys should be non-empty, alphanumeric with underscores/hyphens
func ValidateMetadataKey(key string) bool {
	if key == "" || len(key) > 100 {
		return false
	}
	for _, ch := range key {
		if (ch < 'a' || ch > 'z') && (ch < 'A' || ch > 'Z') &&
			(ch < '0' || ch > '9') && ch != '_' && ch != '-' && ch != '.' {
			return false
		}
	}
	return true
}

// ValidateMetadataValue checks if a metadata value is within acceptable limits
func ValidateMetadataValue(value string) bool {
	// Limit metadata values to 10KB to prevent abuse
	return len(value) <= 10240
}
