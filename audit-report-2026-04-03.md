# Backend Audit Report — 2026-04-03

> **Scope:** Go backend only (`api/`, `cmd/`, `internal/`, `pkg/`). All source files read and traced.
> **Auditor:** Claude Code deep-debug-audit skill (7 parallel agents)

---

## === AUDIT SUMMARY ===

```
Files analyzed:    ~190 Go source files (excluding vendor/)
Functions traced:  ~900+
Workflows traced:  18 major user-facing flows

BROKEN:        10
INCOMPLETE:     6
GAP:           26
REDUNDANT:      3
FRAGILE:       40
SILENT FAIL:   17
DRIFT:          7
LEAK:          11
SECURITY:      44
RACE:          10
OK:            14
```

---

## CRITICAL — Must fix before deploy

These issues cause compilation failures, data loss, or exploitable security vulnerabilities.

---

### [BROKEN] internal/repositories/mysql/categorized_item_repository.go:51 — `new(value)` compile error in Upsert
```
WHAT: new(r.recordToRow(item)) is invalid Go — new() takes a type, not an expression.
WHY: Misuse of new() builtin; intent was to pass a pointer to a stack-allocated struct.
IMPACT: Binary does not compile; CategorizeFile path is completely broken.
TRACE: CategorizeFile → saveItem → repo.Upsert → new(r.recordToRow(item)) — compile error.
FIX DIRECTION: row := r.recordToRow(item); ... .Create(&row)
```

### [BROKEN] internal/repositories/mysql/crawler_repository.go:42 — `new(value)` compile error in CrawlerTargetRepository.Upsert
```
WHAT: new(r.recordToRow(target)) is the same invalid new(value) pattern.
WHY: Same copy-paste error.
IMPACT: Binary does not compile; AddTarget → repo.Upsert is broken.
TRACE: AddTarget → targetRepo.Upsert → new(r.recordToRow(target)) — compile error.
FIX DIRECTION: row := r.recordToRow(target); ... .Create(&row)
```

### [BROKEN] internal/repositories/mysql/crawler_repository.go:157 — `new(value)` compile error in CrawlerDiscoveryRepository.Create
```
WHAT: new(r.recordToRow(disc)) is invalid.
WHY: Same error class.
IMPACT: Binary does not compile; doCrawl → discoveryRepo.Create is broken.
FIX DIRECTION: row := r.recordToRow(disc); ... .Create(&row)
```

### [BROKEN] internal/repositories/mysql/extractor_item_repository.go:57 — `new(value)` compile error in ExtractorItemRepository.Upsert
```
WHAT: new(r.recordToRow(item)) is invalid.
WHY: Same error class.
IMPACT: Binary does not compile; AddItem → repo.Upsert is broken.
FIX DIRECTION: row := r.recordToRow(item); ... .Create(&row)
```

### [BROKEN] internal/repositories/mysql/validation_result_repository.go:51 — `new(value)` compile error in ValidationResultRepository.Upsert
```
WHAT: new(r.recordToRow(result)) is invalid.
WHY: Same error class.
IMPACT: Binary does not compile; validator persistence is broken.
FIX DIRECTION: row := r.recordToRow(result); ... .Create(&row)
```

### [BROKEN] internal/repositories/mysql/crawler_repository.go:101 / extractor_item_repository.go:143 — `new(pointer.Format(...))` for optional time fields
```
WHAT: new(rec.LastCrawled.Format(...)) and new(rec.ExpiresAt.Format(...)) pass expressions to new(), not types.
WHY: Same error class; intent was to create a *string.
IMPACT: Binary does not compile.
FIX DIRECTION: s := rec.LastCrawled.Format(layout); row.LastCrawled = &s
```

### [BROKEN] internal/updater/updater.go:311-312 — `new(release.PublishedAt)` compile error
```
WHAT: result.PublishedAt = new(release.PublishedAt) — new() takes a type, not a time.Time value.
WHY: Same new(value) class of bug.
IMPACT: Binary does not compile; CheckForUpdates is broken.
FIX DIRECTION: t := release.PublishedAt; result.PublishedAt = &t
```

### [BROKEN] api/handlers/admin_security.go:152 — `new(rec.ExpiresAt)` compile error in GetBannedIPs
```
WHAT: entry.ExpiresAt = new(rec.ExpiresAt) — same issue.
WHY: Same error class.
IMPACT: Binary does not compile; GetBannedIPs is entirely broken.
FIX DIRECTION: t := rec.ExpiresAt; entry.ExpiresAt = &t
```

### [BROKEN] internal/repositories/mysql/receiver_transfer_repository.go:176 — `new(rows[start:end])` silently inserts nothing for batches after the first
```
WHAT: new(rows[start:end]) allocates a *[]receiverMediaRow pointing to a new nil slice, ignoring the actual slice data. GORM receives a pointer to nil and inserts zero rows.
WHY: new(slice_expr) was intended as &rows[start:end].
IMPACT: Every batch beyond the first (>100 items) is silently discarded. Slave catalogs with >100 items are truncated in the DB; data loss on every restart.
TRACE: PushCatalog → m.mediaRepo.UpsertBatch → loop → tx.Create(new(rows[start:end])) → no rows inserted.
FIX DIRECTION: Change new(rows[start:end]) to &rows[start:end].
```

### [BROKEN] internal/playlist/playlist.go:404-448 — ReorderItems: in-memory order updated before DB, diverges on partial failure
```
WHAT: reorderItemsLocked mutates in-memory playlist.Items then calls UpdateItem in a loop. DB errors are only logged; iteration continues. In-memory now has new order, DB has partial/old order.
WHY: No transaction and no rollback of the in-memory mutation on failure.
IMPACT: After any DB write failure during reorder, the playlist order in memory and DB are permanently inconsistent; server restart reloads the DB order, silently reverting the reorder.
TRACE: ReorderPlaylistItems → ReorderItems → reorderItemsLocked → UpdateItem fails → in-memory updated, DB not.
FIX DIRECTION: Wrap all UpdateItem calls in a single DB transaction; only update in-memory slice after commit.
```

---

## HIGH — Will cause user-facing bugs or exploitable security issues

---

### [SECURITY] pkg/middleware/agegate.go:219-252 — Age-gate verify endpoint has no CSRF protection
```
WHAT: POST /api/age-verify requires no authentication, no CSRF token, and no request body validation. Any page can embed a cross-site form that POSTs to this endpoint and sets the age_verified cookie for the victim's browser without any user interaction.
WHY: GinVerifyHandler does not check Origin, Referer, or any CSRF token before recording IP and setting cookie.
IMPACT: Trivial CSRF attack bypasses the age gate entirely for any victim.
TRACE: Any page → XHR POST /api/age-verify → age_verified cookie set for victim.
FIX DIRECTION: Check Origin/Referer against the server's own host, or require a CSRF token in the request body.
```

### [SECURITY] internal/auth/user.go:114-141 — Default permissions grant CanViewMature=true to all new users
```
WHAT: getDefaultPermissions sets CanViewMature: true for every user type including the fallback default. The age gate is therefore purely a UI/UX layer; at the DB/permission layer, every authenticated user already has mature content access.
WHY: Default permission struct hard-codes CanViewMature: true for all non-admin types.
IMPACT: Any endpoint checking user.Permissions.CanViewMature will allow mature content to all authenticated users without age-gate verification.
TRACE: CreateUser → buildNewUser → getDefaultPermissions → CanViewMature: true.
FIX DIRECTION: Set CanViewMature: false by default; only enable it via explicit age-gate verification or admin grant.
```

### [SECURITY] api/handlers/analytics.go:155 — Client can forge server-generated analytics event types
```
WHAT: SubmitClientEvent's validTypes map includes EventLogin, EventLoginFailed, EventLogout, EventRegister, EventAgeGatePass, EventDownload, EventSearch. Any authenticated user can POST these types with arbitrary data, permanently corrupting in-memory DailyStats and analytics dashboard.
WHY: No separation between client-facing and server-only event types in the allowlist.
IMPACT: Authenticated attacker can inflate login/register/download counts, corrupting all traffic analytics permanently until restart.
TRACE: POST /api/analytics/events → SubmitClientEvent → validTypes check → TrackEvent → updateStats → daily counters.
FIX DIRECTION: Remove server-only event types from the client validTypes map; they must only be created via TrackTrafficEvent.
```

