# Backend API Summary
> Go/Gin · v0.115.0 · 215+ routes across 14 categories
> All JSON responses use envelope: `{ "success": bool, "data": T, "message": string }`
> Auth: `session_id` cookie (HttpOnly, Strict SameSite)

## Auth Tiers

| Tier | Mechanism | Description |
|------|-----------|-------------|
| **Public** | None | Anyone can call, including unauthenticated browsers |
| **Auth** | `session_id` cookie | Valid, non-expired session for any enabled user |
| **Admin** | `session_id` cookie | Session where `role = admin` and account is enabled |
| **API Key** | `X-API-Key` header | Receiver slave nodes only |

---

## 1. System / Health (Public)

| Method | Path | Auth | Description |
|--------|------|------|-------------|
| `GET` | `/health` | Public | Liveness probe — `200 OK` or `503` when critical modules fail |
| `GET` | `/api/version` | Public | Server version string |
| `GET` | `/api/server-settings` | Public | Feature flags + UI config (safe subset, no secrets) |
| `GET` | `/api/permissions` | Public | Current caller's capabilities (richer when authenticated) |
| `GET` | `/api/age-gate/status` | Public | Whether age-gate is enabled and verified for this session |
| `POST` | `/api/age-verify` | Public | Submit age confirmation (sets verified cookie) |
| `GET` | `/metrics` | Admin | Prometheus metrics scrape endpoint |

---

## 2. Authentication

| Method | Path | Auth | Body / Params | Description |
|--------|------|------|---------------|-------------|
| `POST` | `/api/auth/login` | Public | `{ username, password }` | Login — sets `session_id` cookie, returns `{ username, role, is_admin, expires_at }` |
| `POST` | `/api/auth/logout` | Public | — | Clears session cookie |
| `POST` | `/api/auth/register` | Public | `{ username, password, email? }` | Self-register (if enabled) |
| `GET` | `/api/auth/session` | Public | — | Session check — returns `{ authenticated, allow_guests, user? }` |
| `POST` | `/api/auth/change-password` | Auth | `{ current_password, new_password }` | Self-service password change |
| `POST` | `/api/auth/delete-account` | Auth | `{ password }` | Permanently delete own account |

---

## 3. Media Library (Mostly Public)

| Method | Path | Auth | Params | Description |
|--------|------|------|--------|-------------|
| `GET` | `/api/media` | Public | `search`, `type`, `category`, `sort`, `sort_order`, `limit`, `offset`, `mature` | Paginated media list. Returns `{ items[], total_items, total_pages, scanning, initializing }` |
| `GET` | `/api/media/stats` | Public | — | Total counts + sizes + last scan time |
| `GET` | `/api/media/categories` | Public | — | Category list with item counts |
| `GET` | `/api/media/:id` | Public | — | Single media item by UUID |

---

## 4. Media Streaming (Direct, Non-JSON)

These are file-serving routes, not under `/api`. Excluded from gzip and ETag middleware.

| Method | Path | Auth | Params | Description |
|--------|------|------|--------|-------------|
| `GET` | `/media` | Config-dependent | `id=<uuid>` | Stream media file (byte-range aware) |
| `GET` | `/download` | Config-dependent | `id=<uuid>` | Force-download media file |
| `GET` | `/thumbnail` | Public | `id=<uuid>` | Serve thumbnail image (WebP/JPEG) |
| `HEAD` | `/thumbnail` | Public | `id=<uuid>` | Check thumbnail existence |
| `GET` | `/thumbnails/:filename` | Public | — | Serve pre-generated thumbnail file by name |
| `HEAD` | `/thumbnails/:filename` | Public | — | Check thumbnail file existence |

---

## 5. HLS Streaming (Direct, Non-JSON)

High-frequency; excluded from rate limiting and gzip. Auth not required for segment serving.

| Method | Path | Auth | Description |
|--------|------|------|-------------|
| `GET` | `/hls/:id/master.m3u8` | Public | HLS master playlist |
| `GET` | `/hls/:id/:quality/playlist.m3u8` | Public | HLS variant playlist for a quality level |
| `GET` | `/hls/:id/:quality/:segment` | Public | HLS segment file (`.ts`) |

---

## 6. HLS API (Auth Required)

