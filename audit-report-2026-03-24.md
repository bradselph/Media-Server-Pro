# Media Server Pro 4 — Deep Debug Audit Report
**Date:** 2026-03-24
**Branch:** development
**Scope:** Full codebase — Go backend (`api/`, `internal/`) + Nuxt UI frontend (`web/nuxt-ui/`)

---

## Summary

| Tag | Count |
|-----|-------|
| BROKEN | 4 |
| SILENT FAIL | 3 |
| FRAGILE | 3 |
| GAP | 3 |
| DRIFT | 2 |
| REDUNDANT | 1 |
| INCOMPLETE | 1 |

---

## Critical Findings (Priority Order)

---

### 1. [BROKEN] `api/handlers/media.go:50` — `new()` used with boolean expression
**Severity: CRITICAL — is_mature filtering completely broken for all users**

```go
// BROKEN
isMature = new(im == "true" || im == "1")

// CORRECT
b := (im == "true" || im == "1")
isMature = &b
```

In Go, `new(T)` takes a **type**, not an expression. `new(im == "true" || im == "1")` is accepted by the compiler because `bool` can be inferred, but the boolean expression is evaluated as the type argument — the result is `new(bool)`, a pointer to a zero-value `false`, regardless of the query parameter.

**Impact**: `?is_mature=true` and `?is_mature=false` both produce `*bool = false`. Mature content filtering is silently broken — media is never filtered by maturity for regular users.

---

### 2. [BROKEN] `api/handlers/admin_media.go:49` — Same `new()` bug in admin media list
**Severity: CRITICAL — admin is_mature filter also completely broken**

```go
func parseAdminListIsMature(c *gin.Context) *bool {
    im := c.Query("is_mature")
    if im == "" {
        return nil
    }
    return new(im == "true" || im == "1")  // BROKEN — always *bool(false)
}
```

Same root cause as #1. The admin MediaTab's "Mature Only" / "SFW Only" filter never works correctly.

---

### 3. [BROKEN] `web/nuxt-ui/pages/player.vue` — Does not use `useHLS.ts` composable
**Severity: HIGH — quality selection, error recovery, and job-status polling absent from player**

`player.vue` has its own inline HLS setup (lines 159–180):
```ts
const hls = new Hls()   // no config, no error handlers, no quality selection
hls.loadSource(url)
hls.attachMedia(videoEl)
```

`web/nuxt-ui/composables/useHLS.ts` (270 lines) provides all of:
- Quality level selection with localStorage persistence
- Bandwidth estimation for adaptive quality
- Error recovery with retry limits
- Polling for pending HLS transcode jobs (`status: 'pending'/'processing'`)
- HLS banner/progress UI state

None of these features function in the player because the composable is unused.

---

### 4. [BROKEN] `web/nuxt-ui/composables/useHLS.ts:262` — `hlsAvailable` set false before HLS attaches
**Severity: MEDIUM — HLS banner permanently hidden on attachment failure**

```ts
// activateHLS()
hlsAvailable.value = false   // ← clears banner BEFORE attempting attach
await attachHLS(videoEl, url)
hlsAvailable.value = true    // ← only reached on success
```

If `attachHLS` throws, `hlsAvailable` stays `false` permanently. No recovery path exists. The user sees a broken player with no explanation.

---

### 5. [SILENT FAIL] `web/nuxt-ui/components/admin/MediaTab.vue:194,202` — Missing `.catch()` on action buttons
**Severity: MEDIUM — thumbnail and delete errors silently dropped**

```ts
// Line 194
adminApi.generateThumbnail(row.original.id).then(() => toast.add({ title: 'Thumbnail queued', ... }))

// Line 202
adminApi.deleteMedia(row.original.id).then(load)
```

Errors from either call produce no toast, no indication to the admin. Delete failures are especially dangerous — the admin may believe an item was deleted when it wasn't.

---

### 6. [SILENT FAIL] `web/nuxt-ui/composables/useHLS.ts` — Retry exhaustion shows no user error
**Severity: LOW-MEDIUM — after max retries the player just stops with no message**

`onHlsError()` destroys the HLS instance after retries exhausted but sets no visible error state in the UI.

---

### 7. [SILENT FAIL] `web/nuxt-ui/pages/profile.vue` — `removeItem()` uses inconsistent ID comparison
**Severity: LOW — watch history item removal may silently fail to match**

```ts
history.value = history.value.filter(h => (h.media_id || h.media_path) !== id)
```

