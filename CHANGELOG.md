# Changelog

## [0.27.0] - 2026-03-02 (minor)

- Merge pull request #30 from bradselph/development
- feat(receiver): content fingerprint dedup for slave media


## [0.26.0] - 2026-03-02 (minor)

- Merge pull request #29 from bradselph/development
- feat(receiver): WebSocket tunnel for slave-master communication


## [0.25.0] - 2026-03-02 (minor)

- Merge pull request #28 from bradselph/development
- fix(config): accept both FEATURE_ and FEATURES_ env var names


## [0.24.0] - 2026-03-02 (minor)

- Merge pull request #27 from bradselph/development
- fix(deploy): reliable .deploy.env update, auto-restart after setup-receiver, fix MASTER_URL
- feat(deploy): auto-sync master URL and API key between deploy.sh and deploy-slave.sh
- feat(slave): add --local mode to deploy-slave.sh for running on same machine


## [0.23.0] - 2026-03-02 (minor)

- Merge pull request #26 from bradselph/development
- feat(slave): add deploy-slave.sh and systemd unit for slave receiver node
- Merge branch 'main' into development


## [0.22.0] - 2026-03-02 (minor)

- Merge pull request #25 from bradselph/development
- fix(security,hls,receiver): fix banned IP metadata, HLS env vars, and slave node improvements


## [0.21.0] - 2026-03-02 (minor)

- Merge pull request #24 from bradselph/development
- feat(frontend): implement receiver tab and wire all remote/receiver APIs


## [0.20.0] - 2026-03-02 (minor)

- Merge pull request #23 from bradselph/development
- feat(deploy): add remote media proxy and receiver setup to deploy.sh
- fix(frontend): add missing UserPermissions import in endpoints.ts


## [0.19.0] - 2026-03-02 (minor)

- Merge pull request #22 from bradselph/development
- fix(frontend): apply formatTitle to all suggestion/related media name displays
- oops
- removed
- fix(mature): show blurred mature content to guests with sign-in gate
- chore: remove old unused files and directories
- fix(updater): source update now correctly checkouts configured branch
- fix: API contract fixes, mature scanner persistence, and code cleanup
- add todos


## [0.18.0] - 2026-03-01 (minor)

- Merge pull request #21 from bradselph/development
- feat(receiver): add master-slave media distribution system
- fix(admin): security hardening, bulk tags, state bugs, case-insensitive filter


## [0.17.0] - 2026-03-01 (minor)

- Merge pull request #20 from bradselph/development
- fix(admin): fix bulk category clearing, add missing endpoints and filters


## [0.16.0] - 2026-03-01 (minor)

- Merge pull request #19 from bradselph/development
- feat(admin): improve media management UX and fix update bugs
- fix(types): correct DetectedMediaInfo to match backend categorizer.MediaInfo


## [0.15.0] - 2026-03-01 (minor)

- Merge pull request #18 from bradselph/development
- Update AdminPage.tsx


## [0.14.0] - 2026-03-01 (minor)

- Merge pull request #17 from bradselph/development
- sort filter update


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