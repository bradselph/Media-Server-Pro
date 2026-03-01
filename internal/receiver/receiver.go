// Package receiver implements the master-side of a master-slave media distribution system.
// When enabled, the server acts as a master that accepts catalog registrations from slave
// nodes and proxies media streams from those slaves to authenticated users on demand.
// No media files are stored on the master — all content is streamed in real-time from
// the originating slave node.
package receiver

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"media-server-pro/internal/config"
	"media-server-pro/internal/database"
	"media-server-pro/internal/logger"
	"media-server-pro/internal/repositories"
	mysqlrepo "media-server-pro/internal/repositories/mysql"
	"media-server-pro/pkg/helpers"
	"media-server-pro/pkg/models"
)

// RegisterRequest is the body for POST /api/receiver/register.
type RegisterRequest struct {
	// SlaveID is optional — if provided the existing registration is updated.
	SlaveID string `json:"slave_id,omitempty"`
	Name    string `json:"name"`
	BaseURL string `json:"base_url"`
}

// CatalogItem is a single media entry pushed by a slave node.
type CatalogItem struct {
	ID          string  `json:"id"`
	Path        string  `json:"path"` // relative path served at slave's /media?path=<path>
	Name        string  `json:"name"`
	MediaType   string  `json:"media_type"`
	Size        int64   `json:"size"`
	Duration    float64 `json:"duration"`
	ContentType string  `json:"content_type"`
	Width       int     `json:"width"`
	Height      int     `json:"height"`
}

// CatalogPushRequest is the body for POST /api/receiver/catalog.
type CatalogPushRequest struct {
	SlaveID string         `json:"slave_id"`
	Items   []*CatalogItem `json:"items"`
	// Full signals that this is a complete catalog replacement for the slave.
	Full bool `json:"full"`
}

// SlaveNode represents a registered slave node.
type SlaveNode struct {
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	BaseURL      string    `json:"base_url"`
	Status       string    `json:"status"`
	MediaCount   int       `json:"media_count"`
	LastSeen     time.Time `json:"last_seen"`
	RegisteredAt time.Time `json:"registered_at"`
}

// MediaItem represents a media entry from a slave's catalog.
type MediaItem struct {
	ID          string  `json:"id"`
	SlaveID     string  `json:"slave_id"`
	SlaveName   string  `json:"slave_name,omitempty"`
	Path        string  `json:"path"`
	Name        string  `json:"name"`
	MediaType   string  `json:"media_type"`
	Size        int64   `json:"size"`
	Duration    float64 `json:"duration"`
	ContentType string  `json:"content_type"`
	Width       int     `json:"width"`
	Height      int     `json:"height"`
}

// Stats summarises the receiver module state.
type Stats struct {
	SlaveCount  int `json:"slave_count"`
	OnlineCount int `json:"online_slaves"`
	MediaCount  int `json:"media_count"`
}

// Module handles incoming media catalog registrations from slave nodes.
type Module struct {
	config       *config.Manager
	log          *logger.Logger
	dbModule     *database.Module
	slaveRepo    repositories.ReceiverSlaveRepository
	mediaRepo    repositories.ReceiverMediaRepository
	mu           sync.RWMutex
	slaves       map[string]*SlaveNode
	media        map[string]*MediaItem // keyed by master-assigned ID
	healthMu     sync.RWMutex
	healthy      bool
	healthMsg    string
	healthTicker *time.Ticker
	healthDone   chan struct{}
}

// NewModule creates a new receiver module.
func NewModule(cfg *config.Manager, dbModule *database.Module) *Module {
	return &Module{
		config:     cfg,
		log:        logger.New("receiver"),
		dbModule:   dbModule,
		slaves:     make(map[string]*SlaveNode),
		media:      make(map[string]*MediaItem),
		healthDone: make(chan struct{}),
	}
}

// Name implements server.Module.
func (m *Module) Name() string { return "receiver" }

// Start implements server.Module.
func (m *Module) Start(_ context.Context) error {
	m.log.Info("Starting receiver module...")

	m.slaveRepo = mysqlrepo.NewReceiverSlaveRepository(m.dbModule.GORM())
	m.mediaRepo = mysqlrepo.NewReceiverMediaRepository(m.dbModule.GORM())

	cfg := m.config.Get()
	if !cfg.Receiver.Enabled {
		m.log.Info("Receiver module is disabled")
		m.setHealth(true, "Disabled")
		return nil
	}

	m.loadFromDB()

	if interval := cfg.Receiver.HealthCheck; interval > 0 {
		m.healthTicker = time.NewTicker(interval)
		go m.healthCheckLoop()
	}

	m.setHealth(true, "Running")
	m.log.Info("Receiver module started with %d slaves", len(m.slaves))
	return nil
}

