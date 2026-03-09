# Repository API & Structure Report — Media-Server-Pro-4

## 1. Project type, tech stack, entry points

| Aspect | Details |
|--------|--------|
| **Project type** | Monorepo: Go backend (media server) + React SPA frontend; optional Go slave binary. |
| **Backend** | Go 1.26, **Gin** (`github.com/gin-gonic/gin`), GORM/MySQL, gorilla/websocket. |
| **Frontend** | React 19, TypeScript, Vite 7, React Router 7, TanStack Query, Zustand, HLS.js. |
| **Backend entry** | `cmd/server/main.go` — main server. |
| **Slave entry** | `cmd/media-receiver/main.go` — slave that connects to master via WebSocket. |
| **Frontend entry** | `web/frontend/src/main.tsx` → `App.tsx`; build output: `web/static/react/` (embedded in Go). |
| **Route registration** | `api/routes/routes.go` — `Setup()`; `internal/server/server.go` — `setupBaseRoutes()`; `web/server.go` — SPA/static (called from routes). |

---

## 2. API boundary indicators

### Backend (route registration)

- **Gin routes**: `r.GET`, `r.POST`, `r.PUT`, `r.DELETE`, `r.HEAD` in:
  - **D:\Media-Server-Pro-4\api\routes\routes.go** — all `/api` and direct (media, HLS, health, ws) routes.
  - **D:\Media-Server-Pro-4\web\server.go** — SPA paths and `/web/static/*filepath`.
  - **D:\Media-Server-Pro-4\internal\server\server.go** — `/api/status`, `/api/modules`, `/api/modules/:name/health`.

### Frontend (HTTP client)

- **Fetch**: `web/frontend/src/api/client.ts` — `apiRequest()` / `fetch()` with envelope unwrap.
- **API usage**: `web/frontend/src/api/endpoints.ts` — all `api.get/post/put/delete/upload` and two raw `fetch()` (playlist export, analytics export).
- **Direct POST**: `web/frontend/src/pages/index/IndexPage.tsx` — `xhr.open('POST', '/api/upload', true)` for upload progress.

### WebSocket

- **Backend**: `api/handlers/admin_receiver.go` — `ReceiverWebSocket`; `internal/receiver/wsconn.go` — `HandleWebSocket`; protocol in `internal/receiver/wsconn.go` (e.g. `register`, `catalog`, `heartbeat`, `stream_request`).
- **Slave client**: `cmd/media-receiver/main.go` — WebSocket client to master (`wss://` from master URL).

### No GraphQL or gRPC

- No GraphQL resolvers or gRPC services found.

---

## 3. File locations

| Category | File path | Description |
|----------|-----------|-------------|
| **Route/handler definitions** | `api/routes/routes.go` | Central route setup and middleware (Gin). |
| | `internal/server/server.go` | Registers `/api/status`, `/api/modules`, `/api/modules/:name/health`. |
| | `web/server.go` | SPA and static routes; `RegisterStaticRoutes()` used from routes. |
| | `api/handlers/*.go` | Handler implementations (see list below). |
| **API client** | `web/frontend/src/api/client.ts` | Typed fetch wrapper, envelope handling. |
| | `web/frontend/src/api/endpoints.ts` | Domain API functions (auth, media, playlists, admin, etc.). |
| **Schema/DTO/types** | `pkg/models/models.go` | Go DTOs (MediaItem, User, Playlist, etc.). |
| | `web/frontend/src/api/types.ts` | TypeScript types aligned with backend JSON. |
| **Middleware** | `pkg/middleware/middleware.go` | Request ID, security headers, CORS. |
| | `pkg/middleware/agegate.go` | Age-gate status/verify handlers. |
| | `api/routes/routes.go` | sessionAuth, adminAuth, requireAuth, ginETags; applies security middleware. |
| | `internal/security/security.go` | `GinMiddleware()` — rate limiting, IP filtering. |
| **Gateway / reverse proxy** | `web/frontend/vite.config.ts` | Dev proxy: `/api`, `/media`, `/hls`, etc. → `http://localhost:8080`. |

**Handler files (api/handlers):**

- `handler.go`, `handlers.go` — core handler struct and wiring  
- `auth.go`, `media.go`, `playlists.go`, `thumbnails.go`, `upload.go`, `hls.go`, `system.go`  
- `analytics.go`, `suggestions.go`, `extractor.go`, `crawler.go`  
- `admin.go`, `admin_media.go`, `admin_playlists.go`, `admin_receiver.go`, `admin_receiver_duplicates.go`  
- `admin_backups.go`, `admin_categorizer.go`, `admin_discovery.go`, `admin_hls.go`, `admin_remote.go`  
- `admin_scanner.go`, `admin_security.go`, `admin_thumbnails.go`, `admin_validator.go`  
- `restart_unix.go`, `restart_windows.go`  

