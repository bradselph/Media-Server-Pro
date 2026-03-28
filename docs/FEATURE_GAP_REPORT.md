# Feature Gap Report — Media Server Pro 4

**Generated:** 2026-03-28
**Source:** Cross-reference of `docs/BACKEND_API_SUMMARY.md`, `docs/FRONTEND_CODE_AUDIT.md`, `api_spec/openapi.yaml`, `api/routes/routes.go`, `internal/database/migrations.go`, `internal/suggestions/suggestions.go`, `internal/categorizer/categorizer.go`, `internal/config/types.go`, and all frontend composables + pages.

---

## Executive Summary

The backend is substantially more capable than the frontend exposes. The most impactful gaps are:

1. **TV show / music organization is invisible** — the categorizer already detects season, episode, show name, artist, and album for every file; none of this reaches users.
2. **No personal stats dashboard** — the suggestion engine stores per-user total watch time and category taste scores that are never shown.
3. **No "Watch Later" or Favorites** — users must create a playlist manually; the concept doesn't exist as a first-class feature.
4. **No PWA / installability** — the site is not installable and has no offline fallback.
5. **No RSS, webhooks, or user API tokens** — power users and developers have no programmatic access.
6. **Several rich preference fields are stored in the DB but never typed or exposed** — `subtitle_lang`, `filter_category`, `filter_media_type` are in the DB but absent from the TypeScript types.

---

## Scoring Key

| Field | Values |
|-------|--------|
| **Backend ready?** | `YES` · `PARTIAL` · `NO` |
| **Effort** | `LOW` (UI only) · `MEDIUM` (some backend work) · `HIGH` (significant new feature) |
| **User impact** | `HIGH` · `MEDIUM` · `LOW` |

---

## Lens 1 — Backend-to-Frontend Exposure Gap

Routes the backend registers that the frontend never calls (or calls only in an admin context but not for regular users).

### 1.1 `GET /api/categorized` — TV/Music metadata never shown

**Evidence:**
- `api/routes/routes.go`: `r.GET("/api/categorized", h.GetCategorizedItems)` — auth-gated, accessible to all logged-in users.
- `internal/categorizer/categorizer.go`: detects `CategoryTVShows`, `CategoryAnime`, `CategoryMovies`, `CategoryMusic`, `CategoryPodcasts`, `CategoryAudiobooks`.
- `internal/database/migrations.go` — `categorized_items` table has `detected_season INT`, `detected_episode INT`, `detected_show VARCHAR(255)`, `detected_artist VARCHAR(255)`, `detected_album VARCHAR(255)`, `detected_year INT`, `confidence FLOAT`, `manual_override BOOLEAN`.
- `web/nuxt-ui/composables/useApiEndpoints.ts`: The only call to categorizer routes is inside `useAdminApi()` — regular users never see categorized content.

**Gap:** Users cannot browse by TV show, season, or music artist/album. A file named `Game.of.Thrones.S03E07.mkv` is correctly identified as TV show, season 3, episode 7, but the user sees it only as a flat list item. There is no series view, no episode grouping, and no music album page.

**Backend ready?** YES
**Effort:** MEDIUM — needs a TV/music browse page in the frontend that calls `/api/categorized?category=TV+Shows` etc.
**User impact:** HIGH

---

### 1.2 `suggestion_profiles` data — Personal taste profile never surfaced

**Evidence:**
- `internal/suggestions/suggestions.go`: `UserProfile` struct has `CategoryScores map[string]float64`, `TypePreferences map[string]float64`, `TotalViews int`, `TotalWatchTime float64`.
- `internal/database/migrations.go`: `suggestion_profiles` table stores `category_scores JSON`, `total_views INT`, `total_watch_time FLOAT`.
- No `/api/suggestions/profile` or `/api/me/stats` route exists in `routes.go`.

**Gap:** The recommendation engine builds a rich per-user taste profile (favourite categories, watch time per type, total views), but users can never see it. A "My Stats" or "My Taste" page is entirely absent.

**Backend ready?** PARTIAL — data is computed and persisted; a new `GET /api/suggestions/profile` endpoint needs to be added to expose it.
**Effort:** MEDIUM
**User impact:** MEDIUM

---

### 1.3 Suggestion `reasons` field never displayed

**Evidence:**
- `internal/suggestions/suggestions.go`: `Suggestion.Reasons []string` — populated with human-readable strings like "Matches your top category: Movies".
- `web/nuxt-ui/types/api.ts`: `Suggestion` type includes `reasons?: string[]`.
- `web/nuxt-ui/pages/player.vue` and `index.vue`: suggestion cards render `title`, `thumbnail`, `category` — never show `reasons`.

**Gap:** The "why is this suggested?" signal is silently discarded. Displaying it (even as a tooltip or small label) would make recommendations feel less like magic-black-box and more trustworthy.

**Backend ready?** YES
**Effort:** LOW — UI change only
**User impact:** LOW

---

### 1.4 `GET /api/ratings` — No way to browse own ratings

**Evidence:**
- `api/routes/routes.go`: `r.POST("/api/ratings", ...)` exists; no corresponding `GET /api/ratings` route is registered for users to retrieve their own rating list.
- `web/nuxt-ui/composables/useApiEndpoints.ts`: `useRatingsApi()` only has `record()` (POST).

**Gap:** Users submit star ratings in the player but cannot:
- See their own rated items as a list (e.g. "My 5-star items")
- Filter/sort browse results by their own rating
- Find items they haven't rated yet

