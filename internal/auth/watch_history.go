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
			m.usersMu.Unlock()
			if err := m.userRepo.Update(ctx, user); err != nil {
				m.log.Error("Failed to save user after watch history update: %v", err)
			}
			return nil
		}
	}

	user.WatchHistory = append([]models.WatchHistoryItem{item}, user.WatchHistory...)

	const maxHistory = 100
	if len(user.WatchHistory) > maxHistory {
		user.WatchHistory = user.WatchHistory[:maxHistory]
	}

	m.usersMu.Unlock()

	if err := m.userRepo.Update(ctx, user); err != nil {
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

	updated := user.WatchHistory[:0]
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

// GetWatchHistory returns a user's watch history
func (m *Module) GetWatchHistory(username string) ([]models.WatchHistoryItem, error) {
	m.usersMu.RLock()
	defer m.usersMu.RUnlock()

	user, exists := m.users[username]
	if !exists {
		return nil, ErrUserNotFound
	}
	return user.WatchHistory, nil
}
