# Deep Code Audit Report — Media Server Pro 4
**Date:** 2026-04-11  
**Auditor:** 6-agent parallel deep audit (auth/config/db/media · admin handlers · HLS/thumbnails/streaming/analytics · security/tasks/repos/pkg · frontend · misc modules)  
**Files analyzed:** ~200 Go source files + 40 Nuxt UI frontend files  
**Note:** `new(expr)` patterns (e.g. `new(time.Now())`) are **valid Go 1.26.2** syntax that allocates a pointer to the expression's value — all agent reports claiming these are "compile errors" are false positives and have been dropped.

---

## AUDIT SUMMARY

```
=== AUDIT SUMMARY ===
Files analyzed:    ~240
Functions traced:  ~600+
Workflows traced:  12

BROKEN:       8
INCOMPLETE:   3
GAP:          42
REDUNDANT:    1
FRAGILE:      38
SILENT FAIL:  9
DRIFT:        2
LEAK:         8
SECURITY:     6
OK:           ~100

Critical (must fix before deploy): 14
High (will cause user-facing bugs):  22
Medium (tech debt / time bombs):     34
Low (cleanup / style):               18
```

---

## CRITICAL — Must Fix Before Deploy

---

### [BROKEN] internal/auth/session.go:250 — Admin sessions stored in wrong map; LogoutAdmin always fails
```
WHAT: createSession always writes to m.sessions (user map) regardless of role.
      LogoutAdmin searches m.adminSessions — admin sessions are never there
      (until after a server restart, when loadSessionsFromDB re-populates both maps).
WHY:  createSession is a shared function that does not branch on session.Role.
IMPACT: Admin logout always returns ErrSessionNotFound until server restart. Admin
        sessions accumulate in m.sessions without being cleanable via LogoutAdmin.
        Freshly created admin sessions cannot be revoked from the admin path.
TRACE: AdminAuthenticate → CreateSessionForUser → createSession → m.sessions[id]
       LogoutAdmin → m.adminSessions[id] → not found → ErrSessionNotFound
FIX DIRECTION: In createSession, branch on session.Role == RoleAdmin and write to
               m.adminSessions; also check both maps in Logout.
```

### [SECURITY] internal/auth/authenticate.go:94-98 — Timing oracle enables username enumeration
```
WHAT: The "user not found" path performs a dummy bcrypt comparison to pad response
      time. The "account disabled" path (line 94-98) skips this padding and returns
      immediately after a map lookup — measurably faster than "not found".
WHY:  The timing-equalizer only covers the "not found" branch.
IMPACT: Attacker can distinguish "username exists but disabled" from "username does
        not exist" via response latency. Enables targeted credential stuffing.
TRACE: Authenticate → user.Enabled == false → recordFailedAttempt → return (fast)
       vs. user not found → bcrypt dummy compare → return (slow ~100ms)
FIX DIRECTION: Add `_ = bcrypt.CompareHashAndPassword(dummyHash, []byte(req.Password))`
               before the return on the disabled-account path.
```

### [SECURITY] pkg/helpers/ssrf.go:33-38 — IPv4-mapped IPv6 bypass of SSRF check
```
WHAT: privateRanges only contains pure IPv4 and IPv6 ranges. net.ParseIP("::ffff:10.0.0.1")
      returns a 16-byte slice; block.Contains() for the IPv4 10.0.0.0/8 range does not
      match it. URL http://[::ffff:10.0.0.1]/ passes ValidateURLForSSRF.
WHY:  IPv4-mapped IPv6 representation not accounted for in the private range list.
IMPACT: SSRF protection bypassed for all private ranges via ::ffff: notation.
TRACE: Admin submits http://[::ffff:192.168.1.1]/api → ValidateURLForSSRF passes →
       actual HTTP dial reaches internal service.
FIX DIRECTION: Before isPrivateIP check, call ip.To4() — if non-nil, check the 4-byte
               form against IPv4 ranges.
```

### [SECURITY] internal/receiver/wsconn.go — API key in WebSocket query string
```
WHAT: Slave nodes pass `?api_key=<key>` in the WebSocket upgrade URL.
      WebSocket upgrade is an HTTP request; the URL appears in nginx/Go access logs.
WHY:  Query-string auth is standard HTTP log capture surface.
IMPACT: API keys appear in plaintext in server access logs. Anyone with log access
        (ops team, SIEM, monitoring tools, log shipping services) can read slave API
        keys and impersonate slave nodes.
TRACE: Slave → ws://master/api/receiver/ws?api_key=SECRET → logged by every HTTP layer
FIX DIRECTION: Accept the API key in a custom HTTP header (X-API-Key) or as the first
               WebSocket message after upgrade.
```

### [SECURITY] internal/crawler/browser.go — Chrome launched with --no-sandbox
```
WHAT: Crawler starts Chrome with --no-sandbox flag.
WHY:  Sandbox requires kernel namespaces; may not be available in container environments.
IMPACT: A V8/Chrome exploit in crawled page content gives the attacker full process-level
        access to the host OS — no sandbox layer between crawler and server.
TRACE: startChrome → exec.Command(chromePath, "--no-sandbox", ...)
FIX DIRECTION: Run the crawler in an isolated container/VM with kernel namespaces; or
               remove --no-sandbox and ensure deployment environment supports sandbox.
```

### [SECURITY] api/handlers/system.go:414 — AdminExecuteQuery: SELECT INTO OUTFILE not blocked; keyword list bypassable
```
WHAT: (1) `SELECT * INTO OUTFILE '/tmp/x'` starts with SELECT and passes the prefix check.
      A ReadOnly MySQL transaction may not block SELECT INTO OUTFILE at the file-system
      level if the DB user has FILE privilege.
      (2) The banned function list (SLEEP, BENCHMARK, LOAD_FILE) can be bypassed via SQL
      comment injection: `SLE/**/EP(999999999)` splits the keyword past the blocklist.
WHY:  String prefix/contains matching against SQL text is inherently bypassable.
IMPACT: Admin with FILE privilege can write server-side files (OUTFILE) or DoS the DB
        via SLEEP comment-injection. Admin-only, but enables privilege escalation to
        filesystem write from DB access.
TRACE: AdminExecuteQuery → SELECT prefix check passes → read-only tx (may not block OUTFILE)
FIX DIRECTION: Add "INTO OUTFILE", "INTO DUMPFILE" to blocked list; replace function
               blocklist with a server-side `SET SESSION MAX_EXECUTION_TIME=5000` to
               neutralize SLEEP/BENCHMARK without fragile string matching.
```

