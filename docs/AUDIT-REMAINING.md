# Audit: Remaining issues (as of last full scan)

## Resolved / Low priority
- **TODOs**: Project Go/TS/TSX and scripts have been audited; remaining TODOs are in `node_modules`, CHANGELOG (historical), and docs that reference the word "TODO".
- **panic/recover**: Intentional in handler (nil deps), auth (crypto/rand), config (recover in Apply), admin_classify (recover in classify), hls/jobs (recover in worker). All documented or obvious.
- **admin_media.go.bak**: Removed from disk (was untracked; gitignored by `*.bak`).

## Config
- **config.json**: Contains `_TODO_*` metadata keys (server port, timeouts, admin auth, features, database password, updater). Port was 3000 vs defaults 8080 — align to 8080 and move notes to docs/CONFIG-NOTES.md.

## Frontend lint (eslint)
- **Errors**: Nested ternaries (sonarjs/no-nested-conditional), nested functions (useHLS), void operator (UsersTab), no-nested-functions (useHLS). Fix in batches.
- **Warnings**: Duplicate string literals (sonarjs/no-duplicate-string), eqeqeq (remaining), react-hooks/exhaustive-deps. Can fix incrementally.

## Other
- **HuggingFace client**: Comments reference deprecated api-inference URL (410); code already uses router. No change needed.
- **nolint:dupl**: user_permissions_repository and user_preferences_repository; comment explains parallel-by-design.
