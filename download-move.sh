#!/usr/bin/env bash
# download-move.sh — SSH into the VPS, find completed downloads from the
# downloader service, and move them to Media Server Pro's uploads directory.
#
# The script automatically distinguishes finished files from in-progress ones
# by checking for partial-download markers (.part, .tmp, _remux), zero-byte
# files, and files that are still being actively written to (via lsof/fuser).
#
# Usage:
#   ./download-move.sh                  # copy all completed downloads to MSP uploads
#   ./download-move.sh --dry-run        # preview what would be moved
#   ./download-move.sh --delete         # delete source files after copying (move)
#   ./download-move.sh --list           # just list completed files, don't move
#   ./download-move.sh --help           # show this help
#
# Environment variables (set in shell or .deploy.env):
#   VPS_HOST           SSH host          (required)
#   VPS_USER           SSH user          (default: root)
#   VPS_PORT           SSH port          (default: 22)
#   KEY_FILE           SSH private key   (default: ~/.ssh/id_ed25519)
#   DOWNLOADER_DIR     Downloader dir    (default: /home/deployment/downloader/server/downloads)
#   MSP_DIR            Media Server Pro  (default: /home/deployment/media-server-pro)
#   MEDIA_SUBDIR       MSP subdirectory  (default: uploads — files land in $MSP_DIR/uploads/)

set -euo pipefail

# ── Colour helpers ────────────────────────────────────────────────────────────
RED='\033[0;31m'; GREEN='\033[0;32m'; YELLOW='\033[1;33m'
CYAN='\033[0;36m'; BOLD='\033[1m'; RESET='\033[0m'

info()    { echo -e "${CYAN}[move]${RESET} $*"; }
success() { echo -e "${GREEN}[move]${RESET} $*"; }
warn()    { echo -e "${YELLOW}[move]${RESET} $*"; }
die()     { echo -e "${RED}[move] ERROR:${RESET} $*" >&2; exit 1; }

# ── Load .deploy.env if present ──────────────────────────────────────────────
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
[[ -f "$SCRIPT_DIR/.deploy.env" ]] && source "$SCRIPT_DIR/.deploy.env"

# ── Defaults ──────────────────────────────────────────────────────────────────
VPS_HOST="${VPS_HOST:-}"
VPS_USER="${VPS_USER:-root}"
VPS_PORT="${VPS_PORT:-22}"
KEY_FILE="${KEY_FILE:-$HOME/.ssh/id_ed25519}"
DOWNLOADER_DIR="${DOWNLOADER_DIR:-/opt/downloader}"
MSP_DIR="${MSP_DIR:-/opt/media-server}"
MEDIA_SUBDIR="${MEDIA_SUBDIR:-uploads}"

DRY_RUN=false
LIST_ONLY=false
DELETE_SOURCE=false

while [[ $# -gt 0 ]]; do
  case "$1" in
    --dry-run)   DRY_RUN=true      ; shift ;;
    --list)      LIST_ONLY=true    ; shift ;;
    --delete)    DELETE_SOURCE=true ; shift ;;
    --help|-h)
      sed -n '/^# Usage:/,/^[^#]/p' "$0" | head -n -1
      exit 0
      ;;
    *) die "Unknown option: $1 (use --help)" ;;
  esac
done

[[ -z "$VPS_HOST" ]] && die "VPS_HOST is not set. Export it or add to .deploy.env"

# ── SSH auth setup ────────────────────────────────────────────────────────────
# Reuse the same auth pattern as deploy.sh / vps-auth.sh

if [[ ! -f "$KEY_FILE" ]]; then
  info "Generating SSH key at $KEY_FILE..."
  mkdir -p "$(dirname "$KEY_FILE")"
  ssh-keygen -t ed25519 -f "$KEY_FILE" -N "" -C "mediaserver-deploy"
  echo ""
fi

# Remove passphrase if needed for BatchMode
if ! ssh-keygen -y -P "" -f "$KEY_FILE" &>/dev/null; then
  warn "SSH key has a passphrase — removing it for automated use."
  echo "    Enter the CURRENT key passphrase when prompted:"
  ssh-keygen -p -f "$KEY_FILE" -N ""
  echo ""
fi

# Convert POSIX path for Git Bash / Windows OpenSSH
KEY_FILE_SSH="$KEY_FILE"
if command -v cygpath &>/dev/null 2>&1; then
  KEY_FILE_SSH="$(cygpath -m "$KEY_FILE" 2>/dev/null || echo "$KEY_FILE")"