// Stop implements server.Module.
func (m *Module) Stop(_ context.Context) error {
	m.log.Info("Stopping receiver module...")
	if m.healthTicker != nil {
		m.healthTicker.Stop()
		close(m.healthDone)
	}
	m.setHealth(false, "Stopped")
	return nil
}

// Health implements server.Module.
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

func (m *Module) setHealth(healthy bool, msg string) {
	m.healthMu.Lock()
	m.healthy = healthy
	m.healthMsg = msg
	m.healthMu.Unlock()
}

// loadFromDB populates the in-memory caches from the database on startup.
func (m *Module) loadFromDB() {
	ctx := context.Background()

	slaveRecords, err := m.slaveRepo.List(ctx)
	if err != nil {
		m.log.Warn("Failed to load slaves from DB: %v", err)
		return
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	for _, rec := range slaveRecords {
		m.slaves[rec.ID] = slaveRecordToNode(rec)
	}

	mediaRecords, err := m.mediaRepo.ListAll(ctx)
	if err != nil {
		m.log.Warn("Failed to load media from DB: %v", err)
		return
	}

	for _, rec := range mediaRecords {
		item := mediaRecordToItem(rec)
		if node, ok := m.slaves[rec.SlaveID]; ok {
			item.SlaveName = node.Name
		}
		m.media[rec.ID] = item
	}

	m.log.Info("Loaded %d slaves, %d media items from DB", len(m.slaves), len(m.media))
}

func (m *Module) healthCheckLoop() {
	for {
		select {
		case <-m.healthTicker.C:
			m.markStaleSlaves()
		case <-m.healthDone:
			return
		}
	}
}

// markStaleSlaves sets slaves to "offline" if their last heartbeat is overdue.
func (m *Module) markStaleSlaves() {
	cfg := m.config.Get()
	threshold := cfg.Receiver.HealthCheck * 3
	if threshold == 0 {
		threshold = 90 * time.Second
	}

	ctx := context.Background()

	m.mu.Lock()
	defer m.mu.Unlock()

	for _, node := range m.slaves {
		if node.Status == "online" && time.Since(node.LastSeen) > threshold {
			node.Status = "offline"
			rec := nodeToSlaveRecord(node)
			if err := m.slaveRepo.Upsert(ctx, rec); err != nil {
				m.log.Warn("Health check: failed to update slave %s: %v", node.ID, err)
			}
		}
	}
}

// ValidateAPIKey reports whether the provided key is in the configured API key list.
func (m *Module) ValidateAPIKey(key string) bool {
	if key == "" {
		return false
	}
	for _, k := range m.config.Get().Receiver.APIKeys {
		if k == key {
			return true
		}
	}
	return false
}

// RegisterSlave registers a new slave node or updates an existing one.
func (m *Module) RegisterSlave(req *RegisterRequest) (*SlaveNode, error) {
	if req.Name == "" || req.BaseURL == "" {
		return nil, fmt.Errorf("name and base_url are required")
	}

	slaveID := req.SlaveID
	if slaveID == "" {
		slaveID = uuid.New().String()
	}

	m.mu.Lock()
	existing, exists := m.slaves[slaveID]
	node := &SlaveNode{
		ID:       slaveID,
		Name:     req.Name,
		BaseURL:  strings.TrimRight(req.BaseURL, "/"),
		Status:   "online",
		LastSeen: time.Now(),
	}
	if exists {
		node.MediaCount = existing.MediaCount
		node.RegisteredAt = existing.RegisteredAt
	} else {
		node.RegisteredAt = time.Now()
	}
	m.slaves[slaveID] = node
	m.mu.Unlock()

	rec := nodeToSlaveRecord(node)
	if err := m.slaveRepo.Upsert(context.Background(), rec); err != nil {
		return nil, fmt.Errorf("failed to persist slave: %w", err)
	}

	m.log.Info("Slave registered: %s (%s) at %s", node.Name, node.ID, node.BaseURL)
	return node, nil
}

// PushCatalog updates the slave's media catalog.
// If req.Full is true, the existing catalog for this slave is replaced entirely.
func (m *Module) PushCatalog(req *CatalogPushRequest) (int, error) {
	if req.SlaveID == "" {
		return 0, fmt.Errorf("slave_id is required")
	}

	m.mu.RLock()
	node, exists := m.slaves[req.SlaveID]
	m.mu.RUnlock()
	if !exists {
		return 0, fmt.Errorf("slave not found: %s", req.SlaveID)
	}

	ctx := context.Background()

	// Build DB records
	records := make([]*repositories.ReceiverMediaRecord, len(req.Items))
	for i, item := range req.Items {
		records[i] = &repositories.ReceiverMediaRecord{
			ID:          fmt.Sprintf("%s:%s", req.SlaveID, item.ID),
			SlaveID:     req.SlaveID,
			RemotePath:  item.Path,
			Name:        item.Name,
			MediaType:   item.MediaType,
			Size:        item.Size,
			Duration:    item.Duration,
			ContentType: item.ContentType,
			Width:       item.Width,
			Height:      item.Height,
		}
	}

	if req.Full {
		// Full replacement: delete existing catalog then insert
		if err := m.mediaRepo.DeleteBySlave(ctx, req.SlaveID); err != nil {
			return 0, fmt.Errorf("failed to clear old catalog: %w", err)
		}
	}

	if len(records) > 0 {
		if err := m.mediaRepo.UpsertBatch(ctx, req.SlaveID, records); err != nil {
			return 0, fmt.Errorf("failed to persist catalog: %w", err)
		}
	}

	// Rebuild in-memory media for this slave
	m.mu.Lock()
	if req.Full {
		for id, item := range m.media {
			if item.SlaveID == req.SlaveID {
				delete(m.media, id)
			}
		}
	}
	for _, rec := range records {
		item := mediaRecordToItem(rec)
		item.SlaveName = node.Name
		m.media[rec.ID] = item
	}
	node.MediaCount = len(req.Items)
	node.Status = "online"
	node.LastSeen = time.Now()
	m.mu.Unlock()

	// Update slave record in DB
	if err := m.slaveRepo.Upsert(ctx, nodeToSlaveRecord(node)); err != nil {
		m.log.Warn("Failed to update slave count after catalog push: %v", err)
	}

	m.log.Info("Catalog updated: %d items from slave %s", len(req.Items), req.SlaveID)
	return len(req.Items), nil
}

// Heartbeat updates the slave's last-seen timestamp.
func (m *Module) Heartbeat(slaveID string) error {
	m.mu.Lock()
	node, exists := m.slaves[slaveID]
	if !exists {
		m.mu.Unlock()
		return fmt.Errorf("slave not found: %s", slaveID)
	}
	node.Status = "online"
	node.LastSeen = time.Now()
	m.mu.Unlock()

	return m.slaveRepo.Upsert(context.Background(), nodeToSlaveRecord(node))
}

// UnregisterSlave removes a slave and its entire media catalog.
func (m *Module) UnregisterSlave(slaveID string) error {
	m.mu.Lock()
	if _, exists := m.slaves[slaveID]; !exists {
		m.mu.Unlock()
		return fmt.Errorf("slave not found: %s", slaveID)
	}
	delete(m.slaves, slaveID)
	for id, item := range m.media {
		if item.SlaveID == slaveID {
			delete(m.media, id)
		}
	}
	m.mu.Unlock()

	ctx := context.Background()
	if err := m.mediaRepo.DeleteBySlave(ctx, slaveID); err != nil {
		return fmt.Errorf("failed to remove slave media: %w", err)
	}
	return m.slaveRepo.Delete(ctx, slaveID)
}

// GetSlaves returns all registered slave nodes.
func (m *Module) GetSlaves() []*SlaveNode {
	m.mu.RLock()
	defer m.mu.RUnlock()
	nodes := make([]*SlaveNode, 0, len(m.slaves))
	for _, n := range m.slaves {
		nodes = append(nodes, n)
	}
	return nodes
}

// GetAllMedia returns all media across all slaves.
func (m *Module) GetAllMedia() []*MediaItem {
	m.mu.RLock()
	defer m.mu.RUnlock()
	items := make([]*MediaItem, 0, len(m.media))
	for _, item := range m.media {
		items = append(items, item)
	}
	return items
}

// GetSlaveMedia returns all media from a specific slave.
func (m *Module) GetSlaveMedia(slaveID string) []*MediaItem {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var items []*MediaItem
	for _, item := range m.media {
		if item.SlaveID == slaveID {
			items = append(items, item)
		}
	}
	return items
}

// SearchMedia returns media items whose name contains the query string (case-insensitive).
func (m *Module) SearchMedia(query string) []*MediaItem {
	lower := strings.ToLower(query)
	m.mu.RLock()
	defer m.mu.RUnlock()
	var items []*MediaItem
	for _, item := range m.media {
		if strings.Contains(strings.ToLower(item.Name), lower) {
			items = append(items, item)
		}
	}
	return items
}

// GetMediaItem returns a single media item by its master-assigned ID, or nil if not found.
func (m *Module) GetMediaItem(id string) *MediaItem {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.media[id]
}

// GetStats returns a summary of the receiver module state.
func (m *Module) GetStats() Stats {
	m.mu.RLock()
	defer m.mu.RUnlock()
	stats := Stats{
		SlaveCount: len(m.slaves),
		MediaCount: len(m.media),
	}
	for _, node := range m.slaves {
		if node.Status == "online" {
			stats.OnlineCount++
		}
	}
	return stats
}

// ProxyStream fetches the media stream from the originating slave and pipes it
// to the client. Range requests are forwarded so seeking works correctly.
func (m *Module) ProxyStream(w http.ResponseWriter, r *http.Request, mediaID string) error {
	item := m.GetMediaItem(mediaID)
	if item == nil {
		http.NotFound(w, r)
		return nil
	}

	m.mu.RLock()
	node, exists := m.slaves[item.SlaveID]
	m.mu.RUnlock()
	if !exists {
		return fmt.Errorf("slave not found for media %s", mediaID)
	}

	// The slave serves files at /media?path=<path>
	streamURL := node.BaseURL + "/media?path=" + url.QueryEscape(item.Path)

	cfg := m.config.Get()
	client := &http.Client{Timeout: cfg.Receiver.ProxyTimeout}

	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, streamURL, nil)
	if err != nil {
		return fmt.Errorf("failed to build proxy request: %w", err)
	}

	if rng := r.Header.Get("Range"); rng != "" {
		req.Header.Set("Range", rng)
	}
	req.Header.Set("User-Agent", "MediaServerPro/4.0 Receiver")

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to connect to slave: %w", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			m.log.Warn("Failed to close slave response body: %v", err)
		}
	}()

	// Forward safe response headers only.
	blockedHeaders := map[string]bool{
		"Set-Cookie":       true,
		"Authorization":    true,
		"Cookie":           true,
		"Www-Authenticate": true,
	}
	for key, values := range resp.Header {
		if blockedHeaders[key] {
			continue
		}
		for _, v := range values {
			w.Header().Add(key, v)
		}
	}

	w.WriteHeader(resp.StatusCode)
	_, err = io.Copy(w, resp.Body)
	return err
}

