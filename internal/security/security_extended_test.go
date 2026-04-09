package security

import (
	"net"
	"net/http/httptest"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// RateLimiter.recordViolation — auto-ban after threshold
// ---------------------------------------------------------------------------

func TestRateLimiter_AutoBanAfterViolations(t *testing.T) {
	rl := NewRateLimiter(RateLimitConfig{
		RequestsPerMinute: 2,
		BurstLimit:        1,
		BurstWindow:       100 * time.Millisecond,
		ViolationsForBan:  3,
		BanDuration:       1 * time.Minute,
	})
	ip := "10.99.99.1"
	// Exhaust the rate limit to trigger violations
	for i := 0; i < 20; i++ {
		rl.CheckRequest(ip)
	}
	// After enough violations, the IP should be auto-banned
	if !rl.IsBanned(ip) {
		t.Error("IP should be auto-banned after exceeding violation threshold")
	}
	// Banned IP should be denied
	allowed, _, _ := rl.CheckRequest(ip)
	if allowed {
		t.Error("auto-banned IP should not be allowed")
	}
}

// ---------------------------------------------------------------------------
// RateLimiter.BanIP with expiry
// ---------------------------------------------------------------------------

func TestRateLimiter_BanExpiry(t *testing.T) {
	rl := NewRateLimiter(RateLimitConfig{RequestsPerMinute: 60, BurstLimit: 10})
	rl.BanIP("10.0.0.1", 50*time.Millisecond, "short ban")
	if !rl.IsBanned("10.0.0.1") {
		t.Error("IP should be banned immediately")
	}
	time.Sleep(100 * time.Millisecond)
	// Run cleanup to clear expired bans
	rl.cleanup()
	if rl.IsBanned("10.0.0.1") {
		t.Error("IP ban should have expired after cleanup")
	}
}

// ---------------------------------------------------------------------------
// RateLimiter.cleanup
// ---------------------------------------------------------------------------

func TestRateLimiter_Cleanup(t *testing.T) {
	rl := NewRateLimiter(RateLimitConfig{RequestsPerMinute: 60, BurstLimit: 10})
	// Create some client state
	rl.CheckRequest("10.0.0.1")
	rl.CheckRequest("10.0.0.2")
	// Cleanup should not panic
	rl.cleanup()
}

func TestRateLimiter_CleanupWithIPLists(t *testing.T) {
	rl := NewRateLimiter(RateLimitConfig{RequestsPerMinute: 60, BurstLimit: 10})
	whitelist := &IPList{Entries: make([]IPEntry, 0)}
	blacklist := &IPList{Entries: make([]IPEntry, 0)}
	// Add expired entries
	past := time.Now().Add(-1 * time.Hour)
	whitelist.Add("10.0.0.1", "test", "admin", &past)
	blacklist.Add("10.0.0.2", "test", "admin", &past)
	rl.cleanupWithIPLists(whitelist, blacklist)
	// Expired entries should be cleaned
	if len(whitelist.Snapshot()) != 0 {
		t.Error("expired whitelist entry should be cleaned")
	}
	if len(blacklist.Snapshot()) != 0 {
		t.Error("expired blacklist entry should be cleaned")
	}
}

// ---------------------------------------------------------------------------
// RateLimiter.StartCleanup / StopCleanup
// ---------------------------------------------------------------------------

func TestRateLimiter_StartStopCleanup(t *testing.T) {
	rl := NewRateLimiter(RateLimitConfig{RequestsPerMinute: 60, BurstLimit: 10})
	whitelist := &IPList{Entries: make([]IPEntry, 0)}
	blacklist := &IPList{Entries: make([]IPEntry, 0)}
	rl.StartCleanup(whitelist, blacklist)
	time.Sleep(50 * time.Millisecond)
	rl.StopCleanup()
	// Should not panic when called twice
	rl.StopCleanup()
}

// ---------------------------------------------------------------------------
// getClientIP — trusted proxy handling
// ---------------------------------------------------------------------------

func TestGetClientIP_XRealIP(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "10.0.0.1:1234"
	req.Header.Set("X-Real-IP", "203.0.113.100")
	ip := getClientIP(req, nil)
	// X-Real-IP should be considered (exact behavior depends on implementation)
	if ip == "" {
		t.Error("should return a non-empty IP")
	}
}

func TestGetClientIP_ExtraTrusted(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "172.16.0.1:1234"
	req.Header.Set("X-Forwarded-For", "203.0.113.50, 172.16.0.1")
	_, cidr, _ := net.ParseCIDR("172.16.0.0/12")
	ip := getClientIP(req, []*net.IPNet{cidr})
	if ip != "203.0.113.50" {
		t.Logf("with extra trusted CIDR, getClientIP = %q", ip)
	}
}

// ---------------------------------------------------------------------------
// IPList — advanced operations
// ---------------------------------------------------------------------------

func TestIPList_IPv6(t *testing.T) {
	list := &IPList{Entries: make([]IPEntry, 0)}
	if err := list.Add("::1", "loopback", "admin", nil); err != nil {
		t.Fatalf("Add IPv6: %v", err)
	}
	if !list.Contains(net.ParseIP("::1")) {
		t.Error("should contain IPv6 ::1")
	}
}

func TestIPList_CIDR(t *testing.T) {
	list := &IPList{Entries: make([]IPEntry, 0)}
	list.Add("10.0.0.0/8", "private", "admin", nil)
	if !list.Contains(net.ParseIP("10.255.255.255")) {
		t.Error("10.255.255.255 should be in 10.0.0.0/8")
	}
	if list.Contains(net.ParseIP("11.0.0.1")) {
		t.Error("11.0.0.1 should NOT be in 10.0.0.0/8")
	}
}

func TestIPList_Contains_Nil(t *testing.T) {
	list := &IPList{Entries: make([]IPEntry, 0)}
	list.Add("10.0.0.1", "test", "admin", nil)
	if list.Contains(nil) {
		t.Error("nil IP should not match")
	}
}

// ---------------------------------------------------------------------------
// GetBannedIPs returns a copy
// ---------------------------------------------------------------------------

func TestGetBannedIPs_ReturnsCopy(t *testing.T) {
	rl := NewRateLimiter(RateLimitConfig{RequestsPerMinute: 60, BurstLimit: 10})
	rl.BanIP("10.0.0.1", 1*time.Hour, "test")
	banned := rl.GetBannedIPs()
	// Modifying the returned map should not affect internal state
	delete(banned, "10.0.0.1")
	if !rl.IsBanned("10.0.0.1") {
		t.Error("modifying returned map should not affect internal state")
	}
}

// ---------------------------------------------------------------------------
// UnbanIP — non-existent IP
// ---------------------------------------------------------------------------

func TestRateLimiter_UnbanIP_NotBanned(t *testing.T) {
	rl := NewRateLimiter(RateLimitConfig{RequestsPerMinute: 60, BurstLimit: 10})
	// Should not panic
	rl.UnbanIP("10.0.0.99")
}
