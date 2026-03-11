// Package handlers — response writing and safe header helpers.
package handlers

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"media-server-pro/pkg/models"
)

// writeSuccess writes a successful JSON response.
func writeSuccess(c *gin.Context, data interface{}) {
	c.JSON(http.StatusOK, models.APIResponse{Success: true, Data: data})
}

// writeError writes an error JSON response.
func writeError(c *gin.Context, status int, message string) {
	c.JSON(status, models.APIResponse{Success: false, Error: message})
}

// unsafeContentDispositionChar returns true for runes that must be removed from
// Content-Disposition filenames (quotes, backslashes, newlines, control chars).
func unsafeContentDispositionChar(r rune) bool {
	switch {
	case r == '"', r == '\\', r == '\n', r == '\r':
		return true
	case r < 0x20:
		return true
	default:
		return false
	}
}

// safeContentDisposition returns a Content-Disposition header value with the
// filename sanitized to prevent header injection. Characters that could break
// the header (quotes, backslashes, newlines, control chars) are removed.
func safeContentDisposition(filename string) string {
	var safe strings.Builder
	for _, r := range filename {
		if unsafeContentDispositionChar(r) {
			continue
		}
		safe.WriteRune(r)
	}
	return fmt.Sprintf("attachment; filename=\"%s\"", safe.String())
}
