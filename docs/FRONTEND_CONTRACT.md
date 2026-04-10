# Frontend API Contract Reference

> Generated from backend source code audit (2026-04-09).
> Source of truth: `api/routes/routes.go`, `api/handlers/*.go`, `pkg/middleware/`.

## Response Envelope

All JSON endpoints use a consistent envelope:

```json
// Success (200 unless noted)
{ "success": true, "data": <payload> }

// Error
{ "success": false, "error": "<message>" }
```

Helpers: `writeSuccess(c, data)` / `writeError(c, code, msg)` in `api/handlers/response.go`.

## Authentication

| Mechanism                                      | Description                                                                          |
|------------------------------------------------|--------------------------------------------------------------------------------------|
| Cookie `session_id`                            | Browser sessions. Set by Login/Register. HttpOnly, Strict SameSite, Secure on HTTPS. |
| Bearer token (`Authorization: Bearer <token>`) | Programmatic access via API tokens.                                                  |
| `X-API-Key` header / `api_key` query param     | Receiver (slave) endpoints only.                                                     |

### Middleware Layers (applied in order)

1. **`sessionAuth`** -- always runs, loads session/user into context from cookie or Bearer token.
2. **`requireAuth()`** -- requires valid, non-expired session with enabled user. Returns `401` or `403`.
3. **`adminAuth()`** -- requires `requireAuth` + `role=admin` + `enabled=true`. Returns `401`/`403`.

### Common Error Responses

| Status                    | Condition                                                                                     |
|---------------------------|-----------------------------------------------------------------------------------------------|
| `401 Unauthorized`        | No session / expired session / invalid token                                                  |
| `403 Forbidden`           | Non-admin accessing admin routes; disabled account; mature content denied; missing permission |
| `429 Too Many Requests`   | Account locked (too many failed logins); stream limit reached                                 |
| `503 Service Unavailable` | Optional module not available; server initializing                                            |

---

## Public Endpoints (No Auth Required)

### POST /api/auth/login

**Handler:** `auth.go` `Login`

| Field      | Location | Type   | Required |
|------------|----------|--------|----------|
| `username` | body     | string | yes      |
| `password` | body     | string | yes      |

**Success 200:**

```json
{
  "session_id": "string",
  "username": "string",
  "role": "admin|viewer",
  "is_admin": true,
  "expires_at": "RFC3339"
}
```

Sets `session_id` cookie.

**Errors:** `401` invalid credentials, `429` account locked.

---

### POST /api/auth/logout

**Handler:** `auth.go` `Logout`

No request body. Reads `session_id` from cookie.

**Success 200:** `null`

Clears `session_id` cookie.

---

### GET /api/auth/session

**Handler:** `auth.go` `CheckSession`

No params.

**Success 200 (unauthenticated):**

```json
{ "authenticated": false, "allow_guests": true }
```

**Success 200 (authenticated):**

```json
{ "authenticated": true, "allow_guests": true, "user": <User object> }
```

---

### POST /api/auth/register

**Handler:** `auth.go` `Register`

| Field      | Location | Type   | Required | Validation                  |
|------------|----------|--------|----------|-----------------------------|
| `username` | body     | string | yes      | 3-64 chars, `[a-zA-Z0-9_-]` |
| `password` | body     | string | yes      | min 8 chars                 |
| `email`    | body     | string | no       | valid email if provided     |

**Success 200:** `<User object>`

Sets `session_id` cookie.

**Errors:** `403` registration disabled, `400` validation, `409` username taken.

---

### GET /api/version

**Handler:** `system.go` `GetVersion`

**Success 200:**

```json
{ "version": "1.2.3" }
```

---

### GET /api/server-settings

**Handler:** `system.go` `GetServerSettings`

Returns public feature flags and UI config. No auth needed (fetched before login).

**Success 200:**

```json
{
  "thumbnails": { "enabled": true, "autoGenerate": true, "width": 320, "height": 180, "video_preview_count": 3 },
  "streaming": { "mobileOptimization": false },
  "analytics": { "enabled": true },
  "features": {
    "enableThumbnails": true, "enableHLS": true, "enableAnalytics": true,
    "enablePlaylists": true, "enableUserAuth": true, "enableAdminPanel": true,
    "enableSuggestions": true, "enableAutoDiscovery": true,
    "enableDuplicateDetection": true, "enableDownloader": true
  },
  "uploads": { "enabled": true, "maxFileSize": 10737418240 },
  "admin": { "enabled": true },
  "ui": { "items_per_page": 50, "mobile_items_per_page": 20, "mobile_grid_columns": 2 },
  "age_gate": { "enabled": false },
  "auth": { "allow_registration": true, "allow_guests": true }
}
```

---

### GET /api/permissions

**Handler:** `auth.go` `GetPermissions`

Returns different data based on auth status.

**Success 200 (unauthenticated):**

```json
{
  "authenticated": false,
  "show_mature": false,
  "mature_preference_set": false,
  "capabilities": {
    "canUpload": false, "canDownload": false, "canCreatePlaylists": false,
    "canViewMature": false, "canStream": false, "canDelete": false, "canManage": false
  }
}
```

**Success 200 (authenticated):**

```json
{
  "authenticated": true,
  "username": "string",
  "role": "admin|viewer",
  "user_type": "string",
  "show_mature": false,
  "mature_preference_set": false,
  "capabilities": { "canUpload": true, "canDownload": true, ... },
  "limits": { "storage_quota": 10737418240, "concurrent_streams": 3 }
}
```

---

### GET /api/media

**Handler:** `media.go` `ListMedia`

| Param          | Location | Type   | Default | Notes                                                         |
|----------------|----------|--------|---------|---------------------------------------------------------------|
| `type`         | query    | string |         | `video` or `audio`                                            |
| `category`     | query    | string |         | filter by category                                            |
| `search`       | query    | string |         | search term                                                   |
| `sort`         | query    | string |         | `date`, `date_modified`, `name`, `size`, `views`, `my_rating` |
| `sort_order`   | query    | string |         | `desc` or `asc`                                               |
| `limit`        | query    | int    | 0 (all) | max 500                                                       |
| `offset`       | query    | int    | 0       | max 50000                                                     |
| `tags`         | query    | string |         | comma-separated                                               |
| `is_mature`    | query    | string |         | `true`/`1` or `false`/`0`                                     |
| `min_rating`   | query    | float  |         | min user rating (auth required for effect)                    |
| `hide_watched` | query    | string |         | `true`/`1` (auth required)                                    |

**Success 200:**

```json
{
  "items": [<MediaItem>, ...],
  "total_items": 100,
  "total_pages": 2,
  "scanning": false,
  "initializing": false,
  "user_ratings": { "media-id": 4.5 }
}
```

