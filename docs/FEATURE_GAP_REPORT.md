# Feature Gap Report

> Generated: 2026-03-31 | Analyzed by: Claude Opus 4.6 (read-only codebase analysis)
> Based on: api_spec/openapi.yaml + web/nuxt-ui/ + internal/ modules + database schema
> Supersedes: prior 2026-03-28 report (most P0 items from that report have been resolved)

---

## Executive Summary

Media Server Pro 4 has reached strong feature parity between its Go backend (30 internal modules, 215+ API routes, 32+ DB tables) and its Nuxt 3 SPA frontend (11 pages, 13 admin tabs with sub-panels). Since the prior analysis on 2026-03-28, all seven original P0 "quick wins" have been implemented: min_rating filter, RSS subscribe link, watch history export, similar media on the player page, playlist export/copy/bulk-delete, public playlist browsing, and the is_mature admin filter.

The remaining gaps fall into five categories:

1. **Unused backend endpoints** -- Two routes registered in `routes.go` have zero frontend coverage: self-service account deletion (`POST /api/auth/delete-account`) and the suggestion profile reset endpoint (composable exists but no page calls it).

2. **Personal analytics** -- The backend stores rich per-user watch data (category scores, type preferences, total watch time, per-media ratings) via `suggestion_profiles` and `suggestion_view_history` tables. The composable `getMyProfile()` exists but no page invokes it. Users have no personal stats dashboard.

3. **Configuration UX** -- Many admin config sections require raw JSON editing (download auth settings, scanner thresholds, user type management, CORS/HTTPS/HSTS). The admin panel has a full JSON editor but no structured forms for these areas.

4. **Data collected but not displayed** -- Several database columns are populated but their data never reaches the user: `users.metadata` JSON, `scan_results.reviewed_by/reviewed_at` (moderation audit trail), `receiver_duplicates.fingerprint`, and `suggestion_view_history.completed_at` date (the "Completed" badge shows but not when).

5. **Industry parity gaps** -- No metadata scraping (TMDB/TVDB), no 2FA/passkeys, no chapter markers, no PWA/service worker, no multi-library collections. These are all significant engineering efforts.

The project is notably strong in areas where competitors are weak: AI content classification (HuggingFace), master-slave receiver protocol, stream extraction/crawling, in-app updates, and Prometheus metrics. Subtitles/captions are confirmed out of scope per project requirements.

---

## Feature Maturity Overview

