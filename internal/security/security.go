// Package security provides IP filtering, rate limiting, and security controls.
package security

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"media-server-pro/internal/config"
	"media-server-pro/internal/logger"
	"media-server-pro/pkg/helpers"
	"media-server-pro/pkg/models"
)

const errSaveIPListsFmt = "failed to save IP lists: %v"

// privateCIDRs are common private network ranges trusted for reverse proxy usage.
// Parsed once at package init to avoid per-request overhead.
var privateCIDRs []*net.IPNet

func init() {
	privateRanges := []string{
		"127.0.0.0/8",    // IPv4 loopback
		"10.0.0.0/8",     // IPv4 private
		"172.16.0.0/12",  // IPv4 private
		"192.168.0.0/16", // IPv4 private
		"::1/128",        // IPv6 loopback
		"fc00::/7",       // IPv6 unique local
	}
	for _, cidr := range privateRanges {
		_, ipNet, err := net.ParseCIDR(cidr)
		if err == nil {
			privateCIDRs = append(privateCIDRs, ipNet)
		}
	}
}

// Module handles security controls
type Module struct {
	config           *config.Manager
	log              *logger.Logger
	whitelist        *IPList
	blacklist        *IPList
	rateLimiter      *RateLimiter
	dataDir          string
	healthy          bool
	healthMsg        string
	totalBlocked     int64
	totalRateLimited int64
	mu               sync.RWMutex
}

// IPList manages a list of IP addresses/ranges
type IPList struct {
	Name    string    `json:"name"`
	Enabled bool      `json:"enabled"`
	Entries []IPEntry `json:"entries"`
	mu      sync.RWMutex
}