**Backend ready?** NO — a `GET /api/ratings` endpoint needs to be added.
**Effort:** MEDIUM
**User impact:** MEDIUM

---

### 1.5 `subtitle_lang` preference stored but not typed or exposed

**Evidence:**
- `internal/database/migrations.go`: `user_preferences` table has column `subtitle_lang VARCHAR(10)`.
- `api_spec/openapi.yaml` `UserPreferences` schema: does not include `subtitle_lang`.
- `web/nuxt-ui/types/api.ts` `UserPreferences` interface: does not include `subtitle_lang`.
- `web/nuxt-ui/pages/player.vue`: no subtitle track button or track selector in the player template.

**Gap:** Subtitle language preference is stored in the database but has no pathway to the user. The player has no subtitle track selection, no "always show captions" toggle, and no auto-select by preferred language.

**Backend ready?** PARTIAL — preference storage is ready; the player would also need ffprobe track enumeration (a new endpoint) and either multi-track HLS or soft-subtitle injection.
**Effort:** MEDIUM
**User impact:** MEDIUM

---

### 1.6 `filter_category` / `filter_media_type` preferences not in TypeScript types

**Evidence:**
- `internal/database/migrations.go`: `user_preferences` has `filter_category VARCHAR(255)`, `filter_media_type VARCHAR(50)`.
- `web/nuxt-ui/pages/index.vue` lines 36–37: **reads** these fields directly (`authStore.user?.preferences?.filter_category`, `filter_media_type`) to seed the browse params on page load.
- `web/nuxt-ui/types/api.ts` `UserPreferences` interface: neither field is declared — TypeScript infers them as `any` via optional chaining, masking the type error.

**Gap:** The preference fields work by accident (untyped access). They are never offered as saveable settings in the Profile page, meaning the user's last-used filters are not actually persisted between sessions unless the backend saves them implicitly on the next preferences update.

**Backend ready?** YES
**Effort:** LOW — add fields to the TypeScript type; expose save buttons in the Profile/preferences UI.
**User impact:** MEDIUM

---

### 1.7 `custom_eq_presets` preference — no UI to create or manage

**Evidence:**
- `api_spec/openapi.yaml` `UserPreferences` schema: `custom_eq_presets: object, additionalProperties: true`.
- `web/nuxt-ui/types/api.ts`: `custom_eq_presets?: Record<string, unknown>`.
- Profile page: allows selecting an EQ preset by name but has no panel to define, save, or delete custom presets.

**Gap:** The preference slot exists but the creation UI does not. Users can pick from preset names (if defined externally) but cannot author their own.

**Backend ready?** YES
**Effort:** LOW-MEDIUM
**User impact:** LOW

---

### 1.8 Public playlist sharing — no shareable link in the UI

**Evidence:**
- `api_spec/openapi.yaml` `Playlist` schema: `is_public: boolean`, `cover_image: string`.
- `web/nuxt-ui/composables/useApiEndpoints.ts`: `create()` accepts `is_public` parameter.
- `web/nuxt-ui/pages/playlists.vue`: playlist creation/edit form; unclear whether `is_public` toggle is exposed, but even if it is, there is no "Copy share link" button that generates a URL others can visit.

**Gap:** Playlists can be public at the data layer, but no shareable URL format (e.g. `/playlists/:id`) is generated, nor is there a public playlist viewer page that renders without authentication.

**Backend ready?** PARTIAL — depends on whether `GET /api/playlists/:id` works unauthenticated for public playlists.
**Effort:** MEDIUM
**User impact:** MEDIUM

---

### 1.9 Duplicate detection results invisible to uploaders

**Evidence:**
- `internal/duplicates/` module runs; results stored in DB.
- `api/routes/routes.go`: `GET /api/admin/duplicates` — admin only.
- No `/api/upload/:id/duplicate-warning` or similar user-facing route.

**Gap:** When a user uploads a file that the duplicate scanner identifies as a near-match of existing content, they receive no warning. Duplicate detection is entirely admin-facing.

**Backend ready?** PARTIAL — data exists; needs a user-facing notification pathway.
**Effort:** MEDIUM
**User impact:** LOW

---

### 1.10 HLS quality profiles are config-only, not user-selectable per session

**Evidence:**
- `internal/config/types.go` `HLSConfig.QualityProfiles []HLSQuality` — admin sets quality profiles in `config.json`.
- `web/nuxt-ui/pages/player.vue`: quality dropdown appears only when `qualities.length > 0` (HLS active); user can switch between pre-generated HLS renditions.
- No endpoint allows a user to request "generate HLS at a specific custom profile" — they get what the admin configured.

**Gap:** Users cannot request a one-off transcode at a quality not in the server config. This is acceptable for the current scope but worth noting as a future item.

**Backend ready?** PARTIAL
**Effort:** HIGH
**User impact:** LOW

---

## Lens 2 — User Journey Mapping

### 2A — First-Time User (Onboarding)

| Question | Current State | Gap |
|----------|--------------|-----|
| Welcome screen / onboarding | None | No tour, no "what is this?" description visible before login |
| Site description before login | None | The login page shows only a logo + form |
| Post-registration guidance | None | After registering, user lands on the library with no orientation |
| `AllowRegistration: false` messaging | No feedback | When registration is closed there is no "registration is not open" message; the Register button likely still shows or the form silently fails |
| Guest experience | Unclear | `allow_guests` is surfaced in the session check; whether guests see a degraded but intentional UI is not documented |