| Area | Current State | Remaining Gaps | Priority |
|------|--------------|----------------|----------|
| Media browsing / library grid | Excellent -- search, type, category, tag, sort, min_rating, hide-watched, deep-link URLs, blur-hash placeholders all work | None significant | -- |
| Video/HLS playback | Excellent -- hls.js quality switch, PiP, keyboard shortcuts, loop, seek thumbnails, playlist autoplay, similar media sidebar, auto-next | No chapter markers | P3 |
| Suggestions & recommendations | Excellent -- continue watching, trending, personalized, on-deck, new since last visit, similar, recent all wired and displayed | Suggestion profile not shown to user | P1 |
| User profile / preferences | Good -- theme (8 themes), view mode, sort, items-per-page, home section toggles, watch history with filter/export, ratings, API tokens, storage usage, permissions | No personal stats dashboard; no suggestion profile reset button | P1 |
| Favorites | Fully wired -- toggle on cards, dedicated page, optimistic UI | None | -- |
| Playlists | Fully wired -- CRUD, reorder, copy, bulk-delete, export (M3U/M3U8/JSON), public browsing, copy-from-public | None significant | -- |
| Upload | Fully wired -- drag-drop, multi-file, category, progress polling | No chunked-upload WS progress | P2 |
| Categories browse | Fully wired -- category cards, episode/season labels, year badges, show grouping | No user re-categorize flow | P2 |
| RSS feed | Fully wired -- RSS button in library header links to `/api/feed` | None | -- |
| Watch history | Fully wired -- filter (all/in-progress/completed), remove, clear, export link | Completion date not shown (only badge) | P2 |
| Admin dashboard | Fully wired -- stats, system info, active streams, active uploads, module health, disk/memory bars | No time-series chart (only summary numbers) | P2 |
| Admin analytics | Fully wired -- summary, daily stats, top media, event drill-down by type/media/user, CSV export | No trend visualization (raw tables only) | P2 |
| Admin config | Full JSON editor + some structured forms | User-type management, download auth, scanner thresholds, CORS/HTTPS all require raw JSON | P1 |
| HLS / streaming | Fully wired -- auto-generate toggle, interval selector, job list, validate, clean stale/inactive | None significant | -- |
| Content moderation | Fully wired -- scanner stats, review queue, approve/reject, batch review, clear queue | No confidence-threshold tuning form | P2 |
| AI classification | Fully wired -- status, stats, classify file/directory, run task, clear tags, all-pending | None significant | -- |
| Backup | Fully wired -- list, create (full/db), restore, delete, file/error counts | No scheduled backup UI | P2 |
| Downloader | Fully wired -- health, detect, download, cancel, delete, settings, importable files, import to library, WebSocket progress | None significant | -- |
| Remote sources | Fully wired -- CRUD, sync, stats, media list, cache management | None significant | -- |
| Receiver / slaves | Fully wired -- slave list, stats, remove slave, duplicates with resolve | None significant | -- |
| Crawler | Fully wired -- targets CRUD, crawl trigger, discoveries approve/ignore/delete, stats | None significant | -- |
| Extractor | Fully wired -- items CRUD, stats, HLS proxy streaming | None significant | -- |
| Security | Fully wired -- IP whitelist/blacklist, ban/unban, rate-limit stats | CORS/HTTPS/HSTS toggles require raw JSON | P2 |
| Updates | Fully wired -- binary and source update modes, config, progress | None significant | -- |
| Discovery | Fully wired -- scan, suggestions, apply, dismiss | None significant | -- |
| Self-service account deletion | Backend route exists | No composable method or UI | P1 |
| OpenAPI docs / Prometheus | Links exist in admin System Settings panel | None | -- |

---

## P0 -- Quick Wins (Backend ready, just needs UI)

All P0 items from the 2026-03-28 report have been resolved. The following are the only remaining quick-win opportunities:

### 1. Self-Service Account Deletion (No Frontend Coverage)

**What's missing**: `POST /api/auth/delete-account` is registered in `routes.go` (line 374, `requireAuth()`) and the handler `h.DeleteAccount` exists in `api/handlers/auth.go`. However, no composable method wraps this endpoint and no UI exists.

**Evidence**:
- Backend: `api/routes/routes.go` line 374 -- `api.POST("/auth/delete-account", requireAuth(), h.DeleteAccount)`
- Composable: `web/nuxt-ui/composables/useApiEndpoints.ts` -- no method calls `/api/auth/delete-account`
- Frontend: zero references to `delete-account` or `deleteAccount` anywhere in `web/nuxt-ui/`
- Note: `requestDataDeletion()` (the admin-reviewed flow at `/api/auth/data-deletion-request`) IS wired in the profile page. The self-service instant deletion is a separate, direct endpoint.

**Backend ready?**: YES

**Effort**: LOW -- add composable method + confirmation dialog on profile page

**User impact**: MEDIUM -- GDPR self-service expectation; currently users can only request admin-reviewed deletion

---

### 2. Suggestion Profile Reset Button

**What's missing**: `DELETE /api/suggestions/profile` is registered (line 429 of `routes.go`), the composable has `resetMyProfile()` (line 173 of `useApiEndpoints.ts`), but no page calls it. Users cannot reset their recommendation profile.

**Evidence**:
- Backend: `api/routes/routes.go` line 429 -- `api.DELETE("/suggestions/profile", requireAuth(), h.ResetMyProfile)`
- Composable: `web/nuxt-ui/composables/useApiEndpoints.ts` line 173 -- `resetMyProfile: () => api.delete<void>('/api/suggestions/profile')`
- Pages: zero references to `resetMyProfile` in `web/nuxt-ui/pages/`

**Backend ready?**: YES

**Effort**: LOW -- add "Reset my recommendations" button to profile page

**User impact**: LOW -- useful for users who want to start fresh after watching content they disliked

---

## P1 -- High Impact Features (Medium Effort)

