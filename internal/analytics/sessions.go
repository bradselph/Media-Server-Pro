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

// updateSession updates session tracking.
func (m *Module) updateSession(event models.AnalyticsEvent) {
	m.sessionsMu.Lock()
	defer m.sessionsMu.Unlock()

	session, exists := m.sessions[event.SessionID]
	if !exists {
		session = &sessionData{
			ID:          event.SessionID,
			UserID:      event.UserID,
			IPAddress:   event.IPAddress,
			UserAgent:   event.UserAgent,
			StartedAt:   time.Now(),
			MediaViewed: make(map[string]time.Time),
		}
		m.sessions[event.SessionID] = session
	}

	session.LastActivity = time.Now()
	session.EventCount++

	if event.MediaID != "" {
		session.MediaViewed[event.MediaID] = time.Now()
	}
}

// countActiveSessions counts sessions active within the configured timeout.
func (m *Module) countActiveSessions(timeout time.Duration) int {
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
