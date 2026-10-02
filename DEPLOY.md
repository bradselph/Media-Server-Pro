# Deployment Guide

Media Server Pro is a single Go binary that embeds the Nuxt SPA. The
production path is a systemd unit on a VPS fronted by Caddy or nginx, with
deploys driven from a developer workstation through `deploy.sh`. Federation
between two instances (each one's media appearing on the other) is configured
at runtime through the admin UI — no separate slave binary or extra deploy
step is required.

## First-time bring-up

On a fresh Debian/Ubuntu VPS, from your workstation:

```bash
git clone https://github.com/bradselph/Media-Server-Pro
cd Media-Server-Pro

./deploy.sh --configure    # walk the knob registry — fill in VPS_HOST,
                           # GITHUB_TOKEN, DB credentials, admin creds
./deploy.sh --setup        # SSH into the VPS, install Go + Node + ffmpeg,
                           # clone the repo into $DEPLOY_DIR, install the
                           # systemd unit, open the UFW port
./deploy.sh                # pull, build, restart
```

`deploy.sh --setup` is idempotent — re-running it just re-checks the parts
that need installing. The deploy script is the only supported provisioning
path; it handles SSH key install, dependency pinning from `go.mod` /
`package.json`, and rolls back to the previous binary if the new one fails
the `/health` probe.

## The knob system

Deploy-time and runtime configuration lives in `.deploy.env` (local,
gitignored). Knobs are registered in `deploy-knobs.sh` with description,
default, scope, and section. `deploy-configure.sh` walks newly-added knobs
on every deploy and forwards them to the VPS:

| Scope       | Where it lives                             | When it's read                                    |
|-------------|--------------------------------------------|---------------------------------------------------|
| `vps`       | Local `.deploy.env`                        | Consumed by `deploy.sh` itself (SSH, paths)       |
| `toolchain` | Local `.deploy.env`                        | Version pins (`MSP_GO_VERSION`, `MSP_NODE_MAJOR`) |
| `runtime`   | Forwarded to `$DEPLOY_DIR/.env` on the VPS | See "How runtime knobs take effect" below         |
| `build`     | Exported into the npm build shell          | Baked into the Nuxt bundle (`NUXT_PUBLIC_*`)      |

**Safe-by-default**: pressing Enter on a never-seen knob marks it "seen" as
a commented hint and does **not** push the registry default to the VPS — the
VPS `.env` keeps whatever it had. Only values the operator explicitly types
are forwarded. When a release adds many knobs at once, the walk asks per
section first: Enter walks it, `s` skips the section, `a` skips the rest
(skipped knobs are marked seen; walk them later with `--review` / `--only`).

The registry covers every environment variable the server reads;
`internal/config/knob_registry_test.go` fails the build if a new server env
var ships without a knob (or a runtime knob stops being read).

### How runtime knobs take effect

The server stores its settings in `$DEPLOY_DIR/config.json`. Two kinds of
runtime knob behave differently:

- **Re-read on every start** — data paths (`VIDEOS_DIR`, …), database
  (`DATABASE_*`), object storage (`STORAGE_BACKEND`, `S3_*`) and the admin
  bootstrap login (`ADMIN_USERNAME`, `ADMIN_PASSWORD_HASH`, …). The `.env`
  value always wins; the deploy's restart picks up a change.
- **Owned by config.json** — everything else (server port and timeouts,
  logging, auth, rate limits, CSP/CORS, streaming, HLS, thumbnails, feature
  flags, …). The env var only seeds a brand-new `config.json`; afterwards the
  admin UI edits `config.json` and the server ignores the env var at startup.
  So after forwarding knobs, `deploy.sh` stops the service and runs
  `./server -apply-knobs <forwarded knob names>`, which writes each of these
  knobs **whose value changed since the last deploy** into `config.json`
  (validated, saved atomically, with a per-field diff in the deploy log).
  Knobs you did not change are left alone, so a setting you later change in
  the admin UI keeps your UI value until you change the knob itself.
  `./deploy.sh --reapply-knobs` re-asserts every forwarded knob instead. The
  record of what was applied is `$DEPLOY_DIR/.knobs-applied` (hashes only).
  **The first deploy on an install has no record yet, so it only records a
  baseline:** nothing is applied, and the log lists every field where
  `.deploy.env` disagrees with the live `config.json`. Review that list, then
  either update `.deploy.env` to match or run `./deploy.sh --reapply-knobs`
  to apply it — upgrading never silently reverts admin-UI edits. A knob value that
  fails validation is reported, `config.json` is left untouched, the service
  starts with its previous settings, and the next deploy retries it.
  (`--docker` deploys have no apply step: change these in the admin UI.)

`FEATURE_*` flags are the master switches: the server copies them over the
modules' own `*_ENABLED` flags on every start, so set `FEATURE_UPLOADS`, not
`UPLOADS_ENABLED` (likewise `FEATURE_USER_AUTH` / `AUTH_ENABLED`,
`FEATURE_ADMIN_PANEL` / `ADMIN_ENABLED`, `FEATURE_DOWNLOADER`,
`FEATURE_RECEIVER`, `FEATURE_HUGGINGFACE`).

`GOMEMLIMIT` / `GOGC` are read by the Go runtime when the service starts
(systemd loads `.env` as its `EnvironmentFile`); leave them empty to let the
server size the Go memory limit itself (`SERVER_MEMORY_LIMIT_PERCENT`,
default 75% of RAM).

Commands:

```bash
./deploy.sh --configure                    # walk ★ NEW knobs only
./deploy.sh --review                       # re-walk every knob
./deploy.sh --reapply-knobs                # deploy, re-applying every knob to config.json
./deploy-configure.sh --only NUXT_PUBLIC_GA_ID   # update one knob
./deploy-configure.sh --list               # inventory with current values
./deploy-configure.sh --set KEY=VAL        # set a knob non-interactively

# On the VPS (service stopped), preview what a knob change would do:
cd $DEPLOY_DIR && ./server -apply-knobs HLS_CONCURRENT_LIMIT -knobs-dry-run
```

## Configuration

Runtime config reaches the server through environment variables in
`$DEPLOY_DIR/.env` (set them as knobs in `.deploy.env`; see "How runtime
knobs take effect" above for when each one applies). The full override matrix
lives in `internal/config/env_overrides_*.go`, and `./deploy-configure.sh
--list` prints every knob with its description. Common variables:

- `SERVER_PORT`, `SERVER_HOST` — listening socket
- `DATABASE_NAME`, `DATABASE_USERNAME`, `DATABASE_PASSWORD` — app DB credentials
- `LOG_LEVEL` — `debug` / `info` / `warn` / `error`
- `AUTH_ALLOW_REGISTRATION`, `AUTH_ALLOW_GUESTS` — public exposure. `auth.*` is
  denylisted from the admin config API, so these knobs are the way to change
  them on a running site (the deploy applies a changed value to `config.json`).
- `HLS_AUTO_GENERATE`, `HLS_CONCURRENT_LIMIT`, `HLS_HARDWARE_ACCEL`,
  `HLS_QUALITIES` — transcoding
- `SERVER_MEMORY_LIMIT_PERCENT`, `THUMBNAILS_WORKER_COUNT`,
  `DATABASE_MAX_OPEN_CONNS` / `DATABASE_MAX_IDLE_CONNS` — capacity tuning
- `SECURITY_TRUSTED_PROXY_CIDRS` — reverse proxies trusted for the real
  client IP (rate limits, bans, age-gate IP checks)
- `FEATURE_RECEIVER`, `RECEIVER_API_KEY` — accept federated peers
- `FEATURE_HUGGINGFACE`, `HUGGINGFACE_API_KEY` — visual classifier

Build-time (baked into the Nuxt bundle by `deploy.sh`):

- `NUXT_PUBLIC_GA_ID` — Google Analytics 4 measurement id
- `NUXT_PUBLIC_BUILD_ID` — free-form bundle tag
- `NUXT_PUBLIC_API_BASE` — override API base URL (empty = same-origin)

**Always single-quote secrets** in `.env` — unquoted values containing `#`,
`$`, embedded whitespace, or special chars are silently mangled by the
env-file parser, which is the most common cause of "admin login fails"
reports.

## HiDrive (WebDAV) cold-tier mount

An IONOS HiDrive WebDAV share can back part of the video library as a cheap
cold/overflow tier. HiDrive is **not** S3-compatible, so it doesn't plug into
the `s3` storage backend — instead it's mounted on the VPS and grafted into the
library as a subfolder under `VIDEOS_DIR`, which the scanner indexes normally.

Configure the `HIDRIVE_*` knobs, then run the setup flow:

```bash
./deploy.sh --configure        # fill in the HiDrive mount section:
                               #   HIDRIVE_ENABLED=true
                               #   HIDRIVE_USER / HIDRIVE_PASS
                               #   HIDRIVE_REMOTE_PATH (optional sub-path)
./deploy.sh --setup-hidrive    # install rclone, write rclone.conf, install +
                               # start the hidrive-media.service systemd unit
# → trigger a library rescan from the admin UI (or restart the service)
```

`--setup-hidrive` is reversible: set `HIDRIVE_ENABLED=false` and re-run it to
unmount and remove the unit.

Implementation notes:

- **rclone, not davfs2.** rclone with `--vfs-cache-mode off` does true HTTP
  `Range` reads, so seeking streams byte ranges on demand. davfs2 downloads the
  whole file to a local cache before serving — unusable for large video.
- The mount lands directly at `$VIDEOS_DIR/$HIDRIVE_LIBRARY_SUBDIR` (default
  `hidrive/`). A bind/symlink is deliberately avoided — the scanner's
  `filepath.WalkDir` doesn't descend symlinks, and the local storage backend
  rejects symlinks that resolve outside the videos root.
- **Read-only vs read-write** (`HIDRIVE_READONLY`, default `true`). Read-only is a
  pull-only streaming source (no local cache). Set `HIDRIVE_READONLY=false` to
  mount read-write (`rclone --vfs-cache-mode writes`) so the **downloader can
  store imported media on HiDrive**: the admin Downloader tab's "Import to
  library" prompt then lists HiDrive (shown as `videos/hidrive`) alongside
  Videos/Music/Uploads, and choosing it uploads the file to your HiDrive share.
- The WebDAV password is shipped to the VPS over `scp`, obscured with
  `rclone obscure`, and stored only in `/root/.config/rclone/rclone.conf`
  (mode `600`). It is **never** written to the app's `.env` — the `HIDRIVE_*`
  knobs are scope `vps` and stay in the local `.deploy.env`.
- **Latency caveat.** HiDrive has no CDN and no presigned-URL story like B2.
  Every seek and every HLS transcode pulls bytes from IONOS through the server.
  Direct play is usually fine; transcoded 4K will start slowly. Treat HiDrive as
  a cold tier, not the hot-path store.
- **Read tuning** (applied by `--setup-hidrive`; re-run it after changing them):
  - `HIDRIVE_BUFFER_SIZE` (default `32M`) — rclone's in-memory read-ahead per
    open file. It is what rides out WebDAV latency spikes mid-stream; raise it
    if HiDrive-hosted videos stall while local ones don't. Costs that much RAM
    per file being streamed or transcoded.
  - `HIDRIVE_VFS_CACHE_MODE=full` — keep what has been read on the VPS disk
    (`/var/cache/hidrive-media`, capped by `HIDRIVE_VFS_CACHE_MAX_SIZE`, default
    `20G`; data unread for a week is dropped). Seeking back and re-watching are
    then served locally. Default `off` keeps the mount disk-free.

Mount diagnostics on the VPS: `systemctl status hidrive-media.service` and
`journalctl -u hidrive-media.service -n 40`.

## Reverse proxy / TLS

The Go binary listens on plain HTTP on `${SERVER_PORT}`. Production
deployments should terminate TLS at a reverse proxy (Caddy, nginx, Traefik,
Cloudflare). Set `SERVER_HOST=127.0.0.1` and bind the proxy to the public
interface.

Media is served as long-lived byte-range responses and HLS segments, so the
proxy must not buffer or time them out:

- **Buffering:** the server sends `X-Accel-Buffering: no` on stream, download
  and HLS responses, which nginx honors per response. If your proxy ignores it,
  disable response buffering for the whole site (nginx: `proxy_buffering off;`).
- **Timeouts:** the Go server has no read/write timeout for media. Raise the
  proxy's upstream timeouts so slow storage reads aren't cut off (nginx's
  `proxy_read_timeout` / `proxy_send_timeout` default to 60s; use e.g. `3600s`).
- **Upload size:** the proxy's request-body limit must be at least
  `uploads.max_file_size` (nginx: `client_max_body_size`, default 1m;
  `0` disables the check). Cloudflare's proxy caps request bodies (100 MB on
  Free/Pro plans), so large uploads must go to a host name that bypasses it.
- **Ranges:** forward the `Range` / `If-Range` headers unchanged and don't gzip
  `video/*`, `audio/*`, `video/mp2t` or `application/vnd.apple.mpegurl`.

## Upgrading

```bash
./deploy.sh                # pull, build, restart, auto-rollback on health failure
./deploy.sh --dev          # deploy from the development branch
./deploy.sh --rollback     # restore the previous binary (server.bak)
```

Database schema migrations run on server startup. Take a `mariadbdump`
snapshot before upgrading across major versions.

## Security checklist

- [ ] All secrets (`DATABASE_PASSWORD`, `ADMIN_PASSWORD`, `RECEIVER_API_KEYS`,
  `HUGGINGFACE_API_KEY`) are strong unique values.
- [ ] `SERVER_HOST=127.0.0.1` when running behind a reverse proxy.
- [ ] `AUTH_ALLOW_REGISTRATION=false` unless you intend an open community.
  Seed-only — set it correctly **before the first deploy**; on an
  already-seeded server this env var no longer has any effect, so verify (and
  if needed hand-edit) `auth.allow_registration` in `config.json` and restart.
- [ ] `.env` on the VPS is mode `600`, owned by the `mediaserver` system user.
- [ ] `.deploy.env` is not committed (it is in `.gitignore`).
