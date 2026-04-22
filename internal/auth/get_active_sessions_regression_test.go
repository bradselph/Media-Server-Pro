package auth

import (
	"sync"
	"testing"
	"time"

	"media-server-pro/pkg/models"
)

// FND-0058: Regression test for GetActiveSessions.
// Before the fix, the code used `new(*session)` which is invalid Go syntax and prevented compilation.
// The fix uses a copy-and-address pattern:
//   tmp := *session
//   sessions = append(sessions, &tmp)
// This returns shallow copies (independent pointers) to session objects, preventing callers
// from mutating shared cache entries.

// TestFND0058_GetActiveSessions_ReturnsIndependentCopies verifies that each returned
// session is a new pointer to a copy of the original. Modifying the returned pointer
// must not affect the cache.
func TestFND0058_GetActiveSessions_ReturnsIndependentCopies(t *testing.T) {
	// Setup: create a Module with a session in cache
	m := &Module{
		sessions:   make(map[string]*models.Session),
		sessionsMu: sync.RWMutex{},
	}

	now := time.Now()
	originalSession := &models.Session{
		ID:           "sess-001",
		UserID:       "user-1",
		Username:     "alice",
		Role:         models.RoleViewer,
		CreatedAt:    now,
		ExpiresAt:    now.Add(1 * time.Hour), // Not expired
		LastActivity: now,
		IPAddress:    "192.168.1.1",
		UserAgent:    "Mozilla/5.0",
	}
	m.sessions["sess-001"] = originalSession

	// Test: get active sessions for "alice"
	active := m.GetActiveSessions("alice")

	if len(active) != 1 {
		t.Fatalf("expected 1 session, got %d", len(active))
	}

	returnedSession := active[0]

	// Verify the returned session has the same content as the original
	if returnedSession.ID != originalSession.ID {
		t.Errorf("returned session ID mismatch: %q vs %q", returnedSession.ID, originalSession.ID)
	}
	if returnedSession.Username != originalSession.Username {
		t.Errorf("returned session username mismatch: %q vs %q", returnedSession.Username, originalSession.Username)
	}

	// Verify the returned session is a different pointer (independent copy)
	if returnedSession == originalSession {
		t.Error("returned session should be a different pointer, not the same reference as cached session")
	}

	// Verify the returned session has different underlying struct memory (shallow copy)
	// by modifying the returned pointer and checking it doesn't affect the cache
	originalIPAddress := originalSession.IPAddress
	returnedSession.IPAddress = "10.0.0.1"

	// Re-fetch from cache to verify the cache entry was not mutated
	activeAgain := m.GetActiveSessions("alice")
	if len(activeAgain) != 1 {
		t.Fatalf("expected 1 session on second fetch, got %d", len(activeAgain))
	}

	if activeAgain[0].IPAddress != originalIPAddress {
		t.Errorf("cache was mutated: IPAddress changed from %q to %q",
			originalIPAddress, activeAgain[0].IPAddress)
	}
}

// TestFND0058_GetActiveSessions_FiltersExpiredSessions verifies that expired
// sessions are not returned.
func TestFND0058_GetActiveSessions_FiltersExpiredSessions(t *testing.T) {
	m := &Module{
		sessions:   make(map[string]*models.Session),
		sessionsMu: sync.RWMutex{},
	}

	now := time.Now()

	// Add an expired session
	expiredSession := &models.Session{
		ID:        "sess-expired",
		Username:  "alice",
		ExpiresAt: now.Add(-1 * time.Hour), // Expired
	}
	m.sessions["sess-expired"] = expiredSession

	// Add a valid (non-expired) session
	validSession := &models.Session{
		ID:        "sess-valid",
		Username:  "alice",
		ExpiresAt: now.Add(1 * time.Hour), // Valid
	}
	m.sessions["sess-valid"] = validSession

	// Test: get active sessions for "alice"
	active := m.GetActiveSessions("alice")

	if len(active) != 1 {
		t.Fatalf("expected 1 session (only valid), got %d", len(active))
	}

	if active[0].ID != "sess-valid" {
		t.Errorf("got expired session %q, expected valid session %q", active[0].ID, "sess-valid")
	}
}