// --- Conversion helpers ---

func slaveRecordToNode(rec *repositories.ReceiverSlaveRecord) *SlaveNode {
	return &SlaveNode{
		ID:           rec.ID,
		Name:         rec.Name,
		BaseURL:      rec.BaseURL,
		Status:       rec.Status,
		MediaCount:   rec.MediaCount,
		LastSeen:     rec.LastSeen,
		RegisteredAt: rec.CreatedAt,
	}
}

func nodeToSlaveRecord(node *SlaveNode) *repositories.ReceiverSlaveRecord {
	return &repositories.ReceiverSlaveRecord{
		ID:         node.ID,
		Name:       node.Name,
		BaseURL:    node.BaseURL,
		Status:     node.Status,
		MediaCount: node.MediaCount,
		LastSeen:   node.LastSeen,
		CreatedAt:  node.RegisteredAt,
	}
}

func mediaRecordToItem(rec *repositories.ReceiverMediaRecord) *MediaItem {
	return &MediaItem{
		ID:          rec.ID,
		SlaveID:     rec.SlaveID,
		Path:        rec.RemotePath,
		Name:        rec.Name,
		MediaType:   rec.MediaType,
		Size:        rec.Size,
		Duration:    rec.Duration,
		ContentType: rec.ContentType,
		Width:       rec.Width,
		Height:      rec.Height,
	}
}