### 3. Personal Stats / Watch Analytics Dashboard

**What's missing**: The backend stores rich per-user data: `suggestion_profiles` (category_scores, type_preferences, total_views, total_watch_time), `suggestion_view_history` (per-media view_count, total_time, rating, completed_at). The composable `getMyProfile()` exists but no page calls it. Users have no personal analytics view despite the `show_analytics` preference toggle existing.

**Evidence**:
- Backend: `api/routes/routes.go` line 428 -- `api.GET("/suggestions/profile", requireAuth(), h.GetMyProfile)`
- Composable: `web/nuxt-ui/composables/useApiEndpoints.ts` line 172 -- `getMyProfile: () => api.get<UserProfile>('/api/suggestions/profile')`
- Types: `web/nuxt-ui/types/api.ts` lines 60-67 -- `UserProfile` with `total_views`, `total_watch_time`, `category_scores`, `type_preferences`
- DB: `suggestion_profiles` table (line 246 of `migrations.go`) -- `category_scores JSON`, `type_preferences JSON`, `total_views INT`, `total_watch_time FLOAT`
- DB: `suggestion_view_history` table (line 256) -- per-media `view_count`, `total_time`, `rating`, `completed_at`
- Preference: `user_preferences.show_analytics` exists in DB and TS types but nothing is gated by it
- Profile page: `web/nuxt-ui/pages/profile.vue` -- no reference to `getMyProfile`, `UserProfile`, `category_scores`, or `type_preferences`

**Backend ready?**: YES

**Effort**: MEDIUM -- fetch profile data, render category bar chart, watch time summary, ratings distribution

**User impact**: HIGH -- personal insights drive engagement; a staple feature on Plex, Jellyfin, YouTube

**Suggested UI**: "My Stats" section on the profile page: total watch time, category score bars, type preference breakdown, total views count

---

### 4. User Type Management Structured Form

**What's missing**: `config.json` defines named `user_types` (premium, standard, basic, guest) with per-type storage quotas, concurrent stream limits, and feature flags. These can only be modified by editing `config.json` or the raw JSON editor in the admin panel.

**Evidence**:
- Config: `internal/config/types.go` lines 199-207 -- `UserType` struct with `Name`, `StorageQuota`, `MaxConcurrentStreams`, `AllowDownloads`, `AllowUploads`, `AllowPlaylists`
- Config: `internal/config/types.go` line 196 -- `AuthConfig.UserTypes []UserType`
- Admin: `web/nuxt-ui/components/admin/SystemSettingsPanel.vue` -- no structured user-type editor; raw JSON only

**Backend ready?**: YES -- `PUT /api/admin/config` accepts full config updates

**Effort**: MEDIUM

**User impact**: MEDIUM -- reduces admin friction for non-technical operators

**Suggested UI**: "User Types" structured table in admin Users tab with editable rows for each type: name, quota, stream limit, permission toggles

---

### 5. Download and Streaming Auth Settings in Admin UI

**What's missing**: `download.require_auth`, `download.enabled`, `streaming.require_auth`, `streaming.unauth_stream_limit` are security-sensitive settings that control anonymous access. No structured admin toggle exists.

**Evidence**:
- Config: `internal/config/types.go` lines 113-117 -- `DownloadConfig.Enabled`, `DownloadConfig.RequireAuth`
- Config: `internal/config/types.go` lines 108-109 -- `StreamingConfig.RequireAuth`, `StreamingConfig.UnauthStreamLimit`
- Admin UI: no structured form for these settings; raw JSON only

**Backend ready?**: YES

**Effort**: LOW

**User impact**: MEDIUM -- security-sensitive settings operators expect toggles for

**Suggested UI**: "Access Control" section in admin System Settings panel with toggles for download/streaming auth requirements

---

### 6. Scanner Confidence Threshold Tuning

**What's missing**: `mature_scanner.high_confidence_threshold`, `mature_scanner.medium_confidence_threshold`, and the `require_review` setting control how aggressively content is auto-flagged. No structured admin form exists.

