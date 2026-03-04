/**
 * Shared TypeScript types matching the Go backend's JSON response shapes.
 * Derived from pkg/models/models.go and api/handlers/handlers.go.
 */

// ── Auth ──

export interface User {
    id: string
    username: string
    // Backend UserRole values: "admin" | "viewer" (not "user")
    role: 'admin' | 'viewer'
    // Backend json:"type" (no omitempty) — always present; default "standard"
    type: string
    email?: string
    enabled: boolean
    permissions: UserPermissions
    preferences: UserPreferences
    // Backend json:"storage_used" (no omitempty) — always present; 0 if unset
    storage_used: number
    // Backend json:"active_streams" (no omitempty) — always present; 0 if unset
    active_streams: number
    // Backend json:"created_at" (no omitempty, gorm autoCreateTime) — always present
    created_at: string
    last_login?: string
    // Backend json:"watch_history,omitempty" — only present when non-empty
    watch_history?: WatchHistoryEntry[]
}

export interface UserPermissions {
    can_stream: boolean
    can_download: boolean
    can_upload: boolean
    can_delete: boolean
    can_manage: boolean
    can_view_mature: boolean
    can_create_playlists: boolean
}

export interface UserPreferences {
    theme: 'light' | 'dark' | 'auto'
    default_quality: string
    show_mature: boolean
    // All fields below have no omitempty in the backend — always present in responses
    mature_preference_set: boolean
    // Backend field is "auto_play" (canonical); backend also accepts "autoplay" alias on write
    auto_play: boolean
    // Backend serializes as "equalizer_preset"; also accepted as "equalizer_bands" on write
    equalizer_preset: string
    resume_playback: boolean
    show_analytics: boolean
    items_per_page: number
    view_mode: string
    playback_speed: number
    volume: number
    language: string
    sort_by: string
    sort_order: string
    filter_category: string
    filter_media_type: string
    // custom_eq_presets has omitempty — only present when non-empty
    custom_eq_presets?: Record<string, unknown>
    // Home section visibility (default true = show section)
    show_continue_watching: boolean
    show_recommended: boolean
    show_trending: boolean
}

// GET /api/auth/session response — session check envelope (not a Session object)
// Admin branch returns full permissions (all true) and default preferences — fixed to match makeSessionAuth.
export interface SessionCheckResponse {
    authenticated: boolean
    allow_guests: boolean
    user?: User
}

// Both admin and user login branches always return role and expires_at (RFC3339 string).
export interface LoginResponse {
    session_id: string
    is_admin: boolean
    username: string
    role: string
    expires_at: string
}

// ── Media ──
export interface UserSession {
    id: string
    user_id: string
    username: string
    role: string
    created_at: string
    expires_at: string
    last_activity: string
    ip_address: string
    user_agent: string
}

// ── Media ──

// Backend models.MediaCategory JSON fields (from pkg/models/models.go)
export interface MediaCategory {
    name: string
    display_name: string
    count: number
    tags?: string[]
}

// Backend models.MediaItem JSON fields (from pkg/models/models.go)
// size/bitrate are int64 on the backend — safe in practice (< 2^53 for any real media file).
// date_added/date_modified are RFC3339 strings — use new Date(item.date_added), never parseInt.
export interface MediaItem {
    id: string
    // Backend uses "name" not "title" or "filename"
    name: string
    type: 'video' | 'audio' | 'unknown'
    size: number
    duration: number
    width?: number
    height?: number
    bitrate?: number
    codec?: string
    // Backend uses "container" not "format"
    container?: string
    category?: string
    tags?: string[]
    thumbnail_url?: string
    // Backend uses "date_added" and "date_modified" not "created_at"/"modified_at"
    date_added: string
    date_modified: string
    views: number
    last_played?: string
    is_mature: boolean
    mature_score?: number
    metadata?: Record<string, string>
}

// Handler now guarantees items is [] not null (nil guard added to ListMedia handler).
export interface MediaListResponse {
    items: MediaItem[]
    total_items: number
    total_pages: number
    // Note: backend does not return page or per_page — track these client-side
    // scanning is always present — true while the server's initial media scan is still running
    scanning: boolean
    // initializing is present (and true) only while the first-ever scan is still running.
    // Once the initial scan completes, this field is omitted.
    initializing?: boolean
}

