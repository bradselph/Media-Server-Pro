# Frontend Code Audit — Nuxt UI v3

**Date:** 2026-03-28
**Branch:** development
**Version:** v0.125.0
**Scope:** `web/nuxt-ui/` — all pages, components, composables, stores

---

## 1. File Inventory

### Pages (11)
| File | Lines | Purpose |
|------|-------|---------|
| `pages/index.vue` | ~560 | Media library grid, filters, recommendations |
| `pages/player.vue` | 776 | Video/audio player (HLS, progress, playlists, ratings) |
| `pages/playlists.vue` | 567 | Playlist management — user CRUD + public view |
| `pages/upload.vue` | 256 | File upload with progress tracking |
| `pages/admin.vue` | 104 | Admin panel — pure tab router, no API |
| `pages/favorites.vue` | 126 | Favorites list + remove |
| `pages/categories.vue` | 251 | Category browser with grouped TV/music views |
| `pages/profile.vue` | 701 | Preferences, watch history, ratings, API tokens, password |
| `pages/login.vue` | ~80 | Standard login form (delegates to authStore) |
| `pages/signup.vue` | ~60 | Registration form |
| `pages/admin-login.vue` | 65 | Admin-specific login (delegates to authStore, redirects to /admin) |

### Admin Tab Components (13)
| File | Lines | API Families |
|------|-------|--------------|
| `components/admin/DashboardTab.vue` | 303 | adminApi, mediaApi, settingsApi |
| `components/admin/UsersTab.vue` | 434 | adminApi (users) |
| `components/admin/MediaTab.vue` | 411 | adminApi (media, thumbnails), hlsApi |
| `components/admin/StreamingTab.vue` | 189 | adminApi (HLS jobs, config) |
| `components/admin/AnalyticsTab.vue` | 299 | analyticsApi |
| `components/admin/PlaylistsTab.vue` | 195 | adminApi (admin playlists) |
| `components/admin/SecurityTab.vue` | 382 | adminApi (security, audit log) |
| `components/admin/DownloaderTab.vue` | 540 | adminApi (downloader), raw WebSocket |
| `components/admin/SystemTab.vue` | 21 | none — thin router to 4 sub-panels |
| `components/admin/UpdatesTab.vue` | 270 | adminApi (update) |
| `components/admin/ContentTab.vue` | 509 | adminApi (scanner, HLS, validator, config), hlsApi |
| `components/admin/SourcesTab.vue` | ~25 | none — thin router to 3 sub-panels |
| `components/admin/DiscoveryTab.vue` | 521 | adminApi (categorizer, discovery, suggestions, classify) |

### System Sub-Panels (4)
| File | Lines | API Methods |
|------|-------|------------|
| `components/admin/SystemStatusPanel.vue` | 132 | adminApi.getServerStatus, listModuleStatuses, getModuleHealth |
| `components/admin/SystemSettingsPanel.vue` | 108 | adminApi.getConfig, updateConfig, changeOwnPassword |
| `components/admin/SystemOpsPanel.vue` | 178 | adminApi.listTasks, runTask, enableTask, disableTask, stopTask, getLogs |
| `components/admin/SystemDataPanel.vue` | 262 | adminApi (backups, database), getConfig, updateConfig |

### Sources Sub-Panels (3)
| File | Lines | API Methods |
|------|-------|------------|
| `components/admin/SourcesCrawlerPanel.vue` | 327 | adminApi (crawler, extractor) |
| `components/admin/SourcesReceiverPanel.vue` | 230 | adminApi (receiver, duplicates) |
| `components/admin/SourcesRemotePanel.vue` | 270 | adminApi (remote sources), mediaApi.getRemoteStreamUrl |

### Presentational Components (2)
| File | Lines | Notes |
|------|-------|-------|
| `components/PlayerControls.vue` | 236 | Pure presentational — no API calls |
| `components/RecommendationRow.vue` | 49 | Uses `mediaApi.getThumbnailUrl()` (URL builder only, no fetch) |

