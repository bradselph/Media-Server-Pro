// Package models defines data structures used throughout the media server.
package models

import (
	"encoding/json"
	"fmt"
	"time"
	"unicode/utf8"
)

// MediaType represents the type of media
type MediaType string

const (
	MediaTypeVideo MediaType = "video"
	MediaTypeAudio MediaType = "audio"
	// MediaTypeUnknown is used as a sentinel by internal/media/discovery.go when scanning
	// the uploads directory (mixed content). It is never stored on a MediaItem.Type field —
	// unmatched files are dropped during scanning, not cataloged as unknown.
	MediaTypeUnknown MediaType = "unknown"
)

// MediaItem represents a media file with metadata.
// Path is excluded from JSON serialization to prevent leaking filesystem paths to clients.
// Clients should reference media items by their stable UUID (generated once per file,
// persisted in the database, and decoupled from the filesystem path).
type MediaItem struct {
	ID           string     `json:"id"`
	Path         string     `json:"-"`
	Name         string     `json:"name"`
	Type         MediaType  `json:"type"`
	Size         int64      `json:"size"`
	Duration     float64    `json:"duration"`
	Width        int        `json:"width,omitempty"`
	Height       int        `json:"height,omitempty"`
	Bitrate      int64      `json:"bitrate,omitempty"`
	Codec        string     `json:"codec,omitempty"`
	Container    string     `json:"container,omitempty"`
	Category     string     `json:"category,omitempty"`
	Tags         []string   `json:"tags,omitempty"`
	ThumbnailURL string     `json:"thumbnail_url,omitempty"`
	BlurHash     string     `json:"blur_hash,omitempty"`
	DateAdded    time.Time  `json:"date_added"`
	DateModified time.Time  `json:"date_modified"`
	Views        int        `json:"views"`
	LastPlayed   *time.Time `json:"last_played,omitempty"`
	IsMature     bool       `json:"is_mature"`
	MatureScore  float64    `json:"mature_score,omitempty"`
	// SECURITY WARNING: Metadata contains arbitrary key-value pairs.
	// All metadata values MUST be sanitized using helpers.SanitizeMap before storage
	// to prevent XSS when rendered in HTML templates. Handlers should validate keys/values.
	Metadata map[string]string `json:"metadata,omitempty"`
}

// MediaCategory represents a category of media files
type MediaCategory struct {
	Name        string   `json:"name"`
	DisplayName string   `json:"display_name"`
	Count       int      `json:"count"`
	Tags        []string `json:"tags,omitempty"`
}

// User represents a system user.
// PasswordHash and Salt are excluded from default JSON serialization (json:"-")
// to prevent accidental leakage. All persistence goes through GORM MySQL repositories.
type User struct {
	ID                string             `json:"id" db:"id" gorm:"primaryKey;size:255"`
	Username          string             `json:"username" db:"username" gorm:"uniqueIndex;size:255;not null"`
	PasswordHash      string             `json:"-" db:"password_hash" gorm:"type:text;not null"`
	Salt              string             `json:"-" db:"salt" gorm:"size:255;not null"`
	Email             string             `json:"email,omitempty" db:"email" gorm:"size:255"`
	Role              UserRole           `json:"role" db:"role" gorm:"type:enum('admin','viewer');default:viewer;not null"`
	Type              string             `json:"type" db:"type" gorm:"size:50;default:standard;not null"`
	Enabled           bool               `json:"enabled" db:"enabled" gorm:"default:true;not null"`
	CreatedAt         time.Time          `json:"created_at" db:"created_at" gorm:"autoCreateTime"`
	LastLogin         *time.Time         `json:"last_login,omitempty" db:"last_login"`
	PreviousLastLogin *time.Time         `json:"previous_last_login,omitempty" db:"previous_last_login" gorm:"column:previous_last_login"`
	Permissions       UserPermissions    `json:"permissions" db:"-" gorm:"-"`
	Preferences       UserPreferences    `json:"preferences" db:"-" gorm:"-"`
	WatchHistory      []WatchHistoryItem `json:"watch_history,omitempty" db:"watch_history" gorm:"type:json;serializer:json"`
	StorageUsed       int64              `json:"storage_used" db:"storage_used" gorm:"default:0;not null"`
	ActiveStreams     int                `json:"active_streams" db:"active_streams" gorm:"default:0;not null"`
	// SECURITY WARNING: Metadata is arbitrary JSON that may contain malicious content.
	// Always sanitize metadata values before rendering in HTML to prevent XSS attacks.
	// Use textContent instead of innerHTML, or apply proper HTML escaping.
	Metadata map[string]interface{} `json:"metadata,omitempty" db:"metadata" gorm:"type:json;serializer:json"`
}

