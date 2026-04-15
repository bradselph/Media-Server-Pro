// Package handlers — request parameter and binding helpers.
package handlers

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"media-server-pro/pkg/models"
)

// BindJSON binds the request body to dest. On error writes 400 with errMsg and returns false.
func BindJSON(c *gin.Context, dest any, errMsg string) bool {
	if err := c.ShouldBindJSON(dest); err != nil {
		if errMsg == "" {
			errMsg = errInvalidRequest
		}
		writeError(c, http.StatusBadRequest, errMsg)
		return false
	}
	return true
}

// QueryIntOpts holds default and clamp range for ParseQueryInt.
type QueryIntOpts struct {
	Default int
	Min     int
	Max     int
}

// ParseQueryInt parses key from query as int; returns opts.Default if missing/invalid, then clamps to [opts.Min, opts.Max].
func ParseQueryInt(c *gin.Context, key string, opts QueryIntOpts) int {
	s := c.Query(key)
	if s == "" {
		return opts.Default
	}
	v, err := strconv.Atoi(s)
	if err != nil {
		return opts.Default
	}
	if v < opts.Min {
		return opts.Min
	}
	if v > opts.Max {
		return opts.Max
	}
	return v
}

// LimitOffsetOpts holds default and max values for limit and offset query params.
type LimitOffsetOpts struct {
	DefaultLimit  int
	MaxLimit      int
	DefaultOffset int
	MaxOffset     int
}

// ParseLimitOffset returns limit and offset from query using opts for defaults and caps.
func ParseLimitOffset(c *gin.Context, opts LimitOffsetOpts) (limit, offset int) {
	limit = ParseQueryInt(c, "limit", QueryIntOpts{Default: opts.DefaultLimit, Min: 1, Max: opts.MaxLimit})
	offset = ParseQueryInt(c, "offset", QueryIntOpts{Default: opts.DefaultOffset, Min: 0, Max: opts.MaxOffset})
	return limit, offset
}

// RequireParamID returns the path param value; if empty writes 400 and returns ("", false).
func RequireParamID(c *gin.Context, paramName string) (id string, ok bool) {
	id = strings.TrimSpace(c.Param(paramName))
	if id == "" {
		writeError(c, http.StatusBadRequest, paramName+" is required")
		return "", false
	}
	return id, true
}

// RequireSession returns the session from context; if nil writes 401 and returns nil.
func RequireSession(c *gin.Context) *models.Session {
	session := getSession(c)
	if session == nil {
		writeError(c, http.StatusUnauthorized, errNotAuthenticated)
		return nil
	}
	return session
}