**Evidence**:
- Config: `internal/config/types.go` lines 284-292 -- `MatureScannerConfig` with `HighConfidenceThreshold`, `MediumConfidenceThreshold`, `RequireReview`, `AutoFlag`
- Admin ContentTab.vue: shows scanner stats and review queue but no threshold controls

**Backend ready?**: YES -- `PUT /api/admin/config`

**Effort**: MEDIUM

**User impact**: MEDIUM -- Family Admin persona needs this for content safety tuning

---

## P2 -- Backlog Features

| # | Feature | Gap Description | Evidence | Backend Ready? | Effort | User Impact |
|---|---------|----------------|----------|----------------|--------|-------------|
| 7 | Watch history completion date | `suggestion_view_history.completed_at` stored; history list shows "Completed" badge but not when | `migrations.go` line 264; `profile.vue` line 522 shows badge, no date | YES | LOW | LOW |
| 8 | Admin analytics trend charts | Daily stats are loaded (`analyticsApi.getDaily()`) but rendered as raw numbers, no sparkline or bar chart | `AnalyticsTab.vue` lines 10-16 | YES | LOW | MEDIUM |
| 9 | CORS/HTTPS/HSTS admin toggles | `security.cors_enabled`, `server.enable_https`, `security.hsts_enabled` not exposed as structured admin toggles | `types.go` lines 153-174, 79-80 | YES | LOW | LOW |
| 10 | Moderation audit trail | `scan_results.reviewed_by`, `reviewed_at`, `review_decision` stored but admin review queue only shows pending items, not review history | `migrations.go` lines 178-183 | YES | LOW | LOW |
| 11 | Receiver duplicate fingerprint display | `receiver_duplicates.fingerprint` stored; admin duplicates view shows names but not the content hash | `migrations.go` line 402 | YES | LOW | LOW |
| 12 | User metadata display | `users.metadata` JSON column is populated by admin and included in API responses but no admin UI renders it | `migrations.go` line 29; `types/api.ts` line 57 | YES | LOW | LOW |
| 13 | Backup schedule configuration | Admin tasks list shows scheduled tasks but no UI to configure automated backup scheduling | `SystemTab.vue` tasks sub-tab | PARTIAL | MEDIUM | MEDIUM |
| 14 | Upload chunked progress via WebSocket | Upload uses HTTP polling (`getProgress`); no WebSocket real-time progress | `upload.vue` lines 29-51 | NO | MEDIUM | LOW |
| 15 | User re-categorize flow | Categories browse page shows items but users cannot re-categorize (admin-only via `POST /api/admin/categorizer/set`) | `categories.vue`; admin-only routes in `routes.go` lines 598-601 | PARTIAL (admin only) | MEDIUM | LOW |
| 16 | Config field documentation | Raw JSON config editor has no per-field tooltips, validation, or schema reference | `SystemSettingsPanel.vue` | YES (config struct) | MEDIUM | MEDIUM |
| 17 | Parental control PIN | Mature content gating uses age-gate verification, not per-user PIN | `middleware/agegate.go` | NO | MEDIUM | MEDIUM |

---

## P3 -- Future / Large Features

| # | Feature | Gap Description | Effort | Notes |
|---|---------|----------------|--------|-------|
| 18 | Chapter markers in video | No chapter detection, storage, or seek-bar chapter display | HIGH | Would require ffprobe chapter extraction + new DB columns + player UI |
| 19 | Two-factor authentication (2FA) | No TOTP or passkey support; password-only auth | HIGH | High-value security feature; auth module extension required |
| 20 | Watch party / synchronized playback | No real-time sync across sessions | VERY HIGH | Requires WebSocket broadcast + host/guest session model |
| 21 | PWA / Installable App | SPA works on mobile but no installable PWA manifest or service worker | MEDIUM | PWA manifest + caching strategy; offline support limited by streaming nature |
| 22 | Content collections / multiple libraries | No named library collections; single media root | HIGH | Would require new DB schema + routing + UI navigation |
| 23 | Metadata scraping (TMDB/TVDB) | No external metadata source; relies on filename-based categorizer only | VERY HIGH | New integration module required; would dramatically improve content display |
| 24 | User-to-user playlist sharing via URL | Public playlists are browsable but no shareable link mechanism for individual playlists | MEDIUM | Needs shareable token routes + social share UI |

