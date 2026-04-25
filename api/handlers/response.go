// Package handlers — response writing and safe header helpers.
package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"media-server-pro/pkg/helpers"
	"media-server-pro/pkg/models"
)

// writeSuccess writes a successful JSON response.
func writeSuccess(c *gin.Context, data any) {
	c.JSON(http.StatusOK, models.APIResponse{Success: true, Data: data})
}

// writeError writes an error JSON response.
func writeError(c *gin.Context, status int, message string) {
	c.JSON(status, models.APIResponse{Success: false, Error: message})
}

// safeContentDisposition returns a Content-Disposition header value with the
// filename sanitized to prevent header injection (delegates to helpers).
func safeContentDisposition(filename string) string {
	return helpers.SafeContentDispositionFilename(filename)
}

// truncateQuery caps a user-supplied query string to maxLen runes to prevent
// pathological LIKE-clause fan-out when the value is split into search tokens.
func truncateQuery(s string, maxLen int) string {
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	return string(runes[:maxLen])
}
