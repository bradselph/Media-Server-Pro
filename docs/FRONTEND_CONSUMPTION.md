# Frontend API Consumption Map

Audit date: 2026-04-09
Source: `web/nuxt-ui/composables/useApiEndpoints.ts` (single source of truth for all API calls)
All API calls use the `useApi()` client which unwraps `{ success, data }` envelopes.

---

## Auth (`useApiEndpoints()`)

| Function              | Method | Endpoint                          | Parameters                                                          | Response Type                       |
|-----------------------|--------|-----------------------------------|---------------------------------------------------------------------|-------------------------------------|
| `login`               | POST   | `/api/auth/login`                 | body: `{ username, password }`                                      | `LoginResponse` (normalized)        |
| `logout`              | POST   | `/api/auth/logout`                | none                                                                | `void`                              |
| `register`            | POST   | `/api/auth/register`              | body: `{ username, password, email? }`                              | `User`                              |
| `getSession`          | GET    | `/api/auth/session`               | none                                                                | `SessionCheckResponse` (normalized) |
| `changePassword`      | POST   | `/api/auth/change-password`       | body: `{ current_password, new_password }`                          | `void`                              |
| `requestDataDeletion` | POST   | `/api/auth/data-deletion-request` | body: `{ reason }`                                                  | `{ status, message, id }`           |
| `getPreferences`      | GET    | `/api/preferences`                | none                                                                | `UserPreferences` (normalized)      |
| `updatePreferences`   | POST   | `/api/preferences`                | body: partial `UserPreferences` (filtered via `toPreferencesPatch`) | `UserPreferences` (normalized)      |

## Media (`useMediaApi()`)

| Function               | Method | Endpoint                      | Parameters                                                                                                                                | Response Type                            |
|------------------------|--------|-------------------------------|-------------------------------------------------------------------------------------------------------------------------------------------|------------------------------------------|
| `list`                 | GET    | `/api/media`                  | query: `sort`, `limit`, `offset`, `sort_order`, `type`, `category`, `search`, `tags`, `hide_watched`, `min_rating`, `mature`, `is_mature` | `MediaListResponse`                      |
| `getById`              | GET    | `/api/media/:id`              | path: `id`                                                                                                                                | `MediaItem`                              |
| `getBatch`             | GET    | `/api/media/batch`            | query: `ids` (comma-joined)                                                                                                               | `{ items: Record<string, MediaItem> }`   |
| `getStats`             | GET    | `/api/media/stats`            | none                                                                                                                                      | `MediaStats`                             |
| `getCategories`        | GET    | `/api/media/categories`       | none                                                                                                                                      | `MediaCategory[]`                        |
| `getThumbnailPreviews` | GET    | `/api/thumbnails/previews`    | query: `id`                                                                                                                               | `ThumbnailPreviews`                      |
| `getThumbnailBatch`    | GET    | `/api/thumbnails/batch`       | query: `ids` (comma-joined), `w?`                                                                                                         | `{ thumbnails: Record<string, string> }` |
| `getThumbnailUrl`      | --     | `/thumbnail?id=`              | URL builder only (not a fetch)                                                                                                            | string URL                               |
| `getStreamUrl`         | --     | `/media?id=`                  | URL builder only                                                                                                                          | string URL                               |
| `getDownloadUrl`       | --     | `/download?id=`               | URL builder only                                                                                                                          | string URL                               |
| `getRemoteStreamUrl`   | --     | `/remote/stream?url=&source=` | URL builder only                                                                                                                          | string URL                               |

## HLS (`useHlsApi()`)

| Function               | Method | Endpoint                | Parameters               | Response Type     |
|------------------------|--------|-------------------------|--------------------------|-------------------|
| `getCapabilities`      | GET    | `/api/hls/capabilities` | none                     | `HLSCapabilities` |
| `check`                | GET    | `/api/hls/check`        | query: `id`              | `HLSAvailability` |
| `getStatus`            | GET    | `/api/hls/status/:id`   | path: `id`               | `HLSJob`          |
| `generate`             | POST   | `/api/hls/generate`     | body: `{ id, quality? }` | `HLSJob`          |
| `getMasterPlaylistUrl` | --     | `/hls/:id/master.m3u8`  | URL builder only         | string URL        |

## Playback (`usePlaybackApi()`)

| Function            | Method | Endpoint              | Parameters                         | Response Type                           |
|---------------------|--------|-----------------------|------------------------------------|-----------------------------------------|
| `getPosition`       | GET    | `/api/playback`       | query: `id`                        | `{ position: number }`                  |
| `savePosition`      | POST   | `/api/playback`       | body: `{ id, position, duration }` | `void`                                  |
| `getBatchPositions` | GET    | `/api/playback/batch` | query: `ids` (comma-joined)        | `{ positions: Record<string, number> }` |

