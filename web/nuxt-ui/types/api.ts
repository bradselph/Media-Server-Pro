import type {components} from '~/types/openapi.generated'

// Re-export generated schema types — these are derived from api_spec/openapi.yaml.
// Prefer these over hand-written duplicates when schemas match the backend exactly.
export type {components} from '~/types/openapi.generated'
export type GeneratedSchemas = components['schemas']

// ── Auth ──────────────────────────────────────────────────────────────────────

export type UserRole = 'admin' | 'viewer'

export interface UserPermissions {
    user_id?: string
    can_stream: boolean
    can_download: boolean
    can_upload: boolean
    can_delete: boolean
    can_manage: boolean
    can_view_mature: boolean
    can_create_playlists: boolean
}

export interface UserPreferences {
    user_id?: string
    theme: string
    view_mode: 'grid' | 'list' | 'compact'
    default_quality: string
    auto_play: boolean
    playback_speed: number
    volume: number
    show_mature: boolean
    mature_preference_set: boolean
    language: string
    equalizer_preset: string
    resume_playback: boolean
    show_analytics: boolean
    items_per_page: number
    sort_by: string
    sort_order: string
    filter_category: string
    filter_media_type: string
    custom_eq_presets?: Record<string, unknown>
    show_continue_watching: boolean
    show_recommended: boolean
    show_trending: boolean
    skip_interval: number
    shuffle_enabled: boolean
    show_buffer_bar: boolean
    download_prompt: boolean
}

export interface User {
    id: string
    username: string
    email?: string
    role: UserRole
    type: string
    enabled: boolean
    created_at: string
    last_login?: string
    previous_last_login?: string
    storage_used: number
    active_streams: number
    watch_history?: WatchHistoryItem[]
    permissions: UserPermissions
    preferences: UserPreferences
    metadata?: Record<string, unknown>
}

export interface UserProfile {
    user_id: string
    total_views: number
    total_watch_time: number
    category_scores: Record<string, number>
    type_preferences: Record<string, number>
    last_updated?: string
}

export interface LoginResponse {
    session_id: string
    username: string
    role: UserRole
    is_admin: boolean
    expires_at: string
}

export interface SessionCheckResponse {
    authenticated: boolean
    allow_guests: boolean
    user?: User
}

// ── Media ─────────────────────────────────────────────────────────────────────

export interface MediaItem {
    id: string
    name: string
    type: 'video' | 'audio' | 'unknown'
    size: number
    duration: number
    width?: number
    height?: number
    bitrate?: number
    codec?: string
    container?: string
    category?: string
    tags?: string[]
    thumbnail_url?: string
    blur_hash?: string
    date_added: string
    date_modified: string
    views: number
    last_played?: string
    is_mature: boolean
    mature_score?: number
    metadata?: Record<string, string>
}

export interface MediaChapter {
    id: string
    media_id: string
    start_time: number
    end_time?: number
    label: string
    created_at: string
}

export interface SmartCondition {
    field: string
    op: string
    value: string
}

export interface SmartPlaylistRules {
    match: 'all' | 'any'
    conditions: SmartCondition[]
    order_by: 'date_added' | 'name' | 'duration' | 'views'
    order_dir: 'asc' | 'desc'
    limit: number
}

export interface SmartPlaylist {
    id: string
    name: string
    description?: string
    user_id: string
    rules: string  // JSON string
    created_at: string
    updated_at: string
}

export interface MediaCollectionItem {
    media_id: string
    media_name?: string
    position: number
}

export interface MediaCollection {
    id: string
    name: string
    description?: string
    cover_media_id?: string
    items?: MediaCollectionItem[]
    created_at: string
    updated_at: string
}

export interface AutoTagRule {
    id: string
    name: string
    pattern: string
    tags: string  // comma-separated
    priority: number
    enabled: boolean
    created_at: string
    updated_at: string
}

