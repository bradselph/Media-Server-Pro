# Changelog

## [0.62.0] - 2026-03-07 (minor)

- Merge pull request #65 from bradselph/development
- fix(admin): include extractor, crawler, duplicates in system health check


## [0.61.0] - 2026-03-07 (minor)

- Merge pull request #64 from bradselph/development
- feat(duplicates): extract duplicate detection into independent module


## [0.60.0] - 2026-03-07 (minor)

- Merge pull request #63 from bradselph/development
- feat(receiver): add duplicate detection and fix thumbnail regeneration
- go fmt all


## [0.59.0] - 2026-03-07 (minor)

- Merge pull request #62 from bradselph/development
- fix(frontend): use media ID instead of path for validator API calls


## [0.58.0] - 2026-03-06 (minor)

- Merge pull request #61 from bradselph/development
- refactor(deploy): consolidate scripts into setup.sh + deploy.sh


## [0.57.0] - 2026-03-06 (minor)

- Merge pull request #60 from bradselph/development
- fix(backup): clean up oversize file in extractFile on zip bomb rejection
- fix(autodiscovery): check preconditions before creating destination directory
- fix(handlers): remove dead ScanMetadata field from ScanContent request
- fix(middleware): buffer ETag response body before flushing to client


## [0.56.0] - 2026-03-06 (minor)

- Merge pull request #59 from bradselph/development
- Merge branch 'main' into development
- fix(repo): propagate error instead of silently returning empty playlists
- Merge branch 'main' into development


## [0.55.0] - 2026-03-06 (minor)

- Merge pull request #58 from bradselph/development
- fix(categorizer): remove deprecated isMediaFile wrapper (dead code)
- fix(validator): remove deprecated ValidateDirectory (dead code, nil append bug)
- fix(extractor): clean up all playlist cache entries on RemoveItem
- fix(repo): return nil,nil on not-found in ScanResultRepository.Get
- fix(repo): use errors.Is() for ErrRecordNotFound in ip_list_repository
- fix(upload): sanitize userID in GetUserStorageUsed to prevent path traversal
- fix(remote): fix lock upgrade race in getCachedMedia
- fix(crawler): add nil guards to prevent panic when crawler is disabled
- fix(handlers): fix loop variable pointer aliasing in GetBannedIPs
- fix(backup): use database for manifest storage instead of filesystem
- fix(repo): prevent IncrementViews from inserting rows without stable_id
- fix(repo): use MySQL VALUES() syntax instead of PostgreSQL excluded.col in Upsert
- fix(analytics): remove 7 deprecated Track* methods with zero callers
- fix(analytics): remove deprecated GetActiveSessions with zero callers
- fix(suggestions): remove deprecated GetUserProfile function
- fix(main): move suggestion seeding goroutine before srv.Start()
- fix(handlers): remove unnecessary scanner import hack in upload.go
- fix(handlers): remove redundant trimSpace reimplementation in admin_media.go
- fix(middleware): set written flag on ETag buffer overflow
- fix(server): load TLS certificates into tlsConfig for HTTPS


## [0.54.0] - 2026-03-05 (minor)

- Merge pull request #57 from bradselph/development
- fix(handlers): handle edge cases across auth, media, analytics, security, playlists
- Update backup.go


## [0.53.0] - 2026-03-05 (minor)

- Merge pull request #56 from bradselph/development
- fix(misc): clean up remaining TODOs, fix backup types, fix crawler syntax error
- fix(security): persist IP bans to MySQL so they survive server restarts
- fix(analytics): implement UniqueUsers, TotalWatchTime, UniqueViewers, AvgWatchDuration; fix hourly TZ
- fix(receiver): enforce MaxProxyConns via buffered channel semaphore
- fix(frontend): fix stale comments, remove absent details field, improve media name fallback
- fix(upload): return upload_id in response, keep progress accessible after completion


## [0.52.0] - 2026-03-05 (minor)

- Merge pull request #55 from bradselph/development
- refactor(security): remove unused net/http Middleware method (REC-19)
- refactor(models): remove deprecated userStorage, MarshalStorage, UnmarshalStorage
- fix(streaming): enforce per-user stream limits for receiver-sourced media
- fix(security): validate discovery dir and use media ID in validator handlers
- fix(auth): preserve allowGuests across logout in authStore
- fix: gate autodiscovery on feature flag, make backup retention configurable


## [0.51.0] - 2026-03-04 (minor)

- Merge pull request #54 from bradselph/development
- fix(download-move): scan subdirs recursively, default to move not copy


## [0.50.0] - 2026-03-04 (minor)

- Merge pull request #53 from bradselph/development
- fix(download-move): fix cp+rm misreport, scoped chown, subdir warning
- fix(download-move): fix cp+rm misreport, scoped chown, subdir warning
- feat(crawler): add stream crawler module for M3U8 discovery


## [0.49.0] - 2026-03-04 (minor)

- Merge pull request #52 from bradselph/development
- feat(extractor): add HLS stream proxy module