## Watch History (`useWatchHistoryApi()`)

| Function | Method | Endpoint             | Parameters                    | Response Type        |
|----------|--------|----------------------|-------------------------------|----------------------|
| `list`   | GET    | `/api/watch-history` | query: `limit?`, `completed?` | `WatchHistoryItem[]` |
| `remove` | DELETE | `/api/watch-history` | query: `id`                   | `void`               |
| `clear`  | DELETE | `/api/watch-history` | none                          | `void`               |

## Suggestions (`useSuggestionsApi()`)

| Function               | Method | Endpoint                        | Parameters               | Response Type      |
|------------------------|--------|---------------------------------|--------------------------|--------------------|
| `get`                  | GET    | `/api/suggestions`              | none                     | `Suggestion[]`     |
| `getTrending`          | GET    | `/api/suggestions/trending`     | query: `limit?`          | `Suggestion[]`     |
| `getSimilar`           | GET    | `/api/suggestions/similar`      | query: `id`              | `Suggestion[]`     |
| `getContinueWatching`  | GET    | `/api/suggestions/continue`     | query: `limit?`          | `Suggestion[]`     |
| `getPersonalized`      | GET    | `/api/suggestions/personalized` | query: `limit?`          | `Suggestion[]`     |
| `getMyProfile`         | GET    | `/api/suggestions/profile`      | none                     | `UserProfile`      |
| `resetMyProfile`       | DELETE | `/api/suggestions/profile`      | none                     | `void`             |
| `getRecent`            | GET    | `/api/suggestions/recent`       | query: `days?`, `limit?` | `RecentItem[]`     |
| `getNewSinceLastVisit` | GET    | `/api/suggestions/new`          | query: `limit?`          | `NewSinceResponse` |
| `getOnDeck`            | GET    | `/api/suggestions/on-deck`      | query: `limit?`          | `OnDeckResponse`   |

## Storage & Permissions (`useStorageApi()`)

| Function         | Method | Endpoint             | Parameters | Response Type     |
|------------------|--------|----------------------|------------|-------------------|
| `getUsage`       | GET    | `/api/storage-usage` | none       | `StorageUsage`    |
| `getPermissions` | GET    | `/api/permissions`   | none       | `PermissionsInfo` |

## Playlists (`usePlaylistApi()`)

| Function                 | Method | Endpoint                            | Parameters                                 | Response Type         |
|--------------------------|--------|-------------------------------------|--------------------------------------------|-----------------------|
| `list`                   | GET    | `/api/playlists`                    | none                                       | `Playlist[]`          |
| `listPublic`             | GET    | `/api/playlists/public`             | none                                       | `Playlist[]`          |
| `get`                    | GET    | `/api/playlists/:id`                | path: `id`                                 | `Playlist`            |
| `create`                 | POST   | `/api/playlists`                    | body: `{ name, description?, is_public? }` | `Playlist`            |
| `update`                 | PUT    | `/api/playlists/:id`                | path: `id`, body: `Partial<Playlist>`      | `Playlist`            |
| `delete`                 | DELETE | `/api/playlists/:id`                | path: `id`                                 | `void`                |
| `addItem`                | POST   | `/api/playlists/:id/items`          | path: `id`, body: `{ media_id }`           | `PlaylistItem`        |
| `removeItem`             | DELETE | `/api/playlists/:id/items`          | path: `id`, query: `media_id`              | `void`                |
| `removePlaylistItemById` | DELETE | `/api/playlists/:id/items`          | path: `id`, query: `item_id`               | `void`                |
| `reorder`                | PUT    | `/api/playlists/:id/reorder`        | path: `id`, body: `{ positions }`          | `void`                |
| `clear`                  | DELETE | `/api/playlists/:id/clear`          | path: `id`                                 | `void`                |
| `copy`                   | POST   | `/api/playlists/:id/copy`           | path: `id`, body: `{ name }`               | `Playlist`            |
| `exportPlaylist`         | --     | `/api/playlists/:id/export?format=` | URL builder only                           | string URL            |
| `bulkDelete`             | POST   | `/api/playlists/bulk-delete`        | body: `{ ids }`                            | `{ deleted, failed }` |

## Settings (`useSettingsApi()`)

| Function | Method | Endpoint               | Parameters | Response Type    |
|----------|--------|------------------------|------------|------------------|
| `get`    | GET    | `/api/server-settings` | none       | `ServerSettings` |

## Version (`useVersionApi()`)

