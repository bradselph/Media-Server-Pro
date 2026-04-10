// Package playlist provides playlist management functionality.
// It handles CRUD operations for user playlists.
package playlist

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"media-server-pro/internal/config"
	"media-server-pro/internal/database"
	"media-server-pro/internal/logger"
	"media-server-pro/internal/repositories"
	"media-server-pro/internal/repositories/mysql"
	"media-server-pro/pkg/helpers"
	"media-server-pro/pkg/models"
)

var (
	ErrPlaylistNotFound = errors.New("playlist not found")
	ErrItemNotFound     = errors.New("item not found in playlist")
	ErrAccessDenied     = errors.New("access denied")
)

// PlaylistID is the unique identifier for a playlist (avoids primitive obsession for ID parameters).
type PlaylistID string

// UserID is the unique identifier for a user in playlist operations (avoids primitive obsession).
type UserID string

// Module implements playlist management
type Module struct {
	config       *config.Manager
	log          *logger.Logger
	dbModule     *database.Module
	playlistRepo repositories.PlaylistRepository
	playlists    map[PlaylistID]*models.Playlist // Write-through cache for performance
	mu           sync.RWMutex
	healthy      bool
	healthMsg    string
	healthMu     sync.RWMutex
}

// NewModule creates a new playlist module.
// The database module is stored but repositories are created during Start().
func NewModule(cfg *config.Manager, dbModule *database.Module) (*Module, error) {
	if dbModule == nil {
		return nil, fmt.Errorf("database module is required for playlists")
	}

	return &Module{
		config:    cfg,
		log:       logger.New("playlist"),
		dbModule:  dbModule,
		playlists: make(map[PlaylistID]*models.Playlist),
	}, nil
}

// Name returns the module name
func (m *Module) Name() string {
	return "playlist"
}

// Start initializes the playlist module
func (m *Module) Start(_ context.Context) error {
	m.log.Info("Starting playlist module...")

	// Initialize MySQL repository (database is now connected)
	if !m.dbModule.IsConnected() {
		return fmt.Errorf("database is not connected")
	}
	m.log.Info("Using MySQL repository for playlists")
	m.playlistRepo = mysql.NewPlaylistRepository(m.dbModule.GORM())

	// Populate in-memory cache from MySQL
	if err := m.loadPlaylists(); err != nil {
		m.log.Warn("Failed to load playlists from database: %v", err)
	}

	m.healthMu.Lock()
	m.healthy = true
	m.healthMsg = fmt.Sprintf("Running (%d playlists)", len(m.playlists))
	m.healthMu.Unlock()
	m.log.Info("Playlist module started with %d playlists", len(m.playlists))
	return nil
}

// Stop gracefully stops the module
func (m *Module) Stop(_ context.Context) error {
	m.log.Info("Stopping playlist module...")

	m.healthMu.Lock()
	m.healthy = false
	m.healthMsg = "Stopped"
	m.healthMu.Unlock()
	return nil
}

// Health returns the module health status
func (m *Module) Health() models.HealthStatus {
	m.healthMu.RLock()
	defer m.healthMu.RUnlock()
	return models.HealthStatus{
		Name:      m.Name(),
		Status:    helpers.StatusString(m.healthy),
		Message:   m.healthMsg,
		CheckedAt: time.Now(),
	}
}

// CreatePlaylistInput holds parameters for creating a playlist.
type CreatePlaylistInput struct {
	Name        string
	Description string
	UserID      UserID
	IsPublic    bool
}

// CreatePlaylist creates a new playlist
func (m *Module) CreatePlaylist(ctx context.Context, input CreatePlaylistInput) (*models.Playlist, error) {
	playlist := &models.Playlist{
		ID:          uuid.New().String(),
		Name:        input.Name,
		Description: input.Description,
		UserID:      string(input.UserID),
		Items:       make([]models.PlaylistItem, 0),
		CreatedAt:   time.Now(),
		ModifiedAt:  time.Now(),
		IsPublic:    input.IsPublic,
	}

	if err := m.playlistRepo.Create(ctx, playlist); err != nil {
		return nil, fmt.Errorf("failed to create playlist: %w", err)
	}

	m.mu.Lock()
	m.playlists[PlaylistID(playlist.ID)] = playlist
	result := copyPlaylist(playlist)
	m.mu.Unlock()

	m.log.Info("Created playlist: %s (user: %s)", input.Name, string(input.UserID))

	return result, nil
}

