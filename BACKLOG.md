# Backlog

Open / deferred items, consolidated 2026-06-26 from the now-removed `ADMIN-PANEL-AUDIT.md`
and `ADOPTION-RECOMMENDATIONS.md` (both completed; most of their items shipped). Only the
items still requiring a decision, an asset, or an isolated larger change remain here. Numbers
in parentheses are the original recommendation IDs.

## Admin panel (Theme 5 — discoverability)

- **Media Reports panel placement** — mounted under *Media ▸ All Media*; users expect it under
  *Moderation*. Move it. *(awaiting product call)*

## Backend reliability (deferred)

- **Deletion-request stuck-pending on partial failure** — in `AdminProcessDeletionRequest`
  (`api/handlers/deletion_requests.go`), if `auth.DeleteUser` succeeds but the subsequent
  `UpdateStatus` fails, the account is gone yet the request stays `pending`. The current code
  logs and returns a clear 500. A previously-proposed fix (write a new `inconsistent` status on
  the failure path) is **not** worth it as-is: the failure mode is the DB being unavailable, so a
  second status write would fail identically — and it adds a model enum value plus migration and
  admin-UI rendering surface to the most consequential operation. A real fix needs `DeleteUser` +
  status-update in one transaction (or an idempotent re-process that tolerates an already-deleted
  user). Covered defensively by tests as of 2026-06-29. Decide the transactional approach before
  changing behaviour.

## Adoption / growth (deferred)

