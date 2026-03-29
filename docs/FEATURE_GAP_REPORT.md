# Feature Gap Report

> Generated: 2026-03-28 | Analyzed by: Claude (read-only codebase analysis)
> Based on: api_spec/openapi.yaml + web/nuxt-ui/ + internal/ modules + database schema
> Updated: 2026-03-28 — P0 items 1, 2, 4, 5, 6, 7 resolved in subsequent improvement cycles.
> P0 item 3 (export visibility) improved. P2 docs/metrics links added to admin.
> Updated: 2026-03-28 (cycle 4) — P1 item 12 (personal stats) extended: type_preferences visualization
> and ratings distribution histogram added to profile page. P2 previous_last_login shown in Account card.
> SourcesTab (807 lines) refactored into SourcesRemotePanel, SourcesCrawlerPanel, SourcesReceiverPanel sub-components.
> All listed task-file Tier-1 items now confirmed implemented: progress bars, RSS, user tokens, favorites,
> timestamp deep-links, OpenAPI link, personal stats, persist filter prefs.
> Updated: 2026-03-28 (scheduled) — URL deep-linking for browse filters (index.vue), CSS daily bar chart
> in admin analytics, and user bulk playlist delete (full-stack: POST /api/playlists/bulk-delete + playlists.vue
> multi-select UI) implemented. Pre-existing TS2322 typecheck error in player.vue fixed.
> Updated: 2026-03-28 (scheduled) — analytics event data payload now shown in admin drill-down; backup
> file/error count column added to admin backup table (BackupEntry type extended); scanner review queue
> type mismatch fixed — approve/reject now uses correct item.id (was broken with item.path which was never
> returned by backend). P2 "analytics_events.data" and "backup_manifests.files" items resolved.

---

## Executive Summary

Media Server Pro 4 has a well-built backend (30 modules, 215+ routes, 32+ DB tables) paired with a solid Nuxt 3 SPA. The majority of admin-facing functionality is fully exposed: all admin tabs call their corresponding backend routes. On the user side, most playback and library features are wired correctly. The principal gaps are: (1) several powerful filtering and sorting parameters that exist on the backend but are not surfaced in the UI (min_rating filter, is_mature filter); (2) the RSS/Atom feed and watch-history export exist as routes but have no discoverable UI entry point; (3) public playlist sharing is stored in the database but never surfaced to other users; (4) the `subtitle_lang` preference is stored and typed but the player has no subtitle/caption track UI; and (5) the "similar media" suggestions endpoint is fully implemented but never called from the player page. All P0 findings require only frontend changes — the backend is already ready.

---

## Feature Maturity Overview

| Area | Current State | Gaps Found | Priority |
|------|--------------|------------|----------|
| Media browsing / library grid | Solid — search, type, category, tag, sort, hide-watched all work | min_rating filter not wired in UI | P0 |
| Video/HLS playback | Solid — quality switch, PiP, keyboard shortcuts, loop, playlist autoplay | No subtitle/caption track UI; no chapter markers | P1/P3 |
| Suggestions & recommendations | Solid — continue watching, trending, personalized, on-deck, new since last visit all wired | No "similar to this" surface on media cards | P0 |
| User profile / preferences | Good — theme, view mode, sort, items-per-page, home section toggles, watch history, ratings, API tokens | subtitle_lang pref stored but not shown; no personal stats chart | P1 |
| Favorites | Fully wired | None | — |
| Playlists | Fully wired — create, edit, delete, reorder, copy, export (backend) | No export button or copy button in UI; public playlists not browsable | P0/P1 |
| Upload | Fully wired | No chunked-upload WS progress; no multi-category batch | P2 |
| Categories browse page | Fully wired | No user re-categorize flow | P2 |
| RSS feed | Backend route exists (`GET /api/feed`) | No UI entry point | P0 |
| Watch history export | Backend route exists (`GET /api/watch-history/export`) | Export button present but low-visibility | P0 |
| Admin dashboard | All tabs fully wired | No time-series chart (raw numbers only) | P2 |
| Admin analytics | All aggregate + drill-down routes wired | Per-user analytics not user-accessible | P1 |
| Admin config | Full JSON editor present | Config fields not documented in-UI; user-type management raw JSON only | P1 |
| HLS / streaming | Admin HLS tab fully wired | `hls.auto_generate` flag not togglable in admin UI | P1 |
| Content moderation (scanner) | Admin scanner fully wired | No confidence-threshold tuning UI | P2 |
| Backup | Admin backup tab fully wired | No backup schedule UI | P2 |
| Downloader | Admin downloader tab fully wired | No user-facing download-request flow | P2 |
| Remote / slave receiver | Admin sources tab fully wired | No end-user transparency for file source | P2 |
| Security | Admin security tab fully wired | No CORS/HTTPS/HSTS toggle in admin UI | P2 |
| Metrics / Prometheus | `GET /metrics` exists (admin-protected) | Not linked anywhere in admin UI | P2 |
| OpenAPI docs | `GET /api/docs` (auth-required) | No "Open API docs" link in UI | P2 |

