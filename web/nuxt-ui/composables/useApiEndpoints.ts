import type {
  User, UserPreferences,
  LoginResponse, SessionCheckResponse,
  MediaItem, MediaListParams, MediaListResponse, MediaCategory, MediaStats,
  AdminMediaListResponse, AdminMediaListParams,
  HLSAvailability, HLSCapabilities, HLSJob, HLSStats, HLSValidationResult,
  Playlist, PlaylistItem, AdminPlaylistListResponse, AdminPlaylistStats,
  AnalyticsSummary, AnalyticsEvent, DailyStats, TopMediaItem, EventStats, EventTypeCounts,
  AdminStats, SystemInfo, StreamSession, UploadProgress, UploadResult,
  AuditLogEntry, LogEntry, ScheduledTask, BackupEntry,
  ThumbnailStats, ThumbnailPreviews, ScannerStats, FileScanResult,
  UpdateInfo, UpdateStatus,
  IPListEntry, BannedIP, SecurityStats,
  DatabaseStatus, QueryResult, UserSession,
  ReceiverSlave, ReceiverMedia, ReceiverStats, ReceiverDuplicate, SlaveNode,
  CrawlerTarget, CrawlerDiscovery, CrawlerStats, ExtractorItem, ExtractorStats,
  DownloaderJob, DownloaderHealth, DownloaderDetectResult, DownloaderSettings, ImportableFile, ImportResult,
  WatchHistoryItem, Suggestion, SuggestionStats, StorageUsage, PermissionsInfo, UserProfile,
  ServerSettings, AgeGateStatus,
  ClassifyStatus, ClassifyStats,
  ValidationResult, ValidatorStats,
  CategorizedItem, CategoryStats,
  RemoteSourceState, RemoteSourceResponse, RemoteStats, RemoteMediaItem,
  DiscoverySuggestion,
  ModuleHealth, ServerStatus,
  FavoriteItem, APIToken, APITokenCreated,
  RatedItem, RecentItem, NewSinceResponse, CategoryBrowseResponse, OnDeckResponse,
} from '~/types/api'
import { normalizeLogin, normalizePreferences, normalizeSession, toPreferencesPatch } from '~/utils/apiCompat'
// Explicit import — bypasses Nuxt's #imports virtual module so this file does
// NOT participate in the #imports circular dependency graph.
// useApiEndpoints.ts is in composables/ and is re-exported by #imports.  Any
// auto-import resolved through #imports creates a cycle that Rollup cannot
// untangle at module-evaluation time (→ TDZ in the minified production bundle).
// Importing useApi directly from its source file breaks that cycle entirely.
import { useApi } from '~/composables/useApi'

const api = useApi()

// ── Auth ──────────────────────────────────────────────────────────────────────

export function useApiEndpoints() {
  async function login(username: string, password: string): Promise<LoginResponse> {
    const raw = await api.post<unknown>('/api/auth/login', { username, password })
    return normalizeLogin(raw)
  }
  function logout() { return api.post<void>('/api/auth/logout') }
  function register(username: string, password: string, email?: string) {
    return api.post<User>('/api/auth/register', { username, password, email })
  }
  async function getSession(): Promise<SessionCheckResponse> {
    const raw = await api.get<unknown>('/api/auth/session')
    return normalizeSession(raw)
  }
  function changePassword(currentPassword: string, newPassword: string) {
    return api.post<void>('/api/auth/change-password', { current_password: currentPassword, new_password: newPassword })
  }
  function deleteAccount(password: string) {
    return api.post<void>('/api/auth/delete-account', { password })
  }
  async function getPreferences(): Promise<UserPreferences> {
    const raw = await api.get<unknown>('/api/preferences')
    return normalizePreferences(raw)
  }
  async function updatePreferences(prefs: Partial<UserPreferences>): Promise<UserPreferences> {
    // Backend only registers POST /api/preferences (partial merge), not PUT.
    const raw = await api.post<unknown>('/api/preferences', toPreferencesPatch(prefs))
    return normalizePreferences(raw)
  }
  function getPermissions() { return api.get<PermissionsInfo>('/api/permissions') }

  return {
    login, logout, register, getSession, changePassword, deleteAccount,
    getPreferences, updatePreferences, getPermissions,
  }
}

// ── Media ─────────────────────────────────────────────────────────────────────

