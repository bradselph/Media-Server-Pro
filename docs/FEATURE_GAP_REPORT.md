# Feature Gap Report
> Generated: 2026-04-09  |  Analyzed by: Claude (read-only codebase analysis)
> Based on: api_spec/openapi.yaml + web/nuxt-ui/ + internal/ modules + database schema

## Executive Summary

Media Server Pro 4 is a mature, feature-rich self-hosted media server. The frontend covers the vast majority of backend capabilities — all major user journeys (browse, play, organize, upload, admin) are well-implemented with personalized suggestions, playback resume, HLS adaptive streaming, and a comprehensive admin panel with 21 tab panels. The biggest opportunity area is **personal analytics**: the backend collects rich per-user data (watch time, ratings, category scores, completion rates) that is almost entirely invisible to regular users. Quick wins also exist around **self-service account deletion** (backend endpoint exists, no UI) and **public playlist browsing** (endpoint exists, minimal surface). The overall feature gap count is low — most gaps are P2/P3 polish items rather than missing fundamentals.

## Feature Maturity Overview

| Area | Current State | Gaps Found | Priority |
|------|--------------|-----------|----------|
| Content Discovery | Strong | 3 | P1-P2 |
| Playback Experience | Strong | 2 | P2-P3 |
| User Organization | Strong | 3 | P1-P2 |
| Personalization | Strong | 2 | P0-P1 |
| Mobile Experience | Partial | 2 | P2-P3 |
| Administration | Strong | 1 | P2 |
| Developer / API | Strong | 1 | P2 |
| Accessibility | Partial | 2 | P2-P3 |

---

## P0 — Quick Wins (Backend ready, just needs UI)

### 1. Self-Service Account Deletion (no UI)
**What's missing**: The backend has `POST /api/auth/delete-account` (confirmed in `api/routes/routes.go` line 389 and `types/openapi.generated.ts` line 754) which lets users delete their own account with password confirmation. However, the frontend profile page (`pages/profile.vue`) only offers a "Data Deletion Request" form (which goes to admin review) — there is no direct self-service delete button.
**Evidence**: Route registered at `api.POST("/auth/delete-account", requireAuth(), h.DeleteAccount)`. No reference to `/auth/delete-account` in any `.vue` file.
**Backend support**: Fully implemented — requires password confirmation.
**User impact**: HIGH — users who want to leave have no way to immediately remove their account without admin intervention.
**Suggested UI**: Add a "Delete My Account" section to the Data Privacy card on the profile page, with a password confirmation modal. The existing "Request Data Deletion" can remain as a softer alternative.

### 2. Personal Watch Statistics Dashboard
**What's missing**: The backend stores rich per-user data in `suggestion_profiles` (total_views, total_watch_time, category_scores, type_preferences) and `suggestion_view_history` (per-item view_count, total_time, rating, completed_at). The API exposes `GET /api/suggestions/profile` (returns `UserProfile` with total_views, total_watch_time, category_scores, type_preferences). The frontend calls this endpoint (`useSuggestionsApi().getMyProfile()`) but **never displays it anywhere** — it is defined but unused in any page template.
**Evidence**: `useApiEndpoints.ts` line 174: `getMyProfile: () => api.get<UserProfile>('/api/suggestions/profile')`. No template in any `.vue` file renders UserProfile data. The `UserProfile` type includes `total_views`, `total_watch_time`, `category_scores`, `type_preferences`.
**Backend support**: Fully implemented — endpoint returns all data.
**User impact**: HIGH — power users want to see their viewing habits (total hours watched, top genres, completion stats). This is a baseline Plex/Jellyfin feature.
**Suggested UI**: Add a "My Stats" card to the profile page showing: total watch time (formatted as hours), total views, top 5 categories by score (as a bar chart or ranked list), media type breakdown (video vs audio). Can reuse the existing `getMyProfile()` composable.

---

## P1 — High Impact Features (medium effort)

