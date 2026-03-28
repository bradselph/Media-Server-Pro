# Frontend Code Audit Report
> Date: 2026-03-27 | Nuxt: 3.16.1 | Vue: 3.5.13 | Components: 13 admin + 7 pages + 1 layout + error.vue | Composables: 3 | Stores: 5 | Total .vue/.ts files: 42

## Executive Summary

The frontend is in solid overall health. TypeScript coverage is excellent — zero `any` types, zero `@ts-ignore` comments, and zero hardcoded secrets. The TDZ risk that previously caused a production crash has been correctly resolved with explicit static imports in `useApiEndpoints.ts`. The main weaknesses are **component size** (8 components exceed 400 lines, with `SourcesTab.vue` reaching 812 lines), **duplicate utility code** scattered across components (`formatBytes`, `formatTime`, `asRecord`), and a **duplicate `AnalyticsEvent` interface** in `types/api.ts` that will cause a TypeScript error. No memory leaks were found — all timers, intervals, and WebSocket connections are cleaned up in `onUnmounted`.

## Scores

| Category | Score | Issues |
|----------|-------|--------|
| Component Architecture | C | 8 oversized components; 3 god components |
| TypeScript Coverage | A | 1 duplicate interface; 1 broad union type |
| Reactivity Safety | B | No leaks; 2 polling-without-visibility issues |
| Store Design | A- | 1 swallowed error with no state; 1 extra sequential API call |
| Composable Quality | A | No request cancellation; 1 weak return type |
| Performance | B | Inline arrays in templates; no virtual scroll needed |
| Error Handling | B+ | Some silent swallows are intentional; no global error handler |
| Code Organization | C+ | Duplicate utilities; redundant auth watchEffect in 2 pages |
| TDZ Safety | A | All prior TDZ issues resolved; no new risks found |

---

## Issues Found

### CRITICAL

| # | File:Line | Category | Issue | Recommendation |
|---|-----------|----------|-------|----------------|
| 1 | `types/api.ts:210` & `:900` | TypeScript | `AnalyticsEvent` interface is declared **twice** with identical content. TypeScript will treat the second as a declaration merge, but any divergence will silently produce the wrong type. `useAnalyticsApi` uses the first; the second is a copy-paste leftover. | Delete the duplicate declaration at line 900–911. Keep only the one at line 210. |

---

### HIGH

| # | File:Line | Category | Issue | Recommendation |
|---|-----------|----------|-------|----------------|
| 2 | `components/admin/SourcesTab.vue` (812 lines) | Component size | Single component manages four distinct domains: remote sources, crawler targets, crawler discoveries, and extractor items. Mixing four admin sub-features makes it the hardest component to read, test, or extend. | Split into `SourcesRemoteTab.vue`, `SourcesCrawlerTab.vue`, `SourcesExtractorTab.vue` and use a nested tab/accordion layout within `SourcesTab.vue`. |
| 3 | `pages/player.vue` (695 lines) | Component size | Player page handles video element, HLS activation, analytics tracking, playback position, star ratings, playlist add, seek-bar previews, and sidebar recommendations. | Extract a `PlayerControls.vue` component (seeks, volume, speed, quality, fullscreen) and a `PlayerSidebar.vue` (similar/recommended links). HLS logic is already in `useHLS.ts` — good; the remainder can be further split. |
| 4 | `pages/index.vue` (585 lines) | Component size | Media library page mixes media grid rendering, hover-preview cycling, filter/search controls, pagination, and recommendation carousels. The hover-preview system alone is 40+ lines of stateful logic. | Extract `MediaGrid.vue` (grid + list toggle, hover logic) and `RecommendationRow.vue` (reusable row for Continue Watching / Trending / Recommended) to reduce the page to a coordinator. |
| 5 | `components/admin/SystemTab.vue` (573 lines) | Component size | Handles DB query executor, scheduled tasks, audit log, server logs, and config editor — five distinct admin tools in one component. | Split into `SystemTasksTab.vue`, `SystemLogsTab.vue`, `SystemDatabaseTab.vue`, `SystemConfigTab.vue`. |
| 6 | `composables/useApi.ts` | Composable quality | No request cancellation via `AbortController`. When a component unmounts mid-flight (e.g., navigating away during a slow list load), the `fetch` response still resolves and its callback attempts to write to the unmounted component's reactive state. Vue 3 will silently discard those writes if the component is already garbage-collected, but race conditions can produce brief stale-state flashes or console warnings in development. | Pass an optional `signal: AbortSignal` into `request()`. Components can create an `AbortController` in `onUnmounted` and pass `controller.signal` to long-lived requests. For most short API calls this is low risk; the main candidates are long polls and list fetches in navigation-heavy flows. |
| 7 | `pages/upload.vue:146` | Component responsibility | `@click="($el as HTMLElement).querySelector('input')?.click()"` accesses `$el` directly in the template and uses a DOM traversal to fire the hidden `<input>`. This is fragile (DOM structure change breaks it) and hard to test. | Add `const fileInputRef = ref<HTMLInputElement \| null>(null)`, bind it with `ref="fileInputRef"` on the `<input>`, and call `fileInputRef.value?.click()` from a named `openFilePicker()` method. |

