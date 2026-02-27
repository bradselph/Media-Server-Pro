/**
 * API endpoint functions organized by domain.
 * Each function calls the typed API client and returns strongly-typed data.
 */

import {api} from './client'
import type {
    AdminPlaylistStats,
    AdminStats,
    AdminUser,
    AgeGateStatus,
    AnalyticsEvent,
    AnalyticsSummary,
    AuditLogEntry,
    BackupEntry,
    BannedIP,
    CategorizedItem,
    CategoryStats,
    DailyStats,
    DatabaseStatus,
    DiscoverySuggestion,
    EventStats,
    EventTypeCounts,
    HLSAvailability,
    HLSCapabilities,
    HLSJob,
    HLSStats,
    HLSValidationResult,
    IPEntry,
    LogEntry,
    LoginResponse,
    MediaCategory,
    MediaItem,
    MediaListParams,
    MediaListResponse,
    MediaStats,
    PermissionsInfo,
    Playlist,
    PlaylistItem,
    QueryResult,
    RemoteMediaItem,
    RemoteSource,
    RemoteSourceState,
    RemoteStats,
    ScannerStats,
    ScanResultItem,
    ScheduledTask,
    SecurityStats,
    ServerConfig,
    ServerSettings,
    SessionCheckResponse,
    StorageUsage,
    Suggestion,
    SuggestionStats,
    SystemInfo,
    ThumbnailStats,
    TopMediaItem,
    UploadProgress,
    UploadResult,
    User,
    UserPreferences,
    WatchHistoryEntry,
} from './types'

// ── Feature 1: Storage Usage & Permissions ──

export const storageApi = {
    getUsage: () =>
        api.get<StorageUsage>('/api/storage-usage'),
}

export const permissionsApi = {
    get: () =>
        api.get<PermissionsInfo>('/api/permissions'),
}

// ── Feature 2: Ratings ──

export const ratingsApi = {
    record: (path: string, rating: number) =>
        api.post<void>('/api/ratings', {path, rating}),
}

// ── Feature 4: Upload API ──

export const uploadApi = {
    upload: (files: File[], category?: string): Promise<UploadResult> => {
        const formData = new FormData()
        files.forEach(f => formData.append('files', f))
        if (category) formData.append('category', category)
        return api.upload<UploadResult>('/api/upload', formData)
    },

    getProgress: (id: string) =>
        api.get<UploadProgress>(`/api/upload/${encodeURIComponent(id)}/progress`),
}

// ── Auth ──

export const authApi = {
    login: (username: string, password: string) =>
        api.post<LoginResponse>('/api/auth/login', {username, password}),

    logout: () =>
        api.post<void>('/api/auth/logout'),

    register: (username: string, password: string, email?: string) =>
        api.post<User>('/api/auth/register', {username, password, email}),

    // authStore.checkSession() uses getSession() (not getMe()) — returns allow_guests + user without 401 for guests.
    getSession: () =>
        api.get<SessionCheckResponse>('/api/auth/session'),

    changePassword: (current_password: string, new_password: string) =>
        api.post<void>('/api/auth/change-password', {current_password, new_password}),

    deleteAccount: (password: string) =>
        api.post<void>('/api/auth/delete-account', {password}),
}

// ── Preferences ──

export const preferencesApi = {
    get: () =>
        api.get<UserPreferences>('/api/preferences'),

    // Uses POST with partial-merge semantics (only provided fields overwrite). Route is POST /api/preferences.
    update: (prefs: Partial<UserPreferences>) =>
        api.post<UserPreferences>('/api/preferences', prefs),
}

// ── Media ──