- **(#15) Invalidate sitemap + shell-discovery caches on ingest** — new media is invisible to
  crawlers for up to 1h (`seo.go`) / 10min (`shell.go`). Proper fix needs a scan/import-completion
  callback across the `media → handlers` boundary without creating an import cycle. Caches already
  self-expire, so this is an optimization, not a correctness bug. Files: `api/handlers/seo.go`,
  `api/handlers/shell.go`, scan/import completion hook.
- **(#16) apple-touch-icon + web manifest (+ OG banner asset)** — only `favicon.svg` exists. Needs
  a real 180×180 PNG, a minimal `site.webmanifest` (neutral `short_name` for discreet bookmarking),
  `<meta name="theme-color">`, and a branded `og:image` for social shares. **Needs a real raster
  asset before it can land.** Files: `web/nuxt-ui/nuxt.config.ts`, `web/nuxt-ui/public/`.
- **(#20) Drop confirm-password on signup** — product/security call. Removes typo protection, and
  with no password-reset flow a typo'd password locks the user out. Decide before changing.
  File: `web/nuxt-ui/pages/signup.vue`.
- **(#21) View-weighted sitemap priority + `UpdatedAt` lastmod** — all player entries use hardcoded
  priority 0.6 and `lastmod = date_added`; admin edits don't bump lastmod. Requires adding
  `UpdatedAt` to `MediaItem` → its own change with **DB migration testing**. Files: media model,
  migration, `api/handlers/seo.go`.
- **(#22) Show mature suggestions to age-gate-verified guests** — hero + Popular row exclude
  `is_mature` for guests (`internal/suggestions`), so on an all-adult library the most representative
  content never shows to new visitors. High conversion impact, but **needs explicit legal/compliance
  sign-off** (changes what unauthenticated, age-verified visitors can see).
- **(#23) Consolidate first-load calls into `/api/init`** — a first guest fires ≥6 parallel calls on
  mount (age-gate, cookie-consent, version, settings, suggestions, media). Merge 3–4 into one
  response; defer `/api/version` to `requestIdleCallback`. Larger refactor across several components.

## Round-2 hunt follow-ups (deferred 2026-06-29)

Confirmed by the round-2 adversarial gap hunt; deferred because each is a larger
refactor / behaviour change / hot-path optimisation that wants its own focused,
measured change rather than batching. The 15 higher-confidence, contained fixes
from that hunt already shipped.

- **GetSuggestions O(catalog × view-history) under a held RLock** — on every
  authenticated home load, scoreRecentlyViewed linearly scans up to 500 ViewHistory
  entries per catalog item, all under one `m.mu.RLock()`. Pre-build a
  `recentCategorySet map[string]bool` once and thread it through
  scoreMedia → scoreMediaForProfile → scoreRecentlyViewed (update the 4 test call
  sites in `suggestions_extended_test.go`). Makes the pass O(n). Perf, not a bug —
  benchmark before/after. File: `internal/suggestions/suggestions.go`.
- **GetCategoryMemberIDs uncached** — fires 2 DB round-trips + an O(catalog) tag
  scan on every category-filtered browse page. Add a 30s-TTL member-set cache on the
  media Module + an `InvalidateCategoryMemberCache(id)` called from
  AddCategoryItems/RemoveCategoryItem/DeleteCategory/UpdateCategory. Files:
  `internal/media/categories.go`, `internal/media/discovery.go`, `api/handlers/categories.go`.
- **DeleteJob doesn't fence in-progress lazy transcodes before os.RemoveAll** —
  `lazyTranscodeQuality` runs in the HTTP handler goroutine, tracked in activeJobs but
  with no jobDone entry, so DeleteJob's `<-doneCh` doesn't wait for it and RemoveAll can
  delete the dir mid-write. Add a per-job `lazyWg sync.Map` (Add/Done in
  lazyTranscodeQuality; Wait in DeleteJob before RemoveAll; delete in cleanup paths).
  Files: `internal/hls/{module,transcode,jobs,cleanup}.go`.
- **UpdateMetadata commits in-memory before confirming the DB write** — a 500 is
  returned to the client but the in-memory tags/is_mature/custom fields are already
  mutated (and revert on restart). Restructure DB-first like UpdatePlaybackPosition:
  snapshot under RLock, apply to a copy, Upsert, and only commit to the live maps +
  syncMediaItem on success. Core function — needs careful test coverage.
  File: `internal/media/management.go`.
- **metadata-cleanup task is effectively a no-op** — the 24h task only calls Scan(),
  which replaces `m.media`/`m.mediaByID` but never prunes `m.metadata`/`fingerprintIndex`,
  so externally-deleted files' metadata (and DB rows via the post-scan save) accumulate
  unboundedly. Prune stale paths inside Scan() under the write lock + a background DB
  delete. Trade-off: drops cross-scan-cycle move detection. Files:
  `internal/media/discovery.go` (Scan), `cmd/server/main.go`.
- **saveJob swallows DB errors** — a cancelled HLS job whose status fails to persist
  returns success; on restart it reloads as Running and re-transcodes. Make saveJob
  return its error and have CancelJob log a warning (other call sites `_ =`). Low sev.
  File: `internal/hls/jobs.go`.
- **analytics memo thundering-herd** — concurrent cold-cache misses each run the full
  50k–100k-event aggregation. Wrap compute() in a `singleflight.Group` (adds
  `golang.org/x/sync`). Low sev. File: `internal/analytics/cache.go`.

- **thumbnails & HLS ignore their injected S3 storage backend** (wiring finding RE-1,
  deferred — "report only"). `main.go` calls `m.thumbnails.SetStore(stores.thumbnails)` /
  `m.hls.SetStore(stores.hlsCache)`, but neither module ever reads `m.store` — all thumbnail
  and HLS-cache I/O hard-codes local disk (`m.thumbnailDir`, hls cache dir), unlike
  streaming/upload/media which branch on `!m.store.IsLocal()`. So with `storage.backend="s3"`
  thumbnails/HLS still write to local disk: lost on ephemeral containers, and 404 across
  multi-instance deployments. Fix is a product decision: either implement real S3 I/O
  (mirror `internal/streaming/streaming.go`'s branching across generate/paths/cleanup/
  preview/api + the serving path) OR drop the dead `SetStore` wiring + the misleading
  "storage backend for I/O" doc comments so the code stops implying S3 support it lacks.
  Files: `internal/thumbnails/*.go`, `internal/hls/module.go`, `cmd/server/main.go:338,350`.

## Launch prerequisites (not optional before public launch)

- **Real brand / compliance values** — the 2257 and DMCA pages fall back to `@example.com`
  placeholders when brand env vars are unset; serving those live is a legal liability under
  18 U.S.C. § 2257. Set real custodian/agent/contact values (the UI now warns admins on the
  placeholder, but the values themselves still need to be filled in).
