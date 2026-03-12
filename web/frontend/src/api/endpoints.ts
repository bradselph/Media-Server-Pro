/**
 * API endpoint functions organized by domain.
 * Each function calls the typed API client and returns strongly-typed data.
 */

import {api} from './client'
import type {
    AdminMediaListParams,
    AdminMediaListResponse,
    AdminPlaylistStats,
    AdminStats,
    AgeGateStatus,
    AnalyticsEvent,
    AnalyticsSummary,
    AuditLogEntry,
    BackupEntry,
    BannedIP,
    CachedMediaResult,
    CategorizedItem,
    CategoryStats,
    ClassifyStats,
    ClassifyStatus,
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
    StreamSession,
    Suggestion,
    SuggestionStats,
    SystemInfo,
    ThumbnailPreviews,
    ThumbnailStats,
    TopMediaItem,
    UploadProgress,
    UploadResult,
    ReceiverDuplicate,
    ReceiverMediaItem,
    ReceiverStats,
    SlaveNode,
    User,
    UserPermissions,
    UserPreferences,
    UserSession,
    ValidationResult,
    ValidatorStats,
    WatchHistoryEntry,
    ExtractorItem,
    ExtractorStats,
    CrawlTarget,
    CrawlerDiscovery,
    CrawlerStats,
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
    record: (id: string, rating: number) =>
        api.post<void>('/api/ratings', {id, rating}),
}

// ── Feature 4: Upload API ──

export const uploadApi = {
    upload: (files: File[], category?: string): Promise<UploadResult> => {
        const formData = new FormData()
        files.forEach(f => { formData.append('files', f); })
        if (category) formData.append('category', category)
        return api.upload<UploadResult>('/api/upload', formData)
    },

    // Returns upload progress for the given upload_id (from the uploaded[] array in UploadResult).
    // Completed entries remain accessible for 5 minutes after the upload finishes.
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

    getStreamUrl: (id: string) =>
        `/media?id=${encodeURIComponent(id)}`,

    getDownloadUrl: (id: string) =>
        `/download?id=${encodeURIComponent(id)}`,

    getRemoteStreamUrl: (url: string, source?: string) =>
        `/remote/stream?url=${encodeURIComponent(url)}${source ? `&source=${encodeURIComponent(source)}` : ''}`,

    getThumbnailUrl: (id: string) =>
        `/thumbnail?id=${encodeURIComponent(id)}`,

    getThumbnailPreviews: (id: string) =>
        api.get<ThumbnailPreviews>(`/api/thumbnails/previews?id=${encodeURIComponent(id)}`),

    getThumbnailBatch: (ids: string[], width?: number) => {
        const qs = new URLSearchParams({ids: ids.join(',')})
        if (width) qs.set('w', String(width))
        return api.get<{thumbnails: Record<string, string>}>(`/api/thumbnails/batch?${qs}`)
    },
}

// ── HLS ──

export const hlsApi = {
    getCapabilities: () =>
        api.get<HLSCapabilities>('/api/hls/capabilities'),

    check: (id: string) =>
        api.get<HLSAvailability>(`/api/hls/check?id=${encodeURIComponent(id)}`),

    // Backend accepts `quality` (string) or `qualities` ([]string); sending singular `quality` is handled.
    generate: (id: string, quality?: string) =>
        api.post<HLSJob>('/api/hls/generate', {id, quality}),

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

    create: (name: string, description?: string, is_public?: boolean) =>
        api.post<Playlist>('/api/playlists', {name, ...(description !== undefined && {description}), ...(is_public !== undefined && {is_public})}),

    delete: (id: string) =>
        api.delete<void>(`/api/playlists/${encodeURIComponent(id)}`),

    update: (id: string, data: { name?: string; description?: string; is_public?: boolean }) =>
        api.put<Playlist>(`/api/playlists/${encodeURIComponent(id)}`, data),

    addItem: (id: string, item: Pick<PlaylistItem, 'media_id' | 'title'>) =>
        api.post<void>(`/api/playlists/${encodeURIComponent(id)}/items`, item),

    removeItem: (id: string, mediaId: string) =>
        api.delete<void>(`/api/playlists/${encodeURIComponent(id)}/items?media_id=${encodeURIComponent(mediaId)}`),

    reorderItems: (id: string, positions: number[]) =>
        api.put<void>(`/api/playlists/${encodeURIComponent(id)}/reorder`, {positions}),

    clear: (id: string) =>
        api.delete<void>(`/api/playlists/${encodeURIComponent(id)}/clear`),

    copy: (id: string, name: string) =>
        api.post<Playlist>(`/api/playlists/${encodeURIComponent(id)}/copy`, {name}),

    // Feature 3: Playlist export — returns Blob for file download
    // For m3u/m3u8: raw text. For json: backend wraps in {success:true, data:...} envelope.
    export: (id: string, format: 'json' | 'm3u' | 'm3u8'): Promise<Blob> =>
        fetch(`/api/playlists/${encodeURIComponent(id)}/export?format=${format}`, {credentials: 'include'}).then(async r => {
            if (!r.ok) throw new Error(`Export failed: ${r.status} ${r.statusText}`)
            if (format === 'json') {
                const json = await r.json()
                const data = json.data ?? json
                return new Blob([JSON.stringify(data, null, 2)], {type: 'application/json'})
            }
            return r.blob()
        }),
}