export interface MediaListParams {
    page?: number
    limit?: number
    search?: string
    type?: string
    category?: string
    /** Sent to the API as query param `sort` (backend contract). */
    sort?: string
    /** Alias for `sort`; mapped to `sort` on the wire. */
    sort_by?: string
    sort_order?: string
    mature?: boolean
    /** Filter to items the user has rated at or above this value (1–5). */
    min_rating?: number
    /** Filter by tags (OR logic — item must have at least one). Serialised as comma-joined string. */
    tags?: string[]
    /** Exclude items the authenticated user has already completed watching. */
    hide_watched?: boolean
}

export interface MediaListResponse {
    items: MediaItem[]
    total_items: number
    total_pages: number
    scanning?: boolean
    initializing?: boolean
    /** Map of media_id → user's rating (1–5). Only present for authenticated users who have rated items. */
    user_ratings?: Record<string, number>
    /** Item count per media type (video/audio/image) across the full unfiltered library. Only present on unfiltered (type=all) requests. */
    type_counts?: Record<string, number>
}

export interface MediaCategory {
    name: string
    display_name: string
    count: number
    tags?: string[]
}

export interface AdminMediaListResponse {
    items: MediaItem[]
    total_items: number
    total_pages: number
}

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

// ── HLS ───────────────────────────────────────────────────────────────────────

export interface HLSAvailability {
    available: boolean
    hls_url: string
    id: string
    job_id: string
    status: string
    progress: number
    qualities: string[]
    started_at: string
    error: string
}

export interface HLSJob {
    id: string
    status: 'pending' | 'running' | 'completed' | 'failed' | 'canceled'
    progress: number
    qualities: string[]
    started_at: string
    completed_at?: string
    last_accessed_at?: string
    error?: string
    fail_count?: number
    hls_url?: string
    available: boolean
}

export interface HLSStats {
    total_jobs: number
    running_jobs: number
    completed_jobs: number
    failed_jobs: number
    pending_jobs: number
    cache_size_bytes: number
}

// ── Playlists ─────────────────────────────────────────────────────────────────

export interface Playlist {
    id: string
    name: string
    description?: string
    user_id: string
    items: PlaylistItem[] | null
    created_at: string
    modified_at: string
    is_public: boolean
    cover_image?: string
}

export interface PlaylistItem {
    id?: string
    playlist_id?: string
    media_id: string
    title: string
    position: number
    added_at: string
}

// ── Analytics ─────────────────────────────────────────────────────────────────

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

export interface AnalyticsSummary {
    total_events: number
    active_sessions: number
    today_views: number
    total_views: number
    total_media: number
    total_watch_time: number
    unique_clients: number
    today_logins?: number
    today_logins_failed?: number
    today_logouts?: number
    today_registrations?: number
    today_age_gate_passes?: number
    today_downloads?: number
    today_searches?: number
    today_favorites_added?: number
    today_favorites_removed?: number
    today_ratings_set?: number
    today_playlists_created?: number
    today_playlists_deleted?: number
    today_playlist_items_added?: number
    today_uploads_succeeded?: number
    today_uploads_failed?: number
    today_password_changes?: number
    today_account_deletions?: number
    today_hls_starts?: number
    today_hls_errors?: number
    today_media_deletions?: number
    today_api_tokens_created?: number
    today_api_tokens_revoked?: number
    today_admin_actions?: number
    today_server_errors?: number
    today_stream_starts?: number
    today_stream_ends?: number
    today_bytes_served?: number
    today_mature_blocked?: number
    today_permission_denied?: number
    today_preferences_changes?: number
    today_bulk_deletes?: number
    today_bulk_updates?: number
    today_user_role_changes?: number
    top_viewed: TopMediaItem[]
    // Recent activity carries optional user/IP context so non-media events
    // (login, register, admin_action, etc.) render with attribution rather
    // than a blank filename row.
    recent_activity: {
        type: string
        media_id: string
        filename: string
        user_id?: string
        username?: string
        ip_address?: string
        timestamp: number
    }[]
}