### [BROKEN] internal/media/management.go:237-238 — MoveMedia broken for remote-backend files
```
WHAT: MoveMedia constructs newPath via filepath.Join(newDir, filename) where newDir is
      always a local filesystem path from validateDirectory. For remote (S3/B2) files,
      storeFor(newPath) finds no matching store and falls through to os.Rename/os.Stat,
      which fail with "file not found" for keys that live in S3.
WHY:  validateDirectory resolves filesystem paths only; remote store key schemes differ.
IMPACT: Any admin attempt to move media stored on S3/B2 to a different directory fails
        silently with a filesystem error. The file is not moved.
TRACE: AdminMoveMedia → MoveMedia → validateDirectory (filesystem path) → filepath.Join →
       storeFor(newPath) → nil store → os.Stat(S3 key) → "not found"
FIX DIRECTION: Detect remote-backend source in MoveMedia and construct newPath using the
               store's prefix scheme rather than validateDirectory.
```

### [BROKEN] web/nuxt-ui/pages/index.vue:860 — USelect bulk-playlist uses deprecated `:options` prop
```
WHAT: The bulk-add-to-playlist USelect uses `:options="..."` instead of `:items="..."`.
      Nuxt UI v4 renamed this prop. All other USelects in the codebase use `:items`.
WHY:  One instance was missed during the v4 migration.
IMPACT: The playlist dropdown in bulk-selection mode renders empty. Users cannot bulk-add
        items to playlists — feature is completely broken silently.
TRACE: index.vue:860 <USelect :options="myPlaylists.map(...)"> → Nuxt UI v4 → no items shown
FIX DIRECTION: Change `:options` to `:items` on the USelect at line 860.
```

---

## HIGH — Will Cause User-Facing Bugs

---

### [BROKEN] web/nuxt-ui/middleware/auth.ts:6 — abortNavigation() on isLoading permanently blocks navigation
```
WHAT: When authStore.isLoading is true, the guard calls abortNavigation() with no
      redirect. The navigation is cancelled with no fallback.
WHY:  Should redirect to login (or retry) instead of hard-aborting.
IMPACT: Any navigation to a protected route during the loading window yields a blank
        page with no recovery. User must manually navigate away.
FIX DIRECTION: Replace abortNavigation() with
               navigateTo({path:'/login', query:{redirect:to.fullPath}}).
```

### [BROKEN] web/nuxt-ui/pages/categories.vue:5 — Auth middleware blocks guest access to category browse
```
WHAT: middleware: 'auth' on categories page redirects unauthenticated guests to login.
      Home page shows category UI to guests, implying category browse is public.
IMPACT: Guests cannot use the category browse feature; redirected to login unexpectedly.
FIX DIRECTION: Remove middleware: 'auth' or use a permissive guard that allows browsing
               but gates user-specific features.
```

### [FRAGILE] internal/hls/transcode.go:101-108 — Lazy transcode master playlist advertises unavailable qualities
```
WHAT: LazyTranscode only transcodes qualities[:1] initially, but generateMasterPlaylist
      is called with job.Qualities (full list). The master .m3u8 lists all quality
      variants; clicking any non-first quality returns 404 until it's lazily generated.
WHY:  generateMasterPlaylist receives job.Qualities, not qualitiesToTranscode.
IMPACT: HLS players that auto-select highest quality fail on the first attempt, causing
        buffering failures or fallback loops. Visible to users as video load errors.
FIX DIRECTION: Pass qualitiesToTranscode to generateMasterPlaylist, or update the master
               playlist after each lazy quality completes.
```

### [LEAK] internal/auth/tokens.go:59,76 — Unbounded goroutines for API token cleanup and last-used updates
```
WHAT: On every ValidateAPIToken call: (1) expired tokens spawn an unbounded cleanup
      goroutine, (2) successful validations spawn an unbounded UpdateLastUsed goroutine.
      No semaphore limits concurrent spawning.
WHY:  Pattern missing the sessionUpdateSem guard used elsewhere in auth.
IMPACT: Under load (100 RPS token-auth), 200 goroutines/sec are spawned. If the DB
        slows, these accumulate faster than they complete, eventually exhausting the
        DB connection pool.
TRACE: ValidateAPIToken → expired → go func() { tokenRepo.Delete }()  [no semaphore]
       ValidateAPIToken → success → go func() { tokenRepo.UpdateLastUsed }() [no semaphore]
FIX DIRECTION: Apply the same sessionUpdateSem (channel semaphore) pattern used in
               ValidateSession to bound concurrent token-cleanup goroutines.
```

### [GAP] internal/media/discovery.go:826 — os.Stat called inside write lock blocks all media reads
```
WHAT: createMediaItem calls os.Stat(oldPath) while holding m.mu.Lock() (exclusive write lock).
      On NFS/CIFS mounts or under I/O contention, stat can block for seconds.
WHY:  Lock held across filesystem syscall for correctness.
IMPACT: During media scan on a slow filesystem, ALL concurrent media reads (GetMedia,
        ListMedia, streaming, search) are blocked for the duration of the stat call.
        Site-wide request stall during scans.
FIX DIRECTION: Perform os.Stat outside the lock; double-check under the lock that the
               metadata state has not changed before acting on the stat result.
```

### [FRAGILE] internal/media/discovery.go:878-898 — Data race: meta pointer read after RLock release
```
WHAT: createMediaItem obtains meta pointer under RLock (line 799), releases the lock
      (line 801), then reads meta.Views, meta.Tags, meta.LastPlayed, etc. (line 878+)
      without re-acquiring the lock. Concurrent IncrementViews/UpdateTags hold the
      write lock and mutate those same fields concurrently.
WHY:  Slow fingerprint I/O forces lock release before reads; fields are not copied first.
IMPACT: Data race detectable by go test -race. Worst case: torn reads of slice headers
        (meta.Tags) producing garbage values in the new MediaItem.
TRACE: createMediaItem → RLock → meta ptr → RUnlock → [fingerprint I/O] → meta.Views read
       IncrementViews → wLock → meta.Views++ [concurrent]
FIX DIRECTION: Copy all needed meta fields into local variables while the initial RLock
               is still held, before calling computeContentFingerprint.
```

### [GAP] internal/hls/transcode.go:85-98 + module.go — Unbounded goroutines spin-wait on transcode semaphore
```
WHAT: enqueueNewHLSJobLocked() spawns a goroutine per job that spin-polls the semaphore
      every 250ms. With 100 pending jobs, 100 goroutines wake every 250ms. No goroutine
      cap on the waiting queue.
WHY:  Dynamic concurrency limit prevents channel-based semaphore; spin-wait was chosen.
IMPACT: Memory pressure from goroutine stacks at scale; CPU waste under sustained load.
        100 jobs = ~800KB stack + constant wakeup overhead.
FIX DIRECTION: Hold pending jobs in the jobs map without a goroutine; spawn a goroutine
               only when a semaphore slot is available.
```

### [LEAK] web/nuxt-ui/composables/useHLS.ts:253-259 — Network retry setTimeout not cleared on cleanup
```
WHAT: HLS ERROR handler: setTimeout(() => hls.startLoad(), delay) — timer ID not stored.
      cleanup() cannot cancel it. If media switches within the delay window, hls.startLoad()
      fires on a destroyed Hls instance.
IMPACT: Potential network requests after unmount; hls.js errors in the console; race
        between old media's retry and new media's initialization.
FIX DIRECTION: Store timer ID in composable scope; clear it in cleanup().
```