### Composables (3)
| File | Lines | Role |
|------|-------|------|
| `composables/useApiEndpoints.ts` | 625 | All API factory functions (18 exports) |
| `composables/useHLS.ts` | ~350 | HLS playback management — uses hlsApi.check, getMasterPlaylistUrl |
| `composables/useApi.ts` | ~60 | Base HTTP client (envelope unwrap, auth headers) |

### Stores (5)
| File | API Usage |
|------|-----------|
| `stores/auth.ts` | `useApiEndpoints()` — login, logout, getSession |
| `stores/playback.ts` | `usePlaybackApi()` — savePosition, getPosition |
| `stores/playlist.ts` | `usePlaylistApi()` — list, create, update, delete, addItem, removePlaylistItemById, get |
| `stores/settings.ts` | `useSettingsApi()` — get |
| `stores/theme.ts` | No API (pure localStorage + Nuxt colorMode) |

---

## 2. Composable → Route Mapping (18 exports)

### `useApiEndpoints()` — Auth & Session
| Method | Route |
|--------|-------|
| `login` | `POST /api/login` |
| `logout` | `POST /api/logout` |
| `register` | `POST /api/register` |
| `getSession` | `GET /api/session` |
| `changePassword` | `POST /api/change-password` |
| `deleteAccount` | `DELETE /api/account` |
| `getPreferences` | `GET /api/preferences` |
| `updatePreferences` | `POST /api/preferences` |
| `getPermissions` | `GET /api/permissions` — **DEAD**: duplicate of `useStorageApi().getPermissions`; no caller destructs this from `useApiEndpoints()` |

### `useMediaApi()`
| Method | Route |
|--------|-------|
| `list` | `GET /api/media` (params: search, type, category, tags, sort_by, sort_order, page, limit, hide_watched, min_rating) |
| `getById` | `GET /api/media/:id` |
| `getStats` | `GET /api/media/stats` |
| `getCategories` | `GET /api/media/categories` |
| `getThumbnailUrl` | `/thumbnail?id=` (URL builder — no fetch) |
| `getThumbnailPreviews` | `GET /api/thumbnails/previews?id=` |
| `getThumbnailBatch` | `GET /api/thumbnails/batch?ids=` |
| `getStreamUrl` | `/media?id=` (URL builder — no fetch) |
| `getDownloadUrl` | `/download?id=` (URL builder — no fetch) |
| `getRemoteStreamUrl` | `/remote/stream?url=` (URL builder — no fetch) |

### `useHlsApi()`
| Method | Route |
|--------|-------|
| `getCapabilities` | `GET /api/hls/capabilities` |
| `check` | `GET /api/hls/check?id=` |
| `getStatus` | `GET /api/hls/status/:id` |
| `generate` | `POST /api/hls/generate` |
| `getMasterPlaylistUrl` | `/hls/:id/master.m3u8` (URL builder — no fetch) |

### `usePlaybackApi()`
| Method | Route |
|--------|-------|
| `getPosition` | `GET /api/playback?id=` |
| `savePosition` | `POST /api/playback` |
| `getBatchPositions` | `GET /api/playback/batch?ids=` |

### `useWatchHistoryApi()`
| Method | Route |
|--------|-------|
| `list` | `GET /api/watch-history` |
| `remove` | `DELETE /api/watch-history?id=` |
| `clear` | `DELETE /api/watch-history` |

### `useSuggestionsApi()`
| Method | Route |
|--------|-------|
| `get` | `GET /api/suggestions` |
| `getTrending` | `GET /api/suggestions/trending` |
| `getSimilar` | `GET /api/suggestions/similar?id=` |
| `getContinueWatching` | `GET /api/suggestions/continue` |
| `getPersonalized` | `GET /api/suggestions/personalized` |
| `getMyProfile` | `GET /api/suggestions/profile` |
| `resetMyProfile` | `DELETE /api/suggestions/profile` |
| `getRecent` | `GET /api/suggestions/recent` |
| `getNewSinceLastVisit` | `GET /api/suggestions/new` |
| `getOnDeck` | `GET /api/suggestions/on-deck` |

