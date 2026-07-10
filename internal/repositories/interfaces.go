// Package repositories defines data access layer interfaces for the media server.
package repositories

import (
	"context"
	"errors"
	"time"

	"media-server-pro/pkg/models"
)

// Repository-level errors
var (
	ErrUserExists                = errors.New("user already exists")
	ErrUserNotFound              = errors.New("user not found")
	ErrSessionNotFound           = errors.New("session not found")
	ErrPlaylistNotFound          = errors.New("playlist not found")
	ErrScanResultNotFound        = errors.New("scan result not found")
	ErrAPITokenNotFound          = errors.New("api token not found")
	ErrSuggestionProfileNotFound = errors.New("suggestion profile not found")
	ErrViewHistoryNotFound       = errors.New("view history not found")
	ErrReceiverDuplicateNotFound = errors.New("receiver duplicate not found")
	ErrPathNotFound              = errors.New("path not found")
	ErrMetadataNotFound          = errors.New("media metadata not found")
	ErrBackupManifestNotFound    = errors.New("backup manifest not found")
)

// UserRepository provides user data access methods
type UserRepository interface {
	Create(ctx context.Context, user *models.User) error
	GetByID(ctx context.Context, id string) (*models.User, error)
	GetByUsername(ctx context.Context, username string) (*models.User, error)
	Update(ctx context.Context, user *models.User) error
	// UpdatePasswordHash writes only password_hash and salt for the given username,
	// avoiding the full-snapshot race where a concurrent Update could overwrite the new hash.
	UpdatePasswordHash(ctx context.Context, username, passwordHash, salt string) error
	Delete(ctx context.Context, id string) error
	List(ctx context.Context) ([]*models.User, error)
	IncrementStorageUsed(ctx context.Context, userID string, delta int64) error
}

// SessionRepository provides session data access methods
type SessionRepository interface {
	Create(ctx context.Context, session *models.Session) error
	Get(ctx context.Context, id string) (*models.Session, error)
	Update(ctx context.Context, session *models.Session) error
	Delete(ctx context.Context, id string) error
	DeleteExpired(ctx context.Context) error
	List(ctx context.Context) ([]*models.Session, error)
	ListByUser(ctx context.Context, userID string) ([]*models.Session, error)
}

// MediaMetadataRepository provides media metadata access methods
type MediaMetadataRepository interface {
	Upsert(ctx context.Context, path string, metadata *MediaMetadata) error
	// BulkUpsert writes many metadata rows in chunked multi-row statements
	// instead of one transaction per row. It returns the number of rows
	// successfully persisted. On a large library this turns a post-scan save
	// from tens of thousands of round-trips into a few hundred. Each chunk is
	// its own transaction; the context is honored between chunks so shutdown
	// or scan cancellation stops the save promptly.
	BulkUpsert(ctx context.Context, items map[string]*MediaMetadata) (int, error)
	Get(ctx context.Context, path string) (*MediaMetadata, error)
	Delete(ctx context.Context, path string) error
	List(ctx context.Context) (map[string]*MediaMetadata, error)
	// ListFiltered returns metadata matching the given filter with DB-level
	// pagination. The second return value is the total count of matching rows
	// (before LIMIT/OFFSET) for pagination controls.
	ListFiltered(ctx context.Context, filter MediaFilter) ([]*MediaMetadata, int64, error)
	IncrementViews(ctx context.Context, path string) error
	// UpdatePlaybackPosition persists the playback position, total duration, and
	// progress fraction for a user. Pass 0 for duration and progress when clearing.
	UpdatePlaybackPosition(ctx context.Context, path, userID string, position, duration, progress float64) error
	GetPlaybackPosition(ctx context.Context, path, userID string) (float64, error)
	BatchGetPlaybackPositions(ctx context.Context, paths []string, userID string) (map[string]float64, error)
	DeleteAllPlaybackPositionsByUser(ctx context.Context, userID string) error
	DeletePlaybackPositionsByPath(ctx context.Context, path string) error
	// UpdateBlurHash updates the BlurHash for a metadata row by path
	UpdateBlurHash(ctx context.Context, path string, blurHash string) error
	// GetPathByStableID returns the file path for the given stable ID.
	// Returns ("", ErrPathNotFound) when no row matches (consistent with ScanResultRepository.Get pattern).
	GetPathByStableID(ctx context.Context, stableID string) (string, error)
	// ListDuplicateCandidates returns rows that have both a non-empty
	// content_fingerprint and stable_id, which is the only set used by the
	// duplicate-detection scan.  Tags are not loaded (not needed for fingerprint
	// grouping) so this is significantly cheaper than List for large libraries.
	ListDuplicateCandidates(ctx context.Context) (map[string]*MediaMetadata, error)
}

