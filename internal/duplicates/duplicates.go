// Package duplicates detects and manages duplicate media items across both local
// media files and receiver slave catalogs.  It operates independently of the
// receiver module and can be enabled or disabled via FEATURE_DUPLICATE_DETECTION.
package duplicates

import (
	"context"
	"fmt"
	"os"
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

// ClearForSlave removes all duplicate records (any status) involving the given slave.
// Call this when a slave is permanently unregistered so stale records are purged.
func (m *Module) ClearForSlave(slaveID string) {
	if m.dupRepo == nil {
		return
	}
	if err := m.dupRepo.DeleteBySlave(context.Background(), slaveID); err != nil {
		m.log.Warn("ClearForSlave: failed to delete duplicate records for slave %s: %v", slaveID, err)
	}
}

// ClearPendingForSlave removes only pending duplicate records involving the given slave.
// Call this on a full catalog replacement so the fresh catalog is re-evaluated while
// preserving resolved admin decisions (keep_both / ignore / remove_a / remove_b).
func (m *Module) ClearPendingForSlave(slaveID string) {
	if m.dupRepo == nil {
		return
	}
	if err := m.dupRepo.DeletePendingBySlave(context.Background(), slaveID); err != nil {
		m.log.Warn("ClearPendingForSlave: failed to delete pending duplicates for slave %s: %v", slaveID, err)
	}
}

// CountPending returns the number of unresolved duplicate pairs.
func (m *Module) CountPending() int {
	if !m.enabled() {
		return 0
	}
	count, err := m.dupRepo.CountPending(context.Background())
	if err != nil {
		m.log.Warn("CountPending: failed to query pending duplicates: %v", err)
		return 0
	}
	return int(count)
}

// buildReceiverFingerprintIndex builds a fingerprint -> records index for all
// receiver media excluding the given slave and entries without a fingerprint.
func buildReceiverFingerprintIndex(recs []*repositories.ReceiverMediaRecord, excludeSlaveID string) map[string][]*repositories.ReceiverMediaRecord {
	idx := make(map[string][]*repositories.ReceiverMediaRecord)
	for _, rec := range recs {
		if rec.SlaveID == excludeSlaveID || rec.ContentFingerprint == "" {
			continue
		}
		idx[rec.ContentFingerprint] = append(idx[rec.ContentFingerprint], rec)
	}
	return idx
}

// tryRecordReceiverPair checks if the item/existing pair should be recorded as a
// duplicate (pair not already recorded, no resolved removal for this fingerprint),
// and if so creates the record. Returns true if a record was created.
func (m *Module) tryRecordReceiverPair(ctx context.Context, slaveID string, item ReceiverItemRef, existing *repositories.ReceiverMediaRecord) bool {
	exists, err := m.dupRepo.ExistsByPair(ctx, item.OpaqueID, existing.ID)
	if err != nil {
		m.log.Warn("RecordDuplicatesFromSlave: pair check failed: %v", err)
		return false
	}
	if exists {
		return false
	}
	if resolved, _ := m.dupRepo.ExistsResolvedRemoval(ctx, item.ContentFingerprint); resolved {
		return false
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
		return false
	}
	fpPreview := item.ContentFingerprint
	if len(fpPreview) > 8 {
		fpPreview = fpPreview[:8]
	}
	m.log.Info("Receiver duplicate detected: %q (slave %s) ↔ %q (slave %s) [fp=%s…]",
		item.Name, slaveID, existing.Name, existing.SlaveID, fpPreview)
	return true
}

// RecordDuplicatesFromSlave compares newly-pushed slave items against the full receiver catalog (loads all receiver_media).
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

	fpIndex := buildReceiverFingerprintIndex(allRecs, slaveID)

	for _, item := range items {
		if item.ContentFingerprint == "" {
			continue
		}
		matches := fpIndex[item.ContentFingerprint]
		for _, existing := range matches {
			m.tryRecordReceiverPair(ctx, slaveID, item, existing)
		}
	}
}

// localFpEntry holds stableID and path for grouping local media by fingerprint.
type localFpEntry struct {
	stableID string
	path     string
}