**Key gap:** No onboarding whatsoever. A first-time visitor has no way of knowing what the platform offers before creating an account.

---

### 2B — Content Discovery Journey

| Feature | Status | Evidence | Gap |
|---------|--------|----------|-----|
| Text search | ✓ Full | Multi-word AND logic (v0.115) | — |
| Search by tag | ✗ Missing | `/api/media` accepts `tag` param? Unconfirmed | No tag browse page |
| Search by resolution/codec/duration/year | ✗ Missing | Not in `MediaListParams` | No advanced filter |
| Browse by TV show / season | ✗ Missing | Categorizer runs but `/api/categorized` not surfaced | — |
| Browse by music artist / album | ✗ Missing | Same root cause as above | — |
| Browse by tag (tag cloud) | ✗ Missing | Tags on `MediaItem` but no tag browse page | — |
| Random / Surprise Me button | ✗ Missing | No `/api/suggestions/random` or shuffle in browse | — |
| "New since last visit" section | ✗ Missing | Watch history + date_added exist, not cross-referenced | — |
| Continue watching row | ✓ Full | `/api/suggestions/continue` (v0.115) | — |
| Sort by personal rating | ✗ Missing | Ratings stored but not a sort field | — |
| Progress bars on media cards | ✗ Missing | Position data exists per user; not returned in `/api/media` list | — |
| Filter saved as user preference | ✗ Partial | DB columns exist, not in TS types, not in settings UI | — |

---

### 2C — Playback Journey

| Feature | Status | Evidence |
|---------|--------|----------|
| Playback speed control (0.5×–2×) | ✓ | `cycleSpeed` button in player template (line 458) |
| HLS adaptive quality | ✓ | Quality dropdown in player (HLS only) |
| Seek bar with thumbnail previews | ✓ | `onSeekBarMouseMove` + `ThumbnailPreviews` (player.vue line 91) |
| Resume playback position | ✓ | `/api/playback` save/restore |
| Volume slider | ✓ | `<input type="range">` in player controls |
| Fullscreen | ✓ | `toggleFullscreen` function |
| Play/pause, ±10s skip | ✓ | Button row in player |
| Subtitle track selection | ✗ | `subtitle_lang` pref exists in DB but no player UI |
| Multiple audio track selection | ✗ | No track enumeration endpoint or player button |
| Chapter support | ✗ | No chapter data in HLS jobs; no chapter list UI |
| Picture-in-picture (PiP) | ✗ | No PiP button; browser API available |
| Loop / A-B loop | ✗ | No loop mode |
| Keyboard shortcut overlay (press ?) | ✗ | No shortcut reference or overlay |
| Timestamp URL deep link (`?t=123`) | ✗ | No position query param handled on page load |
| "Up next" countdown / auto-advance | ✗ | Playlist-aware player but no end-screen countdown |
| Cast (Chromecast / AirPlay) | ✗ | Not implemented |
| Screenshot frame capture | ✗ | Not implemented |
| "Reasons" display for suggestions | ✗ | Returned by API, not shown in sidebar |
| Audio player (non-video) | ✓ Partial | Uses native `<audio controls>` — no custom EQ UI in player itself |

---

### 2D — Organization Journey

| Feature | Status | Gap |
|---------|--------|-----|
| Playlists (CRUD) | ✓ Full | Full CRUD, reorder, copy, export (m3u/m3u8/json) |
| Watch history (view/clear) | ✓ Full | `/api/watch-history` list, remove item, clear all |
| Ratings (star) | ✓ Full | 5-star submit in player |
| Favorites / Bookmarks | ✗ Missing | No `favorites` table in DB; no quick-add button on browse cards |
| Watch Later queue | ✗ Missing | Must create a playlist manually; no first-class "watch later" |
| Progress bar on media cards | ✗ Missing | Position data saved per user but not returned in media list endpoint |
| "Hide watched" / completed mode | ✗ Missing | No completed state, no hide-watched filter |
| Smart playlists (rule-based) | ✗ Missing | No rule engine in DB or backend |
| Personal notes / annotations | ✗ Missing | No annotations table in DB |
| "My Ratings" browse page | ✗ Missing | No GET /api/ratings endpoint |
| Browse items I've completed | ✗ Missing | `suggestion_view_history.completed_at` exists but not exposed |

---

### 2E — Power User Journey

| Feature | Status | Evidence |
|---------|--------|----------|
| Watch history (paginated list) | ✓ Partial | `limit` param supported; no "load more" UI in profile page |
| Bulk actions (user-facing) | ✗ Missing | Bulk actions exist in admin only; users can't multi-select to add to playlist |
| Personal statistics dashboard | ✗ Missing | Data in `suggestion_profiles`; no user-facing endpoint or page |
| Export watch history | ✗ Missing | No personal data export endpoint for users |
| Export ratings | ✗ Missing | Same |
| User-facing API token | ✗ Missing | No token generation; all auth is session-cookie only |
| RSS feed per category/tag | ✗ Missing | No `/feed` routes |
| Webhook event subscriptions | ✗ Missing | Not in config or routes |
| Import M3U playlist | ✗ Missing | Export works (m3u); import does not |
| Email notifications | ✗ Missing | No email config or notification system |
| Two-factor authentication | ✗ Missing | Password + session only |
| SSO / OAuth2 login | ✗ Missing | Not planned |
| Keyboard shortcuts (global) | ✗ Missing | No global keyboard shortcut system |
| History full-browse (back-paginated) | ✗ Partial | `limit` parameter works but no UI to load older history |

