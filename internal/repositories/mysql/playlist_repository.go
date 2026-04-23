// Package mysql provides MySQL/GORM implementations of repositories
package mysql

import (
	"context"
	"errors"
	"fmt"

	"media-server-pro/internal/repositories"
	"media-server-pro/pkg/models"

	"gorm.io/gorm"
)

const (
	sqlPlaylistIDEq     = "playlist_id = ?"
	sqlOrderPositionAsc = "position ASC"
)

// PlaylistRepository implements repositories.PlaylistRepository using GORM
type PlaylistRepository struct {
	db *gorm.DB
}

// NewPlaylistRepository creates a new GORM-based playlist repository
func NewPlaylistRepository(db *gorm.DB) repositories.PlaylistRepository {
	if db == nil {
		panic("PlaylistRepository: db cannot be nil")
	}
	return &PlaylistRepository{db: db}
}

// Create stores a new playlist
func (r *PlaylistRepository) Create(ctx context.Context, playlist *models.Playlist) error {
	if playlist == nil {
		return fmt.Errorf("playlist cannot be nil")
	}
	return r.db.WithContext(ctx).Create(playlist).Error
}

// CreateWithItems creates a playlist and its items in a single transaction.
// On failure, no partial data is left (no orphaned playlist or items).
func (r *PlaylistRepository) CreateWithItems(ctx context.Context, playlist *models.Playlist, items []models.PlaylistItem) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(playlist).Error; err != nil {
			return err
		}
		for i := range items {
			if err := tx.Create(&items[i]).Error; err != nil {
				return fmt.Errorf("add item %d: %w", i, err)
			}
		}
		return nil
	})
}

// Get retrieves a playlist by ID with its items
func (r *PlaylistRepository) Get(ctx context.Context, id string) (*models.Playlist, error) {
	var playlist models.Playlist
	err := r.db.WithContext(ctx).First(&playlist, sqlIDEq, id).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, repositories.ErrPlaylistNotFound
		}
		return nil, err
	}

	// Load playlist items
	var items []models.PlaylistItem
	err = r.db.WithContext(ctx).
		Where(sqlPlaylistIDEq, id).
		Order(sqlOrderPositionAsc).
		Find(&items).Error
	if err != nil {
		return nil, err
	}

	playlist.Items = items
	return &playlist, nil
}

// Update updates playlist metadata only (name, description, is_public, cover_image, modified_at).
// Does not cascade to Items, avoiding overwriting concurrent item changes.
func (r *PlaylistRepository) Update(ctx context.Context, playlist *models.Playlist) error {
	if playlist == nil {
		return fmt.Errorf("playlist cannot be nil")
	}
	result := r.db.WithContext(ctx).Model(playlist).Where(sqlIDEq, playlist.ID).Updates(map[string]interface{}{
		"name":        playlist.Name,
		"description": playlist.Description,
		"user_id":     playlist.UserID,
		"modified_at": playlist.ModifiedAt,
		"is_public":   playlist.IsPublic,
		"cover_image": playlist.CoverImage,
	})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return repositories.ErrPlaylistNotFound
	}
	return nil
}

// Delete removes a playlist and its items (cascade).
// Returns repositories.ErrPlaylistNotFound if the playlist did not exist.
func (r *PlaylistRepository) Delete(ctx context.Context, id string) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Delete playlist items first
		if err := tx.Where(sqlPlaylistIDEq, id).Delete(&models.PlaylistItem{}).Error; err != nil {
			return err
		}
		result := tx.Delete(&models.Playlist{}, sqlIDEq, id)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return repositories.ErrPlaylistNotFound
		}
		return nil
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
		Order(sqlOrderPositionAsc).
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
	if item == nil {
		return fmt.Errorf("item cannot be nil")
	}
	return r.db.WithContext(ctx).Create(item).Error
}

// RemoveItem removes an item from a playlist by its ID.
// Returns ErrPlaylistNotFound when no row matches itemID.
func (r *PlaylistRepository) RemoveItem(ctx context.Context, itemID string) error {
	result := r.db.WithContext(ctx).Delete(&models.PlaylistItem{}, sqlIDEq, itemID)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return repositories.ErrPlaylistNotFound
	}
	return nil
}

// UpdateItem updates an existing playlist item (e.g. its position after a reorder).
// Uses targeted Updates() instead of Save() to avoid overwriting unrelated fields.
func (r *PlaylistRepository) UpdateItem(ctx context.Context, item *models.PlaylistItem) error {
	if item == nil {
		return fmt.Errorf("item cannot be nil")
	}
	result := r.db.WithContext(ctx).Model(item).Where(sqlIDEq, item.ID).Updates(map[string]interface{}{
		"playlist_id": item.PlaylistID,
		"media_id":    item.MediaID,
		"media_path":  item.MediaPath,
		"title":       item.Title,
		"position":    item.Position,
	})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return repositories.ErrPlaylistNotFound
	}
	return nil
}

// GetItems retrieves all items for a playlist
func (r *PlaylistRepository) GetItems(ctx context.Context, playlistID string) ([]*models.PlaylistItem, error) {
	var items []*models.PlaylistItem
	err := r.db.WithContext(ctx).
		Where(sqlPlaylistIDEq, playlistID).
		Order(sqlOrderPositionAsc).
		Find(&items).Error
	return items, err
}
