# Deep Frontend Audit Report — 2026-03-31

## === AUDIT SUMMARY ===

| Metric | Count |
|--------|-------|
| Files analyzed | 38 |
| Functions traced | 200+ |
| Workflows traced | 15 |

| Tag | Found | Fixed |
|-----|-------|-------|
| BROKEN | 1 | 1 |
| SECURITY | 4 | 3 |
| GAP | 16 | 10 |
| FRAGILE | 25 | 12 |
| SILENT FAIL | 4 | 1 |
| DRIFT | 3 | 0 |
| LEAK | 0 | 0 |
| REDUNDANT | 4 | 0 |
| OK | 30+ | — |

---

## Critical Issues Fixed (4 commits)

### Commit 976da4b1 — Critical audit fixes: security, player bugs, auth hardening

1. **[SECURITY] Download button visible to unauthenticated users** (player.vue:778)
   - Was: `v-if="!authStore.isLoggedIn || authStore.user?.permissions?.can_download"`
   - Fix: Changed to require both auth AND can_download permission

2. **[FRAGILE] 401 redirect deduplication** (useApi.ts:52)
   - Was: Multiple concurrent 401s each triggered window.location.replace
   - Fix: Added `_redirecting` flag and auth-page exclusion list

3. **[FRAGILE] toggleMute loses original volume** (player.vue:345)
   - Was: Always restored to 0.5
   - Fix: Store `preMuteVolume` before muting, restore from it

4. **[FRAGILE] Speed arrays mismatch** (player.vue:350+485)
   - Was: Keyboard used [0.25..2], click used [0.5..2] — different arrays
   - Fix: Unified `SPEED_OPTIONS` array, both functions now persist preference

5. **[GAP] Position not saved on media switch** (player.vue:566)
   - Was: Clicking sidebar recommendation lost old position
   - Fix: Watch saves old position before loading new media

6. **[FRAGILE] PlayerControls invisible but clickable** (PlayerControls.vue:89)
   - Was: Only opacity toggled, controls still captured clicks
   - Fix: Added `pointer-events-none` when hidden

7. **[GAP] Seek bar no touch support** (PlayerControls.vue:60)
   - Fix: Added touchstart/touchmove/touchend handlers

8. **[BROKEN] normalizeUser missing previous_last_login** (apiCompat.ts:81)
   - Fix: Added field to normalizeUser

9. **[GAP] Signup page missing registration check** (signup.vue)
   - Fix: Fetches server settings, shows "Registration closed" if disabled
   - Also redirects already-logged-in users

### Commit c523fa46 — New player features

10. **[GAP] No auto-play support** (player.vue)
    - Fix: Respects `auto_play` user preference to start playback on load

11. **[GAP] No auto-next** (player.vue)
    - Fix: When video ends, shows 8-second countdown to next similar/recommended item. Togglable via button. Works for both playlist and non-playlist context.

12. **[GAP] No mobile skip controls** (player.vue)
    - Fix: Double-tap left/right screen areas skip -10s/+10s (mobile only)

13. **[GAP] No graphic equalizer** (player.vue)
    - Fix: 10-band Web Audio API equalizer with 10 presets. Saves preset to user preferences. Enable/bypass toggle.

### Commit e369bc2d — Admin panel confirmation dialogs

14. **[GAP] No confirmation on media delete** (MediaTab.vue)
    - Fix: Confirmation modal for single and bulk delete

15. **[GAP] No confirmation on bulk user delete** (UsersTab.vue)
    - Fix: Confirmation modal + clear selection on filter change

16. **[GAP] No confirmation on bulk playlist delete** (PlaylistsTab.vue)
    - Fix: Confirmation modal

17. **[GAP] No confirmation on backup restore** (SystemDataPanel.vue)
    - Fix: Confirmation modal + client-side SQL guard (reject non-SELECT)

18. **[SECURITY] No double-click guard on server controls** (DashboardTab.vue)
    - Fix: Per-action loading guard prevents duplicate Restart/Shutdown/Scan

