# Feature Wishlist — Adult Media Server / Self-Hosted

> Generated: 2026-04-13 · Revised for adult/self-hosted context: 2026-04-14
> References: Stash, Jellyfin, Emby, Plex, Whisparr, Radarr patterns
> Mark each item: `[ ]` = undecided, `[x]` = add it, `[-]` = skip

---

## Metadata & Content Organisation

| Status | Feature | Seen In | Complexity | Backend Ready | Notes |
|--------|---------|---------|-----------|---------------|-------|
| [+] | Per-item metadata editing UI (title, description, tags) | Stash, Plex, Jellyfin | Medium | Partial | Admin has bulk update; no per-item UI yet |
| [-] | Performer / model profiles (name, aliases, bio, linked scenes) | Stash | High | No | New performers table; many-to-many with media; browse by performer |
| [-] | Studio / site profiles (name, URL, logo, linked scenes) | Stash, Whisparr | High | No | New studios table; similar to performer profiles |
| [-] | Scene metadata scraping (ThePornDB / StashDB / site-specific) | Stash | High | No | Scraper plugin system; match by filename or title; fill in fields |
| [-] | Series / movie grouping (group scenes that belong to one title) | Stash, Emby | Medium | No | New series table; scenes → series; player shows scene N of M |
| [-] | Release date field (original release, not just date-added) | Stash, Jellyfin | Low | Partial | Add release_date column to media; show in metadata; filter by it |
| [+] | Scene markers / act chapters (tag timestamp ranges with label) | Stash | Medium | No | Chapters table with start/end + label; rendered on seek bar |
| [+] | Custom thumbnail / poster upload per item | Stash, Plex, Jellyfin | Medium | Partial | Upload endpoint; admin replaces auto-generated thumbnail |
| [-] | NFO / CSV metadata import | Jellyfin, Emby | Medium | No | Batch import titles, descriptions, ratings, performers |
| [+] | Phash duplicate detection and merge UI | Stash | High | No | Perceptual hash of video frames; cluster near-duplicates; admin merges |
| [-] | Batch tag editor (select items → add / remove tags in bulk) | Stash, Plex | Low | Yes | Bulk update endpoint exists; wire tag-only UI |
| [+] | Auto-tag by directory rules (editable rules, not just on-scan) | Stash, Plex | Medium | Partial | Categorizer exists; expose editable rule set in admin UI |
| [-] | Admin notes per item (internal annotation, not shown to users) | Stash | Low | Partial | Nullable notes field; admin-only display |
| [-] | Media version tracking (re-encode replaces original; keep both) | Plex | High | No | Media versions table; player can switch between versions |
| [-] | Soft delete / trash bin (30-day recovery before permanent delete) | Google Drive, Plex | Medium | No | deleted_at column; filter from views; restore option in admin |

---

## Player / Playback

| Status | Feature | Seen In | Complexity | Backend Ready | Notes |
|--------|---------|---------|-----------|---------------|-------|
| [+] | Default quality preference (always start at 1080p / 720p) | Netflix, YouTube | Low | Yes | Prefs exist; just wire a new preference field |
| [+] | VR / 360° video playback mode | Heresphere, DeoVR | High | No | Three.js or A-Frame equirectangular renderer; toggle in player |
| [+] | Scene markers jump menu (click to jump to tagged chapter) | Stash | Medium | No | Depends on scene markers feature; dropdown or seek-bar pins |
| [+] | Playlist shuffle mode | Stash, Spotify | Low | No | Randomize playlist order; already have playlist playback |
| [+] | Repeat all / loop playlist modes | Stash, Jellyfin | Low | No | Loop through full playlist; already have single-item loop |
| [+] | Playback queue (ephemeral "play next" list, separate from playlists) | Plex, YouTube | Medium | No | Temporary ordered queue; queue button on media cards |
| [-] | Sleep timer (auto-stop after X minutes) | Plex, Jellyfin | Low | No | Client-side countdown; pref dropdown in controls |
| [+] | Download quality selector before download (480p / 720p / 1080p) | Netflix, Plex | Low | Yes | HLS serves multi-quality; add dialog before download starts |
| [-] | Bitrate lock (force specific quality; disable HLS auto-switch) | Plex, Jellyfin | Low | Yes | HLS already multi-quality; pin a level in player prefs |
| [-] | Multiple audio track selection (when media has 2+ audio streams) | Plex, Jellyfin, Emby | Medium | Partial | Backend detects streams; UI dropdown to switch |
| [-] | Playback speed separate for audio vs. video | Podcast apps, Plex | Low | Yes | Split speed pref into audio_speed + video_speed |
| [+] | Configurable skip interval (default ±10s; user sets ±5/10/30/60s) | Podcast apps, Plex | Low | Yes | Prefs field; applies to both keyboard and mobile tap skip |
| [-] | Personal bookmarks (save multiple custom timestamps per item) | Stash, Plex | Medium | No | Different from resume — user pins moments; displayed on seek bar |
| [+] | Mini-player / floating audio bar (browse while audio plays) | Spotify, YouTube Music | Medium | No | PiP covers video; separate bottom bar for audio-only content |
| [-] | HDR / codec format badge in player (4K, HEVC, AV1, Dolby) | Netflix, Plex | Low | Yes | Codec already in media info overlay; surface as player badge |
| [+] | Buffer health bar (show buffered range on seek bar) | VLC, Plex | Low | No | Read video.buffered; shade seek bar to show buffer fill |