export function useMediaApi() {
  return {
    list(params?: MediaListParams): Promise<MediaListResponse> {
      const qs = new URLSearchParams()
      if (params) {
        // Backend reads query param "sort" (see handlers.ListMedia), not sort_by.
        const { page, limit, sort_order, sort_by, sort, tags, hide_watched, ...rest } = params
        Object.entries(rest).forEach(([k, v]) => {
          if (v !== undefined && v !== '') qs.set(k, String(v))
        })
        // tags is an array — serialise as comma-joined string (backend splits on comma)
        if (tags && tags.length > 0) qs.set('tags', tags.join(','))
        // hide_watched is a boolean — only send when true to avoid adding a false param
        if (hide_watched) qs.set('hide_watched', 'true')
        const sortKey = sort ?? sort_by
        if (sortKey !== undefined && sortKey !== '') qs.set('sort', String(sortKey))
        if (limit !== undefined) qs.set('limit', String(limit))
        if (page !== undefined && limit !== undefined && page > 1) {
          qs.set('offset', String((page - 1) * limit))
        }
        if (sort_order) qs.set('sort_order', sort_order)
      }
      const q = qs.toString()
      return api.get<MediaListResponse>(`/api/media${q ? `?${q}` : ''}`)
    },
    getById: (id: string) => api.get<MediaItem>(`/api/media/${encodeURIComponent(id)}`),
    getStats: () => api.get<MediaStats>('/api/media/stats'),
    getCategories: () => api.get<MediaCategory[]>('/api/media/categories'),
    getThumbnailUrl: (id: string) => `/thumbnail?id=${encodeURIComponent(id)}`,
    getThumbnailPreviews: (id: string) => api.get<ThumbnailPreviews>(`/api/thumbnails/previews?id=${encodeURIComponent(id)}`),
    getThumbnailBatch: (ids: string[], width?: number) => {
      const qs = new URLSearchParams({ ids: ids.join(',') })
      if (width) qs.set('w', String(width))
      return api.get<{ thumbnails: Record<string, string> }>(`/api/thumbnails/batch?${qs}`)
    },
    getStreamUrl: (id: string) => `/media?id=${encodeURIComponent(id)}`,
    getDownloadUrl: (id: string) => `/download?id=${encodeURIComponent(id)}`,
    getRemoteStreamUrl: (url: string, source?: string) =>
      `/remote/stream?url=${encodeURIComponent(url)}${source ? `&source=${encodeURIComponent(source)}` : ''}`,
  }
}

// ── HLS ───────────────────────────────────────────────────────────────────────

export function useHlsApi() {
  return {
    getCapabilities: () => api.get<HLSCapabilities>('/api/hls/capabilities'),
    check: (id: string) => api.get<HLSAvailability>(`/api/hls/check?id=${encodeURIComponent(id)}`),
    getStatus: (id: string) => api.get<HLSJob>(`/api/hls/status/${encodeURIComponent(id)}`),
    generate: (id: string, quality?: string) => api.post<HLSJob>('/api/hls/generate', { id, quality }),
    getMasterPlaylistUrl: (id: string) => `/hls/${encodeURIComponent(id)}/master.m3u8`,
  }
}

// ── Playback ──────────────────────────────────────────────────────────────────

export function usePlaybackApi() {
  return {
    getPosition: (id: string) => api.get<{ position: number }>(`/api/playback?id=${encodeURIComponent(id)}`),
    savePosition: (id: string, position: number, duration: number) =>
      api.post<void>('/api/playback', { id, position, duration }),
    getBatchPositions: (ids: string[]) =>
      api.get<{ positions: Record<string, number> }>(`/api/playback/batch?ids=${ids.map(encodeURIComponent).join(',')}`),
  }
}

// ── Watch History ─────────────────────────────────────────────────────────────

export function useWatchHistoryApi() {
  return {
    list: (limit?: number, completed?: boolean) => {
      const parts: string[] = []
      if (limit) parts.push(`limit=${limit}`)
      if (completed !== undefined) parts.push(`completed=${completed}`)
      return api.get<WatchHistoryItem[]>(`/api/watch-history${parts.length ? `?${parts.join('&')}` : ''}`)
    },
    remove: (id: string) => api.delete<void>(`/api/watch-history?id=${encodeURIComponent(id)}`),
    clear: () => api.delete<void>('/api/watch-history'),
  }
}

// ── Suggestions ───────────────────────────────────────────────────────────────