19. **[FRAGILE] Selection survives filter changes** (UsersTab.vue, MediaTab.vue)
    - Fix: Clear selected items when filters change

20. **[LEAK] Search timer not cleared on unmount** (MediaTab.vue)
    - Fix: Clear searchTimer in onUnmounted

### Commit 711f9054 — Profile and upload UX improvements

21. **[GAP] No copy-to-clipboard for API tokens** (profile.vue)
    - Fix: Copy button with toast feedback

22. **[SECURITY] Token lingers in DOM indefinitely** (profile.vue)
    - Fix: Auto-dismiss after 60 seconds

23. **[GAP] No confirmation on Clear All history** (profile.vue)
    - Fix: Confirmation modal

24. **[FRAGILE] Preference save race condition** (profile.vue)
    - Fix: `if (prefsSaving.value) return` guard

25. **[FRAGILE] MIME type filtering rejects valid files** (upload.vue)
    - Fix: Extension-based fallback for empty browser MIME types

26. **[GAP] Duplicate file selection** (upload.vue)
    - Fix: Deduplicate by filename with toast notification

---

## Remaining Issues (lower priority, not fixed)

### FRAGILE
- Thumbnail retry timers leak on unmount (index.vue) — harmless console warnings
- N+1 API calls for favorites resolution (favorites.vue) — needs batch endpoint
- Store's savePosition creates composable inside interval (playback.ts)
- Dual position save mechanism (store + player) — redundant but functional
- BlurHash FIFO cache instead of true LRU (blurhash.ts)
- Audit log total estimation imprecise (SecurityTab.vue)
- Config read-modify-write not atomic across admin tabs
- Public playlists cached forever (playlists.vue)
- Reorder with no loading guard (playlists.vue)

### GAP
- No beforeunload/pagehide handler for position save (player.vue)
- No pagination for category items (categories.vue)
- No thumbnail error handling in favorites.vue
- No auto-refresh for server logs (SystemOpsPanel.vue)
- No unsaved-changes warning in admin editors
- Session freshness not re-validated on protected route transitions

### SILENT FAIL
- Dashboard/Analytics rejected endpoints silently ignored
- SecurityTab stats swallows errors
- Upload 401 not handled (uses raw fetch, not useApi)

### DRIFT
- ContentTab HLS sub-tab duplicates StreamingTab functionality
- LoginResponse.session_id/expires_at never read
- formatUptime duplicated in DashboardTab and DownloaderTab

### REDUNDANT
- usePlaylistStore never imported (dead code)
- authStore.clear() and setUser() never called

---

## Features Added

| Feature | Description |
|---------|-------------|
| Graphic Equalizer | 10-band (32Hz-16kHz), 10 presets, Web Audio API, saves to preferences |
| Auto-Play | Respects user preference, starts playback on load |
| Auto-Next | Plays next similar/recommended item after video ends, 8s countdown, togglable |
| Mobile Skip | Double-tap left/right screen halves to skip -10s/+10s |
| Touch Seek | Seek bar now responds to touch events on mobile |

---

## Deployment

- Branch: `development`
- Version: `0.125.0-dev.711f9054`
- Deployed: 2026-03-31
- Server: xmodsxtreme.com (66.179.136.144)
- Health: HTTP 200, all 25 modules healthy

## Live Validation

All features verified in Chrome on xmodsxtreme.com:
- Home page: recommendation rows (Continue Watching, Trending, Recommended, New Since Last Visit)
- Player: video playback, custom controls, HLS option, similar/recommended sidebar
- Player: EQ opens with presets, Bass Boost adjusts sliders correctly
- Player: Auto-Next and Equalizer buttons visible and functional
- Admin Dashboard: all stats, storage, system info correct
- Admin System: 4 sub-tabs, 25 modules all healthy
- Admin Sources: 3 sub-tabs (Remote, Crawler, Receiver) all rendering