// copyPlaylist returns a deep copy of a playlist, including a copy of its Items slice.
func copyPlaylist(p *models.Playlist) *models.Playlist {
	result := *p
	result.Items = make([]models.PlaylistItem, len(p.Items))
	copy(result.Items, p.Items)
	return &result
}

// getPlaylistForUserLocked returns the playlist if it exists and belongs to userID. Caller must hold m.mu.
func (m *Module) getPlaylistForUserLocked(playlistID PlaylistID, userID UserID) (*models.Playlist, error) {
	playlist, exists := m.playlists[playlistID]
	if !exists {
		return nil, ErrPlaylistNotFound
	}
	if playlist.UserID != string(userID) {
		return nil, ErrAccessDenied
	}
	return playlist, nil
}

// GetPlaylist returns a playlist by ID. The returned playlist is a copy that is
// safe to read without holding any lock.
func (m *Module) GetPlaylist(id PlaylistID) (*models.Playlist, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	playlist, exists := m.playlists[id]
	if !exists {
		return nil, ErrPlaylistNotFound
	}
	return copyPlaylist(playlist), nil
}

// GetPlaylistForUser returns a playlist if the user has access
func (m *Module) GetPlaylistForUser(id PlaylistID, userID UserID) (*models.Playlist, error) {
	playlist, err := m.GetPlaylist(id)
	if err != nil {
		return nil, err
	}

	if playlist.UserID != string(userID) && !playlist.IsPublic {
		return nil, ErrAccessDenied
	}

	return playlist, nil
}

// UpdatePlaylist updates playlist metadata
func (m *Module) UpdatePlaylist(ctx context.Context, id PlaylistID, userID UserID, updates map[string]any) error {
	m.mu.RLock()
	playlist, exists := m.playlists[id]
	if !exists {
		m.mu.RUnlock()
		return ErrPlaylistNotFound
	}
	if playlist.UserID != string(userID) {
		m.mu.RUnlock()
		return ErrAccessDenied
	}
	// Build an updated copy under RLock so the DB write happens outside any lock.
	updated := *playlist
	m.mu.RUnlock()

	if name, ok := updates["name"].(string); ok {
		updated.Name = name
	}
	if desc, ok := updates["description"].(string); ok {
		updated.Description = desc
	}
	if isPublic, ok := updates["is_public"].(bool); ok {
		updated.IsPublic = isPublic
	}
	if cover, ok := updates["cover_image"].(string); ok {
		updated.CoverImage = cover
	}
	updated.ModifiedAt = time.Now()

	if err := m.playlistRepo.Update(ctx, &updated); err != nil {
		m.log.Error("Failed to update playlist in database: %v", err)
		return err
	}

	// Apply only the metadata fields back; Items slice is untouched to avoid clobbering
	// concurrent AddItem/RemoveItem mutations that completed during the DB write.
	m.mu.Lock()
	if p, ok := m.playlists[id]; ok && p.UserID == string(userID) {
		p.Name = updated.Name
		p.Description = updated.Description
		p.IsPublic = updated.IsPublic
		p.CoverImage = updated.CoverImage
		p.ModifiedAt = updated.ModifiedAt
	}
	m.mu.Unlock()

	m.log.Info("Updated playlist: %s", string(id))
	return nil
}

// DeletePlaylist removes a playlist
func (m *Module) DeletePlaylist(ctx context.Context, id PlaylistID, userID UserID) error {
	m.mu.RLock()
	playlist, exists := m.playlists[id]
	if !exists {
		m.mu.RUnlock()
		return ErrPlaylistNotFound
	}
	if playlist.UserID != string(userID) {
		m.mu.RUnlock()
		return ErrAccessDenied
	}
	m.mu.RUnlock()

	if err := m.playlistRepo.Delete(ctx, string(id)); err != nil {
		m.log.Error("Failed to delete playlist from database: %v", err)
		return err
	}

	m.mu.Lock()
	delete(m.playlists, id)
	m.mu.Unlock()

	m.log.Info("Deleted playlist: %s", string(id))
	return nil
}