---

## P0 — Quick Wins (Backend ready, just needs UI)

### 1. min_rating Media Filter

**What's missing**: The home/library page (`index.vue`) has no "minimum rating" filter control. The backend handler reads `?min_rating=N` and filters the result set. The TypeScript type already declares the parameter.

**Evidence**:
- Backend reads `min_rating`: `api/handlers/media.go` lines 35-38 — `if mr := c.Query("min_rating"); mr != ""`
- Type defined: `web/nuxt-ui/types/api.ts` line 121 — `min_rating?: number`
- Index.vue `params` reactive object never sets `min_rating`; it is never passed to `mediaApi.list()`

**Backend support**: YES — fully implemented, accepts 1–5

**Effort**: LOW

**User impact**: HIGH — lets users quickly browse only their top-rated content

**Suggested UI**: Star-filter control in the library toolbar (1★ minimum, 2★, 3★, 4★, 5★ only); bind to `params.min_rating`; clearing sets back to undefined

---

### 2. RSS / Atom Feed Entry Point

**What's missing**: `GET /api/feed` returns a valid Atom XML feed (auth-required). No UI link, subscribe button, or discovery text exists anywhere in the frontend.

**Evidence**:
- Backend: `api/routes/routes.go` line 357 — `api.GET("/feed", requireAuth(), h.GetRSSFeed)`
- Handler: `api/handlers/feed.go` — full Atom feed supporting `?category=&type=&limit=`
- Frontend: zero references to `/api/feed` across all files in `web/nuxt-ui/`

**Backend support**: YES

**Effort**: LOW

**User impact**: MEDIUM — power users and Archivist personas value RSS for new-content notifications

**Suggested UI**: "Subscribe (RSS)" icon-button in the library toolbar; links to `/api/feed`; optional popover with category/type filter sub-links

---

### 3. Watch History Export Not Prominently Shown

**What's missing**: `GET /api/watch-history/export` is wired and the URL is referenced in `profile.vue` line 443 as a `NuxtLink`, but it is inside a history tab area and may not be visible to users who do not scroll to it. No clearly-labeled "Download" or "Export" button is consistently shown.

**Evidence**:
- Backend: `api/routes/routes.go` line 387 — `api.GET(pathWatchHistory+"/export", requireAuth(), h.ExportWatchHistory)`
- Frontend: `web/nuxt-ui/pages/profile.vue` line 443 — link exists but placement is low-visibility

**Backend support**: YES

**Effort**: LOW

**User impact**: MEDIUM — data portability expectation; important for Archivist persona

**Suggested UI**: A clearly-labeled "Export (JSON)" button shown at the top of the Watch History tab, always visible regardless of history list content

---

### 4. "Similar Media" Not Surfaced on Player Page

**What's missing**: `GET /api/suggestions/similar?id=` is implemented and the composable function exists. The player page imports `useSuggestionsApi` but never calls `getSimilar()`. No "More like this" section appears below the player.

**Evidence**:
- Backend: `api/routes/routes.go` line 423 — `api.GET("/suggestions/similar", h.GetSimilarMedia)`
- Composable: `web/nuxt-ui/composables/useApiEndpoints.ts` line 167 — `getSimilar: (id) => …`
- Player: `web/nuxt-ui/pages/player.vue` line 10 — `suggestionsApi` imported; `getSimilar` never called

**Backend support**: YES

**Effort**: LOW