fi

SSH_OPTS=(-i "$KEY_FILE_SSH" -p "$VPS_PORT" -o StrictHostKeyChecking=accept-new -o BatchMode=yes -o ConnectTimeout=8)
vps() { ssh "${SSH_OPTS[@]}" "$VPS_USER@$VPS_HOST" -- "$@"; }

# Test SSH connectivity
if ! ssh "${SSH_OPTS[@]}" "$VPS_USER@$VPS_HOST" "exit 0" 2>/dev/null; then
  die "Cannot connect to $VPS_USER@$VPS_HOST:$VPS_PORT — check SSH key and host"
fi

DOWNLOADS_PATH="$DOWNLOADER_DIR/server/downloads"
UPLOADS_PATH="$MSP_DIR/$MEDIA_SUBDIR"

echo -e "\n${BOLD}=== Download → Media Server Pro ===${RESET}\n"
info "VPS             : $VPS_USER@$VPS_HOST:$VPS_PORT"
info "Downloads from  : $DOWNLOADS_PATH"
info "Uploads to      : $UPLOADS_PATH"
$DRY_RUN   && warn "DRY RUN — no files will be moved"
$LIST_ONLY && info "LIST ONLY — showing completed files"
echo ""

# ── Run the detection + move logic on the VPS ─────────────────────────────────
# Everything runs in a single SSH session for efficiency.
# The heredoc is unquoted so local variables ($DOWNLOADS_PATH, etc.) are
# expanded before sending; remote variables use \$ to stay literal.

vps "bash -s" <<REMOTE_SCRIPT
set -euo pipefail

DOWNLOADS_PATH="$DOWNLOADS_PATH"
UPLOADS_PATH="$UPLOADS_PATH"
DRY_RUN="$DRY_RUN"
LIST_ONLY="$LIST_ONLY"
DELETE_SOURCE="$DELETE_SOURCE"

# Valid media extensions (video + audio)
MEDIA_EXTENSIONS="mp4|mkv|webm|mov|avi|flv|wmv|m4v|mpg|mpeg|mp3|m4a|opus|ogg|flac|wav|aac"

# ── Verify directories exist ─────────────────────────────────────────────────
if [ ! -d "\$DOWNLOADS_PATH" ]; then
  echo "ERROR: Downloads directory not found: \$DOWNLOADS_PATH"
  exit 1
fi

if [ "\$LIST_ONLY" != "true" ]; then
  mkdir -p "\$UPLOADS_PATH" 2>/dev/null || {
    echo "ERROR: Cannot create uploads directory: \$UPLOADS_PATH"
    exit 1
  }
fi

# ── Get list of open files (being actively written to) ────────────────────────
# lsof is the most reliable way to detect files still being downloaded.
OPEN_FILES=""
USE_FUSER=false
if command -v lsof &>/dev/null; then
  OPEN_FILES=\$(lsof +D "\$DOWNLOADS_PATH" 2>/dev/null | awk 'NR>1 {print \$9}' | sort -u || true)
elif command -v fuser &>/dev/null; then
  USE_FUSER=true
fi

moved=0
skipped=0
total=0
moved_files=""  # track destination paths for targeted chown

