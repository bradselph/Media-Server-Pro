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
func (m *Module) UpdatePlaylist(ctx context.Context, id PlaylistID, userID UserID, updates map[string]interface{}) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	playlist, exists := m.playlists[id]
	if !exists {
		return ErrPlaylistNotFound
	}

	if playlist.UserID != string(userID) {
		return ErrAccessDenied
	}

	if name, ok := updates["name"].(string); ok {
		playlist.Name = name
	}
	if desc, ok := updates["description"].(string); ok {
		playlist.Description = desc
	}
	if isPublic, ok := updates["is_public"].(bool); ok {
		playlist.IsPublic = isPublic
	}
	if cover, ok := updates["cover_image"].(string); ok {
		playlist.CoverImage = cover
	}

	playlist.ModifiedAt = time.Now()

	if err := m.playlistRepo.Update(ctx, playlist); err != nil {
		m.log.Error("Failed to update playlist in database: %v", err)
		return err
	}

	m.log.Info("Updated playlist: %s", string(id))

	return nil
}

// DeletePlaylist removes a playlist
func (m *Module) DeletePlaylist(ctx context.Context, id PlaylistID, userID UserID) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	playlist, exists := m.playlists[id]
	if !exists {
		return ErrPlaylistNotFound
	}

	if playlist.UserID != string(userID) {
		return ErrAccessDenied
	}

	if err := m.playlistRepo.Delete(ctx, string(id)); err != nil {
		m.log.Error("Failed to delete playlist from database: %v", err)
		return err
	}

	delete(m.playlists, id)
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
	m.mu.Lock()
	defer m.mu.Unlock()

	playlist, exists := m.playlists[input.PlaylistID]
	if !exists {
		return ErrPlaylistNotFound
	}

	if playlist.UserID != string(input.UserID) {
		return ErrAccessDenied
	}

	// Check if already in playlist (by path or by media ID to avoid symlink/move duplicates)
	for _, item := range playlist.Items {
		if item.MediaPath == input.MediaPath || (input.MediaID != "" && item.MediaID == input.MediaID) {
			return nil // Already exists
		}
	}

	item := models.PlaylistItem{
		ID:         uuid.New().String(),
		PlaylistID: string(input.PlaylistID),
		MediaID:    input.MediaID,
		MediaPath:  input.MediaPath,
		Title:      input.Title,
		Position:   len(playlist.Items),
		AddedAt:    time.Now(),
	}

	if err := m.playlistRepo.AddItem(ctx, &item); err != nil {
		m.log.Error("Failed to add item to playlist in database: %v", err)
		return fmt.Errorf("failed to add item to playlist: %w", err)
	}

	playlist.Items = append(playlist.Items, item)
	playlist.ModifiedAt = time.Now()

	m.log.Debug("Added item to playlist %s: %s", string(input.PlaylistID), input.MediaPath)

	return nil
}

// RemoveItem removes an item from a playlist. The key is matched against the
// item's MediaPath, MediaID, and ID fields so callers can pass any of these
// identifiers (the frontend typically sends the media UUID via ?media_id=...).
func (m *Module) RemoveItem(ctx context.Context, playlistID PlaylistID, userID UserID, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.removeItemLocked(ctx, playlistID, userID, key)
}

// removeItemLocked performs the removal; caller must hold m.mu.
func (m *Module) removeItemLocked(ctx context.Context, playlistID PlaylistID, userID UserID, key string) error {
	playlist, newItems, err := m.resolvePlaylistAndFilterItem(ctx, playlistID, userID, key)
	if err != nil {
		return err
	}
	for i := range newItems {
		newItems[i].Position = i
	}
	playlist.Items = newItems
	playlist.ModifiedAt = time.Now()
	m.log.Debug("Removed item from playlist %s: %s", string(playlistID), key)
	return nil
}

// itemMatchesKey returns true if the item matches the given key by MediaPath,
// MediaID, or item ID.
func itemMatchesKey(item *models.PlaylistItem, key string) bool {
	return item.MediaPath == key || item.MediaID == key || item.ID == key
}

// resolvePlaylistAndFilterItem does playlist lookup and filters out the item
// matching key (by MediaPath, MediaID, or item ID); caller must hold m.mu.
func (m *Module) resolvePlaylistAndFilterItem(ctx context.Context, playlistID PlaylistID, userID UserID, key string) (*models.Playlist, []models.PlaylistItem, error) {
	playlist, err := m.getPlaylistForUserLocked(playlistID, userID)
	if err != nil {
		return nil, nil, err
	}
	var newItems []models.PlaylistItem
	found := false
	for _, item := range playlist.Items {
		if !itemMatchesKey(&item, key) {
			newItems = append(newItems, item)
		} else {
			found = true
			if removeErr := m.playlistRepo.RemoveItem(ctx, item.ID); removeErr != nil {
				m.log.Error("Failed to remove item from playlist in database: %v", removeErr)
				return nil, nil, fmt.Errorf("failed to remove item from playlist: %w", removeErr)
			}
		}
	}
	if !found {
		return nil, nil, ErrItemNotFound
	}
	return playlist, newItems, nil
}