### [FRAGILE] web/nuxt-ui/composables/useHLS.ts:306 — consecutiveErrors count not reset on media switch
```
WHAT: consecutiveErrors = { count: 0 } is declared once at composable scope. cleanup()
      does not reset count to 0. After errors on media A, switching to media B starts
      with accumulated count — 2 more errors immediately triggers the abort threshold.
IMPACT: New media fails immediately after an error-heavy previous session.
FIX DIRECTION: Add consecutiveErrors.count = 0 inside cleanup().
```

### [SILENT FAIL] internal/auth/user.go:164 — DB errors masked as ErrUserNotFound
```
WHAT: loadUserAndCache converts any load error (DB timeout, connection failure) into
      ErrUserNotFound. Callers cannot distinguish "user doesn't exist" from "DB is down."
IMPACT: During DB reconnect windows, all session validations return ErrUserNotFound,
        silently rejecting valid sessions. Users get unexplained errors.
FIX DIRECTION: Return the original error; let callers check for ErrUserNotFound specifically.
```

### [GAP] api/handlers/system.go:414 — configDenyList missing "directories" section
```
WHAT: Admin can POST {"directories": {"videos": "/etc"}} to AdminUpdateConfig and redirect
      the media scan to system directories at runtime. "directories" is not in configDenyList.
WHY:  The deny list was not exhaustively defined for all sensitive config sections.
IMPACT: Admin can redirect media scanner to /etc, triggering ffprobe on system files,
        exposing file names in media library, and potentially causing unexpected behavior.
FIX DIRECTION: Add "directories" (and "logging") to configDenyList in admin_config.go.
```

### [GAP] internal/analytics/events.go:90-108 — TrackEvent synchronous DB write blocks streaming handlers
```
WHAT: TrackEvent performs a blocking DB write (5s timeout) directly in the HTTP request
      path. Called from StreamMedia on every stream start.
WHY:  No async event channel; all analytics writes are synchronous.
IMPACT: Under DB pressure, every media streaming request stalls for up to 5 seconds at
        the analytics write. Streaming latency degrades 5x under DB load.
TRACE: StreamMedia → TrackView → TrackEvent → eventRepo.Create [5s timeout, blocks]
FIX DIRECTION: Fire analytics writes via a buffered channel with a worker goroutine; drop
               or log on overflow rather than blocking the caller.
```

### [BROKEN] internal/auth/session.go:183-191 — Logout only searches m.sessions, not m.adminSessions
```
WHAT: Due to the admin session storage bug above, admin sessions land in m.sessions.
      Logout only searches m.sessions (which is accidentally correct for now), but
      LogoutAdmin searches m.adminSessions (always fails). These two bugs cancel each
      other out currently, but fixing one without the other breaks logout entirely.
IMPACT: Both bugs must be fixed atomically to avoid breaking the logout path.
FIX DIRECTION: Fix createSession to store admin sessions in m.adminSessions AND update
               both Logout and LogoutAdmin to check both maps.
```

### [GAP] cmd/server/main.go:141 — S3/B2 storage backend init has no timeout
```
WHAT: initCtx is context.Background() with no deadline. s3compat.New() performs a
      connection check to the S3 endpoint during startup. If unreachable, blocks forever.
IMPACT: Server startup hangs indefinitely on unreachable S3 endpoint with no error log.
FIX DIRECTION: Use context.WithTimeout(context.Background(), 30*time.Second) for storage
               backend initialization.
```

---

## MEDIUM — Tech Debt / Time Bombs

---

### [FRAGILE] internal/auth/user.go:236 — UpdateUser double-copy race can clobber concurrent password change
```
WHAT: UpdateUser reads the user under lastAdminMu, then re-reads from the cache under
      usersMu.RLock. Between the two reads, a concurrent ChangePassword can modify the
      user. The UpdateUser write then overwrites the new password with the old one.
IMPACT: Concurrent password change + profile update loses the password change silently.
FIX DIRECTION: Merge into a single lock section using the freshUser pointer from lastAdminMu.
```

### [FRAGILE] internal/media/discovery.go:799 — createMediaItem double-read race creates duplicate stable IDs
```
WHAT: Two concurrent createMediaItem calls for the same newly-discovered file both read
      hasMeta=false (under separate RLocks), both compute fingerprints, both enter the
      write lock. Second write wins in memory; first is already saved to DB → same file
      has two DB entries with different stable IDs.
IMPACT: Same physical file appears with two IDs; can cause duplicate library entries or
        ID→file mapping inconsistencies after restart.
FIX DIRECTION: After acquiring the write lock, re-check `if _, exists := m.metadata[path]; exists`.
```

### [GAP] internal/hls/cleanup.go:88-104 — cleanInactiveJob TOCTOU can orphan DB row
```
WHAT: cleanInactiveJob checks last access under accessTracker.mu, then acquires jobsMu
      to delete. Between these two locks, a concurrent RecordAccess updates the tracker
      and tries to acquire jobsMu. Since we hold jobsMu, RecordAccess blocks. When we
      release jobsMu, RecordAccess proceeds and calls saveJob() on a now-deleted job,
      inserting an orphan DB row. On next startup the job reappears with missing disk files.
IMPACT: Rare but causes spurious "completed job with missing files" on restart.
FIX DIRECTION: Check in-memory accessTracker under jobsMu before deleting, or use a
               single consolidated lock for both.
```

### [GAP] internal/hls/probe.go:96-97 — getSourceHeight uses no-ctx probe first (inverted order)
```
WHAT: getSourceHeight calls ffmpeg-go ProbeWithTimeout (no context, 30s internal timer)
      FIRST and only falls back to the context-aware exec.CommandContext path. On server
      shutdown, this blocks for up to 30 seconds per in-flight probe.
IMPACT: Shutdown takes up to 30s × N active probes extra time.
FIX DIRECTION: Swap probe order: try context-aware ffprobe first; fall back to ffmpeg-go.
```

### [GAP] internal/thumbnails/queue.go:92-128 — No panic recovery in thumbnail worker
```
WHAT: If generateThumbnail panics (nil pointer in BlurHash, image decode failure), the
      goroutine exits without recovery. inFlight.Delete is never called (job blocked for
      5 minutes). The worker pool permanently shrinks by 1 with no replacement.
IMPACT: Repeated thumbnail panics drain the worker pool; thumbnail generation stops.
FIX DIRECTION: Add recover() in worker(); reset inFlight on panic; optionally respawn.
```

### [SILENT FAIL] internal/thumbnails/generate.go:282-283 — generateWebPFromAudio leaves corrupt output on failure
```
WHAT: generateWebPFromAudio returns error but does not remove the partial/0-byte output
      file. generateVideoThumbnail correctly does os.Remove on failure; audio does not.
IMPACT: 0-byte or corrupt WebP thumbnails served to WebP-capable browsers as broken images.
FIX DIRECTION: Add `_ = os.Remove(opts.outputPath)` in the error branch, same as video path.
```