export interface MediaListParams {
    page?: number
    limit?: number
    sort?: string
    // Backend query param is "sort_order" not "order"
    sort_order?: string
    type?: string
    category?: string
    search?: string
    // DEPRECATED: IC-12 — duplicate of is_mature below; the backend filter param is is_mature
    mature?: string
    // Comma-separated tag filter (e.g. "comedy,drama")
    tags?: string
    // Filter by mature flag: "true" or "false"
    is_mature?: string
}

// Admin media list response — matches updated AdminListMedia handler returning pagination metadata.
export interface AdminMediaListResponse {
    items: MediaItem[]
    total_items: number
    total_pages: number
}

// Parameters for admin media listing — supports full sort/filter like the public endpoint.
export interface AdminMediaListParams {
    page?: number
    limit?: number
    sort?: string
    sort_order?: string
    type?: string
    category?: string
    search?: string
    tags?: string
    is_mature?: string
}

// Matches internal/media/discovery.go Stats struct JSON tags
export interface MediaStats {
    total_count: number
    video_count: number
    audio_count: number
    total_size: number
    last_scan: string
}

// ── HLS ──

export interface HLSCapabilities {
    enabled: boolean
    available: boolean
    ffmpeg_found: boolean
    ffprobe_found: boolean
    healthy: boolean
    message: string
    qualities: string[]
    auto_generate: boolean
    max_concurrent: number
}

// CheckHLSAvailability handler always returns all fields below (none are conditionally omitted).
// qualities is guaranteed [] not null (nil guard added to handler).
export interface HLSAvailability {
    available: boolean
    hls_url: string
    id: string
    job_id: string   // alias for id — backend sends both
    status: string
    progress: number
    qualities: string[]
    started_at: string
    error: string
}

// All handler responses nil-guard qualities to [] — never null.
// started_at is always present (time.Time → RFC3339 string).
// completed_at is conditionally present (only when job finished).
// error: GetHLSStatus and GenerateHLS always include it; ListHLSJobs serializes
// models.HLSJob directly which has json:"error,omitempty" — absent when empty.
export interface HLSJob {
    id: string
    status: 'pending' | 'running' | 'completed' | 'failed' | 'cancelled'
    progress: number
    qualities: string[]
    started_at: string
    completed_at?: string
    hls_url?: string
    available: boolean
    error?: string
    fail_count?: number
}

// ── Playlists ──

// Backend models.Playlist JSON fields
// items may be null when not preloaded — callers must guard: (playlist.items ?? []).map(...)
export interface Playlist {
    id: string
    name: string
    description?: string
    // Backend uses "user_id" not "owner"
    user_id: string
    items: PlaylistItem[] | null
    created_at: string
    // Backend uses "modified_at" not "updated_at"
    modified_at: string
    // Backend json:"is_public" has no omitempty — always present (true or false)
    is_public: boolean
    cover_image?: string
}

// Backend models.PlaylistItem JSON fields
export interface PlaylistItem {
    id?: string
    playlist_id?: string
    media_id: string
    title: string
    position: number
    added_at: string
}

// ── Analytics ──

export interface AnalyticsEvent {
    id: string
    type: string
    media_id?: string
    user_id?: string
    session_id?: string
    ip_address: string
    user_agent: string
    timestamp: string
    data?: Record<string, unknown>
}

// Backend GetAnalyticsSummary returns this shape (from handlers.go)
// When analytics is disabled, only analytics_disabled:true is present; all numeric fields absent.
// Guard: always check !summary.analytics_disabled before accessing numeric fields.
export interface AnalyticsSummary {
    analytics_disabled?: boolean
    total_events?: number
    active_sessions?: number
    today_views?: number
    total_views?: number
    total_media?: number
    unique_clients?: number
    top_viewed?: Array<{ media_id: string; filename: string; views: number }>
    recent_activity?: Array<{ type: string; media_id: string; filename: string; timestamp: number }>
}

// ── Watch History ──

// Backend models.WatchHistoryItem JSON fields
export interface WatchHistoryEntry {
    // Backend models.WatchHistoryItem.MediaID string json:"media_id" (no omitempty) — always present
    media_id: string
    // Backend json:"media_name,omitempty" — human-readable filename, populated by GetWatchHistory
    media_name?: string
    position: number
    duration: number
    // Backend uses "progress" (float ratio 0-1) not "completion"
    progress: number
    // Backend uses "watched_at" not "last_watched"
    watched_at: string
    completed: boolean
}

// ── Server Settings ──
// Matches api/handlers/system.go GetServerSettings response shape