// TableName specifies the table name for GORM
func (*User) TableName() string {
	return "users"
}

// UserRole represents the role of a user
type UserRole string

const (
	RoleAdmin  UserRole = "admin"
	RoleViewer UserRole = "viewer"
)

// UserPermissions defines what a user is allowed to do
type UserPermissions struct {
	UserID             string `json:"user_id,omitempty" db:"user_id" gorm:"primaryKey;size:255"`
	CanStream          bool   `json:"can_stream" db:"can_stream" gorm:"default:true;not null"`
	CanDownload        bool   `json:"can_download" db:"can_download" gorm:"default:false;not null"`
	CanUpload          bool   `json:"can_upload" db:"can_upload" gorm:"default:false;not null"`
	CanDelete          bool   `json:"can_delete" db:"can_delete" gorm:"default:false;not null"`
	CanManage          bool   `json:"can_manage" db:"can_manage" gorm:"default:false;not null"`
	CanViewMature      bool   `json:"can_view_mature" db:"can_view_mature" gorm:"default:false;not null"`
	CanCreatePlaylists bool   `json:"can_create_playlists" db:"can_create_playlists" gorm:"default:true;not null"`
}

// TableName specifies the table name for GORM
func (UserPermissions) TableName() string {
	return "user_permissions"
}

// UserPreferences holds user-specific preferences.
// Custom UnmarshalJSON accepts both "auto_play"/"autoplay" and "equalizer_preset"/"equalizer_bands"
// keys, mapping them to the canonical fields AutoPlay and EqualizerPreset respectively.
// Use Validate() to ensure all fields contain reasonable values before persistence.
type UserPreferences struct {
	UserID              string                 `json:"user_id,omitempty" db:"user_id" gorm:"primaryKey;size:255"`
	Theme               string                 `json:"theme" db:"theme" gorm:"size:50;default:dark"`
	ViewMode            string                 `json:"view_mode" db:"view_mode" gorm:"size:50;default:grid"`
	DefaultQuality      string                 `json:"default_quality" db:"default_quality" gorm:"size:50;default:auto"`
	AutoPlay            bool                   `json:"auto_play" db:"auto_play" gorm:"default:false"`
	PlaybackSpeed       float64                `json:"playback_speed" db:"playback_speed" gorm:"default:1.0"`
	Volume              float64                `json:"volume" db:"volume" gorm:"default:1.0"`
	ShowMature          bool                   `json:"show_mature" db:"show_mature" gorm:"default:false"`
	MaturePreferenceSet bool                   `json:"mature_preference_set" db:"mature_preference_set" gorm:"default:false"`
	Language            string                 `json:"language" db:"language" gorm:"size:10;default:en"`
	EqualizerPreset     string                 `json:"equalizer_preset" db:"equalizer_preset" gorm:"size:100"`
	ResumePlayback      bool                   `json:"resume_playback" db:"resume_playback" gorm:"default:true"`
	ShowAnalytics       bool                   `json:"show_analytics" db:"show_analytics" gorm:"default:true"`
	ItemsPerPage        int                    `json:"items_per_page" db:"items_per_page" gorm:"default:20"`
	SortBy              string                 `json:"sort_by" db:"sort_by" gorm:"size:50;default:date_added"`
	SortOrder           string                 `json:"sort_order" db:"sort_order" gorm:"size:10;default:desc"`
	FilterCategory      string                 `json:"filter_category" db:"filter_category" gorm:"size:100"`
	FilterMediaType     string                 `json:"filter_media_type" db:"filter_media_type" gorm:"size:50"`
	CustomEQPresets     map[string]interface{} `json:"custom_eq_presets,omitempty" db:"custom_eq_presets" gorm:"type:json;serializer:json"`
	// Home section visibility — default true (show all sections)
	ShowContinueWatching bool `json:"show_continue_watching" db:"show_continue_watching" gorm:"default:true"`
	ShowRecommended      bool `json:"show_recommended" db:"show_recommended" gorm:"default:true"`
	ShowTrending         bool `json:"show_trending" db:"show_trending" gorm:"default:true"`
	// Player behaviour
	SkipInterval    int  `json:"skip_interval" db:"skip_interval" gorm:"default:10"`
	ShuffleEnabled  bool `json:"shuffle_enabled" db:"shuffle_enabled" gorm:"default:false"`
	ShowBufferBar   bool `json:"show_buffer_bar" db:"show_buffer_bar" gorm:"default:true"`
	DownloadPrompt  bool `json:"download_prompt" db:"download_prompt" gorm:"default:true"`
}