`user_ratings` only present when authenticated and items have ratings. `initializing` only present when true.

---

### GET /api/media/:id

**Handler:** `media.go` `GetMedia`

**Path param:** `id` (media UUID)

**Success 200:** `<MediaItem>`

**Errors:** `404`, `503` (initializing), `403` (mature content).

---

### GET /api/media/stats

**Handler:** `media.go` `GetMediaStats`

**Success 200:** Media stats object (video_count, audio_count, total_size, etc.)

---

### GET /api/media/categories

**Handler:** `media.go` `GetCategories`

**Success 200:** `["Movies", "TV Shows", "Music", ...]`

---

### GET /api/media/batch

**Handler:** `media.go` `GetBatchMedia`

| Param | Location | Type   | Notes                          |
|-------|----------|--------|--------------------------------|
| `ids` | query    | string | comma-separated UUIDs, max 100 |

**Success 200:**

```json
{ "items": { "id1": <MediaItem>, "id2": <MediaItem> } }
```

---

### GET /health

**Handler:** `system.go` `GetHealth`

**Success 200 / 503:**

```json
{ "status": "ok|degraded", "timestamp": 1712000000 }
```

Authenticated users also see `version`, `modules`, `problems`.

---

### GET /api/age-gate/status

**Handler:** `pkg/middleware/agegate.go` `GinStatusHandler`

### POST /api/age-verify

**Handler:** `pkg/middleware/agegate.go` `GinVerifyHandler`

---

### GET /api/playlists/public

**Handler:** `playlists.go` `ListPublicPlaylists`

**Success 200:** `[<Playlist>, ...]`

---

### GET /api/suggestions

**Handler:** `suggestions.go` `GetSuggestions`

| Param   | Location | Type | Default      |
|---------|----------|------|--------------|
| `limit` | query    | int  | 10 (max 100) |

**Success 200:** `[<Suggestion>, ...]`

---

### GET /api/suggestions/trending

**Handler:** `suggestions.go` `GetTrendingSuggestions`

| Param   | Location | Type | Default      |
|---------|----------|------|--------------|
| `limit` | query    | int  | 10 (max 100) |

**Success 200:** `[<Suggestion>, ...]`

---

### GET /api/suggestions/similar

**Handler:** `suggestions.go` `GetSimilarMedia`

| Param   | Location | Type   | Required     |
|---------|----------|--------|--------------|
| `id`    | query    | string | yes          |
| `limit` | query    | int    | 10 (max 100) |

**Success 200:** `[<Suggestion>, ...]`

---

### GET /api/suggestions/recent

**Handler:** `suggestions.go` `GetRecentContent`

| Param   | Location | Type | Default      |
|---------|----------|------|--------------|
| `days`  | query    | int  | 14 (max 365) |
| `limit` | query    | int  | 20 (max 100) |

**Success 200:** `[{ "id", "name", "type", "category", "date_added", "thumbnail_url" }, ...]`

---

### GET /api/thumbnails/previews

**Handler:** `thumbnails.go` `GetThumbnailPreviews`

| Param | Location | Type   | Required |
|-------|----------|--------|----------|
| `id`  | query    | string | yes      |

**Success 200:**

```json
{ "previews": ["/thumbnails/uuid_preview_0.jpg", ...] }
```

---

### GET /api/thumbnails/batch

**Handler:** `thumbnails.go` `GetThumbnailBatch`

| Param | Location | Type   | Notes                     |
|-------|----------|--------|---------------------------|
| `ids` | query    | string | comma-separated, max 50   |
| `w`   | query    | int    | optional responsive width |

**Success 200:**

```json
{ "thumbnails": { "id1": "/thumbnail?id=id1", "id2": "/thumbnail?id=id2&w=320" } }
```

---

### GET /thumbnail

**Handler:** `thumbnails.go` `GetThumbnail`

| Param  | Location | Type   | Notes                                          |
|--------|----------|--------|------------------------------------------------|
| `id`   | query    | string | media UUID                                     |
| `type` | query    | string | `placeholder`, `audio_placeholder`, `censored` |
| `w`    | query    | int    | responsive width                               |

Returns image bytes (JPEG/WebP). Supports HEAD.

---

### GET /thumbnails/:filename

**Handler:** `thumbnails.go` `ServeThumbnailFile`

Returns image file from thumbnails directory. Supports HEAD.

---

### GET /media

**Handler:** `media.go` `StreamMedia`

| Param     | Location | Type   | Required |
|-----------|----------|--------|----------|
| `id`      | query    | string | yes      |
| `quality` | query    | string | no       |

Streams media bytes. Supports Range requests. May require auth if config `streaming.require_auth` is true.

**Errors:** `401`, `403` (mature), `429` (stream limit), `404`, `503`.

---

### GET /download

**Handler:** `media.go` `DownloadMedia`

| Param | Location | Type   | Required |
|-------|----------|--------|----------|
| `id`  | query    | string | yes      |

Serves file with Content-Disposition attachment. May require auth if config `download.require_auth` is true.

**Errors:** `401`, `403`, `404`, `413` (too large), `503`.

---

### HLS Direct Streaming (No Auth)

| Method | Path                              | Handler                         |
|--------|-----------------------------------|---------------------------------|
| GET    | `/hls/:id/master.m3u8`            | `hls.go` `ServeMasterPlaylist`  |
| GET    | `/hls/:id/:quality/playlist.m3u8` | `hls.go` `ServeVariantPlaylist` |
| GET    | `/hls/:id/:quality/:segment`      | `hls.go` `ServeSegment`         |

Returns HLS playlist/segment bytes. Mature content access checked.

---

## Authenticated Endpoints (requireAuth)

### GET /api/preferences

**Handler:** `auth.go` `GetPreferences`

**Success 200:** `<UserPreferences object>`

---

### POST /api/preferences

**Handler:** `auth.go` `UpdatePreferences`

Body: partial JSON object with any of:
`theme`, `view_mode`, `default_quality`, `auto_play`/`autoplay`, `playback_speed` (0.25-4.0), `volume` (0-1),
`show_mature`, `language`, `equalizer_preset`/`equalizer_bands`, `resume_playback`, `show_analytics`, `items_per_page` (
1-200), `sort_by`, `sort_order`, `filter_category`, `filter_media_type`, `custom_eq_presets`, `show_continue_watching`,
`show_recommended`, `show_trending`.

**Success 200:** `<Updated UserPreferences>`

---

### POST /api/auth/change-password

**Handler:** `auth.go` `ChangePassword`