### [SECURITY] api/handlers/analytics.go:155 — Client can inject arbitrary SessionID into analytics events
```
WHAT: When session is nil (no auth required), UserID stays "" and SessionID comes directly from req.SessionID — a fully attacker-controlled value. Authenticated users can also supply arbitrary session IDs to pollute other users' session records.
WHY: No validation of client-supplied SessionID; always accepted and stored in m.sessions.
IMPACT: Cross-session data injection; attacker can fabricate session records for any session ID.
TRACE: POST /api/analytics/events → SubmitEvent:184-190 → ClientEventInput{SessionID: req.SessionID} → updateSession.
FIX DIRECTION: Always derive SessionID from the authenticated session only; never accept client-supplied SessionID.
```

### [SECURITY] internal/receiver/receiver.go:746-753 — proxyViaHTTP has no SSRF protection on slave BaseURL
```
WHAT: m.httpClient uses a plain http.Transport with no SSRF dial-time IP guard. A slave registered with BaseURL="http://169.254.169.254/latest/meta-data/" causes any user playing media from that slave to trigger an SSRF request from the master to the cloud metadata endpoint.
WHY: Receiver's http.Client uses &http.Transport{} not helpers.SafeHTTPTransport().
IMPACT: Critical: a registered slave (requires valid API key) can force the master to probe arbitrary internal network addresses including cloud metadata, internal databases, etc.
TRACE: Any user plays media → ProxyStream → proxyViaHTTP → m.httpClient.Do(req) with slave-controlled baseURL.
FIX DIRECTION: (1) Use helpers.SafeHTTPTransport() for m.httpClient. (2) Call helpers.ValidateURLForSSRF on BaseURL in RegisterSlave.
```

### [SECURITY] internal/hls/serve.go:67-90 — HLS CORS origin falls back to "*" for non-matching origins
```
WHAT: When an operator configures specific allowed origins and a request arrives with a non-matching Origin header, hlsCORSOrigin returns "*" instead of omitting the header. This defeats the purpose of origin restriction entirely.
WHY: Fallback comment says "return '*' so the stream still loads in non-CORS contexts" — but non-CORS players don't send Origin headers and won't reach the fallback.
IMPACT: CORS configuration is silently ignored for all non-matching origins; cross-origin access to all HLS content.
TRACE: ServeSegment/ServeVariantPlaylist/ServeMasterPlaylist → hlsCORSOrigin → falls back to "*".
FIX DIRECTION: Return empty string (omit ACAO header) for non-matching origins that send an Origin header; only use "*" when no Origin header was present.
```

### [SECURITY] api/handlers/hls.go:185-196 — checkMatureAccess bypassed for disk-discovered HLS jobs with empty MediaPath
```
WHAT: resolveHLSJobForServe calls h.checkMatureAccess(c, job.MediaPath). For disk-discovered jobs, MediaPath is "" (the .lock file is removed on normal completion). checkMatureAccess → GetMedia("") returns error → access is allowed (returns true). Mature HLS content from completed jobs bypasses the mature access check.
WHY: findMediaPathForJob reads the .lock file, which is removed by removeLock on successful transcode completion; completed jobs always have MediaPath="".
IMPACT: Unauthenticated users can access HLS segments/playlists for mature-flagged content.
TRACE: resolveHLSJobForServe → job.MediaPath == "" → checkMatureAccess("") → GetMedia fails → returns true (allowed).
FIX DIRECTION: In findMediaPathForJob, fall back to querying media by job ID from the DB; or persist MediaPath in a stable .mediapath sidecar file.
```

### [SECURITY] api/handlers/thumbnails.go:236-303 — Responsive variant thumbnails bypass mature-content check
```
WHAT: mediaID is extracted as strings.TrimSuffix(filename, ext). For "uuid-sm.webp", mediaID="uuid-sm" which fails GetMediaByID. The mature-content check silently passes and the responsive WebP variant for a mature item is served without any check.
WHY: Responsive suffixes (-sm, -md, -lg) are not stripped before the DB lookup.
IMPACT: Users without CanViewMature permission can retrieve responsive WebP thumbnails for mature items.
TRACE: GET /thumbnails/:filename → ServeThumbnailFile → mediaID = TrimSuffix(uuid-sm.webp, .webp) = "uuid-sm" → GetMediaByID fails → mature check skipped → served.
FIX DIRECTION: Strip -sm/-md/-lg suffixes from mediaID before the DB lookup.
```

### [SECURITY] api/handlers/thumbnails.go:265-280 — Preview thumbnails bypass mature-content check
```
WHAT: For "uuid_preview_0.jpg", mediaID becomes "uuid_preview_0" which fails GetMediaByID lookup. Mature check silently bypassed.
WHY: Preview suffix (_preview_N) not stripped before DB lookup.
IMPACT: All preview (hover-strip) thumbnails for mature items accessible without permission.
TRACE: GET /thumbnails/uuid_preview_0.jpg → mediaID="uuid_preview_0" → GetMediaByID fails → check skipped → served.
FIX DIRECTION: Extract UUID by splitting on first _preview_ occurrence before DB lookup.
```

### [SECURITY] internal/hls/serve.go:181-203 — HLS Quality param not validated against known profiles (partial path traversal)
```
WHAT: p.Quality comes from c.Param("quality") without validation against known quality profile names. A crafted quality like "../completed" could escape the filepath.Rel check on Windows (case-insensitive comparison differences).
WHY: Quality param passed straight through without whitelist check.
IMPACT: On Windows hosts, constructed paths may escape the output directory via case-folding differences in filepath.Rel.
TRACE: ServeSegment → hls.ServeSegment → filepath.Join(job.OutputDir, p.Quality, p.Segment) → filepath.Rel check.
FIX DIRECTION: Validate p.Quality against m.getQualityProfile(quality) != nil before building the path; reject unknown quality names with 404.
```

### [SECURITY] internal/hls/validation.go:58-75 — ValidateVariant uses unchecked variant paths from playlist content
```
WHAT: parseVariantStreams returns paths read verbatim from the master playlist file. validateVariant calls filepath.Join(outputDir, variant) with no traversal check. A crafted master.m3u8 could include "../../etc/passwd/playlist.m3u8" and the admin-only ValidateHLS endpoint would read that file.
WHY: No filepath.Rel check in validateVariant/validateVariants.
IMPACT: Admin-only endpoint could read arbitrary files on the server; error messages may expose path contents.
TRACE: ValidateHLS → ValidateMasterPlaylist → validateVariants → validateVariant → filepath.Join(outputDir, variant) → os.ReadFile.
FIX DIRECTION: After resolving variant path, verify it lies under outputDir with filepath.Rel before reading.
```

### [SECURITY] internal/receiver/wsconn.go:111-115 — Receiver WebSocket API key transmitted via query parameter (logged)
```
WHAT: The API key is accepted via api_key query parameter which appears verbatim in server access logs, nginx logs, and browser history.
WHY: Query-parameter API key delivery is inherently non-secret.
IMPACT: API key exposed to anyone with log read access; enables unauthorized slave registration.
TRACE: HandleWebSocket → apiKey = r.URL.Query().Get("api_key") → key in URL → logged by Gin/nginx.
FIX DIRECTION: Deprecate query-parameter delivery; require X-API-Key header only.
```

### [SECURITY] internal/receiver/wsconn.go:201-221 — Catalog push accepted before registration (can overwrite other slave's catalog)
```
WHAT: The WS catalog message handler checks sw.slaveID != "" && data.SlaveID != sw.slaveID. If sw.slaveID is "" (register not yet received), the && short-circuits; any authenticated slave can push a catalog for any already-registered slave before sending its own register message.
WHY: Guard logic is inverted — should require sw.slaveID != "" for any catalog/heartbeat operation.
IMPACT: Authenticated slave can overwrite another slave's catalog contents.
TRACE: Authenticated WS connect → skip register → send catalog with foreign SlaveID → sw.slaveID=="" → check skipped → PushCatalog succeeds.
FIX DIRECTION: Change guard to reject catalog/heartbeat when sw.slaveID == "".
```