---

### MEDIUM

| # | File:Line | Category | Issue | Recommendation |
|---|-----------|----------|-------|----------------|
| 8 | `pages/profile.vue:27` & `pages/playlists.vue:13` | Code organization | Both pages declare `middleware: 'auth'` in `definePageMeta` AND add a redundant `watchEffect(() => { if (!isLoggedIn) router.replace('/login') })`. The middleware fires before the page mounts and already handles the redirect; the watchEffect is a duplicate. | Remove the `watchEffect` blocks. The `auth` middleware + `plugins/auth.ts` fetching the session before navigation is sufficient. |
| 9 | `components/admin/DashboardTab.vue:82` & `composables/useHLS.ts:309` | Performance | `setInterval` polling (dashboard: 30 s, HLS job status: 3 s) runs even when the browser tab is hidden. This wastes bandwidth and server resources. | Wrap the interval callbacks with `if (document.hidden) return` or use the Page Visibility API: `document.addEventListener('visibilitychange', ...)` to pause/resume the interval. |
| 10 | `stores/settings.ts:18` | Store design | Settings fetch errors are silently swallowed with `catch { // Non-critical }` but the store has no `error` ref. Callers have no way to know whether the `settings` being `null` means "still loading" or "failed to load". | Add `const error = ref<string \| null>(null)` to the store. Set it in the catch block and expose it so consuming components can show a degraded-mode indicator. |
| 11 | Multiple files | Performance | Inline array/object literals passed as component props in templates create a new reference on every render, preventing Vue from short-circuiting the child diff. Examples: `USelect :items="[{ label: 'All Types', value: 'all' }, ...]"` in `index.vue:374–386`, `UTable :columns="[...]"` in `DashboardTab.vue:158–167`, and the toggle array in `profile.vue:256–263`. | Move static option arrays to module-level `const` declarations or computed properties. E.g.: `const TYPE_OPTIONS = [{ label: 'All Types', value: 'all' }, ...]` defined once at the top of `<script setup>`. |
| 12 | `components/admin/DashboardTab.vue:18–23` & `pages/upload.vue:75–80` | Code organization | `formatBytes` is defined identically in two components. `formatTime`/`formatDuration` with equivalent logic also appear in `player.vue:258` and `index.vue:166`. | Extract to `utils/format.ts` exporting `formatBytes(bytes)`, `formatTime(seconds)`, `formatDuration(seconds)`. All components import from there. |
| 13 | `utils/apiCompat.ts:5` & `utils/mediaTitle.ts:3` | Code organization | `asRecord(value: unknown)` is defined identically in both files. | Move to a shared `utils/typeGuards.ts` and import from both. |
| 14 | `composables/useApiEndpoints.ts:421` | TypeScript | `cacheRemoteMedia` uses `api.post<unknown>` — the weakest possible return type. Every caller must cast or ignore the result. | Check the backend response shape for `POST /api/admin/remote/cache` and define a typed interface (or inline `{ message: string }` if the response is simple). |
| 15 | Multiple files | Code organization | Magic numbers without named constants: `50` ms (HLS check debounce, `useHLS.ts:336`), `3000` ms (HLS poll interval, `useHLS.ts:331`), `8000` ms (downloader cleanup delay, `DownloaderTab.vue`), `30_000` ms (dashboard refresh), `15_000` ms (playback autosave, `stores/playback.ts:43`), `300` ms (search debounce, `index.vue:87`). | Define named constants at the top of each file (e.g., `const HLS_POLL_INTERVAL_MS = 3000`). |
| 16 | `types/api.ts:78` | TypeScript | `MediaItem.type: 'video' \| 'audio' \| 'unknown' \| string` — the trailing `\| string` collapses the discriminated union to plain `string`, meaning TypeScript cannot narrow this field in switch/if blocks. All `item.type === 'video'` checks work accidentally, not by type safety. | Remove `\| string`. The backend either sends a known value or it doesn't. If unknown values are possible, add `\| string & {}` (branded open union) so known literals still narrow correctly, or simply add `'image'` and `'other'` as explicit members if they are valid values. |

