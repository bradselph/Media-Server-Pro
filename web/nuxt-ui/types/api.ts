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

export interface WatchHistoryEntry {
  media_id: string
  title: string
  position: number
  duration: number
  last_watched: string
  completed: boolean
}

// ── Media ──

export interface MediaItem {
  id: string
  title: string
  path: string
  type: 'video' | 'audio'
  size: number
  duration?: number
  thumbnail?: string
  category?: string
  is_mature?: boolean
  created_at: string
  updated_at?: string
}

export interface MediaListParams {
  search?: string
  type?: string
  category?: string
  sort_by?: string
  sort_order?: string
  limit?: number
  offset?: number
}

export interface MediaListResponse {
  items: MediaItem[]
  total: number
}
