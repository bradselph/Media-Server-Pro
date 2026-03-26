# Changelog

## [0.115.0] - 2026-03-26 (minor)

- fix(frontend): explicit import useApi to break #imports circular TDZ
- fix(frontend): remove spurious body param from api.get delegation wrapper
- fix(frontend): defer useApi() call in useApiEndpoints to fix remaining TDZ
- fix(frontend): safelist blur-lg/blur-md/scale-110 in Tailwind v4 for mature content gate
- fix(frontend): replace navigateTo with window.location.replace in useApi to fix TDZ bundle error


## [0.114.0] - 2026-03-26 (minor)

- docs: update audit report — all critical/high/medium bugs resolved
- fix: correct new(time.Now()) and new(*T) pointer semantics across codebase
- fix(hls): correct new(time.Now()) — CompletedAt was always epoch
- fix(frontend): remove /upload/** proxy rule that collided with SPA /upload route during prerender
- fix(auth): log session LastActivity persist errors instead of silently discarding
- fix(hls): log stack trace on panic instead of swallowing it
- fix(api): preserve redirect URL on 401 session expiry
- fix(frontend): surface scanning and initializing states in media library
- fix(player): add mature content gate and clear stale suggestions on navigation
- fix(admin): return rejected_keys in config update response
- fix(auth): correct new() pointer semantics — LastLogin, LockedAt, user copy, HLS job copy
- feat(admin): add bulk selection and bulk actions to MediaTab
- feat(admin): add user sessions viewer to UsersTab
- fix(admin): correct ValidatorStats field names and HLS job status badge color type
- feat(frontend): add media upload page with drag-and-drop and file browser
- chore: merge development — mature content gate, ratings, media edit, HLS debounce
- feat(admin): add edit media modal to MediaTab
- feat(player): add star ratings and personalized recommendations
- fix(frontend): implement mature content gate on media library page
- Merge branch 'main' into development
- fix(frontend): initialize media browser limit from user items_per_page preference
- fix(player): debounce HLS availability check to prevent burst requests
- fix(frontend): initialize media browser limit from user items_per_page preference
- fix(player): debounce HLS availability check to prevent burst requests
- fix(db): use empty model struct in GORM Updates to prevent struct field merging
- fix(db): use empty model struct in GORM Updates to prevent struct field merging
- chore: merge development — Sources/Discovery tabs, duplicate UI, home recommendations
- feat(frontend): add continue watching and trending recommendation rows to home page
- feat(frontend): add duplicate detection and resolution UI to receiver tab
- chore: merge development into main, resolve VERSION conflict
- feat(frontend): add Discovery admin tab with categorizer, auto-discovery, suggestions, and HuggingFace classification UI
- feat(frontend): add Sources admin tab with remote sources, crawler, extractor, and receiver/slaves UI
- Merge branch 'development'
- fix(frontend): use computed ref for delete modal open state in playlists page


## [0.113.0] - 2026-03-25 (minor)

- chore: merge development — mature content gate, ratings, media edit, HLS debounce
- feat(admin): add edit media modal to MediaTab
- feat(player): add star ratings and personalized recommendations
- fix(frontend): implement mature content gate on media library page
- Merge branch 'main' into development
- fix(frontend): initialize media browser limit from user items_per_page preference
- fix(player): debounce HLS availability check to prevent burst requests
- fix(frontend): initialize media browser limit from user items_per_page preference
- fix(player): debounce HLS availability check to prevent burst requests
- fix(db): use empty model struct in GORM Updates to prevent struct field merging
- fix(db): use empty model struct in GORM Updates to prevent struct field merging
- chore: merge development — Sources/Discovery tabs, duplicate UI, home recommendations
- feat(frontend): add continue watching and trending recommendation rows to home page
- feat(frontend): add duplicate detection and resolution UI to receiver tab
- chore: merge development into main, resolve VERSION conflict
- feat(frontend): add Discovery admin tab with categorizer, auto-discovery, suggestions, and HuggingFace classification UI
- feat(frontend): add Sources admin tab with remote sources, crawler, extractor, and receiver/slaves UI
- Merge branch 'development'
- fix(frontend): use computed ref for delete modal open state in playlists page


## [0.113.0] - 2026-03-25 (minor)

- fix(frontend): initialize media browser limit from user items_per_page preference
- fix(player): debounce HLS availability check to prevent burst requests
- fix(db): use empty model struct in GORM Updates to prevent struct field merging
- chore: merge development — Sources/Discovery tabs, duplicate UI, home recommendations
- feat(frontend): add continue watching and trending recommendation rows to home page
- feat(frontend): add duplicate detection and resolution UI to receiver tab
- chore: merge development into main, resolve VERSION conflict
- feat(frontend): add Discovery admin tab with categorizer, auto-discovery, suggestions, and HuggingFace classification UI
- feat(frontend): add Sources admin tab with remote sources, crawler, extractor, and receiver/slaves UI
- Merge branch 'development'
- fix(frontend): use computed ref for delete modal open state in playlists page


## [0.113.0] - 2026-03-25 (minor)

- chore: merge development — Sources/Discovery tabs, duplicate UI, home recommendations
- feat(frontend): add continue watching and trending recommendation rows to home page
- feat(frontend): add duplicate detection and resolution UI to receiver tab
- chore: merge development into main, resolve VERSION conflict
- feat(frontend): add Discovery admin tab with categorizer, auto-discovery, suggestions, and HuggingFace classification UI
- feat(frontend): add Sources admin tab with remote sources, crawler, extractor, and receiver/slaves UI
- Merge branch 'development'
- fix(frontend): use computed ref for delete modal open state in playlists page


## [0.113.0] - 2026-03-25 (minor)

- feat(frontend): add Content admin tab with scanner review queue, HLS job management, and validator
- feat(frontend): user-facing playlists page with create/view/delete and Add to Playlist from player
- fix(frontend): add aria-label to downloader delete button
- fix(frontend): downloader created date — remove erroneous *1000 (field is already ms)
- chore: sync development with main


## [0.112.0] - 2026-03-25 (minor)

- fix(frontend): improve accessibility by adding aria-labels to additional buttons and enhancing touch targets


## [0.111.0] - 2026-03-25 (minor)

- fix(frontend): add aria-label to all icon-only buttons in admin tabs and profile page


## [0.110.0] - 2026-03-25 (minor)

- fix(frontend): add 20 as selectable items-per-page option
- fix(auth): serialize JSON fields in user Update() to fix preferences 500
- fix(frontend): accessibility — aria-labels, touch targets, thumbnail condition
- fix(frontend): add lang attribute and meta description to HTML head
- Merge branch 'main' into development
- chore(frontend): OpenAPI codegen in npm run check (synthesis loop 2)
- fix(frontend): mobile player uses viewport height below header
- chore(frontend): add npm run check for synthesis CI
- ci(synthesis): align workflow and smoke with Go, MySQL, and Nuxt
- Merge branch 'main' into development


## [0.109.0] - 2026-03-25 (minor)

- chore(contract): canonical OpenAPI spec under api_spec/


## [0.108.0] - 2026-03-25 (minor)

- Merge pull request #109 from bradselph/synthesis
- Merge branch 'main' into synthesis
- Refactor HLS configuration mapping and enhance JSON field setting for complex types
- Implement view cooldown mechanism to prevent inflated media view counts
- Add error handling component, enhance default layout, and update API types
- up
- update
- initial
- Create partition-frontend.mdc
- Create MIGRATION_FROM_PRIOR_AGENTS.md
- Create integration_smoke.py
- Create ADOPTION.md
- Create SCENARIOS.md
- Create check_completeness.py
- Create synthesis-ci.yml
- Create INSTALL_IN_YOUR_REPO.md
- Create TOOLING.md
- Create WORKFLOWS.md
- Create _discover_prior_agents.py
- Create partition-backend.mdc
- Update CLAUDE.md
- Create mutation-contract-extension.mdc
- Create agent-playbook.mdc
- fix(nuxt-ui): add watch history pagination to profile page
- fix(nuxt-ui): remove dead saveLocation field from downloader API call
- fix(nuxt-ui): correct profile page TypeScript cast and watch history filter
- fix(nuxt-ui): integrate useHLS composable into player with quality selector
- fix(nuxt-ui): add error handling to admin MediaTab action buttons
- fix(handlers): repair admin is_mature filter — new(expr) always returned *bool(false)
- fix: normalize API response shapes and fix bool pointer syntax
- fix(nuxt-ui): normalize filename-like title strings
- fix(nuxt-ui): centralize display-title resolution across pages
- fix(nuxt-ui): repair HLS URL resolution and profile history actions
- fix(nuxt-ui): bundle pagination chevrons for strict CSP


## [0.107.0] - 2026-03-24 (minor)

- Merge pull request #108 from bradselph/development
- Merge branch 'main' into development
- chore(migration): finalize remaining Nuxt + handler integration fixes


## [0.106.0] - 2026-03-24 (minor)

- Merge pull request #107 from bradselph/development
- fix(nuxt-ui): align UserPermissions, UserPreferences, WatchHistory types with Go
- fix(nuxt-ui): align API types for player, suggestions, admin tabs
- Merge branch '102'
- Merge pull request #106 from bradselph/development
- Merge branch 'main' into development
- Squashed commit of the following:
- fix(nuxt-ui): sync auth, preferences, and analytics types with API
- fix(nuxt-ui): align API client with Go routes and auth redirects
- fix(nuxt-ui): sync auth, preferences, and analytics types with API
- fix(nuxt-ui): align API client with Go routes and auth redirects
- Merge branch 'main' into development
- Delete package-lock.json
- fix(web): serve Nuxt assets under /web/static/react with legacy /_nuxt redirect
- fix(web): force Content-Type for embedded /_nuxt and /web/static assets
- fix(web+nuxt): align SPA baseURL with Go routes (index loads at /)
- fix(nuxt-ui/player): loop binding, no duplicate speed label, shortcut R
- fix(web): correct MIME types for embedded SPA assets (nosniff-safe)


## [1.0.0] - 2026-03-23 (major)

- Merge pull request #105 from bradselph/102
- fix(nuxt-ui): align API response types and fix admin tab overflow


## [0.105.0] - 2026-03-23 (minor)

- Merge branch '102'
- fix(nuxt-ui): include dynamic icons (chevron-down etc.) in client bundle
- fix(nuxt-ui): bundle icons client-side to avoid CSP connect-src violation
- fix(nuxt-ui): replace empty-string select values with sentinel, update CSP
- fix(web): use all:static embed to include _nuxt/ and _fonts/ directories
- fix(web): remove StripPrefix mismatch breaking /_nuxt/ and /_fonts/ asset serving
- fix(deploy): use static preset + nuxt generate for Go embed
- fix(nuxt-ui): render admin tab content inside UTabs #content slot
- fix(nuxt-ui): align @pinia/nuxt to ^0.10.0, add package-lock.json
- fix(nuxt-ui): SPA mode, route guards, canonical CSS classes
- feat(deploy): switch frontend build from React to Nuxt UI
- feat(nuxt-ui): full Nuxt UI v3 frontend + audit fixes, bump to v0.103.0
- fix(player): mobile chrome auto-hide and no stuck hover overlay
- feat(admin): load full user profile in edit modal via getUser
- feat(admin): categorize directory in Categorizer tab
- feat(admin): audit log viewer under System + user_id query param
- feat(admin): list login sessions in edit-user modal
- feat(admin): show active streams and uploads on dashboard
- refactor(admin): clarify media delete and thumbnail field names
- fix(admin): align playlists tab with server pagination and filters
- fix(frontend): sync downloader WS refs in effects (react-hooks/refs)
- fix(frontend): ContentReview TEXT_MUTED; drop stale playerHLS eslint-disable (audit 10/10)
- fix(frontend): DownloaderTab health metrics use typeof number (audit loop 9/10)
- fix(frontend): listMedia strict number checks for query params (audit loop 8/10)
- fix(a11y): DownloaderTab settings table header row (audit loop 7/10)
- fix(frontend): DownloaderTab dependency cell formatter (audit loop 6/10)
- fix(frontend): DownloaderTab progress color helper; audit rows 4–5 (loop 5/10)
- fix(frontend): Toast dismiss without extra nested callback (audit loop 4/10)
- fix(frontend): useDownloaderWebSocket reconnect ref + flatter handlers (audit loop 3/10)
- fix(frontend): drop redundant ThemeId type alias (audit loop 2/10)
- fix(security): exempt /web/static/ from rate limit (audit loop 1/10)
- chore(gitignore): ignore web/nuxt-ui local Nuxt sandbox
- chore: baseline commit before audit-fix loop


## [0.104.0] - 2026-03-20 (minor)

- Merge pull request #103 from bradselph/development
- chore(deploy): quieter git/npm, robust SERVER_PORT, align UFW default
- fix(deploy): force VPS branch to match origin (clean package-lock drift)
- fix(deploy): clean node_modules before npm ci to avoid ENOTEMPTY
- refactor: update frontend structure and deployment process
- fix(todo): implement error response for receiver thumbnail placeholders
- fix: resolve medium severity audit issues + build Nuxt UI pages, bump to v0.105.0
- feat: scaffold Nuxt UI v3 frontend project
- fix: resolve 20 critical and high severity audit findings, bump to v0.104.0


## [0.103.0] - 2026-03-18 (minor)

- Merge pull request #102 from bradselph/development
- Delete audit-report-2026-03-18.md
- chore: update dependencies, add theme engine, and apply Go new() refactor
- feat: add background HLS pre-generation and fix startup job resume
- test: add comprehensive unit tests for 22 previously untested packages
- Delete audit-report-2026-03-14.md
- Delete audit-report-2026-03-15.md
- chore: update dependencies and improve build process
- chore: synchronize with main branch
- Merge branch 'main' into development
- Merge branch 'main' into development
- feat: add backup ID validation and improve error handling
- test: enhance age gate and CORS tests for security and configuration clarity
- feat: enhance security and configuration management
- fix: improve handling of optional values and enhance accessibility in various components
- feat: enhance accessibility and UI in various components
- refactor: enhance downloader import functionality and API responses
- fix: handle optional supported sites in DownloaderTab component
- refactor: update downloader API responses and frontend types


## [0.102.0] - 2026-03-15 (minor)

- Merge pull request #101 from bradselph/devin/1773599958-audit-improvements
- audit: fix P0-6, P1-9, P1-36, P2-48, P3-5 security and correctness issues


## [0.101.0] - 2026-03-15 (minor)

- Merge pull request #100 from bradselph/development
- feat: add downloader integration with configuration and API endpoints
- Audit: P2-52 video src after HLS check; P2-50 export via fetchBlob/ApiError
- audit: fix P2-53, P2-33, P2-51; update report
- audit: fix P2-54, P2-47, P1-36 (user); update report
- audit: fix P1-20, P2-44; update report
- audit: fix P1-33, P1-36 (playlist), P2-43; update report
- audit: fix P1-34, P1-35, P1-40, P1-47, P1-31, P2-34, P2-56; update report
- fix: enhance validation and security measures across components
- fix: enhance security and validation in various components
- Update audit-report-2026-03-14.md


## [0.100.0] - 2026-03-15 (minor)

- Merge pull request #99 from bradselph/development
- fix: resolve multiple issues in thumbnail generation, path validation, and process management
- fix: resolve multiple issues related to session management, configuration updates, and symlink handling
- fix(security): address multiple security vulnerabilities and enhance route protection
- feat(security): enhance streaming security with unauthenticated access controls
- feat(api): implement read-only transactions for query execution and enforce API key validation for WebSocket
- Update audit-report-2026-03-14.md
- feat(tests): enhance test user creation and update RequireAuth tests
- feat(tests): update ErrorBoundary tests and refine authStore mock data
- refactor(api): enhance apiRequest tests and add convenience methods
- Merge branch 'main' into development
- audit fixes batch 6: P3-6, P3-2, P2-7, P1-10


## [0.99.0] - 2026-03-14 (minor)

- Merge pull request #98 from bradselph/development
- Merge branch 'main' into development
- audit fixes batch 5: P2-23, P0-5, P1-7, P2-5
- audit fixes batch 4: P3-13, P1-13, P2-26
- audit fixes batch 3: P1-5, P2-4
- audit fixes batch 2: P0-8, P1-1, P1-12, P2-6
- audit fixes batch 1: P0-10, P1-2/3/4/6/8/11
- fix(api): improve JSON parsing error handling and enhance media loading feedback
- refactor(scheduler): reduce default startup delay for task execution
- feat(player): enhance HLS handling and fallback mechanism for video sources
- fix(analytics): enhance CSV export error handling and ensure proper file cleanup


## [0.98.0] - 2026-03-14 (minor)

- Merge pull request #97 from bradselph/development
- fix(media-receiver): improve error handling for range requests and ensure full file delivery on failure
- fix(errors): eliminate silent json.Unmarshal failure in hls_job_repository
- chore(verbose-audit-loop): cycle 2/10
- chore(verbose-audit-loop): cycle 2/10
- fix(validation): fix cross-platform path tests for Windows compatibility
- fix(errors): check scanner.Err() in media-receiver loadEnvFile to surface I/O failures
- fix(edge-cases): harden copyZipEntryToFile — close file before cleanup on oversize/error
- fix(feature): complete execution path for analytics CSV export cleanup
- fix(lint): remove unused BackupInfo struct from models
- fix(feature): complete execution path for writeBackupArchive — propagate zipFile.Close errors
- fix(lint): remove unused shouldValidateFile function (U1000)
- fix(player-settings): ensure bandwidth check is type-safe by validating as a number
- fix(todo): add missing config validation for extractor and crawler sections
- chore(verbose-audit-loop): cycle 1/10
- chore(verbose-audit-loop): cycle 1/10
- chore(verbose-audit-loop): cycle 1/10
- fix(errors): eliminate silent json.Unmarshal failures in suggestion_profile_repository
- fix(contract): reconcile interface mismatch in NewHandler critical dependency validation
- fix(edge-cases): harden WebSocket slave registration to use authoritative node ID
- fix(edge-cases): harden extractor proxyStream with zero-timeout fallback
- fix(feature): complete execution path for HLS cleanup by deleting orphaned DB records
- fix(feature): complete execution path for media ID resolution with consistent trimming
- fix(lint): extract validUsername to reduce AdminCreateUser cyclomatic complexity
- fix(lint): check w.Write return values in servePlaylist
- Merge branch 'main' into development
- refactor(preview): handle media duration error gracefully
- fix(todo): propagate DB error in AddToWatchHistory update path
- chore(new-audit-loop): cycle 1/10
- fix(errors): log silent GetUser failure in StreamMedia stream limit check
- fix(contract): remove dead handleHealth/GetHealth from server (replaced by api/handlers)
- fix(edge-cases): harden CleanOldBackups negative keepCount and GetBackupStats ordering
- fix(feature): complete execution path for DownloadMedia IsReady check
- fix(lint): remove unused slave variable assignment in receiver proxy


## [0.97.0] - 2026-03-14 (minor)

- Merge pull request #96 from bradselph/development
- Merge branch 'main' into development
- fix(lint): improve variable naming consistency in UpdatesTab by renaming TEXT_MUTED constant
- fix(lint): extract duplicated var(--text-muted) literal into TEXT_MUTED constant in UpdatesTab
- fix(todo): reject update when SHA256SUMS download/parse fails instead of silently skipping
- chore(audit-loop): cycle 6/25
- fix(errors): propagate item deletion errors in duplicates applyRemoveResolution instead of silent log-and-continue
- fix(contract): enforce pending-status check in IgnoreDiscovery to match ApproveDiscovery contract
- fix(edge-cases): guard against short fingerprint panic in tryRecordLocalPair
- fix(feature): use RLock in crawler GetStats and log query errors instead of silencing them
- fix(lint): use strict inequality in PlayerSettingsPanel bandwidth check
- fix(todo): propagate context through crawler crawl pipeline for cancellation support
- fix(PlayerSettingsPanel): narrow bandwidth type for formatBitrate call
- chore(audit-loop): cycle 5/25
- fix(errors): log ignored DB errors in duplicates CountPending and isResolvedRemovalCached
- fix(contract): validate email format in AdminUpdateUser to match AdminCreateUser contract
- fix(edge-cases): harden extractAllFiles to report partial restore failures instead of silent success
- fix(feature): complete execution path for RestoreBackup/DeleteBackup — validate backupID against path traversal
- fix(lint): extract duplicated var(--text-muted) literal into TEXT_MUTED constant in AnalyticsTab
- fix(todo): add self-action and last-admin protection to AdminBulkUsers
- chore(audit-loop): cycle 4/25
- fix(errors): remove silent Flush failure and orphaned file in analytics ExportCSV
- fix(contract): validate BaseURL scheme and host in RegisterSlave before use in proxyViaHTTP
- fix(edge-cases): prevent panic in sanitizeFilename when extension exceeds 255 chars
- fix(feature): complete execution path for ExportAuditLog — clean up orphaned files on error, check Flush
- fix(lint): extract duplicated var(--text-muted) literal into TEXT_MUTED constant in SourcesTab
- chore(audit-loop): cycle 3/25
- fix(errors): propagate file removal error in admin DeleteBackup instead of silent log
- fix(contract): validate email format in AdminCreateUser to match Register handler
- fix(edge-cases): validate URL format in CreateRemoteSource and require URL in CacheRemoteMedia
- fix(feature): enforce admin permissions after applyPermissionsFromMap in UpdateUser
- fix(lint): resolve react-hooks/exhaustive-deps warnings in IndexPage mini-player
- fix(todo): enforce http/https scheme in remote module validateURL


## [0.96.0] - 2026-03-14 (minor)

- Merge pull request #95 from bradselph/development
- Merge branch 'main' into development
- chore(audit-loop): cycle 2/25
- fix(errors): propagate proxy copy error in extractor proxyStream
- fix(contract): pass userID instead of username to GetUserStorageUsed
- fix(edge-cases): set WebSocket read limit to prevent memory exhaustion
- fix(feature): guard writeError after partial stream/download writes
- fix(lint): resolve eqeqeq warnings in PlayerSettingsPanel and IndexPage
- fix(todo): persist validator FixFile status to database
- chore(audit-loop): cycle 1/25
- fix(errors): eliminate silent failure in GetServerLogs ReadDir error path
- fix(edge-cases): harden readLastNLines with ring buffer and scanner limits
- fix(feature): complete execution path for DeleteMedia version tracking
- fix(lint): resolve sonarjs/no-nested-functions in useHLS hook
- fix(todo): return error for unsupported bulk action in processOneBulkMediaItem


## [0.95.0] - 2026-03-14 (minor)

- Merge pull request #94 from bradselph/development
- audit(frontend): SourcesTab nested ternaries -> if/else and split conditionals (lint)
- audit(frontend): UpdatesTab nested ternaries -> helper functions (lint)
- Merge branch 'main' into development
- fix(frontend): resolve TS build errors (TEXT_MUTED, bandwidth, versionData)
- audit(frontend): fix redundant assignment and StreamingTab nested ternaries (lint)
- audit(frontend): PlaylistsTab nested ternary; update AUDIT-REMAINING with loop summary
- audit(frontend): HuggingFaceTab duplicate string constant and nested ternary (lint)
- audit(frontend): fix UsersTab void operator and nested ternary (lint)
- audit(cleanup): remove stray admin_media.go.bak; update AUDIT-REMAINING
- audit(config,docs): add CONFIG-NOTES.md and AUDIT-REMAINING.md; config.json _TODO cleanup local (file gitignored)
- audit(docs): note script and config audit in TODO-COUNT.md
- audit(frontend): fix eqeqeq lint (use !== instead of !=)
- audit(go): go fmt
- audit(deploy,go.mod): remove remaining TODO in deploy setup_ssh_auth and go.mod
- audit(docs): update TODO-COUNT.md to reflect audit completion
- audit(package): remove _TODO fields from package.json and root package-lock.json
- audit(scripts,systemd): correct TODOs in pre-push-check and systemd units
- audit(git): correct .gitignore/.gitattributes TODOs; fix codacy.mdc path to forward slash
- audit(setup): correct TODOs to short comments or remove
- audit(deploy): correct TODOs to short comments or remove
- audit(go,golangci,tsconfig): remove TODO comments from go.mod, .golangci.yml, tsconfig
- audit(frontend): correct TODOs in types, hooks, components, pages, client, index, vite.config
- audit(cmd/media-receiver): correct TODOs to doc comments
- audit(cmd/server): correct main.go TODOs to doc comments or remove
- audit(server,crawler): correct handleHealth/GetHealth and browser probe TODOs to doc comments
- audit(thumbnails,admin,scanner,repositories): correct TODOs to doc comments
- audit(suggestions,analytics,validator,remote,upload): correct TODOs to doc comments
- audit(categorizer,crawler,duplicates): correct TODOs to doc comments
- audit(media): correct discovery TODOs to doc comments (Stop, dedup, saveMetadata, IncrementViews, ClearAllPlaybackPositions)
- audit(routes): remove remaining WebSocket TODO comment
- audit(handlers): shorten admin_config, system, lifecycle, analytics TODO comments
- audit(handlers): shorten HLS and media TODO comments to doc-only
- audit(handlers): shorten admin_logs and admin_backups TODO comments
- audit(config): shorten save/loadEnvFile TODO comments to doc-only
- audit(security,models,helpers): shorten TODO comments to doc-only notes
- audit(updater): guard empty backup path; fix source up-to-date check before checkout
- audit(routes): replace TODOs with concise route auth comments
- audit(backup): document data/full backup types as config-only; remove TODO
- audit(streaming): RFC 7233 range response, short-write loop, Stop() session log
- fix(updater): align binary asset names with release workflow; fix source build race
- audit(streaming): document StreamStats reset-on-restart behavior
- audit(admin): use inputBaseStyle for SecurityTab whitelist/blacklist/ban inputs
- audit(pkg/helpers): systemd build constraint !windows + Windows no-op stub
- audit(cmd): add parentheses to thumbnail needsGen expression for clarity
- audit(handlers): atomic storage_used increment via AddStorageUsed/IncrementStorageUsed
- audit(upload): document HandleUpload w usage
- audit(validator): FixFile re-fetch result under write lock before modifying


## [0.94.0] - 2026-03-14 (minor)

- Merge pull request #93 from bradselph/development
- Merge branch 'main' into development
- audit(suggestions+categorizer): saveProfiles continue on error; clarify detectMovie parseNumber
- audit(middleware): ETag skip for responses >1MB to avoid buffering large bodies
- audit(config+db): syncFeatureToggles bidirectional; UserRepository.List batch-load perms/prefs
- audit(frontend): sync theme from server including 'auto' on profile load and save
- audit(analytics): AvgWatchDuration over playback-event count, not TotalViews
- audit(scanner+thumbnails): convertScannerToRepo under lock; audio thumbnail size stats
- audit(playlists): collapse redundant filter helpers; return error on copy AddItem failure
- audit(admin): cleanup temp CSV after audit log export send
- audit(remote+extractor): SHA-256 generateID; fetchURL with context for cancellation
- audit(receiver): snapshot slave record under lock in Heartbeat; clarify stream-push TODO
- audit(streaming+HLS): range 206, short-write handling, Stop log, tryResolveExistingJob lock, force_key_frames
- audit(media): fingerprintIndex and dedup fixes
- audit(auth): clearer error on admin preferences when user record create fails; login/signup error a11y
- fix: receiver/updater/wsconn TODOs and index page audit
- fix: address TODOs — security, bugs, perf, cleanup
- refactor: enhance security and performance in media handling
- refactor(admin): update playlist query to return items directly
- refactor: streamline query parameter handling and static file serving
- refactor(admin): improve query parameter handling and tag parsing
- fix(admin): avoid config text overwrite from query side effect
- fix: address TODOs - tag loading, backup sort, dead code
- Merge branch 'main' into development
- config: resolve CustomStatic path in resolveAbsolutePaths


## [0.93.0] - 2026-03-14 (minor)

- Merge pull request #92 from bradselph/split-development
- feat(admin): add visibility filter and owner search for playlists
- docs: update ListMediaPaginated comment, remove redundant re-sort
- fix(admin): add Tags filter at DB level for admin media pagination
- fix(admin): media pagination and playlists API
- docs: document custom static overlay and CUSTOM_STATIC_DIR in CLAUDE.md
- config: resolve CustomStatic path in resolveAbsolutePaths
- feat: implement custom static file serving with optional overrides
- refactor: remove unused ReceiverProxyStream handler and clean up streaming error handling


## [0.92.0] - 2026-03-13 (minor)

- Merge pull request #91 from bradselph/development
- Merge branch 'main' into development
- feat: add public version endpoint and display in footer
- fix: ensure user preferences update does not desynchronize cache and database
- fix: resolve timestamp collision in backup creation and enhance cleanup logic
- feat: enhance security and session management


## [0.91.0] - 2026-03-13 (minor)

- Merge pull request #90 from bradselph/development
- refactor: improve mobile responsiveness and UI elements across frontend


## [0.90.0] - 2026-03-13 (minor)

- Merge pull request #89 from bradselph/development
- Merge branch 'main' into development
- Update sanitize.go
- Update server.go
- fix(backup): use app version in manifest via CreateBackupOptions.Version (handler passes buildInfo)
- fix(admin): log ReadDir errors in scanBackups for operator diagnostics
- Merge pull request #88 from bradselph/claude/fix-workflow-cycle-PDVd3
- fix: break workflow cycle by using workflow_run trigger and path guards
- Merge pull request #87 from bradselph/development


## [0.89.0] - 2026-03-13 (minor)

- Update sync-development.yml
- Update dev-version.yml
- Update release-version.yml
- Update ci.yml
- docs: add TODO count audit (approx. 140 remaining)
- refactor(hls): rename SaveJobsToFile to SaveJobs (jobs are persisted to DB, not file)
- fix(tasks): start runTaskLoop when RegisterTask is called after Start()
- fix(hls): compute cache size outside jobsMu to avoid blocking job mutations during Walk
- fix(repositories): propagate perms/prefs Get errors in user repo (GetByUsername, GetByID, List)
- chore(web): remove unused thumbnails param from RegisterStaticRoutes; shorten spaRoutes TODO
- fix(tasks): track RunNow goroutine in wg so Stop() waits for it
- chore(admin): make last_check explicit in health response (remove omitempty)
- fix(hls): serve playlist with explicit headers when cdnBase empty; remove stale path-traversal TODO
- chore(middleware): remove unused net/http AgeGate handlers (only Gin handlers are registered)


## [0.88.0] - 2026-03-13 (minor)

- Merge branch 'main' into development
- perf(web): cache SPA index.html on first request and set Cache-Control: no-cache
- chore(api): remove stale TODOs, use typed response for CreateRemoteSource (omitempty username)


## [0.87.0] - 2026-03-13 (minor)

- Merge pull request #86 from bradselph/development
- fix(models): truncateString by rune count to avoid splitting UTF-8 characters
- fix(analytics): add default LIMIT to GetByMediaID and GetByUserID to prevent unbounded queries
- Update ci.yml
- fix(extractor): remove redundant in-memory add in AddItem (item already added under lock at line 234)
- Merge pull request #85 from bradselph/development
- Update ci.yml


## [0.86.0] - 2026-03-13 (minor)

- Merge pull request #84 from bradselph/development
- Merge branch 'main' into development
- fix(ci): update CI workflows to improve efficiency and versioning
- fix(config): skip redundant filepath.Abs in CreateDirectories when path is already absolute
- fix(database): make connectWithRetry respect context cancellation
- fix(admin): cap audit log export at 100k rows to avoid OOM
- fix(backup): check database IsConnected before using GORM in Start
- fix(auth): delete user evicts admin sessions and removes sessions from DB


## [0.85.0] - 2026-03-13 (minor)

- Merge pull request #83 from bradselph/development
- feat(auth): add AllowRegistration config; Register returns 403 when disabled
- fix(auth): delete expired admin session from repository in ValidateAdminSession
- fix(security): GetWhitelist and GetBlacklist return copies to prevent mutation
- fix(database): set m.db and m.sqlDB to nil after Stop so GORM() returns nil
- fix(remote): always close syncDone in Stop so waiters do not hang when syncTicker was nil
- fix(config): deep-copy Receiver.APIKeys in getCopy to prevent mutation of internal state
- fix(repo): escape LIKE wildcards in receiver_transfer_repository Search
- fix(auth): reset login attempt when lockout expires so next failure does not re-lock immediately
- fix(repo): escape LIKE wildcards (% and _) in media_metadata_repository search
- fix(handlers): requireReceiverAPIKey accepts api_key query param as well as X-API-Key header
- fix(handlers): verify absolute paths are under allowedDirs in resolvePathToAbsoluteNoWrite
- fix(extractor): RemoveItem returns ErrNotFound when item does not exist; handler returns 404
- fix(streaming): sanitize Content-Disposition filename to prevent header injection
- fix(categorizer): cap animeScore at 1.0
- fix(upload): reject backslash in filenames to prevent Windows path traversal
- fix(streaming): respond with 416 and Content-Range when parseRange fails in Stream
- fix(analytics): return copies from GetDailyStats and GetMediaStats to prevent mutation
- docs(receiver): clarify CheckOrigin and API key as access control
- fix(hls): validate Quality and Segment in ServeSegment to prevent path traversal
- fix(handlers): use req.RangeHeader instead of re-reading Range in StreamMedia
- fix(analytics): countActiveSessions acquires sessionsMu internally; adjust callers
- fix(hls): skip cleanup when RetentionMinutes <= 0 to avoid deleting all segments
- fix(repo): extractor Upsert and UpdateStatus include updated_at
- fix(thumbnails): use PreviewCount when req.Count <= 0, remove dead code
- fix(repo): propagate json.Marshal error in HLS job Save
- fix(repo): crawler Upsert includes updated_at in DoUpdates
- fix(repo): propagate json.Marshal error in autodiscovery Save
- fix(hls): recognize .aac and .m4s/.mp4 segment types in validation
- fix(handlers): filterLogEntries allocates new slice instead of reusing backing array
- fix(repo): firstByUserID returns (nil, nil) for ErrRecordNotFound
- fix(repo): UpdateBlurHash returns error when path not found (RowsAffected 0)
- fix(frontend): handle null items in DuplicatesTab itemCard function
- chore: remove outdated TODO in AdminExportAnalytics (date validation already returns 400)
- fix(middleware): prevent unbounded goroutines in age gate eviction
- fix(repo): playlist Get returns ErrPlaylistNotFound instead of gorm.ErrRecordNotFound
- fix(handlers): validate paths for ApplyDiscoverySuggestion and DismissDiscoverySuggestion
- fix(handlers): validate path for ScanContent before scanning
- fix(handlers): avoid leaking internal error details in extractor API
- fix(handlers): validate email with net/mail.ParseAddress in Register
- fix(frontend): toggleTheme cycles through dark, light, and auto
- fix(config): unify GetValue and SetValue field-matching logic
- fix(handlers): add nil check for thumbnails in ListMedia and GetMedia
- fix(repo): ip_list SetEnabled returns error when config row not found
- fix(middleware): add Access-Control-Allow-Credentials for credentialed CORS
- fix(middleware): only trust X-Forwarded-Proto from trusted proxies


## [0.84.0] - 2026-03-13 (minor)

- Merge pull request #82 from bradselph/development
- Fix API contract mismatches across backend handlers and frontend types
- Address API contract mismatches in remote source handling and system info responses
- Delete Repository-API-Structure-Report.md
- Fix bugs identified in code review: SPA routing, Stop panic, timing side-channel, duplicate formatter
- Fix analytics CSV export by ensuring the analytics directory is created if it doesn't exist
- Refactor and clean up code across multiple files
- Update default base URL in Hugging Face client to reflect new inference provider path
- Update Hugging Face client to use new router endpoint and improve error logging
- Refactor Hugging Face client to use ImageData type for image classification
- Update client.go
- Update client.go
- Update validate.go
- Update defaults.go
- Update Hugging Face client documentation and default base URL for Inference API
- Update defaults.go
- Update client.go
- Merge branch 'main' into development
- Update default base URL for Hugging Face client to new router endpoint
- Fix HuggingFace pipeline: race conditions, resource leak, and redundant classification
- Refactor MatureScanner to improve thread safety for Hugging Face client
- Implement path validation for categorization and media handling
- Enhance module validation and configuration checks
- Enhance classification functionality and add new endpoints
- Fix HLS module concurrency limit and enhance rate limiter cleanup safety
- Refactor admin handlers and improve error handling
- Refactor playlist and media management functions for improved validation and error handling
- Enhance error handling and validation across admin handlers
- Add TODO annotations for config, build, and deployment files
- Add TODO annotations for frontend React/TypeScript code
- Add TODO annotations for pkg/, repositories, and web/server.go
- Add TODO annotations for internal/ modules (batch 3)
- Add TODO annotations for internal/ modules (batch 2)
- Add TODO annotations for internal/ modules (batch 1)
- Add TODO annotations for api/ handlers and routes
- Add TODO annotations for cmd/ entry points
- Remove redundant await in test assertion
- Use strict equality operators in frontend code
- Remove pointless double negation on is_mature
- Simplify trivial if statements and remove redundant variable
- Use consistent pointer receivers on jobHeap methods
- Remove redundant else after return in crawler
- Remove unused constants from middleware package
- Prefix unused function parameters with underscore
- Remove redundant type conversion in upload.go
- Rename variable 'copy' to avoid shadowing Go builtin
- Lowercase error strings per Go conventions
- Use errors.Is for error comparisons instead of == operator
- Use errors.As instead of type assertions on errors
- Fix resource leaks in backup.go
- Update main.go
- Delete duplicate-code-consolidation.md


## [0.83.0] - 2026-03-12 (minor)

- Merge pull request #81 from bradselph/development
- Refactor admin handlers for improved modularity and clarity
- Refactor admin media handling and enhance receiver API security
- Update analysis scripts and improve report generation
- Refactor playlist handling and enhance upload functionality
- Refactor suggestion handling in API to improve limit parsing and response structure
- Enhance media resolution logic and update API endpoints
- Implement preview hover functionality and refactor thumbnail handling in MediaCard component
- Refactor LoginPage component for improved readability and functionality
- Enhance ESLint configuration and refactor player keyboard hook
- Refactor usePlayerPageState.ts to improve media query handling and HLS checks
- Update usePlayerPageState.ts
- Update usePlayerPageState.ts
- Update playerKeyboard.ts
- Add MediaCardThumbnailBlock and MediaCardMatureOverlay components for enhanced media display
- Refactor formatting utilities to use value objects for improved type safety
- Update TypeScript configuration for improved build performance and compatibility
- Fix bandwidth null check in PlayerSettingsPanel and update error handling type in useHLS hook
- Enhance project structure and linting capabilities
- Refactor request handling in admin handlers for improved clarity and maintainability
- Refactor admin handlers for improved request handling and configuration mapping
- Refactor admin classification handlers to improve path resolution and logging
- Admin: Hugging Face tab — view status, run classification, edit settings
- feat: integrate Hugging Face visual classification support


## [0.82.0] - 2026-03-11 (minor)

- Merge pull request #80 from bradselph/development
- Merge branch 'main' into development
- Enhance CI workflows with concurrency and path-ignore features
- Merge pull request #79 from bradselph/development
- Delete DATABASE_TABLE_AUDIT.md
- Update .gitignore
- fix: resolve ESLint and npm audit issues for pre-push CI
- refactor(categorizer): introduce PathContext for improved file categorization
- Merge pull request #78 from bradselph/development
- refactor(backup): introduce structured options for backup creation and enhance error handling
- refactor(analytics): restructure event tracking and session management
- refactor(scheduler): enhance task registration and execution handling
- refactor(upload): enhance upload handling with structured types and improved processing
- refactor(validator): streamline validation logic and enhance caching mechanisms
- refactor(userPreferences): enhance JSON handling and validation for user preferences
- chore(dependencies): update Go version and clean up go.sum
- refactor(server): improve server initialization and configuration management
- Merge branch 'main' into development
- refactor(thumbnails): standardize thumbnail generation with structured requests
- refactor(thumbnails): introduce structured types for thumbnail generation
- Merge pull request #77 from bradselph/development
- Merge branch 'main' into development
- refactor(duplicates and hls): enhance duplicate detection and playlist generation
- refactor(database): improve database connection handling and schema management
- refactor(autodiscovery): introduce domain types for clarity and maintainability
- Merge pull request #76 from bradselph/development
- refactor(auth and hls): streamline user creation and session management
- refactor(admin logging): update LogAction method to use AuditLogParams struct
- fix(auth): improve session deletion error handling
- refactor(playlists): simplify playlist item removal logic
- refactor(admin tabs): streamline admin page tabs and enhance component structure
- refactor(mature content): enhance handling of mature content visibility in media components
- refactor(thumbnails): enhance thumbnail URL handling for mature content
- refactor(pagination): normalize pagination limits and improve default limit handling
- refactor(suggestions): enhance suggestion retrieval with mature content filtering
- Merge pull request #75 from bradselph/development
- Merge branch 'main' into development
- refactor(media): streamline media grid and enhance user experience
- refactor(audio): update useEqualizer hook to accept audioRef and readiness state
- refactor(thumbnails): improve thumbnail state synchronization in IndexPage
- update go
- Update IndexPage.tsx
- chore(dependencies): update indirect dependencies in go.mod and go.sum
- Merge pull request #74 from bradselph/development
- feat(virtualization): implement virtualized media grid for improved performance
- refactor(thumbnails): enhance thumbnail generation with priority queue
- feat(thumbnails): implement responsive thumbnail sizes for improved media loading
- feat(thumbnails): add batch thumbnail retrieval endpoint
- feat(thumbnails): add WebP support and BlurHash generation for thumbnails
- Merge pull request #73 from bradselph/development
- fix(admin): defensive null check for watch time, type updates for sources/extractor
- feat(audit): enhance audit log functionality and add user-specific event retrieval
- add todos
- feat(config): enable incremental builds in TypeScript configuration
- Merge pull request #72 from bradselph/development
- fix(stats): handle potential null values in analytics and dashboard components
- feat(admin): expand Analytics and Dashboard stats
- fix(media): improve category assignment fallback mechanism
- feat(media): enhance category assignment logic for media items
- feat(config): unify feature flags into single source of truth; improve suggestions resilience
- chore(cleanup): remove dead ReceiverProxyStream handler and stale scan_metadata field
- Create Repository-API-Structure-Report.md
- Create Repository-API-Structure-Report.md
- refactor(media): replace suggestions seeding poll loop with callback-based approach


## [0.65.0] - 2026-03-07 (minor)

- Merge pull request #68 from bradselph/development
- fix(security): harden P0/P1 vulnerabilities from architectural review


## [0.64.0] - 2026-03-07 (minor)

- Merge pull request #67 from bradselph/development
- fix(setup): align env vars with config loader, fix extractor cache and autodiscovery persistence
- fix(modules): edge case fixes across extractor, upload, autodiscovery, validator
- fix(duplicates): clean up orphaned records on slave unregister and catalog replace
- fix(duplicates): cascade resolution and suppress re-detection edge cases
- fix(handlers): prevent nil dereference in AdminResolveDuplicate
- fix(duplicates): prevent resolved pairs from reappearing after removal
- fix(restart): fix server restart failing to come back after shutdown


## [0.63.0] - 2026-03-07 (minor)

- Merge pull request #66 from bradselph/development
- fix(admin): fix broken CSS in extractor/crawler tabs and duplicate analytics stat


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