// Top-N user leaderboard entry returned by /admin/analytics/top-users.
// `metric` carries whichever ranking value the caller asked for (views,
// watch_time, uploads, downloads, events) so the UI can render a single
// "value" column without a separate switch on metric name.
export interface TopUserEntry {
    user_id: string
    username?: string
    metric: number
    total_views: number
    total_watch_time: number
    total_uploads: number
    total_downloads: number
    total_events: number
}

// Top-search bucket. `empty_count` is how many of the `count` occurrences
// returned zero results, so the dashboard can flag "popular but unanswered"
// queries — a strong product signal.
export interface SearchQueryEntry {
    query: string
    count: number
    empty_count: number
}

// Recent failed login attempt for the security review panel.
export interface FailedLoginEntry {
    ip_address: string
    username?: string
    user_agent?: string
    timestamp: string
    reason?: string
}

// 5xx error grouped by (method, path, status).
export interface ErrorPathEntry {
    method: string
    path: string
    status: number
    count: number
    last_seen: string
}

// One bucket on a per-day metric timeline. Always gap-filled — the backend
// emits a zero entry for missing days so chart axes are even.
export interface MetricTimelineEntry {
    date: string
    value: number
}

// Rolling unique-user counts. Stickiness ratios are 0..1 (DAU/WAU and
// DAU/MAU) — useful to detect engagement decay independent of absolute
// install size.
export interface CohortMetrics {
    dau: number
    wau: number
    mau: number
    stickiness_dau_wau: number
    stickiness_dau_mau: number
}

// One cell on the day-of-week × hour heatmap. day_of_week follows
// time.Weekday (0=Sunday … 6=Saturday); hour is 0..23 in server local time.
export interface HourlyHeatmapCell {
    day_of_week: number
    hour: number
    count: number
}

// One row of the stream-quality breakdown.
export interface QualityBucket {
    quality: string
    streams: number
    bytes_sent: number
}

// Period-over-period comparison for a single DailyStats metric.
export interface PeriodComparison {
    metric: string
    current: number
    previous: number
    delta_absolute: number
    delta_pct: number
    window_days: number
}

// One stage of a conversion funnel. from_top_pct is relative to the first
// stage (always 100); from_previous_pct is the step-over-step rate, which
// is what the dashboard shades to highlight drop-off.
export interface FunnelStage {
    stage: string
    count: number
    from_previous_pct: number
    from_top_pct: number
}

// View → Playback → Completion funnel split by overall / authenticated /
// anonymous traffic. Anonymous traffic typically converts differently (the
// auth gate is itself a step) so it gets its own row.
export interface Funnel {
    window_days: number
    stages: FunnelStage[]
    authenticated: FunnelStage[]
    anonymous: FunnelStage[]
}

// Device or browser breakdown row.
export interface DeviceBucket {
    family: string
    events: number
    unique_users: number
}

// One bucket of the playback abandonment histogram. The dashboard
// renders the 10 buckets as a horizontal bar chart so curators can see
// where viewers drop off in a media item.
export interface AbandonmentBucket {
    range: string
    count: number
}

// One IP's traffic snapshot: total events, unique users behind that IP,
// total bytes streamed (from stream_end events), and last-seen timestamp.
export interface IPBucket {
    ip_address: string
    events: number
    unique_user_ids: number
    bytes_sent: number
    last_seen: string
}

export interface IPSummary {
    unique_ips: number
    top_by_events: IPBucket[]
    top_by_bytes: IPBucket[]
}

// Analytics module's internal counters — exposed so admins can debug
// "why is the dashboard slow / stale" without server log access.
export interface ModuleDiagnostics {
    cache_entries: number
    dirty_days: number
    active_subscribers: number
    sessions_tracked: number
    media_tracked: number
    max_reconstruct_events: number
    healthy: boolean
}