// MediaFilter defines DB-level filtering and pagination for media queries.
type MediaFilter struct {
	CategoryID string   // curated MediaCategory.id — restricts to items in media_category_items for that category (empty = no filter)
	IsMature   *bool    // filter by mature flag
	Search     string   // substring match on path (LIKE %search%)
	Type       string   // "video" or "audio" — filters by path extension (matches discovery logic)
	Tags       []string // filter by tags (default OR — item must have at least one of these)
	TagsAll    bool     // when true, item must have ALL of the listed tags (AND mode)
	SortBy     string   // column to sort by: "views", "date_added", "path"
	SortDesc   bool     // descending sort
	Limit      int      // max results (0 = no limit)
	Offset     int      // skip N results
}

// ScanResultRepository provides mature content scan result storage.
// Get returns (nil, ErrScanResultNotFound) when no result exists for the path.
type ScanResultRepository interface {
	Save(ctx context.Context, result *ScanResult) error
	Get(ctx context.Context, path string) (*ScanResult, error)
	// GetByPaths batch-loads scan results for many paths at once (one query per
	// chunk instead of one Get per path), returning a path->result map. Paths with
	// no persisted row are simply absent from the map. Used by full directory scans
	// to avoid an N+1 of per-file Get calls.
	GetByPaths(ctx context.Context, paths []string) (map[string]*ScanResult, error)
	GetPendingReview(ctx context.Context) ([]*ScanResult, error)
	MarkReviewed(ctx context.Context, path, reviewedBy, decision string) error
	Delete(ctx context.Context, path string) error
}

// MediaMetadata represents metadata stored for a media file (temporal fields as RFC3339 strings in domain).
type MediaMetadata struct {
	Path string
	// StableID is a UUID generated on first scan and persisted in the DB.
	// It serves as the public-facing MediaItem.ID, decoupling it from the
	// filesystem path so that IDs survive renames, moves, and config changes.
	// Empty string means the row predates stable-ID support; callers should
	// treat a missing StableID as requiring a new UUID.
	StableID string
	// ContentFingerprint is a SHA-256 hash of sampled file content
	// (first 64KB + last 64KB + file size). Used to detect moved/renamed
	// files and identify duplicates regardless of path or filename.
	ContentFingerprint string
	Views              int
	LastPlayed         *string
	DateAdded          string
	IsMature           bool
	MatureScore        float64
	Category           string
	Tags               []string
	// ProbeModTime is the file mtime at the time ffprobe was last run.
	// A zero value means the file has not been probed yet.
	ProbeModTime *time.Time
	// BlurHash is a compact representation for LQIP placeholders (~20-30 bytes)
	BlurHash string
	// Duration is the media file duration in seconds, extracted by ffprobe.
	Duration float64
	// CustomMeta holds admin-set custom key/value fields (e.g. description).
	// Persisted as a JSON object in the media_metadata.custom_meta column.
	CustomMeta map[string]string
}

// ScanResult holds scan metadata (ScannedAt/ReviewedAt as strings; MySQL impl parses to time).
type ScanResult struct {
	Path           string
	IsMature       bool
	Confidence     float64
	AutoFlagged    bool
	NeedsReview    bool
	ScannedAt      string
	ReviewedBy     string
	ReviewedAt     string
	ReviewDecision string
	Reasons        []string
}