| Field              | Location | Type   | Required    |
|--------------------|----------|--------|-------------|
| `current_password` | body     | string | yes         |
| `new_password`     | body     | string | yes (min 8) |

**Success 200:** `{ "status": "password_changed" }`

---

### POST /api/auth/delete-account

**Handler:** `auth.go` `DeleteAccount`

| Field      | Location | Type   | Required |
|------------|----------|--------|----------|
| `password` | body     | string | yes      |

**Success 200:** `{ "status": "account_deleted", "message": "..." }`

**Errors:** `403` (admin accounts cannot self-delete).

---

### POST /api/auth/data-deletion-request

**Handler:** `deletion_requests.go` `RequestDataDeletion`

| Field    | Location | Type   | Required |
|----------|----------|--------|----------|
| `reason` | body     | string | no       |

**Success 200:**

```json
{ "status": "submitted", "message": "...", "id": "string" }
```

**Errors:** `409` existing pending request.

---

### API Tokens

| Method | Path                   | Handler                           | Notes                                                                                  |
|--------|------------------------|-----------------------------------|----------------------------------------------------------------------------------------|
| GET    | `/api/auth/tokens`     | `auth_tokens.go` `ListAPITokens`  | Returns `[{ id, name, last_used_at?, expires_at?, created_at }]`                       |
| POST   | `/api/auth/tokens`     | `auth_tokens.go` `CreateAPIToken` | Body: `{ name, ttl_seconds? }`. Returns `{ id, name, token, created_at, expires_at? }` |
| DELETE | `/api/auth/tokens/:id` | `auth_tokens.go` `DeleteAPIToken` | Success: `null`                                                                        |

---

### Playback

| Method | Path                  | Handler                                | Params/Body                        |
|--------|-----------------------|----------------------------------------|------------------------------------|
| GET    | `/api/playback`       | `media.go` `GetPlaybackPosition`       | Query: `id`                        |
| GET    | `/api/playback/batch` | `media.go` `GetBatchPlaybackPositions` | Query: `ids` (comma-sep, max 100)  |
| POST   | `/api/playback`       | `media.go` `TrackPlayback`             | Body: `{ id, position, duration }` |

**GET /api/playback Success:** `{ "position": 42.5 }`

**GET /api/playback/batch Success:** `{ "positions": { "id1": 42.5, "id2": 100.0 } }`

**POST /api/playback Success:** `null`

---

### Watch History

| Method | Path                        | Handler                        | Params                             |
|--------|-----------------------------|--------------------------------|------------------------------------|
| GET    | `/api/watch-history`        | `auth.go` `GetWatchHistory`    | Query: `id`, `completed`, `limit`  |
| DELETE | `/api/watch-history`        | `auth.go` `ClearWatchHistory`  | Query: `id` (single) or none (all) |
| GET    | `/api/watch-history/export` | `auth.go` `ExportWatchHistory` | Returns CSV file                   |

**GET Success:** `[<WatchHistoryItem>, ...]`

**DELETE Success:** `{ "status": "removed" }` or `{ "status": "cleared" }`

---

### Favorites (Watch Later)

| Method | Path                       | Handler                         | Params/Body          |
|--------|----------------------------|---------------------------------|----------------------|
| GET    | `/api/favorites`           | `favorites.go` `GetFavorites`   | None                 |
| POST   | `/api/favorites`           | `favorites.go` `AddFavorite`    | Body: `{ media_id }` |
| GET    | `/api/favorites/:media_id` | `favorites.go` `CheckFavorite`  | Path: `media_id`     |
| DELETE | `/api/favorites/:media_id` | `favorites.go` `RemoveFavorite` | Path: `media_id`     |

**GET /api/favorites Success:** `[{ id, media_id, media_path, added_at }]`

**GET /api/favorites/:media_id Success:** `{ "is_favorite": true }`

---

### Playlists (User)

| Method | Path                         | Handler                               | Notes                                                                      |
|--------|------------------------------|---------------------------------------|----------------------------------------------------------------------------|
| GET    | `/api/playlists`             | `playlists.go` `ListPlaylists`        | Returns user's playlists                                                   |
| POST   | `/api/playlists`             | `playlists.go` `CreatePlaylist`       | Body: `{ name, description?, is_public? }`. Requires `CanCreatePlaylists`. |
| POST   | `/api/playlists/bulk-delete` | `playlists.go` `BulkDeletePlaylists`  | Body: `{ ids: [] }` (max 100)                                              |
| GET    | `/api/playlists/:id`         | `playlists.go` `GetPlaylist`          |                                                                            |
| PUT    | `/api/playlists/:id`         | `playlists.go` `UpdatePlaylist`       | Body: partial `{ name?, description?, is_public?, cover_image? }`          |
| DELETE | `/api/playlists/:id`         | `playlists.go` `DeletePlaylist`       |                                                                            |
| GET    | `/api/playlists/:id/export`  | `playlists.go` `ExportPlaylist`       | Query: `format` (`json`, `m3u`, `m3u8`)                                    |
| POST   | `/api/playlists/:id/items`   | `playlists.go` `AddPlaylistItem`      | Body: `{ media_id, title?, name? }`                                        |
| DELETE | `/api/playlists/:id/items`   | `playlists.go` `RemovePlaylistItem`   | Query: `media_id` or `item_id`                                             |
| PUT    | `/api/playlists/:id/reorder` | `playlists.go` `ReorderPlaylistItems` | Body: `{ positions: [int] }`                                               |
| DELETE | `/api/playlists/:id/clear`   | `playlists.go` `ClearPlaylist`        |                                                                            |
| POST   | `/api/playlists/:id/copy`    | `playlists.go` `CopyPlaylist`         | Body: `{ name }`                                                           |

**POST /api/playlists Success:** `<Playlist>`

**POST /api/playlists/bulk-delete Success:** `{ "deleted": 3, "failed": 0 }`

---

### HLS API

| Method | Path                    | Handler                         | Notes                                      |
|--------|-------------------------|---------------------------------|--------------------------------------------|
| GET    | `/api/hls/capabilities` | `hls.go` `GetHLSCapabilities`   | Returns HLS module config                  |
| GET    | `/api/hls/check`        | `hls.go` `CheckHLSAvailability` | Query: `id`. Auto-generates if configured. |
| POST   | `/api/hls/generate`     | `hls.go` `GenerateHLS`          | Body: `{ id, qualities?, quality? }`       |
| GET    | `/api/hls/status/:id`   | `hls.go` `GetHLSStatus`         | Path: job ID                               |

**HLS Check/Status Response:**