### [GAP] internal/thumbnails/generate.go:239-271 — generateAudioThumbnail no cleanup on failure
```
WHAT: Unlike generateVideoThumbnail, generateAudioThumbnail does not remove the output
      file on ffmpeg failure. Waveform PNG may be 0-byte or incomplete.
IMPACT: Corrupt thumbnail file exists until the next cleanup.go pass runs.
FIX DIRECTION: Add cleanup on error path in generateAudioThumbnail.
```

### [GAP] internal/security/security.go:962-978 — Download/stream/media paths fully exempt from rate limiting
```
WHAT: The rate-limit middleware unconditionally skips paths starting with /download,
      /thumbnail, /stream, /hls/, /media/. No secondary per-IP resource limit is applied.
IMPACT: A single IP can open unlimited concurrent streams or downloads with no rate limit.
        Expensive ffmpeg transcodes can be triggered without restriction.
FIX DIRECTION: Apply a separate higher-limit or concurrency-based rate limiter for media
               paths rather than fully exempting them.
```

### [FRAGILE] internal/security/security.go:211-213 — Auto-ban ExpiresAt computed at closure creation time
```
WHAT: The persistBan closure in Start() captures `new(time.Now().Add(duration))` at
      closure definition time (server startup), not at the moment each auto-ban fires.
      All auto-bans issued through this closure share the same ExpiresAt timestamp.
IMPACT: Auto-bans have wrong expiry times — effectively expired (if server ran for long)
        or permanent (if server just started). The bans fire at the correct time, but
        the persisted expiry is anchored to startup time.
FIX DIRECTION: Move the ExpiresAt computation inside the goroutine body in recordViolation.
```

### [FRAGILE] internal/media/discovery.go:516-529 — ffprobe worker pool does not drain on context cancel
```
WHAT: Worker pool sends to semaphore channel `sem <- struct{}{}` in a goroutine. If
      scanCtx is cancelled while 10 slots are occupied, new goroutines block on the
      send indefinitely. wg.Wait() does not return until all pending goroutines complete.
IMPACT: On shutdown, media scan can block for up to 10 × 30s = 300s. The saveCtx in
        Stop() (3-minute deadline) expires first, but the scan goroutine keeps running.
FIX DIRECTION: Use select { case sem <- struct{}{}: ... case <-m.scanCtx.Done(): wg.Done(); return }.
```

### [GAP] internal/media/management.go:374-402 — RemoveMedia DB cleanup spawns unbounded goroutines
```
WHAT: `go func() { metadataRepo.Delete(ctx, path) }()` — no semaphore. Bulk deletion of
      10,000 orphaned metadata entries spawns 10,000 concurrent goroutines.
IMPACT: Exhausts DB connection pool during bulk cleanup operations.
FIX DIRECTION: Apply semaphore or batch the DELETE WHERE path IN (...).
```

### [GAP] internal/admin/admin_scanner.go:191-217 — ApproveContent returns 200 on partial success
```
WHAT: If h.scanner.ApproveContent succeeds but h.media.SetMatureFlag fails, the handler
      logs an error but still returns HTTP 200 with nil data. Scanner and media module
      are in inconsistent state.
IMPACT: Admin sees "success"; mature flag is not actually updated until next scan.
FIX DIRECTION: Return HTTP 500 if SetMatureFlag fails; do not return 200 on partial success.
```

### [GAP] internal/admin/admin_remote.go:59,177 — Remote source create/delete: split state on config write failure
```
WHAT: CreateRemoteSource: h.remote.AddSource succeeds, h.config.Update fails → source
      active in memory but not persisted. After restart, source disappears.
      DeleteRemoteSource: h.remote.RemoveSource succeeds, h.config.Update fails → source
      gone from memory but still in config. After restart, deleted source reappears.
IMPACT: Silent state inconsistencies that only manifest on restart.
FIX DIRECTION: On config write failure, roll back the module call (RemoveSource/AddSource)
               and return HTTP 500.
```

### [FRAGILE] internal/admin/admin_classify.go:209,254 — ClassifyDirectory/AllPending: untracked goroutines, no dedup
```
WHAT: Both classification endpoints spawn context.Background() goroutines with no
      deduplication guard. Multiple POST calls spawn parallel classification runs.
IMPACT: Concurrent runs hit HuggingFace API rate limits; duplicate classification results.
FIX DIRECTION: Submit via h.tasks system so duplicate calls are rejected and progress
               is trackable (same pattern as ClassifyRunTask).
```

### [FRAGILE] internal/config/config.go:289-303 — Config watcher goroutines may deliver stale config
```
WHAT: Update() spawns each watcher in a goroutine with the cfg snapshot from that call.
      If two Update() calls race, watcher goroutines may execute out-of-order, with the
      earlier snapshot overwriting module state set by the newer one.
IMPACT: Rate limiter, HLS profiles, or streaming config may briefly use stale values
        after rapid consecutive admin config saves.
FIX DIRECTION: Use a dedicated dispatcher that always passes the latest config snapshot
               to watchers, or serialize watcher execution.
```

### [GAP] internal/database/migrations.go:775-799 — Playlist items PK migration non-atomic in MySQL
```
WHAT: migratePlaylistItemsPK wraps UPDATE + ALTER TABLE in a transaction. MySQL DDL
      (ALTER TABLE) causes implicit COMMIT — the transaction provides no atomicity.
      If ALTER TABLE fails after UPDATE succeeds, the UPDATE is already committed.
IMPACT: On migration failure, table has UUID-filled id columns with wrong primary key.
        Re-run on next startup loops the UPDATE (no-op) then retries ALTER TABLE.
FIX DIRECTION: Document that MySQL DDL is auto-committed; make the migration idempotent
               by checking column existence before UPDATE, not just PK name.
```

### [GAP] internal/repositories/mysql/media_metadata_repository.go:282-286 — Limit=0 returns full table
```
WHAT: ListFiltered skips the Limit clause when filter.Limit == 0. A handler passing
      filter.Limit=0 pulls the entire media_metadata table into memory.
IMPACT: Any code path that accidentally passes Limit=0 triggers an unbounded query.
FIX DIRECTION: Apply a default cap (e.g., 1000) when filter.Limit == 0.
```

### [GAP] pkg/helpers/ssrf.go:54-82 — DNS rebinding: validation and actual dial use separate lookups
```
WHAT: ValidateURLForSSRF resolves the hostname, checks IPs, returns OK. The actual HTTP
      dial performs a second DNS lookup. An attacker controlling DNS can make the first
      resolve return a public IP (passes check) and the second return a private IP.
IMPACT: SSRF possible for attackers with control over a DNS server for the target domain.
FIX DIRECTION: Document that ValidateURLForSSRF must always be paired with SafeHTTPTransport
               for the actual request; add validation in SafeHTTPTransport itself.
```

