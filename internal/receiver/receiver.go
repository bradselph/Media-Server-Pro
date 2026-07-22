// Package receiver implements the master-side of a master-slave media distribution system.
// When enabled, the server acts as a master that accepts catalog registrations from slave
// nodes and proxies media streams from those slaves to authenticated users on demand.
// No media files are stored on the master — all content is streamed in real-time from
// the originating slave node.
package receiver

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
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
	"media-server-pro/internal/duplicates"
	"media-server-pro/internal/logger"
	"media-server-pro/internal/repositories"
	mysqlrepo "media-server-pro/internal/repositories/mysql"
	"media-server-pro/pkg/helpers"
	"media-server-pro/pkg/models"
)

const (
	defaultProxyTimeout = 30 * time.Second
	receiverDBTimeout   = 15 * time.Second
)

// duplicateCoordinator is the receiver-facing duplicate contract. Keeping the
// reverse direction (duplicates -> receiver) interface-based avoids an import
// cycle while still letting receiver own all catalog serialization.
type duplicateCoordinator interface {
	ClearForSlave(ctx context.Context, slaveID string) error
	ClearPendingForSlave(ctx context.Context, slaveID string) error
	RemovedReceiverItemIDs(ctx context.Context, slaveID string) (map[string]struct{}, error)
	RecordDuplicatesFromSlave(ctx context.Context, slaveID string, items []duplicates.ReceiverItemRef) error
	CountPending() int
}

// opaqueMediaID produces a deterministic, opaque 32-char hex identifier from
// a slave ID and item ID.  This hides internal topology (which slave hosts
// what) from the public API so that clients never see raw slave identifiers
// in URLs or responses.
func opaqueMediaID(slaveID, itemID string) string {
	h := sha256.Sum256([]byte(slaveID + "\x00" + itemID))
	return hex.EncodeToString(h[:16])
}

// RegisterRequest is the body for POST /api/receiver/register.
type RegisterRequest struct {
	// SlaveID is optional — if provided the existing registration is updated.
	SlaveID string `json:"slave_id,omitempty"`
	Name    string `json:"name"`
	BaseURL string `json:"base_url"`
}

// CatalogItem is a single media entry pushed by a slave node.
//
// In addition to the basic file properties the slave forwards display
// metadata (category, tags, dates, blur_hash, is_mature) so federated content
// renders identically to local content in the unified library. Anything new
// added here must also flow through ReceiverMediaRecord, MediaItem,
// PushCatalog, mediaRecordToItem, and the api/handlers/media.go merge.
type CatalogItem struct {
	ID                 string    `json:"id"`
	Path               string    `json:"path"` // relative path served at slave's /media?path=<path>
	Name               string    `json:"name"`
	MediaType          string    `json:"media_type"`
	Size               int64     `json:"size"`
	Duration           float64   `json:"duration"`
	ContentType        string    `json:"content_type"`
	ContentFingerprint string    `json:"content_fingerprint,omitempty"`
	Width              int       `json:"width"`
	Height             int       `json:"height"`
	Category           string    `json:"category,omitempty"`
	Tags               []string  `json:"tags,omitempty"`
	BlurHash           string    `json:"blur_hash,omitempty"`
	DateAdded          time.Time `json:"date_added,omitzero"`
	DateModified       time.Time `json:"date_modified,omitzero"`
	IsMature           bool      `json:"is_mature,omitempty"`
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
	ID                 string    `json:"id"`
	SlaveID            string    `json:"slave_id"`
	SlaveName          string    `json:"slave_name,omitempty"`
	RemoteID           string    `json:"remote_id,omitempty"` // slave's own item.ID
	Path               string    `json:"path"`
	Name               string    `json:"name"`
	MediaType          string    `json:"media_type"`
	Size               int64     `json:"size"`
	Duration           float64   `json:"duration"`
	ContentType        string    `json:"content_type"`
	ContentFingerprint string    `json:"content_fingerprint,omitempty"`
	Width              int       `json:"width"`
	Height             int       `json:"height"`
	Category           string    `json:"category,omitempty"`
	Tags               []string  `json:"tags,omitempty"`
	BlurHash           string    `json:"blur_hash,omitempty"`
	DateAdded          time.Time `json:"date_added,omitzero"`
	DateModified       time.Time `json:"date_modified,omitzero"`
	IsMature           bool      `json:"is_mature,omitempty"`
}

// Stats summarizes the receiver module state.
type Stats struct {
	SlaveCount     int `json:"slave_count"`
	OnlineCount    int `json:"online_slaves"`
	MediaCount     int `json:"media_count"`
	DuplicateCount int `json:"duplicate_count"`
}

// Module handles incoming media catalog registrations from slave nodes.
type Module struct {
	config         *config.Manager
	log            *logger.Logger
	dbModule       *database.Module
	slaveRepo      repositories.ReceiverSlaveRepository
	mediaRepo      repositories.ReceiverMediaRepository
	dupModule      duplicateCoordinator
	slaveWriteMu   sync.Mutex // serializes DB-first slave registration updates
	mu             sync.RWMutex
	slaves         map[string]*SlaveNode
	media          map[string]*MediaItem // keyed by master-assigned ID
	healthMu       sync.RWMutex
	healthy        bool
	healthMsg      string
	healthTicker   *time.Ticker
	healthDone     chan struct{}
	healthDoneOnce sync.Once // guards close(healthDone) to avoid double-close panic
	// WebSocket connections from slaves (keyed by slave ID).
	wsMu           sync.RWMutex
	wsConns        map[string]*slaveWS
	pendingMu      sync.Mutex
	pendingStreams map[string]*PendingStream
	// proxySem limits the number of concurrent proxy connections (MaxProxyConns).
	proxySem chan struct{}
	// httpClient is shared for HTTP fallback proxy to slaves (connection pooling).
	httpClient *http.Client
}

