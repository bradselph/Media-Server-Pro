// Package duplicates detects and manages duplicate media items across both local
// media files and receiver slave catalogs.  It operates independently of the
// receiver module and can be enabled or disabled via FEATURE_DUPLICATE_DETECTION.
package duplicates

import (
	"context"
	"fmt"
	"path/filepath"
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

// DuplicateItem represents one side of a detected duplicate pair.
type DuplicateItem struct {
	ID      string `json:"id"`
	SlaveID string `json:"slave_id,omitempty"` // empty for local media
	Name    string `json:"name"`
	Source  string `json:"source"` // "local" or "receiver"
}

// DuplicateGroup represents a pair of media items sharing the same content fingerprint.
type DuplicateGroup struct {
	ID          string         `json:"id"`
	Fingerprint string         `json:"fingerprint"`
	ItemA       *DuplicateItem `json:"item_a"`
	ItemB       *DuplicateItem `json:"item_b"`
	ItemAName   string         `json:"item_a_name"`
	ItemBName   string         `json:"item_b_name"`
	Status      string         `json:"status"`
	ResolvedBy  string         `json:"resolved_by,omitempty"`
	ResolvedAt  *time.Time     `json:"resolved_at,omitempty"`
	DetectedAt  time.Time      `json:"detected_at"`
}

// ReceiverItemRef carries the fields the duplicates module needs from a
// receiver catalog item when checking for cross-slave fingerprint collisions.
type ReceiverItemRef struct {
	OpaqueID           string
	Name               string
	ContentFingerprint string
}

// Module manages detection and resolution of duplicate media items.
type Module struct {
	cfg          *config.Manager
	log          *logger.Logger
	dbModule     *database.Module
	dupRepo      repositories.ReceiverDuplicateRepository
	metaRepo     repositories.MediaMetadataRepository
	receiverRepo repositories.ReceiverMediaRepository
	healthMu     sync.RWMutex
	healthy      bool
	healthMsg    string
}

// NewModule creates a new duplicates module.
func NewModule(cfg *config.Manager, dbModule *database.Module) *Module {
	return &Module{
		cfg:      cfg,
		log:      logger.New("duplicates"),
		dbModule: dbModule,
	}
}

// Name implements server.Module.
func (m *Module) Name() string { return "duplicates" }

// Start implements server.Module.
func (m *Module) Start(_ context.Context) error {
	m.log.Info("Starting duplicates module...")

	db := m.dbModule.GORM()
	m.dupRepo = mysqlrepo.NewReceiverDuplicateRepository(db)
	m.metaRepo = mysqlrepo.NewMediaMetadataRepository(db)
	m.receiverRepo = mysqlrepo.NewReceiverMediaRepository(db)

	if !m.cfg.Get().Features.EnableDuplicateDetection {
		m.log.Info("Duplicate detection is disabled")
		m.setHealth(true, "Disabled")
		return nil
	}

	m.setHealth(true, "Running")
	m.log.Info("Duplicates module started")
	return nil
}

// Stop implements server.Module.
func (m *Module) Stop(_ context.Context) error {
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

// enabled reports whether the module is fully initialised and the feature flag is on.
func (m *Module) enabled() bool {
	return m.dupRepo != nil && m.cfg.Get().Features.EnableDuplicateDetection
}

// CountPending returns the number of unresolved duplicate pairs.
func (m *Module) CountPending() int {
	if !m.enabled() {
		return 0
	}
	count, _ := m.dupRepo.CountPending(context.Background())
	return int(count)
}

// RecordDuplicatesFromSlave compares newly-pushed slave items against the full
// receiver catalog and persists any new fingerprint collisions.  It is safe to
// call in a background goroutine.
func (m *Module) RecordDuplicatesFromSlave(slaveID string, items []ReceiverItemRef) {
	if !m.enabled() || m.receiverRepo == nil {
		return
	}
	ctx := context.Background()

	allRecs, err := m.receiverRepo.ListAll(ctx)
	if err != nil {
		m.log.Warn("RecordDuplicatesFromSlave: failed to list receiver media: %v", err)
		return
	}

	// Build fingerprint index of items NOT belonging to this slave.
	fpIndex := make(map[string][]*repositories.ReceiverMediaRecord)
	for _, rec := range allRecs {
		if rec.SlaveID == slaveID || rec.ContentFingerprint == "" {
			continue
		}
		fpIndex[rec.ContentFingerprint] = append(fpIndex[rec.ContentFingerprint], rec)
	}

	for _, item := range items {
		if item.ContentFingerprint == "" {
			continue
		}
		matches := fpIndex[item.ContentFingerprint]
		if len(matches) == 0 {
			continue
		}
		for _, existing := range matches {
			exists, err := m.dupRepo.ExistsByPair(ctx, item.OpaqueID, existing.ID)
			if err != nil {
				m.log.Warn("RecordDuplicatesFromSlave: pair check failed: %v", err)
				continue
			}
			if exists {
				continue
			}
			rec := &repositories.ReceiverDuplicateRecord{
				ID:           uuid.New().String(),
				Fingerprint:  item.ContentFingerprint,
				ItemAID:      item.OpaqueID,
				ItemASlaveID: slaveID,
				ItemAName:    item.Name,
				ItemBID:      existing.ID,
				ItemBSlaveID: existing.SlaveID,
				ItemBName:    existing.Name,
				Status:       "pending",
				DetectedAt:   time.Now(),
			}
			if err := m.dupRepo.Create(ctx, rec); err != nil {
				m.log.Warn("RecordDuplicatesFromSlave: failed to store record: %v", err)
			} else {
				m.log.Info("Receiver duplicate detected: %q (slave %s) ↔ %q (slave %s) [fp=%s…]",
					item.Name, slaveID, existing.Name, existing.SlaveID, item.ContentFingerprint[:8])
			}
		}
	}
}

// ScanLocalMedia queries media_metadata for fingerprint collisions among local
// files and persists any new pairs.  Intended to be run as a background task.
func (m *Module) ScanLocalMedia(ctx context.Context) error {
	if !m.enabled() || m.metaRepo == nil {
		return nil
	}

	all, err := m.metaRepo.List(ctx)
	if err != nil {
		return fmt.Errorf("failed to list media metadata: %w", err)
	}

	// Group stable IDs by fingerprint.
	type entry struct{ stableID, path string }
	fpGroups := make(map[string][]entry)
	for path, meta := range all {
		if meta.ContentFingerprint == "" || meta.StableID == "" {
			continue
		}
		fpGroups[meta.ContentFingerprint] = append(fpGroups[meta.ContentFingerprint], entry{meta.StableID, path})
	}

	for fp, group := range fpGroups {
		if len(group) < 2 {
			continue
		}
		for i := 0; i < len(group); i++ {
			for j := i + 1; j < len(group); j++ {
				a, b := group[i], group[j]
				exists, err := m.dupRepo.ExistsByPair(ctx, a.stableID, b.stableID)
				if err != nil {
					m.log.Warn("ScanLocalMedia: pair check failed: %v", err)
					continue
				}
				if exists {
					continue
				}
				rec := &repositories.ReceiverDuplicateRecord{
					ID:           uuid.New().String(),
					Fingerprint:  fp,
					ItemAID:      a.stableID,
					ItemASlaveID: "",
					ItemAName:    filepath.Base(a.path),
					ItemBID:      b.stableID,
					ItemBSlaveID: "",
					ItemBName:    filepath.Base(b.path),
					Status:       "pending",
					DetectedAt:   time.Now(),
				}
				if err := m.dupRepo.Create(ctx, rec); err != nil {
					m.log.Warn("ScanLocalMedia: failed to store duplicate: %v", err)
				} else {
					m.log.Info("Local duplicate detected: %q ↔ %q [fp=%s…]",
						filepath.Base(a.path), filepath.Base(b.path), fp[:8])
				}
			}
		}
	}
	return nil
}

// ListDuplicates returns duplicate groups.  Pass statusFilter="" or "pending" for
// unresolved pairs only; any other non-empty value returns all records.
func (m *Module) ListDuplicates(statusFilter string) ([]*DuplicateGroup, error) {
	if m.dupRepo == nil {
		return nil, nil
	}
	ctx := context.Background()
	var records []*repositories.ReceiverDuplicateRecord
	var err error
	if statusFilter == "" || statusFilter == "pending" {
		records, err = m.dupRepo.ListPending(ctx)
	} else {
		records, err = m.dupRepo.List(ctx)
	}
	if err != nil {
		return nil, err
	}

	groups := make([]*DuplicateGroup, 0, len(records))
	for _, rec := range records {
		g := &DuplicateGroup{
			ID:          rec.ID,
			Fingerprint: rec.Fingerprint,
			ItemAName:   rec.ItemAName,
			ItemBName:   rec.ItemBName,
			Status:      rec.Status,
			ResolvedBy:  rec.ResolvedBy,
			ResolvedAt:  rec.ResolvedAt,
			DetectedAt:  rec.DetectedAt,
			ItemA: &DuplicateItem{
				ID:      rec.ItemAID,
				SlaveID: rec.ItemASlaveID,
				Name:    rec.ItemAName,
				Source:  sourceFor(rec.ItemASlaveID),
			},
			ItemB: &DuplicateItem{
				ID:      rec.ItemBID,
				SlaveID: rec.ItemBSlaveID,
				Name:    rec.ItemBName,
				Source:  sourceFor(rec.ItemBSlaveID),
			},
		}
		groups = append(groups, g)
	}
	return groups, nil
}

func sourceFor(slaveID string) string {
	if slaveID == "" {
		return "local"
	}
	return "receiver"
}

// ResolveDuplicate acts on an admin decision for a detected duplicate pair.
// action must be one of: "remove_a", "remove_b", "keep_both", "ignore".
func (m *Module) ResolveDuplicate(id, action, resolvedBy string) error {
	if m.dupRepo == nil {
		return fmt.Errorf("duplicate detection is not available")
	}
	ctx := context.Background()

	rec, err := m.dupRepo.Get(ctx, id)
	if err != nil {
		return fmt.Errorf("failed to fetch duplicate: %w", err)
	}
	if rec == nil {
		return fmt.Errorf("duplicate not found: %s", id)
	}

	switch action {
	case "remove_a":
		m.removeItem(ctx, rec.ItemAID, rec.ItemASlaveID)
		return m.dupRepo.DeleteForItem(ctx, rec.ItemAID)
	case "remove_b":
		m.removeItem(ctx, rec.ItemBID, rec.ItemBSlaveID)
		return m.dupRepo.DeleteForItem(ctx, rec.ItemBID)
	case "keep_both", "ignore":
		return m.dupRepo.UpdateStatus(ctx, id, action, resolvedBy)
	default:
		return fmt.Errorf("unknown action %q — must be remove_a, remove_b, keep_both, or ignore", action)
	}
}

// removeItem deletes the item from the appropriate backing store.
// For receiver items it removes the row from receiver_media.
// For local items it removes the metadata row (the file on disk is untouched).
func (m *Module) removeItem(ctx context.Context, itemID, slaveID string) {
	if slaveID == "" {
		// Local media — find by stable ID, then delete by path.
		if m.metaRepo == nil {
			return
		}
		all, err := m.metaRepo.List(ctx)
		if err != nil {
			m.log.Warn("removeItem: failed to list metadata: %v", err)
			return
		}
		for path, meta := range all {
			if meta.StableID == itemID {
				if err := m.metaRepo.Delete(ctx, path); err != nil {
					m.log.Warn("removeItem: failed to delete local metadata for %s: %v", path, err)
				}
				return
			}
		}
		m.log.Warn("removeItem: local item %s not found in metadata", itemID)
	} else {
		// Receiver media — remove row from receiver_media.
		if m.receiverRepo == nil {
			return
		}
		if err := m.receiverRepo.DeleteByID(ctx, itemID); err != nil {
			m.log.Warn("removeItem: failed to delete receiver media %s: %v", itemID, err)
		}
	}
}