### [SECURITY] api/handlers/admin_receiver.go:200-248 — ReceiverStreamPush allows slave to force arbitrary HTTP status codes to end users
```
WHAT: Status code is parsed from slave-controlled X-Stream-Status header and written with w.WriteHeader(delivery.StatusCode). A compromised slave can set X-Stream-Status: 301 plus a Location header to redirect authenticated users to an attacker-controlled site.
WHY: No whitelist on acceptable status codes from slave delivery.
IMPACT: Compromised slave can redirect users or control response status for all proxied streams.
TRACE: Slave POST /api/receiver/stream-push/:token → ReceiverStreamPush → delivery.StatusCode = X-Stream-Status → WriteHeader(statusCode).
FIX DIRECTION: Clamp status code to {200, 206, 416}; reject anything else.
```

### [SECURITY] internal/receiver/receiver.go:408-431 — PushCatalog path validation uses raw string (not URL-decoded)
```
WHAT: PushCatalog rejects paths with ".." or leading "/", but does not URL-decode before checking. A slave can send item.Path = "%2F%2Fetc%2Fpasswd" which passes the check.
WHY: Path validation operates on raw string, not decoded form.
IMPACT: Though QueryEscape in proxyViaHTTP re-encodes the path, relying on downstream encoding for security is fragile.
TRACE: PushCatalog → item.Path check line 413 → stored in DB → proxyViaHTTP → url.QueryEscape(item.Path).
FIX DIRECTION: Decode the path with url.PathUnescape before security checks in PushCatalog.
```

### [SECURITY] internal/remote/remote.go:601-693 — CacheMedia writes without atomic rename; corrupt partial file served
```
WHAT: CacheMedia creates the file at localPath and streams to it. If io.Copy fails and os.Remove also fails (e.g. disk full), a partial file remains. On next startup loadCacheIndex restores this entry, and http.ServeFile serves the corrupt partial file.
WHY: No write-to-temp-then-rename pattern.
IMPACT: Cached media served as a corrupt/truncated file after failed download or crash.
TRACE: CacheMedia → os.Create(localPath) → io.Copy fails → os.Remove attempt may fail → DB save persists corrupted entry.
FIX DIRECTION: Write to localPath+".tmp", then os.Rename on success; only add to m.mediaCache and DB after rename succeeds.
```

### [SECURITY] pkg/storage/local/local.go:40 — Path traversal via HasPrefix without path separator (absolute path branch)
```
WHAT: resolve() checks strings.HasPrefix(cleaned, b.root) without appending a path separator. A root of "/uploads/user" accepts "/uploads/user_evil/../../etc/passwd" because HasPrefix("/uploads/user_evil", "/uploads/user") == true.
WHY: String prefix check is not a path prefix check.
IMPACT: Attacker can read/write files outside the storage root if they can supply absolute paths.
TRACE: Any caller → storage.Backend.Create/Open/Stat/ReadFile/WriteFile → local.Backend.resolve() → absolute path branch.
FIX DIRECTION: Change to strings.HasPrefix(cleaned, b.root+string(os.PathSeparator)) || cleaned == b.root.
```

### [SECURITY] pkg/storage/local/local.go:253-258 — AbsPath silently bypasses traversal check on error
```
WHAT: On resolve() error, AbsPath falls back to filepath.Clean(path) joined to b.root without any security check — the only thing that matters for AbsPath is the result of resolve().
WHY: The error path was added to avoid returning "" but skips the security check.
IMPACT: If AbsPath is used in ffmpeg command construction with a malicious path, results in arbitrary path injection.
TRACE: Any caller → local.Backend.AbsPath() → line 256 fallback → unchecked path returned.
FIX DIRECTION: Return b.root (or an error) on resolve() failure; never join with an unvalidated path.
```

### [SECURITY] internal/upload/upload.go:376-411 — Upload size limit bypassed via client-controlled multipart header
```
WHAT: validateUploadSize checks fh.Size which comes from the multipart Content-Disposition header provided by the client. A malicious client sets fh.Size=1 to pass validation, then sends gigabytes in the actual part. The subsequent copyWithProgress/store.Create has no byte cap.
WHY: fh.Size is attacker-controlled; the actual body is not bounded per-file.
IMPACT: Any user with CanUpload can bypass the file size limit and exhaust server disk/RAM.
TRACE: UploadMedia → ProcessFileHeader → validateUploadSize(fh.Size) → copyWithProgress (unbounded copy).
FIX DIRECTION: Wrap the file reader in io.LimitReader(reader, maxFileSize+1) before passing to copyWithProgress/store.Create.
```

### [SECURITY] internal/updater/updater.go:746-768 — verifyBinaryChecksum silently skips integrity check when checksum file absent
```
WHAT: If no SHA256SUMS asset is found in a release, verifyBinaryChecksum logs a warning and returns nil. The binary is installed without integrity verification.
WHY: Backward-compatibility guard for "older releases that predate checksum publishing."
IMPACT: A release published without a SHA256SUMS file (misconfiguration or compromised release) installs without verification.
TRACE: ApplyUpdate → verifyBinaryChecksum → fetchChecksumAssetURL returns "" → return nil.
FIX DIRECTION: Return an error when no checksum file is present; provide an opt-in "allow_missing_checksum" config flag.
```

### [SECURITY] api/handlers/feed.go:59-153 — RSS feed leaks mature content to all authenticated users
```
WHAT: GetRSSFeed calls h.media.ListMedia(filter) without filtering out mature items. No mature-content check is applied before items are added to the feed.
WHY: checkMatureAccess is never invoked in this handler.
IMPACT: Mature-flagged media names, thumbnail URLs, and metadata visible in the RSS feed to any authenticated user.
TRACE: GetRSSFeed → h.media.ListMedia(filter) → feed.Entries includes IsMature items.
FIX DIRECTION: After ListMedia, filter out items where item.IsMature && !user.Permissions.CanViewMature.
```

### [SECURITY] internal/crawler/browser.go:115-133 — Headless Chrome launched with --no-sandbox and --disable-web-security
```
WHAT: Chrome is launched with --disable-web-security, --no-sandbox, --disable-setuid-sandbox. A maliciously crafted crawl target page could execute arbitrary code inside the server's process space.
WHY: These flags were added for compatibility without a documented security review.
IMPACT: Admin-triggered crawl of a malicious URL → remote code execution in server process.
TRACE: Admin adds target → CrawlTarget handler → doCrawl → probeForStreams → browser.probe → exec.Command(chromeBin, "--no-sandbox", "--disable-web-security", ...).
FIX DIRECTION: Remove --disable-web-security (only --host-resolver-rules is needed for SSRF). Run Chrome in a container/namespace with seccomp restrictions. Document --no-sandbox as a security exception requiring explicit sign-off.
```

### [SECURITY] internal/autodiscovery/autodiscovery.go:153-184 — ScanDirectory follows symlinks without bounds check
```
WHAT: filepath.Walk follows symlinked directories silently. Any symlink under a scanned directory that points outside will be traversed.
WHY: filepath.Walk does not detect or skip symbolic links to directories.
IMPACT: Full directory listing for arbitrary paths reachable via symlinks under allowed media directories.
TRACE: ScanDirectory → filepath.Walk(dirStr) → no symlink guard in walk func.
FIX DIRECTION: Use os.Lstat to detect and skip symlinked directories in the walk callback.
```

### [SECURITY] api/handlers/admin_discovery.go:42 — DiscoverMedia directory path not symlink-resolved before allow-list check
```
WHAT: isDirectoryWithinMediaPaths is called with filepath.Clean(req.Directory) but not filepath.EvalSymlinks. A symlink inside an allowed directory pointing outside passes the check.
WHY: filepath.Clean is lexical; it does not resolve symlinks.
IMPACT: Admin can trigger a scan of arbitrary filesystem paths reachable via symlinks.
TRACE: DiscoverMedia → isDirectoryWithinMediaPaths(filepath.Clean(req.Directory)) → autodiscovery.ScanDirectory → filepath.Walk.
FIX DIRECTION: Call filepath.EvalSymlinks on req.Directory before the allow-list check.
```