---

## Browse / Discovery

| Status | Feature | Seen In | Complexity | Backend Ready | Notes |
|--------|---------|---------|-----------|---------------|-------|
| [-] | Faceted sidebar filters (duration, date added, codec, resolution) | Stash, Plex, Jellyfin | Medium | Yes | API already accepts these params; need sidebar UI |
| [-] | Search history | Stash, YouTube | Low | Partial | Store recent queries; show dropdown on search focus |
| [+] | Smart / auto-updating playlists (rule-based) | Stash, Plex | High | No | e.g. "all 4K added in last 30 days" or "performer = X" |
| [-] | Browse by performer (performer profile page + their scenes) | Stash | High | No | Depends on performer profiles feature |
| [-] | Browse by studio (studio profile page + their scenes) | Stash, Whisparr | High | No | Depends on studio profiles feature |
| [-] | Series detail page (all scenes in a series, episode order) | Stash, Plex | Medium | No | Depends on series grouping feature |
| [-] | Advanced search filters (resolution, codec, bitrate, duration, size) | Stash, Jellyfin | Medium | Yes | API accepts some; add resolution + size range params |
| [-] | Multi-tag filter (AND across multiple tags simultaneously) | Stash, Jellyfin | Low | Yes | Currently one active tag; allow stacking multiple |
| [-] | Exclude tag filter (NOT logic — show items without this tag) | Stash | Low | Yes | Add tag_exclude param; SQL WHERE NOT EXISTS |
| [-] | Performer filter in main browse (filter scenes by performer tag) | Stash | Medium | No | Depends on performer profiles or structured performer tags |
| [-] | Release year filter / decade browsing | Netflix, Plex | Low | Yes | Add year_min / year_max query params; already has date_added |
| [-] | Saved searches / filter presets (name and reuse a filter set) | Stash, Plex | Low | No | Store filter JSON per user; dropdown to recall |
| [-] | Minimum duration filter (hide clips shorter than N minutes) | Stash, Plex | Low | Yes | Add min_duration param; filter out shorts |
| [-] | Folder / directory browsing mode | Stash, Jellyfin | Medium | Partial | Browse by actual filesystem path structure |
| [+] | "Don't show this again" per recommendation item | Netflix, Stash | Low | Partial | Per-item suppress from suggestions; store suppressed list |
| [-] | Recently fully-watched row (distinct from Continue Watching) | Plex, Stash | Low | Partial | Show completed items sorted by completion date |
| [-] | Random / shuffle full library view | Stash, Plex | Low | Yes | Surprise Me exists on homepage; add to full browse view |

---

## User Features

