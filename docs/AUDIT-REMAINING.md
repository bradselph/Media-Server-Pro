# Audit: Remaining issues (as of last full scan)

Five fix loops completed: config/docs (CONFIG-NOTES, AUDIT-REMAINING), cleanup (.bak removed), UsersTab (void + nested ternary), HuggingFaceTab (duplicate string + nested ternary), PlaylistsTab (nested ternary). Additional repair loop: see table below.

## Audit repair loop — 2026-03-21 (10 cycles)

| # | Issue | Status |
|---|--------|--------|
| 1 | Security middleware exempted `/static/` but SPA assets are served at `/web/static/` — static chunks were incorrectly rate-limited | **Resolved** |
| 2 | `themeStore.ts`: redundant `ThemeId` alias (`string`) tripped sonarjs | **Resolved** |
| 3 | `useDownloaderWebSocket.ts`: `connect` before declare + deep nesting; timer cleanup snapshot | **Resolved** |
| 4 | `Toast.tsx`: nested functions depth (dismiss toast) | **Resolved** |
| 5 | `DownloaderTab.tsx`: nested ternary for download progress bar color | **Resolved** |
| 6 | `DownloaderTab.tsx`: nested ternaries for dependency cell value | **Resolved** |
| 7 | `DownloaderTab.tsx`: Settings KV table missing header row | **Resolved** |
| 8 | `endpoints.ts`: `listMedia` `!=` for optional numbers | **Resolved** |
| 9 | `DownloaderTab.tsx` StatusSection: `!= null` health fields | **Resolved** |
| 10 | `ContentReviewTab.tsx` duplicate muted color; `playerHLS.ts` unused eslint-disable | Pending |

### TODO (discovered, out of scope for loop above)

- **DownloaderTab / other admin tabs**: Remaining `sonarjs/no-duplicate-string` warnings — fix incrementally.
- **vite.config.ts**: Proxy target URL repeated — extract constant if desired.

## Resolved / Low priority
- **TODOs**: Project Go/TS/TSX and scripts have been audited; remaining TODOs are in `node_modules`, CHANGELOG (historical), and docs that reference the word "TODO".
- **panic/recover**: Intentional in handler (nil deps), auth (crypto/rand), config (recover in Apply), admin_classify (recover in classify), hls/jobs (recover in worker). All documented or obvious.
- **admin_media.go.bak**: Removed from disk (was untracked; gitignored by `*.bak`).

## Config
- **config.json**: Contains `_TODO_*` metadata keys (server port, timeouts, admin auth, features, database password, updater). Port was 3000 vs defaults 8080 — align to 8080 and move notes to docs/CONFIG-NOTES.md.

## Frontend lint (eslint)
- **Errors**: Nested ternaries, nested functions (Toast, useDownloaderWebSocket), table-header, redundant type alias — address via repair loop table.
- **Warnings**: Duplicate string literals (sonarjs/no-duplicate-string), eqeqeq (remaining), react-hooks/exhaustive-deps. Fix incrementally.

## Other
- **HuggingFace client**: Comments reference deprecated api-inference URL (410); code already uses router. No change needed.
- **nolint:dupl**: user_permissions_repository and user_preferences_repository; comment explains parallel-by-design.