**User impact**: HIGH — content discovery is a core engagement driver; every streaming platform shows this

**Suggested UI**: "More like this" horizontal scroll row below the player, 6–8 items, rendered after media loads

---

### 5. Playlist Export Button Missing in UI

**What's missing**: `GET /api/playlists/:id/export?format=json|m3u|m3u8` is implemented and `usePlaylistApi().exportPlaylist()` returns the URL. However, no export button or action exists in the playlists page.

**Evidence**:
- Backend: `api/routes/routes.go` line 395 — `api.GET("/playlists/:id/export", requireAuth(), h.ExportPlaylist)`
- Composable: `web/nuxt-ui/composables/useApiEndpoints.ts` lines 221-223 — `exportPlaylist(id, format)` returns URL string
- Frontend: `web/nuxt-ui/pages/playlists.vue` — no call to `exportPlaylist()` anywhere

**Backend support**: YES (JSON, M3U, M3U8)

**Effort**: LOW

**User impact**: MEDIUM — allows integration with VLC, Kodi, Jellyfin; important for Power User persona

**Suggested UI**: "Export" dropdown on each playlist card (or inside the edit modal) with three options: JSON, M3U, M3U8 — each opens the respective export URL

---

### 6. Copy / Duplicate Playlist Button Missing

**What's missing**: `POST /api/playlists/:id/copy` is implemented and the composable has `playlistApi.copy(id, name)`. No copy button or action exists in the playlists page.

**Evidence**:
- Backend: `api/routes/routes.go` line 400 — `api.POST("/playlists/:id/copy", requireAuth(), h.CopyPlaylist)`
- Composable: `web/nuxt-ui/composables/useApiEndpoints.ts` line 219-220 — `copy: (id, name) => …`
- Frontend: `web/nuxt-ui/pages/playlists.vue` — no call to `copy()` anywhere

**Backend support**: YES

**Effort**: LOW

**User impact**: LOW-MEDIUM — convenience feature; Curator persona uses it to remix playlists

**Suggested UI**: "Duplicate" option in the playlist three-dot action menu; prompts for a new name

---

### 7. is_mature Filter Missing in Admin Media Tab

**What's missing**: `GET /api/admin/media` supports `?is_mature=true|false` (defined in `AdminMediaListParams.is_mature`). The admin MediaTab does not include this filter in the params sent to `listMedia()`.

**Evidence**:
- Type: `web/nuxt-ui/types/api.ts` line 163 — `is_mature?: string`
- Component: `web/nuxt-ui/components/admin/MediaTab.vue` — no `is_mature` in the filter object passed to `adminApi.listMedia()`

**Backend support**: YES

**Effort**: LOW

**User impact**: MEDIUM — admin efficiency for mature content management

**Suggested UI**: "All / Mature only / Clean only" dropdown in the admin media filter bar

---

## P1 — High Impact Features (Medium Effort)

### 8. Public Playlist Browsing

**What's missing**: The `playlists` table has `is_public BOOLEAN` stored. Users can set `is_public: true` when creating playlists. However, there is no public listing route and no frontend page for other users to discover public playlists.

**Evidence**:
- DB: `internal/database/migrations.go` line 125 — `is_public BOOLEAN DEFAULT FALSE`
- DB: `internal/database/migrations.go` line 124 — `cover_image VARCHAR(1024)` also stored
- Backend: `GET /api/playlists` (handler checks `session.UserID`) returns only the user's own playlists
- No public playlist listing route in `api/routes/routes.go`

**Backend support**: PARTIAL — data stored; a new `GET /api/playlists/public` route is needed

**Effort**: MEDIUM

**User impact**: HIGH — community sharing is a key differentiator for a self-hosted community server

**Suggested UI**: "Community Playlists" section on the home page or a separate browse page; shows public playlists from all users with owner name and cover image

---

### 9. Subtitle / Caption Language Preference Not Implemented

**What's missing**: `subtitle_lang` is stored in `user_preferences`, defined in the TypeScript type, and has a DB migration. The player page has zero references to subtitles, captions, WebVTT, `<track>` elements, or the `subtitle_lang` preference value.