// NewModule creates a new receiver module.
func NewModule(cfg *config.Manager, dbModule *database.Module) *Module {
	// Initialize proxySem with a safe default capacity so that hot-reload
	// enabling the receiver after a disabled startup does not leave it nil
	// (which would silently bypass the MaxProxyConns limit forever).
	// Start() re-makes the channel with the configured capacity when it runs.
	return &Module{
		config:         cfg,
		log:            logger.New("receiver"),
		dbModule:       dbModule,
		slaves:         make(map[string]*SlaveNode),
		media:          make(map[string]*MediaItem),
		healthDone:     make(chan struct{}),
		wsConns:        make(map[string]*slaveWS),
		pendingStreams: make(map[string]*PendingStream),
		proxySem:       make(chan struct{}, 50), // default; overridden by Start() with configured value
	}
}

// SetDuplicatesModule wires the duplicates module so the receiver can report
// fingerprint collisions without depending on the receiver feature being enabled.
func (m *Module) SetDuplicatesModule(d *duplicates.Module) {
	m.dupModule = d
}

func boundedReceiverContext(parent context.Context) (context.Context, context.CancelFunc) {
	if parent == nil {
		parent = context.Background()
	}
	return context.WithTimeout(parent, receiverDBTimeout)
}

// Name implements server.Module.
func (m *Module) Name() string { return "receiver" }

// Start implements server.Module.
func (m *Module) Start(_ context.Context) error {
	m.log.Info("Starting receiver module...")
	// Shared client for HTTP fallback proxy.
	// SafeHTTPTransport adds SSRF protection (rejects private/loopback IPs at dial time)
	// in addition to the connection-pooling and timeout settings.
	m.httpClient = &http.Client{Transport: helpers.SafeHTTPTransport()}

	m.slaveRepo = mysqlrepo.NewReceiverSlaveRepository(m.dbModule.GORM())
	m.mediaRepo = mysqlrepo.NewReceiverMediaRepository(m.dbModule.GORM())

	cfg := m.config.Get()
	if !cfg.Receiver.Enabled {
		m.log.Info("Receiver module is disabled")
		m.setHealth(true, "Disabled")
		return nil
	}

	maxConns := cfg.Receiver.MaxProxyConns
	if maxConns <= 0 {
		maxConns = 50
	}
	m.proxySem = make(chan struct{}, maxConns)

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
	}
	m.healthDoneOnce.Do(func() { close(m.healthDone) })
	// Close all WebSocket connections so slaves stop heartbeating and reconnect on next Start.
	m.wsMu.Lock()
	for _, sw := range m.wsConns {
		_ = sw.conn.Close()
	}
	m.wsConns = make(map[string]*slaveWS)
	m.wsMu.Unlock()
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
	slaveCtx, cancel := boundedReceiverContext(context.Background())
	slaveRecords, err := m.slaveRepo.List(slaveCtx)
	cancel()
	if err != nil {
		m.log.Warn("Failed to load slaves from DB: %v", err)
		return
	}

	loadedSlaves := make(map[string]*SlaveNode, len(slaveRecords))
	for _, rec := range slaveRecords {
		if rec != nil {
			loadedSlaves[rec.ID] = slaveRecordToNode(rec)
		}
	}

	mediaCtx, cancel := boundedReceiverContext(context.Background())
	mediaRecords, err := m.mediaRepo.ListAll(mediaCtx)
	cancel()
	if err != nil {
		m.log.Warn("Failed to load media from DB: %v", err)
		m.mu.Lock()
		m.slaves = loadedSlaves
		m.media = make(map[string]*MediaItem)
		m.mu.Unlock()
		m.setHealth(true, "Running (media cache empty — awaiting slave catalog push)")
		return
	}

	removedIDs := make(map[string]struct{})
	if m.dupModule != nil {
		tombstoneCtx, tombstoneCancel := boundedReceiverContext(context.Background())
		removedIDs, err = m.dupModule.RemovedReceiverItemIDs(tombstoneCtx, "")
		tombstoneCancel()
		if err != nil {
			// Fail closed: loading all rows when the durable-deletion set could not
			// be read would make previously removed media visible after restart.
			m.log.Warn("Failed to load receiver removal tombstones: %v", err)
			m.mu.Lock()
			m.slaves = loadedSlaves
			m.media = make(map[string]*MediaItem)
			m.mu.Unlock()
			m.setHealth(true, "Running (media cache withheld — removal history unavailable)")
			return
		}
	}

	type legacyMigration struct {
		legacyID string
		newID    string
		rec      *repositories.ReceiverMediaRecord
	}
	var migrations []legacyMigration
	var tombstonedRows []string
	loadedMedia := make(map[string]*MediaItem, len(mediaRecords))
	mediaCounts := make(map[string]int, len(loadedSlaves))

	for _, rec := range mediaRecords {
		func() {
			rowID := "<nil>"
			if rec != nil {
				rowID = rec.ID
			}
			defer func() {
				if r := recover(); r != nil {
					m.log.Warn("Skipping corrupt receiver media row (id=%q): %v", rowID, r)
				}
			}()
			if rec == nil {
				return
			}
			item := mediaRecordToItem(rec)
			if node, ok := loadedSlaves[rec.SlaveID]; ok {
				item.SlaveName = node.Name
			}

			// Migrate legacy "slaveID:itemID" composite keys to opaque IDs.
			id := rec.ID
			var migration *legacyMigration
			if strings.Contains(id, ":") {
				parts := strings.SplitN(id, ":", 2)
				if len(parts) == 2 {
					id = opaqueMediaID(parts[0], parts[1])
					item.ID = id
					migration = &legacyMigration{
						legacyID: rec.ID,
						newID:    id,
						rec:      rec,
					}
				}
			}
			if _, removed := removedIDs[id]; removed {
				tombstonedRows = append(tombstonedRows, rec.ID)
				return
			}
			if migration != nil {
				// Collect for async DB migration so we don't hold m.mu during I/O.
				migrations = append(migrations, *migration)
			}
			loadedMedia[id] = item
			mediaCounts[rec.SlaveID]++
		}()
	}

	type countRepair struct {
		record *repositories.ReceiverSlaveRecord
	}
	var countRepairs []countRepair
	for _, node := range loadedSlaves {
		count := mediaCounts[node.ID]
		if node.MediaCount != count {
			node.MediaCount = count
			countRepairs = append(countRepairs, countRepair{record: nodeToSlaveRecord(node)})
		}
	}

	m.mu.Lock()
	m.slaves = loadedSlaves
	m.media = loadedMedia
	m.mu.Unlock()

	m.log.Info("Loaded %d slaves, %d media items from DB", len(loadedSlaves), len(loadedMedia))

	// Persist migrated IDs: upsert the opaque-ID row FIRST, then delete the stale
	// composite-key row. This ordering ensures the new row exists before the old is
	// removed, so a crash between the two operations never loses the record.
	if len(migrations) > 0 || len(tombstonedRows) > 0 || len(countRepairs) > 0 {
		migs := migrations
		staleRows := tombstonedRows
		repairs := countRepairs
		go func() {
			for _, mig := range migs {
				migCtx, migCancel := boundedReceiverContext(context.Background())
				newRec := *mig.rec
				newRec.ID = mig.newID
				if err := m.mediaRepo.UpsertBatch(migCtx, newRec.SlaveID, []*repositories.ReceiverMediaRecord{&newRec}); err != nil {
					m.log.Warn("Legacy media ID migration: failed to upsert %s: %v", mig.newID, err)
					migCancel()
					continue
				}
				if err := m.mediaRepo.DeleteByID(migCtx, mig.legacyID); err != nil {
					m.log.Warn("Legacy media ID migration: failed to delete %s: %v", mig.legacyID, err)
				}
				migCancel()
			}
			if len(migs) > 0 {
				m.log.Info("Migrated %d legacy composite receiver media IDs to opaque IDs", len(migs))
			}
			for _, id := range staleRows {
				cleanupCtx, cleanupCancel := boundedReceiverContext(context.Background())
				if err := m.mediaRepo.DeleteByID(cleanupCtx, id); err != nil {
					m.log.Warn("Failed to clean tombstoned receiver media row %s: %v", id, err)
				}
				cleanupCancel()
			}
			for _, repair := range repairs {
				repairCtx, repairCancel := boundedReceiverContext(context.Background())
				if err := m.slaveRepo.Upsert(repairCtx, repair.record); err != nil {
					m.log.Warn("Failed to repair slave media count for %s: %v", repair.record.ID, err)
				}
				repairCancel()
			}
		}()
	}
}