// Linear-trend projection for one metric — slope per day, tomorrow's
// projected value, and a residual-stddev confidence band. Direction is
// "up" / "down" / "flat" based on a small threshold around the mean so a
// noisy series isn't declared trending.
export interface MetricForecast {
    metric: string
    window_days: number
    slope: number
    intercept: number
    projection: number
    confidence_band: number
    direction: 'up' | 'down' | 'flat'
}

// Anomaly detection — one daily metric whose value is statistically
// far from its rolling baseline. The dashboard renders these as a
// banner at the top so admins notice incidents within a day.
export interface Anomaly {
    date: string
    metric: string
    value: number
    baseline: number
    std_dev: number
    z_score: number
    direction: 'spike' | 'dip'
}

export interface AnomalyReport {
    window_days: number
    anomalies: Anomaly[]
}

// Cohort retention grid. Rows are signup-week cohorts (oldest first);
// each row carries the percentage of users still active in week-N. The
// matrix is upper-triangular by construction — older cohorts have more
// retention buckets than younger ones.
export interface RetentionCohort {
    cohort_start: string
    cohort_size: number
    retention: number[]
}

export interface RetentionGrid {
    cohort_weeks: number
    weeks: RetentionCohort[]
}

// Per-media analytics drill payload — the cached stats plus a 30-day
// view + playback timeline so the modal can render a sparkline.
export interface MediaDetail {
    media_id: string
    stats: {
        total_views: number
        completion_rate: number
        last_viewed: string
        unique_viewers: number
        avg_watch_duration: number
        peak_concurrent: number
    }
    view_timeline: MetricTimelineEntry[]
    playback_timeline: MetricTimelineEntry[]
    abandonment: AbandonmentBucket[]
}

// Per-user aggregated analytics returned by /admin/users/:username/analytics.
// Field names mirror the Go UserStats struct so the codegen pipeline doesn't
// need a translation step. All counts default to 0 for inactive users.
export interface UserAnalytics {
    user_id: string
    total_events: number
    total_views: number
    total_playbacks: number
    total_completions: number
    total_watch_time: number
    total_downloads: number
    total_searches: number
    favorites_added: number
    favorites_removed: number
    ratings_set: number
    playlists_created: number
    playlists_deleted: number
    uploads_succeeded: number
    uploads_failed: number
    logins: number
    logins_failed: number
    logouts: number
    unique_media: number
    first_seen?: string
    last_seen?: string
    most_viewed_media_id?: string
    most_viewed_count: number
}

export interface ContentPerformanceItem {
    media_id: string
    filename: string
    total_views: number
    total_playbacks: number
    total_completions: number
    completion_rate: number
    avg_watch_duration: number
    unique_viewers: number
}

export interface TopMediaItem {
    media_id: string
    filename: string
    title?: string
    name?: string
    views: number
}

export interface DailyStats {
    date: string
    total_views: number
    unique_users: number
    total_watch_time: number
    new_users: number
    top_media: string[]
    // Traffic breakdown
    logins: number
    logins_failed: number
    logouts: number
    registrations: number
    age_gate_passes: number
    downloads: number
    searches: number
    // Extended traffic breakdown — every notable user/admin action is tracked
    // server-side so the dashboard reflects real activity rather than just the
    // narrow auth-and-stream subset. Every field is required (zero-defaulted)
    // because the backend always emits it.
    favorites_added: number
    favorites_removed: number
    ratings_set: number
    playlists_created: number
    playlists_deleted: number
    playlist_items_added: number
    uploads_succeeded: number
    uploads_failed: number
    password_changes: number
    account_deletions: number
    hls_starts: number
    hls_errors: number
    media_deletions: number
    api_tokens_created: number
    api_tokens_revoked: number
    admin_actions: number
    server_errors: number
    stream_starts: number
    stream_ends: number
    bytes_served: number
    mature_blocked: number
    permission_denied: number
    preferences_changes: number
    bulk_deletes: number
    bulk_updates: number
    user_role_changes: number
}

// ── Admin ─────────────────────────────────────────────────────────────────────

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