// TableName specifies the table name for GORM
func (*UserPreferences) TableName() string {
	return "user_preferences"
}

// MarshalJSON uses standard JSON encoding with only canonical field names.
// Alias fields (autoplay, equalizer_bands) are only accepted during unmarshal for
// backward compatibility, but should never be emitted to avoid ambiguity errors.
func (p *UserPreferences) MarshalJSON() ([]byte, error) {
	type Alias UserPreferences
	return json.Marshal(Alias(*p))
}

// userPrefsHasKey returns whether the key exists in the raw JSON map.
func userPrefsHasKey(m map[string]interface{}, key string) bool {
	_, ok := m[key]
	return ok
}

// userPrefsCheckAmbiguous returns an error if both canonical and alias keys are present.
func userPrefsCheckAmbiguous(rawMap map[string]interface{}) error {
	if userPrefsHasKey(rawMap, "auto_play") && userPrefsHasKey(rawMap, "autoplay") {
		return fmt.Errorf("ambiguous fields: both 'auto_play' and 'autoplay' provided; use only one")
	}
	if userPrefsHasKey(rawMap, "equalizer_preset") && userPrefsHasKey(rawMap, "equalizer_bands") {
		return fmt.Errorf("ambiguous fields: both 'equalizer_preset' and 'equalizer_bands' provided; use only one")
	}
	return nil
}

// userPrefsAliasAux holds alias fields decoded from JSON for UserPreferences.
type userPrefsAliasAux struct {
	Autoplay       *bool   `json:"autoplay"`
	EqualizerBands *string `json:"equalizer_bands"`
}

// userPrefsApplyAliasOverrides applies alias field values when the canonical key was not present.
func userPrefsApplyAliasOverrides(p *UserPreferences, aux *userPrefsAliasAux, rawMap map[string]interface{}) {
	if aux.Autoplay != nil && !userPrefsHasKey(rawMap, "auto_play") {
		p.AutoPlay = *aux.Autoplay
	}
	if aux.EqualizerBands != nil && !userPrefsHasKey(rawMap, "equalizer_preset") {
		p.EqualizerPreset = *aux.EqualizerBands
	}
}

// UnmarshalJSON accepts both canonical and alias keys.
// Canonical keys take precedence; aliases are used only if canonical key is absent.
func (p *UserPreferences) UnmarshalJSON(data []byte) error {
	type Alias UserPreferences

	var rawMap map[string]interface{}
	if err := json.Unmarshal(data, &rawMap); err != nil {
		return err
	}
	if err := userPrefsCheckAmbiguous(rawMap); err != nil {
		return err
	}

	aux := &struct {
		*Alias
		userPrefsAliasAux
	}{Alias: (*Alias)(p)}
	if err := json.Unmarshal(data, aux); err != nil {
		return err
	}

	userPrefsApplyAliasOverrides(p, &aux.userPrefsAliasAux, rawMap)
	return nil
}

// WatchHistoryItem represents an item in watch history
type WatchHistoryItem struct {
	MediaID   string    `json:"media_id"`
	MediaName string    `json:"media_name,omitempty"`
	MediaPath string    `json:"-"`
	Position  float64   `json:"position"`
	Duration  float64   `json:"duration"`
	Progress  float64   `json:"progress"`
	WatchedAt time.Time `json:"watched_at"`
	Completed bool      `json:"completed"`
}

// PlaybackPosition represents a user's playback position for a media file.
// Primary key is (Path, UserID); positions are path-based and may be orphaned on rename/move.
type PlaybackPosition struct {
	Path      string    `json:"path" db:"path" gorm:"primaryKey;size:500"`
	UserID    string    `json:"user_id" db:"user_id" gorm:"primaryKey;size:255"`
	Position  float64   `json:"position" db:"position"`
	Duration  float64   `json:"duration" db:"duration"`
	Progress  float64   `json:"progress" db:"progress"`
	UpdatedAt time.Time `json:"updated_at" db:"updated_at" gorm:"autoUpdateTime"`
}