// AnalyticsRepository provides analytics event storage
type AnalyticsRepository interface {
	Create(ctx context.Context, event *models.AnalyticsEvent) error
	// CreateBatch inserts many events in one (chunked) multi-row INSERT. Used by the
	// analytics module's event-buffer flush to avoid a synchronous per-event round
	// trip on the hot tracking path.
	CreateBatch(ctx context.Context, events []*models.AnalyticsEvent) error
	List(ctx context.Context, filter AnalyticsFilter) ([]*models.AnalyticsEvent, error)
	DeleteOlderThan(ctx context.Context, before string) error
	DeleteByMediaID(ctx context.Context, mediaID string) error
	Count(ctx context.Context, filter AnalyticsFilter) (int64, error)
	CountByType(ctx context.Context) (map[string]int64, error)

	// UpsertDailyStats writes (or replaces) a daily aggregate row. Called from
	// the analytics module's flush loop so dashboard numbers survive restarts.
	UpsertDailyStats(ctx context.Context, stats *models.DailyStats) error
	// ListDailyStatsBetween returns persisted daily stats for [startDate, endDate]
	// inclusive ("YYYY-MM-DD"). Used on Start to rehydrate the in-memory map.
	ListDailyStatsBetween(ctx context.Context, startDate, endDate string) ([]*models.DailyStats, error)
	// DeleteDailyStatsOlderThan removes daily_stats rows older than the cutoff
	// date ("YYYY-MM-DD"). Mirrors the retention policy applied to raw events.
	DeleteDailyStatsOlderThan(ctx context.Context, beforeDate string) error
}

// AnalyticsFilter defines filtering options for analytics queries
type AnalyticsFilter struct {
	Type      string
	MediaID   string
	UserID    string
	StartDate string
	EndDate   string
	Limit     int
	Offset    int
}

// PlaylistRepository provides playlist storage
type PlaylistRepository interface {
	Create(ctx context.Context, playlist *models.Playlist) error
	CreateWithItems(ctx context.Context, playlist *models.Playlist, items []models.PlaylistItem) error
	Get(ctx context.Context, id string) (*models.Playlist, error)
	Update(ctx context.Context, playlist *models.Playlist) error
	Delete(ctx context.Context, id string) error
	ListByUser(ctx context.Context, userID string) ([]*models.Playlist, error)
	ListAll(ctx context.Context) ([]*models.Playlist, error)
	AddItem(ctx context.Context, item *models.PlaylistItem) error
	RemoveItem(ctx context.Context, itemID string) error
	UpdateItem(ctx context.Context, item *models.PlaylistItem) error
}

// UserPreferencesRepository provides user preferences storage
type UserPreferencesRepository interface {
	Upsert(ctx context.Context, prefs *models.UserPreferences) error
	Get(ctx context.Context, userID string) (*models.UserPreferences, error)
	Delete(ctx context.Context, userID string) error
}

// UserPermissionsRepository provides user permissions storage
type UserPermissionsRepository interface {
	Upsert(ctx context.Context, perms *models.UserPermissions) error
	Get(ctx context.Context, userID string) (*models.UserPermissions, error)
	Delete(ctx context.Context, userID string) error
}

// AuditLogRepository provides audit log storage
type AuditLogRepository interface {
	Create(ctx context.Context, entry *models.AuditLogEntry) error
	List(ctx context.Context, filter AuditLogFilter) ([]*models.AuditLogEntry, error)
	Count(ctx context.Context, filter AuditLogFilter) (int64, error)
	DeleteOlderThan(ctx context.Context, before string) error
}

// AuditLogFilter defines filtering options for audit log queries
type AuditLogFilter struct {
	UserID    string
	Action    string
	Resource  string
	Success   *bool
	StartDate string
	EndDate   string
	Limit     int
	Offset    int
}

// HLSJobRepository provides HLS job persistence
type HLSJobRepository interface {
	Save(ctx context.Context, job *models.HLSJob) error
	Get(ctx context.Context, id string) (*models.HLSJob, error)
	Delete(ctx context.Context, id string) error
	List(ctx context.Context) ([]*models.HLSJob, error)
}