func (m *Module) healthCheckLoop() {
	defer func() {
		if r := recover(); r != nil {
			m.log.Error("healthCheckLoop panicked: %v", r)
		}
	}()
	for {
		select {
		case <-m.healthTicker.C:
			m.markStaleSlaves()
			m.cleanupStalePending()
		case <-m.healthDone:
			return
		}
	}
}

// markStaleSlaves sets slaves to "offline" if their last heartbeat is overdue.
func (m *Module) markStaleSlaves() {
	m.slaveWriteMu.Lock()
	defer m.slaveWriteMu.Unlock()
	cfg := m.config.Get()
	threshold := cfg.Receiver.HealthCheck * 3
	if threshold == 0 {
		threshold = 90 * time.Second
	}

	m.mu.Lock()
	var toUpdate []*repositories.ReceiverSlaveRecord
	for _, node := range m.slaves {
		if node.Status == "online" && time.Since(node.LastSeen) > threshold {
			node.Status = "offline"
			toUpdate = append(toUpdate, nodeToSlaveRecord(node))
		}
	}
	m.mu.Unlock()

	// Perform DB writes outside the global lock. Holding m.mu while calling
	// slaveRepo.Upsert blocks all media serving (GetAllMedia, ProxyStream,
	// Heartbeat) for the duration of each DB round-trip, with no timeout.
	for _, rec := range toUpdate {
		dbCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		if err := m.slaveRepo.Upsert(dbCtx, rec); err != nil {
			m.log.Warn("Health check: failed to update slave %s: %v", rec.ID, err)
		}
		cancel()
	}
}

// ValidateAPIKey reports whether the provided key is in the configured API key list.
// Uses constant-time comparison to prevent timing side-channel attacks.
func (m *Module) ValidateAPIKey(key string) bool {
	if key == "" {
		return false
	}
	for _, k := range m.config.Get().Receiver.APIKeys {
		if subtle.ConstantTimeCompare([]byte(k), []byte(key)) == 1 {
			return true
		}
	}
	return false
}

// RegisterSlave registers a new slave node or updates an existing one.
// FND-0239: ctx bounds the DB Upsert so a hung database cannot block the
// caller (notably the WebSocket read loop) indefinitely.
func (m *Module) RegisterSlave(ctx context.Context, req *RegisterRequest) (*SlaveNode, error) {
	if req.Name == "" || req.BaseURL == "" {
		return nil, fmt.Errorf("name and base_url are required")
	}

	if req.BaseURL != "ws-connected" {
		u, err := url.Parse(req.BaseURL)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return nil, fmt.Errorf("base_url must be a valid http(s) URL")
		}
		if err := helpers.ValidateURLForSSRF(req.BaseURL); err != nil {
			return nil, fmt.Errorf("base_url rejected: %w", err)
		}
	}

	slaveID := req.SlaveID
	if slaveID == "" {
		slaveID = uuid.New().String()
	}

	m.slaveWriteMu.Lock()
	defer m.slaveWriteMu.Unlock()
	dbCtx, cancel := boundedReceiverContext(ctx)
	defer cancel()

	m.mu.RLock()
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
	m.mu.RUnlock()

	rec := nodeToSlaveRecord(node)
	if err := m.slaveRepo.Upsert(dbCtx, rec); err != nil {
		return nil, fmt.Errorf("failed to persist slave: %w", err)
	}
	m.mu.Lock()
	m.slaves[slaveID] = node
	m.mu.Unlock()

	m.log.Info("Slave registered: %s (%s) at %s", node.Name, node.ID, node.BaseURL)
	return node, nil
}