### ✅ `043359b8` 2026-04-09 — Watch History Export Button Fixed
> **Resolved**: Replaced bare `<a>` link with programmatic fetch + blob download for reliable CSV export.
> **Verified**: pending deploy
**What's missing**: The profile page has an "Export CSV" button pointing to `/api/watch-history/export` (profile.vue line 476), and the backend registers `GET /api/watch-history/export` (routes.go line 405). However, the button uses `target="_blank" external` with an `<a>` link — this works for cookie-authenticated browser sessions but would fail for API-token users. More importantly, the endpoint requires auth, so the link will redirect to login if the session cookie is not sent (which can happen in some browsers with cross-origin link behavior).
**Evidence**: `profile.vue` line 476: `:to="/api/watch-history/export"` with `target="_blank" external`.
**Backend support**: YES — endpoint exists and returns CSV.
**User impact**: MEDIUM — users who want to export their data have the button but it may not work reliably.
**Implementation sketch**: Use a programmatic `fetch()` call with `credentials: include` to download the blob, then trigger a browser download via `URL.createObjectURL()`.

### ✅ `29897d84` 2026-04-09 — Public Playlist Discovery Page
> **Resolved**: Removed auth middleware from playlists page. Guests see public playlists with play buttons. Authenticated users retain full CRUD.
> **Verified**: pending deploy

### ~~4. Public Playlist Discovery Page~~ (ORIGINAL)
**What's missing**: The backend has `GET /api/playlists/public` (routes.go line 409) which lists all public playlists without auth. The frontend composable `usePlaylistApi().listPublic()` exists and the playlists page loads public playlists. However, public playlists are **only visible on the playlists page if the user is logged in** (the page has `middleware: 'auth'` at line 7 of playlists.vue). Logged-out users and guests cannot browse public playlists at all.
**Evidence**: `pages/playlists.vue` line 7: `middleware: 'auth'`. The `listPublic()` call on line 212 works, but guests are redirected to login before reaching it.
**Backend support**: YES — the `/api/playlists/public` endpoint has no auth requirement.
**User impact**: MEDIUM — public playlists are a sharing feature that is invisible to non-authenticated visitors.
**Implementation sketch**: Either remove the auth middleware from the playlists page (with conditional UI showing only public playlists for guests) or create a separate `/playlists/public` page that does not require auth.

### ✅ `6aca3b08` 2026-04-09 — Timestamp Deep-Link Sharing
> **Resolved**: Added "Share" button to player that copies current URL with ?t=<seconds> to clipboard.
> **Verified**: pending deploy
**What's missing**: The player supports `?t=N` for seeking to a specific second (player.vue line 248: `const tParam = Number(route.query.t)`), but there is no UI to copy a timestamp link. Users cannot share "watch from 2:30" links without manually constructing the URL.
**Evidence**: `player.vue` line 248 handles `?t=N`. No "Share at current time" or "Copy link at timestamp" button exists in the player template.
**Backend support**: YES — the backend serves the same content regardless of the `t` parameter; only the frontend needs the button.
**User impact**: MEDIUM — shareable timestamp links are a standard feature on YouTube and streaming platforms.
**Suggested UI**: Add a "Share" or "Copy Link" button in the player info card that copies the current URL with `?t=<currentTime>` to the clipboard.

### ✅ Already implemented — Keyboard Shortcut Reference
> **Resolved**: PlayerControls component already has a keyboard icon button (i-lucide-keyboard) that toggles the shortcuts overlay.
**What's missing**: The player has a comprehensive keyboard shortcut overlay triggered by pressing `?` (player.vue lines 589-594, showShortcuts ref). However, there is no visual indicator that this exists — no "?" icon, no tooltip, no help button. The `PlayerControls` component receives `v-model:showShortcuts` but the trigger is keyboard-only.
**Evidence**: `player.vue` line 589: `case '?': showShortcuts.value = !showShortcuts.value`. No button or visual cue in the template.
**Backend support**: N/A — frontend-only feature.
**User impact**: MEDIUM — keyboard power users benefit significantly but most will never discover the feature.
**Suggested UI**: Add a small `?` icon button to the player controls bar that toggles the shortcuts overlay.