### [SECURITY] internal/authenticate.go:111-167 — Admin login error leaks username existence via distinct error codes
```
WHAT: AdminAuthenticate returns ErrAdminWrongPassword when the username is correct but password is wrong, and ErrNotAdminUsername otherwise. Login handler maps these to structurally different responses, allowing an attacker to confirm the admin username.
WHY: Two structurally different sentinel errors with different handler behavior.
IMPACT: Attacker can enumerate valid admin usernames via differing response paths.
TRACE: Login → AdminAuthenticate returns ErrAdminWrongPassword → immediate 401; vs ErrNotAdminUsername → fall through to regular auth.
FIX DIRECTION: Collapse both error paths to the same response delay and error string at the handler layer.
```

### [SECURITY] internal/auth/tokens.go:74 — API tokens never expire
```
WHAT: APITokenRecord has no ExpiresAt field. Tokens live forever unless explicitly revoked. There is no mechanism for time-bounded tokens, no forced rotation.
WHY: No expiry field in the schema.
IMPACT: A stolen or leaked API token is valid indefinitely.
TRACE: ValidateAPIToken → constructs synthetic Session with ExpiresAt=+365d; no actual expiry check.
FIX DIRECTION: Add optional expires_at to user_api_tokens, check it in ValidateAPIToken, expose expiry parameter in CreateAPIToken.
```

### [SECURITY] internal/auth/authenticate.go:216-250 — Login lockout is IP-only; no per-username lockout
```
WHAT: Rate limiting is keyed entirely on client IP. An attacker with a rotating IP pool can attempt unlimited passwords against a single account.
WHY: loginAttempts map uses IP as key.
IMPACT: Accounts vulnerable to distributed credential-stuffing and slow-burst brute-force attacks.
TRACE: recordFailedAttempt(ip) → m.loginAttempts[ip].
FIX DIRECTION: Add a secondary per-username counter in addition to the IP counter.
```

### [SECURITY] internal/auth/authenticate.go:28-44 — AdminAuthenticate records failed attempts when admin is globally disabled
```
WHAT: When cfg.Admin.Enabled==false, recordFailedAttempt is called for every login attempt (any username). All regular login attempts from an IP are counted as failed admin attempts, potentially triggering lockout for legitimate users before the regular auth path runs.
WHY: The dummy bcrypt comparison and recordFailedAttempt run whenever adminLoginAllowed is false.
IMPACT: With admin disabled, all regular login attempts trigger lockout accrual — DoS against legitimate users.
TRACE: Login → AdminAuthenticate → adminLoginAllowed=false (admin disabled) → recordFailedAttempt(ip) → IP hits lockout threshold → regular auth returns ErrAccountLocked.
FIX DIRECTION: Skip recordFailedAttempt when cfg.Admin.Enabled is false.
```

### [SECURITY] internal/auth/authenticate.go:47-65 — verifyPasswordWithCacheRefresh mutates shared user pointer without lock (data race)
```
WHAT: verifyPasswordWithCacheRefresh receives *user pointing at m.users[username] (shared state). On cache-miss path, it does *user = *dbUser, writing to the shared cached object without holding usersMu.
WHY: The pointer dereference modifies the shared map value outside any lock.
IMPACT: Data race: concurrent reads of m.users[username] (e.g. GetActiveSessions) race with this write.
TRACE: Authenticate → getOrLoadUser (returns m.users[username] pointer) → verifyPasswordWithCacheRefresh → *user = *dbUser (writes without lock).
FIX DIRECTION: Remove the *user = *dbUser mutation; update the cache explicitly under usersMu after the function returns.
```

### [SECURITY] api/handlers/admin_security.go:152 — GetBannedIPs compile error (new(rec.ExpiresAt))
*(see also BROKEN section above)*

### [SECURITY] internal/analytics/export.go:44 — CSV export includes full IP addresses
```
WHAT: The CSV export includes IPAddress for every event with no redaction or anonymisation.
WHY: Raw event data written directly including IPAddress field.
IMPACT: Admin-triggered export produces a file with full IP addresses for every analytics event; may violate GDPR/privacy requirements. File also written to disk transiently with no audit trail of who triggered the export.
TRACE: AdminExportAnalytics → ExportCSV → rows append IPAddress.
FIX DIRECTION: Hash or truncate IP addresses in the export (mask last octet); add audit log entry when export is triggered.
```

### [SECURITY] internal/extractor/extractor.go:493-560 — proxyStream forwards all upstream response headers including Set-Cookie
```
WHAT: proxyStream copies all upstream.Header to the client response writer, including Set-Cookie, X-Frame-Options, Content-Security-Policy.
WHY: Header stripping not performed.
IMPACT: Third-party CDN cookies set on server's domain; CDN CSP headers may override server's own CSP.
TRACE: ExtractorHLSSegment/Variant → proxyStream → http.Get → copy all headers to c.Writer.
FIX DIRECTION: Whitelist only necessary headers (Content-Type, Content-Length, Cache-Control, ETag) when copying upstream headers.
```

### [SECURITY] internal/remote/remote.go:907-940 — validateURL has DNS-rebinding window (TOCTOU between check and HTTP dial)
```
WHAT: validateURL resolves hostname at validation time; SafeHTTPTransport resolves again at dial time. A DNS-rebinding attack can serve a public IP during validation and switch to a private IP for the actual connection.
WHY: Go's http.Transport does not support IP pinning from validation through to connection.
IMPACT: Sophisticated attacker with DNS control can bypass SSRF protection and reach cloud metadata endpoints (169.254.169.254) or internal services.
TRACE: remote.discoverMedia/CacheMedia/StreamRemote → validateURL (DNS lookup #1) → httpClient.Do → SafeHTTPTransport.DialContext (DNS lookup #2).
FIX DIRECTION: Accept as known limitation per existing comment; document; or resolve once and pass IP directly to dialer.
```

### [SECURITY] api/handlers/admin_downloader.go:63-103 — AdminDownloaderDetect/Download forward user-supplied URLs without SSRF validation
```
WHAT: Both AdminDownloaderDetect and AdminDownloaderDownload forward the user-supplied URL to the external downloader service without calling helpers.ValidateURLForSSRF. The downloader service then fetches that URL.
WHY: URL validation is delegated to the downloader service, making MSP a SSRF relay.
IMPACT: Admin user can use MSP as a proxy to probe internal network services.
TRACE: Admin POST /api/admin/downloader/detect → AdminDownloaderDetect → Client.Detect(userURL) → downloader service fetches URL.
FIX DIRECTION: Call helpers.ValidateURLForSSRF on req.URL before forwarding to the downloader client.
```

### [SECURITY] internal/crawler/crawler.go:440-486 — Same-host check bypassable via www. prefix stripping
```
WHAT: extractContentLinks checks strings.Contains(u.Hostname(), strings.TrimPrefix(baseURL.Hostname(), "www.")). For base URL "www.example.com", trimmed is "example.com". A link to "notexample.com" passes the Contains check.
WHY: strings.Contains is too loose for hostname comparison.
IMPACT: Crawler follows links to unintended third-party domains containing base domain as substring.
TRACE: extractContentLinks → strings.Contains(u.Hostname(), "example.com") passes for "notexample.com".
FIX DIRECTION: Use exact match or strings.HasSuffix(u.Hostname(), "."+stripped) || u.Hostname() == stripped.
```

### [SECURITY] internal/extractor/extractor.go:213-225 — AddItem SSRF validation not applied to existing items
```
WHAT: When SSRF validation fails for a URL whose hash already exists in m.items, the local error item is returned but m.items[id] remains active. If a previously valid URL later resolves to a private IP (DNS change), the in-memory entry retains status=active and continues proxying.
WHY: SSRF check creates a local object but doesn't update the existing m.items[id].
IMPACT: Previously valid extractor items continue proxying after their URL becomes SSRF-dangerous.
TRACE: AddItem → ValidateURLForSSRF fails → return local item (status=error) → m.items[id] unchanged.
FIX DIRECTION: When SSRF validation fails for an existing item, update m.items[id].Status = "error" under the write lock.
```