**Evidence**:
- DB: `internal/database/migrations.go` line 57 — `subtitle_lang VARCHAR(10) DEFAULT 'en'`
- Type: `web/nuxt-ui/types/api.ts` line 39 — `subtitle_lang?: string`
- Player: `web/nuxt-ui/pages/player.vue` — no `<track>` element, no subtitle logic in 400+ lines

**Backend support**: PARTIAL — preference stored; no subtitle file serving routes exist

**Effort**: MEDIUM-HIGH

**User impact**: HIGH — accessibility and international users; required for WCAG 2.1 AA compliance

**Suggested UI**: CC button in player controls; dropdown of available tracks; language preference selector in profile settings

---

### 10. User Type Management Not in Admin UI

**What's missing**: `config.json` defines four named `user_types` (premium, standard, basic, guest) with per-type storage quotas, concurrent stream limits, and feature flags. These can only be modified by editing `config.json` or the raw JSON editor in the admin panel.

**Evidence**:
- Config: `config.json` lines 116-149 — `auth.user_types[]` with 4 entries, each with `storage_quota`, `max_concurrent_streams`, `allow_downloads`, `allow_uploads`, `allow_playlists`
- Admin SystemTab.vue uses `configText` (raw JSON string); no structured user-type form exists

**Backend support**: YES — `PUT /api/admin/config` accepts full config updates

**Effort**: MEDIUM

**User impact**: MEDIUM — reduces admin friction for non-technical operators

**Suggested UI**: "User Types" structured table in admin Users tab with editable rows for each type

---

### 11. HLS Auto-Generate Toggle Missing from Admin UI

**What's missing**: `config.json` `hls.auto_generate` is false by default. No admin UI toggle exists; changing it requires raw JSON editing.

**Evidence**:
- Config: `config.json` line 158 — `"auto_generate": false`
- Admin StreamingTab.vue: shows HLS jobs and stats but no `auto_generate` toggle

**Backend support**: YES — `PUT /api/admin/config`

**Effort**: LOW-MEDIUM

**User impact**: MEDIUM — operators of large libraries need this toggle

**Suggested UI**: "Auto-generate HLS on scan" toggle in the admin StreamingTab HLS sub-tab

---

### 12. Personal Stats / Analytics Dashboard

**What's missing**: `GET /api/suggestions/profile` returns `UserProfile` with `total_views`, `total_watch_time`, `category_scores`, `type_preferences`. The profile page loads this data but only shows a brief summary. No time-series chart, category breakdown, or ratings distribution is shown.

**Evidence**:
- Backend: `api/routes/routes.go` line 428 — `api.GET("/suggestions/profile", requireAuth(), h.GetMyProfile)`
- Types: `web/nuxt-ui/types/api.ts` lines 59-66 — `UserProfile` with category_scores JSON
- DB: `suggestion_view_history` table stores per-media ratings, view counts, total_time, completed_at
- Profile: `web/nuxt-ui/pages/profile.vue` line 30-33 — loads profile; minimal display

**Backend support**: YES (data available for personal stats)

**Effort**: MEDIUM

**User impact**: HIGH — personal insights drive engagement; a staple feature on Plex, Jellyfin, YouTube

**Suggested UI**: "My Stats" tab on the profile page: total watch time, watch time by category bar chart, ratings distribution, recently-completed list

---

### 13. Download and Auth Config Not Exposed in Admin UI

**What's missing**: `config.json` has `download.require_auth: false` and `download.enabled: true`. These control anonymous download access. There is no structured admin toggle — requires raw JSON editing.

**Evidence**:
- Config: `config.json` lines 36-40 — `download.enabled`, `download.require_auth`, `download.chunk_size_kb`
- Admin SystemTab.vue: raw JSON editor only, no structured download section

**Backend support**: YES — `PUT /api/admin/config`

**Effort**: LOW

**User impact**: MEDIUM — security-sensitive setting operators expect a toggle for

**Suggested UI**: "Downloads" section in admin Settings sub-tab with enabled/require-auth toggles

---

## P2 — Backlog Features