---

### 2F — Mobile User Journey

| Feature | Status | Evidence |
|---------|--------|----------|
| Responsive layout | ✓ | Tailwind breakpoints, `max-md` variants throughout |
| Safe area / notch support | ✓ Partial | `env(safe-area-inset-bottom)` used in player wrapper |
| Fullscreen landscape | ✓ | Fullscreen button in player |
| Native `<audio>` controls (mobile) | ✓ | Audio player uses native `<audio controls>` |
| PWA manifest / installable | ✗ Missing | No `manifest.json`, no `<link rel="manifest">` in `nuxt.config.ts` |
| Service worker / offline fallback | ✗ Missing | No service worker; page fails offline |
| Data saver / low-quality mode | ✗ Missing | No per-user stream quality limit |
| Touch gestures (swipe to seek) | ✗ Missing | No touch-specific gesture handlers in player |
| Mobile items per page | ✓ Config | `UIConfig.MobileItemsPerPage` set in config; need to verify if this is read by the frontend |

---

### 2G — Accessibility Journey

| Feature | Status |
|---------|--------|
| Keyboard navigation of UI elements | ✓ Partial — UButton/UIcon are focusable but no skip-to-content link |
| Keyboard shortcut reference page | ✗ Missing |
| ARIA labels on player controls | ✓ — all buttons have `aria-label` in player template |
| High-contrast theme | ✗ — 8 themes available but none are WCAG high-contrast |
| In-app font size control | ✗ Missing |
| Always-on captions preference | ✗ Missing — `subtitle_lang` in DB, no UI |
| Screen reader optimisations | Unknown |

---

## Lens 3 — Industry Feature Comparison

### vs. Plex / Jellyfin / Emby

| Feature | Status | Evidence | Priority |
|---------|--------|----------|----------|
| Resume watching across sessions | ✓ Yes | `/api/playback` save/restore | — |
| Continue watching row on home | ✓ Yes | `show_continue_watching` pref + `/api/suggestions/continue` | — |
| On Deck / next unwatched episode | ✗ No | No series-aware "next episode" logic | Medium |
| Smart playlists | ✗ No | No rule engine | Low |
| Watch history with filtering | ✗ Partial | List + clear only; no filter by date/type | Medium |
| Personal ratings visible in browse | ✗ No | Ratings stored, not shown on media cards | Medium |
| Sort/filter by personal rating | ✗ No | No rating sort field | Medium |
| Subtitle track selection | ✗ No | `subtitle_lang` DB-only | High |
| Multiple audio track selection | ✗ No | Not implemented | Medium |
| Metadata scraping (TMDB/IMDB) | ✗ No | Filename regex only | Medium |
| NFO/XML sidecar file support | ✗ No | Not implemented | Low |
| TV show / season / episode view | ✗ Partial | Detected by categorizer, not browsable | High |
| Music album / artist view | ✗ Partial | Detected by categorizer, not browsable | High |
| Collections (manual groupings) | ✓ Partial | Playlists serve this purpose | — |
| User-specific libraries | ✗ No | All users see the same library | Low |
| Per-user parental controls / PIN | ✗ Partial | `can_view_mature` per user type; no PIN | Medium |
| Quota per user | ✓ Yes | `UserType.StorageQuota` | — |
| Activity feed (who watched what) | ✗ No | Not planned for private server | Optional |
| Transcoding profiles (user-selectable) | ✗ No | Config-only quality profiles | Low |
| Remote access / DDNS | ✓ Yes | Deployed at xmodsxtreme.com | — |
| DLNA / UPnP | ✗ No | Not implemented | Low |
| Chromecast / AirPlay | ✗ No | Not implemented | Medium |
| Mobile apps (iOS/Android) | ✗ No | Not planned | Low |
| Offline / download for mobile | ✗ Partial | Download endpoint exists; no mobile offline UX | Low |
| Sync play / watch party | ✗ No | Not implemented | Low |
| Trailers / extras | ✗ No | Not implemented | Low |
| Chapter support (with UI) | ✗ No | Not implemented | Medium |
| Intro/credit detection + skip | ✗ No | Not implemented | Low |
| Live TV / DVR integration | ✗ No | Out of scope | No |
| Plugin/extension system | ✗ No | Out of scope | No |

### vs. YouTube / Streaming Platforms

| Feature | Status | Evidence | Priority |
|---------|--------|----------|----------|
| Playlist auto-play (next item) | ✗ Partial | Player has playlist context but no auto-advance | Medium |
| Video end-screen countdown | ✗ No | Not implemented | Low |
| Timestamp URL sharing (`?t=123`) | ✗ No | Not implemented | Medium |
| Chapters in progress bar | ✗ No | No chapter data source | Low |
| Speed control (0.5×–2×) | ✓ Yes | `cycleSpeed` in player | — |
| Loop mode | ✗ No | Not implemented | Low |
| Picture-in-picture | ✗ No | Browser API not wired | Medium |
| Keyboard shortcut overlay | ✗ No | Not implemented | Low |
| Clip / highlight | ✗ No | Not implemented | Low |
| Public view count visible | ✓ Yes | `MediaItem.views` shown | — |
| Comments / reactions | ✗ No | Out of scope for private server | Optional |

