// Package middleware provides HTTP middleware for the media server.
package middleware

import (
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gin-gonic/gin"

	"media-server-pro/internal/logger"
)

// ContextKey is a type for context keys
type ContextKey string

const (
	RequestIDKey ContextKey = "request_id"
)

// trustedProxies is a configurable set of trusted proxy IPs/CIDRs.
// Only requests from these addresses will have X-Forwarded-For / X-Real-IP headers honored.
// By default, private network ranges are trusted.
var (
	trustedProxyNets []*net.IPNet
	trustedProxyOnce sync.Once
)

func initTrustedProxies() {
	trustedProxyOnce.Do(func() {
		privateCIDRs := []string{
			"127.0.0.0/8", "10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16", "::1/128", "fc00::/7",
		}
		for _, cidr := range privateCIDRs {
			_, ipNet, err := net.ParseCIDR(cidr)
			if err == nil {
				trustedProxyNets = append(trustedProxyNets, ipNet)
			}
		}
	})
}

func isTrustedProxy(remoteAddr string) bool {
	initTrustedProxies()
	ip := net.ParseIP(remoteAddr)
	if ip == nil {
		return false
	}
	for _, ipNet := range trustedProxyNets {
		if ipNet.Contains(ip) {
			return true
		}
	}
	return false
}

// GinRequestID adds a unique request ID to each request via X-Request-ID header.
// The request ID is stored in:
//   - Gin context (c.Set) for handler access
//   - c.Request.Context() for framework-agnostic module access via logger.RequestIDFromContext
func GinRequestID() gin.HandlerFunc {
	var counter uint64
	return func(c *gin.Context) {
		id := atomic.AddUint64(&counter, 1)
		requestID := fmt.Sprintf("%d-%d", time.Now().UnixNano(), id)
		c.Set(string(RequestIDKey), requestID)
		c.Header("X-Request-ID", requestID)
		// Also store in request context so modules can extract it via logger.RequestIDFromContext
		c.Request = c.Request.WithContext(logger.ContextWithRequestID(c.Request.Context(), requestID))
		c.Next()
	}
}

func isHTTPS(c *gin.Context) bool {
	return c.Request.TLS != nil || c.GetHeader("X-Forwarded-Proto") == "https"
}

// GinSecurityHeaders adds security headers (CSP, HSTS, X-Frame-Options, etc.)
func GinSecurityHeaders(csp string, hstsMaxAge int) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("X-Content-Type-Options", "nosniff")
		c.Header("X-Frame-Options", "DENY")
		c.Header("X-XSS-Protection", "1; mode=block")
		c.Header("Referrer-Policy", "strict-origin-when-cross-origin")

		if csp != "" {
			c.Header("Content-Security-Policy", csp)
		}

		if hstsMaxAge > 0 && isHTTPS(c) {
			c.Header("Strict-Transport-Security",
				fmt.Sprintf("max-age=%d; includeSubDomains", hstsMaxAge))
		}

		c.Next()
	}
}

// corsConfig holds parsed CORS settings for the handler.
type corsConfig struct {
	allowAll       bool
	allowedOrigins map[string]bool
	methodsStr     string
	headersStr     string
}

func parseCORSConfig(origins, methods, headers []string) corsConfig {
	allowedOrigins := make(map[string]bool)
	allowAll := false
	for _, origin := range origins {
		if origin == "*" {
			allowAll = true
			break
		}
		allowedOrigins[origin] = true
	}
	return corsConfig{
		allowAll:       allowAll,
		allowedOrigins: allowedOrigins,
		methodsStr:     strings.Join(methods, ", "),
		headersStr:     strings.Join(headers, ", "),
	}
}

// allowOrigin returns the value for Access-Control-Allow-Origin and whether CORS is allowed.
func (cfg *corsConfig) allowOrigin(origin string) (value string, allowed bool) {
	if cfg.allowAll {
		if origin != "" {
			return origin, true
		}
		return "*", true
	}
	if cfg.allowedOrigins[origin] {
		return origin, true
	}
	return "", false
}

// GinCORS adds CORS headers to Gin responses
func GinCORS(origins, methods, headers []string) gin.HandlerFunc {
	cfg := parseCORSConfig(origins, methods, headers)

	return func(c *gin.Context) {
		origin := c.GetHeader("Origin")
		if value, allowed := cfg.allowOrigin(origin); allowed {
			c.Header("Access-Control-Allow-Origin", value)
			c.Header("Vary", "Origin")
			c.Header("Access-Control-Allow-Methods", cfg.methodsStr)
			c.Header("Access-Control-Allow-Headers", cfg.headersStr)
			c.Header("Access-Control-Max-Age", "86400")
		}

		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}

		c.Next()
	}
}
