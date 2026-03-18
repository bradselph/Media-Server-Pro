package handlers

import (
	"net"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"media-server-pro/internal/security"
)

// GetSecurityStats returns security module statistics
func (h *Handler) GetSecurityStats(c *gin.Context) {
	if !h.requireSecurity(c) {
		return
	}
	stats := h.security.GetStats()
	writeSuccess(c, map[string]interface{}{
		"banned_ips":         stats.BannedIPs,
		"whitelisted_ips":    stats.WhitelistCount,
		"blacklisted_ips":    stats.BlacklistCount,
		"active_rate_limits": stats.ActiveClients,
		"total_blocks_today": stats.TotalBlocked,
	})
}

// ipListEntryJSON is the JSON shape for a whitelist/blacklist entry.
type ipListEntryJSON struct {
	IP        string     `json:"ip"`
	Comment   string     `json:"comment"`
	AddedBy   string     `json:"added_by"`
	AddedAt   time.Time  `json:"added_at"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
}

func ipEntriesToJSON(raw []security.IPEntry) []ipListEntryJSON {
	entries := make([]ipListEntryJSON, len(raw))
	for i, e := range raw {
		entries[i] = ipListEntryJSON{IP: e.Value, Comment: e.Comment, AddedBy: e.AddedBy, AddedAt: e.AddedAt, ExpiresAt: e.ExpiresAt}
	}
	return entries
}

// getIPList returns an IP list (whitelist or blacklist) as JSON. getSnapshot returns the list snapshot.
func (h *Handler) getIPList(c *gin.Context, getSnapshot func() []security.IPEntry) {
	if !h.requireSecurity(c) {
		return
	}
	raw := getSnapshot()
	writeSuccess(c, ipEntriesToJSON(raw))
}

// GetWhitelist returns the IP whitelist as a flat array.
func (h *Handler) GetWhitelist(c *gin.Context) {
	h.getIPList(c, func() []security.IPEntry { return h.security.GetWhitelist().Snapshot() })
}

type addIPListReq struct {
	IP        string     `json:"ip"`
	Comment   string     `json:"comment"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
}

func (h *Handler) addToIPList(c *gin.Context, addFn func(ip, comment, addedBy string, expiresAt *time.Time) error, successMsg string) {
	if !h.requireSecurity(c) {
		return
	}
	var req addIPListReq
	if c.ShouldBindJSON(&req) != nil {
		writeError(c, http.StatusBadRequest, errInvalidRequest)
		return
	}
	session := getSession(c)
	addedBy := "admin"
	if session != nil {
		addedBy = session.Username
	}
	if err := addFn(req.IP, req.Comment, addedBy, req.ExpiresAt); err != nil {
		writeError(c, http.StatusBadRequest, "Invalid IP address")
		return
	}
	writeSuccess(c, map[string]string{"message": successMsg})
}

// AddToWhitelist adds an IP to the whitelist
func (h *Handler) AddToWhitelist(c *gin.Context) {
	h.addToIPList(c, h.security.AddToWhitelist, "Added to whitelist")
}

// AddToBlacklist adds an IP to the blacklist
func (h *Handler) AddToBlacklist(c *gin.Context) {
	h.addToIPList(c, h.security.AddToBlacklist, "Added to blacklist")
}

// removeFromIPList binds the JSON body (ip), calls removeFn(ip), and responds with 404 and notFoundMsg if not found.
func (h *Handler) removeFromIPList(c *gin.Context, removeFn func(string) bool, notFoundMsg string) {
	if !h.requireSecurity(c) {
		return
	}
	var req struct {
		IP string `json:"ip"`
	}
	if c.ShouldBindJSON(&req) != nil {
		writeError(c, http.StatusBadRequest, errInvalidRequest)
		return
	}
	if !removeFn(req.IP) {
		writeError(c, http.StatusNotFound, notFoundMsg)
		return
	}
	writeSuccess(c, nil)
}

// RemoveFromWhitelist removes an IP from the whitelist
func (h *Handler) RemoveFromWhitelist(c *gin.Context) {
	h.removeFromIPList(c, h.security.RemoveFromWhitelist, "IP not found in whitelist")
}

// GetBlacklist returns the IP blacklist as a flat array.
func (h *Handler) GetBlacklist(c *gin.Context) {
	h.getIPList(c, func() []security.IPEntry { return h.security.GetBlacklist().Snapshot() })
}

// RemoveFromBlacklist removes an IP from the blacklist
func (h *Handler) RemoveFromBlacklist(c *gin.Context) {
	h.removeFromIPList(c, h.security.RemoveFromBlacklist, "IP not found in blacklist")
}

// GetBannedIPs returns currently banned IPs as a typed array.
func (h *Handler) GetBannedIPs(c *gin.Context) {
	if !h.requireSecurity(c) {
		return
	}
	banned := h.security.GetBannedIPs()
	type bannedIP struct {
		IP        string     `json:"ip"`
		BannedAt  time.Time  `json:"banned_at"`
		ExpiresAt *time.Time `json:"expires_at,omitempty"`
		Reason    string     `json:"reason"`
	}
	result := make([]bannedIP, 0, len(banned))
	for ip, rec := range banned {
		entry := bannedIP{
			IP:       ip,
			BannedAt: rec.BannedAt,
			Reason:   rec.Reason,
		}
		if !rec.ExpiresAt.IsZero() {
			entry.ExpiresAt = new(rec.ExpiresAt)
		}
		result = append(result, entry)
	}
	writeSuccess(c, result)
}

// BanIP manually bans an IP. Validates IPv4/IPv6 and rejects banning the caller's IP.
func (h *Handler) BanIP(c *gin.Context) {
	if !h.requireSecurity(c) {
		return
	}
	var req struct {
		IP       string `json:"ip"`
		Duration int    `json:"duration_minutes"`
		Reason   string `json:"reason"`
	}
	if c.ShouldBindJSON(&req) != nil {
		writeError(c, http.StatusBadRequest, errInvalidRequest)
		return
	}

	if req.IP == "" {
		writeError(c, http.StatusBadRequest, "IP address is required")
		return
	}
	if net.ParseIP(req.IP) == nil {
		writeError(c, http.StatusBadRequest, "Invalid IP address format")
		return
	}
	if req.IP == c.ClientIP() {
		writeError(c, http.StatusBadRequest, "Cannot ban your own IP address")
		return
	}

	duration := time.Duration(req.Duration) * time.Minute
	if duration <= 0 {
		duration = 15 * time.Minute
	}

	reason := req.Reason
	if reason == "" {
		reason = "Manual ban"
	}

	h.security.BanIP(req.IP, duration, reason)
	writeSuccess(c, map[string]string{"message": "IP banned"})
}

// UnbanIP removes a ban on an IP
func (h *Handler) UnbanIP(c *gin.Context) {
	if !h.requireSecurity(c) {
		return
	}
	var req struct {
		IP string `json:"ip"`
	}
	if c.ShouldBindJSON(&req) != nil {
		writeError(c, http.StatusBadRequest, errInvalidRequest)
		return
	}

	if req.IP == "" {
		writeError(c, http.StatusBadRequest, "IP address is required")
		return
	}

	h.security.UnbanIP(req.IP)
	writeSuccess(c, nil)
}