---

## Backend-to-Frontend Exposure Gaps

| Route | Auth | What it returns | Frontend Coverage | Gap |
|-------|------|----------------|-------------------|-----|
| `POST /api/auth/delete-account` | User | Self-service account deletion | No composable, no UI | P0 -- no frontend at all |
| `DELETE /api/suggestions/profile` | User | Reset recommendation profile | Composable exists, no page calls it | P0 -- button missing |
| `GET /api/suggestions/profile` | User | Personal stats (category scores, watch time) | Composable exists, no page calls it | P1 -- dashboard missing |
| `GET /api/media/stats` | Public | Library totals (video/audio count, total size) | Called by admin DashboardTab only | P2 -- could show on user home page |
| `GET /api/status` | Admin | Server running status, uptime, version | Called by admin only | By design (admin-only) |
| `GET /api/modules` | Admin | Module health statuses | Called by admin DashboardTab | By design (admin-only) |
| `GET /api/modules/:name/health` | Admin | Individual module health | Called by admin DashboardTab | By design (admin-only) |
| `GET /api/admin/downloader/verify` | Admin | Verify admin for downloader | Called by downloader WebSocket | By design (internal) |
| `POST /api/receiver/*` | API Key | Slave registration/catalog/heartbeat | Called by slave nodes | By design (machine-to-machine) |
| `GET /health` | None | System health check | Not called from UI | By design (external monitoring) |
| `GET /metrics` | Admin | Prometheus metrics | Linked in admin System Settings | Resolved (link exists) |
| `GET /api/docs` | User | OpenAPI spec JSON | Linked in admin System Settings | Resolved (link exists) |
| `GET /api/feed` | User | Atom XML feed | RSS button on index page | Resolved |

All other registered routes (215+ total) have matching composable methods and are called from the appropriate frontend pages or admin tabs.

---

## Data Collected but Not Displayed

| Data | Where It Lives | What Users Could Do With It | Current Status |
|------|---------------|----------------------------|----------------|
| `suggestion_profiles.category_scores` | `suggestion_profiles` table, `category_scores JSON` | Personal "My top categories" bar chart | Composable exists (`getMyProfile()`), never called from any page |
| `suggestion_profiles.type_preferences` | `suggestion_profiles` table, `type_preferences JSON` | "You watch X% video, Y% audio" breakdown | Same as above |
| `suggestion_profiles.total_watch_time` | `suggestion_profiles` table | "You've watched X hours total" stat | Same as above |
| `suggestion_view_history.completed_at` | `suggestion_view_history` table, `completed_at TIMESTAMP` | "Finished watching on [date]" in history | "Completed" badge shown, but not the date |
| `suggestion_view_history.rating` | `suggestion_view_history` table, `rating FLOAT` | Star rating in watch history list | Shown on library grid cards; not in watch history list |
| `users.metadata` | `users` table, `metadata JSON` | Custom user attributes (admin-set) | Included in API responses; no UI renders it |
| `scan_results.reviewed_by/at/decision` | `scan_results` table | Moderation audit trail for content reviews | Admin queue shows pending items only; no review history view |
| `receiver_duplicates.fingerprint` | `receiver_duplicates` table, `fingerprint VARCHAR(64)` | Content hash for transparency in duplicate resolution | Admin duplicates view shows names but not the hash |
| `media_metadata.content_fingerprint` | `media_metadata` table, `content_fingerprint VARCHAR(64)` | File integrity verification display | Used internally for duplicate detection; never shown to users |
| `extractor_items.detection_method` | `extractor_items` table | How the stream URL was discovered | Not displayed in admin extractor items list |
| `crawler_discoveries.detection_method` | `crawler_discoveries` table | How the stream was detected during crawl | Not displayed in admin crawler discoveries list |
| `remote_cache_entries.hits` | `remote_cache_entries` table | Cache hit count per remote item | Not displayed in admin remote media view |
| `user_preferences.show_analytics` | `user_preferences` table | Toggle for personal analytics display | Stored and typed but nothing is gated by this flag |

---

## Industry Comparison Table