---

### LOW

| # | File:Line | Category | Issue | Recommendation |
|---|-----------|----------|-------|----------------|
| 17 | `package.json` | Config | `nuxt`, `vue`, `vue-router`, `pinia`, `hls.js` are in `dependencies`. For a fully static build with no runtime Node.js server these are build-time-only. | Move to `devDependencies`. No functional impact, but it signals the correct usage intent and prevents accidental bundling if the project ever gains a server-side entry point. |
| 18 | `tsconfig.json` | TypeScript | Delegates entirely to `.nuxt/tsconfig.json`. There is no project-level override to enforce `strict: true`. If Nuxt's generated tsconfig changes this could silently disable strict checking. | Add `"compilerOptions": { "strict": true }` to `tsconfig.json` as an explicit declaration of intent. |
| 19 | `pages/playlists.vue:63,296,305` & `player.vue:307` | TypeScript | Non-null assertions (`!`) where the surrounding logic already guarantees non-null. Each is safe today but brittle if the surrounding guard changes. | Replace `deleteTarget.value!.id` with `deleteTarget.value?.id ?? ''` and similar optional-chain patterns to be explicit about the fallback. |
| 20 | `pages/admin.vue:9–13` | Code organization | `watchEffect` for admin redirect duplicates the `admin` middleware. Same pattern as issue #8. | Remove the `watchEffect`; rely on the `admin` middleware exclusively. |

---

## TDZ Risk Inventory

| File | Import Pattern | Risk | Fix |
|------|---------------|------|-----|
| `composables/useApiEndpoints.ts:34` | `import { useApi } from '~/composables/useApi'` — explicit static import | **None** — explicitly breaks the `#imports` cycle. This is the correct pattern. | N/A — already correct |
| `composables/useApiEndpoints.ts:36` | `const api = useApi()` at module scope | **None** — `useApi()` is a plain function with no Vue lifecycle dependency. The comment in the file correctly explains why this is safe. | N/A — already correct |
| `composables/useHLS.ts:83` | `useHlsApi()` called inside `useHLS()` function body | **None** — called at component setup time, not at module evaluation time. | N/A |
| `stores/theme.ts:36–38` | `setTheme()` called at module scope inside `defineStore` (guarded by `import.meta.client`) | **Low** — `setTheme` calls `useColorMode()` which is a Nuxt composable. This runs during Pinia store initialization from a Nuxt plugin, which is within the Nuxt app context. Guard is correct. | N/A — acceptable |
| All Pinia stores | `useXxxApi()` composables called inside action function bodies (not at module scope) | **None** — all API composable calls occur inside action functions that execute within the Vue/Nuxt context. | N/A |