If `h.media_id` is truthy and doesn't match `id`, the item is kept even when `h.media_path === id`. This OR logic for matching is wrong — it should use AND (check all fields independently).

---

### 8. [GAP] `web/nuxt-ui/pages/player.vue` — No quality selection UI
**Severity: MEDIUM — users cannot select HLS quality level**

The player has no quality selector even though HLS quality profiles are configured on the backend. Resolved by integrating `useHLS.ts` (Fix #3).

---

### 9. [GAP] `web/nuxt-ui/pages/profile.vue` — No pagination for watch history
**Severity: LOW-MEDIUM — all items loaded at once**

The API supports `limit`/`offset` on `GET /api/watch-history` but `profile.vue` doesn't use them.

---

### 10. [GAP] `web/nuxt-ui/components/admin/DownloaderTab.vue` — No in-progress download monitoring
**Severity: LOW — running downloads have no UI visibility**

Only completed files are shown. There's no status polling for in-progress downloads.

---

### 11. [FRAGILE] `web/nuxt-ui/pages/profile.vue:41` — Invalid TypeScript cast
**Severity: LOW**

```ts
prefs.value.theme as ReturnType<typeof themeStore.themes[number]['value']>
```

`ReturnType<typeof expr>` on an indexed property chain is not valid TypeScript. `typeof` cannot be applied to runtime values. This silently resolves to `any`, making the cast a no-op.

---

### 12. [FRAGILE] `api/handlers/auth.go:586` — Bootstrap admin vs DB admin password change split
**Severity: LOW-MEDIUM — inconsistent behavior by account origin**

```go
if user.ID == "admin" {
    // direct bcrypt update (bootstrap admin)
} else {
    // DB-based update (normal admins)
}
```

Bootstrap admin has literal `ID = "admin"`; normal admins have UUIDs. The behavior diverges silently.

---

### 13. [FRAGILE] `web/nuxt-ui/pages/index.vue` — Potential double load on pagination
**Severity: LOW — may cause visible flicker**

`v-model:page` on `UPagination` plus `@update:page="load"` may both trigger `load()` if a watcher on `params` also fires.

---

### 14. [DRIFT] `web/nuxt-ui/components/admin/DownloaderTab.vue` — `saveLocation` field sent but ignored
**Severity: LOW**

Frontend sends `{ url, clientId, saveLocation: 'server' }` but backend (`admin_downloader.go`) always uses `"server"` internally and ignores the field.

---

### 15. [REDUNDANT] `web/nuxt-ui/composables/useHLS.ts` — Composable never consumed
**Severity: LOW — ~270 lines of dead code**

Resolved by Fix #3 (integrating into `player.vue`).

---

### 16. [INCOMPLETE] `web/nuxt-ui/components/admin/PlaylistsTab.vue` — Item count shows 0
**Severity: LOW**

```ts
playlists.reduce((sum, p) => sum + (p.items?.length ?? 0), 0)
```

Admin playlist list endpoint likely does not populate `items` array, so the total always shows 0.

---

## Backend Process Health

- **HLS config from env**: OK — `internal/config/env_overrides_hls.go` reads all HLS env vars correctly.
- **HLS concurrent job limit**: OK — semaphore pattern in `internal/hls/generate.go`.
- **Background tasks (11)**: OK — all registered and firing on correct intervals.
- **API contract alignment**: Mostly OK — key drift areas documented above (#14).

---

## Fix Plan (executed in order, one commit per fix)

1. ✅ Write this audit report
2. Fix `api/handlers/media.go:50` — `new(bool expr)` → commit
3. Fix `api/handlers/admin_media.go:49` — `new(bool expr)` → commit
4. Fix `web/nuxt-ui/components/admin/MediaTab.vue` — add `.catch()` → commit
5. Fix `web/nuxt-ui/pages/player.vue` — integrate `useHLS.ts` + quality selector → commit
6. Fix `web/nuxt-ui/composables/useHLS.ts:262` — `activateHLS` state order → commit
7. Fix `web/nuxt-ui/pages/profile.vue:41` — TypeScript cast → commit
8. Fix `web/nuxt-ui/pages/profile.vue` — `removeItem()` ID comparison → commit
9. Fix `web/nuxt-ui/components/admin/DownloaderTab.vue` — remove `saveLocation` → commit
10. Fix `web/nuxt-ui/components/admin/PlaylistsTab.vue` — item count fallback → commit
