# Deployment Guide

Media Server Pro ships as a single OCI image plus an optional media-receiver
("slave") image. Production deployments use Docker Compose; the published
images are also suitable for Kubernetes / Nomad / Swarm.

## Images

Both images are published to the GitHub Container Registry on every release
tag (`v[0-9]+.[0-9]+.[0-9]+`) and on manual workflow dispatch:

- `ghcr.io/<owner>/media-server-pro` — master server (HTTP + Nuxt UI)
- `ghcr.io/<owner>/media-server-pro-receiver` — slave node (catalog/stream relay)

Built for `linux/amd64` and `linux/arm64`.

## Quick start (Docker Compose)

```bash
cp .env.docker.example .env.docker     # then edit secrets and paths
docker compose --env-file .env.docker up -d --build
```

`.env.docker` is git-ignored. The example template lists every variable the
stack needs, including the required ones (DB passwords, MinIO image tag).

The stack defines these services:

| Service    | Profile     | Notes                                                        |
| ---------- | ----------- | ------------------------------------------------------------ |
| `db`       | always      | MariaDB 11 with healthcheck and persistent `db-data` volume  |
| `server`   | always      | Master server, depends on `db: service_healthy`              |
| `receiver` | `receiver`  | Slave node — needs `MASTER_URL` and `RECEIVER_API_KEY`       |
| `minio`    | `minio`     | Optional S3-compatible storage; pin `MINIO_IMAGE_TAG`        |

Enable optional services with profiles:

```bash
docker compose --env-file .env.docker --profile receiver up -d
docker compose --env-file .env.docker --profile minio up -d
```

## Configuration

All runtime config is supplied via environment variables. The full override
matrix lives in `internal/config/env_overrides_*.go`. Common variables:

- `SERVER_PORT`, `SERVER_BIND` — listening socket
- `DATABASE_NAME`, `DATABASE_USERNAME`, `DATABASE_PASSWORD` — app DB credentials
- `DB_ROOT_PASSWORD` — MariaDB root (only used by the `db` service)
- `LOG_LEVEL` — `debug` / `info` / `warn` / `error`
- `APP_UID`, `APP_GID` — match host owner of bind-mounted media directories

## Volumes

Named volumes are scoped per data type so each can be backed up / restored
independently:

- `db-data` — MariaDB
- `videos`, `music`, `uploads`, `thumbnails`, `playlists`, `analytics`,
  `hls-cache`, `data`, `logs`, `temp` — server state

Switch any of them to bind mounts in `docker-compose.override.yml` if you
want host-managed paths.

## Healthchecks

- `db` — `mariadb-admin ping`, 30 s interval
- `server` — `GET /health`, 30 s interval, 20 s start period
- `minio` — `GET /minio/health/live`, 30 s interval

The compose file uses `service_healthy` for dependency ordering, so `server`
will not start until the database is accepting connections.

## Reverse proxy / TLS

The image listens on plain HTTP on `${SERVER_PORT}`. Production deployments
should terminate TLS at a reverse proxy (Caddy, nginx, Traefik, Cloudflare).
Set `SERVER_BIND=127.0.0.1` and bind the proxy to the public interface.

## CI publish workflow

`.github/workflows/docker-publish.yml` builds and pushes both images on:

- semver tags (`v1.2.3` → `:1.2.3`, `:1.2`, `:1`)
- manual `workflow_dispatch` (publishes a `branch-sha` tag for hotfixes)

Multi-arch builds run via QEMU on the self-hosted amd64 runner. Layer cache
is keyed by branch ref so sequential pushes reuse the npm and Go module
download layers.

## Upgrading

```bash
docker compose --env-file .env.docker pull
docker compose --env-file .env.docker up -d
```

Database schema migrations run on server startup. Take a `mariadbdump`
snapshot before upgrading across major versions.

## Security checklist

- [ ] All secrets (`DB_ROOT_PASSWORD`, `DATABASE_PASSWORD`, `RECEIVER_API_KEY`,
      `MINIO_ROOT_PASSWORD`) replaced with strong unique values.
- [ ] `MINIO_IMAGE_TAG` pinned to a specific RELEASE — `latest` is rejected
      by compose at `up` time.
- [ ] `SERVER_BIND=127.0.0.1` when running behind a reverse proxy.
- [ ] Bind-mount UIDs match `APP_UID`/`APP_GID` so the container can write.
- [ ] `.env.docker` not committed (it is in `.dockerignore` and `.gitignore`).
