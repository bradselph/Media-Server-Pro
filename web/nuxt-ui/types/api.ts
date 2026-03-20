/**
 * Shared TypeScript types matching the Go backend's JSON response shapes.
 * Derived from pkg/models/models.go and api/handlers/.
 *
 * Migration plan: stay aligned with web/frontend/src/api/types.ts — when the
 * React client gains or changes a type, update this file the same way (then
 * endpoints/composables as needed). Types are added incrementally per page.
 */

// ── Auth ──

export interface User {
  id: string
  username: string
  /** Backend UserRole values: "admin" | "viewer" (not "user") */
  role: 'admin' | 'viewer'
  /** Backend json:"type" — always present; default "standard" */
  type: string
  email?: string
  enabled: boolean
  permissions: UserPermissions
  preferences: UserPreferences
  /** Backend json:"storage_used" — always present; 0 if unset */
  storage_used: number
  /** Backend json:"active_streams" — always present; 0 if unset */
  active_streams: number
  /** Backend json:"created_at" (gorm autoCreateTime) — always present */
  created_at: string
  last_login?: string
  /** Backend json:"watch_history,omitempty" — only present when non-empty */
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
  theme: string
  default_quality: string
  show_mature: boolean
  mature_preference_set: boolean
  /** Backend field is "auto_play" (canonical); also accepts "autoplay" alias on write */
  auto_play: boolean
  /** Backend serializes as "equalizer_preset"; also accepted as "equalizer_bands" on write */
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
  /** custom_eq_presets has omitempty — only present when non-empty */
  custom_eq_presets?: Record<string, unknown>
  /** Home section visibility (default true = show section) */
  show_continue_watching: boolean
  show_recommended: boolean
  show_trending: boolean
}

/** GET /api/auth/session response */
export interface SessionCheckResponse {
  authenticated: boolean
  allow_guests: boolean
  user?: User
}

/**
 * POST /api/auth/login response.
 * Keep in lockstep with web/frontend/src/api/types.ts — admin and user branches
 * both return username, role, and expires_at (RFC3339).
 */
export interface LoginResponse {
  session_id: string
  is_admin: boolean
  username: string
  role: string
  expires_at: string
}

// ── Watch History ──

/** Backend models.WatchHistoryItem JSON fields */
export interface WatchHistoryEntry {
  media_id: string
  /** Backend json:"media_name,omitempty" — human-readable filename */
  media_name?: string
  position: number
  duration: number
  /** Backend uses "progress" (float ratio 0-1) not "completion" */
  progress: number
  /** Backend uses "watched_at" not "last_watched" */
  watched_at: string
  completed: boolean
}

// ── HLS ──

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
  status: 'pending' | 'running' | 'completed' | 'failed' | 'cancelled'
  progress: number
  qualities: string[]
  started_at: string
  completed_at?: string
  hls_url?: string
  available: boolean
  error?: string
}

// ── Suggestions ──

export interface Suggestion {
  media_id: string
  title: string
  category: string
  media_type: string
  score: number
  reasons: string[] | null
  thumbnail_url?: string
}

// ── Storage & Permissions ──

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
    storage_quota: number
    concurrent_streams: number
  }
}

// ── Media ──

/** Backend models.MediaCategory JSON fields */
export interface MediaCategory {
  name: string
  display_name: string
  count: number
  tags?: string[]
}

/**
 * Backend models.MediaItem JSON fields.
 * Note: backend uses "name" not "title", "date_added"/"date_modified" not "created_at".
 */
export interface MediaItem {
  id: string
  /** Backend uses "name" not "title" */
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

export interface MediaListParams {
  page?: number
  limit?: number
  sort?: string
  /** Backend query param is "sort_order" not "order" */
  sort_order?: string
  type?: string
  category?: string
  search?: string
  tags?: string
  is_mature?: string
}

export interface MediaListResponse {
  items: MediaItem[]
  total_items: number
  total_pages: number
  /** true while the server's initial media scan is still running */
  scanning: boolean
  /** present (and true) only while the first-ever scan is still running */
  initializing?: boolean
}

/** Matches internal/media/discovery.go Stats struct JSON tags */
export interface MediaStats {
  total_count: number
  video_count: number
  audio_count: number
  total_size: number
  last_scan: string
}

// ── Auth (Sessions) ──

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

// ── Admin ──

/**
 * disk_usage/disk_total/disk_free are uint64 bytes.
 * disk_usage = used bytes (not a ratio); use (disk_usage / disk_total * 100) for percentage.
 */
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

/**
 * Matches AdminGetSystemInfo response.
 * memory_total = runtime.MemStats.Sys (Go runtime reservation from OS).
 * memory_used = runtime.MemStats.Alloc (bytes currently in use by Go heap).
 */
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
  /** Backend constants: "healthy" | "unhealthy". "degraded"/"failed"/"disabled" kept for display logic. */
  status: 'healthy' | 'unhealthy' | 'degraded' | 'failed' | 'disabled'
  message?: string
  last_check: string
}

