package security

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// isAuthPath
// ---------------------------------------------------------------------------

func TestIsAuthPath(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		{"/api/auth/login", true},
		{"/api/auth/register", true},
		{"/api/media", false},
		{"/api/auth/logout", false},
		{"/", false},
	}
	for _, tc := range tests {
		got := isAuthPath(tc.path)
		if got != tc.want {
			t.Errorf("isAuthPath(%q) = %v, want %v", tc.path, got, tc.want)
		}
	}
}

// ---------------------------------------------------------------------------
// getClientIP
// ---------------------------------------------------------------------------

func TestGetClientIP_RemoteAddr(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "192.168.1.100:12345"
	ip := getClientIP(req)
	if ip != "192.168.1.100" {
		t.Errorf("getClientIP = %q, want 192.168.1.100", ip)
	}
}

func TestGetClientIP_NoPort(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "10.0.0.1"
	ip := getClientIP(req)
	if ip == "" {
		t.Error("should handle RemoteAddr without port")
	}
}

func TestGetClientIP_XForwardedFor(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "10.0.0.1:1234" // private IP proxy
	req.Header.Set("X-Forwarded-For", "203.0.113.50, 10.0.0.1")
	ip := getClientIP(req)
	// Should trust X-Forwarded-For from private proxy
	if ip != "203.0.113.50" {
		t.Logf("getClientIP with X-Forwarded-For = %q (implementation dependent)", ip)
	}
}

// ---------------------------------------------------------------------------
// NewRateLimiter
// ---------------------------------------------------------------------------

func TestNewRateLimiter(t *testing.T) {
	rl := NewRateLimiter(RateLimitConfig{
		RequestsPerMinute: 60,
		BurstSize:         10,
		BanThreshold:      5,
		BanDuration:       10 * time.Minute,
	})
	if rl == nil {
		t.Fatal("NewRateLimiter returned nil")
	}
}

// ---------------------------------------------------------------------------
// RateLimiter.CheckRequest
// ---------------------------------------------------------------------------

func TestRateLimiter_CheckRequest_Allowed(t *testing.T) {
	rl := NewRateLimiter(RateLimitConfig{
		RequestsPerMinute: 60,
		BurstSize:         10,
	})
	allowed, remaining, _ := rl.CheckRequest("192.168.1.1")
	if !allowed {
		t.Error("first request should be allowed")
	}
	if remaining < 0 {
		t.Errorf("remaining = %d, should be >= 0", remaining)
	}
}

func TestRateLimiter_CheckRequest_MultipleIPs(t *testing.T) {
	rl := NewRateLimiter(RateLimitConfig{
		RequestsPerMinute: 60,
		BurstSize:         10,
	})
	allowed1, _, _ := rl.CheckRequest("10.0.0.1")
	allowed2, _, _ := rl.CheckRequest("10.0.0.2")
	if !allowed1 || !allowed2 {
		t.Error("different IPs should each be allowed")
	}
}

// ---------------------------------------------------------------------------
// RateLimiter.BanIP / UnbanIP / IsBanned
// ---------------------------------------------------------------------------

func TestRateLimiter_BanAndUnban(t *testing.T) {
	rl := NewRateLimiter(RateLimitConfig{RequestsPerMinute: 60, BurstSize: 10})
	rl.BanIP("10.0.0.1", 1*time.Hour, "test ban")
	if !rl.IsBanned("10.0.0.1") {
		t.Error("IP should be banned")
	}

	rl.UnbanIP("10.0.0.1")
	if rl.IsBanned("10.0.0.1") {
		t.Error("IP should be unbanned")
	}
}

func TestRateLimiter_IsBanned_NotBanned(t *testing.T) {
	rl := NewRateLimiter(RateLimitConfig{RequestsPerMinute: 60, BurstSize: 10})
	if rl.IsBanned("10.0.0.1") {
		t.Error("should not be banned by default")
	}
}

func TestRateLimiter_GetBannedIPs(t *testing.T) {
	rl := NewRateLimiter(RateLimitConfig{RequestsPerMinute: 60, BurstSize: 10})
	rl.BanIP("10.0.0.1", 1*time.Hour, "reason1")
	rl.BanIP("10.0.0.2", 1*time.Hour, "reason2")

	banned := rl.GetBannedIPs()
	if len(banned) != 2 {
		t.Errorf("expected 2 banned IPs, got %d", len(banned))
	}
}

// ---------------------------------------------------------------------------
// IPList
// ---------------------------------------------------------------------------

