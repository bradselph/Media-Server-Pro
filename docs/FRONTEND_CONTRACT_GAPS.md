# Frontend Contract Gaps

Audit date: 2026-04-09
Audited files:

- `web/nuxt-ui/composables/useApiEndpoints.ts` (all frontend API calls)
- `web/nuxt-ui/types/api.ts` (TypeScript response types)
- `api/routes/routes.go` (backend route registration)
- `api/handlers/*.go` (backend handler implementations)

---

## Critical: Runtime-Breaking Issues

### 1. BulkDeletePlaylists response shape mismatch (user-facing)

- **Frontend** (`usePlaylistApi().bulkDelete`): expects `{ deleted: number; failed: number }`
- **Backend** (`BulkDeletePlaylists` in `playlists.go:424`): returns `{ deleted: number; failed: number }`
- **Status**: MATCH. No issue.

### 2. APIToken `last_used_at` — backend returns `null`, frontend type allows `null`

- **Backend** (`auth_tokens.go:37`): `LastUsedAt *string` with `json:"last_used_at"` -- when nil, serializes to JSON
  `null`
- **Frontend** (`api.ts:1063`): `last_used_at: string | null` -- correctly handles null
- **Status**: MATCH. No issue.

### 3. APIToken response — backend returns `expires_at` field, frontend type omits it

- **Backend** (`auth_tokens.go:28-29`): ListAPITokens returns `expires_at` field (nullable)
- **Frontend** (`api.ts:1060-1065`): `APIToken` interface has no `expires_at` field
- **Severity**: LOW. Frontend ignores the field; no runtime error, but data is silently discarded.
- **Impact**: If the UI ever needs to show token expiry, it would read `undefined`.

### 4. CreateAPIToken response — backend returns `expires_at`, frontend type omits it

- **Backend** (`auth_tokens.go:84-87`): Returns `expires_at` when token has TTL
- **Frontend** (`api.ts:1067-1069`): `APITokenCreated extends APIToken` -- no `expires_at`
- **Severity**: LOW. Same as above.

---

## Medium: Response Shape Gaps (frontend reads field backend may not return)

### 5. GetPermissions — backend returns extra `limits` and `user_type` fields

- **Backend** (`auth.go:617-638`): Returns `user_type`, `limits.storage_quota`, `limits.concurrent_streams`
- **Frontend** (`api.ts:591-606`): `PermissionsInfo` has no `user_type` or `limits` fields
- **Severity**: LOW. Extra backend fields are harmlessly ignored.

### ✅ `ad830f94` 2026-04-09 — DownloaderSettings — backend omits several fields frontend type declares

> **Resolved**: Backend now returns `downloadsDir`, `theme`, `browserRelayConfigured`. Frontend type removes phantom
> fields (`maxConcurrent`, `videoFormat`, `audioQuality`, `proxy`). Admin UI updated to reflect actual fields.
> **Verified**: pending deploy

### 7. DownloaderHealth — backend `dependencies` is `Record<string, string>`, frontend type is `Record<string, unknown>`

- **Backend** (`admin_downloader.go:41-48`): Builds `deps` as `map[string]string`
- **Frontend** (`api.ts:914`): `dependencies?: Record<string, unknown>`
- **Severity**: NONE. Frontend type is a supertype; no mismatch.

### 8. MediaListResponse — `total` / `page` / `limit` fields

- **Backend** (`media.go:285-297`): Returns `items`, `total_items`, `total_pages`, `scanning`, optionally `initializing`
  and `user_ratings`
- **Frontend** (`api.ts:137-147`): Declares `total`, `page`, `limit` fields as optional
- **Severity**: LOW. Backend never returns `total`, `page`, or `limit`. Frontend has them as optional (`?`), so they're
  always `undefined` at runtime. If any code reads `response.total` expecting a number, it silently gets `undefined`.

### 9. WatchHistory DELETE — frontend sends `id` query param, backend also accepts path-based removal

- **Frontend** (`useWatchHistoryApi().remove`): `DELETE /api/watch-history?id=<mediaId>`
- **Backend** (`auth.go:535`): Reads `c.Query("id")` -- MATCH
- **Status**: MATCH. No issue.

---

## Low: Phantom/Unreachable Endpoints

### 10. No phantom API calls detected

Every endpoint the frontend calls has a corresponding registered route in `routes.go` and a handler implementation. No
frontend call targets a non-existent backend route.

---

## Low: Backend Endpoints Not Consumed by Frontend

