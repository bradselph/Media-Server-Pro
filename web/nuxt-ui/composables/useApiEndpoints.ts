/**
 * API endpoint functions organized by domain.
 * Mirrors the React endpoints.ts — each function calls the typed API client
 * and returns strongly-typed data.
 *
 * This file starts with auth endpoints; additional domains will be added
 * as the migration progresses.
 */

import type {
  User,
  UserPermissions,
  UserPreferences,
  LoginResponse,
  SessionCheckResponse,
  MediaItem,
  MediaListParams,
  MediaListResponse,
  MediaCategory,
  HLSAvailability,
  HLSJob,
  WatchHistoryEntry,
  Suggestion,
  StorageUsage,
  PermissionsInfo,
} from '~/types/api'

/**
 * Composable providing typed API endpoint functions.
 * Usage: const { login, logout, register, getSession } = useApiEndpoints()
 */
export function useApiEndpoints() {
  // ── Auth ──

  function login(username: string, password: string) {
    return api.post<LoginResponse>('/api/auth/login', { username, password })
  }

  function logout() {
    return api.post<void>('/api/auth/logout')
  }

  function register(username: string, password: string, email?: string) {
    return api.post<User>('/api/auth/register', { username, password, email })
  }

  function getSession() {
    return api.get<SessionCheckResponse>('/api/auth/session')
  }

  function changePassword(currentPassword: string, newPassword: string) {
    return api.post<void>('/api/auth/change-password', {
      current_password: currentPassword,
      new_password: newPassword,
    })
  }

  function deleteAccount(password: string) {
    return api.post<void>('/api/auth/delete-account', { password })
  }

  // ── Preferences ──

  function getPreferences() {
    return api.get<UserPreferences>('/api/preferences')
  }

  function updatePreferences(prefs: Partial<UserPreferences>) {
    return api.put<UserPreferences>('/api/preferences', prefs)
  }

  // ── Permissions ──

  function getPermissions() {
    return api.get<UserPermissions>('/api/permissions')
  }

  return {
    // Auth
    login,
    logout,
    register,
    getSession,
    changePassword,
    deleteAccount,
    // Preferences
    getPreferences,
    updatePreferences,
    // Permissions
    getPermissions,
  }
}

/**
 * Media API endpoints.
 * Usage: const mediaApi = useMediaApi()
 */
export function useMediaApi() {
  return {
    /**
     * List media with pagination, search, sort, and filter.
     * Converts 1-based page to 0-based offset for the backend.
     */
    list: (params?: MediaListParams): Promise<MediaListResponse> => {
      const searchParams = new URLSearchParams()
      if (params) {
        const { page, limit, sort_order, ...rest } = params
        Object.entries(rest).forEach(([key, value]) => {
          if (value !== undefined && value !== '') {
            searchParams.set(key, String(value))
          }
        })
        if (limit !== undefined) {
          searchParams.set('limit', String(limit))
        }
        // Convert 1-based page to 0-based offset for the backend
        if (page !== undefined && limit !== undefined && page > 1) {
          searchParams.set('offset', String((page - 1) * limit))
        }
        // Backend reads "sort_order", not "order"
        if (sort_order !== undefined && sort_order !== '') {
          searchParams.set('sort_order', sort_order)
        }
      }
      const qs = searchParams.toString()
      return api.get<MediaListResponse>(`/api/media${qs ? `?${qs}` : ''}`)
    },

    /** Get a single media item by ID. */
    getById: (id: string): Promise<MediaItem> =>
      api.get<MediaItem>(`/api/media/${encodeURIComponent(id)}`),

    /** Get available media categories. */
    getCategories: (): Promise<MediaCategory[]> =>
      api.get<MediaCategory[]>('/api/media/categories'),

    /** Build thumbnail URL for a media item (served directly by the Go backend). */
    getThumbnailUrl: (id: string): string =>
      `/thumbnail?id=${encodeURIComponent(id)}`,

    /** Build stream URL for a media item. */
    getStreamUrl: (id: string): string =>
      `/media?id=${encodeURIComponent(id)}`,

    /** Build download URL for a media item. */
    getDownloadUrl: (id: string): string =>
      `/download?id=${encodeURIComponent(id)}`,
  }
}

/**
 * HLS API endpoints.
 * Usage: const hlsApi = useHlsApi()
 */
export function useHlsApi() {
  return {
    /** Check if HLS is available for a media item. */
    check: (id: string): Promise<HLSAvailability> =>
      api.get<HLSAvailability>(`/api/hls/check?id=${encodeURIComponent(id)}`),

    /** Get HLS job status. */
    getStatus: (id: string): Promise<HLSJob> =>
      api.get<HLSJob>(`/api/hls/status/${encodeURIComponent(id)}`),

    /** Trigger HLS generation for a media item. */
    generate: (id: string, quality?: string): Promise<HLSJob> =>
      api.post<HLSJob>('/api/hls/generate', { id, quality }),

    /** Build the master playlist URL for a media item. */
    getMasterPlaylistUrl: (id: string): string =>
      `/hls/${encodeURIComponent(id)}/master.m3u8`,
  }
}

/**
 * Playback position & watch history API endpoints.
 * Usage: const playbackApi = usePlaybackApi()
 */
export function usePlaybackApi() {
  return {
    /** Get saved playback position for a media item. */
    getPosition: (id: string): Promise<{ position: number }> =>
      api.get<{ position: number }>(`/api/playback?id=${encodeURIComponent(id)}`),

    /** Save playback position for a media item. */
    savePosition: (id: string, position: number, duration: number): Promise<void> =>
      api.post<void>('/api/playback', { id, position, duration }),
  }
}

/**
 * Watch history API endpoints.
 * Usage: const watchHistoryApi = useWatchHistoryApi()
 */
export function useWatchHistoryApi() {
  return {
    /** List all watch history entries. */
    list: (): Promise<WatchHistoryEntry[]> =>
      api.get<WatchHistoryEntry[]>('/api/watch-history'),

    /** Delete a specific watch history entry. */
    remove: (id: string): Promise<void> => {
      if (!id) throw new Error('watchHistoryApi.remove: id must not be empty')
      return api.delete<void>(`/api/watch-history?id=${encodeURIComponent(id)}`)
    },

    /** Clear all watch history. */
    clear: (): Promise<void> =>
      api.delete<void>('/api/watch-history'),
  }
}

/**
 * Suggestions API endpoints.
 * Usage: const suggestionsApi = useSuggestionsApi()
 */
export function useSuggestionsApi() {
  return {
    /** Get similar media suggestions for a given media ID. */
    getSimilar: (id: string): Promise<Suggestion[]> =>
      api.get<Suggestion[]>(`/api/suggestions/similar?id=${encodeURIComponent(id)}`),
  }
}

/**
 * Storage & permissions API endpoints.
 * Usage: const storageApi = useStorageApi()
 */
export function useStorageApi() {
  return {
    /** Get current user's storage usage. */
    getUsage: (): Promise<StorageUsage> =>
      api.get<StorageUsage>('/api/storage-usage'),

    /** Get current user's permissions info. */
    getPermissions: (): Promise<PermissionsInfo> =>
      api.get<PermissionsInfo>('/api/permissions'),
  }
}