```json
{
  "id": "string", "job_id": "string", "status": "pending|processing|completed|failed",
  "progress": 0.75, "qualities": ["720p","1080p"], "available": true,
  "hls_url": "/hls/uuid/master.m3u8", "started_at": "RFC3339",
  "completed_at": "RFC3339", "error": "", "fail_count": 0
}
```

---

### Suggestions (Auth Required)

| Method | Path                            | Handler                                       |
|--------|---------------------------------|-----------------------------------------------|
| GET    | `/api/suggestions/continue`     | `suggestions.go` `GetContinueWatching`        |
| GET    | `/api/suggestions/personalized` | `suggestions.go` `GetPersonalizedSuggestions` |
| GET    | `/api/suggestions/profile`      | `suggestions.go` `GetMyProfile`               |
| DELETE | `/api/suggestions/profile`      | `suggestions.go` `ResetMyProfile`             |
| GET    | `/api/suggestions/new`          | `suggestions.go` `GetNewSinceLastVisit`       |
| GET    | `/api/suggestions/on-deck`      | `suggestions.go` `GetOnDeck`                  |
| POST   | `/api/ratings`                  | `suggestions.go` `RecordRating`               |
| GET    | `/api/ratings`                  | `suggestions.go` `GetMyRatings`               |

**POST /api/ratings Body:** `{ id, rating }` (0-5)

**GET /api/ratings Success:** `[{ media_id, name, category, media_type, rating, thumbnail_url? }]`

**GET /api/suggestions/new Success:** `{ "items": [...], "since": "RFC3339", "total": 5 }`

**GET /api/suggestions/on-deck Success:**
`{ "items": [{ media_id, name, show_name, season, episode, category, thumbnail_url }], "total": 3 }`

---

### Category Browse

| Method | Path                     | Handler                        |
|--------|--------------------------|--------------------------------|
| GET    | `/api/browse/categories` | `media.go` `GetCategoryBrowse` |

| Param      | Location | Type   | Notes                  |
|------------|----------|--------|------------------------|
| `category` | query    | string | if empty returns stats |
| `limit`    | query    | int    | default 200, max 500   |

---

### Upload

| Method | Path                       | Handler                         | Notes                                                                        |
|--------|----------------------------|---------------------------------|------------------------------------------------------------------------------|
| POST   | `/api/upload`              | `upload.go` `UploadMedia`       | multipart/form-data; field `files` or `file`; optional `category` form field |
| GET    | `/api/upload/:id/progress` | `upload.go` `GetUploadProgress` | Path: upload ID                                                              |

Requires `CanUpload` permission and uploads enabled.

**POST Success:**

```json
{ "uploaded": [{ "upload_id": "string", "filename": "string", "size": 12345 }], "errors": [] }
```

---

### Analytics (User)

| Method | Path                    | Handler                      |
|--------|-------------------------|------------------------------|
| POST   | `/api/analytics/events` | `analytics.go` `SubmitEvent` |

Body: `{ type, media_id, session_id?, duration?, data? }`

**Success 200:** `{ "status": "recorded" }`

---

### Storage Usage

| Method | Path                 | Handler                       |
|--------|----------------------|-------------------------------|
| GET    | `/api/storage-usage` | `system.go` `GetStorageUsage` |

**Success 200:**

```json
{
  "used_bytes": 1073741824, "used_gb": 1.0, "quota_gb": 10.0,
  "percentage": 10.0, "user_type": "standard", "is_authenticated": true
}
```

---

### Remote Stream (Auth Required)

| Method | Path             | Handler                               |
|--------|------------------|---------------------------------------|
| GET    | `/remote/stream` | `admin_remote.go` `StreamRemoteMedia` |

| Param    | Location | Type   | Required |
|----------|----------|--------|----------|
| `url`    | query    | string | yes      |
| `source` | query    | string | no       |

Proxies a remote media stream. URL must belong to a known remote source.

---

### Extractor HLS Proxy (Auth Required)

| Method | Path                                        | Handler                              |
|--------|---------------------------------------------|--------------------------------------|
| GET    | `/extractor/hls/:id/master.m3u8`            | `extractor.go` `ExtractorHLSMaster`  |
| GET    | `/extractor/hls/:id/:quality/playlist.m3u8` | `extractor.go` `ExtractorHLSVariant` |
| GET    | `/extractor/hls/:id/:quality/:segment`      | `extractor.go` `ExtractorHLSSegment` |

---

### Feed

| Method | Path        | Handler                |
|--------|-------------|------------------------|
| GET    | `/api/feed` | `feed.go` `GetRSSFeed` |

| Param      | Location | Type   | Default         |
|------------|----------|--------|-----------------|
| `category` | query    | string |                 |
| `type`     | query    | string | `video`/`audio` |
| `limit`    | query    | int    | 20 (max 50)     |

Returns `application/atom+xml`.

---

### OpenAPI Spec

| Method | Path        | Handler                      |
|--------|-------------|------------------------------|
| GET    | `/api/docs` | `system.go` `GetOpenAPISpec` |

Returns `application/yaml; charset=utf-8`.

---

## Admin Endpoints (adminAuth required)

All paths under `/api/admin/` require admin authentication.

### Server Status

| Method | Path                        | Handler                     | Notes                                 |
|--------|-----------------------------|-----------------------------|---------------------------------------|
| GET    | `/api/status`               | `server.HandleStatus`       | Server status (requires `srv != nil`) |
| GET    | `/api/modules`              | `server.HandleModules`      | Module list                           |
| GET    | `/api/modules/:name/health` | `server.HandleModuleHealth` | Single module health                  |

---

### Admin Stats & System

| Method | Path                | Handler                         |
|--------|---------------------|---------------------------------|
| GET    | `/api/admin/stats`  | `admin.go` `AdminGetStats`      |
| GET    | `/api/admin/system` | `admin.go` `AdminGetSystemInfo` |

**GET /api/admin/stats Success:**

```json
{
  "total_videos": 100, "total_audio": 50, "active_sessions": 3,
  "total_users": 10, "disk_usage": 1073741824, "disk_total": 10737418240,
  "disk_free": 9663676416, "hls_jobs_running": 0, "hls_jobs_completed": 5,
  "server_uptime": 86400, "total_views": 500
}
```

**GET /api/admin/system Success:**

```json
{
  "version": "1.2.3", "build_date": "...", "os": "linux", "arch": "amd64",
  "go_version": "1.22", "cpu_count": 8, "memory_used": 104857600,
  "memory_total": 8589934592, "uptime": 86400,
  "modules": [{ "name": "database", "status": "healthy", "message": "", "last_check": "RFC3339" }]
}
```

---

### Cache

