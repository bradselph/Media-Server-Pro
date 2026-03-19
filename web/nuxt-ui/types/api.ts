/**
 * Shared TypeScript types matching the Go backend's JSON response shapes.
 * Derived from pkg/models/models.go and api/handlers/.
 *
 * This file mirrors web/frontend/src/api/types.ts — types are added
 * incrementally as pages are migrated.
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

/** POST /api/auth/login response */
export interface LoginResponse {
  session_id: string
  is_admin: boolean
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
