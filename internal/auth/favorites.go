package auth

import (
	"context"
	"errors"
	"time"

	"media-server-pro/internal/repositories"
)

var errInvalidFavoriteParams = errors.New("userID and mediaID are required")

// AddFavorite adds a media item to the user's favorites. Idempotent — safe to call if already favorited.
func (m *Module) AddFavorite(ctx context.Context, userID, mediaID, mediaPath string) error {
	if userID == "" || mediaID == "" {
		return errInvalidFavoriteParams
	}
	rec := &repositories.FavoriteRecord{
		ID:        generateID(),
		UserID:    userID,
		MediaID:   mediaID,
		MediaPath: mediaPath,
		AddedAt:   time.Now(),
	}
	return m.favoriteRepo.Add(ctx, rec)
}

// RemoveFavorite removes a media item from the user's favorites.
func (m *Module) RemoveFavorite(ctx context.Context, userID, mediaID string) error {
	if userID == "" || mediaID == "" {
		return errInvalidFavoriteParams
	}
	return m.favoriteRepo.Remove(ctx, userID, mediaID)
}

// GetFavorites returns all favorite records for a user.
func (m *Module) GetFavorites(ctx context.Context, userID string) ([]*repositories.FavoriteRecord, error) {
	if userID == "" {
		return nil, errInvalidFavoriteParams
	}
	return m.favoriteRepo.List(ctx, userID)
}

// IsFavorite returns true if the user has favorited the given media item.
func (m *Module) IsFavorite(ctx context.Context, userID, mediaID string) (bool, error) {
	if userID == "" || mediaID == "" {
		return false, errInvalidFavoriteParams
	}
	return m.favoriteRepo.Exists(ctx, userID, mediaID)
}