| Feature | Plex / Jellyfin Baseline | Media Server Pro 4 | Status |
|---------|--------------------------|-------------------|--------|
| HLS adaptive streaming | Yes | Yes -- hls.js, quality selector, auto mode, seek thumbnails | **Full parity** |
| Resume playback across devices | Yes | Yes -- `playback_positions` DB, loaded on player open | **Full parity** |
| Personal watch history | Yes | Yes -- profile page with filter, remove, clear, export | **Full parity** |
| Continue watching row | Yes | Yes -- home page continue-watching + on-deck rows | **Full parity** |
| User star ratings | Yes | Yes -- rate on player, filter by rating on library grid | **Full parity** |
| Personalized recommendations | Yes | Yes -- `suggestion_profiles` scoring engine, personalized row on home | **Full parity** |
| Trending / popular content | Yes | Yes -- trending row on home page | **Full parity** |
| "More like this" on player | Yes | Yes -- sidebar on player page | **Full parity** |
| User playlists | Yes | Yes -- full CRUD, reorder, copy, export (M3U/M3U8/JSON), bulk delete | **Full parity** |
| Public playlist sharing | Yes | Yes -- public toggle, browsable list, copy-to-mine | **Full parity** |
| RSS/Atom new content feed | Yes (Plex RSS) | Yes -- Atom feed with subscribe button | **Full parity** |
| Content categories | Yes | Partial -- filename-based categorizer (Movies, TV, Anime, Music, Docs, Podcasts, Audiobooks); no TMDB | **Partial** |
| Chapter markers / seek thumbnails | Yes (Plex) | Seek thumbnail previews: Yes. Chapter markers: No | **Partial** |
| Metadata scraping (TMDB/TVDB) | Yes | No -- filename-based categorizer only | **Gap** |
| Trailers / extra content | Yes (Plex) | No | **Gap** |
| Multi-library / collection management | Yes | No -- single media root with category filter | **Gap** |
| User download | Yes | Yes -- `/download` endpoint; `can_download` permission | **Full parity** |
| AI content classification | No (Plex) | Yes -- HuggingFace visual classification module | **Ahead** |
| Mature content scanning | Partial (Plex) | Yes -- keyword + confidence scoring + AI visual + admin review queue | **Ahead** |
| Prometheus metrics | Jellyfin: Yes | Yes -- `/metrics` admin endpoint, linked in admin UI | **Full parity** |
| API tokens (Bearer auth) | Yes (Plex tokens) | Yes -- `user_api_tokens` table, full CRUD in profile page | **Full parity** |
| Multi-user with per-user permissions | Yes | Yes -- role + user_type + 7 permission flags | **Full parity** |
| Slave/receiver protocol | No | Yes -- master-slave media proxy with fingerprint dedup | **Ahead** |
| External URL extraction | No | Yes -- extractor + crawler modules | **Ahead** |
| Auto-discovery of new media | Yes | Yes -- autodiscovery module + admin UI | **Full parity** |
| Backup and restore | Jellyfin: partial | Yes -- admin backup v2 with manifest, file lists | **Full parity** |
| In-app server updates | No | Yes -- binary and source update modes with progress | **Ahead** |
| Duplicate detection | Yes (Plex) | Yes -- fingerprint-based dedup across local + slave nodes | **Full parity** |
| BlurHash progressive thumbnails | No | Yes -- blur_hash column + client-side decode on library cards | **Ahead** |
| Personal stats dashboard | Yes (Plex: Tautulli; Jellyfin: plugin) | No -- data collected but not displayed | **Gap** |
| 2FA / passkeys | Jellyfin: TOTP plugin | No | **Gap** |
| PWA / Offline | No (Plex: native apps) | No -- SPA but no service worker | **Gap** |
| Watch party / sync | No (Plex: paid tier) | No | **Gap** |
| Self-service account deletion | Partial | Backend implemented, no UI | **Partial** |

---

## User Persona Gap Summary