| Function | Method | Endpoint       | Parameters | Response Type         |
|----------|--------|----------------|------------|-----------------------|
| `get`    | GET    | `/api/version` | none       | `{ version: string }` |

## Age Gate (`useAgeGateApi()`)

| Function    | Method | Endpoint               | Parameters | Response Type   |
|-------------|--------|------------------------|------------|-----------------|
| `getStatus` | GET    | `/api/age-gate/status` | none       | `AgeGateStatus` |
| `verify`    | POST   | `/api/age-verify`      | none       | `void`          |

## Ratings (`useRatingsApi()`)

| Function       | Method | Endpoint       | Parameters             | Response Type |
|----------------|--------|----------------|------------------------|---------------|
| `record`       | POST   | `/api/ratings` | body: `{ id, rating }` | `void`        |
| `getMyRatings` | GET    | `/api/ratings` | none                   | `RatedItem[]` |

## Category Browse (`useCategoryBrowseApi()`)

| Function        | Method | Endpoint                 | Parameters                  | Response Type            |
|-----------------|--------|--------------------------|-----------------------------|--------------------------|
| `getStats`      | GET    | `/api/browse/categories` | none                        | `CategoryStats`          |
| `getByCategory` | GET    | `/api/browse/categories` | query: `category`, `limit?` | `CategoryBrowseResponse` |

## Upload (`useUploadApi()`)

| Function      | Method | Endpoint                   | Parameters                             | Response Type    |
|---------------|--------|----------------------------|----------------------------------------|------------------|
| `upload`      | POST   | `/api/upload`              | FormData: `files` (multi), `category?` | `UploadResult`   |
| `getProgress` | GET    | `/api/upload/:id/progress` | path: `id`                             | `UploadProgress` |

## Favorites (`useFavoritesApi()`)

| Function | Method | Endpoint                   | Parameters           | Response Type              |
|----------|--------|----------------------------|----------------------|----------------------------|
| `list`   | GET    | `/api/favorites`           | none                 | `FavoriteItem[]`           |
| `add`    | POST   | `/api/favorites`           | body: `{ media_id }` | `void`                     |
| `remove` | DELETE | `/api/favorites/:media_id` | path: `media_id`     | `void`                     |
| `check`  | GET    | `/api/favorites/:media_id` | path: `media_id`     | `{ is_favorite: boolean }` |

## API Tokens (`useAPITokensApi()`)

| Function | Method | Endpoint               | Parameters       | Response Type     |
|----------|--------|------------------------|------------------|-------------------|
| `list`   | GET    | `/api/auth/tokens`     | none             | `APIToken[]`      |
| `create` | POST   | `/api/auth/tokens`     | body: `{ name }` | `APITokenCreated` |
| `delete` | DELETE | `/api/auth/tokens/:id` | path: `id`       | `void`            |

## Analytics (`useAnalyticsApi()`)

| Function                | Method | Endpoint                         | Parameters                                         | Response Type              |
|-------------------------|--------|----------------------------------|----------------------------------------------------|----------------------------|
| `getSummary`            | GET    | `/api/analytics`                 | query: `period?`                                   | `AnalyticsSummary`         |
| `getDaily`              | GET    | `/api/analytics/daily`           | query: `days?`                                     | `DailyStats[]`             |
| `getTopMedia`           | GET    | `/api/analytics/top`             | query: `limit?`                                    | `TopMediaItem[]`           |
| `submitEvent`           | POST   | `/api/analytics/events`          | body: `{ type, media_id, duration?, data? }`       | `{ status: string }`       |
| `getEventStats`         | GET    | `/api/analytics/events/stats`    | none                                               | `EventStats`               |
| `getEventsByType`       | GET    | `/api/analytics/events/by-type`  | query: `type`, `limit?`                            | `AnalyticsEvent[]`         |
| `getEventsByMedia`      | GET    | `/api/analytics/events/by-media` | query: `media_id`, `limit?`                        | `AnalyticsEvent[]`         |
| `getEventsByUser`       | GET    | `/api/analytics/events/by-user`  | query: `user_id`, `limit?`                         | `AnalyticsEvent[]`         |
| `getEventTypeCounts`    | GET    | `/api/analytics/events/counts`   | none                                               | `EventTypeCounts`          |
| `getContentPerformance` | GET    | `/api/analytics/content`         | query: `limit?`                                    | `ContentPerformanceItem[]` |
| `exportCsv`             | --     | `/api/admin/analytics/export`    | URL builder with query: `start_date?`, `end_date?` | string URL                 |

## Admin (`useAdminApi()`)

### Dashboard & System