// buildLocalFingerprintGroups groups metadata by content fingerprint, ignoring entries without fingerprint or stableID.
func buildLocalFingerprintGroups(all map[string]*repositories.MediaMetadata) map[string][]localFpEntry {
	fpGroups := make(map[string][]localFpEntry)
	for path, meta := range all {
		if meta.ContentFingerprint == "" || meta.StableID == "" {
			continue
		}
		fpGroups[meta.ContentFingerprint] = append(fpGroups[meta.ContentFingerprint], localFpEntry{meta.StableID, path})
	}
	return fpGroups
}

// localPairForRecord groups a fingerprint and two entries for duplicate recording.
type localPairForRecord struct {
	fp string
	a  localFpEntry
	b  localFpEntry
}

// tryRecordLocalPair checks whether the pair should be recorded as a duplicate, and if so creates the record.
// resolvedFPs is updated with per-fingerprint resolved-removal cache. Returns true if a record was created.
func (m *Module) tryRecordLocalPair(ctx context.Context, pair localPairForRecord, resolvedFPs map[string]bool) bool {
	fp, a, b := pair.fp, pair.a, pair.b
	exists, err := m.dupRepo.ExistsByPair(ctx, a.stableID, b.stableID)
	if err != nil {
		m.log.Warn("ScanLocalMedia: pair check failed: %v", err)
		return false
	}
	if exists {
		return false
	}
	if m.isResolvedRemovalCached(ctx, fp, resolvedFPs) {
		return false
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
		return false
	}
	fpPreview := fp
	if len(fpPreview) > 8 {
		fpPreview = fpPreview[:8]
	}
	m.log.Info("Local duplicate detected: %q ↔ %q [fp=%s…]",
		filepath.Base(a.path), filepath.Base(b.path), fpPreview)
	return true
}

// isResolvedRemovalCached returns true if this fingerprint has a resolved removal; updates resolvedFPs cache.
func (m *Module) isResolvedRemovalCached(ctx context.Context, fp string, resolvedFPs map[string]bool) bool {
	if resolved, ok := resolvedFPs[fp]; ok {
		return resolved
	}
	resolved, err := m.dupRepo.ExistsResolvedRemoval(ctx, fp)
	if err != nil {
		m.log.Warn("isResolvedRemovalCached: failed to check fingerprint %s: %v", fp, err)
	}
	resolvedFPs[fp] = resolved
	return resolved
}

// processFingerprintGroup records duplicate pairs for all unordered (i,j) pairs in group.
func (m *Module) processFingerprintGroup(ctx context.Context, fp string, group []localFpEntry, resolvedFPs map[string]bool) {
	for i := 0; i < len(group); i++ {
		for j := i + 1; j < len(group); j++ {
			m.tryRecordLocalPair(ctx, localPairForRecord{fp: fp, a: group[i], b: group[j]}, resolvedFPs)
		}
	}
}