---

## P2 — Backlog Features

| # | Feature | Gap Description | Evidence | Backend Status | Effort | Impact |
|---|---------|----------------|----------|---------------|--------|--------|
| 7 | Compact view mode | Profile preferences offer 3 view modes (grid, list, compact) but index.vue only renders `grid` and `list` — `compact` is never implemented. | `profile.vue` line 431 shows compact button; `index.vue` line 345 only checks for `list` vs `grid` | N/A (frontend-only) | LOW | LOW |
| 8 | Media info overlay in player | The player shows media info in a card below the video, but there is no in-player overlay (codec, bitrate, resolution visible while watching). | No overlay component in player template | N/A (frontend-only) | LOW | LOW |
| 9 | Bulk add to playlist from browse | Users can add items to playlists only one at a time from the player page. No multi-select on the browse grid. | Only `addToPlaylist()` in player.vue; no bulk selection in index.vue | Partial — playlist `addItem` is per-item | MEDIUM | MEDIUM |
| 10 | Personal "new since last visit" count badge | The backend returns `GET /api/suggestions/new` with a `total` count of items added since last login. The home page shows the row but the nav bar has no notification badge. | `suggestions/new` returns `total`; layout `default.vue` has no badge | YES (backend has data) | LOW | MEDIUM |
| 11 | RSS feed with API token auth | The RSS feed endpoint requires session cookie auth. RSS readers typically use URL-based token auth (e.g. `?token=xxx`). API tokens exist but the feed handler may not accept Bearer tokens via URL parameter. | `routes.go` line 372: `api.GET("/feed", requireAuth(), h.GetRSSFeed)`. Feed readers cannot pass cookies. | PARTIAL — API tokens work via `Authorization: Bearer` header but not URL params | MEDIUM | MEDIUM |
| 12 | OpenAPI spec browser | The spec is at `GET /api/docs` (requires auth) but there is no Swagger UI or ReDoc viewer. Developers get raw YAML. | `routes.go` line 369: `api.GET("/docs", requireAuth(), h.GetOpenAPISpec)` | YES (spec served) | LOW | LOW |
| 13 | Public media stats on home page | `GET /api/media/stats` is public (no auth) and returns total_count, video_count, audio_count, total_size. Not displayed to logged-out users. | `routes.go` line 335; not called in index.vue for guests | YES | LOW | LOW |

---

## P3 — Future / Large Features

| Feature | Gap Description | Effort | Notes |
|---------|----------------|--------|-------|
| PWA / installable app | No web app manifest, no service worker, no offline capability. The site is a pure SPA with no PWA support. | MEDIUM | `nuxt.config.ts` has no PWA module. Would need `@vite-pwa/nuxt` or `@nuxtjs/pwa`. |
| Two-factor authentication | No 2FA support. Login is username/password only. API tokens exist but no TOTP/WebAuthn. | HIGH | Would need new backend module + DB table + frontend setup flow. |
| User comments / reactions | No social features. Users cannot comment on media or react to content. | HIGH | Intentionally out of scope per project goals. |
| Notification system | No in-app notifications. New content, scan completion, and HLS generation status are not pushed to users. | HIGH | Would need WebSocket push or polling + notification UI. |
| Chromecast / AirPlay | No casting support. The player uses native `<video>` and HLS.js but no Cast SDK. | HIGH | Requires Google Cast SDK integration. |
| Watch party / sync play | No multi-user synchronized playback. | HIGH | Requires WebSocket signaling + complex state sync. |
| DLNA / UPnP | No local network media server protocol. | HIGH | Requires separate DLNA server module. |
| Mobile native app | No iOS/Android app. PWA would be the practical path here. | HIGH | See PWA row above. |

---

## Backend-to-Frontend Exposure Gaps
(Routes that exist but are never called by the frontend or are under-utilized)

