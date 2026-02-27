package handlers

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

// GetSecurityStats returns security module statistics
func (h *Handler) GetSecurityStats(c *gin.Context) {
	stats := h.security.GetStats()
	writeSuccess(c, map[string]interface{}{
		"banned_ips":         stats.BannedIPs,
		"whitelisted_ips":    stats.WhitelistCount,
		"blacklisted_ips":    stats.BlacklistCount,
		"active_rate_limits": stats.ActiveClients,
		"total_blocks_today": stats.TotalBlocked,
	})
}

// GetWhitelist returns the IP whitelist as a flat array.
func (h *Handler) GetWhitelist(c *gin.Context) {
	type ipEntry struct {
		IP        string     `json:"ip"`
		Comment   string     `json:"comment"`
		AddedBy   string     `json:"added_by"`
		AddedAt   time.Time  `json:"added_at"`
		ExpiresAt *time.Time `json:"expires_at,omitempty"`
	}
	raw := h.security.GetWhitelist().Snapshot()
	entries := make([]ipEntry, len(raw))
	for i, e := range raw {
		entries[i] = ipEntry{IP: e.Value, Comment: e.Comment, AddedBy: e.AddedBy, AddedAt: e.AddedAt, ExpiresAt: e.ExpiresAt}
	}
	writeSuccess(c, entries)
}

// AddToWhitelist adds an IP to the whitelist
func (h *Handler) AddToWhitelist(c *gin.Context) {
	var req struct {
		IP        string     `json:"ip"`
		Comment   string     `json:"comment"`
		ExpiresAt *time.Time `json:"expires_at,omitempty"`
	}
	if c.ShouldBindJSON(&req) != nil {
		writeError(c, http.StatusBadRequest, errInvalidRequest)
		return
	}

	session := getSession(c)
	addedBy := "admin"
	if session != nil {
		addedBy = session.Username
	}

	if err := h.security.AddToWhitelist(req.IP, req.Comment, addedBy, req.ExpiresAt); err != nil {
		writeError(c, http.StatusBadRequest, "Invalid IP address")
		return
	}

	writeSuccess(c, map[string]string{"message": "Added to whitelist"})
}

// RemoveFromWhitelist removes an IP from the whitelist
func (h *Handler) RemoveFromWhitelist(c *gin.Context) {
	var req struct {
		IP string `json:"ip"`
	}
	if c.ShouldBindJSON(&req) != nil {
		writeError(c, http.StatusBadRequest, errInvalidRequest)
		return
	}

	if !h.security.RemoveFromWhitelist(req.IP) {
		writeError(c, http.StatusNotFound, "IP not found in whitelist")
		return
	}

	writeSuccess(c, nil)
}

// GetBlacklist returns the IP blacklist as a flat array.
func (h *Handler) GetBlacklist(c *gin.Context) {
	type ipEntry struct {
		IP        string     `json:"ip"`
		Comment   string     `json:"comment"`
		AddedBy   string     `json:"added_by"`
		AddedAt   time.Time  `json:"added_at"`
		ExpiresAt *time.Time `json:"expires_at,omitempty"`
	}
	raw := h.security.GetBlacklist().Snapshot()
	entries := make([]ipEntry, len(raw))
	for i, e := range raw {
		entries[i] = ipEntry{IP: e.Value, Comment: e.Comment, AddedBy: e.AddedBy, AddedAt: e.AddedAt, ExpiresAt: e.ExpiresAt}
	}
	writeSuccess(c, entries)
}

// AddToBlacklist adds an IP to the blacklist
func (h *Handler) AddToBlacklist(c *gin.Context) {
	var req struct {
		IP        string     `json:"ip"`
		Comment   string     `json:"comment"`
		ExpiresAt *time.Time `json:"expires_at,omitempty"`
	}
	if c.ShouldBindJSON(&req) != nil {
		writeError(c, http.StatusBadRequest, errInvalidRequest)
		return
	}

	session := getSession(c)
	addedBy := "admin"
	if session != nil {
		addedBy = session.Username
	}

	if err := h.security.AddToBlacklist(req.IP, req.Comment, addedBy, req.ExpiresAt); err != nil {
		writeError(c, http.StatusBadRequest, "Invalid IP address")
		return
	}

	writeSuccess(c, map[string]string{"message": "Added to blacklist"})
}

// RemoveFromBlacklist removes an IP from the blacklist
func (h *Handler) RemoveFromBlacklist(c *gin.Context) {
	var req struct {
		IP string `json:"ip"`
	}
	if c.ShouldBindJSON(&req) != nil {
		writeError(c, http.StatusBadRequest, errInvalidRequest)
		return
	}

	if !h.security.RemoveFromBlacklist(req.IP) {
		writeError(c, http.StatusNotFound, "IP not found in blacklist")
		return
	}

	writeSuccess(c, nil)
}

// GetBannedIPs returns currently banned IPs as a typed array.
func (h *Handler) GetBannedIPs(c *gin.Context) {
	banned := h.security.GetBannedIPs()
	type bannedIP struct {
		IP        string     `json:"ip"`
		BannedAt  time.Time  `json:"banned_at"`
		ExpiresAt *time.Time `json:"expires_at,omitempty"`
		Reason    string     `json:"reason"`
	}
	now := time.Now()
	result := make([]bannedIP, 0, len(banned))
	for ip, expiresAt := range banned {
		entry := bannedIP{
			IP:       ip,
			BannedAt: now,
			Reason:   "Rate limit violation",
		}
		if !expiresAt.IsZero() {
			entry.ExpiresAt = &expiresAt
		}
		result = append(result, entry)
	}
	writeSuccess(c, result)
}

// BanIP manually bans an IP
func (h *Handler) BanIP(c *gin.Context) {
	var req struct {
		IP       string `json:"ip"`
		Duration int    `json:"duration_minutes"`
	}
	if c.ShouldBindJSON(&req) != nil {
		writeError(c, http.StatusBadRequest, errInvalidRequest)
		return
	}

	duration := time.Duration(req.Duration) * time.Minute
	if duration == 0 {
		duration = 15 * time.Minute
	}

	h.security.BanIP(req.IP, duration)
	writeSuccess(c, map[string]string{"message": "IP banned"})
}

// UnbanIP removes a ban on an IP
func (h *Handler) UnbanIP(c *gin.Context) {
	var req struct {
		IP string `json:"ip"`
	}
	if c.ShouldBindJSON(&req) != nil {
		writeError(c, http.StatusBadRequest, errInvalidRequest)
		return
	}

	h.security.UnbanIP(req.IP)
	writeSuccess(c, nil)
}

// ensure json import is used
var _ = json.Marshal