// TableName specifies the table name for GORM
func (PlaybackPosition) TableName() string {
	return "playback_positions"
}

// MediaTag represents a tag associated with media
type MediaTag struct {
	Path string `json:"path" db:"path" gorm:"primaryKey;size:500"`
	Tag  string `json:"tag" db:"tag" gorm:"primaryKey;size:255;index"`
}

// TableName specifies the table name for GORM
func (MediaTag) TableName() string {
	return "media_tags"
}

// MediaChapter represents a named time range (scene marker / act chapter) for a media item.
type MediaChapter struct {
	ID        string     `json:"id" db:"id" gorm:"primaryKey;size:36"`
	MediaID   string     `json:"media_id" db:"media_id" gorm:"size:255;index"`
	StartTime float64    `json:"start_time" db:"start_time"`
	EndTime   *float64   `json:"end_time,omitempty" db:"end_time"`
	Label     string     `json:"label" db:"label" gorm:"size:255"`
	CreatedAt time.Time  `json:"created_at" db:"created_at" gorm:"autoCreateTime"`
}

// TableName specifies the table name for GORM
func (MediaChapter) TableName() string {
	return "media_chapters"
}

// clampSkipInterval clamps skip interval to 1–300 seconds or returns default 10 if invalid.
func clampSkipInterval(n int) int {
	if n <= 0 {
		return 10
	}
	if n > 300 {
		return 300
	}
	return n
}

// clampPlaybackSpeed clamps speed to 0.25–3.0 or returns default 1.0 if invalid.
func clampPlaybackSpeed(v float64) float64 {
	if v <= 0 || v > 3.0 {
		return 1.0
	}
	if v < 0.25 {
		return 0.25
	}
	return v
}

// clampVolume clamps volume to 0.0–1.0.
func clampVolume(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1.0 {
		return 1.0
	}
	return v
}

// clampItemsPerPage clamps to 1–200 or returns default 20 if non-positive.
func clampItemsPerPage(n int) int {
	if n <= 0 {
		return 20
	}
	if n > 200 {
		return 200
	}
	return n
}

// stringInSetOrDefault returns s if it is in allowed or empty; otherwise defaultVal.
func stringInSetOrDefault(s string, allowed map[string]bool, defaultVal string) string {
	if s == "" || allowed[s] {
		return s
	}
	return defaultVal
}

// truncateString truncates s to at most maxLen runes, never splitting a multi-byte UTF-8 character.
func truncateString(s string, maxLen int) string {
	if maxLen <= 0 || utf8.RuneCountInString(s) <= maxLen {
		return s
	}
	n := 0
	for i := range s {
		if n == maxLen {
			return s[:i]
		}
		n++
	}
	return s
}

// Validate validates and normalizes UserPreferences fields to ensure they contain reasonable values
func (p *UserPreferences) Validate() {
	p.PlaybackSpeed = clampPlaybackSpeed(p.PlaybackSpeed)
	p.Volume = clampVolume(p.Volume)
	p.ItemsPerPage = clampItemsPerPage(p.ItemsPerPage)
	p.SkipInterval = clampSkipInterval(p.SkipInterval)

	p.Theme = stringInSetOrDefault(p.Theme, map[string]bool{
		"light": true, "dark": true, "auto": true,
		"midnight": true, "nord": true, "dracula": true,
		"solarized-light": true, "forest": true, "sunset": true,
	}, "auto")
	p.ViewMode = stringInSetOrDefault(p.ViewMode, map[string]bool{"grid": true, "list": true, "compact": true}, "grid")
	p.SortOrder = stringInSetOrDefault(p.SortOrder, map[string]bool{"asc": true, "desc": true, "": true}, "asc")

	p.DefaultQuality = truncateString(p.DefaultQuality, 50)
	p.Language = truncateString(p.Language, 10)
	p.EqualizerPreset = truncateString(p.EqualizerPreset, 100)
	p.SortBy = truncateString(p.SortBy, 50)
	p.FilterCategory = truncateString(p.FilterCategory, 100)
	p.FilterMediaType = truncateString(p.FilterMediaType, 50)
}