| Method | Path | Auth | Body / Params | Description |
|--------|------|------|---------------|-------------|
| `GET` | `/api/hls/capabilities` | Auth | — | ffmpeg availability, qualities list, auto_generate setting |
| `GET` | `/api/hls/check` | Auth | `id=<uuid>` | HLS availability + job progress for a media item |
| `POST` | `/api/hls/generate` | Auth | `{ id, quality? }` | Start on-demand HLS transcoding job |
| `GET` | `/api/hls/status/:id` | Auth | — | Poll job status by job UUID |

---

## 7. Playback Position (Auth Required)

| Method | Path | Auth | Body / Params | Description |
|--------|------|------|---------------|-------------|
| `GET` | `/api/playback` | Auth | `id=<uuid>` | Retrieve saved position for a media item |
| `POST` | `/api/playback` | Auth | `{ id, position, duration }` | Save current playback position |

---

## 8. User Preferences (Auth Required)

| Method | Path | Auth | Body | Description |
|--------|------|------|------|-------------|
| `GET` | `/api/preferences` | Auth | — | Fetch full `UserPreferences` object |
| `POST` | `/api/preferences` | Auth | `Partial<UserPreferences>` | Partial merge update (backend handles defaults) |
| `GET` | `/api/storage-usage` | Auth | — | Used/quota GB + percentage |

---

## 9. Watch History (Auth Required)

| Method | Path | Auth | Params | Description |
|--------|------|------|--------|-------------|
| `GET` | `/api/watch-history` | Auth | `limit?` | List recent watch history items |
| `DELETE` | `/api/watch-history` | Auth | `id=<media_uuid>` | Remove single entry; no query param = clear all |

---

## 10. Suggestions & Ratings

| Method | Path | Auth | Params | Description |
|--------|------|------|--------|-------------|
| `GET` | `/api/suggestions` | Public | — | General curated suggestions |
| `GET` | `/api/suggestions/trending` | Public | — | Trending media |
| `GET` | `/api/suggestions/similar` | Public | `id=<uuid>` | Similar items to given media |
| `GET` | `/api/suggestions/continue` | Auth | — | Continue-watching list for current user |
| `GET` | `/api/suggestions/personalized` | Auth | `limit?` | Personalized recommendations |
| `POST` | `/api/ratings` | Auth | `{ id, rating }` | Record star rating (1–5) |

---

## 11. Thumbnails API (Public)

| Method | Path | Auth | Params | Description |
|--------|------|------|--------|-------------|
| `GET` | `/api/thumbnails/previews` | Public | `id=<uuid>` | List of thumbnail preview URLs for seek-bar hover |
| `GET` | `/api/thumbnails/batch` | Public | `ids=id1,id2,...&w=<px>` | Batch thumbnail URL map (max 50 IDs) |

---

## 12. Playlists (Auth Required)

| Method | Path | Auth | Body / Params | Description |
|--------|------|------|---------------|-------------|
| `GET` | `/api/playlists` | Auth | — | List all playlists for current user |
| `POST` | `/api/playlists` | Auth | `{ name, description?, is_public? }` | Create playlist |
| `GET` | `/api/playlists/:id` | Auth | — | Get full playlist with items |
| `PUT` | `/api/playlists/:id` | Auth | `Partial<Playlist>` | Update playlist metadata |
| `DELETE` | `/api/playlists/:id` | Auth | — | Delete playlist |
| `GET` | `/api/playlists/:id/export` | Auth | `format=json|m3u|m3u8` | Export playlist (redirects to file) |
| `POST` | `/api/playlists/:id/items` | Auth | `{ media_id }` | Add media item to playlist |
| `DELETE` | `/api/playlists/:id/items` | Auth | `media_id=` or `item_id=` | Remove item from playlist |
| `PUT` | `/api/playlists/:id/reorder` | Auth | `{ positions: number[] }` | Reorder playlist items |
| `DELETE` | `/api/playlists/:id/clear` | Auth | — | Remove all items from playlist |
| `POST` | `/api/playlists/:id/copy` | Auth | `{ name }` | Duplicate playlist under new name |

---

## 13. Upload (Auth Required)

| Method | Path | Auth | Body | Description |
|--------|------|------|------|-------------|
| `POST` | `/api/upload` | Auth | `multipart/form-data: files[], category?` | Upload one or more media files |
| `GET` | `/api/upload/:id/progress` | Auth | — | Poll upload processing progress |

---

## 14. Remote Streaming (Auth Required)