// ListPlaylists returns all playlists for a user. Returned playlists are copies
// that are safe to read without holding any lock.
func (m *Module) ListPlaylists(userID UserID, includePublic bool) []*models.Playlist {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var playlists []*models.Playlist
	for _, playlist := range m.playlists {
		isOwned := playlist.UserID == string(userID)
		isPublicAndIncluded := includePublic && playlist.IsPublic
		if isOwned || isPublicAndIncluded {
			playlists = append(playlists, copyPlaylist(playlist))
		}
	}
	return playlists
}

// ListPublicPlaylists returns all public playlists across all users.
// Returned playlists are copies safe to read without holding any lock.
func (m *Module) ListPublicPlaylists() []*models.Playlist {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var playlists []*models.Playlist
	for _, playlist := range m.playlists {
		if playlist.IsPublic {
			playlists = append(playlists, copyPlaylist(playlist))
		}
	}
	return playlists
}

// AddItemInput holds parameters for adding an item to a playlist.
type AddItemInput struct {
	PlaylistID PlaylistID
	UserID     UserID
	MediaID    string
	MediaPath  string
	Title      string
}

// AddItem adds an item to a playlist
func (m *Module) AddItem(ctx context.Context, input AddItemInput) error {
	m.mu.RLock()
	playlist, exists := m.playlists[input.PlaylistID]
	if !exists {
		m.mu.RUnlock()
		return ErrPlaylistNotFound
	}
	if playlist.UserID != string(input.UserID) {
		m.mu.RUnlock()
		return ErrAccessDenied
	}
	// Check for duplicate under RLock.
	for _, item := range playlist.Items {
		if item.MediaPath == input.MediaPath || (input.MediaID != "" && item.MediaID == input.MediaID) {
			m.mu.RUnlock()
			return nil // Already exists
		}
	}
	m.mu.RUnlock()

	item := models.PlaylistItem{
		ID:         uuid.New().String(),
		PlaylistID: string(input.PlaylistID),
		MediaID:    input.MediaID,
		MediaPath:  input.MediaPath,
		Title:      input.Title,
		AddedAt:    time.Now(),
	}

	if err := m.playlistRepo.AddItem(ctx, &item); err != nil {
		m.log.Error("Failed to add item to playlist in database: %v", err)
		return fmt.Errorf("failed to add item to playlist: %w", err)
	}

	m.mu.Lock()
	if p, ok := m.playlists[input.PlaylistID]; ok && p.UserID == string(input.UserID) {
		// Re-check under write lock: a concurrent AddItem may have added the same item
		// between our RLock check and now. Prefer the earlier DB row; remove ours.
		for _, existing := range p.Items {
			if existing.MediaPath == input.MediaPath || (input.MediaID != "" && existing.MediaID == input.MediaID) {
				m.mu.Unlock()
				if err := m.playlistRepo.RemoveItem(ctx, item.ID); err != nil {
					m.log.Warn("Failed to remove duplicate playlist item %s from DB: %v", item.ID, err)
				}
				return nil
			}
		}
		item.Position = len(p.Items)
		p.Items = append(p.Items, item)
		p.ModifiedAt = time.Now()
	}
	m.mu.Unlock()

	m.log.Debug("Added item to playlist %s: %s", string(input.PlaylistID), input.MediaPath)
	return nil
}

// itemMatchesKey returns true if the item matches the given key by MediaPath,
// MediaID, or item ID.
func itemMatchesKey(item *models.PlaylistItem, key string) bool {
	return item.MediaPath == key || item.MediaID == key || item.ID == key
}

