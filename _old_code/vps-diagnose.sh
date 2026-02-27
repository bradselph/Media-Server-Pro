#!/usr/bin/env bash
# vps-diagnose.sh — Full VPS diagnostic for Media Server Pro 3.
# Designed for Claude Code to run remotely and identify issues automatically.
#
# Usage:
#   ./vps-diagnose.sh              # full diagnostic
#   ./vps-diagnose.sh --quick      # service status + last 30 log lines only
#   ./vps-diagnose.sh --errors     # only error/warning log lines
#   ./vps-diagnose.sh --thumbnails # thumbnail-specific diagnostics

set -euo pipefail

VPS_HOST="${VPS_HOST:-66.179.136.144}"
VPS_USER="${VPS_USER:-root}"
VPS_PORT="${VPS_PORT:-22}"
KEY_FILE="${KEY_FILE:-$HOME/.ssh/ED_25519}"
DEPLOY_DIR="${DEPLOY_DIR:-/home/Media-Server-Pro-3}"
SERVICE="${SERVICE:-mediaserver}"

QUICK=false
ERRORS_ONLY=false
THUMBNAILS=false

while [[ $# -gt 0 ]]; do
  case "$1" in
    --quick)      QUICK=true      ; shift ;;
    --errors)     ERRORS_ONLY=true; shift ;;
    --thumbnails) THUMBNAILS=true ; shift ;;
    *) echo "Unknown option: $1"; exit 1 ;;
  esac
done

source "$(dirname "$0")/vps-auth.sh"

sep() { echo ""; echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"; echo "  $*"; echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"; }

# ── Service status (always) ───────────────────────────────────────────────────
sep "SERVICE STATUS"
vps "systemctl status $SERVICE --no-pager || true"

sep "LAST 50 LOG LINES"
if $ERRORS_ONLY; then
  vps "journalctl -u $SERVICE --no-pager -n 50 -p err..warning"
else
  vps "journalctl -u $SERVICE --no-pager -n 50"
fi

if $QUICK; then exit 0; fi

# ── Runtime environment ───────────────────────────────────────────────────────
sep "RUNTIME CHECKS (binary, port, config)"
vps bash -s -- "$DEPLOY_DIR" <<'REMOTE'
DEPLOY="${1:-/home/Media-Server-Pro-3}"

echo "=== Binary ==="
ls -lh $DEPLOY/server 2>/dev/null || echo "ERROR: server binary missing"
$DEPLOY/server -version 2>/dev/null || true

echo ""
echo "=== Port binding ==="
ss -tlnp | grep -E ':80|:8080|:443' || echo "(no ports in use)"

echo ""
echo "=== .env (secrets redacted) ==="
if [[ -f $DEPLOY/.env ]]; then
  sed 's/\(PASSWORD\|SECRET\|TOKEN\|KEY\)=.*/\1=[REDACTED]/gi' $DEPLOY/.env
else
  echo "WARNING: .env not found"
fi

echo ""
echo "=== config.json (passwords redacted) ==="
if [[ -f $DEPLOY/config.json ]]; then
  python3 -c "
