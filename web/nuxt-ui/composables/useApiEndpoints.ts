import type {
  User, UserPermissions, UserPreferences,
  LoginResponse, SessionCheckResponse,
  MediaItem, MediaListParams, MediaListResponse, MediaCategory,
  AdminMediaListResponse, AdminMediaListParams,
  HLSAvailability, HLSJob, HLSStats,
  Playlist, PlaylistItem,
  AnalyticsSummary, DailyStats, TopMediaItem,
  AdminStats, SystemInfo, StreamSession, UploadProgress,
  AuditLogEntry, LogEntry, ScheduledTask, BackupEntry,
  ThumbnailStats, ScannerStats, FileScanResult,
  UpdateInfo, UpdateStatus,
  IPListEntry, SecurityStats,
  DatabaseStatus, ReceiverSlave, ReceiverMedia,
  CrawlerTarget, CrawlerDiscovery, ExtractorItem, DownloaderJob,
  WatchHistoryItem, Suggestion, StorageUsage, PermissionsInfo,
  ServerSettings,
} from '~/types/api'
import { normalizeLogin, normalizePreferences, normalizeSession, toPreferencesPatch } from '~/utils/apiCompat'

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
        const { page, limit, sort_order, sort_by, sort, ...rest } = params
        Object.entries(rest).forEach(([k, v]) => {
          if (v !== undefined && v !== '') qs.set(k, String(v))
        })
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
    getCategories: () => api.get<MediaCategory[]>('/api/media/categories'),
    getThumbnailUrl: (id: string) => `/thumbnail?id=${encodeURIComponent(id)}`,
    getStreamUrl: (id: string) => `/media?id=${encodeURIComponent(id)}`,
    getDownloadUrl: (id: string) => `/download?id=${encodeURIComponent(id)}`,
  }
}

// ── HLS ───────────────────────────────────────────────────────────────────────

export function useHlsApi() {
  return {
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
  }
}

// ── Watch History ─────────────────────────────────────────────────────────────

export function useWatchHistoryApi() {
  return {
    list: (limit?: number) => api.get<WatchHistoryItem[]>(`/api/watch-history${limit ? `?limit=${limit}` : ''}`),
    remove: (id: string) => api.delete<void>(`/api/watch-history?id=${encodeURIComponent(id)}`),
    clear: () => api.delete<void>('/api/watch-history'),
  }
}

// ── Suggestions ───────────────────────────────────────────────────────────────

export function useSuggestionsApi() {
  return {
    getSimilar: (id: string) => api.get<Suggestion[]>(`/api/suggestions/similar?id=${encodeURIComponent(id)}`),
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
  }
}

// ── Settings ──────────────────────────────────────────────────────────────────