// maxCatalogItems is the upper bound on items accepted in a single catalog push.
// Prevents memory exhaustion from a misbehaving or compromised slave.
const maxCatalogItems = 100_000

// maxCatalogPayloadBytes caps the raw JSON byte size of a catalog message before
// it is unmarshaled, so a crafted payload cannot drive large allocations during decode.
// Sized for ~100k items at ~640 bytes each (FND-0236).
const maxCatalogPayloadBytes = 64 * 1024 * 1024

const errSlaveNotFound = "slave not found: %s"

// PushCatalog updates the slave's media catalog.
// If req.Full is true, the existing catalog for this slave is replaced entirely.
func (m *Module) PushCatalog(ctx context.Context, req *CatalogPushRequest) (int, error) {
	if req.SlaveID == "" {
		return 0, fmt.Errorf("slave_id is required")
	}

	if len(req.Items) > maxCatalogItems {
		return 0, fmt.Errorf("catalog too large: %d items (max %d)", len(req.Items), maxCatalogItems)
	}
	m.slaveWriteMu.Lock()
	defer m.slaveWriteMu.Unlock()
	dbCtx, cancel := boundedReceiverContext(ctx)
	defer cancel()

	var exists bool
	m.mu.RLock()
	_, exists = m.slaves[req.SlaveID]
	m.mu.RUnlock()
	if !exists {
		return 0, fmt.Errorf(errSlaveNotFound, req.SlaveID)
	}

	// Build DB records — validate slave-supplied paths to prevent path-traversal
	// or SSRF when the master uses the path in downstream HTTP/proxy requests.
	records := make([]*repositories.ReceiverMediaRecord, 0, len(req.Items))
	for i, item := range req.Items {
		// A slave can send {"items":[null]}; encoding/json decodes JSON null into a
		// nil *CatalogItem, so guard before dereferencing any field below.
		if item == nil {
			m.log.Warn("Slave %s: rejected nil catalog item %d", req.SlaveID, i)
			continue
		}
		// Reject paths containing ".." segments or absolute paths that could be
		// used to escape the slave's media directory in proxy requests.
		if strings.Contains(item.Path, "..") || strings.HasPrefix(item.Path, "/") || strings.HasPrefix(item.Path, "\\") {
			m.log.Warn("Slave %s: rejected catalog item %d with suspicious path %q", req.SlaveID, i, item.Path)
			continue
		}
		_ = i // index used only for logging above
		records = append(records, &repositories.ReceiverMediaRecord{
			ID:                 opaqueMediaID(req.SlaveID, item.ID),
			SlaveID:            req.SlaveID,
			RemoteID:           item.ID,
			RemotePath:         item.Path,
			Name:               item.Name,
			MediaType:          item.MediaType,
			Size:               item.Size,
			Duration:           item.Duration,
			ContentType:        item.ContentType,
			ContentFingerprint: item.ContentFingerprint,
			Width:              item.Width,
			Height:             item.Height,
			Category:           item.Category,
			Tags:               strings.Join(item.Tags, ","),
			BlurHash:           item.BlurHash,
			DateAdded:          item.DateAdded,
			DateModified:       item.DateModified,
			IsMature:           item.IsMature,
		})
	}

	// Apply exact durable tombstones before either a full replacement or an
	// incremental upsert. If the lookup fails, fail closed: accepting an
	// unfiltered catalog could resurrect an item the administrator removed.
	if m.dupModule != nil && len(records) > 0 {
		removedIDs, err := m.dupModule.RemovedReceiverItemIDs(dbCtx, req.SlaveID)
		if err != nil {
			return 0, fmt.Errorf("failed to load resolved receiver removals: %w", err)
		}
		if len(removedIDs) > 0 {
			filtered := records[:0]
			for _, rec := range records {
				if _, removed := removedIDs[rec.ID]; removed {
					continue
				}
				filtered = append(filtered, rec)
			}
			records = filtered
		}
	}

	if req.Full {
		// Full replacement: atomically delete existing catalog and insert the new one
		// inside a single transaction so a crash between the two operations cannot
		// permanently empty the slave's catalog.
		if err := m.mediaRepo.ReplaceSlaveMedia(dbCtx, req.SlaveID, records); err != nil {
			return 0, fmt.Errorf("failed to replace catalog: %w", err)
		}
		// Clear pending duplicate records for this slave so the fresh catalog
		// is re-evaluated — resolved admin decisions are preserved.
		if m.dupModule != nil {
			if err := m.dupModule.ClearPendingForSlave(dbCtx, req.SlaveID); err != nil {
				m.log.Warn("Failed to clear stale pending duplicates for slave %s: %v", req.SlaveID, err)
			}
		}
	} else if len(records) > 0 {
		if err := m.mediaRepo.UpsertBatch(dbCtx, req.SlaveID, records); err != nil {
			return 0, fmt.Errorf("failed to persist catalog: %w", err)
		}
	}

	// Rebuild in-memory media for this slave.
	// Re-read node under write lock to avoid TOCTOU if UnregisterSlave ran during DB I/O.
	m.mu.Lock()
	node, exists := m.slaves[req.SlaveID]
	if !exists {
		m.mu.Unlock()
		return len(records), nil // slave unregistered during DB I/O; media persisted, skip cache
	}
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
	// For full pushes, use len(records) (complete replacement).
	// For incremental pushes, count all media for this slave in the map
	// since the batch was merged, not replaced.
	if req.Full {
		node.MediaCount = len(records)
	} else {
		count := 0
		for _, item := range m.media {
			if item.SlaveID == req.SlaveID {
				count++
			}
		}
		node.MediaCount = count
	}
	node.Status = "online"
	node.LastSeen = time.Now()
	// Snapshot the record while holding the lock. nodeToSlaveRecord must not be
	// called after Unlock because Heartbeat may concurrently mutate node.Status
	// and node.LastSeen under m.mu, causing a data race on the same struct fields.
	slaveRecord := nodeToSlaveRecord(node)
	m.mu.Unlock()

	// Update slave record in DB (outside the lock — no shared state accessed here)
	if err := m.slaveRepo.Upsert(dbCtx, slaveRecord); err != nil {
		m.log.Warn("Failed to update slave count after catalog push: %v", err)
	}

	m.log.Info("Catalog updated: %d items from slave %s", len(records), req.SlaveID)

	// Detect synchronously while slaveWriteMu still fences this catalog version.
	// A detached goroutine could run after a newer full push or unregister and
	// recreate pending records for media that no longer exists.
	if m.dupModule != nil {
		refs := make([]duplicates.ReceiverItemRef, len(records))
		for i, rec := range records {
			refs[i] = duplicates.ReceiverItemRef{
				OpaqueID:           rec.ID,
				Name:               rec.Name,
				ContentFingerprint: rec.ContentFingerprint,
			}
		}
		if err := m.dupModule.RecordDuplicatesFromSlave(dbCtx, req.SlaveID, refs); err != nil {
			m.log.Warn("Failed to record receiver duplicates for slave %s: %v", req.SlaveID, err)
		}
	}

	return len(records), nil
}