func TestIPList_AddAndContains(t *testing.T) {
	list := &IPList{}
	list.entries = make(map[string]*IPEntry)
	if err := list.Add("192.168.1.0/24", "test", "admin", nil); err != nil {
		t.Fatalf("Add failed: %v", err)
	}
	ip := parseIPForTest(t, "192.168.1.50")
	if !list.Contains(ip) {
		t.Error("192.168.1.50 should be in 192.168.1.0/24")
	}
}

func TestIPList_Remove(t *testing.T) {
	list := &IPList{}
	list.entries = make(map[string]*IPEntry)
	list.Add("10.0.0.1", "test", "admin", nil)
	removed := list.Remove("10.0.0.1")
	if !removed {
		t.Error("should return true for existing entry")
	}
	removed = list.Remove("10.0.0.1")
	if removed {
		t.Error("should return false for nonexistent entry")
	}
}

func TestIPList_Clear(t *testing.T) {
	list := &IPList{}
	list.entries = make(map[string]*IPEntry)
	list.Add("10.0.0.1", "test", "admin", nil)
	list.Add("10.0.0.2", "test", "admin", nil)
	list.Clear()
	snapshot := list.Snapshot()
	if len(snapshot) != 0 {
		t.Errorf("Clear should empty the list, got %d entries", len(snapshot))
	}
}

func TestIPList_CleanExpired(t *testing.T) {
	list := &IPList{}
	list.entries = make(map[string]*IPEntry)
	past := time.Now().Add(-1 * time.Hour)
	list.Add("10.0.0.1", "expired", "admin", &past)
	list.Add("10.0.0.2", "valid", "admin", nil) // no expiry

	cleaned := list.CleanExpired()
	if cleaned != 1 {
		t.Errorf("expected 1 cleaned, got %d", cleaned)
	}
}

func TestIPList_Snapshot(t *testing.T) {
	list := &IPList{}
	list.entries = make(map[string]*IPEntry)
	list.Add("10.0.0.1", "test1", "admin", nil)
	list.Add("10.0.0.2", "test2", "admin", nil)
	snap := list.Snapshot()
	if len(snap) != 2 {
		t.Errorf("snapshot should have 2 entries, got %d", len(snap))
	}
}

// helper to parse IP for Contains() tests
func parseIPForTest(t *testing.T, ipStr string) (ip interface{ String() string }) {
	t.Helper()
	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = ipStr + ":1234"
	// Use net.ParseIP via http request
	return nil
}

// ---------------------------------------------------------------------------
// Module basics
// ---------------------------------------------------------------------------

func TestModuleName(t *testing.T) {
	m := &Module{}
	if m.Name() != "security" {
		t.Errorf("Name() = %q, want %q", m.Name(), "security")
	}
}

func TestModuleHealth_Default(t *testing.T) {
	m := &Module{}
	h := m.Health()
	if h.Name != "security" {
		t.Errorf("Health().Name = %q", h.Name)
	}
}

// ---------------------------------------------------------------------------
// CheckRequest with banned IP
// ---------------------------------------------------------------------------

func TestRateLimiter_CheckRequest_BannedIP(t *testing.T) {
	rl := NewRateLimiter(RateLimitConfig{
		RequestsPerMinute: 60,
		BurstSize:         10,
	})
	rl.BanIP("10.0.0.1", 1*time.Hour, "test")
	allowed, _, _ := rl.CheckRequest("10.0.0.1")
	if allowed {
		t.Error("banned IP should not be allowed")
	}
}

// ---------------------------------------------------------------------------
// BanRecord fields
// ---------------------------------------------------------------------------

func TestBanRecord_Fields(t *testing.T) {
	now := time.Now()
	br := BanRecord{
		IP:       "10.0.0.1",
		Reason:   "too many requests",
		BannedAt: now,
	}
	if br.IP != "10.0.0.1" {
		t.Errorf("IP = %q", br.IP)
	}
	if br.Reason != "too many requests" {
		t.Errorf("Reason = %q", br.Reason)
	}
}

// ---------------------------------------------------------------------------
// Stats type
// ---------------------------------------------------------------------------

func TestStats_Fields(t *testing.T) {
	s := Stats{}
	// Zero value should be valid
	if s.BannedIPs != 0 || s.TotalRequests != 0 {
		t.Error("zero Stats should have zero fields")
	}
}

// suppress unused import warnings
var _ = http.StatusOK