---

## 4. OpenAPI / Swagger / GraphQL schema

| File | Description |
|------|-------------|
| **D:\Media-Server-Pro-4\docs\openapi.yaml** | OpenAPI 3.0.3; title "Media Server Pro API"; servers `url: /`; documents auth, media, streaming, HLS, playback, playlists, analytics, watch-history, suggestions, upload, preferences, permissions, settings, age-gate, ratings, thumbnails, storage, system, admin-* tags and paths. |

No Swagger UI or GraphQL schema files found.

---

## 5. Environment / config (base URLs, API version, route prefixes)

| Location | Purpose |
|----------|---------|
| **internal/config/config.go** | Config struct (Server, Directories, Streaming, Security, Auth, HLS, etc.); JSON + env; no explicit API version or route prefix. |
| **web/frontend/vite.config.ts** | Dev proxy target: `http://localhost:8080`; no base URL or API version in frontend code. |
| **api/routes/routes.go** | Route prefix: `/api` for JSON API; direct paths `/media`, `/download`, `/hls/`, `/thumbnail`, `/thumbnails/`, `/health`, `/metrics`, `/remote/stream`, `/extractor/hls/`, `/ws/receiver`. |
| **internal/receiver/receiver.go** | `baseURL` from slave config (e.g. master URL); `ws-connected` for WebSocket-connected slaves. |
| **internal/updater/updater.go** | `GitHubAPI = "https://api.github.com"` for updates. |
| **CLAUDE.md** | Documents HLS CDN base URL (`HLS_CDN_BASE_URL`) and security/CORS. |

No `.env` or `.env.example` in the repo; config is JSON + env vars (e.g. `UPDATER_GITHUB_TOKEN`) as referenced in config.

---

## Backend route definitions (path, method, file)

**Direct routes (no `/api` prefix):**

| Path | Method | File |
|------|--------|------|
| `/media` | GET | api/routes/routes.go → h.StreamMedia |
| `/download` | GET | api/routes/routes.go → h.DownloadMedia |
| `/hls/:id/master.m3u8` | GET | api/routes/routes.go → h.ServeMasterPlaylist |
| `/hls/:id/:quality/playlist.m3u8` | GET | api/routes/routes.go → h.ServeVariantPlaylist |
| `/hls/:id/:quality/:segment` | GET | api/routes/routes.go → h.ServeSegment |
| `/thumbnail` | GET, HEAD | api/routes/routes.go → h.GetThumbnail |
| `/thumbnails/:filename` | GET, HEAD | api/routes/routes.go → h.ServeThumbnailFile |
| `/health` | GET | api/routes/routes.go → h.GetHealth |
| `/metrics` | GET | api/routes/routes.go → h.GetMetrics (adminAuth) |
| `/remote/stream` | GET | api/routes/routes.go → h.StreamRemoteMedia (requireAuth) |
| `/extractor/hls/:id/master.m3u8` | GET | api/routes/routes.go → h.ExtractorHLSMaster |
| `/extractor/hls/:id/:quality/playlist.m3u8` | GET | api/routes/routes.go → h.ExtractorHLSVariant |
| `/extractor/hls/:id/:quality/:segment` | GET | api/routes/routes.go → h.ExtractorHLSSegment |
| `/ws/receiver` | GET | api/routes/routes.go → h.ReceiverWebSocket |

**Internal server (module status):**

| Path | Method | File |
|------|--------|------|
| `/api/status` | GET | internal/server/server.go → handleStatus |
| `/api/modules` | GET | internal/server/server.go → handleModules |
| `/api/modules/:name/health` | GET | internal/server/server.go → handleModuleHealth |