export interface ModuleHealth {
    name: string
    status: 'healthy' | 'unhealthy' | 'degraded' | 'failed' | 'disabled'
    message?: string
    last_check: string
}

export interface ServerStatus {
    running: boolean
    uptime: string
    start_time: string
    version: string
    go_version: string
    module_count: number
}

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

export interface StreamSession {
    id: string
    user_id: string
    media_id: string
    quality: string
    bytes_sent: number
    ip_address: string
    started_at: string
    position: number
    last_update: string
}

export interface UploadProgress {
    id: string
    filename: string
    user_id: string
    progress: number
    status: string
    size: number
    uploaded: number
    started_at: string
    completed_at?: string
    error?: string
}

export interface AuditLogEntry {
    id: string
    timestamp: string
    username: string
    user_id: string
    action: string
    resource: string
    details?: Record<string, unknown>
    ip_address: string
    success: boolean
}

export interface AuditLogResponse {
    items: AuditLogEntry[]
    total: number
    total_pages: number
}

export interface LogEntry {
    timestamp: string
    level: string
    module: string
    message: string
    raw?: string
}

export interface ScheduledTask {
    id: string
    name: string
    description: string
    schedule: string
    last_run: string
    next_run: string
    enabled: boolean
    running: boolean
    last_error?: string
}

export interface BackupEntry {
    id: string
    filename: string
    size: number
    created_at: string
    type: string
    description?: string
    files?: string[]
    errors?: string[]
    version?: string
}

export interface ThumbnailStats {
    total_thumbnails: number
    total_size_mb: number
    pending_generation: number
    generation_errors: number
}

export interface ScannerStats {
    total_scanned: number
    mature_count: number
    auto_flagged: number
    pending_review: number
}


// Matches the backend models.MatureReviewItem JSON response from GET /api/admin/scanner/queue
export interface ReviewQueueItem {
    id: string
    name: string
    detected_at: string
    confidence: number
    reasons: string[]
    reviewed_by?: string
    reviewed_at?: string
    decision?: string
}

export interface UpdateInfo {
    current_version: string
    latest_version: string
    update_available: boolean
    release_url?: string
    release_notes?: string
    published_at?: string
    checked_at?: string | null
    error?: string
}

export interface UpdateStatus {
    in_progress: boolean
    stage: string
    progress: number
    started_at?: string
    error?: string
    backup_path?: string
}

export interface IPListEntry {
    ip: string
    comment: string
    added_by: string
    added_at: string
    expires_at?: string
}

export interface SecurityStats {
    banned_ips: number
    whitelisted_ips: number
    blacklisted_ips: number
    active_rate_limits: number
    total_blocks_today: number
    total_rate_limited: number
    whitelist_enabled: boolean
    blacklist_enabled: boolean
    rate_limit_enabled: boolean
}

export interface DatabaseStatus {
    connected: boolean
    host: string
    database: string
    app_version: string
    repository_type: string
    message: string
    checked_at: string
}

export interface ReceiverSlave {
    id: string
    name: string
    address: string
    status: string
    last_heartbeat: string
    media_count: number
    api_key?: string
}

export interface ReceiverMedia {
    id: string
    slave_id: string
    slave_name?: string
    name: string
    path: string
    media_type: string
    size: number
    duration?: number
    content_type?: string
    content_fingerprint?: string
    width?: number
    height?: number
}

export interface CrawlerTarget {
    id: string
    url: string
    name: string
    site: string
    last_crawled?: string
    created_at: string
    enabled: boolean
}

export interface CrawlerDiscovery {
    id: string
    target_id: string
    page_url: string
    title: string
    stream_url: string
    stream_type: string
    quality: number
    status: string
    reviewed_by?: string
    reviewed_at?: string | null
    discovered_at: string
}

export interface ExtractorItem {
    id: string
    stream_url: string
    title: string
    added_by: string
    status: string
    error_message?: string
    created_at: string
}