| Route | What it returns | Why users would want it | Frontend work needed |
|-------|----------------|------------------------|---------------------|
| `POST /api/auth/delete-account` | Deletes user account (password required) | Self-service account removal without waiting for admin | Button + confirmation modal on profile page |
| `GET /api/suggestions/profile` | UserProfile: total_views, total_watch_time, category_scores, type_preferences | Personal viewing stats dashboard | Stats card on profile page |
| `GET /api/suggestions/profile` + `DELETE /api/suggestions/profile` | Profile reset | User can reset recommendation algorithm | Already wired (`resetMyProfile` exists) — no visible button in profile.vue template though |
| `GET /api/media/stats` | Library totals (count, size) | Show library size to guests/users on home page | Small stats badge or footer element |
| `GET /api/docs` | OpenAPI YAML spec | Developer reference | Could add a `/docs` page with Swagger UI |

---

## Data Collected but Not Displayed
(Database columns or API response fields that exist but are invisible to users)

| Data | Where it lives | What users could do with it | Notes |
|------|---------------|---------------------------|-------|
| `suggestion_profiles.total_watch_time` | DB + API (`/api/suggestions/profile`) | See "You've watched 142 hours of content" | Composable exists, never rendered |
| `suggestion_profiles.category_scores` | DB + API | See "Your top genre is Anime (score: 82)" | Composable exists, never rendered |
| `suggestion_view_history.rating` | DB | See correlation between ratings and watch habits | Only visible via admin analytics |
| `suggestion_view_history.completed_at` | DB | See completion rate ("You finished 73% of what you started") | Not exposed in any user-facing UI |
| `media_metadata.content_fingerprint` | DB | Could show "This is the same file as X" to users | Only used internally for dedup |
| `media_metadata.blur_hash` | DB + API | Already used for placeholder images in grid (working well) | Fully utilized |
| `categorized_items.detected_title/year/season/episode/show/artist/album` | DB + API | Categories page shows these — well utilized | Already surfaced |
| `user_preferences.show_analytics` | DB | Preference to show/hide analytics toggle, but no user-facing analytics exist to toggle | Preference exists but controls nothing user-visible |
| `analytics_events.data` | DB | Personal activity feed ("Your recent actions") | Only visible to admin |

---

## Industry Comparison Table