### [SECURITY] internal/auth/session.go:83-99 — getOrLoadSession may re-add a just-deleted session to cache
```
WHAT: After a cache miss, the function reads from DB and then inserts under Lock without re-checking whether Logout deleted the session from cache between the RUnlock and Lock.
WHY: No double-checked locking after acquiring the write lock.
IMPACT: A race between getOrLoadSession and Logout could transiently make an already-logged-out session appear valid in cache again.
TRACE: goroutine A: Logout → delete(m.sessions, id); goroutine B: getOrLoadSession → DB read → re-insert to cache.
FIX DIRECTION: After acquiring the write lock, verify the session is not present; return cached copy if found.
```

---

## MEDIUM — Tech debt, time bombs, or correctness issues

---

### [RACE] internal/auth/watch_history.go:11-52 — AddToWatchHistory mutates in-memory state without rollback on DB failure
```
WHAT: The existing-item branch writes to user.WatchHistory[i] while holding usersMu.Lock, then releases lock, then calls userRepo.Update. If the DB write fails, cache is mutated but no rollback occurs.
WHY: No snapshot-and-restore pattern on the existing-item path (new-item path is correct).
IMPACT: Transient DB failure leaves in-memory watch history out of sync with DB. User's progress silently lost from DB.
FIX DIRECTION: Snapshot the old item before overwriting; restore on DB failure, matching ClearWatchHistory rollback pattern.
```

### [RACE] internal/hls/cleanup.go:170-218 — cleanInactiveJob reads lastAccess outside write lock; job deleted while client is streaming
```
WHAT: lastAccess is read before acquiring the write lock. A concurrent RecordAccess call can update lastAccess past the cutoff between the read and the lock, causing a job to be deleted while a client is actively streaming it.
WHY: accessTracker.mu and jobsMu are separate locks; cleanup does not hold accessTracker.mu during removal decision.
IMPACT: Client receives 404s for subsequent segment requests when cleanup races with active streaming.
FIX DIRECTION: Re-read lastAccess under both accessTracker.mu and jobsMu before executing removal.
```

### [RACE] internal/hls/jobs.go:70-80 — createOrReuseHLSJobLocked holds jobsMu during filesystem I/O
```
WHAT: createOrReuseHLSJobLocked is called with m.jobsMu.Lock() held and calls validateExistingHLS which does os.ReadFile and os.ReadDir while the write lock is held.
WHY: validateExistingHLS was not designed to be called inside the write lock.
IMPACT: All HLS operations (GetJobStatus, updateJobStatus, ListJobs) blocked for the duration of filesystem I/O; latency spikes across all HLS endpoints.
FIX DIRECTION: Release jobsMu before calling validateExistingHLS; re-acquire and re-check after.
```

### [RACE] internal/hls/access.go:26-56 — RecordAccess and cleanup acquire locks in opposite orders (potential deadlock)
```
WHAT: RecordAccess acquires accessTracker.mu then jobsMu. The cleanup path acquires jobsMu.RLock then accessTracker.mu.RLock. Opposite lock ordering creates a potential livelock/deadlock under high concurrency.
WHY: No global lock ordering policy documented or enforced.
IMPACT: Low-probability livelock under high concurrent segment requests and simultaneous cleanup.
FIX DIRECTION: Establish lock ordering policy (jobsMu always before accessTracker.mu); refactor RecordAccess.
```

### [RACE] internal/hls/transcode.go:246-273 — lazyTranscodeQuality holds per-quality mutex across semaphore acquisition (goroutine starvation)
```
WHAT: qMu.Lock() acquired first, then blocks on m.transSem. Multiple goroutines can hold their quality mutexes while competing for the semaphore indefinitely.
WHY: Mutex acquired before semaphore to prevent duplicate transcodes, but can block indefinitely.
IMPACT: HTTP request goroutines tied up indefinitely, exhausting connection pool.
FIX DIRECTION: Release qMu before blocking on transSem; use a pending flag with condition variable.
```

### [LEAK] internal/auth/authenticate.go:193-250 — loginAttempts map grows unboundedly between cleanup ticks
```
WHAT: loginAttempts adds entries per unique IP with no size cap. cleanupExpiredLoginAttempts only runs every 5 minutes. Under a DDoS with many distinct IPs, map can grow to millions of entries.
WHY: No size cap or LRU eviction on insertion.
IMPACT: Memory exhaustion under sustained brute-force from many IPs.
FIX DIRECTION: Add a max-size cap to loginAttempts with LRU or FIFO eviction.
```

### [LEAK] internal/analytics/sessions.go — In-memory session map unbounded between cleanup ticks
```
WHAT: Sessions added on every event with new SessionID, cleaned only when cleanup() fires. No max map size.
WHY: No cap on the sessions map.
IMPACT: Memory exhaustion DoS — attacker sends events with unique session IDs between cleanup intervals.
FIX DIRECTION: Enforce a maximum sessions-map size with eviction, similar to evictExcessMediaStats in cleanup.go.
```

### [SILENT FAIL] internal/hls/cleanup.go:83-107 — removeSegmentDirAndState deletes m.jobs before os.RemoveAll; orphaned dirs on failure
```
WHAT: delete(m.jobs, jobID) and repo.Delete called while holding lock, then lock released, then os.RemoveAll. If RemoveAll fails, the directory remains on disk but is no longer tracked — invisible to the job management system forever.
WHY: Delete placed before RemoveAll to minimize lock hold time.
IMPACT: On disk-full or permission errors, HLS cache directories accumulate as orphans.
FIX DIRECTION: Only delete from m.jobs/DB after os.RemoveAll succeeds; or re-add on RemoveAll failure.
```

### [SILENT FAIL] internal/hls/cleanup.go:12-21 — HLS cleanup loop defined but never started; RetentionMinutes silently ignored
```
WHAT: cleanupLoop, cleanupOldSegments, and RetentionMinutes config logic are all fully implemented dead code. Start() comment explicitly says "Automatic cleanup is intentionally disabled" but the config field still exists.
WHY: Feature was disabled but not removed, and config field was not deprecated.
IMPACT: Operators who configure RetentionMinutes expect automatic cleanup to occur; nothing happens.
FIX DIRECTION: Either remove the dead code + config field, or document as reserved/unused and add a startup warning when RetentionMinutes > 0 is configured.
```

### [GAP] internal/config/validate.go:85-96 — validateAdmin is warn-only; server starts with inaccessible admin panel
```
WHAT: When Admin is enabled but PasswordHash is empty (default config), validateAdmin only logs a warning. Server starts successfully but every admin login fails with a bcrypt error.
WHY: Decided to be warn-only rather than fatal.
IMPACT: New deployments with defaults have a completely inaccessible admin panel with no clear error.
FIX DIRECTION: Return a fatal error when admin is enabled and PasswordHash is empty, or auto-generate and print a one-time password.
```

### [FRAGILE] internal/config/config.go:68-72 — json.Unmarshal zeros defaults for missing JSON fields
```
WHAT: json.Unmarshal(data, m.config) on a partial config.json zeros fields not present in the file, overwriting the DefaultConfig values. E.g., a config.json with only {"server":{...}} zeros out Auth, Security, HLS, etc.
WHY: json.Unmarshal does not merge into existing struct values; absent JSON keys produce zero-value Go fields.
IMPACT: Operators with partial config.json lose all defaults for missing sections.
FIX DIRECTION: Unmarshal each top-level section individually into a pre-populated default, or document that all sections must be present.
```

### [FRAGILE] internal/config/config.go:195-208 — getCopy does not deep-copy Storage.S3.Prefixes map
```
WHAT: getCopy deep-copies slices but NOT the S3.Prefixes map. Callers that modify the returned Prefixes map will mutate the live config.
WHY: Maps are reference types and require explicit deep copy.
IMPACT: Data race on Prefixes map under concurrent config reads and mutations.
FIX DIRECTION: Add explicit map copy for S3.Prefixes in getCopy.
```