| Function           | Method | Endpoint                    | Parameters | Response Type      |
|--------------------|--------|-----------------------------|------------|--------------------|
| `getStats`         | GET    | `/api/admin/stats`          | none       | `AdminStats`       |
| `getSystemInfo`    | GET    | `/api/admin/system`         | none       | `SystemInfo`       |
| `getActiveStreams` | GET    | `/api/admin/streams`        | none       | `StreamSession[]`  |
| `getActiveUploads` | GET    | `/api/admin/uploads/active` | none       | `UploadProgress[]` |

### Controls

| Function         | Method | Endpoint                     | Parameters | Response Type |
|------------------|--------|------------------------------|------------|---------------|
| `clearCache`     | POST   | `/api/admin/cache/clear`     | none       | `void`        |
| `restartServer`  | POST   | `/api/admin/server/restart`  | none       | `void`        |
| `shutdownServer` | POST   | `/api/admin/server/shutdown` | none       | `void`        |

### Users

| Function             | Method | Endpoint                              | Parameters                                   | Response Type                 |
|----------------------|--------|---------------------------------------|----------------------------------------------|-------------------------------|
| `listUsers`          | GET    | `/api/admin/users`                    | none                                         | `User[]`                      |
| `getUser`            | GET    | `/api/admin/users/:username`          | path: `username`                             | `User`                        |
| `createUser`         | POST   | `/api/admin/users`                    | body: `{ username, password, email?, role }` | `User`                        |
| `updateUser`         | PUT    | `/api/admin/users/:username`          | path: `username`, body: `Partial<User>`      | `User`                        |
| `deleteUser`         | DELETE | `/api/admin/users/:username`          | path: `username`                             | `void`                        |
| `bulkUsers`          | POST   | `/api/admin/users/bulk`               | body: `{ usernames, action }`                | `{ success, failed, errors }` |
| `changeUserPassword` | POST   | `/api/admin/users/:username/password` | path: `username`, body: `{ new_password }`   | `void`                        |
| `getUserSessions`    | GET    | `/api/admin/users/:username/sessions` | path: `username`                             | `UserSession[]`               |
| `changeOwnPassword`  | POST   | `/api/admin/change-password`          | body: `{ current_password, new_password }`   | `void`                        |

### Media

| Function            | Method | Endpoint                         | Parameters                                                                                               | Response Type                 |
|---------------------|--------|----------------------------------|----------------------------------------------------------------------------------------------------------|-------------------------------|
| `listMedia`         | GET    | `/api/admin/media`               | query: `page?`, `limit?`, `sort?`, `sort_order?`, `type?`, `category?`, `search?`, `tags?`, `is_mature?` | `AdminMediaListResponse`      |
| `scanMedia`         | POST   | `/api/admin/media/scan`          | none                                                                                                     | `void`                        |
| `updateMedia`       | PUT    | `/api/admin/media/:id`           | path: `id`, body: `Partial<MediaItem>`                                                                   | `MediaItem`                   |
| `deleteMedia`       | DELETE | `/api/admin/media/:id`           | path: `id`                                                                                               | `void`                        |
| `bulkMedia`         | POST   | `/api/admin/media/bulk`          | body: `{ ids, action, data? }`                                                                           | `{ success, failed, errors }` |
| `generateThumbnail` | POST   | `/api/admin/thumbnails/generate` | body: `{ id, is_audio }`                                                                                 | `void`                        |
| `getThumbnailStats` | GET    | `/api/admin/thumbnails/stats`    | none                                                                                                     | `ThumbnailStats`              |

### HLS

| Function             | Method | Endpoint                        | Parameters | Response Type         |
|----------------------|--------|---------------------------------|------------|-----------------------|
| `getHLSStats`        | GET    | `/api/admin/hls/stats`          | none       | `HLSStats`            |
| `listHLSJobs`        | GET    | `/api/admin/hls/jobs`           | none       | `HLSJob[]`            |
| `deleteHLSJob`       | DELETE | `/api/admin/hls/jobs/:id`       | path: `id` | `void`                |
| `validateHLS`        | GET    | `/api/admin/hls/validate/:id`   | path: `id` | `HLSValidationResult` |
| `cleanHLSStaleLocks` | POST   | `/api/admin/hls/clean/locks`    | none       | `void`                |
| `cleanHLSInactive`   | POST   | `/api/admin/hls/clean/inactive` | none       | `void`                |

### Validator

| Function            | Method | Endpoint                        | Parameters     | Response Type      |
|---------------------|--------|---------------------------------|----------------|--------------------|
| `validateMedia`     | POST   | `/api/admin/validator/validate` | body: `{ id }` | `ValidationResult` |
| `fixMedia`          | POST   | `/api/admin/validator/fix`      | body: `{ id }` | `ValidationResult` |
| `getValidatorStats` | GET    | `/api/admin/validator/stats`    | none           | `ValidatorStats`   |

