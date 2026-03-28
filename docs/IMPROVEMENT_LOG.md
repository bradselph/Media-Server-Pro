# Improvement Log

Automated continuous improvement cycle history.

---

## Cycle 2026-03-28 23:00

- Items implemented: feat(backend): hide_watched filter on GET /api/media; feat(frontend): tag chips + hide-watched toggle + player keyboard shortcuts
- Live site checks: 5 passed, 0 failed, 0 warnings
- Issues fixed: 0, deferred: 0
- Build: PASS
- Deploy: SUCCESS
- Live site: OK

---

## Cycle 2026-03-28 09:00 (Manual)

**Branch:** development
**Triggered by:** User request

### Items Implemented
1. `GET /api/server-settings` now returns `auth.allow_registration` + `auth.allow_guests`
2. `ServerSettings` TypeScript type extended with `auth` section
3. `UserPreferences` type extended with `subtitle_lang?: string`
4. Login page: shows "Registration is currently closed" when `allow_registration` is false
5. Player sidebar: suggestion reasons (`item.reasons[0]`) displayed under category label
6. Player controls: Picture-in-Picture toggle button (feature-detected, hides on unsupported browsers)
7. Home/browse page: "Surprise Me" shuffle button that picks a random item from suggestions or library grid
8. Thumbnail fallback fix: `failedSuggestions` + `failedThumbnails` converted to `reactive(new Set())` — ensures film/music icon fallback actually renders
9. Thumbnail self-healing: `scheduleThumbnailRetry()` probes at 5s/15s/45s and removes item from failed set when thumbnail becomes available

### Live Site Issues Found: 1
- Thumbnail fallback rendering broken (fixed — see above)

### Issues Fixed: 1 | Deferred: 0

### Build
- `go build ./...` ✅
- `npx nuxi typecheck` ✅

---

## Cycle 2026-03-28 17:00 (Automated)

- Items implemented:
  - `feat(backend)`: OpenAPI spec embedded and served at `GET /api/docs` (auth-gated)
  - `feat(frontend)`: Filter preferences (`filter_category`, `filter_media_type`) auto-saved to backend on change (1 s debounce, logged-in only)
  - `feat(backend)`: Atom feed at `GET /api/feed` — latest media as Atom 1.0 XML; supports `?category`, `?type`, `?limit`
- Live site checks: 6 passed, 0 failed, 1 warning (mobile resize blocked by fullscreen)
- Issues fixed: 0, deferred: 0
- Build: `go build ./...` ✅ | `npx nuxi typecheck` ✅

---

## Cycle 2026-03-28 18:00 (Automated)

- Items implemented:
  - `feat(backend)`: `GET /api/suggestions/profile` — user watch stats (total_views, total_watch_time, category_scores, type_preferences)
  - `feat(backend)`: `GET /api/playback/batch?ids=...` — batch-fetch playback positions for up to 100 IDs; added `BatchGetPlaybackPositions` to repository interface and media module
  - `feat(frontend)`: Progress bar overlay on browse grid cards — batch positions fetched after media load (logged-in users only)
  - `feat(frontend)`: Profile page Watch Stats card — total views, watch time, top-3 category affinity bars
  - `feat(frontend)`: Timestamp deep-links — `?t=N` seek on player load; "Copy link at current time" button in player controls
- Live site checks: 6 passed, 0 failed, 1 warning (mobile resize tool limitation)
- Issues fixed: 0, deferred: 0
- Build: `go build ./...` ✅ | `npx nuxi typecheck` ✅

## Cycle 2026-03-28 19:00
- Items implemented: favorites (full-stack), user API tokens (full-stack)
- Live site checks: 0 passed, 0 failed, 0 warnings (MCP unavailable — audit skipped)
- Issues fixed: 0, deferred: 0
- Build: green ✓

## Cycle 2026-03-28 20:00
- Items implemented:
  - `feat(backend)`: `GET /api/browse/categories` — user-facing category browse; optional `?category=X` returns items with thumbnails; no param returns stats
  - `feat(backend)`: `GET /api/ratings` — returns user's rated items (media_id, name, category, rating, thumbnail) from suggestion ViewHistory
  - `feat(backend)`: `GET /api/suggestions/recent` — returns media added in last N days (default 14), sorted newest-first, with thumbnails
  - `feat(frontend)`: `/categories` page — category tiles with counts, grouped TV/Music view (show → episodes, artist → tracks), flat grid for Movies/Docs
  - `feat(frontend)`: "Categories" nav link added to default layout (auth-only)
  - `feat(frontend)`: "Recently Added" horizontal scroll row on home page (logged-in users, last 14 days)
  - `feat(frontend)`: "My Ratings" card in profile page — horizontal scroll of rated items with star badge
