# Admin Panel — Cleanup Notes (2026-06-24)

## Resolution (2026-06-24) — implemented, typecheck + `nuxt generate` green
- **① Duplicate settings → DONE.** Each now lives in one tab: HLS auto-gen/pre-gen →
  *HLS Jobs*; backup retention → *Backups & DB*; updater method/branch → *Updates*;
  HTTPS/HSTS/CORS (+ cert/key, max-age, origins) → *Security ▸ Settings*. Removed from the
  big Settings panel. (commit 35124101)
- **② Redundant views → DONE.** One home each: module health → *System ▸ Status*; live
  streams → *Dashboard*; audit log → *Security*; feature flags → *Settings*; scan → *Media*.
  (commit f550e42a)
- **③ Dead UI knobs → DONE (made functional).** items-per-page (desktop/mobile) + mobile grid
  columns now drive the public browse grid; per-user prefs still override. (commit 7e55be31)
- **④ Misleading labels → DONE.** Adaptive Bitrate, Max Reconstruct, session counters,
  alert-rules note, Run-Scan rename. (commit f141f328)
- **⑥ Small bugs → DONE.** Generate-HLS now video-only; per-user analytics shows a
  disabled message; category cover UUID validated. (#21 was a false positive.) (commit f141f328)
- **⑤ Discoverability → NOT STARTED** (wasn't in the decision round): Media Reports still
  under *Media* not *Moderation*; Duplicates tab still shows a bare error when its feature is
  off. Awaiting your call.

---



Catalog of admin-panel features that are **duplicated**, **dead/non-working**, or **unclear**.
Built from a full sweep of all 9 top-level tabs + 28 admin components, wiring each control to its
Go endpoint/config field and verifying it does real work. Nothing here is changed yet — this is the
notes list to decide against. Mark each item: **FIX / REMOVE / MERGE / LEAVE**.

Tab map: Dashboard · Users · Media(All Media, Duplicates) · Library(Categories, Playlists) ·
Ingest(Downloader, Remote, Crawler, Receiver) · Moderation(Mature & Tagging, Discovery & AI) ·
Analytics · Security · System(Status, Settings, HLS Jobs, Tasks & Logs, Backups & DB, Updates)

---

## THEME 1 — Same setting editable in TWO places (true duplicates)
Each is one config field with two separate UI controls. They can show conflicting states and double the
surface to maintain.

| # | Setting | Place A | Place B | Field |
|---|---------|---------|---------|-------|
| 1 | HLS auto-generate | System ▸ Settings (HLS) | System ▸ HLS Jobs ("Auto-generate HLS on scan") | `hls.auto_generate` |
| 2 | HLS pre-generate interval | System ▸ Settings (HLS) | System ▸ HLS Jobs | `hls.pre_generate_interval_hours` |
| 3 | Backup retention count | System ▸ Settings (Backup) | System ▸ Backups & DB | `backup.retention_count` |
| 4 | Updater method + branch | System ▸ Settings (Updater) | System ▸ Updates | `updater.update_method`, `updater.branch` |
| 5 | HTTPS / HSTS / CORS toggles | System ▸ Settings (Server/Security) | Security ▸ Settings sub-tab | `server.enable_https`, `security.hsts_enabled`, `security.cors_enabled` |

**Decision:** which side is the single source of truth?

---

## THEME 2 — Same read-only data shown in TWO places (redundant views)
| # | Data | Place A | Place B | Note |
|---|------|---------|---------|------|
| 6 | Module health grid | Dashboard | System ▸ Status | two endpoints, same concept |
| 7 | Active/live streams | Dashboard | Analytics ▸ Active Streams | both `GET /api/admin/streams` |
| 8 | Admin audit log | Security ▸ Audit Log | Analytics ▸ "Admin Actions" feed | same endpoint, filtered view |
| 9 | Feature flags | Dashboard (read-only grid) | System ▸ Settings (editable toggles) | display vs edit |
| 10 | "Scan media" button | Dashboard | Media ▸ All Media ("Scan Library") | both `POST /api/admin/media/scan` |

---

## THEME 3 — Controls that DON'T DO ANYTHING (verified dead)
| # | Control | Where | Evidence |
|---|---------|-------|----------|
| 11 | Items per page (desktop) | System ▸ Settings ▸ UI Defaults | site reads page size from per-user prefs + hardcoded `?? 24`; `ui.items_per_page` never read (`index.vue:299/541/851`) |
| 12 | Items per page (mobile) | System ▸ Settings ▸ UI Defaults | `ui.mobile_items_per_page` only in type defs, no consumer |
| 13 | Mobile grid columns | System ▸ Settings ▸ UI Defaults | `ui.mobile_grid_columns` exposed in `/api/server-settings`, no FE reader |

---

## THEME 4 — Works, but label/behavior is misleading or unclear
| # | Control | Where | Problem |
|---|---------|-------|---------|
| 14 | "Adaptive Bitrate (HLS)" | System ▸ Settings ▸ Streaming | Sounds like backend quality-switching. Actually only toggles whether the **player** auto-starts HLS. No backend streaming code reads `streaming.adaptive`. |
| 15 | "Max Reconstruct Events" | System ▸ Settings ▸ Analytics | Only applied at startup (`analytics/module.go:83`); changing it live does nothing until restart, no warning shown. |
| 16 | "Blocked (Session)" / "Rate Limited (Session)" | Security ▸ Stats | In-memory counters that reset on restart; not labeled as such. |
| 17 | Custom Alert Rules | Analytics | Saved only in browser localStorage — per-browser, lost on clear, not shared across admins; no indication it isn't server-side. |
| 18 | "Scan Media" vs "Run Scan" | Dashboard vs Moderation ▸ Mature & Tagging | Near-identical name + icon, completely different jobs (library re-index vs mature-content scorer). |

---

## THEME 5 — Discoverability
| # | Item | Note |
|---|------|------|
| 19 | Media Reports panel | Mounted under **Media ▸ All Media**, not **Moderation** where reports are expected. |
| 20 | Duplicates tab when feature is OFF | Shows only an error toast (HTTP 503), no "feature disabled" banner/empty state. |

---

## THEME 6 — Small bugs noticed in passing (confirm before fixing)
| # | Bug | Where |
|---|-----|-------|
| 21 | "Run Task Now" (classification) fires a 503 if its status check failed to load (missing null-guard) | Moderation ▸ Discovery & AI |
| 22 | Per-user analytics silently shows all-zeros when analytics is disabled (no "disabled" message) | Users ▸ View Analytics |
| 23 | "Generate HLS" row button shows on image-type items (HLS can't apply); no guard | Media ▸ All Media |
| 24 | Category "Cover Media ID" accepts any string, no validation/feedback on bad UUID | Library ▸ Categories |

---

## Conceptual overlaps (probably LEAVE — architecturally distinct, noting for completeness)
- **Remote Sources** (this server polls an outside source) vs **Receiver** (other servers push into this one) — both surface "remote media" but are opposite directions.
- **Crawler approve → Extractor** vs **direct "Extract from URL"** — two paths to the same extractor, intentional (reviewed vs manual).
- **Downloader "Download (best quality)"** vs per-stream "Download" — same endpoint, different URL arg; intentional.