/** Backend models.AuditLogEntry JSON fields */
export interface AuditLogEntry {
  id: string
  timestamp: string
  /** Backend uses "username" not "admin" */
  username: string
  user_id: string
  action: string
  /** Backend uses "resource" not "target" */
  resource: string
  /** Backend json:"details,omitempty" — arbitrary key-value metadata, absent when empty */
  details?: Record<string, unknown>
  /** Backend uses "ip_address" not "ip"; always present (empty string when unavailable) */
  ip_address: string
  /** Backend models.AuditLogEntry.Success — always present */
  success: boolean
}

export interface LogEntry {
  timestamp: string
  level: string
  module: string
  message: string
  /** Backend parseLogLine always includes unparsed "raw" line */
  raw?: string
}

export interface ServerConfig {
  [key: string]: unknown
}

/**
 * tasks.TaskInfo JSON fields.
 * last_run zero value "0001-01-01T00:00:00Z" means never run.
 */
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

/** BackupEntry: subset of backend backup.Manifest */
export interface BackupEntry {
  id: string
  filename: string
  size: number
  created_at: string
  type: string
  /** Backend json:"description,omitempty" — absent when empty */
  description?: string
}

// ── Admin Media ──

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

// ── Scanner / Content Review ──

export interface ScannerStats {
  total_scanned: number
  mature_count: number
  auto_flagged: number
  pending_review: number
}

/** Returned when runScan is called with a specific path */
export interface FileScanResult {
  path: string
  is_mature: boolean
  confidence: number
  reasons: string[]
  auto_flagged: boolean
  needs_review: boolean
  scanned_at: string
  reviewed_by?: string
  reviewed_at?: string
  review_decision?: string
  high_conf_matches?: string[]
  med_conf_matches?: string[]
}

/** Returned when runScan is called without a path (directory scan) */
export interface DirectoryScanResult {
  stats: ScannerStats
  scanned: number
  auto_flagged_count: number
  review_queue_count: number
  clean: number
  message: string
}

/** Matches backend models.MatureReviewItem JSON */
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

// ── Hugging Face Classification ──

/** GET /api/admin/classify/status */
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

/** GET /api/admin/classify/stats */
export interface ClassifyStats {
  total_media: number
  mature_total: number
  mature_classified: number
  mature_pending: number
  recent_items: ClassifiedItem[]
}

export interface ClassifiedItem {
  id: string
  name: string
  tags: string[]
  mature_score: number
  date_modified: string
}

// ── HLS Admin ──

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

export interface HLSStats {
  total_jobs: number
  running_jobs: number
  completed_jobs: number
  failed_jobs: number
  pending_jobs: number
  cache_size_bytes: number
}

/** Matches internal/hls ValidationResult JSON tags */
export interface HLSValidationResult {
  job_id: string
  valid: boolean
  variant_count: number
  segment_count: number
  errors?: string[]
}

// ── Validator ──

/** Matches internal/validator ValidationResult JSON tags */
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

/** Matches internal/validator Stats JSON tags */
export interface ValidatorStats {
  total: number
  validated: number
  needs_fix: number
  fixed: number
  failed: number
  unsupported: number
}

// ── Database Admin ──

/** Matches AdminGetDatabaseStatus response */
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

/**
 * Backend GetAnalyticsSummary response.
 * When analytics is disabled, only analytics_disabled:true is present.
 */
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