export interface DownloaderJob {
    filename: string
    size: number
    created: number
    url: string
}

// ── Watch history ─────────────────────────────────────────────────────────────

export interface WatchHistoryItem {
    media_id: string
    media_name?: string
    position: number
    duration: number
    progress: number
    watched_at: string
    completed: boolean
}

// ── Suggestions ───────────────────────────────────────────────────────────────

// Generated from api_spec/openapi.yaml — do not edit manually
export type Suggestion = components['schemas']['Suggestion']

// ── Storage / Permissions ─────────────────────────────────────────────────────

export interface StorageUsage {
    used_bytes: number
    used_gb: number
    quota_gb: number
    percentage: number
    user_type: string
    is_authenticated: boolean
}

// Shape returned by GET /api/permissions — capabilities use camelCase keys
export interface PermissionsInfo {
    authenticated: boolean
    username?: string
    role?: string
    user_type?: string
    show_mature?: boolean
    mature_preference_set?: boolean
    capabilities: {
        canStream: boolean
        canUpload: boolean
        canDownload: boolean
        canCreatePlaylists: boolean
        canViewMature: boolean
        canDelete?: boolean
        canManage?: boolean
    }
    limits?: {
        storage_quota: number
        concurrent_streams: number
    }
}

// ── Server Config ─────────────────────────────────────────────────────────────

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
        adaptive: boolean
    }
    analytics: {
        enabled: boolean
    }
    features: {
        enableThumbnails: boolean
        enableHLS: boolean
        enableAnalytics: boolean
        enablePlaylists: boolean
        enableUserAuth: boolean
        enableAdminPanel: boolean
        enableSuggestions: boolean
        enableAutoDiscovery: boolean
        enableDuplicateDetection: boolean
        enableDownloader: boolean
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
    auth: {
        allow_registration: boolean
        allow_guests: boolean
    }
}

// ── Age Gate ──────────────────────────────────────────────────────────────────

export interface AgeGateStatus {
    enabled: boolean
    verified: boolean
}

// ── Cookie Consent ────────────────────────────────────────────────────────────

export interface CookieConsentStatus {
    required: boolean
    given: boolean
    analytics_accepted: boolean
}

// ── Media Stats ───────────────────────────────────────────────────────────────

export interface MediaStats {
    total_count: number
    video_count: number
    audio_count: number
    total_size: number
    last_scan: string
    version?: number
}

// ── HLS Capabilities ─────────────────────────────────────────────────────────

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

// ── Upload ────────────────────────────────────────────────────────────────────

export interface UploadResult {
    uploaded: Array<{ upload_id: string; filename: string; size: number }>
    errors: Array<{ filename: string; error: string }>
}

// ── Thumbnail Previews ────────────────────────────────────────────────────────

export interface ThumbnailPreviews {
    previews: string[]
}

// ── Validator ─────────────────────────────────────────────────────────────────

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

export interface ValidatorStats {
    total: number
    validated: number
    needs_fix: number
    fixed: number
    failed: number
    unsupported: number
}

// ── Categorizer ───────────────────────────────────────────────────────────────

export interface CategorizedItem {
    id: string
    name: string
    category: string
    confidence: number
    detected_info?: Record<string, unknown>
    categorized_at: string
    manual_override: boolean
}

export interface CategoryStats {
    total_items: number
    by_category: Record<string, number>
    manual_overrides: number
}

export interface CategoryBrowseItem {
    id: string
    name: string
    category: string
    confidence: number
    duration?: number
    detected_info?: {
        title?: string
        year?: number
        season?: number
        episode?: number
        show_name?: string
        artist?: string
        album?: string
    }
    thumbnail_url?: string
}

export interface CategoryBrowseResponse {
    category: string
    items: CategoryBrowseItem[]
    total: number
}

// Generated from api_spec/openapi.yaml — do not edit manually
export type RatedItem = components['schemas']['RatedItem']
export type RecentItem = components['schemas']['RecentItem']
export type OnDeckItem = components['schemas']['OnDeckItem']