// ── Analytics ──

export const analyticsApi = {
    getSummary: () =>
        api.get<AnalyticsSummary>('/api/analytics'),

    trackEvent: (event: { type: string; media_id: string; duration?: number; data?: Record<string, unknown> }) =>
        api.post<{ status: string }>('/api/analytics/events', event),
}

// ── Watch History ──

export const watchHistoryApi = {
    list: () =>
        api.get<WatchHistoryEntry[]>('/api/watch-history'),

    // Returns single-element array for the specific media, or empty array if not found.
    // More efficient than list() for resume-position lookups.
    getEntry: (id: string) =>
        api.get<WatchHistoryEntry[]>(`/api/watch-history?id=${encodeURIComponent(id)}`),

    getPosition: (id: string) =>
        api.get<{ position: number }>(`/api/playback?id=${encodeURIComponent(id)}`),

    trackPosition: (id: string, position: number, duration: number) =>
        api.post<void>('/api/playback', {id, position, duration}),

    // DELETE /api/watch-history?id= removes one item; without id removes all history.
    // Guard against empty id to prevent accidentally clearing all history.
    delete: (id: string) => {
        if (!id) throw new Error('watchHistoryApi.delete: id must not be empty')
        return api.delete<void>(`/api/watch-history?id=${encodeURIComponent(id)}`)
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

    // Returns Suggestion[] similar to the given media ID (public)
    getSimilar: (id: string) =>
        api.get<Suggestion[]>(`/api/suggestions/similar?id=${encodeURIComponent(id)}`),

    // Returns Suggestion[] (media_id, title, thumbnail_url, score) — not WatchHistoryEntry
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

    // checked_at is null before first check; string after first check.
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

    // Returns 202 Accepted immediately; poll getSourceUpdateProgress() every 2s for live status
    applySourceUpdate: () =>
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
        api.get<User[]>('/api/admin/users'),

    createUser: (data: {
        username: string;
        password: string;
        email?: string;
        role?: 'admin' | 'viewer';
        type?: string
    }) =>
        api.post<User>('/api/admin/users', data),

    getUser: (username: string) =>
        api.get<User>(`/api/admin/users/${encodeURIComponent(username)}`),

    updateUser: (username: string, data: { role?: string; enabled?: boolean; email?: string; permissions?: Partial<UserPermissions> }) =>
        api.put<User>(`/api/admin/users/${encodeURIComponent(username)}`, data),

    deleteUser: (username: string) =>
        api.delete<void>(`/api/admin/users/${encodeURIComponent(username)}`),

    // Bulk action on multiple users. action: "delete"|"enable"|"disable". Max 200 users per call.
    // The built-in "admin" account is always skipped. Returns { success, failed, errors[] }.
    bulkUsers: (usernames: string[], action: 'delete' | 'enable' | 'disable') =>
        api.post<{ success: number; failed: number; errors: string[] }>('/api/admin/users/bulk', {usernames, action}),

    getUserSessions: (username: string) =>
        api.get<UserSession[]>(`/api/admin/users/${encodeURIComponent(username)}/sessions`),

    changeUserPassword: (username: string, newPassword: string) =>
        api.post<void>(`/api/admin/users/${encodeURIComponent(username)}/password`, {new_password: newPassword}),

    changeAdminPassword: (current_password: string, new_password: string) =>
        api.post<void>('/api/admin/change-password', {current_password, new_password}),

    getActiveStreams: () =>
        api.get<StreamSession[]>('/api/admin/streams'),

    getActiveUploads: () =>
        api.get<UploadProgress[]>('/api/admin/uploads/active'),

    // Audit log
    getAuditLog: (limit?: number, offset?: number) => {
        const params = new URLSearchParams()
        if (limit !== undefined) params.set('limit', String(limit))
        if (offset !== undefined) params.set('offset', String(offset))
        const qs = params.toString()
        return api.get<AuditLogEntry[]>(`/api/admin/audit-log${qs ? `?${qs}` : ''}`)
    },

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
        api.post<{ message: string }>('/api/admin/media/scan'),

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

    runScan: (path?: string, autoApply?: boolean) =>
        api.post<void>('/api/admin/scanner/scan', {
            path: path ?? '',
            auto_apply: autoApply ?? false,
        }),

    getReviewQueue: () =>
        api.get<ScanResultItem[]>('/api/admin/scanner/queue'),

    batchReview: (action: 'approve' | 'reject', ids: string[]) =>
        api.post<{ updated: number; total: number }>('/api/admin/scanner/queue', {action, ids}),

    clearReviewQueue: () =>
        api.delete<void>('/api/admin/scanner/queue'),

    // Hugging Face visual classification
    getClassifyStatus: () =>
        api.get<ClassifyStatus>('/api/admin/classify/status'),
    getClassifyStats: () =>
        api.get<ClassifyStats>('/api/admin/classify/stats'),
    classifyFile: (path: string) =>
        api.post<{ path: string; tags: string[] }>('/api/admin/classify/file', {path}),
    classifyDirectory: (path: string) =>
        api.post<{ message: string; directory: string }>('/api/admin/classify/directory', {path}),
    classifyRunTask: () =>
        api.post<{ message: string }>('/api/admin/classify/run-task'),
    classifyClearTags: (id: string) =>
        api.post<{ message: string; id: string }>('/api/admin/classify/clear-tags', {id}),
    classifyAllPending: () =>
        api.post<{ message: string; count: number }>('/api/admin/classify/all-pending'),

    // HLS admin
    getHLSStats: () =>
        api.get<HLSStats>('/api/admin/hls/stats'),

    listHLSJobs: () =>
        api.get<HLSJob[]>('/api/admin/hls/jobs'),

    deleteHLSJob: (id: string) =>
        api.delete<void>(`/api/admin/hls/jobs/${encodeURIComponent(id)}`),

    cleanHLSStaleLocks: () =>
        api.post<void>('/api/admin/hls/clean/locks'),

    // threshold is Go time.Duration.String() format (e.g. "24h0m0s"), not ISO 8601
    cleanHLSInactive: (maxAge?: number) =>
        api.post<{
            removed: number;
            threshold: string
        }>('/api/admin/hls/clean/inactive', maxAge !== undefined ? {max_age_hours: maxAge} : {}),

    // Validator — surfaced in Admin Media > Validator tab
    validateMedia: (id: string) =>
        api.post<ValidationResult>('/api/admin/validator/validate', {id}),

    fixMedia: (id: string) =>
        api.post<ValidationResult>('/api/admin/validator/fix', {id}),

    getValidatorStats: () =>
        api.get<ValidatorStats>('/api/admin/validator/stats'),

    // AdminListMedia supports full sorting, filtering, and pagination — returns { items, total_items, total_pages }.
    listMedia: (params?: AdminMediaListParams) => {
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
            if (page !== undefined) {
                searchParams.set('page', String(page))
            }
            if (sort_order !== undefined && sort_order !== '') {
                searchParams.set('sort_order', sort_order)
            }
        }
        const qs = searchParams.toString()
        return api.get<AdminMediaListResponse>(`/api/admin/media${qs ? `?${qs}` : ''}`)
    },

    // On success returns the updated MediaItem; on lookup failure returns { message, path } instead.
    // Callers should check for `.id` before treating result as a full MediaItem.
    updateMedia: (id: string, data: Partial<Pick<MediaItem, 'name' | 'category' | 'tags' | 'is_mature'>>) =>
        api.put<MediaItem>(`/api/admin/media/${encodeURIComponent(id)}`, data),

    deleteMedia: (id: string) =>
        api.delete<void>(`/api/admin/media/${encodeURIComponent(id)}`),

    // Bulk action on multiple media items. action="delete" removes files; action="update" applies data fields.
    // Returns { success, failed, errors[] }. Max 500 ids per call.
    bulkMedia: (ids: string[], action: 'delete' | 'update', data?: { category?: string; is_mature?: boolean }) =>
        api.post<{ success: number; failed: number; errors: string[] }>('/api/admin/media/bulk', {ids, action, data}),

    getDailyStats: (days?: number) =>
        api.get<DailyStats[]>(`/api/analytics/daily${days ? `?days=${days}` : ''}`),

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
    getExtractorItems: () =>
        api.get<ExtractorItem[]>('/api/admin/extractor/items'),

    addExtractorItem: (url: string, title?: string) =>
        api.post<ExtractorItem>('/api/admin/extractor/items', { url, ...(title ? { title } : {}) }),

    removeExtractorItem: (id: string) =>
        api.delete<void>(`/api/admin/extractor/items/${encodeURIComponent(id)}`),

    getExtractorStats: () =>
        api.get<ExtractorStats>('/api/admin/extractor/stats'),

    // Crawler: target management + discovery review
    getCrawlerTargets: () =>
        api.get<CrawlTarget[]>('/api/admin/crawler/targets'),

    addCrawlerTarget: (url: string, name?: string) =>
        api.post<CrawlTarget>('/api/admin/crawler/targets', { url, ...(name ? { name } : {}) }),

    removeCrawlerTarget: (id: string) =>
        api.delete<void>(`/api/admin/crawler/targets/${encodeURIComponent(id)}`),

    crawlTarget: (id: string) =>
        api.post<{ new_discoveries: number }>(`/api/admin/crawler/targets/${encodeURIComponent(id)}/crawl`),

    getCrawlerDiscoveries: (status?: string) =>
        api.get<CrawlerDiscovery[]>(`/api/admin/crawler/discoveries${status ? `?status=${status}` : ''}`),

    approveCrawlerDiscovery: (id: string) =>
        api.post<CrawlerDiscovery>(`/api/admin/crawler/discoveries/${encodeURIComponent(id)}/approve`),

    ignoreCrawlerDiscovery: (id: string) =>
        api.post<void>(`/api/admin/crawler/discoveries/${encodeURIComponent(id)}/ignore`),

    deleteCrawlerDiscovery: (id: string) =>
        api.delete<void>(`/api/admin/crawler/discoveries/${encodeURIComponent(id)}`),

    getCrawlerStats: () =>
        api.get<CrawlerStats>('/api/admin/crawler/stats'),

    // Feature 5: Analytics detail + export
    exportAnalytics: (): Promise<Blob> =>
        fetch('/api/admin/analytics/export', {credentials: 'include'}).then(r => {
            if (!r.ok) throw new Error(`Export failed: ${r.status} ${r.statusText}`)
            return r.blob()
        }),

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
        // media_id must be the stable UUID from MediaItem.id (generated by the backend)
        const qs = new URLSearchParams({media_id: mediaId})
        if (limit) qs.set('limit', String(limit))
        return api.get<AnalyticsEvent[]>(`/api/analytics/events/by-media?${qs}`)
    },

    getEventsByUser: (userId: string, limit?: number) => {
        const qs = new URLSearchParams({user_id: userId})
        if (limit) qs.set('limit', String(limit))
        return api.get<AnalyticsEvent[]>(`/api/analytics/events/by-user?${qs}`)
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
    generateThumbnail: (id: string, isAudio?: boolean) =>
        api.post<void>('/api/admin/thumbnails/generate', {id, is_audio: isAudio ?? false}),

    getThumbnailStats: () =>
        api.get<ThumbnailStats>('/api/admin/thumbnails/stats'),

    // Feature 8: HLS validation
    validateHLS: (id: string) =>
        api.get<HLSValidationResult>(`/api/admin/hls/validate/${encodeURIComponent(id)}`),

    // Feature 9: Suggestion stats + scanner reject
    getSuggestionStats: () =>
        api.get<SuggestionStats>('/api/admin/suggestions/stats'),

    approveContent: (id: string) =>
        api.post<void>(`/api/admin/scanner/approve/${encodeURIComponent(id)}`),

    rejectContent: (id: string) =>
        api.post<void>(`/api/admin/scanner/reject/${encodeURIComponent(id)}`),

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

    // expires_at must be RFC3339 format (e.g. "2026-02-27T15:04:05Z") or omitted
    addToBlacklist: (ip: string, comment?: string, expiresAt?: string) =>
        api.post<void>('/api/admin/security/blacklist', {
            ip,
            comment,
            ...(expiresAt ? {expires_at: new Date(expiresAt).toISOString()} : {}),
        }),

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
        api.post<{ message: string }>('/api/admin/categorizer/set', {path, category}),

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
        api.post<CachedMediaResult>('/api/admin/remote/cache', {url, source_name: sourceName}),

    // ── Receiver admin (master-side slave management) ──
    getReceiverSlaves: () =>
        api.get<SlaveNode[]>('/api/admin/receiver/slaves'),

    getReceiverStats: () =>
        api.get<ReceiverStats>('/api/admin/receiver/stats'),

    removeReceiverSlave: (id: string) =>
        api.delete<void>(`/api/admin/receiver/slaves/${encodeURIComponent(id)}`),

    listReceiverDuplicates: (status = 'pending') =>
        api.get<ReceiverDuplicate[]>(`/api/admin/duplicates?status=${encodeURIComponent(status)}`),

    resolveReceiverDuplicate: (id: string, action: string) =>
        api.post<{message: string; action: string}>(
            `/api/admin/duplicates/${encodeURIComponent(id)}/resolve`,
            {action},
        ),
}

// ── Receiver media (admin diagnostics only — regular users see receiver media
// transparently in the main /api/media listing, streamed via /media?id=) ──────
export const receiverApi = {
    listMedia: () =>
        api.get<ReceiverMediaItem[]>('/api/receiver/media'),

    getMedia: (id: string) =>
        api.get<ReceiverMediaItem>(`/api/receiver/media/${encodeURIComponent(id)}`),
}