These backend routes exist but are never called from the frontend. They may be intended for external tooling, scripts,
or future features.

| Route                                | Handler                | Notes                                                            |
|--------------------------------------|------------------------|------------------------------------------------------------------|
| `POST /api/auth/delete-account`      | `DeleteAccount`        | Self-service account deletion; no frontend UI                    |
| `GET /api/watch-history/export`      | `ExportWatchHistory`   | CSV export; no frontend trigger                                  |
| `GET /api/docs`                      | `GetOpenAPISpec`       | OpenAPI spec; developer tooling                                  |
| `GET /api/feed`                      | `GetRSSFeed`           | RSS/Atom feed; external readers                                  |
| `GET /health`                        | `GetHealth`            | Health check; monitoring tools                                   |
| `GET /metrics`                       | `GetMetrics`           | Prometheus metrics; monitoring                                   |
| `POST /api/admin/thumbnails/cleanup` | `CleanupThumbnails`    | No frontend button                                               |
| `POST /api/admin/scanner/scan`       | `ScanContent`          | Scanner scan (distinct from media scan)                          |
| `GET /api/admin/analytics/export`    | `AdminExportAnalytics` | CSV export URL is generated but used as download link, not fetch |

---

## Medium: Request Body Field Gaps (frontend sends field backend ignores)

### 11. HLS GenerateHLS — frontend sends `quality` (string), backend also reads `qualities` (array)

- **Frontend** (`useHlsApi().generate`): `{ id, quality }` where quality is a string
- **Backend** (`hls.go:73-77`): Struct has both `Quality string` and `Qualities []string`. Logic at line 87-89: if
  `Qualities` is empty and `Quality` is non-empty, wraps Quality into Qualities.
- **Status**: COMPATIBLE. Frontend sends `quality` as a single string; backend correctly converts.

### 12. Downloader createDownloaderJob — frontend sends `clientId` camelCase, backend reads `clientId` camelCase

- **Frontend** (`useAdminApi().createDownloaderJob`): `{ url, title, clientId, isYouTube, isYouTubeMusic, relayId }`
- **Backend** (`admin_downloader.go:123-129`): `ClientID string \`json:"clientId"\``
- **Status**: MATCH. Uses `json:"clientId"` tag with camelCase.

### 13. Admin createUser — frontend sends `role`, backend struct has both `type` and `role`

- **Frontend** (`useAdminApi().createUser`): `{ username, password, email, role }` -- does NOT send `type`
- **Backend** (`admin_users.go:41-46`): Struct has `Type string \`json:"type"\``, defaults to "standard" if empty
- **Severity**: NONE. Backend defaults `type` to "standard" when omitted.

---

## Info: Correct Compat Layers

### 14. Preferences POST (not PUT)

- **Frontend** (`useApiEndpoints.ts:68`): Uses `api.post()` with comment noting backend only registers POST
- **Backend** (`routes.go:380`): `api.POST("/preferences", ...)` -- confirmed POST only
- **Status**: Correctly aligned.

### 15. Media list `sort` param mapping

- **Frontend** (`useMediaApi().list`): Maps `sort_by` to `sort` query param
- **Backend** (`media.go:27`): Reads `c.Query("sort")`
- **Status**: Correctly aligned via frontend mapping at lines 85-94.

### 16. Preferences normalization handles legacy keys

- **Frontend** (`apiCompat.ts`): `normalizePreferences` handles `show_mature_content`, `collect_analytics`,
  `show_home_continue_watching`, `show_home_suggestions`, `show_home_recently_added` as legacy aliases
- **Status**: Defensive. Prevents breakage if backend ever changes key names.

---

## Summary

| Severity | Count | Description                                                                                          |
|----------|-------|------------------------------------------------------------------------------------------------------|
| CRITICAL | 0     | No runtime-breaking mismatches found                                                                 |
| MEDIUM   | 1     | DownloaderSettings: 5 fields the frontend type declares that backend never returns                   |
| LOW      | 4     | APIToken missing `expires_at`; extra backend fields ignored; `total`/`page`/`limit` always undefined |
| INFO     | 9+    | Backend routes with no frontend caller; correct compat layers                                        |

**Overall assessment**: The frontend-backend contract is well-maintained. The `apiCompat.ts` normalization layer and the
centralized `useApiEndpoints.ts` pattern prevent most common mismatch issues. The only actionable medium-severity item
is the DownloaderSettings response gap -- the backend should either return the missing fields or the frontend type
should mark them all as optional to match reality.