// ScanLocalMedia finds fingerprint collisions in media_metadata and persists pairs (loads full table).
func (m *Module) ScanLocalMedia(ctx context.Context) error {
	if !m.enabled() || m.metaRepo == nil {
		return nil
	}

	all, err := m.metaRepo.List(ctx)
	if err != nil {
		return fmt.Errorf("failed to list media metadata: %w", err)
	}

	fpGroups := buildLocalFingerprintGroups(all)
	resolvedFPs := make(map[string]bool)

	for fp, group := range fpGroups {
		if len(group) < 2 {
			continue
		}
		m.processFingerprintGroup(ctx, fp, group, resolvedFPs)
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

// ResolveDuplicateInput holds parameters for resolving a duplicate pair.
type ResolveDuplicateInput struct {
	ID         string // duplicate record ID
	Action     string // "remove_a", "remove_b", "keep_both", "ignore"
	ResolvedBy string // admin user identifier
}

// ResolveDuplicate acts on an admin decision for a detected duplicate pair.
// Action must be one of: "remove_a", "remove_b", "keep_both", "ignore".
func (m *Module) ResolveDuplicate(in ResolveDuplicateInput) error {
	if m.dupRepo == nil {
		return fmt.Errorf("duplicate detection is not available")
	}
	ctx := context.Background()

	rec, err := m.dupRepo.Get(ctx, in.ID)
	if err != nil {
		return fmt.Errorf("failed to fetch duplicate: %w", err)
	}
	if rec == nil {
		return fmt.Errorf("duplicate not found: %s", in.ID)
	}

	switch in.Action {
	case "remove_a":
		return m.applyRemoveResolution(ctx, removeResolutionParams{in.ID, in.Action, in.ResolvedBy, rec.ItemAID, rec.ItemASlaveID})
	case "remove_b":
		return m.applyRemoveResolution(ctx, removeResolutionParams{in.ID, in.Action, in.ResolvedBy, rec.ItemBID, rec.ItemBSlaveID})
	case "keep_both", "ignore":
		return m.dupRepo.UpdateStatus(ctx, in.ID, in.Action, in.ResolvedBy)
	default:
		return fmt.Errorf("unknown action %q — must be remove_a, remove_b, keep_both, or ignore", in.Action)
	}
}

// removeResolutionParams holds parameters for applyRemoveResolution.
type removeResolutionParams struct {
	id, action, resolvedBy, itemID, slaveID string
}

// applyRemoveResolution removes one item of a duplicate pair and updates status for the record and any cascade.
func (m *Module) applyRemoveResolution(ctx context.Context, p removeResolutionParams) error {
	m.removeItem(ctx, p.itemID, p.slaveID)
	if err := m.dupRepo.UpdateStatus(ctx, p.id, p.action, p.resolvedBy); err != nil {
		return err
	}
	if err := m.dupRepo.UpdateStatusForItem(ctx, p.itemID, p.action, p.resolvedBy); err != nil {
		m.log.Warn("ResolveDuplicate: cascade update failed for item %s: %v", p.itemID, err)
	}
	return nil
}

// removeItem deletes the item from the appropriate backing store.
// For receiver items it removes the row from receiver_media.
// For local items it removes the metadata row and the file on disk.
func (m *Module) removeItem(ctx context.Context, itemID, slaveID string) {
	if slaveID == "" {
		m.removeLocalItem(ctx, itemID)
		return
	}
	m.removeReceiverItem(ctx, itemID)
}

// removeLocalItem finds the local file by stable ID and deletes its metadata and file.
func (m *Module) removeLocalItem(ctx context.Context, itemID string) {
	if m.metaRepo == nil {
		return
	}
	path, err := m.findLocalPathByStableID(ctx, itemID)
	if err != nil {
		m.log.Warn("removeItem: %v", err)
		return
	}
	if path == "" {
		m.log.Warn("removeItem: local item %s not found in metadata", itemID)
		return
	}
	m.deleteLocalFileAndMetadata(ctx, path)
}

// findLocalPathByStableID returns the file path for the given stable ID (scans full metadata list).
func (m *Module) findLocalPathByStableID(ctx context.Context, itemID string) (string, error) {
	all, err := m.metaRepo.List(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to list metadata: %w", err)
	}
	for path, meta := range all {
		if meta.StableID == itemID {
			return path, nil
		}
	}
	return "", nil
}

// deleteLocalFileAndMetadata removes the metadata row and the file on disk for the given path.
func (m *Module) deleteLocalFileAndMetadata(ctx context.Context, path string) {
	if err := m.metaRepo.Delete(ctx, path); err != nil {
		m.log.Warn("removeItem: failed to delete local metadata for %s: %v", path, err)
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		m.log.Warn("removeItem: failed to delete local file %s: %v", path, err)
	}
}

// removeReceiverItem deletes the item from receiver_media by ID.
func (m *Module) removeReceiverItem(ctx context.Context, itemID string) {
	if m.receiverRepo == nil {
		return
	}
	if err := m.receiverRepo.DeleteByID(ctx, itemID); err != nil {
		m.log.Warn("removeItem: failed to delete receiver media %s: %v", itemID, err)
	}
}