### Tasks

| Function      | Method | Endpoint                       | Parameters | Response Type     |
|---------------|--------|--------------------------------|------------|-------------------|
| `listTasks`   | GET    | `/api/admin/tasks`             | none       | `ScheduledTask[]` |
| `runTask`     | POST   | `/api/admin/tasks/:id/run`     | path: `id` | `void`            |
| `enableTask`  | POST   | `/api/admin/tasks/:id/enable`  | path: `id` | `void`            |
| `disableTask` | POST   | `/api/admin/tasks/:id/disable` | path: `id` | `void`            |
| `stopTask`    | POST   | `/api/admin/tasks/:id/stop`    | path: `id` | `void`            |

### Audit Log

| Function            | Method | Endpoint                      | Parameters                             | Response Type     |
|---------------------|--------|-------------------------------|----------------------------------------|-------------------|
| `getAuditLog`       | GET    | `/api/admin/audit-log`        | query: `offset?`, `limit?`, `user_id?` | `AuditLogEntry[]` |
| `exportAuditLogUrl` | --     | `/api/admin/audit-log/export` | URL builder only                       | string URL        |

### Logs

| Function  | Method | Endpoint          | Parameters                                        | Response Type |
|-----------|--------|-------------------|---------------------------------------------------|---------------|
| `getLogs` | GET    | `/api/admin/logs` | query: `level?`, `module?`, `limit` (default 200) | `LogEntry[]`  |

### Config

| Function       | Method | Endpoint            | Parameters                      | Response Type             |
|----------------|--------|---------------------|---------------------------------|---------------------------|
| `getConfig`    | GET    | `/api/admin/config` | none                            | `Record<string, unknown>` |
| `updateConfig` | PUT    | `/api/admin/config` | body: `Record<string, unknown>` | `void`                    |

### Backups

| Function        | Method | Endpoint                            | Parameters                             | Response Type   |
|-----------------|--------|-------------------------------------|----------------------------------------|-----------------|
| `listBackups`   | GET    | `/api/admin/backups/v2`             | none                                   | `BackupEntry[]` |
| `createBackup`  | POST   | `/api/admin/backups/v2`             | body: `{ description?, backup_type? }` | `BackupEntry`   |
| `restoreBackup` | POST   | `/api/admin/backups/v2/:id/restore` | path: `id`                             | `void`          |
| `deleteBackup`  | DELETE | `/api/admin/backups/v2/:id`         | path: `id`                             | `void`          |

### Scanner / Content Review

| Function           | Method | Endpoint                         | Parameters              | Response Type        |
|--------------------|--------|----------------------------------|-------------------------|----------------------|
| `getScannerStats`  | GET    | `/api/admin/scanner/stats`       | none                    | `ScannerStats`       |
| `runScan`          | POST   | `/api/admin/scanner/scan`        | body: `{ path? }`       | `void`               |
| `getReviewQueue`   | GET    | `/api/admin/scanner/queue`       | none                    | `ReviewQueueItem[]`  |
| `batchReview`      | POST   | `/api/admin/scanner/queue`       | body: `{ action, ids }` | `{ updated, total }` |
| `clearReviewQueue` | DELETE | `/api/admin/scanner/queue`       | none                    | `void`               |
| `approveContent`   | POST   | `/api/admin/scanner/approve/:id` | path: `id`              | `void`               |
| `rejectContent`    | POST   | `/api/admin/scanner/reject/:id`  | path: `id`              | `void`               |

### Classify (HuggingFace)

| Function             | Method | Endpoint                          | Parameters       | Response Type            |
|----------------------|--------|-----------------------------------|------------------|--------------------------|
| `getClassifyStatus`  | GET    | `/api/admin/classify/status`      | none             | `ClassifyStatus`         |
| `getClassifyStats`   | GET    | `/api/admin/classify/stats`       | none             | `ClassifyStats`          |
| `classifyFile`       | POST   | `/api/admin/classify/file`        | body: `{ path }` | `{ path, tags }`         |
| `classifyDirectory`  | POST   | `/api/admin/classify/directory`   | body: `{ path }` | `{ message, directory }` |
| `classifyRunTask`    | POST   | `/api/admin/classify/run-task`    | none             | `{ message }`            |
| `classifyClearTags`  | POST   | `/api/admin/classify/clear-tags`  | body: `{ id }`   | `{ message, id }`        |
| `classifyAllPending` | POST   | `/api/admin/classify/all-pending` | none             | `{ message, count }`     |

### Security

