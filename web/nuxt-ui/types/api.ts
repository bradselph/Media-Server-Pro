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
  show_mature_content: boolean
  collect_analytics: boolean
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
  token?: string
  session_id?: string
  user: User
}

export interface SessionCheckResponse {
  authenticated: boolean
  user?: User
}

// ── Media ─────────────────────────────────────────────────────────────────────

export interface MediaItem {
  id: string
  name: string
  path: string
  type: 'video' | 'audio' | 'image' | string
  size: number
  duration?: number
  thumbnail_url?: string
  date_added: string
  date_modified?: string
  category?: string
  tags?: string[]
  is_mature?: boolean
  views?: number
  resolution?: string
  container?: string
  bitrate?: number
  source?: 'local' | 'remote' | 'slave'
}

export interface MediaListParams {
  page?: number
  limit?: number
  search?: string
  type?: string
  category?: string
  sort_by?: string
  sort_order?: string
  mature?: boolean
}

export interface MediaListResponse {
  items: MediaItem[]
  total: number
  page: number
  limit: number
}

export interface MediaCategory {
  name: string
  count: number
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
  total_views: number
  unique_viewers: number
  total_watch_time: number
  avg_watch_time: number
  top_media: TopMediaItem[]
}

export interface TopMediaItem {
  media_id: string
  title: string
  views: number
  unique_viewers: number
  avg_completion: number
}

export interface DailyStats {
  date: string
  views: number
  unique_viewers: number
  bandwidth: number
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
  release_notes?: string
  download_url?: string
  published_at?: string
}

export interface UpdateStatus {
  state: 'idle' | 'downloading' | 'applying' | 'error' | 'success'
  progress?: number
  message?: string
  error?: string
}

export interface IPListEntry {
  ip: string
  comment?: string
  added_at: string
}

export interface SecurityStats {
  blocked_requests: number
  rate_limited_requests: number
  banned_ips: number
  whitelist_count: number
  blacklist_count: number
}

export interface DatabaseStatus {
  connected: boolean
  host: string
  database: string
  tables: number
  total_rows: number
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
  id: string
  url: string
  filename?: string
  status: 'pending' | 'downloading' | 'completed' | 'failed' | 'cancelled'
  progress?: number
  speed?: number
  size?: number
  downloaded?: number
  error?: string
  created_at: string
  completed_at?: string
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
  id: string
  name: string
  type: string
  thumbnail_url?: string
  duration?: number
  category?: string
  score?: number
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