export function useSuggestionsApi() {
  return {
    get: () => api.get<Suggestion[]>('/api/suggestions'),
    getTrending: () => api.get<Suggestion[]>('/api/suggestions/trending'),
    getSimilar: (id: string) => api.get<Suggestion[]>(`/api/suggestions/similar?id=${encodeURIComponent(id)}`),
    getContinueWatching: () => api.get<Suggestion[]>('/api/suggestions/continue'),
    getPersonalized: (limit?: number) =>
      api.get<Suggestion[]>(`/api/suggestions/personalized${limit ? `?limit=${limit}` : ''}`),
    getMyProfile: () => api.get<UserProfile>('/api/suggestions/profile'),
    resetMyProfile: () => api.delete<void>('/api/suggestions/profile'),
    getRecent: (days?: number, limit?: number) => {
      const params: string[] = []
      if (days) params.push(`days=${days}`)
      if (limit) params.push(`limit=${limit}`)
      return api.get<RecentItem[]>(`/api/suggestions/recent${params.length ? `?${params.join('&')}` : ''}`)
    },
    getNewSinceLastVisit: (limit?: number) => {
      const qs = limit ? `?limit=${limit}` : ''
      return api.get<NewSinceResponse>(`/api/suggestions/new${qs}`)
    },
    getOnDeck: (limit?: number) => {
      const qs = limit ? `?limit=${limit}` : ''
      return api.get<OnDeckResponse>(`/api/suggestions/on-deck${qs}`)
    },
  }
}

// ── Storage & Permissions ─────────────────────────────────────────────────────

export function useStorageApi() {
  return {
    getUsage: () => api.get<StorageUsage>('/api/storage-usage'),
    getPermissions: () => api.get<PermissionsInfo>('/api/permissions'),
  }
}

// ── Playlists ─────────────────────────────────────────────────────────────────

export function usePlaylistApi() {
  return {
    list: () => api.get<Playlist[]>('/api/playlists'),
    listPublic: () => api.get<Playlist[]>('/api/playlists/public'),
    get: (id: string) => api.get<Playlist>(`/api/playlists/${encodeURIComponent(id)}`),
    create: (data: { name: string; description?: string; is_public?: boolean }) =>
      api.post<Playlist>('/api/playlists', data),
    update: (id: string, data: Partial<Playlist>) =>
      api.put<Playlist>(`/api/playlists/${encodeURIComponent(id)}`, data),
    delete: (id: string) => api.delete<void>(`/api/playlists/${encodeURIComponent(id)}`),
    addItem: (id: string, mediaId: string) =>
      api.post<PlaylistItem>(`/api/playlists/${encodeURIComponent(id)}/items`, { media_id: mediaId }),
    // DELETE is /playlists/:id/items?media_id= or ?item_id= (no path segment).
    removeItem: (playlistId: string, mediaId: string) =>
      api.delete<void>(`/api/playlists/${encodeURIComponent(playlistId)}/items?media_id=${encodeURIComponent(mediaId)}`),
    removePlaylistItemById: (playlistId: string, itemId: string) =>
      api.delete<void>(`/api/playlists/${encodeURIComponent(playlistId)}/items?item_id=${encodeURIComponent(itemId)}`),
    reorder: (id: string, positions: number[]) =>
      api.put<void>(`/api/playlists/${encodeURIComponent(id)}/reorder`, { positions }),
    clear: (id: string) => api.delete<void>(`/api/playlists/${encodeURIComponent(id)}/clear`),
    copy: (id: string, name: string) =>
      api.post<Playlist>(`/api/playlists/${encodeURIComponent(id)}/copy`, { name }),
    exportPlaylist: (id: string, format: 'json' | 'm3u' | 'm3u8') =>
      `/api/playlists/${encodeURIComponent(id)}/export?format=${format}`,
  }
}

// ── Settings ──────────────────────────────────────────────────────────────────

export function useSettingsApi() {
  return {
    get: () => api.get<ServerSettings>('/api/server-settings'),
  }
}

// ── Version ───────────────────────────────────────────────────────────────────

export function useVersionApi() {
  return {
    get: () => api.get<{ version: string }>('/api/version'),
  }
}

// ── Age Gate ──────────────────────────────────────────────────────────────────

export function useAgeGateApi() {
  return {
    getStatus: () => api.get<AgeGateStatus>('/api/age-gate/status'),
    verify: () => api.post<void>('/api/age-verify'),
  }
}

// ── Ratings ───────────────────────────────────────────────────────────────────

export function useRatingsApi() {
  return {
    record: (id: string, rating: number) => api.post<void>('/api/ratings', { id, rating }),
    getMyRatings: () => api.get<RatedItem[]>('/api/ratings'),
  }
}