export interface ServerSettings {
    thumbnails: {
        enabled: boolean
        autoGenerate: boolean
        width: number
        height: number
        video_preview_count: number
    }
    streaming: {
        mobileOptimization: boolean
    }
    analytics: {
        enabled: boolean
    }
    features: {
        enableThumbnails: boolean
        enableHLS: boolean
        enableAnalytics: boolean
    }
    uploads: {
        enabled: boolean
        maxFileSize: number
    }
    admin: {
        enabled: boolean
    }
    ui: {
        items_per_page: number
        mobile_items_per_page: number
        mobile_grid_columns: number
    }
    age_gate: {
        enabled: boolean
    }
}

// ── Age Gate ──

export interface AgeGateStatus {
    enabled: boolean
    verified: boolean
}

// ── Suggestions ──

// Backend suggestions.Suggestion JSON fields (internal/suggestions/suggestions.go)
export interface Suggestion {
    media_id: string
    title: string
    category: string
    media_type: string
    score: number
    reasons: string[] | null
    thumbnail_url?: string
}

// ── Admin ──

// disk_usage/disk_total/disk_free are uint64 bytes — safe in practice (< 9 PB).
// disk_usage = used bytes (not a ratio); use (disk_usage / disk_total * 100) for percentage.
export interface AdminStats {
    total_videos: number
    total_audio: number
    active_sessions: number
    total_users: number
    disk_usage: number
    disk_total: number
    disk_free: number
    hls_jobs_running: number
    hls_jobs_completed: number
    server_uptime: number
    total_views: number
}

// Matches AdminGetSystemInfo response (handlers.go). uptime is seconds since server start.
// memory_total = runtime.MemStats.Sys (Go runtime reservation from OS, not total installed RAM).
// memory_used = runtime.MemStats.Alloc (bytes currently in use by Go heap).
export interface SystemInfo {
    version: string
    build_date: string
    os: string
    arch: string
    go_version: string
    cpu_count: number
    memory_used: number
    memory_total: number
    uptime: number
    modules: ModuleHealth[]
}

export interface ModuleHealth {
    name: string
    // Backend constants: "healthy" | "unhealthy". "degraded"/"failed"/"disabled" kept for display logic.
    status: 'healthy' | 'unhealthy' | 'degraded' | 'failed' | 'disabled'
    message?: string
    last_check?: string
}

// Backend models.AuditLogEntry JSON fields
export interface AuditLogEntry {
    id: string
    timestamp: string
    // Backend uses "username" not "admin"
    username: string
    user_id: string
    action: string
    // Backend uses "resource" not "target"
    resource: string
    // TODO: details is declared as optional here but is ALWAYS absent from the backend response.
    // models.AuditLogEntry.Details has gorm:"-" and db:"-" tags, so it is never written to or
    // read from MySQL. admin.LogAction() assigns values to this field in-memory, but they are
    // lost immediately and never serialized. The audit log UI always renders an empty details
    // section. Remove this field from the interface (or mark it as permanently absent) until
    // backend persistence for AuditLogEntry.Details is implemented.
    details?: Record<string, unknown>
    // Backend uses "ip_address" not "ip"
    ip_address?: string
    // Backend models.AuditLogEntry.Success bool json:"success" (no omitempty) — always present
    success: boolean
}

export interface LogEntry {
    timestamp: string
    level: string
    module: string
    message: string
    // Backend parseLogLine always includes unparsed "raw" line
    raw?: string
}

export interface ServerConfig {
    [key: string]: unknown
}

// tasks.TaskInfo.LastError has json:"last_error,omitempty" — absent when no error, optional is correct.
export interface ScheduledTask {
    id: string
    name: string
    description: string
    schedule: string   // backend json:"schedule" (was incorrectly "interval")
    last_run: string   // always present; zero value "0001-01-01T00:00:00Z" = never run
    next_run: string
    enabled: boolean
    running: boolean
    last_error?: string
}

// Matches backend models.BackupInfo JSON tags.
// created_at is RFC3339 — use new Date(entry.created_at). type defaults to "full" if not specified.
export interface BackupEntry {
    id: string
    filename: string
    size: number
    created_at: string
    type: string
    // Backend json:"description,omitempty" — absent when empty
    description?: string
}

export interface ScannerStats {
    total_scanned: number
    mature_count: number
    auto_flagged: number
    pending_review: number
}

