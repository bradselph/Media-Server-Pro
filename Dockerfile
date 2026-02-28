# ─── Stage 1: Build React frontend ────────────────────────────────────────────
FROM node:22-alpine AS frontend-builder
WORKDIR /app/web/frontend
COPY web/frontend/package*.json ./
RUN npm ci
COPY web/frontend/ ./
RUN npm run build

# ─── Stage 2: Build Go binary ──────────────────────────────────────────────────
FROM golang:1.26-alpine AS go-builder
RUN apk add --no-cache git
WORKDIR /app

# Cache Go module downloads separately from source changes
COPY go.mod go.sum ./
RUN go mod download

# Copy source and frontend build output
COPY . .
COPY --from=frontend-builder /app/web/static/react/ ./web/static/react/

# Build with version injection
ARG VERSION=dev
ARG BUILD_DATE=unknown
RUN CGO_ENABLED=0 GOOS=linux go build \
    -ldflags "-X main.Version=${VERSION} -X main.BuildDate=${BUILD_DATE} -s -w" \
    -o /server \
    ./cmd/server

# ─── Stage 3: Minimal runtime image ───────────────────────────────────────────
FROM alpine:3.21
RUN apk add --no-cache ffmpeg ca-certificates tzdata

# Create a non-root service user
RUN addgroup -S mediaserver && adduser -S -G mediaserver mediaserver

WORKDIR /data
RUN mkdir -p videos music uploads thumbnails hls_cache data logs temp playlists && \
    chown -R mediaserver:mediaserver /data

COPY --from=go-builder /server /usr/local/bin/server
RUN chmod +x /usr/local/bin/server

USER mediaserver

EXPOSE 8080

HEALTHCHECK --interval=30s --timeout=10s --start-period=30s --retries=3 \
    CMD wget -qO- http://localhost:8080/health || exit 1

ENTRYPOINT ["/usr/local/bin/server"]