// Heartbeat updates the slave's last-seen timestamp.
// The in-memory timestamp is always updated, but the DB write is debounced:
// we only persist when the last DB write for this slave is older than 60 seconds.
// This avoids a DB UPSERT on every 15-second heartbeat while keeping the
// in-memory state accurate for the stale-slave detector.
func (m *Module) Heartbeat(slaveID string) error {
	m.slaveWriteMu.Lock()
	defer m.slaveWriteMu.Unlock()
	m.mu.Lock()
	node, exists := m.slaves[slaveID]
	if !exists {
		m.mu.Unlock()
		return fmt.Errorf(errSlaveNotFound, slaveID)
	}
	prevLastSeen := node.LastSeen
	node.Status = "online"
	node.LastSeen = time.Now()
	// Snapshot the record under the lock so Upsert uses consistent state if we persist.
	record := nodeToSlaveRecord(node)
	m.mu.Unlock()

	debounce := m.config.Get().Receiver.HeartbeatDBDebounce
	if debounce <= 0 {
		debounce = 60 * time.Second
	}
	if time.Since(prevLastSeen) < debounce {
		return nil
	}
	// Bound the DB write so a slow/hung database can't block the WS read loop
	// (the sole caller, wsconn.go) indefinitely — mirrors RegisterSlave (FND-0239)
	// and markStaleSlaves, which bound the identical Upsert for the same reason.
	dbCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return m.slaveRepo.Upsert(dbCtx, record)
}

// UnregisterSlave removes a slave and its entire media catalog.
func (m *Module) UnregisterSlave(slaveID string) error {
	removed := false
	err := func() error {
		m.slaveWriteMu.Lock()
		defer m.slaveWriteMu.Unlock()
		dbCtx, cancel := boundedReceiverContext(context.Background())
		defer cancel()

		m.mu.RLock()
		_, exists := m.slaves[slaveID]
		m.mu.RUnlock()
		if !exists {
			// A previous attempt may have completed the authoritative deletes but
			// failed while clearing duplicate history. Make that cleanup retryable.
			if m.dupModule != nil {
				if err := m.dupModule.ClearForSlave(dbCtx, slaveID); err != nil {
					return fmt.Errorf("failed to clear duplicate history: %w", err)
				}
			}
			return fmt.Errorf(errSlaveNotFound, slaveID)
		}

		// Delete the DB rows BEFORE mutating in-memory state. Clearing memory first
		// meant a failed DB delete left the caches empty while the rows persisted, so
		// the slave reappeared as a phantom on the next restart. Media rows are deleted
		// before the slave row: if the second delete fails the residue is a benign empty
		// slave (reloads as an empty node, retryable) rather than orphan media rows
		// pointing at a slave that no longer exists.
		if err := m.mediaRepo.DeleteBySlave(dbCtx, slaveID); err != nil {
			return fmt.Errorf("failed to remove slave media: %w", err)
		}
		if err := m.slaveRepo.Delete(dbCtx, slaveID); err != nil {
			return fmt.Errorf("failed to remove slave: %w", err)
		}

		// The DB is authoritative now; drop the slave and its media from the caches.
		m.mu.Lock()
		delete(m.slaves, slaveID)
		for id, item := range m.media {
			if item.SlaveID == slaveID {
				delete(m.media, id)
			}
		}
		m.mu.Unlock()
		removed = true

		// Remove all duplicate records for this slave (any status) — the slave is gone permanently.
		if m.dupModule != nil {
			if err := m.dupModule.ClearForSlave(dbCtx, slaveID); err != nil {
				return fmt.Errorf("failed to clear duplicate history: %w", err)
			}
		}
		return nil
	}()

	// Close the slave's live WebSocket if it still has one, so its read loop and
	// 25s ping goroutine exit via their existing deferred cleanup instead of
	// pinging a slave that was just removed. Closing the conn makes ReadMessage
	// error out, which triggers removeSlaveWS — the same teardown setSlaveWS uses
	// when replacing a reconnecting slave's old connection.
	if removed {
		if sw := m.getSlaveWS(slaveID); sw != nil {
			_ = sw.conn.Close()
		}
	}
	return err
}