// RemoveItem removes an item from a playlist. The key is matched against the
// item's MediaPath, MediaID, and ID fields so callers can pass any of these
// identifiers (the frontend typically sends the media UUID via ?media_id=...).
func (m *Module) RemoveItem(ctx context.Context, playlistID PlaylistID, userID UserID, key string) error {
	// Find the matching item's ID under read lock.
	m.mu.RLock()
	playlist, err := m.getPlaylistForUserLocked(playlistID, userID)
	if err != nil {
		m.mu.RUnlock()
		return err
	}
	var itemID string
	for _, item := range playlist.Items {
		if itemMatchesKey(&item, key) {
			itemID = item.ID
			break
		}
	}
	m.mu.RUnlock()

	if itemID == "" {
		return ErrItemNotFound
	}

	if err := m.playlistRepo.RemoveItem(ctx, itemID); err != nil {
		m.log.Error("Failed to remove item from playlist in database: %v", err)
		return fmt.Errorf("failed to remove item from playlist: %w", err)
	}

	m.mu.Lock()
	if p, ok := m.playlists[playlistID]; ok && p.UserID == string(userID) {
		newItems := p.Items[:0]
		for _, item := range p.Items {
			if item.ID != itemID {
				newItems = append(newItems, item)
			}
		}
		for i := range newItems {
			newItems[i].Position = i
		}
		p.Items = newItems
		p.ModifiedAt = time.Now()
	}
	m.mu.Unlock()

	m.log.Debug("Removed item from playlist %s: %s", string(playlistID), key)
	return nil
}

// ReorderItems reorders playlist items
func (m *Module) ReorderItems(ctx context.Context, playlistID PlaylistID, userID UserID, positions []int) error {
	m.mu.RLock()
	newItems, err := m.buildReorderedItems(playlistID, userID, positions)
	m.mu.RUnlock()
	if err != nil {
		return err
	}

	for i := range newItems {
		if dbErr := m.playlistRepo.UpdateItem(ctx, &newItems[i]); dbErr != nil {
			return fmt.Errorf("failed to update item position in database: %w", dbErr)
		}
	}

	m.mu.Lock()
	if p, ok := m.playlists[playlistID]; ok && p.UserID == string(userID) && len(p.Items) == len(newItems) {
		p.Items = newItems
		p.ModifiedAt = time.Now()
	}
	m.mu.Unlock()
	return nil
}

// buildReorderedItems resolves the playlist and builds a reordered items slice; caller must hold m.mu for reading.
func (m *Module) buildReorderedItems(playlistID PlaylistID, userID UserID, positions []int) ([]models.PlaylistItem, error) {
	playlist, err := m.getPlaylistForUserLocked(playlistID, userID)
	if err != nil {
		return nil, err
	}
	if len(positions) != len(playlist.Items) {
		return nil, fmt.Errorf("position count mismatch")
	}
	seen := make(map[int]bool, len(positions))
	newItems := make([]models.PlaylistItem, len(playlist.Items))
	for i, pos := range positions {
		if pos < 0 || pos >= len(playlist.Items) {
			return nil, fmt.Errorf("invalid position: %d", pos)
		}
		if seen[pos] {
			return nil, fmt.Errorf("duplicate position: %d", pos)
		}
		seen[pos] = true
		newItems[i] = playlist.Items[pos]
		newItems[i].Position = i
	}
	return newItems, nil
}

// GetPlaylistItems returns items in a playlist
func (m *Module) GetPlaylistItems(playlistID PlaylistID, userID UserID) ([]models.PlaylistItem, error) {
	playlist, err := m.GetPlaylistForUser(playlistID, userID)
	if err != nil {
		return nil, err
	}
	return playlist.Items, nil
}