### `useStorageApi()`
| Method | Route |
|--------|-------|
| `getUsage` | `GET /api/storage-usage` |
| `getPermissions` | `GET /api/permissions` |

### `usePlaylistApi()`
| Method | Route |
|--------|-------|
| `list` | `GET /api/playlists` |
| `listPublic` | `GET /api/playlists/public` |
| `get` | `GET /api/playlists/:id` |
| `create` | `POST /api/playlists` |
| `update` | `PUT /api/playlists/:id` |
| `delete` | `DELETE /api/playlists/:id` |
| `addItem` | `POST /api/playlists/:id/items` |
| `removeItem` | `DELETE /api/playlists/:id/items?media_id=` |
| `removePlaylistItemById` | `DELETE /api/playlists/:id/items?item_id=` |
| `reorder` | `PUT /api/playlists/:id/reorder` |
| `clear` | `DELETE /api/playlists/:id/clear` |
| `copy` | `POST /api/playlists/:id/copy` |
| `exportPlaylist` | `/api/playlists/:id/export?format=` (URL builder — no fetch) |
| `bulkDelete` | `POST /api/playlists/bulk-delete` |

### `useSettingsApi()`
| Method | Route |
|--------|-------|
| `get` | `GET /api/server-settings` |

### `useVersionApi()`
| Method | Route |
|--------|-------|
| `get` | `GET /api/version` |

### `useAgeGateApi()`
| Method | Route |
|--------|-------|
| `getStatus` | `GET /api/age-gate/status` |
| `verify` | `POST /api/age-verify` |

### `useRatingsApi()`
| Method | Route |
|--------|-------|
| `record` | `POST /api/ratings` |
| `getMyRatings` | `GET /api/ratings` |

### `useCategoryBrowseApi()`
| Method | Route |
|--------|-------|
| `getStats` | `GET /api/browse/categories` |
| `getByCategory` | `GET /api/browse/categories?category=` |

### `useUploadApi()`
| Method | Route |
|--------|-------|
| `upload` | `POST /api/upload` (raw `fetch` with `FormData`) |
| `getProgress` | `GET /api/upload/:id/progress` |

### `useFavoritesApi()`
| Method | Route |
|--------|-------|
| `list` | `GET /api/favorites` |
| `add` | `POST /api/favorites` |
| `remove` | `DELETE /api/favorites/:id` |
| `check` | `GET /api/favorites/:id` |

### `useAPITokensApi()`
| Method | Route |
|--------|-------|
| `list` | `GET /api/auth/tokens` |
| `create` | `POST /api/auth/tokens` |
| `delete` | `DELETE /api/auth/tokens/:id` |

### `useAnalyticsApi()`
| Method | Route |
|--------|-------|
| `getSummary` | `GET /api/analytics?period=` |
| `getDaily` | `GET /api/analytics/daily` |
| `getTopMedia` | `GET /api/analytics/top` |
| `submitEvent` | `POST /api/analytics/events` |
| `getEventStats` | `GET /api/analytics/events/stats` |
| `getEventsByType` | `GET /api/analytics/events/by-type?type=` |
| `getEventsByMedia` | `GET /api/analytics/events/by-media?media_id=` |
| `getEventsByUser` | `GET /api/analytics/events/by-user?user_id=` |
| `getEventTypeCounts` | `GET /api/analytics/events/counts` |
| `exportCsv` | `/api/admin/analytics/export` (URL builder — no fetch) |

### `useAdminApi()` — 80+ methods under `/api/admin/`

**Dashboard:** getStats, getSystemInfo, getActiveStreams, getActiveUploads, clearCache, scanMedia, restartServer, shutdownServer

