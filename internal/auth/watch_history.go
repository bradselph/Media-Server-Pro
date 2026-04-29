// Watch history for users.

package auth

import (
	"context"
	"fmt"

	"media-server-pro/pkg/models"
)

// AddToWatchHistory adds or updates an item in the user's watch history.
// The lock is held through the DB write to prevent two concurrent callers from
// committing out-of-order: without this, caller A could update the cache first
// (correct ordering) but have its DB write arrive after caller B's, leaving the
// DB with a stale snapshot while the cache holds the newer one.
func (m *Module) AddToWatchHistory(ctx context.Context, username string, item models.WatchHistoryItem) error {
	if item.MediaPath == "" {
		return fmt.Errorf("media path is required")
	}

	m.usersMu.Lock()
	defer m.usersMu.Unlock()

	user, exists := m.users[username]
	if !exists {
		return ErrUserNotFound
	}

	for i, existing := range user.WatchHistory {
		if existing.MediaPath != item.MediaPath {
			continue
		}
		user.WatchHistory[i] = item
		userCopy := *user
		userCopy.WatchHistory = make([]models.WatchHistoryItem, len(user.WatchHistory))
		copy(userCopy.WatchHistory, user.WatchHistory)
		if err := m.userRepo.Update(ctx, &userCopy); err != nil {
			m.log.Error("Failed to save user after watch history update: %v", err)
			user.WatchHistory[i] = existing
			return err
		}
		return nil
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

	if err := m.userRepo.Update(ctx, &userCopy); err != nil {
		m.log.Error("Failed to save user after watch history update: %v", err)
		user.WatchHistory = oldHistory
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
	defer m.usersMu.Unlock()

	user, exists := m.users[username]
	if !exists {
		return ErrUserNotFound
	}

	oldHistory := append([]models.WatchHistoryItem(nil), user.WatchHistory...)

	user.WatchHistory = make([]models.WatchHistoryItem, 0)

	userCopy := *user
	userCopy.WatchHistory = nil

	if err := m.userRepo.Update(ctx, &userCopy); err != nil {
		m.log.Error("Failed to save user after clearing watch history: %v", err)
		user.WatchHistory = oldHistory
		return err
	}
	return nil
}

// RemoveWatchHistoryItem removes a single item from a user's watch history by media path.
// The lock is held through the DB write for the same ordering-race reason as AddToWatchHistory.
func (m *Module) RemoveWatchHistoryItem(ctx context.Context, username, mediaPath string) error {
	m.usersMu.Lock()
	defer m.usersMu.Unlock()

	user, exists := m.users[username]
	if !exists {
		return ErrUserNotFound
	}

	oldHistory := append([]models.WatchHistoryItem(nil), user.WatchHistory...)

	updated := make([]models.WatchHistoryItem, 0, len(user.WatchHistory))
	for _, item := range user.WatchHistory {
		if item.MediaPath != mediaPath {
			updated = append(updated, item)
		}
	}

	user.WatchHistory = updated

	userCopy := *user
	userCopy.WatchHistory = make([]models.WatchHistoryItem, len(updated))
	copy(userCopy.WatchHistory, updated)

	if err := m.userRepo.Update(ctx, &userCopy); err != nil {
		m.log.Error("Failed to save user after removing watch history item: %v", err)
		user.WatchHistory = oldHistory
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