/** date is a "YYYY-MM-DD" date string */
export interface DailyStats {
  date: string
  total_views: number
  unique_users: number
  total_watch_time: number
  new_users: number
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

/** Backend analytics.EventStats shape */
export interface EventStats {
  total_events: number
  event_counts: Record<string, number>
  hourly_events: number[]
}

// ── Thumbnails ──

export interface ThumbnailStats {
  total_thumbnails: number
  total_size_mb: number
  pending_generation: number
  generation_errors: number
}

export interface ThumbnailPreviews {
  previews: string[]
}

// ── Suggestions (Admin) ──

/** Matches internal/suggestions SuggestionStats JSON tags */
export interface SuggestionStats {
  total_profiles: number
  total_media: number
  total_views: number
  total_watch_time: number
}

// ── Security ──

export interface SecurityStats {
  banned_ips: number
  whitelisted_ips: number
  blacklisted_ips: number
  active_rate_limits: number
  total_blocks_today: number
}

export interface IPEntry {
  ip: string
  comment: string
  added_by: string
  added_at: string
  expires_at?: string
}

export interface BannedIP {
  ip: string
  /** Timestamp when the ban was created (RFC3339) */
  banned_at: string
  expires_at?: string
  /** Reason for the ban. Manual bans default to "Manual ban". */
  reason: string
}

// ── Categorizer ──

/** Matches internal/categorizer MediaInfo struct (all fields omitempty) */
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

/** Matches internal/categorizer CategoryStats JSON tags */
export interface CategoryStats {
  total_items: number
  by_category: Record<string, number>
  manual_overrides: number
}

// ── Auto-Discovery ──

/** Matches pkg/models AutoDiscoverySuggestion JSON tags */
export interface DiscoverySuggestion {
  original_path: string
  suggested_name: string
  type: string
  confidence: number
  metadata?: Record<string, string>
}

// ── Remote Sources ──

export interface RemoteSourceResponse {
  name: string
  url: string
  username?: string
  enabled: boolean
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

export interface RemoteSourceState {
  source: RemoteSourceResponse
  status: string
  /** Always an ISO timestamp; zero value is "0001-01-01T00:00:00Z" (never synced) */
  last_sync: string
  media_count: number
  error?: string
}

export interface RemoteStats {
  source_count: number
  cached_item_count: number
  total_media_count: number
  cache_size: number
  sources: Array<{
    name: string
    status: string
    media_count: number
    last_sync: string
    error?: string
  }>
}

/** Matches internal/remote CachedMedia struct */
export interface CachedMediaResult {
  remote_url: string
  size: number
  content_type: string
  cached_at: string
  last_access: string
  hits: number
}

// ── Receiver (Master/Slave) ──

/** Matches internal/receiver/receiver.go SlaveNode struct */
export interface SlaveNode {
  id: string
  name: string
  base_url: string
  /** "online" | "offline" | "stale" */
  status: string
  media_count: number
  last_seen: string
  registered_at: string
}

/** Matches internal/receiver/receiver.go MediaItem struct */
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

/** Matches internal/receiver/receiver.go Stats struct */
export interface ReceiverStats {
  slave_count: number
  online_slaves: number
  media_count: number
  duplicate_count: number
}

/** Matches internal/duplicates/duplicates.go DuplicateItem struct */
export interface DuplicateItem {
  id: string
  slave_id?: string
  name: string
  source: 'local' | 'receiver'
}

/** Matches internal/duplicates/duplicates.go DuplicateGroup struct */
export interface ReceiverDuplicate {
  id: string
  fingerprint: string
  /** Pointer fields in Go — serialize as null when the media entry has been deleted */
  item_a: DuplicateItem | null
  item_b: DuplicateItem | null
  item_a_name: string
  item_b_name: string
  /** "pending" | "remove_a" | "remove_b" | "keep_both" | "ignore" */
  status: string
  resolved_by?: string
  resolved_at?: string
  detected_at: string
}

// ── Extractor ──

/** Matches internal/extractor ExtractedItem struct */
export interface ExtractorItem {
  id: string
  title: string
  stream_url: string
  status: 'active' | 'error'
  error_message?: string
  added_by: string
  created_at: string
}

/** Matches internal/extractor Stats struct */
export interface ExtractorStats {
  total_items: number
  active_items: number
  error_items: number
}

// ── Crawler ──

/** Matches internal/crawler CrawlTarget struct */
export interface CrawlTarget {
  id: string
  name: string
  url: string
  site: string
  enabled: boolean
  last_crawled?: string
  created_at: string
}

/** Matches internal/crawler Discovery struct */
export interface CrawlerDiscovery {
  id: string
  target_id: string
  page_url: string
  title: string
  stream_url: string
  stream_type: string
  quality: number
  status: 'pending' | 'added' | 'ignored'
  reviewed_by?: string
  reviewed_at?: string
  discovered_at: string
}

/** Matches internal/crawler Stats struct */
export interface CrawlerStats {
  total_targets: number
  enabled_targets: number
  total_discoveries: number
  pending_discoveries: number
  crawling: boolean
}

// ── Playlists ──

/** Backend models.Playlist JSON fields */
export interface Playlist {
  id: string
  name: string
  description?: string
  /** Backend uses "user_id" not "owner" */
  user_id: string
  /** items may be null when not preloaded — callers must guard: (playlist.items ?? []) */
  items: PlaylistItem[] | null
  created_at: string
  /** Backend uses "modified_at" not "updated_at" */
  modified_at: string
  /** Backend json:"is_public" — always present (true or false) */
  is_public: boolean
  cover_image?: string
}

/** Backend models.PlaylistItem JSON fields */
export interface PlaylistItem {
  id?: string
  playlist_id?: string
  media_id: string
  title: string
  position: number
  added_at: string
}

/** AdminListPlaylists returns { items, total_items, total_pages } */
export interface AdminPlaylistListResponse {
  items: Playlist[]
  total_items: number
  total_pages: number
}

/** Backend playlist.Stats shape */
export interface AdminPlaylistStats {
  total_playlists: number
  public_playlists: number
  total_items: number
}

// ── Upload ──

export interface UploadResult {
  uploaded: Array<{ upload_id: string; filename: string; size: number }>
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

// ── Streams ──

/** Matches pkg/models StreamSession — returned by AdminGetActiveStreams */
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

// ── Server Settings ──

/** Matches api/handlers/system.go GetServerSettings response */
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
}

// ── Age Gate ──

export interface AgeGateStatus {
  enabled: boolean
  verified: boolean
}

// ── Downloader ──

/** Dependency value: backend may send string (version) or legacy { available, version } */
export type DownloaderDependencyValue = string | { available?: boolean; version?: string }

export interface DownloaderHealth {
  online: boolean
  activeDownloads?: number
  queuedDownloads?: number
  uptime?: number
  dependencies?: Record<string, DownloaderDependencyValue>
  error?: string
}

export interface DownloaderStreamInfo {
  url: string
  quality: string
  type: string
  size?: number
  format?: string
  resolution?: string
  bitrate?: number
  label?: string
}

export interface DownloaderDetectResult {
  url: string
  title: string
  isYouTube: boolean
  isYouTubeMusic: boolean
  streams: DownloaderStreamInfo[]
  relayId?: string
}

export interface DownloaderDownloadResult {
  downloadId: string
  status: string
  message?: string
}

export interface DownloaderDownloadFile {
  filename: string
  size: number
  created: number
  url?: string
}

export interface DownloaderSettings {
  maxConcurrent?: number
  downloadsDir?: string
  allowServerStorage: boolean
  audioFormat?: string
  audioQuality?: string
  videoFormat?: string
  proxy?: { enabled: boolean }
  supportedSites?: string[]
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
  /** False if "delete source after import" was requested but the source file could not be removed */
  sourceDeleted?: boolean
}

export interface DownloaderProgress {
  downloadId: string
  status: 'downloading' | 'converting' | 'complete' | 'error' | 'cancelled'
  progress: number
  speed?: string
  eta?: string
  filename?: string
  error?: string
  title?: string
}

// ── Update Info ──

export interface UpdateCheckResult {
  update_available: boolean
  current_version: string
  latest_version: string
  release_url?: string
  release_notes?: string
  published_at?: string
  checked_at: string
  error?: string
}

export interface UpdateStatus {
  update_available: boolean
  current_version: string
  latest_version: string
  release_url?: string
  release_notes?: string
  published_at?: string
  /** null before first check; string after first check */
  checked_at: string | null
  error?: string
}

export interface UpdateApplyResult {
  stage: string
  progress: number
  in_progress: boolean
  started_at: string
  error?: string
  backup_path?: string
}

export interface SourceUpdateCheckResult {
  updates_available: boolean
  remote_commit: string
}

export interface SourceUpdateProgress {
  stage: string
  progress: number
  in_progress: boolean
  /** Absent when idle; guard with !started_at.startsWith('0001') when present */
  started_at?: string
  error?: string
  backup_path?: string
}

export interface UpdateConfig {
  update_method: 'source' | 'binary'
  branch: string
}