// ValidationResultRepository provides media validation result storage
type ValidationResultRepository interface {
	Upsert(ctx context.Context, result *ValidationResultRecord) error
	Delete(ctx context.Context, path string) error
	List(ctx context.Context) ([]*ValidationResultRecord, error)
}

// ValidationResultRecord represents a media validation result in the database
type ValidationResultRecord struct {
	Path           string
	Status         string
	ValidatedAt    time.Time
	Duration       float64
	VideoCodec     string
	AudioCodec     string
	Width          int
	Height         int
	Bitrate        int64
	Container      string
	Issues         []string
	Error          string
	VideoSupported bool
	AudioSupported bool
}

// SuggestionProfileRepository provides user suggestion profile storage
type SuggestionProfileRepository interface {
	SaveProfile(ctx context.Context, profile *SuggestionProfileRecord) error
	GetProfile(ctx context.Context, userID string) (*SuggestionProfileRecord, error)
	DeleteProfile(ctx context.Context, userID string) error
	ListProfiles(ctx context.Context) ([]*SuggestionProfileRecord, error)
	SaveViewHistory(ctx context.Context, userID string, entry *ViewHistoryRecord) error
	BatchSaveViewHistory(ctx context.Context, userID string, entries []*ViewHistoryRecord) error
	GetViewHistory(ctx context.Context, userID string) ([]*ViewHistoryRecord, error)
	DeleteViewHistory(ctx context.Context, userID string) error
	DeleteViewHistoryByMediaPath(ctx context.Context, mediaPath string) error
	RenameViewHistoryMediaPath(ctx context.Context, oldPath, newPath string) error
}

// SuggestionProfileRecord represents a user's suggestion profile
type SuggestionProfileRecord struct {
	UserID          string
	CategoryScores  map[string]float64
	TypePreferences map[string]float64
	TotalViews      int
	TotalWatchTime  float64
	LastUpdated     time.Time
}

// ViewHistoryRecord represents a single view history entry
type ViewHistoryRecord struct {
	UserID      string
	MediaPath   string
	Category    string
	MediaType   string
	ViewCount   int
	TotalTime   float64
	LastViewed  time.Time
	CompletedAt *time.Time
	Rating      float64
}

// AutoDiscoverySuggestionRepository provides file rename suggestion storage
type AutoDiscoverySuggestionRepository interface {
	Save(ctx context.Context, suggestion *AutoDiscoveryRecord) error
	Get(ctx context.Context, originalPath string) (*AutoDiscoveryRecord, error)
	Delete(ctx context.Context, originalPath string) error
	List(ctx context.Context) ([]*AutoDiscoveryRecord, error)
	// DeleteAll removes ALL suggestion records. Use with caution.
	DeleteAll(ctx context.Context) error
}

// AutoDiscoveryRecord represents an auto-discovery file rename suggestion
type AutoDiscoveryRecord struct {
	OriginalPath  string
	SuggestedName string
	SuggestedPath string
	Type          string
	Confidence    float64
	Metadata      map[string]string
}

// IPListRepository provides IP whitelist/blacklist storage
type IPListRepository interface {
	SaveListConfig(ctx context.Context, listType string, name string, enabled bool) error
	GetListConfig(ctx context.Context, listType string) (name string, enabled bool, err error)
	SaveEntries(ctx context.Context, listType string, entries []*IPEntryRecord) error
	GetEntries(ctx context.Context, listType string) ([]*IPEntryRecord, error)
	AddEntry(ctx context.Context, listType string, entry *IPEntryRecord) error
	RemoveEntry(ctx context.Context, listType string, ipValue string) error
}

// IPEntryRecord represents an IP list entry in the database
type IPEntryRecord struct {
	ListType  string
	Value     string
	Comment   string
	AddedAt   time.Time
	AddedBy   string
	ExpiresAt *time.Time
}