| Feature | Gap Description | Evidence | Backend Status | Effort | Personas |
|---------|----------------|----------|----------------|--------|----------|
| Metrics / Prometheus link in admin | `/metrics` admin endpoint has no admin UI link | `routes.go` line 285 | YES | LOW | Developer |
| OpenAPI docs link in admin | `GET /api/docs` (auth) not linked anywhere | `routes.go` line 354 | YES | LOW | Developer |
| Media codec/bitrate sort | Backend stores `codec`, `bitrate`, `container`; sort only exposes 6 options | `media.go` + `types/api.ts` `MediaItem` | YES | LOW | Power User |
| Bulk playlist actions for users | Only admin can bulk-delete playlists; user has no multi-select in playlists page | `playlists.vue`; `routes.go` lines 511-512 | YES (admin only) | LOW | Curator |
| Batch playback positions in player | `GET /api/playback/batch` exists and is in composable but only used when loading the library grid hover progress; player always calls single-item `/api/playback?id=` | `routes.go` line 328; `useApiEndpoints.ts` line 142 | YES | LOW | Casual Viewer |
| Duplicate media management for users | `GET /api/admin/duplicates` is admin-only; users cannot see if their uploads are duplicates | `routes.go` line 625 | YES (admin) | MEDIUM | Archivist |
| Config field documentation in admin | Raw JSON config editor has no per-field tooltips or schema reference | `SystemTab.vue` | YES (config) | MEDIUM | Family Admin |
| Scanner confidence thresholds in admin | `config.json` mature_scanner thresholds have no structured admin form | `config.json` lines 199-216 | YES | MEDIUM | Family Admin |
| Backup schedule configuration | No UI to schedule automated backups | `SystemTab.vue` tasks sub-tab | PARTIAL | MEDIUM | Archivist |
| Previous last login display | `users.previous_last_login` column exists but not shown in profile or admin user list | `migrations.go` line 501 | YES | LOW | Family Admin |
| Suggestion profile reset | No "reset my profile" button for users | profile page | PARTIAL | LOW | Casual Viewer |
| CORS / HTTPS / HSTS admin toggles | `security.cors_enabled`, `server.enable_https`, `security.hsts_enabled` not exposed as admin toggles | `config.json` lines 82-100; 8-11 | YES | LOW | Family Admin |
| Blur-hash thumbnail placeholders | `blur_hash` column and TypeScript type exist but not used for progressive image loading in library cards or player | `migrations.go` line 510; `types/api.ts` line 98 | YES | LOW | Casual Viewer |
| Completed-at date in watch history | `suggestion_view_history.completed_at` stored; watch history list does not show completion date | `migrations.go` line 264 | YES | LOW | Archivist |
| Episode/season labels on category cards | `categorized_items.detected_season/episode` stored and used for grouping but not displayed as "S01E03" label on cards | `migrations.go` lines 200-202; `categories.vue` | YES | LOW | Casual Viewer |

---

## P3 — Future / Large Features

| Feature | Gap Description | Effort | Notes |
|---------|----------------|--------|-------|
| Subtitle/Caption Serving | No VTT/SRT file discovery, serving endpoints, or player track-switching UI | HIGH | `subtitle_lang` pref already stored; needs new backend module |
| Chapter Markers in Video | No chapter detection, storage, or seek-bar chapter display | HIGH | Would require ffprobe chapter extraction + new DB columns + player UI |
| Two-factor Authentication (2FA) | No TOTP or passkey support; password-only auth | HIGH | High-value security feature; auth module extension required |
| User-to-User Playlist Sharing | No mechanism to share a specific playlist URL with another user | HIGH | Needs shareable token routes + public viewer page |
| Watch Party / Synchronized Playback | No real-time sync across sessions | VERY HIGH | Requires WebSocket broadcast + host/guest session model |
| PWA / Installable App | SPA works on mobile but no installable PWA manifest or service worker | MEDIUM | PWA manifest + caching strategy |
| Content Collections / Multiple Libraries | No named library collections | HIGH | Would require new DB schema + routing |
| Parental Control PIN | Mature content gating uses age-gate, not per-user PIN | MEDIUM | Separate from mature preference; needed for Family Admin |
| Metadata Scraping (TMDB/TVDB) | No external metadata source; relies on filename-based categorizer only | VERY HIGH | New integration module required |

---

## Backend-to-Frontend Exposure Gaps

