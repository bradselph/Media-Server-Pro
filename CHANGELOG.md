# Changelog

## [0.13.0] - 2026-03-01 (minor)

- Merge pull request #16 from bradselph/development
- fix(updater): backup only binary instead of entire deployment directory
- Update deploy.sh


## [0.12.0] - 2026-03-01 (minor)

- Merge pull request #15 from bradselph/development
- feat(media-id): Implement stable UUIDs and content fingerprinting


## [0.11.0] - 2026-02-28 (minor)

- Merge pull request #14 from bradselph/development
- Fix vite.config.ts TS error: import defineConfig from vitest/config
- Fix deploy: commit package-lock.json to ensure reproducible npm builds


## [0.10.0] - 2026-02-28 (minor)

- Merge pull request #13 from bradselph/development
- Update deploy.sh
- Implement stable UUID-based media IDs (decouple ID from file path)


## [0.9.0] - 2026-02-28 (minor)

- Merge pull request #12 from bradselph/development
- Add home section visibility preferences and fix suggestion rendering
- Fix thumbnail URL format: use ?id= instead of ?path=
- REC-01: Add frontend test infrastructure (Vitest + testing-library)
- REC-01 + REC-16 fix: Add test infrastructure and fix scanner boundary patterns
- REC-15: Add SectionErrorBoundary for per-section error isolation
- REC-14: Add signal/AbortController support to API client wrappers
- Add Docker and Docker Compose for containerized development (REC-18)
- Add CI workflow: build, vet, test, security, frontend checks (REC-13)
- Evict stale suggestion profiles to prevent unbounded memory growth (REC-06)
- Replace strings.Contains keyword matching with word-boundary regex (REC-16)
- Add SHA256 checksum verification to binary updater (REC-12)
- Limit ETag buffering to 64 KB per response (REC-04)
- Fix thumbnail inFlight leak: timestamp values + stale-entry eviction (REC-20)
- Fix task scheduler race: set loopRunning under write lock in Start (REC-05)
- Add Makefile for build automation (REC-17)


## [0.8.0] - 2026-02-28 (minor)

- Merge pull request #11 from bradselph/development
- Add frontend API endpoints for playlist reorder, clear, and copy
- Add 15-second timeout to all ffmpeg.Probe calls
- Improve useHLS: swapAudioCodec recovery, stable onFallback ref, cancelled guard
- Wire security whitelist/blacklist enable flags to runtime config updates
- Add admin endpoints for active streams, uploads, and user sessions
- Add ReorderPlaylistItems, ClearPlaylist, CopyPlaylist handlers and routes
- Fix analytics.TrackView to record views for authenticated users
- Wire suggestions.RecordCompletion when playback finishes
- Add HLS RecordAccess calls when serving playlists and segments
- Add missing background tasks and skip partial downloads in media scanner


## [0.7.0] - 2026-02-28 (minor)

- Merge pull request #10 from bradselph/development
- Fix systemd service: substitute DEPLOY_DIR at install time, move StartLimit to [Unit]
- Fix useHLS: don't call setError/onFallback when component unmounted


## [0.6.0] - 2026-02-28 (minor)

- Merge pull request #9 from bradselph/development
- Add requireThumbnails guard; fix nil panics in thumbnails and backups
- Fix nil dereference bugs in media and upload handlers
- Fix nil dereference in ApplySourceUpdate goroutine
- Fix: Security/Debug - Harden path resolution in resolveRelativePath and log symlink errors
- Fix: Bug - UpdatePlaylist returns null data when post-update fetch fails
- Fix: Bug - nil pointer panic in UpdatePreferences when GetUser returns (nil, nil)


## [0.5.0] - 2026-02-28 (minor)

- Merge pull request #8 from bradselph/development
- Add nil guards for all optional modules to prevent nil pointer panics


## [0.4.0] - 2026-02-28 (minor)

- Merge pull request #7 from bradselph/development
- Remove deprecated frontend types and consolidate AdminUser → User
- Fix authStore 401 detection to match ApiError class shape
- Add nil guards for optional admin/playlist modules to prevent panics
- Fix media-not-found on deploy, improve startup readiness, enrich watch history


## [0.3.0] - 2026-02-28 (minor)

- Merge pull request #6 from bradselph/development
- Use PlaylistItem type in addItem instead of removing the import
- Fix frontend build errors: mediaPath→mediaId, discovery type, unused import
- Wire module constructors to accept *database.Module for lazy repo init
- Merge pull request #5 from bradselph/development
- Merge branch 'main' into development
- Switch modules to DB-backed persistence
- Merge branch 'main' into development
- Add MySQL repositories, migrations and auth fixes
- Switch media APIs and UI to use IDs (not paths)
- Use media ID in APIs and hide internal paths
- Hide filesystem paths in APIs; SPA route refactor
- Security and robustness hardening across codebase
- Split auto-version into dev/release/sync workflows


## [0.2.0] - 2026-02-28 (minor)

- Merge pull request #5 from bradselph/development
- Merge branch 'main' into development
- Switch modules to DB-backed persistence
- Merge branch 'main' into development
- Add MySQL repositories, migrations and auth fixes
- Switch media APIs and UI to use IDs (not paths)
- Use media ID in APIs and hide internal paths
- Hide filesystem paths in APIs; SPA route refactor
- Security and robustness hardening across codebase
- Split auto-version into dev/release/sync workflows


## [0.2.0] - 2026-02-28 (minor)

- Generate dev build label instead of bumping patch version


## [0.1.0] - 2026-02-27 (minor)

- Split auto-version into dev/release/sync workflows