import json, sys, re
txt = open('$DEPLOY/config.json').read()
txt = re.sub(r'\"password\":\s*\"[^\"]*\"', '\"password\": \"[REDACTED]\"', txt, flags=re.IGNORECASE)
print(txt)
" 2>/dev/null || sed 's/"password":\s*"[^"]*"/"password": "[REDACTED]"/gi' $DEPLOY/config.json
else
  echo "INFO: config.json not found (using .env only)"
fi
REMOTE

# ── System resources ─────────────────────────────────────────────────────────
sep "SYSTEM RESOURCES"
vps "bash -s" <<'REMOTE'
echo "=== Disk ==="
df -h
echo ""
echo "=== Memory ==="
free -h
echo ""
echo "=== CPU load ==="
uptime
REMOTE

# ── Dependencies ─────────────────────────────────────────────────────────────
sep "RUNTIME DEPENDENCIES"
vps "bash -s" <<'REMOTE'
for bin in ffmpeg ffprobe gh git; do
  if command -v $bin &>/dev/null; then
    echo "  ✓ $bin: $(command -v $bin) — $($bin -version 2>&1 | head -1 || $bin --version 2>&1 | head -1)"
  else
    echo "  ✗ $bin: NOT FOUND"
  fi
done
REMOTE

# ── Thumbnail diagnostics ─────────────────────────────────────────────────────
if $THUMBNAILS || ! $QUICK; then
sep "THUMBNAILS"
vps bash -s -- "$DEPLOY_DIR" "$SERVICE" <<'REMOTE'
DEPLOY="${1:-/home/Media-Server-Pro-3}"
SERVICE_NAME="${2:-mediaserver}"
THUMB_DIR=$(grep -oP '(?<=THUMBNAILS_DIR=)\S+' $DEPLOY/.env 2>/dev/null || echo "$DEPLOY/thumbnails")

echo "=== Thumbnail directory: $THUMB_DIR ==="
if [[ -d "$THUMB_DIR" ]]; then
  COUNT=$(find "$THUMB_DIR" -name "*.jpg" 2>/dev/null | wc -l)
  SIZE=$(du -sh "$THUMB_DIR" 2>/dev/null | cut -f1)
  echo "  ✓ Exists — $COUNT thumbnails, $SIZE total"
  echo "  Permissions: $(stat -c '%A %U:%G' "$THUMB_DIR")"
else
  echo "  ✗ Directory MISSING — thumbnails cannot be stored"
  echo "  Fix: mkdir -p $THUMB_DIR && chown root:root $THUMB_DIR"
fi

echo ""
echo "=== Recent thumbnail errors in logs ==="
journalctl -u "${SERVICE_NAME}" --no-pager -n 500 2>/dev/null \
  | grep -i "thumbnail\|ffmpeg\|ffprobe" | tail -20 || echo "(none found)"
REMOTE
fi

# ── Nginx ─────────────────────────────────────────────────────────────────────
sep "NGINX STATUS"
vps "bash -s" <<'REMOTE'
nginx -t 2>&1 || true
echo ""
systemctl status nginx --no-pager | head -20 || true
REMOTE

sep "HTTP HEALTH CHECK"
vps bash -s -- "$DEPLOY_DIR" <<'REMOTE'
DEPLOY="${1:-/home/Media-Server-Pro-3}"
PORT="$(grep -oP '(?<=^SERVER_PORT=)\d+' "$DEPLOY/.env" 2>/dev/null | head -1 || echo 8080)"
HEALTH_URL="http://127.0.0.1:${PORT}/health"

echo "=== GET $HEALTH_URL ==="
HTTP_CODE="$(curl --silent --output /tmp/_health_body --write-out '%{http_code}' \
  --connect-timeout 5 --max-time 10 "$HEALTH_URL" 2>/dev/null || echo '000')"
BODY="$(cat /tmp/_health_body 2>/dev/null || echo '')"
rm -f /tmp/_health_body

if [[ "$HTTP_CODE" == "200" ]]; then
  echo "  ✓ HTTP $HTTP_CODE — server healthy"
elif [[ "$HTTP_CODE" == "503" ]]; then
  echo "  ✗ HTTP $HTTP_CODE — server degraded"
else
  echo "  ✗ HTTP $HTTP_CODE — server unreachable or crashed"
fi
if [[ -n "$BODY" ]]; then
  echo "  Response: $BODY"
fi

echo ""
echo "=== Last healthcheck cron run ==="
journalctl -t mediaserver-healthcheck --no-pager -n 10 2>/dev/null || echo "(no healthcheck cron log found)"
REMOTE

sep "DIAGNOSTIC COMPLETE"
echo "  VPS: $VPS_USER@$VPS_HOST"
echo "  Service: $SERVICE"
echo "  Deploy dir: $DEPLOY_DIR"
echo ""
echo "  To stream live logs:  ./pull-vps-logs.sh --follow"
echo "  To run a VPS command: ./vps-exec.sh '<command>'"