| Method | Path                     | Handler                       |
|--------|--------------------------|-------------------------------|
| POST   | `/api/admin/cache/clear` | `system.go` `ClearMediaCache` |

Returns `202 Accepted`: `{ "status": "accepted", "message": "Media rescan started in background" }`

---

### User Management

| Method | Path                                  | Handler                  | Body                                               |
|--------|---------------------------------------|--------------------------|----------------------------------------------------|
| GET    | `/api/admin/users`                    | `AdminListUsers`         |                                                    |
| POST   | `/api/admin/users`                    | `AdminCreateUser`        | `{ username, password, email?, type?, role? }`     |
| POST   | `/api/admin/users/bulk`               | `AdminBulkUsers`         | `{ usernames: [], action: "delete"                 |"enable"|"disable" }` (max 200) |
| GET    | `/api/admin/users/:username`          | `AdminGetUser`           |                                                    |
| PUT    | `/api/admin/users/:username`          | `AdminUpdateUser`        | `{ role?, enabled?, email?, type?, permissions? }` |
| DELETE | `/api/admin/users/:username`          | `AdminDeleteUser`        |                                                    |
| POST   | `/api/admin/users/:username/password` | `AdminChangePassword`    | `{ new_password }`                                 |
| GET    | `/api/admin/users/:username/sessions` | `AdminGetUserSessions`   |                                                    |
| POST   | `/api/admin/change-password`          | `AdminChangeOwnPassword` | `{ current_password, new_password }`               |

**AdminBulkUsers Success:** `{ "success": 5, "failed": 0, "errors": [] }`

---

### Data Deletion Requests

| Method | Path                                            | Handler                       |
|--------|-------------------------------------------------|-------------------------------|
| GET    | `/api/admin/data-deletion-requests`             | `AdminListDeletionRequests`   |
| POST   | `/api/admin/data-deletion-requests/:id/process` | `AdminProcessDeletionRequest` |

**GET Query:** `status` (optional filter)

**POST Body:** `{ action: "approve"|"deny", admin_notes? }`

**POST Success:** `{ "status": "approved|denied" }`

---

### Audit Log

| Method | Path                          | Handler               |
|--------|-------------------------------|-----------------------|
| GET    | `/api/admin/audit-log`        | `AdminGetAuditLog`    |
| GET    | `/api/admin/audit-log/export` | `AdminExportAuditLog` |

**GET Query:** `limit` (1-1000, default 100), `offset` (0-100000), `user_id`

**Export:** Returns CSV file download.

---

### Server Logs

| Method | Path              | Handler         |
|--------|-------------------|-----------------|
| GET    | `/api/admin/logs` | `GetServerLogs` |

| Param    | Location | Type   | Default          |
|----------|----------|--------|------------------|
| `limit`  | query    | int    | 200 (max 2000)   |
| `level`  | query    | string | filter by level  |
| `module` | query    | string | filter by module |

**Success 200:** `[{ raw, timestamp, level, module, message }]`

---

### Configuration

| Method | Path                | Handler             |
|--------|---------------------|---------------------|
| GET    | `/api/admin/config` | `AdminGetConfig`    |
| PUT    | `/api/admin/config` | `AdminUpdateConfig` |

**PUT Body:** Flat/nested JSON config updates. Denied sections: `database`, `auth`, `admin`, `receiver`, `storage`,
`huggingface`.

**PUT Success:** `{ "config": {...}, "rejected_keys"?: [...] }`

---

### Task Management

| Method | Path                           | Handler            |
|--------|--------------------------------|--------------------|
| GET    | `/api/admin/tasks`             | `AdminListTasks`   |
| POST   | `/api/admin/tasks/:id/run`     | `AdminRunTask`     |
| POST   | `/api/admin/tasks/:id/enable`  | `AdminEnableTask`  |
| POST   | `/api/admin/tasks/:id/disable` | `AdminDisableTask` |
| POST   | `/api/admin/tasks/:id/stop`    | `AdminStopTask`    |

---

### Active Streams & Uploads

| Method | Path                        | Handler                 |
|--------|-----------------------------|-------------------------|
| GET    | `/api/admin/streams`        | `AdminGetActiveStreams` |
| GET    | `/api/admin/uploads/active` | `AdminGetActiveUploads` |

---

### Admin Media Management

| Method | Path                    | Handler            | Notes                                                                                                         |
|--------|-------------------------|--------------------|---------------------------------------------------------------------------------------------------------------|
| GET    | `/api/admin/media`      | `AdminListMedia`   | Query: `type`, `category`, `search`, `tags`, `is_mature`, `sort`, `sort_order`, `limit` (1-1000), `page` (1+) |
| POST   | `/api/admin/media/bulk` | `AdminBulkMedia`   | Body: `{ ids: [], action: "delete"                                                                            |"update", data?: { category?, is_mature?, tags? } }` (max 500) |
| PUT    | `/api/admin/media/:id`  | `AdminUpdateMedia` | Body: `{ name?, tags?, category?, is_mature?, mature_content?, metadata? }`                                   |
| DELETE | `/api/admin/media/:id`  | `AdminDeleteMedia` |                                                                                                               |
| POST   | `/api/admin/media/scan` | `ScanMedia`        | Triggers rescan                                                                                               |

**AdminListMedia Success:**

```json
{ "items": [...], "total_items": 100, "total_pages": 2 }
```

**AdminBulkMedia Success:** `{ "success": 10, "failed": 0, "errors": [] }`

---

### Admin Playlists

| Method | Path                         | Handler                    | Notes                                                               |
|--------|------------------------------|----------------------------|---------------------------------------------------------------------|
| GET    | `/api/admin/playlists`       | `AdminListPlaylists`       | Query: `search`, `visibility` (`public`/`private`), `limit`, `page` |
| GET    | `/api/admin/playlists/stats` | `AdminPlaylistStats`       |                                                                     |
| POST   | `/api/admin/playlists/bulk`  | `AdminBulkDeletePlaylists` | Body: `{ ids: [] }` (max 500)                                       |
| DELETE | `/api/admin/playlists/:id`   | `AdminDeletePlaylist`      |                                                                     |

---

### Analytics (Admin)

