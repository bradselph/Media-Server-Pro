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

// Module implements playlist management
type Module struct {
	config       *config.Manager
	log          *logger.Logger
	dbModule     *database.Module
	playlistRepo repositories.PlaylistRepository
	playlists    map[string]*models.Playlist // Write-through cache for performance
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
		playlists: make(map[string]*models.Playlist),
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

// CreatePlaylist creates a new playlist
func (m *Module) CreatePlaylist(ctx context.Context, name, description, userID string, isPublic bool) (*models.Playlist, error) {
	playlist := &models.Playlist{
		ID:          uuid.New().String(),
		Name:        name,
		Description: description,
		UserID:      userID,
		Items:       make([]models.PlaylistItem, 0),
		CreatedAt:   time.Now(),
		ModifiedAt:  time.Now(),
		IsPublic:    isPublic,
	}

	if err := m.playlistRepo.Create(ctx, playlist); err != nil {
		return nil, fmt.Errorf("failed to create playlist: %w", err)
	}

	m.mu.Lock()
	m.playlists[playlist.ID] = playlist
	result := copyPlaylist(playlist)
	m.mu.Unlock()

	m.log.Info("Created playlist: %s (user: %s)", name, userID)

	return result, nil
}

// copyPlaylist returns a deep copy of a playlist, including a copy of its Items slice.
func copyPlaylist(p *models.Playlist) *models.Playlist {
	result := *p
	result.Items = make([]models.PlaylistItem, len(p.Items))
	copy(result.Items, p.Items)
	return &result
}

// GetPlaylist returns a playlist by ID. The returned playlist is a copy that is
// safe to read without holding any lock.
func (m *Module) GetPlaylist(id string) (*models.Playlist, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	playlist, exists := m.playlists[id]
	if !exists {
		return nil, ErrPlaylistNotFound
	}
	return copyPlaylist(playlist), nil
}

// GetPlaylistForUser returns a playlist if the user has access
func (m *Module) GetPlaylistForUser(id, userID string) (*models.Playlist, error) {
	playlist, err := m.GetPlaylist(id)
	if err != nil {
		return nil, err
	}

	if playlist.UserID != userID && !playlist.IsPublic {
		return nil, ErrAccessDenied
	}

	return playlist, nil
}

// UpdatePlaylist updates playlist metadata
func (m *Module) UpdatePlaylist(ctx context.Context, id, userID string, updates map[string]interface{}) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	playlist, exists := m.playlists[id]
	if !exists {
		return ErrPlaylistNotFound
	}

	if playlist.UserID != userID {
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
	}

	m.log.Info("Updated playlist: %s", id)

	return nil
}

// DeletePlaylist removes a playlist
func (m *Module) DeletePlaylist(ctx context.Context, id, userID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	playlist, exists := m.playlists[id]
	if !exists {
		return ErrPlaylistNotFound
	}

	if playlist.UserID != userID {
		return ErrAccessDenied
	}

	if err := m.playlistRepo.Delete(ctx, id); err != nil {
		m.log.Error("Failed to delete playlist from database: %v", err)
	}

	delete(m.playlists, id)
	m.log.Info("Deleted playlist: %s", id)

	return nil
}

// ListPlaylists returns all playlists for a user. Returned playlists are copies
// that are safe to read without holding any lock.
func (m *Module) ListPlaylists(userID string, includePublic bool) []*models.Playlist {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var playlists []*models.Playlist
	for _, playlist := range m.playlists {
		if playlist.UserID == userID || (includePublic && playlist.IsPublic) {
			playlists = append(playlists, copyPlaylist(playlist))
		}
	}
	return playlists
}

// AddItem adds an item to a playlist
func (m *Module) AddItem(ctx context.Context, playlistID, userID, mediaID, mediaPath, title string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	playlist, exists := m.playlists[playlistID]
	if !exists {
		return ErrPlaylistNotFound
	}

	if playlist.UserID != userID {
		return ErrAccessDenied
	}

	// Check if already in playlist
	for _, item := range playlist.Items {
		if item.MediaPath == mediaPath {
			return nil // Already exists
		}
	}

	item := models.PlaylistItem{
		ID:         uuid.New().String(),
		PlaylistID: playlistID,
		MediaID:    mediaID,
		MediaPath:  mediaPath,
		Title:      title,
		Position:   len(playlist.Items),
		AddedAt:    time.Now(),
	}

	if err := m.playlistRepo.AddItem(ctx, &item); err != nil {
		m.log.Error("Failed to add item to playlist in database: %v", err)
	}

	playlist.Items = append(playlist.Items, item)
	playlist.ModifiedAt = time.Now()

	m.log.Debug("Added item to playlist %s: %s", playlistID, mediaPath)

	return nil
}