| Route | What it returns | Why users would want it | Frontend work needed |
|-------|----------------|------------------------|---------------------|
| `GET /api/feed` | Atom XML of latest media | RSS subscription for new content | Subscribe button in library header |
| `GET /api/suggestions/similar?id=` | Similar media items | "More like this" on player page | Row below player |
| `POST /api/playlists/:id/copy` | Copy of playlist | Quick playlist duplication | Three-dot menu in playlists page |
| `GET /api/playlists/:id/export` | Playlist in JSON/M3U/M3U8 | Media player integration (VLC/Kodi) | Export menu in playlists page |
| `GET /api/watch-history/export` | Watch history JSON | Data portability | Prominent button in profile history tab |
| `GET /api/metrics` | Prometheus metrics | Developer / ops monitoring | Link in admin System tab |
| `GET /api/docs` | OpenAPI spec JSON | Developer integration reference | Link in admin footer |
| `GET /api/admin/media?is_mature=true` | Mature-only media list | Admin mature content review | is_mature filter in admin MediaTab |
| `GET /api/media?min_rating=N` | Rating-filtered media list | Browse own top-rated content | min_rating filter in library toolbar |

The following routes are wired but surface area is under-visible:
- `GET /api/watch-history/export` — link exists in profile.vue line 443 but placement is low-visibility
- `GET /api/playback/batch` — called via composable for progress overlays; correctly used

---

## Data Collected but Not Displayed

| Data | Where it lives | What users could do with it | Notes |
|------|---------------|----------------------------|-------|
| `suggestion_view_history.total_time` | `suggestion_view_history` per user/media | "You've spent X hours on this series" | Available via `/api/suggestions/profile` but not charted |
| `suggestion_view_history.completed_at` | `suggestion_view_history` table | "Completed on [date]" badge in history | History list in profile.vue shows items but not completion date |
| `suggestion_view_history.rating` | `suggestion_view_history` table | Per-media star rating shown in history view | Shown on library grid; not in watch history list |
| `categorized_items.detected_season / detected_episode` | `categorized_items` table | Episode labels "S01E03" on TV show cards | Used for grouping in `categories.vue`; not displayed as episode number on cards |
| `categorized_items.detected_artist / detected_album` | `categorized_items` table | Artist/album metadata on music cards | Partially shown in categories.vue music grouping |
| `media_metadata.blur_hash` | `media_metadata` column | Progressive image placeholder before thumbnail load | Type defined (`types/api.ts` line 98); not used in library cards or player |
| `users.previous_last_login` | `users` table column (migration line 501) | Security awareness — "previous session was from X IP" | Not shown in profile or admin user detail |
| `scan_results.reviewed_by / reviewed_at / review_decision` | `scan_results` table | Moderation audit trail — who approved/rejected items | Admin scanner queue shows pending items; no review history shown |
| `backup_manifests.files / errors` | `backup_manifests.files / .errors` JSON columns | Show which files included / failed in backup | Admin backup list shows name/size/date but not file manifest |
| `analytics_events.data` | `analytics_events.data` JSON column | Rich event payload (seek position, selected quality) | Admin analytics drill-down shows event type but not the data JSON |
| `users.metadata` JSON | `users` table | Custom user attributes | Admin GetUser response includes it; no UI renders it |
| `receiver_duplicates.fingerprint` | `receiver_duplicates` table | Content-identical file hash for transparency | Admin duplicates tab shows names but not fingerprint |

---

## Industry Comparison Table