| Status | Feature | Seen In | Complexity | Backend Ready | Notes |
|--------|---------|---------|-----------|---------------|-------|
| [-] | Download for regular users | Plex, Stash | Low | Yes | Backend gated by permission flag; just needs UI button |
| [-] | Two-factor authentication (TOTP) | Plex, Jellyfin | High | No | New DB schema + auth flow + recovery codes |
| [-] | In-app notifications (new content alerts) | Plex, Jellyfin | Medium | Partial | Backend tracks scans; needs notification store + UI bell |
| [-] | M3U playlist import | Jellyfin, Emby | Medium | No | M3U export already works; import is missing |
| [-] | Watchlist / Watch Later (ordered priority queue, not favorites) | Plex, Stash | Low | Partial | Favorites exist but unordered; watchlist = explicit priority list |
| [-] | Multiple profiles per account (separate history / prefs per profile) | Netflix, Plex | High | No | Profiles table with own watch history / prefs; select on login |
| [-] | PIN-lock profile (require PIN to access a specific profile) | Netflix, Plex | Medium | No | PIN column on profiles; lock screen before loading profile |
| [+] | Privacy mode toggle (pause all history tracking temporarily) | Custom | Low | Partial | Session-level pref: disable history + recommendations tracking |
| [+] | Incognito / private session (no history, no recommendations impact) | Custom | Medium | No | Per-session flag; don't record any watch events |
| [-] | Custom avatar / profile picture upload | Plex, Jellyfin | Low | No | Upload stored as user avatar blob; shown in header and profile |
| [-] | Per-item personal rating notes (private note alongside 1–5 star) | Stash | Low | Partial | Add nullable note column to ratings table |
| [-] | "Already seen" quick-mark without playing (bulk mark as watched) | Stash, Plex | Low | Yes | POST to playback with position=duration; batch version useful |
| [-] | Invite link registration (admin generates single-use invite code) | Plex, Jellyfin | Medium | No | Token-gated registration bypass; time-limited invite tokens |
| [-] | Guest pass (time-limited access without creating a full account) | Plex | Medium | No | Temporary credentials with expiry; scope to specific content |
| [-] | "Remember me" session duration preference (1 / 7 / 30 days) | Netflix, Plex | Low | Yes | Let user choose session lifetime; prefs already exist |
| [-] | Language / locale preference (date format, number format) | Netflix, Plex | Low | Yes | Prefs exist; add locale field; affects display formatting only |
| [-] | Yearly / monthly personal stats recap | Spotify Wrapped, Plex | Medium | Partial | Backend has all watch data; surface as recap card per period |

---

## Sharing & Access Control

| Status | Feature | Seen In | Complexity | Backend Ready | Notes |
|--------|---------|---------|-----------|---------------|-------|
| [-] | Per-device playback restrictions (admin lock-down per device) | Netflix, Plex | Medium | No | Track device fingerprint; admin can block specific devices |
| [-] | Webhooks / outbound HTTP on new upload | Plex | Medium | No | Outbound HTTP hooks on scan complete; configurable URL + payload |
| [-] | Time-limited public share link for individual media | Plex, Google Drive | Medium | No | Signed URL with expiry; no login needed to view the one item |
| [-] | Password-protected share link | Google Drive, Nextcloud | Medium | No | Share token + optional password; scoped to one item |
| [+] | Embed player on external pages (iframe embed code) | Plex, Vimeo | Medium | No | /embed/:id route with stripped-down player; CSP config needed |
| [-] | Concurrent stream limit per user (max N active streams) | Netflix, Plex | Medium | No | Track active HLS sessions per user; 429 when over limit |
| [-] | Per-category access control (restrict a category to specific roles) | Plex, Jellyfin | High | No | Category → allowed_roles mapping; filter media queries per user |

---

## Self-Hosted / Infrastructure

