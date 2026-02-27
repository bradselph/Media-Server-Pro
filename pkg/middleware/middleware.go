// Package middleware provides HTTP middleware for the media server.
package middleware

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"net/http"
	"runtime/debug"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gin-gonic/gin"

	"media-server-pro/internal/logger"
	"media-server-pro/pkg/models"
)

// ContextKey is a type for context keys
type ContextKey string

const (
	RequestIDKey ContextKey = "request_id"
	UserKey      ContextKey = "user"
	SessionKey   ContextKey = "session"
	StartTimeKey ContextKey = "start_time"
)

// Middleware is a function that wraps an http.Handler
type Middleware func(http.Handler) http.Handler

// Chain combines multiple middleware into a single middleware
func Chain(middlewares ...Middleware) Middleware {
	return func(next http.Handler) http.Handler {
		for i := len(middlewares) - 1; i >= 0; i-- {
			next = middlewares[i](next)
		}
		return next
	}
}

// RequestID adds a unique request ID to each request
func RequestID() Middleware {
	var counter uint64

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id := atomic.AddUint64(&counter, 1)

			requestID := fmt.Sprintf("%d-%d", time.Now().UnixNano(), id)
			ctx := context.WithValue(r.Context(), RequestIDKey, requestID)
			w.Header().Set("X-Request-ID", requestID)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// Logger logs HTTP requests with timing information
func Logger(log *logger.Logger) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			ctx := context.WithValue(r.Context(), StartTimeKey, start)

			// Wrap response writer to capture status code
			wrapped := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}

			// Log request start
			requestID, _ := r.Context().Value(RequestIDKey).(string)
			log.Debug("[%s] Started %s %s from %s",
				requestID, r.Method, r.URL.Path, getClientIP(r))

			next.ServeHTTP(wrapped, r.WithContext(ctx))

			// Log request completion
			duration := time.Since(start)
			log.Info("[%s] %s %s %d %s",
				requestID, r.Method, r.URL.Path, wrapped.statusCode, duration)
		})
	}
}

// Recovery recovers from panics and logs them.
// Only sends a 500 error response if headers have not yet been written.
func Recovery(log *logger.Logger) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			wrapped := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}
			defer func() {
				if err := recover(); err != nil {
					requestID, _ := r.Context().Value(RequestIDKey).(string)
					log.Error("[%s] PANIC RECOVERED: %v\n%s",
						requestID, err, debug.Stack())

					if wrapped.written == 0 {
						http.Error(wrapped, "Internal Server Error", http.StatusInternalServerError)
					}
				}
			}()
			// Recovery middleware should be placed after RequestID and Logger middleware
			// in the chain so that context values (RequestIDKey, StartTimeKey) are available
			// for logging during panic recovery
			next.ServeHTTP(wrapped, r)
		})
	}
}

// CORS adds CORS headers to responses
func CORS(origins, methods, headers []string) Middleware {
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

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")

			if allowAll || allowedOrigins[origin] {
				// Consistent CORS header handling with proper caching hints
				if allowAll {
					if origin != "" {
						// Echo origin for credentials support, with Vary for correct caching
						w.Header().Set("Access-Control-Allow-Origin", origin)
						w.Header().Set("Vary", "Origin")
					} else {
						// Use wildcard only when no origin, with Vary for consistency
						w.Header().Set("Access-Control-Allow-Origin", "*")
						w.Header().Set("Vary", "Origin")
					}
				} else {
					w.Header().Set("Access-Control-Allow-Origin", origin)
					w.Header().Set("Vary", "Origin")
				}
				w.Header().Set("Access-Control-Allow-Methods", methodsStr)
				w.Header().Set("Access-Control-Allow-Headers", headersStr)
				w.Header().Set("Access-Control-Max-Age", "86400")
			}

			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// SecurityHeaders adds security headers to responses
func SecurityHeaders(csp string, hstsMaxAge int) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("X-Content-Type-Options", "nosniff")
			w.Header().Set("X-Frame-Options", "DENY")
			w.Header().Set("X-XSS-Protection", "1; mode=block")
			w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")

			if csp != "" {
				w.Header().Set("Content-Security-Policy", csp)
			}

			if hstsMaxAge > 0 && (r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https") {
				w.Header().Set("Strict-Transport-Security",
					fmt.Sprintf("max-age=%d; includeSubDomains", hstsMaxAge))
			}

			next.ServeHTTP(w, r)
		})
	}
}