// Session represents an authenticated session
type Session struct {
	ID           string    `json:"id" db:"id" gorm:"primaryKey;size:255"`
	UserID       string    `json:"user_id" db:"user_id" gorm:"size:255;not null;index"`
	Username     string    `json:"username" db:"username" gorm:"size:255;not null"`
	Role         UserRole  `json:"role" db:"role" gorm:"type:enum('admin','viewer');not null"`
	CreatedAt    time.Time `json:"created_at" db:"created_at" gorm:"autoCreateTime"`
	ExpiresAt    time.Time `json:"expires_at" db:"expires_at" gorm:"not null;index"`
	LastActivity time.Time `json:"last_activity" db:"last_activity" gorm:"autoUpdateTime"`
	IPAddress    string    `json:"ip_address" db:"ip_address" gorm:"size:45"`
	UserAgent    string    `json:"user_agent" db:"user_agent" gorm:"type:text"`
}

// TableName specifies the table name for GORM
func (*Session) TableName() string {
	return "sessions"
}

// IsExpired returns true if the session has expired (now >= ExpiresAt).
func (s *Session) IsExpired() bool {
	return time.Now().After(s.ExpiresAt)
}

// AdminSession represents an admin session.
// IsAdmin is derived from Session.Role and always kept consistent.
type AdminSession struct {
	Session
}

// IsAdmin returns true if the session role is admin.
func (a *AdminSession) IsAdmin() bool {
	return a.Role == RoleAdmin
}

// MarshalJSON includes an "is_admin" field derived from Role for API consumers.
func (a *AdminSession) MarshalJSON() ([]byte, error) {
	type SessionAlias Session
	return json.Marshal(&struct {
		SessionAlias
		IsAdmin bool `json:"is_admin"`
	}{
		SessionAlias: SessionAlias(a.Session),
		IsAdmin:      a.Role == RoleAdmin,
	})
}

// Playlist represents a user playlist
type Playlist struct {
	ID          string `json:"id" db:"id" gorm:"primaryKey;size:255"`
	Name        string `json:"name" db:"name" gorm:"size:255;not null"`
	Description string `json:"description,omitempty" db:"description" gorm:"type:text"`
	UserID      string `json:"user_id" db:"user_id" gorm:"size:255;not null;index"`
	// NOTE: Items is a one-to-many relationship. Use Preload("Items") when querying to load playlist items.
	Items      []PlaylistItem `json:"items" gorm:"foreignKey:PlaylistID"`
	CreatedAt  time.Time      `json:"created_at" db:"created_at" gorm:"autoCreateTime"`
	ModifiedAt time.Time      `json:"modified_at" db:"modified_at" gorm:"autoUpdateTime"`
	IsPublic   bool           `json:"is_public" db:"is_public" gorm:"default:false"`
	CoverImage string         `json:"cover_image,omitempty" db:"cover_image" gorm:"size:1024"`
}

// TableName specifies the table name for GORM
func (Playlist) TableName() string {
	return "playlists"
}

// PlaylistItem represents an item in a playlist
type PlaylistItem struct {
	ID         string    `json:"id,omitempty" db:"id" gorm:"primaryKey;size:255"`
	PlaylistID string    `json:"playlist_id,omitempty" db:"playlist_id" gorm:"size:255;not null;index"`
	MediaID    string    `json:"media_id" db:"media_id" gorm:"size:255;not null"`
	MediaPath  string    `json:"-" db:"media_path" gorm:"size:1024;not null"`
	Title      string    `json:"title" db:"title" gorm:"size:500"`
	Position   int       `json:"position" db:"position" gorm:"default:0"`
	AddedAt    time.Time `json:"added_at" db:"added_at" gorm:"autoCreateTime"`
}

// TableName specifies the table name for GORM
func (PlaylistItem) TableName() string {
	return "playlist_items"
}

// AnalyticsEvent represents a tracked event
type AnalyticsEvent struct {
	ID        string                 `json:"id" db:"id" gorm:"primaryKey;size:255"`
	Type      string                 `json:"type" db:"type" gorm:"size:100;not null;index"`
	MediaID   string                 `json:"media_id,omitempty" db:"media_id" gorm:"size:255;index"`
	UserID    string                 `json:"user_id,omitempty" db:"user_id" gorm:"size:255;index"`
	SessionID string                 `json:"session_id,omitempty" db:"session_id" gorm:"size:255"`
	IPAddress string                 `json:"ip_address" db:"ip_address" gorm:"size:45"`
	UserAgent string                 `json:"user_agent" db:"user_agent" gorm:"type:text"`
	Timestamp time.Time              `json:"timestamp" db:"timestamp" gorm:"autoCreateTime;index"`
	Data      map[string]interface{} `json:"data,omitempty" db:"data" gorm:"type:json;serializer:json"`
}

