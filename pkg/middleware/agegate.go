// Package middleware provides HTTP middleware for the media server.
package middleware

import (
	"encoding/json"
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
		cidr := strings.TrimSpace(raw)
		if cidr == "" {
			continue
		}
		// Support plain IPs without a prefix length
		if !strings.Contains(cidr, "/") {
			if strings.Contains(cidr, ":") {
				cidr += "/128" // IPv6
			} else {
				cidr += "/32" // IPv4
			}
		}
		_, network, err := net.ParseCIDR(cidr)
		if err != nil {
			ag.log.Warn("Invalid bypass IP/CIDR %q: %v", cidr, err)
			continue
		}
		ag.bypassNetworks = append(ag.bypassNetworks, network)
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

	if isTrustedProxy(remoteIP) {
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

// evictExpired removes stale IP entries from the verified map (called async).
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

// ageGateSecure detects HTTPS, including through TLS-terminating reverse proxies
// (nginx, Cloudflare, etc.).
func ageGateSecure(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	if r.Header.Get("X-Forwarded-Proto") == "https" {
		return true
	}
	if strings.Contains(r.Header.Get("Cf-Visitor"), `"scheme":"https"`) {
		return true
	}
	return false
}

// StatusHandler handles GET /api/age-gate/status.
// Returns { enabled, verified } so the frontend can decide whether to show the overlay.
// This endpoint is intentionally public — no auth required.
func (ag *AgeGate) StatusHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"data": map[string]interface{}{
			"enabled":  ag.cfg.Enabled,
			"verified": ag.IsVerified(r),
		},
	})
}

// VerifyHandler handles POST /api/age-verify.
// Records the visitor's age confirmation: sets a persistent cookie and caches the IP.
func (ag *AgeGate) VerifyHandler(w http.ResponseWriter, r *http.Request) {
	if ag.cfg.Enabled {
		ip := extractClientIP(r)

		ag.mu.Lock()
		ag.verifiedIPs[ip] = time.Now()
		ag.mu.Unlock()

		// Async GC so eviction doesn't block the response
		go ag.evictExpired()

		http.SetCookie(w, &http.Cookie{
			Name:     ag.cfg.CookieName,
			Value:    "1",
			MaxAge:   ag.cfg.CookieMaxAge,
			Path:     "/",
			HttpOnly: true,
			SameSite: http.SameSiteLaxMode,
			Secure:   ageGateSecure(r),
		})
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"success": true})
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
			ag.verifiedIPs[ip] = time.Now()
			ag.mu.Unlock()
			go ag.evictExpired()
			http.SetCookie(c.Writer, &http.Cookie{
				Name:     ag.cfg.CookieName,
				Value:    "1",
				MaxAge:   ag.cfg.CookieMaxAge,
				Path:     "/",
				HttpOnly: true,
				SameSite: http.SameSiteLaxMode,
				Secure:   ageGateSecure(c.Request),
			})
		}
		c.JSON(http.StatusOK, gin.H{"success": true})
	}
}