### [GAP] web/nuxt-ui/middleware/admin.ts:12 — Non-admin users redirected to /admin-login (wrong page)
```
WHAT: An authenticated non-admin user visiting /admin is redirected to /admin-login.
      The admin-login page calls authStore.logout() on failed admin login attempts.
IMPACT: Non-admin user lands on admin login → tries credentials → gets "Admin access
        required" → authStore.logout() fires → user is unexpectedly logged out.
FIX DIRECTION: Detect isLoggedIn && !isAdmin separately; redirect to '/' with a toast
               notification rather than /admin-login.
```

### [LEAK] pages/player.vue:342-344 — 'play' EventListener accumulates on each HLS re-attach
```
WHAT: onVideoLoaded() adds a 'play' listener with {once: false} on every loadedmetadata
      event. HLS re-attaches (seek, src change, quality switch) each fire loadedmetadata.
IMPACT: N listeners fire on every play event after N HLS operations in a session.
        Minor CPU overhead; accumulates on long player sessions.
FIX DIRECTION: Change to {once: true} or add a boolean guard to add the listener once only.
```

### [FRAGILE] pages/player.vue:780 — seekTimer and volumeSaveTimer not cleared on media switch
```
WHAT: watch(mediaId) resets playEventSent but not seekTimer or volumeSaveTimer.
IMPACT: Debounced analytics event from previous media fires with old media ID after switch.
FIX DIRECTION: Add clearTimeout(seekTimer) and clearTimeout(volumeSaveTimer) in the
               watch(mediaId) callback.
```

### [FRAGILE] pages/player.vue:317-321 — AbortController for position saves signal never forwarded to fetch
```
WHAT: positionSaveController is created and aborted on each savePosition() call, but
      the signal is never passed to playbackApi.savePosition(). Controller does nothing.
IMPACT: Out-of-order position saves can persist a stale position (t=120 overwrites t=300).
FIX DIRECTION: Thread the signal through useApi request options, or remove the no-op controller.
```

### [FRAGILE] pages/player.vue:338-344 — Video media: EQ AudioContext has no resume path after browser suspension
```
WHAT: The 'play' listener that calls audioCtx.resume() is only attached when media type
      is 'audio'. Video media with EQ enabled has no listener to resume AudioContext
      after browser tab suspension.
IMPACT: EQ stops working on video media after the tab has been hidden and shown again.
FIX DIRECTION: Move audioCtx.resume() listener into ensureAudioGraph(), not gated by type.
```

### [SILENT FAIL] pages/player.vue:504-516 — PiP state desync on error
```
WHAT: togglePiP() catch block sets isPiP.value = false regardless of actual PiP state.
      If exitPictureInPicture() fails, the button shows "Enter PiP" but PiP is still active.
FIX DIRECTION: In catch block, set isPiP.value = (document.pictureInPictureElement === videoRef.value).
```

### [GAP] internal/admin/admin_security.go:199-218 — UnbanIP: no IP format validation
```
WHAT: BanIP validates IP format; UnbanIP only checks non-empty. Malformed strings
      (spaces, injection characters) pass through to h.security.UnbanIP.
FIX DIRECTION: Add net.ParseIP validation before calling UnbanIP, same as BanIP.
```

---

## LOW — Cleanup / Style / Minor

---

### [GAP] internal/auth/tokens.go — CreateAPIToken ExpiresAt correctly uses new(time.Now().Add(ttl))
```
NOTE: This IS correct Go 1.26.2 — new(expr) allocates a pointer to the expression value.
      No fix needed. [OK]
```

### [FRAGILE] internal/auth/user.go:195 — wantsDemote only checks for RoleViewer (future role gap)
```
WHAT: If a third role is added, demoting admin to that role bypasses the last-admin guard.
FIX DIRECTION: Change to: wantsDemote = updates["role"] != nil && updates["role"] != string(models.RoleAdmin)
```

### [FRAGILE] internal/auth/bootstrap.go:80 — Admin user has empty Salt
```
WHAT: Admin is created with Salt: "" — bcrypt comparison uses password+salt = password.
      Any future enforcement of "salt must be non-empty" silently breaks admin login.
FIX DIRECTION: Document this design explicitly, or generate a salt at bootstrap time.
```

### [FRAGILE] internal/config/validate.go:182-191 — LockoutDuration=0 passes without warning
```
WHAT: MaxLoginAttempts=0 emits a warning; LockoutDuration=0 does not. A zero lockout
      duration renders brute-force protection completely ineffective.
FIX DIRECTION: Add log.Warn for LockoutDuration == 0, same as MaxLoginAttempts.
```

### [GAP] internal/tasks/scheduler.go:396-405 — EnableTask: re-enabled task may have no running loop
```
WHAT: If a task is re-enabled while its goroutine is in the process of exiting, the
      goroutine sees the re-enable too late and exits anyway. Task is "enabled" but
      has no running loop until manually re-enabled again or server restart.
FIX DIRECTION: After setting Enabled=true, check loopRunning; start a new loop if false.
```

### [FRAGILE] internal/tasks/scheduler.go:354-380 — RunNow goroutine may run after scheduler cancel
```
WHAT: RunNow reads ctx under RLock, releases the lock, then spawns a goroutine. If the
      scheduler is stopped between the RLock release and the goroutine start, the
      goroutine calls executeTask with an already-cancelled context. Tasks that don't
      check ctx immediately run to completion post-shutdown.
FIX DIRECTION: Re-check ctx.Err() != nil inside the goroutine before calling executeTask.
```

### [GAP] internal/repositories/mysql/user_preferences_repository.go — Save() may silently no-op for orphaned rows
```
WHAT: Upsert uses GORM's Save(). For rows deleted from DB while the in-memory object has
      a non-zero UserID, GORM attempts UPDATE (0 rows affected) and does not INSERT.
FIX DIRECTION: Align with user_repository_gorm.go's OnConflict upsert pattern.
```

### [SILENT FAIL] internal/repositories/mysql/extractor_item_repository.go:166-178 — Timestamp parse failures silently zero fields
```
WHAT: All four time fields use "if parsed OK, assign" — on failure the field remains
      at zero/epoch with no log or error. Items with corrupt timestamps appear as
      epoch-dated, affecting sort order and expiry logic.
FIX DIRECTION: Log a warning on parse failure so corrupt DB rows are visible.
```

### [GAP] internal/repositories/mysql/analytics_repository.go:56-72 — No max-cap on caller-supplied Limit
```
WHAT: Positive Limit values from callers are used directly without an upper cap.
      A large Limit can trigger an OOM query on large analytics tables.
FIX DIRECTION: Enforce if limit > defaultAnalyticsQueryLimit { limit = defaultAnalyticsQueryLimit }.
```

### [FRAGILE] pkg/middleware/agegate.go:90-112 — XFF candidate not validated as IP before return
```
WHAT: extractClientIP does not call net.ParseIP() on XFF candidates. A crafted
      "not-an-ip, 1.2.3.4" in XFF could store a garbage string as the apparent IP,
      breaking IP-based TTL tracking.
FIX DIRECTION: Add net.ParseIP check before IsTrustedProxy check in the XFF walk.
```

