// Package middleware provides HTTP middleware for the media server.
package middleware

import (
	"fmt"
	"net"
	"net/http"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode"

	"github.com/gin-gonic/gin"

	"media-server-pro/internal/logger"
)

const (
	maxRequestIDLen = 64
	headerRequestID = "X-Request-ID"
)

// sanitizeRequestID truncates to maxRequestIDLen and strips control/non-printable
// characters to prevent log injection via X-Request-ID.
func sanitizeRequestID(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if b.Len() >= maxRequestIDLen {
			break
		}
		if unicode.IsPrint(r) {
			b.WriteRune(r)
		}
	}
	return strings.TrimSpace(b.String())
}

// ContextKey is a type for context keys
type ContextKey string

const (
	RequestIDKey ContextKey = "request_id"
)

// ipNetList aliases the parsed CIDR slice so atomic.Pointer's type-parameter
// brackets aren't followed directly by slice brackets (Go's parser rejects
// `atomic.Pointer[[]*net.IPNet]` even though the type is otherwise valid).
type ipNetList = []*net.IPNet

// trustedProxyNets is the built-in set of RFC-1918 + loopback ranges that are
// always trusted. Only requests from these addresses (or from the configured
// extra set, see SetExtraTrustedProxies) will have X-Forwarded-For / X-Real-IP /
// X-Forwarded-Proto honored.
var (
	trustedProxyNets []*net.IPNet
	trustedProxyOnce sync.Once

	// extraTrustedNets holds the operator-configured additional CIDRs from
	// SecurityConfig.TrustedProxyCIDRs. Updated atomically by SetExtraTrustedProxies
	// so the security module's config watcher can hot-reload the list without
	// taking any read-side locks on the request hot path.
	extraTrustedNets atomic.Pointer[ipNetList]
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

// SetExtraTrustedProxies registers additional trusted-proxy CIDRs on top of the
// built-in RFC-1918 + loopback ranges. Pass an empty slice to revert to
// private-ranges-only behavior. Invalid CIDR strings are silently dropped.
//
// This is the bridge between SecurityConfig.TrustedProxyCIDRs (the admin-editable
// list) and every IsTrustedProxy() callsite — without it, those callsites would
// only see the hard-coded private list and the admin setting would have no effect
// on the request hot path.
//
// Safe to call concurrently and at any time; readers will observe the new set
// atomically on their next IsTrustedProxy call.
func SetExtraTrustedProxies(cidrs []string) {
	parsed := make(ipNetList, 0, len(cidrs))
	for _, cidr := range cidrs {
		s := strings.TrimSpace(cidr)
		if s == "" {
			continue
		}
		if _, ipNet, err := net.ParseCIDR(s); err == nil {
			parsed = append(parsed, ipNet)
		}
	}
	extraTrustedNets.Store(&parsed)
}

// IsTrustedProxy reports whether remoteAddr belongs to a trusted proxy range —
// either one of the built-in private ranges (RFC-1918, loopback, ULA) or one of
// the operator-configured extra CIDRs registered via SetExtraTrustedProxies.
// Exported so handlers can use the same logic as the middleware.
func IsTrustedProxy(remoteAddr string) bool {
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
	if extra := extraTrustedNets.Load(); extra != nil {
		for _, ipNet := range *extra {
			if ipNet.Contains(ip) {
				return true
			}
		}
	}
	return false
}

// GinRequestID adds a unique request ID to each request via X-Request-ID header.
// If the client or upstream proxy sends X-Request-ID, it is propagated; otherwise a new ID is generated.
// The request ID is stored in:
//   - Gin context (c.Set) for handler access
//   - c.Request.Context() for framework-agnostic module access via logger.RequestIDFromContext
func GinRequestID() gin.HandlerFunc {
	var counter uint64
	return func(c *gin.Context) {
		requestID := c.GetHeader(headerRequestID)
		if requestID == "" {
			id := atomic.AddUint64(&counter, 1)
			requestID = fmt.Sprintf("%d-%d", time.Now().UnixNano(), id)
			c.Header(headerRequestID, requestID)
		} else {
			requestID = sanitizeRequestID(requestID)
			if requestID == "" {
				id := atomic.AddUint64(&counter, 1)
				requestID = fmt.Sprintf("%d-%d", time.Now().UnixNano(), id)
			}
			c.Header(headerRequestID, requestID)
		}
		c.Set(string(RequestIDKey), requestID)
		c.Request = c.Request.WithContext(logger.ContextWithRequestID(c.Request.Context(), requestID))
		c.Next()
	}
}

// isHTTPS returns true when the connection is HTTPS, either via TLS or via
// X-Forwarded-Proto from a trusted proxy. X-Forwarded-Proto is only honored when
// the request came from a trusted proxy to prevent clients from spoofing HTTPS
// and tricking the server into setting HSTS over insecure transport.
func isHTTPS(c *gin.Context) bool {
	if c.Request.TLS != nil {
		return true
	}
	remoteIP, _, err := net.SplitHostPort(c.Request.RemoteAddr)
	if err != nil {
		remoteIP = c.Request.RemoteAddr
	}
	if IsTrustedProxy(remoteIP) && c.GetHeader("X-Forwarded-Proto") == "https" {
		return true
	}
	return false
}

// GinSecurityHeaders adds security headers (CSP, HSTS, X-Frame-Options, etc.)
// The getCfg function is called on every request so changes to CSP/HSTS config
// take effect immediately without a server restart.
func GinSecurityHeaders(getCfg func() (csp string, hstsMaxAge int)) gin.HandlerFunc {
	return func(c *gin.Context) {
		// When behind Cloudflare (CF-Ray present), skip the headers CF adds
		// automatically to avoid duplicate/conflicting values in the response.
		// On direct connections (no CF-Ray) we set them ourselves.
		behindCloudflare := c.GetHeader("CF-Ray") != ""
		if !behindCloudflare {
			c.Header("X-Content-Type-Options", "nosniff")
			c.Header("X-Frame-Options", "DENY")
			c.Header("X-XSS-Protection", "1; mode=block")
			c.Header("Referrer-Policy", "strict-origin-when-cross-origin")
		}
		c.Header("Permissions-Policy", "camera=(), microphone=(), geolocation=(), payment=()")

		csp, hstsMaxAge := getCfg()
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

// originSet holds the parsed CORS origin allowlist.
//
// Factored out from corsConfig so both the static GinCORS (whose origins list
// is fixed at construction) and the dynamic GinCORSDynamic (which re-reads it
// on every request) can share the same matching logic.
type originSet struct {
	allowAll       bool
	allowedOrigins map[string]bool
}

func parseOriginSet(origins []string) originSet {
	s := originSet{allowedOrigins: make(map[string]bool, len(origins))}
	for _, origin := range origins {
		if origin == "*" {
			s.allowAll = true
			return s
		}
		s.allowedOrigins[origin] = true
	}
	return s
}

// allowOrigin returns the value for Access-Control-Allow-Origin and whether CORS is allowed.
// When allowAll is true, always returns literal "*" to prevent browsers from sending credentials
// cross-origin — reflecting the specific origin with Allow-Credentials: true would allow any
// site to make credentialed requests and steal session cookies.
func (s *originSet) allowOrigin(origin string) (value string, allowed bool) {
	if s.allowAll {
		return "*", true
	}
	if s.allowedOrigins[origin] {
		return origin, true
	}
	return "", false
}

// corsConfig holds parsed CORS settings for the handler.
type corsConfig struct {
	originSet
	methodsStr string
	headersStr string
}

func parseCORSConfig(origins, methods, headers []string) corsConfig {
	return corsConfig{
		originSet:  parseOriginSet(origins),
		methodsStr: strings.Join(methods, ", "),
		headersStr: strings.Join(headers, ", "),
	}
}

// writeCORSHeaders writes the per-response CORS headers when the request's
// Origin is in the allowed set. Shared by GinCORS and GinCORSDynamic.
func writeCORSHeaders(c *gin.Context, set *originSet, methodsStr, headersStr string) {
	origin := c.GetHeader("Origin")
	value, allowed := set.allowOrigin(origin)
	if !allowed {
		return
	}
	c.Header("Access-Control-Allow-Origin", value)
	c.Header("Vary", "Origin")
	c.Header("Access-Control-Allow-Methods", methodsStr)
	c.Header("Access-Control-Allow-Headers", headersStr)
	c.Header("Access-Control-Max-Age", "86400")
	if value != "*" {
		c.Header("Access-Control-Allow-Credentials", "true")
	}
}

// GinCORS adds CORS headers to Gin responses with a fixed origin allowlist
// captured at construction time. Prefer GinCORSDynamic when the allowlist
// needs to track config changes without a server restart.
//
// When allowAll is true and a specific Origin is present, Access-Control-Allow-Credentials
// is set so cookie-based session auth works for cross-origin credentialed requests.
func GinCORS(origins, methods, headers []string) gin.HandlerFunc {
	cfg := parseCORSConfig(origins, methods, headers)

	return func(c *gin.Context) {
		writeCORSHeaders(c, &cfg.originSet, cfg.methodsStr, cfg.headersStr)

		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}

		c.Next()
	}
}

// GinCORSDynamic is like GinCORS but reads the allowed origins list on every
// request via getOrigins, so edits to cors_origins take effect immediately
// without a server restart. Methods and headers are fixed at construction time
// since those rarely change at runtime.
//
// The parsed origin set is cached and only rebuilt when getOrigins returns a
// slice that differs from the previous one — avoiding the per-request map
// allocation in the common case where the config is steady.
//
// If getOrigins returns nil or empty, no CORS headers are written (treated as
// "CORS disabled"). This lets the caller's closure encode policy like
// "wildcard + auth → no CORS" cleanly.
func GinCORSDynamic(getOrigins func() []string, methods, headers []string) gin.HandlerFunc {
	methodsStr := strings.Join(methods, ", ")
	headersStr := strings.Join(headers, ", ")

	var (
		mu        sync.Mutex
		cachedRaw []string
		cachedSet originSet
	)

	return func(c *gin.Context) {
		raw := getOrigins()
		mu.Lock()
		if !slices.Equal(raw, cachedRaw) {
			// Copy to avoid retaining a reference the caller might mutate.
			cachedRaw = append(cachedRaw[:0], raw...)
			cachedSet = parseOriginSet(raw)
		}
		set := cachedSet
		mu.Unlock()

		if len(raw) > 0 {
			writeCORSHeaders(c, &set, methodsStr, headersStr)
		}

		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}

		c.Next()
	}
}
