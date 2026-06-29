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

## Launch prerequisites (not optional before public launch)

- **Real brand / compliance values** — the 2257 and DMCA pages fall back to `@example.com`
  placeholders when brand env vars are unset; serving those live is a legal liability under
  18 U.S.C. § 2257. Set real custodian/agent/contact values (the UI now warns admins on the
  placeholder, but the values themselves still need to be filled in).