# ── Scan all files in the downloads directory ─────────────────────────────────
while IFS= read -r -d '' file; do
  total=\$((total + 1))
  filename=\$(basename "\$file")
  filesize=\$(stat -c%s "\$file" 2>/dev/null || echo 0)

  # 1. Skip partial downloads (.part = yt-dlp in-progress)
  if [[ "\$filename" == *.part ]]; then
    echo "SKIP [partial]   \$filename"
    skipped=\$((skipped + 1))
    continue
  fi

  # 2. Skip temp files (.tmp = HLS concatenation in progress)
  if [[ "\$filename" == *.tmp ]]; then
    echo "SKIP [temp]      \$filename"
    skipped=\$((skipped + 1))
    continue
  fi

  # 3. Skip remux intermediates (_remux in name = ffmpeg remuxing)
  if [[ "\$filename" == *_remux* ]]; then
    echo "SKIP [remux]     \$filename"
    skipped=\$((skipped + 1))
    continue
  fi

  # 4. Skip non-media files (metadata, json, txt, etc.)
  ext="\${filename##*.}"
  ext_lower=\$(echo "\$ext" | tr '[:upper:]' '[:lower:]')
  if ! echo "\$ext_lower" | grep -qE "^(\$MEDIA_EXTENSIONS)\$"; then
    echo "SKIP [non-media] \$filename"
    skipped=\$((skipped + 1))
    continue
  fi

  # 5. Skip zero-byte files (failed or just-created downloads)
  if [ "\$filesize" -eq 0 ]; then
    echo "SKIP [empty]     \$filename"
    skipped=\$((skipped + 1))
    continue
  fi

  # 6. Skip files still being written to (open by another process)
  is_open=false
  if \$USE_FUSER; then
    if fuser "\$file" &>/dev/null 2>&1; then
      is_open=true
    fi
  elif [ -n "\$OPEN_FILES" ]; then
    if echo "\$OPEN_FILES" | grep -qF "\$file"; then
      is_open=true
    fi
  fi

  if \$is_open; then
    echo "SKIP [in-use]    \$filename"
    skipped=\$((skipped + 1))
    continue
  fi

  # 7. Skip files modified in the last 30 seconds (likely still being written)
  mod_age=\$(( \$(date +%s) - \$(stat -c%Y "\$file" 2>/dev/null || echo 0) ))
  if [ "\$mod_age" -lt 30 ]; then
    echo "SKIP [recent]    \$filename (modified \${mod_age}s ago)"
    skipped=\$((skipped + 1))
    continue
  fi

  # ── File passed all checks — it's complete ─────────────────────────────────
  human_size=\$(numfmt --to=iec-i --suffix=B "\$filesize" 2>/dev/null || echo "\${filesize} bytes")

  if [ "\$LIST_ONLY" = "true" ]; then
    echo "READY            \$filename (\$human_size)"
    moved=\$((moved + 1))
    continue
  fi

  if [ "\$DRY_RUN" = "true" ]; then
    echo "WOULD MOVE       \$filename (\$human_size)"
    moved=\$((moved + 1))
    continue
  fi

  # Handle filename collisions in the destination
  dest="\$UPLOADS_PATH/\$filename"
  if [ -f "\$dest" ]; then
    base="\${filename%.*}"
    ext_orig="\${filename##*.}"
    timestamp=\$(date +%Y%m%d_%H%M%S)
    dest="\$UPLOADS_PATH/\${base}_\${timestamp}.\${ext_orig}"
  fi

  # Move or copy the file
  if [ "\$DELETE_SOURCE" = "true" ]; then
    # Try mv first (instant if same filesystem), fall back to cp+rm
    if mv "\$file" "\$dest" 2>/dev/null; then
      echo "MOVED            \$filename → \$(basename "\$dest") (\$human_size)"
    else
      if cp "\$file" "\$dest"; then
        # cp succeeded — report as moved even if the source rm fails
        echo "MOVED            \$filename → \$(basename "\$dest") (\$human_size)"
        rm "\$file" 2>/dev/null || true
      else
        echo "FAILED           \$filename"
        skipped=\$((skipped + 1))
        continue
      fi
    fi
  else
    # Copy, keeping the original in downloads
    if cp "\$file" "\$dest" 2>/dev/null; then
      echo "COPIED           \$filename → \$(basename "\$dest") (\$human_size)"
    else
      echo "FAILED           \$filename"
      skipped=\$((skipped + 1))
      continue
    fi
  fi

  moved=\$((moved + 1))
  moved_files="\$moved_files \$dest"

done < <(find "\$DOWNLOADS_PATH" -maxdepth 1 -type f -print0 2>/dev/null)

# ── Fix ownership so MSP can read the files ──────────────────────────────────
if [ "\$LIST_ONLY" != "true" ] && [ "\$DRY_RUN" != "true" ] && [ "\$moved" -gt 0 ]; then
  MSP_OWNER=\$(stat -c%U "\$UPLOADS_PATH" 2>/dev/null || echo "")
  MSP_GROUP=\$(stat -c%G "\$UPLOADS_PATH" 2>/dev/null || echo "")
  if [ -n "\$MSP_OWNER" ] && [ "\$MSP_OWNER" != "root" ]; then
    chown "\${MSP_OWNER}:\${MSP_GROUP}" "\$UPLOADS_PATH"/* 2>/dev/null || true
  fi
fi

echo ""
echo "SUMMARY: \$total scanned, \$moved completed, \$skipped skipped"
REMOTE_SCRIPT

echo ""
success "Done."
echo ""