// TestFND0058_GetActiveSessions_FiltersByUsername verifies that only sessions
// matching the requested username are returned.
func TestFND0058_GetActiveSessions_FiltersByUsername(t *testing.T) {
	m := &Module{
		sessions:   make(map[string]*models.Session),
		sessionsMu: sync.RWMutex{},
	}

	now := time.Now()

	// Add sessions for different users
	aliceSession := &models.Session{
		ID:        "sess-alice",
		Username:  "alice",
		ExpiresAt: now.Add(1 * time.Hour),
	}
	m.sessions["sess-alice"] = aliceSession

	bobSession := &models.Session{
		ID:        "sess-bob",
		Username:  "bob",
		ExpiresAt: now.Add(1 * time.Hour),
	}
	m.sessions["sess-bob"] = bobSession

	// Test: get active sessions for "alice" only
	active := m.GetActiveSessions("alice")

	if len(active) != 1 {
		t.Fatalf("expected 1 session for alice, got %d", len(active))
	}

	if active[0].ID != "sess-alice" {
		t.Errorf("got session for wrong user: %q", active[0].ID)
	}
}

// TestFND0058_GetActiveSessions_EmptyResult verifies that the function
// returns an empty slice (not nil) when no sessions match.
func TestFND0058_GetActiveSessions_EmptyResult(t *testing.T) {
	m := &Module{
		sessions:   make(map[string]*models.Session),
		sessionsMu: sync.RWMutex{},
	}

	// No sessions in cache
	active := m.GetActiveSessions("alice")

	if active == nil {
		t.Error("should return empty slice, not nil")
	}

	if len(active) != 0 {
		t.Errorf("expected empty slice, got %d sessions", len(active))
	}
}

// TestFND0058_GetActiveSessions_MultipleValidSessions verifies that multiple
// non-expired sessions for the same user are all returned as independent copies.
func TestFND0058_GetActiveSessions_MultipleValidSessions(t *testing.T) {
	m := &Module{
		sessions:   make(map[string]*models.Session),
		sessionsMu: sync.RWMutex{},
	}

	now := time.Now()

	// Add multiple sessions for alice
	for i := 0; i < 3; i++ {
		sess := &models.Session{
			ID:        sessID(i),
			Username:  "alice",
			ExpiresAt: now.Add(time.Duration(i+1) * time.Hour),
		}
		m.sessions[sess.ID] = sess
	}

	// Test: get all active sessions for alice
	active := m.GetActiveSessions("alice")

	if len(active) != 3 {
		t.Fatalf("expected 3 sessions, got %d", len(active))
	}

	// Verify all are independent pointers by modifying each
	for i, sess := range active {
		originalID := m.sessions[sess.ID].ID
		sess.ID = "modified-" + sess.ID

		// Re-fetch and verify cache was not mutated
		activeAgain := m.GetActiveSessions("alice")
		cacheSession := findSessionByID(activeAgain, originalID)
		if cacheSession == nil {
			t.Errorf("cache entry for session %d was lost after modifying returned copy", i)
		}
	}
}

// TestFND0058_GetActiveSessions_ThreadSafety verifies that concurrent reads
// from multiple goroutines do not race (the RWMutex prevents this).
func TestFND0058_GetActiveSessions_ThreadSafety(t *testing.T) {
	m := &Module{
		sessions:   make(map[string]*models.Session),
		sessionsMu: sync.RWMutex{},
	}

	now := time.Now()
	sess := &models.Session{
		ID:        "sess-001",
		Username:  "alice",
		ExpiresAt: now.Add(1 * time.Hour),
	}
	m.sessions["sess-001"] = sess

	// Spawn multiple goroutines calling GetActiveSessions concurrently
	numGoroutines := 10
	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func() {
			defer wg.Done()
			active := m.GetActiveSessions("alice")
			if len(active) != 1 {
				t.Errorf("expected 1 session, got %d", len(active))
			}
		}()
	}

	wg.Wait()
	// If we reach here without a race condition being detected, the test passes.
}

// Helper function to build a session ID for testing.
func sessID(index int) string {
	switch index {
	case 0:
		return "sess-001"
	case 1:
		return "sess-002"
	case 2:
		return "sess-003"
	default:
		return ""
	}
}

// Helper function to find a session by ID in a slice.
func findSessionByID(sessions []*models.Session, id string) *models.Session {
	for _, s := range sessions {
		if s.ID == id {
			return s
		}
	}
	return nil
}