export function useCategoryBrowseApi() {
  return {
    getStats: () => api.get<CategoryStats>('/api/browse/categories'),
    getByCategory: (category: string, limit?: number) =>
      api.get<CategoryBrowseResponse>(`/api/browse/categories?category=${encodeURIComponent(category)}${limit ? `&limit=${limit}` : ''}`),
  }
}

// ── Upload ────────────────────────────────────────────────────────────────────

export function useUploadApi() {
  return {
    upload: async (files: File[], category?: string): Promise<UploadResult> => {
      const formData = new FormData()
      files.forEach(f => formData.append('files', f))
      if (category) formData.append('category', category)
      const res = await fetch('/api/upload', { method: 'POST', credentials: 'include', body: formData })
      const envelope = await res.json()
      if (!res.ok || envelope.success === false) throw new Error(envelope.message ?? envelope.error ?? `HTTP ${res.status}`)
      return (envelope.data ?? envelope) as UploadResult
    },
    getProgress: (id: string) => api.get<UploadProgress>(`/api/upload/${encodeURIComponent(id)}/progress`),
  }
}

// ── Admin: Dashboard ─────────────────────────────────────────────────────────

export function useAdminApi() {
  const base = '/api/admin'
  return {
    // Dashboard
    getStats: () => api.get<AdminStats>(`${base}/stats`),
    getSystemInfo: () => api.get<SystemInfo>(`${base}/system`),
    getActiveStreams: () => api.get<StreamSession[]>(`${base}/streams`),
    getActiveUploads: () => api.get<UploadProgress[]>(`${base}/uploads/active`),

    // Controls
    clearCache: () => api.post<void>(`${base}/cache/clear`),
    restartServer: () => api.post<void>(`${base}/server/restart`),
    shutdownServer: () => api.post<void>(`${base}/server/shutdown`),

    // Users
    listUsers: () => api.get<User[]>(`${base}/users`),
    getUser: (username: string) => api.get<User>(`${base}/users/${encodeURIComponent(username)}`),
    createUser: (data: { username: string; password: string; email?: string; role: string }) =>
      api.post<User>(`${base}/users`, data),
    updateUser: (username: string, data: Partial<User>) =>
      api.put<User>(`${base}/users/${encodeURIComponent(username)}`, data),
    deleteUser: (username: string) => api.delete<void>(`${base}/users/${encodeURIComponent(username)}`),
    bulkUsers: (usernames: string[], action: 'delete' | 'enable' | 'disable') =>
      api.post<{ success: number; failed: number; errors: string[] }>(`${base}/users/bulk`, { usernames, action }),
    changeUserPassword: (username: string, password: string) =>
      api.post<void>(`${base}/users/${encodeURIComponent(username)}/password`, { new_password: password }),
    getUserSessions: (username: string) =>
      api.get<UserSession[]>(`${base}/users/${encodeURIComponent(username)}/sessions`),
    changeOwnPassword: (currentPassword: string, newPassword: string) =>
      api.post<void>(`${base}/change-password`, { current_password: currentPassword, new_password: newPassword }),

    // Media
    listMedia: (params?: AdminMediaListParams) => {
      const qs = new URLSearchParams()
      if (params) {
        Object.entries(params).forEach(([k, v]) => {
          if (v !== undefined && v !== '') qs.set(k, String(v))
        })
      }
      const q = qs.toString()
      return api.get<AdminMediaListResponse>(`${base}/media${q ? `?${q}` : ''}`)
    },
    scanMedia: () => api.post<void>(`${base}/media/scan`),
    updateMedia: (id: string, data: Partial<MediaItem>) =>
      api.put<MediaItem>(`${base}/media/${encodeURIComponent(id)}`, data),
    deleteMedia: (id: string) => api.delete<void>(`${base}/media/${encodeURIComponent(id)}`),
    bulkMedia: (ids: string[], action: 'delete' | 'update', data?: { category?: string; is_mature?: boolean }) =>
      api.post<{ success: number; failed: number; errors: string[] }>(`${base}/media/bulk`, { ids, action, data }),
    generateThumbnail: (id: string, isAudio?: boolean) =>
      api.post<void>(`${base}/thumbnails/generate`, { id, is_audio: isAudio ?? false }),
    getThumbnailStats: () => api.get<ThumbnailStats>(`${base}/thumbnails/stats`),

    // HLS
    getHLSStats: () => api.get<HLSStats>(`${base}/hls/stats`),
    listHLSJobs: () => api.get<HLSJob[]>(`${base}/hls/jobs`),
    deleteHLSJob: (id: string) => api.delete<void>(`${base}/hls/jobs/${encodeURIComponent(id)}`),
    validateHLS: (id: string) => api.get<HLSValidationResult>(`${base}/hls/validate/${encodeURIComponent(id)}`),
    cleanHLSStaleLocks: () => api.post<void>(`${base}/hls/clean/locks`),
    cleanHLSInactive: () => api.post<void>(`${base}/hls/clean/inactive`),

    // Validator
    validateMedia: (id: string) => api.post<ValidationResult>(`${base}/validator/validate`, { id }),
    fixMedia: (id: string) => api.post<ValidationResult>(`${base}/validator/fix`, { id }),
    getValidatorStats: () => api.get<ValidatorStats>(`${base}/validator/stats`),

    // Tasks
    listTasks: () => api.get<ScheduledTask[]>(`${base}/tasks`),
    runTask: (id: string) => api.post<void>(`${base}/tasks/${encodeURIComponent(id)}/run`),
    enableTask: (id: string) => api.post<void>(`${base}/tasks/${encodeURIComponent(id)}/enable`),
    disableTask: (id: string) => api.post<void>(`${base}/tasks/${encodeURIComponent(id)}/disable`),
    stopTask: (id: string) => api.post<void>(`${base}/tasks/${encodeURIComponent(id)}/stop`),

    // Audit log
    getAuditLog: (params?: { offset?: number; limit?: number; user_id?: string }) => {
      const qs = new URLSearchParams()
      if (params) Object.entries(params).forEach(([k, v]) => { if (v !== undefined) qs.set(k, String(v)) })
      return api.get<AuditLogEntry[]>(`${base}/audit-log?${qs}`)
    },
    exportAuditLogUrl: () => `${base}/audit-log/export`,

    // Logs
    getLogs: (level?: string, module?: string, limit = 200) => {
      const qs = new URLSearchParams()
      if (level) qs.set('level', level)
      if (module) qs.set('module', module)
      qs.set('limit', String(limit))
      return api.get<LogEntry[]>(`${base}/logs?${qs}`)
    },

    // Config
    getConfig: () => api.get<Record<string, unknown>>(`${base}/config`),
    updateConfig: (data: Record<string, unknown>) => api.put<void>(`${base}/config`, data),

    // Backups
    listBackups: () => api.get<BackupEntry[]>(`${base}/backups/v2`),
    createBackup: (description?: string, backupType?: string) =>
      api.post<BackupEntry>(`${base}/backups/v2`, { description: description ?? '', backup_type: backupType ?? 'full' }),
    restoreBackup: (id: string) => api.post<void>(`${base}/backups/v2/${encodeURIComponent(id)}/restore`),
    deleteBackup: (id: string) => api.delete<void>(`${base}/backups/v2/${encodeURIComponent(id)}`),

    // Scanner / Content review
    getScannerStats: () => api.get<ScannerStats>(`${base}/scanner/stats`),
    runScan: (path?: string) => api.post<void>(`${base}/scanner/scan`, path ? { path } : undefined),
    getReviewQueue: () => api.get<FileScanResult[]>(`${base}/scanner/queue`),
    batchReview: (action: 'approve' | 'reject', ids: string[]) =>
      api.post<{ updated: number; total: number }>(`${base}/scanner/queue`, { action, ids }),
    clearReviewQueue: () => api.delete<void>(`${base}/scanner/queue`),
    approveContent: (id: string) => api.post<void>(`${base}/scanner/approve/${encodeURIComponent(id)}`),
    rejectContent: (id: string) => api.post<void>(`${base}/scanner/reject/${encodeURIComponent(id)}`),

    // Classify (HuggingFace visual classification)
    getClassifyStatus: () => api.get<ClassifyStatus>(`${base}/classify/status`),
    getClassifyStats: () => api.get<ClassifyStats>(`${base}/classify/stats`),
    classifyFile: (path: string) =>
      api.post<{ path: string; tags: string[] }>(`${base}/classify/file`, { path }),
    classifyDirectory: (path: string) =>
      api.post<{ message: string; directory: string }>(`${base}/classify/directory`, { path }),
    classifyRunTask: () => api.post<{ message: string }>(`${base}/classify/run-task`),
    classifyClearTags: (id: string) =>
      api.post<{ message: string; id: string }>(`${base}/classify/clear-tags`, { id }),
    classifyAllPending: () =>
      api.post<{ message: string; count: number }>(`${base}/classify/all-pending`),

    // Security
    getSecurityStats: () => api.get<SecurityStats>(`${base}/security/stats`),
    getWhitelist: () => api.get<IPListEntry[]>(`${base}/security/whitelist`),
    addToWhitelist: (ip: string, comment?: string) =>
      api.post<void>(`${base}/security/whitelist`, { ip, comment }),
    removeFromWhitelist: (ip: string) =>
      api.delete<void>(`${base}/security/whitelist`, { ip }),
    getBlacklist: () => api.get<IPListEntry[]>(`${base}/security/blacklist`),
    addToBlacklist: (ip: string, comment?: string, expiresAt?: string) =>
      api.post<void>(`${base}/security/blacklist`, {
        ip, comment, ...(expiresAt ? { expires_at: new Date(expiresAt).toISOString() } : {}),
      }),
    removeFromBlacklist: (ip: string) =>
      api.delete<void>(`${base}/security/blacklist`, { ip }),
    getBannedIPs: () => api.get<BannedIP[]>(`${base}/security/banned`),
    banIP: (ip: string, durationMinutes?: number) =>
      api.post<void>(`${base}/security/ban`, { ip, ...(durationMinutes ? { duration_minutes: durationMinutes } : {}) }),
    unbanIP: (ip: string) => api.post<void>(`${base}/security/unban`, { ip }),

    // Categorizer
    categorizeFile: (path: string) =>
      api.post<CategorizedItem>(`${base}/categorizer/file`, { path }),
    categorizeDirectory: (dir: string) =>
      api.post<CategorizedItem[]>(`${base}/categorizer/directory`, { directory: dir }),
    getCategoryStats: () => api.get<CategoryStats>(`${base}/categorizer/stats`),
    setMediaCategory: (path: string, category: string) =>
      api.post<{ message: string }>(`${base}/categorizer/set`, { path, category }),
    getByCategory: (category: string) =>
      api.get<CategorizedItem[]>(`${base}/categorizer/by-category?category=${encodeURIComponent(category)}`),
    cleanStaleCategories: () => api.post<{ removed: number }>(`${base}/categorizer/clean`),

    // Database
    getDatabaseStatus: () => api.get<DatabaseStatus>(`${base}/database/status`),
    executeQuery: (query: string) => api.post<QueryResult>(`${base}/database/query`, { query }),

    // Remote sources
    getRemoteSources: () => api.get<RemoteSourceState[]>(`${base}/remote/sources`),
    createRemoteSource: (data: { name: string; url: string; username?: string; password?: string }) =>
      api.post<RemoteSourceResponse>(`${base}/remote/sources`, { ...data, enabled: true }),
    deleteRemoteSource: (name: string) =>
      api.delete<void>(`${base}/remote/sources/${encodeURIComponent(name)}`),
    syncRemoteSource: (name: string) =>
      api.post<{ status: string }>(`${base}/remote/sources/${encodeURIComponent(name)}/sync`),
    getRemoteStats: () => api.get<RemoteStats>(`${base}/remote/stats`),
    getRemoteMedia: () => api.get<RemoteMediaItem[]>(`${base}/remote/media`),
    getRemoteSourceMedia: (source: string) =>
      api.get<RemoteMediaItem[]>(`${base}/remote/sources/${encodeURIComponent(source)}/media`),
    cacheRemoteMedia: (url: string, sourceName: string) =>
      api.post<unknown>(`${base}/remote/cache`, { url, source_name: sourceName }),
    cleanRemoteCache: () => api.post<{ removed: number }>(`${base}/remote/cache/clean`),

    // Auto-discovery
    discoveryScan: (directory: string) =>
      api.post<DiscoverySuggestion[]>(`${base}/discovery/scan`, { directory }),
    getDiscoverySuggestions: () =>
      api.get<DiscoverySuggestion[]>(`${base}/discovery/suggestions`),
    applyDiscoverySuggestion: (originalPath: string) =>
      api.post<void>(`${base}/discovery/apply`, { original_path: originalPath }),
    dismissDiscoverySuggestion: (originalPath: string) =>
      api.delete<void>(`${base}/discovery/${originalPath.split('/').map(encodeURIComponent).join('/')}`),

    // Suggestion stats
    getSuggestionStats: () => api.get<SuggestionStats>(`${base}/suggestions/stats`),

    // Receiver / Slaves
    listSlaves: () => api.get<SlaveNode[]>(`${base}/receiver/slaves`),
    getReceiverStats: () => api.get<ReceiverStats>(`${base}/receiver/stats`),
    removeReceiverSlave: (id: string) =>
      api.delete<void>(`${base}/receiver/slaves/${encodeURIComponent(id)}`),
    getSlaveMedia: () => api.get<ReceiverMedia[]>(`/api/receiver/media`),
    listDuplicates: (status = 'pending') =>
      api.get<ReceiverDuplicate[]>(`${base}/duplicates?status=${encodeURIComponent(status)}`),
    resolveDuplicate: (id: string, action: string) =>
      api.post<{ message: string; action: string }>(`${base}/duplicates/${encodeURIComponent(id)}/resolve`, { action }),

    // Crawler
    listCrawlerTargets: () => api.get<CrawlerTarget[]>(`${base}/crawler/targets`),
    addCrawlerTarget: (url: string, name?: string) =>
      api.post<CrawlerTarget>(`${base}/crawler/targets`, { url, name }),
    deleteCrawlerTarget: (id: string) =>
      api.delete<void>(`${base}/crawler/targets/${encodeURIComponent(id)}`),
    startCrawl: (targetId: string) =>
      api.post<void>(`${base}/crawler/targets/${encodeURIComponent(targetId)}/crawl`),
    getCrawlerDiscoveries: (targetId?: string) => {
      const qs = targetId ? `?target_id=${encodeURIComponent(targetId)}` : ''
      return api.get<CrawlerDiscovery[]>(`${base}/crawler/discoveries${qs}`)
    },
    approveCrawlerDiscovery: (id: string) =>
      api.post<CrawlerDiscovery>(`${base}/crawler/discoveries/${encodeURIComponent(id)}/approve`),
    ignoreCrawlerDiscovery: (id: string) =>
      api.post<void>(`${base}/crawler/discoveries/${encodeURIComponent(id)}/ignore`),
    deleteCrawlerDiscovery: (id: string) =>
      api.delete<void>(`${base}/crawler/discoveries/${encodeURIComponent(id)}`),
    getCrawlerStats: () => api.get<CrawlerStats>(`${base}/crawler/stats`),

    // Extractor
    listExtractorItems: () => api.get<ExtractorItem[]>(`${base}/extractor/items`),
    addExtractorUrl: (url: string) => api.post<ExtractorItem>(`${base}/extractor/items`, { url }),
    deleteExtractorItem: (id: string) =>
      api.delete<void>(`${base}/extractor/items/${encodeURIComponent(id)}`),
    getExtractorStats: () => api.get<ExtractorStats>(`${base}/extractor/stats`),

    // Playlists (admin)
    listAllPlaylists: (params?: { page?: number; limit?: number; search?: string; visibility?: string }) => {
      const qs = params
        ? '?' + new URLSearchParams(Object.entries(params).filter(([, v]) => v !== undefined).map(([k, v]) => [k, String(v)])).toString()
        : ''
      return api.get<AdminPlaylistListResponse>(`${base}/playlists${qs}`)
    },
    getPlaylistStats: () => api.get<AdminPlaylistStats>(`${base}/playlists/stats`),
    bulkDeletePlaylists: (ids: string[]) =>
      api.post<{ success: number; failed: number; errors: string[] }>(`${base}/playlists/bulk`, { ids }),
    deletePlaylist: (id: string) => api.delete<void>(`${base}/playlists/${encodeURIComponent(id)}`),

    // Updates
    checkForUpdates: () => api.get<UpdateInfo>(`${base}/update/check`),
    getUpdateStatus: () => api.get<UpdateStatus>(`${base}/update/status`),
    applyUpdate: () => api.post<UpdateStatus>(`${base}/update/apply`),
    checkSourceUpdates: () =>
      api.get<{ updates_available: boolean; remote_commit: string }>(`${base}/update/source/check`),
    applySourceUpdate: () => api.post<UpdateStatus>(`${base}/update/source/apply`),
    getSourceUpdateProgress: () => api.get<UpdateStatus>(`${base}/update/source/progress`),
    getUpdateConfig: () =>
      api.get<{ update_method: 'source' | 'binary'; branch: string }>(`${base}/update/config`),
    setUpdateConfig: (data: { update_method?: 'source' | 'binary'; branch?: string }) =>
      api.put<{ update_method: 'source' | 'binary'; branch: string }>(`${base}/update/config`, data),

    // Downloader
    getDownloaderHealth: () => api.get<DownloaderHealth>(`${base}/downloader/health`),
    detectDownload: (url: string) =>
      api.post<DownloaderDetectResult>(`${base}/downloader/detect`, { url }),
    listDownloaderJobs: () => api.get<DownloaderJob[]>(`${base}/downloader/downloads`),
    createDownloaderJob: (params: { url: string; title?: string; clientId: string; isYouTube?: boolean; isYouTubeMusic?: boolean; relayId?: string }) =>
      api.post<{ downloadId: string; status: string }>(`${base}/downloader/download`, params),
    cancelDownloaderJob: (id: string) =>
      api.post<void>(`${base}/downloader/cancel/${encodeURIComponent(id)}`),
    deleteDownloaderJob: (filename: string) =>
      api.delete<void>(`${base}/downloader/downloads/${encodeURIComponent(filename)}`),
    getDownloaderSettings: () => api.get<DownloaderSettings>(`${base}/downloader/settings`),
    listImportable: () => api.get<ImportableFile[]>(`${base}/downloader/importable`),
    importFile: (filename: string, deleteSource: boolean, triggerScan: boolean) =>
      api.post<ImportResult>(`${base}/downloader/import`, { filename, delete_source: deleteSource, trigger_scan: triggerScan }),

    // Server diagnostic routes (admin-only)
    getServerStatus: () => api.get<ServerStatus>('/api/status'),
    listModuleStatuses: () => api.get<ModuleHealth[]>('/api/modules'),
    getModuleHealth: (name: string) => api.get<ModuleHealth>(`/api/modules/${encodeURIComponent(name)}/health`),

    // Receiver media — individual item
    getSlaveMediaItem: (id: string) => api.get<ReceiverMedia>(`/api/receiver/media/${encodeURIComponent(id)}`),
  }
}