| Feature | Plex / Jellyfin Baseline | Media Server Pro 4 | Status |
|---------|--------------------------|-------------------|--------|
| HLS adaptive streaming | Yes | Yes — hls.js, quality selector, auto mode | Yes |
| Resume playback across devices | Yes | Yes — `playback_positions` DB, loaded on player open | Yes |
| Personal watch history | Yes | Yes — profile page with filter, remove, export | Yes |
| Continue watching row | Yes | Yes — home page on-deck + continue rows | Yes |
| User star ratings | Yes | Yes — `/api/ratings`, shown on library cards | Yes |
| Personalized recommendations | Yes | Yes — `suggestion_profiles` scoring engine | Yes |
| Trending / popular content | Yes | Yes — `GetTrendingSuggestions` endpoint | Yes |
| User playlists | Yes | Yes — full CRUD, reorder, copy (backend), export (backend) | Partial (UI missing export/copy buttons) |
| Content categories | Yes | Partial — categorizer assigns single category per file; no TMDB | Partial |
| Chapter markers / seek thumbnails | Yes (Plex) | Seek thumbnail previews: Yes. Chapter markers: No | Partial |
| Subtitle / caption support | Yes | Preference stored, no serving or player UI | No |
| Public playlist sharing | Yes | Data stored, no discovery route or viewer page | Partial |
| RSS/Atom new content feed | Yes (Plex RSS) | Yes — backend fully implemented, no UI entry point | Partial |
| Metadata scraping (TMDB/TVDB) | Yes | No — filename-based categorizer only | No |
| Trailers / extra content | Yes (Plex) | No | No |
| Multi-library / collection management | Yes | No — single media root | No |
| User download | Yes | Yes — `/download` endpoint; `can_download` permission | Yes |
| AI content classification | No (Plex) | Yes — HuggingFace visual classification module | Yes (unique) |
| Mature content scanning | Partial (Plex) | Yes — keyword + confidence scoring + admin review queue | Yes (strong) |
| Prometheus metrics | Jellyfin: Yes | Yes — `/metrics` admin endpoint (not linked in UI) | Partial |
| API tokens (Bearer auth) | Yes (Plex tokens) | Yes — `user_api_tokens` table, full CRUD in profile | Yes |
| Multi-user with per-user permissions | Yes | Yes — role + user_type system | Yes |
| Slave/receiver protocol | No | Yes — master-slave media proxy | Yes (unique) |
| External URL extraction | No | Yes — extractor + crawler modules | Yes (unique) |
| Auto-discovery of new media paths | Yes | Yes — autodiscovery module + admin UI | Yes |
| Backup and restore | Jellyfin: partial | Yes — admin backup v2 with manifest | Yes |
| In-app server updates | No | Yes — binary and source update modes | Yes (unique) |
| Duplicate detection | Yes (Plex) | Yes — fingerprint-based dedup across slaves | Yes |
| Mobile-optimized streaming | Yes | Partial — `mobile_chunk_size` config; no native app | Partial |
| PWA / Offline | No | No — SPA but no service worker | No |
| Watch party / sync | No (Plex: paid tier) | No | No |
| 2FA / passkeys | Jellyfin: TOTP plugin | No | No |
| Social features (comments, likes) | No | No — by design (out of scope) | N/A |
| Min-rating filter in library | Partial (Plex) | Backend YES; no UI control | Partial |
| "More like this" on player | Yes | Backend YES; no player UI surface | Partial |

---

## User Persona Gap Summary

| Persona | Top 3 Unmet Needs | Effort to Address |
|---------|-------------------|------------------|
| **Curator** (organizes collections, curates playlists) | 1. No playlist export button in UI (P0 — LOW). 2. No copy/duplicate playlist button (P0 — LOW). 3. No public playlist discovery page so curated lists can be shared (P1 — MEDIUM). | LOW / MEDIUM |
| **Casual Viewer** (browse, watch, discover) | 1. "More like this" row never appears below the player despite backend support (P0 — LOW). 2. No min_rating quick filter on library grid (P0 — LOW). 3. No personal stats page to reflect on watch habits (P1 — MEDIUM). | LOW / MEDIUM |
| **Archivist** (data integrity, portability) | 1. Watch history export button is low-visibility / buried (P0 — LOW). 2. No discoverable RSS subscription entry point (P0 — LOW). 3. Completion date not shown in watch history list (P2 — LOW). | LOW |
| **Family Admin** (manages household accounts) | 1. User type management requires raw JSON config edits (P1 — MEDIUM). 2. No parental control PIN separate from age-gate (P3 — MEDIUM). 3. Per-user previous last login not visible in admin user detail (P2 — LOW). | LOW / MEDIUM / HIGH |
| **Developer** (API integration, automation) | 1. No in-app link to `/api/docs` (OpenAPI spec) (P2 — LOW). 2. No link to `/metrics` Prometheus endpoint in admin (P2 — LOW). 3. API token creation is in profile but token scopes/expiry are not configurable (P2 — MEDIUM). | LOW |

---

*Report end. All findings are based on static read-only code analysis of the committed codebase as of 2026-03-28.*
*Every finding cites a specific file path, line number, or route from the actual source tree.*