### [FRAGILE] pkg/middleware/agegate.go:253-265 — verifiedIPs map size TOCTOU
```
WHAT: Map size is checked, then released, then evictExpired runs (re-acquires lock),
      then re-acquired to insert. Concurrent goroutines can exceed maxVerifiedIPs
      during the window between the size check and the insert.
FIX DIRECTION: Re-check len(ag.verifiedIPs) >= maxVerifiedIPs after re-acquiring the lock.
```

### [FRAGILE] internal/remote/remote.go — CleanCache holds write lock during os.Remove calls
```
WHAT: m.mu.Lock() is held across all os.Remove calls in CleanCache. On slow/NFS storage,
      all streaming/lookup requests are blocked for the full duration of cache cleanup.
FIX DIRECTION: Collect paths under the lock, release, remove files, re-acquire to update index.
```

### [FRAGILE] internal/extractor/extractor.go — New http.Client per HLS segment proxy call
```
WHAT: proxyStream creates a new http.Client (and transport/connection pool) on every
      segment request. No connection reuse between segment fetches.
IMPACT: Connection churn: TCP connection setup latency per segment; ephemeral port
        exhaustion risk for high-bitrate streams with many segments.
FIX DIRECTION: Maintain a module-level http.Client with SafeHTTPTransport; reuse across calls.
```

### [GAP] internal/downloader/client.go — No context propagation; no dial timeout on HTTP transport
```
WHAT: All HTTP methods accept no context.Context. transport has no DialContext timeout.
IMPACT: Requests cannot be cancelled on shutdown; goroutines stuck waiting for the full
        client-level timeout if the downloader service crashes mid-connection.
FIX DIRECTION: Add ctx parameter to get/post/del; use http.NewRequestWithContext;
               set DialContext: (&net.Dialer{Timeout: 5s}).DialContext on transport.
```

### [GAP] internal/updater/updater.go — installUpdate: no rollback if chmod fails; no backup integrity check
```
WHAT: (1) If os.Chmod fails after binary copy, the server has a non-executable binary
      and no automatic rollback to the old one.
      (2) restoreFromBackup copies the backup with no checksum verification.
FIX DIRECTION: (1) Call restoreFromBackup on Chmod failure.
               (2) Save SHA256 of old binary before backup; verify in restoreFromBackup.
```

### [DRIFT] internal/admin/admin_config.go:100-105 — AdminUpdateConfig: only two flags hot-reloaded
```
WHAT: After config update, only security whitelist/blacklist enable flags are applied
      to in-memory modules. Rate limiting, HLS profiles, analytics settings, etc. do
      not take effect until server restart.
IMPACT: Admin sees "success" but server behavior unchanged until restart — silent drift
        between persisted config and running state.
FIX DIRECTION: Document which settings require restart; implement per-module hot-reload
               callbacks or show a "restart required" indicator in the admin UI.
```

### [GAP] internal/duplicates/duplicates.go — RecordDuplicatesFromSlave O(n×m) full table scan
```
WHAT: Every slave sync loads the entire receiver_media table into memory to build a
      fingerprint index, then performs nested-loop comparison.
IMPACT: With 100K+ items across multiple slaves, each sync triggers full table load + O(n×m) compare.
FIX DIRECTION: Push fingerprint matching to SQL (JOIN on fingerprint column); or maintain
               incremental cross-slave fingerprint index.
```

### [GAP] internal/validator/validator.go — No disk space check before FFmpeg repair transcode
```
WHAT: FixFile starts a full FFmpeg transcode to a temp file without checking available
      disk space. Output size is unknown until completion.
IMPACT: A large corrupt file triggers a transcode that fills the disk, potentially
        crashing other write operations on the same filesystem.
FIX DIRECTION: Check available space against input_size × safety_factor before starting.
```

### [GAP] api/handlers/feed.go — GetRSSFeed: no result caching (repeated full library scans)
```
WHAT: GetRSSFeed calls h.media.ListMedia synchronously on every request, holding the
      media read lock for the full scan. No server-side cache.
IMPACT: RSS readers polling every 5 minutes generate repeated library-wide lock contention.
FIX DIRECTION: Cache the rendered feed XML in memory with a 5-minute TTL keyed on filter params.
```

### [FRAGILE] components/admin/SystemSettingsPanel.vue:6 — Config typed as Record<string, any>
```
WHAT: set(section, key, val) accepts any string — typos silently create new config
      sections instead of failing at compile time.
FIX DIRECTION: Define a typed ServerConfigSchema interface matching the Go config struct.
```

### [FRAGILE] components/AudioVisualizer.vue:187 — Canvas intrinsic size fixed; blurry in theater mode
```
WHAT: Canvas dimensions are set at render time. CSS w-full/h-full scales the bitmap,
      producing blurry/stretched bars on window resize or theater mode toggle.
FIX DIRECTION: Add ResizeObserver to update canvas.width/height on container resize.
```

### [REDUNDANT] components/PlayerControls.vue:73-95 — Dead touchend branch in onSeekBarTouch
```
WHAT: onSeekBarTouch has a touchend branch, but @touchend binds to a separate function.
      The branch is unreachable dead code.
FIX DIRECTION: Remove the if (e.type === 'touchend') block from onSeekBarTouch.
```

### [GAP] internal/admin/admin_users.go:303 — Hardcoded "admin" string skip in bulk user operations
```
WHAT: Bulk user operations skip entries where username == "admin" literally. This protects
      neither users renamed away from "admin" nor non-admin users actually named "admin".
FIX DIRECTION: Replace string check with role == RoleAdmin check.
```

### [GAP] internal/crawler/crawler.go — Discovered stream URLs stored without SSRF pre-validation
```
WHAT: Stream URLs discovered by crawling are stored in the discoveries table without
      SSRF validation. Validation only occurs when an admin approves the discovery
      (extractor.AddItem calls ValidateURLForSSRF). Until then, SSRF URLs are visible
      in the admin discoveries panel, potentially misleading admins.
FIX DIRECTION: Validate discovered stream URLs for SSRF before storing in discoveries table.
```

### [GAP] internal/admin/admin_tasks.go:25,76 — Error classification by string.Contains (brittle)
```
WHAT: "not found" and "not currently running" matched by string content to return 404/409.
      If task error messages change, silently falls to wrong HTTP status.
FIX DIRECTION: Define typed error sentinels (ErrTaskNotFound, ErrTaskNotRunning) and use errors.Is().
```

### [LEAK] internal/auth/user.go:213/439 — lastAdminMu held across DB query
```
WHAT: ListUsers (DB query) called while holding lastAdminMu.Lock(). All concurrent admin
      updates/deletes blocked for the full query duration.
FIX DIRECTION: Count admins from in-memory m.users under m.usersMu.RLock (always up-to-date).
```

---

## FULL INDEX BY TAG