export function useSettingsApi() {
  return {
    get: () => api.get<ServerSettings>('/api/server-settings'),
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
    changeUserPassword: (username: string, password: string) =>
      api.post<void>(`${base}/users/${encodeURIComponent(username)}/password`, { new_password: password }),
    getUserSessions: (username: string) =>
      api.get<unknown[]>(`${base}/users/${encodeURIComponent(username)}/sessions`),
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
    generateThumbnail: (id: string) =>
      api.post<void>(`${base}/thumbnails/generate`, { id }),
    getThumbnailStats: () => api.get<ThumbnailStats>(`${base}/thumbnails/stats`),

    // HLS
    getHLSStats: () => api.get<HLSStats>(`${base}/hls/stats`),
    listHLSJobs: () => api.get<HLSJob[]>(`${base}/hls/jobs`),
    deleteHLSJob: (id: string) => api.delete<void>(`${base}/hls/jobs/${encodeURIComponent(id)}`),
    cleanHLSInactive: () => api.post<void>(`${base}/hls/clean/inactive`),

    // Tasks
    listTasks: () => api.get<ScheduledTask[]>(`${base}/tasks`),
    runTask: (id: string) => api.post<void>(`${base}/tasks/${encodeURIComponent(id)}/run`),
    enableTask: (id: string) => api.post<void>(`${base}/tasks/${encodeURIComponent(id)}/enable`),
    disableTask: (id: string) => api.post<void>(`${base}/tasks/${encodeURIComponent(id)}/disable`),

    // Audit log — backend reads `limit` and `offset` (not `page`)
    getAuditLog: (params?: { offset?: number; limit?: number; user_id?: string }) => {
      const qs = new URLSearchParams()
      if (params) Object.entries(params).forEach(([k, v]) => { if (v !== undefined) qs.set(k, String(v)) })
      return api.get<AuditLogEntry[]>(`${base}/audit-log?${qs}`)
    },

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
    createBackup: () => api.post<BackupEntry>(`${base}/backups/v2`),
    restoreBackup: (id: string) => api.post<void>(`${base}/backups/v2/${encodeURIComponent(id)}/restore`),
    deleteBackup: (id: string) => api.delete<void>(`${base}/backups/v2/${encodeURIComponent(id)}`),

    // Scanner / Content review
    getScannerStats: () => api.get<ScannerStats>(`${base}/scanner/stats`),
    getReviewQueue: () => api.get<FileScanResult[]>(`${base}/scanner/queue`),
    approveContent: (id: string) => api.post<void>(`${base}/scanner/approve/${encodeURIComponent(id)}`),
    rejectContent: (id: string) => api.post<void>(`${base}/scanner/reject/${encodeURIComponent(id)}`),
    runScan: (path?: string) => api.post<void>(`${base}/scanner/scan`, path ? { path } : undefined),

    // Security
    getSecurityStats: () => api.get<SecurityStats>(`${base}/security/stats`),
    getWhitelist: () => api.get<IPListEntry[]>(`${base}/security/whitelist`),
    addToWhitelist: (ip: string, comment?: string) =>
      api.post<void>(`${base}/security/whitelist`, { ip, comment }),
    removeFromWhitelist: (ip: string) =>
      api.delete<void>(`${base}/security/whitelist?ip=${encodeURIComponent(ip)}`),
    getBlacklist: () => api.get<IPListEntry[]>(`${base}/security/blacklist`),
    addToBlacklist: (ip: string, comment?: string) =>
      api.post<void>(`${base}/security/blacklist`, { ip, comment }),
    removeFromBlacklist: (ip: string) =>
      api.delete<void>(`${base}/security/blacklist?ip=${encodeURIComponent(ip)}`),
    getBannedIPs: () => api.get<IPListEntry[]>(`${base}/security/banned`),
    banIP: (ip: string) => api.post<void>(`${base}/security/ban`, { ip }),
    unbanIP: (ip: string) => api.post<void>(`${base}/security/unban`, { ip }),

    // Database
    getDatabaseStatus: () => api.get<DatabaseStatus>(`${base}/database/status`),

    // Receiver / Slaves — slaves list is under /api/admin/; media browse is at /api/receiver/media
    listSlaves: () => api.get<ReceiverSlave[]>(`${base}/receiver/slaves`),
    getSlaveMedia: () => api.get<ReceiverMedia[]>(`/api/receiver/media`),

    // Crawler — all under /api/admin/crawler/
    listCrawlerTargets: () => api.get<CrawlerTarget[]>(`${base}/crawler/targets`),
    addCrawlerTarget: (url: string, name?: string) =>
      api.post<CrawlerTarget>(`${base}/crawler/targets`, { url, name }),
    deleteCrawlerTarget: (id: string) =>
      api.delete<void>(`${base}/crawler/targets/${encodeURIComponent(id)}`),
    getCrawlerDiscoveries: (targetId?: string) => {
      const qs = targetId ? `?target_id=${encodeURIComponent(targetId)}` : ''
      return api.get<CrawlerDiscovery[]>(`${base}/crawler/discoveries${qs}`)
    },
    startCrawl: (targetId: string) =>
      api.post<void>(`${base}/crawler/targets/${encodeURIComponent(targetId)}/crawl`),

    // Extractor — all under /api/admin/extractor/
    listExtractorItems: () => api.get<ExtractorItem[]>(`${base}/extractor/items`),
    addExtractorUrl: (url: string) => api.post<ExtractorItem>(`${base}/extractor/items`, { url }),
    deleteExtractorItem: (id: string) =>
      api.delete<void>(`${base}/extractor/items/${encodeURIComponent(id)}`),

    // Playlists (admin)
    listAllPlaylists: () => api.get<{ items: Playlist[] } | Playlist[]>(`${base}/playlists`),
    deletePlaylist: (id: string) => api.delete<void>(`${base}/playlists/${encodeURIComponent(id)}`),

    // Updates
    checkForUpdates: () => api.get<UpdateInfo>(`${base}/update/check`),
    getUpdateStatus: () => api.get<UpdateStatus>(`${base}/update/status`),
    applyUpdate: () => api.post<UpdateStatus>(`${base}/update/apply`),

    // Downloader
    listDownloaderJobs: () => api.get<DownloaderJob[]>(`${base}/downloader/downloads`),
    createDownloaderJob: (url: string, clientId: string) =>
      api.post<{ id: string }>(`${base}/downloader/download`, { url, clientId }),
    cancelDownloaderJob: (id: string) =>
      api.post<void>(`${base}/downloader/cancel/${encodeURIComponent(id)}`),
    deleteDownloaderJob: (filename: string) =>
      api.delete<void>(`${base}/downloader/downloads/${encodeURIComponent(filename)}`),
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
    exportCsv: () => `/api/admin/analytics/export`,
  }
}