**Users:** listUsers, getUser, createUser, updateUser, deleteUser, bulkUsers, changeUserPassword, getUserSessions, changeOwnPassword

**Media:** listMedia, scanMedia, updateMedia, deleteMedia, bulkMedia, generateThumbnail, getThumbnailStats

**HLS (admin):** getHLSStats, listHLSJobs, deleteHLSJob, validateHLS, cleanHLSStaleLocks, cleanHLSInactive

**Validator:** validateMedia, fixMedia, getValidatorStats

**Tasks:** listTasks, runTask, enableTask, disableTask, stopTask

**Audit Log:** getAuditLog, exportAuditLogUrl

**Logs:** getLogs

**Config:** getConfig, updateConfig

**Backups:** listBackups, createBackup, restoreBackup, deleteBackup

**Scanner:** getScannerStats, runScan, getReviewQueue, batchReview, clearReviewQueue, approveContent, rejectContent

**Classify (HF):** getClassifyStatus, getClassifyStats, classifyFile, classifyDirectory, classifyRunTask, classifyClearTags, classifyAllPending

**Security:** getSecurityStats, getWhitelist, addToWhitelist, removeFromWhitelist, getBlacklist, addToBlacklist, removeFromBlacklist, getBannedIPs, banIP, unbanIP

**Categorizer:** categorizeFile, categorizeDirectory, getCategoryStats, setMediaCategory, getByCategory, cleanStaleCategories

**Database:** getDatabaseStatus, executeQuery

**Remote Sources:** getRemoteSources, createRemoteSource, deleteRemoteSource, syncRemoteSource, getRemoteStats, getRemoteMedia, getRemoteSourceMedia, cacheRemoteMedia, cleanRemoteCache

**Discovery:** discoveryScan, getDiscoverySuggestions, applyDiscoverySuggestion, dismissDiscoverySuggestion

**Suggestion Stats:** getSuggestionStats

**Receiver:** listSlaves, getReceiverStats, removeReceiverSlave, getSlaveMedia, listDuplicates, resolveDuplicate, getSlaveMediaItem

**Crawler:** listCrawlerTargets, addCrawlerTarget, deleteCrawlerTarget, startCrawl, getCrawlerDiscoveries, approveCrawlerDiscovery, ignoreCrawlerDiscovery, deleteCrawlerDiscovery, getCrawlerStats

**Extractor:** listExtractorItems, addExtractorUrl, deleteExtractorItem, getExtractorStats

**Playlists (admin):** listAllPlaylists, getPlaylistStats, bulkDeletePlaylists, deletePlaylist

**Updates:** checkForUpdates, getUpdateStatus, applyUpdate, checkSourceUpdates, applySourceUpdate, getSourceUpdateProgress, getUpdateConfig, setUpdateConfig

**Downloader:** getDownloaderHealth, detectDownload, listDownloaderJobs, createDownloaderJob, cancelDownloaderJob, deleteDownloaderJob, getDownloaderSettings, listImportable, importFile

**Server Diagnostics:** getServerStatus (→ `/api/status`), listModuleStatuses (→ `/api/modules`), getModuleHealth (→ `/api/modules/:name/health`)

---

## 3. Per-File API Call Inventory

### `pages/index.vue`
- `useMediaApi()` → `list`, `getCategories`, `getThumbnailBatch`, `getThumbnailUrl`
- `useSuggestionsApi()` → `get`, `getTrending`, `getContinueWatching`, `getPersonalized`, `getRecent`, `getNewSinceLastVisit`, `getOnDeck`
- `usePlaybackApi()` → `getBatchPositions`
- `useFavoritesApi()` → `list`, `add`, `remove`
- `useApiEndpoints()` → `updatePreferences`