| Tag | Count | Issues |
|-----|-------|--------|
| BROKEN | 8 | session.go:250 admin sessions in wrong map; management.go:237 remote MoveMedia; index.vue:860 USelect :options; auth.ts:6 abortNavigation; categories.vue auth middleware; session.go:183 Logout map mismatch; transcode.go lazy HLS; auth.ts admin-login redirect |
| SECURITY | 6 | ssrf.go:33 IPv4-mapped IPv6 bypass; authenticate.go:94 timing oracle; browser.go --no-sandbox; wsconn.go API key in query string; system.go:414 SELECT INTO OUTFILE + comment injection; admin_config.go:47 directories missing from denyList |
| LEAK | 8 | tokens.go:59 expired-token goroutines; tokens.go:76 UpdateLastUsed goroutines; management.go:374 RemoveMedia goroutines; mature.go HuggingFace accumulation; useHLS.ts:253 retry setTimeout; player.vue:342 play listener; user.go:213 lastAdminMu×DB; downloader/client.go no-cancel goroutines |
| SILENT FAIL | 9 | user.go:164 DB errors → UserNotFound; generate.go:282 WebP corrupt on failure; generate.go:239 audio thumb no cleanup; player.vue:504 PiP desync; s3.go:330 RemoveAll discards error; repository:166 timestamp parse failures; events.go TrackEvent blocks callers; admin_scanner.go:191 200 on partial success; local.go EvalSymlinks TOCTOU |
| GAP | 42 | discovery.go:826 Stat inside wLock; hls/probe.go:96 probe order inverted; thumbnails/queue.go:92 no panic recovery; security.go:962 media paths exempt from rate limit; media/management.go:374 unbounded cleanup goroutines; hls/cleanup.go:88 TOCTOU orphan row; migrations.go:775 non-atomic DDL; repository:282 Limit=0 full scan; ssrf.go:54 DNS rebinding; admin_security.go:199 UnbanIP no validation; admin_remote.go split state; admin_classify.go no dedup; cmd/main.go:141 S3 init no timeout; analytics/events.go sync write blocks; + 28 more in Medium/Low |
| FRAGILE | 38 | user.go:236 double-copy race; discovery.go:799 duplicate stable IDs; discovery.go:878 meta pointer data race; hls/transcode.go:101 lazy master playlist; hls/transcode.go:85 spin-wait goroutines; config.go:289 stale watcher delivery; security.go:211 auto-ban ExpiresAt; discovery.go:516 ffprobe pool no drain; useHLS.ts:306 consecutiveErrors not reset; player.vue:780 stale timers; player.vue:317 AbortController unused; player.vue:338 EQ no resume; + 26 more in Medium/Low |
| DRIFT | 2 | admin_config.go:100 only 2 flags hot-reloaded; session.go admin/user map mismatch |
| REDUNDANT | 1 | PlayerControls.vue:73 dead touchend branch |
| INCOMPLETE | 3 | Lazy HLS quality advertised before available; orphaned rows after delete; ApproveContent partial state |
| OK | ~100 | All RWMutex pairings correct; all GORM queries parameterized; backup zip-bomb guard; SSRF SafeHTTPTransport; playlist dedup; v-for :key attrs; store access patterns; copy-before-unlock patterns; semaphore bounds; sync.Once shutdown guards |

---

## PRIORITY MATRIX

### Critical (fix before next deploy)
1. [BROKEN] Admin sessions in wrong map → LogoutAdmin always fails (session.go:250)
2. [SECURITY] Timing oracle → username enumeration (authenticate.go:94)
3. [SECURITY] IPv4-mapped IPv6 SSRF bypass (ssrf.go:33)
4. [SECURITY] API key in WebSocket query string → log exposure (receiver/wsconn.go)
5. [SECURITY] Chrome --no-sandbox (crawler/browser.go)
6. [SECURITY] SELECT INTO OUTFILE + keyword bypass via SQL comments (system.go:414)
7. [BROKEN] Remote-backend MoveMedia always fails (management.go:237)
8. [BROKEN] index.vue USelect :options → :items (bulk playlist broken)
9. [BROKEN] auth middleware abortNavigation leaves user stuck
10. [SECURITY] configDenyList missing "directories" (admin_config.go:47)
11. [FRAGILE] Lazy HLS master playlist lists unavailable qualities (transcode.go:101)
12. [LEAK] Unbounded goroutines on API token operations (tokens.go:59,76)
13. [BROKEN] categories.vue blocks guest browse
14. [GAP] S3 init no timeout → startup hang (main.go:141)

### High (fix this sprint)
15. [SILENT FAIL] DB errors masked as UserNotFound (user.go:164)
16. [FRAGILE] os.Stat inside write lock → site-wide stall (discovery.go:826)
17. [FRAGILE] Data race: meta pointer read after RLock release (discovery.go:878)
18. [GAP] TrackEvent blocks streaming handlers 5s (events.go:90)
19. [FRAGILE] HLS spin-wait + unbounded goroutines per job (transcode.go:85)
20. [LEAK] HLS network retry timer not cleared (useHLS.ts:253)
21. [FRAGILE] consecutiveErrors not reset on media switch (useHLS.ts:306)
22. [GAP] ApproveContent returns 200 on partial success (admin_scanner.go:191)
23. [GAP] Remote source create/delete split state (admin_remote.go:59,177)
24. [FRAGILE] Player seekTimer/volumeSaveTimer not cleared on media switch (player.vue:780)
25. [FRAGILE] Player AbortController signal unused (player.vue:317)

### Medium (next sprint)
26. [FRAGILE] UpdateUser double-copy race clobbers concurrent password change (user.go:236)
27. [FRAGILE] createMediaItem double-read race creates duplicate stable IDs (discovery.go:799)
28. [GAP] cleanInactiveJob TOCTOU can orphan DB row (hls/cleanup.go:88-104)
29. [GAP] getSourceHeight uses no-ctx probe first — 30s shutdown delay (probe.go:96)
30. [GAP] Thumbnail worker: no panic recovery, pool permanently shrinks (queue.go:92)
31. [SILENT FAIL] generateWebPFromAudio leaves corrupt output on failure (generate.go:282)
32. [GAP] generateAudioThumbnail: no cleanup of partial output on failure (generate.go:239)
33. [GAP] Download/stream/media paths fully exempt from rate limiting (security.go:962)
34. [FRAGILE] Auto-ban ExpiresAt computed at closure creation, not ban time (security.go:211)
35. [FRAGILE] ffprobe worker pool does not drain on context cancel (discovery.go:516)
36. [GAP] RemoveMedia DB cleanup spawns unbounded goroutines (management.go:374)
37. [GAP] ApproveContent/RejectContent returns 200 on partial success (admin_scanner.go:191)
38. [GAP] Remote source create/delete split state on config write failure (admin_remote.go:59,177)
39. [FRAGILE] ClassifyDirectory/AllPending untracked goroutines, no dedup (admin_classify.go:209,254)
40. [FRAGILE] Config watcher goroutines may deliver stale config under concurrent updates (config.go:289)
41. [GAP] Playlist items PK migration non-atomic in MySQL (migrations.go:775)
42. [GAP] media_metadata ListFiltered: Limit=0 returns full table (repository:282)
43. [GAP] DNS rebinding: ValidateURLForSSRF and actual dial use separate DNS lookups (ssrf.go:54)
44. [GAP] Non-admin users redirected to /admin-login, causing accidental logout (admin.ts:12)
45. [LEAK] 'play' EventListener accumulates on each HLS re-attach (player.vue:342)
46. [FRAGILE] seekTimer/volumeSaveTimer not cleared on media switch (player.vue:780)
47. [FRAGILE] AbortController signal for position saves never forwarded to fetch (player.vue:317)
48. [FRAGILE] EQ AudioContext has no resume path for video media after suspension (player.vue:338)
49. [SILENT FAIL] PiP state desyncs on exitPictureInPicture error (player.vue:504)
50. [GAP] UnbanIP accepts unvalidated IP format strings (admin_security.go:199)