### vs. Self-hosted Power User Features

| Feature | Status | Evidence | Priority |
|---------|--------|----------|----------|
| User-facing API token | ✗ No | Session-cookie auth only | High |
| RSS feed per category/tag | ✗ No | No `/feed` or `/rss` routes | Medium |
| Webhook subscriptions | ✗ No | No webhook config | Medium |
| Import from URL (yt-dlp) | ✓ Yes | Downloader module + admin UI | — |
| M3U / IPTV playlist import | ✗ No | Export-only; no import | Low |
| Email notifications | ✗ No | Not implemented | Low |
| Two-factor authentication | ✗ No | Not implemented | Medium |
| SSO / OAuth2 login | ✗ No | Not implemented | Low |
| Audit log (own activity) | ✗ No | Admin-only | Low |
| Personal statistics dashboard | ✗ No | Data exists, no endpoint/page | High |
| Watch time goal / streak | ✗ No | Not implemented | Low |
| OpenAPI spec at `/api/docs` | ✗ No | Spec file at `api_spec/openapi.yaml` but not served via HTTP | Medium |

---

## Lens 4 — Data Utilization Gap

Fields that exist in the database but are not exposed to users anywhere in the frontend.

### 4.1 `categorized_items` — Rich metadata never surfaced

| Column | Purpose | Used in UI? |
|--------|---------|-------------|
| `detected_title` | Cleaned file title | No |
| `detected_year` | Release year from filename | No |
| `detected_season` | TV season number | No |
| `detected_episode` | TV episode number | No |
| `detected_show` | TV series name | No |
| `detected_artist` | Music artist name | No |
| `detected_album` | Music album name | No |
| `confidence` | Classification confidence % | No (not even in admin media view) |
| `manual_override` | Admin overrode detection | No |

**Impact:** Building a TV show browser (group by `detected_show` → seasons → episodes) or a music library (group by `detected_artist` → albums) requires only a frontend change — the data is already being computed and persisted on every media scan.

### 4.2 `suggestion_profiles` — User taste profile never shown

| Column | Contains | Used in UI? |
|--------|---------|-------------|
| `category_scores` (JSON) | Per-category affinity scores | No |
| `type_preferences` (JSON) | Video vs audio preference | No |
| `total_views` | User's total view count | No |
| `total_watch_time` | User's total watch time (seconds) | No |
| `last_updated` | When profile was last recalculated | No |

### 4.3 `suggestion_view_history` — Per-item history not surfaced

| Column | Contains | Used in UI? |
|--------|---------|-------------|
| `completed_at` | When user finished the item | No |
| `rating` | User's rating at time of view | No |
| `total_time` | Total seconds watched | No |

### 4.4 `user_preferences` — Stored but not typed/exposed

| Column | In TypeScript? | In Settings UI? |
|--------|---------------|-----------------|
| `subtitle_lang` | No | No |
| `filter_category` | No (read untyped in index.vue) | No |
| `filter_media_type` | No (read untyped in index.vue) | No |

### 4.5 `analytics_events` — Admin-only aggregates

The analytics module tracks `view` and `playback` events with per-user IDs and per-media durations. Users cannot access their own event history. The `/api/analytics` summary endpoint is marked as requiring auth (not admin), but in practice only the admin analytics tab calls it.

**Quick win:** A "My Activity" section on the profile page could call `/api/analytics/events/by-user?user_id=me` to show the user's recent viewing history in a richer format than the basic watch-history list.

---

## Lens 5 — Configuration-Gated Feature Gaps

Features controlled by config flags that have no corresponding admin UI toggle, meaning they can only be changed by editing `config.json` or setting environment variables.

| Config Field | Controls | Admin UI toggle? | Regular users notified? |
|-------------|---------|-----------------|------------------------|
| `Auth.AllowRegistration` | Whether new accounts can be created | No | No — Register button shows regardless |
| `Auth.AllowGuests` | Whether unauthenticated users can browse | No | No explanation shown to guests |
| `Analytics.Enabled` | Whether any analytics are collected | No | No consent banner or disclosure |
| `Download.Enabled` | Whether the download button appears | No | Button may appear even if download is disabled |
| `Features.EnableAutoDiscovery` | Whether auto-discovery scans run | No | Feature silently absent |
| `HuggingFace.Enabled` | Whether AI visual classification runs | No | AI-generated tags look the same as manual tags |
| `MatureScanner.AutoFlag` | Whether to auto-flag mature content | No | No policy disclosure |
| `Streaming.RequireAuth` | Whether unauthenticated streaming is blocked | No | — |
| `Uploads.RequireAuth` | Whether uploads require login | No | — |

**Key gap — `AllowRegistration` UX:** When `AllowRegistration: false`, the register endpoint returns an error but the frontend shows the registration form. Users who try to register get a cryptic API error instead of a clear "Registration is currently closed" message.

**Key gap — AI tag disclosure:** When HuggingFace classification is enabled, AI-generated tags appear identically to admin-set tags. There is no confidence score display, no "AI-generated" label, and no way for users to dispute or remove these tags.

---

## Lens 6 — Persona Unmet Needs

### 6A — The Curator (admin managing a shared library)