// Matches backend models.MatureReviewItem JSON
export interface ScanResultItem {
    id: string
    name: string
    detected_at: string
    confidence: number
    reasons: string[] | null
    reviewed_by?: string
    reviewed_at?: string
    decision?: string
}

// Matches api/handlers/system.go AdminGetDatabaseStatus response
export interface DatabaseStatus {
    connected: boolean
    host: string
    database: string
    app_version: string
    repository_type: string
    message: string
    checked_at: string
}

export interface QueryResult {
    columns?: string[]
    rows?: unknown[][]
    rows_affected?: number
    message?: string
    error?: string
    truncated?: boolean
}

// ── HLS Admin Stats ──

export interface HLSStats {
    total_jobs: number
    running_jobs: number
    completed_jobs: number
    failed_jobs: number
    pending_jobs: number
    cache_size_bytes: number
}

// ── Analytics ──

// Matches pkg/models/models.go DailyStats. date is a "YYYY-MM-DD" date string.
export interface DailyStats {
    date: string
    total_views: number
    // Backend models.DailyStats.UniqueUsers int json:"unique_users" (no omitempty) — always present
    unique_users: number
    // Backend models.DailyStats.TotalWatchTime float64 json:"total_watch_time" (no omitempty) — always present
    total_watch_time: number
    // Backend models.DailyStats.NewUsers int json:"new_users" (no omitempty) — always present
    new_users: number
    // Backend models.DailyStats.TopMedia []string json:"top_media" (no omitempty) — always present (empty slice, not null)
    top_media: string[]
}

export interface TopMediaItem {
    media_id: string
    filename: string
    views: number
}

export interface EventTypeCounts {
    [eventType: string]: number
}

// ── Remote Sources ──

export interface RemoteSource {
    name: string
    url: string
    username?: string
    password?: string
    enabled: boolean
}

// Matches internal/remote/remote.go MediaItem struct (distinct from models.MediaItem)
export interface RemoteMediaItem {
    id: string
    name: string
    url: string
    source_name: string
    size: number
    content_type: string
    duration?: number
    metadata?: Record<string, string>
    cached_at?: string
}

export interface RemoteSourceState {
    source: RemoteSource
    status: string       // "idle" | "syncing" | "error"
    // last_sync is always an ISO timestamp string; zero value is "0001-01-01T00:00:00Z" (never synced)
    last_sync: string
    media_count: number
    error?: string
    media?: RemoteMediaItem[]
}

export interface RemoteStats {
    source_count: number
    cached_item_count: number
    total_media_count: number
    cache_size: number  // backend json tag is "cache_size" (remote.Stats.CacheSize)
    sources: Array<{
        name: string
        status: string
        media_count: number
        last_sync: string
        error?: string
    }>
}

// ── Receiver (master/slave) ──

// Matches internal/receiver/receiver.go SlaveNode struct
export interface SlaveNode {
    id: string
    name: string
    base_url: string
    // "online" | "offline" | "stale"
    status: string
    media_count: number
    last_seen: string
    registered_at: string
}

// Matches internal/receiver/receiver.go MediaItem struct
export interface ReceiverMediaItem {
    id: string
    slave_id: string
    slave_name?: string
    path: string
    name: string
    media_type: string
    size: number
    duration: number
    content_type: string
    content_fingerprint?: string
    width: number
    height: number
}

// Matches internal/receiver/receiver.go Stats struct
export interface ReceiverStats {
    slave_count: number
    online_slaves: number
    media_count: number
}

// ── Feature 1: Storage & Permissions ──

export interface StorageUsage {
    used_bytes: number
    used_gb: number
    quota_gb: number
    percentage: number
    user_type: string
    is_authenticated: boolean
}

export interface PermissionsInfo {
    authenticated: boolean
    username?: string
    role?: string
    user_type?: string
    show_mature: boolean
    mature_preference_set: boolean
    capabilities: {
        canUpload: boolean
        canDownload: boolean
        canCreatePlaylists: boolean
        canViewMature: boolean
        canStream: boolean
        canDelete?: boolean
        canManage?: boolean
    }
    limits?: {
        // storage_quota is in bytes (e.g. 1073741824 = 1 GB). Divide by 1073741824 for display in GB.
        storage_quota: number
        concurrent_streams: number
    }
}

// ── Feature 2: Ratings ──

// ── Feature 4: Upload ──

export interface UploadResult {
    uploaded: Array<{ filename: string; size: number }>
    errors: Array<{ filename: string; error: string }>
}

