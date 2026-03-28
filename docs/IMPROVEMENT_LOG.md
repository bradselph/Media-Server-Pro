# Improvement Log

Automated continuous improvement cycle history.

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
