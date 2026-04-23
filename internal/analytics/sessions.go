package analytics

import (
	"time"

	"media-server-pro/pkg/models"
)

type sessionData struct {
	ID           string
	UserID       string
	IPAddress    string
	UserAgent    string
	StartedAt    time.Time
	LastActivity time.Time
	MediaViewed  map[string]time.Time
	EventCount   int
}

// maxAnalyticsSessions is the maximum number of in-memory analytics sessions.
// Entries beyond this cap are evicted LRU-style to prevent OOM from bot/scraper traffic.
const maxAnalyticsSessions = 10_000

// updateSession updates session tracking.
func (m *Module) updateSession(event models.AnalyticsEvent) {
	m.sessionsMu.Lock()
	defer m.sessionsMu.Unlock()

	session, exists := m.sessions[event.SessionID]
	if !exists {
		// Enforce cap: evict the least-recently-active entry before adding a new one.
		if len(m.sessions) >= maxAnalyticsSessions {
			var oldestID string
			var oldestTime time.Time
			for id, s := range m.sessions {
				if oldestID == "" || s.LastActivity.Before(oldestTime) {
					oldestID = id
					oldestTime = s.LastActivity
				}
			}
			delete(m.sessions, oldestID)
		}
		session = &sessionData{
			ID:          event.SessionID,
			UserID:      event.UserID,
			IPAddress:   event.IPAddress,
			UserAgent:   event.UserAgent,
			StartedAt:   event.Timestamp,
			MediaViewed: make(map[string]time.Time),
		}
		m.sessions[event.SessionID] = session
	}

	session.LastActivity = event.Timestamp
	session.EventCount++

	if event.MediaID != "" {
		session.MediaViewed[event.MediaID] = event.Timestamp
	}
}

// countActiveSessions counts sessions active within the configured timeout.
func (m *Module) countActiveSessions(timeout time.Duration) int {
	m.sessionsMu.RLock()
	defer m.sessionsMu.RUnlock()
	active := 0
	for _, session := range m.sessions {
		if time.Since(session.LastActivity) < timeout {
			active++
		}
	}
	return active
}

// cleanupStaleSessions removes sessions inactive for more than timeout*2.
func (m *Module) cleanupStaleSessions(timeout time.Duration) {
	m.sessionsMu.Lock()
	defer m.sessionsMu.Unlock()
	for id, session := range m.sessions {
		if time.Since(session.LastActivity) > timeout*2 {
			delete(m.sessions, id)
		}
	}
}