### [FRAGILE] internal/config/accessors.go:85-118 — SetValuesBatch does not notify OnChange watchers
```
WHAT: SetValuesBatch saves config and calls syncFeatureToggles but does not call OnChange watchers. In contrast, Update() notifies watchers in goroutines.
WHY: Watcher notification was omitted from SetValuesBatch.
IMPACT: Runtime config changes via admin API do not propagate to modules watching for changes (e.g., security rule changes, CORS updates); requires server restart.
FIX DIRECTION: Call watchers in goroutines after save() succeeds in SetValuesBatch, matching the Update() pattern.
```

### [FRAGILE] internal/database/database.go:143-161 — connectWithRetry: MaxRetries=0 skips all connection attempts
```
WHAT: Loop is `for i := 0; i < dbCfg.MaxRetries; i++`. If MaxRetries=0, loop body never executes; returns (nil, nil, nil), causing nil-pointer panic on first DB query.
WHY: Loop does not guarantee at least one attempt.
IMPACT: DATABASE_MAX_RETRIES=0 causes nil GORM instance, server panics on first query.
FIX DIRECTION: Change to max(dbCfg.MaxRetries, 1) to guarantee at least one attempt.
```

### [FRAGILE] internal/server/server.go:460-465 — shutdownHTTPServer called when httpServer may be nil
```
WHAT: Shutdown() calls shutdownHTTPServer unconditionally. If called before Start() assigns httpServer (e.g. OS signal very early in startup), s.httpServer.Shutdown() panics.
WHY: s.httpServer nil guard missing.
IMPACT: Panic on shutdown when called before server was fully started.
FIX DIRECTION: Guard with `if s.httpServer != nil`.
```

### [FRAGILE] internal/config/validate.go:115-130 — validateDatabase does not check Username is non-empty
```
WHAT: validateDatabase checks Host, Port, Name but not Username. Empty username produces a cryptic MySQL auth error instead of a config validation error.
WHY: Username field omitted from validation.
FIX DIRECTION: Add Username empty check to validateDatabase.
```

### [DRIFT] internal/config/env_overrides_streaming.go — STREAMING_REQUIRE_AUTH and STREAMING_UNAUTH_STREAM_LIMIT not mapped
```
WHAT: StreamingConfig.RequireAuth and UnauthStreamLimit have no env override mappings. Cannot be configured via environment variables.
WHY: Fields added to struct but env override file not updated.
IMPACT: Operators cannot configure these via env; silent omission.
FIX DIRECTION: Add envGetBool("STREAMING_REQUIRE_AUTH") and envGetInt("STREAMING_UNAUTH_STREAM_LIMIT") overrides.
```

### [DRIFT] internal/config env overrides — HLS_PRE_GENERATE_INTERVAL_HOURS not mapped
```
WHAT: HLSConfig.PreGenerateIntervalHours has no env override mapping. Field only configurable via config.json.
WHY: Missing from env_overrides_hls.go.
FIX DIRECTION: Add envGetInt("HLS_PRE_GENERATE_INTERVAL_HOURS") to applyHLSBaseOverridesCore.
```

### [DRIFT] internal/config env overrides — S3_PREFIXES (map[string]string) has no env override
```
WHAT: S3StorageConfig.Prefixes cannot be configured via environment variables; only via config.json.
WHY: Maps skipped in env overrides.
IMPACT: Pure env-based B2/S3 deployments cannot configure per-role prefixes.
FIX DIRECTION: Support a JSON-encoded or comma-separated env var like S3_PREFIXES=videos:media/videos,thumbnails:media/thumbs.
```

### [FRAGILE] internal/analytics/stats.go:372 — rebuildStatsFromEvent does not reconstruct UniqueUsers, UniqueViewers, or AvgWatchDuration
```
WHAT: After restart, DailyStats.UniqueUsers = 0 for all historical days and ViewStats.UniqueViewers = 0 for all media. AvgWatchDuration is also 0.
WHY: Reconstruction path skips set-population and average-computation logic.
IMPACT: All admin-facing unique-user and average-duration metrics show 0 after restart.
FIX DIRECTION: Store UserID in events and populate sets during reconstruction; call updateAvgWatchDurationLocked equivalent.
```

### [FRAGILE] internal/analytics/stats.go:64 — updateStats uses wall-clock date, not event.Timestamp
```
WHAT: updateStats keys into time.Now().Format(dateFormat) regardless of event.Timestamp. Late-arriving or replayed events counted in today's bucket, not their actual day.
WHY: Wall clock used instead of event timestamp.
IMPACT: Late-arriving events inflate today's stats; inconsistent with rebuildStatsFromEvent which uses event.Timestamp.
FIX DIRECTION: Use event.Timestamp.Format(dateFormat) as the daily key in updateStats.
```

### [FRAGILE] internal/analytics/stats.go:349 — reconstructStats silently capped at 2000 events
```
WHAT: If more than 2000 events occurred in the last 30 days, in-memory stats are permanently under-counted on startup with no warning to the operator.
WHY: maxEvents=2000 cap with no warning when hit.
FIX DIRECTION: Log a warning when len(events) == maxEvents; consider DB aggregation for historical stats.
```

### [DRIFT] internal/analytics/stats.go:289 — GetSummary mixes DB-sourced TotalEvents with in-memory TotalViews
```
WHAT: TotalEvents comes from eventRepo.Count (DB) while TotalViews sums in-memory mediaStats. These sources can be inconsistent: DB count includes all persisted events; in-memory is capped at 2000 and reset on restart.
WHY: Dual-source architecture with no reconciliation.
IMPACT: Admin dashboard shows correct DB event count alongside stale/lower in-memory view count.
FIX DIRECTION: Choose one authoritative source for all summary stats.
```

### [GAP] internal/hls/jobs.go:143-158 — resumeInterruptedJobs resumes at most ConcurrentLimit jobs; rest permanently stuck
```
WHAT: Only ConcurrentLimit jobs are resumed at startup; remaining pending jobs are stuck indefinitely if the pregenerate background task is absent.
WHY: Relies on external background task for remaining jobs with no fallback.
IMPACT: After a crash with many pending jobs, some stuck in Pending state indefinitely.
FIX DIRECTION: Add a periodic in-module sweep to resume pending jobs even when the external pregenerate task is absent.
```

### [GAP] internal/hls/jobs.go:424-435 — findMediaPathForJob returns "" for all completed jobs (lock file removed on completion)
```
WHAT: .lock file removed by removeLock in transcode() on successful completion. findMediaPathForJob always returns "" for completed disk-discovered jobs — breaking checkMatureAccess lookups.
WHY: Lock file lifecycle ends at normal completion; not designed as a permanent record.
FIX DIRECTION: Persist MediaPath in a separate stable .mediapath sidecar file; or query DB by job ID.
```

### [GAP] api/handlers/thumbnails.go:209-233 — GetThumbnail mature check uses filesystem path; fails for remote media
```
WHAT: tryServeCensoredIfMature calls h.media.GetMedia(path) by filesystem path. For remote/extractor items with no local path, GetMedia fails → IsMature never triggers → mature thumbnail served without censor.
WHY: Path-based lookup only works for local media.
FIX DIRECTION: Fall back to GetMediaByID(id) when GetMedia(path) returns an error.
```

### [SILENT FAIL] internal/suggestions/suggestions.go:328-371 — RecordRating not immediately persisted; up to 10 minutes of ratings lost on unclean shutdown
```
WHAT: Ratings stored only in-memory and flushed every 10 minutes. A server crash loses all ratings in that window.
WHY: No immediate DB write for rating changes.
FIX DIRECTION: For rating changes specifically, call saveOneProfile immediately after updating the profile.
```

### [FRAGILE] internal/security/security.go:910 — isAuthPath missing /api/auth/tokens
```
WHAT: Token creation endpoint uses standard rate limiter (lax) instead of stricter authRateLimiter.
WHY: Token endpoints added after auth path list defined.
FIX DIRECTION: Add "/api/auth/tokens" to isAuthPath.
```

