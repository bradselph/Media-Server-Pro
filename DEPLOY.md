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
