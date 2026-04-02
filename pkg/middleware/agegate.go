// Package middleware provides HTTP middleware for the media server.
package middleware

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"media-server-pro/internal/config"
	"media-server-pro/internal/logger"
)

// AgeGate enforces an 18+ age verification gate.
//
// Verification is cookie-primary. When a visitor confirms their age:
//   - A persistent HttpOnly cookie is set (default: 1 year)
//   - The client IP is recorded in an in-memory map for the configured TTL
//
// On subsequent requests the gate is bypassed if EITHER the cookie is present
// OR the IP was verified within the TTL window. This means users who clear
// cookies are not re-prompted within the TTL, which is the expected UX on
// shared or private networks.
//
// CIDR bypass ranges (e.g. LAN subnets, admin IPs) always skip the gate.
type AgeGate struct {
	cfg            config.AgeGateConfig
	mu             sync.RWMutex
	verifiedIPs    map[string]time.Time
	bypassNetworks []*net.IPNet
	log            *logger.Logger
	evictMu        sync.Mutex
	evicting       bool
}

// parseBypassCIDR parses a single bypass IP or CIDR string.
// Returns the network and true on success; logs and returns (nil, false) for empty or invalid entries.
func parseBypassCIDR(log *logger.Logger, raw string) (*net.IPNet, bool) {
	cidr := strings.TrimSpace(raw)
	if cidr == "" {
		return nil, false
	}
	if !strings.Contains(cidr, "/") {
		if strings.Contains(cidr, ":") {
			cidr += "/128" // IPv6
		} else {
			cidr += "/32" // IPv4
		}
	}
	_, network, err := net.ParseCIDR(cidr)
	if err != nil {
		log.Warn("Invalid bypass IP/CIDR %q: %v", cidr, err)
		return nil, false
	}
	return network, true
}

// NewAgeGate creates an AgeGate from the provided config.
// Invalid CIDR entries in BypassIPs are logged and skipped.
func NewAgeGate(cfg config.AgeGateConfig) *AgeGate {
	ag := &AgeGate{
		cfg:         cfg,
		verifiedIPs: make(map[string]time.Time),
		log:         logger.New("agegate"),
	}
	for _, raw := range cfg.BypassIPs {
		if network, ok := parseBypassCIDR(ag.log, raw); ok {
			ag.bypassNetworks = append(ag.bypassNetworks, network)
		}
	}
	if cfg.Enabled {
		ag.log.Info("Age gate enabled (IP TTL: %v, bypass CIDRs: %d)", cfg.IPVerifyTTL, len(ag.bypassNetworks))
	}
	return ag
}

// extractClientIP returns the real client IP, honouring X-Forwarded-For only
// from trusted reverse proxies (private network ranges). Uses the same trusted
// proxy validation as the main middleware to prevent IP spoofing.
func extractClientIP(r *http.Request) string {
	remoteIP, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		remoteIP = r.RemoteAddr
	}

	if IsTrustedProxy(remoteIP) {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			if idx := strings.IndexByte(xff, ','); idx != -1 {
				return strings.TrimSpace(xff[:idx])
			}
			return strings.TrimSpace(xff)
		}
		if realIP := r.Header.Get("X-Real-IP"); realIP != "" {
			return realIP
		}
	}

	return remoteIP
}

// isBypass returns true if the IP matches any configured bypass network.
func (ag *AgeGate) isBypass(ip string) bool {
	parsed := net.ParseIP(ip)
	if parsed == nil {
		return false
	}
	for _, network := range ag.bypassNetworks {
		if network.Contains(parsed) {
			return true
		}
	}
	return false
}

// isIPVerified returns true if this IP was verified within the configured TTL.
func (ag *AgeGate) isIPVerified(ip string) bool {
	if ag.cfg.IPVerifyTTL <= 0 {
		return false
	}
	ag.mu.RLock()
	t, ok := ag.verifiedIPs[ip]
	ag.mu.RUnlock()
	return ok && time.Since(t) < ag.cfg.IPVerifyTTL
}

// hasCookie returns true if the request carries a valid age-verified cookie.
func (ag *AgeGate) hasCookie(r *http.Request) bool {
	cookie, err := r.Cookie(ag.cfg.CookieName)
	return err == nil && cookie.Value == "1"
}

// IsVerified returns true if this request should bypass the age gate.
func (ag *AgeGate) IsVerified(r *http.Request) bool {
	if !ag.cfg.Enabled {
		return true
	}
	ip := extractClientIP(r)
	return ag.isBypass(ip) || ag.hasCookie(r) || ag.isIPVerified(ip)
}

// scheduleEvict runs evictExpired in a goroutine but only if no eviction is
// already in progress, preventing unbounded goroutine growth under high traffic.
func (ag *AgeGate) scheduleEvict() {
	ag.evictMu.Lock()
	if ag.evicting {
		ag.evictMu.Unlock()
		return
	}
	ag.evicting = true
	ag.evictMu.Unlock()
	go func() {
		defer func() {
			ag.evictMu.Lock()
			ag.evicting = false
			ag.evictMu.Unlock()
		}()
		ag.evictExpired()
	}()
}

// evictExpired removes stale IP entries from the verified map.
func (ag *AgeGate) evictExpired() {
	if ag.cfg.IPVerifyTTL <= 0 {
		return
	}
	ag.mu.Lock()
	defer ag.mu.Unlock()
	cutoff := time.Now().Add(-ag.cfg.IPVerifyTTL)
	for ip, t := range ag.verifiedIPs {
		if t.Before(cutoff) {
			delete(ag.verifiedIPs, ip)
		}
	}
}

// ageGateSecure detects HTTPS, including through TLS-terminating reverse proxies.
// X-Forwarded-Proto and Cf-Visitor are only trusted when the request comes from
// a configured trusted proxy IP to prevent clients from spoofing secure cookies.
func ageGateSecure(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	remoteIP, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		remoteIP = r.RemoteAddr
	}
	if IsTrustedProxy(remoteIP) {
		if r.Header.Get("X-Forwarded-Proto") == "https" {
			return true
		}
		if strings.Contains(r.Header.Get("Cf-Visitor"), `"scheme":"https"`) {
			return true
		}
	}
	return false
}

// GinStatusHandler returns a gin handler for GET /api/age-gate/status
func (ag *AgeGate) GinStatusHandler() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"success": true,
			"data": gin.H{
				"enabled":  ag.cfg.Enabled,
				"verified": ag.IsVerified(c.Request),
			},
		})
	}
}

// GinVerifyHandler returns a gin handler for POST /api/age-verify
func (ag *AgeGate) GinVerifyHandler() gin.HandlerFunc {
	return func(c *gin.Context) {
		if ag.cfg.Enabled {
			ip := extractClientIP(c.Request)
			ag.mu.Lock()
			// Cap map size to prevent unbounded memory growth under high traffic.
			const maxVerifiedIPs = 100000
			if len(ag.verifiedIPs) >= maxVerifiedIPs {
				// Evict oldest entries when at capacity
				ag.mu.Unlock()
				ag.evictExpired()
				ag.mu.Lock()
			}
			ag.verifiedIPs[ip] = time.Now()
			ag.mu.Unlock()
			ag.scheduleEvict()
			http.SetCookie(c.Writer, &http.Cookie{
				Name:     ag.cfg.CookieName,
				Value:    "1",
				MaxAge:   ag.cfg.CookieMaxAge,
				Path:     "/",
				HttpOnly: true,
				SameSite: http.SameSiteStrictMode,
				Secure:   ageGateSecure(c.Request),
			})
		}
		c.JSON(http.StatusOK, gin.H{"success": true})
	}
}