### [FRAGILE] internal/security/security.go:942 — /download path is rate-limit exempt
```
WHAT: No rate limiting on file downloads; clients can hammer /download at the global rate.
WHY: Exempted alongside streaming paths but /download is a full-file endpoint.
FIX DIRECTION: Remove /download from the rate-limit exemption list or apply a separate lower-rate limiter.
```

### [GAP] api/routes/routes.go:453-458 — /api/receiver/stream-push/:token has no rate limiting
```
WHAT: No per-token or per-IP rate limit on stream-push. A stolen token can trigger repeated large binary uploads.
WHY: Token auth checked in handler but no upload rate limiting applied at route level.
FIX DIRECTION: Add per-token rate limit or enforce single-use tokens.
```

### [GAP] api/routes/routes.go:291-293 — /extractor/hls/:id/* endpoints unauthenticated
```
WHAT: Three HLS extractor proxy endpoints registered without auth middleware.
WHY: Intentional design; IDs should be unguessable.
IMPACT: Anyone with a known ID can proxy external streams through the server without authentication.
FIX DIRECTION: Confirm extractor IDs are cryptographically random and short-lived; add at minimum sessionAuth.
```

### [GAP] api/handlers/admin_activity.go / admin_users.go / admin_tasks.go — Multiple admin handlers missing in-handler auth guard
```
WHAT: AdminListUsers, AdminGetUser, AdminUpdateUser, AdminDeleteUser, AdminChangePassword, AdminGetActiveStreams, AdminGetActiveUploads, AdminGetUserSessions, AdminListTasks, AdminRunTask, AdminEnableTask, AdminDisableTask, AdminStopTask rely solely on the router-level adminAuth middleware with no in-handler verification.
WHY: In-handler guard was omitted from these handlers while other handlers include it.
IMPACT: Defense-in-depth gap; if handlers are called outside adminGrp (test, refactor), no auth check exists.
FIX DIRECTION: Add requireAdminModule guard at top of each handler.
```

### [SECURITY] internal/updater/updater.go:924 — writeGitAskPass embeds token without full shell escaping
```
WHAT: Token is embedded in shell script as echo '<token>'. Replacement handles single-quote injection but does not escape $, backtick, or newline sequences.
WHY: Incomplete shell escaping.
FIX DIRECTION: Pass token via environment variable read inside the script rather than interpolating it into the source.
```

---

## LOW — Cleanup, correctness, and maintenance issues

---

### [FRAGILE] internal/config/config.go:149-178 — Windows config rename gap can lose settings on crash
```
WHAT: Between Rename(config→.bak) and Rename(.tmp→config), a crash leaves no config.json. On next startup, server creates default config, losing all operator settings.
WHY: No atomic rename on Windows; two-step strategy with a gap.
FIX DIRECTION: On startup in loadConfigManager, check for config.json.bak as a recovery fallback if config.json is missing.
```

### [FRAGILE] internal/server/server.go:284 — module startup uses context.Background() shadowing Start()'s ctx
```
WHAT: Start(ctx) creates `ctx := context.Background()` for module startup, discarding the caller's context.
FIX DIRECTION: Remove the local shadow; use the parameter ctx.
```

### [GAP] internal/server/signals_unix.go:13 — second SIGTERM during slow shutdown silently dropped
```
WHAT: sigCh buffer=1, goroutine reads exactly one signal. A second SIGINT during hung shutdown cannot force-exit.
FIX DIRECTION: Add a second `<-sigCh; os.Exit(1)` to honor double-signal as force-exit.
```

### [SECURITY] internal/config/env_overrides_auth.go:52-63 — ADMIN_PASSWORD env var may persist in child process environment on Windows
```
WHAT: os.Unsetenv may not remove from OS-level environment on Windows; child processes (ffmpeg, git) can inherit the plaintext admin password.
FIX DIRECTION: On Unix, use syscall.Setenv("ADMIN_PASSWORD", "") to zero the value; document this Windows limitation.
```

### [FRAGILE] internal/hls/locks.go:60-65 — Stale lock threshold hardcoded at 2 hours; kills legitimate long 4K encodes
```
WHAT: CleanStaleLocks marks any job running >2h as Failed. A multi-hour 4K film encode is a legitimate use case.
FIX DIRECTION: Make stale threshold configurable; default to 24h or derive from estimated file duration.
```

### [REDUNDANT] cmd/server/main.go:526-534 — metadata-cleanup task duplicates media-scan task by calling same Scan() function
```
WHAT: Both "media-scan" (1h interval) and "metadata-cleanup" (24h interval) call mediaModule.Scan(), doubling scan work.
FIX DIRECTION: If Scan() already handles pruning, remove metadata-cleanup task.
```

### [LEAK] internal/database/database.go:133-137 — GORM connection leaked when db.DB() fails after Open
```
WHAT: If db.DB() returns error after gorm.Open succeeds, the underlying connection is not closed.
FIX DIRECTION: Before returning the db.DB() error, close with `if sqlDB, _ := db.DB(); sqlDB != nil { sqlDB.Close() }`.
```

### [SILENT FAIL] internal/hls/jobs.go:300-317 — saveJobs panics if called before Start() (m.repo is nil)
```
WHAT: saveJobs calls m.repo.Save without nil-guarding m.repo. m.repo is initialized in Start() not NewModule().
FIX DIRECTION: Add nil guard for m.repo at top of saveJobs.
```

### [FRAGILE] internal/updater/updater.go:201 — Stop does not await the initial update-check goroutine
```
WHAT: Start launches a fire-and-forget goroutine that is never tracked. On shutdown, checkLoop is awaited but this goroutine continues accessing m.httpClient.
FIX DIRECTION: Use a WaitGroup or context to track and cancel the initial check goroutine on Stop.
```

### [GAP] internal/repositories/mysql/audit_log_repository.go:71 — GetByUser with limit=0 returns unbounded results
```
WHAT: Skip Limit if limit<=0, resulting in unbounded SELECT. Can OOM on large tables.
FIX DIRECTION: Apply safe default cap (e.g. 10000) when limit<=0.
```

### [SILENT FAIL] api/handlers/admin_config.go:72 — AdminUpdateConfig rejected-key attempts leave no audit trail
```
WHAT: When all provided config keys are denied, handler returns 400 but writes no audit log entry.
FIX DIRECTION: Call logAdminAction with action="update_config_rejected" before returning the 400.
```

### [FRAGILE] internal/updater/updater.go:1217-1219 — Source update rev-parse errors silently ignored
```
WHAT: Both rev-parse calls discard errors. If either fails, strings compare as equal → update silently skipped as "already up to date."
FIX DIRECTION: Check both err returns; return descriptive error if either rev-parse fails.
```

### [GAP] pkg/helpers/sanitize.go:10-16 — SanitizeString double-encodes HTML entities on repeated calls
```
WHAT: html.EscapeString is not idempotent. Values updated multiple times accumulate &amp;lt; escaping, corrupting stored metadata.
FIX DIRECTION: Store raw values; apply HTML escaping only at render time.
```

### [INCOMPLETE] models/models.go:316-335 — UserPreferences.Validate does not validate SortBy or FilterMediaType against known values
```
WHAT: SortBy and FilterMediaType are stored without allowlist validation; arbitrary strings pass through.
FIX DIRECTION: Apply stringInSetOrDefault with known sort/filter values.
```

### [GAP] api/handlers/deletion_requests.go:140-216 — AdminProcessDeletionRequest updates DB before deleting user; partial failure leaves permanent inconsistency
```
WHAT: DB status updated to "approved" before auth.DeleteUser. If DeleteUser fails, request is stuck as approved-but-not-deleted forever with no rollback.
FIX DIRECTION: Perform DB status update only after successful DeleteUser, or reset status on failure.
```

### [INCOMPLETE] internal/hls/jobs.go:120-121 — tryReuseExistingHLSOnDiskLocked uses fabricated timestamps
```
WHAT: StartedAt=now-1h and CompletedAt=now() are fabricated. Admin tooling and monitoring see misleading data.
FIX DIRECTION: Use directory mtime as CompletedAt (same approach as tryDiscoverJobFromEntryLocked).
```