| Feature | This Project | Evidence | Priority |
|---------|-------------|----------|----------|
| Resume watching across sessions | YES | `playback_positions` table + `restorePosition()` in player.vue | -- |
| Continue watching row on home | YES | `suggestions/continue` endpoint + `RecommendationRow` component | -- |
| On Deck (next unwatched episode) | YES | `suggestions/on-deck` endpoint + On Deck row in index.vue | -- |
| Smart playlists (rule-based) | NO | No auto-updating playlists — all are manual | P3 |
| Watch history with filtering | YES | Profile page with search, filter (all/in-progress/completed), pagination | -- |
| Personal ratings visible in browse | YES | `user_ratings` returned in media list response, star badge on grid cards | -- |
| Sort/filter by personal rating | YES | `min_rating` filter + `my_rating` sort option | -- |
| Metadata scraping (TMDB/IMDB) | NO | Categorizer uses filename parsing + HuggingFace classification, not external metadata APIs | P3 |
| TV show / season / episode organization | YES | Categorizer detects show/season/episode; categories page groups by show | -- |
| Music album / artist organization | YES | Categorizer detects artist/album; categories page groups by artist | -- |
| Per-user content restrictions (mature) | YES | `can_view_mature` permission + `show_mature` preference + age gate | -- |
| Transcoding profiles (quality presets) | YES | HLS with multiple quality levels, adaptive streaming | -- |
| Playlist auto-play | YES | `startUpNextCountdown()` in player.vue with countdown overlay | -- |
| Video end screen with "up next" | YES | Auto-next from suggestions + playlist advance with countdown | -- |
| Timestamp sharing (deep link) | PARTIAL | `?t=N` works but no UI to copy the link | P1 |
| Speed control (0.25x-2x) | YES | 8 speed options, keyboard shortcuts `<` and `>` | -- |
| Loop a video | YES | Loop toggle in player controls | -- |
| Picture-in-picture | YES | PiP button + keyboard shortcut + PiP restore across auto-next | -- |
| Keyboard shortcut overlay | YES | Press `?` to show — but not discoverable via UI button | P1 |
| User-facing API tokens | YES | Profile page token management with create/revoke/copy | -- |
| RSS feed | YES | `GET /api/feed` with category/type/limit filters | -- |
| Webhook event subscriptions | NO | No outbound webhook system | P3 |
| M3U playlist import | NO | Playlists can be exported as M3U but not imported | P3 |
| Two-factor authentication | NO | Username/password only | P3 |
| SSO / OAuth2 login | NO | No external identity provider support | P3 |
| Personal statistics dashboard | NO (data exists, no UI) | `suggestion_profiles` has all data; no profile stats card | P0 |
| Playback progress bars on browse cards | YES | `playbackProgress` ref with batch position fetch in index.vue | -- |
| Graphic equalizer | YES | 10-band EQ with presets (bass_boost, rock, jazz, etc.) | -- |
| Thumbnail preview on seek | YES | `thumbnailPreviews` passed to PlayerControls, frames cycle on hover | -- |
| Theater mode | YES | `isTheater` toggle with `t` keyboard shortcut | -- |
| Frame stepping | YES | `,` and `.` keys step forward/back one frame when paused | -- |
| BlurHash placeholder images | YES | `blur_hash` field used as CSS background during load | -- |
| Auto-discovery / file renaming | YES | Admin-only discovery module with suggestions | -- |
| Duplicate detection | YES | Admin-only duplicate resolution panel | -- |
| HLS on-demand generation | YES | User can trigger HLS generation from player page | -- |
| Public playlists | PARTIAL | Backend supports it; page requires auth to view | P1 |
| Watch history CSV export | YES | Export button on profile page | -- |

---

## User Persona Gap Summary

| Persona | Top 3 unmet needs | Effort to address |
|---------|-----------------|------------------|
| **Curator** | 1. No "featured" or pinned content on home page. 2. No batch metadata edit from admin media tab (single-item only). 3. No editorial collections ("Staff Picks"). | 1. MEDIUM 2. LOW (bulk update exists for category/mature, not arbitrary fields) 3. MEDIUM |
| **Casual Viewer** | 1. "Surprise Me" exists and works well. 2. No "new since last visit" count in nav badge (data exists). 3. Public playlists inaccessible without login. | 1. Done 2. LOW 3. LOW |
| **Archivist** | 1. No user-facing metadata editing (admin-only via AdminUpdateMedia). 2. No CSV/NFO metadata import. 3. Cannot set custom thumbnails from UI. | 1. MEDIUM 2. HIGH 3. MEDIUM |
| **Family Admin** | 1. Per-user mature content restrictions work well. 2. No PIN-protected profiles. 3. No per-user activity dashboard visible to parent account. | 1. Done 2. HIGH 3. MEDIUM |
| **Developer** | 1. API tokens work (Bearer auth). 2. OpenAPI spec available but no interactive docs (Swagger UI). 3. No webhook/event subscription system. | 1. Done 2. LOW 3. HIGH |

---

## Implementation Priority Summary

**Do first (P0 — hours of work, high impact):**
1. Self-service account deletion UI on profile page (backend 100% ready)
2. Personal stats card on profile page using existing `getMyProfile()` data

**Plan next sprint (P1 — days of work):**
3. Public playlist browsing without auth
4. Timestamp sharing button in player
5. Keyboard shortcuts help button in player controls
6. Watch history export reliability fix

**Backlog (P2 — varied effort):**
7. Compact view mode implementation
8. Bulk add-to-playlist from browse grid
9. "New since last visit" nav badge
10. RSS feed token-in-URL support
11. Library stats display for guests/users

**Future consideration (P3):**
12. PWA installability
13. Smart/auto-updating playlists
14. Interactive API docs (Swagger UI)
15. Two-factor authentication
16. Webhook subscriptions