| Method | Path                             | Handler                 | Params                                |
|--------|----------------------------------|-------------------------|---------------------------------------|
| GET    | `/api/analytics`                 | `GetAnalyticsSummary`   |                                       |
| GET    | `/api/analytics/daily`           | `GetDailyStats`         | `days` (1-365, default 30)            |
| GET    | `/api/analytics/top`             | `GetTopMedia`           | `limit` (1-500, default 10)           |
| GET    | `/api/analytics/content`         | `GetContentPerformance` | `limit` (1-500, default 20)           |
| GET    | `/api/analytics/events/stats`    | `GetEventStats`         |                                       |
| GET    | `/api/analytics/events/by-type`  | `GetEventsByType`       | `type` (required), `limit` (1-1000)   |
| GET    | `/api/analytics/events/by-media` | `GetEventsByMedia`      | `media_id` (required), `limit`        |
| GET    | `/api/analytics/events/by-user`  | `GetEventsByUser`       | `user_id` (required), `limit`         |
| GET    | `/api/analytics/events/counts`   | `GetEventTypeCounts`    |                                       |
| GET    | `/api/admin/analytics/export`    | `AdminExportAnalytics`  | `start_date`, `end_date` (YYYY-MM-DD) |

**Export:** Returns CSV file download.

---

### Scanner (Content Moderation)

| Method | Path                             | Handler             | Notes                          |
|--------|----------------------------------|---------------------|--------------------------------|
| POST   | `/api/admin/scanner/scan`        | `ScanContent`       | Body: `{ path?, auto_apply? }` |
| GET    | `/api/admin/scanner/stats`       | `GetScannerStats`   |                                |
| GET    | `/api/admin/scanner/queue`       | `GetReviewQueue`    |                                |
| POST   | `/api/admin/scanner/queue`       | `BatchReviewAction` | Body: `{ action: "approve"     |"reject", ids: [] }` |
| DELETE | `/api/admin/scanner/queue`       | `ClearReviewQueue`  |                                |
| POST   | `/api/admin/scanner/approve/:id` | `ApproveContent`    |                                |
| POST   | `/api/admin/scanner/reject/:id`  | `RejectContent`     |                                |

---

### Classify (Hugging Face)

| Method | Path                              | Handler              | Notes                          |
|--------|-----------------------------------|----------------------|--------------------------------|
| GET    | `/api/admin/classify/status`      | `ClassifyStatus`     |                                |
| GET    | `/api/admin/classify/stats`       | `ClassifyStats`      |                                |
| POST   | `/api/admin/classify/file`        | `ClassifyFile`       | Body: `{ path }`               |
| POST   | `/api/admin/classify/directory`   | `ClassifyDirectory`  | Body: `{ path }`. Returns 202. |
| POST   | `/api/admin/classify/run-task`    | `ClassifyRunTask`    | Triggers background task       |
| POST   | `/api/admin/classify/clear-tags`  | `ClassifyClearTags`  | Body: `{ id }`                 |
| POST   | `/api/admin/classify/all-pending` | `ClassifyAllPending` | Returns 202.                   |

---

### Thumbnails (Admin)

| Method | Path                             | Handler             |
|--------|----------------------------------|---------------------|
| POST   | `/api/admin/thumbnails/generate` | `GenerateThumbnail` |
| POST   | `/api/admin/thumbnails/cleanup`  | `CleanupThumbnails` |
| GET    | `/api/admin/thumbnails/stats`    | `GetThumbnailStats` |

**POST /generate Body:** `{ id, is_audio? }`

**POST /cleanup Success:** `{ orphans_removed, excess_removed, corrupt_removed, bytes_freed }`

**GET /stats Success:**
`{ total_thumbnails, total_size_mb, pending_generation, generation_errors, orphans_removed, excess_removed, corrupt_removed, last_cleanup? }`

---

### HLS (Admin)

| Method | Path                            | Handler              |
|--------|---------------------------------|----------------------|
| GET    | `/api/admin/hls/stats`          | `GetHLSStats`        |
| GET    | `/api/admin/hls/jobs`           | `ListHLSJobs`        |
| DELETE | `/api/admin/hls/jobs/:id`       | `DeleteHLSJob`       |
| GET    | `/api/admin/hls/validate/:id`   | `ValidateHLS`        |
| POST   | `/api/admin/hls/clean/locks`    | `CleanHLSStaleLocks` |
| POST   | `/api/admin/hls/clean/inactive` | `CleanHLSInactive`   |

**CleanHLSInactive Body/Query:** `max_age_hours`/`threshold_hours` (default 24h)

**Success:** `{ "removed": 3, "threshold": "24h0m0s" }`

---

### Validator

| Method | Path                            | Handler             | Body     |
|--------|---------------------------------|---------------------|----------|
| POST   | `/api/admin/validator/validate` | `ValidateMedia`     | `{ id }` |
| POST   | `/api/admin/validator/fix`      | `FixMedia`          | `{ id }` |
| GET    | `/api/admin/validator/stats`    | `GetValidatorStats` |          |

---

### Database

| Method | Path                         | Handler                  | Notes                                                 |
|--------|------------------------------|--------------------------|-------------------------------------------------------|
| GET    | `/api/admin/database/status` | `AdminGetDatabaseStatus` |                                                       |
| POST   | `/api/admin/database/query`  | `AdminExecuteQuery`      | Body: `{ query }`. SELECT/SHOW/DESCRIBE/EXPLAIN only. |

**Query Success:** `{ "columns": [...], "rows": [[...]], "rows_affected": 10, "truncated": false }`

---

### Backups

| Method | Path                                | Handler          | Notes                                                   |
|--------|-------------------------------------|------------------|---------------------------------------------------------|
| GET    | `/api/admin/backups/v2`             | `ListBackupsV2`  |                                                         |
| POST   | `/api/admin/backups/v2`             | `CreateBackupV2` | Body: `{ description?, backup_type? }` (default "full") |
| POST   | `/api/admin/backups/v2/:id/restore` | `RestoreBackup`  |                                                         |
| DELETE | `/api/admin/backups/v2/:id`         | `DeleteBackup`   |                                                         |

---

### Auto-Discovery

| Method | Path                               | Handler                      | Notes                     |
|--------|------------------------------------|------------------------------|---------------------------|
| POST   | `/api/admin/discovery/scan`        | `DiscoverMedia`              | Body: `{ directory }`     |
| GET    | `/api/admin/discovery/suggestions` | `GetDiscoverySuggestions`    |                           |
| POST   | `/api/admin/discovery/apply`       | `ApplyDiscoverySuggestion`   | Body: `{ original_path }` |
| DELETE | `/api/admin/discovery/*path`       | `DismissDiscoverySuggestion` |                           |

---

### Categorizer

| Method | Path                                 | Handler                | Notes                      |
|--------|--------------------------------------|------------------------|----------------------------|
| POST   | `/api/admin/categorizer/file`        | `CategorizeFile`       | Body: `{ path }`           |
| POST   | `/api/admin/categorizer/directory`   | `CategorizeDirectory`  | Body: `{ directory }`      |
| GET    | `/api/admin/categorizer/stats`       | `GetCategoryStats`     |                            |
| POST   | `/api/admin/categorizer/set`         | `SetMediaCategory`     | Body: `{ path, category }` |
| GET    | `/api/admin/categorizer/by-category` | `GetByCategory`        | Query: `category`          |
| POST   | `/api/admin/categorizer/clean`       | `CleanStaleCategories` |                            |