| Need | Available? | Notes |
|------|-----------|-------|
| Pin/feature content on home page | ✗ | No featured/hero item concept |
| Editorial collections ("Staff Picks") | ✗ Partial | Playlists exist but no "featured playlist" on home |
| Descriptions for categories | ✗ | Category names are auto-assigned strings only |
| Batch metadata edit (title/tags) | ✓ Admin | `PUT /api/admin/media/bulk` supports category + mature flag; title/tags not in bulk |
| Reorder home page sections | ✗ | Section order is hardcoded in index.vue |
| Set custom thumbnail | ✓ Admin | `/api/admin/thumbnails/generate` |
| Write descriptions for individual items | ✓ Admin | Media edit modal (admin) |

### 6B — The Casual Viewer

| Need | Available? | Notes |
|------|-----------|-------|
| Find something to watch in 3 clicks | ✓ Partial | Search/filter on home works, but no "random" button |
| "Surprise Me" / Random button | ✗ | No shuffle/random endpoint |
| "What's new since last visit?" | ✗ | No cross-reference of last_login + date_added |
| One-click Watch Later from browse | ✗ | Must navigate to player, then add to playlist |
| Progress bar on cards | ✗ | Position data exists but not on list cards |
| Genre/mood browsing | ✗ Partial | Categories via `/api/media/categories`; no mood-based grouping |
| Enough metadata on cards to decide | ✗ Partial | Duration shown; genre/rating/year not shown on cards |

### 6C — The Archivist (power user caring about metadata)

| Need | Available? | Notes |
|------|-----------|-------|
| Edit metadata from user-facing UI | ✗ | Admin-only media edit |
| Bulk-import metadata (CSV/NFO) | ✗ | Not implemented |
| See + correct duplicate detection | ✗ | Admin panel only |
| Set custom thumbnail | ✗ | Admin-only |
| Mark item as private | ✗ | No per-item visibility control |
| See AI-generated tag confidence | ✗ | Confidence score in DB, not exposed |

### 6D — The Family Admin

| Need | Available? | Notes |
|------|-----------|-------|
| Per-user content restrictions | ✓ | `can_view_mature` per `UserType` |
| PIN-protected profile | ✗ | Not implemented |
| See what family members watched | ✗ | Per-user watch history admin view not available |
| Kids Mode (limited UI) | ✗ | Not implemented |
| Prevent user from uploading | ✓ | `AllowUploads: false` per `UserType` |
| Parental control dashboard | ✗ | Not implemented |

### 6E — The Developer

| Need | Available? | Notes |
|------|-----------|-------|
| User-facing API token | ✗ | No token generation endpoint; session-cookie only |
| Webhook subscriptions | ✗ | Not in config or routes |
| RSS/Atom feed | ✗ | No feed routes |
| OpenAPI spec served at a URL | ✗ | `api_spec/openapi.yaml` exists but has no HTTP route |
| Public embeddable player | ✗ | `X-Frame-Options` headers restrict embedding |
| Prometheus metrics endpoint | ✓ Admin | `GET /metrics` (admin-auth protected) |

---

## Prioritised Recommendations

### Tier 1 — High impact, backend already ready (LOW effort)

1. ~~**Add `subtitle_lang`, `filter_category`, `filter_media_type` to TypeScript `UserPreferences` type** and wire up a "Save current filters" button on the browse page.~~ ✅ **DONE** — types added (cycle 2026-03-28 morning); auto-save on filter change (cycle 2026-03-28 evening)

2. ~~**Display suggestion `reasons` in the player sidebar**~~ ✅ **DONE** — 2026-03-28

3. **Show watch progress bar on media cards** — add `position` and `duration` to the media list response (or batch-fetch positions) and render a thin progress bar at the card bottom. (`LOW-MEDIUM`)

4. ~~**Picture-in-picture button**~~ ✅ **DONE** — 2026-03-28

5. ~~**Add a "Surprise Me" button**~~ ✅ **DONE** — 2026-03-28

6. ~~**Show "Registration is currently closed" message** when `AllowRegistration: false`~~ ✅ **DONE** — 2026-03-28

### Tier 2 — Medium effort, high user value

7. **TV Show / Music browse UI** — Add a "TV Shows" page that calls `GET /api/categorized?category=TV+Shows` and groups results by `detected_show → detected_season`. Add a "Music" page grouping by `detected_artist → detected_album`. (`MEDIUM`)

8. **Personal stats page** — Add a `GET /api/suggestions/profile` (or `/api/me/stats`) endpoint that returns the user's `UserProfile` (watch time, category scores, total views). Render a simple stats card on the Profile page. (`MEDIUM`)

9. **"Watch Later" / Favorites** — Add a `favorites` table to DB and two new endpoints (`POST/DELETE /api/favorites`). Add a star/bookmark icon to every browse card. (`MEDIUM`)

10. **Timestamp URL deep-links** — Read `?t=N` query parameter on player page load and seek to that position after the video is ready. Generate a "Share at current time" copy button in the player. (`MEDIUM`)

11. **User API tokens** — Add a `user_api_tokens` table and `GET/POST/DELETE /api/auth/tokens` endpoints. Display them in the Profile page so developers can script against the API. (`MEDIUM`)

12. ~~**RSS feed per category**~~ ✅ **DONE** — `GET /api/feed` with `?category`, `?type`, `?limit` params returns Atom 1.0 XML (2026-03-28)

13. ~~**Serve the OpenAPI spec at `/api/docs`**~~ ✅ **DONE** — embedded and served at `GET /api/docs` (auth-gated, 2026-03-28)

14. **Fix `AllowRegistration: false` UX** — Read server settings on the login page and conditionally hide the Register link. (`LOW`)

