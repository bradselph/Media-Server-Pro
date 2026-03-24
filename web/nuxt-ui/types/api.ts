// ── Auth ──────────────────────────────────────────────────────────────────────

export type UserRole = 'admin' | 'viewer'

export interface UserPermissions {
  can_upload: boolean
  can_download: boolean
  can_delete: boolean
  can_manage_playlists: boolean
  can_view_mature: boolean
  bypass_age_gate: boolean
  max_storage_mb: number
}

export interface UserPreferences {
  theme: string
  playback_speed: number
  volume: number
  auto_play: boolean
  resume_playback: boolean
  items_per_page: number
  view_mode: 'grid' | 'list' | 'compact'
  default_quality: string
  language: string
  equalizer_preset: string
  sort_by: string
  sort_order: string
  filter_category: string
  filter_media_type: string
  show_mature: boolean
  show_analytics: boolean
  show_home_recently_added: boolean
  show_home_continue_watching: boolean
  show_home_suggestions: boolean
}

export interface User {
  id: string
  username: string
  email?: string
  role: UserRole
  enabled: boolean
  created_at: string
  last_login?: string
  storage_used?: number
  watch_history?: WatchHistoryItem[]
  permissions: UserPermissions
  preferences: UserPreferences
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
  user?: User
}

// ── Media ─────────────────────────────────────────────────────────────────────

export interface MediaItem {
  id: string
  name: string
  type: 'video' | 'audio' | 'image' | string
  size: number
  duration?: number
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
  date_modified?: string
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
}

export interface MediaListResponse {
  items: MediaItem[]
  total_items: number
  total_pages: number
  total?: number
  page?: number
  limit?: number
  scanning?: boolean
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
  media_id?: string
  media_name?: string
  status: 'pending' | 'running' | 'completed' | 'failed' | 'cancelled'
  progress: number
  qualities: string[]
  started_at: string
  completed_at?: string
  hls_url?: string
  available: boolean
  error?: string
  fail_count?: number
  last_accessed_at?: string
}

export interface HLSStats {
  total_jobs: number
  running: number
  completed: number
  failed: number
  pending: number
  disk_used: number
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
  unique_clients: number
  top_viewed: TopMediaItem[]
  recent_activity: { type: string; media_id: string; filename: string; timestamp: number }[]
}

export interface TopMediaItem {
  media_id: string
  filename: string
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
  user_id?: string
  media_id: string
  quality?: string
  bytes_sent?: number
  ip_address?: string
  started_at: string
}

export interface UploadProgress {
  id: string
  filename: string
  user_id?: string
  progress?: number
  status: string
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
}

export interface ThumbnailStats {
  total: number
  with_thumbnail: number
  without_thumbnail: number
  webp_count: number
  generating: number
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
  comment?: string
  added_at: string
}

export interface SecurityStats {
  banned_ips: number
  whitelisted_ips: number
  blacklisted_ips: number
  active_rate_limits: number
  total_blocks_today: number
}

export interface DatabaseStatus {
  connected: boolean
  host: string
  database: string
  app_version?: string
  repository_type?: string
  message?: string
  checked_at?: string
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
  media_path?: string
  media_id?: string
  media_name?: string
  title?: string
  watched_at: string
  position?: number
  duration?: number
  progress?: number
}

// ── Suggestions ───────────────────────────────────────────────────────────────

export interface Suggestion {
  media_id: string
  title: string
  category: string
  media_type: string
  score: number
  reasons: string[]
  thumbnail_url?: string
}

// ── Storage / Permissions ─────────────────────────────────────────────────────

export interface StorageUsage {
  used: number
  limit: number
  percent: number
}

export interface PermissionsInfo {
  can_upload: boolean
  can_download: boolean
  can_delete: boolean
  can_manage_playlists: boolean
  can_view_mature: boolean
  bypass_age_gate: boolean
  max_storage_mb: number
}

// ── Server Config ─────────────────────────────────────────────────────────────

export interface ServerFeatures {
  enable_hls: boolean
  enable_analytics: boolean
  enable_playlists: boolean
  enable_upload: boolean
  enable_download: boolean
  enable_mature_filter: boolean
  enable_age_gate: boolean
  enable_downloader: boolean
  enable_remote: boolean
  enable_suggestions: boolean
  enable_autodiscovery: boolean
}

export interface ServerSettings {
  version?: string
  features: ServerFeatures
}
