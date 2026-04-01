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
  subtitle_lang?: string
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
  total?: number
  page?: number
  limit?: number
  scanning?: boolean
  initializing?: boolean
  /** Map of media_id → user's rating (1–5). Only present for authenticated users who have rated items. */
  user_ratings?: Record<string, number>
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
  status: 'pending' | 'running' | 'completed' | 'failed' | 'cancelled'
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
  top_viewed: TopMediaItem[]
  recent_activity: { type: string; media_id: string; filename: string; timestamp: number }[]
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
  checked_at?: string
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
  name: string
  path: string
  type: string
  size: number
  duration?: number
  fingerprint?: string
  created_at: string
}

export interface CrawlerTarget {
  id: string
  url: string
  name?: string
  status: string
  last_crawl?: string
  discoveries: number
  enabled: boolean
}

export interface CrawlerDiscovery {
  id: string
  target_id: string
  url: string
  title?: string
  status: string
  created_at: string
}

export interface ExtractorItem {
  id: string
  url: string
  title?: string
  status: string
  error?: string
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

export interface Suggestion {
  media_id: string
  title: string
  category: string
  media_type: string
  score: number
  reasons: string[] | null
  thumbnail_url?: string
}

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
  show_mature?: boolean
  mature_preference_set?: boolean
  capabilities: {
    canStream: boolean
    canUpload: boolean
    canDownload: boolean
    canCreatePlaylists: boolean
    canViewMature: boolean
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

// ── Media Stats ───────────────────────────────────────────────────────────────

export interface MediaStats {
  total_count: number
  video_count: number
  audio_count: number
  total_size: number
  last_scan: string
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

export interface RatedItem {
  media_id: string
  name: string
  category: string
  media_type: string
  rating: number
  thumbnail_url?: string
}

export interface RecentItem {
  id: string
  name: string
  type: string
  category: string
  date_added: string
  thumbnail_url?: string
}

export interface NewSinceResponse {
  items: RecentItem[]
  since: string
  total: number
}

export interface OnDeckItem {
  media_id: string
  name: string
  show_name: string
  season: number
  episode: number
  category: string
  thumbnail_url?: string
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
  status: 'queued' | 'downloading' | 'processing' | 'complete' | 'error' | 'cancelled'
  progress?: number
  speed?: string
  eta?: string
  title?: string
  filename?: string
  error?: string
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
  media_path: string
  added_at: string
}

// ── API Tokens ────────────────────────────────────────────────────────────────

export interface APIToken {
  id: string
  name: string
  last_used_at: string | null
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
