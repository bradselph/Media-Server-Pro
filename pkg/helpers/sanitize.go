package helpers

import (
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

// validMetadataKeyRunes is the set of runes allowed in a metadata key
// (alphanumeric, underscore, hyphen, period). Built in init to keep isValidMetadataKeyChar simple.
var validMetadataKeyRunes map[rune]struct{}

func init() {
	validMetadataKeyRunes = make(map[rune]struct{}, 26+26+10+3)
	for r := 'a'; r <= 'z'; r++ {
		validMetadataKeyRunes[r] = struct{}{}
	}
	for r := 'A'; r <= 'Z'; r++ {
		validMetadataKeyRunes[r] = struct{}{}
	}
	for r := '0'; r <= '9'; r++ {
		validMetadataKeyRunes[r] = struct{}{}
	}
	for _, r := range []rune{'_', '-', '.'} {
		validMetadataKeyRunes[r] = struct{}{}
	}
}

// isValidMetadataKeyChar returns true if the rune is allowed in a metadata key.
func isValidMetadataKeyChar(ch rune) bool {
	_, ok := validMetadataKeyRunes[ch]
	return ok
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