// GetSlaves returns all registered slave nodes as value copies. Unlike media
// items (which are always replaced wholesale on update), SlaveNode instances are
// mutated in place under m.mu — Heartbeat/markStaleSlaves/PushCatalog write
// Status/LastSeen/MediaCount on the stored struct. Returning the live pointers
// would let callers (e.g. AdminReceiverListSlaves -> c.JSON) read those fields
// with no lock held, racing the mutating goroutines. Copy under the RLock so the
// caller gets a stable snapshot.
func (m *Module) GetSlaves() []*SlaveNode {
	m.mu.RLock()
	defer m.mu.RUnlock()
	nodes := make([]*SlaveNode, 0, len(m.slaves))
	for _, n := range m.slaves {
		cp := *n
		nodes = append(nodes, &cp)
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

// RemoveMediaItem durably resolves and removes one receiver item under the same
// serialization used by catalog pushes. The tombstone is persisted first, so a
// failed media delete or process crash cannot let a later catalog replay restore
// the item. Cache eviction and the owning slave's MediaCount are then committed
// and the corrected count is persisted.
func (m *Module) RemoveMediaItem(ctx context.Context, itemID, slaveID string, persistTombstone func(context.Context) error) error {
	if itemID == "" || slaveID == "" {
		return fmt.Errorf("item_id and slave_id are required")
	}
	if persistTombstone == nil {
		return fmt.Errorf("receiver removal requires a durable tombstone")
	}

	m.slaveWriteMu.Lock()
	defer m.slaveWriteMu.Unlock()
	dbCtx, cancel := boundedReceiverContext(ctx)
	defer cancel()

	if err := persistTombstone(dbCtx); err != nil {
		return fmt.Errorf("failed to persist receiver removal tombstone: %w", err)
	}
	if err := m.mediaRepo.DeleteByID(dbCtx, itemID); err != nil {
		return fmt.Errorf("failed to delete receiver media: %w", err)
	}

	var slaveRecord *repositories.ReceiverSlaveRecord
	m.mu.Lock()
	delete(m.media, itemID)
	count := 0
	for _, item := range m.media {
		if item.SlaveID == slaveID {
			count++
		}
	}
	if node, ok := m.slaves[slaveID]; ok {
		node.MediaCount = count
		slaveRecord = nodeToSlaveRecord(node)
	}
	m.mu.Unlock()

	if slaveRecord != nil {
		if err := m.slaveRepo.Upsert(dbCtx, slaveRecord); err != nil {
			return fmt.Errorf("failed to persist slave media count: %w", err)
		}
	}
	return nil
}

// GetStats returns a summary of the receiver module state.
func (m *Module) GetStats() Stats {
	m.mu.RLock()
	stats := Stats{
		SlaveCount: len(m.slaves),
		MediaCount: len(m.media),
	}
	for _, node := range m.slaves {
		if node.Status == "online" {
			stats.OnlineCount++
		}
	}
	m.mu.RUnlock()

	if m.dupModule != nil {
		stats.DuplicateCount = m.dupModule.CountPending()
	}
	return stats
}

// ProxyStream streams media from a slave to the client.
// It first attempts a WebSocket-based request (slave pushes data back via HTTP
// POST).  If the slave has no active WebSocket connection, it falls back to a
// direct HTTP proxy through the slave's BaseURL.
func (m *Module) ProxyStream(w http.ResponseWriter, r *http.Request, mediaID string) error {
	// Enforce MaxProxyConns limit via a buffered channel semaphore.
	if m.proxySem != nil {
		select {
		case m.proxySem <- struct{}{}:
			defer func() { <-m.proxySem }()
		default:
			http.Error(w, "Too many concurrent proxy connections", http.StatusServiceUnavailable)
			return nil
		}
	}

	item := m.GetMediaItem(mediaID)
	if item == nil {
		http.NotFound(w, r)
		return nil
	}

	m.mu.RLock()
	_, exists := m.slaves[item.SlaveID]
	m.mu.RUnlock()
	if !exists {
		return fmt.Errorf("slave not found for media %s", mediaID)
	}

	// Try WebSocket-based streaming first (slave pushes data).
	if m.getSlaveWS(item.SlaveID) != nil {
		err := m.proxyViaWS(w, r, item, mediaID)
		if err == nil {
			return nil
		}
		m.log.Warn("WS stream failed for %s, trying HTTP fallback: %v", mediaID, err)
	}

	// Re-read slave under lock before fallback — it may have been removed since the initial read.
	m.mu.RLock()
	slave, exists := m.slaves[item.SlaveID]
	m.mu.RUnlock()
	if !exists {
		return fmt.Errorf("slave no longer registered for media %s", mediaID)
	}
	return m.proxyViaHTTP(w, r, slave, item)
}

// proxyViaWS uses the WebSocket-based push protocol to stream media.
func (m *Module) proxyViaWS(w http.ResponseWriter, r *http.Request, item *MediaItem, mediaID string) error {
	token := uuid.New().String()
	rangeHeader := r.Header.Get("Range")

	ps, err := m.RequestStream(item.SlaveID, token, item.Path, rangeHeader)
	if err != nil {
		return fmt.Errorf("failed to request stream: %w", err)
	}
	// Always cancel ps so ReceiverStreamPush's watcher goroutine can unblock.
	defer ps.cancel()

	cfg := m.config.Get()
	timeout := cfg.Receiver.ProxyTimeout
	if timeout == 0 {
		timeout = defaultProxyTimeout
	}

	select {
	case delivery, ok := <-ps.Ready:
		if !ok || delivery == nil {
			return fmt.Errorf("stream delivery failed for %s", mediaID)
		}
		for key, values := range delivery.Headers {
			if !helpers.AllowedProxyHeaders[key] {
				continue
			}
			for _, v := range values {
				w.Header().Add(key, v)
			}
		}
		w.WriteHeader(delivery.StatusCode)
		_, copyErr := io.Copy(w, delivery.Body)
		_ = delivery.Body.Close()
		return copyErr

	case <-time.After(timeout):
		m.pendingMu.Lock()
		delete(m.pendingStreams, token)
		m.pendingMu.Unlock()
		select {
		case d := <-ps.Ready:
			if d != nil && d.Body != nil {
				_ = d.Body.Close()
			}
		default:
		}
		return fmt.Errorf("stream request timed out for %s", mediaID)

	case <-r.Context().Done():
		m.pendingMu.Lock()
		delete(m.pendingStreams, token)
		m.pendingMu.Unlock()
		select {
		case d := <-ps.Ready:
			if d != nil && d.Body != nil {
				_ = d.Body.Close()
			}
		default:
		}
		return r.Context().Err()
	}
}

// ProxyThumbnail asks the slave for the thumbnail of one of its catalog items
// and pipes the bytes back to w. Returns an error if the slave is offline,
// the request times out, or the thumbnail does not exist on the slave (in
// which case the caller can fall through to a placeholder).
func (m *Module) ProxyThumbnail(w http.ResponseWriter, r *http.Request, mediaID string, preferWebP bool) error {
	if m.proxySem != nil {
		select {
		case m.proxySem <- struct{}{}:
			defer func() { <-m.proxySem }()
		default:
			return fmt.Errorf("too many concurrent proxy connections")
		}
	}

	item := m.GetMediaItem(mediaID)
	if item == nil {
		return fmt.Errorf("receiver media not found: %s", mediaID)
	}
	if item.RemoteID == "" {
		// Pre-RemoteID catalog: cannot resolve thumbnail on slave side.
		// Caller should fall through to placeholder.
		return fmt.Errorf("receiver item has no remote_id; slave catalog needs re-push")
	}
	if m.getSlaveWS(item.SlaveID) == nil {
		return fmt.Errorf("slave %s is not connected", item.SlaveID)
	}

	token := uuid.New().String()
	ps, err := m.RequestThumbnail(item.SlaveID, token, item.RemoteID, preferWebP)
	if err != nil {
		return fmt.Errorf("failed to request thumbnail: %w", err)
	}
	defer ps.cancel()

	cfg := m.config.Get()
	timeout := cfg.Receiver.ProxyTimeout
	if timeout == 0 {
		timeout = defaultProxyTimeout
	}

	select {
	case delivery, ok := <-ps.Ready:
		if !ok || delivery == nil {
			return fmt.Errorf("thumbnail delivery failed for %s", mediaID)
		}
		if delivery.StatusCode >= 400 {
			if delivery.Body != nil {
				_ = delivery.Body.Close()
			}
			return fmt.Errorf("slave returned status %d for thumbnail", delivery.StatusCode)
		}
		for key, values := range delivery.Headers {
			if !helpers.AllowedProxyHeaders[key] {
				continue
			}
			for _, v := range values {
				w.Header().Add(key, v)
			}
		}
		w.WriteHeader(delivery.StatusCode)
		_, copyErr := io.Copy(w, delivery.Body)
		_ = delivery.Body.Close()
		return copyErr
	case <-time.After(timeout):
		m.pendingMu.Lock()
		delete(m.pendingStreams, token)
		m.pendingMu.Unlock()
		select {
		case d := <-ps.Ready:
			if d != nil && d.Body != nil {
				_ = d.Body.Close()
			}
		default:
		}
		return fmt.Errorf("thumbnail request timed out for %s", mediaID)
	case <-r.Context().Done():
		m.pendingMu.Lock()
		delete(m.pendingStreams, token)
		m.pendingMu.Unlock()
		return r.Context().Err()
	}
}

// proxyViaHTTP fetches the media from the slave's HTTP endpoint and relays it
// to the client.  This is the fallback when the slave has no active WebSocket.
func (m *Module) proxyViaHTTP(w http.ResponseWriter, r *http.Request, slave *SlaveNode, item *MediaItem) error {
	baseURL := slave.BaseURL
	if baseURL == "" || baseURL == "ws-connected" {
		return fmt.Errorf("slave %s has no HTTP base URL for fallback", item.SlaveID)
	}

	// Build the upstream request to the slave's media endpoint (path is query-encoded).
	targetURL := strings.TrimRight(baseURL, "/") + "/media?path=" + url.QueryEscape(item.Path)
	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, targetURL, http.NoBody)
	if err != nil {
		return fmt.Errorf("failed to build proxy request: %w", err)
	}

	// Forward range header for seeking support.
	if rh := r.Header.Get("Range"); rh != "" {
		req.Header.Set("Range", rh)
	}
	req.Header.Set("User-Agent", "MediaServerPro-Receiver/1.0")

	cfg := m.config.Get()
	timeout := cfg.Receiver.ProxyTimeout
	if timeout == 0 {
		timeout = defaultProxyTimeout
	}
	// Use request context with timeout so client cancellation and config timeout both apply.
	ctx, cancel := context.WithTimeout(r.Context(), timeout)
	defer cancel()
	req = req.WithContext(ctx)

	resp, err := m.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("HTTP proxy to slave failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	// Forward allowed headers only.
	for key, values := range resp.Header {
		if !helpers.AllowedProxyHeaders[key] {
			continue
		}
		for _, v := range values {
			w.Header().Add(key, v)
		}
	}
	w.WriteHeader(resp.StatusCode)
	_, copyErr := io.Copy(w, resp.Body)
	return copyErr
}

// mediaFetchReader wraps a federated item's byte stream so Close releases the
// proxy-connection slot (and, on the WS path, cancels the pending-stream
// context). Close is guarded by sync.Once so it is safe to call from both a
// defer and a cancellation watcher.
type mediaFetchReader struct {
	body    io.ReadCloser
	release func()
	once    sync.Once
}

func (r *mediaFetchReader) Read(p []byte) (int, error) { return r.body.Read(p) }

func (r *mediaFetchReader) Close() error {
	err := r.body.Close()
	r.once.Do(r.release)
	return err
}

// FetchMedia opens the full byte stream of a federated (slave) media item for
// server-side consumption — e.g. copying it into the local library so it
// survives the peer disconnecting. Unlike ProxyStream it returns an
// io.ReadCloser rather than writing to an http.ResponseWriter. No Range is
// requested, so the whole file is fetched.
//
// The caller MUST Close the returned reader; Close releases the
// MaxProxyConns slot (and the WS pending-stream context). It mirrors
// ProxyStream's resolution order: WebSocket push first (works even when the
// slave has no reachable BaseURL, e.g. a follower that dialed out), HTTP GET to
// the slave's BaseURL as a fallback.
func (m *Module) FetchMedia(ctx context.Context, mediaID string) (io.ReadCloser, *MediaItem, error) {
	item := m.GetMediaItem(mediaID)
	if item == nil {
		return nil, nil, fmt.Errorf("receiver media not found: %s", mediaID)
	}

	// Enforce MaxProxyConns like ProxyStream; the slot is held until the caller
	// closes the returned reader.
	if m.proxySem != nil {
		select {
		case m.proxySem <- struct{}{}:
		default:
			return nil, nil, fmt.Errorf("too many concurrent proxy connections")
		}
	}
	releaseSem := func() {
		if m.proxySem != nil {
			<-m.proxySem
		}
	}

	m.mu.RLock()
	_, exists := m.slaves[item.SlaveID]
	m.mu.RUnlock()
	if !exists {
		releaseSem()
		return nil, nil, fmt.Errorf("slave not found for media %s", mediaID)
	}

	if m.getSlaveWS(item.SlaveID) != nil {
		body, cancel, err := m.fetchViaWS(ctx, item)
		if err == nil {
			return &mediaFetchReader{body: body, release: func() { cancel(); releaseSem() }}, item, nil
		}
		m.log.Warn("WS fetch failed for %s, trying HTTP fallback: %v", mediaID, err)
	}

	// Re-read slave under lock before fallback — it may have been removed since.
	m.mu.RLock()
	slave, exists := m.slaves[item.SlaveID]
	m.mu.RUnlock()
	if !exists {
		releaseSem()
		return nil, nil, fmt.Errorf("slave no longer registered for media %s", mediaID)
	}
	body, err := m.fetchViaHTTP(ctx, slave, item)
	if err != nil {
		releaseSem()
		return nil, nil, err
	}
	return &mediaFetchReader{body: body, release: releaseSem}, item, nil
}

// fetchViaWS requests the whole file from a connected slave over WebSocket and
// returns the delivery body plus the pending-stream cancel func to invoke when
// the caller is done reading. The cancel must NOT be called before the read
// completes: ps.ctx is the consumer context, and canceling it early makes the
// push handler tear the pipe down (see DeliverStream).
func (m *Module) fetchViaWS(ctx context.Context, item *MediaItem) (io.ReadCloser, context.CancelFunc, error) {
	token := uuid.New().String()
	ps, err := m.RequestStream(item.SlaveID, token, item.Path, "")
	if err != nil {
		return nil, nil, fmt.Errorf("failed to request stream: %w", err)
	}

	cfg := m.config.Get()
	timeout := cfg.Receiver.ProxyTimeout
	if timeout == 0 {
		timeout = defaultProxyTimeout
	}

	select {
	case delivery, ok := <-ps.Ready:
		if !ok || delivery == nil {
			ps.cancel()
			return nil, nil, fmt.Errorf("stream delivery failed for %s", item.ID)
		}
		if delivery.StatusCode >= 400 {
			if delivery.Body != nil {
				_ = delivery.Body.Close()
			}
			ps.cancel()
			return nil, nil, fmt.Errorf("slave returned status %d for %s", delivery.StatusCode, item.ID)
		}
		return delivery.Body, ps.cancel, nil
	case <-time.After(timeout):
		m.pendingMu.Lock()
		delete(m.pendingStreams, token)
		m.pendingMu.Unlock()
		ps.cancel()
		return nil, nil, fmt.Errorf("stream request timed out for %s", item.ID)
	case <-ctx.Done():
		m.pendingMu.Lock()
		delete(m.pendingStreams, token)
		m.pendingMu.Unlock()
		ps.cancel()
		return nil, nil, ctx.Err()
	}
}

// fetchViaHTTP downloads the whole file from the slave's HTTP endpoint. Fallback
// for when the slave has no live WebSocket. The caller must Close the body.
// Unlike proxyViaHTTP it applies no per-request timeout — a full-file copy can
// legitimately run long; ctx (the caller's request context) governs cancellation.
func (m *Module) fetchViaHTTP(ctx context.Context, slave *SlaveNode, item *MediaItem) (io.ReadCloser, error) {
	baseURL := slave.BaseURL
	if baseURL == "" || baseURL == "ws-connected" {
		return nil, fmt.Errorf("slave %s has no HTTP base URL for fallback", item.SlaveID)
	}
	targetURL := strings.TrimRight(baseURL, "/") + "/media?path=" + url.QueryEscape(item.Path)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, targetURL, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("failed to build fetch request: %w", err)
	}
	req.Header.Set("User-Agent", "MediaServerPro-Receiver/1.0")

	resp, err := m.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("HTTP fetch from slave failed: %w", err)
	}
	if resp.StatusCode >= 400 {
		_ = resp.Body.Close()
		return nil, fmt.Errorf("slave returned status %d for %s", resp.StatusCode, item.ID)
	}
	return resp.Body, nil
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
	var tags []string
	if rec.Tags != "" {
		for t := range strings.SplitSeq(rec.Tags, ",") {
			if v := strings.TrimSpace(t); v != "" {
				tags = append(tags, v)
			}
		}
	}
	return &MediaItem{
		ID:                 rec.ID,
		SlaveID:            rec.SlaveID,
		RemoteID:           rec.RemoteID,
		Path:               rec.RemotePath,
		Name:               rec.Name,
		MediaType:          rec.MediaType,
		Size:               rec.Size,
		Duration:           rec.Duration,
		ContentType:        rec.ContentType,
		ContentFingerprint: rec.ContentFingerprint,
		Width:              rec.Width,
		Height:             rec.Height,
		Category:           rec.Category,
		Tags:               tags,
		BlurHash:           rec.BlurHash,
		DateAdded:          rec.DateAdded,
		DateModified:       rec.DateModified,
		IsMature:           rec.IsMature,
	}
}