// RemoteCacheRepository provides remote media cache index storage
type RemoteCacheRepository interface {
	Save(ctx context.Context, entry *RemoteCacheRecord) error
	Get(ctx context.Context, remoteURL string) (*RemoteCacheRecord, error)
	Delete(ctx context.Context, remoteURL string) error
	List(ctx context.Context) ([]*RemoteCacheRecord, error)
}

// RemoteCacheRecord represents a cached remote media entry
type RemoteCacheRecord struct {
	RemoteURL   string
	LocalPath   string
	Size        int64
	ContentType string
	CachedAt    time.Time
	LastAccess  time.Time
	Hits        int
}

// ReceiverSlaveRepository provides slave node registry storage
type ReceiverSlaveRepository interface {
	Upsert(ctx context.Context, slave *ReceiverSlaveRecord) error
	Get(ctx context.Context, slaveID string) (*ReceiverSlaveRecord, error)
	Delete(ctx context.Context, slaveID string) error
	List(ctx context.Context) ([]*ReceiverSlaveRecord, error)
}

// ReceiverSlaveRecord represents a registered slave node
type ReceiverSlaveRecord struct {
	ID         string
	Name       string
	BaseURL    string
	Status     string // online, offline, degraded
	MediaCount int
	LastSeen   time.Time
	CreatedAt  time.Time
}

// ReceiverMediaRepository provides slave media catalog storage
type ReceiverMediaRepository interface {
	UpsertBatch(ctx context.Context, slaveID string, items []*ReceiverMediaRecord) error
	// ReplaceSlaveMedia atomically deletes all existing records for slaveID then
	// inserts records in a single transaction, preventing data loss if the server
	// crashes between the two operations.
	ReplaceSlaveMedia(ctx context.Context, slaveID string, items []*ReceiverMediaRecord) error
	ListAll(ctx context.Context) ([]*ReceiverMediaRecord, error)
	// ListByFingerprints returns receiver media rows whose content_fingerprint is in
	// the given set and whose slave_id != excludeSlaveID. Used by duplicate detection
	// to fetch only the rows relevant to a pushed batch instead of loading the entire
	// receiver_media table on every push.
	ListByFingerprints(ctx context.Context, excludeSlaveID string, fingerprints []string) ([]*ReceiverMediaRecord, error)
	DeleteBySlave(ctx context.Context, slaveID string) error
	DeleteByID(ctx context.Context, id string) error
}

// ReceiverMediaRecord represents a media item from a slave node's catalog
type ReceiverMediaRecord struct {
	ID                 string
	SlaveID            string
	RemoteID           string // slave's own item.ID — used by thumbnail proxy to look up the on-slave thumbnail file
	RemotePath         string
	Name               string
	MediaType          string // video, audio
	Size               int64
	Duration           float64
	ContentType        string
	ContentFingerprint string
	Width              int
	Height             int
	// Display metadata mirrored from the slave's local item so federated content
	// looks identical to local content in the unified library.
	Category     string
	Tags         string // CSV; mirrors models.MediaItem.Tags []string flattened for storage
	BlurHash     string
	DateAdded    time.Time
	DateModified time.Time
	IsMature     bool
	UpdatedAt    time.Time
}

// ReceiverDuplicateRepository provides storage for detected duplicate media pairs.
type ReceiverDuplicateRepository interface {
	Create(ctx context.Context, dup *ReceiverDuplicateRecord) error
	Get(ctx context.Context, id string) (*ReceiverDuplicateRecord, error)
	List(ctx context.Context) ([]*ReceiverDuplicateRecord, error)
	ListPending(ctx context.Context) ([]*ReceiverDuplicateRecord, error)
	ExistsByPair(ctx context.Context, itemAID, itemBID string) (bool, error)
	ExistsResolvedRemoval(ctx context.Context, fingerprint string) (bool, error)
	UpdateStatus(ctx context.Context, id, status, resolvedBy string) error
	UpdateStatusForItem(ctx context.Context, itemID, resolvedBy string) error
	CountPending(ctx context.Context) (int64, error)
	// DeleteBySlave removes all duplicate records where either side belongs to slaveID.
	DeleteBySlave(ctx context.Context, slaveID string) error
	// DeletePendingBySlave removes only pending duplicate records for slaveID.
	DeletePendingBySlave(ctx context.Context, slaveID string) error
}