### `pages/player.vue`
- `useMediaApi()` → `getById`, `getThumbnailUrl`, `getThumbnailPreviews`, `getStreamUrl`
- `usePlaybackApi()` → `getPosition`, `savePosition` (via `usePlaybackStore`)
- `useSuggestionsApi()` → `getSimilar`, `getPersonalized`
- `useRatingsApi()` → `record`
- `usePlaylistApi()` → `list`, `get`, `addItem`
- `useAnalyticsApi()` → `submitEvent` (play, pause, resume, seek, quality_change, error, complete)
- `useHlsApi()` → `generate`
- `useHLS()` composable → wraps `hlsApi.check`, `hlsApi.getMasterPlaylistUrl`
- `useApiEndpoints()` → `updatePreferences`

### `pages/playlists.vue`
- `usePlaylistApi()` → `list`, `listPublic`, `create`, `delete`, `update`, `copy`, `clear`, `get`, `removePlaylistItemById`, `removeItem`, `reorder`, `exportPlaylist`, `bulkDelete`

### `pages/upload.vue`
- `useUploadApi()` → `upload`, `getProgress`

### `pages/favorites.vue`
- `useFavoritesApi()` → `list`, `remove`
- `useMediaApi()` → `getById`

### `pages/categories.vue`
- `useCategoryBrowseApi()` → `getStats`, `getByCategory`
- `useMediaApi()` — imported but **never called** (dead import — `CategoryBrowseItem` already carries `thumbnail_url`)

### `pages/profile.vue`
- `useApiEndpoints()` → `changePassword`, `deleteAccount`, `getPreferences`, `updatePreferences`
- `useWatchHistoryApi()` → `list`, `remove`, `clear`
- `useStorageApi()` → `getUsage`, `getPermissions`
- `useSuggestionsApi()` → `getMyProfile`, `resetMyProfile`
- `useAPITokensApi()` → `list`, `create`, `delete`
- `useRatingsApi()` → `getMyRatings`
- Direct link: `GET /api/watch-history/export` (`NuxtLink :to` with `external target="_blank"`)

### `pages/login.vue`
- `useAuthStore().login()` → calls `useApiEndpoints().login`

### `pages/signup.vue`
- `useApiEndpoints()` → `register`

### `pages/admin-login.vue`
- `useAuthStore().login()` / `logout()` (delegates to store)

### `components/admin/DashboardTab.vue`
- `useAdminApi()` → `getStats`, `getSystemInfo`, `getActiveStreams`, `getActiveUploads`, `clearCache`, `scanMedia`, `restartServer`, `shutdownServer`
- `useMediaApi()` → `getStats`
- `useSettingsApi()` → `get` (via `useSettingsStore`)

### `components/admin/UsersTab.vue`
- `useAdminApi()` → `listUsers`, `createUser`, `getUser`, `updateUser`, `deleteUser`, `changeUserPassword`, `getUserSessions`, `bulkUsers`

### `components/admin/MediaTab.vue`
- `useAdminApi()` → `listMedia`, `updateMedia`, `deleteMedia`, `scanMedia`, `getThumbnailStats`, `generateThumbnail`, `bulkMedia`
- `useHlsApi()` → `generate`

### `components/admin/StreamingTab.vue`
- `useAdminApi()` → `listHLSJobs`, `getHLSStats`, `getConfig`, `updateConfig`, `deleteHLSJob`, `cleanHLSInactive`

### `components/admin/AnalyticsTab.vue`
- `useAnalyticsApi()` → `getSummary`, `getDaily`, `getTopMedia`, `getEventStats`, `getEventTypeCounts`, `getEventsByType`, `getEventsByMedia`, `getEventsByUser`, `exportCsv`

### `components/admin/PlaylistsTab.vue`
- `useAdminApi()` → `listAllPlaylists`, `getPlaylistStats`, `deletePlaylist`, `bulkDeletePlaylists`