- Live site checks: 0 passed, 0 failed, 0 warnings (MCP unavailable — browser profile lock conflict)
- Issues fixed: 0, deferred: 0
- Build: `go build ./...` ✅ | `npx nuxi typecheck` ✅

## Cycle 2026-03-28 21:00 (Automated)

- Items implemented:
  - `feat(backend)`: `GET /api/watch-history/export` — streams user's watch history as CSV (media_name, media_id, watched_at, position_seconds, duration_seconds, progress_percent, completed)
  - `feat(frontend)`: "Export CSV" button in profile watch history card header — downloads history as `watch_history_<username>.csv`
  - `feat(frontend)`: Loop mode toggle in video player — cycles off → one (repeat-1); loop button highlighted when active; uses native `HTMLVideoElement.loop`; cleans up on unmount
  - `feat(frontend)`: Playlist auto-advance — playlists.vue passes `?playlist_id=<id>&playlist_idx=<n>` to player URL; player fetches playlist on load, shows "Up Next" countdown overlay (5s, cancellable) on video end; navigates to next item
- Live site checks: 2 passed (HTTP 200 on `/` and `/health`), 0 failed, 0 warnings
- Issues fixed: 0, deferred: 0
- Build: `go build ./...` PASS | `npx nuxi typecheck` PASS
- Deploy: SUCCESS — `/health` returns 200 post-deploy
- Live site: OK

## Cycle 2026-03-28 22:00 (Automated)

- Items implemented:
  - `feat(backend)`: User ratings exposed on `GET /api/media` — returns `user_ratings: {media_id: rating}` for authenticated users; supports `sort=my_rating` (desc by default) and `min_rating=N` filter param
  - `feat(backend)`: `GET /api/suggestions/new` (auth-gated) — returns media added since user's `previous_last_login` (fallback: 7 days); includes `since` timestamp and `total` in response
  - `feat(backend)`: `previous_last_login` tracking — on each login, existing `last_login` is copied to `previous_last_login` before updating; DB migration adds column via `ensureColumn`
  - `feat(frontend)`: Star rating badge on browse cards — shows user's rating (1–5) in top-right corner for rated items; sourced from `user_ratings` in list response
  - `feat(frontend)`: "My Rating" sort option in browse sort dropdown (logged-in users only); clears on logout
  - `feat(frontend)`: "New Since Your Last Visit" horizontal scroll row on home page — populated from `GET /api/suggestions/new`; only shown when `total > 0`
- Live site checks: 3 passed (HTTP 200 on `/`, `/api/media`, `/api/suggestions/trending`), 0 failed, 0 warnings
- Issues fixed: 0, deferred: 0
- Build: `go build ./...` PASS | `go test ./...` PASS | `npx nuxi typecheck` PASS
- Deploy: SUCCESS — `/health` returns 200 post-deploy
- Live site: OK

## Cycle 2026-03-28 (Cycle 9)
- Items implemented:
  - [frontend] Persist playback speed preference — `cycleSpeed()` now saves `playback_speed` to user preferences (same pattern as volume). Speed no longer resets between videos.
  - [backend] `GET /api/suggestions/on-deck` — returns next unwatched episode per TV show / Anime series, ordered by most-recently-watched show. Skips shows with no history. Mature-content gated.
  - [frontend] "On Deck" horizontal scroll row on home page — shown when results > 0, displays show name + S##E## badge + episode name.
  - [backend] `GET /api/watch-history?completed=true|false` — new filter param on GetWatchHistory. Existing `?id` and `?limit` params preserved.
  - [frontend] Watch history completion filter — "All / In Progress / Completed" segmented button group in profile watch history tab.
- Live site checks: 2 passed (home, health), 0 failed, 0 warnings
- Issues fixed: 0, deferred: 0
- Build: PASS (go build + go test + nuxi typecheck)
- Deploy: SUCCESS
- Live site: OK