// ReceiverDuplicateRecord represents a detected duplicate pair between two slave media items.
type ReceiverDuplicateRecord struct {
	ID           string
	Fingerprint  string
	ItemAID      string
	ItemASlaveID string
	ItemAName    string
	ItemBID      string
	ItemBSlaveID string
	ItemBName    string
	// Status is one of: "pending", "remove_a", "remove_b", "keep_both", "ignore"
	Status     string
	ResolvedBy string
	ResolvedAt *time.Time
	DetectedAt time.Time
}

// BackupManifestRepository provides backup manifest storage
type BackupManifestRepository interface {
	Save(ctx context.Context, manifest *BackupManifestRecord) error
	Get(ctx context.Context, id string) (*BackupManifestRecord, error)
	Delete(ctx context.Context, id string) error
	List(ctx context.Context) ([]*BackupManifestRecord, error)
}

// BackupManifestRecord represents a backup manifest in the database
type BackupManifestRecord struct {
	ID          string
	Filename    string
	CreatedAt   time.Time
	Size        int64
	Type        string
	Description string
	Files       []string
	Errors      []string
	Version     string
}

// ExtractorItemRepository provides extracted media item storage
type ExtractorItemRepository interface {
	Upsert(ctx context.Context, item *ExtractorItemRecord) error
	Get(ctx context.Context, id string) (*ExtractorItemRecord, error)
	Delete(ctx context.Context, id string) error
	List(ctx context.Context) ([]*ExtractorItemRecord, error)
	UpdateStatus(ctx context.Context, id, status, errorMsg string) error
}