// TableName specifies the table name for GORM
func (AnalyticsEvent) TableName() string {
	return "analytics_events"
}

// ViewStats holds view statistics for a media item
type ViewStats struct {
	TotalViews       int       `json:"total_views"`
	TotalPlaybacks   int       `json:"-"` // playback attempts (denominator for CompletionRate)
	TotalCompletions int       `json:"-"` // completions (progress >= 90%)
	CompletionRate   float64   `json:"completion_rate"`
	LastViewed       time.Time `json:"last_viewed"`
	UniqueViewers    int       `json:"unique_viewers"`
	AvgWatchDuration float64   `json:"avg_watch_duration"`
	PeakConcurrent   int       `json:"peak_concurrent"` // populated by streaming.Module.GetStats()
}

// DailyStats holds daily aggregate statistics
type DailyStats struct {
	Date           string   `json:"date"` // "YYYY-MM-DD" date string, not a timestamp
	TotalViews     int      `json:"total_views"`
	UniqueUsers    int      `json:"unique_users"`
	TotalWatchTime float64  `json:"total_watch_time"`
	NewUsers       int      `json:"new_users"`
	TopMedia       []string `json:"top_media"`

	// Traffic breakdown (server-generated events)
	Logins        int `json:"logins"`
	LoginsFailed  int `json:"logins_failed"`
	Logouts       int `json:"logouts"`
	Registrations int `json:"registrations"`
	AgeGatePasses int `json:"age_gate_passes"`
	Downloads     int `json:"downloads"`
	Searches      int `json:"searches"`
}

// HLSJob represents an HLS transcoding job
type HLSJob struct {
	ID             string     `json:"id"`
	MediaPath      string     `json:"-"`
	OutputDir      string     `json:"-"`
	Status         HLSStatus  `json:"status"`
	Progress       float64    `json:"progress"`
	Qualities      []string   `json:"qualities"`
	StartedAt      time.Time  `json:"started_at"`
	CompletedAt    *time.Time `json:"completed_at,omitempty"`
	LastAccessedAt *time.Time `json:"last_accessed_at,omitempty"`
	Error          string     `json:"error,omitempty"`
	FailCount      int        `json:"fail_count,omitempty"` // Number of consecutive transcode failures; job is not retried after maxHLSFailures
	HLSUrl         string     `json:"hls_url,omitempty"`
	Available      bool       `json:"available"`
}

// HLSStatus represents the status of an HLS job
type HLSStatus string

const (
	HLSStatusPending   HLSStatus = "pending"
	HLSStatusRunning   HLSStatus = "running"
	HLSStatusCompleted HLSStatus = "completed"
	HLSStatusFailed    HLSStatus = "failed"
	HLSStatusCanceled  HLSStatus = "canceled"
)

// StreamSession represents an active media streaming session.
// Used by the streaming module (internal/streaming/streaming.go) and returned
// by the GET /api/admin/streams endpoint.
type StreamSession struct {
	ID         string    `json:"id"`
	MediaID    string    `json:"media_id"`
	UserID     string    `json:"user_id"`
	IPAddress  string    `json:"ip_address"`
	Quality    string    `json:"quality"`
	Position   float64   `json:"position"`
	StartedAt  time.Time `json:"started_at"`
	LastUpdate time.Time `json:"last_update"`
	BytesSent  int64     `json:"bytes_sent"`
}

// MatureReviewItem represents an item in the mature content review queue
type MatureReviewItem struct {
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	MediaPath  string     `json:"-"`
	DetectedAt time.Time  `json:"detected_at"`
	Confidence float64    `json:"confidence"`
	Reasons    []string   `json:"reasons"`
	ReviewedBy string     `json:"reviewed_by,omitempty"`
	ReviewedAt *time.Time `json:"reviewed_at,omitempty"`
	Decision   string     `json:"decision,omitempty"`
}