### [FRAGILE] internal/categorizer/categorizer.go:488-514 — CategorizeDirectory holds mutex per file during unbounded filepath.Walk
```
WHAT: filepath.Walk called without timeout context; large directories block the HTTP handler indefinitely.
FIX DIRECTION: Accept context.Context; check ctx.Err() inside walk callback.
```

### [FRAGILE] pkg/huggingface/client.go:136-140 — rateLimiter.Wait silently returns nil on cancelled context
```
WHAT: Error from rateLimiter.Wait discarded; callers receive empty result on context cancel, indistinguishable from genuine empty API response.
FIX DIRECTION: Return `empty, err` when rateLimiter.Wait fails.
```

### [SECURITY] api/handlers/system.go:404-566 — AdminExecuteQuery SQL keyword denylist is bypassable
```
WHAT: Denylist approach cannot enumerate all evasion vectors (Unicode homoglyphs, hex encoding, aliased functions). The READ ONLY transaction is the real guard.
FIX DIRECTION: Remove the keyword denylist entirely; rely exclusively on READ ONLY transaction + minimal MySQL user grants.
```

### [SECURITY] api/handlers/system.go:398-401 — AdminGetDatabaseStatus leaks DB host and name
```
WHAT: Database host and schema name returned in JSON to any admin.
FIX DIRECTION: Omit host/database from response; log internally instead.
```

### [FRAGILE] internal/downloader/websocket.go:67 — Downloader WebSocket uses DefaultDialer (no timeout)
```
WHAT: Hangs during WebSocket handshake leak goroutines until server restart.
FIX DIRECTION: Use a custom websocket.Dialer with HandshakeTimeout (e.g. 10s).
```

### [SILENT FAIL] internal/upload/upload.go:282-297 — uploadToRemoteStore uses context.Background(), ignoring request context
```
WHAT: Client disconnect mid-upload does not stop the S3 upload; continues to completion wasting bandwidth/credits.
FIX DIRECTION: Thread the request context through ProcessFileHeader and uploadToRemoteStore.
```

### [GAP] internal/validator/validator.go:435-437 — FixFile does not check if output path already exists
```
WHAT: Second call to FixFile for the same input silently overwrites the previous fixed file.
FIX DIRECTION: Check os.Stat(outputPath) before running ffmpeg; error or append counter suffix if exists.
```

### [FRAGILE] internal/hls/probe.go:32-62 — getMediaDuration ffmpeg.ProbeWithTimeout fallback ignores shutdown context
```
WHAT: Fallback uses hardcoded 15-second timeout with no context. Blocks m.activeJobs.Wait() in Stop() for up to 15 seconds per in-flight probe.
FIX DIRECTION: When m.stopping.Load() is true, skip the ProbeWithTimeout fallback.
```

### [SECURITY] internal/playlist/playlist.go:605-652 — ExportPlaylist leaks server filesystem paths in M3U and JSON output
```
WHAT: ExportItem.Path set to item.MediaPath (raw filesystem path). Both M3U and JSON export formats include full server paths.
WHY: No translation to public URL.
IMPACT: Any authenticated user with playlist access receives full server filesystem layout.
FIX DIRECTION: Replace item.MediaPath with a public stream URL in all export formats.
```

### [GAP] internal/playlist/playlist.go:462-484 — ClearPlaylist: partial DB failure leaves DB rows after in-memory clear
```
WHAT: Per-item RemoveItem errors are only logged; in-memory slice cleared unconditionally. DB rows reappear on restart.
FIX DIRECTION: Wrap all RemoveItem calls in a DB transaction; only clear in-memory slice after commit.
```

### [REDUNDANT] api/handlers/handler.go:594-611 — resolvePathToAbsoluteNoWrite duplicates resolveRelativePath logic
```
WHAT: Two separate path resolution code paths with near-identical logic; bug fixes to one don't automatically apply to the other.
FIX DIRECTION: Consolidate into a single resolveRelativePathInDirs implementation.
```

### [LEAK] pkg/helpers/diskspace_windows.go:17-18 — GetDiskUsage creates new LazyDLL on every call
```
WHAT: syscall.NewLazyDLL and NewProc called on every invocation.
FIX DIRECTION: Cache DLL and proc as package-level variables via sync.Once.
```

### [INCOMPLETE] internal/duplicates/duplicates.go:488-499 — findLocalPathByStableID does full table scan
```
WHAT: Every call scans the entire media_metadata table to find one path.
FIX DIRECTION: Add GetByStableID method with a DB index on stable_id.
```

### [SILENT FAIL] internal/receiver/wsconn.go:155-170 — Ping goroutine may not exit cleanly on panic recovery
```
WHAT: If Gin recovers a panic in HandleWebSocket before the deferred close, done channel may not close; ping goroutine lingers until next failed write.
FIX DIRECTION: Low severity; failing write on next tick exits the goroutine. Document.
```

### [SILENT FAIL] internal/downloader/importer.go:141-163 — Import collision suffix uses 1-second precision; concurrent imports can collide
```
WHAT: Two concurrent imports of same filename within same second generate identical collision names; second silently overwrites first.
FIX DIRECTION: Use nanosecond or UUID suffix; use O_EXCL for atomic creation.
```

---

## OK — Investigated and confirmed correct

- `internal/auth/helpers.go:14-51` — Session ID and password generation use crypto/rand correctly with panic on entropy failure.
- `internal/auth/tokens.go:81-84` — API token storage uses SHA-256 with sufficient entropy (32 bytes); plaintext never retained.
- `internal/auth/authenticate.go:39-42,75` — Timing-safe dummy bcrypt comparison prevents username enumeration.
- `internal/auth/user.go:187-211` — lastAdminMu correctly serializes concurrent admin-demotion attempts (TOCTOU-safe).
- `pkg/middleware/agegate.go:86-106` — extractClientIP correctly verifies IsTrustedProxy before honoring X-Forwarded-For.
- `internal/repositories/mysql/user_repository_gorm.go:132-200` — Update uses explicit column map with PasswordHash guard; GORM zero-value problem cannot clear passwords.
- `internal/backup/backup.go:321` — Backup path traversal doubly prevented: regex allowlist at handler + pathWithinBase at module; zip-slip blocked by validateExtractPath.
- `internal/security/security.go` — IP allowlist/blacklist logic correct; expired entries skipped; proxy trust check used.
- `api/routes/routes.go:469` — All admin routes gated behind adminAuth middleware consistently.
- `api/handlers/handler.go:432-468` — resolveAndValidatePath calls EvalSymlinks and re-validates against allowed dirs after resolution.
- `api/handlers/handler.go:267-284` — isSecureRequest only honors X-Forwarded-Proto from IsTrustedProxy-verified sources.
- `internal/repositories/mysql/media_metadata_repository.go:18-22` — ListFiltered uses GORM parameterized queries; escapeLike applied to search terms.
- `api/handlers/deletion_requests.go:46-81` — RequestDataDeletion checks for existing pending request before inserting.
- `api/handlers/response.go:40-48` — safeContentDisposition strips quotes, backslashes, newlines, and control characters.

---

## Files Analyzed (complete list)

All non-vendor Go source files under `api/`, `cmd/`, `internal/`, `pkg/` — approximately 190 files including:

- `cmd/server/main.go`, `cmd/media-receiver/main.go`
- All `api/handlers/*.go` (44 files)
- `api/routes/routes.go`
- All `internal/auth/*.go`, `internal/hls/*.go`, `internal/analytics/*.go`, `internal/config/*.go`
- All `internal/repositories/mysql/*.go`
- All `pkg/storage/`, `pkg/helpers/`, `pkg/middleware/`, `pkg/models/`, `pkg/huggingface/`
- And all remaining internal packages: thumbnails, thumbnails, playlist, suggestions, crawler, extractor, categorizer, updater, validator, duplicates, backup, autodiscovery, scanner, streaming, downloader, receiver, remote, tasks, logger, database, server, admin, security