// RemoveItem removes an item from a playlist
func (m *Module) RemoveItem(ctx context.Context, playlistID, userID, mediaPath string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	playlist, exists := m.playlists[playlistID]
	if !exists {
		return ErrPlaylistNotFound
	}

	if playlist.UserID != userID {
		return ErrAccessDenied
	}

	found := false
	var newItems []models.PlaylistItem
	for _, item := range playlist.Items {
		if item.MediaPath != mediaPath {
			newItems = append(newItems, item)
		} else {
			found = true
			if err := m.playlistRepo.RemoveItem(ctx, item.ID); err != nil {
				m.log.Error("Failed to remove item from playlist in database: %v", err)
			}
		}
	}

	if !found {
		return ErrItemNotFound
	}

	// Reindex positions
	for i := range newItems {
		newItems[i].Position = i
	}

	playlist.Items = newItems
	playlist.ModifiedAt = time.Now()

	m.log.Debug("Removed item from playlist %s: %s", playlistID, mediaPath)

	return nil
}

// ReorderItems reorders playlist items
func (m *Module) ReorderItems(ctx context.Context, playlistID, userID string, positions []int) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	playlist, exists := m.playlists[playlistID]
	if !exists {
		return ErrPlaylistNotFound
	}

	if playlist.UserID != userID {
		return ErrAccessDenied
	}

	if len(positions) != len(playlist.Items) {
		return fmt.Errorf("position count mismatch")
	}

	// Reorder items - validate no duplicate positions
	seen := make(map[int]bool, len(positions))
	newItems := make([]models.PlaylistItem, len(playlist.Items))
	for i, pos := range positions {
		if pos < 0 || pos >= len(playlist.Items) {
			return fmt.Errorf("invalid position: %d", pos)
		}
		if seen[pos] {
			return fmt.Errorf("duplicate position: %d", pos)
		}
		seen[pos] = true
		newItems[i] = playlist.Items[pos]
		newItems[i].Position = i
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

// GetPlaylistItems returns items in a playlist
func (m *Module) GetPlaylistItems(playlistID, userID string) ([]models.PlaylistItem, error) {
	playlist, err := m.GetPlaylistForUser(playlistID, userID)
	if err != nil {
		return nil, err
	}
	return playlist.Items, nil
}

// ClearPlaylist removes all items from a playlist
func (m *Module) ClearPlaylist(ctx context.Context, playlistID, userID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	playlist, exists := m.playlists[playlistID]
	if !exists {
		return ErrPlaylistNotFound
	}

	if playlist.UserID != userID {
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
func (m *Module) CopyPlaylist(ctx context.Context, sourceID, userID, newName string) (*models.Playlist, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Look up source playlist (inlined to avoid nested lock acquisition)
	source, exists := m.playlists[sourceID]
	if !exists {
		return nil, ErrPlaylistNotFound
	}
	if source.UserID != userID && !source.IsPublic {
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
		UserID:      userID,
		Items:       make([]models.PlaylistItem, 0),
		CreatedAt:   time.Now(),
		ModifiedAt:  time.Now(),
		IsPublic:    false,
	}

	if err := m.playlistRepo.Create(ctx, newPlaylist); err != nil {
		m.log.Error("Failed to create copied playlist in database: %v", err)
		return nil, fmt.Errorf("failed to create playlist: %w", err)
	}

	for _, item := range items {
		item.PlaylistID = newPlaylist.ID
		item.ID = uuid.New().String()
		if err := m.playlistRepo.AddItem(ctx, &item); err != nil {
			m.log.Error("Failed to add item to copied playlist in database: %v", err)
		}
		newPlaylist.Items = append(newPlaylist.Items, item)
	}

	m.playlists[newPlaylist.ID] = newPlaylist

	m.log.Info("Copied playlist %s to %s (user: %s)", sourceID, newPlaylist.ID, userID)

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
func (m *Module) AdminDeletePlaylist(ctx context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.playlists[id]; !exists {
		return ErrPlaylistNotFound
	}

	if err := m.playlistRepo.Delete(ctx, id); err != nil {
		m.log.Error("Failed to delete playlist from database: %v", err)
	}

	delete(m.playlists, id)
	m.log.Info("Admin deleted playlist: %s", id)

	return nil
}

// ExportPlaylist exports a playlist in the specified format (json or m3u).
// All playlist data is snapshotted under a read lock before building the export.
func (m *Module) ExportPlaylist(playlistID, userID, format string) (*Export, error) {
	// Snapshot playlist data under the read lock to avoid races
	m.mu.RLock()
	playlist, exists := m.playlists[playlistID]
	if !exists {
		m.mu.RUnlock()
		return nil, ErrPlaylistNotFound
	}
	if playlist.UserID != userID && !playlist.IsPublic {
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
		m.playlists[playlist.ID] = playlist
	}

	m.log.Info("Loaded %d playlists from database", len(playlists))
	return nil
}