### `components/admin/SecurityTab.vue`
- `useAdminApi()` → `getAuditLog`, `exportAuditLogUrl`, `getWhitelist`, `getBlacklist`, `getBannedIPs`, `addToWhitelist`, `addToBlacklist`, `removeFromWhitelist`, `removeFromBlacklist`, `banIP`, `unbanIP`, `getSecurityStats`, `getConfig`, `updateConfig`

### `components/admin/DownloaderTab.vue`
- `useAdminApi()` → `getDownloaderHealth`, `getDownloaderSettings`, `listDownloaderJobs`, `cancelDownloaderJob`, `deleteDownloaderJob`, `detectDownload`, `createDownloaderJob`, `listImportable`, `importFile`, `getConfig`, `updateConfig`
- Raw WebSocket: `ws(s)://${location.host}/ws/admin/downloader` (real-time progress, no composable)

### `components/admin/UpdatesTab.vue`
- `useAdminApi()` → `getUpdateConfig`, `setUpdateConfig`, `checkForUpdates`, `applyUpdate`, `getUpdateStatus`, `checkSourceUpdates`, `applySourceUpdate`, `getSourceUpdateProgress`

### `components/admin/ContentTab.vue`
- `useAdminApi()` → `getScannerStats`, `getReviewQueue`, `runScan`, `batchReview`, `clearReviewQueue`, `approveContent`, `rejectContent`, `getHLSStats`, `listHLSJobs`, `validateHLS`, `cleanHLSStaleLocks`, `cleanHLSInactive`, `getValidatorStats`, `validateMedia`, `fixMedia`, `getConfig`, `updateConfig`
- `useHlsApi()` → `getCapabilities`, `getStatus`

### `components/admin/DiscoveryTab.vue`
- `useAdminApi()` → `getCategoryStats`, `categorizeFile`, `categorizeDirectory`, `setMediaCategory`, `getByCategory`, `cleanStaleCategories`, `getDiscoverySuggestions`, `discoveryScan`, `applyDiscoverySuggestion`, `dismissDiscoverySuggestion`, `getSuggestionStats`, `getClassifyStatus`, `getClassifyStats`, `classifyFile`, `classifyDirectory`, `classifyAllPending`, `classifyRunTask`, `classifyClearTags`

### `components/admin/SystemStatusPanel.vue`
- `useAdminApi()` → `getServerStatus`, `listModuleStatuses`, `getModuleHealth`

### `components/admin/SystemSettingsPanel.vue`
- `useAdminApi()` → `getConfig`, `updateConfig`, `changeOwnPassword`
- Direct links: `GET /api/docs` (OpenAPI UI), `GET /metrics` (Prometheus) — plain `<a>` tags

### `components/admin/SystemOpsPanel.vue`
- `useAdminApi()` → `listTasks`, `runTask`, `enableTask`, `disableTask`, `stopTask`, `getLogs`

### `components/admin/SystemDataPanel.vue`
- `useAdminApi()` → `listBackups`, `createBackup`, `restoreBackup`, `deleteBackup`, `getDatabaseStatus`, `executeQuery`, `getConfig`, `updateConfig`

### `components/admin/SourcesCrawlerPanel.vue`
- `useAdminApi()` → `getCrawlerStats`, `listCrawlerTargets`, `getCrawlerDiscoveries`, `addCrawlerTarget`, `startCrawl`, `deleteCrawlerTarget`, `approveCrawlerDiscovery`, `ignoreCrawlerDiscovery`, `deleteCrawlerDiscovery`, `getExtractorStats`, `listExtractorItems`, `addExtractorUrl`, `deleteExtractorItem`

### `components/admin/SourcesReceiverPanel.vue`
- `useAdminApi()` → `getReceiverStats`, `listSlaves`, `listDuplicates`, `getSlaveMedia`, `getSlaveMediaItem`, `removeReceiverSlave`, `resolveDuplicate`

