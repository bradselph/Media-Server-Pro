package analytics

import (
	"time"

	"media-server-pro/pkg/models"
)

type sessionData struct {
	ID               string
	UserID           string
	IPAddress        string
	UserAgent        string
	StartedAt        time.Time
	LastActivity     time.Time
	MediaViewed      map[string]time.Time
	MediaPositions   map[string]float64
	MediaCompletions map[string]struct{} // key: MediaID -> completed?
	EventCount       int
}

// maxAnalyticsSessions is the maximum number of in-memory analytics sessions.
// Entries beyond this cap are evicted LRU-style to prevent OOM from bot/scraper traffic.
const maxAnalyticsSessions = 10_000

// updateSession updates session tracking and returns the playback delta, whether it's a new media view,
// and whether this is the first time the media reached completion in this session.
func (m *Module) updateSession(event models.AnalyticsEvent) (delta float64, isNewMedia bool, isFirstCompletion bool) {
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
			ID:               event.SessionID,
			UserID:           event.UserID,
			IPAddress:        event.IPAddress,
			UserAgent:        event.UserAgent,
			StartedAt:        event.Timestamp,
			MediaViewed:      make(map[string]time.Time),
			MediaPositions:   make(map[string]float64),
			MediaCompletions: make(map[string]struct{}),
		}
		m.sessions[event.SessionID] = session
	}

	session.LastActivity = event.Timestamp
	session.EventCount++

	if event.MediaID != "" {
		_, seen := session.MediaViewed[event.MediaID]
		isNewMedia = !seen
		session.MediaViewed[event.MediaID] = event.Timestamp

		if event.Type == "playback" {
			if pos, ok := event.Data["position"].(float64); ok {
				prev := session.MediaPositions[event.MediaID]
				// Only count forward progress as watch time. Backward seeks reset
				// the baseline so forward re-watching is counted correctly.
				if pos > prev {
					delta = pos - prev
				}
				session.MediaPositions[event.MediaID] = pos
			}

			if progress, ok := event.Data["progress"].(float64); ok && progress >= 90 {
				if _, completed := session.MediaCompletions[event.MediaID]; !completed {
					isFirstCompletion = true
					session.MediaCompletions[event.MediaID] = struct{}{}
				}
			}
		}
	}
	return delta, isNewMedia, isFirstCompletion
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
	var toDelete []string
	for id, session := range m.sessions {
		if time.Since(session.LastActivity) > timeout*2 {
			toDelete = append(toDelete, id)
		}
	}
	for _, id := range toDelete {
		delete(m.sessions, id)
	}
}