**API group `/api`:**  
All below are in **api/routes/routes.go**; handler names map to **api/handlers/*.go**.

- `/api/media` GET → ListMedia  
- `/api/media/stats` GET → GetMediaStats  
- `/api/media/categories` GET → GetCategories  
- `/api/media/:id` GET → GetMedia  
- `/api/playback` GET/POST → GetPlaybackPosition, TrackPlayback (requireAuth)  
- `/api/hls/capabilities` GET → GetHLSCapabilities  
- `/api/hls/check` GET → CheckHLSAvailability (requireAuth)  
- `/api/hls/generate` POST → GenerateHLS (requireAuth)  
- `/api/hls/status/:id` GET → GetHLSStatus  
- `/api/auth/login` POST → Login  
- `/api/auth/logout` POST → Logout  
- `/api/auth/register` POST → Register  
- `/api/auth/session` GET → CheckSession  
- `/api/permissions` GET → GetPermissions  
- `/api/storage-usage` GET → GetStorageUsage (requireAuth)  
- `/api/server-settings` GET → GetServerSettings  
- `/api/age-gate/status` GET → ageGate.GinStatusHandler  
- `/api/age-verify` POST → ageGate.GinVerifyHandler  
- `/api/preferences` GET/POST → GetPreferences, UpdatePreferences (requireAuth)  
- `/api/auth/change-password` POST → ChangePassword (requireAuth)  
- `/api/auth/delete-account` POST → DeleteAccount (requireAuth)  
- `/api/watch-history` GET/DELETE → GetWatchHistory, ClearWatchHistory (requireAuth)  
- `/api/playlists` GET/POST → ListPlaylists, CreatePlaylist (requireAuth)  
- `/api/playlists/:id` GET/DELETE/PUT → GetPlaylist, DeletePlaylist, UpdatePlaylist (requireAuth)  
- `/api/playlists/:id/export` GET → ExportPlaylist (requireAuth)  
- `/api/playlists/:id/items` POST/DELETE → AddPlaylistItem, RemovePlaylistItem (requireAuth)  
- `/api/playlists/:id/reorder` PUT → ReorderPlaylistItems (requireAuth)  
- `/api/playlists/:id/clear` DELETE → ClearPlaylist (requireAuth)  
- `/api/playlists/:id/copy` POST → CopyPlaylist (requireAuth)  
- `/api/analytics` GET → GetAnalyticsSummary (adminAuth)  
- `/api/analytics/daily` GET → GetDailyStats (adminAuth)  
- `/api/analytics/top` GET → GetTopMedia (adminAuth)  
- `/api/analytics/events` POST → SubmitEvent (requireAuth)  
- `/api/analytics/events/stats` GET → GetEventStats (adminAuth)  
- `/api/analytics/events/by-type` GET → GetEventsByType (adminAuth)  
- `/api/analytics/events/by-media` GET → GetEventsByMedia (adminAuth)  
- `/api/analytics/events/counts` GET → GetEventTypeCounts (adminAuth)  
- `/api/thumbnails/previews` GET → GetThumbnailPreviews  
- `/api/suggestions` GET → GetSuggestions  
- `/api/suggestions/trending` GET → GetTrendingSuggestions  
- `/api/suggestions/similar` GET → GetSimilarMedia  
- `/api/suggestions/continue` GET → GetContinueWatching (requireAuth)  
- `/api/suggestions/personalized` GET → GetPersonalizedSuggestions (requireAuth)  
- `/api/ratings` POST → RecordRating (requireAuth)  
- `/api/upload` POST → UploadMedia (requireAuth)  
- `/api/upload/:id/progress` GET → GetUploadProgress (requireAuth)  
- `/api/receiver/register` POST → ReceiverRegisterSlave  
- `/api/receiver/catalog` POST → ReceiverPushCatalog  
- `/api/receiver/heartbeat` POST → ReceiverHeartbeat  
- `/api/receiver/stream-push/:token` POST → ReceiverStreamPush  
- `/api/receiver/media` GET → ReceiverListMedia (adminAuth)  
- `/api/receiver/media/:id` GET → ReceiverGetMedia (adminAuth)  

**Admin group `/api/admin` (adminAuth):**  
Same file; only path and handler names are listed.

- `/api/admin/stats`, `/api/admin/system`, `/api/admin/cache/clear`  
- `/api/admin/update/*` (check, status, apply, source/check, source/apply, source/progress, config GET/PUT)  
- `/api/admin/server/restart`, `/api/admin/server/shutdown`  
- `/api/admin/streams`, `/api/admin/uploads/active`  
- `/api/admin/users` GET/POST, `/api/admin/users/bulk` POST, `/api/admin/users/:username` GET/PUT/DELETE, `/api/admin/users/:username/password` POST, `/api/admin/users/:username/sessions` GET  
- `/api/admin/change-password` POST  
- `/api/admin/audit-log` GET, `/api/admin/audit-log/export` GET  
- `/api/admin/logs` GET, `/api/admin/analytics/export` GET  
- `/api/admin/config` GET/PUT  
- `/api/admin/tasks` GET, `/api/admin/tasks/:id/run|enable|disable|stop` POST  
- `/api/admin/playlists` GET, `/api/admin/playlists/stats` GET, `/api/admin/playlists/bulk` POST, `/api/admin/playlists/:id` DELETE  
- `/api/admin/media` GET, `/api/admin/media/scan` POST, `/api/admin/media/bulk` POST, `/api/admin/media/:id` PUT/DELETE  
- `/api/admin/scanner/scan` POST, `/api/admin/scanner/stats` GET, `/api/admin/scanner/queue` GET/POST/DELETE, `/api/admin/scanner/approve|reject/:id` POST  
- `/api/admin/thumbnails/generate` POST, `/api/admin/thumbnails/stats` GET  
- `/api/admin/hls/stats` GET, `/api/admin/hls/jobs` GET, `/api/admin/hls/jobs/:id` DELETE, `/api/admin/hls/validate/:id` GET, `/api/admin/hls/clean/locks|inactive` POST  
- `/api/admin/validator/validate` POST, `/api/admin/validator/fix` POST, `/api/admin/validator/stats` GET  
- `/api/admin/database/status` GET, `/api/admin/database/query` POST  
- `/api/admin/backups/v2` GET/POST, `/api/admin/backups/v2/:id/restore` POST, `/api/admin/backups/v2/:id` DELETE  
- `/api/admin/discovery/scan` POST, `/api/admin/discovery/suggestions` GET, `/api/admin/discovery/apply` POST, `/api/admin/discovery/*path` DELETE  
- `/api/admin/suggestions/stats` GET  
- `/api/admin/security/stats` GET, `/api/admin/security/whitelist` GET/POST/DELETE, `/api/admin/security/blacklist` GET/POST/DELETE, `/api/admin/security/banned` GET, `/api/admin/security/ban|unban` POST  
- `/api/admin/categorizer/file` POST, `/api/admin/categorizer/directory` POST, `/api/admin/categorizer/stats` GET, `/api/admin/categorizer/set` POST, `/api/admin/categorizer/by-category` GET, `/api/admin/categorizer/clean` POST  
- `/api/admin/remote/sources` GET/POST, `/api/admin/remote/stats` GET, `/api/admin/remote/media` GET, `/api/admin/remote/sources/:source/media` GET, `/api/admin/remote/sources/:source/sync` POST, `/api/admin/remote/sources/:source` DELETE, `/api/admin/remote/cache` POST, `/api/admin/remote/cache/clean` POST  
- `/api/admin/extractor/items` GET/POST, `/api/admin/extractor/items/:id` DELETE, `/api/admin/extractor/stats` GET  
- `/api/admin/crawler/targets` GET/POST, `/api/admin/crawler/targets/:id` DELETE, `/api/admin/crawler/targets/:id/crawl` POST, `/api/admin/crawler/discoveries` GET, `/api/admin/crawler/discoveries/:id/approve|ignore` POST, `/api/admin/crawler/discoveries/:id` DELETE, `/api/admin/crawler/stats` GET  
- `/api/admin/receiver/slaves` GET, `/api/admin/receiver/stats` GET, `/api/admin/receiver/slaves/:id` DELETE  
- `/api/admin/duplicates` GET, `/api/admin/duplicates/:id/resolve` POST  

**SPA/static (web/server.go):**

- `/`, `/login`, `/signup`, `/admin-login`, `/profile`, `/player`, `/admin` → SPA  
- `/web/static/*filepath` → static files  

---

## Frontend / client call sites (path, method, file)

All go through **web/frontend/src/api/endpoints.ts** (and **web/frontend/src/api/client.ts**) unless noted.

| Path / usage | Method | File |
|-------------|--------|------|
| `/api/storage-usage` | GET | endpoints.ts — storageApi.getUsage |
| `/api/permissions` | GET | endpoints.ts — permissionsApi.get |
| `/api/ratings` | POST | endpoints.ts — ratingsApi.record |
| `/api/upload` | POST (FormData) | endpoints.ts — uploadApi.upload; IndexPage.tsx — xhr POST for progress |
| `/api/upload/:id/progress` | GET | endpoints.ts — uploadApi.getProgress |
| `/api/auth/login` | POST | endpoints.ts — authApi.login |
| `/api/auth/logout` | POST | endpoints.ts — authApi.logout |
| `/api/auth/register` | POST | endpoints.ts — authApi.register |
| `/api/auth/session` | GET | endpoints.ts — authApi.getSession |
| `/api/auth/change-password` | POST | endpoints.ts — authApi.changePassword |
| `/api/auth/delete-account` | POST | endpoints.ts — authApi.deleteAccount |
| `/api/preferences` | GET/POST | endpoints.ts — preferencesApi.get, update |
| `/api/media` (query params) | GET | endpoints.ts — mediaApi.list |
| `/api/media/:id` | GET | endpoints.ts — mediaApi.get |
| `/api/media/stats` | GET | endpoints.ts — mediaApi.getStats |
| `/api/media/categories` | GET | endpoints.ts — mediaApi.getCategories |
| `/api/thumbnails/previews` | GET | endpoints.ts — mediaApi.getThumbnailPreviews |
| `/api/hls/capabilities` | GET | endpoints.ts — hlsApi.getCapabilities |
| `/api/hls/check` | GET | endpoints.ts — hlsApi.check |
| `/api/hls/generate` | POST | endpoints.ts — hlsApi.generate |
| `/api/hls/status/:id` | GET | endpoints.ts — hlsApi.getStatus |
| `/api/playlists` | GET/POST | endpoints.ts — playlistApi.list, create |
| `/api/playlists/:id` | GET/DELETE/PUT | endpoints.ts — playlistApi.get, delete, update |
| `/api/playlists/:id/export` | GET | endpoints.ts — playlistApi.export (raw fetch) |
| `/api/playlists/:id/items` | POST/DELETE | endpoints.ts — playlistApi.addItem, removeItem |
| `/api/playlists/:id/reorder` | PUT | endpoints.ts — playlistApi.reorderItems |
| `/api/playlists/:id/clear` | DELETE | endpoints.ts — playlistApi.clear |
| `/api/playlists/:id/copy` | POST | endpoints.ts — playlistApi.copy |
| `/api/analytics` | GET | endpoints.ts — analyticsApi.getSummary |
| `/api/analytics/events` | POST | endpoints.ts — analyticsApi.trackEvent |
| `/api/watch-history` | GET/DELETE | endpoints.ts — watchHistoryApi.list, clear |
| `/api/watch-history?id=` | GET/DELETE | endpoints.ts — watchHistoryApi.getEntry, delete |
| `/api/playback` | GET/POST | endpoints.ts — watchHistoryApi.getPosition, trackPosition |
| `/api/server-settings` | GET | endpoints.ts — settingsApi.getServerSettings |
| `/api/age-gate/status` | GET | endpoints.ts — ageGateApi.getStatus |
| `/api/age-verify` | POST | endpoints.ts — ageGateApi.verify |
| `/api/suggestions` | GET | endpoints.ts — suggestionsApi.get |
| `/api/suggestions/trending` | GET | endpoints.ts — suggestionsApi.getTrending |
| `/api/suggestions/similar` | GET | endpoints.ts — suggestionsApi.getSimilar |
| `/api/suggestions/continue` | GET | endpoints.ts — suggestionsApi.getContinueWatching |
| `/api/suggestions/personalized` | GET | endpoints.ts — suggestionsApi.getPersonalized |
| All admin routes under `/api/admin/*` | GET/POST/PUT/DELETE | endpoints.ts — adminApi.* |
| `/api/admin/analytics/export` | GET | endpoints.ts — adminApi.exportAnalytics (raw fetch) |
| `/api/receiver/media` | GET | endpoints.ts — receiverApi.listMedia |
| `/api/receiver/media/:id` | GET | endpoints.ts — receiverApi.getMedia |

**URL-only (no fetch from endpoints):**  
mediaApi.getStreamUrl, getDownloadUrl, getRemoteStreamUrl, getThumbnailUrl; hlsApi.getMasterPlaylistUrl — used by pages/components to set `src` or links.

---

## Schema / type definitions

| File | Description |
|------|-------------|
| **pkg/models/models.go** | Go: MediaItem, MediaCategory, User, UserRole, UserPermissions, UserPreferences, Session, WatchHistoryItem, Playlist, PlaylistItem, and other request/response structs. |
| **web/frontend/src/api/types.ts** | TypeScript: User, UserPermissions, UserPreferences, SessionCheckResponse, LoginResponse, MediaCategory, MediaItem, MediaListResponse, and 60+ interfaces matching backend. |
| **docs/openapi.yaml** | OpenAPI 3.0.3 schema with components/schemas and path specs. |
| **internal/receiver/wsconn.go** | WebSocket message types (e.g. wsMessage, register, catalog, heartbeat). |

---