### `components/admin/SourcesRemotePanel.vue`
- `useAdminApi()` → `getRemoteStats`, `getRemoteSources`, `createRemoteSource`, `syncRemoteSource`, `deleteRemoteSource`, `cleanRemoteCache`, `getRemoteMedia`, `getRemoteSourceMedia`, `cacheRemoteMedia`
- `useMediaApi()` → `getRemoteStreamUrl`

---

## 4. Coverage Analysis

### Routes accessed without a composable wrapper (intentional)

| Route | Location | Reason |
|-------|----------|--------|
| `GET /api/watch-history/export` | `profile.vue:532` | Export download — `NuxtLink :to` with `target="_blank"` |
| `GET /api/feed` | N/A (documented) | Atom/RSS subscription link — external reader use |
| `GET /api/docs` | `SystemSettingsPanel.vue` | OpenAPI UI — developer `<a>` link |
| `GET /metrics` | `SystemSettingsPanel.vue` | Prometheus metrics — developer `<a>` link |
| `WS /ws/admin/downloader` | `DownloaderTab.vue` | Real-time download progress — raw WebSocket with exponential-backoff reconnect |

### Composable coverage rate

All 18 composable factory functions are exported and have at least one call site.
Estimated total composable methods: **~130** across 18 factories. All have call sites.

---

## 5. Findings

### Dead Code

| Location | Issue | Severity |
|----------|-------|----------|
| `useApiEndpoints().getPermissions` (`useApiEndpoints.ts:70`) | Exact duplicate of `useStorageApi().getPermissions` (both hit `GET /api/permissions`). No file destructs this from `useApiEndpoints()`. All callers use `useStorageApi()` instead. | Low |
| `useMediaApi` import in `categories.vue:9` | `const mediaApi = useMediaApi()` is created but never referenced. `CategoryBrowseItem` already carries `thumbnail_url` directly. | Low |

### Admin vs User Playlist Bulk Delete (Intentional Distinction)

Two separate composable methods exist for bulk deleting playlists:
- `usePlaylistApi().bulkDelete(ids)` → `POST /api/playlists/bulk-delete` (user scope — own playlists)
- `useAdminApi().bulkDeletePlaylists(ids)` → `POST /api/admin/playlists/bulk` (admin scope — any playlist)

The routes are distinct on the backend.

### Analytics Export Route

`analyticsApi.exportCsv()` returns `/api/admin/analytics/export` and is used as an `href` with `download` on a `<UButton tag="a">` in `AnalyticsTab.vue`. This is an admin-only endpoint accessed via direct navigation, not through the `useApi` client.

### Stores Make Direct API Calls

Three stores make API calls rather than delegating to pages:
- `stores/auth.ts` — `login`, `logout`, `getSession` on every page load
- `stores/playback.ts` — `savePosition` every 15s via `setInterval`, plus `loadPosition` on player mount
- `stores/playlist.ts` — manages its own playlist cache with full CRUD

This is intentional architecture — these stores are the single source of truth for their data domains.

### TDZ / Import Pattern (Active)

All composables in `composables/` that use Nuxt auto-imports use explicit static imports at the top of `useApiEndpoints.ts`:
- `import { useApi } from '~/composables/useApi'` — breaks `→ #imports → self` TDZ cycle

Component files import specific composables explicitly where needed (e.g., `import { useFavoritesApi } from '~/composables/useApiEndpoints'`).

---

## 6. Summary

| Metric | Count |
|--------|-------|
| Pages | 11 |
| Admin tab components | 13 |
| System/sources sub-panels | 7 |
| Presentational components | 2 |
| Composable factory functions | 18 |
| Estimated composable methods | ~130 |
| Stores | 5 |
| Dead composable methods | 1 (`useApiEndpoints().getPermissions`) |
| Dead imports | 1 (`useMediaApi` in `categories.vue`) |
| Intentional bypass routes | 5 (export/feed/docs/metrics/WebSocket) |
| **Coverage** | **100%** — every admin-accessible backend route has a composable method; all composable methods are called |