export const mediaApi = {
    list: (params?: MediaListParams) => {
        const searchParams = new URLSearchParams()
        if (params) {
            const {page, limit, sort_order, ...rest} = params
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

    get: (id: string) =>
        api.get<MediaItem>(`/api/media/${encodeURIComponent(id)}`),

    getStats: () =>
        api.get<MediaStats>('/api/media/stats'),

    getCategories: () =>
        api.get<MediaCategory[]>('/api/media/categories'),

    getStreamUrl: (path: string) =>
        `/media?path=${encodeURIComponent(path)}`,

    getDownloadUrl: (path: string) =>
        `/download?path=${encodeURIComponent(path)}`,

    getThumbnailUrl: (path: string) =>
        `/thumbnail?path=${encodeURIComponent(path)}`,
}

// ── HLS ──

export const hlsApi = {
    getCapabilities: () =>
        api.get<HLSCapabilities>('/api/hls/capabilities'),

    check: (path: string) =>
        api.get<HLSAvailability>(`/api/hls/check?path=${encodeURIComponent(path)}`),

    generate: (path: string, quality?: string) =>
        api.post<HLSJob>('/api/hls/generate', {path, quality}),

    getStatus: (id: string) =>
        api.get<HLSJob>(`/api/hls/status/${encodeURIComponent(id)}`),

    getMasterPlaylistUrl: (id: string) =>
        `/hls/${encodeURIComponent(id)}/master.m3u8`,
}

// ── Playlists ──

export const playlistApi = {
    list: () =>
        api.get<Playlist[]>('/api/playlists'),

    get: (id: string) =>
        api.get<Playlist>(`/api/playlists/${encodeURIComponent(id)}`),

    create: (name: string) =>
        api.post<Playlist>('/api/playlists', {name}),

    delete: (id: string) =>
        api.delete<void>(`/api/playlists/${encodeURIComponent(id)}`),

    update: (id: string, data: { name?: string; description?: string; is_public?: boolean }) =>
        api.put<Playlist>(`/api/playlists/${encodeURIComponent(id)}`, data),

    addItem: (id: string, item: Omit<PlaylistItem, 'position'>) =>
        api.post<void>(`/api/playlists/${encodeURIComponent(id)}/items`, item),

    removeItem: (id: string, path: string) =>
        api.delete<void>(`/api/playlists/${encodeURIComponent(id)}/items`, {media_path: path}),

    // Feature 3: Playlist export — returns Blob for file download
    export: (id: string, format: 'json' | 'm3u' | 'm3u8'): Promise<Blob> =>
        fetch(`/api/playlists/${encodeURIComponent(id)}/export?format=${format}`, {credentials: 'include'}).then(r => r.blob()),
}

// ── Analytics ──

export const analyticsApi = {
    // Backend route is GET /api/analytics (not /api/analytics/summary)
    getSummary: () =>
        api.get<AnalyticsSummary>('/api/analytics'),

    trackEvent: (event: { type: string; media_id: string; duration?: number; data?: Record<string, unknown> }) =>
        api.post<void>('/api/analytics/events', event),
}

// ── Watch History ──

export const watchHistoryApi = {
    list: () =>
        api.get<WatchHistoryEntry[]>('/api/watch-history'),

    // Returns single-element array for the specific file, or empty array if not found.
    // More efficient than list() for resume-position lookups.
    getEntry: (path: string) =>
        api.get<WatchHistoryEntry[]>(`/api/watch-history?path=${encodeURIComponent(path)}`),

    getPosition: (path: string) =>
        api.get<{ position: number }>(`/api/playback?path=${encodeURIComponent(path)}`),

    trackPosition: (path: string, position: number, duration: number) =>
        api.post<void>('/api/playback', {path, position, duration}),

    // DELETE /api/watch-history?path= removes one item; without path removes all history.
    // Guard against empty path to prevent accidentally clearing all history.
    delete: (path: string) => {
        if (!path) throw new Error('watchHistoryApi.delete: path must not be empty')
        return api.delete<void>(`/api/watch-history?path=${encodeURIComponent(path)}`)
    },

    // Backend DELETE /api/watch-history clears all history (no /all suffix route exists)
    clear: () =>
        api.delete<void>('/api/watch-history'),
}

// ── Server Settings ──

export const settingsApi = {
    getServerSettings: () =>
        api.get<ServerSettings>('/api/server-settings'),
}

// ── Age Gate ──

export const ageGateApi = {
    // GET /api/age-gate/status — public, returns { enabled, verified }
    getStatus: () =>
        api.get<AgeGateStatus>('/api/age-gate/status'),

    // POST /api/age-verify — records age consent, sets cookie + caches IP
    verify: () =>
        api.post<void>('/api/age-verify'),
}

// ── Suggestions ──

export const suggestionsApi = {
    get: () =>
        api.get<Suggestion[]>('/api/suggestions'),

    getTrending: () =>
        api.get<Suggestion[]>('/api/suggestions/trending'),

    // Returns Suggestion[] similar to the given media path (public)
    getSimilar: (path: string) =>
        api.get<Suggestion[]>(`/api/suggestions/similar?path=${encodeURIComponent(path)}`),

    // Returns Suggestion[] (media_path, title, thumbnail_url, score) — not WatchHistoryEntry
    getContinueWatching: () =>
        api.get<Suggestion[]>('/api/suggestions/continue'),

    // Auth-gated personalized suggestions (Feature 2)
    getPersonalized: (limit?: number) =>
        api.get<Suggestion[]>(`/api/suggestions/personalized${limit ? `?limit=${limit}` : ''}`),
}

// ── Admin ──

export const adminApi = {
    // Dashboard
    getStats: () =>
        // disk_usage = raw used bytes (uint64); active_sessions = concurrent file streams (not auth sessions)
        api.get<AdminStats>('/api/admin/stats'),

    getSystemInfo: () =>
        api.get<SystemInfo>('/api/admin/system'),


    // Server control
    restartServer: () =>
        api.post<void>('/api/admin/server/restart'),

    shutdownServer: () =>
        api.post<void>('/api/admin/server/shutdown'),

    clearCache: () =>
        api.post<void>('/api/admin/cache/clear'),

    // checked_at is always populated by CheckForUpdates so non-nullable here is correct.
    // Backend also sends published_at?: string (omitempty) from UpdateCheckResult.
    checkUpdates: () =>
        api.get<{
            update_available: boolean
            current_version: string
            latest_version: string
            release_url?: string
            release_notes?: string
            published_at?: string
            checked_at: string
            error?: string
        }>('/api/admin/update/check'),

    // checked_at is null before first check; latest_version is "" before first check.
    getUpdateStatus: () =>
        api.get<{
            update_available: boolean
            current_version: string
            latest_version: string
            release_url?: string
            release_notes?: string
            published_at?: string
            checked_at: string | null
            error?: string
        }>('/api/admin/update/status'),

    // Synchronous — blocks until install completes. Returns final UpdateStatus (stage, progress, error).
    applyUpdate: () =>
        api.post<{
            stage: string
            progress: number
            in_progress: boolean
            started_at: string
            error?: string
            backup_path?: string
        }>('/api/admin/update/apply'),

    // Source-based updates (git pull + go build)
    checkSourceUpdates: () =>
        api.get<{
            updates_available: boolean
            remote_commit: string
        }>('/api/admin/update/source/check'),

    applySourceUpdate: () =>
        // Returns 202 Accepted immediately; poll getSourceUpdateProgress() every 2s for live status
        api.post<{
            stage: string
            progress: number
            in_progress: boolean
            started_at: string
            error?: string
            backup_path?: string
        }>('/api/admin/update/source/apply'),

    getSourceUpdateProgress: () =>
        // started_at absent when idle; guard with !started_at.startsWith('0001') when present
        api.get<{
            stage: string
            progress: number
            in_progress: boolean
            started_at?: string
            error?: string
            backup_path?: string
        }>('/api/admin/update/source/progress'),

    getUpdateConfig: () =>
        api.get<{
            update_method: 'source' | 'binary'
            branch: string
        }>('/api/admin/update/config'),

    setUpdateConfig: (data: { update_method?: 'source' | 'binary'; branch?: string }) =>
        api.put<{
            update_method: 'source' | 'binary'
            branch: string
        }>('/api/admin/update/config', data),

    // Users
    listUsers: () =>
        api.get<AdminUser[]>('/api/admin/users'),

    createUser: (data: {
        username: string;
        password: string;
        email?: string;
        role?: 'admin' | 'viewer';
        type?: string
    }) =>
        api.post<AdminUser>('/api/admin/users', data),

    getUser: (username: string) =>
        api.get<AdminUser>(`/api/admin/users/${encodeURIComponent(username)}`),

    updateUser: (username: string, data: Partial<AdminUser>) =>
        api.put<AdminUser>(`/api/admin/users/${encodeURIComponent(username)}`, data),

    deleteUser: (username: string) =>
        api.delete<void>(`/api/admin/users/${encodeURIComponent(username)}`),

    // Bulk action on multiple users. action: "delete"|"enable"|"disable". Max 200 users per call.
    // The built-in "admin" account is always skipped. Returns { success, failed, errors[] }.
    bulkUsers: (usernames: string[], action: 'delete' | 'enable' | 'disable') =>
        api.post<{ success: number; failed: number; errors: string[] }>('/api/admin/users/bulk', {usernames, action}),

    changeUserPassword: (username: string, newPassword: string) =>
        api.post<void>(`/api/admin/users/${encodeURIComponent(username)}/password`, {new_password: newPassword}),

    changeAdminPassword: (current_password: string, new_password: string) =>
        api.post<void>('/api/admin/change-password', {current_password, new_password}),

    // Audit log
    getAuditLog: () =>
        api.get<AuditLogEntry[]>('/api/admin/audit-log'),

    // DEPRECATED: R-08 — returns a raw string, not a Promise, breaking the typed API pattern.
    // Use exportAuditLogUrl() instead, which makes the return type explicit.
    exportAuditLog: () =>
        '/api/admin/audit-log/export',

    // R-08: explicit URL helper — callers use this as an <a href> or window.open() target
    exportAuditLogUrl: (): string =>
        '/api/admin/audit-log/export',

    // Logs
    getLogs: (level?: string, module?: string, limit?: number) => {
        const params = new URLSearchParams()
        if (level) params.set('level', level)
        if (module) params.set('module', module)
        if (limit) params.set('limit', String(limit))
        const qs = params.toString()
        return api.get<LogEntry[]>(`/api/admin/logs${qs ? `?${qs}` : ''}`)
    },

    // Config
    getConfig: () =>
        api.get<ServerConfig>('/api/admin/config'),

    updateConfig: (config: Partial<ServerConfig>) =>
        api.put<ServerConfig>('/api/admin/config', config),

    // Tasks
    listTasks: () =>
        api.get<ScheduledTask[]>('/api/admin/tasks'),

    runTask: (id: string) =>
        api.post<void>(`/api/admin/tasks/${encodeURIComponent(id)}/run`),

    enableTask: (id: string) =>
        api.post<void>(`/api/admin/tasks/${encodeURIComponent(id)}/enable`),

    disableTask: (id: string) =>
        api.post<void>(`/api/admin/tasks/${encodeURIComponent(id)}/disable`),

    stopTask: (id: string) =>
        api.post<void>(`/api/admin/tasks/${encodeURIComponent(id)}/stop`),

    // Media management
    scanMedia: () =>
        api.post<void>('/api/admin/media/scan'),

    // Backups (v2 — uses backup module, supports delete and type selection)
    // Backend also sends files[], errors[], version — extra fields ignored by frontend type.
    listBackups: () =>
        api.get<BackupEntry[]>('/api/admin/backups/v2'),

    createBackup: (description?: string, backupType?: string) =>
        api.post<BackupEntry>('/api/admin/backups/v2', {
            description: description ?? '',
            backup_type: backupType ?? 'full'
        }),

    restoreBackup: (id: string) =>
        api.post<void>(`/api/admin/backups/v2/${encodeURIComponent(id)}/restore`),

    deleteBackup: (id: string) =>
        api.delete<void>(`/api/admin/backups/v2/${encodeURIComponent(id)}`),

    // Scanner (content review) — backend route is /api/admin/scanner/queue (not /review-queue)
    getScannerStats: () =>
        api.get<ScannerStats>('/api/admin/scanner/stats'),

    runScan: () =>
        api.post<void>('/api/admin/scanner/scan'),

    getReviewQueue: () =>
        api.get<ScanResultItem[]>('/api/admin/scanner/queue'),

    batchReview: (action: string, paths: string[]) =>
        api.post<void>('/api/admin/scanner/queue', {action, paths}),

    clearReviewQueue: () =>
        api.delete<void>('/api/admin/scanner/queue'),

    approveContent: (path: string) =>
        api.post<void>(`/api/admin/scanner/approve/${path.split('/').map(encodeURIComponent).join('/')}`),

    // HLS admin
    getHLSStats: () =>
        api.get<HLSStats>('/api/admin/hls/stats'),

    listHLSJobs: () =>
        api.get<HLSJob[]>('/api/admin/hls/jobs'),

    deleteHLSJob: (id: string) =>
        api.delete<void>(`/api/admin/hls/jobs/${encodeURIComponent(id)}`),

    cleanHLSStaleLocks: () =>
        api.post<void>('/api/admin/hls/clean/locks'),

    cleanHLSInactive: (maxAge?: number) =>
        api.post<void>('/api/admin/hls/clean/inactive', maxAge !== undefined ? {max_age_hours: maxAge} : {}),

    // Validator
    validateMedia: (path: string) =>
        api.post<{ valid: boolean; errors?: string[] }>('/api/admin/validator/validate', {path}),

    fixMedia: (path: string) =>
        api.post<{ fixed: boolean; message?: string }>('/api/admin/validator/fix', {path}),

    getValidatorStats: () =>
        api.get<{ total: number; validated: number; needs_fix: number; fixed: number; failed: number; unsupported: number }>('/api/admin/validator/stats'),

    // AdminListMedia only supports search/page/limit — other filters (sort, type, category) are ignored.
    listMedia: (params?: { page?: number; limit?: number; search?: string }) => {
        const qs = params ? '?' + new URLSearchParams(Object.entries(params).filter(([, v]) => v !== undefined).map(([k, v]) => [k, String(v)])).toString() : ''
        return api.get<MediaItem[]>(`/api/admin/media${qs}`)
    },

    // On success returns the updated MediaItem; on lookup failure returns { message, path } instead.
    // Callers should check for `.id` before treating result as a full MediaItem.
    updateMedia: (path: string, data: Partial<Pick<MediaItem, 'name' | 'category' | 'tags' | 'is_mature'>>) =>
        api.put<MediaItem>(`/api/admin/media/${path.split('/').map(encodeURIComponent).join('/')}`, data),

    deleteMedia: (path: string) =>
        api.delete<void>(`/api/admin/media/${path.split('/').map(encodeURIComponent).join('/')}`),

    // Bulk action on multiple files. action="delete" removes files; action="update" applies data fields.
    // Returns { success, failed, errors[] }. Max 500 paths per call.
    bulkMedia: (paths: string[], action: 'delete' | 'update', data?: { category?: string; is_mature?: boolean }) =>
        api.post<{ success: number; failed: number; errors: string[] }>('/api/admin/media/bulk', {paths, action, data}),

    // DailyStats.date is a "YYYY-MM-DD" string. Route requires admin auth.
    getDailyStats: (days?: number) =>
        api.get<DailyStats[]>(`/api/analytics/daily${days ? `?days=${days}` : ''}`),

    // media_path is optional — guard with `if (item.media_path)` before building player URLs.
    getTopMedia: (limit?: number) =>
        api.get<TopMediaItem[]>(`/api/analytics/top${limit ? `?limit=${limit}` : ''}`),

    getEventTypeCounts: () =>
        api.get<EventTypeCounts>('/api/analytics/events/counts'),

    // Database
    getDatabaseStatus: () =>
        api.get<DatabaseStatus>('/api/admin/database/status'),

    executeQuery: (query: string) =>
        api.post<QueryResult>('/api/admin/database/query', {query}),

    // last_sync "0001-01-01T00:00:00Z" means never synced. Returns 404 if remote feature disabled.
    getRemoteSources: () =>
        api.get<RemoteSourceState[]>('/api/admin/remote/sources'),

    // Returns RemoteSource (not RemoteSourceState) — wrap in {source, status:"idle",...} before adding to list.
    createRemoteSource: (data: { name: string; url: string; username?: string; password?: string }) =>
        api.post<RemoteSource>('/api/admin/remote/sources', {...data, enabled: true}),

    deleteRemoteSource: (name: string) =>
        api.delete<void>(`/api/admin/remote/sources/${encodeURIComponent(name)}`),

    // status is always "sync_started" — sync is async, poll getRemoteSources() for completion.
    syncRemoteSource: (name: string) =>
        api.post<{ status: string }>(`/api/admin/remote/sources/${encodeURIComponent(name)}/sync`),

    getRemoteStats: () =>
        api.get<RemoteStats>('/api/admin/remote/stats'),

    cleanRemoteCache: () =>
        api.post<{ removed: number }>('/api/admin/remote/cache/clean'),

    // Feature 5: Analytics detail + export
    exportAnalytics: (): Promise<Blob> =>
        fetch('/api/admin/analytics/export', {credentials: 'include'}).then(r => r.blob()),

    // Route is /api/analytics/events/stats (not /api/admin/...) but requires admin auth.
    getEventStats: () =>
        api.get<EventStats>('/api/analytics/events/stats'),

    // Returns []models.AnalyticsEvent — use AnalyticsEvent[] not AnalyticsSummary[]
    getEventsByType: (type: string, limit?: number) => {
        const qs = new URLSearchParams({type})
        if (limit) qs.set('limit', String(limit))
        return api.get<AnalyticsEvent[]>(`/api/analytics/events/by-type?${qs}`)
    },

    getEventsByMedia: (mediaId: string, limit?: number) => {
        // media_id must be the MD5 hash of the file path (MediaItem.id), not the file path itself
        const qs = new URLSearchParams({media_id: mediaId})
        if (limit) qs.set('limit', String(limit))
        return api.get<AnalyticsEvent[]>(`/api/analytics/events/by-media?${qs}`)
    },

    // Feature 6: Admin playlists management — backend returns []*models.Playlist → use Playlist[]
    listAllPlaylists: (params?: { page?: number; limit?: number; search?: string }) => {
        const qs = params ? '?' + new URLSearchParams(Object.entries(params).filter(([, v]) => v !== undefined).map(([k, v]) => [k, String(v)])).toString() : ''
        return api.get<Playlist[]>(`/api/admin/playlists${qs}`)
    },

    getPlaylistStats: () =>
        api.get<AdminPlaylistStats>('/api/admin/playlists/stats'),

    deletePlaylist: (id: string) =>
        api.delete<void>(`/api/admin/playlists/${encodeURIComponent(id)}`),

    // Bulk delete playlists by ID. Returns { success, failed, errors[] }.
    bulkDeletePlaylists: (ids: string[]) =>
        api.post<{ success: number; failed: number; errors: string[] }>('/api/admin/playlists/bulk', {ids}),

    // Feature 7: Thumbnail admin
    generateThumbnail: (path: string, isAudio?: boolean) =>
        api.post<void>('/api/admin/thumbnails/generate', {path, is_audio: isAudio ?? false}),

    getThumbnailStats: () =>
        api.get<ThumbnailStats>('/api/admin/thumbnails/stats'),

    // Feature 8: HLS validation
    validateHLS: (id: string) =>
        api.get<HLSValidationResult>(`/api/admin/hls/validate/${encodeURIComponent(id)}`),

    // Feature 9: Suggestion stats + scanner reject
    getSuggestionStats: () =>
        api.get<SuggestionStats>('/api/admin/suggestions/stats'),

    // POST /api/admin/scanner/reject/{path:.*} — backend registered as POST (not DELETE)
    rejectContent: (path: string) =>
        api.post<void>(`/api/admin/scanner/reject/${path.split('/').map(encodeURIComponent).join('/')}`),

    // Feature 10: Security
    getSecurityStats: () =>
        api.get<SecurityStats>('/api/admin/security/stats'),

    getWhitelist: () =>
        api.get<IPEntry[]>('/api/admin/security/whitelist'),

    addToWhitelist: (ip: string, comment?: string) =>
        api.post<void>('/api/admin/security/whitelist', {ip, comment}),

    // Backend: DELETE /security/whitelist (no path var) — reads IP from request body
    removeFromWhitelist: (ip: string) =>
        api.delete<void>('/api/admin/security/whitelist', {ip}),

    getBlacklist: () =>
        api.get<IPEntry[]>('/api/admin/security/blacklist'),

    addToBlacklist: (ip: string, comment?: string, expiresAt?: string) =>
        api.post<void>('/api/admin/security/blacklist', {ip, comment, expires_at: expiresAt}),

    // Backend: DELETE /security/blacklist (no path var) — reads IP from request body
    removeFromBlacklist: (ip: string) =>
        api.delete<void>('/api/admin/security/blacklist', {ip}),

    getBannedIPs: () =>
        api.get<BannedIP[]>('/api/admin/security/banned'),

    banIP: (ip: string, durationMinutes: number) =>
        api.post<void>('/api/admin/security/ban', {ip, duration_minutes: durationMinutes}),

    // Backend: POST /security/unban (not DELETE) — reads IP from request body
    unbanIP: (ip: string) =>
        api.post<void>('/api/admin/security/unban', {ip}),

    // Feature 11: Categorizer
    categorizeFile: (path: string) =>
        api.post<CategorizedItem>('/api/admin/categorizer/file', {path}),

    categorizeDirectory: (dir: string) =>
        api.post<CategorizedItem[]>('/api/admin/categorizer/directory', {directory: dir}),

    getCategoryStats: () =>
        api.get<CategoryStats>('/api/admin/categorizer/stats'),

    setMediaCategory: (path: string, category: string) =>
        api.post<void>('/api/admin/categorizer/set', {path, category}),

    getByCategory: (category: string) =>
        api.get<CategorizedItem[]>(`/api/admin/categorizer/by-category?category=${encodeURIComponent(category)}`),

    cleanStaleCategories: () =>
        api.post<{ removed: number }>('/api/admin/categorizer/clean'),

    // Feature 12: Auto-discovery
    discoveryScan: (directory: string) =>
        api.post<DiscoverySuggestion[]>('/api/admin/discovery/scan', {directory}),

    getDiscoverySuggestions: () =>
        api.get<DiscoverySuggestion[]>('/api/admin/discovery/suggestions'),

    applyDiscoverySuggestion: (originalPath: string) =>
        api.post<void>('/api/admin/discovery/apply', {original_path: originalPath}),

    // Backend: DELETE /api/admin/discovery/{path:.*} — path in URL, no body
    dismissDiscoverySuggestion: (originalPath: string) =>
        api.delete<void>(`/api/admin/discovery/${originalPath.split('/').map(encodeURIComponent).join('/')}`),

    // Feature 13: Remote media browsing
    getAllRemoteMedia: () =>
        api.get<RemoteMediaItem[]>('/api/admin/remote/media'),

    getSourceMedia: (source: string) =>
        api.get<RemoteMediaItem[]>(`/api/admin/remote/sources/${encodeURIComponent(source)}/media`),

    cacheRemoteMedia: (url: string, sourceName: string) =>
        api.post<void>('/api/admin/remote/cache', {url, source_name: sourceName}),
}