// ClearPlaylist removes all items from a playlist
func (m *Module) ClearPlaylist(ctx context.Context, playlistID PlaylistID, userID UserID) error {
	m.mu.RLock()
	playlist, exists := m.playlists[playlistID]
	if !exists {
		m.mu.RUnlock()
		return ErrPlaylistNotFound
	}
	if playlist.UserID != string(userID) {
		m.mu.RUnlock()
		return ErrAccessDenied
	}
	// Snapshot item IDs to delete; DB writes happen outside the lock.
	itemIDs := make([]string, len(playlist.Items))
	for i, item := range playlist.Items {
		itemIDs[i] = item.ID
	}
	m.mu.RUnlock()

	for _, id := range itemIDs {
		if err := m.playlistRepo.RemoveItem(ctx, id); err != nil {
			m.log.Error("Failed to remove item from database during clear: %v", err)
			return fmt.Errorf("failed to clear playlist: %w", err)
		}
	}

	// Remove only the items we deleted; preserve any items added concurrently.
	deleted := make(map[string]bool, len(itemIDs))
	for _, id := range itemIDs {
		deleted[id] = true
	}
	m.mu.Lock()
	if p, ok := m.playlists[playlistID]; ok && p.UserID == string(userID) {
		remaining := p.Items[:0]
		for _, item := range p.Items {
			if !deleted[item.ID] {
				remaining = append(remaining, item)
			}
		}
		p.Items = remaining
		p.ModifiedAt = time.Now()
	}
	m.mu.Unlock()
	return nil
}

// CopyPlaylist creates a copy of a playlist.
func (m *Module) CopyPlaylist(ctx context.Context, sourceID PlaylistID, userID UserID, newName string) (*models.Playlist, error) {
	// Snapshot source under read lock.
	m.mu.RLock()
	source, exists := m.playlists[sourceID]
	if !exists {
		m.mu.RUnlock()
		return nil, ErrPlaylistNotFound
	}
	if source.UserID != string(userID) && !source.IsPublic {
		m.mu.RUnlock()
		return nil, ErrAccessDenied
	}
	srcDescription := source.Description
	srcItems := make([]models.PlaylistItem, len(source.Items))
	copy(srcItems, source.Items)
	m.mu.RUnlock()

	newPlaylist := &models.Playlist{
		ID:          uuid.New().String(),
		Name:        newName,
		Description: srcDescription,
		UserID:      string(userID),
		Items:       make([]models.PlaylistItem, 0),
		CreatedAt:   time.Now(),
		ModifiedAt:  time.Now(),
		IsPublic:    false,
	}

	// Assign new IDs outside the lock; create playlist + items in one transaction to avoid orphans.
	newItems := make([]models.PlaylistItem, len(srcItems))
	for i, item := range srcItems {
		newItems[i] = models.PlaylistItem{
			ID:         uuid.New().String(),
			PlaylistID: newPlaylist.ID,
			MediaID:    item.MediaID,
			MediaPath:  item.MediaPath,
			Title:      item.Title,
			Position:   i,
			AddedAt:    time.Now(),
		}
	}
	if err := m.playlistRepo.CreateWithItems(ctx, newPlaylist, newItems); err != nil {
		m.log.Error("Failed to create copied playlist in database: %v", err)
		return nil, fmt.Errorf("failed to create playlist: %w", err)
	}
	newPlaylist.Items = newItems

	m.mu.Lock()
	m.playlists[PlaylistID(newPlaylist.ID)] = newPlaylist
	m.mu.Unlock()

	m.log.Info("Copied playlist %s to %s (user: %s)", string(sourceID), newPlaylist.ID, string(userID))
	return copyPlaylist(newPlaylist), nil
}

// GetStats returns playlist statistics
func (m *Module) GetStats() Stats {
	m.mu.RLock()
	defer m.mu.RUnlock()

	stats := Stats{
		TotalPlaylists: len(m.playlists),
	}

	for _, playlist := range m.playlists {
		stats.TotalItems += len(playlist.Items)
		if playlist.IsPublic {
			stats.PublicPlaylists++
		}
	}

	return stats
}

// Stats holds playlist statistics.
type Stats struct {
	TotalPlaylists  int `json:"total_playlists"`
	PublicPlaylists int `json:"public_playlists"`
	TotalItems      int `json:"total_items"`
}

// ListAllPlaylists returns all playlists (for admin). Returned playlists are
// copies that are safe to read without holding any lock.
func (m *Module) ListAllPlaylists() []*models.Playlist {
	m.mu.RLock()
	defer m.mu.RUnlock()

	playlists := make([]*models.Playlist, 0, len(m.playlists))
	for _, playlist := range m.playlists {
		playlists = append(playlists, copyPlaylist(playlist))
	}
	return playlists
}