| Method | Path | Auth | Params | Description |
|--------|------|------|--------|-------------|
| `GET` | `/remote/stream` | Auth | `url=<encoded>&source=<name>` | Proxy-stream a remote media URL through the server |

---

## 15. Analytics (Mixed Auth)

| Method | Path | Auth | Params | Description |
|--------|------|------|--------|-------------|
| `POST` | `/api/analytics/events` | Auth | `{ type, media_id, duration?, data? }` | Submit a playback event |
| `GET` | `/api/analytics` | Admin | `period?` | Summary: total views, active sessions, top media |
| `GET` | `/api/analytics/daily` | Admin | `days?` | Per-day view counts |
| `GET` | `/api/analytics/top` | Admin | `limit?` | Top-viewed media items |
| `GET` | `/api/analytics/events/stats` | Admin | — | Aggregate event counts by type |
| `GET` | `/api/analytics/events/by-type` | Admin | `type, limit?` | Event list filtered by type |
| `GET` | `/api/analytics/events/by-media` | Admin | `media_id, limit?` | Events for a specific media item |
| `GET` | `/api/analytics/events/by-user` | Admin | `user_id, limit?` | Events for a specific user |
| `GET` | `/api/analytics/events/counts` | Admin | — | Map of event-type → count |
| `GET` | `/api/admin/analytics/export` | Admin | — | Download analytics as CSV |

---

## 16. Admin — Dashboard & Server

| Method | Path | Auth | Body / Params | Description |
|--------|------|------|---------------|-------------|
| `GET` | `/api/admin/stats` | Admin | — | Videos, audio, users, sessions, disk usage, HLS jobs |
| `GET` | `/api/admin/system` | Admin | — | OS, arch, Go version, uptime, CPU, memory, module health |
| `GET` | `/api/admin/streams` | Admin | — | Currently active stream sessions |
| `GET` | `/api/admin/uploads/active` | Admin | — | In-progress upload jobs |
| `POST` | `/api/admin/cache/clear` | Admin | — | Clear media metadata cache |
| `POST` | `/api/admin/server/restart` | Admin | — | Graceful server restart |
| `POST` | `/api/admin/server/shutdown` | Admin | — | Server shutdown |
| `GET` | `/api/status` | Admin | — | Server running status + uptime |
| `GET` | `/api/modules` | Admin | — | All registered module names and statuses |
| `GET` | `/api/modules/:name/health` | Admin | — | Single module health detail |

---

## 17. Admin — Users

| Method | Path | Auth | Body / Params | Description |
|--------|------|------|---------------|-------------|
| `GET` | `/api/admin/users` | Admin | — | List all users |
| `POST` | `/api/admin/users` | Admin | `{ username, password, email?, role }` | Create user |
| `GET` | `/api/admin/users/:username` | Admin | — | Get user details |
| `PUT` | `/api/admin/users/:username` | Admin | `Partial<User>` | Update user |
| `DELETE` | `/api/admin/users/:username` | Admin | — | Delete user |
| `POST` | `/api/admin/users/:username/password` | Admin | `{ new_password }` | Reset user password |
| `GET` | `/api/admin/users/:username/sessions` | Admin | — | List active sessions for user |
| `POST` | `/api/admin/users/bulk` | Admin | `{ usernames[], action: delete|enable|disable }` | Bulk user action |
| `POST` | `/api/admin/change-password` | Admin | `{ current_password, new_password }` | Admin changes their own password |
| `GET` | `/api/admin/audit-log` | Admin | `offset?, limit?, user_id?` | Paginated audit log |
| `GET` | `/api/admin/audit-log/export` | Admin | — | Download audit log |
| `GET` | `/api/admin/logs` | Admin | `level?, module?, limit?` | Server log entries |

---

## 18. Admin — Media Management

| Method | Path | Auth | Body / Params | Description |
|--------|------|------|---------------|-------------|
| `GET` | `/api/admin/media` | Admin | `page, limit, sort, sort_order, type, category, search, tags, is_mature` | Admin media list (all fields exposed) |
| `PUT` | `/api/admin/media/:id` | Admin | `Partial<MediaItem>` | Update media metadata |
| `DELETE` | `/api/admin/media/:id` | Admin | — | Delete media item and file |
| `POST` | `/api/admin/media/bulk` | Admin | `{ ids[], action: delete|update, data? }` | Bulk media operation |
| `POST` | `/api/admin/media/scan` | Admin | — | Trigger full media library scan |