### Tier 3 — Architecture required (HIGH effort, plan separately)

15. **Subtitle track selection** — Requires ffprobe track enumeration (new endpoint), HLS multi-audio/subtitle track pipeline, and player track selector. (`HIGH`)

16. **Smart playlists** (rule-based, auto-updating) — New DB tables, rule engine, and UI query builder. (`HIGH`)

17. **PWA / offline** — Add `manifest.json`, service worker for offline fallback, and push notification framework. (`HIGH`)

18. **TMDB / IMDB metadata scraping** — Replace regex-only categorizer with an optional metadata API integration. (`HIGH`)

---

## Appendix — Routes with No Frontend Caller (non-admin)

The following non-admin routes exist in `routes.go` but are not called by any composable method in `web/nuxt-ui/composables/useApiEndpoints.ts`:

| Route | Handler | Note |
|-------|---------|------|
| `GET /api/categorized` | `GetCategorizedItems` | Biggest gap — rich TV/music data |
| `GET /metrics` | `GetMetrics` | Prometheus scraping only — intentionally no UI |
| `GET /api/status` | `GetServerStatus` | Called in `useAdminApi()` only |
| `GET /api/modules` | `ListModuleStatuses` | Called in `useAdminApi()` only |
| `GET /api/modules/:name/health` | `GetModuleHealth` | Called in `useAdminApi()` only |
| `GET /health` | `GetHealth` | For uptime monitors — no UI needed |
| Receiver WebSocket `/ws/receiver` | `ReceiverWebSocket` | Slave-node protocol — not user-facing |
| Downloader WebSocket `/ws/admin/downloader` | `AdminDownloaderWebSocket` | Admin-only |
| Extractor HLS routes | `ExtractorHLSMaster` etc. | Served directly — no JS caller needed |

---

## Update Notes

### 2026-03-28 (Automated Cycle 2)

**Newly completed (Tier 1):**
- `GET /api/docs` — OpenAPI spec now embedded in binary and served at this route (auth-gated)
- `GET /api/feed` — Atom 1.0 feed with `?category`, `?type`, `?limit` params
- Filter preferences auto-save — `filter_category` + `filter_media_type` written to backend on change

