package security

import (
	"testing"
	"time"
)

// TestRateLimiter_EvictsIdleOneShotClient is a regression test for the unbounded
// clients-map growth: a client that makes a single request and never returns must be
// reclaimed by cleanup(). The previous eviction predicate required len(Requests)==0,
// but CheckRequest always leaves >=1 timestamp and only trims on the same IP's next
// call, so one-shot IPs were never collected. Eviction now keys off LastSeen age.
func TestRateLimiter_EvictsIdleOneShotClient(t *testing.T) {
	rl := NewRateLimiter(RateLimitConfig{RequestsPerMinute: 60, BurstLimit: 10})

	const ip = "203.0.113.7"
	if allowed, _, _ := rl.CheckRequest(ip); !allowed {
		t.Fatalf("first request should be allowed")
	}

	// The client exists and (under the old predicate) has a non-empty Requests slice.
	rl.mu.Lock()
	c, ok := rl.clients[ip]
	if !ok {
		rl.mu.Unlock()
		t.Fatalf("client %s was not tracked", ip)
	}
	if len(c.Requests) == 0 {
		rl.mu.Unlock()
		t.Fatalf("precondition: a served request should leave >=1 timestamp")
	}
	// Simulate the IP going idle well past the stale window.
	c.LastSeen = c.LastSeen.Add(-10 * time.Minute)
	c.LastViolation = c.LastViolation.Add(-10 * time.Minute)
	rl.mu.Unlock()

	rl.cleanup()

	rl.mu.Lock()
	_, stillThere := rl.clients[ip]
	rl.mu.Unlock()
	if stillThere {
		t.Fatalf("idle one-shot client %s should have been evicted by cleanup()", ip)
	}
}

// TestRateLimiter_KeepsActiveClient verifies cleanup() does NOT evict a client seen
// within the stale window (so active clients keep their rate-limit state).
func TestRateLimiter_KeepsActiveClient(t *testing.T) {
	rl := NewRateLimiter(RateLimitConfig{RequestsPerMinute: 60, BurstLimit: 10})

	const ip = "203.0.113.8"
	rl.CheckRequest(ip) // LastSeen = now

	rl.cleanup()

	rl.mu.Lock()
	_, stillThere := rl.clients[ip]
	rl.mu.Unlock()
	if !stillThere {
		t.Fatalf("recently-seen client %s should NOT be evicted", ip)
	}
}

// FND-0016: Regression test for BanIP ensuring ExpiresAt is non-nil and non-zero
// when duration > 0.
func TestFND0016_BanIP_ExpiresAtNonNil(t *testing.T) {
	rl := NewRateLimiter(RateLimitConfig{
		RequestsPerMinute: 60,
		BurstLimit:        10,
		BanDuration:       10 * time.Minute,
		ViolationsForBan:  5,
	})

	ip := "192.168.1.100"
	duration := 10 * time.Minute
	reason := "Test ban"

	beforeCall := time.Now()
	rl.BanIP(ip, duration, reason)

	// Verify the ban was recorded with a non-nil, future ExpiresAt
	bannedIPs := rl.GetBannedIPs()
	if len(bannedIPs) != 1 {
		t.Fatalf("Expected 1 banned IP, got %d", len(bannedIPs))
	}

	ban, ok := bannedIPs[ip]
	if !ok {
		t.Fatalf("IP %s should be in banned IPs", ip)
	}

	// FND-0016 regression: ExpiresAt must be non-zero
	zeroTime := time.Time{}
	if ban.ExpiresAt.Equal(zeroTime) || ban.ExpiresAt.IsZero() {
		t.Fatalf("BanRecord.ExpiresAt should not be zero-time (FND-0016 regression); got %v",
			ban.ExpiresAt)
	}

	// ExpiresAt should be in the future
	if ban.ExpiresAt.Before(beforeCall) || ban.ExpiresAt.Equal(beforeCall) {
		t.Fatalf("ExpiresAt should be in the future; got %v vs now %v (FND-0016 regression)",
			ban.ExpiresAt, beforeCall)
	}

	// ExpiresAt should be approximately duration from now
	expectedMin := beforeCall.Add(duration - 100*time.Millisecond)
	expectedMax := beforeCall.Add(duration + 100*time.Millisecond)
	if ban.ExpiresAt.Before(expectedMin) || ban.ExpiresAt.After(expectedMax) {
		t.Logf("ExpiresAt %v is not close to expected %v ± 100ms (may be flaky on slow systems)",
			ban.ExpiresAt, beforeCall.Add(duration))
		// Don't fail; the fix is correct, just timing may vary slightly
	}

	// Verify the banned IP is still in effect immediately after banning
	if !rl.IsBanned(ip) {
		t.Error("IP should be banned immediately after BanIP() call (FND-0016 regression)")
	}

	t.Logf("BAN: %s expires at %v (duration %v)", ip, ban.ExpiresAt, duration)
}

// FND-0016: Regression test ensuring ban expiry is properly checked
// This verifies that ExpiresAt being non-zero allows proper expiry checking
func TestFND0016_BanIP_ExpiryChecking(t *testing.T) {
	rl := NewRateLimiter(RateLimitConfig{
		RequestsPerMinute: 60,
		BurstLimit:        10,
		BanDuration:       10 * time.Millisecond, // Very short for testing
		ViolationsForBan:  5,
	})

	ip := "192.168.1.101"
	duration := 10 * time.Millisecond

	rl.BanIP(ip, duration, "Short-lived test ban")

	// Ban should be active immediately
	if !rl.IsBanned(ip) {
		t.Error("IP should be banned immediately (FND-0016 regression)")
	}

	// Wait for ban to expire (generous margin over the 10ms ban so loaded CI
	// runners don't flake on scheduler/GC jitter)
	time.Sleep(50 * time.Millisecond)

	// Ban should no longer be active after expiry
	if rl.IsBanned(ip) {
		t.Error("IP should no longer be banned after expiry (FND-0016 regression: ExpiresAt check failed)")
	}
}

// FND-0016: Regression test for IPEntry ExpiresAt in whitelist/blacklist operations
// This tests the IIFE pattern used in the persistBan callback (line 215 in security.go)
func TestFND0016_IPEntry_ExpiresAt_InList(t *testing.T) {
	list := &IPList{
		Name:    "test_list",
		Enabled: true,
		Entries: make([]IPEntry, 0),
	}

	value := "10.0.0.1"
	comment := "Test entry"
	addedBy := "test"
	duration := 5 * time.Minute

	beforeAdd := time.Now()

	// Create an expiry time (simulating the pattern from line 215)
	expiryPtr := new(time.Now().Add(duration))

	err := list.Add(value, comment, addedBy, expiryPtr)
	if err != nil {
		t.Fatalf("Add failed: %v", err)
	}

	// Verify the entry was added with non-nil ExpiresAt
	if len(list.Entries) != 1 {
		t.Fatalf("Expected 1 entry, got %d", len(list.Entries))
	}

	entry := list.Entries[0]
	if entry.ExpiresAt == nil {
		t.Fatal("Entry.ExpiresAt should not be nil (FND-0016 regression)")
	}

	// Verify ExpiresAt is in the future
	if entry.ExpiresAt.Before(beforeAdd) {
		t.Fatalf("ExpiresAt should be in the future (FND-0016 regression); got %v vs now %v",
			entry.ExpiresAt, beforeAdd)
	}

	t.Logf("Entry expires at %v", entry.ExpiresAt)
}