| Status | Feature | Seen In | Complexity | Backend Ready | Notes |
|--------|---------|---------|-----------|---------------|-------|
| [+] | Maintenance mode (block users, show message; admin still works) | Plex, Jellyfin | Low | No | Config flag; middleware returns 503 with message for non-admins |
| [-] | LDAP / Active Directory authentication (household SSO) | Jellyfin, Emby | High | No | LDAP bind for login; sync roles from group membership |
| [-] | SSL certificate management in admin (Let's Encrypt auto-renew) | Caddy, Nginx Proxy Mgr | High | No | ACME client built-in; configure domain in admin UI |
| [+] | Reverse proxy detection / trusted-proxy configuration in UI | Plex, Jellyfin | Low | Yes | trusted_proxies config exists; expose in admin settings panel |
| [-] | Local network only mode (block all WAN requests) | Jellyfin | Low | No | Bind to LAN interface only; config option in admin |
| [-] | Config export / import (backup all settings as JSON) | Jellyfin, Emby | Low | Partial | Config exists; add export + import endpoints |
| [-] | Automatic scheduled backups to local path | Plex, Jellyfin | Medium | Partial | Backup system exists; add cron schedule + local destination path |
| [-] | Import from mounted external drive / network share | Jellyfin, Emby | Medium | Partial | Scanner exists; expose a one-off path scan from admin |
| [-] | Docker / container health check endpoint (lightweight liveness) | Custom | Low | Partial | /health exists; ensure it returns fast with no DB dependency |
| [-] | Admin impersonation (view site as a specific user for debugging) | Custom | Medium | No | Admin can load another user's session context |
| [-] | User activity live view (admin: who is streaming what right now) | Plex | Low | Partial | Active HLS jobs + sessions exist; surface as live admin table |
| [+] | Scheduled task configuration UI (set cron for scans, cleanup) | Plex, Jellyfin | Medium | Partial | Task runner exists; expose schedule fields in admin settings |
| [-] | Storage quota report by category / tag (admin) | Plex | Low | Yes | GROUP BY on DB; bar chart in admin dashboard |
| [+] | Media quality report (flag items missing thumbnails or unreadable) | Plex, Jellyfin | Medium | Partial | Validator endpoint exists; surface results as sortable report |
| [-] | Media expiry / auto-archive (hide after N days with zero views) | Custom | Medium | No | Configurable TTL per category; auto-set visible=false |
| [-] | Concurrent stream limit (global cap on total active HLS sessions) | Custom | Low | No | Config value; reject new HLS generate when at cap |
| [-] | Bulk thumbnail re-generation scoped to a specific directory | Plex | Low | Yes | Thumbnail generate endpoint exists; add folder-scoped param |
| [-] | Import from URL / paste URL (server fetches + indexes directly) | Jellyfin, Emby | Medium | Partial | Downloader exists; expose a simpler "import URL" admin form |

---

## Notifications & Alerts

| Status | Feature | Seen In | Complexity | Backend Ready | Notes |
|--------|---------|---------|-----------|---------------|-------|
| [-] | In-app notification bell (new uploads, admin announcements) | Plex, Jellyfin | Medium | No | Notifications table; bell icon in nav; mark-as-read |
| [-] | Admin site-wide announcement banner | Plex, Jellyfin | Low | No | Admin posts message; dismissible banner shown to all users |
| [-] | Admin alert emails (storage full, scan errors, failed HLS jobs) | Plex | Medium | No | SMTP config + threshold triggers; opt-in per alert type |
| [-] | Email digest (daily / weekly new content summary for users) | Plex | Medium | No | SMTP + user opt-in pref; list new media; unsubscribe link |
| [-] | Browser push notifications | YouTube | Medium | No | Service worker + Push API; requires user opt-in; less useful self-hosted |

---

## Technical / Performance

| Status | Feature | Seen In | Complexity | Backend Ready | Notes |
|--------|---------|---------|-----------|---------------|-------|
| [-] | Installable PWA (add to home screen; offline shell) | YouTube, Plex | Low | No | manifest.json + service worker; shell loads without internet |
| [-] | Low-bandwidth / data saver mode (force lowest quality; reduce animations) | Netflix | Low | Yes | Pref flag; forces lowest HLS tier; disables thumbnail cycling |
| [+] | Preload / prefetch next item during current playback | YouTube, Netflix | Medium | No | Fetch next item metadata + first HLS segment while playing current |
| [-] | Keyboard shortcut customisation | VLC, Plex | Medium | No | User-configurable key bindings stored in prefs; reset to defaults |
| [+] | Configurable items per page up to 200 (currently max 96) | Jellyfin | Low | Yes | Raise max_limit config value; backend already paginates |
| [-] | Dark / light / auto theme toggle in nav (one-click, not in profile) | Netflix, Plex | Low | Yes | Theme pref exists; add quick-toggle button to header nav |

---

## Explicitly Out of Scope (never add)

| Feature | Reason |
|---------|--------|
| Subtitles / captions | Product decision — will never be added |
| User comments / reviews | Out of scope |
| Live streaming ingest | Out of scope |
| Social features (likes, shares, follows, feed) | Out of scope |
| Native mobile app (iOS / Android) | Web-only by design |
| Podcast feed management | Out of scope |

---

## Already Implemented (for reference)

Resume playback · Continue watching row · HLS adaptive streaming · On-demand HLS generation · Graphic 10-band EQ · Picture-in-picture · Theater mode · Fullscreen · Keyboard shortcuts (full set + help overlay) · Thumbnail seek preview · Mobile tap-to-skip (±10s zones) · Mute button · Playback speed control · Loop single item · Auto-next with up-next countdown · Audio visualizer (48-bar) · Public playlists · M3U export · Per-card playlist quick-add (no interruption) · Bulk playlist operations · Time-stamped deep links · Personal watch stats (genre breakdown, top types) · Reset recommendations · API tokens (admin) · RSS / Atom feeds · Watch history CSV export · Per-user mature content gate · New-since-last-visit badge · Personalized recommendations · Favorites · On-deck next episode · Trending row · Rating system (1–5 stars) · View mode persistence (grid / list / compact) · Category filter · Tag filter · Sort by 8+ fields · Items per page preference · Hide watched toggle · Min rating filter · Bulk select + add to playlist · Surprise Me · Play All · Storage quota display · Self-serve account deletion · Watch history (remove, clear, export, search, filter) · Session management (view + revoke) · Admin: full user management, scanner, HLS jobs, thumbnails, backups, discovery, downloader, crawler, categorizer, receiver/slaves, audit logs, analytics dashboard, Prometheus metrics