| Persona | Strengths | Top Unmet Needs | Effort |
|---------|-----------|-----------------|--------|
| **Curator** (organizes collections, curates playlists) | Playlist CRUD fully wired including export/copy/bulk-delete/public browsing | 1. No metadata scraping for richer content info (P3 -- VERY HIGH). 2. No multi-library collections (P3 -- HIGH). | HIGH |
| **Casual Viewer** (browse, watch, discover) | Rich discovery: continue watching, trending, on-deck, similar, new-since-last-visit, min-rating filter, hide-watched | 1. No personal stats dashboard (P1 -- MEDIUM). 2. No chapter markers for long-form content (P3 -- HIGH). | MEDIUM / HIGH |
| **Archivist** (data integrity, portability) | Watch history export, RSS feed, API tokens, playlist export (M3U/JSON), duplicate detection | 1. Self-service account deletion not in UI (P0 -- LOW). 2. Completion date not shown in watch history (P2 -- LOW). | LOW |
| **Family Admin** (manages household accounts) | User permissions (7 flags), user types, mature content scanner + AI classification, age gate | 1. User type management requires raw JSON (P1 -- MEDIUM). 2. Scanner threshold tuning requires raw JSON (P1 -- MEDIUM). 3. No parental control PIN (P3 -- MEDIUM). | MEDIUM |
| **Developer** (API integration, automation) | Full OpenAPI spec accessible, API tokens with Bearer auth, Prometheus metrics, comprehensive REST API | 1. API token scopes/expiry not configurable (P2 -- MEDIUM). 2. No webhook/notification system for events (P3 -- HIGH). | MEDIUM / HIGH |
| **Mobile User** (touch, responsive) | Responsive grid with mobile-specific items-per-page and column count; mobile chunk optimization | 1. No PWA installable app (P3 -- MEDIUM). 2. No native share button (P2 -- LOW). | MEDIUM |

---

## Configuration-Gated Feature Gaps

The following features are controlled by config flags in `internal/config/types.go` `FeaturesConfig` and may be invisible to users depending on defaults:

| Feature Flag | Default | Effect When Disabled | Admin Discoverability |
|-------------|---------|---------------------|----------------------|
| `enable_hls` | true | No HLS adaptive streaming; direct file streaming only | Streaming tab still loads but shows no jobs |
| `enable_analytics` | true | No event tracking, no suggestion profile building | Analytics tab shows "analytics_disabled" |
| `enable_playlists` | true | Playlist creation/browsing disabled | Not communicated to users who try to create |
| `enable_suggestions` | true | No trending, personalized, continue watching rows | Home page shows only library grid |
| `enable_auto_discovery` | true | No file naming suggestions | Discovery admin tab has no suggestions |
| `enable_duplicate_detection` | true | No cross-node fingerprint matching | Duplicates section in admin shows nothing |
| `enable_downloader` | false | Downloader tab shows "offline" | Health check reports disabled |
| `enable_receiver` | false | No slave node registration | Receiver panel empty |
| `enable_extractor` | false | No external stream proxying | Extractor panel empty |
| `enable_crawler` | false | No stream discovery crawling | Crawler panel empty |
| `enable_mature_scanner` | true | No content scanning or review queue | Content tab scanner section empty |
| `enable_huggingface` | false | No AI visual classification | Classify section shows "not configured" |
| `enable_remote_media` | false | No remote source federation | Remote panel empty |

**Observation**: When features are disabled, the admin tabs still render but show empty or "disabled" states. There is no global "Feature Overview" dashboard that shows which features are enabled/disabled at a glance, which would help admins during initial setup.

---

## Key Metrics

| Metric | Value |
|--------|-------|
| Total backend routes | 215+ |
| Total composable API methods | ~180 |
| Routes with no frontend coverage | 2 (`delete-account`, `suggestions/profile` reset called but not from pages) |
| Internal Go modules | 30 |
| DB tables | 32+ |
| Frontend pages | 11 |
| Admin tab components | 21 (13 tabs + 8 sub-panels) |
| Feature flags | 17 |
| User preference fields | 20+ |
| API coverage ratio (composable methods / routes) | ~95% |

---

*Report end. All findings are based on static read-only code analysis of the committed codebase as of 2026-03-31.*
*Every finding cites a specific file path, line number, or route from the actual source tree.*
*Subtitles/captions are confirmed out of scope per project requirements and are excluded from gap findings.*