---

## 19. Admin — HLS Jobs

| Method | Path | Auth | Body / Params | Description |
|--------|------|------|---------------|-------------|
| `GET` | `/api/admin/hls/stats` | Admin | — | Total/running/completed/failed job counts + cache size |
| `GET` | `/api/admin/hls/jobs` | Admin | — | Full HLS job list |
| `DELETE` | `/api/admin/hls/jobs/:id` | Admin | — | Delete HLS job and cached segments |
| `GET` | `/api/admin/hls/validate/:id` | Admin | — | Validate HLS segments for a job |
| `POST` | `/api/admin/hls/clean/locks` | Admin | — | Remove stale processing locks |
| `POST` | `/api/admin/hls/clean/inactive` | Admin | — | Remove inactive/expired jobs |

---

## 20. Admin — Thumbnails

| Method | Path | Auth | Body | Description |
|--------|------|------|------|-------------|
| `POST` | `/api/admin/thumbnails/generate` | Admin | `{ id, is_audio? }` | Force-generate thumbnail for media |
| `GET` | `/api/admin/thumbnails/stats` | Admin | — | Total thumbnails, size, pending, errors |

---

## 21. Admin — Scheduled Tasks

| Method | Path | Auth | Description |
|--------|------|------|-------------|
| `GET` | `/api/admin/tasks` | Admin | List all 11 registered tasks with schedule, last/next run, enabled, running status |
| `POST` | `/api/admin/tasks/:id/run` | Admin | Trigger task immediately |
| `POST` | `/api/admin/tasks/:id/enable` | Admin | Enable scheduled task |
| `POST` | `/api/admin/tasks/:id/disable` | Admin | Disable scheduled task |
| `POST` | `/api/admin/tasks/:id/stop` | Admin | Stop currently running task |

Tasks: `media-scan`, `metadata-cleanup`, `thumbnail-generation`, `session-cleanup`, `backup-cleanup`, `mature-content-scan`, `hf-classification`, `duplicate-scan`, `audit-log-cleanup`, `health-check`, `hls-pregenerate`

---

## 22. Admin — Configuration & Database

| Method | Path | Auth | Body | Description |
|--------|------|------|------|-------------|
| `GET` | `/api/admin/config` | Admin | — | Full server configuration as key-value map |
| `PUT` | `/api/admin/config` | Admin | `Record<string, unknown>` | Update server config |
| `GET` | `/api/admin/database/status` | Admin | — | DB connection status, host, database, version |
| `POST` | `/api/admin/database/query` | Admin | `{ query }` | Execute raw SQL (columns, rows, rows_affected) |

---

## 23. Admin — Backups

| Method | Path | Auth | Body | Description |
|--------|------|------|------|-------------|
| `GET` | `/api/admin/backups/v2` | Admin | — | List backup entries (id, filename, size, created_at, type) |
| `POST` | `/api/admin/backups/v2` | Admin | `{ description?, backup_type? }` | Create backup |
| `POST` | `/api/admin/backups/v2/:id/restore` | Admin | — | Restore from backup |
| `DELETE` | `/api/admin/backups/v2/:id` | Admin | — | Delete backup file |

---

## 24. Admin — Security / IP Management

| Method | Path | Auth | Body / Params | Description |
|--------|------|------|---------------|-------------|
| `GET` | `/api/admin/security/stats` | Admin | — | Ban counts, whitelist/blacklist sizes, rate limit stats |
| `GET` | `/api/admin/security/whitelist` | Admin | — | Whitelisted IP entries |
| `POST` | `/api/admin/security/whitelist` | Admin | `{ ip, comment? }` | Add IP to whitelist |
| `DELETE` | `/api/admin/security/whitelist` | Admin | `{ ip }` | Remove IP from whitelist |
| `GET` | `/api/admin/security/blacklist` | Admin | — | Blacklisted IP entries |
| `POST` | `/api/admin/security/blacklist` | Admin | `{ ip, comment?, expires_at? }` | Add IP to blacklist |
| `DELETE` | `/api/admin/security/blacklist` | Admin | `{ ip }` | Remove from blacklist |
| `GET` | `/api/admin/security/banned` | Admin | — | Auto-banned IPs (rate-limit bans) |
| `POST` | `/api/admin/security/ban` | Admin | `{ ip, duration_minutes? }` | Manually ban IP |
| `POST` | `/api/admin/security/unban` | Admin | `{ ip }` | Remove ban |