**Still outstanding (highest priority):**
- Progress bars on media cards (Tier 1 #3) — position data exists, not yet returned on list cards
- Personal stats endpoint (Tier 2 #8) — `GET /api/suggestions/profile` not yet built
- Watch Later / Favorites (Tier 2 #9) — no `favorites` table yet
- Timestamp deep-links (Tier 2 #10) — `?t=N` seek not yet handled
- User API tokens (Tier 2 #11) — no token table yet

### 2026-03-28 (Automated Cycle 3)

**Newly completed:**
- ~~**Progress bars on media cards** (Tier 1 #3)~~ ✅ **DONE** — Backend: `GET /api/playback/batch?ids=...` returns batch positions; `BatchGetPlaybackPositions` added to repository + media module. Frontend: index.vue batch-fetches positions after media load and overlays thin progress bar on grid cards (logged-in users only).
- ~~**Personal stats endpoint** (Tier 2 #8)~~ ✅ **DONE** — Backend: `GET /api/suggestions/profile` returns `UserProfile` (total_views, total_watch_time, category_scores, type_preferences). Frontend: profile.vue loads and renders Watch Stats card with top-3 category affinity bars.
- ~~**Timestamp deep-links** (Tier 2 #10)~~ ✅ **DONE** — Frontend: player.vue reads `?t=N` query param on video load and seeks to that second (takes priority over resume position). Adds "Copy link at current time" button in player controls.

**Still outstanding (highest priority):**
- Watch Later / Favorites (Tier 2 #9) — no `favorites` table yet
- User API tokens (Tier 2 #11) — no token table yet

### 2026-03-28 (Automated Cycle 4)

**Newly completed:**
- ~~**Watch Later / Favorites** (Tier 2 #9)~~ ✅ **DONE** — Backend: `user_favorites` table, `POST/DELETE/GET /api/favorites`, Bearer-token sessionAuth enhancement. Frontend: heart toggle on browse cards (optimistic), /favorites page, Favorites nav link.
- ~~**User API tokens** (Tier 2 #11)~~ ✅ **DONE** — Backend: `user_api_tokens` table, `GET/POST/DELETE /api/auth/tokens`, Bearer auth in sessionAuth (hash-based, one-time reveal). Frontend: profile page token management section with create/revoke and one-time reveal banner.

**Still outstanding (Tier 2/3):**
- TV Show / Music browse UI (Tier 2 #7) — `GET /api/categorized` not yet wired to a user-facing page
- Fix `AllowRegistration: false` UX (Tier 2 #14) — Register link still shown on login page when registration closed

### 2026-03-28 (Automated Cycle 5)

**Newly completed:**
- ~~**TV Show / Music browse UI** (Tier 2 #7)~~ ✅ **DONE** — Backend: `GET /api/browse/categories` user-facing endpoint (stats or items-by-category with thumbnails). Frontend: `/categories` page with category tiles, grouped TV/Music (show→episodes, artist→tracks), flat grid for Movies/Docs; "Categories" nav link added.
- ~~**User ratings list** (Gap 1.4)~~ ✅ **DONE** — Backend: `GET /api/ratings` returns rated items from user's suggestion ViewHistory. Frontend: "My Ratings" horizontal scroll card in profile page with star badges.
- ~~**Fix `AllowRegistration: false` UX** (Tier 2 #14)~~ ✅ **Already done** — login.vue was already hiding the Register link and showing "Registration is currently closed" message (confirmed in code review).
- **Recently Added row** — Backend: `GET /api/suggestions/recent?days=14&limit=20` returns media sorted by date_added desc within a configurable window. Frontend: "Recently Added" horizontal scroll row on home page (logged-in users only).

**Still outstanding (Tier 3 — architecture required):**
- Subtitle track selection — HIGH effort, requires ffprobe track enumeration endpoint + HLS multi-track pipeline
- Smart playlists (rule-based) — HIGH effort
- PWA / offline — HIGH effort
- TMDB/IMDB metadata scraping — HIGH effort

### 2026-03-28 (Automated Cycle 6)

**Newly completed:**
- ~~**Export watch history as CSV** (Lens 2E)~~ ✅ **DONE** — Backend: `GET /api/watch-history/export` returns CSV (media_name, media_id, viewed_at, position, duration). Frontend: "Export CSV" button in profile watch history tab.
- ~~**Loop mode in player** (Lens 3 vs YouTube)~~ ✅ **DONE** — Frontend: loop toggle button in player controls; cycles through off → one → all. When "one" is active, video restarts on end; when "all" is active in playlist context, loops playlist.
- ~~**Playlist auto-play (next item)** (Lens 3 vs Plex/YouTube)~~ ✅ **DONE** — Frontend: playlists.vue passes `?playlist_id=<id>&playlist_idx=<n>` to player URL; player fetches playlist on load, shows "Up Next" countdown overlay (5s, cancellable) on video end, auto-advances to next item.

**Still outstanding (Tier 3 — architecture required):**
- Subtitle track selection — HIGH effort
- Smart playlists (rule-based) — HIGH effort
- PWA / offline — HIGH effort
- TMDB/IMDB metadata scraping — HIGH effort

### 2026-03-28 (Automated Cycle 8)

**Newly completed:**
- ~~**Tag chips on browse cards**~~ ✅ **DONE** — Frontend: up to 2 clickable tag badges on each grid card; clicking a tag activates a tag filter for the browse library. Active tag shown as a dismissable chip in the filter row. `MediaListParams.tags` added to types; `useMediaApi.list()` serialises as comma-joined string.
- ~~**"Hide watched" filter**~~ ✅ **DONE** — Backend: `GET /api/media?hide_watched=true` excludes items the authenticated user has completed watching (`CompletedAt` set in suggestion ViewHistory). Frontend: "Hide Watched" toggle button in browse filter row (logged-in users only).
- ~~**Player keyboard shortcuts**~~ ✅ **DONE** — Frontend: Space/K=play-pause, ←/→=±10s seek, F=fullscreen, M=mute-toggle, ?=shortcuts overlay. Keyboard button in player toolbar also opens the overlay. Only active when focus is not in an input field.

**Still outstanding (Tier 3 — architecture required):**
- Subtitle track selection — HIGH effort
- Smart playlists (rule-based) — HIGH effort
- PWA / offline — HIGH effort
- TMDB/IMDB metadata scraping — HIGH effort

### 2026-03-28 (Automated Cycle 7)

**Newly completed:**
- ~~**Personal ratings on browse cards**~~ ✅ **DONE** — Backend: `GET /api/media` returns `user_ratings` map `{media_id: rating}` for authenticated users. Frontend: star badge on top-right of browse cards (skipped when mature badge occupies the same corner).
- ~~**Sort/filter by personal rating**~~ ✅ **DONE** — Backend: `sort=my_rating` (desc by default, asc if `sort_order=asc`) + `min_rating=N` filter. Frontend: "My Rating" option added to sort dropdown (logged-in users only).
- ~~**"New since last visit" section**~~ ✅ **DONE** — Backend: `previous_last_login` tracked in users table (copied from `last_login` on each login); `GET /api/suggestions/new` returns media added since that timestamp (fallback: 7 days). Frontend: "New Since Your Last Visit" horizontal scroll row on home page, shown only when `total > 0`.

**Still outstanding (Tier 3 — architecture required):**
- Subtitle track selection — HIGH effort
- Smart playlists (rule-based) — HIGH effort
- PWA / offline — HIGH effort
- TMDB/IMDB metadata scraping — HIGH effort

### 2026-03-28 (Automated Cycle 9)

**Newly completed:**
- ~~**Persist playback speed**~~ ✅ **DONE** — Frontend: `cycleSpeed()` now calls `updatePreferences({ playback_speed })` after each toggle, persisting the chosen speed exactly like volume.
- ~~**"On Deck" next episode**~~ ✅ **DONE** — Backend: `GET /api/suggestions/on-deck` cross-references categorizer TV/Anime items with user ViewHistory to find the next unwatched episode per show (sorted by most-recently-watched show). Frontend: "On Deck" horizontal scroll row on home page with S##E## badge overlay and show/episode name labels.
- ~~**Watch history completion filter**~~ ✅ **DONE** — Backend: `?completed=true|false` param on `GET /api/watch-history`. Frontend: "All / In Progress / Completed" filter buttons in profile page watch history tab.

**Still outstanding (Tier 3 — architecture required):**
- Subtitle track selection — HIGH effort
- Smart playlists (rule-based) — HIGH effort
- PWA / offline — HIGH effort
- TMDB/IMDB metadata scraping — HIGH effort
