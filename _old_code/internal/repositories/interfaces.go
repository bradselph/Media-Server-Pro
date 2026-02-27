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
	ErrUserExists      = errors.New("user already exists")
	ErrUserNotFound    = errors.New("user not found")
	ErrSessionNotFound = errors.New("session not found")
)

// UserRepository provides user data access methods
type UserRepository interface {
	Create(ctx context.Context, user *models.User) error
	GetByID(ctx context.Context, id string) (*models.User, error)
	GetByUsername(ctx context.Context, username string) (*models.User, error)
	Update(ctx context.Context, user *models.User) error
	Delete(ctx context.Context, id string) error
	List(ctx context.Context) ([]*models.User, error)
}

// SessionRepository provides session data access methods
type SessionRepository interface {
	Create(ctx context.Context, session *models.Session) error
	Get(ctx context.Context, id string) (*models.Session, error)
	Delete(ctx context.Context, id string) error
	DeleteExpired(ctx context.Context) error
	List(ctx context.Context) ([]*models.Session, error)
	ListByUser(ctx context.Context, userID string) ([]*models.Session, error)
}

// MediaMetadataRepository provides media metadata access methods
type MediaMetadataRepository interface {
	Upsert(ctx context.Context, path string, metadata *MediaMetadata) error
	Get(ctx context.Context, path string) (*MediaMetadata, error)
	Delete(ctx context.Context, path string) error
	List(ctx context.Context) (map[string]*MediaMetadata, error)
	IncrementViews(ctx context.Context, path string) error
	UpdatePlaybackPosition(ctx context.Context, path, userID string, position float64) error
	GetPlaybackPosition(ctx context.Context, path, userID string) (float64, error)
}

// ScanResultRepository provides mature content scan result storage
type ScanResultRepository interface {
	Save(ctx context.Context, result *ScanResult) error
	Get(ctx context.Context, path string) (*ScanResult, error)
	GetPendingReview(ctx context.Context) ([]*ScanResult, error)
	MarkReviewed(ctx context.Context, path, reviewedBy, decision string) error
}

// MediaMetadata represents metadata stored for a media file
type MediaMetadata struct {
	Path        string
	Views       int
	LastPlayed  *string
	DateAdded   string
	IsMature    bool
	MatureScore float64
	Category    string
	Tags        []string
	// ProbeModTime is the file mtime at the time ffprobe was last run.
	// A zero value means the file has not been probed yet.
	ProbeModTime *time.Time
}

// ScanResult represents a mature content scan result
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
	List(ctx context.Context, filter AnalyticsFilter) ([]*models.AnalyticsEvent, error)
	GetByMediaID(ctx context.Context, mediaID string) ([]*models.AnalyticsEvent, error)
	GetByUserID(ctx context.Context, userID string) ([]*models.AnalyticsEvent, error)
	DeleteOlderThan(ctx context.Context, before string) error
	Count(ctx context.Context, filter AnalyticsFilter) (int64, error)
	CountByType(ctx context.Context) (map[string]int, error)
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
	Get(ctx context.Context, id string) (*models.Playlist, error)
	Update(ctx context.Context, playlist *models.Playlist) error
	Delete(ctx context.Context, id string) error
	ListByUser(ctx context.Context, userID string) ([]*models.Playlist, error)
	ListAll(ctx context.Context) ([]*models.Playlist, error)
	AddItem(ctx context.Context, item *models.PlaylistItem) error
	RemoveItem(ctx context.Context, itemID string) error
	UpdateItem(ctx context.Context, item *models.PlaylistItem) error
	GetItems(ctx context.Context, playlistID string) ([]*models.PlaylistItem, error)
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
	GetByUser(ctx context.Context, userID string, limit int) ([]*models.AuditLogEntry, error)
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