// AuditLogEntry represents an entry in the audit log
type AuditLogEntry struct {
	ID        string                 `json:"id" db:"id" gorm:"primaryKey;size:255"`
	Timestamp time.Time              `json:"timestamp" db:"timestamp" gorm:"autoCreateTime;index"`
	UserID    string                 `json:"user_id" db:"user_id" gorm:"size:255;index"`
	Username  string                 `json:"username" db:"username" gorm:"size:255"`
	Action    string                 `json:"action" db:"action" gorm:"size:255;not null;index"`
	Resource  string                 `json:"resource" db:"resource" gorm:"size:1024;index"`
	Details   map[string]interface{} `json:"details,omitempty" db:"details" gorm:"type:json;serializer:json"`
	IPAddress string                 `json:"ip_address" db:"ip_address" gorm:"size:45"`
	Success   bool                   `json:"success" db:"success" gorm:"index"`
}

// TableName specifies the table name for GORM
func (AuditLogEntry) TableName() string {
	return "audit_log"
}

// ServerStats holds server statistics
type ServerStats struct {
	Uptime         time.Duration `json:"uptime"`
	TotalVideos    int           `json:"total_videos"`
	TotalAudio     int           `json:"total_audio"`
	TotalSize      int64         `json:"total_size"`
	ActiveUsers    int           `json:"active_users"`
	ActiveStreams  int           `json:"active_streams"`
	MemoryUsage    uint64        `json:"memory_usage"`
	CPUUsage       float64       `json:"cpu_usage"`
	DiskUsage      float64       `json:"disk_usage"`
	RequestsPerSec float64       `json:"requests_per_sec"`
	BandwidthUsage int64         `json:"bandwidth_usage"`
}

// Health status constants
const (
	StatusHealthy   = "healthy"
	StatusUnhealthy = "unhealthy"
)

// HealthStatus represents the health status of a component
type HealthStatus struct {
	Name      string    `json:"name"`
	Status    string    `json:"status"`
	Message   string    `json:"message,omitempty"`
	CheckedAt time.Time `json:"checked_at"`
}

// APIResponse represents a standard API response
type APIResponse struct {
	Success bool        `json:"success"`
	Data    interface{} `json:"data,omitempty"`
	Error   string      `json:"error,omitempty"`
	Message string      `json:"message,omitempty"`
}

// SuggestionType represents a naming suggestion type
// for a media file discovered by the auto-discovery module.
// It uses dedicated types instead of raw primitives to avoid
// scattering magic strings and loosely-typed metadata keys.
type SuggestionType string

const (
	SuggestionTypeMovie     SuggestionType = "movie"
	SuggestionTypeTVEpisode SuggestionType = "tv_episode"
	SuggestionTypeMusic     SuggestionType = "music"
	SuggestionTypeUnknown   SuggestionType = "unknown"
)

// SuggestionMetadataKey represents a well-known metadata key for
// suggestions (e.g. show, season, title).
type SuggestionMetadataKey string

const (
	MetadataKeyShow    SuggestionMetadataKey = "show"
	MetadataKeySeason  SuggestionMetadataKey = "season"
	MetadataKeyEpisode SuggestionMetadataKey = "episode"
	MetadataKeyTitle   SuggestionMetadataKey = "title"
	MetadataKeyYear    SuggestionMetadataKey = "year"
	MetadataKeyArtist  SuggestionMetadataKey = "artist"
	MetadataKeyAlbum   SuggestionMetadataKey = "album"
	MetadataKeyTrack   SuggestionMetadataKey = "track"
)

// SuggestionMetadata holds key-value metadata for a suggestion.
type SuggestionMetadata map[SuggestionMetadataKey]string

type AutoDiscoverySuggestion struct {
	OriginalPath  string             `json:"original_path"`
	SuggestedName string             `json:"suggested_name"`
	SuggestedPath string             `json:"-"`
	Type          SuggestionType     `json:"type"` // movie, tv_episode, music
	Confidence    float64            `json:"confidence"`
	Metadata      SuggestionMetadata `json:"metadata,omitempty"`
}

// RemoteMediaSource represents a remote media source
type RemoteMediaSource struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	URL        string    `json:"url"`
	Type       string    `json:"type"`
	Enabled    bool      `json:"enabled"`
	LastSync   time.Time `json:"last_sync"`
	MediaCount int       `json:"media_count"`
	Status     string    `json:"status"`
	Error      string    `json:"error,omitempty"`
}