## [0.48.0] - 2026-03-04 (minor)

- Merge pull request #51 from bradselph/development
- go fmt all


## [0.47.0] - 2026-03-04 (minor)

- Merge pull request #50 from bradselph/development
- fix(system): remove duplicate analytics_tracking feature flag
- Merge branch 'main' into development
- fix(system): replace misleading schema_version with app_version
- fix(auth): remove legacy admin_session dead code from Logout
- fix(thumbnails): use canonical IsAudioExtension for audio detection
- feat(media): add random sort option to media library
- fix(suggestions): seed catalogue immediately after initial media scan
- fix(suggestions): repair broken similar media and improve variety
- Merge branch 'main' into development


## [0.46.0] - 2026-03-04 (minor)

- Merge pull request #49 from bradselph/development
- chore: annotate incomplete/wired-but-unused features with TODO comments
- fix: prevent pagination reset when navigating pages in React Router v7


## [0.45.0] - 2026-03-04 (minor)

- Merge pull request #48 from bradselph/development
- Update CLAUDE.md


## [0.44.0] - 2026-03-04 (minor)

- Merge pull request #47 from bradselph/development
- fix: prevent pagination resetting to page 1 on async data load


## [0.43.0] - 2026-03-04 (minor)

- Merge pull request #46 from bradselph/development
- feat: bug fixes, CDN-ready HLS hosting, smart cleanup, lazy transcoding


## [0.42.0] - 2026-03-04 (minor)

- Merge pull request #45 from bradselph/development
- feat: mature content access control with full redirect flow


## [0.41.0] - 2026-03-04 (minor)

- Merge pull request #44 from bradselph/development
- Merge branch 'main' into development
- feat: enhanced player settings panel, quality persistence, and UI polish
- fix: show mature items blurred for guests and auto-activate HLS
- Merge branch 'main' into development


## [0.40.0] - 2026-03-03 (minor)

- Merge pull request #43 from bradselph/development
- fix: deploy script reads branch from .env UPDATER_BRANCH
- fix: non-null assertion for bandwidth in PlayerSettingsPanel


## [0.39.0] - 2026-03-03 (minor)

- Merge pull request #42 from bradselph/development
- fix: correct broken player controls, settings panel, and missing code
- fix: handle thumbnail pending status and typed nil interface panic


## [0.38.0] - 2026-03-03 (minor)

- Merge pull request #41 from bradselph/development
- fix: multiple admin handler bugs across tabs


## [0.37.0] - 2026-03-03 (minor)

- Merge pull request #40 from bradselph/development
- go fmt all
- fix: suppress noisy schema_migrations warning on GORM auto-migration
- fix: persist scan results to DB in ScanDirectory for review queue
- fix: use MySQL VALUES() syntax instead of PostgreSQL excluded.* in upsert


## [0.36.0] - 2026-03-03 (minor)

- Merge pull request #39 from bradselph/development
- fix: add missing content_fingerprint field to ReceiverMediaItem type
- fix: serve placeholder thumbnails for receiver media items
- fix: complete receiver media item data and enable playback tracking
- fix: strip password from RemoteSource in API responses
- fix: correct log viewer display order and thumbnail input label
- Merge branch 'main' into development
- fix: correct misleading TODO comments on BannedIP and storage_quota types
- fix: replace incorrect TODO comment on storage_quota in GetPermissions
- fix: remove unused encoding/json import from admin_security.go
- fix: apply scanner mature flags to media library in background task
- fix: prevent media list from refreshing on navigation and filter changes


## [0.35.0] - 2026-03-03 (minor)

- Merge pull request #38 from bradselph/development
- feat: auto-discover slave config from local master config files
- docs: update CLAUDE.md to reflect current project state


## [0.34.0] - 2026-03-03 (minor)

- Merge pull request #37 from bradselph/development
- Update deploy.sh


## [0.33.0] - 2026-03-03 (minor)

- Merge pull request #36 from bradselph/development
- fix: apply all filters and sort to receiver items in ListMedia


## [0.32.0] - 2026-03-03 (minor)

- Merge pull request #35 from bradselph/development
- fix: migrate legacy receiver media IDs on startup
- fix: opaque receiver media IDs and HTTP streaming fallback
- fix: remove filesystem path from media search matching
- fix: distinguish duplicate files from moved files during scan


## [0.31.0] - 2026-03-03 (minor)

- Merge pull request #34 from bradselph/development
- fix: deduplicate local media by content fingerprint during scan


## [0.30.0] - 2026-03-03 (minor)

- Merge pull request #33 from bradselph/development
- fix: mature content enforcement, source transparency, slave efficiency, dedup


## [0.29.0] - 2026-03-03 (minor)

- Merge pull request #32 from bradselph/development
- refactor(admin): consolidate 16 tabs into 10 with sub-tabs


## [0.28.0] - 2026-03-02 (minor)

- Merge pull request #31 from bradselph/development
- fix: 413 stream push, URL source exposure, mature content for guests


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