// IPEntry represents a single IP or CIDR range
type IPEntry struct {
	Value     string     `json:"value"`
	CIDR      *net.IPNet `json:"-"`
	IP        net.IP     `json:"-"`
	Comment   string     `json:"comment"`
	AddedAt   time.Time  `json:"added_at"`
	AddedBy   string     `json:"added_by"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
}

// RateLimiter implements sliding window rate limiting with burst detection
type RateLimiter struct {
	config      RateLimitConfig
	clients     map[string]*ClientState
	bannedIPs   map[string]time.Time
	mu          sync.RWMutex
	cleanupTick *time.Ticker
	stopCleanup chan struct{}
}

// RateLimitConfig holds rate limiter configuration
type RateLimitConfig struct {
	RequestsPerMinute int           `json:"requests_per_minute"`
	BurstLimit        int           `json:"burst_limit"`
	BurstWindow       time.Duration `json:"burst_window"`
	BanDuration       time.Duration `json:"ban_duration"`
	ViolationsForBan  int           `json:"violations_for_ban"`
}

// ClientState tracks a client's request history
type ClientState struct {
	Requests      []time.Time
	Violations    int
	LastViolation time.Time
	BurstRequests []time.Time
}

// Stats holds security statistics.
type Stats struct {
	WhitelistEnabled bool  `json:"whitelist_enabled"`
	WhitelistCount   int   `json:"whitelist_count"`
	BlacklistEnabled bool  `json:"blacklist_enabled"`
	BlacklistCount   int   `json:"blacklist_count"`
	RateLimitEnabled bool  `json:"rate_limit_enabled"`
	ActiveClients    int   `json:"active_clients"`
	BannedIPs        int   `json:"banned_ips"`
	TotalBlocked     int64 `json:"total_blocked"`
	TotalRateLimited int64 `json:"total_rate_limited"`
}

// NewModule creates a new security module
func NewModule(cfg *config.Manager) *Module {
	return &Module{
		config:  cfg,
		log:     logger.New("security"),
		dataDir: cfg.Get().Directories.Data,
		whitelist: &IPList{
			Name:    "whitelist",
			Enabled: false,
			Entries: make([]IPEntry, 0),
		},
		blacklist: &IPList{
			Name:    "blacklist",
			Enabled: true,
			Entries: make([]IPEntry, 0),
		},
		rateLimiter: NewRateLimiter(RateLimitConfig{
			RequestsPerMinute: cfg.Get().Security.RateLimitRequests,
			BurstLimit:        cfg.Get().Security.BurstLimit,
			BurstWindow:       cfg.Get().Security.BurstWindow,
			BanDuration:       cfg.Get().Security.BanDuration,
			ViolationsForBan:  cfg.Get().Security.ViolationsForBan,
		}),
	}
}

// Name returns the module name
func (m *Module) Name() string {
	return "security"
}

// Start initializes the security module
func (m *Module) Start(_ context.Context) error {
	m.log.Info("Starting security module...")

	// Load IP lists
	if err := m.loadIPLists(); err != nil {
		m.log.Warn("Failed to load IP lists: %v", err)
	}

	// Start rate limiter cleanup (also cleans expired IP list entries)
	m.rateLimiter.StartCleanup(m.whitelist, m.blacklist)

	m.mu.Lock()
	m.healthy = true
	m.healthMsg = "Running"
	m.mu.Unlock()

	m.log.Info("Security module started (whitelist: %d entries, blacklist: %d entries)",
		len(m.whitelist.Entries), len(m.blacklist.Entries))
	return nil
}

// Stop gracefully stops the module
func (m *Module) Stop(_ context.Context) error {
	m.log.Info("Stopping security module...")

	// Save IP lists
	if err := m.saveIPLists(); err != nil {
		m.log.Error(errSaveIPListsFmt, err)
	}

	// Stop rate limiter cleanup
	m.rateLimiter.StopCleanup()

	m.mu.Lock()
	m.healthy = false
	m.healthMsg = "Stopped"
	m.mu.Unlock()

	return nil
}

// Health returns the module health status
func (m *Module) Health() models.HealthStatus {
	m.mu.RLock()
	healthy := m.healthy
	healthMsg := m.healthMsg
	m.mu.RUnlock()

	return models.HealthStatus{
		Name:      m.Name(),
		Status:    helpers.StatusString(healthy),
		Message:   healthMsg,
		CheckedAt: time.Now(),
	}
}

// CheckAccess validates if an IP is allowed to access
func (m *Module) CheckAccess(ip string) (allowed bool, reason string) {
	parsedIP := net.ParseIP(ip)
	if parsedIP == nil {
		// Log invalid IP attempts to detect reconnaissance and probing activity
		m.log.Warn("Access attempt with invalid IP address: %s", ip)
		return false, "Invalid IP address"
	}

	// Check blacklist first
	if m.blacklist.Enabled && m.blacklist.Contains(parsedIP) {
		return false, "IP is blacklisted"
	}

	// If whitelist is enabled, IP must be in it
	if m.whitelist.Enabled {
		if !m.whitelist.Contains(parsedIP) {
			return false, "IP not in whitelist"
		}
	}

	return true, ""
}

// CheckRateLimit checks if a request should be rate limited
func (m *Module) CheckRateLimit(ip string) (allowed bool, remaining int, resetAt time.Time) {
	return m.rateLimiter.CheckRequest(ip)
}

// IsBanned checks if an IP is currently banned
func (m *Module) IsBanned(ip string) bool {
	return m.rateLimiter.IsBanned(ip)
}

// BanIP manually bans an IP
func (m *Module) BanIP(ip string, duration time.Duration) {
	m.rateLimiter.BanIP(ip, duration)
}

// UnbanIP removes a ban on an IP
func (m *Module) UnbanIP(ip string) {
	m.rateLimiter.UnbanIP(ip)
}

// GetBannedIPs returns list of currently banned IPs
func (m *Module) GetBannedIPs() map[string]time.Time {
	return m.rateLimiter.GetBannedIPs()
}

// IPList methods

// Snapshot returns a safe copy of the Entries slice, safe to read outside the lock.
func (l *IPList) Snapshot() []IPEntry {
	l.mu.RLock()
	defer l.mu.RUnlock()
	out := make([]IPEntry, len(l.Entries))
	copy(out, l.Entries)
	return out
}

// Contains checks if an IP is in the list
func (l *IPList) Contains(ip net.IP) bool {
	l.mu.RLock()
	defer l.mu.RUnlock()

	for _, entry := range l.Entries {
		// Skip expired entries
		if entry.ExpiresAt != nil && time.Now().After(*entry.ExpiresAt) {
			continue
		}

		// Check CIDR range
		if entry.CIDR != nil {
			if entry.CIDR.Contains(ip) {
				return true
			}
			continue
		}

		// Check single IP
		if entry.IP != nil && entry.IP.Equal(ip) {
			return true
		}
	}

	return false
}

// Add adds an IP or CIDR to the list
func (l *IPList) Add(value, comment, addedBy string, expiresAt *time.Time) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	entry := IPEntry{
		Value:     value,
		Comment:   comment,
		AddedAt:   time.Now(),
		AddedBy:   addedBy,
		ExpiresAt: expiresAt,
	}

	// Try parsing as CIDR first
	if strings.Contains(value, "/") {
		_, cidr, err := net.ParseCIDR(value)
		if err != nil {
			return fmt.Errorf("invalid CIDR notation: %w", err)
		}
		entry.CIDR = cidr
	} else {
		// Parse as single IP
		ip := net.ParseIP(value)
		if ip == nil {
			return fmt.Errorf("invalid IP address: %s", value)
		}
		entry.IP = ip
	}

	// Check for duplicates
	for _, existing := range l.Entries {
		if existing.Value == value {
			return fmt.Errorf("entry already exists: %s", value)
		}
	}

	l.Entries = append(l.Entries, entry)
	return nil
}

// Remove removes an IP or CIDR from the list
func (l *IPList) Remove(value string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	for i, entry := range l.Entries {
		if entry.Value == value {
			l.Entries = append(l.Entries[:i], l.Entries[i+1:]...)
			return true
		}
	}
	return false
}

// Clear removes all entries from the list
func (l *IPList) Clear() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.Entries = make([]IPEntry, 0)
}

// CleanExpired removes expired entries
func (l *IPList) CleanExpired() int {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now()
	removed := 0
	newEntries := make([]IPEntry, 0, len(l.Entries))

	for _, entry := range l.Entries {
		if entry.ExpiresAt == nil || now.Before(*entry.ExpiresAt) {
			newEntries = append(newEntries, entry)
		} else {
			removed++
		}
	}

	l.Entries = newEntries
	return removed
}

// Module IP list management methods

// AddToWhitelist adds an IP to the whitelist
func (m *Module) AddToWhitelist(value, comment, addedBy string, expiresAt *time.Time) error {
	err := m.whitelist.Add(value, comment, addedBy, expiresAt)
	if err == nil {
		if saveErr := m.saveIPLists(); saveErr != nil {
			m.log.Warn(errSaveIPListsFmt, saveErr)
		}
		m.log.Info("Added to whitelist: %s by %s", value, addedBy)
	}
	return err
}

// RemoveFromWhitelist removes an IP from the whitelist
func (m *Module) RemoveFromWhitelist(value string) bool {
	removed := m.whitelist.Remove(value)
	if removed {
		if err := m.saveIPLists(); err != nil {
			m.log.Warn(errSaveIPListsFmt, err)
		}
		m.log.Info("Removed from whitelist: %s", value)
	}
	return removed
}

// AddToBlacklist adds an IP to the blacklist
func (m *Module) AddToBlacklist(value, comment, addedBy string, expiresAt *time.Time) error {
	err := m.blacklist.Add(value, comment, addedBy, expiresAt)
	if err == nil {
		if saveErr := m.saveIPLists(); saveErr != nil {
			m.log.Warn(errSaveIPListsFmt, saveErr)
		}
		m.log.Info("Added to blacklist: %s by %s", value, addedBy)
	}
	return err
}

// RemoveFromBlacklist removes an IP from the blacklist
func (m *Module) RemoveFromBlacklist(value string) bool {
	removed := m.blacklist.Remove(value)
	if removed {
		if err := m.saveIPLists(); err != nil {
			m.log.Warn(errSaveIPListsFmt, err)
		}
		m.log.Info("Removed from blacklist: %s", value)
	}
	return removed
}

// SetWhitelistEnabled enables or disables the whitelist
func (m *Module) SetWhitelistEnabled(enabled bool) {
	m.whitelist.mu.Lock()
	m.whitelist.Enabled = enabled
	m.whitelist.mu.Unlock()
	if err := m.saveIPLists(); err != nil {
		m.log.Warn(errSaveIPListsFmt, err)
	}
	m.log.Info("Whitelist enabled: %v", enabled)
}

// SetBlacklistEnabled enables or disables the blacklist
func (m *Module) SetBlacklistEnabled(enabled bool) {
	m.blacklist.mu.Lock()
	m.blacklist.Enabled = enabled
	m.blacklist.mu.Unlock()
	if err := m.saveIPLists(); err != nil {
		m.log.Warn(errSaveIPListsFmt, err)
	}
	m.log.Info("Blacklist enabled: %v", enabled)
}

// GetWhitelist returns the whitelist entries
func (m *Module) GetWhitelist() *IPList {
	return m.whitelist
}

// GetBlacklist returns the blacklist entries
func (m *Module) GetBlacklist() *IPList {
	return m.blacklist
}

// GetStats returns security statistics
func (m *Module) GetStats() Stats {
	m.whitelist.mu.RLock()
	whitelistCount := len(m.whitelist.Entries)
	whitelistEnabled := m.whitelist.Enabled
	m.whitelist.mu.RUnlock()

	m.blacklist.mu.RLock()
	blacklistCount := len(m.blacklist.Entries)
	blacklistEnabled := m.blacklist.Enabled
	m.blacklist.mu.RUnlock()

	m.rateLimiter.mu.RLock()
	activeClients := len(m.rateLimiter.clients)
	bannedIPs := len(m.rateLimiter.bannedIPs)
	m.rateLimiter.mu.RUnlock()

	m.mu.RLock()
	totalBlocked := m.totalBlocked
	totalRateLimited := m.totalRateLimited
	m.mu.RUnlock()

	return Stats{
		WhitelistEnabled: whitelistEnabled,
		WhitelistCount:   whitelistCount,
		BlacklistEnabled: blacklistEnabled,
		BlacklistCount:   blacklistCount,
		RateLimitEnabled: m.config.Get().Security.RateLimitEnabled,
		ActiveClients:    activeClients,
		BannedIPs:        bannedIPs,
		TotalBlocked:     totalBlocked,
		TotalRateLimited: totalRateLimited,
	}
}

// Persistence

func (m *Module) loadIPLists() error {
	var loadErr error

	// Load whitelist
	whitelistPath := filepath.Join(m.dataDir, "whitelist.json")
	if data, err := os.ReadFile(whitelistPath); err == nil {
		if err := json.Unmarshal(data, m.whitelist); err != nil {
			m.log.Warn("Failed to parse whitelist: %v", err)
			loadErr = fmt.Errorf("corrupt whitelist: %w", err)
		} else {
			for i := range m.whitelist.Entries {
				m.parseIPEntry(&m.whitelist.Entries[i])
			}
		}
	}

	// Load blacklist
	blacklistPath := filepath.Join(m.dataDir, "blacklist.json")
	if data, err := os.ReadFile(blacklistPath); err == nil {
		if err := json.Unmarshal(data, m.blacklist); err != nil {
			m.log.Warn("Failed to parse blacklist: %v", err)
			loadErr = fmt.Errorf("corrupt blacklist: %w", err)
		} else {
			for i := range m.blacklist.Entries {
				m.parseIPEntry(&m.blacklist.Entries[i])
			}
		}
	}

	return loadErr
}

func (m *Module) parseIPEntry(entry *IPEntry) {
	if strings.Contains(entry.Value, "/") {
		_, cidr, err := net.ParseCIDR(entry.Value)
		if err == nil {
			entry.CIDR = cidr
		}
	} else {
		entry.IP = net.ParseIP(entry.Value)
	}
}

// saveIPLists saves whitelist and blacklist to disk with secure permissions (0600)
func (m *Module) saveIPLists() error {
	// Save whitelist (hold lock during marshal)
	m.whitelist.mu.RLock()
	whitelistData, err := json.MarshalIndent(m.whitelist, "", "  ")
	m.whitelist.mu.RUnlock()
	if err == nil {
		whitelistPath := filepath.Join(m.dataDir, "whitelist.json")
		tempPath := whitelistPath + ".tmp"
		// Use 0600 permissions (read/write for owner only) for security-sensitive data
		if err := os.WriteFile(tempPath, whitelistData, 0600); err != nil {
			m.log.Error("Failed to save whitelist: %v", err)
			return fmt.Errorf("failed to save whitelist: %w", err)
		}
		if err := os.Rename(tempPath, whitelistPath); err != nil {
			m.log.Error("Failed to finalize whitelist save: %v", err)
			return fmt.Errorf("failed to finalize whitelist: %w", err)
		}
	}

	// Save blacklist (hold lock during marshal)
	m.blacklist.mu.RLock()
	blacklistData, err := json.MarshalIndent(m.blacklist, "", "  ")
	m.blacklist.mu.RUnlock()
	if err == nil {
		blacklistPath := filepath.Join(m.dataDir, "blacklist.json")
		tempPath := blacklistPath + ".tmp"
		// Use 0600 permissions (read/write for owner only) for security-sensitive data
		if err := os.WriteFile(tempPath, blacklistData, 0600); err != nil {
			m.log.Error("Failed to save blacklist: %v", err)
			return fmt.Errorf("failed to save blacklist: %w", err)
		}
		if err := os.Rename(tempPath, blacklistPath); err != nil {
			m.log.Error("Failed to finalize blacklist save: %v", err)
			return fmt.Errorf("failed to finalize blacklist: %w", err)
		}
	}

	return nil
}

// RateLimiter implementation

// NewRateLimiter creates a new rate limiter
func NewRateLimiter(config RateLimitConfig) *RateLimiter {
	return &RateLimiter{
		config:      config,
		clients:     make(map[string]*ClientState),
		bannedIPs:   make(map[string]time.Time),
		stopCleanup: make(chan struct{}),
	}
}

// StartCleanup starts the background cleanup goroutine, also cleaning expired IP list entries.
func (r *RateLimiter) StartCleanup(whitelist, blacklist *IPList) {
	r.cleanupTick = time.NewTicker(1 * time.Minute)
	go func() {
		for {
			select {
			case <-r.cleanupTick.C:
				r.cleanupWithIPLists(whitelist, blacklist)
			case <-r.stopCleanup:
				return
			}
		}
	}()
}

// StopCleanup stops the background cleanup
func (r *RateLimiter) StopCleanup() {
	if r.cleanupTick != nil {
		r.cleanupTick.Stop()
	}
	close(r.stopCleanup)
}

// CheckRequest checks if a request should be allowed
func (r *RateLimiter) CheckRequest(ip string) (allowed bool, remaining int, resetAt time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()

	now := time.Now()
	windowStart := now.Add(-1 * time.Minute)
	burstStart := now.Add(-r.config.BurstWindow)

	// Check if banned
	if banExpires, banned := r.bannedIPs[ip]; banned {
		if now.Before(banExpires) {
			return false, 0, banExpires
		}
		delete(r.bannedIPs, ip)
	}

	// Get or create client state
	client, exists := r.clients[ip]
	if !exists {
		client = &ClientState{
			Requests:      make([]time.Time, 0),
			BurstRequests: make([]time.Time, 0),
		}
		r.clients[ip] = client
	}

	// Clean old requests outside the window
	validRequests := make([]time.Time, 0)
	for _, t := range client.Requests {
		if t.After(windowStart) {
			validRequests = append(validRequests, t)
		}
	}
	client.Requests = validRequests

	// Clean old burst requests
	validBurst := make([]time.Time, 0)
	for _, t := range client.BurstRequests {
		if t.After(burstStart) {
			validBurst = append(validBurst, t)
		}
	}
	client.BurstRequests = validBurst

	// Check rate limit
	remaining = r.config.RequestsPerMinute - len(client.Requests)
	resetAt = now.Add(1 * time.Minute)

	if len(client.Requests) >= r.config.RequestsPerMinute {
		r.recordViolation(client, ip, now)
		return false, 0, resetAt
	}

	// Check burst limit
	if len(client.BurstRequests) >= r.config.BurstLimit {
		r.recordViolation(client, ip, now)
		return false, remaining, resetAt
	}

	// Record this request
	client.Requests = append(client.Requests, now)
	client.BurstRequests = append(client.BurstRequests, now)

	return true, remaining - 1, resetAt
}

func (r *RateLimiter) recordViolation(client *ClientState, ip string, now time.Time) {
	client.Violations++
	client.LastViolation = now

	// Ban if too many violations
	if client.Violations >= r.config.ViolationsForBan {
		r.bannedIPs[ip] = now.Add(r.config.BanDuration)
		client.Violations = 0 // Reset for after ban expires
	}
}

// IsBanned checks if an IP is currently banned
func (r *RateLimiter) IsBanned(ip string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if banExpires, banned := r.bannedIPs[ip]; banned {
		return time.Now().Before(banExpires)
	}
	return false
}

// BanIP manually bans an IP
func (r *RateLimiter) BanIP(ip string, duration time.Duration) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.bannedIPs[ip] = time.Now().Add(duration)
}

// UnbanIP removes a ban
func (r *RateLimiter) UnbanIP(ip string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.bannedIPs, ip)
}

// GetBannedIPs returns the list of banned IPs
func (r *RateLimiter) GetBannedIPs() map[string]time.Time {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make(map[string]time.Time)
	now := time.Now()
	for ip, expires := range r.bannedIPs {
		if now.Before(expires) {
			result[ip] = expires
		}
	}
	return result
}

func (r *RateLimiter) cleanup() {
	r.mu.Lock()
	defer r.mu.Unlock()

	now := time.Now()
	windowStart := now.Add(-2 * time.Minute)

	// Clean up old client states
	for ip, client := range r.clients {
		if len(client.Requests) == 0 && client.LastViolation.Before(windowStart) {
			delete(r.clients, ip)
		}
	}

	// Clean up expired bans
	for ip, expires := range r.bannedIPs {
		if now.After(expires) {
			delete(r.bannedIPs, ip)
		}
	}
}

// cleanupWithIPLists runs the standard cleanup plus expired entries from IP lists.
func (r *RateLimiter) cleanupWithIPLists(whitelist, blacklist *IPList) {
	r.cleanup()
	if whitelist != nil {
		whitelist.CleanExpired()
	}
	if blacklist != nil {
		blacklist.CleanExpired()
	}
}

// Middleware creates HTTP middleware for security checks
func (m *Module) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip := getClientIP(r)

		// Check IP access
		allowed, reason := m.CheckAccess(ip)
		if !allowed {
			m.log.Warn("Access denied for %s: %s", ip, reason)
			m.mu.Lock()
			m.totalBlocked++
			m.mu.Unlock()
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}

		// Skip rate limiting for static assets and streaming endpoints
		// These are high-frequency requests that should not count toward API rate limits
		path := r.URL.Path
		if strings.HasPrefix(path, "/static/") ||
			strings.HasPrefix(path, "/stream") ||
			strings.HasPrefix(path, "/hls/") ||
			strings.HasPrefix(path, "/download") ||
			strings.HasPrefix(path, "/thumbnail") ||
			strings.HasPrefix(path, "/media") ||
			path == "/health" ||
			path == "/metrics" {
			next.ServeHTTP(w, r)
			return
		}

		// Check if rate limiting is enabled via config
		cfg := m.config.Get()
		if !cfg.Security.RateLimitEnabled {
			next.ServeHTTP(w, r)
			return
		}

		// Check rate limit
		allowed, remaining, resetAt := m.CheckRateLimit(ip)

		// Set rate limit headers
		w.Header().Set("X-RateLimit-Limit", fmt.Sprintf("%d", m.rateLimiter.config.RequestsPerMinute))
		w.Header().Set("X-RateLimit-Remaining", fmt.Sprintf("%d", remaining))
		w.Header().Set("X-RateLimit-Reset", fmt.Sprintf("%d", resetAt.Unix()))

		if !allowed {
			m.log.Warn("Rate limit exceeded for %s", ip)
			m.mu.Lock()
			m.totalRateLimited++
			m.mu.Unlock()
			w.Header().Set("Retry-After", fmt.Sprintf("%d", int(time.Until(resetAt).Seconds())))
			http.Error(w, "Too Many Requests", http.StatusTooManyRequests)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// GinMiddleware returns a gin.HandlerFunc that applies the same security checks
// (IP access control and rate limiting) as the net/http Middleware method.
// Called from api/routes/routes.go when registering global middleware on the gin engine.
func (m *Module) GinMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		ip := getClientIP(c.Request)

		// Check IP access
		allowed, reason := m.CheckAccess(ip)
		if !allowed {
			m.log.Warn("Access denied for %s: %s", ip, reason)
			m.mu.Lock()
			m.totalBlocked++
			m.mu.Unlock()
			c.AbortWithStatus(http.StatusForbidden)
			return
		}

		// Skip rate limiting for static assets and streaming endpoints
		// These are high-frequency requests that should not count toward API rate limits
		path := c.Request.URL.Path
		if strings.HasPrefix(path, "/static/") ||
			strings.HasPrefix(path, "/stream") ||
			strings.HasPrefix(path, "/hls/") ||
			strings.HasPrefix(path, "/download") ||
			strings.HasPrefix(path, "/thumbnail") ||
			strings.HasPrefix(path, "/media") ||
			path == "/health" ||
			path == "/metrics" {
			c.Next()
			return
		}

		// Check if rate limiting is enabled via config
		cfg := m.config.Get()
		if !cfg.Security.RateLimitEnabled {
			c.Next()
			return
		}

		// Check rate limit
		allowed, remaining, resetAt := m.CheckRateLimit(ip)

		// Set rate limit headers
		c.Header("X-RateLimit-Limit", fmt.Sprintf("%d", m.rateLimiter.config.RequestsPerMinute))
		c.Header("X-RateLimit-Remaining", fmt.Sprintf("%d", remaining))
		c.Header("X-RateLimit-Reset", fmt.Sprintf("%d", resetAt.Unix()))

		if !allowed {
			m.log.Warn("Rate limit exceeded for %s", ip)
			m.mu.Lock()
			m.totalRateLimited++
			m.mu.Unlock()
			c.Header("Retry-After", fmt.Sprintf("%d", int(time.Until(resetAt).Seconds())))
			c.AbortWithStatus(http.StatusTooManyRequests)
			return
		}

		c.Next()
	}
}

// getClientIP extracts the real client IP, trusting X-Forwarded-For only from private network proxies.
// Uses pre-parsed privateCIDRs for performance. Validates the extracted IP to ensure it's well-formed.
func getClientIP(r *http.Request) string {
	remoteIP, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		remoteIP = r.RemoteAddr
	}

	// Only trust forwarded headers from private network ranges (reverse proxies)
	ip := net.ParseIP(remoteIP)
	trusted := false
	if ip != nil {
		for _, ipNet := range privateCIDRs {
			if ipNet.Contains(ip) {
				trusted = true
				break
			}
		}
	}

	if trusted {
		// Trust X-Forwarded-For header from proxy, but validate the IP
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			parts := strings.Split(xff, ",")
			if len(parts) > 0 {
				clientIP := strings.TrimSpace(parts[0])
				// Validate that the extracted IP is well-formed
				if parsedIP := net.ParseIP(clientIP); parsedIP != nil {
					return clientIP
				}
			}
		}
		// Fallback to X-Real-IP with validation
		if xri := r.Header.Get("X-Real-IP"); xri != "" {
			if parsedIP := net.ParseIP(xri); parsedIP != nil {
				return xri
			}
		}
	}

	return remoteIP
}