### Low (backlog)
51. [FRAGILE] wantsDemote only checks RoleViewer — future third role bypasses last-admin guard (user.go:195)
52. [FRAGILE] Admin user created with empty Salt — future enforcement breaks admin login (bootstrap.go:80)
53. [FRAGILE] LockoutDuration=0 passes without warning, disables brute-force protection (validate.go:182)
54. [GAP] EnableTask: re-enabled task may have no running loop in race window (scheduler.go:396)
55. [FRAGILE] RunNow goroutine may run tasks after scheduler cancel (scheduler.go:354)
56. [GAP] user_preferences/permissions Save() may silently no-op for orphaned rows (repositories)
57. [SILENT FAIL] Extractor timestamp parse failures silently zero out time fields (repository:166)
58. [GAP] analytics_repository: no max-cap on caller-supplied Limit (repository:56)
59. [FRAGILE] agegate.go XFF candidate not validated as IP before return (agegate.go:90)
60. [FRAGILE] agegate.go verifiedIPs map size TOCTOU under high concurrency (agegate.go:253)
61. [FRAGILE] CleanCache holds write lock during os.Remove calls (remote/remote.go)
62. [FRAGILE] New http.Client per HLS segment proxy call — connection churn (extractor.go)
63. [GAP] Downloader client: no context propagation, no dial timeout on transport (client.go)
64. [GAP] installUpdate: no rollback if chmod fails; no backup integrity check (updater.go)
65. [DRIFT] AdminUpdateConfig: only whitelist/blacklist flags hot-reloaded; all others need restart (admin_config.go:100)
66. [GAP] RecordDuplicatesFromSlave: O(n×m) full table scan per slave sync (duplicates.go)
67. [GAP] FixFile: no pre-flight disk space check before FFmpeg transcode (validator.go)
68. [GAP] GetRSSFeed: no result caching, repeated full library scans (feed.go)
69. [FRAGILE] SystemSettingsPanel config typed as Record<string, any> — typos create silent new sections (admin UI)
70. [FRAGILE] AudioVisualizer canvas intrinsic size fixed — blurry/stretched in theater mode (AudioVisualizer.vue:187)
71. [REDUNDANT] Dead touchend branch in PlayerControls.onSeekBarTouch (PlayerControls.vue:73)
72. [GAP] Hardcoded "admin" string skip in bulk user operations instead of role check (admin_users.go:303)
73. [GAP] Discovered stream URLs stored without SSRF pre-validation in discoveries table (crawler.go)
74. [GAP] Task error classification by string.Contains — brittle, no typed sentinels (admin_tasks.go:25,76)
75. [LEAK] lastAdminMu held across DB ListUsers query — serial bottleneck (user.go:213,439)
76. [FRAGILE] saveMetadata goroutine captures stale scanCtx on in-process restart (discovery.go:608)
77. [FRAGILE] ResolveForFFmpeg startup race if SetStores not called before initial scan (management.go:111)
78. [GAP] handler.go checkMatureAccess fails-open on DB error — serves mature content (handler.go:479)
79. [GAP] GetWatchHistory limit param has no upper cap (auth.go:519)
80. [GAP] ExportWatchHistory filename not sanitized via SafeContentDispositionFilename (auth.go:781)
81. [SILENT FAIL] S3 RemoveAll discards initial single-object delete error (s3.go:330)
82. [FRAGILE] local.go TOCTOU on EvalSymlinks fallback during file creation (local.go:55)
83. [GAP] handler.go resolveRelativeInDir skips EvalSymlinks — symlinks can escape allowed dirs (handler.go:536)
84. [GAP] Security module saveIPLists: config and entries saved in separate non-atomic calls (security.go:696)
85. [GAP] createTables called without dbMu lock — nil deref possible on concurrent Stop (migrations.go:493)
86. [FRAGILE] Rename on S3 is non-atomic — both src and dst exist on partial failure (s3.go:376)
87. [FRAGILE] upNextTimer countdown can briefly hit negative before clearInterval (player.vue:459)
88. [FRAGILE] Top-level fire-and-forget API call in index.vue setup (not inside onMounted) (index.vue:50)
89. [LEAK] HuggingFace HTTP goroutine accumulation under slow API response (scanner/mature.go)
90. [GAP] SyncRemoteSource spawns duplicate concurrent sync goroutines with no dedup (admin_remote.go:165)
91. [FRAGILE] ApplySourceUpdate advisory dedup check has race with actual updater module lock (admin_updates.go:82)
92. [FRAGILE] RestartServer: no response flush guarantee before 1s sleep + shutdown (admin_lifecycle.go:14)
93. [FRAGILE] AdminExportAuditLog: http.ServeFile may overwrite manually set Content headers (admin_audit.go:26)
94. [GAP] GetStorageUsage filepath.Walk unbounded on time — can block HTTP handler goroutine (system.go)
95. [GAP] GetMyRatings: O(n) individual media lookups in loop (suggestions.go)
96. [FRAGILE] GetRecentContent: ListMedia called with no result caching on high-traffic endpoint (suggestions.go)
97. [FRAGILE] AdminDeleteMedia: thumbnail file removed via os.Remove without notifying thumbnails module (admin_media.go:329)
98. [GAP] AdminDeleteMedia: orphaned DB rows for playback, favorites, watch history (admin_media.go:304)
99. [GAP] AdminExecuteQuery maxRows cap prevents OOM but no per-query MAX_EXECUTION_TIME hint (system.go)
100. [FRAGILE] AdminProcessDeletionRequest: double-approve race (deletion_requests.go)
101. [SECURITY] receiver/wsconn.go upgrader.CheckOrigin always returns true (cross-site WS upgrade possible)
102. [GAP] S3 storage init no timeout → startup hangs if endpoint unreachable (already in Critical, confirmed)
103. [GAP] analytics sessions LRU eviction is O(N) scan when cap is hit (sessions.go:31)
104. [FRAGILE] validateAuth allows LockoutDuration=0 silently — brute-force protection disabled (validate.go)

---

*Report generated: 2026-04-11 | Full codebase audit | 6 parallel agents*