**TDZ verdict:** The `#imports` circular dependency that caused the v0.115 crash is fully resolved. No new TDZ risks were introduced. The explicit static import pattern in `useApiEndpoints.ts` is the right long-term approach and should be maintained.

---

## `any` Type Inventory

**Zero `any` usages found.** All external inputs use `unknown` with safe narrowing. All API responses use generics.

---

## Component Size Report

| Component | Total Lines | Status |
|-----------|-------------|--------|
| `components/admin/SourcesTab.vue` | 812 | 🔴 SPLIT |
| `pages/player.vue` | 695 | 🔴 SPLIT |
| `pages/index.vue` | 585 | 🔴 SPLIT |
| `components/admin/SystemTab.vue` | 573 | 🔴 SPLIT |
| `components/admin/DiscoveryTab.vue` | 524 | 🟡 Large |
| `components/admin/DownloaderTab.vue` | 474 | 🟡 Large |
| `components/admin/ContentTab.vue` | 440 | 🟡 Large |
| `components/admin/MediaTab.vue` | 425 | 🟡 Large |
| `pages/playlists.vue` | 402 | 🟡 Large |
| `pages/profile.vue` | 379 | 🟡 Large |
| `components/admin/UsersTab.vue` | 359 | 🟡 Large |
| `components/admin/DashboardTab.vue` | 305 | 🟢 OK |
| `components/admin/AnalyticsTab.vue` | 281 | 🟢 OK |
| `components/admin/SecurityTab.vue` | 280 | 🟢 OK |
| `components/admin/UpdatesTab.vue` | 269 | 🟢 OK |
| `pages/upload.vue` | 255 | 🟢 OK |
| `layouts/default.vue` | 204 | 🟢 OK |
| `components/admin/PlaylistsTab.vue` | 194 | 🟢 OK |
| `components/admin/StreamingTab.vue` | 147 | 🟢 OK |
| `pages/login.vue` | 89 | 🟢 OK |
| `pages/admin.vue` | 89 | 🟢 OK |

---

## Recommendations

### Priority 1 — Fix Now (Correctness)

1. **Delete the duplicate `AnalyticsEvent` interface** (`types/api.ts:900–911`). This is a latent TypeScript bug — if the two declarations ever diverge, the compiler will silently accept the wrong type.

2. **Fix `upload.vue:146`** — replace the `$el.querySelector` hack with a proper `fileInputRef` template ref and named method.

3. **Fix `MediaItem.type` union** — remove the trailing `| string` to restore type narrowing in the components that switch on `item.type`.

### Priority 2 — High Value Refactors

4. **Extract shared utilities** — `formatBytes`, `formatTime`, and `asRecord` are duplicated across 3–4 files each. A single `utils/format.ts` and `utils/typeGuards.ts` eliminates the drift risk.

5. **Remove redundant `watchEffect` auth redirects** in `profile.vue`, `playlists.vue`, and `admin.vue`. The middleware layer already handles this.

6. **Add `document.hidden` guard** to the dashboard's 30 s interval and the HLS job poll interval. Low effort, meaningful server load reduction for users who leave admin tabs open.

### Priority 3 — Architecture (When Touching These Files)

7. **Split `SourcesTab.vue`** (812 lines, 4 distinct domains). This is the single largest refactoring win in the codebase.

8. **Split `SystemTab.vue`** (573 lines, 5 distinct admin tools).

9. **Extract `PlayerControls.vue`** from `player.vue` (the custom video control overlay is a natural extraction boundary).

10. **Extract `RecommendationRow.vue`** from `index.vue` — the "Continue Watching", "Trending", and "Recommended" rows share identical markup structure and can be a single parameterized component.

### Priority 4 — Polish

11. Move inline USelect/UTable option arrays to module-level constants to prevent unnecessary re-renders.

12. Add named constants for magic timeout/interval values.

13. Move framework packages from `dependencies` to `devDependencies` in `package.json`.

14. Add an explicit `"strict": true` override to `tsconfig.json`.

15. Replace `!` non-null assertions with optional-chaining patterns where safe alternatives exist.