---

## 25. Admin — Content Scanner (Mature Content)

| Method | Path | Auth | Body / Params | Description |
|--------|------|------|---------------|-------------|
| `POST` | `/api/admin/scanner/scan` | Admin | `{ path? }` | Trigger scan of path (or all) for mature content |
| `GET` | `/api/admin/scanner/stats` | Admin | — | Scanned/mature/auto-flagged/pending counts |
| `GET` | `/api/admin/scanner/queue` | Admin | — | Items pending human review |
| `POST` | `/api/admin/scanner/queue` | Admin | `{ action: approve|reject, ids[] }` | Batch review decision |
| `DELETE` | `/api/admin/scanner/queue` | Admin | — | Clear entire review queue |
| `POST` | `/api/admin/scanner/approve/:id` | Admin | — | Approve single item |
| `POST` | `/api/admin/scanner/reject/:id` | Admin | — | Reject single item |

---

## 26. Admin — HuggingFace Visual Classification

| Method | Path | Auth | Body | Description |
|--------|------|------|------|-------------|
| `GET` | `/api/admin/classify/status` | Admin | — | HF model config, rate limits, task schedule |
| `GET` | `/api/admin/classify/stats` | Admin | — | Total/classified/pending mature counts + recent items |
| `POST` | `/api/admin/classify/file` | Admin | `{ path }` | Classify single file |
| `POST` | `/api/admin/classify/directory` | Admin | `{ path }` | Classify all files in directory |
| `POST` | `/api/admin/classify/run-task` | Admin | — | Trigger classification background task |
| `POST` | `/api/admin/classify/clear-tags` | Admin | `{ id }` | Clear classification tags for media item |
| `POST` | `/api/admin/classify/all-pending` | Admin | — | Classify all pending items |

---

## 27. Admin — Validator

| Method | Path | Auth | Body | Description |
|--------|------|------|------|-------------|
| `POST` | `/api/admin/validator/validate` | Admin | `{ id }` | Validate media file (codec, container, streams) |
| `POST` | `/api/admin/validator/fix` | Admin | `{ id }` | Auto-fix media file issues |
| `GET` | `/api/admin/validator/stats` | Admin | — | Validated/needs-fix/fixed/failed counts |

---

## 28. Admin — Categorizer

| Method | Path | Auth | Body / Params | Description |
|--------|------|------|---------------|-------------|
| `POST` | `/api/admin/categorizer/file` | Admin | `{ path }` | Auto-categorize a file |
| `POST` | `/api/admin/categorizer/directory` | Admin | `{ directory }` | Categorize all files in directory |
| `GET` | `/api/admin/categorizer/stats` | Admin | — | Total items, count by category, manual overrides |
| `POST` | `/api/admin/categorizer/set` | Admin | `{ path, category }` | Manually set category for a file |
| `GET` | `/api/admin/categorizer/by-category` | Admin | `category=<name>` | Get all items in a category |
| `POST` | `/api/admin/categorizer/clean` | Admin | — | Remove stale category entries |

---

## 29. Admin — Remote Sources

| Method | Path | Auth | Body / Params | Description |
|--------|------|------|---------------|-------------|
| `GET` | `/api/admin/remote/sources` | Admin | — | List configured remote sources with sync status |
| `POST` | `/api/admin/remote/sources` | Admin | `{ name, url, username?, password?, enabled }` | Add remote source |
| `DELETE` | `/api/admin/remote/sources/:source` | Admin | — | Remove remote source |
| `POST` | `/api/admin/remote/sources/:source/sync` | Admin | — | Trigger sync for a source |
| `GET` | `/api/admin/remote/stats` | Admin | — | Source count, cached items, cache size |
| `GET` | `/api/admin/remote/media` | Admin | — | All cached remote media items |
| `GET` | `/api/admin/remote/sources/:source/media` | Admin | — | Media from a specific source |
| `POST` | `/api/admin/remote/cache` | Admin | `{ url, source_name }` | Cache a specific remote URL |
| `POST` | `/api/admin/remote/cache/clean` | Admin | — | Remove expired/stale cache entries |

---

## 30. Admin — Extractor