export interface UploadProgress {
    id: string
    filename: string
    size: number
    uploaded: number
    progress: number
    status: string
    started_at: string
    completed_at?: string
    error?: string
    user_id: string
}

// Matches pkg/models StreamSession — returned by AdminGetActiveStreams
export interface StreamSession {
    id: string
    media_id: string
    user_id: string
    ip_address: string
    quality: string
    position: number
    started_at: string
    last_update: string
    bytes_sent: number
}

// ── Feature 5: Analytics Detail ──

// Backend analytics.EventStats shape (handlers.go GetEventStats)
export interface EventStats {
    total_events: number
    event_counts: Record<string, number>
    hourly_events: number[]
}

// ── Feature 6: Admin Playlists ──
// AdminListPlaylists returns []*models.Playlist — reuse Playlist type directly (see above).
// AdminPlaylistEntry is intentionally removed; use Playlist[] for admin playlist lists.

// Backend playlist.Stats shape
export interface AdminPlaylistStats {
    total_playlists: number
    public_playlists: number
    total_items: number
}

// ── Feature 7: Thumbnail Stats ──

export interface ThumbnailStats {
    total_thumbnails: number
    total_size_mb: number
    pending_generation: number
    generation_errors: number
}

// ── Feature 8: HLS Validation ──

// Matches internal/hls ValidationResult JSON tags
// errors uses omitempty — absent when validation passes. Callers must guard: (result.errors ?? [])
export interface HLSValidationResult {
    job_id: string
    valid: boolean
    variant_count: number
    segment_count: number
    errors?: string[]
}

// ── Feature 9: Suggestion Stats ──

// Matches internal/suggestions SuggestionStats JSON tags
export interface SuggestionStats {
    total_profiles: number
    total_media: number
    total_views: number
    total_watch_time: number
}

// ── Feature 10: Security ──

export interface SecurityStats {
    banned_ips: number
    whitelisted_ips: number
    blacklisted_ips: number
    active_rate_limits: number
    total_blocks_today: number
}

export interface IPEntry {
    ip: string
    comment?: string
    added_by?: string
    added_at: string
    expires_at?: string
}

export interface BannedIP {
    ip: string
    // Timestamp when the ban was created (RFC3339). Stored in the BanRecord at ban time.
    banned_at: string
    expires_at?: string
    // Reason for the ban. Manual bans default to "Manual ban"; auto-bans use the triggering reason.
    // The POST /api/admin/security/ban endpoint accepts an optional "reason" field in the body.
    reason: string
}

// ── Feature 11: Categorizer ──

// Matches internal/categorizer MediaInfo struct (all fields omitempty → optional)
export interface DetectedMediaInfo {
    title?: string
    year?: number
    season?: number
    episode?: number
    show_name?: string
    artist?: string
    album?: string
}

export interface CategorizedItem {
    id: string
    name: string
    category: string
    confidence: number
    detected_info?: DetectedMediaInfo
    categorized_at: string
    manual_override: boolean
}

// Matches internal/categorizer CategoryStats JSON tags
export interface CategoryStats {
    total_items: number
    by_category: Record<string, number>
    manual_overrides: number
}

// ── Feature 11b: Media Validator ──

// Matches internal/validator ValidationResult JSON tags
export interface ValidationResult {
    status: string
    validated_at: string
    duration: number
    video_codec?: string
    audio_codec?: string
    width?: number
    height?: number
    bitrate?: number
    container?: string
    issues?: string[]
    error?: string
    video_supported: boolean
    audio_supported: boolean
}

// Matches internal/validator Stats JSON tags
export interface ValidatorStats {
    total: number
    validated: number
    needs_fix: number
    fixed: number
    failed: number
    unsupported: number
}

// ── Cached Remote Media ──

// Matches internal/remote CachedMedia struct returned by CacheRemoteMedia handler
export interface CachedMediaResult {
    remote_url: string
    size: number
    content_type: string
    cached_at: string
    last_access: string
    hits: number
}

// ── Thumbnail Previews ──

// Matches api/handlers/thumbnails.go GetThumbnailPreviews response
export interface ThumbnailPreviews {
    previews: string[]
}

// ── Feature 12: Auto-Discovery ──

// Matches pkg/models AutoDiscoverySuggestion JSON tags
export interface DiscoverySuggestion {
    original_path: string
    suggested_name: string
    type: string
    confidence: number
    metadata?: Record<string, string>
}