// ── Analytics (admin) ─────────────────────────────────────────────────────────

export function useAnalyticsApi() {
  return {
    getSummary: (period?: string) => {
      const qs = period ? `?period=${encodeURIComponent(period)}` : ''
      return api.get<AnalyticsSummary>(`/api/analytics${qs}`)
    },
    getDaily: (days?: number) => {
      const qs = days ? `?days=${days}` : ''
      return api.get<DailyStats[]>(`/api/analytics/daily${qs}`)
    },
    getTopMedia: (limit?: number) => {
      const qs = limit ? `?limit=${limit}` : ''
      return api.get<TopMediaItem[]>(`/api/analytics/top${qs}`)
    },
    submitEvent: (event: { type: string; media_id: string; duration?: number; data?: Record<string, unknown> }) =>
      api.post<{ status: string }>('/api/analytics/events', event),
    getEventStats: () => api.get<EventStats>('/api/analytics/events/stats'),
    getEventsByType: (type: string, limit?: number) => {
      const qs = new URLSearchParams({ type })
      if (limit) qs.set('limit', String(limit))
      return api.get<AnalyticsEvent[]>(`/api/analytics/events/by-type?${qs}`)
    },
    getEventsByMedia: (mediaId: string, limit?: number) => {
      const qs = new URLSearchParams({ media_id: mediaId })
      if (limit) qs.set('limit', String(limit))
      return api.get<AnalyticsEvent[]>(`/api/analytics/events/by-media?${qs}`)
    },
    getEventsByUser: (userId: string, limit?: number) => {
      const qs = new URLSearchParams({ user_id: userId })
      if (limit) qs.set('limit', String(limit))
      return api.get<AnalyticsEvent[]>(`/api/analytics/events/by-user?${qs}`)
    },
    getEventTypeCounts: () => api.get<EventTypeCounts>('/api/analytics/events/counts'),
    exportCsv: () => `/api/admin/analytics/export`,
  }
}

// ── Favorites ─────────────────────────────────────────────────────────────────

export function useFavoritesApi() {
  return {
    list: () => api.get<FavoriteItem[]>('/api/favorites'),
    add: (mediaId: string) => api.post<void>('/api/favorites', { media_id: mediaId }),
    remove: (mediaId: string) => api.delete<void>(`/api/favorites/${encodeURIComponent(mediaId)}`),
    check: (mediaId: string) => api.get<{ is_favorite: boolean }>(`/api/favorites/${encodeURIComponent(mediaId)}`),
  }
}

// ── API Tokens ────────────────────────────────────────────────────────────────

export function useAPITokensApi() {
  return {
    list: () => api.get<APIToken[]>('/api/auth/tokens'),
    create: (name: string) => api.post<APITokenCreated>('/api/auth/tokens', { name }),
    delete: (id: string) => api.delete<void>(`/api/auth/tokens/${encodeURIComponent(id)}`),
  }
}