| Method | Path | Auth | Body | Description |
|--------|------|------|------|-------------|
| `GET` | `/api/admin/extractor/items` | Admin | — | List tracked extractor URLs/items |
| `POST` | `/api/admin/extractor/items` | Admin | `{ url }` | Add URL to extractor |
| `DELETE` | `/api/admin/extractor/items/:id` | Admin | — | Remove extractor item |
| `GET` | `/api/admin/extractor/stats` | Admin | — | Total/active/error counts |

Extractor HLS proxy (unauthenticated, rate-limited):

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/extractor/hls/:id/master.m3u8` | Extractor HLS master |
| `GET` | `/extractor/hls/:id/:quality/playlist.m3u8` | Extractor HLS variant |
| `GET` | `/extractor/hls/:id/:quality/:segment` | Extractor HLS segment |

---

## 31. Admin — Crawler

| Method | Path | Auth | Body / Params | Description |
|--------|------|------|---------------|-------------|
| `GET` | `/api/admin/crawler/targets` | Admin | — | List crawler targets |
| `POST` | `/api/admin/crawler/targets` | Admin | `{ url, name? }` | Add crawl target |
| `DELETE` | `/api/admin/crawler/targets/:id` | Admin | — | Remove target |
| `POST` | `/api/admin/crawler/targets/:id/crawl` | Admin | — | Start crawl job |
| `GET` | `/api/admin/crawler/discoveries` | Admin | `target_id?` | List discovered URLs |
| `POST` | `/api/admin/crawler/discoveries/:id/approve` | Admin | — | Approve discovery (adds to library) |
| `POST` | `/api/admin/crawler/discoveries/:id/ignore` | Admin | — | Mark discovery as ignored |
| `DELETE` | `/api/admin/crawler/discoveries/:id` | Admin | — | Delete discovery record |
| `GET` | `/api/admin/crawler/stats` | Admin | — | Target/discovery counts, active crawl status |

---

## 32. Admin — Receiver / Slave Nodes

| Method | Path | Auth | Body / Params | Description |
|--------|------|------|---------------|-------------|
| `GET` | `/api/admin/receiver/slaves` | Admin | — | List registered slave nodes |
| `GET` | `/api/admin/receiver/stats` | Admin | — | Slave count, online count, media count, duplicates |
| `DELETE` | `/api/admin/receiver/slaves/:id` | Admin | — | Remove slave node |
| `GET` | `/api/admin/duplicates` | Admin | `status=pending|resolved` | List duplicate media across nodes |
| `POST` | `/api/admin/duplicates/:id/resolve` | Admin | `{ action }` | Resolve a duplicate decision |
| `GET` | `/api/receiver/media` | Admin | — | All media across slave nodes |
| `GET` | `/api/receiver/media/:id` | Admin | — | Single slave media item |

Slave node API (X-API-Key auth):

| Method | Path | Description |
|--------|------|-------------|
| `POST` | `/api/receiver/register` | Slave registers with master |
| `POST` | `/api/receiver/catalog` | Slave pushes media catalog |
| `POST` | `/api/receiver/heartbeat` | Slave heartbeat |
| `POST` | `/api/receiver/stream-push/:token` | Slave delivers file data |
| `GET` | `/ws/receiver` | Slave ↔ master WebSocket |

---

## 33. Admin — Downloader

| Method | Path | Auth | Body / Params | Description |
|--------|------|------|---------------|-------------|
| `GET` | `/api/admin/downloader/health` | Admin | — | yt-dlp online status, active/queued counts |
| `POST` | `/api/admin/downloader/detect` | Admin | `{ url }` | Detect URL type, extract available streams |
| `POST` | `/api/admin/downloader/download` | Admin | `{ url, title?, clientId, isYouTube?, relayId? }` | Start download job |
| `POST` | `/api/admin/downloader/cancel/:id` | Admin | — | Cancel in-progress download |
| `GET` | `/api/admin/downloader/downloads` | Admin | — | List completed download files |
| `DELETE` | `/api/admin/downloader/downloads/:filename` | Admin | — | Delete a downloaded file |
| `GET` | `/api/admin/downloader/settings` | Admin | — | Downloader config (concurrent, dirs, formats) |
| `GET` | `/api/admin/downloader/importable` | Admin | — | Files in downloads dir ready to import |
| `POST` | `/api/admin/downloader/import` | Admin | `{ filename, delete_source, trigger_scan }` | Import downloaded file into library |
| `GET` | `/api/admin/downloader/verify` | Admin | — | Identity verification for downloader service |
| `GET` | `/ws/admin/downloader` | Admin | — | WebSocket — real-time download progress |

---

## 34. Admin — Auto-Discovery

| Method | Path | Auth | Body | Description |
|--------|------|------|------|-------------|
| `POST` | `/api/admin/discovery/scan` | Admin | `{ directory }` | Scan directory for importable media |
| `GET` | `/api/admin/discovery/suggestions` | Admin | — | Pending discovery suggestions |
| `POST` | `/api/admin/discovery/apply` | Admin | `{ original_path }` | Apply a discovery suggestion |
| `DELETE` | `/api/admin/discovery/*path` | Admin | — | Dismiss a suggestion by path |
| `GET` | `/api/admin/suggestions/stats` | Admin | — | Suggestion engine profile/view statistics |

---

## 35. Admin — Updates

| Method | Path | Auth | Body | Description |
|--------|------|------|------|-------------|
| `GET` | `/api/admin/update/check` | Admin | — | Check GitHub releases for newer version |
| `GET` | `/api/admin/update/status` | Admin | — | Current update job progress |
| `POST` | `/api/admin/update/apply` | Admin | — | Download + apply binary update |
| `GET` | `/api/admin/update/source/check` | Admin | — | Check source repo for new commits |
| `POST` | `/api/admin/update/source/apply` | Admin | — | Pull + rebuild from source |
| `GET` | `/api/admin/update/source/progress` | Admin | — | Source update build progress |
| `GET` | `/api/admin/update/config` | Admin | — | Update method (`source`/`binary`) and branch |
| `PUT` | `/api/admin/update/config` | Admin | `{ update_method?, branch? }` | Change update strategy |

---

## Admin Playlists (Cross-User View)

| Method | Path | Auth | Params | Description |
|--------|------|------|--------|-------------|
| `GET` | `/api/admin/playlists` | Admin | `page, limit, search, visibility` | List all users' playlists |
| `GET` | `/api/admin/playlists/stats` | Admin | — | Total/public playlist counts |
| `POST` | `/api/admin/playlists/bulk` | Admin | `{ ids[] }` | Bulk delete playlists |
| `DELETE` | `/api/admin/playlists/:id` | Admin | — | Delete any playlist by ID |

---

## Route Count Summary

| Category | Public | Auth | Admin | Total |
|----------|--------|------|-------|-------|
| System / Health | 6 | — | 1 | 7 |
| Authentication | 4 | 2 | — | 6 |
| Media Library | 4 | — | — | 4 |
| Media Streaming (direct) | 6 | — | — | 6 |
| HLS Streaming (direct) | 3 | — | — | 3 |
| HLS API | — | 4 | 6 | 10 |
| Playback | — | 2 | — | 2 |
| Preferences / Storage | — | 3 | — | 3 |
| Watch History | — | 2 | — | 2 |
| Suggestions & Ratings | 3 | 3 | — | 6 |
| Thumbnails API | 2 | — | — | 2 |
| Playlists | — | 11 | 4 | 15 |
| Upload | — | 2 | — | 2 |
| Remote Streaming | — | 1 | — | 1 |
| Analytics | — | 1 | 9 | 10 |
| Admin Dashboard | — | — | 10 | 10 |
| Admin Users | — | — | 11 | 11 |
| Admin Media | — | — | 5 | 5 |
| Admin HLS Jobs | — | — | 6 | 6 |
| Admin Thumbnails | — | — | 2 | 2 |
| Admin Tasks | — | — | 5 | 5 |
| Admin Config & DB | — | — | 4 | 4 |
| Admin Backups | — | — | 4 | 4 |
| Admin Security | — | — | 10 | 10 |
| Admin Content Scanner | — | — | 7 | 7 |
| Admin HF Classify | — | — | 7 | 7 |
| Admin Validator | — | — | 3 | 3 |
| Admin Categorizer | — | — | 6 | 6 |
| Admin Remote Sources | — | — | 9 | 9 |
| Admin Extractor | 3 | — | 4 | 7 |
| Admin Crawler | — | — | 9 | 9 |
| Admin Receiver / Slaves | 5 | — | 7 | 12 |
| Admin Downloader | — | — | 11 | 11 |
| Admin Discovery | — | — | 5 | 5 |
| Admin Updates | — | — | 8 | 8 |
| **Totals** | **36** | **31** | **148** | **~215** |
