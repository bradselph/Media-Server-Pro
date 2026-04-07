// Watch history for users.
package auth

import (
	"context"

	"media-server-pro/pkg/models"
)

// AddToWatchHistory adds or updates an item in the user's watch history.
func (m *Module) AddToWatchHistory(ctx context.Context, username string, item models.WatchHistoryItem) error {
	m.usersMu.Lock()

	user, exists := m.users[username]
	if !exists {
		m.usersMu.Unlock()
		return ErrUserNotFound
	}

	for i, existing := range user.WatchHistory {
		if existing.MediaPath == item.MediaPath {
			oldItem := user.WatchHistory[i]
			user.WatchHistory[i] = item
			userCopy := *user
			userCopy.WatchHistory = make([]models.WatchHistoryItem, len(user.WatchHistory))
			copy(userCopy.WatchHistory, user.WatchHistory)
			m.usersMu.Unlock()
			if err := m.userRepo.Update(ctx, &userCopy); err != nil {
				m.log.Error("Failed to save user after watch history update: %v", err)
				m.usersMu.Lock()
				if u, ok := m.users[username]; ok && i < len(u.WatchHistory) {
					u.WatchHistory[i] = oldItem
				}
				m.usersMu.Unlock()
				return err
			}
			return nil
		}
	}

	oldHistory := make([]models.WatchHistoryItem, len(user.WatchHistory))
	copy(oldHistory, user.WatchHistory)

	user.WatchHistory = append([]models.WatchHistoryItem{item}, user.WatchHistory...)

	const maxHistory = 100
	if len(user.WatchHistory) > maxHistory {
		user.WatchHistory = user.WatchHistory[:maxHistory]
	}

	userCopy := *user
	userCopy.WatchHistory = make([]models.WatchHistoryItem, len(user.WatchHistory))
	copy(userCopy.WatchHistory, user.WatchHistory)
	m.usersMu.Unlock()

	if err := m.userRepo.Update(ctx, &userCopy); err != nil {
		m.log.Error("Failed to save user after watch history update: %v", err)
		m.usersMu.Lock()
		if u, ok := m.users[username]; ok {
			u.WatchHistory = oldHistory
		}
		m.usersMu.Unlock()
		return err
	}
	return nil
}

// ClearWatchHistory clears a user's watch history.
// Uses a copy-before-unlock pattern to prevent data races: the DB write happens
// after the lock is released, so we pass a snapshot copy rather than the live
// pointer. We also keep the old slice around so we can roll the cache back if
// the DB write fails, avoiding cache/DB divergence.
func (m *Module) ClearWatchHistory(ctx context.Context, username string) error {
	m.usersMu.Lock()

	user, exists := m.users[username]
	if !exists {
		m.usersMu.Unlock()
		return ErrUserNotFound
	}

	// Snapshot old history so we can roll back on DB failure.
	oldHistory := user.WatchHistory

	// Update cache optimistically.
	user.WatchHistory = make([]models.WatchHistoryItem, 0)

	// Build a copy to pass to the DB write (lock released before IO).
	userCopy := *user
	userCopy.WatchHistory = nil

	m.usersMu.Unlock()

	if err := m.userRepo.Update(ctx, &userCopy); err != nil {
		m.log.Error("Failed to save user after clearing watch history: %v", err)
		// Roll the cache back to avoid cache/DB divergence.
		m.usersMu.Lock()
		if u, ok := m.users[username]; ok {
			u.WatchHistory = oldHistory
		}
		m.usersMu.Unlock()
		return err
	}
	return nil
}

// RemoveWatchHistoryItem removes a single item from a user's watch history by media path.
// Uses a copy-before-unlock pattern (same rationale as ClearWatchHistory).
func (m *Module) RemoveWatchHistoryItem(ctx context.Context, username, mediaPath string) error {
	m.usersMu.Lock()

	user, exists := m.users[username]
	if !exists {
		m.usersMu.Unlock()
		return ErrUserNotFound
	}

	// Snapshot old history for rollback.
	oldHistory := user.WatchHistory

	updated := make([]models.WatchHistoryItem, 0, len(user.WatchHistory))
	for _, item := range user.WatchHistory {
		if item.MediaPath != mediaPath {
			updated = append(updated, item)
		}
	}

	// Update cache optimistically.
	user.WatchHistory = updated

	// Build a safe copy for the DB write.
	userCopy := *user
	userCopy.WatchHistory = make([]models.WatchHistoryItem, len(updated))
	copy(userCopy.WatchHistory, updated)

	m.usersMu.Unlock()

	if err := m.userRepo.Update(ctx, &userCopy); err != nil {
		m.log.Error("Failed to save user after removing watch history item: %v", err)
		// Roll back cache to avoid divergence.
		m.usersMu.Lock()
		if u, ok := m.users[username]; ok {
			u.WatchHistory = oldHistory
		}
		m.usersMu.Unlock()
		return err
	}
	return nil
}

// GetWatchHistory returns a copy of a user's watch history so callers cannot mutate internal state.
func (m *Module) GetWatchHistory(username string) ([]models.WatchHistoryItem, error) {
	m.usersMu.RLock()
	defer m.usersMu.RUnlock()

	user, exists := m.users[username]
	if !exists {
		return nil, ErrUserNotFound
	}
	if len(user.WatchHistory) == 0 {
		return nil, nil
	}
	out := make([]models.WatchHistoryItem, len(user.WatchHistory))
	copy(out, user.WatchHistory)
	return out, nil
}