---

### Security

| Method | Path                            | Handler               | Notes                                      |
|--------|---------------------------------|-----------------------|--------------------------------------------|
| GET    | `/api/admin/security/stats`     | `GetSecurityStats`    |                                            |
| GET    | `/api/admin/security/whitelist` | `GetWhitelist`        |                                            |
| POST   | `/api/admin/security/whitelist` | `AddToWhitelist`      | Body: `{ ip, comment?, expires_at? }`      |
| DELETE | `/api/admin/security/whitelist` | `RemoveFromWhitelist` | Body: `{ ip }`                             |
| GET    | `/api/admin/security/blacklist` | `GetBlacklist`        |                                            |
| POST   | `/api/admin/security/blacklist` | `AddToBlacklist`      | Body: `{ ip, comment?, expires_at? }`      |
| DELETE | `/api/admin/security/blacklist` | `RemoveFromBlacklist` | Body: `{ ip }`                             |
| GET    | `/api/admin/security/banned`    | `GetBannedIPs`        |                                            |
| POST   | `/api/admin/security/ban`       | `BanIP`               | Body: `{ ip, duration_minutes?, reason? }` |
| POST   | `/api/admin/security/unban`     | `UnbanIP`             | Body: `{ ip }`                             |

---

### Remote Media

| Method | Path                                      | Handler                | Notes                                                 |
|--------|-------------------------------------------|------------------------|-------------------------------------------------------|
| GET    | `/api/admin/remote/sources`               | `GetRemoteSources`     |                                                       |
| POST   | `/api/admin/remote/sources`               | `CreateRemoteSource`   | Body: `{ name, url, username?, password?, enabled? }` |
| GET    | `/api/admin/remote/stats`                 | `GetRemoteStats`       |                                                       |
| GET    | `/api/admin/remote/media`                 | `GetRemoteMedia`       |                                                       |
| GET    | `/api/admin/remote/sources/:source/media` | `GetRemoteSourceMedia` |                                                       |
| POST   | `/api/admin/remote/sources/:source/sync`  | `SyncRemoteSource`     | Async                                                 |
| DELETE | `/api/admin/remote/sources/:source`       | `DeleteRemoteSource`   |                                                       |
| POST   | `/api/admin/remote/cache`                 | `CacheRemoteMedia`     | Body: `{ url, source_name? }`                         |
| POST   | `/api/admin/remote/cache/clean`           | `CleanRemoteCache`     |                                                       |

---

### Extractor

| Method | Path                             | Handler               | Notes                   |
|--------|----------------------------------|-----------------------|-------------------------|
| GET    | `/api/admin/extractor/items`     | `ListExtractorItems`  |                         |
| POST   | `/api/admin/extractor/items`     | `AddExtractorItem`    | Body: `{ url, title? }` |
| DELETE | `/api/admin/extractor/items/:id` | `RemoveExtractorItem` |                         |
| GET    | `/api/admin/extractor/stats`     | `GetExtractorStats`   |                         |

---

### Crawler

| Method | Path                                         | Handler                   | Notes                  |
|--------|----------------------------------------------|---------------------------|------------------------|
| GET    | `/api/admin/crawler/targets`                 | `ListCrawlerTargets`      |                        |
| POST   | `/api/admin/crawler/targets`                 | `AddCrawlerTarget`        | Body: `{ url, name? }` |
| DELETE | `/api/admin/crawler/targets/:id`             | `RemoveCrawlerTarget`     |                        |
| POST   | `/api/admin/crawler/targets/:id/crawl`       | `CrawlTarget`             |                        |
| GET    | `/api/admin/crawler/discoveries`             | `ListCrawlerDiscoveries`  | Query: `status`        |
| POST   | `/api/admin/crawler/discoveries/:id/approve` | `ApproveCrawlerDiscovery` |                        |
| POST   | `/api/admin/crawler/discoveries/:id/ignore`  | `IgnoreCrawlerDiscovery`  |                        |
| DELETE | `/api/admin/crawler/discoveries/:id`         | `DeleteCrawlerDiscovery`  |                        |
| GET    | `/api/admin/crawler/stats`                   | `GetCrawlerStats`         |                        |

---

### Receiver (Master-Slave)

| Method | Path                             | Handler                    | Notes                              |
|--------|----------------------------------|----------------------------|------------------------------------|
| GET    | `/api/receiver/media`            | `ReceiverListMedia`        | Admin only. Query: `q`, `slave_id` |
| GET    | `/api/receiver/media/:id`        | `ReceiverGetMedia`         | Admin only                         |
| GET    | `/api/admin/receiver/slaves`     | `AdminReceiverListSlaves`  |                                    |
| GET    | `/api/admin/receiver/stats`      | `AdminReceiverGetStats`    |                                    |
| DELETE | `/api/admin/receiver/slaves/:id` | `AdminReceiverRemoveSlave` |                                    |

---

### Duplicates

| Method | Path                                | Handler                 | Notes                                         |
|--------|-------------------------------------|-------------------------|-----------------------------------------------|
| GET    | `/api/admin/duplicates`             | `AdminListDuplicates`   | Query: `status` (default `pending`, or `all`) |
| POST   | `/api/admin/duplicates/:id/resolve` | `AdminResolveDuplicate` | Body: `{ action: "remove_a"                   |"remove_b"|"keep_both"|"ignore" }` |

---

### Suggestions (Admin)

| Method | Path                           | Handler              |
|--------|--------------------------------|----------------------|
| GET    | `/api/admin/suggestions/stats` | `GetSuggestionStats` |

---

### Updates

| Method | Path                                | Handler                   | Notes                               |
|--------|-------------------------------------|---------------------------|-------------------------------------|
| GET    | `/api/admin/update/check`           | `CheckForUpdates`         |                                     |
| GET    | `/api/admin/update/status`          | `GetUpdateStatus`         |                                     |
| POST   | `/api/admin/update/apply`           | `ApplyUpdate`             |                                     |
| GET    | `/api/admin/update/source/check`    | `CheckForSourceUpdates`   |                                     |
| POST   | `/api/admin/update/source/apply`    | `ApplySourceUpdate`       | Returns 202                         |
| GET    | `/api/admin/update/source/progress` | `GetSourceUpdateProgress` |                                     |
| GET    | `/api/admin/update/config`          | `GetUpdateConfig`         |                                     |
| PUT    | `/api/admin/update/config`          | `SetUpdateConfig`         | Body: `{ update_method?, branch? }` |

