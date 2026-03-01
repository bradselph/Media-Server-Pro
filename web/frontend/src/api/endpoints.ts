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
    User,
    UserPreferences,
    UserSession,
    ValidationResult,
    ValidatorStats,
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
    record: (id: string, rating: number) =>
        api.post<void>('/api/ratings', {id, rating}),
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
}

// ── HLS ──

export const hlsApi = {
    getCapabilities: () =>
        api.get<HLSCapabilities>('/api/hls/capabilities'),

    // TODO: API Contract Mismatch - Frontend sends query param `id` (endpoints.ts:199)
    // but backend CheckHLSAvailability handler (api/handlers/hls.go:29) reads `c.Query("id")`.
    // This is ALIGNED. However, the handler resolves the id via resolveMediaByID which expects
    // a UUID (stable media ID). If a caller passes anything other than the stable UUID from
    // MediaItem.id, this will return 404. No change needed if callers always use MediaItem.id.
    check: (id: string) =>
        api.get<HLSAvailability>(`/api/hls/check?id=${encodeURIComponent(id)}`),

    // TODO: API Contract Mismatch - Frontend sends `{id, quality}` where quality is a single
    // string (endpoints.ts:202) but backend GenerateHLS handler (api/handlers/hls.go:77-88)
    // expects EITHER `id + qualities` ([]string) OR `id + quality` (string, normalized to
    // []string internally). The frontend only sends `quality` (singular), which maps to the
    // `quality` field in the backend struct — this is handled. However, the return type
    // HLSJob interface does not include `job_id` as a separate field, yet the handler
    // (hls.go:109) returns BOTH `"job_id": job.ID` and `"id": job.ID`. The TypeScript
    // HLSJob type only has `id` (types.ts:237) — job_id is silently ignored by the client,
    // which is fine since both map to the same value. No functional breakage.
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
    // TODO: API Contract Mismatch - `getSummary` calls GET /api/analytics (endpoints.ts:277)
    // but the backend route (api/routes/routes.go:311) registers this as:
    //   api.GET("/analytics", adminAuth(authModule), h.GetAnalyticsSummary)
    // — it requires adminAuth, not just requireAuth.
    // IndexPage.tsx (line 766) calls this with `enabled: isAuthenticated` — meaning any
    // authenticated regular user triggers this call and receives a 401 Unauthorized response.
    // AdminPage.tsx (line 1754) correctly calls this only in the admin context.
    // Fix: either (a) change the route to requireAuth() if summary data is intended for all users,
    // OR (b) add `enabled: isAdmin` guard in IndexPage.tsx query (line 767) so only admins call it.
    // Backend route: api/routes/routes.go:311. Frontend callers: IndexPage.tsx:766, AdminPage.tsx:1754.
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
    // Backend also sends published_at from UpdateCheckResult.
    // TODO: API Contract Mismatch - `published_at?: string` is typed as optional (may be absent).
    // The backend updater.UpdateCheckResult.PublishedAt has json:"published_at,omitempty"
    // (internal/updater/updater.go:73). However, Go's omitempty on time.Time does NOT omit
    // a zero time.Time value — Go only omits zero values for primitive types (int, string, bool)
    // with omitempty; struct types (including time.Time) are never considered zero by reflect.
    // Result: when a release has no published date, the frontend receives
    // "published_at":"0001-01-01T00:00:00Z" (field IS present), not an absent field.
    // Callers that check `if (status.published_at)` will find it truthy even for the epoch date.
    // Fix: use *time.Time (pointer) with omitempty in the Go struct — nil pointer IS omitted.
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

    // checked_at is null before first check (handler.go:685 explicitly sets checked_at: nil);
    // when a check has been run, checked_at is updater.UpdateCheckResult.CheckedAt serialized
    // as RFC3339 string (time.Time json:"checked_at"). The string | null type is ALIGNED.
    // Note: when result != nil (after first check), the full UpdateCheckResult struct is serialized
    // and published_at uses json:"published_at,omitempty" (time.Time) — Go omitempty on time.Time
    // does NOT omit zero value (omitempty only omits Go zero values for primitive types, not structs).
    // published_at will serialize as "0001-01-01T00:00:00Z" when no release date exists, not absent.
    // TODO: API Contract Mismatch - `published_at?: string` is typed as optional (endpoints.ts here)
    // but the backend updater.UpdateCheckResult.PublishedAt has json:"published_at,omitempty"
    // (internal/updater/updater.go:73). Go's omitempty on time.Time does NOT work as expected —
    // a zero time.Time is NOT a Go zero value for struct types, so it will NOT be omitted.
    // When no publish date exists, frontend receives "published_at":"0001-01-01T00:00:00Z" (present
    // but misleading) rather than the field being absent. Callers must check for "0001" prefix.
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
    // TODO: API Contract Mismatch - `started_at: string` is typed as required here, but
    // when the source build has just started and GetActiveBuildStatus() returns nil,
    // the handler (api/handlers/admin.go:737-742) creates an initial UpdateStatus with
    // `{InProgress: true, Stage: "starting", Progress: 0}` — StartedAt is the zero value
    // (time.Time{}), which serializes as "started_at":"0001-01-01T00:00:00Z" rather than
    // the actual start time. Callers must guard: `if (status.started_at && !status.started_at.startsWith('0001'))`.
    // Fix: set `StartedAt: time.Now()` when creating the initial UpdateStatus in the handler,
    // OR change the type to `started_at?: string` and omit when zero.
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
        // TODO: API Contract Mismatch - GetUpdateConfig handler (api/handlers/admin.go:780-793)
        // applies defaults of "source" for update_method and "main" for branch when the config
        // fields are empty. So the actual response always contains valid non-empty strings.
        // However, the handler ONLY defaults when the raw config is empty — if a user saves
        // an empty string via SetUpdateConfig (which validates, so it would not), the field
        // could be empty. The handler defensive default means the type is accurate in practice,
        // but callers should still guard against empty strings defensively.
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

    // TODO: API Contract Mismatch - `updateUser` accepts `Partial<User>` (endpoints.ts:470)
    // which includes ALL User fields (id, username, type, created_at, watch_history, etc.),
    // but the backend AdminUpdateUser handler (api/handlers/admin.go:203-208) only reads
    // four specific fields: role, enabled, email, permissions. All other fields in the
    // Partial<User> payload are silently ignored. Callers may send fields like `type` or
    // `storage_used` expecting them to be updated, but the backend will not apply them.
    // Fix: narrow the parameter type to an explicit update DTO, e.g.:
    //   data: { role?: string; enabled?: boolean; email?: string; permissions?: Partial<UserPermissions> }
    updateUser: (username: string, data: Partial<User>) =>
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
            scan_metadata: false,
        }),

    getReviewQueue: () =>
        api.get<ScanResultItem[]>('/api/admin/scanner/queue'),

    batchReview: (action: 'approve' | 'reject', ids: string[]) =>
        api.post<{ updated: number; total: number }>('/api/admin/scanner/queue', {action, ids}),

    clearReviewQueue: () =>
        api.delete<void>('/api/admin/scanner/queue'),

    // TODO: API Contract Mismatch - `approveContent` is defined here (endpoints.ts) but
    // has NO callers in the frontend codebase. The admin scanner UI (AdminPage.tsx:2821)
    // uses `adminApi.batchReview('approve', [item.id])` (POST /api/admin/scanner/queue)
    // for individual approvals instead of calling this endpoint.
    // Backend route: POST /api/admin/scanner/approve/:id (routes.go:415).
    // The two paths are functionally equivalent but only batchReview is actually wired up.
    // Current state: orphaned endpoint function — never called from web/frontend/src/.
    // Fix: either call approveContent for single-item approvals and batchReview for bulk,
    // OR remove this function since batchReview covers all cases.
    approveContent: (id: string) =>
        api.post<void>(`/api/admin/scanner/approve/${encodeURIComponent(id)}`),

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

    // Validator
    validateMedia: (path: string) =>
        api.post<ValidationResult>('/api/admin/validator/validate', {path}),

    fixMedia: (path: string) =>
        api.post<ValidationResult>('/api/admin/validator/fix', {path}),

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

    // TODO: API Contract Mismatch - `getDailyStats` is defined here (endpoints.ts:697) but
    // has NO callers in the frontend codebase. The backend route GET /api/analytics/daily
    // (routes.go:312) requires adminAuth — only admins can call it. If a frontend component
    // ever calls this outside an admin context, it will receive a 401 Unauthorized.
    // Additionally, if called from a non-admin page (similar to the analyticsApi.getSummary issue
    // in IndexPage.tsx), it would cause a 401 on every page load for authenticated non-admins.
    // Current state: orphaned endpoint function — no callers found in web/frontend/src/.
    // Fix: either wire this into AdminPage.tsx analytics tab, or remove the function to
    // prevent accidental misuse.
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
}
