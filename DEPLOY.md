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

| Scope       | Where it lives                | When it's read                       |
| ----------- | ----------------------------- | ------------------------------------ |
| `vps`       | Local `.deploy.env`           | Consumed by `deploy.sh` itself (SSH, paths) |
| `toolchain` | Local `.deploy.env`           | Version pins (`MSP_GO_VERSION`, `MSP_NODE_MAJOR`) |
| `runtime`   | Forwarded to `$DEPLOY_DIR/.env` on the VPS | Read by the Go server on every start |
| `build`     | Exported into the npm build shell | Baked into the Nuxt bundle (`NUXT_PUBLIC_*`) |

**Safe-by-default**: pressing Enter on a never-seen knob marks it "seen" as
a commented hint and does **not** push the registry default to the VPS — the
VPS `.env` keeps whatever it had. Only values the operator explicitly types
are forwarded.

Commands:

```bash
./deploy.sh --configure                    # walk ★ NEW knobs only
./deploy.sh --review                       # re-walk every knob
./deploy-configure.sh --only NUXT_PUBLIC_GA_ID   # update one knob
./deploy-configure.sh --list               # inventory with current values
./deploy-configure.sh --set KEY=VAL        # set a knob non-interactively
```

## Configuration

All runtime config is supplied via environment variables read from
`$DEPLOY_DIR/.env`. The full override matrix lives in
`internal/config/env_overrides_*.go`. Common variables:

- `SERVER_PORT`, `SERVER_HOST` — listening socket
- `DATABASE_NAME`, `DATABASE_USERNAME`, `DATABASE_PASSWORD` — app DB credentials
- `LOG_LEVEL` — `debug` / `info` / `warn` / `error`
- `AUTH_ALLOW_REGISTRATION`, `AUTH_ALLOW_GUESTS` — public exposure
- `RECEIVER_ENABLED`, `RECEIVER_API_KEYS` — accept federated peers
- `FEATURE_HUGGINGFACE`, `HUGGINGFACE_API_KEY` — visual classifier
- `FEATURE_CLAUDE`, `ANTHROPIC_API_KEY`, `CLAUDE_MODE` — admin assistant

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

Mount diagnostics on the VPS: `systemctl status hidrive-media.service` and
`journalctl -u hidrive-media.service -n 40`.

## Reverse proxy / TLS

The Go binary listens on plain HTTP on `${SERVER_PORT}`. Production
deployments should terminate TLS at a reverse proxy (Caddy, nginx, Traefik,
Cloudflare). Set `SERVER_HOST=127.0.0.1` and bind the proxy to the public
interface.

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
      `HUGGINGFACE_API_KEY`, `ANTHROPIC_API_KEY`) are strong unique values.
- [ ] `SERVER_HOST=127.0.0.1` when running behind a reverse proxy.
- [ ] `AUTH_ALLOW_REGISTRATION=false` unless you intend an open community.
- [ ] `.env` on the VPS is mode `600`, owned by the `mediaserver` system user.
- [ ] `.deploy.env` is not committed (it is in `.gitignore`).