export interface NewSinceResponse {
    items: RecentItem[]
    since: string
    total: number
}

export interface OnDeckResponse {
    items: OnDeckItem[]
    total: number
}

// ── Classify (HuggingFace) ────────────────────────────────────────────────────

export interface ClassifyStatus {
    configured: boolean
    enabled: boolean
    model: string
    rate_limit: number
    max_frames: number
    max_concurrent: number
    task_running?: boolean
    task_last_run?: string
    task_next_run?: string
    task_last_error?: string
    task_enabled?: boolean
}

export interface ClassifyStats {
    total_media: number
    mature_total: number
    mature_classified: number
    mature_pending: number
    recent_items: Array<{ id: string; name: string; tags: string[]; mature_score: number; date_modified: string }>
}

// ── Remote Sources ────────────────────────────────────────────────────────────

export interface RemoteSourceResponse {
    name: string
    url: string
    username?: string
    enabled: boolean
}

export interface RemoteSourceState {
    source: RemoteSourceResponse
    status: string
    last_sync: string
    media_count: number
    error?: string
}

export interface RemoteStats {
    source_count: number
    cached_item_count: number
    total_media_count: number
    cache_size: number
    sources: Array<{ name: string; status: string; media_count: number; last_sync: string; error?: string }>
}

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

// ── Auto-Discovery ────────────────────────────────────────────────────────────

export interface DiscoverySuggestion {
    original_path: string
    suggested_name: string
    type: string
    confidence: number
    metadata?: Record<string, string>
}

// ── Receiver (master/slave) ───────────────────────────────────────────────────

export interface SlaveNode {
    id: string
    name: string
    base_url: string
    status: string
    media_count: number
    last_seen: string
    registered_at: string
}

export interface ReceiverStats {
    slave_count: number
    online_slaves: number
    media_count: number
    duplicate_count: number
}

// ReceiverAdminSettings exposes the configured API keys to admins so they
// can copy a key into another VPS's follower form during pairing setup.
export interface ReceiverAdminSettings {
    enabled: boolean
    api_keys: string[]
}

export interface ReceiverDuplicate {
    id: string
    fingerprint: string
    item_a: { id: string; slave_id?: string; name: string; source: string } | null
    item_b: { id: string; slave_id?: string; name: string; source: string } | null
    item_a_name: string
    item_b_name: string
    status: string
    resolved_by?: string
    resolved_at?: string
    detected_at: string
}

// FollowerSettings is the redacted view of the follower (this-server-as-slave)
// pairing config returned by GET /api/admin/follower/settings. The api_key is
// never returned — api_key_configured signals whether one is set.
export interface FollowerSettings {
    enabled: boolean
    master_url: string
    api_key_configured: boolean
    slave_id: string
    slave_name: string
    scan_interval_seconds: number
    heartbeat_interval_seconds: number
    max_streams: number
    reconnect_base_seconds: number
    reconnect_max_seconds: number
}

// FollowerSettingsUpdate is the body for POST /api/admin/follower/settings.
// Leave api_key empty to keep the existing key (lets admins edit other fields
// without re-entering the secret).
export interface FollowerSettingsUpdate {
    enabled: boolean
    master_url: string
    api_key?: string
    slave_id?: string
    slave_name?: string
    scan_interval_seconds?: number
    heartbeat_interval_seconds?: number
    max_streams?: number
    reconnect_base_seconds?: number
    reconnect_max_seconds?: number
}

export interface FollowerStatus {
    enabled: boolean
    configured: boolean
    connected: boolean
    master_url?: string
    slave_id?: string
    slave_name?: string
    last_connected_at?: string
    last_catalog_push?: string
    last_catalog_size?: number
    last_error?: string
    last_error_at?: string
}

export interface FollowerSaveResult {
    saved: boolean
    reload_error?: string
    reload_status?: FollowerStatus
}