| Function              | Method | Endpoint                        | Parameters                            | Response Type   |
|-----------------------|--------|---------------------------------|---------------------------------------|-----------------|
| `getSecurityStats`    | GET    | `/api/admin/security/stats`     | none                                  | `SecurityStats` |
| `getWhitelist`        | GET    | `/api/admin/security/whitelist` | none                                  | `IPListEntry[]` |
| `addToWhitelist`      | POST   | `/api/admin/security/whitelist` | body: `{ ip, comment? }`              | `void`          |
| `removeFromWhitelist` | DELETE | `/api/admin/security/whitelist` | body: `{ ip }`                        | `void`          |
| `getBlacklist`        | GET    | `/api/admin/security/blacklist` | none                                  | `IPListEntry[]` |
| `addToBlacklist`      | POST   | `/api/admin/security/blacklist` | body: `{ ip, comment?, expires_at? }` | `void`          |
| `removeFromBlacklist` | DELETE | `/api/admin/security/blacklist` | body: `{ ip }`                        | `void`          |
| `getBannedIPs`        | GET    | `/api/admin/security/banned`    | none                                  | `BannedIP[]`    |
| `banIP`               | POST   | `/api/admin/security/ban`       | body: `{ ip, duration_minutes? }`     | `void`          |
| `unbanIP`             | POST   | `/api/admin/security/unban`     | body: `{ ip }`                        | `void`          |

### Categorizer

| Function               | Method | Endpoint                             | Parameters                 | Response Type       |
|------------------------|--------|--------------------------------------|----------------------------|---------------------|
| `categorizeFile`       | POST   | `/api/admin/categorizer/file`        | body: `{ path }`           | `CategorizedItem`   |
| `categorizeDirectory`  | POST   | `/api/admin/categorizer/directory`   | body: `{ directory }`      | `CategorizedItem[]` |
| `getCategoryStats`     | GET    | `/api/admin/categorizer/stats`       | none                       | `CategoryStats`     |
| `setMediaCategory`     | POST   | `/api/admin/categorizer/set`         | body: `{ path, category }` | `{ message }`       |
| `getByCategory`        | GET    | `/api/admin/categorizer/by-category` | query: `category`          | `CategorizedItem[]` |
| `cleanStaleCategories` | POST   | `/api/admin/categorizer/clean`       | none                       | `{ removed }`       |

### Database

| Function            | Method | Endpoint                     | Parameters        | Response Type    |
|---------------------|--------|------------------------------|-------------------|------------------|
| `getDatabaseStatus` | GET    | `/api/admin/database/status` | none              | `DatabaseStatus` |
| `executeQuery`      | POST   | `/api/admin/database/query`  | body: `{ query }` | `QueryResult`    |

### Remote Sources

| Function               | Method | Endpoint                                  | Parameters                                           | Response Type          |
|------------------------|--------|-------------------------------------------|------------------------------------------------------|------------------------|
| `getRemoteSources`     | GET    | `/api/admin/remote/sources`               | none                                                 | `RemoteSourceState[]`  |
| `createRemoteSource`   | POST   | `/api/admin/remote/sources`               | body: `{ name, url, username?, password?, enabled }` | `RemoteSourceResponse` |
| `deleteRemoteSource`   | DELETE | `/api/admin/remote/sources/:name`         | path: `name`                                         | `void`                 |
| `syncRemoteSource`     | POST   | `/api/admin/remote/sources/:name/sync`    | path: `name`                                         | `{ status }`           |
| `getRemoteStats`       | GET    | `/api/admin/remote/stats`                 | none                                                 | `RemoteStats`          |
| `getRemoteMedia`       | GET    | `/api/admin/remote/media`                 | none                                                 | `RemoteMediaItem[]`    |
| `getRemoteSourceMedia` | GET    | `/api/admin/remote/sources/:source/media` | path: `source`                                       | `RemoteMediaItem[]`    |
| `cacheRemoteMedia`     | POST   | `/api/admin/remote/cache`                 | body: `{ url, source_name }`                         | `unknown`              |
| `cleanRemoteCache`     | POST   | `/api/admin/remote/cache/clean`           | none                                                 | `{ removed }`          |

### Auto-Discovery

| Function                     | Method | Endpoint                           | Parameters                  | Response Type           |
|------------------------------|--------|------------------------------------|-----------------------------|-------------------------|
| `discoveryScan`              | POST   | `/api/admin/discovery/scan`        | body: `{ directory }`       | `DiscoverySuggestion[]` |
| `getDiscoverySuggestions`    | GET    | `/api/admin/discovery/suggestions` | none                        | `DiscoverySuggestion[]` |
| `applyDiscoverySuggestion`   | POST   | `/api/admin/discovery/apply`       | body: `{ original_path }`   | `void`                  |
| `dismissDiscoverySuggestion` | DELETE | `/api/admin/discovery/*path`       | path: encoded original path | `void`                  |

