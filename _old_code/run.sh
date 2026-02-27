#!/usr/bin/env bash
# run.sh — Build everything (React + Go server + tools) then start the server.
# Run this every time you want to pick up the latest changes.
#
# Usage:
#   ./run.sh                        # Build React + server, then start
#   ./run.sh --tools                # Also build hls-pregenerate and media-doctor
#   ./run.sh --no-react             # Skip React build (use existing bundle)
#   ./run.sh --no-start             # Build only, don't start the server
#   ./run.sh --debug                # Start server with -log-level debug
#   ./run.sh -- -config alt.json    # Pass extra flags straight to ./server
#   ./run.sh --help                 # Show this help

set -euo pipefail

# ── Colour helpers ────────────────────────────────────────────────────────────
RED='\033[0;31m'; GREEN='\033[0;32m'; YELLOW='\033[1;33m'
CYAN='\033[0;36m'; BOLD='\033[1m'; RESET='\033[0m'

info()    { echo -e "${CYAN}[run]${RESET} $*"; }
success() { echo -e "${GREEN}[run]${RESET} $*"; }
warn()    { echo -e "${YELLOW}[run]${RESET} $*"; }
die()     { echo -e "${RED}[run] ERROR:${RESET} $*" >&2; exit 1; }

# ── Flags ─────────────────────────────────────────────────────────────────────
BUILD_REACT=true
BUILD_TOOLS=false
START_SERVER=true
LOG_LEVEL="${LOG_LEVEL:-info}"   # honour env var; override with --debug
SERVER_ARGS=()                   # extra args passed after --

while [[ $# -gt 0 ]]; do
  case "$1" in
    --no-react)  BUILD_REACT=false  ; shift ;;
    --tools)     BUILD_TOOLS=true   ; shift ;;
    --no-start)  START_SERVER=false ; shift ;;
    --debug)     LOG_LEVEL=debug    ; shift ;;
    --help|-h)
      sed -n '/^# Usage:/,/^$/p' "$0"
      exit 0
      ;;
    --)           shift; SERVER_ARGS=("$@"); break ;;
    *) die "Unknown option: $1" ;;
  esac
done

# ── Paths ─────────────────────────────────────────────────────────────────────
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
FRONTEND_DIR="$SCRIPT_DIR/web/frontend"

cd "$SCRIPT_DIR"

echo -e "\n${BOLD}=== Media Server Pro 4 — Build & Run ===${RESET}\n"

# ── Step 1: React frontend ────────────────────────────────────────────────────
if $BUILD_REACT; then
  info "Building React frontend..."

  info "Installing npm dependencies..."
  (cd "$FRONTEND_DIR" && npm ci) || die "npm ci failed"

  (cd "$FRONTEND_DIR" && npm run build) || die "React build failed"
  success "React bundle built → web/static/react/"
else
  warn "Skipping React build (--no-react)"
fi

# ── Step 2: Go modules ────────────────────────────────────────────────────────
info "Downloading Go modules..."
go mod download || die "go mod download failed"
success "go mod download complete"

# ── Step 3: Go server ─────────────────────────────────────────────────────────
info "Building Go server..."
go build -o server ./cmd/server || die "Go server build failed"
success "Server built → ./server"

# ── Step 4: Optional tools ────────────────────────────────────────────────────
if $BUILD_TOOLS; then
  info "Building hls-pregenerate..."
  go build -o hls-pregenerate ./cmd/hls-pregenerate || die "hls-pregenerate build failed"
  success "Tool built → ./hls-pregenerate"

  info "Building media-doctor..."
  go build -o media-doctor ./cmd/media-doctor || die "media-doctor build failed"
  success "Tool built → ./media-doctor"
fi

# ── Step 5: Start server ──────────────────────────────────────────────────────
if $START_SERVER; then
  echo ""
  success "All builds complete. Starting server (log-level=${LOG_LEVEL})..."
  echo ""
  # exec replaces this shell so the server owns stdout/stderr from the very
  # first log line — nothing is buffered or dropped before the handoff.
  exec ./server -log-level "$LOG_LEVEL" "${SERVER_ARGS[@]}"
else
  echo ""
  success "All builds complete (--no-start: server not launched)."
fi