// ExtractorItemRecord represents a stored extracted media item
type ExtractorItemRecord struct {
	ID              string
	SourceURL       string
	Title           string
	StreamURL       string
	StreamType      string // "hls" or "mp4"
	ContentType     string
	Quality         string
	Width           int
	Height          int
	Duration        float64
	Site            string
	DetectionMethod string
	Status          string // "active", "expired", "error"
	ErrorMessage    string
	AddedBy         string
	ResolvedAt      time.Time
	ExpiresAt       *time.Time
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// CrawlerTargetRepository provides crawler target storage
type CrawlerTargetRepository interface {
	Upsert(ctx context.Context, target *CrawlerTargetRecord) error
	Get(ctx context.Context, id string) (*CrawlerTargetRecord, error)
	Delete(ctx context.Context, id string) error
	List(ctx context.Context) ([]*CrawlerTargetRecord, error)
	UpdateLastCrawled(ctx context.Context, id string, crawledAt time.Time) error
}

// CrawlerTargetRecord represents a site target for crawling
type CrawlerTargetRecord struct {
	ID          string
	Name        string
	URL         string
	Site        string
	Enabled     bool
	LastCrawled *time.Time
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// CrawlerDiscoveryRepository provides crawler discovery storage
type CrawlerDiscoveryRepository interface {
	Create(ctx context.Context, disc *CrawlerDiscoveryRecord) error
	Get(ctx context.Context, id string) (*CrawlerDiscoveryRecord, error)
	Delete(ctx context.Context, id string) error
	List(ctx context.Context) ([]*CrawlerDiscoveryRecord, error)
	ListByTarget(ctx context.Context, targetID string) ([]*CrawlerDiscoveryRecord, error)
	ListPending(ctx context.Context) ([]*CrawlerDiscoveryRecord, error)
	UpdateStatus(ctx context.Context, id, status, reviewedBy string) error
	ExistsByStreamURL(ctx context.Context, streamURL string) (bool, error)
}

// CrawlerDiscoveryRecord represents a discovered stream from crawling
type CrawlerDiscoveryRecord struct {
	ID              string
	TargetID        string
	PageURL         string
	Title           string
	StreamURL       string
	StreamType      string
	Quality         int
	DetectionMethod string
	Status          string // "pending", "added", "ignored"
	ReviewedBy      string
	ReviewedAt      *time.Time
	DiscoveredAt    time.Time
}

// FavoriteRepository provides user favorites (Watch Later) storage.
type FavoriteRepository interface {
	Add(ctx context.Context, rec *FavoriteRecord) error
	Remove(ctx context.Context, userID, mediaID string) error
	List(ctx context.Context, userID string) ([]*FavoriteRecord, error)
	Exists(ctx context.Context, userID, mediaID string) (bool, error)
}

// FavoriteRecord represents a single user favorite entry.
type FavoriteRecord struct {
	ID        string
	UserID    string
	MediaID   string
	MediaPath string
	AddedAt   time.Time
}

// SavedSearchRepository provides per-user saved-search storage. Saved
// searches are soft subscriptions: the homepage shows new matches added
// since the user's last visit to that saved search.
type SavedSearchRepository interface {
	Create(ctx context.Context, rec *SavedSearchRecord) error
	Delete(ctx context.Context, id, userID string) error
	List(ctx context.Context, userID string) ([]*SavedSearchRecord, error)
	Get(ctx context.Context, id, userID string) (*SavedSearchRecord, error)
	UpdateLastSeen(ctx context.Context, id, userID string, seenAt time.Time) error
}

// SavedSearchRecord represents a single user-saved search.
type SavedSearchRecord struct {
	ID         string
	UserID     string
	Name       string
	Query      string
	Tags       []string
	TagMode    string // "or" (default) or "and"
	MediaType  string // optional filter ("video", "audio", "image", or "")
	CreatedAt  time.Time
	LastSeenAt time.Time
}

// DataDeletionRequestRepository provides data deletion request storage.
type DataDeletionRequestRepository interface {
	Create(ctx context.Context, req *DataDeletionRequestRecord) error
	Get(ctx context.Context, id string) (*DataDeletionRequestRecord, error)
	ListByStatus(ctx context.Context, status string) ([]*DataDeletionRequestRecord, error)
	CountPendingByUser(ctx context.Context, userID string) (int64, error)
	UpdateStatus(ctx context.Context, id string, status, reviewedBy, adminNotes string) error
}

// DataDeletionRequestRecord represents a data deletion request in the database.
type DataDeletionRequestRecord struct {
	ID         string
	UserID     string
	Username   string
	Email      string
	Reason     string
	Status     string // "pending", "approved", "denied"
	CreatedAt  time.Time
	ReviewedAt *time.Time
	ReviewedBy string
	AdminNotes string
}

// APITokenRepository provides user API token storage.
type APITokenRepository interface {
	Create(ctx context.Context, token *APITokenRecord) error
	GetByHash(ctx context.Context, tokenHash string) (*APITokenRecord, error)
	ListByUser(ctx context.Context, userID string) ([]*APITokenRecord, error)
	Delete(ctx context.Context, id, userID string) error
	UpdateLastUsed(ctx context.Context, tokenHash string) error
}

// APITokenRecord represents a stored API token (raw value is never stored).
type APITokenRecord struct {
	ID         string
	UserID     string
	Name       string
	TokenHash  string
	LastUsedAt *time.Time
	ExpiresAt  *time.Time // nil means no expiry
	CreatedAt  time.Time
}

// MediaReportRepository persists user-submitted moderation reports on
// individual media items.
type MediaReportRepository interface {
	Create(ctx context.Context, rec *MediaReportRecord) error
	List(ctx context.Context, status string, limit, offset int) ([]*MediaReportRecord, error)
	UpdateStatus(ctx context.Context, id, status, resolvedBy string) error
	CountByStatus(ctx context.Context, status string) (int64, error)
}

// MediaReportRecord captures one report. Status values: "open",
// "resolved", "dismissed". ReporterID is empty for guests.
type MediaReportRecord struct {
	ID         string
	MediaID    string
	ReporterID string
	Reason     string
	Notes      string
	Status     string
	CreatedAt  time.Time
	ResolvedAt *time.Time
	ResolvedBy string
	IPAddress  string
}