### Suggestions Stats

| Function             | Method | Endpoint                       | Parameters | Response Type     |
|----------------------|--------|--------------------------------|------------|-------------------|
| `getSuggestionStats` | GET    | `/api/admin/suggestions/stats` | none       | `SuggestionStats` |

### Receiver / Slaves

| Function              | Method | Endpoint                            | Parameters                          | Response Type         |
|-----------------------|--------|-------------------------------------|-------------------------------------|-----------------------|
| `listSlaves`          | GET    | `/api/admin/receiver/slaves`        | none                                | `SlaveNode[]`         |
| `getReceiverStats`    | GET    | `/api/admin/receiver/stats`         | none                                | `ReceiverStats`       |
| `removeReceiverSlave` | DELETE | `/api/admin/receiver/slaves/:id`    | path: `id`                          | `void`                |
| `getSlaveMedia`       | GET    | `/api/receiver/media`               | none                                | `ReceiverMedia[]`     |
| `listDuplicates`      | GET    | `/api/admin/duplicates`             | query: `status` (default "pending") | `ReceiverDuplicate[]` |
| `resolveDuplicate`    | POST   | `/api/admin/duplicates/:id/resolve` | path: `id`, body: `{ action }`      | `{ message, action }` |

### Crawler

| Function                  | Method | Endpoint                                     | Parameters             | Response Type        |
|---------------------------|--------|----------------------------------------------|------------------------|----------------------|
| `listCrawlerTargets`      | GET    | `/api/admin/crawler/targets`                 | none                   | `CrawlerTarget[]`    |
| `addCrawlerTarget`        | POST   | `/api/admin/crawler/targets`                 | body: `{ url, name? }` | `CrawlerTarget`      |
| `deleteCrawlerTarget`     | DELETE | `/api/admin/crawler/targets/:id`             | path: `id`             | `void`               |
| `startCrawl`              | POST   | `/api/admin/crawler/targets/:id/crawl`       | path: `id`             | `void`               |
| `getCrawlerDiscoveries`   | GET    | `/api/admin/crawler/discoveries`             | query: `target_id?`    | `CrawlerDiscovery[]` |
| `approveCrawlerDiscovery` | POST   | `/api/admin/crawler/discoveries/:id/approve` | path: `id`             | `CrawlerDiscovery`   |
| `ignoreCrawlerDiscovery`  | POST   | `/api/admin/crawler/discoveries/:id/ignore`  | path: `id`             | `void`               |
| `deleteCrawlerDiscovery`  | DELETE | `/api/admin/crawler/discoveries/:id`         | path: `id`             | `void`               |
| `getCrawlerStats`         | GET    | `/api/admin/crawler/stats`                   | none                   | `CrawlerStats`       |

### Extractor

| Function              | Method | Endpoint                         | Parameters      | Response Type     |
|-----------------------|--------|----------------------------------|-----------------|-------------------|
| `listExtractorItems`  | GET    | `/api/admin/extractor/items`     | none            | `ExtractorItem[]` |
| `addExtractorUrl`     | POST   | `/api/admin/extractor/items`     | body: `{ url }` | `ExtractorItem`   |
| `deleteExtractorItem` | DELETE | `/api/admin/extractor/items/:id` | path: `id`      | `void`            |
| `getExtractorStats`   | GET    | `/api/admin/extractor/stats`     | none            | `ExtractorStats`  |

### Playlists (Admin)

| Function              | Method | Endpoint                     | Parameters                                         | Response Type                 |
|-----------------------|--------|------------------------------|----------------------------------------------------|-------------------------------|
| `listAllPlaylists`    | GET    | `/api/admin/playlists`       | query: `page?`, `limit?`, `search?`, `visibility?` | `AdminPlaylistListResponse`   |
| `getPlaylistStats`    | GET    | `/api/admin/playlists/stats` | none                                               | `AdminPlaylistStats`          |
| `bulkDeletePlaylists` | POST   | `/api/admin/playlists/bulk`  | body: `{ ids }`                                    | `{ success, failed, errors }` |
| `deletePlaylist`      | DELETE | `/api/admin/playlists/:id`   | path: `id`                                         | `void`                        |

### Updates

