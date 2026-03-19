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
			user.WatchHistory[i] = item
			userCopy := *user
			userCopy.WatchHistory = make([]models.WatchHistoryItem, len(user.WatchHistory))
			copy(userCopy.WatchHistory, user.WatchHistory)
			m.usersMu.Unlock()
			if err := m.userRepo.Update(ctx, &userCopy); err != nil {
				m.log.Error("Failed to save user after watch history update: %v", err)
				return err
			}
			return nil
		}
	}

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
		return err
	}
	return nil
}

// ClearWatchHistory clears a user's watch history
func (m *Module) ClearWatchHistory(ctx context.Context, username string) error {
	m.usersMu.Lock()

	user, exists := m.users[username]
	if !exists {
		m.usersMu.Unlock()
		return ErrUserNotFound
	}

	user.WatchHistory = make([]models.WatchHistoryItem, 0)

	m.usersMu.Unlock()

	if err := m.userRepo.Update(ctx, user); err != nil {
		m.log.Error("Failed to save user after clearing watch history: %v", err)
		return err
	}
	return nil
}

// RemoveWatchHistoryItem removes a single item from a user's watch history by media path
func (m *Module) RemoveWatchHistoryItem(ctx context.Context, username, mediaPath string) error {
	m.usersMu.Lock()

	user, exists := m.users[username]
	if !exists {
		m.usersMu.Unlock()
		return ErrUserNotFound
	}

	updated := make([]models.WatchHistoryItem, 0, len(user.WatchHistory))
	for _, item := range user.WatchHistory {
		if item.MediaPath != mediaPath {
			updated = append(updated, item)
		}
	}
	user.WatchHistory = updated

	m.usersMu.Unlock()

	if err := m.userRepo.Update(ctx, user); err != nil {
		m.log.Error("Failed to save user after removing watch history item: %v", err)
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