export interface FollowerTestResult {
    ok: boolean
    error?: string
    http_status?: number
}

// ── Extractor ─────────────────────────────────────────────────────────────────

export interface ExtractorStats {
    total_items: number
    active_items: number
    error_items: number
}

// ── Crawler ───────────────────────────────────────────────────────────────────

export interface CrawlerStats {
    total_targets: number
    enabled_targets: number
    total_discoveries: number
    pending_discoveries: number
    crawling: boolean
}

// ── Downloader ────────────────────────────────────────────────────────────────

export interface DownloaderHealth {
    online: boolean
    activeDownloads?: number
    queuedDownloads?: number
    uptime?: number
    dependencies?: Record<string, unknown>
    error?: string
}

export interface DownloaderStreamInfo {
    url: string
    quality: string
    type: string
    resolution: string
    size?: number
    isAd?: boolean
}

export interface DownloaderDetectResult {
    url: string
    title: string
    isYouTube: boolean
    isYouTubeMusic: boolean
    streams: DownloaderStreamInfo[]
    relayId?: string
}

export interface DownloaderProgress {
    downloadId: string
    status: 'queued' | 'detecting' | 'downloading' | 'processing' | 'completed' | 'complete' | 'error' | 'cancelled'
    progress?: number
    speed?: string
    eta?: string
    title?: string
    filename?: string
    error?: string
}

export interface DownloaderSettings {
    allowServerStorage: boolean
    audioFormat?: string
    supportedSites?: string[]
    theme?: string
    browserRelayConfigured?: boolean
    downloadsDir?: string
}

export interface ImportableFile {
    name: string
    size: number
    modified: number
    isAudio: boolean
}

export interface ImportResult {
    source: string
    destination: string
    scanTriggered: boolean
    sourceDeleted?: boolean
}

// ── Suggestion Stats ──────────────────────────────────────────────────────────

export interface SuggestionStats {
    total_profiles: number
    total_media: number
    total_views: number
    total_watch_time: number
}

// ── Admin Playlists ───────────────────────────────────────────────────────────

export interface AdminPlaylistListResponse {
    items: Playlist[]
    total_items: number
    total_pages: number
}

export interface AdminPlaylistStats {
    total_playlists: number
    public_playlists: number
    total_items: number
}

// ── HLS Validation ────────────────────────────────────────────────────────────

export interface HLSValidationResult {
    job_id: string
    valid: boolean
    variant_count: number
    segment_count: number
    errors?: string[]
}

// ── Banned IPs ────────────────────────────────────────────────────────────────

export interface BannedIP {
    ip: string
    banned_at: string
    expires_at?: string
    reason: string
}

export interface EventStats {
    total_events: number
    event_counts: Record<string, number>
    hourly_events: number[]
}

export interface EventTypeCounts {
    [eventType: string]: number
}

// ── Query Result ──────────────────────────────────────────────────────────────

export interface QueryResult {
    columns?: string[]
    rows?: unknown[][]
    rows_affected?: number
    message?: string
    error?: string
    truncated?: boolean
}

// ── User Sessions ─────────────────────────────────────────────────────────────

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

// ── Favorites ─────────────────────────────────────────────────────────────────

export interface FavoriteItem {
    id: string
    media_id: string
    added_at: string
}

// ── API Tokens ────────────────────────────────────────────────────────────────

export interface APIToken {
    id: string
    name: string
    last_used_at: string | null
    expires_at: string | null
    created_at: string
}

export interface APITokenCreated extends APIToken {
    token: string // raw value — only available on creation response
}

// ── Data Deletion Requests ────────────────────────────────────────────────────

export type DataDeletionRequestStatus = 'pending' | 'approved' | 'denied'

export interface DataDeletionRequest {
    id: string
    user_id: string
    username: string
    email?: string
    reason?: string
    status: DataDeletionRequestStatus
    created_at: string
    reviewed_at?: string | null
    reviewed_by?: string
    admin_notes?: string
}