| Function                  | Method | Endpoint                            | Parameters                          | Response Type                          |
|---------------------------|--------|-------------------------------------|-------------------------------------|----------------------------------------|
| `checkForUpdates`         | GET    | `/api/admin/update/check`           | none                                | `UpdateInfo`                           |
| `getUpdateStatus`         | GET    | `/api/admin/update/status`          | none                                | `UpdateStatus`                         |
| `applyUpdate`             | POST   | `/api/admin/update/apply`           | none                                | `UpdateStatus`                         |
| `checkSourceUpdates`      | GET    | `/api/admin/update/source/check`    | none                                | `{ updates_available, remote_commit }` |
| `applySourceUpdate`       | POST   | `/api/admin/update/source/apply`    | none                                | `UpdateStatus`                         |
| `getSourceUpdateProgress` | GET    | `/api/admin/update/source/progress` | none                                | `UpdateStatus`                         |
| `getUpdateConfig`         | GET    | `/api/admin/update/config`          | none                                | `{ update_method, branch }`            |
| `setUpdateConfig`         | PUT    | `/api/admin/update/config`          | body: `{ update_method?, branch? }` | `{ update_method, branch }`            |

### Downloader

| Function                | Method | Endpoint                                    | Parameters                                                               | Response Type            |
|-------------------------|--------|---------------------------------------------|--------------------------------------------------------------------------|--------------------------|
| `getDownloaderHealth`   | GET    | `/api/admin/downloader/health`              | none                                                                     | `DownloaderHealth`       |
| `detectDownload`        | POST   | `/api/admin/downloader/detect`              | body: `{ url }`                                                          | `DownloaderDetectResult` |
| `listDownloaderJobs`    | GET    | `/api/admin/downloader/downloads`           | none                                                                     | `DownloaderJob[]`        |
| `createDownloaderJob`   | POST   | `/api/admin/downloader/download`            | body: `{ url, title?, clientId, isYouTube?, isYouTubeMusic?, relayId? }` | `{ downloadId, status }` |
| `cancelDownloaderJob`   | POST   | `/api/admin/downloader/cancel/:id`          | path: `id`                                                               | `void`                   |
| `deleteDownloaderJob`   | DELETE | `/api/admin/downloader/downloads/:filename` | path: `filename`                                                         | `void`                   |
| `getDownloaderSettings` | GET    | `/api/admin/downloader/settings`            | none                                                                     | `DownloaderSettings`     |
| `listImportable`        | GET    | `/api/admin/downloader/importable`          | none                                                                     | `ImportableFile[]`       |
| `importFile`            | POST   | `/api/admin/downloader/import`              | body: `{ filename, delete_source, trigger_scan }`                        | `ImportResult`           |

### Server Diagnostics (non-admin prefix routes called by admin composable)

| Function             | Method | Endpoint                    | Parameters   | Response Type    |
|----------------------|--------|-----------------------------|--------------|------------------|
| `getServerStatus`    | GET    | `/api/status`               | none         | `ServerStatus`   |
| `listModuleStatuses` | GET    | `/api/modules`              | none         | `ModuleHealth[]` |
| `getModuleHealth`    | GET    | `/api/modules/:name/health` | path: `name` | `ModuleHealth`   |

### Receiver Media (non-admin prefix)

| Function            | Method | Endpoint                  | Parameters | Response Type   |
|---------------------|--------|---------------------------|------------|-----------------|
| `getSlaveMediaItem` | GET    | `/api/receiver/media/:id` | path: `id` | `ReceiverMedia` |

### Data Deletion Requests

| Function                 | Method | Endpoint                                        | Parameters                                   | Response Type           |
|--------------------------|--------|-------------------------------------------------|----------------------------------------------|-------------------------|
| `listDeletionRequests`   | GET    | `/api/admin/data-deletion-requests`             | query: `status?`                             | `DataDeletionRequest[]` |
| `processDeletionRequest` | POST   | `/api/admin/data-deletion-requests/:id/process` | path: `id`, body: `{ action, admin_notes? }` | `{ status }`            |

---

## Totals

- **Total API call sites**: 133
- **Composable modules**: 15 (useApiEndpoints, useMediaApi, useHlsApi, usePlaybackApi, useWatchHistoryApi,
  useSuggestionsApi, useStorageApi, usePlaylistApi, useSettingsApi, useVersionApi, useAgeGateApi, useRatingsApi,
  useCategoryBrowseApi, useUploadApi, useFavoritesApi, useAPITokensApi, useAnalyticsApi, useAdminApi)
- **URL builders (no fetch)**: 6 (getThumbnailUrl, getStreamUrl, getDownloadUrl, getRemoteStreamUrl,
  getMasterPlaylistUrl, exportPlaylist, exportAuditLogUrl, exportCsv)
- **HTTP methods used**: GET, POST, PUT, DELETE (no PATCH)