// IPFilter filters requests by IP address
type IPFilter struct {
	whitelist map[string]bool
	blacklist map[string]bool
	useWhite  bool
	useBlack  bool
	log       *logger.Logger
	mu        sync.RWMutex
}

// NewIPFilter creates a new IP filter
func NewIPFilter(whitelist, blacklist []string, log *logger.Logger) *IPFilter {
	f := &IPFilter{
		whitelist: make(map[string]bool),
		blacklist: make(map[string]bool),
		useWhite:  len(whitelist) > 0,
		useBlack:  len(blacklist) > 0,
		log:       log,
	}

	for _, ip := range whitelist {
		f.whitelist[ip] = true
	}
	for _, ip := range blacklist {
		f.blacklist[ip] = true
	}

	return f
}

// Middleware returns IP filtering middleware
func (f *IPFilter) Middleware() Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip := getClientIP(r)

			f.mu.RLock()
			defer f.mu.RUnlock()

			// Check blacklist first
			if f.useBlack && f.blacklist[ip] {
				f.log.Warn("Blocked blacklisted IP: %s", ip)
				http.Error(w, "Forbidden", http.StatusForbidden)
				return
			}

			// Check whitelist if enabled
			if f.useWhite && !f.whitelist[ip] {
				f.log.Warn("Blocked non-whitelisted IP: %s", ip)
				http.Error(w, "Forbidden", http.StatusForbidden)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// responseWriter wraps http.ResponseWriter to capture status code
type responseWriter struct {
	http.ResponseWriter
	statusCode int
	written    int64
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}

func (rw *responseWriter) Write(b []byte) (int, error) {
	n, err := rw.ResponseWriter.Write(b)
	rw.written += int64(n)
	return n, err
}

// Flush implements http.Flusher
func (rw *responseWriter) Flush() {
	if f, ok := rw.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// Hijack implements http.Hijacker, delegating to the underlying ResponseWriter.
func (rw *responseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if h, ok := rw.ResponseWriter.(http.Hijacker); ok {
		return h.Hijack()
	}
	return nil, nil, fmt.Errorf("underlying ResponseWriter does not implement http.Hijacker")
}

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

// GetClientIP is a public helper to get client IP
func GetClientIP(r *http.Request) string {
	return getClientIP(r)
}

// GetUser gets the user from context. Returns nil if no user is in the context.
// Callers MUST check for nil before dereferencing.
func GetUser(r *http.Request) *models.User {
	if user, ok := r.Context().Value(UserKey).(*models.User); ok {
		return user
	}
	return nil
}

// GetSession gets the session from context. Returns nil if no session is in the context.
// Callers MUST check for nil before dereferencing.
func GetSession(r *http.Request) *models.Session {
	if session, ok := r.Context().Value(SessionKey).(*models.Session); ok {
		return session
	}
	return nil
}

// SetUser adds a user to the request context
func SetUser(r *http.Request, user *models.User) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), UserKey, user))
}

// SetSession adds a session to the request context
func SetSession(r *http.Request, session *models.Session) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), SessionKey, session))
}

// GetClientIPFromGin returns client IP from a gin context
func GetClientIPFromGin(c *gin.Context) string {
	return getClientIP(c.Request)
}

// GetUserFromGin returns the user from gin context
func GetUserFromGin(c *gin.Context) *models.User {
	if user, exists := c.Get("user"); exists {
		if u, ok := user.(*models.User); ok {
			return u
		}
	}
	return nil
}

// GetSessionFromGin returns the session from gin context
func GetSessionFromGin(c *gin.Context) *models.Session {
	if session, exists := c.Get("session"); exists {
		if s, ok := session.(*models.Session); ok {
			return s
		}
	}
	return nil
}

// GinRequestID adds a unique request ID to each request via X-Request-ID header
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
