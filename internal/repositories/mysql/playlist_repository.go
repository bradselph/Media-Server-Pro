// Package mysql provides MySQL/GORM implementations of repositories
package mysql

import (
	"context"
	"fmt"

	"media-server-pro/internal/repositories"
	"media-server-pro/pkg/models"

	"gorm.io/gorm"
)

// PlaylistRepository implements repositories.PlaylistRepository using GORM
type PlaylistRepository struct {
	db *gorm.DB
}

// NewPlaylistRepository creates a new GORM-based playlist repository
func NewPlaylistRepository(db *gorm.DB) repositories.PlaylistRepository {
	return &PlaylistRepository{db: db}
}

// Create stores a new playlist
func (r *PlaylistRepository) Create(ctx context.Context, playlist *models.Playlist) error {
	return r.db.WithContext(ctx).Create(playlist).Error
}

// TODO: Bug — Get does not translate gorm.ErrRecordNotFound into a domain error.
// When a playlist is not found, it returns a raw GORM error. The caller must know
// to check for gorm.ErrRecordNotFound, which leaks the persistence layer into the
// domain. Other repositories (e.g. UserRepository, SessionRepository) translate
// this into domain errors like repositories.ErrUserNotFound. Consider adding
// a repositories.ErrPlaylistNotFound sentinel error.

// Get retrieves a playlist by ID with its items
func (r *PlaylistRepository) Get(ctx context.Context, id string) (*models.Playlist, error) {
	var playlist models.Playlist
	err := r.db.WithContext(ctx).First(&playlist, "id = ?", id).Error
	if err != nil {
		return nil, err
	}

	// Load playlist items
	var items []models.PlaylistItem
	err = r.db.WithContext(ctx).
		Where("playlist_id = ?", id).
		Order("position ASC").
		Find(&items).Error
	if err != nil {
		return nil, err
	}

	playlist.Items = items
	return &playlist, nil
}

// Update updates an existing playlist
func (r *PlaylistRepository) Update(ctx context.Context, playlist *models.Playlist) error {
	return r.db.WithContext(ctx).Save(playlist).Error
}

// Delete removes a playlist and its items (cascade)
func (r *PlaylistRepository) Delete(ctx context.Context, id string) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Delete playlist items first
		if err := tx.Where("playlist_id = ?", id).Delete(&models.PlaylistItem{}).Error; err != nil {
			return err
		}
		// Delete playlist
		return tx.Delete(&models.Playlist{}, "id = ?", id).Error
	})
}

// ListByUser retrieves all playlists for a user
func (r *PlaylistRepository) ListByUser(ctx context.Context, userID string) ([]*models.Playlist, error) {
	var playlists []*models.Playlist
	err := r.db.WithContext(ctx).
		Where("user_id = ?", userID).
		Order("modified_at DESC").
		Find(&playlists).Error
	if err != nil {
		return nil, err
	}

	return r.batchLoadItems(ctx, playlists)
}

// ListAll retrieves all playlists with their items (used for cache population on startup)
func (r *PlaylistRepository) ListAll(ctx context.Context) ([]*models.Playlist, error) {
	var playlists []*models.Playlist
	err := r.db.WithContext(ctx).
		Order("modified_at DESC").
		Find(&playlists).Error
	if err != nil {
		return nil, err
	}

	return r.batchLoadItems(ctx, playlists)
}

// batchLoadItems loads items for all playlists in a single query (fixes N+1).
func (r *PlaylistRepository) batchLoadItems(ctx context.Context, playlists []*models.Playlist) ([]*models.Playlist, error) {
	if len(playlists) == 0 {
		return playlists, nil
	}

	// Collect playlist IDs
	ids := make([]string, len(playlists))
	playlistMap := make(map[string]*models.Playlist, len(playlists))
	for i, p := range playlists {
		ids[i] = p.ID
		p.Items = []models.PlaylistItem{} // initialize to empty slice
		playlistMap[p.ID] = p
	}

	// Single batch query for all items
	var allItems []models.PlaylistItem
	if err := r.db.WithContext(ctx).
		Where("playlist_id IN ?", ids).
		Order("position ASC").
		Find(&allItems).Error; err != nil {
		return nil, fmt.Errorf("failed to load playlist items: %w", err)
	}

	// Distribute items to their playlists
	for _, item := range allItems {
		if p, ok := playlistMap[item.PlaylistID]; ok {
			p.Items = append(p.Items, item)
		}
	}

	return playlists, nil
}

// AddItem adds an item to a playlist
func (r *PlaylistRepository) AddItem(ctx context.Context, item *models.PlaylistItem) error {
	return r.db.WithContext(ctx).Create(item).Error
}

// RemoveItem removes an item from a playlist by its ID
func (r *PlaylistRepository) RemoveItem(ctx context.Context, itemID string) error {
	return r.db.WithContext(ctx).Delete(&models.PlaylistItem{}, "id = ?", itemID).Error
}

// UpdateItem updates an existing playlist item (e.g. its position after a reorder)
func (r *PlaylistRepository) UpdateItem(ctx context.Context, item *models.PlaylistItem) error {
	return r.db.WithContext(ctx).Save(item).Error
}

// GetItems retrieves all items for a playlist
func (r *PlaylistRepository) GetItems(ctx context.Context, playlistID string) ([]*models.PlaylistItem, error) {
	var items []*models.PlaylistItem
	err := r.db.WithContext(ctx).
		Where("playlist_id = ?", playlistID).
		Order("position ASC").
		Find(&items).Error
	return items, err
}
