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
)

// ContextKey is a type for context keys
type ContextKey string

const (
	RequestIDKey ContextKey = "request_id"
	UserKey      ContextKey = "user"
	SessionKey   ContextKey = "session"
	StartTimeKey ContextKey = "start_time"
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

func getClientIP(r *http.Request) string {
	remoteIP, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		remoteIP = r.RemoteAddr
	}

	// Only trust forwarded headers from known proxies
	if isTrustedProxy(remoteIP) {
		if forwarded := r.Header.Get("X-Forwarded-For"); forwarded != "" {
			ips := strings.Split(forwarded, ",")
			return strings.TrimSpace(ips[0])
		}
		if realIP := r.Header.Get("X-Real-IP"); realIP != "" {
			return realIP
		}
	}

	return remoteIP
}

// GinRequestID adds a unique request ID to each request via X-Request-ID header.
// The request ID is also stored in c.Request.Context() so framework-agnostic
// modules can retrieve it via context.Value(middleware.RequestIDKey).
func GinRequestID() gin.HandlerFunc {
	var counter uint64
	return func(c *gin.Context) {
		id := atomic.AddUint64(&counter, 1)
		requestID := fmt.Sprintf("%d-%d", time.Now().UnixNano(), id)
		c.Set(string(RequestIDKey), requestID)
		c.Header("X-Request-ID", requestID)
		c.Next()
	}
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

		if hstsMaxAge > 0 && (c.Request.TLS != nil || c.GetHeader("X-Forwarded-Proto") == "https") {
			c.Header("Strict-Transport-Security",
				fmt.Sprintf("max-age=%d; includeSubDomains", hstsMaxAge))
		}

		c.Next()
	}
}

// GinCORS adds CORS headers to Gin responses
func GinCORS(origins, methods, headers []string) gin.HandlerFunc {
	allowedOrigins := make(map[string]bool)
	allowAll := false
	for _, origin := range origins {
		if origin == "*" {
			allowAll = true
			break
		}
		allowedOrigins[origin] = true
	}

	methodsStr := strings.Join(methods, ", ")
	headersStr := strings.Join(headers, ", ")

	return func(c *gin.Context) {
		origin := c.GetHeader("Origin")

		if allowAll || allowedOrigins[origin] {
			if allowAll {
				if origin != "" {
					c.Header("Access-Control-Allow-Origin", origin)
					c.Header("Vary", "Origin")
				} else {
					c.Header("Access-Control-Allow-Origin", "*")
					c.Header("Vary", "Origin")
				}
			} else {
				c.Header("Access-Control-Allow-Origin", origin)
				c.Header("Vary", "Origin")
			}
			c.Header("Access-Control-Allow-Methods", methodsStr)
			c.Header("Access-Control-Allow-Headers", headersStr)
			c.Header("Access-Control-Max-Age", "86400")
		}

		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}

		c.Next()
	}
}