// ReorderItems reorders playlist items
func (m *Module) ReorderItems(ctx context.Context, playlistID PlaylistID, userID UserID, positions []int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.reorderItemsLocked(ctx, playlistID, userID, positions)
}

// reorderItemsLocked performs the reorder; caller must hold m.mu.
func (m *Module) reorderItemsLocked(ctx context.Context, playlistID PlaylistID, userID UserID, positions []int) error {
	playlist, newItems, err := m.getPlaylistAndBuildReorderLocked(playlistID, userID, positions)
	if err != nil {
		return err
	}
	playlist.Items = newItems
	playlist.ModifiedAt = time.Now()
	for i := range newItems {
		if err := m.playlistRepo.UpdateItem(ctx, &newItems[i]); err != nil {
			m.log.Error("Failed to update item position in database: %v", err)
		}
	}
	return nil
}

// getPlaylistAndBuildReorderLocked resolves playlist and builds reordered items slice; caller must hold m.mu.
func (m *Module) getPlaylistAndBuildReorderLocked(playlistID PlaylistID, userID UserID, positions []int) (*models.Playlist, []models.PlaylistItem, error) {
	playlist, err := m.getPlaylistForUserLocked(playlistID, userID)
	if err != nil {
		return nil, nil, err
	}
	if len(positions) != len(playlist.Items) {
		return nil, nil, fmt.Errorf("position count mismatch")
	}
	seen := make(map[int]bool, len(positions))
	newItems := make([]models.PlaylistItem, len(playlist.Items))
	for i, pos := range positions {
		if pos < 0 || pos >= len(playlist.Items) {
			return nil, nil, fmt.Errorf("invalid position: %d", pos)
		}
		if seen[pos] {
			return nil, nil, fmt.Errorf("duplicate position: %d", pos)
		}
		seen[pos] = true
		newItems[i] = playlist.Items[pos]
		newItems[i].Position = i
	}
	return playlist, newItems, nil
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
	m.mu.Lock()
	defer m.mu.Unlock()

	playlist, exists := m.playlists[playlistID]
	if !exists {
		return ErrPlaylistNotFound
	}

	if playlist.UserID != string(userID) {
		return ErrAccessDenied
	}

	for _, item := range playlist.Items {
		if err := m.playlistRepo.RemoveItem(ctx, item.ID); err != nil {
			m.log.Error("Failed to remove item from database during clear: %v", err)
		}
	}

	playlist.Items = make([]models.PlaylistItem, 0)
	playlist.ModifiedAt = time.Now()

	return nil
}

// CopyPlaylist creates a copy of a playlist. The entire read-source and write-copy
// operation is performed under a single lock to avoid race conditions.
func (m *Module) CopyPlaylist(ctx context.Context, sourceID PlaylistID, userID UserID, newName string) (*models.Playlist, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Look up source playlist (inlined to avoid nested lock acquisition)
	source, exists := m.playlists[sourceID]
	if !exists {
		return nil, ErrPlaylistNotFound
	}
	if source.UserID != string(userID) && !source.IsPublic {
		return nil, ErrAccessDenied
	}

	// Snapshot source items and build the copy under the same lock
	items := make([]models.PlaylistItem, len(source.Items))
	for i, item := range source.Items {
		items[i] = models.PlaylistItem{
			MediaID:   item.MediaID,
			MediaPath: item.MediaPath,
			Title:     item.Title,
			Position:  i,
			AddedAt:   time.Now(),
		}
	}

	newPlaylist := &models.Playlist{
		ID:          uuid.New().String(),
		Name:        newName,
		Description: source.Description,
		UserID:      string(userID),
		Items:       make([]models.PlaylistItem, 0),
		CreatedAt:   time.Now(),
		ModifiedAt:  time.Now(),
		IsPublic:    false,
	}

	// Prepare items with new IDs; create playlist + items in one transaction to avoid orphans.
	newItems := make([]models.PlaylistItem, 0, len(items))
	for _, item := range items {
		item.PlaylistID = newPlaylist.ID
		item.ID = uuid.New().String()
		newItems = append(newItems, item)
	}
	if err := m.playlistRepo.CreateWithItems(ctx, newPlaylist, newItems); err != nil {
		m.log.Error("Failed to create copied playlist in database: %v", err)
		return nil, fmt.Errorf("failed to create playlist: %w", err)
	}
	newPlaylist.Items = newItems

	m.playlists[PlaylistID(newPlaylist.ID)] = newPlaylist

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
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.playlists[id]; !exists {
		return ErrPlaylistNotFound
	}

	if err := m.playlistRepo.Delete(ctx, string(id)); err != nil {
		m.log.Error("Failed to delete playlist from database: %v", err)
		return fmt.Errorf("failed to delete playlist from database: %w", err)
	}

	delete(m.playlists, id)
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
		b.WriteString(fmt.Sprintf("#PLAYLIST:%s\n", snapshot.Name))
		for _, item := range snapshot.Items {
			b.WriteString(fmt.Sprintf("#EXTINF:-1,%s\n", item.Title))
			b.WriteString(item.MediaPath + "\n")
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
