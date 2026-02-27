#!/usr/bin/env bash
# vps-exec.sh — Run any command on the VPS and stream output back.
# Auto-sets up SSH key if needed.
#
# Usage:
#   ./vps-exec.sh <command>
#   ./vps-exec.sh "journalctl -u mediaserver -n 50"
#   ./vps-exec.sh "systemctl status mediaserver"

set -euo pipefail

VPS_HOST="${VPS_HOST:-66.179.136.144}"
VPS_USER="${VPS_USER:-root}"
VPS_PORT="${VPS_PORT:-22}"
KEY_FILE="${KEY_FILE:-$HOME/.ssh/ED_25519}"
DEPLOY_DIR="${DEPLOY_DIR:-/home/Media-Server-Pro-3}"

if [[ $# -eq 0 ]]; then
  echo "Usage: $0 <command>"
  echo "  Runs <command> on the VPS and streams output here."
  echo ""
  echo "Environment overrides: VPS_HOST VPS_USER VPS_PORT KEY_FILE DEPLOY_DIR"
  exit 1
fi

source "$(dirname "$0")/vps-auth.sh"

exec ssh "${SSH_OPTS[@]}" "$VPS_USER@$VPS_HOST" -- "$@"