---

### Server Lifecycle

| Method | Path                         | Handler          |
|--------|------------------------------|------------------|
| POST   | `/api/admin/server/restart`  | `RestartServer`  |
| POST   | `/api/admin/server/shutdown` | `ShutdownServer` |

---

### Downloader

| Method | Path                                        | Handler                         | Notes                                                                    |
|--------|---------------------------------------------|---------------------------------|--------------------------------------------------------------------------|
| GET    | `/api/admin/downloader/health`              | `AdminDownloaderHealth`         |                                                                          |
| GET    | `/api/admin/downloader/verify`              | `AdminDownloaderVerify`         | Lightweight admin session check                                          |
| POST   | `/api/admin/downloader/detect`              | `AdminDownloaderDetect`         | Body: `{ url }`                                                          |
| POST   | `/api/admin/downloader/download`            | `AdminDownloaderDownload`       | Body: `{ url, title?, clientId, isYouTube?, isYouTubeMusic?, relayId? }` |
| POST   | `/api/admin/downloader/cancel/:id`          | `AdminDownloaderCancel`         |                                                                          |
| GET    | `/api/admin/downloader/downloads`           | `AdminDownloaderListDownloads`  |                                                                          |
| DELETE | `/api/admin/downloader/downloads/:filename` | `AdminDownloaderDeleteDownload` |                                                                          |
| GET    | `/api/admin/downloader/settings`            | `AdminDownloaderSettings`       |                                                                          |
| GET    | `/api/admin/downloader/importable`          | `AdminDownloaderImportable`     |                                                                          |
| POST   | `/api/admin/downloader/import`              | `AdminDownloaderImport`         | Body: `{ filename, delete_source?, trigger_scan? }`                      |

---

### Metrics

| Method | Path       | Handler      | Auth  |
|--------|------------|--------------|-------|
| GET    | `/metrics` | `GetMetrics` | Admin |

Returns Prometheus text/plain metrics.

---

## Receiver Slave Endpoints (X-API-Key Auth)

These require a valid receiver API key via `X-API-Key` header or `api_key` query param.

| Method | Path                               | Handler                 | Notes                               |
|--------|------------------------------------|-------------------------|-------------------------------------|
| POST   | `/api/receiver/register`           | `ReceiverRegisterSlave` | Body: RegisterRequest               |
| POST   | `/api/receiver/catalog`            | `ReceiverPushCatalog`   | Body: CatalogPushRequest (max 32MB) |
| POST   | `/api/receiver/heartbeat`          | `ReceiverHeartbeat`     | Body: `{ slave_id }`                |
| POST   | `/api/receiver/stream-push/:token` | `ReceiverStreamPush`    | File body, token must be UUID       |

---

## WebSocket Endpoints

| Path                   | Auth          | Handler                    | Notes                         |
|------------------------|---------------|----------------------------|-------------------------------|
| `/ws/receiver`         | X-API-Key     | `ReceiverWebSocket`        | Slave node connection         |
| `/ws/admin/downloader` | Admin session | `AdminDownloaderWebSocket` | Proxies to downloader service |

---

## Handler File Index

| File                           | Scope                                                                                                   |
|--------------------------------|---------------------------------------------------------------------------------------------------------|
| `handler.go`                   | Handler struct, deps, helpers (`getSession`, `getUser`, `resolveMediaByID`, `checkMatureAccess`)        |
| `response.go`                  | `writeSuccess`, `writeError`                                                                            |
| `params.go`                    | `BindJSON`, `ParseQueryInt`, `ParseLimitOffset`, `RequireParamID`, `RequireSession`                     |
| `auth.go`                      | Login, Logout, Register, Session, Permissions, Preferences, WatchHistory, ChangePassword, DeleteAccount |
| `auth_tokens.go`               | API token CRUD                                                                                          |
| `media.go`                     | ListMedia, GetMedia, StreamMedia, DownloadMedia, Playback, BatchMedia                                   |
| `playlists.go`                 | User playlist CRUD                                                                                      |
| `analytics.go`                 | Analytics summary, daily, top, events, export                                                           |
| `suggestions.go`               | Suggestions, trending, similar, continue watching, ratings, on-deck                                     |
| `hls.go`                       | HLS capabilities, generate, serve, admin HLS                                                            |
| `thumbnails.go`                | Thumbnail serving, previews, batch                                                                      |
| `favorites.go`                 | Favorites (watch later)                                                                                 |
| `upload.go`                    | File upload, progress                                                                                   |
| `feed.go`                      | Atom/RSS feed                                                                                           |
| `system.go`                    | Version, health, metrics, server settings, storage, database, OpenAPI                                   |
| `deletion_requests.go`         | Data deletion requests                                                                                  |
| `admin.go`                     | Admin stats, system info                                                                                |
| `admin_activity.go`            | Active streams, uploads, user sessions                                                                  |
| `admin_audit.go`               | Audit log, export                                                                                       |
| `admin_backups.go`             | Backup CRUD                                                                                             |
| `admin_categorizer.go`         | Categorizer operations                                                                                  |
| `admin_classify.go`            | Hugging Face classification                                                                             |
| `admin_config.go`              | Config get/update                                                                                       |
| `admin_discovery.go`           | Auto-discovery                                                                                          |
| `admin_downloader.go`          | Downloader operations                                                                                   |
| `admin_lifecycle.go`           | Server restart/shutdown                                                                                 |
| `admin_logs.go`                | Server log viewer                                                                                       |
| `admin_media.go`               | Admin media CRUD, bulk ops                                                                              |
| `admin_playlists.go`           | Admin playlist management                                                                               |
| `admin_receiver.go`            | Receiver slave management, WebSocket                                                                    |
| `admin_receiver_duplicates.go` | Duplicate detection                                                                                     |
| `admin_remote.go`              | Remote media sources                                                                                    |
| `admin_scanner.go`             | Content moderation scanner                                                                              |
| `admin_security.go`            | IP whitelist/blacklist, bans                                                                            |
| `admin_tasks.go`               | Background task management                                                                              |
| `admin_thumbnails.go`          | Thumbnail cleanup                                                                                       |
| `admin_updates.go`             | Update management                                                                                       |
| `admin_users.go`               | User CRUD, bulk ops                                                                                     |
| `admin_validator.go`           | Media validator                                                                                         |
| `crawler.go`                   | Web crawler management                                                                                  |
| `extractor.go`                 | M3U8 stream extractor                                                                                   |