// AdminDeletePlaylist deletes a playlist without checking ownership (admin only)
func (m *Module) AdminDeletePlaylist(ctx context.Context, id PlaylistID) error {
	m.mu.RLock()
	_, exists := m.playlists[id]
	m.mu.RUnlock()
	if !exists {
		return ErrPlaylistNotFound
	}

	if err := m.playlistRepo.Delete(ctx, string(id)); err != nil {
		m.log.Error("Failed to delete playlist from database: %v", err)
		return fmt.Errorf("failed to delete playlist from database: %w", err)
	}

	m.mu.Lock()
	delete(m.playlists, id)
	m.mu.Unlock()
	m.log.Info("Admin deleted playlist: %s", string(id))
	return nil
}

// ExportPlaylist exports a playlist in the specified format (json or m3u).
// All playlist data is snapshotted under a read lock before building the export.
func (m *Module) ExportPlaylist(playlistID PlaylistID, userID UserID, format string) (*Export, error) {
	// Snapshot playlist data under the read lock to avoid races
	m.mu.RLock()
	playlist, exists := m.playlists[playlistID]
	if !exists {
		m.mu.RUnlock()
		return nil, ErrPlaylistNotFound
	}
	if playlist.UserID != string(userID) && !playlist.IsPublic {
		m.mu.RUnlock()
		return nil, ErrAccessDenied
	}
	snapshot := copyPlaylist(playlist)
	m.mu.RUnlock()

	// Build export from the snapshot (no lock needed)
	export := &Export{
		Name:        snapshot.Name,
		Description: snapshot.Description,
		ItemCount:   len(snapshot.Items),
		CreatedAt:   snapshot.CreatedAt,
		ExportedAt:  time.Now(),
		Format:      format,
		Items:       make([]ExportItem, len(snapshot.Items)),
	}

	for i, item := range snapshot.Items {
		export.Items[i] = ExportItem{
			Title:    item.Title,
			Path:     item.MediaPath,
			Position: item.Position,
			AddedAt:  item.AddedAt,
		}
	}

	// Generate M3U content if requested
	if format == "m3u" || format == "m3u8" {
		var b strings.Builder
		b.WriteString("#EXTM3U\n")
		fmt.Fprintf(&b, "#PLAYLIST:%s\n", snapshot.Name)
		for _, item := range snapshot.Items {
			fmt.Fprintf(&b, "#EXTINF:-1,%s\n", item.Title)
			// Use the API stream path instead of the filesystem path to avoid
			// leaking server-side directory structure.
			fmt.Fprintf(&b, "/api/stream/%s\n", item.MediaID)
		}
		export.M3UContent = b.String()
	}

	return export, nil
}

// Export holds exported playlist data.
type Export struct {
	Name        string       `json:"name"`
	Description string       `json:"description"`
	ItemCount   int          `json:"item_count"`
	CreatedAt   time.Time    `json:"created_at"`
	ExportedAt  time.Time    `json:"exported_at"`
	Format      string       `json:"format"`
	Items       []ExportItem `json:"items"`
	M3UContent  string       `json:"m3u_content,omitempty"`
}

// ExportItem holds exported playlist item data.
type ExportItem struct {
	Title    string    `json:"title"`
	Path     string    `json:"path"`
	Position int       `json:"position"`
	AddedAt  time.Time `json:"added_at"`
}

// loadPlaylists populates the in-memory cache from MySQL on startup.
func (m *Module) loadPlaylists() error {
	ctx := context.Background()
	playlists, err := m.playlistRepo.ListAll(ctx)
	if err != nil {
		return fmt.Errorf("failed to list playlists from database: %w", err)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	for _, playlist := range playlists {
		if playlist.Items == nil {
			playlist.Items = make([]models.PlaylistItem, 0)
		}
		m.playlists[PlaylistID(playlist.ID)] = playlist
	}

	m.log.Info("Loaded %d playlists from database", len(playlists))
	return nil
}
